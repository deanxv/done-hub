package model

import "testing"

func strPtr(s string) *string { return &s }

func TestShouldUseResponsesForModel(t *testing.T) {
	tests := []struct {
		name   string
		custom *string
		model  string
		want   bool
	}{
		// 未配置：继承全局默认表，对齐改造前的 need2Response
		{"nil inherits default hit", nil, "o3-pro", true},
		{"nil inherits default miss", nil, "gpt-4o", false},
		// 留空（前端保存时 null 会被转成 ""）：同样继承默认表，避免保存一次后静默回退 chat
		{"empty inherits default", strPtr(""), "o1-pro", true},
		{"whitespace inherits default", strPtr("  \n "), "codex-mini-latest", true},
		// 自定义列表：全量覆盖，不与默认表合并
		{"custom hit", strPtr("gpt-5-pro\nfoo"), "gpt-5-pro", true},
		{"custom overrides default", strPtr("gpt-5-pro"), "o3-pro", false},
		{"custom comma separated", strPtr("a, b ,c"), "b", true},
		{"custom json array", strPtr(`["x","y"]`), "y", true},
		{"empty model", nil, "  ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &Channel{Need2ResponseModels: tt.custom}
			if got := ch.ShouldUseResponsesForModel(tt.model); got != tt.want {
				t.Errorf("ShouldUseResponsesForModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
