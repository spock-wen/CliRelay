# OpenAI 兼容 Vision 识图回填 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 OpenAI 兼容提供商在用户用文本模型发送图片时，自动用全局配置的视觉模型识图，把识图摘要作为文本回填，再发给原文本模型处理。

**Architecture:** 在 `OpenAICompatExecutor` 的 `Execute`/`ExecuteStream` 转发上游前插入「识图回填」逻辑：检测本轮带图片且当前模型非视觉 → 用配置的视觉模型（复用 `vision.OpenCodeGoAnalyzer`）对每张图生成结构化摘要 → 用摘要文本替换图片 part → 继续原流程。配置通过 `config.yml` 顶层 `vision-recognition-model: "provider名/模型名"` 指向一个已配置的 `openai-compatibility` provider，复用其 base-url/api-key/proxy。

**Tech Stack:** Go 1.26+，gjson/sjson（payload 操作），gorilla HTTP，logrus，Go 内置 testing。

## Global Constraints

- 模块路径：`github.com/router-for-me/CLIProxyAPI/v6`
- 配置字段：`vision-recognition-model`（顶层，`yaml:"vision-recognition-model,omitempty"`），值为 `"<provider-name>/<model-name>"`
- 识图摘要格式：结构化 SUMMARY/OCR/LAYOUT/DETAILS（复用 `vision.OpenCodeGoAnalyzer` 的 `buildInitialPrompt`）
- 视觉模型判断：启发式（vision/multimodal/omni/vl token），抽取为 `vision.SupportsVisionByModelName`
- 识图失败不阻断主请求，单张失败降级为 `[图片识别失败]`
- 不做模型替换、不做注册表历史清理、不做异步预取
- 测试用 `go test ./internal/...`
- 提交信息语义化：`feat:`、`test:`、`docs:` 等
- 目标分支：`dev`（PR 目标），但本计划在 feature 分支上实现

---

## File Structure

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/vision/vision_model.go` | 视觉模型名启发式判断 `SupportsVisionByModelName` | 新建 |
| `internal/vision/vision_model_test.go` | 启发式判断单测 | 新建 |
| `internal/vision/analyzer.go` | 加 `NewOpenAICompatAnalyzer` 构造别名 | 修改 |
| `internal/vision/recognizer.go` | 识图回填编排：解析配置→遍历图片→识图→替换 | 新建 |
| `internal/vision/recognizer_test.go` | 回填编排单测（mock HTTP） | 新建 |
| `internal/config/config.go` | `Config` 加 `VisionRecognitionModel` 字段 | 修改 |
| `internal/config/normalize.go` | trim 处理 | 修改 |
| `internal/config/config_test.go` | 字段解析测试 | 修改 |
| `internal/runtime/executor/openai_compat_executor.go` | Execute/ExecuteStream 接入识图回填 | 修改 |
| `internal/runtime/executor/openai_compat_vision_test.go` | 接入集成测试 | 新建 |

---

### Task 1: 视觉模型名启发式判断

把 OpenCodeGo 的 `opencodeGoModelNameImpliesVision` 规则抽取到 vision 包，供 OpenCodeGo 与 OpenAI 兼容共用。

**Files:**
- Create: `internal/vision/vision_model.go`
- Create: `internal/vision/vision_model_test.go`
- Modify: `internal/runtime/executor/opencode_go_executor.go:681-695`（让 `opencodeGoModelNameImpliesVision` 调用新函数）

**Interfaces:**
- Produces: `vision.SupportsVisionByModelName(model string) bool` — 供 Task 5 的 executor 调用

- [ ] **Step 1: 写失败测试**

创建 `internal/vision/vision_model_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run TestSupportsVisionByModelName -v`
Expected: FAIL，编译错误 `undefined: SupportsVisionByModelName`

- [ ] **Step 3: 写实现**

创建 `internal/vision/vision_model.go`：

```go
package vision

import "strings"

// SupportsVisionByModelName 用名字启发式判断模型是否支持原生视觉。
// 规则：模型名（已转小写）包含 vision/multimodal/omni，或按分隔符切出的
// token 之一为 "vl"，视为视觉模型。
func SupportsVisionByModelName(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	if strings.Contains(model, "vision") ||
		strings.Contains(model, "multimodal") ||
		strings.Contains(model, "omni") {
		return true
	}
	for _, token := range strings.FieldsFunc(model, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
	}) {
		if token == "vl" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run TestSupportsVisionByModelName -v`
Expected: PASS

- [ ] **Step 5: 让 OpenCodeGo 复用新函数**

修改 `internal/runtime/executor/opencode_go_executor.go`，把 `opencodeGoModelNameImpliesVision`（681-695 行）改为：

```go
func opencodeGoModelNameImpliesVision(model string) bool {
	return vision.SupportsVisionByModelName(model)
}
```

确认 `opencode_go_executor.go` 已 import `github.com/router-for-me/CLIProxyAPI/v6/internal/vision`（该文件已 import，见 opencode_go_analyzer.go:7，但 opencode_go_executor.go 需自行确认；若未 import 则加）。

- [ ] **Step 6: 全量测试确认未破坏 OpenCodeGo**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/runtime/executor/ -run "opencodeGo" -v -count=1 2>&1 | tail -20`
Expected: PASS（既有 OpenCodeGo vision 测试不受影响）

- [ ] **Step 7: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add internal/vision/vision_model.go internal/vision/vision_model_test.go internal/runtime/executor/opencode_go_executor.go
git commit -m "refactor: 抽取 SupportsVisionByModelName 到 vision 包"
```

---

### Task 2: OpenAI 兼容识图器构造别名

加一个语义清晰的构造函数，指向 OpenAI 兼容视觉模型，复用 `OpenCodeGoAnalyzer` 的 prompt 和 HTTP 逻辑。

**Files:**
- Modify: `internal/vision/analyzer.go`（在 `NewOpenCodeGoAnalyzer` 后加别名）

**Interfaces:**
- Produces: `vision.NewOpenAICompatAnalyzer(baseURL, apiKey, model string) *OpenCodeGoAnalyzer` — 供 Task 4 使用
- 依赖：`OpenCodeGoAnalyzer.Analyze(ctx, AnalyzeRequest) (AnalyzeResponse, error)`（已存在，analyzer.go:68-108）

- [ ] **Step 1: 写失败测试**

在 `internal/vision/analyzer.go` 同包下没有现成测试文件，创建 `internal/vision/analyzer_alias_test.go`：

```go
package vision

import "testing"

func TestNewOpenAICompatAnalyzer(t *testing) {
	a := NewOpenAICompatAnalyzer("https://api.openai.com/v1", "sk-test", "gpt-4o")
	if a == nil {
		t.Fatal("expected non-nil analyzer")
	}
	if a.Name() == "" {
		t.Fatal("expected analyzer to have a name")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run TestNewOpenAICompatAnalyzer -v`
Expected: FAIL，编译错误 `undefined: NewOpenAICompatAnalyzer`

- [ ] **Step 3: 写实现**

在 `internal/vision/analyzer.go` 的 `NewOpenCodeGoAnalyzer` 函数（约 50 行）之后添加：

```go
// NewOpenAICompatAnalyzer 构造一个指向 OpenAI 兼容视觉模型的识图器。
// 复用 OpenCodeGoAnalyzer 的 prompt 与 HTTP 逻辑，仅语义上区分调用目标。
func NewOpenAICompatAnalyzer(baseURL, apiKey, model string) *OpenCodeGoAnalyzer {
	return NewOpenCodeGoAnalyzer(baseURL, apiKey, model)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/vision/ -run TestNewOpenAICompatAnalyzer -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add internal/vision/analyzer.go internal/vision/analyzer_alias_test.go
git commit -m "feat: 新增 NewOpenAICompatAnalyzer 识图器构造别名"
```

---

### Task 3: 配置字段 VisionRecognitionModel

在 `config.yml` 顶层新增 `vision-recognition-model` 字段，带 trim 归一化。

**Files:**
- Modify: `internal/config/config.go`（`Config` 结构加字段）
- Modify: `internal/config/normalize.go`（加 trim）
- Modify: `internal/config/config_test.go`（加解析测试）

**Interfaces:**
- Produces: `config.Config.VisionRecognitionModel string` — 供 Task 4 解析

- [ ] **Step 1: 写失败测试**

在 `internal/config/config_test.go` 末尾追加（若文件已存在测试函数则追加到末尾；import 已有 `yaml`/`gopkg.in/yaml.v3`）：

```go
func TestVisionRecognitionModelConfig(t *testing.T) {
	raw := []byte("vision-recognition-model: openai-official/gpt-4o\n")
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.VisionRecognitionModel != "openai-official/gpt-4o" {
		t.Errorf("VisionRecognitionModel = %q, want %q", cfg.VisionRecognitionModel, "openai-official/gpt-4o")
	}
	// trim 归一化
	rawTrim := []byte("vision-recognition-model: '  openai-official/gpt-4o  '\n")
	var cfg2 Config
	if err := yaml.Unmarshal(rawTrim, &cfg2); err != nil {
		t.Fatalf("unmarshal trim: %v", err)
	}
	Normalize(&cfg2)
	if cfg2.VisionRecognitionModel != "openai-official/gpt-4o" {
		t.Errorf("after Normalize = %q, want %q", cfg2.VisionRecognitionModel, "openai-official/gpt-4o")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/config/ -run TestVisionRecognitionModelConfig -v`
Expected: FAIL，`cfg.VisionRecognitionModel` 字段不存在（编译错误）

- [ ] **Step 3: 加配置字段**

在 `internal/config/config.go` 的 `Config` 结构中，`OAuthModelAlias` 字段（约 168 行）之后添加：

```go
	// VisionRecognitionModel 指定全局视觉识图模型，格式 "<provider-name>/<model-name>"，
	// 指向 openai-compatibility 中的一个 provider，复用其 base-url/api-key/proxy。
	// 用于 OpenAI 兼容提供商的图片识图回填。仅自用，不暴露前端。
	VisionRecognitionModel string `yaml:"vision-recognition-model,omitempty" json:"vision-recognition-model,omitempty"`
```

- [ ] **Step 4: 加归一化**

在 `internal/config/normalize.go` 中找到 `Normalize` 函数（函数签名应为 `func Normalize(cfg *Config)`）。在函数体内（OpenAICompatibility 归一化附近）加一行：

```go
	cfg.VisionRecognitionModel = strings.TrimSpace(cfg.VisionRecognitionModel)
```

若不确定插入位置，放在 `Normalize` 函数体末尾的 `return` 之前即可。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/config/ -run TestVisionRecognitionModelConfig -v`
Expected: PASS

- [ ] **Step 6: 全量 config 测试回归**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/config/ -count=1 2>&1 | tail -10`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add internal/config/config.go internal/config/normalize.go internal/config/config_test.go
git commit -m "feat: 新增 vision-recognition-model 配置字段"
```

---

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

### Task 5: OpenAI 兼容 executor 接入识图回填

在 `Execute` 和 `ExecuteStream` 转发上游前调用 `RecognizeCurrentTurnImages`。

**Files:**
- Modify: `internal/runtime/executor/openai_compat_executor.go:73-83`（Execute）
- Modify: `internal/runtime/executor/openai_compat_executor.go:216-226`（ExecuteStream）

**Interfaces:**
- Consumes:
  - `vision.ResolveRecognitionTarget(cfg *config.Config, spec string) (*RecognitionTarget, bool)`（Task 4）
  - `vision.NewOpenAICompatAnalyzer(baseURL, apiKey, model string) *OpenCodeGoAnalyzer`（Task 2）
  - `vision.RecognizeCurrentTurnImages(ctx, analyzer, payload, currentModel) RecognizeImagesResult`（Task 4）
  - `contextWithVisionFallbackLog(ctx, requestedModel, upstreamModel, fallbackModel) context.Context`（execution_context.go:118）
  - `thinking.ParseSuffix(model).ModelName`（已有）
- Produces：executor 行为变更（无需导出新符号）

- [ ] **Step 1: 写接入测试（mock 视觉模型端点）**

创建 `internal/runtime/executor/openai_compat_vision_test.go`：

```go
package executor

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// 一个模拟的视觉模型端点，返回固定 SUMMARY。
func visionMockServer(t *testing.T, wantModel string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: a red box\nOCR: hello"}}]}`))
	}))
}

func TestResolveRecognitionTargetFromConfig(t *testing.T) {
	cfg := &config.Config{
		VisionRecognitionModel: "openai-official/gpt-4o",
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "openai-official",
				BaseURL: "http://example",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{
					{APIKey: "sk-test"},
				},
			},
		},
	}
	target, ok := resolveRecognitionAnalyzer(cfg)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if target == nil {
		t.Fatal("expected non-nil analyzer")
	}
}
```

注意：`resolveRecognitionAnalyzer` 是本任务新增的 executor 包内辅助函数（见 Step 3）。测试里先验证它能从配置构造出 analyzer。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/runtime/executor/ -run TestResolveRecognitionTargetFromConfig -v`
Expected: FAIL，`undefined: resolveRecognitionAnalyzer`

- [ ] **Step 3: 加 executor 辅助函数**

在 `internal/runtime/executor/openai_compat_executor.go` 文件末尾（`statusErr` 相关代码之前或之后均可，保持文件内函数聚集）添加：

```go
// resolveRecognitionAnalyzer 根据 config.VisionRecognitionModel 解析识图模型，
// 成功则构造 analyzer，失败返回 nil。
func (e *OpenAICompatExecutor) resolveRecognitionAnalyzer() *vision.OpenCodeGoAnalyzer {
	if e.cfg == nil {
		return nil
	}
	target, ok := vision.ResolveRecognitionTarget(e.cfg, e.cfg.VisionRecognitionModel)
	if !ok || target == nil {
		return nil
	}
	if target.BaseURL == "" || target.APIKey == "" || target.Model == "" {
		return nil
	}
	return vision.NewOpenAICompatAnalyzer(target.BaseURL, target.APIKey, target.Model)
}

// recognizeCurrentTurnImages 对当前轮图片做识图回填，返回处理后的 payload 与是否应用。
func (e *OpenAICompatExecutor) recognizeCurrentTurnImages(ctx context.Context, payload []byte, model string) vision.RecognizeImagesResult {
	analyzer := e.resolveRecognitionAnalyzer()
	return vision.RecognizeCurrentTurnImages(ctx, analyzer, payload, model)
}
```

包级辅助函数版本（供测试直接调用，无需 executor 实例）——在测试中用的是实例方法，所以测试改成：

```go
func TestResolveRecognitionTargetFromConfig(t *testing.T) {
	cfg := &config.Config{
		VisionRecognitionModel: "openai-official/gpt-4o",
		OpenAICompatibility: []config.OpenAICompatibility{
			{Name: "openai-official", BaseURL: "http://example",
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "sk-test"}}},
		},
	}
	e := &OpenAICompatExecutor{provider: "openai-official", cfg: cfg}
	a := e.resolveRecognitionAnalyzer()
	if a == nil {
		t.Fatal("expected non-nil analyzer")
	}
}
```

确认 `openai_compat_executor.go` 顶部已 import `github.com/router-for-me/CLIProxyAPI/v6/internal/vision`，若未 import 则加。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/runtime/executor/ -run TestResolveRecognitionTargetFromConfig -v`
Expected: PASS

- [ ] **Step 5: 接入 Execute**

修改 `internal/runtime/executor/openai_compat_executor.go` 的 `Execute` 方法（73-83 行）。现有 cline 分支**之后**、`to := sdktranslator.FromString("openai")`（85 行）**之前**插入识图回填：

把：

```go
	fallback := opencodeGoVisionFallbackResult{Request: req}
	if e.provider == "cline" {
		originalRequestedModel := payloadRequestedModel(opts, req.Model)
		originalUpstreamModel := thinking.ParseSuffix(req.Model).ModelName
		fallback = applyVisionFallback(req, opts, clineVisionFallbackModel(e.cfg, auth))
		if fallback.Applied {
			ctx = contextWithVisionFallbackLog(ctx, originalRequestedModel, originalUpstreamModel, fallback.FallbackModel)
		}
		req = fallback.Request
	}

	to := sdktranslator.FromString("openai")
```

改为：

```go
	fallback := opencodeGoVisionFallbackResult{Request: req}
	if e.provider == "cline" {
		originalRequestedModel := payloadRequestedModel(opts, req.Model)
		originalUpstreamModel := thinking.ParseSuffix(req.Model).ModelName
		fallback = applyVisionFallback(req, opts, clineVisionFallbackModel(e.cfg, auth))
		if fallback.Applied {
			ctx = contextWithVisionFallbackLog(ctx, originalRequestedModel, originalUpstreamModel, fallback.FallbackModel)
		}
		req = fallback.Request
	}

	// OpenAI 兼容识图回填：文本模型发图时，用配置的视觉模型识图并回填文本。
	recog := e.recognizeCurrentTurnImages(ctx, req.Payload, req.Model)
	if recog.Applied {
		ctx = contextWithVisionFallbackLog(ctx, req.Model, thinking.ParseSuffix(req.Model).ModelName, recog.FallbackModel)
		req.Payload = recog.Payload
	}

	to := sdktranslator.FromString("openai")
```

- [ ] **Step 6: 接入 ExecuteStream**

修改 `ExecuteStream`（216-226 行）。同样在 cline 分支之后、`to := sdktranslator.FromString("openai")`（228 行）之前插入相同逻辑：

把：

```go
	fallback := opencodeGoVisionFallbackResult{Request: req}
	if e.provider == "cline" {
		originalRequestedModel := payloadRequestedModel(opts, req.Model)
		originalUpstreamModel := thinking.ParseSuffix(req.Model).ModelName
		fallback = applyVisionFallback(req, opts, clineVisionFallbackModel(e.cfg, auth))
		if fallback.Applied {
			ctx = contextWithVisionFallbackLog(ctx, originalRequestedModel, originalUpstreamModel, fallback.FallbackModel)
		}
		req = fallback.Request
	}

	to := sdktranslator.FromString("openai")
```

改为（ExecuteStream 版）：

```go
	fallback := opencodeGoVisionFallbackResult{Request: req}
	if e.provider == "cline" {
		originalRequestedModel := payloadRequestedModel(opts, req.Model)
		originalUpstreamModel := thinking.ParseSuffix(req.Model).ModelName
		fallback = applyVisionFallback(req, opts, clineVisionFallbackModel(e.cfg, auth))
		if fallback.Applied {
			ctx = contextWithVisionFallbackLog(ctx, originalRequestedModel, originalUpstreamModel, fallback.FallbackModel)
		}
		req = fallback.Request
	}

	// OpenAI 兼容识图回填：文本模型发图时，用配置的视觉模型识图并回填文本。
	recog := e.recognizeCurrentTurnImages(ctx, req.Payload, req.Model)
	if recog.Applied {
		ctx = contextWithVisionFallbackLog(ctx, req.Model, thinking.ParseSuffix(req.Model).ModelName, recog.FallbackModel)
		req.Payload = recog.Payload
	}

	to := sdktranslator.FromString("openai")
```

- [ ] **Step 7: 编译并跑 executor 包测试**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go build ./internal/runtime/executor/ && go test ./internal/runtime/executor/ -run "OpenAICompat|Vision" -v -count=1 2>&1 | tail -30`
Expected: 编译通过，相关测试 PASS

- [ ] **Step 8: 写端到端集成测试**

在 `internal/runtime/executor/openai_compat_vision_test.go` 追加端到端测试，验证「文本模型请求带图 → mock 视觉端点被调 → 摘要回填 → 上游收到纯文本」：

```go
func TestExecuteVisionRecognitionEndToEnd(t *testing.T) {
	// 1. 视觉识图 mock 端点
	visionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: a red box\nOCR: hello"}}]}`))
	}))
	defer visionSrv.Close()

	// 2. 上游文本模型 mock 端点，记录收到的 payload
	var upstreamBody string
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1<<16)
		n, _ := r.Body.Read(buf)
		upstreamBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstreamSrv.Close()

	cfg := &config.Config{
		VisionRecognitionModel: "vision-provider/gpt-4o",
		OpenAICompatibility: []config.OpenAICompatibility{
			{
				Name:    "vision-provider",
				BaseURL: visionSrv.URL,
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "sk-vision"}},
			},
			{
				Name:    "text-provider",
				BaseURL: upstreamSrv.URL,
				APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "sk-text"}},
			},
		},
	}

	e := &OpenAICompatExecutor{provider: "text-provider", cfg: cfg}
	auth := &cliproxyauth.Auth{
		Provider: "text-provider",
		Attributes: map[string]string{
			"base_url": upstreamSrv.URL,
			"api_key":  "sk-text",
		},
	}

	payload := []byte(`{
		"model": "deepseek-v4",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "看图"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
			]}
		]
	}`)
	req := cliproxyexecutor.Request{Model: "deepseek-v4", Payload: payload}
	resp, err := e.Execute(context.Background(), auth, req, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(upstreamBody, "a red box") {
		t.Errorf("upstream should receive summary, got: %s", upstreamBody)
	}
	if strings.Contains(upstreamBody, "image_url") {
		t.Errorf("upstream should not receive image_url, got: %s", upstreamBody)
	}
	if len(resp.Payload) == 0 {
		t.Error("expected non-empty response")
	}
}
```

补 import：`"context"`、`"strings"`、`cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"`、`cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"`。

注意：`Execute` 内部会做翻译（`sdktranslator`）和 thinking 处理，payload 可能被改写。本测试验证核心行为——上游收到的内容含摘要、不含 image_url。若翻译层导致断言不稳定，可降级为只验证 `recog.Applied` 流程（用 `e.recognizeCurrentTurnImages` 直接测），不跑完整 Execute。实现时若完整 Execute 测试因翻译层不稳定而 flaky，改用直接测 `recognizeCurrentTurnImages` 的方式并记录原因。

- [ ] **Step 9: 运行集成测试**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go test ./internal/runtime/executor/ -run TestExecuteVisionRecognitionEndToEnd -v -count=1`
Expected: PASS（若因翻译层 flaky，按 Step 8 注记降级并仍提交直接流程测试）

- [ ] **Step 10: 全量回归**

Run: `cd D:/Dev/Spock/Relay/CliRelay && go build ./... && go test ./internal/vision/... ./internal/config/... ./internal/runtime/executor/... -count=1 2>&1 | tail -20`
Expected: 全部编译通过、测试 PASS

- [ ] **Step 11: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add internal/runtime/executor/openai_compat_executor.go internal/runtime/executor/openai_compat_vision_test.go
git commit -m "feat: OpenAI 兼容 executor 接入 vision 识图回填"
```

---

### Task 6: 配置示例与文档

在 `config.example.yaml` 加示例配置项，方便自用。

**Files:**
- Modify: `config.example.yaml`

- [ ] **Step 1: 加示例**

在 `config.example.yaml` 末尾（或 `openai-compatibility` 段附近）追加：

```yaml
# Vision recognition model for OpenAI-compatible providers.
# When a user sends an image to a text-only model via an openai-compatibility
# provider, the image is sent to this vision model for recognition, and the
# resulting text summary is fed back to the original text model.
# Format: "<provider-name>/<model-name>" where provider-name matches an
# openai-compatibility entry below.
# vision-recognition-model: "openai-official/gpt-4o"
```

- [ ] **Step 2: 提交**

```bash
cd D:/Dev/Spock/Relay/CliRelay
git add config.example.yaml
git commit -m "docs: 添加 vision-recognition-model 配置示例"
```

---

## Self-Review 结果

**1. Spec 覆盖：**
- §5 配置 → Task 3
- §6.1 视觉模型判断 → Task 1
- §6.2 识图调用器 → Task 2
- §6.3 图片替换 → Task 4（复用 `ReplaceImagePartEx`）
- §7.1 Execute 接入 → Task 5 Step 5
- §7.2 ExecuteStream 接入 → Task 5 Step 6
- §7.3 错误处理 → Task 4 `recognizeOneImage` 降级、Task 4 测试覆盖
- §8 usage 记录 → Task 5 接入 `contextWithVisionFallbackLog`
- §9 测试 → 各 Task 均含 TDD 步骤 + Task 5 端到端
- §10 影响范围 → 文件结构与各 Task 一致

**2. 类型一致性：** `SupportsVisionByModelName`、`NewOpenAICompatAnalyzer`、`ResolveRecognitionTarget`、`RecognitionTarget`、`RecognizeCurrentTurnImages`、`RecognizeImagesResult` 在各 Task 中签名一致。

**3. 已知风险点（实现时关注）：**
- `ImageSourceKindInline` 常量名需核对 types.go
- 远程 URL 图片本版本不专门处理，降级为失败占位符
- Task 5 端到端测试若被翻译层影响，降级为直接流程测试
