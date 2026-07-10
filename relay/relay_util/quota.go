package relay_util

import (
	"context"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/model"
	"done-hub/types"
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

type Quota struct {
	modelName        string
	promptTokens     int
	price            model.Price
	groupName        string
	isBackupGroup    bool // 新增字段记录是否使用备用分组
	backupGroupName  string
	groupRatio       float64
	inputRatio       float64
	outputRatio      float64
	costRatio        float64
	preConsumedQuota int
	cacheQuota       int
	userId           int
	channelId        int
	tokenId          int
	unlimitedQuota   bool
	HandelStatus     bool

	startTime         time.Time
	firstResponseTime time.Time
	extraBillingData  map[string]ExtraBillingData
}

func NewQuota(c *gin.Context, modelName string, promptTokens int) *Quota {
	isBackupGroup := c.GetBool("is_backupGroup")

	quota := &Quota{
		modelName:      modelName,
		promptTokens:   promptTokens,
		userId:         c.GetInt("id"),
		channelId:      c.GetInt("channel_id"),
		tokenId:        c.GetInt("token_id"),
		unlimitedQuota: c.GetBool("token_unlimited_quota"),
		HandelStatus:   false,
		isBackupGroup:  isBackupGroup, // 记录是否使用备用分组
	}

	quota.price = *model.PricingInstance.GetPrice(quota.modelName)

	// 记录分组信息用于日志
	if isBackupGroup {
		// 发生了降级：记录原始分组 → 实际使用的分组
		quota.groupName = c.GetString("original_token_group") // 降级链的起点
		quota.backupGroupName = c.GetString("token_group")    // 实际使用的分组
	} else {
		// 没有降级：只记录使用的分组
		quota.groupName = c.GetString("token_group")
		quota.backupGroupName = ""
	}

	quota.groupRatio = c.GetFloat64("group_ratio") // 这里的倍率已经在 common.go 中正确设置了
	quota.inputRatio = quota.price.GetInput() * quota.groupRatio
	quota.outputRatio = quota.price.GetOutput() * quota.groupRatio

	// 成本倍率：仅用于成本/利润统计，不参与用户扣费。未配置或取不到渠道时为 0（不计成本）。
	quota.costRatio = 0
	if channel := model.ChannelGroup.GetChannel(quota.channelId); channel != nil {
		quota.costRatio = channel.GetCostRatio()
	}

	return quota

}

func (q *Quota) PreQuotaConsumption() *types.OpenAIErrorWithStatusCode {
	if q.price.Type == model.TimesPriceType {
		q.preConsumedQuota = common.QuotaFromFloat(1000 * q.inputRatio)
	} else if q.price.Input != 0 || q.price.Output != 0 {
		q.preConsumedQuota = common.QuotaFromFloat(float64(q.promptTokens)*q.inputRatio) + config.PreConsumedQuota
	}

	if q.preConsumedQuota == 0 {
		return nil
	}

	userQuota, err := model.CacheGetUserQuota(q.userId)
	if err != nil {
		return common.ErrorWrapper(err, "get_user_quota_failed", http.StatusInternalServerError)
	}

	if userQuota > 100*q.preConsumedQuota {
		q.preConsumedQuota = 0
		return nil
	}

	if userQuota < q.preConsumedQuota {
		return common.ErrorWrapperLocal(errors.New("user quota is not enough"), "insufficient_user_quota", http.StatusPaymentRequired)
	}

	if q.preConsumedQuota > 0 {
		err := model.PreConsumeTokenQuota(q.tokenId, q.preConsumedQuota)
		if err != nil {
			return common.ErrorWrapperLocal(err, "pre_consume_token_quota_failed", http.StatusForbidden)
		}
		_ = model.CacheUpdateUserQuota(q.userId)
		q.HandelStatus = true
	}

	return nil
}

// 更新用户实时配额
func (q *Quota) UpdateUserRealtimeQuota(usage *types.UsageEvent, nowUsage *types.UsageEvent) error {
	usage.Merge(nowUsage)

	// 不开启Redis，则不更新实时配额
	if !config.RedisEnabled {
		return nil
	}

	promptTokens, completionTokens := q.getComputeTokensByUsageEvent(nowUsage)
	// 长上下文分档：实时路径逐增量结算，用累计输入 token（而非单次增量）判断档位，
	// 与最终结算按整次请求原始输入判档的效果保持收敛；对本次增量套用分档倍率。
	inRatio, outRatio := q.price.GetLongContextMultiplier(usage.InputTokens)
	increaseQuota := q.calcQuota(promptTokens, completionTokens, q.inputRatio*inRatio, q.outputRatio*outRatio, q.groupRatio)

	cacheQuota, err := model.CacheIncreaseUserRealtimeQuota(q.userId, increaseQuota)
	if err != nil {
		return errors.New("error update user realtime quota cache: " + err.Error())
	}

	q.cacheQuota += increaseQuota
	userQuota, err := model.CacheGetUserQuota(q.userId)
	if err != nil {
		return errors.New("error get user quota cache: " + err.Error())
	}

	if cacheQuota >= int64(userQuota) {
		return errors.New("user quota is not enough")
	}

	return nil
}

func (q *Quota) completedQuotaConsumption(usage *types.Usage, tokenName string, isStream bool, sourceIp string, ctx context.Context) error {
	defer func() {
		if q.cacheQuota > 0 {
			model.CacheDecreaseUserRealtimeQuota(q.userId, q.cacheQuota)
		}
	}()

	quota := q.GetTotalQuotaByUsage(usage)
	costQuota := q.GetCostQuotaByUsage(usage)

	quotaDelta := quota - q.preConsumedQuota
	var quotaErr error
	if quotaDelta != 0 {
		err := model.PostConsumeTokenQuotaWithInfo(q.tokenId, q.userId, q.unlimitedQuota, quotaDelta)
		if err != nil {
			quotaErr = errors.New("error consuming token remain quota: " + err.Error())
			logger.LogError(ctx, quotaErr.Error())
		} else {
			err = model.CacheUpdateUserQuota(q.userId)
			if err != nil {
				quotaErr = errors.New("error update user quota cache: " + err.Error())
				logger.LogError(ctx, quotaErr.Error())
			}
		}
	}
	if quota > 0 {
		model.UpdateChannelUsedQuota(q.channelId, quota)
	}

	// 无论配额操作是否成功，都要记录日志，避免上游已计费但本地无记录
	model.RecordConsumeLog(
		ctx,
		q.userId,
		q.channelId,
		usage.PromptTokens,
		usage.CompletionTokens,
		q.modelName,
		tokenName,
		quota,
		costQuota,
		"",
		q.getRequestTime(),
		isStream,
		q.GetLogMeta(usage),
		sourceIp,
	)
	model.UpdateUserUsedQuotaAndRequestCount(q.userId, quota)

	return quotaErr
}

func (q *Quota) Undo(c *gin.Context) {
	if !q.HandelStatus {
		return
	}
	// Undo 所有调用方都在 gin handler 同步路径上，panic 由 gin.Recovery 兜底（带 stack）。
	// 不再加本地 recover：之前的"defense-in-depth"在已有 gin.Recovery 时是 anti-pattern：
	// 截胡 panic 让上层拿不到信号、日志失去堆栈、可调试性反而下降。
	ctx := c.Request.Context()
	if err := model.PostConsumeTokenQuotaWithInfo(q.tokenId, q.userId, q.unlimitedQuota, -q.preConsumedQuota); err != nil {
		logger.LogError(ctx, "error return pre-consumed quota: "+err.Error())
	}
	_ = model.CacheUpdateUserQuota(q.userId)
}

func (q *Quota) Consume(c *gin.Context, usage *types.Usage, isStream bool) {
	// 同步调用方（handler 主流程）走这里：c 在调用栈上活着，直接当场 snapshot 即可。
	// 异步调用方（TrackedGoroutine 路径）应直接调 ConsumeWithSnapshot，
	// 在 spawn goroutine 之前先 NewConsumeSnapshot(c)，闭包持有快照值而非 c 指针，
	// 彻底避免 handler return 后 c 被 gin pool 复用造成的数据竞争。
	q.ConsumeWithSnapshot(NewConsumeSnapshot(c), usage, isStream)
}

// ConsumeSnapshot 是 Quota.Consume 需要的 gin.Context 字段的不可变快照。
// 用于把 handler 派生 goroutine 上的 c 访问全部前置到 handler 还活着的时刻，
// 闭包只持有值，杜绝 c-pool 复用窗口。
type ConsumeSnapshot struct {
	TokenName string
	SourceIP  string
	StartTime time.Time
	Ctx       context.Context
}

// NewConsumeSnapshot 立即从 c 抓取计费所需字段，构造不再持有 c 指针的快照。
// 必须在 handler 还在调用栈上（c 仍归本请求所有）时调用。
func NewConsumeSnapshot(c *gin.Context) ConsumeSnapshot {
	return ConsumeSnapshot{
		TokenName: c.GetString("token_name"),
		SourceIP:  c.ClientIP(),
		StartTime: c.GetTime("requestStartTime"),
		Ctx:       c.Request.Context(),
	}
}

// ConsumeWithSnapshot 用预先抓取好的快照执行同步扣费 + 写消费日志。
// 同 Consume：DB/Cache 调用刻意不传 ctx，防客户端断连取消计费。
//
// recover 保留的理由：本函数被 realtime / task 的 TrackedGoroutine 异步路径调用，
// 虽然外层 TrackedGoroutine 自带 stack-aware recover 兜底，但本地 recover 能给 panic
// 加上 "Quota.ConsumeWithSnapshot" 的上下文标签 + 完整堆栈，定位 quota 步骤更直接。
// 同步路径（Consume wrapper）下本地 recover 会截胡 gin.Recovery，但权衡后保留这层
// 是因为异步路径 panic 没有 gin.Recovery 接，需要 quota 这层就抓住堆栈。
//
// 延迟成本（同步化的已知代价）：
//   - BatchUpdateEnabled=true（推荐生产配置）：扣费/日志/统计全部走 in-memory append，
//     同步路径上唯一真 IO 是 CacheUpdateUserQuota 一次 Redis 调用，~几 ms。
//   - BatchUpdateEnabled=false：5 个调用里 4 个走真 DB UPDATE/INSERT，TTLB 增加 ~10-20ms。
//
// 这是反压换一致性的有意取舍：异步会让 handler 早返 200 但扣费 goroutine 在 DB 池满时堆积，
// 正是"上游已计费、本地未记账"的真凶。
func (q *Quota) ConsumeWithSnapshot(snap ConsumeSnapshot, usage *types.Usage, isStream bool) {
	q.startTime = snap.StartTime
	ctx := snap.Ctx
	defer func() {
		if r := recover(); r != nil {
			logger.LogError(ctx, fmt.Sprintf("panic in Quota.ConsumeWithSnapshot: %v, stack: %s", r, string(debug.Stack())))
		}
	}()
	if err := q.completedQuotaConsumption(usage, snap.TokenName, isStream, snap.SourceIP, ctx); err != nil {
		logger.LogError(ctx, err.Error())
	}
}

func (q *Quota) GetInputRatio() float64 {
	return q.inputRatio
}

func (q *Quota) GetLogMeta(usage *types.Usage) map[string]any {
	meta := map[string]any{
		"group_name":        q.groupName,
		"backup_group_name": q.backupGroupName,
		"is_backup_group":   q.isBackupGroup, // 添加是否使用备用分组的标识
		"price_type":        q.price.Type,
		"group_ratio":       q.groupRatio,
		"input_ratio":       q.price.GetInput(),
		"output_ratio":      q.price.GetOutput(),
	}

	firstResponseTime := q.GetFirstResponseTime()
	if firstResponseTime > 0 {
		meta["first_response"] = firstResponseTime
	}

	if usage != nil {
		extraTokens := usage.GetExtraTokens()

		for key, value := range extraTokens {
			meta[key] = value
			extraRatio := q.price.GetExtraRatio(key)
			meta[key+"_ratio"] = extraRatio
		}

		// 长上下文分档命中时记录分档倍率，供日志详情展示。
		if inRatio, outRatio := q.price.GetLongContextMultiplier(usage.PromptTokens); inRatio != 1 || outRatio != 1 {
			meta["long_context_input_ratio"] = inRatio
			meta["long_context_output_ratio"] = outRatio
		}
	}

	if q.extraBillingData != nil {
		meta["extra_billing"] = q.extraBillingData
	}

	return meta
}

func (q *Quota) getRequestTime() int {
	return int(time.Since(q.startTime).Milliseconds())
}

// calcQuota 按给定倍率计算配额。inputRatio/outputRatio 已含对应倍率（分组倍率或成本倍率），
// ratio 用于额外计费项。销售额与成本共用同一计算逻辑，仅倍率不同。
// 依赖调用方已通过 GetExtraBillingData 设置好 extraBillingData。
func (q *Quota) calcQuota(promptTokens, completionTokens int, inputRatio, outputRatio, ratio float64) (quota int) {
	if q.price.Type == model.TimesPriceType {
		quota = common.QuotaFromFloat(1000 * inputRatio)
	} else {
		quota = common.QuotaFromFloat(math.Ceil((float64(promptTokens) * inputRatio) + (float64(completionTokens) * outputRatio)))
	}

	extraBillingQuota := 0
	if q.extraBillingData != nil {
		for _, value := range q.extraBillingData {
			extraBillingQuota += common.QuotaFromFloat(
				math.Ceil(float64(value.Price)*float64(config.QuotaPerUnit)) * float64(value.CallCount),
			)
		}
	}

	if extraBillingQuota > 0 {
		quota += common.QuotaFromFloat(math.Ceil(
			float64(extraBillingQuota) * ratio,
		))
	}

	if inputRatio != 0 && quota <= 0 {
		quota = 1
	}
	totalTokens := promptTokens + completionTokens
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
	}

	// 如果禁用了空回复计费且没有输出token，则不计费
	if !config.EmptyResponseBillingEnabled && completionTokens == 0 {
		quota = 0
	}

	return quota
}

// 获取计算的 token 数
func (q *Quota) getComputeTokensByUsage(usage *types.Usage) (promptTokens, completionTokens int) {
	promptTokens = usage.PromptTokens
	completionTokens = usage.CompletionTokens

	extraTokens := usage.GetExtraTokens()

	for key, value := range extraTokens {
		extraRatio := q.price.GetExtraRatio(key)
		if model.GetExtraPriceIsPrompt(key) {
			promptTokens += model.GetIncreaseTokens(value, extraRatio)
		} else {
			completionTokens += model.GetIncreaseTokens(value, extraRatio)
		}
	}

	return
}

func (q *Quota) getComputeTokensByUsageEvent(usage *types.UsageEvent) (promptTokens, completionTokens int) {
	promptTokens = usage.InputTokens
	completionTokens = usage.OutputTokens
	extraTokens := usage.GetExtraTokens()

	for key, value := range extraTokens {
		extraRatio := q.price.GetExtraRatio(key)
		if model.GetExtraPriceIsPrompt(key) {
			promptTokens += model.GetIncreaseTokens(value, extraRatio)
		} else {
			completionTokens += model.GetIncreaseTokens(value, extraRatio)
		}
	}

	return
}

// 通过 usage 获取消费配额
func (q *Quota) GetTotalQuotaByUsage(usage *types.Usage) (quota int) {
	promptTokens, completionTokens := q.getComputeTokensByUsage(usage)
	// 长上下文分档：按原始输入 token（未经缓存折算）判断档位，超阈值时整次请求套用分档倍率。
	inRatio, outRatio := q.price.GetLongContextMultiplier(usage.PromptTokens)
	q.GetExtraBillingData(usage.ExtraBilling)
	return q.calcQuota(promptTokens, completionTokens, q.inputRatio*inRatio, q.outputRatio*outRatio, q.groupRatio)
}

// GetCostQuotaByUsage 按渠道成本倍率计算本次请求的上游成本配额，仅用于成本/利润统计，不参与扣费。
func (q *Quota) GetCostQuotaByUsage(usage *types.Usage) (costQuota int) {
	// 未配置成本倍率时不计成本，省去无谓计算。
	if q.costRatio <= 0 {
		return 0
	}
	promptTokens, completionTokens := q.getComputeTokensByUsage(usage)
	inRatio, outRatio := q.price.GetLongContextMultiplier(usage.PromptTokens)
	q.GetExtraBillingData(usage.ExtraBilling)
	return q.calcQuota(promptTokens, completionTokens, q.price.GetInput()*q.costRatio*inRatio, q.price.GetOutput()*q.costRatio*outRatio, q.costRatio)
}

func (q *Quota) GetFirstResponseTime() int64 {
	// 先判断 firstResponseTime 是否为0
	if q.firstResponseTime.IsZero() {
		return 0
	}

	return q.firstResponseTime.Sub(q.startTime).Milliseconds()
}

func (q *Quota) SetFirstResponseTime(firstResponseTime time.Time) {
	q.firstResponseTime = firstResponseTime
}

type ExtraBillingData struct {
	Type      string  `json:"type"`
	CallCount int     `json:"call_count"`
	Price     float64 `json:"price"`
}

func (q *Quota) GetExtraBillingData(extraBilling map[string]types.ExtraBilling) {
	if extraBilling == nil {
		return
	}

	extraBillingData := make(map[string]ExtraBillingData)
	for serviceType, value := range extraBilling {
		extraBillingData[serviceType] = ExtraBillingData{
			Type:      value.Type,
			CallCount: value.CallCount,
			Price:     getDefaultExtraServicePrice(serviceType, q.modelName, value.Type),
		}

	}

	if len(extraBillingData) == 0 {
		return
	}

	q.extraBillingData = extraBillingData
}
