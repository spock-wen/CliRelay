package vision

import "testing"

func TestNewOpenAICompatAnalyzer(t *testing.T) {
	a := NewOpenAICompatAnalyzer("https://api.openai.com/v1", "sk-test", "gpt-4o")
	if a == nil {
		t.Fatal("expected non-nil analyzer")
	}
	if a.Name() == "" {
		t.Fatal("expected analyzer to have a name")
	}
}
