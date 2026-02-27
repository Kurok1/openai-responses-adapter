# Responses API -> Chat Completions 代理迁移清单

## 1. 目标

构建一个 Go 中间代理，使客户端继续使用 `POST /v1/responses`，而代理将请求转换并转发到仅支持 `POST /v1/chat/completions` 的上游厂商，并尽量保持 Responses 语义兼容。

## 2. 当前状态（截至 2026-02-27）

- 已完成：
  - 基础服务骨架与路由
  - `POST /v1/responses` 最小文本输入转换（单轮）
  - 上游调用与透传响应
- 未完成（核心）：
  - `function calling` 全流程
  - `previous_response_id` 多轮状态链
  - Responses 流式事件语义
  - 完整 schema 兼容与测试

## 3. 官方语义差异（需要在代理中补齐）

### 3.1 输入/状态模型差异

- Responses 使用 `input`（字符串或 item 列表），Chat Completions 使用 `messages`。
- Responses 支持 `previous_response_id` 进行多轮链式上下文；Chat Completions 需要客户端/服务端手动管理完整历史。
- `instructions` 在 Responses 中是顶层字段；且与 `previous_response_id` 同用时，上一轮 `instructions` 不会自动继承。

## 3.2 Function Calling 差异（重点）

- 工具定义结构不同：
  - Chat Completions：`{"type":"function","function":{...}}`（外层 `function` 包裹）
  - Responses：`{"type":"function","name":"...","parameters":...}`（内部标记）
- 严格模式默认值不同：
  - 迁移文档指出：Chat Completions 默认 non-strict，Responses 默认 strict。
- 工具调用结果回传模型的形态不同：
  - Responses 中，`function_call` 与 `function_call_output` 是两类独立 item，通过 `call_id` 关联。
  - Chat Completions 中通常通过 `assistant.tool_calls` + 后续 `tool` role message（`tool_call_id`）完成闭环。

### 3.3 Structured Outputs 差异

- `response_format`（Chat Completions）需要映射为 `text.format`（Responses 语义）。

### 3.4 Streaming 差异

- Responses 流式是语义化事件（如 `response.output_text.delta`、`response.function_call_arguments.delta`）。
- Chat Completions 流式是 `choices[].delta` 风格。
- 代理需将上游流式增量重组为 Responses 事件格式。

## 4. 代理实现任务清单（按优先级）

### P0: 协议与数据模型落地（必须先做）

- 定义完整的请求/响应 DTO（至少覆盖）：
  - 请求：`model`, `input`, `instructions`, `tools`, `tool_choice`, `stream`, `previous_response_id`, `store`, `text.format`
  - 响应：`id`, `output[]`, `output_text`, `status`, `usage`（先做最小可用子集）
- 统一错误模型：将上游错误转换为 Responses 风格错误结构。

### P1: `previous_response_id` 支持（重点）

- 增加状态存储层（先内存，预留 Redis 接口）：
  - `response_id -> conversation snapshot/messages`
  - TTL、容量上限、淘汰策略（LRU）
- 请求处理逻辑：
  - 当请求带 `previous_response_id` 时，装载历史并拼接当前输入，再转为上游 `messages`
  - 处理 `instructions` 覆盖策略（不自动继承上一轮）
  - `previous_response_id` 找不到时返回明确 4xx 错误
- `store` 语义：
  - `store=true`：持久化该轮映射
  - `store=false`：仅本轮生效，不可被后续 `previous_response_id` 引用（或以配置开关允许短暂缓存）

### P2: `function calling` 端到端支持（重点）

- 请求映射：
  - Responses `tools`（function） -> Chat `tools`（function wrapper）
  - Responses `tool_choice` -> Chat `tool_choice`
  - strict 默认值差异显式处理，避免行为漂移
- 响应映射：
  - 上游 `assistant.tool_calls[]` -> Responses `output[]` 中 `function_call` items
  - 保留并映射 `call_id`（与上游 `tool_call_id` 建立稳定映射）
- 工具结果回注：
  - 识别客户端传入的 `function_call_output` item
  - 转为上游 `tool` role message（携带对应 `tool_call_id`）
  - 与历史上下文合并后继续发起上游请求
- 多工具并发：
  - 支持一次响应内多个 function_call
  - `parallel_tool_calls` 与顺序执行策略可配置

### P3: Streaming 兼容

- 当 `stream=true` 时：
  - 接收上游 Chat Completions SSE
  - 转换为 Responses 语义事件流（至少覆盖文本增量、函数参数增量、完成/失败事件）
- 增加断流/重试/关闭连接处理，避免 goroutine 泄漏。

### P4: 测试与兼容性验证

- 单元测试（表驱动）：
  - `input -> messages` 映射
  - `tools/tool_choice` 映射
  - `function_call/function_call_output` 映射
  - `previous_response_id` 历史拼装与错误分支
- 集成测试：
  - 单轮、多轮、函数调用、多函数并行、流式
- 回归基线：
  - 与官方迁移示例对齐（同输入下关键字段语义一致）

## 5. 推荐里程碑

1. M1（1-2 天）：P0 + P1，拿下多轮 `previous_response_id`
2. M2（2-4 天）：P2，拿下 function calling 闭环
3. M3（2-3 天）：P3，补齐流式事件
4. M4（1-2 天）：P4，完成测试与兼容性基线

## 6. 验收标准（DoD）

- 客户端仅调用 `/v1/responses`，无需感知上游是 Chat Completions。
- 支持：
  - 单轮文本
  - `previous_response_id` 多轮链式对话
  - function calling（含多工具调用与 `function_call_output` 回注）
  - `stream=true` 的 Responses 事件流
- 核心路径具备自动化测试覆盖。

## 7. 参考文档

- 迁移总览（官方）  
  https://developers.openai.com/api/docs/guides/migrate-to-responses
- Responses API Reference（`previous_response_id`/`tools`/`parallel_tool_calls` 等）  
  https://platform.openai.com/docs/api-reference/responses
- Streaming Responses（语义事件流）  
  https://platform.openai.com/docs/guides/streaming-responses
- Chat Completions API Reference（上游兼容目标）  
  https://platform.openai.com/docs/api-reference/chat/create-chat-completion
