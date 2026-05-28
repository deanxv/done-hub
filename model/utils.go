package model

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/redis"
	"done-hub/common/utils"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var batchUpdateStores []map[int]int
var batchUpdateLocks []sync.Mutex

var batchLogStore []*Log
var batchLogLock sync.Mutex

// batchUpdaterStop / batchUpdaterDone 由 InitBatchUpdater 初始化，
// 仅在 BatchUpdateEnabled=true 时有效；用于 graceful shutdown 时停掉后台 ticker。
var (
	batchUpdaterStop chan struct{}
	batchUpdaterDone chan struct{}
)

func init() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	batchUpdaterStop = make(chan struct{})
	batchUpdaterDone = make(chan struct{})
	go func() {
		defer close(batchUpdaterDone)
		ticker := time.NewTicker(time.Duration(config.BatchUpdateInterval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-batchUpdaterStop:
				return
			case <-ticker.C:
				batchUpdate()
				flushBatchLogs()
			}
		}
	}()
}

// StopBatchUpdater 停止后台 ticker 并等待其退出（保证不会再有新的 batchUpdate 启动）。
// 必须在 FlushAllBatches 之前调用，避免 ticker 已 swap map 但未写完 DB 时主线程
// 拿到空 map 就返回，导致那批数据丢失。
// 若 BatchUpdateEnabled=false（未调用过 InitBatchUpdater），则 noop。
func StopBatchUpdater() {
	if batchUpdaterStop == nil {
		return
	}
	close(batchUpdaterStop)
	<-batchUpdaterDone
}

// FlushAllBatches 同步清空所有 batch 队列（quota updates + consume logs），用于进程优雅退出
// 必须在 server.Shutdown 与所有 tracked goroutine 完成之后调用，
// 避免 flush 期间仍有新请求往队列里塞数据
func FlushAllBatches() {
	batchUpdate()
	flushBatchLogs()
}

func AddLogToBatch(log *Log) {
	batchLogLock.Lock()
	defer batchLogLock.Unlock()
	batchLogStore = append(batchLogStore, log)
}

func flushBatchLogs() {
	batchLogLock.Lock()
	logs := batchLogStore
	batchLogStore = nil
	batchLogLock.Unlock()

	if len(logs) == 0 {
		return
	}

	logger.SysLog(fmt.Sprintf("batch inserting %d logs", len(logs)))
	err := BatchInsert(DB, logs)
	if err != nil {
		logger.SysError("failed to batch insert logs: " + err.Error())
	}
}

func addNewRecord(type_ int, id int, value int) {
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	if _, ok := batchUpdateStores[type_][id]; !ok {
		batchUpdateStores[type_][id] = value
	} else {
		batchUpdateStores[type_][id] += value
	}
}

func batchUpdate() {
	logger.SysLog("batch update started")
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		store := batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()

		if len(store) == 0 {
			continue
		}

		// batchAddColumn / batchIncrease* 内部已做 fallback + 错误日志，
		// 调用方无法对失败做更聪明的处理（数据已 swap 出来），所以这些函数不上抛 error。
		switch i {
		case BatchUpdateTypeUserQuota:
			batchIncreaseUserQuota(store)
		case BatchUpdateTypeTokenQuota:
			batchIncreaseTokenQuota(store)
		case BatchUpdateTypeUsedQuota:
			batchAddColumn("users", "used_quota", store)
		case BatchUpdateTypeRequestCount:
			batchAddColumn("users", "request_count", store)
		case BatchUpdateTypeChannelUsedQuota:
			batchAddColumn("channels", "used_quota", store)
		}
	}
	logger.SysLog("batch update finished")
}

// batchUpdateChunkSize controls how many ids per single CASE WHEN statement.
// MySQL prepared-statement parameter limit is 65535. Args/id varies by call site:
//   - batchAddColumn:        3 args/id (CASE when + CASE then + WHERE IN)
//   - batchIncreaseTokenQuota: 5 args/id (two CASE columns × 2 + WHERE IN) + 1 for accessed_time
//
// 500 stays well under the limit for both paths.
const batchUpdateChunkSize = 500

// batchAddColumn issues `UPDATE <table> SET <col> = <col> + CASE id WHEN .. END WHERE id IN (..)`
// in chunks of batchUpdateChunkSize. Each chunk is one round-trip to the DB.
//
// 失败处理：单个 chunk 的批量 SQL 出错时，store 已经从 batchUpdateStores 里 swap 出来回不去，
// 一刀丢 500 条对账数据风险大。降级为逐 id 单条 UPDATE：失败 id 单独打日志，其它 id 仍能写入。
// 单条也失败的（极少数）才真正丢，但每条都有日志可以人工补。返回值已省略——所有错误都在内部
// 落日志，调用方无法对失败做更有意义的恢复。
func batchAddColumn(table, column string, store map[int]int) {
	ids := make([]int, 0, len(store))
	for id := range store {
		ids = append(ids, id)
	}
	for start := 0; start < len(ids); start += batchUpdateChunkSize {
		end := start + batchUpdateChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		sqlStr, args := buildCaseAddSQL(table, column, chunk, store)
		if err := DB.Exec(sqlStr, args...).Error; err != nil {
			logger.SysError(fmt.Sprintf("batch UPDATE %s.%s chunk failed (%d ids, first=%d): %s — falling back to per-id",
				table, column, len(chunk), chunk[0], err.Error()))
			fallbackPerIDAddColumn(table, column, chunk, store)
		}
	}
}

// fallbackPerIDAddColumn runs the same additive update one row at a time.
// Mirrors the original pre-batching behavior so a single bad row can't drop a whole chunk.
func fallbackPerIDAddColumn(table, column string, chunk []int, store map[int]int) {
	singleSQL := fmt.Sprintf("UPDATE %s SET %s = %s + ? WHERE id = ?",
		quotePostgresField(table), quotePostgresField(column), quotePostgresField(column))
	for _, id := range chunk {
		if err := DB.Exec(singleSQL, store[id], id).Error; err != nil {
			logger.SysError(fmt.Sprintf("fallback UPDATE %s.%s id=%d delta=%d failed: %s",
				table, column, id, store[id], err.Error()))
		}
	}
}

// buildCaseAddSQL produces a single batch additive UPDATE statement.
// Identifier quoting (MySQL backtick / PostgreSQL double-quote) is delegated to quotePostgresField.
func buildCaseAddSQL(table, column string, ids []int, store map[int]int) (string, []interface{}) {
	tbl := quotePostgresField(table)
	col := quotePostgresField(column)
	var sb strings.Builder
	args := make([]interface{}, 0, len(ids)*3)
	fmt.Fprintf(&sb, "UPDATE %s SET %s = %s + CASE id ", tbl, col, col)
	for _, id := range ids {
		sb.WriteString("WHEN ? THEN ? ")
		args = append(args, id, store[id])
	}
	sb.WriteString("END WHERE id IN (")
	for i, id := range ids {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args = append(args, id)
	}
	sb.WriteByte(')')
	return sb.String(), args
}

func batchIncreaseUserQuota(store map[int]int) {
	batchAddColumn("users", "quota", store)
	// 故意：无论批量 SQL / fallback 路径成功失败，都对全部 id 清缓存。原 increaseUserQuota 是
	// 写库成功才清，但批量路径下"哪些 id 真的写进去了"的判定开销不值（fallback 内部已经写过）；
	// 多清一次只是让下次读多走一次 DB，正确性不变。别按"成功才清"的单条直觉改回来。
	if config.RedisEnabled {
		for id := range store {
			redis.RedisDel(fmt.Sprintf(UserQuotaCacheKey, id))
		}
	}
}

// batchIncreaseTokenQuota mirrors increaseTokenQuota's three-column update:
//
//	remain_quota += delta, used_quota -= delta, accessed_time = now
//
// done as one CASE WHEN per column per chunk; then a single Pluck to look up
// token keys for cache invalidation. Returns nothing — see batchAddColumn for rationale.
func batchIncreaseTokenQuota(store map[int]int) {
	ids := make([]int, 0, len(store))
	for id := range store {
		ids = append(ids, id)
	}
	now := utils.GetTimestamp()
	tokensTbl := quotePostgresField("tokens")
	remainCol := quotePostgresField("remain_quota")
	usedCol := quotePostgresField("used_quota")
	accessedCol := quotePostgresField("accessed_time")

	// successIDs 收集"通过批量 SQL 成功写入"的 ids；fallback 路径走的 increaseTokenQuota 内部已经
	// 自行清过对应 token 的 cache，外层不应再清一遍，避免每次 chunk 失败都多一次全量 Pluck。
	successIDs := make([]int, 0, len(ids))

	for start := 0; start < len(ids); start += batchUpdateChunkSize {
		end := start + batchUpdateChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		var sb strings.Builder
		// 每个 id 贡献 5 个参数：remain_quota CASE 的 (when, then) + used_quota CASE 的 (when, then) + WHERE IN 的 id。再 +1 个 now。
		args := make([]interface{}, 0, len(chunk)*5+1)
		fmt.Fprintf(&sb, "UPDATE %s SET %s = %s + CASE id ", tokensTbl, remainCol, remainCol)
		for _, id := range chunk {
			sb.WriteString("WHEN ? THEN ? ")
			args = append(args, id, store[id])
		}
		sb.WriteString("END, ")
		fmt.Fprintf(&sb, "%s = %s - CASE id ", usedCol, usedCol)
		for _, id := range chunk {
			sb.WriteString("WHEN ? THEN ? ")
			args = append(args, id, store[id])
		}
		sb.WriteString("END, ")
		fmt.Fprintf(&sb, "%s = ? WHERE id IN (", accessedCol)
		args = append(args, now)
		for i, id := range chunk {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('?')
			args = append(args, id)
		}
		sb.WriteByte(')')

		if err := DB.Exec(sb.String(), args...).Error; err != nil {
			// Same fallback rationale as batchAddColumn: never drop a whole chunk.
			// 复用原 increaseTokenQuota（含 used_quota -= delta + accessed_time + 单 token cache 清理）。
			logger.SysError(fmt.Sprintf("batch UPDATE tokens chunk failed (%d ids, first=%d): %s — falling back to per-id",
				len(chunk), chunk[0], err.Error()))
			for _, id := range chunk {
				if e := increaseTokenQuota(id, store[id]); e != nil {
					logger.SysError(fmt.Sprintf("fallback increaseTokenQuota id=%d delta=%d failed: %s",
						id, store[id], e.Error()))
				}
			}
			continue
		}
		successIDs = append(successIDs, chunk...)
	}

	if config.RedisEnabled && len(successIDs) > 0 {
		// 与 model/user.go:283、model/cache.go:212 同款 Pluck 写法：挂 Model schema 后，GORM 会
		// 帮 reserved word 列名（如 `key`）做 dialect-aware quote。
		var keys []string
		if err := DB.Model(&Token{}).Where("id IN ?", successIDs).Pluck("key", &keys).Error; err == nil {
			for _, k := range keys {
				if k != "" {
					redis.RedisDel(fmt.Sprintf(UserTokensKey, k))
				}
			}
		} else {
			logger.SysError(fmt.Sprintf("batchIncreaseTokenQuota: failed to fetch keys for cache invalidation (%d ids): %s",
				len(successIDs), err.Error()))
		}
	}
}

func BatchInsert[T any](db *gorm.DB, data []T) error {
	batchSize := 200
	for i := 0; i < len(data); i += batchSize {
		end := i + batchSize
		if end > len(data) {
			end = len(data)
		}
		if err := batchInsertWithRetry(db, data[i:end]); err != nil {
			logger.SysError(fmt.Sprintf("batch insert failed after retry, lost %d records: %s", end-i, err.Error()))
		}
	}
	return nil
}

// batchInsertWithRetry 使用二分法进行容错插入
// 当批量插入失败时，将数据二分后分别尝试插入，递归直到单条记录
func batchInsertWithRetry[T any](db *gorm.DB, data []T) error {
	if len(data) == 0 {
		return nil
	}

	err := db.Create(data).Error
	if err == nil {
		return nil
	}

	if len(data) == 1 {
		logger.SysError(fmt.Sprintf("failed to insert single record: %s", err.Error()))
		return err
	}

	mid := len(data) / 2
	logger.SysLog(fmt.Sprintf("batch insert failed, splitting %d records into two halves", len(data)))

	err1 := batchInsertWithRetry(db, data[:mid])
	err2 := batchInsertWithRetry(db, data[mid:])

	if err1 != nil {
		return err1
	}
	return err2
}
