package base

import (
	"context"
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type ProviderConfig struct {
	BaseURL             string
	Completions         string
	ChatCompletions     string
	Embeddings          string
	AudioSpeech         string
	Moderation          string
	AudioTranscriptions string
	AudioTranslations   string
	ImagesGenerations   string
	ImagesEdit          string
	ImagesVariations    string
	ModelList           string
	Rerank              string
	ChatRealtime        string
	Responses           string
}

func (pc *ProviderConfig) SetAPIUri(customMapping map[string]interface{}) {
	relayModeMap := map[int]*string{
		config.RelayModeChatCompletions:    &pc.ChatCompletions,
		config.RelayModeCompletions:        &pc.Completions,
		config.RelayModeEmbeddings:         &pc.Embeddings,
		config.RelayModeAudioSpeech:        &pc.AudioSpeech,
		config.RelayModeAudioTranscription: &pc.AudioTranscriptions,
		config.RelayModeAudioTranslation:   &pc.AudioTranslations,
		config.RelayModeModerations:        &pc.Moderation,
		config.RelayModeImagesGenerations:  &pc.ImagesGenerations,
		config.RelayModeImagesEdits:        &pc.ImagesEdit,
		config.RelayModeImagesVariations:   &pc.ImagesVariations,
		config.RelayModeResponses:          &pc.Responses,
	}

	for key, value := range customMapping {
		keyInt := utils.String2Int(key)
		customValue, isString := value.(string)
		if !isString || customValue == "" {
			continue
		}

		if _, exists := relayModeMap[keyInt]; !exists {
			continue
		}

		value := customValue
		if value == "disable" {
			value = ""
		}

		*relayModeMap[keyInt] = value

	}
}

type BaseProvider struct {
	OriginalModel   string
	Usage           *types.Usage
	Config          ProviderConfig
	Context         *gin.Context
	Channel         *model.Channel
	Requester       *requester.HTTPRequester
	OtherArg        string
	SupportResponse bool
}

// 获取基础URL
func (p *BaseProvider) GetBaseURL() string {
	if p.Channel.GetBaseURL() != "" {
		return p.Channel.GetBaseURL()
	}

	return p.Config.BaseURL
}

// 获取完整请求URL
func (p *BaseProvider) GetFullRequestURL(requestURL string, _ string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")

	return fmt.Sprintf("%s%s", baseURL, requestURL)
}

// 获取请求头
func (p *BaseProvider) CommonRequestHeaders(headers map[string]string) {
	if p.Context != nil {
		headers["Content-Type"] = p.Context.Request.Header.Get("Content-Type")
		headers["Accept"] = p.Context.Request.Header.Get("Accept")
	}

	if headers["Content-Type"] == "" {
		headers["Content-Type"] = "application/json"
	}
	// 自定义header
	if p.Channel.ModelHeaders != nil {
		var customHeaders map[string]string
		err := json.Unmarshal([]byte(*p.Channel.ModelHeaders), &customHeaders)
		if err == nil {
			for key, value := range customHeaders {
				headers[key] = value
			}
		}
	}
}

func (p *BaseProvider) GetUsage() *types.Usage {
	return p.Usage
}

func (p *BaseProvider) SetUsage(usage *types.Usage) {
	p.Usage = usage
}

func (p *BaseProvider) SetContext(c *gin.Context) {
	p.Context = c
	// 使用 WithoutCancel 创建一个不受客户端断开影响的 context
	// 这样即使客户端断开，上游请求也会继续完成，确保计费和日志正常记录
	if c != nil && p.Requester != nil {
		p.Requester.Context = context.WithoutCancel(c.Request.Context())
	}
}

func (p *BaseProvider) GetContext() *gin.Context {
	return p.Context
}

func (p *BaseProvider) SetOriginalModel(ModelName string) {
	p.OriginalModel = ModelName
}

func (p *BaseProvider) GetOriginalModel() string {
	return p.OriginalModel
}

// GetResponseModelName 获取响应中应该使用的模型名称
// 默认使用原始模型名称（用户友好），保持用户体验一致性
func (p *BaseProvider) GetResponseModelName(requestModel string) string {
	return GetResponseModelNameFromContext(p.Context, requestModel)
}

// GetResponseModelNameFromContext 从 Context 获取响应模型名称的静态函数
// 用于流式响应等无法访问 BaseProvider 的场景
func GetResponseModelNameFromContext(ctx *gin.Context, fallbackModel string) string {
	if ctx == nil {
		return fallbackModel
	}

	// 检查是否启用了统一请求响应模型功能
	if !config.UnifiedRequestResponseModelEnabled {
		return fallbackModel
	}

	// 优先使用存储的原始模型名称
	if originalModel, exists := ctx.Get("original_model"); exists {
		if originalModelStr, ok := originalModel.(string); ok && originalModelStr != "" {
			return originalModelStr
		}
	}

	return fallbackModel
}

func (p *BaseProvider) GetChannel() *model.Channel {
	return p.Channel
}

func (p *BaseProvider) ModelMappingHandler(modelName string) (string, error) {
	p.OriginalModel = modelName

	modelMapping := p.Channel.GetModelMapping()

	if modelMapping == "" || modelMapping == "{}" {
		return modelName, nil
	}

	modelMap := make(map[string]string)
	err := json.Unmarshal([]byte(modelMapping), &modelMap)
	if err != nil {
		return "", err
	}

	if modelMap[modelName] != "" {
		return modelMap[modelName], nil
	}

	return modelName, nil
}

// CustomParameterHandler processes extra parameters from the channel and returns them as a map
func (p *BaseProvider) CustomParameterHandler() (map[string]interface{}, error) {
	customParameter := p.Channel.GetCustomParameter()
	if customParameter == "" || customParameter == "{}" {
		return nil, nil
	}

	customParams := make(map[string]interface{})
	err := json.Unmarshal([]byte(customParameter), &customParams)
	if err != nil {
		return nil, err
	}

	return customParams, nil
}

func (p *BaseProvider) GetAPIUri(relayMode int) string {
	switch relayMode {
	case config.RelayModeChatCompletions:
		return p.Config.ChatCompletions
	case config.RelayModeCompletions:
		return p.Config.Completions
	case config.RelayModeEmbeddings:
		return p.Config.Embeddings
	case config.RelayModeAudioSpeech:
		return p.Config.AudioSpeech
	case config.RelayModeAudioTranscription:
		return p.Config.AudioTranscriptions
	case config.RelayModeAudioTranslation:
		return p.Config.AudioTranslations
	case config.RelayModeModerations:
		return p.Config.Moderation
	case config.RelayModeImagesGenerations:
		return p.Config.ImagesGenerations
	case config.RelayModeImagesEdits:
		return p.Config.ImagesEdit
	case config.RelayModeImagesVariations:
		return p.Config.ImagesVariations
	case config.RelayModeRerank:
		return p.Config.Rerank
	case config.RelayModeChatRealtime:
		return p.Config.ChatRealtime
	case config.RelayModeResponses:
		return p.Config.Responses
	default:
		return ""
	}
}

func (p *BaseProvider) GetSupportedAPIUri(relayMode int) (url string, err *types.OpenAIErrorWithStatusCode) {
	url = p.GetAPIUri(relayMode)
	if url == "" {
		err = common.StringErrorWrapperLocal("The API interface is not supported", "unsupported_api", http.StatusNotImplemented)
		return
	}

	return
}

func (p *BaseProvider) GetRequester() *requester.HTTPRequester {
	return p.Requester
}

func (p *BaseProvider) GetOtherArg() string {
	return p.OtherArg
}

func (p *BaseProvider) SetOtherArg(otherArg string) {
	p.OtherArg = otherArg
}

// NewRequestWithCustomParams 创建带有额外参数处理的请求
// 这个方法会自动处理channel中配置的额外参数，并将其合并到请求体中
func (p *BaseProvider) NewRequestWithCustomParams(method, url string, originalRequest interface{}, headers map[string]string, modelName string) (*http.Request, *types.OpenAIErrorWithStatusCode) {

	// 处理额外参数
	customParams, err := p.CustomParameterHandler()
	if err != nil {
		return nil, common.ErrorWrapper(err, "custom_parameter_error", http.StatusInternalServerError)
	}

	// 如果有额外参数，将其添加到请求体中
	if customParams != nil {
		// 将请求体转换为map，以便添加额外参数
		var requestMap map[string]interface{}
		var requestBytes []byte

		// 检查 originalRequest 是否已经是 []byte 类型
		if rawBytes, ok := originalRequest.([]byte); ok {
			// 如果已经是 []byte，直接使用
			requestBytes = rawBytes
		} else {
			// 否则进行 JSON 编码
			requestBytes, err = json.Marshal(originalRequest)
			if err != nil {
				return nil, common.ErrorWrapper(err, "marshal_request_failed", http.StatusInternalServerError)
			}
		}

		err = json.Unmarshal(requestBytes, &requestMap)
		if err != nil {
			return nil, common.ErrorWrapper(err, "unmarshal_request_failed", http.StatusInternalServerError)
		}

		// 处理自定义额外参数
		requestMap = p.MergeCustomParams(requestMap, customParams, modelName)

		// 使用修改后的请求体创建请求
		req, err := p.Requester.NewRequest(method, url, p.Requester.WithBody(requestMap), p.Requester.WithHeader(headers))
		if err != nil {
			return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
		}

		return req, nil
	}

	// 如果没有额外参数，使用原始请求体创建请求
	req, err := p.Requester.NewRequest(method, url, p.Requester.WithBody(originalRequest), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}

// removeNestedParam removes a parameter from the map, supporting nested paths like "generationConfig.thinkingConfig"
func removeNestedParam(requestMap map[string]interface{}, paramPath string) {
	// 使用 "." 分割路径
	parts := strings.Split(paramPath, ".")
	
	// 如果只有一层,直接删除
	if len(parts) == 1 {
		delete(requestMap, paramPath)
		return
	}
	
	// 处理嵌套路径
	current := requestMap
	for i := 0; i < len(parts)-1; i++ {
		if next, ok := current[parts[i]].(map[string]interface{}); ok {
			current = next
		} else {
			// 如果中间路径不存在或不是 map,则无法继续
			return
		}
	}
	
	// 删除最后一级的键
	delete(current, parts[len(parts)-1])
}

// MergeCustomParams 将自定义参数合并到请求体中
func (p *BaseProvider) MergeCustomParams(requestMap map[string]interface{}, customParams map[string]interface{}, modelName string) map[string]interface{} {
	// 检查是否需要覆盖已有参数
	shouldOverwrite := false
	if overwriteValue, exists := customParams["overwrite"]; exists {
		if boolValue, ok := overwriteValue.(bool); ok {
			shouldOverwrite = boolValue
		}
	}

	// 检查是否按照模型粒度控制
	perModel := false
	if perModelValue, exists := customParams["per_model"]; exists {
		if boolValue, ok := perModelValue.(bool); ok {
			perModel = boolValue
		}
	}

	customParamsModel := customParams
	if perModel && modelName != "" {
		if v, exists := customParams[modelName]; exists {
			if modelConfig, ok := v.(map[string]interface{}); ok {
				customParamsModel = modelConfig
			} else {
				customParamsModel = map[string]interface{}{}
			}
		} else {
			customParamsModel = map[string]interface{}{}
		}
	}

	// 处理参数删除
	if removeParams, exists := customParamsModel["remove_params"]; exists {
		if paramsList, ok := removeParams.([]interface{}); ok {
			for _, param := range paramsList {
				if paramName, ok := param.(string); ok {
					removeNestedParam(requestMap, paramName)
				}
			}
		}
	}

	// 添加额外参数
	for key, value := range customParamsModel {
		// 忽略控制参数
		if key == "stream" || key == "overwrite" || key == "per_model" || key == "remove_params" {
			continue
		}
		// 根据覆盖设置决定如何添加参数
		if shouldOverwrite {
			// 覆盖模式：直接添加/覆盖参数
			requestMap[key] = value
		} else {
			// 非覆盖模式：进行深度合并
			if existingValue, exists := requestMap[key]; exists {
				// 如果都是map类型，进行深度合并
				if existingMap, ok := existingValue.(map[string]interface{}); ok {
					if newMap, ok := value.(map[string]interface{}); ok {
						requestMap[key] = p.DeepMergeMap(existingMap, newMap)
						continue
					}
				}
				// 如果不是map类型或类型不匹配，保持原值（不覆盖）
			} else {
				// 参数不存在时直接添加
				requestMap[key] = value
			}
		}
	}

	return requestMap
}

// DeepMergeMap 深度合并两个map
func (p *BaseProvider) DeepMergeMap(existing map[string]interface{}, new map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// 先复制现有的所有键值
	for k, v := range existing {
		result[k] = v
	}

	// 然后合并新的键值
	for k, newValue := range new {
		if existingValue, exists := result[k]; exists {
			// 如果都是map类型，递归深度合并
			if existingMap, ok := existingValue.(map[string]interface{}); ok {
				if newMap, ok := newValue.(map[string]interface{}); ok {
					result[k] = p.DeepMergeMap(existingMap, newMap)
					continue
				}
			}
			// 如果不是map类型，新值覆盖旧值
			result[k] = newValue
		} else {
			// 键不存在，直接添加
			result[k] = newValue
		}
	}

	return result
}

func (p *BaseProvider) GetSupportedResponse() bool {
	return p.SupportResponse
}

func (p *BaseProvider) GetRawBody() ([]byte, bool) {
	if raw, exists := p.Context.Get(config.GinRequestBodyKey); exists {
		if bytes, ok := raw.([]byte); ok {
			return bytes, true
		}
	}
	return nil, false
}

// ClearRawBody 清理缓存的请求体以释放内存
// 应在请求体不再需要后调用（如已发送到上游后）
func (p *BaseProvider) ClearRawBody() {
	p.Context.Set(config.GinRequestBodyKey, nil)
}

// GetProcessedBody 获取已处理的 Gemini 请求体，返回：map、是否 VertexAI 模式、是否存在
func (p *BaseProvider) GetProcessedBody() (map[string]interface{}, bool, bool) {
	if processed, exists := p.Context.Get(config.GinProcessedBodyKey); exists {
		if dataMap, ok := processed.(map[string]interface{}); ok {
			isVertexAI, _ := p.Context.Get(config.GinProcessedBodyIsVertexAI)
			return dataMap, isVertexAI == true, true
		}
	}
	return nil, false, false
}

// SetProcessedBody 缓存处理后的 Gemini 请求体，同时清理原始请求体以释放内存
func (p *BaseProvider) SetProcessedBody(dataMap map[string]interface{}, isVertexAI bool) {
	p.Context.Set(config.GinProcessedBodyKey, dataMap)
	p.Context.Set(config.GinProcessedBodyIsVertexAI, isVertexAI)
	p.Context.Set(config.GinRequestBodyKey, nil)
}
