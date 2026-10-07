## MODIFIED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL 向解析后的相对 `messages` endpoint 发送 Anthropic Messages 请求，并保留 `base_url` 已有路径前缀。请求 SHALL 使用 `x-api-key` 鉴权、`anthropic-version: 2023-06-01`、JSON content type 和 SSE accept header；正文 MUST 包含配置的 model、正整数 `max_tokens`、可选的临时项目指令上下文 message、按原顺序排列的 Anthropic 原生历史 messages、当前用户 message，以及 `stream=true`。

存在项目指令文档时，RequestCompiler SHALL 把规范化快照编码为一个 Anthropic 原生 user context message，并将它置于全部已提交原生历史和当前用户 message 之前。该 message MUST 与真实用户输入保持独立，使用确定的来源边界，并在每轮从同一快照重新生成；它不得进入 Conversation native history、prepared sample 或 semantic history。不存在项目指令文档时，编译结果 MUST 与既有请求完全相同且不得生成空 context message。

当前切片 SHALL 将用户文本编码为 user text content block，MUST NOT 声明 tools、system、thinking 配置或 `cache_control`。API key、项目指令正文和绝对来源路径 MUST NOT 进入错误、日志、fixture 或 snapshot；项目指令只可出现在专门验证请求编译的脱敏 golden 中。

#### Scenario: Compile the first Anthropic turn

- **WHEN** 空会话在存在项目指令文档时收到第一条非空用户文本
- **THEN** 请求发送到 `messages` endpoint，messages 先包含项目指令 user context message，再包含真实用户 message
- **THEN** 请求包含确定的 model、`max_tokens` 和 `stream=true`，且不包含延期字段

#### Scenario: Compile the first Anthropic turn without project instructions

- **WHEN** 空会话使用无文档项目指令快照收到第一条非空用户文本
- **THEN** messages 只包含该用户文本对应的 user message
- **THEN** 请求 canonical bytes 与本变更前的相同输入 fixture 完全一致

#### Scenario: Preserve an Anthropic API path prefix

- **WHEN** `base_url` 为带路径的 API 前缀并提交一轮文本输入
- **THEN** 系统在该前缀后追加 `messages`，不会回退到 host 根路径或隐式补充其他 wire 路径

#### Scenario: Send Anthropic authentication and version headers

- **WHEN** 系统建立 Messages SSE 请求
- **THEN** 请求使用配置的 secret 作为 `x-api-key` 并发送 `anthropic-version: 2023-06-01`
- **THEN** secret 不出现在请求错误、诊断摘要或测试快照中

#### Scenario: Compile a later Anthropic turn from native history

- **WHEN** 已完成一轮 Anthropic 对话后使用同一项目指令快照提交下一条用户文本
- **THEN** messages 按项目指令 context message、已提交原生 messages、新用户 message 的顺序排列
- **THEN** 项目指令 context message 不作为已提交历史被重复或叠加

#### Scenario: Produce stable request bytes

- **WHEN** model、输出 token 上限、项目指令快照、原生历史和当前输入完全相同
- **THEN** 请求的 canonical JSON bytes 和对应 fingerprint 完全相同
- **THEN** API key、项目根绝对路径、cwd、时间戳、随机 ID、TUI 状态和其他动态宿主数据不参与 fingerprint
