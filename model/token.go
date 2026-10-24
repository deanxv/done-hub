package model

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/database"
	"done-hub/common/logger"
	"done-hub/common/redis"
	"done-hub/common/stmp"
	"done-hub/common/utils"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

var (
	ErrTokenNotFound          = errors.New("令牌不存在")
	ErrTokenExpired           = errors.New("令牌已过期")
	ErrTokenQuotaExhausted    = errors.New("令牌额度已用尽")
	ErrTokenStatusUnavailable = errors.New("令牌状态不可用")
	ErrTokenInvalid           = errors.New("无效的令牌")
	ErrTokenQuotaGet          = errors.New("获取令牌额度失败")
)

type Token struct {
	Id             int            `json:"id"`
	UserId         int            `json:"user_id"`
	Key            string         `json:"key" gorm:"type:varchar(59);uniqueIndex"`
	Status         int            `json:"status" gorm:"default:1"`
	Name           string         `json:"name" gorm:"index" `
	CreatedTime    int64          `json:"created_time" gorm:"bigint"`
	AccessedTime   int64          `json:"accessed_time" gorm:"bigint"`
	ExpiredTime    int64          `json:"expired_time" gorm:"bigint;default:-1"` // -1 means never expired
	RemainQuota    int            `json:"remain_quota" gorm:"default:0"`
	UnlimitedQuota bool           `json:"unlimited_quota" gorm:"default:false"`
	UsedQuota      int            `json:"used_quota" gorm:"default:0"` // used quota
	Group          string         `json:"group" gorm:"default:''"`
	BackupGroup    string         `json:"backup_group" gorm:"default:''"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`

	Setting database.JSONType[TokenSetting] `json:"setting" form:"setting" gorm:"type:json"`
}

var allowedTokenOrderFields = map[string]bool{
	"id":           true,
	"name":         true,
	"status":       true,
	"expired_time": true,
	"created_time": true,
	"remain_quota": true,
	"used_quota":   true,
}

// 添加 AfterCreate 钩子方法
func (token *Token) AfterCreate(tx *gorm.DB) (err error) {
	tokenKey, err := common.GenerateToken(token.Id, token.UserId)
	if err != nil {
		return err
	}

	// 同步到内存中的 token 实例，使 caller 可以在 Insert 返回后直接读取 Key
	token.Key = tokenKey

	// 更新 key 字段
	return tx.Model(token).Update("key", tokenKey).Error
}

type TokenSetting struct {
	Heartbeat  HeartbeatSetting `json:"heartbeat,omitempty"`
	Limits     LimitsConfig     `json:"limits,omitempty"`
	BillingTag *string          `json:"billing_tag,omitempty"` // 费用标签，用于按分组统计费用，仅可信内部员工和管理员可见
}

type HeartbeatSetting struct {
	Enabled        bool `json:"enabled"`
	TimeoutSeconds int  `json:"timeout_seconds"`
}

type LimitsConfig struct {
	LimitModelSetting LimitModelSetting `json:"limit_model_setting,omitempty"`
	LimitsIPSetting   LimitsIPSetting   `json:"limits_ip_setting,omitempty"`
}

type LimitModelSetting struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models"`
}

type LimitsIPSetting struct {
	Enabled   bool     `json:"enabled"`
	Whitelist []string `json:"whitelist"`
}

func GetUserTokensList(userId int, params *GenericParams) (*DataResult[Token], error) {
	var tokens []*Token
	db := DB.Where("user_id = ?", userId)

	if params.Keyword != "" {
		db = db.Where("name LIKE ?", params.Keyword+"%")
	}

	return PaginateAndOrder(db, &params.PaginationParams, &tokens, allowedTokenOrderFields)
}

func GetUserTokenGroupSymbols(userId int) ([]string, error) {
	var tokens []Token
	err := DB.Where("user_id = ?", userId).Select("group", "backup_group").Find(&tokens).Error
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	for _, t := range tokens {
		if t.Group != "" {
			seen[t.Group] = struct{}{}
		}
		if t.BackupGroup != "" {
			seen[t.BackupGroup] = struct{}{}
		}
	}

	symbols := make([]string, 0, len(seen))
	for s := range seen {
		symbols = append(symbols, s)
	}
	return symbols, nil
}

// AdminSearchTokensParams 管理员搜索令牌的参数
type AdminSearchTokensParams struct {
	GenericParams
	UserId  int    `form:"user_id"`
	TokenId int    `form:"token_id"`
	Key     string `form:"key"`
}

// TokenWithOwner 包含令牌信息和所属用户信息
type TokenWithOwner struct {
	Token
	OwnerName string `json:"owner_name"` // 用户名称（优先显示 display_name，其次 username）
}

// GetTokensListByAdmin 管理员查询令牌列表（可按用户ID或令牌ID查询）
func GetTokensListByAdmin(params *AdminSearchTokensParams) (*DataResult[TokenWithOwner], error) {
	var tokens []*Token
	db := DB.Model(&Token{})

	if params.UserId > 0 {
		db = db.Where("user_id = ?", params.UserId)
	}

	if params.TokenId > 0 {
		db = db.Where("id = ?", params.TokenId)
	}

	if params.Key != "" {
		keyCol := "`key`"
		if common.UsingPostgreSQL {
			keyCol = `"key"`
		}
		key := strings.TrimPrefix(params.Key, "sk-")
		db = db.Where(keyCol+" LIKE ?", key+"%")
	}

	if params.Keyword != "" {
		db = db.Where("name LIKE ?", params.Keyword+"%")
	}

	result, err := PaginateAndOrder(db, &params.PaginationParams, &tokens, allowedTokenOrderFields)
	if err != nil {
		return nil, err
	}

	userIds := make([]int, 0)
	userIdMap := make(map[int]bool)
	for _, token := range *result.Data {
		if !userIdMap[token.UserId] {
			userIds = append(userIds, token.UserId)
			userIdMap[token.UserId] = true
		}
	}

	userNameMap := make(map[int]string)
	if len(userIds) > 0 {
		var users []User
		DB.Select("id, username, display_name").Where("id IN ?", userIds).Find(&users)
		for _, user := range users {
			name := user.DisplayName
			if name == "" {
				name = user.Username
			}
			userNameMap[user.Id] = name
		}
	}

	tokensWithOwner := make([]*TokenWithOwner, len(*result.Data))
	for i, token := range *result.Data {
		tokensWithOwner[i] = &TokenWithOwner{
			Token:     *token,
			OwnerName: userNameMap[token.UserId],
		}
	}

	return &DataResult[TokenWithOwner]{
		Data:       &tokensWithOwner,
		TotalCount: result.TotalCount,
	}, nil
}

func GetTokenModel(key string) (token *Token, err error) {
	if key == "" {
		return nil, ErrTokenInvalid
	}

	var userId int
	var tokenId int
	validUser := false

	switch len(key) {
	case 48:
		validUser = true
		if config.RedisEnabled {
			exists, _ := redis.RedisSIsMember(OldUserTokensCacheKey, key)
			if !exists {
				return nil, ErrTokenInvalid
			}
		}
	case 59:
		tokenId, userId, err = common.ValidateToken(key)
		if err != nil || userId == 0 || tokenId == 0 {
			return nil, ErrTokenInvalid
		}
		if userEnabled, err := CacheIsUserEnabled(userId); err != nil || !userEnabled {
			return nil, ErrTokenInvalid
		}
	default:
		return nil, ErrTokenInvalid
	}

	token, err = CacheGetTokenByKey(key)
	if err != nil {
		maskedKey := key[:3] + "*********" + key[len(key)-3:]
		logger.SysError(fmt.Sprintf("DB Not Found: userId=%d, tokenId=%d, key=%s, err=%s", userId, tokenId, maskedKey, err.Error()))
		return nil, ErrTokenInvalid
	}

	if validUser {
		if userEnabled, err := CacheIsUserEnabled(token.UserId); err != nil || !userEnabled {
			return nil, ErrTokenInvalid
		}
	}

	return token, nil
}

func ValidateUserToken(key string) (token *Token, err error) {
	token, err = GetTokenModel(key)
	if err != nil {
		return nil, err
	}

	if token.Status != config.TokenStatusEnabled {
		switch token.Status {
		case config.TokenStatusExhausted:
			return nil, ErrTokenQuotaExhausted
		case config.TokenStatusExpired:
			return nil, ErrTokenExpired
		default:
			return nil, ErrTokenStatusUnavailable
		}
	}

	if token.ExpiredTime != -1 && token.ExpiredTime < utils.GetTimestamp() {
		return nil, ErrTokenExpired
	}

	if !token.UnlimitedQuota {
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			if !config.RedisEnabled {
				// in this case, we can make sure the token is exhausted
				token.Status = config.TokenStatusExhausted
				err := token.SelectUpdate()
				if err != nil {
					logger.SysError("failed to update token status" + err.Error())
				}
			}
			return nil, ErrTokenQuotaExhausted
		}
	}

	return token, nil
}

func GetTokenByIds(id int, userId int) (*Token, error) {
	if id == 0 || userId == 0 {
		return nil, errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	var err error = nil
	err = DB.First(&token, "id = ? and user_id = ?", id, userId).Error
	return &token, err
}

func GetTokenById(id int) (*Token, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	var token Token
	err := DB.First(&token, "id = ?", id).Error
	return &token, err
}

func GetTokenByName(name string, userId int) (*Token, error) {
	if name == "" {
		return nil, errors.New("name 为空！")
	}
	token := Token{Name: name}
	var err error = nil
	err = DB.First(&token, "user_id = ? and name = ?", userId, name).Error
	return &token, err
}

func GetTokenByKey(key string) (*Token, error) {
	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}

	var token Token

	err := DB.Where(keyCol+" = ?", key).First(&token).Error
	return &token, err
}

func (token *Token) Insert() error {
	err := DB.Create(token).Error
	return err
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (token *Token) Update() error {
	err := DB.Model(token).Select("name", "status", "expired_time", "remain_quota", "unlimited_quota", "group", "backup_group", "setting").Updates(token).Error
	// 防止Redis缓存不生效，直接删除
	if err == nil && config.RedisEnabled {
		redis.RedisDel(fmt.Sprintf(UserTokensKey, token.Key))
	}

	return err
}

// UpdateByAdmin 管理员更新token，支持更新user_id字段
func (token *Token) UpdateByAdmin() error {
	err := DB.Model(token).Select("user_id", "name", "status", "expired_time", "remain_quota", "unlimited_quota", "group", "backup_group", "setting").Updates(token).Error
	if err == nil && config.RedisEnabled {
		redis.RedisDel(fmt.Sprintf(UserTokensKey, token.Key))
	}

	return err
}

func (token *Token) SelectUpdate() error {
	// This can update zero values
	err := DB.Model(token).Select("accessed_time", "status").Updates(token).Error

	// 清除缓存
	if err == nil && config.RedisEnabled {
		redis.RedisDel(fmt.Sprintf(UserTokensKey, token.Key))
	}

	return err
}

func (token *Token) Delete() error {
	err := DB.Delete(token).Error
	return err
}

func DeleteTokenById(id int, userId int) (err error) {
	// Why we need userId here? In case user want to delete other's token.
	if id == 0 || userId == 0 {
		return errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	err = DB.Where(token).First(&token).Error
	if err != nil {
		return err
	}
	err = token.Delete()

	if err == nil && config.RedisEnabled {
		redis.RedisDel(fmt.Sprintf(UserTokensKey, token.Key))
	}

	return err

}

// DeleteTokenByIdAdmin 管理员删除任意token（不限制所属用户）
func DeleteTokenByIdAdmin(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	token := Token{Id: id}
	err = DB.Where("id = ?", id).First(&token).Error
	if err != nil {
		return err
	}
	err = token.Delete()

	if err == nil && config.RedisEnabled {
		redis.RedisDel(fmt.Sprintf(UserTokensKey, token.Key))
	}

	return err
}

func IncreaseTokenQuota(id int, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if config.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, quota)
		return nil
	}
	return increaseTokenQuota(id, quota)
}

func increaseTokenQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota - ?", quota),
			"accessed_time": utils.GetTimestamp(),
		},
	).Error

	// 清除缓存
	if err == nil && config.RedisEnabled {
		var key string
		keyCol := "`key`"
		if common.UsingPostgreSQL {
			keyCol = `"key"`
		}
		if getErr := DB.Model(&Token{}).Where("id = ?", id).Select(keyCol).Scan(&key).Error; getErr == nil && key != "" {
			redis.RedisDel(fmt.Sprintf(UserTokensKey, key))
		}
	}

	return err
}

func DecreaseTokenQuota(id int, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if config.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, -quota)
		return nil
	}
	return decreaseTokenQuota(id, quota)
}

func decreaseTokenQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": utils.GetTimestamp(),
		},
	).Error

	// 清除缓存
	if err == nil && config.RedisEnabled {
		var key string
		keyCol := "`key`"
		if common.UsingPostgreSQL {
			keyCol = `"key"`
		}
		if getErr := DB.Model(&Token{}).Where("id = ?", id).Select(keyCol).Scan(&key).Error; getErr == nil && key != "" {
			redis.RedisDel(fmt.Sprintf(UserTokensKey, key))
		}
	}

	return err
}

// AddTokenUsedQuota 仅按带符号增量调整令牌的 used_quota（用量计量），不触碰 remain_quota（额度上限）。
// 用于无限额度令牌：它没有额度上限的概念，但仍需统计真实用量。quota 为正表示消费、为负表示退还。
func AddTokenUsedQuota(id int, quota int) (err error) {
	if config.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenUsedQuota, id, quota)
		return nil
	}
	return addTokenUsedQuota(id, quota)
}

func addTokenUsedQuota(id int, quota int) (err error) {
	err = DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": utils.GetTimestamp(),
		},
	).Error

	// 清除缓存
	if err == nil && config.RedisEnabled {
		var key string
		keyCol := "`key`"
		if common.UsingPostgreSQL {
			keyCol = `"key"`
		}
		if getErr := DB.Model(&Token{}).Where("id = ?", id).Select(keyCol).Scan(&key).Error; getErr == nil && key != "" {
			redis.RedisDel(fmt.Sprintf(UserTokensKey, key))
		}
	}

	return err
}

func PreConsumeTokenQuota(tokenId int, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	token, err := GetTokenById(tokenId)
	if err != nil {
		return err
	}
	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return errors.New("令牌额度不足")
	}
	userQuota, remindThreshold, remindEnabled, err := GetUserQuotaWithRemindSetting(token.UserId)
	if err != nil {
		return err
	}
	if userQuota < quota {
		return errors.New("用户额度不足")
	}
	if token.UnlimitedQuota {
		// 无限额度令牌没有上限，只累计用量，不扣减 remain_quota
		err = AddTokenUsedQuota(tokenId, quota)
	} else {
		err = DecreaseTokenQuota(tokenId, quota)
	}
	if err != nil {
		return err
	}
	err = DecreaseUserQuota(token.UserId, quota)
	if err != nil {
		return err
	}
	notifyQuotaWarningIfNeeded(token.UserId, userQuota, quota, remindThreshold, remindEnabled)
	return nil
}

// 额度提醒语义是「状态跨越」事件：余额跌到阈值之下（或用尽）发一封，充值明显回到阈值之上后再次跌破再发一封。
// 实现为带滞回的状态机：每类告警一把「已发送」锁，SETNX 拿到才发；余额回到「重新上膛水位」
// （阈值 + 余量，或退回余量之上）时主动释放锁。滞回基于余额状态而非单笔扣减量，小步扣费同样生效。
//   - quotaWarningSentTTL：「已发送」锁的保底过期，余额长期低于阈值（例如阈值设得比余额高）时每天兜底一封；
//   - quotaWarningBurstTTL：纯并发去重，兜住跨节点 release/acquire 的极小概率竞态；代价是
//     重新上膛后 10 分钟内再次跌破不会重发，此时用户刚收到过同类邮件，可以接受。
const (
	quotaWarningSentTTL  = 24 * time.Hour
	quotaWarningBurstTTL = 10 * time.Minute
)

var (
	// quotaWarningLocalDedup 是进程内的去重兜底：Redis 未启用或 SETNX 抖动失败时仍能避免逐请求刷邮件。
	quotaWarningLocalDedup = struct {
		sync.Mutex
		expireAt map[string]time.Time
	}{expireAt: map[string]time.Time{}}

	// quotaWarningNow 为去重窗口的时钟源，变量化以便测试推进时间。
	quotaWarningNow = time.Now

	// sendQuotaWarningEmailFunc 为实际发送入口（异步），变量化以便测试替换成同步收集器。
	sendQuotaWarningEmailFunc = func(userId int, quota int, noMoreQuota bool) {
		go sendQuotaWarningEmail(userId, quota, noMoreQuota)
	}
)

func quotaWarningSentKey(userId int, noMoreQuota bool) string {
	return fmt.Sprintf("notify_lock:quota_warning:%d:%t", userId, noMoreQuota)
}

func quotaWarningBurstKey(userId int, noMoreQuota bool) string {
	return fmt.Sprintf("notify_lock:quota_warning:burst:%d:%t", userId, noMoreQuota)
}

// quotaWarningLocalAcquire 在进程内对 key 做一次性占位，窗口内重复调用返回 false。
// 命中已有占位时直接返回，不做扫描：低余额用户的每次请求都会走到这里，持锁下的 O(n) 清理
// 只放在真正插入（状态跨越，很少发生）的路径上。
func quotaWarningLocalAcquire(key string, ttl time.Duration, now time.Time) bool {
	d := &quotaWarningLocalDedup
	d.Lock()
	defer d.Unlock()
	if exp, ok := d.expireAt[key]; ok && now.Before(exp) {
		return false
	}
	for k, exp := range d.expireAt {
		if !now.Before(exp) {
			delete(d.expireAt, k)
		}
	}
	d.expireAt[key] = now.Add(ttl)
	return true
}

func quotaWarningLocalRelease(key string) {
	d := &quotaWarningLocalDedup
	d.Lock()
	delete(d.expireAt, key)
	d.Unlock()
}

// quotaWarningLocalTTL 决定进程内占位的寿命：Redis 未启用时本地就是唯一状态，用完整 ttl；
// Redis 启用时跨节点状态以 Redis 为准，本地只兜 SETNX 抖动，不能长于并发去重窗口，
// 否则其它节点执行的 rearm（只删 Redis）对本节点不生效，用户会被本地残留的 24h 占位压住。
func quotaWarningLocalTTL(ttl time.Duration) time.Duration {
	if config.RedisEnabled {
		return min(ttl, quotaWarningBurstTTL)
	}
	return ttl
}

// acquireQuotaWarningLock 先过进程内窗口，再过 Redis 跨节点窗口。
// Redis 未启用时只靠进程内去重；SETNX 抖动失败时宁可多发也不静默丢。
func acquireQuotaWarningLock(key string, ttl time.Duration) bool {
	if !quotaWarningLocalAcquire(key, quotaWarningLocalTTL(ttl), quotaWarningNow()) {
		return false
	}
	if !config.RedisEnabled {
		return true
	}
	ok, err := redis.RedisSetNX(key, "1", ttl)
	if err != nil {
		logger.SysError(fmt.Sprintf("quota warning notify dedup SETNX failed (key=%s): %v", key, err))
		return true
	}
	return ok
}

// shouldSendQuotaWarningNotify 决定本次告警是否放行：先拿「已发送」锁，再过并发去重锁。
func shouldSendQuotaWarningNotify(userId int, noMoreQuota bool) bool {
	if !acquireQuotaWarningLock(quotaWarningSentKey(userId, noMoreQuota), quotaWarningSentTTL) {
		return false
	}
	return acquireQuotaWarningLock(quotaWarningBurstKey(userId, noMoreQuota), quotaWarningBurstTTL)
}

// rearmQuotaWarning 在余额回到安全水位时释放对应的「已发送」锁，让下一次跌破能再次发送。
// 充值发生在扣费路径之外，这里观察不到「回升」这个瞬间，只能在每次余额健康的扣费后无条件释放；
// 本地是一次 map delete，Redis 是一次 DEL，相对扣费路径本身的 DB 与缓存写入可忽略。
func rearmQuotaWarning(userId int, low bool, exhausted bool) {
	keys := make([]string, 0, 2)
	if low {
		keys = append(keys, quotaWarningSentKey(userId, false))
	}
	if exhausted {
		keys = append(keys, quotaWarningSentKey(userId, true))
	}
	if len(keys) == 0 {
		return
	}
	for _, k := range keys {
		quotaWarningLocalRelease(k)
	}
	if config.RedisEnabled {
		if err := redis.RedisDel(keys...); err != nil {
			logger.SysError(fmt.Sprintf("quota warning notify rearm failed (user=%d): %v", userId, err))
		}
	}
}

// quotaWarningRefundMargin 是「已用尽」告警的重新上膛余量：每次结算都可能把预扣多出的部分退回来，
// 余额在 0 附近来回抖动是常态；退回金额的上限正是预扣固定开销 PreConsumedQuota。
func quotaWarningRefundMargin() int {
	if config.PreConsumedQuota < 0 {
		return 0
	}
	return config.PreConsumedQuota
}

// quotaWarningThresholdMargin 是「即将用尽」告警的重新上膛余量：在退回上限的基础上再按阈值的 10% 放大，
// 余额必须明显回到阈值之上才重新上膛，避免阈值附近小幅抖动重复触发。
func quotaWarningThresholdMargin(threshold int) int {
	margin := quotaWarningRefundMargin()
	if threshold/10 > margin {
		margin = threshold / 10
	}
	return margin
}

// quotaWarningState 描述扣费后余额所处的状态。
type quotaWarningState int

const (
	quotaWarningStateSafe      quotaWarningState = iota // 高于阈值+余量：两类告警都重新上膛
	quotaWarningStateBand                               // 阈值附近的滞回带：什么都不做
	quotaWarningStateLow                                // 低于阈值但未用尽：发「即将用尽」；「已用尽」若高于退回余量则上膛
	quotaWarningStateExhausted                          // 用尽：发「已用尽」
)

// evaluateQuotaWarning 根据扣费后余额判定状态；lowRearm 表示余额是否高到足以给「即将用尽」重新上膛，
// exhaustedRearm 同理针对「已用尽」。两者互相独立，大阈值不会放大「已用尽」的上膛水位。
func evaluateQuotaWarning(quotaAfter, threshold int) (state quotaWarningState, lowRearm bool, exhaustedRearm bool) {
	exhaustedRearm = quotaAfter > quotaWarningRefundMargin()
	lowRearm = quotaAfter >= threshold+quotaWarningThresholdMargin(threshold)
	switch {
	case quotaAfter <= 0:
		state = quotaWarningStateExhausted
	case quotaAfter < threshold:
		state = quotaWarningStateLow
	case lowRearm:
		state = quotaWarningStateSafe
	default:
		state = quotaWarningStateBand
	}
	return state, lowRearm, exhaustedRearm
}

// notifyQuotaWarningIfNeeded 在一次成功扣费后评估是否需要发额度提醒。
// quotaBefore 为扣费前余额、delta 为本次扣减量；threshold 为用户阈值（nil 回退全局），enabled 为用户开关。
func notifyQuotaWarningIfNeeded(userId int, quotaBefore int, delta int, threshold *int, enabled bool) {
	if delta <= 0 || !config.QuotaRemindEnabled || !enabled {
		return
	}
	effective := config.QuotaRemindThreshold
	if threshold != nil {
		effective = *threshold
	}
	quotaAfter := quotaBefore - delta
	state, lowRearm, exhaustedRearm := evaluateQuotaWarning(quotaAfter, effective)
	rearmQuotaWarning(userId, lowRearm, exhaustedRearm)
	switch state {
	case quotaWarningStateExhausted:
		if shouldSendQuotaWarningNotify(userId, true) {
			// 最终结算可能把余额扣成负数，邮件里不展示负余额
			sendQuotaWarningEmailFunc(userId, max(quotaAfter, 0), true)
		}
	case quotaWarningStateLow:
		if shouldSendQuotaWarningNotify(userId, false) {
			sendQuotaWarningEmailFunc(userId, quotaAfter, false)
		}
	}
}

func sendQuotaWarningEmail(userId int, userQuota int, noMoreQuota bool) {
	user := User{Id: userId}

	if err := user.FillUserById(); err != nil {
		logger.SysError("failed to fetch user email: " + err.Error())
		return
	}

	if user.Email == "" {
		logger.SysError("user email is empty")
		return
	}

	userName := user.DisplayName
	if userName == "" {
		userName = user.Username
	}

	err := stmp.SendQuotaWarningCodeEmail(userName, user.Email, userQuota, noMoreQuota)

	if err != nil {
		logger.SysError("failed to send email" + err.Error())
	}
}

// PostConsumeTokenQuotaWithInfo 消费 token 配额，直接使用传入的 userId 和 unlimitedQuota，不再查询 token 表；
// 额度提醒全局开关开启时会额外读一次 users 单行，用于扣费后的提醒判断。
func PostConsumeTokenQuotaWithInfo(tokenId int, userId int, unlimitedQuota bool, quota int) (err error) {
	if quota == 0 {
		return nil
	}
	if quota > 0 {
		// 最终结算是余额真正跌破阈值的地方，提醒判断必须挂在这里；只有全局开关开启时才多读一次用户行。
		var (
			quotaBefore     int
			remindThreshold *int
			remindEnabled   bool
		)
		if config.QuotaRemindEnabled {
			quotaBefore, remindThreshold, remindEnabled, err = GetUserQuotaWithRemindSetting(userId)
			if err != nil {
				logger.SysError(fmt.Sprintf("failed to load quota remind setting (user=%d): %v", userId, err))
				remindEnabled = false
			}
		}
		err = DecreaseUserQuota(userId, quota)
		if err == nil {
			notifyQuotaWarningIfNeeded(userId, quotaBefore, quota, remindThreshold, remindEnabled)
		}
	} else {
		err = IncreaseUserQuota(userId, -quota)
	}
	if err != nil {
		return err
	}
	if unlimitedQuota {
		// 无限额度令牌没有上限，只按带符号增量调整用量，不触碰 remain_quota
		err = AddTokenUsedQuota(tokenId, quota)
	} else if quota > 0 {
		err = DecreaseTokenQuota(tokenId, quota)
	} else {
		err = IncreaseTokenQuota(tokenId, -quota)
	}
	if err != nil {
		return err
	}
	return nil
}
