# 服务器端图像外部化设计（Server-side Image Externalization）

> **日期**: 2026-08-04
> **分支**: `worktree-feat+vision-externalization`（从 origin/main 建出，后续 PR 目标 `dev`）
> **目标**: 在 CliRelay 服务器端完整接管图像识别，图片不进对话模型上下文，彻底解决「文本模型收图 400」与「视觉模型被大 base64 撑爆」两个问题，并支持 100 并发

---

## 1. 背景与问题

### 1.1 现状两个缺陷

**缺陷一：文本模型收图直接 400**
Claude Code 粘贴图片 → 请求含 `image` 内容块 → `ClaudeExecutor` 原样转发（`claude_executor.go:141`，无任何图片处理）→ 上游纯文本模型（如 `deepseek-v4-flash`）的 API 拒绝 `image_url` 变体 → 400 `unknown variant 'image_url', expected 'text'`。失败发生在模型请求层，**早于任何工具调用**（MCP 来不及参与）。

**缺陷二：视觉模型被大 base64 撑爆**
客户端只传 base64（服务器上没有文件），图片字节可能在几 MB 以上。服务器端识别器 `OpenCodeGoAnalyzer.buildRequestBody`（`analyzer.go:155-173`）把 base64 **原样**塞进 `image_url` 发给视觉模型 → 超出 kimi-k2.6 的上下文窗口（注册表 `ContextLength: 131072`，`model_definitions_static_data.go:1228`）→ 直接爆掉。

### 1.2 历史背景：为什么现在是 MCP

现有视觉识别做成客户端 MCP（`vision-mcp-server`）的历史原因：
- 服务器（Go）**没有图像处理管线**（go.mod 零图像依赖）
- MCP 在客户端本地用 `sharp` 缩放（`image-processor.ts:63-68`：标准 ≤2048px / OCR ≤4096px，JPEG q80，>10MB 拒绝），保证发给 kimi 的图很小
- MCP 能读本地文件路径；走独立视觉模型（讯飞 MaaS `xopkimik26` = kimi-k2.6）

**但 MCP 堵不住缺陷一和缺陷二**：图片在 CliRelay 请求流里时不是一个文件，MCP 看不见。**只有服务器能看到这些字节**。

## 2. 目标与成功标准

**成功标准**：
1. 任何模型（文本或视觉）收到图片都不再 400 / 不爆上下文
2. 图片字节**不进对话模型上下文**——只进服务器识别器，上下文里只有结构化摘要文本
3. 服务器端识别质量达到客户端 MCP 程度（对比验证门通过后才弃用 MCP）
4. 支持 100 并发识别（现 100+ 用户在用，讯飞199 渠道 10 个 kimi key）
5. 不同用户/不同会话的图片**严格隔离**，绝不串号

## 3. 架构总览：图像外部化

```
客户端粘贴图片 → POST /v1/messages（含 image 块）
  → Clirelay 请求分发（解析会话 key）
    → ClaudeExecutor.Execute / ExecuteStream（转发前拦截）
       ├─ walk 请求找图片块
       ├─ 每张【新】图（按 hash 查注册表）：
       │    PreprocessImage（缩放 ≤2048 / JPEG q80）
       │    并发槽获取（cap 100，analyze_timeout_ms 预算）
       │    识别器调 kimi-k2.6（讯飞199 渠道）→ 结构化摘要
       │    写回注册表（SessionKey = auth.ID + 会话ID，entry 按 imageHash 索引）
       ├─ 当前轮图片块 → 【替换成摘要文本】
       ├─ 历史图片块 → 占位 + 注册表注记
       └─ 转发给对话模型（上下文只有文本）
```

**核心原则**：图片字节只存在于「识别请求」里，对话模型上下文永远只有摘要文本（约 200-400 token）。文本模型、视觉模型一视同仁。

## 4. 组件设计

### 4.1 PreprocessImage（新增 `internal/vision/preprocess.go`）

Go 复刻 MCP 的 `image-processor.ts`：
- 解码 base64 → 格式识别（PNG/JPEG/WebP/BMP/GIF，magic bytes）
- 尺寸守卫：> `max_size_mb` 拒绝或按配置降级
- 缩放：长边 ≤ `max_dimension`（standard 2048）/ ≤ `ocr_max_dimension`（4096），`fit: inside`，不放大
- 重编码：JPEG `jpeg_quality`（默认 80，对齐 MCP）
- 返回：规范化 base64 + media type + 宽高 + `downsized` 标记

依赖：新增 `golang.org/x/image`（CatmullRom 高质量缩放 + webp 解码）。

### 4.2 识别器（改造 `OpenCodeGoAnalyzer` 或新增）

- **上游改用讯飞199 渠道**（复用 10 个 kimi key + base URL），模型走配置
- 结构化 prompt 与 MCP **逐字对齐**（读 `C:\Users\wensp\.claude\mcp-servers\vision-mcp-server\src\prompts\index.ts`），输出 SUMMARY / OCR / LAYOUT / DETAILS
- 现有 `OpenCodeGoAnalyzer` 写死 `opencodeGoBaseURL = "https://opencode.ai/zen/go/v1"`（`opencode_go_executor.go:24`）——识别器需要可配置上游，不能继续写死

### 4.3 并发控制 + 密钥选择（新增 `internal/vision/limiter.go`）

Go 移植 MCP 已验证的参数：
- `ConcurrencyLimiter`：有界信号量（`max_concurrency=100`）+ 等待队列 + 获取超时（`golang.org/x/sync/semaphore`）
- `KeyPool`：10 个 kimi key，每 key `per_key_concurrency=20`，出错/429 冷却 `key_cooldown_ms=60000`，round-robin
- `singleflight`：同一 (会话, 图hash, 查询) 只识别一次（注册表已有，扩展到首次分析）

### 4.4 上下文替换（改造 `claude_executor.go`）

把 vision 管线接进 `Execute`（`:78`）/ `ExecuteStream`（`:215`）/ `count_tokens`（`:419`）：
- 当前轮图片 → 识别摘要替换
- 历史图片 → 占位 + 注册表注记（复用 `processor.Process` Phase 1）
- 多轮追问「Image #1 里是什么」→ 注册表注入缓存摘要（Phase 2）

### 4.5 注册表（复用 `registry.go`，改 key 组成）

见 §5 会话隔离。

## 5. 会话隔离（关键安全设计）

**结论：当前 `ResolveSessionKey`（`vision/session.go:18`）只按裸 SessionKey 隔离，不加用户命名空间，存在理论串号风险。设计必须修正。**

查证证据：
- `ExecutionSessionMetadataKey` 唯一设置点是 OpenAI websocket，`passthroughSessionID := uuid.NewString()`（`openai_responses_websocket.go:51`）——服务器生成，安全
- `Session-Id` header（`ResolveSessionKey` 优先级 2）——**客户端可控**，理论可串
- Claude Code 路径目前不接 vision，`ResolveSessionKey` 也不读 `x-claude-code-session-id` → 现在既无记忆也不串号

**修正方案（方案②，最小爆炸半径）**：
- 新增 `ResolveIsolatedSessionKey(opts, auth) SessionKey`，返回复合键 `auth.ID + "::" + 客户端会话ID`
- **只给新的 Claude 接入路径用**；现有 `ResolveSessionKey` 与 opencode 路径**一行不动**

隔离保证：
- 同一用户不同会话 → `auth.ID` 相同、会话 ID 不同 → 隔离 ✅
- 不同用户相同会话 ID → `auth.ID` 不同 → 隔离 ✅
- 客户端乱发会话 ID → 拼上 `auth.ID`，串不过去 ✅
- `auth` 为 nil / `auth.ID` 为空 / 无会话 ID → 返回空 key → 单次请求不跨轮（安全兜底，现状行为）

## 6. 配置（config.yml 新增 `vision` 段）

```yaml
vision:
  enabled: true
  channel: xunfei-199        # 复用现有渠道（10 key，均支持 kimi）
  model: kimi-k2.6           # 识别模型，可配
  max_size_mb: 10
  max_dimension: 2048
  ocr_max_dimension: 4096
  jpeg_quality: 80
  max_concurrency: 100
  per_key_concurrency: 20
  key_cooldown_ms: 60000
  analyze_timeout_ms: 5000   # 识别等待预算，先定 5s，后续按实际运行调整
  retries: 2
```

**模型与 `analyze_timeout_ms` 必须做成配置项**——这两个是用户明确要求可调的点。

## 7. 错误处理与降级（保证 100 并发下不挂）

| 场景 | 行为 |
|---|---|
| 并发满 / 超 `analyze_timeout_ms` | 立即落占位，主请求不排队不无限等 |
| 识别失败 / kimi 超时 | 占位（现有 A3 模式），请求正常继续 |
| 注册表写失败 | 非致命，仍转发摘要 |
| 任何情况 | 图片块不漏给对话模型 → 400 根除 |

## 8. 对比验证门（服务器端 vs 客户端 MCP）

**目的**：证明 Go 预处理 + 讯飞199 kimi 达到 sharp + MCP kimi 程度，达标才弃用 MCP。

**测试集**（15-30 张，按三个维度分类）：
- 极限尺寸：>10MB PNG、超长边 >4096px、WebP/BMP/GIF 混合
- OCR 密集：报错、代码截图、终端、UI 标签
- 结构化：图表、复杂界面、流程图

**公平性**：两端同用 kimi-k2.6，唯一变量 = 预处理（sharp vs Go）+ prompt。**先逐字复用 MCP prompt**，收敛到只比预处理差异。

**判定标准**（先定死再跑）：

| 维度 | 指标 | 通过线 |
|---|---|---|
| OCR 准确性 | 两端 OCR 片段归一化后 token 级 F1 | ≥ 0.9 |
| 结构化摘要质量 | 双盲 LLM-judge（kimi 评判）+ 人工抽检 | 「服务器更好或持平」≥ 50% |
| 极限尺寸 | 每张大图两端无报错、模型不爆 | 100% |

**产物**：逐图对比报告（OCR 匹配分 / 尺寸处理 / 摘要评分），脚本化可重跑。

**不达标归因**：OCR 掉分 → 调 Go 预处理（分辨率/JPEG 质量/保 PNG）；摘要掉分 → 调 prompt 对齐。通过才弃用 MCP，否则保留 MCP + 服务器只防爆。

## 9. 测试计划

**单元测试**
- `preprocess_test.go`：格式识别、10MB 守卫、缩放上限、JPEG 重编码、WebP/BMP/GIF
- `limiter_test.go`：并发上限、排队、获取超时、key round-robin、冷却
- `ResolveIsolatedSessionKey`：auth.ID 命名空间、nil auth、无会话 → 空 key
- 注册表隔离：**两个不同 auth 用户、相同会话 ID → 各自独立 store**（防串号回归）
- ClaudeExecutor 接线：图 payload → 摘要替换（文本上下文）、流式、count_tokens

**集成测试**
- mock kimi 上游（httptest）：识别器发预处理后的图、正确解析结构化响应
- 端到端：ClaudeExecutor + mock 上游 → 转发前图片块已替换成摘要文本

**负载测试（100 并发硬指标）**
- 100 并发含图请求 → 全部完成、无挂起；in-flight 卡在配置上限；每 key 调用有界；超预算降级为占位

**回归**
- 方案② 不动现有 opencode 路径 → 现有 `go test ./internal/vision/... ./internal/runtime/executor/...` 全绿

## 10. 风险与缓解

| # | 风险 | 等级 | 缓解 |
|---|---|---|---|
| R1 | 热路径同步识别：每张新图首次出现多等识别耗时 | 中 | 预处理后图小识别快、hash 去重（每图每会话一次）、历史轮走缓存摘要 |
| R2 | 识别负载集中在服务器 + kimi API | 中 | 并发限流 100 + 每 key 20 + 冷却；10 key 容量 200 |
| R3 | kimi 上游故障 → 图片降级 | 中 | 占位兜底，请求不挂 |
| R4 | 会话串号（客户端可控 session ID） | 高 | 复合键 `auth.ID + "::" + 会话ID`，方案② |
| R5 | Go 图像处理质量不达 sharp 程度 | 中 | 对比验证门归因迭代 |
| R6 | 新增依赖 `golang.org/x/image` | 低 | 官方库，供审视 |
| R7 | 摘要有损（对话模型看不了原图） | 低中 | 与 MCP 行为一致；OCR 全文 + 结构化布局补偿 |

## 11. 范围外（后续可选）

- 对话模型「看原图做开放式推理」：当前统一外部化，不做透传；如对比门发现直连视觉更优，再加 `pass_through_when_vision` 配置开关
- 把现有 opencode 路径的 `ResolveSessionKey` 也统一为 auth 命名空间（方案①），风险更低但属正确性加固，可后续做
- 服务器端分析 API 对外暴露（供客户端/MCP 调用），本期不做

## 12. 环境注意

当前环境 Google 端点（proxy.golang.org / sum.golang.org）不可达，所有 go 命令需：
```bash
export GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn
```
（工具链 go1.26.5 已通过该镜像下载缓存）
