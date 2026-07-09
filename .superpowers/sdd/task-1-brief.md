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

