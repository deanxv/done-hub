package controller

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/notify"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/providers"
	providers_base "done-hub/providers/base"
	"done-hub/types"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	embeddingsRegex = regexp.MustCompile(`(?:^text-|embed|Embed|rerank|davinci|babbage|bge-|e5-|LLM2Vec|retrieval|uae-|gte-|jina-clip|jina-embeddings)`)
	imageRegex      = regexp.MustCompile(`flux|diffusion|stabilityai|sd-|dall|cogview|janus|image`)
	responseRegex   = regexp.MustCompile(`(?:^o[1-9])`)
	noSupportRegex  = regexp.MustCompile(`(?:^tts|rerank|whisper|speech|^mj_|^chirp)`)
)

func testChannel(channel *model.Channel, testModel string) (openaiErr *types.OpenAIErrorWithStatusCode, err error) {
	if testModel == "" {
		testModel = channel.TestModel
		if testModel == "" {
			return nil, errors.New("请填写测速模型后再试")
		}
	}

	// 解析模型名称中的 # 后缀（用于 o1/o3-mini/gpt-5 的 reasoning_effort 等参数）
	// 例如：gpt-5#low -> gpt-5 (otherArg: low)
	var otherArg string
	parts := strings.Split(testModel, "#")
	if len(parts) > 1 {
		otherArg = parts[1]
		testModel = parts[0]
	}

	channelType := getModelType(testModel)
	channel.SetProxy()

	var url string
	switch channelType {
	case "embeddings":
		url = "/v1/embeddings"
	case "image":
		url = "/v1/images/generations"
	case "chat":
		url = "/v1/chat/completions"
	case "response":
		url = "/v1/responses"
	default:
		return nil, errors.New("不支持的模型类型")
	}

	// 创建测试上下文
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	// 获取并验证provider
	provider := providers.GetProvider(channel, c)
	if provider == nil {
		return nil, errors.New("channel not implemented")
	}

	// 设置 otherArg（用于 reasoning_effort 等参数）
	if otherArg != "" {
		provider.SetOtherArg(otherArg)
	}

	newModelName, err := provider.ModelMappingHandler(testModel)
	if err != nil {
		return nil, err
	}

	newModelName = strings.TrimPrefix(newModelName, "+")

	usage := &types.Usage{}
	provider.SetUsage(usage)

	// 执行测试请求
	var response any
	var openAIErrorWithStatusCode *types.OpenAIErrorWithStatusCode

	switch channelType {
	case "embeddings":
		embeddingsProvider, ok := provider.(providers_base.EmbeddingsInterface)
		if !ok {
			return nil, errors.New("channel not implemented")
		}
		testRequest := &types.EmbeddingRequest{
			Model: newModelName,
			Input: "hi",
		}
		response, openAIErrorWithStatusCode = embeddingsProvider.CreateEmbeddings(testRequest)
	case "image":
		imageProvider, ok := provider.(providers_base.ImageGenerationsInterface)
		if !ok {
			return nil, errors.New("channel not implemented")
		}

		testRequest := &types.ImageRequest{
			Model:  newModelName,
			Prompt: "A cute cat",
			N:      1,
		}
		response, openAIErrorWithStatusCode = imageProvider.CreateImageGenerations(testRequest)
	case "response":
		responseProvider, ok := provider.(providers_base.ResponsesInterface)
		if !ok {
			return nil, errors.New("channel not implemented")
		}

		response, openAIErrorWithStatusCode = createResponsesForTest(responseProvider, newModelName, false)
	case "chat":
		if channel.ShouldUseResponsesForModel(newModelName) {
			responseProvider, ok := provider.(providers_base.ResponsesInterface)
			if ok {
				response, openAIErrorWithStatusCode = createResponsesForTest(responseProvider, newModelName, true)
				break
			}
		}

		chatProvider, ok := provider.(providers_base.ChatInterface)
		if !ok {
			return nil, errors.New("channel not implemented")
		}
		testRequest := &types.ChatCompletionRequest{
			Messages: []types.ChatCompletionMessage{
				{
					Role:    "user",
					Content: "You just need to output 'hi' next.",
				},
			},
			Model:  newModelName,
			Stream: false,
		}

		response, openAIErrorWithStatusCode = chatProvider.CreateChatCompletion(testRequest)
	default:
		return nil, errors.New("不支持的模型类型")
	}

	if openAIErrorWithStatusCode != nil {
		return openAIErrorWithStatusCode, errors.New(openAIErrorWithStatusCode.Message)
	}

	// 转换为JSON字符串
	jsonBytes, _ := json.Marshal(response)
	logger.SysLog(fmt.Sprintf("测试渠道 %s : %s 返回内容为：%s", channel.Name, newModelName, string(jsonBytes)))

	return nil, nil
}

func getModelType(modelName string) string {
	if noSupportRegex.MatchString(modelName) {
		return "noSupport"
	}

	if embeddingsRegex.MatchString(modelName) {
		return "embeddings"
	}

	if imageRegex.MatchString(modelName) {
		return "image"
	}

	if responseRegex.MatchString(modelName) {
		return "response"
	}

	return "chat"
}

func buildTestResponsesRequest(modelName string) *types.OpenAIResponsesRequest {
	chatRequest := &types.ChatCompletionRequest{
		Messages: []types.ChatCompletionMessage{
			{
				Role:    "user",
				Content: "You just need to output 'hi' next.",
			},
		},
		Model:  modelName,
		Stream: false,
	}

	responsesRequest := chatRequest.ToResponsesRequest()
	responsesRequest.Model = modelName
	responsesRequest.Stream = false
	return responsesRequest
}

func createResponsesForTest(responseProvider providers_base.ResponsesInterface, modelName string, convertChat bool) (any, *types.OpenAIErrorWithStatusCode) {
	streamRequest := buildTestResponsesRequest(modelName)
	streamRequest.ConvertChat = convertChat
	streamRequest.Stream = true

	stream, streamErr := responseProvider.CreateResponsesStream(streamRequest)
	if streamErr != nil {
		return nil, streamErr
	}
	defer stream.Close()

	dataChan, errChan := stream.Recv()
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case data, ok := <-dataChan:
			if !ok {
				return nil, common.StringErrorWrapperLocal("responses stream has no data", "no_response", http.StatusInternalServerError)
			}

			if strings.TrimSpace(data) == "" {
				continue
			}

			return map[string]any{
				"model":        modelName,
				"mode":         "responses_stream",
				"convert_chat": convertChat,
				"ok":           true,
			}, nil
		case readErr, ok := <-errChan:
			if !ok {
				continue
			}
			if readErr != nil {
				if strings.Contains(strings.ToLower(readErr.Error()), "eof") {
					continue
				}
				return nil, common.ErrorWrapperLocal(readErr, "stream_read_failed", http.StatusInternalServerError)
			}
		case <-timeout.C:
			return nil, common.StringErrorWrapperLocal("responses stream test timeout", "stream_timeout", http.StatusGatewayTimeout)
		}
	}
}

func TestChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel, err := model.GetChannelById(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	testModel := c.Query("model")
	tik := time.Now()
	openaiErr, err := testChannel(channel, testModel)
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	consumedTime := float64(milliseconds) / 1000.0

	success := false
	msg := ""
	if openaiErr != nil {
		if ShouldDisableChannel(channel.Type, openaiErr) {
			msg = fmt.Sprintf("测速失败，已被禁用，原因：%s", err.Error())
			DisableChannel(channel.Id, channel.Name, err.Error(), false)
		} else {
			msg = fmt.Sprintf("测速失败，原因：%s", err.Error())
		}
	} else if err != nil {
		msg = fmt.Sprintf("测速失败，原因：%s", err.Error())
	} else {
		success = true
		msg = "测速成功"
		go channel.UpdateResponseTime(milliseconds)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": msg,
		"time":    consumedTime,
	})
}

var testAllChannelsLock sync.Mutex
var testAllChannelsRunning = false

func testAllChannels(isNotify bool) error {
	testAllChannelsLock.Lock()
	if testAllChannelsRunning {
		testAllChannelsLock.Unlock()
		return errors.New("测试已在运行中")
	}
	testAllChannelsRunning = true
	testAllChannelsLock.Unlock()
	channels, err := model.GetAllChannels()
	if err != nil {
		testAllChannelsLock.Lock()
		testAllChannelsRunning = false
		testAllChannelsLock.Unlock()
		return err
	}
	var disableThreshold = int64(config.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000 // a impossible value
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("testAllChannels panic: %v\n%s", r, debug.Stack()))
			}
			testAllChannelsLock.Lock()
			testAllChannelsRunning = false
			testAllChannelsLock.Unlock()
			debug.FreeOSMemory()
		}()

		var sb strings.Builder
		for _, channel := range channels {
			time.Sleep(config.RequestInterval)

			isChannelEnabled := channel.Status == config.ChannelStatusEnabled
			sb.WriteString(fmt.Sprintf("**通道 %s - #%d - %s** : \n\n", utils.EscapeMarkdownText(channel.Name), channel.Id, channel.StatusToStr()))
			tik := time.Now()
			openaiErr, err := testChannel(channel, "")
			tok := time.Now()
			milliseconds := tok.Sub(tik).Milliseconds()
			// 通道为禁用状态，并且还是请求错误 或者 响应时间超过阈值 直接跳过，也不需要更新响应时间。
			if !isChannelEnabled {
				if err != nil {
					sb.WriteString(fmt.Sprintf("- 测试报错: %s \n\n- 无需改变状态，跳过\n\n", utils.EscapeMarkdownText(err.Error())))
					continue
				}
				if milliseconds > disableThreshold {
					sb.WriteString(fmt.Sprintf("- 响应时间 %.2fs 超过阈值 %.2fs \n\n- 无需改变状态，跳过\n\n", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0))
					continue
				}
				// 如果已被禁用，但是请求成功，需要判断是否需要恢复
				// 手动禁用的通道，不会自动恢复
				if shouldEnableChannel(err, openaiErr) {
					if channel.Status == config.ChannelStatusAutoDisabled {
						EnableChannel(channel.Id, channel.Name, false)
						sb.WriteString("- 已被启用 \n\n")
					} else {
						sb.WriteString("- 手动禁用的通道，不会自动恢复 \n\n")
					}
				}
			} else {
				// 如果通道启用状态，但是返回了错误 或者 响应时间超过阈值，需要判断是否需要禁用
				if milliseconds > disableThreshold {
					errMsg := fmt.Sprintf("响应时间 %.2fs 超过阈值 %.2fs ", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
					sb.WriteString(fmt.Sprintf("- %s \n\n- 禁用\n\n", errMsg))
					DisableChannel(channel.Id, channel.Name, errMsg, false)
					continue
				}

				if ShouldDisableChannel(channel.Type, openaiErr) {
					sb.WriteString(fmt.Sprintf("- 已被禁用，原因：%s\n\n", utils.EscapeMarkdownText(err.Error())))
					DisableChannel(channel.Id, channel.Name, err.Error(), false)
					continue
				}

				if err != nil {
					sb.WriteString(fmt.Sprintf("- 测试报错: %s \n\n", utils.EscapeMarkdownText(err.Error())))
					continue
				}
			}
			channel.UpdateResponseTime(milliseconds)
			sb.WriteString(fmt.Sprintf("- 测试完成，耗时 %.2fs\n\n", float64(milliseconds)/1000.0))
		}
		if isNotify {
			notify.Send("通道测试完成", sb.String())
		}
	}()
	return nil
}

func TestAllChannels(c *gin.Context) {
	err := testAllChannels(true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func AutomaticallyTestChannels(frequency int) {
	if frequency <= 0 {
		return
	}

	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		logger.SysLog("testing all channels")
		_ = testAllChannels(false)
		logger.SysLog("channel test finished")
	}
}
