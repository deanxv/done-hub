package openai

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// rawMessageToString 把 image 响应里回显的 quality/size（json.RawMessage，可能是字符串 / 数字 /
// 对象）解成裸字符串，仅当它本就是 JSON 字符串时返回其值，否则返回空串。
// 用途：上游漏返 usage 时，兜底估算优先采用上游回显的真实渲染档位，非字符串写法安全回落到
// 请求参数，不会 panic 也不会误传带引号的值给 GPTImageOutputTokens。
func rawMessageToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// GPTImageOutputTokens 返回 gpt-image-1 / gpt-image-2 等 OpenAI image 系列在给定 quality+size
// 组合下的 output_tokens 数（OpenAI 官方公式，来源 https://platform.openai.com/docs/guides/images）。
// 上游漏返 usage 时按这张表兜底，避免老的 imageCount*258 常数严重低估高 quality 大图的计费。
//
// quality 取值：low / medium / high / auto（auto 等价 medium）；空字符串等价 auto。
// size 取值：1024x1024 / 1024x1536 / 1536x1024；空字符串或不在表内时按 1024x1024 估算。
func GPTImageOutputTokens(quality, size string) int {
	q := strings.ToLower(strings.TrimSpace(quality))
	if q == "" || q == "auto" {
		q = "medium"
	}
	s := strings.ToLower(strings.TrimSpace(size))
	if s == "" || s == "auto" {
		s = "1024x1024"
	}

	table := map[string]map[string]int{
		"low": {
			"1024x1024": 272,
			"1024x1536": 408,
			"1536x1024": 400,
		},
		"medium": {
			"1024x1024": 1056,
			"1024x1536": 1584,
			"1536x1024": 1568,
		},
		"high": {
			"1024x1024": 4160,
			"1024x1536": 6240,
			"1536x1024": 6208,
		},
	}

	row, ok := table[q]
	if !ok {
		row = table["medium"]
	}
	if v, ok := row[s]; ok {
		return v
	}
	return row["1024x1024"]
}

// IsGPTImageModel 判断模型是否走 OpenAI gpt-image-* 系列的官方 token 公式。
// dall-e 系列另算（实际 token 量比 gpt-image 小一个数量级），维持原 258 常数兜底。
func IsGPTImageModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "gpt-image-") ||
		strings.HasPrefix(strings.ToLower(model), "chatgpt-image-")
}

// ImageFallbackOutputTokens 上游漏返 usage 时，单张图 output tokens 的兜底估算。
// gpt-image-* 走 OpenAI 官方 quality+size 公式，dall-e 等其他维持 258 常数。
// 三处调用点（image_generations / image_edits / codex/image）共用此函数，避免口径靠注释维系而漂移；
// 各调用点自行决定 quality/size 的来源（响应回显优先 / 回落 request）。
func ImageFallbackOutputTokens(model, quality, size string) int {
	if IsGPTImageModel(model) {
		return GPTImageOutputTokens(quality, size)
	}
	return 258
}

func (p *OpenAIProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &OpenAIProviderImageResponse{}
	// 开启渠道 PassThroughBody 且 relay 层已放行（入口协议 == images、响应原样直返）时，
	// 用 outputResp=true 让 SendRequest 回填 resp.Body：既 unmarshal 一份供计费，又能拿到上游
	// 原始字节用于响应字节透传，保留 output_tokens_details.image_tokens/text_tokens 等未知字段。
	passThrough := p.Channel.PassThroughBody && p.Context != nil && p.Context.GetBool(config.GinRawPassThroughAllowedKey)
	// 发送请求
	resp, errWithCode := p.Requester.SendRequest(req, response, passThrough)
	if passThrough && resp != nil {
		defer resp.Body.Close()
	}

	// 即便后续判错也先落 usage：覆盖"HTTP 200 + body 带 error 字段 + 仍含 usage"这种聚合上游场景。
	if response.Usage != nil && response.Usage.TotalTokens > 0 {
		*p.Usage = *response.Usage.ToOpenAIUsage()
	}

	if errWithCode != nil {
		return nil, errWithCode
	}

	// 检测是否错误
	openaiErr := ErrorHandle(&response.OpenAIErrorResponse)
	if openaiErr != nil {
		errWithCode = &types.OpenAIErrorWithStatusCode{
			OpenAIError: *openaiErr,
			StatusCode:  http.StatusBadRequest,
		}
		return nil, errWithCode
	}

	if p.Usage.TotalTokens == 0 {
		// 上游漏返 usage 兜底：gpt-image-* 走 OpenAI 官方 quality+size 公式，dall-e 等其他
		// 维持 258 常数（dall-e 实际按张定价、token 量与 gpt-image 不在一个量级）。
		imageCount := len(response.Data)
		// gpt-image 客户端常不传 quality/size（relay 层只给 dall-e 补默认值），此时按请求估算会
		// 锁在 medium/1024（1056），而上游可能实际渲染 high/1536x1024（6208）——低估近 6 倍。
		// 优先采用响应回显的真实渲染档位，回显缺失再回落请求值。触发条件恰是上游漏返 usage 的
		// 聚合中转商，正是最需要防低估白嫖的那类渠道。
		quality, size := request.Quality, request.Size
		if v := rawMessageToString(response.Quality); v != "" {
			quality = v
		}
		if v := rawMessageToString(response.Size); v != "" {
			size = v
		}
		perImage := ImageFallbackOutputTokens(request.Model, quality, size)
		p.Usage.CompletionTokens = imageCount * perImage
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
	}

	// 暂存上游原始字节，由 relay 层字节透传，保留未知字段 / 字段顺序。
	// 有别名映射需改 model 时，在原始字节上就地 sjson 改写顶层 model；无映射时恒 no-op。
	// images 官方响应无顶层 model 字段，此处仅兜聚合上游额外回显 model 的情形。
	// 必须放在 ErrorHandle 之后：出错时若落键，字节会残留在 gin.Context 上（重试链路不清理
	// 该 key），被后续重试成功的渠道当作响应体以 200 直返。
	if passThrough && resp != nil {
		if rawBytes, readErr := io.ReadAll(resp.Body); readErr == nil && len(rawBytes) > 0 {
			if patched, changed := base.UnifyModelInJSONBytes(p.Context, rawBytes, "model"); changed {
				rawBytes = patched
			}
			p.Context.Set(config.GinRawResponseBodyKey, rawBytes)
		}
	}

	return &response.ImageResponse, nil

}

func IsWithinRange(element string, value int) bool {
	if _, ok := common.DalleGenerationImageAmounts[element]; !ok {
		return true
	}
	minCount := common.DalleGenerationImageAmounts[element][0]
	maxCount := common.DalleGenerationImageAmounts[element][1]

	return value >= minCount && value <= maxCount
}
