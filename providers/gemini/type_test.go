package gemini

import (
	"done-hub/types"
	"encoding/json"
	"strings"
	"testing"
)

// 原生 generateContent 非流式 relay 会把上游响应解析进 GeminiChatResponse 再重新序列化给客户端，
// 结构体缺字段就会把上游字段吞掉。Long Decode 续写依赖 candidates[].continuationToken 回传，
// 这里固定该字段在 Unmarshal → Marshal 往返后仍然保留。
func TestGeminiChatResponse_ContinuationTokenRoundTrip(t *testing.T) {
	upstream := `{
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "partial"}]},
			"finishReason": "CONTINUATION",
			"index": 0,
			"continuationToken": "CiQ4ZmQ3-token"
		}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 32768, "totalTokenCount": 32778}
	}`

	var resp GeminiChatResponse
	if err := json.Unmarshal([]byte(upstream), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp.Candidates[0].ContinuationToken; got != "CiQ4ZmQ3-token" {
		t.Fatalf("ContinuationToken = %q, want %q", got, "CiQ4ZmQ3-token")
	}

	out, err := json.Marshal(&resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"continuationToken":"CiQ4ZmQ3-token"`) {
		t.Fatalf("continuationToken dropped after round trip: %s", out)
	}
	if !strings.Contains(string(out), `"finishReason":"CONTINUATION"`) {
		t.Fatalf("finishReason dropped after round trip: %s", out)
	}
}

// 没有续写时不应凭空多出一个空的 continuationToken 字段。
func TestGeminiChatResponse_ContinuationTokenOmittedWhenEmpty(t *testing.T) {
	resp := GeminiChatResponse{Candidates: []GeminiChatCandidate{{Content: GeminiChatContent{Role: "model"}}}}
	out, err := json.Marshal(&resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "continuationToken") {
		t.Fatalf("empty continuationToken should be omitted: %s", out)
	}
}

func TestConvertFinishReason_Continuation(t *testing.T) {
	if got := ConvertFinishReason("CONTINUATION"); got != types.FinishReasonLength {
		t.Fatalf("CONTINUATION → %q, want %q", got, types.FinishReasonLength)
	}
	if got := ConvertFinishReason("SAFETY"); got != types.FinishReasonContentFilter {
		t.Fatalf("SAFETY → %q, want %q", got, types.FinishReasonContentFilter)
	}
}
