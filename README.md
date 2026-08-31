# TokenHub

一个类似 OpenRouter 的 LLM 网关（中转站）。下游客户端用 OpenAI 或 Anthropic 协议请求，网关按模型映射表路由到上游供应商，同格式透传、异格式自动转换，支持多 Key 轮询、多渠道优先级降级、流式转发、用量计费与全链路追踪。

单二进制部署（内嵌 Web 管理台），SQLite 存储，无外部依赖。

## 功能

- **双协议下游**：`POST /v1/chat/completions`（OpenAI）、`POST /v1/messages`（Anthropic），`GET /v1/models` 返回模型列表与元属性/价格
- **双协议上游**：供应商类型为 `openai` 或 `anthropic`，上下游格式相同直接透传（仅改写模型名并注入 `stream_options.include_usage`），不同则内部转换
- **格式转换**：请求/非流式响应/流式 SSE 双向转换，覆盖 system、图片（base64/URL）、工具调用（tool_calls ↔ tool_use/tool_result）、推理内容（reasoning_content ↔ thinking）
- **多 Key 轮询**：每个供应商可配多个 API Key，原子轮询分发；`401/403` 标记 Key 失效，`429` 冷却 60 秒并轮换下一个 Key
- **渠道降级**：一个下游模型映射多个上游渠道，按优先级依次尝试；Key 耗尽自动降级到下一渠道；首包输出前失败可静默重试
- **完整流式**：SSE 边转发边解析，首字延迟统计；上游异常断流时自动补齐下游收尾帧
- **model 改写**：下游响应（含流式每个 chunk）中的 `model` 字段统一为内部设定的下游模型名，客户端感知不到上游真实模型
- **计费**：缓存命中不重复计费——输入价只对未命中缓存的 tokens 收取，缓存读/写按各自单价计费（与 DeepSeek/Anthropic 口径一致）。公式：`(输入-缓存)×输入价 + 缓存读×缓存读价 + 缓存写×缓存写价 + 输出×输出价`，单位每百万 token；日志与统计展示完整输入 tokens
- **全链路追踪**：下游请求日志与上游调用日志分表存储，不记录正文内容；按 trace_id 查看完整调用链（每次尝试的供应商、Key、状态码、错误类型、tokens、耗时）
- **用户体系**：用户自助创建/删除下游 Key（明文仅创建时返回一次）、查看模型列表与价格、查询自己的日志与用量统计
- **管理后台**：供应商与 Key 管理（含 Key 连通性测试）、模型/属性/价格/渠道映射管理、用户管理、全站日志与调用链、全站用量统计
- **MCP 服务器**：`POST /mcp`（Streamable HTTP），让用户在自己的 AI Agent 中以 `Bearer th-xxx` 接入网关查询自己的数据，6 个只读工具按 user 隔离

## 快速开始

```bash
# 构建（首次需先构建前端管理台）
cd web && npm install && npm run build && cd ..
go build -o tokenhub ./cmd/server

# 启动（首次启动自动创建管理员账号）
PORT=8080 DB_PATH=tokenhub.db ADMIN_USERNAME=admin ADMIN_PASSWORD=admin123 ./tokenhub

# 前端开发模式
cd web && npm run dev   # 热加载，/api 与 /v1 代理到 localhost:8080
```

打开 `http://localhost:8080` 进入管理台，默认账号 `admin / admin123`（请立即修改密码）。

### 配置步骤（管理台）

1. **供应商管理** → 新建供应商（协议选 openai/anthropic，填 Base URL）→ 添加一个或多个 API Key，可用「测试」按钮验证连通性。Base URL 直接照供应商文档填：**OpenAI 兼容地址含 `/v1` 后缀**（如 `https://api.openai.com/v1`，网关内部补 `/chat/completions`）；**Anthropic 兼容地址不带 `/v1`**（如 `https://api.anthropic.com`，网关内部补 `/v1/messages`）
2. **模型列表** → 新建模型（模型名即下游调用名，配置元属性与四项价格）→ 点「渠道」添加上游渠道（供应商 + 上游模型名 + 优先级，数字越小越优先）
3. **我的 API Key** → 创建下游 Key
4. 用下游 Key 调用网关：

```bash
# OpenAI 格式
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer th-xxxx" \
  -d '{"model":"gpt-mock","messages":[{"role":"user","content":"hi"}]}'

# Anthropic 格式（同一个 Key）
curl http://localhost:8080/v1/messages \
  -H "x-api-key: th-xxxx" \
  -d '{"model":"gpt-mock","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}'
```

### MCP 接入（让 Agent 查你的网关数据）

TokenHub 在 `/mcp` 暴露一个 MCP（Model Context Protocol）服务器，用户可用自己创建的**下游 Key** 在 Claude Desktop / Cline / Cursor 等客户端接入。**所有工具都只读，且只返回当前 Key 所属用户的数据。**

可用的 6 个工具：

| 工具 | 用途 |
|---|---|
| `list_models` | 列出所有可用模型与元属性/价格 |
| `list_my_keys` | 列出自己的下游 Key（不含明文/哈希） |
| `list_my_logs` | 分页查询自己的请求日志（按 model/status/days/trace_id 过滤） |
| `get_trace_detail` | 查询单次调用的完整 trace（下游 + 所有上游尝试） |
| `get_my_stats` | 按天聚合用量 + 按模型 TOP |
| `get_my_account` | 返回当前用户账号信息（用户名、角色、累计消费） |

客户端配置示例（Claude Desktop `claude_desktop_config.json` / Cline / Cursor 等通用）：

```json
{
  "mcpServers": {
    "tokenhub": {
      "url": "https://your-host/mcp",
      "headers": { "Authorization": "Bearer th-你的key" }
    }
  }
}
```

用 curl 验证握手：

```bash
curl -X POST https://your-host/mcp \
  -H "Authorization: Bearer th-xxxx" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
```

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PORT` | 8080 | 监听端口 |
| `DB_PATH` | tokenhub.db | SQLite 文件路径 |
| `JWT_SECRET` | 自动生成并持久化 | 管理台会话签名密钥 |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | admin / admin123 | 首次启动创建的管理员 |
| `BODY_LIMIT_MB` | 20 | 请求体大小上限 |

## 项目结构

```
cmd/server/          入口与路由装配
internal/
  config/            环境变量配置
  db/                SQLite 迁移、GORM 模型、异步日志写入器
  auth/              JWT（管理台）与下游 Key 鉴权（SHA-256 哈希存储）
  convert/           OpenAI ↔ Anthropic 双向转换（请求/响应/流式状态机）+ 单测
  relay/             网关核心：渠道降级、Key 轮询、流式转发、重试
  billing/           计费（含缓存 token 单独计价）
  api/               管理/用户 REST API
  mcp/               MCP 服务器：6 个工具（只读，按 user 隔离）+ Streamable HTTP transport
  web/               嵌入的 Vue3 管理台（go:embed）
web/                 Vue3 + Element Plus 前端源码
```

## 测试

```bash
go test ./...
```

- `internal/convert`：请求/响应/流式转换的单元测试（含双向 round-trip）
- `internal/relay`：基于 httptest mock 上游的端到端测试，覆盖透传、跨协议转换、流式转换、Key 轮询（401/429）、渠道降级、鉴权失败、未知模型
- `internal/mcp`：MCP 协议握手、`tools/list`、6 个工具 happy path、user 隔离与越权防护

## 设计说明

- **重试边界**：上游响应首字节写往下游之前失败可静默重试（换 Key/换渠道）；流式已开始输出后失败，向前端发送错误帧并终止
- **Key 状态机**：`active` → 401/403 → `invalid`（不再参与轮询，可手动恢复）；429 → `rate_limited`（冷却 60s 后自动回归）；连续失败仅计数，不摘除
- **日志无正文**：两张日志表只存元数据与用量，避免敏感内容落库；`prompt_tokens` 统一为完整输入口径（含缓存命中，Anthropic 上游的 `input_tokens` 自动补齐），缓存读/写作为其中子集单独记录，计费在 billing 层拆分
- **SQLite**：WAL 模式 + busy_timeout，日志异步批量落库；高并发场景可平滑替换为 PostgreSQL（GORM 层兼容）
