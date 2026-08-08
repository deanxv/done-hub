package relay

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/requester"
	providersBase "done-hub/providers/base"
	"done-hub/providers/openai"
	"done-hub/types"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type relayImageGenerations struct {
	relayBase
	request types.ImageRequest
}

// MaxImageN 单次请求允许的最大图片数量。用户可控的 n / sampleCount 作为
// 计费与上游请求的乘数,必须有上界,防止超大值溢出或滥用(纵深防御)。
const MaxImageN = 64

// clampImageN 把图片数量钳制到 [1, MaxImageN]。
func clampImageN(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxImageN {
		return MaxImageN
	}
	return n
}

// MaxPartialImages partial_images 官方取值范围 [0, 3]。与 n 同理：用户可控、上游按帧
// 计费（每 partial 帧 100 output tokens）的乘数必须钳上界（纵深防御）。
const MaxPartialImages = 3

// clampPartialImages 把 partial_images 钳制到 [0, MaxPartialImages]，nil 原样透传。
func clampPartialImages(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	if v < 0 {
		v = 0
	}
	if v > MaxPartialImages {
		v = MaxPartialImages
	}
	return &v
}

func newRelayImageGenerations(c *gin.Context) *relayImageGenerations {
	relay := &relayImageGenerations{}
	relay.c = c
	return relay
}

func (r *relayImageGenerations) setRequest() error {
	// 检查是否是Gemini格式的请求 (model:predict)
	path := r.c.Request.URL.Path
	if strings.Contains(path, ":predict") {
		return r.setGeminiRequest()
	}

	// 标准OpenAI格式
	if err := common.UnmarshalBodyReusable(r.c, &r.request); err != nil {
		return err
	}

	if r.request.Model == "" {
		r.request.Model = "dall-e-2"
	}

	if r.request.N == 0 {
		r.request.N = 1
	}
	r.request.N = clampImageN(r.request.N)
	r.request.PartialImages = clampPartialImages(r.request.PartialImages)

	if strings.HasPrefix(r.request.Model, "dall-e") {
		if r.request.Size == "" {
			r.request.Size = "1024x1024"
		}

		if r.request.Quality == "" {
			r.request.Quality = "standard"
		}
	}

	r.setOriginalModel(r.request.Model)

	return nil
}

// Gemini格式请求处理
func (r *relayImageGenerations) setGeminiRequest() error {
	// 直接从Gin的路由参数获取模型名（包含冒号部分）
	modelParam := r.c.Param("model")
	if modelParam == "" {
		return errors.New("model parameter not found")
	}

	// 分离模型名和动作 (imagen-3.0-generate-002:predict -> imagen-3.0-generate-002)
	modelName := strings.Split(modelParam, ":")[0]

	// 解析Gemini格式的请求体 - 使用通用结构以支持参数透传
	var geminiRequest struct {
		Instances []struct {
			Prompt string `json:"prompt"`
		} `json:"instances"`
		Parameters map[string]interface{} `json:"parameters"`
	}

	if err := common.UnmarshalBodyReusable(r.c, &geminiRequest); err != nil {
		return err
	}

	// 验证instances数量
	if len(geminiRequest.Instances) == 0 {
		return errors.New("instances is required")
	}

	if len(geminiRequest.Instances) > 1 {
		return errors.New("only one instance is supported, multiple image generation is not allowed")
	}

	r.request = types.ImageRequest{
		Model:       modelName,
		Prompt:      geminiRequest.Instances[0].Prompt,
		N:           1, // 默认值
		ExtraParams: make(map[string]interface{}),
	}

	// 处理parameters中的所有参数
	for key, value := range geminiRequest.Parameters {
		switch key {
		case "sampleCount":
			if sampleCount, ok := value.(float64); ok {
				// 先在 float 域钳到安全区间,避免超大 float 转 int 时结果
				// 实现相关(Go 规范)而绕过下方 clampImageN。
				if sampleCount < 1 {
					sampleCount = 1
				} else if sampleCount > float64(MaxImageN) {
					sampleCount = float64(MaxImageN)
				}
				r.request.N = int(sampleCount)
			}
		case "aspectRatio":
			if aspectRatio, ok := value.(string); ok && aspectRatio != "" {
				r.request.AspectRatio = &aspectRatio
			}
		default:
			// 其他所有参数都作为额外参数透传
			r.request.ExtraParams[key] = value
		}
	}

	if r.request.N == 0 {
		r.request.N = 1
	}
	r.request.N = clampImageN(r.request.N)

	r.setOriginalModel(r.request.Model)

	return nil
}

func (r *relayImageGenerations) getPromptTokens() (int, error) {
	// PromptTokens应该根据请求中的prompt文本计算，而不是图像参数
	return common.CountTokenText(r.request.Prompt, r.getOriginalModel()), nil
}

func (r *relayImageGenerations) IsStream() bool {
	return r.request.StreamEnabled()
}

func (r *relayImageGenerations) send() (err *types.OpenAIErrorWithStatusCode, done bool) {
	provider, ok := r.provider.(providersBase.ImageGenerationsInterface)
	if !ok {
		err = common.StringErrorWrapperLocal("channel not implemented", "channel_error", http.StatusServiceUnavailable)
		done = true
		return
	}

	r.request.Model = r.modelName

	if r.request.StreamEnabled() {
		var stream requester.StreamReaderInterface[string]
		if streamProvider, ok := r.provider.(providersBase.ImageGenerationsStreamInterface); ok {
			stream, err = streamProvider.CreateImageGenerationsStream(&r.request)
		} else {
			err = providersBase.ImageStreamNotSupportedError()
		}

		// 渠道不支持流式：降级走非流式请求，把结果合成 completed 事件回吐给客户端。
		// 哨兵是本地错误、未发过上游请求，降级不会造成重复生成。
		if providersBase.IsImageStreamNotSupported(err) {
			// 拷贝后摘掉流式参数再发非流式请求，防按结构体序列化的渠道把 stream:true 外泄给
			// 上游；不动 r.request，send 之后 RelayHandler 还要用 IsStream() 落计费口径
			fallbackRequest := r.request
			fallbackRequest.Stream = nil
			fallbackRequest.PartialImages = nil
			var response *types.ImageResponse
			response, err = provider.CreateImageGenerations(&fallbackRequest)
			if err != nil {
				return
			}
			// 无 error 但 Data 为空时合成结果是零事件流，客户端等不到 completed 也收不到报错，
			// 显式报错走重试链路（对齐 codex parseImagesStream 的 no_image_output 口径）
			if len(response.Data) == 0 {
				err = common.StringErrorWrapper("upstream did not return any image output", "no_image_output", http.StatusBadGateway)
				return
			}
			stream = openai.NewImageSyntheticStream(openai.ImageResponseToSSE(openai.ImageGenerationStreamPrefix, response))
		}
		if err != nil {
			return
		}

		// 官方 images SSE 靠连接关闭收尾、不发 [DONE]
		doneStr := func() string {
			return ""
		}
		firstResponseTime := responseGeneralStreamClient(r.c, stream, doneStr)
		r.SetFirstResponseTime(firstResponseTime)
		return
	}

	// 入口协议 == images 且响应原样直返：放行 provider 字节透传，
	// 保留上游 usage.output_tokens_details.image_tokens/text_tokens 等未知字段。
	// 仅非流式路径放行：流式降级路径不走 responseJsonClient，暂存的原始字节无人消费。
	r.c.Set(config.GinRawPassThroughAllowedKey, true)

	response, err := provider.CreateImageGenerations(&r.request)
	if err != nil {
		return
	}
	err = responseJsonClient(r.c, response)

	if err != nil {
		done = true
	}

	return
}
