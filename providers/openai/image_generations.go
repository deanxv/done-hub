package openai

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/types"
	"net/http"
	"strings"
)

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

func (p *OpenAIProvider) CreateImageGenerations(request *types.ImageRequest) (*types.ImageResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, request.Model, request)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	response := &OpenAIProviderImageResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, response, false)

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
		perImage := 258
		if IsGPTImageModel(request.Model) {
			perImage = GPTImageOutputTokens(request.Quality, request.Size)
		}
		p.Usage.CompletionTokens = imageCount * perImage
		p.Usage.TotalTokens = p.Usage.PromptTokens + p.Usage.CompletionTokens
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
