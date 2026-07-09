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

