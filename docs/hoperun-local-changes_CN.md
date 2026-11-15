# 相对上游的本地修改

上游基线是 kittors/CliRelay v0.6.1，提交 `8d8ba075`（2026-11-10，PR #1275）。`internal/v0.6.1` 在这之上有已提交的 TokenHub（`a195e908`）。该工作区里还有未提交的 `wire-api`、Responses 透传用量、Chat 流用量，以及 GLM 的 `stop` 数组。

这些代码补丁已经重放到 `internal/v0.6.3`（上游 v0.6.3 是 `6c358782`），并推到 `origin/internal/v0.6.3`：

- TokenHub：`1d3d5630`（`a195e908` 的拣选）。
- `wire-api`、Responses 透传用量、Chat 流用量、`Dockerfile.backend`，以及 Claude 消息角色 `system` → `developer`：`cfd4d5ca`。
- GLM 的 `normalizeGLMStopToArray` 没有带上。v0.6.3 的 `309e0041` 已在翻译器里把 Claude `stop_sequences` 写成数组。

不要把 `internal/main` 整支合并进来。下面各节是重放时要对照的行为，不是部署记录。

## TokenHub 数据源

`internal/v0.6.1` 的提交是 `a195e908`，`internal/v0.6.3` 的提交是 `1d3d5630`。上游没有这两条只读接口：

- `GET /v0/management/data-source/members`
- `GET /v0/management/data-source/usage-events`

行为：

- 只报默认租户 `00000000-0000-0000-0000-000000000001`。
- 成员和用量里的邮箱用 `end_users.username`。用户名已经是 `@hoperun.com` 邮箱，没有静态邮箱表，也不再合成只有用量、没有账号的成员。
- 按显示名排除已删除的 10 人：丛悦、吕世祺、张霖、方浩、皇甫秉顺、虞瑞娴、贺旺隆、陈瑞、饶明友、高明。
- 权限沿用 `request_logs.read`。观察者角色可以读。衡山租户管理员不能调。

文件：`internal/api/handlers/management/data_source.go`、`internal/api/routes/management_data_source_routes.go`、`internal/usage/data_source_members.go`、`internal/usage/data_source_usage.go`，以及对应测试和路由权限表。

## OpenAI 兼容渠道的 wire-api

v0.6.1 工作区尚未提交。v0.6.3 在 `cfd4d5ca`。

面板没有这个字段。用 `PATCH /v0/management/openai-compatibility`，body 只带 `{name, value:{wire-api}}`。整份保存会把已有的 `wire-api` 清掉。只保存值 `responses`，其它值（含 `chat`）都当成空，渠道仍走 Chat Completions。

配置字段是 `wire-api`，凭证属性是 `wire_api`。合成进 openai-compatibility 的两条分支，不进 opencode-go。

运行时在 `openAICompatUpstream`：

- 进来的请求已经是 Responses，并且该渠道 `wire-api` 为 `responses`：原样转发到 `{base-url}/responses`，只改模型名。不走 Chat 翻译，因此 Codex 的 `namespace` / `custom` 工具还在。
- 其它请求，包括 Claude 的 `/v1/messages`：仍翻译成 `{base-url}/chat/completions`。
- `codex-tool-bridge` 保持关闭。不要靠它补工具。

文件：

- `internal/config/provider_keys.go`
- `internal/config/normalize.go`
- `internal/config/wire_api_test.go`
- `internal/management/settings/providers/compatibility.go`
- `internal/management/settings/providers/wire_api_test.go`
- `internal/watcher/synthesizer/helpers.go`
- `internal/watcher/synthesizer/config.go`
- `internal/watcher/synthesizer/wire_api_test.go`
- `internal/watcher/diff/openai_compat.go`
- `internal/watcher/wire_api_test.go`
- `internal/runtime/executor/openai_compat_executor.go`
- `internal/runtime/executor/openai_compat_wire_api_test.go`

## Responses 透传要记下用量

v0.6.1 工作区尚未提交。v0.6.3 在 `cfd4d5ca`，与 `wire-api` 同一段执行器改动。

透传时上游 SSE 原样转给客户端。在 `response.completed` 和 `response.incomplete` 上用已有的 `parseCodexUsage` 读 `response.usage`，并在 `ensurePublished` 之前 `publish`。转发的字节不变。

用量字段：`input_tokens`、`output_tokens`、`total_tokens`，缓存来自 `input_tokens_details.cached_tokens`，推理来自 `output_tokens_details.reasoning_tokens`。全 0 且未失败的明细不会占住发布位。缓存没解析到时库里是 NULL，面板显示「—」；解析到 0 则显示 0。

## Claude 转成 Chat 时要向上游要用量

v0.6.1 工作区尚未提交。v0.6.3 在 `cfd4d5ca`。

Claude 不是 Responses，所以不走透传，会被译成 Chat。上游只有看到 `stream_options.include_usage=true` 才会在 Chat 流末尾补 `prompt_tokens`、`completion_tokens` 和 `prompt_tokens_details.cached_tokens`。

`shouldRequestOpenAICompatStreamUsage`：流式 Chat，并且渠道 `wire-api` 是 `responses` 时加上这个字段。NVIDIA 原来的判断保留。GLM、DeepSeek 不加。Codex 的 Responses 透传不加。

再用现有的 `parseOpenAIStreamUsage` 把这三项记进请求日志。

## Claude 消息里的 system 角色

只在 v0.6.3 的 `cfd4d5ca`。v0.6.1 工作区没有这段。

Claude Code 会在对话中途再发 `messages[].role` 为 `system`。`ConvertClaudeRequestToCodex` 原先把这个 role 原样写进 Codex 的 `input`。ChatGPT 返回 400，错误是 `System messages are not allowed`。顶层 `system` 本来就会变成 `developer`。

在 `internal/translator/codex/claude/codex_claude_request.go` 里，`role` 与 `system` 大小写不敏感匹配时改成 `developer`，再写入 `input`。其它 role 不动。

测试是 `TestConvertClaudeRequestToCodex_SystemRoleBecomesDeveloper`。它检查顶层 system 和中途 system 都变成 `developer`，并且中途那段正文还在。

## GLM 的 stop 数组不要再带

v0.6.1 工作区里还有 `normalizeGLMStopToArray`。不要把它提交进 v0.6.3，下次升级也不要再带。

Claude 的 `stop_sequences` 只有一个元素时，旧翻译器把它写成 JSON 字符串 `stop`。智谱只接受 `string[]`，否则报 `cannot construct java.util.ArrayList from String value`。

这段旧补丁不改翻译器，在 Chat 请求发出去之前改最终 body。函数在 `internal/runtime/executor/iflow_executor.go`：

- 模型名转小写后不是 `glm-` 开头：原样返回。
- `stop` 不存在，或已经是数组：原样返回。
- `stop` 是 JSON 字符串：改成只含这一个字符串的数组。

调用点，改完的字节才是 HTTP body：

- `internal/runtime/executor/openai_compat_executor.go` 的 `Execute` 和 `ExecuteStream`：只在非 Responses 透传分支，`ApplyPayloadConfig` 之后、`ApplyThinking` 之前。透传分支不要调用。
- `internal/runtime/executor/iflow_executor.go` 的 `Execute` 和 `ExecuteStream`：`preserveReasoningContentInMessages` 之后、`ApplyPayloadConfig` 之前。

测试是 `TestNormalizeGLMStopToArray`。覆盖非 GLM 不动、没有 `stop` 不动、已经是数组不动、`glm-` 的字符串改成单元素数组、模型名大写同样改。

v0.6.3 的提交 `309e0041` 已经在翻译器里把 `stop` 一律写成数组，单条 `stop_sequences` 不再变成字符串。客户端自己把 OpenAI Chat 的 `stop` 写成字符串时，上游仍然原样转发。

## 本机镜像构建

`Dockerfile.backend` 在 v0.6.1 工作区仍未提交，那份只编后端，面板不打进镜像，运行时用 `MANAGEMENT_PANEL_DIR` 挂载。

v0.6.3 的 `Dockerfile.backend` 在 `cfd4d5ca`。它会 `COPY panel/`。文件头注释仍写着面板不打进镜像，以 `COPY` 那行为准。运行时若把目录挂到 `MANAGEMENT_PANEL_DIR`，会盖住镜像里的面板。

两份构建都用 `GOPROXY=https://goproxy.cn,direct` 和 `GOSUMDB=sum.golang.google.cn`。入口脚本在镜像里去掉 CRLF。健康检查写死容器内 `127.0.0.1:8317/healthz`。

构建命令形态：

```text
docker buildx build --builder clirelaybuilder --load --network=host -f Dockerfile.backend
```

## 不在代码里、升级时也不要当成补丁

- 门户账号、邮箱用户名、密码、权限来源「标准配置」、衡山不建门户，都在库里，不在 git 里。
- 请求日志清理开关在库里。不要把新版本对着仍开着短保留期的库启动，启动会删明细。
- `context canceled` 仍记成失败。客户端掐断未读完的流时，行上有响应时间，用量可能是 0。决定不改。

## 重放后先跑

v0.6.1 工作区还带着 GLM stop 补丁时，在 `C:\Users\wensp\orca\CliRelay-v0.6.1`：

```text
go test ./internal/config ./internal/management/settings/providers ./internal/watcher ./internal/watcher/synthesizer ./internal/runtime/executor -count=1 -run "WireAPI|OpenAICompatResponsesWireAPI|OpenAICompatExecutorRequestsUsageForNVIDIA|NormalizeGLMStopToArray"
```

v0.6.3 不要带 GLM stop 补丁。在 `C:\Users\wensp\orca\CliRelay-v0.6.3`：

```text
go test ./internal/config ./internal/management/settings/providers ./internal/watcher ./internal/watcher/synthesizer ./internal/runtime/executor ./internal/translator/codex/claude -count=1 -run "WireAPI|OpenAICompatResponsesWireAPI|OpenAICompatExecutorRequestsUsageForNVIDIA|SystemRoleBecomesDeveloper"
```
