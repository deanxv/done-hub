package openai

import (
	"done-hub/common/config"
	"done-hub/types"
	"encoding/json"
	"strings"
	"testing"
)

// OpenAI Images 接口的 safety_identifier 是双向字段：请求里传入、响应里回显本次实际生效值。
// 结构体编码路径（未开 PassThroughBody）下，请求/响应/合成 SSE 三处都要显式接住才不会静默丢参。
func TestImageSafetyIdentifier_RequestBodyCarriesField(t *testing.T) {
	p := newStoreTestProvider("{}", false)

	t.Run("present when set", func(t *testing.T) {
		req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, "gpt-image-1", &types.ImageRequest{
			Model: "gpt-image-1", Prompt: "a cat", SafetyIdentifier: "user_hash_123",
		})
		if errWithCode != nil {
			t.Fatalf("unexpected error: %v", errWithCode)
		}
		got := decodeBody(t, req)
		if got["safety_identifier"] != "user_hash_123" {
			t.Fatalf("safety_identifier must be forwarded upstream, got %v", got)
		}
	})

	t.Run("omitted when empty", func(t *testing.T) {
		req, errWithCode := p.GetRequestTextBody(config.RelayModeImagesGenerations, "gpt-image-1", &types.ImageRequest{
			Model: "gpt-image-1", Prompt: "a cat",
		})
		if errWithCode != nil {
			t.Fatalf("unexpected error: %v", errWithCode)
		}
		got := decodeBody(t, req)
		if _, ok := got["safety_identifier"]; ok {
			t.Fatalf("empty safety_identifier must be omitted, got %v", got)
		}
	})
}

func TestImageSafetyIdentifier_ResponseRoundTrip(t *testing.T) {
	upstream := `{"created":1700000000,"data":[{"b64_json":"QUJD"}],"quality":"high","size":"1024x1024","safety_identifier":"user_hash_123","usage":{"total_tokens":10,"input_tokens":2,"output_tokens":8}}`

	var resp OpenAIProviderImageResponse
	if err := json.Unmarshal([]byte(upstream), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(&resp.ImageResponse)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"safety_identifier":"user_hash_123"`) {
		t.Fatalf("safety_identifier dropped after round trip: %s", out)
	}

	t.Run("omitted when upstream has none", func(t *testing.T) {
		var resp OpenAIProviderImageResponse
		if err := json.Unmarshal([]byte(`{"created":1,"data":[{"url":"https://x/y.png"}]}`), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		out, _ := json.Marshal(&resp.ImageResponse)
		if strings.Contains(string(out), "safety_identifier") {
			t.Fatalf("missing safety_identifier must not be emitted: %s", out)
		}
	})
}

func TestImageSafetyIdentifier_SyntheticSSECarriesField(t *testing.T) {
	resp := &types.ImageResponse{
		Created:          1700000000,
		Data:             []types.ImageResponseDataInner{{B64JSON: "QUJD"}},
		SafetyIdentifier: json.RawMessage(`"user_hash_123"`),
	}
	sse := ImageResponseToSSE(ImageGenerationStreamPrefix, resp)
	if !strings.Contains(sse, `"safety_identifier":"user_hash_123"`) {
		t.Fatalf("synthetic completed event must echo safety_identifier: %s", sse)
	}
}
