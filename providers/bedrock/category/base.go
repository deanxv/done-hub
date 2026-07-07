package category

import (
	"done-hub/common/model_utils"
	"done-hub/common/requester"
	"done-hub/providers/base"
	"done-hub/types"
	"errors"
	"net/http"
	"strings"
)

// 基础模型映射关系
var bedrockMap = map[string]string{
	//base model id
	"claude-3-7-sonnet-20250219": "anthropic.claude-3-7-sonnet-20250219-v1:0",
	"claude-3-5-sonnet-20240620": "anthropic.claude-3-5-sonnet-20240620-v1:0",
	"claude-3-5-sonnet-20241022": "anthropic.claude-3-5-sonnet-20241022-v2:0",
	"claude-3-opus-20240229":     "anthropic.claude-3-opus-20240229-v1:0",
	"claude-3-sonnet-20240229":   "anthropic.claude-3-sonnet-20240229-v1:0",
	"claude-3-haiku-20240307":    "anthropic.claude-3-haiku-20240307-v1:0",
	"claude-3-5-haiku-20241022":  "anthropic.claude-3-5-haiku-20241022-v1:0",
	"claude-2.1":                 "anthropic.claude-v2:1",
	"claude-2.0":                 "anthropic.claude-v2",
	"claude-instant-1.2":         "anthropic.claude-instant-v1",
	"claude-sonnet-4-20250514":   "anthropic.claude-sonnet-4-20250514-v1:0",
	"claude-opus-4-20250514":     "anthropic.claude-opus-4-20250514-v1:0",
	"claude-opus-4-1-20250805":   "anthropic.claude-opus-4-1-20250805-v1:0",
	"claude-sonnet-4-5-20250929": "anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-6":          "anthropic.claude-sonnet-4-6",
	"claude-haiku-4-5-20251001":  "anthropic.claude-haiku-4-5-20251001-v1:0",
	"claude-opus-4-5-20251101":   "anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-6":            "anthropic.claude-opus-4-6-v1",
	"claude-opus-4-7":            "anthropic.claude-opus-4-7",
	"claude-opus-4-8":            "anthropic.claude-opus-4-8",
	"claude-sonnet-5":            "anthropic.claude-sonnet-5",
	"claude-fable-5":             "anthropic.claude-fable-5",
}

// 用户显式书写的区域前缀（手动覆盖优先）
var regionPrefixes = []string{"global.", "us.", "eu.", "apac."}

// 各 bedrock 模型支持的跨区 inference profile：region 根（aws region 第一段，如
// us-east-1 -> us）映射到该模型在此 region 实际可用的 profile 前缀。未列出的模型/region
// 按裸 id 直连，不加前缀。
//
// 注意 AP 区分两代：claude-3.x / sonnet-4 / opus-4-1 等旧世代有 apac. profile；
// 4.5+ 新世代（sonnet-4-5/4-6、haiku-4-5、opus-4-5/4-6/4-7/4-8）没有 apac.，AP 区统一
// 走 global.（这些新模型在 AP 仅有 global，无 jp/au 时也用 global 兜底）。
var awsModelCanCrossRegionMap = map[string]map[string]string{
	"anthropic.claude-3-sonnet-20240229-v1:0":   {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-3-opus-20240229-v1:0":     {"us": "us"},
	"anthropic.claude-3-haiku-20240307-v1:0":    {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-3-5-sonnet-20240620-v1:0": {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-3-5-sonnet-20241022-v2:0": {"us": "us", "ap": "apac"},
	"anthropic.claude-3-5-haiku-20241022-v1:0":  {"us": "us"},
	"anthropic.claude-3-7-sonnet-20250219-v1:0": {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-sonnet-4-20250514-v1:0":   {"us": "us", "eu": "eu", "ap": "apac"},
	"anthropic.claude-opus-4-20250514-v1:0":     {"us": "us"},
	"anthropic.claude-opus-4-1-20250805-v1:0":   {"us": "us"},
	"anthropic.claude-sonnet-4-5-20250929-v1:0": {"us": "us", "eu": "eu", "ap": "global"},
	"anthropic.claude-sonnet-4-6":               {"us": "us", "eu": "eu", "ap": "global"},
	"anthropic.claude-haiku-4-5-20251001-v1:0":  {"us": "us", "eu": "eu", "ap": "global"},
	"anthropic.claude-opus-4-5-20251101-v1:0":   {"us": "us", "eu": "eu", "ap": "global"},
	"anthropic.claude-opus-4-6-v1":              {"us": "us", "eu": "eu", "ap": "global"},
	"anthropic.claude-opus-4-7":                 {"us": "us", "eu": "eu", "ap": "global"},
	"anthropic.claude-opus-4-8":                 {"us": "us", "eu": "eu", "ap": "global"},
	// sonnet-5：EU 无 geo profile，仅 Global（AWS model card 2026-06-30）
	"anthropic.claude-sonnet-5": {"us": "us", "eu": "global", "ap": "global"},
	"anthropic.claude-fable-5":  {"us": "us", "eu": "global", "ap": "global"},
}

var CategoryMap = map[string]Category{}

type Category struct {
	ModelName                 string
	ChatComplete              ChatCompletionConvert
	ResponseChatComplete      ChatCompletionResponse
	ResponseChatCompleteStrem ChatCompletionStreamResponse
}

func GetCategory(modelName, region string) (*Category, error) {
	modelName = GetModelName(modelName, region)
	// 获取provider
	provider := ""

	if model_utils.ContainsCaseInsensitive(modelName, "anthropic") {
		provider = "anthropic"
	}

	if category, exists := CategoryMap[provider]; exists {
		category.ModelName = modelName
		return &category, nil
	}

	return nil, errors.New("category_not_found")
}

func GetModelName(modelName, region string) string {
	// 提取用户显式书写的区域前缀
	regionPrefix := ""
	for _, prefix := range regionPrefixes {
		if strings.HasPrefix(modelName, prefix) {
			regionPrefix = prefix
			modelName = modelName[len(prefix):]
			break
		}
	}

	// 查找基础模型映射
	if mappedName, exists := bedrockMap[modelName]; exists {
		modelName = mappedName
	}

	// 用户未显式写前缀时，按 region 自动推断跨区前缀
	if regionPrefix == "" {
		regionPrefix = autoCrossRegionPrefix(modelName, region)
	}

	// 如果有区域前缀，添加回去
	if regionPrefix != "" {
		modelName = regionPrefix + modelName
	}

	return modelName
}

// 按 region 与模型跨区可用性返回需拼接的前缀（如 "us."），不可跨区时返回空串
func autoCrossRegionPrefix(awsModelID, region string) string {
	regionRoot := region
	if i := strings.Index(region, "-"); i > 0 {
		regionRoot = region[:i]
	}

	profileMap, ok := awsModelCanCrossRegionMap[awsModelID]
	if !ok {
		return ""
	}

	profilePrefix, ok := profileMap[regionRoot]
	if !ok {
		return ""
	}

	return profilePrefix + "."
}

type ChatCompletionConvert func(*types.ChatCompletionRequest) (any, *types.OpenAIErrorWithStatusCode)
type ChatCompletionResponse func(base.ProviderInterface, *http.Response, *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode)

type ChatCompletionStreamResponse func(base.ProviderInterface, *types.ChatCompletionRequest) requester.HandlerPrefix[string]
