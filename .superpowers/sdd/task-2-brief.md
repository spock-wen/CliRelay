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

