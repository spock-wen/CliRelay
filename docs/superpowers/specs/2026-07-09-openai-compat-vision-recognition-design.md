# OpenAI 兼容提供商 Vision 识图回填设计

> 日期：2026-07-09
> 状态：设计待评审
> 范围：CliRelay 后端（`internal/runtime/executor/openai_compat_executor.go`）

## 1. 背景与动机

当前 OpenAI 兼容提供商（`OpenAICompatExecutor`）对图片的处理存在盲区：

- 只对 `provider == "cline"` 做了 vision fallback（模型替换），其他兼容提供商**完全没有任何图片处理**。
- 当用户用纯文本模型（如 deepseek、kimi、qwen 纯文本版）发送图片时，base64 图片原样透传给上游文本模型，导致报错或降级输出。

OpenCodeGo 已有一套完整的三阶段 vision fallback 机制（注册表 + 模型替换 + A3 文本摘要），但 OpenAI 兼容提供商场景下用户通常没有 OpenCodeGo 的 key，无法复用其 analyzer 链路。

## 2. 目标

让 OpenAI 兼容提供商支持：用户用文本模型发图片时，自动拦截 → 用一个全局配置的视觉模型识别图片 → 把识图内容作为文本回填 → 发给原文本模型处理。多轮对话上下文连贯，历史图片天然转为文本，无需额外清理。

## 3. 非目标（YAGNI）

- 不做 Phase 2 模型替换（不把整个请求的模型换成视觉模型）。
- 不做 vision 注册表的历史图片清理（识图回填后历史天然是文本）。
- 不在前端管理界面暴露识图模型配置（自用场景，仅在 `config.yml` 配置）。
- 不做异步预取/流式预取优化（接受首字延迟）。
- 不做 per-provider / per-key 的识图模型配置（全局统一一个）。

## 4. 方案概述

在 `OpenAICompatExecutor` 的 `Execute` 和 `ExecuteStream` 两个入口，请求转发上游前插入一段「识图回填」逻辑：

1. 检测本轮请求是否带图片（复用 `vision.CurrentTurnHasImages`）。
2. 若当前模型本身是视觉模型（启发式判断），跳过回填，直接透传。
3. 否则，用全局配置的视觉模型对每张图片生成结构化摘要。
4. 用摘要文本替换图片 content part（复用 `vision` 包的替换函数）。
5. 把处理后的纯文本请求发给原文本模型。

识图调用同步阻塞，流式场景下先完成所有图片识图再开始流式响应。

## 5. 配置设计

### 5.1 新增配置字段

在 `config.yml` 顶层新增一个全局字段：

```yaml
vision-recognition-model: "provider名/模型名"
```

**语义**：指向一个已配置的 `openai-compatibility` provider，复用其 `base-url`、`api-key`、`proxy-url`/`proxy-id`（保证识图请求和该 provider 正常请求走一致的网络路径），模型名是该 provider 下的一个视觉模型。

**解析规则**：
- 格式为 `"<provider-name>/<model-name>"`，其中 `<provider-name>` 对应 `config.OpenAICompatibility[i].Name`。
- 从匹配到的 `OpenAICompatibility` 项取 `BaseURL`，从 `APIKeyEntries` 取第一个可用（非 disabled）的 `APIKey`，同时取该项的 `ProxyURL`/`ProxyID` 用于识图请求。
- `<model-name>` 直接作为识图请求的 `model` 字段（不再做 alias 解析，要求配置时就写上游真实模型名）。
- 若未配置或配置无法解析（provider 名不存在、无可用 key），识图回填逻辑整体跳过（降级为当前透传行为）。

**示例**：

```yaml
openai-compatibility:
  - name: "openai-official"
    base-url: "https://api.openai.com/v1"
    api-key-entries:
      - api-key: "sk-xxx"
    models:
      - name: "gpt-4o"
        alias: "gpt-4o"

vision-recognition-model: "openai-official/gpt-4o"
```

### 5.2 配置结构变更

`internal/config/config.go` 的 `Config` 结构新增字段：

```go
// VisionRecognitionModel 指定全局视觉识图模型，格式 "provider名/模型名"，
// 用于 OpenAI 兼容提供商的图片识图回填。仅自用，不暴露前端。
VisionRecognitionModel string `yaml:"vision-recognition-model,omitempty" json:"vision-recognition-model,omitempty"`
```

在 `internal/config/normalize.go` 增加 trim 处理（与现有 `VisionFallbackModel` 一致）。

## 6. 核心组件设计

### 6.1 视觉模型判断（启发式）

复用 OpenCodeGo 已有的启发式逻辑 `opencodeGoModelNameImpliesVision`（`opencode_go_executor.go:681-695`）：模型名包含 `vision`、`multimodal`、`omni`，或 token `vl` 等，视为视觉模型。

把该规则抽取为 vision 包内的统一函数，供 OpenCodeGo 和 OpenAI 兼容 executor 共同调用，避免规则分叉：

```go
// internal/vision/vision_model.go
package vision

// SupportsVisionByModelName 用名字启发式判断模型是否支持原生视觉。
func SupportsVisionByModelName(model string) bool
```

`opencodeGoModelNameImpliesVision` 改为调用 `vision.SupportsVisionByModelName`，OpenAI 兼容 executor 也调用同一函数。

### 6.2 识图调用器

复用 `vision.OpenCodeGoAnalyzer`（`analyzer.go:42-108`）。它本质就是个通用 OpenAI Chat Completions 调用器，`NewOpenCodeGoAnalyzer` 已接受 `baseURL/apiKey/model`，prompt 用 `buildInitialPrompt`（`analyzer.go:110-118`，结构化 SUMMARY/OCR/LAYOUT/DETAILS）。

直接复用，加一个语义清晰的构造别名避免 "OpenCodeGo" 命名误导：

```go
// internal/vision/analyzer.go
// NewOpenAICompatAnalyzer 构造一个指向 OpenAI 兼容视觉模型的识图器。
func NewOpenAICompatAnalyzer(baseURL, apiKey, model string) *OpenCodeGoAnalyzer {
    return NewOpenCodeGoAnalyzer(baseURL, apiKey, model)
}
```

`Analyze` 方法返回 `ImageSummary`（含 Summary/OCRHints/LayoutHints/DetailHints），直接用于回填。不复制 prompt，不新建 HTTP 逻辑。

### 6.3 图片替换

复用 `vision.ReplaceCurrentTurnImages`（`processor.go:531-545`）或 `ReplaceImagePartEx`（`walk.go:217-261`），把当前轮的图片 content part 替换为文本 part。

回填文本格式参考 OpenCodeGo A3 的占位符（`mutate.go` 中的 builder），形如：

```
[图片内容]
摘要：<Summary>
OCR：<OCRHints>
布局：<LayoutHints>
细节：<DetailHints>
```

## 7. 执行流程

### 7.1 Execute（非流式）

在 `openai_compat_executor.go:73` 的 `Execute` 方法中，现有 cline fallback 分支之后插入：

```
1. 解析 config.VisionRecognitionModel → (baseURL, apiKey, model)
   若解析失败 → 跳过，走原流程
2. 若 vision.CurrentTurnHasImages(req.Payload) 为 false → 跳过
3. 若 supportsVisionByModelName(req.Model) 为 true → 跳过（当前模型本就能看图）
4. 构造 analyzer（baseURL, apiKey, model）
5. 遍历当前轮图片，逐张调 analyzer.Analyze → ImageSummary
   任一张失败 → 记日志，该张降级为 "[图片识别失败]" 文本，不中断整体请求
6. 用摘要文本替换所有图片 part
7. 继续原 Execute 流程（翻译、转发上游）
```

### 7.2 ExecuteStream（流式）

`ExecuteStream`（`openai_compat_executor.go:217` 附近）同构处理：在开始流式响应前完成步骤 1-6，再进入流式转发。识图同步阻塞，首字延迟 = 所有图片识图耗时之和。

### 7.3 错误处理

- 识图模型未配置 / 配置无法解析 → 静默跳过，降级透传（不报错给用户）。
- 单张图片识图失败 → 该张替换为 `[图片识别失败]`，其余图片继续，原请求正常转发。
- 识图模型整体不可用（如 401/超时）→ 记 error 日志，所有图片降级为 `[图片识别失败]`，原请求正常转发。
- 不因为识图失败而阻断用户的主请求。

## 8. usage 记录

复用现有 vision fallback 的 usage 记录机制：`contextWithVisionFallbackLog`（`execution_context.go:118-131`）+ `usageReporter.setVisionFallbackModel`（`usage_reporter.go:103-109`）。

在 OpenAI 兼容 executor 触发识图回填时，记录 `VisionFallbackModel = 配置的视觉模型名`，便于在请求日志中区分哪些请求做了识图回填。

## 9. 测试策略

- **单元测试**：
  - `supportsVisionByModelName`：覆盖 vision/vl/omni/multimodal 关键字，及纯文本模型名（deepseek/kimi）。
  - 配置解析：`vision-recognition-model` 字段解析为 baseURL/apiKey/model，覆盖未配置、provider 名不存在、格式错误等。
  - 图片替换：给定带图片的 payload 和 ImageSummary，验证替换后 payload 无图片 part、含摘要文本。
  - 多轮场景：第 1 轮带图（触发回填）、第 2 轮不带图（历史已是文本，不触发）。
- **集成测试**：mock 一个 OpenAI 兼容视觉模型 endpoint，验证端到端：文本模型请求带图 → 识图模型被调用 → 摘要回填 → 文本模型收到纯文本请求。
- **错误路径**：识图模型返回错误时，主请求仍正常完成且图片降级为失败占位符。

## 10. 影响范围

| 文件 | 变更 |
|---|---|
| `internal/config/config.go` | 新增 `VisionRecognitionModel` 字段 |
| `internal/config/normalize.go` | 新增 trim 处理 |
| `internal/config/config_test.go` | 新增字段解析测试 |
| `internal/runtime/executor/openai_compat_executor.go` | Execute/ExecuteStream 插入识图回填逻辑 |
| `internal/vision/analyzer.go` | （可选）新增 `NewOpenAICompatAnalyzer` 构造别名 |
| 新增 `openai_compat_vision_test.go` | 单元 + 集成测试 |

不动 OpenCodeGo 现有 vision 逻辑，不影响 cline 现有 fallback 行为。

## 11. 已决策记录

1. **配置位置**：`vision-recognition-model` 放顶层，简单直接。
2. **视觉模型判断**：抽取为 `vision.SupportsVisionByModelName`，OpenCodeGo 与 OpenAI 兼容共用，避免规则分叉。
3. **识图请求网络路径**：复用所指向 provider 的 `ProxyURL`/`ProxyID`，保证和该 provider 正常请求一致。
4. **识图调用器**：复用 `OpenCodeGoAnalyzer`，加 `NewOpenAICompatAnalyzer` 别名，不重复 prompt/HTTP 逻辑。
5. **摘要格式**：结构化 SUMMARY/OCR/LAYOUT/DETAILS，复用 OpenCodeGo analyzer prompt。
