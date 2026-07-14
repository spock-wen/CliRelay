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

