package base

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/types"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Requestable interface {
	types.CompletionRequest | types.ChatCompletionRequest | types.EmbeddingRequest | types.ModerationRequest | types.SpeechAudioRequest | types.AudioRequest | types.ImageRequest | types.ImageEditRequest
}

// 基础接口
type ProviderInterface interface {
	// 获取基础URL
	// GetBaseURL() string
	// 获取完整请求URL
	// GetFullRequestURL(requestURL string, modelName string) string
	// 获取请求头
	GetRequestHeaders() map[string]string
	// 获取用量
	GetUsage() *types.Usage
	// 设置用量
	SetUsage(usage *types.Usage)
	// 设置Context
	SetContext(c *gin.Context)
	// 获取Context (用于流式响应等场景)
	GetContext() *gin.Context
	// 设置原始模型
	SetOriginalModel(ModelName string)
	// 获取原始模型
	GetOriginalModel() string
	// 获取响应中应该使用的模型名称
	GetResponseModelName(requestModel string) string

	// SupportAPI(relayMode int) bool
	GetChannel() *model.Channel
	ModelMappingHandler(modelName string) (string, error)
	GetRequester() *requester.HTTPRequester
	SetOtherArg(otherArg string)
	GetOtherArg() string
	CustomParameterHandler() (map[string]interface{}, error)
	GetSupportedResponse() bool
}

// 完成接口
type CompletionInterface interface {
	ProviderInterface
	CreateCompletion(request *types.CompletionRequest) (*types.CompletionResponse, *types.OpenAIErrorWithStatusCode)
	CreateCompletionStream(request *types.CompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode)
}

// 聊天接口
type ChatInterface interface {
	ProviderInterface
	CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode)
	CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode)
}

// 嵌入接口
type EmbeddingsInterface interface {
	ProviderInterface
	CreateEmbeddings(request *types.EmbeddingRequest) (*types.EmbeddingResponse, *types.OpenAIErrorWithStatusCode)
}

// 审查接口
type ModerationInterface interface {
	ProviderInterface
	CreateModeration(request *types.ModerationRequest) (*types.ModerationResponse, *types.OpenAIErrorWithStatusCode)
}

// 文字转语音接口
type SpeechInterface interface {
	ProviderInterface
	CreateSpeech(request *types.SpeechAudioRequest) (*http.Response, *types.OpenAIErrorWithStatusCode)
}

// 语音转文字接口
type TranscriptionsInterface interface {
	ProviderInterface
	CreateTranscriptions(request *types.AudioRequest) (*types.AudioResponseWrapper, *types.OpenAIErrorWithStatusCode)
}

// 语音翻译接口
type TranslationInterface interface {
	ProviderInterface
	CreateTranslation(request *types.AudioRequest) (*types.AudioResponseWrapper, *types.OpenAIErrorWithStatusCode)
}

// 图片生成接口
type ImageGenerationsInterface interface {
	ProviderInterface
	CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode)
}

// 图片编辑接口
type ImageEditsInterface interface {
	ProviderInterface
	CreateImageEdits(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode)
}

type ImageVariationsInterface interface {
	ProviderInterface
	CreateImageVariations(request *types.ImageEditRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode)
}

// 图片生成流式接口。provider 未实现（或实现但返回 image_stream_not_supported 哨兵）时，
// relay 层降级为：走非流式方法，再把结果合成 SSE 返回客户端。
type ImageGenerationsStreamInterface interface {
	ProviderInterface
	CreateImageGenerationsStream(request *types.ImageRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode)
}

// 图片编辑流式接口
type ImageEditsStreamInterface interface {
	ProviderInterface
	CreateImageEditsStream(request *types.ImageEditRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode)
}

// imageStreamNotSupportedCode 图像流式降级哨兵。嵌入 openai.OpenAIProvider 的渠道
// （codex/gemini/siliconflow 等）会继承流式方法但走的是各自的原生端点，需覆写返回此哨兵；
// relay 层识别后降级为合成 SSE，而非当作请求失败。
const imageStreamNotSupportedCode = "image_stream_not_supported"

func ImageStreamNotSupportedError() *types.OpenAIErrorWithStatusCode {
	return &types.OpenAIErrorWithStatusCode{
		OpenAIError: types.OpenAIError{
			Message: "image stream is not supported by this channel",
			Type:    "one_hub_error",
			Code:    imageStreamNotSupportedCode,
		},
		StatusCode: http.StatusNotImplemented,
		LocalError: true,
	}
}

func IsImageStreamNotSupported(err *types.OpenAIErrorWithStatusCode) bool {
	return err != nil && err.Code == imageStreamNotSupportedCode
}

// type RelayInterface interface {
// 	ProviderInterface
// 	CreateRelay() (*http.Response, *types.OpenAIErrorWithStatusCode)
// }

type ModelListInterface interface {
	ProviderInterface
	GetModelList() ([]string, error)
}

// 余额接口
type BalanceInterface interface {
	Balance() (float64, error)
}

// type ProviderResponseHandler interface {
// 	// 响应处理函数
// 	ResponseHandler(resp *http.Response) (OpenAIResponse any, errWithCode *types.OpenAIErrorWithStatusCode)
// }

// Rerank接口
type RerankInterface interface {
	ProviderInterface
	CreateRerank(request *types.RerankRequest) (*types.RerankResponse, *types.OpenAIErrorWithStatusCode)
}

type RealtimeInterface interface {
	ProviderInterface
	CreateChatRealtime(modelName string) (*websocket.Conn, requester.MessageHandler, *types.OpenAIErrorWithStatusCode)
}

type ResponsesInterface interface {
	ProviderInterface
	CreateResponses(request *types.OpenAIResponsesRequest) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode)
	CreateResponsesStream(request *types.OpenAIResponsesRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode)
}

// ResponsesModelSupport 可选能力接口：渠道实现了 ResponsesInterface，但原生
// /v1/responses 仅对部分模型族生效（如 bedrock 仅 GPT-5.x 走原生端点，claude/gpt-oss
// 需回落 chat 兼容层）。relay 层在原生分发前按模型询问；未实现视为全部模型支持。
type ResponsesModelSupport interface {
	SupportsNativeResponses(modelName string) bool
}

// ResponsesCompactInterface /v1/responses/compact 端点的能力。
// compact 永远是非流式响应，因此不需要 stream 版本。
type ResponsesCompactInterface interface {
	ProviderInterface
	CreateResponsesCompaction(request *types.OpenAIResponsesRequest) (*types.OpenAIResponsesResponses, *types.OpenAIErrorWithStatusCode)
}
