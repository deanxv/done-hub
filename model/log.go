package model

import (
	"context"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Log struct {
	Id               int    `json:"id"`
	UserId           int    `json:"user_id" gorm:"index;index:idx_user_created_at"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_type;index:idx_user_created_at"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index:index_username_model_name,priority:2;default:''"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota            int    `json:"quota" gorm:"default:0"`
	CostQuota        int    `json:"cost_quota" gorm:"default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	ChannelId        int    `json:"channel_id" gorm:"index"`
	RequestTime      int    `json:"request_time" gorm:"default:0"`
	IsStream         bool   `json:"is_stream" gorm:"default:false"`
	SourceIp         string `json:"source_ip" gorm:"default:''"`
	RequestId        string `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	// 不建索引：管理员偶发排障用，且查询恒带 created_at 范围（前端默认当天），由
	// idx_created_at_type 收窄后再过滤即可；口径同样可过滤的 source_ip。logs 是写入最热的表，
	// 不为低频查询摊索引维护成本。若某部署确实高频按上游 ID 反查，加回 index 即可。
	UpstreamRequestId string                             `json:"upstream_request_id" gorm:"type:varchar(128);default:''"`
	Metadata          datatypes.JSONType[map[string]any] `json:"metadata" gorm:"type:json"`

	Channel *Channel `json:"channel" gorm:"foreignKey:Id;references:ChannelId"`
}

const (
	LogTypeUnknown = iota
	LogTypeTopup
	LogTypeConsume
	LogTypeManage
	LogTypeSystem
)

func RecordQuotaLog(userId int, logType int, quota int, ip string, content string) {
	if logType == LogTypeConsume && !config.LogConsumeEnabled {
		return
	}
	username, _ := CacheGetUsername(userId)
	log := &Log{
		UserId:    userId,
		Username:  username,
		Quota:     quota,
		CreatedAt: utils.GetTimestamp(),
		Type:      logType,
		SourceIp:  ip,
		Content:   content,
	}
	err := DB.Create(log).Error
	if err != nil {
		logger.SysError("failed to record log: " + err.Error())
	}
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !config.LogConsumeEnabled {
		return
	}
	username, _ := CacheGetUsername(userId)

	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: utils.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := DB.Create(log).Error
	if err != nil {
		logger.SysError("failed to record log: " + err.Error())
	}
}

// RecordLogWithTx 在指定事务中记录日志
func RecordLogWithTx(tx *gorm.DB, userId int, logType int, content string) {
	if logType == LogTypeConsume && !config.LogConsumeEnabled {
		return
	}

	// 在事务中查询用户名，避免事务未提交时查询不到用户
	var username string
	err := tx.Model(&User{}).Where("id = ?", userId).Select("username").Find(&username).Error
	if err != nil {
		logger.SysError("failed to get username in tx: " + err.Error())
		// 如果事务中查询失败，尝试从缓存获取
		username, _ = CacheGetUsername(userId)
	}

	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: utils.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err = tx.Create(log).Error
	if err != nil {
		logger.SysError("failed to record log with tx: " + err.Error())
	}
}

func RecordConsumeLog(
	ctx context.Context,
	userId int,
	channelId int,
	promptTokens int,
	completionTokens int,
	modelName string,
	tokenName string,
	quota int,
	costQuota int,
	content string,
	requestTime int,
	isStream bool,
	metadata map[string]any,
	sourceIp string) {
	logger.LogInfo(ctx, fmt.Sprintf("record consume log: userId=%d, channelId=%d, promptTokens=%d, completionTokens=%d, modelName=%s, tokenName=%s, quota=%d, content=%s ,sourceIp=%s", userId, channelId, promptTokens, completionTokens, modelName, tokenName, quota, content, sourceIp))
	if !config.LogConsumeEnabled {
		return
	}

	username, _ := CacheGetUsername(userId)

	// request id 由 middleware 注入 ctx；upstream request id 由 provider 暂存、
	// 经 relay_util.WithUpstreamRequestID 带入。两者缺失时为空串。
	requestId, _ := ctx.Value(logger.RequestIdKey).(string)
	upstreamRequestId, _ := ctx.Value(config.GinUpstreamRequestIdKey).(string)
	// 上游返回的超长 header 会让整条计费日志插入失败，按列宽 varchar(128) 做 rune 安全截断。
	if len(upstreamRequestId) > 128 {
		if r := []rune(upstreamRequestId); len(r) > 128 {
			upstreamRequestId = string(r[:128])
		}
	}

	log := &Log{
		UserId:            userId,
		Username:          username,
		CreatedAt:         utils.GetTimestamp(),
		Type:              LogTypeConsume,
		Content:           content,
		PromptTokens:      promptTokens,
		CompletionTokens:  completionTokens,
		TokenName:         tokenName,
		ModelName:         modelName,
		Quota:             quota,
		CostQuota:         costQuota,
		ChannelId:         channelId,
		RequestTime:       requestTime,
		IsStream:          isStream,
		SourceIp:          sourceIp,
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
	}

	if metadata != nil {
		log.Metadata = datatypes.NewJSONType(metadata)
	}

	if config.BatchUpdateEnabled {
		AddLogToBatch(log)
	} else {
		err := DB.Create(log).Error
		if err != nil {
			logger.LogError(ctx, "failed to record log: "+err.Error())
		}
	}
}

type LogsListParams struct {
	PaginationParams
	LogType           int    `form:"log_type"`
	StartTimestamp    int64  `form:"start_timestamp"`
	EndTimestamp      int64  `form:"end_timestamp"`
	ModelName         string `form:"model_name"`
	Username          string `form:"username"`
	TokenName         string `form:"token_name"`
	ChannelId         int    `form:"channel_id"`
	SourceIp          string `form:"source_ip"`
	RequestId         string `form:"request_id"`
	UpstreamRequestId string `form:"upstream_request_id"`
}

var allowedLogsOrderFields = map[string]bool{
	"created_at": true,
	"channel_id": true,
	"user_id":    true,
	"token_name": true,
	"model_name": true,
	"type":       true,
	"source_ip":  true,
}

// 用户侧排序白名单：不含 channel_id（管理员维度，虽被 Omit 不返回值，
// 但 ORDER BY 仍可按隐藏渠道给自己的请求分组排序，构成侧信道），
// 也不含 user_id（self 路径下恒为常量，无排序意义）。
var allowedUserLogsOrderFields = map[string]bool{
	"created_at": true,
	"token_name": true,
	"model_name": true,
	"type":       true,
	"source_ip":  true,
}

func GetLogsList(params *LogsListParams) (*DataResult[Log], error) {
	var tx *gorm.DB
	var logs []*Log

	tx = DB.Preload("Channel", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, name")
	})

	if params.LogType != LogTypeUnknown {
		tx = tx.Where("type = ?", params.LogType)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.Username != "" {
		tx = tx.Where("username = ?", params.Username)
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if params.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.SourceIp != "" {
		tx = tx.Where("source_ip = ?", params.SourceIp)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	if params.UpstreamRequestId != "" {
		tx = tx.Where("upstream_request_id = ?", params.UpstreamRequestId)
	}

	return PaginateAndOrder[Log](tx, &params.PaginationParams, &logs, allowedLogsOrderFields)
}

// GetAllLogsList returns all logs matching the criteria without pagination (for export)
func GetAllLogsList(params *LogsListParams) ([]*Log, error) {
	var logs []*Log

	tx := DB.Preload("Channel", func(db *gorm.DB) *gorm.DB {
		return db.Select("id, name")
	})

	if params.LogType != LogTypeUnknown {
		tx = tx.Where("type = ?", params.LogType)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.Username != "" {
		tx = tx.Where("username = ?", params.Username)
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if params.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.SourceIp != "" {
		tx = tx.Where("source_ip = ?", params.SourceIp)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	if params.UpstreamRequestId != "" {
		tx = tx.Where("upstream_request_id = ?", params.UpstreamRequestId)
	}

	// Apply ordering
	if params.Order != "" {
		orderFields := strings.Split(params.Order, ",")
		for _, field := range orderFields {
			field = strings.TrimSpace(field)
			desc := strings.HasPrefix(field, "-")
			if desc {
				field = field[1:]
			}
			if allowedLogsOrderFields[field] {
				if desc {
					field = field + " DESC"
				}
				tx = tx.Order(field)
			}
		}
	} else {
		// Default ordering
		tx = tx.Order("id DESC")
	}

	err := tx.Find(&logs).Error
	return logs, err
}

func GetUserLogsList(userId int, params *LogsListParams) (*DataResult[Log], error) {
	var logs []*Log

	tx := DB.Where("user_id = ?", userId).Omit("id", "cost_quota", "channel_id", "upstream_request_id")

	if params.LogType != LogTypeUnknown {
		tx = tx.Where("type = ?", params.LogType)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	// upstream_request_id 属管理员维度，不在用户侧过滤（口径同 channel_id，由 GetLogsSelfStat 清空保对齐）

	result, err := PaginateAndOrder[Log](tx, &params.PaginationParams, &logs, allowedUserLogsOrderFields)
	if err != nil {
		return nil, err
	}

	for _, log := range *result.Data {
		if log.Type == LogTypeManage || log.Type == LogTypeSystem {
			log.SourceIp = ""
		}
	}

	return result, nil
}

// GetAllUserLogsList returns all user logs matching the criteria without pagination (for export)
func GetAllUserLogsList(userId int, params *LogsListParams) ([]*Log, error) {
	var logs []*Log

	tx := DB.Where("user_id = ?", userId).Omit("id", "cost_quota", "channel_id", "upstream_request_id")

	if params.LogType != LogTypeUnknown {
		tx = tx.Where("type = ?", params.LogType)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	// upstream_request_id 属管理员维度，不在用户侧过滤（口径同 channel_id）

	// Apply ordering
	if params.Order != "" {
		orderFields := strings.Split(params.Order, ",")
		for _, field := range orderFields {
			field = strings.TrimSpace(field)
			desc := strings.HasPrefix(field, "-")
			if desc {
				field = field[1:]
			}
			if allowedUserLogsOrderFields[field] {
				if desc {
					field = field + " DESC"
				}
				tx = tx.Order(field)
			}
		}
	} else {
		// Default ordering
		tx = tx.Order("id DESC")
	}

	err := tx.Find(&logs).Error
	if err != nil {
		return nil, err
	}

	// Apply the same filtering as in GetUserLogsList
	for _, log := range logs {
		if log.Type == LogTypeManage || log.Type == LogTypeSystem {
			log.SourceIp = ""
		}
	}

	return logs, nil
}

func SearchAllLogs(keyword string) (logs []*Log, err error) {
	err = DB.Where("type = ? or content LIKE ?", keyword, keyword+"%").Order("id desc").Limit(config.MaxRecentItems).Find(&logs).Error
	return logs, err
}

func SearchUserLogs(userId int, keyword string) (logs []*Log, err error) {
	err = DB.Where("user_id = ? and type = ?", userId, keyword).Order("id desc").Limit(config.MaxRecentItems).Omit("id", "cost_quota", "channel_id", "upstream_request_id").Find(&logs).Error
	return logs, err
}

func SumUsedQuota(params *LogsListParams) (quota int) {
	tx := DB.Table("logs").Select(assembleSumSelectStr("quota"))
	if params.Username != "" {
		tx = tx.Where("username = ?", params.Username)
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.SourceIp != "" {
		tx = tx.Where("source_ip = ?", params.SourceIp)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	if params.UpstreamRequestId != "" {
		tx = tx.Where("upstream_request_id = ?", params.UpstreamRequestId)
	}
	// 「总消费」按定义只统计 LogTypeConsume，调用方传入的 params.LogType 在此被有意忽略。
	// 即便 Tab 切到「全部」（log_type=0）也只汇总消费类型，避免把充值/管理/系统日志的 quota 混进总消费。
	tx.Where("type = ?", LogTypeConsume).Scan(&quota)
	return quota
}

func DeleteOldLog(targetTimestamp int64) (int64, error) {
	result := DB.Where("type = ? AND created_at < ?", LogTypeConsume, targetTimestamp).Delete(&Log{})
	return result.RowsAffected, result.Error
}

// DeleteOldLogBatch 分批删除指定时间之前的消费日志，返回本批删除行数。
// 先查一批 ID 再按 ID 删，避免超大事务锁表。
func DeleteOldLogBatch(targetTimestamp int64, batchSize int) (int64, error) {
	var ids []int
	err := DB.Model(&Log{}).Select("id").
		Where("type = ? AND created_at < ?", LogTypeConsume, targetTimestamp).
		Limit(batchSize).Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("id IN ?", ids).Delete(&Log{})
	return result.RowsAffected, result.Error
}

type LogStatistic struct {
	Date             string `gorm:"column:date"`
	RequestCount     int64  `gorm:"column:request_count"`
	Quota            int64  `gorm:"column:quota"`
	CostQuota        int64  `gorm:"column:cost_quota"`
	PromptTokens     int64  `gorm:"column:prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens"`
	RequestTime      int64  `gorm:"column:request_time"`
}

type LogStatisticGroupModel struct {
	LogStatistic
	ModelName string `gorm:"column:model_name"`
}

type LogStatisticGroupChannel struct {
	LogStatistic
	Channel string `gorm:"column:channel"`
}

type RpmTpmStatistics struct {
	RPM int64   `json:"rpm"`
	TPM int64   `json:"tpm"`
	CPM float64 `json:"cpm"`
	PPM float64 `json:"ppm"` // Profit Per Minute (美元)：每分钟利润 = (收入 - 成本) / QuotaPerUnit
}

func GetRpmTpmStatistics() (*RpmTpmStatistics, error) {
	var result struct {
		RPM        int64 `gorm:"column:rpm"`
		TPM        int64 `gorm:"column:tpm"`
		TotalQuota int64 `gorm:"column:total_quota"`
		CostQuota  int64 `gorm:"column:cost_quota"`
	}

	// 获取最近60秒的统计数据
	now := time.Now().Unix()
	startTime := now - 60

	err := DB.Table("logs").
		Select("COUNT(*) as rpm, COALESCE(SUM(prompt_tokens + completion_tokens), 0) as tpm, COALESCE(SUM(quota), 0) as total_quota, COALESCE(SUM(cost_quota), 0) as cost_quota").
		Where("type = ? AND created_at >= ?", LogTypeConsume, startTime).
		Scan(&result).Error

	if err != nil {
		return nil, err
	}

	// 计算每分钟消费金额 (美元)
	// total_quota 是系统内部的配额单位，需要转换为美元
	cpm := float64(result.TotalQuota) / float64(config.QuotaPerUnit)
	// 每分钟利润 = (收入 - 成本) / QuotaPerUnit
	ppm := float64(result.TotalQuota-result.CostQuota) / float64(config.QuotaPerUnit)

	return &RpmTpmStatistics{
		RPM: result.RPM,
		TPM: result.TPM,
		CPM: cpm,
		PPM: ppm,
	}, nil
}
