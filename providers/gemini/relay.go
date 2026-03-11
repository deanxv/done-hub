package gemini

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/types"
	"encoding/json"
	"net/http"
	"strings"
)

// countImagesInResponse 统计响应中的图片数量
func countImagesInResponse(response *GeminiChatResponse) int {
	if response == nil || len(response.Candidates) == 0 {
		return 0
	}

	imageCount := 0
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") && len(part.InlineData.Data) > 0 {
				imageCount++
			}
		}
	}

	return imageCount
}

type GeminiRelayStreamHandler struct {
	Usage     *types.Usage
	Prefix    string
	ModelName string

	Key string
}

func (p *GeminiProvider) CreateGeminiChat(request *GeminiChatRequest) (*GeminiChatResponse, *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request, true)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	geminiResponse := &GeminiChatResponse{}
	// 发送请求
	_, errWithCode = p.Requester.SendRequest(req, geminiResponse, false)
	if errWithCode != nil {
		return nil, errWithCode
	}

	// 只有非 countTokens 请求才检查 candidates
	if request.Action != "countTokens" && len(geminiResponse.Candidates) == 0 {
		return nil, common.StringErrorWrapper("no candidates", "no_candidates", http.StatusInternalServerError)
	}

	usage := p.GetUsage()
	*usage = ConvertOpenAIUsage(geminiResponse.UsageMetadata)

	return geminiResponse, nil
}

func (p *GeminiProvider) CreateGeminiChatStream(request *GeminiChatRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	req, errWithCode := p.getChatRequest(request, true)
	if errWithCode != nil {
		return nil, errWithCode
	}
	defer req.Body.Close()

	channel := p.GetChannel()

	chatHandler := &GeminiRelayStreamHandler{
		Usage:     p.Usage,
		ModelName: request.Model,
		Prefix:    `data: `,

		Key: channel.Key,
	}

	// 发送请求
	resp, errWithCode := p.Requester.SendRequestRaw(req)
	if errWithCode != nil {
		return nil, errWithCode
	}

	stream, errWithCode := requester.RequestNoTrimStream(p.Requester, resp, chatHandler.HandlerStream)
	if errWithCode != nil {
		return nil, errWithCode
	}

	return stream, nil
}

func (h *GeminiRelayStreamHandler) HandlerStream(rawLine *[]byte, dataChan chan string, errChan chan error) {
	rawStr := string(*rawLine)
	// 如果rawLine 前缀不为data:，则直接返回
	if !strings.HasPrefix(rawStr, h.Prefix) {
		dataChan <- rawStr
		return
	}

	noSpaceLine := bytes.TrimSpace(*rawLine)
	noSpaceLine = noSpaceLine[6:]

	var geminiResponse GeminiChatResponse
	err := json.Unmarshal(noSpaceLine, &geminiResponse)
	if err != nil {
		errChan <- ErrorToGeminiErr(err)
		return
	}

	if geminiResponse.ErrorInfo != nil {
		cleaningError(geminiResponse.ErrorInfo, h.Key)
		errChan <- geminiResponse.ErrorInfo
		return
	}

	// 累积流式内容到 TextBuilder，用于 UsageMetadata 缺失或不准确时的 token 计算备用
	for _, candidate := range geminiResponse.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" && !part.Thought {
				h.Usage.TextBuilder.WriteString(part.Text)
			}
		}
	}

	// 检查 UsageMetadata 是否为 nil 或所有字段都是 0（VertexAI 流式响应中间块只有 trafficType）
	hasValidUsage := false
	if geminiResponse.UsageMetadata != nil &&
		(geminiResponse.UsageMetadata.TotalTokenCount > 0 || geminiResponse.UsageMetadata.PromptTokenCount > 0) {
		hasValidUsage = true
	}

	if !hasValidUsage {
		// 没有有效的 UsageMetadata，尝试从响应内容中统计图片数量
		imageCount := countImagesInResponse(&geminiResponse)
		if imageCount > 0 {
			// 按图片数量计费：每张图片 1290 tokens
			const tokensPerImage = 1290
			h.Usage.CompletionTokens = imageCount * tokensPerImage
			h.Usage.TotalTokens = h.Usage.PromptTokens + h.Usage.CompletionTokens
		}
		dataChan <- rawStr
		return
	}

	h.Usage.PromptTokens = geminiResponse.UsageMetadata.PromptTokenCount

	// 计算 completion tokens，确保不为负数
	completionTokens := geminiResponse.UsageMetadata.CandidatesTokenCount + geminiResponse.UsageMetadata.ThoughtsTokenCount
	if completionTokens < 0 {
		completionTokens = 0
	}
	h.Usage.CompletionTokens = completionTokens
	h.Usage.CompletionTokensDetails.ReasoningTokens = geminiResponse.UsageMetadata.ThoughtsTokenCount

	// 如果 TotalTokenCount 为 0 但有 PromptTokenCount，则计算总数
	totalTokens := geminiResponse.UsageMetadata.TotalTokenCount
	if totalTokens == 0 && geminiResponse.UsageMetadata.PromptTokenCount > 0 {
		totalTokens = geminiResponse.UsageMetadata.PromptTokenCount + completionTokens
	}
	h.Usage.TotalTokens = totalTokens

	dataChan <- rawStr
}
