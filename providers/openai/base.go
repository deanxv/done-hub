package openai

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"done-hub/providers/base"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type OpenAIProviderFactory struct{}

type UsageHandler func(usage *types.Usage) (ForcedFormatting bool)
type RequestHandleBefore func(request *types.ChatCompletionRequest) (errWithCode *types.OpenAIErrorWithStatusCode)
type ResponsesBodyPatch func(model string, body []byte) []byte

type OpenAIProvider struct {
	base.BaseProvider
	IsAzure              bool
	BalanceAction        bool
	SupportStreamOptions bool
	StreamEscapeJSON     bool
	ReasoningHandler     bool

	UsageHandler        UsageHandler
	RequestHandleBefore RequestHandleBefore
	ResponsesBodyPatch  ResponsesBodyPatch
}

// 创建 OpenAIProvider
func (f OpenAIProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	openAIProvider := CreateOpenAIProvider(channel, "https://api.openai.com")
	openAIProvider.BalanceAction = true
	return openAIProvider
}

// 创建 OpenAIProvider
// https://platform.openai.com/docs/api-reference/introduction
func CreateOpenAIProvider(channel *model.Channel, baseURL string) *OpenAIProvider {
	openaiConfig := getOpenAIConfig(baseURL, channel)

	OpenAIProvider := &OpenAIProvider{
		BaseProvider: base.BaseProvider{
			Config:          openaiConfig,
			Channel:         channel,
			Requester:       requester.NewHTTPRequester(channel.GetProxy(), RequestErrorHandle),
			SupportResponse: true,
		},
		IsAzure:       false,
		BalanceAction: true,
	}

	if channel.Type == config.ChannelTypeOpenAI {
		OpenAIProvider.SupportStreamOptions = true
	}

	return OpenAIProvider
}

func getOpenAIConfig(baseURL string, channel *model.Channel) base.ProviderConfig {
	providerConfig := base.ProviderConfig{
		BaseURL:             baseURL,
		Completions:         "/v1/completions",
		ChatCompletions:     "/v1/chat/completions",
		Embeddings:          "/v1/embeddings",
		Moderation:          "/v1/moderations",
		AudioSpeech:         "/v1/audio/speech",
		AudioTranscriptions: "/v1/audio/transcriptions",
		AudioTranslations:   "/v1/audio/translations",
		ImagesGenerations:   "/v1/images/generations",
		ImagesEdit:          "/v1/images/edits",
		ImagesVariations:    "/v1/images/variations",
		ModelList:           "/v1/models",
		ChatRealtime:        "/v1/realtime",
		Responses:           "/v1/responses",
	}

	if channel.Type != config.ChannelTypeCustom || channel.Plugin == nil {
		return providerConfig
	}

	customMapping, ok := channel.Plugin.Data()["customize"]
	if !ok {
		return providerConfig
	}

	providerConfig.SetAPIUri(customMapping)

	return providerConfig
}

// 请求错误处理
func RequestErrorHandle(resp *http.Response) *types.OpenAIError {
	errorResponse := &types.OpenAIErrorResponse{}
	err := json.NewDecoder(resp.Body).Decode(errorResponse)
	if err != nil {
		return nil
	}

	return ErrorHandle(errorResponse)
}

// 错误处理
func ErrorHandle(openaiError *types.OpenAIErrorResponse) *types.OpenAIError {
	if openaiError.Error.Message == "" {
		return nil
	}
	return &openaiError.Error
}

// 获取完整请求 URL
func (p *OpenAIProvider) GetFullRequestURL(requestURL string, modelName string) string {
	baseURL := strings.TrimSuffix(p.GetBaseURL(), "/")

	if strings.Contains(modelName, "-realtime") {
		if strings.HasPrefix(baseURL, "https://") {
			baseURL = strings.Replace(baseURL, "https://", "wss://", 1)
		} else {
			baseURL = strings.Replace(baseURL, "http://", "ws://", 1)
		}

		if p.IsAzure {
			// wss://my-eastus2-openai-resource.openai.azure.com/openai/realtime?api-version=2024-10-01-preview&deployment=gpt-4o-realtime-preview-1001
			requestURL = fmt.Sprintf("/openai/%s?api-version=%s&deployment=%s", requestURL, p.Channel.Other, modelName)
		} else {
			requestURL += fmt.Sprintf("?model=%s", modelName)
		}

		// 处理 {model} 占位符替换（realtime 模式）
		if strings.Contains(baseURL, "{model}") && modelName != "" {
			baseURL = strings.ReplaceAll(baseURL, "{model}", modelName)
		}

		return fmt.Sprintf("%s%s", baseURL, requestURL)
	}

	if p.IsAzure {
		apiVersion := p.Channel.Other
		if modelName != "" {
			// 检测模型是是否包含 . 如果有则直接去掉
			// modelName = strings.Replace(modelName, ".", "", -1)

			if modelName == "dall-e-2" {
				// 因为dall-e-3需要api-version=2023-12-01-preview，但是该版本
				// 已经没有dall-e-2了，所以暂时写死
				requestURL = fmt.Sprintf("/openai/%s:submit?api-version=2023-09-01-preview", requestURL)
			} else {
				if strings.HasPrefix(requestURL, "/v1") {
					requestURL = fmt.Sprintf("/openai/%s?api-version=%s", requestURL, apiVersion)
				} else {
					requestURL = fmt.Sprintf("/openai/deployments/%s%s?api-version=%s", modelName, requestURL, apiVersion)
				}
			}
		} else {
			if strings.Contains(requestURL, "isGetAzureModelList") {
				//专门生成用于azure获取模型部署列表的URL，因为azure只有2023-03-15-preview版本等特定版本支持通过api-key获取models 所以本url固定写死
				requestURL = "/openai/deployments?api-version=2023-03-15-preview"
			} else {
				requestURL = strings.TrimPrefix(requestURL, "/v1")
				requestURL = fmt.Sprintf("/openai%s?api-version=%s", requestURL, apiVersion)
			}
		}
	}

	if strings.HasPrefix(baseURL, "https://gateway.ai.cloudflare.com") {
		if p.IsAzure {
			requestURL = strings.TrimPrefix(requestURL, "/openai")
			requestURL = strings.TrimPrefix(requestURL, "/deployments")
		} else {
			requestURL = strings.TrimPrefix(requestURL, "/v1")
		}
	}

	// 处理 {model} 占位符替换（适用于自定义渠道）
	// 检查 baseURL 中是否包含 {model} 占位符，如果包含且 modelName 不为空，则进行替换
	if strings.Contains(baseURL, "{model}") && modelName != "" {
		baseURL = strings.ReplaceAll(baseURL, "{model}", modelName)
	}

	return fmt.Sprintf("%s%s", baseURL, requestURL)
}

// 获取请求头
func (p *OpenAIProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)
	if p.IsAzure {
		headers["api-key"] = p.Channel.Key
		headers["Authorization"] = fmt.Sprintf("Bearer %s", p.Channel.Key)
	} else {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", p.Channel.Key)
	}

	return headers
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

// mergeCustomParams 将自定义参数合并到请求体中
func (p *OpenAIProvider) mergeCustomParams(requestMap map[string]interface{}, customParams map[string]interface{}) map[string]interface{} {
	// 检查是否需要覆盖已有参数
	shouldOverwrite := false
	if overwriteValue, exists := customParams["overwrite"]; exists {
		if boolValue, ok := overwriteValue.(bool); ok {
			shouldOverwrite = boolValue
		}
	}

	// 如果配置是pre_add，而不是发送阶段，则此处跳过所有处理
	if preAdd, exists := customParams["pre_add"]; exists && preAdd == true {
		return requestMap
	}

	// 检查是否按照模型粒度控制
	perModel := false
	if perModelValue, exists := customParams["per_model"]; exists {
		if boolValue, ok := perModelValue.(bool); ok {
			perModel = boolValue
		}
	}

	customParamsModel := customParams
	if perModel {
		if modelValue, ok := requestMap["model"].(string); ok {
			if v, exists := customParams[modelValue]; exists {
				if modelConfig, ok := v.(map[string]interface{}); ok {
					customParamsModel = modelConfig
				} else {
					customParamsModel = map[string]interface{}{}
				}
			} else {
				customParamsModel = map[string]interface{}{}
			}
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
		// 忽略 keys "stream", "overwrite", "per_model", "pre_add", and "remove_params"
		if key == "stream" || key == "overwrite" || key == "per_model" || key == "pre_add" || key == "remove_params" {
			continue
		}
		// 根据覆盖设置决定如何添加参数
		if shouldOverwrite {
			// 覆盖模式：直接添加/覆盖参数
			requestMap[key] = value
		} else {
			// 非覆盖模式：仅当参数不存在时添加
			if _, exists := requestMap[key]; !exists {
				requestMap[key] = value
			}
		}
	}

	return requestMap
}

// mergeExtraBodyFromRawRequest 从原始请求基础上合并处理后的请求字段
// 以用户原始请求为基础，用处理后的字段覆盖，这样额外字段自然保留
func (p *OpenAIProvider) mergeExtraBodyFromRawRequest(requestMap map[string]interface{}) map[string]interface{} {
	rawBody, ok := p.GetRawBody()
	if !ok || rawBody == nil {
		return requestMap
	}

	var rawRequest map[string]interface{}
	err := json.Unmarshal(rawBody, &rawRequest)
	if err != nil {
		return requestMap
	}

	// 以原始请求为基础，用处理后的请求字段覆盖
	for key, value := range requestMap {
		rawRequest[key] = value
	}

	return rawRequest
}

// patchPassThroughBody 读取 gin 缓存的原始请求体，仅对 model 做字段级最小修改
// （模型映射会改写 ModelName），其余一律按字节保留。
// 返回 (字节, true) 表示透传可用；返回 (nil, false) 表示应回退结构体序列化路径。
func (p *OpenAIProvider) patchPassThroughBody(modelName string) ([]byte, bool) {
	rawBody, ok := p.ReadNativeRawBody("")
	if !ok {
		return nil, false
	}

	out := rawBody
	// 模型重写：仅在 body 中已有 model 且与映射目标不一致时回写
	if modelName != "" {
		if current := gjson.GetBytes(out, "model"); current.Exists() && current.String() != modelName {
			patched, err := sjson.SetBytes(out, "model", modelName)
			if err != nil {
				return nil, false
			}
			out = patched
		}
	}

	return out, true
}

// storeParamName 是 OpenAI 的 store 参数名，仅 Chat Completions / Responses 接受
// （以官方 OpenAPI spec 生成的 openai-go SDK 为准：CompletionNewParams 无该字段）；
// Legacy Completions、图片、Embeddings、Speech、Moderation 等接口收到会直接 400 "Unknown parameter: 'store'"。
const storeParamName = "store"

// relayModeAcceptsStore 判断指定接口是否接受 store 参数。
// Responses 原生透传不经过 GetRequestTextBody，但 chat→responses 兼容路径
// （ConvertChat / need2response）和透传不可用时的回退都会以 RelayModeResponses 走到这里。
func relayModeAcceptsStore(relayMode int) bool {
	switch relayMode {
	case config.RelayModeChatCompletions, config.RelayModeResponses:
		return true
	default:
		return false
	}
}

// FilterCustomParamsForRelayMode 按接口类型剔除渠道自定义参数中上游不接受的字段。
// 目前只处理 store：渠道级配置 {"store": false} 是为了阻止 OpenAI 默认留存对话，
// 但同一渠道也承载生图等接口，不过滤会把整条渠道的非对话请求打挂。
// 顶层恒处理；子对象仅在 per_model 模式下处理（与 MergeCustomParamsBytes 的判定一致），
// 避免误删普通对象型参数里恰好同名的子键。无需剔除时原样返回，否则返回新 map，不修改入参；
// 剔除后为空时返回 nil（与 CustomParameterHandler 把 "{}" 视作 nil 的语义一致，调用方可直接跳过合并）。
// 导出给 relay 的 pre_add 预合并路径复用，保证 legacy completions 在两条路径下行为一致。
func FilterCustomParamsForRelayMode(customParams map[string]interface{}, relayMode int) map[string]interface{} {
	if customParams == nil || relayModeAcceptsStore(relayMode) {
		return customParams
	}

	perModel, _ := customParams["per_model"].(bool)

	needFilter := false
	if _, has := customParams[storeParamName]; has {
		needFilter = true
	} else if perModel {
		for _, value := range customParams {
			if sub, ok := value.(map[string]interface{}); ok {
				if _, has := sub[storeParamName]; has {
					needFilter = true
					break
				}
			}
		}
	}
	if !needFilter {
		return customParams
	}

	filtered := make(map[string]interface{}, len(customParams))
	for key, value := range customParams {
		if key == storeParamName {
			continue
		}
		if perModel {
			if sub, ok := value.(map[string]interface{}); ok {
				if _, has := sub[storeParamName]; has {
					subCopy := make(map[string]interface{}, len(sub))
					for k, v := range sub {
						if k != storeParamName {
							subCopy[k] = v
						}
					}
					value = subCopy
				}
			}
		}
		filtered[key] = value
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// 修改GetRequestTextBody函数中的对应部分
func (p *OpenAIProvider) GetRequestTextBody(relayMode int, ModelName string, request any) (*http.Request, *types.OpenAIErrorWithStatusCode) {
	url, errWithCode := p.GetSupportedAPIUri(relayMode)
	if errWithCode != nil {
		return nil, errWithCode
	}
	// 获取请求地址
	fullRequestURL := p.GetFullRequestURL(url, ModelName)

	// 获取请求头
	headers := p.GetRequestHeaders()

	// 处理额外参数
	customParams, err := p.CustomParameterHandler()
	if err != nil {
		return nil, common.ErrorWrapper(err, "custom_parameter_error", http.StatusInternalServerError)
	}
	customParams = FilterCustomParamsForRelayMode(customParams, relayMode)

	// 请求体完整透传：以原始字节为基础，仅改写映射后的模型名，其余字节原样保留
	if p.Channel.PassThroughBody {
		if patched, ok := p.patchPassThroughBody(ModelName); ok {
			return p.NewRequestWithMergedParamsBytes(http.MethodPost, fullRequestURL, patched, headers, customParams, ModelName)
		}
	}

	// 检查是否需要合并额外字段（来自渠道配置的额外参数或用户请求中的extra_body）
	needMerge := customParams != nil || p.Channel.AllowExtraBody

	if needMerge {
		// 将请求体转换为map，以便添加额外参数
		var requestMap map[string]interface{}
		requestBytes, err := json.Marshal(request)
		if err != nil {
			return nil, common.ErrorWrapper(err, "marshal_request_failed", http.StatusInternalServerError)
		}

		err = json.Unmarshal(requestBytes, &requestMap)
		if err != nil {
			return nil, common.ErrorWrapper(err, "unmarshal_request_failed", http.StatusInternalServerError)
		}

		// 如果允许额外字段透传，从原始请求中获取额外字段
		if p.Channel.AllowExtraBody {
			requestMap = p.mergeExtraBodyFromRawRequest(requestMap)
		}

		// 处理自定义额外参数
		if customParams != nil {
			requestMap = p.mergeCustomParams(requestMap, customParams)
		}

		// 使用修改后的请求体创建请求
		req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(requestMap), p.Requester.WithHeader(headers))
		if err != nil {
			return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
		}

		return req, nil
	}

	// 如果没有额外参数，使用原始请求体创建请求
	req, err := p.Requester.NewRequest(http.MethodPost, fullRequestURL, p.Requester.WithBody(request), p.Requester.WithHeader(headers))
	if err != nil {
		return nil, common.ErrorWrapper(err, "new_request_failed", http.StatusInternalServerError)
	}

	return req, nil
}
