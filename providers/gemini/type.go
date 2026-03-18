package gemini

import (
	"done-hub/common"
	"done-hub/common/image"
	"done-hub/common/storage"
	"done-hub/common/utils"
	"done-hub/types"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	goahocorasick "github.com/anknown/ahocorasick"
	"github.com/gin-gonic/gin"
)

const GeminiImageSymbol = "![done-hub-gemini-image]"

const (
	ModalityTEXT  = "TEXT"
	ModalityAUDIO = "AUDIO"
	ModalityIMAGE = "IMAGE"
	ModalityVIDEO = "VIDEO"
)

var ImageSymbolAcMachines = &goahocorasick.Machine{}
var imageRegex = regexp.MustCompile(`\!\[done-hub-gemini-image\]\((.*?)\)`)

// 视频文件扩展名和常见视频域名
var videoExtensions = []string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v", ".3gp", ".ts"}
var videoDomains = []string{
	// 国外主流视频平台
	"youtube.com", "youtu.be", "www.youtube.com", // YouTube
	"vimeo.com", "player.vimeo.com", "vimeo.video", // Vimeo
	"dailymotion.com", "www.dailymotion.com", // Dailymotion
	"twitch.tv", "www.twitch.tv", "clips.twitch.tv", // Twitch
	"tiktok.com", "www.tiktok.com", "vm.tiktok.com", // TikTok
	"instagram.com", "www.instagram.com", // Instagram
	"facebook.com", "www.facebook.com", "fb.watch", // Facebook
	"twitter.com", "x.com", "t.co", // Twitter/X
	"vine.co", "v.ine.co", // Vine
	"snapchat.com", "www.snapchat.com", // Snapchat
	"reddit.com", "v.redd.it", // Reddit
	"streamable.com", "streamja.com", // 其他短视频平台

	// 国内主流视频平台
	"bilibili.com", "www.bilibili.com", "b23.tv", // B站
	"douyin.com", "www.douyin.com", "v.douyin.com", // 抖音
	"kuaishou.com", "www.kuaishou.com", "v.kuaishou.com", // 快手
	"xiaohongshu.com", "www.xiaohongshu.com", "xhslink.com", // 小红书
	"weishi.qq.com", "isee.weishi.qq.com", // 微视
	"huoshan.com", "www.huoshan.com", // 火山小视频
	"pipigx.com", "h5.pipigx.com", // 皮皮虾
	"miaopai.com", "www.miaopai.com", // 秒拍
	"meipai.com", "www.meipai.com", // 美拍
	"v.qq.com", "qq.com", // 腾讯视频
	"iqiyi.com", "www.iqiyi.com", "m.iqiyi.com", // 爱奇艺
	"youku.com", "v.youku.com", "player.youku.com", // 优酷
	"tudou.com", "www.tudou.com", // 土豆
	"sohu.com", "tv.sohu.com", "my.tv.sohu.com", // 搜狐视频
	"le.com", "www.le.com", "yuntv.letv.com", // 乐视视频
	"pptv.com", "v.pptv.com", // PPTV
	"56.com", "www.56.com", // 56网
	"acfun.cn", "www.acfun.cn", // A站
	"zhihu.com", "www.zhihu.com", "video.zhihu.com", // 知乎视频
	"weibo.com", "video.weibo.com", "n.sinaimg.cn", // 微博视频
	"xinpianchang.com", "www.xinpianchang.com", // 新片场
}

// isVideoURL 判断URL是否为视频URL
func isVideoURL(urlStr string) (bool, string) {
	if urlStr == "" {
		return false, ""
	}

	// 解析URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false, ""
	}

	// 检查文件扩展名
	ext := strings.ToLower(path.Ext(parsedURL.Path))
	for _, videoExt := range videoExtensions {
		if ext == videoExt {
			return true, getMimeTypeFromExtension(ext)
		}
	}

	// 检查域名
	host := strings.ToLower(parsedURL.Host)
	for _, videoDomain := range videoDomains {
		if strings.Contains(host, videoDomain) {
			return true, "video/mp4" // 默认使用mp4作为视频MIME类型
		}
	}

	return false, ""
}

// getMimeTypeFromExtension 根据文件扩展名获取MIME类型
func getMimeTypeFromExtension(ext string) string {
	switch ext {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".3gp":
		return "video/3gpp"
	case ".ts":
		return "video/mp2t"
	default:
		return "video/mp4"
	}
}

func init() {
	ImageSymbolAcMachines.Build([][]rune{[]rune(GeminiImageSymbol)})
}

type GeminiChatRequest struct {
	Model             string                     `json:"-"`
	Stream            bool                       `json:"-"`
	Action            string                     `json:"-"` // 添加 Action 字段
	Contents          []GeminiChatContent        `json:"contents"`
	SafetySettings    []GeminiChatSafetySettings `json:"safetySettings,omitempty"`
	GenerationConfig  GeminiChatGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []GeminiChatTools          `json:"tools,omitempty"`
	ToolConfig        *GeminiToolConfig          `json:"toolConfig,omitempty"`
	SystemInstruction any                        `json:"systemInstruction,omitempty"`

	JsonRaw []byte `json:"-"`
}

func (r *GeminiChatRequest) GetJsonRaw() []byte {
	return r.JsonRaw
}

func (r *GeminiChatRequest) SetJsonRaw(c *gin.Context) {
	rawData, err := c.GetRawData()
	if err != nil {
		return
	}
	r.JsonRaw = rawData
}

type GeminiToolConfig struct {
	FunctionCallingConfig *GeminiFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type GeminiFunctionCallingConfig struct {
	Model                string `json:"model,omitempty"`
	AllowedFunctionNames any    `json:"allowedFunctionNames,omitempty"`
}
type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileUri  string `json:"fileUri,omitempty"`
}

type GeminiPart struct {
	FunctionCall        *GeminiFunctionCall            `json:"functionCall,omitempty"`
	FunctionResponse    *GeminiFunctionResponse        `json:"functionResponse,omitempty"`
	Text                string                         `json:"text,omitempty"`
	InlineData          *GeminiInlineData              `json:"inlineData,omitempty"`
	FileData            *GeminiFileData                `json:"fileData,omitempty"`
	ExecutableCode      *GeminiPartExecutableCode      `json:"executableCode,omitempty"`
	CodeExecutionResult *GeminiPartCodeExecutionResult `json:"codeExecutionResult,omitempty"`
	Thought             bool                           `json:"thought,omitempty"` // 是否是思考内容
	ThoughtSignature    json.RawMessage                `json:"thoughtSignature,omitempty"`
	MediaResolution     json.RawMessage                `json:"mediaResolution,omitempty"`
	VideoMetadata       json.RawMessage                `json:"videoMetadata,omitempty"`
}

type GeminiPartExecutableCode struct {
	Language string `json:"language,omitempty"`
	Code     string `json:"code,omitempty"`
}

type GeminiPartCodeExecutionResult struct {
	Outcome string `json:"outcome,omitempty"`
	Output  string `json:"output,omitempty"`
}

type GeminiFunctionCall struct {
	Name string                 `json:"name,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
	Id   string                 `json:"id,omitempty"`
}

func (candidate *GeminiChatCandidate) ToOpenAIStreamChoice(request *types.ChatCompletionRequest) types.ChatCompletionStreamChoice {
	choice := types.ChatCompletionStreamChoice{
		Index: int(candidate.Index),
		Delta: types.ChatCompletionStreamChoiceDelta{
			Role: types.ChatMessageRoleAssistant,
		},
	}

	if candidate.FinishReason != nil {
		choice.FinishReason = ConvertFinishReason(*candidate.FinishReason)
	}

	var content []string
	isTools := false
	images := make([]types.MultimediaData, 0)
	reasoningContent := make([]string, 0)

	for _, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			if choice.Delta.ToolCalls == nil {
				choice.Delta.ToolCalls = make([]*types.ChatCompletionToolCalls, 0)
			}
			isTools = true
			choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, part.FunctionCall.ToOpenAITool())
		} else if part.InlineData != nil {
			imgText := ""
			if strings.HasPrefix(part.InlineData.MimeType, "image/") {

				images = append(images, types.MultimediaData{
					Data: part.InlineData.Data,
				})
				url := ""
				imageData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err == nil {
					url = storage.Upload(imageData, utils.GetUUID()+".png")
				}
				if url == "" {
					imgText = "![image](data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data + ")"
				} else {
					imgText = fmt.Sprintf("%s(%s)", GeminiImageSymbol, url)
				}
				content = append(content, imgText)
			}
			//  else if strings.HasPrefix(part.InlineData.MimeType, "audio/") {
			// 	choice.Message.Audio = types.MultimediaData{
			// 		Data: part.InlineData.Data,
			// 	}
			// }
		} else {
			if part.ExecutableCode != nil {
				content = append(content, "```"+part.ExecutableCode.Language+"\n"+part.ExecutableCode.Code+"\n```")
			} else if part.CodeExecutionResult != nil {
				content = append(content, "```output\n"+part.CodeExecutionResult.Output+"\n```")
			} else if part.Thought {
				reasoningContent = append(reasoningContent, part.Text)
			} else {
				content = append(content, part.Text)
			}
		}
	}

	if len(images) > 0 {
		choice.Delta.Image = images
	}

	// Add grounding metadata as markdown citations
	if candidate.GroundingMetadata != nil && showGoogleSearchMeta(request) {
		groundingMarkdown := formatGroundingMetadataAsMarkdown(candidate.GroundingMetadata)
		if groundingMarkdown != "" {
			content = append(content, "\n\n"+groundingMarkdown)
		}
	}

	choice.Delta.Content = strings.Join(content, "\n")

	if len(reasoningContent) > 0 {
		choice.Delta.ReasoningContent = strings.Join(reasoningContent, "\n")
	}

	// 处理 GroundingMetadata，转换为 OpenAI 格式的 Annotations
	if candidate.GroundingMetadata != nil {
		annotations := candidate.ConvertGroundingToAnnotations()
		if len(annotations) > 0 {
			choice.Delta.Annotations = annotations
		}
	}

	if isTools {
		choice.FinishReason = types.FinishReasonToolCalls
	}
	choice.CheckChoice(request)

	return choice
}

func (candidate *GeminiChatCandidate) ToOpenAIChoice(request *types.ChatCompletionRequest) types.ChatCompletionChoice {
	choice := types.ChatCompletionChoice{
		Index: int(candidate.Index),
		Message: types.ChatCompletionMessage{
			Role: "assistant",
		},
		// FinishReason: types.FinishReasonStop,
	}

	if candidate.FinishReason != nil {
		choice.FinishReason = ConvertFinishReason(*candidate.FinishReason)
	}

	if len(candidate.Content.Parts) == 0 {
		choice.Message.Content = ""
		return choice
	}

	var content []string
	useTools := false
	images := make([]types.MultimediaData, 0)
	reasoningContent := make([]string, 0)

	for _, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			if choice.Message.ToolCalls == nil {
				choice.Message.ToolCalls = make([]*types.ChatCompletionToolCalls, 0)
			}
			useTools = true
			choice.Message.ToolCalls = append(choice.Message.ToolCalls, part.FunctionCall.ToOpenAITool())
		} else if part.InlineData != nil {
			imgText := ""
			if strings.HasPrefix(part.InlineData.MimeType, "image/") {

				images = append(images, types.MultimediaData{
					Data: part.InlineData.Data,
				})
				url := ""
				imageData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err == nil {
					url = storage.Upload(imageData, utils.GetUUID()+".png")
				}
				if url == "" {
					imgText = "![image](data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data + ")"
				} else {
					imgText = fmt.Sprintf("%s(%s)", GeminiImageSymbol, url)
				}
				content = append(content, imgText)
			}
			//  else if strings.HasPrefix(part.InlineData.MimeType, "audio/") {
			// 	choice.Message.Audio = types.MultimediaData{
			// 		Data: part.InlineData.Data,
			// 	}
			// }
		} else {
			if part.ExecutableCode != nil {
				content = append(content, "```"+part.ExecutableCode.Language+"\n"+part.ExecutableCode.Code+"\n```")
			} else if part.CodeExecutionResult != nil {
				content = append(content, "```output\n"+part.CodeExecutionResult.Output+"\n```")
			} else if part.Thought {
				reasoningContent = append(reasoningContent, part.Text)
			} else {
				content = append(content, part.Text)
			}
		}
	}

	choice.Message.Content = strings.Join(content, "\n")

	// Add grounding metadata as markdown citations
	if candidate.GroundingMetadata != nil && showGoogleSearchMeta(request) {
		groundingMarkdown := formatGroundingMetadataAsMarkdown(candidate.GroundingMetadata)
		if groundingMarkdown != "" {
			if contentStr, ok := choice.Message.Content.(string); ok && contentStr != "" {
				choice.Message.Content = contentStr + "\n\n" + groundingMarkdown
			} else {
				choice.Message.Content = groundingMarkdown
			}
		}
	}

	if len(reasoningContent) > 0 {
		choice.Message.ReasoningContent = strings.Join(reasoningContent, "\n")
	}

	if len(images) > 0 {
		choice.Message.Image = images
	}

	// 处理 GroundingMetadata，转换为 OpenAI 格式的 Annotations
	if candidate.GroundingMetadata != nil {
		annotations := candidate.ConvertGroundingToAnnotations()
		if len(annotations) > 0 {
			choice.Message.Annotations = annotations
		}
	}

	if useTools {
		choice.FinishReason = types.FinishReasonToolCalls
	}

	choice.CheckChoice(request)

	return choice
}

type GeminiFunctionResponse struct {
	Name         string          `json:"name,omitempty"`
	Response     any             `json:"response,omitempty"`
	WillContinue json.RawMessage `json:"willContinue,omitempty"`
	Scheduling   json.RawMessage `json:"scheduling,omitempty"`
	Parts        json.RawMessage `json:"parts,omitempty"`
	ID           json.RawMessage `json:"id,omitempty"`
}

type GeminiFunctionResponseContent struct {
	Output string `json:"output,omitempty"`
}

func (g *GeminiFunctionCall) ToOpenAITool() *types.ChatCompletionToolCalls {
	args, _ := json.Marshal(g.Args)

	return &types.ChatCompletionToolCalls{
		Id:    "call_" + utils.GetRandomString(24),
		Type:  types.ChatMessageRoleFunction,
		Index: 0,
		Function: &types.ChatCompletionToolCallsFunction{
			Name:      g.Name,
			Arguments: string(args),
		},
	}
}

type GeminiChatContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts,omitempty"`
}

type GeminiChatSafetySettings struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type GeminiChatTools struct {
	FunctionDeclarations  []types.ChatCompletionFunction `json:"functionDeclarations,omitempty"`
	CodeExecution         *GeminiCodeExecution           `json:"codeExecution,omitempty"`
	GoogleSearch          *GeminiCodeExecution           `json:"googleSearch,omitempty"`
	UrlContext            *GeminiCodeExecution           `json:"urlContext,omitempty"`
	GoogleSearchRetrieval any                            `json:"googleSearchRetrieval,omitempty"`
}

type GeminiCodeExecution struct {
}

type GeminiChatGenerationConfig struct {
	Temperature        *float64        `json:"temperature,omitempty"`
	TopP               *float64        `json:"topP,omitempty"`
	TopK               *float64        `json:"topK,omitempty"`
	MaxOutputTokens    int             `json:"maxOutputTokens,omitempty"`
	CandidateCount     int             `json:"candidateCount,omitempty"`
	StopSequences      []string        `json:"stopSequences,omitempty"`
	ResponseMimeType   string          `json:"responseMimeType,omitempty"`
	ResponseSchema     any             `json:"responseSchema,omitempty"`
	ResponseModalities []string        `json:"responseModalities,omitempty"`
	ImageConfig        *ImageConfig    `json:"imageConfig,omitempty"` // 图像生成配置
	ThinkingConfig     *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"` // 图像宽高比，如 "16:9", "1:1", "9:16" 等
}

type ThinkingConfig struct {
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
	IncludeThoughts bool   `json:"includeThoughts,omitempty"`
}

type GeminiError struct {
	Code    int                  `json:"code"`
	Message string               `json:"message"`
	Status  string               `json:"status"`
	Details []GeminiErrorDetails `json:"details,omitempty"`
}

type GeminiErrorDetails struct {
	Type     string                 `json:"@type,omitempty"`
	Reason   string                 `json:"reason,omitempty"`
	Domain   string                 `json:"domain,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

func (e *GeminiError) Error() string {
	bytes, _ := json.Marshal(e)
	return string(bytes) + "\n"
}

type GeminiErrorResponse struct {
	ErrorInfo *GeminiError `json:"error,omitempty"`
}

func (e *GeminiErrorResponse) Error() string {
	bytes, _ := json.Marshal(e)
	return string(bytes) + "\n"
}

type GeminiChatResponse struct {
	Candidates     []GeminiChatCandidate    `json:"candidates"`
	PromptFeedback GeminiChatPromptFeedback `json:"promptFeedback"`
	UsageMetadata  *GeminiUsageMetadata     `json:"usageMetadata,omitempty"`
	ModelVersion   string                   `json:"modelVersion,omitempty"`
	Model          string                   `json:"model,omitempty"`
	ResponseId     string                   `json:"responseId,omitempty"`

	// Vertex AI countTokens 响应字段
	TotalTokens             int                          `json:"totalTokens,omitempty"`
	TotalBillableCharacters int                          `json:"totalBillableCharacters,omitempty"`
	PromptTokensDetails     []GeminiUsageMetadataDetails `json:"promptTokensDetails,omitempty"`

	GeminiErrorResponse
}

type GeminiUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount,omitempty"`
	ToolUsePromptTokenCount int `json:"toolUsePromptTokenCount,omitempty"`

	PromptTokensDetails     []GeminiUsageMetadataDetails `json:"promptTokensDetails,omitempty"`
	CandidatesTokensDetails []GeminiUsageMetadataDetails `json:"candidatesTokensDetails,omitempty"`
}

type GeminiUsageMetadataDetails struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

type GeminiChatCandidate struct {
	Content               GeminiChatContent        `json:"content"`
	FinishReason          *string                  `json:"finishReason,omitempty"`
	Index                 int64                    `json:"index"`
	SafetyRatings         []GeminiChatSafetyRating `json:"safetyRatings"`
	CitationMetadata      any                      `json:"citationMetadata,omitempty"`
	TokenCount            int                      `json:"tokenCount,omitempty"`
	GroundingAttributions []any                    `json:"groundingAttributions,omitempty"`
	GroundingMetadata     *GeminiGroundingMetadata `json:"groundingMetadata,omitempty"`
	AvgLogprobs           any                      `json:"avgLogprobs,omitempty"`
}

type GeminiChatSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// GroundingMetadata 相关结构定义
type GeminiGroundingMetadata struct {
	SearchEntryPoint  *GeminiSearchEntryPoint  `json:"searchEntryPoint,omitempty"`
	GroundingChunks   []GeminiGroundingChunk   `json:"groundingChunks,omitempty"`
	GroundingSupports []GeminiGroundingSupport `json:"groundingSupports,omitempty"`
	WebSearchQueries  []string                 `json:"webSearchQueries,omitempty"`
}

type GeminiSearchEntryPoint struct {
	RenderedContent string `json:"renderedContent,omitempty"`
}

type GeminiGroundingChunk struct {
	Web *GeminiWebChunk `json:"web,omitempty"`
}

type GeminiWebChunk struct {
	Uri   string `json:"uri,omitempty"`
	Title string `json:"title,omitempty"`
}

type GeminiGroundingSupport struct {
	Segment               *GeminiGroundingSegment `json:"segment,omitempty"`
	GroundingChunkIndices []int                   `json:"groundingChunkIndices,omitempty"`
}

type GeminiGroundingSegment struct {
	StartIndex int    `json:"startIndex,omitempty"`
	EndIndex   int    `json:"endIndex,omitempty"`
	Text       string `json:"text,omitempty"`
}

// ConvertGroundingToAnnotations 将 Gemini GroundingMetadata 转换为 OpenAI Annotations 格式
func (candidate *GeminiChatCandidate) ConvertGroundingToAnnotations() []types.Annotations {
	if candidate.GroundingMetadata == nil || len(candidate.GroundingMetadata.GroundingSupports) == 0 {
		return nil
	}

	var annotations []types.Annotations
	// 使用 map 来去重，避免重复的引用
	seenAnnotations := make(map[string]bool)

	// 遍历 GroundingSupports，为每个支持的文本段创建引用
	for _, support := range candidate.GroundingMetadata.GroundingSupports {
		if support.Segment == nil || len(support.GroundingChunkIndices) == 0 {
			continue
		}

		// 为每个引用的 chunk 创建 annotation
		for _, chunkIndex := range support.GroundingChunkIndices {
			if chunkIndex >= 0 && chunkIndex < len(candidate.GroundingMetadata.GroundingChunks) {
				chunk := candidate.GroundingMetadata.GroundingChunks[chunkIndex]
				if chunk.Web != nil && chunk.Web.Uri != "" {
					// 创建唯一键来去重
					key := fmt.Sprintf("%s_%d_%d", chunk.Web.Uri, support.Segment.StartIndex, support.Segment.EndIndex)
					if !seenAnnotations[key] {
						annotation := types.Annotations{
							Type:       "url_citation",
							Url:        chunk.Web.Uri,
							Title:      chunk.Web.Title,
							StartIndex: support.Segment.StartIndex,
							EndIndex:   support.Segment.EndIndex,
						}
						annotations = append(annotations, annotation)
						seenAnnotations[key] = true
					}
				}
			}
		}
	}

	return annotations
}

type GeminiChatPromptFeedback struct {
	BlockReason   string                   `json:"blockReason"`
	SafetyRatings []GeminiChatSafetyRating `json:"safetyRatings"`
}

func (g *GeminiChatResponse) GetResponseText() string {
	if g == nil {
		return ""
	}
	if len(g.Candidates) > 0 && len(g.Candidates[0].Content.Parts) > 0 {
		return g.Candidates[0].Content.Parts[0].Text
	}
	return ""
}

func OpenAIToGeminiChatContent(openaiContents []types.ChatCompletionMessage) ([]GeminiChatContent, string, *types.OpenAIErrorWithStatusCode) {
	contents := make([]GeminiChatContent, 0)
	// useToolName := ""
	var systemContent []string
	toolCallId := make(map[string]string)

	for _, openaiContent := range openaiContents {
		if openaiContent.IsSystemRole() {
			systemContent = append(systemContent, openaiContent.StringContent())
			continue
		}

		content := GeminiChatContent{
			Role:  ConvertRole(openaiContent.Role),
			Parts: make([]GeminiPart, 0),
		}
		openaiContent.FuncToToolCalls()

		if openaiContent.ToolCalls != nil {
			for _, toolCall := range openaiContent.ToolCalls {
				toolCallId[toolCall.Id] = toolCall.Function.Name

				args := map[string]interface{}{}
				if toolCall.Function.Arguments != "" {
					json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
				}

				content.Parts = append(content.Parts, GeminiPart{
					FunctionCall: &GeminiFunctionCall{
						Name: toolCall.Function.Name,
						Args: args,
						Id:   toolCall.Id,
					},
				})

			}
			text := openaiContent.StringContent()
			if text != "" {
				contents = append(contents, createSystemResponse(text))
			}
		} else if openaiContent.Role == types.ChatMessageRoleFunction || openaiContent.Role == types.ChatMessageRoleTool {
			if openaiContent.Name == nil {
				if toolName, exists := toolCallId[openaiContent.ToolCallID]; exists {
					openaiContent.Name = &toolName
				}
			}

			// 安全检查：如果 Name 仍然为 nil，跳过这个工具结果
			if openaiContent.Name == nil {
				// 跳过没有名称的工具结果消息
				continue
			}

			// 构建 ID 字段（如果有 ToolCallID）
			var idField json.RawMessage
			if openaiContent.ToolCallID != "" {
				if idBytes, err := json.Marshal(openaiContent.ToolCallID); err == nil {
					idField = idBytes
				}
			}

			functionPart := GeminiPart{
				FunctionResponse: &GeminiFunctionResponse{
					Name: *openaiContent.Name,
					ID:   idField,
					Response: GeminiFunctionResponseContent{
						Output: openaiContent.StringContent(),
					},
				},
			}

			if len(contents) > 0 && contents[len(contents)-1].Role == "function" {
				contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, functionPart)
			} else {
				contents = append(contents, GeminiChatContent{
					Role:  "function",
					Parts: []GeminiPart{functionPart},
				})
			}

			continue
		} else {
			openaiMessagePart := openaiContent.ParseContent()
			imageNum := 0
			for _, openaiPart := range openaiMessagePart {
				// 处理 thinking 和 redacted_thinking 类型
				if openaiPart.Type == "thinking" || openaiPart.Type == "redacted_thinking" {
					sig := openaiPart.ThinkingSignature
					// 签名缺失或长度不足时注入哨兵值，让 Gemini 跳过签名校验；
					// 合法的 Gemini 签名（round-trip 回来的）长度 >= 50，原样透传
					if sig == "" || len(sig) < minThoughtSignatureLength {
						sig = skipThoughtSignatureValidator
					}
					var sigField json.RawMessage
					if sigBytes, err := json.Marshal(sig); err == nil {
						sigField = sigBytes
					}
					content.Parts = append(content.Parts, GeminiPart{
						Text:             openaiPart.Thinking,
						Thought:          true,
						ThoughtSignature: sigField,
					})
					continue
				}

				if openaiPart.Type == types.ContentTypeText {
					// 过滤纯空白文本
					if isEmptyOrOnlyNewlines(openaiPart.Text) {
						continue
					}
					imageSymbols := ImageSymbolAcMachines.MultiPatternSearch([]rune(openaiPart.Text), false)
					if len(imageSymbols) > 0 {
						lastEndPos := 0 // 上一段文本的结束位置
						textRunes := []rune(openaiPart.Text)
						geminiImageSymbolRunesLen := len([]rune(GeminiImageSymbol))
						// 提取图片地址
						for _, match := range imageSymbols {
							// 添加图片符号前面的文本，如果不为空且不仅包含换行符
							if match.Pos > lastEndPos {
								textSegment := string(textRunes[lastEndPos:match.Pos])
								if !isEmptyOrOnlyNewlines(textSegment) {
									content.Parts = append(content.Parts, GeminiPart{
										Text: textSegment,
									})
								}
							}

							pos := match.Pos + geminiImageSymbolRunesLen

							if pos < len(textRunes) && textRunes[pos] == '(' {
								endPos := -1
								for i := pos + 1; i < len(textRunes); i++ {
									if textRunes[i] == ')' {
										endPos = i
										break
									}
								}
								if endPos > 0 {
									imageUrl := string(textRunes[pos+1 : endPos])
									// 处理图片URL
									mimeType, data, err := image.GetImageFromUrl(imageUrl)
									if err == nil {
										content.Parts = append(content.Parts, GeminiPart{
											InlineData: &GeminiInlineData{
												MimeType: mimeType,
												Data:     data,
											},
										})
									}
									lastEndPos = endPos + 1
								}
							}

							// 添加最后一个图片符号后面的文本，如果不为空且不仅包含换行符
							if lastEndPos < len(textRunes) {
								finalText := string(textRunes[lastEndPos:])
								if !isEmptyOrOnlyNewlines(finalText) {
									content.Parts = append(content.Parts, GeminiPart{
										Text: finalText,
									})
								}
							}
						}
					} else {
						content.Parts = append(content.Parts, GeminiPart{
							Text: openaiPart.Text,
						})
					}

				} else if openaiPart.Type == types.ContentTypeImageURL {
					// 检查是否为视频URL
					isVideo, videoMimeType := isVideoURL(openaiPart.ImageURL.URL)
					if isVideo {
						// 视频使用fileData
						content.Parts = append(content.Parts, GeminiPart{
							FileData: &GeminiFileData{
								MimeType: videoMimeType,
								FileUri:  openaiPart.ImageURL.URL,
							},
						})
					} else {
						// 图片使用inlineData
						imageNum += 1
						if imageNum > GeminiVisionMaxImageNum {
							continue
						}
						mimeType, data, err := image.GetImageFromUrl(openaiPart.ImageURL.URL)
						if err != nil {
							return nil, "", common.ErrorWrapper(err, "image_url_invalid", http.StatusBadRequest)
						}
						content.Parts = append(content.Parts, GeminiPart{
							InlineData: &GeminiInlineData{
								MimeType: mimeType,
								Data:     data,
							},
						})
					}
				}
			}
		}

		// 确保每个消息至少有一个 part，避免 Gemini API 错误
		if len(content.Parts) == 0 {
			// 如果没有任何 parts，添加一个空文本 part
			content.Parts = append(content.Parts, GeminiPart{
				Text: " ", // 使用空格而不是空字符串
			})
		}

		contents = append(contents, content)

	}

	return contents, strings.Join(systemContent, "\n"), nil
}

func createSystemResponse(text string) GeminiChatContent {
	return GeminiChatContent{
		Role: "model",
		Parts: []GeminiPart{
			{
				Text: text,
			},
		},
	}
}

type ModelListResponse struct {
	Models []ModelDetails `json:"models"`
}

type ModelDetails struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

type GeminiErrorWithStatusCode struct {
	GeminiErrorResponse
	StatusCode int  `json:"status_code"`
	LocalError bool `json:"-"`
}

func (e *GeminiErrorWithStatusCode) ToOpenAiError() *types.OpenAIErrorWithStatusCode {
	return &types.OpenAIErrorWithStatusCode{
		StatusCode: e.StatusCode,
		OpenAIError: types.OpenAIError{
			Code:    e.ErrorInfo.Code,
			Type:    e.ErrorInfo.Status,
			Message: e.ErrorInfo.Message,
		},
		LocalError: e.LocalError,
	}
}

type GeminiErrors []*GeminiErrorResponse

func (e *GeminiErrors) Error() *GeminiErrorResponse {
	return (*e)[0]
}

type GeminiImageRequest struct {
	Instances  []GeminiImageInstance        `json:"instances"`
	Parameters GeminiImageParametersDynamic `json:"parameters"`
}

type GeminiImageInstance struct {
	Prompt string `json:"prompt"`
}

type GeminiImageParameters struct {
	PersonGeneration string `json:"personGeneration,omitempty"`
	AspectRatio      string `json:"aspectRatio,omitempty"`
	SampleCount      int    `json:"sampleCount,omitempty"`
}

// 动态参数结构，用于完全透传
type GeminiImageParametersDynamic map[string]interface{}

type GeminiImageResponse struct {
	Predictions []GeminiImagePrediction `json:"predictions"`
}

type GeminiImagePrediction struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded"`
	MimeType           string `json:"mimeType"`
	RaiFilteredReason  string `json:"raiFilteredReason,omitempty"`
	SafetyAttributes   any    `json:"safetyAttributes,omitempty"`
}

// Veo 3.0 Video Generation Types
type VeoVideoRequest struct {
	Instances  []VeoVideoInstance  `json:"instances"`
	Parameters *VeoVideoParameters `json:"parameters,omitempty"`
}

type VeoVideoInstance struct {
	Prompt string `json:"prompt"`
}

type VeoVideoParameters struct {
	AspectRatio     string `json:"aspectRatio,omitempty"`     // e.g., "16:9"
	NegativePrompt  string `json:"negativePrompt,omitempty"`  // e.g., "cartoon, drawing, low quality"
	SampleCount     int    `json:"sampleCount,omitempty"`     // Number of videos to generate
	DurationSeconds int    `json:"durationSeconds,omitempty"` // Video duration
}

// Veo 3.0 Long Running Operation Response
type VeoLongRunningResponse struct {
	Name     string                 `json:"name"` // Operation name for polling
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Done     bool                   `json:"done"`
	Response *VeoVideoResponse      `json:"response,omitempty"`
	Error    *VeoOperationError     `json:"error,omitempty"`
}

type VeoVideoResponse struct {
	GenerateVideoResponse *VeoGenerateVideoResponse `json:"generateVideoResponse,omitempty"`
}

type VeoGenerateVideoResponse struct {
	GeneratedSamples []VeoGeneratedSample `json:"generatedSamples"`
}

type VeoGeneratedSample struct {
	Video *VeoVideoData `json:"video"`
}

type VeoVideoData struct {
	Uri      string `json:"uri"`      // Download URI
	MimeType string `json:"mimeType"` // e.g., "video/mp4"
}

type VeoOperationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func isEmptyOrOnlyNewlines(s string) bool {
	trimmed := strings.TrimSpace(s)
	return trimmed == ""
}

// checks if googleSearch tool has "show" parameter
func showGoogleSearchMeta(request *types.ChatCompletionRequest) bool {
	functions := request.GetFunctions()
	if functions == nil {
		return false
	}

	for _, function := range functions {
		if function.Name == "googleSearch" && function.Parameters != nil {
			if paramStr, ok := function.Parameters.(string); ok && paramStr == "show" {
				return true
			}
		}
	}

	return false
}

// formats grounding metadata as markdown citation
func formatGroundingMetadataAsMarkdown(metadata *GeminiGroundingMetadata) string {
	if metadata == nil || len(metadata.GroundingChunks) == 0 {
		return ""
	}
	var result strings.Builder
	// Add search queries
	if len(metadata.WebSearchQueries) > 0 {
		result.WriteString("> Searched ")
		for i, query := range metadata.WebSearchQueries {
			if i > 0 {
				result.WriteString(" and ")
			}
			result.WriteString(fmt.Sprintf(`"%s"`, query))
		}
		result.WriteString("\n")
	}
	// Add grounding chunks as numbered list
	linkCount := 0
	for _, chunk := range metadata.GroundingChunks {
		if chunk.Web != nil && chunk.Web.Uri != "" {
			linkCount++
			title := chunk.Web.Title
			if title == "" {
				title = chunk.Web.Uri
			}
			result.WriteString(fmt.Sprintf("> %d. [%s](%s)\n", linkCount, title, chunk.Web.Uri))
		}
	}
	return result.String()
}
