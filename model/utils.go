package model

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"fmt"
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
		// TODO: maybe we can combine updates with same key?
		for key, value := range store {
			switch i {
			case BatchUpdateTypeUserQuota:
				err := increaseUserQuota(key, value)
				if err != nil {
					logger.SysError("failed to batch update user quota: " + err.Error())
				}
			case BatchUpdateTypeTokenQuota:
				err := increaseTokenQuota(key, value)
				if err != nil {
					logger.SysError("failed to batch update token quota: " + err.Error())
				}
			case BatchUpdateTypeUsedQuota:
				updateUserUsedQuota(key, value)
			case BatchUpdateTypeRequestCount:
				updateUserRequestCount(key, value)
			case BatchUpdateTypeChannelUsedQuota:
				updateChannelUsedQuota(key, value)
			}
		}
	}
	logger.SysLog("batch update finished")
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
