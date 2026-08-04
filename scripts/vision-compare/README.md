# vision-compare — 服务器识别管线 vs 客户端 MCP image_analysis 对比验证 harness

在退役客户端 MCP（vision-mcp-server）之前，用同一批测试图跑两条识别通道，逐图对比质量，
验证服务器识别管线（`internal/vision`）是否达到 MCP 级水平。

## 两条通道

| 通道 | 实现 | 上游 |
|------|------|------|
| 服务器管线 | `vision.NewRecognizer` + `Analyze` | `-base-url` 的 OpenAI 兼容 `/chat/completions`，返回结构化 `ImageSummary`（SUMMARY/OCR/LAYOUT/DETAILS） |
| 客户端 MCP | HTTP 直调 MCP `image_analysis` 的模型端点 | `-mcp-base-url` 的 Anthropic Messages `/v1/messages`，与 vision-mcp-server 的 `image-analysis.ts` 请求形状一致（`x-api-key`、`anthropic-version: 2023-06-01`、`image` + `text` 消息），并用相同预处理（≤2048px 内缩放、JPEG q80） |

MCP 模型端点按 Anthropic Messages API 直接调用，因此不需要本地起 MCP server（stdio）。

## 用法

```bash
# 构建（仅需编译验证）
GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go build ./scripts/vision-compare/

# 运行（需要两条通道的真实 key；不提供 key 时无法真正出结果）
GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go run ./scripts/vision-compare \
  -samples scripts/vision-compare/samples \
  -base-url https://<server-upstream> \
  -keys key1,key2 \
  -model <server-model> \
  -mcp-base-url https://maas-coding-api.cn-huabei-1.xf-yun.com/anthropic \
  -mcp-key <mcp-key> \
  -report out.json
```

## Flags

| Flag | 默认 | 说明 |
|------|------|------|
| `-samples` | `scripts/vision-compare/samples` | 测试图目录 |
| `-base-url` | — | 服务器识别管线上游（OpenAI 兼容 `/chat/completions`） |
| `-keys` | — | 服务器识别 key，逗号分隔 |
| `-model` | — | 服务器识别模型 |
| `-mcp-base-url` | — | MCP `image_analysis` 模型端点 base（Anthropic `/v1/messages`） |
| `-mcp-key` | — | MCP API key |
| `-mcp-model` | `xopkimik26` | MCP 模型 id |
| `-report` | `vision-compare-report.json` | 输出 JSON 报告路径 |
| `-question` | 见代码常量 | 无 sidecar 时的默认问题（默认会要求列出全部可见文字） |
| `-timeout` | `90s` | 单次调用 HTTP 超时 |
| `-concurrency` | `8` | 服务器识别管线最大并发 |
| `-judge-url` / `-judge-key` / `-judge-model` | 空 | 可选 LLM 评判（OpenAI 兼容 `/chat/completions`） |

## 测试集布局

`samples/` 下放按 spec §8 分类的图片（极限尺寸 / OCR 密集 / 结构化）。每张图可选配
同名的 `<base>.txt` sidecar 作为该图的 `question`（例如 `screenshot.png` →
`screenshot.txt`）；没有 sidecar 时使用 `-question` 默认值。

## 报告字段

每个 JSON entry（`results[]`）：

- `image` / `question`
- `server_ocr` / `mcp_ocr` — 两端的 OCR 片段（服务器取 `ImageSummary.OCRHints`；MCP 只取响应文本中 OCR/文字/文本 段内的行）
- `mcp_ocr_fallback` — `true` 表示 MCP 响应没有 OCR 标记段，`mcp_ocr` 用了整段文本兜底（此时 `ocr_f1` 是上界启发值）
- `ocr_f1` — 词集合 F1（归一化：去空白/小写/去标点）
- `server_summary` / `mcp_summary` — 两份摘要原文，供人工/LLM 抽检
- `server_ok` / `mcp_ok` — 该通道是否成功返回分析（含极限尺寸处理）
- `server_error` / `mcp_error` — 失败原因
- `judge_winner` — `server` / `mcp` / `tie` / `pending`（见下）
- `size_bytes` — 原图字节数

`aggregate`：

- `avg_ocr_f1`、`both_ok_pass_rate`（两条通道都成功的比例，对应极限尺寸 100% 通过线）
- `mcp_ocr_fallback_count` — 走整段文本兜底的图数
- `llm_judge_pass_rate`（server 获胜 / 被评判图数，对应 LLM-judge ≥50% 通过线）
- 各 winner 计数

## OCR-F1 的解读（重要）

- `mcp_ocr_fallback=false` 时，`ocr_f1` 才是两端 OCR 段的直接可比指标（MCP 只取
  OCR/文字/文本 段内行，不含摘要散文）。
- `mcp_ocr_fallback=true` 时（MCP 响应没有 OCR 标记段），`mcp_ocr` 用整段文本兜底，
  摘要散文 token 会拉低 MCP 侧 precision，`ocr_f1` 只是**上界启发值**：即便服务器 OCR
  已完全包含在 MCP 响应里，F1 也大概率低于 0.9。此时**不要用 `ocr_f1` 判定 §8 OCR 准确率**，
  以人工/LLM 评判（`judge_winner` + 落盘的两份摘要）作为门槛。

## LLM 评判

默认**桩化**：`judge_winner=pending`，两份摘要都会落盘到报告，供人工或后续 LLM 抽检。
如需自动评判，传 `-judge-url`、`-judge-key`、`-judge-model`，harness 会把两份摘要发给
评判模型，让其回答 A/B/TIE 并记 `server`/`mcp`/`tie`。

## 通过线（spec §8）

- OCR 准确率：优先以人工/LLM 评判为准；当 `mcp_ocr_fallback` 全为 `false` 时可直接看
  `ocr_f1 ≥ 0.9`，否则 `ocr_f1` 仅作上界参考
- LLM-judge server 胜率 ≥ 50%
- 极限尺寸两张通道 100% 成功

## 依赖

仅 Go 标准库 + 模块内 `internal/vision`（无新增 go 依赖）。
