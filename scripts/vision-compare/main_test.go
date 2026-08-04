package main

import "testing"

func TestExtractOCR(t *testing.T) {
	// Has an OCR-marked section with prose before it: only OCR section returned.
	text := "这张图片显示一个错误弹窗。\n\nOCR:\nFile not found\nError 404\n\nLAYOUT: center dialog"
	got, fallback := extractOCR(text)
	if fallback {
		t.Errorf("expected fallback=false, got true")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 merged OCR fragment, got %d: %q", len(got), got)
	}
	if got[0] != "File not found Error 404" {
		t.Errorf("unexpected OCR content: %q", got)
	}

	// No OCR header: whole-text fallback.
	text2 := "这是没有结构的自由文本描述。"
	got2, fallback2 := extractOCR(text2)
	if !fallback2 {
		t.Errorf("expected fallback=true, got false")
	}
	if len(got2) != 1 || got2[0] != text2 {
		t.Errorf("expected whole-text fallback, got %q", got2)
	}

	// Chinese OCR header + continuation lines.
	text3 := "文字：\n1. 你好世界\n2. 再见"
	got3, fallback3 := extractOCR(text3)
	if fallback3 {
		t.Errorf("expected fallback=false for Chinese header, got true")
	}
	if len(got3) != 1 {
		t.Fatalf("expected 1 merged fragment, got %d: %q", len(got3), got3)
	}
	if got3[0] != "1. 你好世界 2. 再见" {
		t.Errorf("unexpected Chinese OCR content: %q", got3)
	}
}

func TestOCRSetF1(t *testing.T) {
	cases := []struct {
		name   string
		server []string
		mcp    []string
		want   float64
	}{
		{"both empty", []string{}, []string{}, 1.0},
		{"one empty", []string{"a"}, []string{}, 0.0},
		{"exact", []string{"hello world"}, []string{"hello world"}, 1.0},
		{"case/punct normalization", []string{"Hello, World!"}, []string{"hello world"}, 1.0},
		{"partial", []string{"hello world"}, []string{"hello there"}, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ocrF1(c.server, c.mcp); got != c.want {
				t.Errorf("ocrF1(%q,%q)=%v want %v", c.server, c.mcp, got, c.want)
			}
		})
	}
}
