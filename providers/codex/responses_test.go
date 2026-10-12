package codex

import (
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/types"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// chat→responses 兼容路径（ConvertChat=true）必须把上游 responses 事件转换为 chat chunk，不能原样透传给 chat 客户端。
func TestCreateResponsesStreamConvertChat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"hi"}`+"\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`+"\n\n")
	}))
	defer upstream.Close()

	// InitHttpClient 会打日志，测试环境未 SetupLogger
	oldLogger := logger.Logger
	logger.Logger = zap.NewNop()
	defer func() { logger.Logger = oldLogger }()
	requester.InitHttpClient()
	baseURL := upstream.URL
	channel := &model.Channel{Type: 59, Key: "token", BaseURL: &baseURL}
	provider := CodexProviderFactory{}.Create(channel).(*CodexProvider)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	provider.SetContext(ctx)
	provider.SetUsage(&types.Usage{})

	stream, errWithCode := provider.CreateResponsesStream(&types.OpenAIResponsesRequest{Model: "codex-mini-latest", ConvertChat: true})
	if errWithCode != nil {
		t.Fatal(errWithCode.Message)
	}
	defer stream.Close()

	dataChan, errChan := stream.Recv()
	var chunks []string
	for done := false; !done; {
		select {
		case data, ok := <-dataChan:
			if !ok {
				done = true
				break
			}
			chunks = append(chunks, data)
		case <-errChan:
			done = true
		}
	}

	if len(chunks) == 0 {
		t.Fatal("no chat chunk received")
	}
	for _, chunk := range chunks {
		if strings.Contains(chunk, "event:") || !strings.Contains(chunk, "chat.completion.chunk") {
			t.Fatalf("expected chat chunk, got %q", chunk)
		}
	}
}
