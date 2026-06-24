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
	// 上游 promptTokenCount 在中转链路里有时丢字段或为 0（中转商裁字段 / cache 命中等），
	// 整对象赋值会把 RelayHandler 在 send 前填的本地预估 PromptTokens 一并抹掉，
	// 导致消费日志记成 "0 in / N out"、平台漏收 input 部分费用。保留本地预估值兜底。
	upstreamUsage := ConvertOpenAIUsageWithFallback(geminiResponse.UsageMetadata, geminiResponse)
	if upstreamUsage.PromptTokens <= 0 && usage.PromptTokens > 0 {
		upstreamUsage.PromptTokens = usage.PromptTokens
	}
	// total 兜底（max 逻辑，与流式 HandlerStream 对齐）：保证 total >= prompt + completion。
	// 覆盖两种中转商裁字段模式：
	//   - 只裁 prompt 留 total：upstream total 仍含真实 prompt，>= expected，不动
	//   - prompt 和 total 一起裁：upstream total=0 或偏小，提升到 expected
	if upstreamUsage.PromptTokens > 0 {
		expected := upstreamUsage.PromptTokens + upstreamUsage.CompletionTokens
		if upstreamUsage.TotalTokens < expected {
			upstreamUsage.TotalTokens = expected
		}
	}
	*usage = upstreamUsage

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
	// 注意 PromptTokenCount/TotalTokenCount 可能被中转商裁掉，需要把 Candidates/Thoughts 也纳入判断，
	// 否则上游只返回 output 部分时 hasValidUsage=false，CompletionTokens 会漏算（计费缺失）
	hasValidUsage := false
	if geminiResponse.UsageMetadata != nil &&
		(geminiResponse.UsageMetadata.TotalTokenCount > 0 ||
			geminiResponse.UsageMetadata.PromptTokenCount > 0 ||
			geminiResponse.UsageMetadata.CandidatesTokenCount > 0 ||
			geminiResponse.UsageMetadata.ThoughtsTokenCount > 0) {
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

	// 上游 PromptTokenCount 可能为 0（中转商裁字段、cache 命中等）。
	// 跟非流式 CreateGeminiChat 的兜底语义对齐：上游给 0 时不要覆盖掉
	// RelayHandler 在 send 前填的本地预估值，否则消费日志会落成 "0 in / N out"。
	if geminiResponse.UsageMetadata.PromptTokenCount > 0 {
		h.Usage.PromptTokens = geminiResponse.UsageMetadata.PromptTokenCount
	}

	// 缓存命中 token：与 PromptTokens 一致取最后一个非零值，计费时按缓存倍率折算。
	if geminiResponse.UsageMetadata.CachedContentTokenCount > 0 {
		h.Usage.PromptTokensDetails.CachedTokens = geminiResponse.UsageMetadata.CachedContentTokenCount
	}

	// 计算 completion tokens，确保不为负数
	completionTokens := geminiResponse.UsageMetadata.CandidatesTokenCount + geminiResponse.UsageMetadata.ThoughtsTokenCount
	if completionTokens < 0 {
		completionTokens = 0
	}
	h.Usage.CompletionTokens = completionTokens
	h.Usage.CompletionTokensDetails.ReasoningTokens = geminiResponse.UsageMetadata.ThoughtsTokenCount

	// total 兜底：保证 total >= prompt + completion（OpenAI 协议契约）。
	// 允许 upstream total 比它大（reasoning 模型 thoughts 已计入 completion 不会偏大；
	// 真大说明 upstream 有额外计费维度如 cache 包含在 prompt 里，信任 upstream 值不去动）。
	// 这条同时覆盖了两种中转商裁字段模式：
	//   - 只裁 prompt 留 total（total 仍含真实 prompt，>= expected，不改）
	//   - prompt 和 total 一起裁（total=0 或偏小，提升到 expected）
	totalTokens := geminiResponse.UsageMetadata.TotalTokenCount
	if h.Usage.PromptTokens > 0 {
		expected := h.Usage.PromptTokens + completionTokens
		if totalTokens < expected {
			totalTokens = expected
		}
	}
	h.Usage.TotalTokens = totalTokens

	dataChan <- rawStr
}
