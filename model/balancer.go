package model

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/redis"
	"done-hub/common/session"
	"done-hub/common/utils"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// 错误消息常量
const (
	ErrNoAvailableChannelForModel        = "当前分组 %s 下对于模型 %s 无可用渠道"
	ErrModelNotFound                     = "model not found"
	ErrChannelNotFound                   = "channel not found"
	ErrModelNotFoundInGroup              = "model not found in group"
	ErrNoChannelsAvailable               = "no channels available for model"
	ErrNoAvailableChannelsAfterFiltering = "no available channels after filtering"
	ErrDatabaseConsistencyBroken         = "数据库一致性已被破坏，请联系管理员"
	ErrInvalidChannelId                  = "无效的渠道 Id"
	ErrChannelDisabled                   = "该渠道已被禁用"
)

// runtime 错误 sentinel：表示"模型有配但渠道暂时不可用"（列表空、全冷却/过滤、指定 id 失效或被禁），
// 与"配置层就没配"的错误区分开。必须是 sentinel —— fetchChannelByModel 会 wrap，上层用 errors.Is 跨层判定。
var (
	ErrNoChannelsAvailableSentinel               = errors.New(ErrNoChannelsAvailable)
	ErrNoAvailableChannelsAfterFilteringSentinel = errors.New(ErrNoAvailableChannelsAfterFiltering)
	ErrInvalidChannelIdSentinel                  = errors.New(ErrInvalidChannelId)
	ErrChannelDisabledSentinel                   = errors.New(ErrChannelDisabled)
)

type ChannelChoice struct {
	Channel       *Channel
	CooldownsTime int64
	Disable       bool
}

type ChannelsChooser struct {
	sync.RWMutex
	Channels  map[int]*ChannelChoice
	Rule      map[string]map[string][][]int // group -> model -> priority -> channelIds
	Match     []string
	Cooldowns sync.Map

	ModelGroup map[string]map[string]bool
}

type ChannelsFilterFunc func(channelId int, choice *ChannelChoice) bool

func FilterChannelId(skipChannelIds []int) ChannelsFilterFunc {
	return func(channelId int, _ *ChannelChoice) bool {
		return utils.Contains(channelId, skipChannelIds)
	}
}

func FilterChannelTypes(channelTypes []int) ChannelsFilterFunc {
	return func(_ int, choice *ChannelChoice) bool {
		return !utils.Contains(choice.Channel.Type, channelTypes)
	}
}

func FilterOnlyChat() ChannelsFilterFunc {
	return func(channelId int, choice *ChannelChoice) bool {
		return choice.Channel.OnlyChat
	}
}

func FilterDisabledStream(modelName string) ChannelsFilterFunc {
	return func(_ int, choice *ChannelChoice) bool {
		return !choice.Channel.AllowStream(modelName)
	}
}

func init() {
	// 每5分钟清理一次过期的冷却时间，加快内存回收
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			ChannelGroup.CleanupExpiredCooldowns()
		}
	}()
}

func (cc *ChannelsChooser) SetCooldowns(channelId int, modelName string) bool {
	return cc.SetCooldownsWithDuration(channelId, modelName, int64(config.RetryCooldownSeconds))
}

// SetCooldownsWithDuration 设置指定渠道和模型的冷却时间（支持自定义冻结时长）
func (cc *ChannelsChooser) SetCooldownsWithDuration(channelId int, modelName string, durationSeconds int64) bool {
	if channelId == 0 || modelName == "" || durationSeconds == 0 {
		return false
	}

	key := fmt.Sprintf("%d:%s", channelId, modelName)
	nowTime := time.Now().Unix()
	newCooldownTime := nowTime + durationSeconds

	// 使用LoadOrStore的原子性，避免竞态条件
	actualValue, loaded := cc.Cooldowns.LoadOrStore(key, newCooldownTime)

	if loaded {
		// key已存在，检查是否仍在冷却期内
		existingCooldownTime := actualValue.(int64)
		if nowTime < existingCooldownTime {
			// 仍在冷却期内，无需重新设置
			return true
		}
		// 冷却期已过，尝试更新为新的冷却时间
		// 如果CompareAndSwap失败，说明其他线程已经更新了，这也是可以接受的
		cc.Cooldowns.CompareAndSwap(key, existingCooldownTime, newCooldownTime)
	}

	return true
}

func (cc *ChannelsChooser) IsInCooldown(channelId int, modelName string) bool {
	if channelId == 0 || modelName == "" {
		return false
	}

	key := fmt.Sprintf("%d:%s", channelId, modelName)
	nowTime := time.Now().Unix()

	cooldownTime, exists := cc.Cooldowns.Load(key)
	if !exists {
		return false
	}

	// 直接返回冷却状态，不进行任何清理操作
	return nowTime < cooldownTime.(int64)
}

func (cc *ChannelsChooser) CleanupExpiredCooldowns() {
	now := time.Now().Unix()
	cc.Cooldowns.Range(func(key, value interface{}) bool {
		if now >= value.(int64) {
			cc.Cooldowns.Delete(key)
		}
		return true
	})
}

// ClearChannelCooldowns 清除指定渠道的所有冻结缓存
func (cc *ChannelsChooser) ClearChannelCooldowns(channelId int) {
	prefix := fmt.Sprintf("%d:", channelId)
	cc.Cooldowns.Range(func(key, value interface{}) bool {
		if strings.HasPrefix(key.(string), prefix) {
			cc.Cooldowns.Delete(key)
		}
		return true
	})
}

func (cc *ChannelsChooser) Disable(channelId int) {
	cc.Lock()
	defer cc.Unlock()
	if _, ok := cc.Channels[channelId]; !ok {
		return
	}

	cc.Channels[channelId].Disable = true
}

func (cc *ChannelsChooser) Enable(channelId int) {
	cc.Lock()
	defer cc.Unlock()
	if _, ok := cc.Channels[channelId]; !ok {
		return
	}

	cc.Channels[channelId].Disable = false
}

func (cc *ChannelsChooser) ChangeStatus(channelId int, status bool) {
	if status {
		cc.Enable(channelId)
	} else {
		cc.Disable(channelId)
	}
}

// checkStickySession 检查是否有粘性 session 映射，如果有且渠道可用，则返回该渠道
func (cc *ChannelsChooser) checkStickySession(channelIds []int, filters []ChannelsFilterFunc, modelName string, ginContext interface{}) *Channel {
	if !config.RedisEnabled || ginContext == nil || len(channelIds) == 0 {
		return nil
	}

	// 获取第一个候选渠道以确定渠道类型（同一批候选渠道类型相同）
	firstChoice, ok := cc.Channels[channelIds[0]]
	if !ok {
		return nil
	}

	// 根据渠道类型生成 session hash（session hash 与具体渠道无关，只与请求内容和渠道类型有关）
	sessionHash := generateSessionHashForChannel(firstChoice.Channel, ginContext)
	if sessionHash == "" {
		return nil
	}

	// 检查 Redis 中是否有映射（传递渠道类型以使用正确的 key 前缀）
	channelType := firstChoice.Channel.Type
	mappedChannelID, err := redis.GetStickySessionMapping(sessionHash, channelType)
	if err != nil || mappedChannelID <= 0 {
		return nil
	}

	// 检查映射的渠道是否存在且未被禁用
	mappedChoice, ok := cc.Channels[mappedChannelID]
	if !ok || mappedChoice.Disable {
		// 映射的渠道不存在或已禁用，删除映射
		redis.DeleteStickySessionMapping(sessionHash, channelType)
		return nil
	}

	// 检查映射的渠道是否在候选列表中
	isInCandidates := false
	for _, channelId := range channelIds {
		if channelId == mappedChannelID {
			isInCandidates = true
			break
		}
	}
	if !isInCandidates {
		// 映射的渠道不在候选列表中，删除映射
		redis.DeleteStickySessionMapping(sessionHash, channelType)
		return nil
	}

	// 检查渠道是否在冷却中
	if cc.IsInCooldown(mappedChannelID, modelName) {
		// 渠道在冷却中，删除映射
		redis.DeleteStickySessionMapping(sessionHash, channelType)
		return nil
	}

	// 检查过滤器
	for _, filter := range filters {
		if filter(mappedChannelID, mappedChoice) {
			// 渠道被过滤器排除，删除映射
			redis.DeleteStickySessionMapping(sessionHash, channelType)
			return nil
		}
	}

	// 渠道可用，续期 TTL 并返回
	ttl := 1 * time.Hour
	renewalThresholdMinutes := 0 // 默认 0（不续期），与 code-relay-demo 保持一致
	redis.ExtendStickySessionMappingTTL(sessionHash, channelType, ttl, renewalThresholdMinutes)

	// 安全截取 session hash 用于日志显示
	sessionHashPreview := sessionHash
	if len(sessionHash) > 8 {
		sessionHashPreview = sessionHash[:8]
	}

	// 从 ginContext 获取 context.Context 用于日志
	if gc, ok := ginContext.(*gin.Context); ok {
		logger.LogInfo(gc.Request.Context(), fmt.Sprintf("✓ Using sticky session for %s channel %d (session: %s)",
			getChannelTypeName(mappedChoice.Channel.Type), mappedChannelID, sessionHashPreview))
	}

	return mappedChoice.Channel
}

// createStickySession 为选定的渠道创建粘性 session 映射
func (cc *ChannelsChooser) createStickySession(channel *Channel, ginContext interface{}) {
	if !config.RedisEnabled || ginContext == nil || channel == nil {
		return
	}

	sessionHash := generateSessionHashForChannel(channel, ginContext)
	if sessionHash == "" {
		return
	}

	ttl := 1 * time.Hour
	channelType := channel.Type
	err := redis.SetStickySessionMapping(sessionHash, channel.Id, channelType, ttl)
	if err != nil {
		// 从 ginContext 获取 context.Context 用于日志
		if gc, ok := ginContext.(*gin.Context); ok {
			logger.LogError(gc.Request.Context(), fmt.Sprintf("Failed to create sticky session mapping: %v", err))
		}
		return
	}

	// 安全截取 session hash 用于日志显示
	sessionHashPreview := sessionHash
	if len(sessionHash) > 8 {
		sessionHashPreview = sessionHash[:8]
	}

	// 从 ginContext 获取 context.Context 用于日志
	if gc, ok := ginContext.(*gin.Context); ok {
		logger.LogInfo(gc.Request.Context(), fmt.Sprintf("✓ Created sticky session for %s channel %d (session: %s)",
			getChannelTypeName(channel.Type), channel.Id, sessionHashPreview))
	}
}

// generateSessionHashForChannel 根据渠道类型生成 session hash
func generateSessionHashForChannel(channel *Channel, ginContext interface{}) string {
	if channel == nil || ginContext == nil {
		return ""
	}

	// 类型断言为 *gin.Context
	c, ok := ginContext.(interface {
		Get(key string) (value interface{}, exists bool)
		GetHeader(key string) string
		ClientIP() string
	})
	if !ok {
		return ""
	}

	switch channel.Type {
	case config.ChannelTypeClaudeCode:
		// ClaudeCode: 从请求体中提取 Claude 请求并生成 session hash
		rawBody, exists := c.Get(config.GinRequestBodyKey)
		if !exists {
			return ""
		}
		bodyBytes, ok := rawBody.([]byte)
		if !ok {
			return ""
		}

		// 解析为 map 以便提取 metadata
		var requestMap map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &requestMap); err != nil {
			return ""
		}

		// 直接在这里生成 session hash，避免循环导入
		return generateClaudeCodeSessionHash(requestMap)

	case config.ChannelTypeGeminiCli:
		// GeminiCli: 基于 User-Agent + IP + API Key 前缀
		userAgent := c.GetHeader("User-Agent")
		ip := c.ClientIP()

		// 获取 API Key 并提取前缀（前20个字符，与 demo 保持一致）
		apiKeyInterface, _ := c.Get("token")
		apiKey, _ := apiKeyInterface.(string)
		apiKeyPrefix := ""
		if len(apiKey) >= 20 {
			apiKeyPrefix = apiKey[:20]
		} else {
			apiKeyPrefix = apiKey
		}

		return session.GenerateGeminiCliSessionHashFromParts(userAgent, ip, apiKeyPrefix)

	case config.ChannelTypeCodex:
		// Codex: 基于请求头中的 session_id
		sessionID := c.GetHeader("session_id")
		if sessionID == "" {
			sessionID = c.GetHeader("x-session-id")
		}
		return session.GenerateCodexSessionHashFromSessionID(sessionID)

	default:
		return ""
	}
}

// getChannelTypeName 获取渠道类型名称（用于日志）
func getChannelTypeName(channelType int) string {
	switch channelType {
	case config.ChannelTypeClaudeCode:
		return "ClaudeCode"
	case config.ChannelTypeGeminiCli:
		return "GeminiCli"
	case config.ChannelTypeCodex:
		return "Codex"
	default:
		return fmt.Sprintf("Type%d", channelType)
	}
}

// generateClaudeCodeSessionHash 生成 ClaudeCode 的 session hash
// 完全复刻 code-relay-demo 的实现逻辑
// 优先级：
// 1. metadata.user_id 中的 session ID（如果存在）
// 2. 带有 cache_control: {"type": "ephemeral"} 的内容
// 3. system 内容
// 4. 第一条消息内容
func generateClaudeCodeSessionHash(requestMap map[string]interface{}) string {
	if requestMap == nil {
		return ""
	}

	// 1. 最高优先级：使用 metadata.user_id 中的 session ID。
	//    user_id 可能是旧字符串格式 user_<hex>_account__session_<uuid>，
	//    也可能是新对象格式 {"device_id":...,"account_uuid":...,"session_id":"<uuid>"}（claude-cli）。
	if metadataInterface, exists := requestMap["metadata"]; exists {
		if metadataMap, ok := metadataInterface.(map[string]interface{}); ok {
			if userIDRaw, exists := metadataMap["user_id"]; exists {
				if sessionID := session.ExtractSessionIDFromMetadataValue(userIDRaw); sessionID != "" {
					return sessionID
				}
			}
		}
	}

	// 2. 检查是否有 cache_control 内容
	cacheableContent := extractCacheableContentFromMap(requestMap)
	if cacheableContent != "" {
		return session.HashContent(cacheableContent)
	}

	// 3. 使用 system 内容
	if systemInterface, exists := requestMap["system"]; exists {
		systemText := extractSystemTextFromInterface(systemInterface)
		if systemText != "" {
			return session.HashContent(systemText)
		}
	}

	// 4. Fallback: 使用第一条消息内容
	if messagesInterface, exists := requestMap["messages"]; exists {
		if messagesArray, ok := messagesInterface.([]interface{}); ok && len(messagesArray) > 0 {
			if firstMsg, ok := messagesArray[0].(map[string]interface{}); ok {
				firstMessageText := extractMessageContent(firstMsg)
				if firstMessageText != "" {
					return session.HashContent(firstMessageText)
				}
			}
		}
	}

	return ""
}

// extractCacheableContentFromMap 提取带有 cache_control: {"type": "ephemeral"} 的内容
func extractCacheableContentFromMap(requestMap map[string]interface{}) string {
	var cacheableContent string

	// 检查 system 中的 cacheable 内容
	if systemInterface, exists := requestMap["system"]; exists {
		if systemArray, ok := systemInterface.([]interface{}); ok {
			for _, item := range systemArray {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if cacheControl, ok := itemMap["cache_control"].(map[string]interface{}); ok {
						if cacheType, ok := cacheControl["type"].(string); ok && cacheType == "ephemeral" {
							if text, ok := itemMap["text"].(string); ok {
								cacheableContent += text
							}
						}
					}
				}
			}
		}
	}

	// 检查 messages 中的 cacheable 内容
	if messagesInterface, exists := requestMap["messages"]; exists {
		if messagesArray, ok := messagesInterface.([]interface{}); ok {
			for _, msgInterface := range messagesArray {
				if msgMap, ok := msgInterface.(map[string]interface{}); ok {
					hasCacheControl := false

					// 检查消息内容是否有 cache_control
					if contentInterface, exists := msgMap["content"]; exists {
						// 如果 content 是数组
						if contentArray, ok := contentInterface.([]interface{}); ok {
							for _, item := range contentArray {
								if itemMap, ok := item.(map[string]interface{}); ok {
									if cacheControl, ok := itemMap["cache_control"].(map[string]interface{}); ok {
										if cacheType, ok := cacheControl["type"].(string); ok && cacheType == "ephemeral" {
											hasCacheControl = true
											break
										}
									}
								}
							}
						} else if _, ok := contentInterface.(string); ok {
							// 如果 content 是字符串，检查消息级别的 cache_control
							if cacheControl, ok := msgMap["cache_control"].(map[string]interface{}); ok {
								if cacheType, ok := cacheControl["type"].(string); ok && cacheType == "ephemeral" {
									hasCacheControl = true
								}
							}
						}
					}

					// 如果找到 cache_control，提取第一条消息的文本内容
					if hasCacheControl {
						for _, message := range messagesArray {
							if messageMap, ok := message.(map[string]interface{}); ok {
								messageText := extractMessageContent(messageMap)
								if messageText != "" {
									cacheableContent += messageText
									break
								}
							}
						}
						break
					}
				}
			}
		}
	}

	return cacheableContent
}

// extractSystemTextFromInterface 从 system 字段中提取文本内容
func extractSystemTextFromInterface(system interface{}) string {
	if system == nil {
		return ""
	}

	// 如果是字符串，直接返回
	if systemStr, ok := system.(string); ok {
		return systemStr
	}

	// 如果是数组，提取所有 text 字段
	if systemArray, ok := system.([]interface{}); ok {
		var texts []string
		for _, item := range systemArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if text, ok := itemMap["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "")
	}

	return ""
}

// extractMessageContent 从消息 map 中提取文本内容
func extractMessageContent(msgMap map[string]interface{}) string {
	if contentInterface, exists := msgMap["content"]; exists {
		// 如果 content 是字符串
		if contentStr, ok := contentInterface.(string); ok {
			return contentStr
		}

		// 如果 content 是数组
		if contentArray, ok := contentInterface.([]interface{}); ok {
			var texts []string
			for _, item := range contentArray {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemType, ok := itemMap["type"].(string); ok && itemType == "text" {
						if text, ok := itemMap["text"].(string); ok {
							texts = append(texts, text)
						}
					}
				}
			}
			return strings.Join(texts, "")
		}
	}

	return ""
}

func (cc *ChannelsChooser) balancer(channelIds []int, filters []ChannelsFilterFunc, modelName string, ginContext interface{}) *Channel {
	// 1. 检查粘性 session（优先级最高）
	stickyChannel := cc.checkStickySession(channelIds, filters, modelName, ginContext)
	if stickyChannel != nil {
		return stickyChannel
	}

	// 2. 按权重选择渠道
	totalWeight := 0

	validChannels := make([]*ChannelChoice, 0, len(channelIds))
	for _, channelId := range channelIds {
		choice, ok := cc.Channels[channelId]
		if !ok || choice.Disable {
			continue
		}

		if cc.IsInCooldown(channelId, modelName) {
			continue
		}

		isSkip := false
		for _, filter := range filters {
			if filter(channelId, choice) {
				isSkip = true
				break
			}
		}
		if isSkip {
			continue
		}

		weight := int(*choice.Channel.Weight)
		totalWeight += weight
		validChannels = append(validChannels, choice)
	}

	if len(validChannels) == 0 {
		return nil
	}

	if len(validChannels) == 1 {
		selectedChannel := validChannels[0].Channel
		// 建立新的粘性 session 映射
		cc.createStickySession(selectedChannel, ginContext)
		return selectedChannel
	}

	choiceWeight := rand.Intn(totalWeight)
	for _, choice := range validChannels {
		weight := int(*choice.Channel.Weight)
		choiceWeight -= weight
		if choiceWeight < 0 {
			selectedChannel := choice.Channel
			// 建立新的粘性 session 映射
			cc.createStickySession(selectedChannel, ginContext)
			return selectedChannel
		}
	}

	return nil
}

// GetMatchedModelName 获取匹配到的实际模型名称
func (cc *ChannelsChooser) GetMatchedModelName(group, modelName string) (string, error) {
	cc.RLock()
	defer cc.RUnlock()
	if _, ok := cc.Rule[group]; !ok {
		return "", fmt.Errorf(ErrNoAvailableChannelForModel, GlobalUserGroupRatio.GetDisplayName(group), modelName)
	}

	// 如果直接匹配到了，返回原始模型名称
	if _, ok := cc.Rule[group][modelName]; ok {
		return modelName, nil
	}

	var matchModel string

	if config.ModelNameCaseInsensitiveEnabled {
		// 1. 先尝试精确的大小写不敏感匹配
		modelNameLower := strings.ToLower(modelName)
		for existingModel := range cc.Rule[group] {
			if strings.ToLower(existingModel) == modelNameLower {
				matchModel = existingModel
				break
			}
		}
		// 2. 如果没找到，再尝试通配符的大小写不敏感匹配
		if matchModel == "" {
			matchModel = utils.GetModelsWithMatchCaseInsensitive(&cc.Match, modelName)
		}
	}

	// 3. 如果还是没找到，使用原始匹配作为后备
	if matchModel == "" {
		matchModel = utils.GetModelsWithMatch(&cc.Match, modelName)
	}

	if matchModel == "" {
		message := fmt.Sprintf(ErrNoAvailableChannelForModel, GlobalUserGroupRatio.GetDisplayName(group), modelName)
		return "", errors.New(message)
	}

	return matchModel, nil
}

func (cc *ChannelsChooser) Next(group, modelName string, filters ...ChannelsFilterFunc) (*Channel, error) {
	cc.RLock()
	defer cc.RUnlock()
	if _, ok := cc.Rule[group]; !ok {
		return nil, fmt.Errorf(ErrNoAvailableChannelForModel, GlobalUserGroupRatio.GetDisplayName(group), modelName)
	}

	channelsPriority, ok := cc.Rule[group][modelName]
	if !ok {
		var matchModel string

		if config.ModelNameCaseInsensitiveEnabled {
			// 1. 先尝试精确的大小写不敏感匹配
			modelNameLower := strings.ToLower(modelName)
			for existingModel := range cc.Rule[group] {
				if strings.ToLower(existingModel) == modelNameLower {
					matchModel = existingModel
					break
				}
			}
			// 2. 如果没找到，再尝试通配符的大小写不敏感匹配
			if matchModel == "" {
				matchModel = utils.GetModelsWithMatchCaseInsensitive(&cc.Match, modelName)
			}
		}

		// 3. 如果还是没找到，使用原始匹配作为后备
		if matchModel == "" {
			matchModel = utils.GetModelsWithMatch(&cc.Match, modelName)
		}

		channelsPriority, ok = cc.Rule[group][matchModel]
		if !ok {
			return nil, errors.New(ErrModelNotFound)
		}
	}

	if len(channelsPriority) == 0 {
		return nil, errors.New(ErrChannelNotFound)
	}

	for _, priority := range channelsPriority {
		channel := cc.balancer(priority, filters, modelName, nil)
		if channel != nil {
			return channel, nil
		}
	}

	return nil, errors.New(ErrChannelNotFound)
}

// NextByValidatedModel 使用已经验证过的模型名称获取渠道，跳过模型匹配逻辑
// ginContext 用于生成 session hash 和粘性 session 处理
func (cc *ChannelsChooser) NextByValidatedModel(group, validatedModelName string, ginContext interface{}, filters ...ChannelsFilterFunc) (*Channel, error) {
	cc.RLock()
	defer cc.RUnlock()

	if _, ok := cc.Rule[group]; !ok {
		return nil, fmt.Errorf(ErrNoAvailableChannelForModel, GlobalUserGroupRatio.GetDisplayName(group), validatedModelName)
	}

	channelsPriority, ok := cc.Rule[group][validatedModelName]
	if !ok {
		return nil, errors.New(ErrModelNotFoundInGroup)
	}

	if len(channelsPriority) == 0 {
		return nil, ErrNoChannelsAvailableSentinel
	}

	for _, priority := range channelsPriority {
		channel := cc.balancer(priority, filters, validatedModelName, ginContext)
		if channel != nil {
			return channel, nil
		}
	}

	return nil, ErrNoAvailableChannelsAfterFilteringSentinel
}

func (cc *ChannelsChooser) GetGroupModels(group string) ([]string, error) {
	cc.RLock()
	defer cc.RUnlock()

	if _, ok := cc.Rule[group]; !ok {
		return nil, fmt.Errorf(ErrNoAvailableChannelForModel, GlobalUserGroupRatio.GetDisplayName(group), "*")
	}

	models := make([]string, 0, len(cc.Rule[group]))
	for model := range cc.Rule[group] {
		models = append(models, model)
	}

	return models, nil
}

func (cc *ChannelsChooser) GetModelsGroups() map[string]map[string]bool {
	cc.RLock()
	defer cc.RUnlock()

	return cc.ModelGroup
}

func (cc *ChannelsChooser) GetChannel(channelId int) *Channel {
	cc.RLock()
	defer cc.RUnlock()

	if choice, ok := cc.Channels[channelId]; ok {
		return choice.Channel
	}

	return nil
}

// CountAvailableChannels 计算指定分组和模型的可用渠道数量（排除禁用、冷却和过滤的渠道）
func (cc *ChannelsChooser) CountAvailableChannels(group, modelName string, filters ...ChannelsFilterFunc) int {
	cc.RLock()
	defer cc.RUnlock()

	if _, ok := cc.Rule[group]; !ok {
		return 0
	}

	channelsPriority, ok := cc.Rule[group][modelName]
	if !ok {
		return 0
	}

	if len(channelsPriority) == 0 {
		return 0
	}

	totalAvailable := 0
	for _, priority := range channelsPriority {
		totalAvailable += cc.countValidChannels(priority, filters, modelName)
	}

	return totalAvailable
}

// countValidChannels 计算指定渠道列表中的可用渠道数量
// 与balancer方法使用相同的过滤逻辑
func (cc *ChannelsChooser) countValidChannels(channelIds []int, filters []ChannelsFilterFunc, modelName string) int {
	count := 0
	for _, channelId := range channelIds {
		choice, ok := cc.Channels[channelId]
		if !ok || choice.Disable {
			continue
		}

		if cc.IsInCooldown(channelId, modelName) {
			continue
		}

		isSkip := false
		for _, filter := range filters {
			if filter(channelId, choice) {
				isSkip = true
				break
			}
		}
		if isSkip {
			continue
		}

		count++
	}
	return count
}

var ChannelGroup = ChannelsChooser{}

func (cc *ChannelsChooser) Load() {
	var channels []*Channel
	DB.Where("status = ?", config.ChannelStatusEnabled).Find(&channels)

	newGroup := make(map[string]map[string][][]int)
	newChannels := make(map[int]*ChannelChoice)
	newMatch := make(map[string]bool)
	newModelGroup := make(map[string]map[string]bool)

	type groupModelKey struct {
		group string
		model string
	}
	channelGroups := make(map[groupModelKey]map[int64][]int)

	// 处理每个channel
	for _, channel := range channels {
		channel.SetProxy()
		if *channel.Weight == 0 {
			channel.Weight = &config.DefaultChannelWeight
		}
		newChannels[channel.Id] = &ChannelChoice{
			Channel:       channel,
			CooldownsTime: 0,
			Disable:       false,
		}

		// 处理groups和models
		groups := strings.Split(channel.Group, ",")
		models := strings.Split(channel.Models, ",")

		for _, group := range groups {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}

			for _, model := range models {
				model = strings.TrimSpace(model)
				if model == "" {
					continue
				}

				key := groupModelKey{group: group, model: model}
				if _, ok := channelGroups[key]; !ok {
					channelGroups[key] = make(map[int64][]int)
				}

				// 按priority分组存储channelId
				priority := *channel.Priority
				channelGroups[key][priority] = append(channelGroups[key][priority], channel.Id)

				// 处理通配符模型
				if strings.HasSuffix(model, "*") {
					newMatch[model] = true
				}

				// 初始化ModelGroup
				if _, ok := newModelGroup[model]; !ok {
					newModelGroup[model] = make(map[string]bool)
				}
				newModelGroup[model][group] = true
			}
		}
	}

	// 构建最终的newGroup结构
	for key, priorityMap := range channelGroups {
		// 初始化group和model的map
		if _, ok := newGroup[key.group]; !ok {
			newGroup[key.group] = make(map[string][][]int)
		}

		// 获取所有优先级并排序（从大到小）
		var priorities []int64
		for priority := range priorityMap {
			priorities = append(priorities, priority)
		}
		sort.Slice(priorities, func(i, j int) bool {
			return priorities[i] > priorities[j]
		})

		// 按优先级顺序构建[][]int
		var channelsList [][]int
		for _, priority := range priorities {
			channelsList = append(channelsList, priorityMap[priority])
		}

		newGroup[key.group][key.model] = channelsList
	}

	// 构建newMatchList
	newMatchList := make([]string, 0, len(newMatch))
	for match := range newMatch {
		newMatchList = append(newMatchList, match)
	}

	// 更新ChannelsChooser
	cc.Lock()
	cc.Rule = newGroup
	cc.Channels = newChannels
	cc.Match = newMatchList
	cc.ModelGroup = newModelGroup
	cc.Unlock()
	logger.SysLog("channels Load success")
}
