package vision

import "testing"

func TestSupportsVisionByModelName(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"qwen-vl-max", true},
		{"gpt-4o", false},          // "o" 不是 vl token，"omni" 才算
		{"glm-4v", false},          // 不含关键词
		{"some-model-vision", true},
		{"some-multimodal-model", true},
		{"qwen-omni", true},
		{"deepseek-v4", false},
		{"kimi-k2", false},
		{"my-org/vl", true},
		{"prefix/vl-suffix", true},
		{"", false},
		{"claude-sonnet-4", false},
	}
	for _, tt := range tests {
		got := SupportsVisionByModelName(tt.model)
		if got != tt.want {
			t.Errorf("SupportsVisionByModelName(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}
