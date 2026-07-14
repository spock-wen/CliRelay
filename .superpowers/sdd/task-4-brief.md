### Task 4: 识图回填编排 Recognizer

新建 `recognizer.go`：解析配置 → 构造 analyzer → 遍历当前轮图片 → 识图 → 用摘要替换图片 part。这是 executor 接入的核心，独立可测。

**Files:**
- Create: `internal/vision/recognizer.go`
- Create: `internal/vision/recognizer_test.go`

**Interfaces:**
- Consumes:
  - `config.OpenAICompatibility`（结构，含 Name/BaseURL/APIKeyEntries）
  - `vision.NewOpenAICompatAnalyzer(baseURL, apiKey, model string) *OpenCodeGoAnalyzer`（Task 2）
  - `vision.WalkPayload(payload []byte) *WalkResult`（walk.go:33）
  - `vision.CurrentTurnHasImages(payload []byte) bool`（processor.go:389）
  - `vision.ReplaceImagePartEx(payload []byte, ip ImagePart, placeholderText string, arrayType string) ([]byte, error)`（walk.go:223）
  - `vision.ImagePart`（walk.go:9，字段 Data/MIMEType/RemoteURL/ArrayName/IsCurrent）
  - `analyzer.Analyze(ctx, AnalyzeRequest) (AnalyzeResponse, error)`（analyzer.go:68）
  - `ImageSummary`（types.go，字段 Summary/OCRHints/LayoutHints/DetailHints）
  - `RenderSummary(s ImageSummary) string`（mutate.go:95）
- Produces:
  - `vision.RecognizeImagesResult{Payload []byte; Applied bool; FallbackModel string}` — 供 Task 5 使用
  - `vision.ResolveRecognitionTarget(cfg interface{ ... }, spec string) (*RecognitionTarget, bool)` — 解析配置

- [ ] **Step 1: 写失败测试（配置解析）**

创建 `internal/vision/recognizer_test.go`：

```go
package vision

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestResolveRecognitionTarget(t *testing.T) {
	cfg := &config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "openai-official",
				BaseURL: "https://api.openai.com/v1",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "sk-aaa"},
				},
			},
		},
	}

	t.Run("valid spec", func(t *testing.T) {
		got, ok := ResolveRecognitionTarget(cfg, "openai-official/gpt-4o")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if got.BaseURL != "https://api.openai.com/v1" {
			t.Errorf("BaseURL = %q", got.BaseURL)
		}
		if got.APIKey != "sk-aaa" {
			t.Errorf("APIKey = %q", got.APIKey)
		}
		if got.Model != "gpt-4o" {
			t.Errorf("Model = %q", got.Model)
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, ok := ResolveRecognitionTarget(cfg, "nope/gpt-4o")
		if ok {
			t.Fatal("expected ok=false for unknown provider")
		}
	})

	t.Run("no slash", func(t *testing.T) {
		_, ok := ResolveRecognitionTarget(cfg, "gpt-4o")
		if ok {
			t.Fatal("expected ok=false for spec without slash")
		}
	})

	t.Run("empty spec", func(t *testing.T) {
		_, ok := ResolveRecognitionTarget(cfg, "")
		if ok {
			t.Fatal("expected ok=false for empty spec")
		}
	})

	t.Run("nil cfg", func(t *testing.T) {
		_, ok := ResolveRecognitionTarget(nil, "openai-official/gpt-4o")
		if ok {
			t.Fatal("expected ok=false for nil cfg")
		}
	})

	t.Run("no usable api key", func(t *testing.T) {
		cfg2 := &config.Config{
			OpenAICompatibility: []config.OpenAICompatibility{
				{Name: "x", BaseURL: "https://x", APIKeyEntries: nil},
			},
		}
		_, ok := ResolveRecognitionTarget(cfg2, "x/m")
		if ok {
			t.Fatal("expected ok=false when no api key")
		}
	})
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run TestResolveRecognitionTarget -v`
Expected: FAIL，`undefined: ResolveRecognitionTarget`

- [ ] **Step 3: 写配置解析实现**

创建 `internal/vision/recognizer.go`：

```go
package vision

import (
	"context"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

// RecognitionTarget 是解析配置得到的识图模型调用参数。
type RecognitionTarget struct {
	BaseURL string
	APIKey  string
	Model   string
}

// ResolveRecognitionTarget 把 "provider名/模型名" 解析为可调用的识图目标。
// provider 名对应 config.OpenAICompatibility[i].Name（大小写不敏感）。
// APIKey 取该项第一个 APIKeyEntries（非空即可）。
// 解析失败返回 ok=false。
func ResolveRecognitionTarget(cfg *config.Config, spec string) (*RecognitionTarget, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" || cfg == nil {
		return nil, false
	}
	idx := strings.IndexByte(spec, '/')
	if idx <= 0 || idx >= len(spec)-1 {
		return nil, false
	}
	providerName := strings.TrimSpace(spec[:idx])
	model := strings.TrimSpace(spec[idx+1:])
	if providerName == "" || model == "" {
		return nil, false
	}
	for i := range cfg.OpenAICompatibility {
		compat := &cfg.OpenAICompatibility[i]
		if !strings.EqualFold(strings.TrimSpace(compat.Name), providerName) {
			continue
		}
		apiKey := ""
		for _, entry := range compat.APIKeyEntries {
			if strings.TrimSpace(entry.APIKey) != "" {
				apiKey = strings.TrimSpace(entry.APIKey)
				break
			}
		}
		if apiKey == "" {
			return nil, false
		}
		return &RecognitionTarget{
			BaseURL: strings.TrimSpace(compat.BaseURL),
			APIKey:  apiKey,
			Model:   model,
		}, true
	}
	return nil, false
}

// RecognizeImagesResult 是识图回填的结果。
type RecognizeImagesResult struct {
	Payload        []byte
	Applied        bool
	FallbackModel  string // 用于 usage 记录的识图模型名
}

// RecognizeCurrentTurnImages 对当前轮的图片做识图回填。
//   - cfg/spec 解析识图模型；解析失败则跳过（Applied=false，payload 原样返回）。
//   - 当前轮无图片或当前模型已支持原生视觉则跳过。
//   - analyzer 为 nil 时跳过（由调用方决定是否构造）。
// 失败的图片降级为 "[图片识别失败]"，不阻断主请求。
func RecognizeCurrentTurnImages(ctx context.Context, analyzer ImageAnalyzer, payload []byte, currentModel string) RecognizeImagesResult {
	result := RecognizeImagesResult{Payload: payload}
	if analyzer == nil {
		return result
	}
	if !CurrentTurnHasImages(payload) {
		return result
	}
	if SupportsVisionByModelName(currentModel) {
		return result
	}

	walk := WalkPayload(payload)
	for _, ip := range walk.Parts {
		if !ip.IsCurrent {
			continue
		}
		placeholder := recognizeOneImage(ctx, analyzer, ip)
		newPayload, err := ReplaceImagePartEx(payload, ip, placeholder, ip.ArrayName)
		if err != nil {
			log.Errorf("vision: replace image part failed: %v", err)
			continue
		}
		payload = newPayload
	}

	result.Payload = payload
	result.Applied = true
	result.FallbackModel = analyzer.Name()
	return result
}

func recognizeOneImage(ctx context.Context, analyzer ImageAnalyzer, ip ImagePart) string {
	req := AnalyzeRequest{
		ImageData:  ip.Data,
		ImageURL:   ip.RemoteURL,
		MIMEType:   ip.MIMEType,
		SourceKind: ImageSourceKindInline,
		TurnIndex:  0,
	}
	resp, err := analyzer.Analyze(ctx, req)
	if err != nil {
		log.Errorf("vision: analyze image failed: %v", err)
		return "[图片识别失败]"
	}
	text := RenderSummary(resp.Summary)
	if strings.TrimSpace(text) == "" {
		return "[图片识别失败]"
	}
	return "[图片内容] " + text
}
```

注意：`recognizeOneImage` 处理 RemoteURL 的情况——若 `ip.Data == ""` 但 `ip.RemoteURL != ""`，`OpenCodeGoAnalyzer.buildRequestBody` 当前只用 `imageData` 拼 data URL（analyzer.go:155），远程 URL 场景会得到空 data URL。本计划范围内不专门处理远程 URL（自用场景图片均为 base64 内联），识图失败降级为 `[图片识别失败]`。若后续需要可扩展 analyzer 支持 `image_url` 远程地址。

`ImageSourceKindInline` 是 types.go 已有的常量，若名字不符需在实现时核对 types.go 中的 `ImageSourceKind` 枚举值并使用正确常量名。

- [ ] **Step 4: 运行配置解析测试确认通过**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run TestResolveRecognitionTarget -v`
Expected: PASS

- [ ] **Step 5: 写回填流程测试（mock analyzer）**

在 `internal/vision/recognizer_test.go` 追加：

```go
// mockAnalyzer 用于测试识图回填流程
type mockAnalyzer struct {
	name string
	sum  ImageSummary
	err  error
	calls int
}

func (m *mockAnalyzer) Name() string { return m.name }
func (m *mockAnalyzer) Analyze(ctx context.Context, req AnalyzeRequest) (AnalyzeResponse, error) {
	m.calls++
	if m.err != nil {
		return AnalyzeResponse{}, m.err
	}
	return AnalyzeResponse{Summary: m.sum}, nil
}

func TestRecognizeCurrentTurnImages(t *testing.T) {
	// 一张 base64 图片的 Chat Completions payload
	payload := []byte(`{
		"model": "deepseek-v4",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "看图"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
			]}
		]
	}`)

	t.Run("replaces image with summary", func(t *testing.T) {
		a := &mockAnalyzer{name: "gpt-4o", sum: ImageSummary{Summary: "a red box", OCRHints: []string{"hello"}}}
		res := RecognizeCurrentTurnImages(context.Background(), a, payload, "deepseek-v4")
		if !res.Applied {
			t.Fatal("expected Applied=true")
		}
		s := string(res.Payload)
		if !strings.Contains(s, "a red box") {
			t.Errorf("payload missing summary, got: %s", s)
		}
		if strings.Contains(s, "image_url") {
			t.Errorf("payload still contains image_url: %s", s)
		}
		if a.calls != 1 {
			t.Errorf("analyzer calls = %d, want 1", a.calls)
		}
	})

	t.Run("skips when current model is vision", func(t *testing.T) {
		a := &mockAnalyzer{name: "gpt-4o"}
		res := RecognizeCurrentTurnImages(context.Background(), a, payload, "qwen-vl-max")
		if res.Applied {
			t.Fatal("expected Applied=false for vision model")
		}
		if a.calls != 0 {
			t.Errorf("analyzer should not be called, calls=%d", a.calls)
		}
	})

	t.Run("skips when no image", func(t *testing.T) {
		noImg := []byte(`{"model":"deepseek-v4","messages":[{"role":"user","content":"hi"}]}`)
		a := &mockAnalyzer{name: "gpt-4o"}
		res := RecognizeCurrentTurnImages(context.Background(), a, noImg, "deepseek-v4")
		if res.Applied {
			t.Fatal("expected Applied=false when no image")
		}
	})

	t.Run("nil analyzer skips", func(t *testing.T) {
		res := RecognizeCurrentTurnImages(context.Background(), nil, payload, "deepseek-v4")
		if res.Applied {
			t.Fatal("expected Applied=false for nil analyzer")
		}
	})

	t.Run("analyze failure degrades to placeholder", func(t *testing.T) {
		a := &mockAnalyzer{name: "gpt-4o", err: fmt.Errorf("boom")}
		res := RecognizeCurrentTurnImages(context.Background(), a, payload, "deepseek-v4")
		if !res.Applied {
			t.Fatal("expected Applied=true even on analyze error")
		}
		if !strings.Contains(string(res.Payload), "图片识别失败") {
			t.Errorf("expected failure placeholder, got: %s", string(res.Payload))
		}
	})
}
```

记得在 `recognizer_test.go` 顶部补 import：`"context"`、`"fmt"`、`"strings"`。

- [ ] **Step 6: 运行测试确认通过**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run "TestRecognizeCurrentTurnImages|TestResolveRecognitionTarget" -v`
Expected: PASS。若有 `ImageSourceKindInline` 常量名不符的编译错误，核对 `internal/vision/types.go` 改用正确常量。

- [ ] **Step 7: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add internal/vision/recognizer.go internal/vision/recognizer_test.go
git commit -m "feat: 新增 vision 识图回填编排 RecognizeCurrentTurnImages"
```

---

