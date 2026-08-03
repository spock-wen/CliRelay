# TokenHub 平台数据采集接口设计

> **日期**: 2026-08-03
> **分支**: feat/reset-cooldown-api（新功能建议从 origin/dev 拉独立分支）
> **目标**: 让 TokenHub 统计平台能主动拉取 CliRelay 的成员与用量数据

## 1. 背景

TokenHub（AI IDE 综合统计平台）提供了一份《TokenHub 平台数据采集接口手册》，要求各 AI Coding 平台**按手册对外提供 HTTP REST 接口**，由 TokenHub 定时主动拉取。手册定义了**两个核心接口**：

| 接口 | 必需 | 说明 |
|-----|------|------|
| 成员列表接口 | 是 | 返回平台所有成员信息，用于用户身份匹配 / 席位快照 |
| 用量数据接口 | 是 | 返回指定时间范围内各用户的 AI 调用明细事件 |

TokenHub 调用频率：默认每 2 小时一次增量同步；每日凌晨 2 点全量校正昨日数据；数据粒度按「用户 + 日期 + 模型 + 来源」日聚合（聚合在 TokenHub 本地完成，提供方只需返回明细事件）。

## 2. 目标

在 CliRelay 中新增两个对外接口，按手册的请求/响应格式返回 CliRelay 的成员与用量数据，使 TokenHub 能对接采集。

**成功标准**：TokenHub 用「运管」账号配置数据源后，可正常拉取成员列表和指定时间窗口的用量事件，字段值符合本 spec 的映射规则。

## 3. 现状调研结论（生产库验证）

生产环境为 PostgreSQL（容器 `clirelay-postgres`，库 `clirelay`），CLIProxyAPI 正在运行。已逐字段验证：

### 3.1 `request_logs` 表（用量数据源）✅

存在字段：`timestamp`、`api_key`、`api_key_name`、`model`、`upstream_model`、`source`、`channel_name`、`auth_index`、`failed`、`streaming`、`latency_ms`、`first_token_ms`、`input_tokens`、`output_tokens`、`reasoning_tokens`、`cached_tokens`、`total_tokens`、`cost`、`tenant_id`。

关键验证：
- `api_key_name` **全部为中文姓名**，217 万行**无空值**（如 方珍、文国荣、闫鹏…）
- 每个中文姓名唯一对应一个 API key，**无重名** → 拼音 email 不会撞车
- `source` 列内容不可靠：存的是**上游 API key**（`sk-7e6b...`）或加密标识（`hash:base64`），不是 provider/客户端类型 → 必须用固定值
- `cached_tokens` 为合并值，无拆分 `cache_read_tokens`/`cache_write_tokens` 列

### 3.2 `end_users` 表（成员数据源）✅

存在字段：`id`（uuid）、`username`（拼音风格，如 `wenguo56c6`）、`display_name`（**中文姓名**）、`status`（全为 `active`）、`password_hash` 等。共 111 名成员，无重名。

### 3.3 不存在的字段（全库信息架构扫描）❌

- `operation`：无（只有无关的 `reset_credit_count`/`reset_credit_expirations` 配额重置字段）
- `credits`：无
- `cost_currency`：无
- `cache_read_tokens` / `cache_write_tokens`：无（只有合并 `cached_tokens`）
- `end_users` 无 `role` 列

### 3.4 成员关联

`api_keys.end_user_id` 在生产为**空**（key↔用户未绑定）。因此用量→成员靠 `request_logs.api_key_name` 匹配 `end_users.display_name`；Email 生成**直接用 `api_key_name`/`display_name` 中文姓名转拼音**，不依赖关联表。

边角差异（不影响规则）：
- 3 个姓名在日志但不在 `end_users`（肖亮 / 蔡泽洲 / 齐云涛）
- 18 名 `end_users` 暂无日志（无用量）

## 4. 接口定义

### 4.1 端点与鉴权

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v0/management/data-source/members` | GET | 成员列表 |
| `/v0/management/data-source/usage-events` | GET | 用量事件 |

> 路径说明：手册 Q5 允许路径自定义，TokenHub 配置数据源时记录实际 API 路径。挂在 `/v0/management/data-source/` 下以复用管理端身份鉴权与权限映射。

**鉴权流程**（TokenHub 侧）：
1. `POST /v0/auth/login`，body `{"username": "hoperun_yunguan", "password": "<运管密码>"}` → 返回 `cps_*` session token
2. 后续请求带 `Authorization: Bearer cps_xxx`

鉴权由现有 `h.Middleware()` 处理（识别 `cps_*` Bearer token 并做 RBAC）。新路径须在 `permissionForManagementRequest` 中映射权限，否则默认拒绝（fail closed）。

### 4.2 成员列表接口 `GET /v0/management/data-source/members`

**查询参数**：

| 参数 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `page` | int | 否 | 1 | 页码，从 1 开始 |
| `pageSize` | int | 否 | 100 | 每页条数，最大 500 |

**响应**（200）：

```json
{
  "members": [
    {
      "id": "4ba92e06-f5f2-4b23-9158-ab0c166d7342",
      "email": "wen_guorong@hoperun.com",
      "name": "文国荣",
      "role": "member",
      "status": "active"
    }
  ],
  "total": 111,
  "page": 1,
  "pageSize": 100
}
```

**字段来源**（数据源 `end_users`）：

| 响应字段 | 来源 | 规则 |
|---------|------|------|
| `id` | `end_users.id` | 直接使用（uuid） |
| `email` | `end_users.display_name` | `pinyin(display_name) + "@hoperun.com"` |
| `name` | `end_users.display_name` | 直接使用（中文姓名） |
| `role` | — | 固定 `"member"` |
| `status` | `end_users.status` | 直接使用（`active` / `disabled` / `suspended` 等） |
| `total` / `page` / `pageSize` | 分页 | 返回总记录数与当前分页 |

空数据时 `members` 返回空数组 `[]`（手册检查清单要求，不报错）。

### 4.3 用量数据接口 `GET /v0/management/data-source/usage-events`

**查询参数**：

| 参数 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| `startDate` | string | **是** | — | 起始时间，ISO 8601，如 `2026-08-01T00:00:00+08:00` |
| `endDate` | string | **是** | — | 截止时间，ISO 8601，如 `2026-08-01T23:59:59+08:00` |
| `page` | int | 否 | 1 | 页码，从 1 开始 |
| `pageSize` | int | 否 | 100 | 每页条数，最大 500 |

**响应**（200）：

```json
{
  "usages": [
    {
      "timestamp": "2026-08-01T10:23:45+08:00",
      "userEmail": "wen_guorong@hoperun.com",
      "model": "deepseek-v4-flash",
      "source": "CLI",
      "operation": "Agent",
      "credits": 0,
      "cost": 0.0009,
      "costCurrency": "USD",
      "inputTokens": 28437,
      "outputTokens": 164,
      "cacheReadTokens": 24064,
      "cacheWriteTokens": 0
    }
  ],
  "total": 256,
  "page": 1,
  "pageSize": 100
}
```

**字段来源**（数据源 `request_logs`）：

| 响应字段 | 来源 | 规则 |
|---------|------|------|
| `timestamp` | `request_logs.timestamp` | 直接使用（ISO 8601） |
| `userEmail` | `request_logs.api_key_name` | `pinyin(api_key_name) + "@hoperun.com"`；`api_key_name` 为空的行**跳过** |
| `model` | `request_logs.model` | 直接使用 |
| `source` | — | 固定 `"CLI"` |
| `operation` | — | 固定 `"Agent"` |
| `credits` | — | 固定 `0` |
| `cost` | `request_logs.cost` | 直接使用 |
| `costCurrency` | — | 固定 `"USD"` |
| `inputTokens` | `request_logs.input_tokens` | 直接使用 |
| `outputTokens` | `request_logs.output_tokens` | 直接使用 |
| `cacheReadTokens` | `request_logs.cached_tokens` | 使用合并缓存值 |
| `cacheWriteTokens` | — | 固定 `0` |

`startDate`/`endDate` 为**半开区间 `[start, end)`** 查询（含起始、不含截止），与 TokenHub 增量同步窗口语义一致。空数据时 `usages` 返回空数组 `[]`。

## 5. 拼音转换规则

- 引入依赖：`github.com/mozillazg/go-pinyin`
- 输入：中文姓名（如 `文国荣`）
- 输出：全拼小写，姓与名用 `_` 连接，如 `wen_guorong`
- 拼接：`pinyin(姓名) + "@hoperun.com"` → `wen_guorong@hoperun.com`
- 多音字：使用库默认读音兜底（如姓氏 单/仇/曾 按常见读音），不要求精准
- 非中文字符（如已含英文字母/数字的姓名）原样保留小写

## 6. 组件设计

### 6.1 新增/修改文件

| 文件 | 改动 |
|------|------|
| `internal/api/handlers/management/data_source.go` | **新增**：`GetDataSourceMembers`、`GetDataSourceUsageEvents` handler |
| `internal/api/routes/management_data_source_routes.go` | **新增**：注册两个端点，挂到 `/v0/management` 组 |
| `internal/api/routes/management.go` | 调用新的路由注册函数 |
| `internal/api/handlers/management/route_permissions.go` | `permissionForManagementRequest` 新增 `datasource.read` 映射（两个路径） |
| `internal/util/pinyin.go` | **新增**：中文姓名 → 拼音小写 `姓_名` 工具函数 + 单测 |
| `internal/usage/usage_db.go`（或 `request_log_query.go`） | `LogQueryParams` 新增 `StartTime`/`EndTime` 可选字段，`QueryLogs` 支持绝对时间窗口过滤 |
| `go.mod` | 新增 `github.com/mozillazg/go-pinyin` |

### 6.2 Handler 逻辑

- `GetDataSourceMembers`：分页读取 `end_users`，逐条映射为 members 响应，返回 `{members, total, page, pageSize}`。
- `GetDataSourceUsageEvents`：
  1. 解析并校验 `startDate`/`endDate`（ISO 8601，含时区），非法或缺失 → 400
  2. 用 `LogQueryParams{StartTime, EndTime, Page, Size}` 调用现有 `QueryLogs`
  3. 逐行映射字段，`api_key_name` 为空跳过，返回 `{usages, total, page, pageSize}`

### 6.3 时间窗口查询

现有 `LogQueryParams` 只有相对 `Days`。扩展方案：新增可选 `StartTime`/`EndTime` `time.Time` 字段，`QueryLogs` 的 SQL WHERE 在二者提供时用 `timestamp >= ? AND timestamp < ?`（postgres 与 sqlite 均支持）；未提供时保持现有 `Days` 行为不变（非破坏性）。

## 7. 错误处理

| 场景 | 行为 |
|------|------|
| 未认证 / token 失效 | 401（现有中间件） |
| 运管角色无 `datasource.read` 权限 | 403（现有中间件 fail-closed） |
| `startDate`/`endDate` 缺失或非法 | 400，`{"error": "..."}` |
| `endDate` < `startDate` | 400 |
| `pageSize` 超 500 | 钳制到 500（响应 `pageSize` 返回实际钳制值） |
| 查询无数据 | 200 + 空数组 `[]` |
| 数据库查询失败 | 500，`{"error": "..."}` |

## 8. 测试

- **单测**：`pinyin.go`（文国荣→wen_guorong、多音字兜底、非中文字符、空输入）
- **单测**：字段映射函数（固定值 source/operation/credits/costCurrency/cacheWriteTokens、email 拼接）
- **集成测试**：handler 测试——插入 request_logs / end_users 行，验证 members 与 usage-events 的响应结构、分页、时间过滤、空 api_key_name 跳过
- **路由/权限测试**：`management_test.go` 断言两个新路径存在且权限映射为 `datasource.read`

## 9. 非目标（Out of Scope）

- 不实现 TokenHub 侧的拉取/聚合逻辑（TokenHub 平台负责）
- 不改动现有 `/v0/management/usage/logs` 等接口的响应格式
- 不做拼音人工校正表（多音字按库默认值兜底即可）
- 不实现 TokenHub 手册中的「数据源配置」「凭证管理」等 TokenHub 侧功能
- 不接入 Redis 或改动请求日志采集逻辑
