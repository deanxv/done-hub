package common

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"done-hub/common/image"
	"done-hub/types"

	"github.com/pkoukk/tiktoken-go"
	"github.com/spf13/viper"
)

var tokenEncoderMap = map[string]*tiktoken.Tiktoken{}
var gpt35TokenEncoder *tiktoken.Tiktoken
var gpt4TokenEncoder *tiktoken.Tiktoken
var gpt4oTokenEncoder *tiktoken.Tiktoken

func InitTokenEncoders() {
	if viper.GetBool("disable_token_encoders") {
		config.DisableTokenEncoders = true
		logger.SysLog("token encoders disabled")
		return
	}
	logger.SysLog("initializing token encoders")
	var err error
	gpt35TokenEncoder, err = tiktoken.EncodingForModel("gpt-3.5-turbo")
	if err != nil {
		logger.FatalLog(fmt.Sprintf("failed to get gpt-3.5-turbo token encoder: %s", err.Error()))
	}

	gpt4TokenEncoder, err = tiktoken.EncodingForModel("gpt-4")
	if err != nil {
		logger.FatalLog(fmt.Sprintf("failed to get gpt-4 token encoder: %s", err.Error()))
	}

	gpt4oTokenEncoder, err = tiktoken.EncodingForModel("gpt-4o")
	if err != nil {
		logger.FatalLog(fmt.Sprintf("failed to get gpt-4o token encoder: %s", err.Error()))
	}

	logger.SysLog("token encoders initialized")
}

func GetTokenEncoder(model string) *tiktoken.Tiktoken {
	if config.DisableTokenEncoders {
		return nil
	}

	tokenEncoder, ok := tokenEncoderMap[model]
	if ok {
		return tokenEncoder
	}

	if strings.HasPrefix(model, "gpt-3.5") {
		tokenEncoder = gpt35TokenEncoder
	} else if strings.HasPrefix(model, "gpt-4o") {
		tokenEncoder = gpt4oTokenEncoder
	} else if strings.HasPrefix(model, "gpt-4") {
		tokenEncoder = gpt4TokenEncoder
	} else {
		var err error
		tokenEncoder, err = tiktoken.EncodingForModel(model)
		if err != nil {
			logger.SysError(fmt.Sprintf("failed to get token encoder for model %s: %s, using encoder for gpt-3.5-turbo", model, err.Error()))
			tokenEncoder = gpt35TokenEncoder
		}
	}

	tokenEncoderMap[model] = tokenEncoder
	return tokenEncoder
}

func GetTokenNum(tokenEncoder *tiktoken.Tiktoken, text string) int {
	if config.DisableTokenEncoders || config.ApproximateTokenEnabled {
		return int(float64(len(text)) * 0.38)
	}
	return len(tokenEncoder.Encode(text, nil, nil))
}

func CountTokenMessages(messages []types.ChatCompletionMessage, model string, preCostType int) int {

	if preCostType == config.PreContNotAll {
		return 0
	}

	tokenEncoder := GetTokenEncoder(model)
	// Reference:
	// https://github.com/openai/openai-cookbook/blob/main/examples/How_to_count_tokens_with_tiktoken.ipynb
	// https://github.com/pkoukk/tiktoken-go/issues/6
	//
	// Every message follows <|start|>{role/name}\n{content}<|end|>\n
	var tokensPerMessage int
	var tokensPerName int
	if model == "gpt-3.5-turbo-0301" {
		tokensPerMessage = 4
		tokensPerName = -1 // If there's a name, the role is omitted
	} else {
		tokensPerMessage = 3
		tokensPerName = 1
	}
	tokenNum := 0
	var textMsg strings.Builder

	for _, message := range messages {
		tokenNum += tokensPerMessage
		switch v := message.Content.(type) {
		case string:
			textMsg.WriteString(v + "\n")
		case []any:
			for _, it := range v {
				m := it.(map[string]any)
				switch m["type"] {
				case "text":
					textMsg.WriteString(m["text"].(string) + "\n")
				case "image_url":
					if preCostType == config.PreCostNotImage {
						continue
					}
					imageUrl, ok := m["image_url"].(map[string]any)
					if !ok {
						continue
					}
					url := imageUrl["url"].(string)
					detail := ""
					if imageUrl["detail"] != nil {
						detail = imageUrl["detail"].(string)
					}
					countImageTokens := getCountImageFun(model)
					imageTokens, err := countImageTokens(url, detail, model)
					if err != nil {
						//Due to the excessive length of the error information, only extract and record the most critical part.
						logger.SysError("error counting image tokens: " + err.Error())
					} else {
						tokenNum += imageTokens
					}
				}
			}
		}
		textMsg.WriteString(message.Role + "\n")

		if message.Name != nil {
			tokenNum += tokensPerName
			textMsg.WriteString(*message.Name + "\n")
		}
	}

	if textMsg.Len() > 0 {
		tokenNum += GetTokenNum(tokenEncoder, textMsg.String())
	}

	tokenNum += 3 // Every reply is primed with <|start|>assistant<|message|>
	return tokenNum
}

func CountTokenInputMessages(input any, model string, preCostType int) int {

	if preCostType == config.PreContNotAll {
		return 0
	}

	tokenEncoder := GetTokenEncoder(model)
	// Reference:
	// https://github.com/openai/openai-cookbook/blob/main/examples/How_to_count_tokens_with_tiktoken.ipynb
	// https://github.com/pkoukk/tiktoken-go/issues/6
	//
	// Every message follows <|start|>{role/name}\n{content}<|end|>\n
	var tokensPerMessage int
	var tokensPerName int
	if model == "gpt-3.5-turbo-0301" {
		tokensPerMessage = 4
		tokensPerName = -1 // If there's a name, the role is omitted
	} else {
		tokensPerMessage = 3
		tokensPerName = 1
	}
	tokenNum := 0

	content, ok := input.(string)
	if ok {
		tokenNum = GetTokenNum(tokenEncoder, content)
		tokenNum += 3

		return tokenNum
	}

	jsonStr, err := json.Marshal(input)
	if err != nil {
		logger.SysError("error marshalling input: " + err.Error())
		return 0
	}

	var textMsg strings.Builder
	var messages []types.ChatCompletionMessage
	err = json.Unmarshal(jsonStr, &messages)
	if err != nil {
		logger.SysError("error unmarshalling input: " + err.Error())
		return 0
	}

	for _, message := range messages {
		tokenNum += tokensPerMessage
		switch v := message.Content.(type) {
		case string:
			textMsg.WriteString(v + "\n")
		case []any:
			for _, it := range v {
				m := it.(map[string]any)
				switch m["type"] {
				case "text":
					textMsg.WriteString(m["text"].(string) + "\n")
				case "image_url":
					if preCostType == config.PreCostNotImage {
						continue
					}
					imageUrl, ok := m["image_url"].(map[string]any)
					if !ok {
						continue
					}
					url := imageUrl["url"].(string)
					detail := ""
					if imageUrl["detail"] != nil {
						detail = imageUrl["detail"].(string)
					}
					countImageTokens := getCountImageFun(model)
					imageTokens, err := countImageTokens(url, detail, model)
					if err != nil {
						//Due to the excessive length of the error information, only extract and record the most critical part.
						logger.SysError("error counting image tokens: " + err.Error())
					} else {
						tokenNum += imageTokens
					}
				}
			}
		}
		textMsg.WriteString(message.Role + "\n")
		if message.Name != nil {
			tokenNum += tokensPerName
			textMsg.WriteString(*message.Name + "\n")
		}
	}

	if textMsg.Len() > 0 {
		tokenNum += GetTokenNum(tokenEncoder, textMsg.String())
	}

	tokenNum += 3 // Every reply is primed with <|start|>assistant<|message|>
	return tokenNum
}

func CountTokenRerankMessages(messages types.RerankRequest, model string, preCostType int) int {
	if preCostType == config.PreContNotAll {
		return 0
	}

	tokenEncoder := GetTokenEncoder(model)
	tokenNum := 0
	var textMsg strings.Builder

	textMsg.WriteString(messages.Query + "\n")

	for _, document := range messages.Documents {
		docStr, ok := document.(string)
		if ok {
			textMsg.WriteString(docStr + "\n")
		} else {
			docMultimodal, ok := document.(map[string]string)
			if ok {
				text := docMultimodal["text"]
				if text != "" {
					textMsg.WriteString(text + "\n")
				} else {
					// 意思意思加点
					tokenNum += 10
				}
			}
		}
	}

	if textMsg.Len() > 0 {
		tokenNum += GetTokenNum(tokenEncoder, textMsg.String())
	}

	return tokenNum
}

func getCountImageFun(model string) CountImageFun {
	for prefix, fun := range CountImageFunMap {
		if strings.HasPrefix(model, prefix) {
			return fun
		}
	}
	return CountImageFunMap["gpt-"]
}

type CountImageFun func(url, detail, modelName string) (int, error)

var CountImageFunMap = map[string]CountImageFun{
	"gpt-":    countOpenaiImageTokens,
	"gemini-": countGeminiImageTokens,
	"claude-": countClaudeImageTokens,
	"glm-":    countGlmImageTokens,
}

type OpenAIImageCost struct {
	Low        int
	High       int
	Additional int
}

var OpenAIImageCostMap = map[string]*OpenAIImageCost{
	"general": {
		Low:        85,
		High:       170,
		Additional: 85,
	},
	"gpt-4o-mini": {
		Low:        2833,
		High:       5667,
		Additional: 2833,
	},
}

// https://platform.openai.com/docs/guides/vision/calculating-costs
// https://github.com/openai/openai-cookbook/blob/05e3f9be4c7a2ae7ecf029a7c32065b024730ebe/examples/How_to_count_tokens_with_tiktoken.ipynb
func countOpenaiImageTokens(url, detail, modelName string) (_ int, err error) {
	// var fetchSize = true
	var width, height int
	var openAIImageCost *OpenAIImageCost
	if strings.HasPrefix(modelName, "gpt-4o-mini") {
		openAIImageCost = OpenAIImageCostMap["gpt-4o-mini"]
	} else {
		openAIImageCost = OpenAIImageCostMap["general"]
	}
	// Reference: https://platform.openai.com/docs/guides/vision/low-or-high-fidelity-image-understanding
	// detail == "auto" is undocumented on how it works, it just said the model will use the auto setting which will look at the image input size and decide if it should use the low or high setting.
	// According to the official guide, "low" disable the high-res model,
	// and only receive low-res 512px x 512px version of the image, indicating
	// that image is treated as low-res when size is smaller than 512px x 512px,
	// then we can assume that image size larger than 512px x 512px is treated
	// as high-res. Then we have the following logic:
	// if detail == "" || detail == "auto" {
	// 	width, height, err = image.GetImageSize(url)
	// 	if err != nil {
	// 		return 0, err
	// 	}
	// 	fetchSize = false
	// 	// not sure if this is correct
	// 	if width > 512 || height > 512 {
	// 		detail = "high"
	// 	} else {
	// 		detail = "low"
	// 	}
	// }

	// However, in my test, it seems to be always the same as "high".
	// The following image, which is 125x50, is still treated as high-res, taken
	// 255 tokens in the response of non-stream chat completion api.
	// https://upload.wikimedia.org/wikipedia/commons/1/10/18_Infantry_Division_Messina.jpg
	if detail == "" || detail == "auto" {
		// assume by test, not sure if this is correct
		detail = "high"
	}
	switch detail {
	case "low":
		return openAIImageCost.Low, nil
	case "high":
		width, height, err = image.GetImageSize(url)
		if err != nil {
			return 0, err
		}
		if width > 2048 || height > 2048 { // max(width, height) > 2048
			ratio := float64(2048) / math.Max(float64(width), float64(height))
			width = int(float64(width) * ratio)
			height = int(float64(height) * ratio)
		}
		if width > 768 && height > 768 { // min(width, height) > 768
			ratio := float64(768) / math.Min(float64(width), float64(height))
			width = int(float64(width) * ratio)
			height = int(float64(height) * ratio)
		}
		numSquares := int(math.Ceil(float64(width)/512) * math.Ceil(float64(height)/512))
		result := numSquares*openAIImageCost.High + openAIImageCost.Additional
		return result, nil
	default:
		return 0, errors.New("invalid detail option")
	}
}

func countGeminiImageTokens(_, _, _ string) (int, error) {
	return 258, nil
}

func countClaudeImageTokens(url, _, _ string) (int, error) {
	width, height, err := image.GetImageSize(url)
	if err != nil {
		return 0, err
	}

	return int(math.Ceil(float64(width*height) / 750)), nil
}

func countGlmImageTokens(_, _, _ string) (int, error) {
	return 1047, nil
}

func CountTokenInput(input any, model string) int {
	switch v := input.(type) {
	case string:
		return CountTokenText(v, model)
	case []string:
		text := ""
		for _, s := range v {
			text += s
		}
		return CountTokenText(text, model)
	}
	return CountTokenInput(fmt.Sprintf("%v", input), model)
}

func CountTokenText(text string, model string) int {
	tokenEncoder := GetTokenEncoder(model)
	return GetTokenNum(tokenEncoder, text)
}

func CountTokenImage(input interface{}) (int, error) {
	switch v := input.(type) {
	case types.ImageRequest:
		// 处理 ImageRequest
		return calculateToken(v.Model, v.Size, v.N, v.Quality, v.Style)
	case types.ImageEditRequest:
		// 处理 ImageEditsRequest
		return calculateToken(v.Model, v.Size, v.N, "", "")
	default:
		return 0, errors.New("unsupported type")
	}
}

func calculateToken(model string, size string, n int, quality, style string) (int, error) {

	imageCostRatio := 1.0
	hasValidSize := false

	switch model {
	case "recraft20b", "recraftv3":
		if style == "vector_illustration" {
			imageCostRatio = 2
		}

	default:
		imageCostRatio, hasValidSize = DalleSizeRatios[model][size]

		if hasValidSize {
			if quality == "hd" && model == "dall-e-3" {
				if size == "1024x1024" {
					imageCostRatio *= 2
				} else {
					imageCostRatio *= 1.5
				}
			}
		} else {
			imageCostRatio = 1
		}
	}

	return int(imageCostRatio*1000) * n, nil
}
