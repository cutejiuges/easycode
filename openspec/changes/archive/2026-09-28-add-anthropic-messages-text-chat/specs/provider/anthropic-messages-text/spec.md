## Purpose

定义 Anthropic Messages 在文本对话切片中的确定性请求、content-block 流式状态机和原生历史契约，使自定义 API 前缀能够完成多轮对话，同时无损保留 thinking signature 与 redacted thinking 等 Anthropic 原生数据。

## ADDED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL 向解析后的相对 `messages` endpoint 发送 Anthropic Messages 请求，并保留 `base_url` 已有路径前缀。请求 SHALL 使用 `x-api-key` 鉴权、`anthropic-version: 2023-06-01`、JSON content type 和 SSE accept header；正文 MUST 包含配置的 model、正整数 `max_tokens`、按原顺序排列的 Anthropic 原生 messages，以及 `stream=true`。

当前切片 SHALL 将用户文本编码为 user text content block，MUST NOT 声明 tools、system、thinking 配置或 `cache_control`。API key 和请求正文 MUST NOT 进入错误、日志、fixture 或 snapshot。

#### Scenario: Compile the first Anthropic turn

- **WHEN** 空会话收到第一条非空用户文本
- **THEN** 请求发送到 `messages` endpoint，messages 只包含该用户文本对应的 user message
- **THEN** 请求包含确定的 model、`max_tokens` 和 `stream=true`，且不包含延期字段

#### Scenario: Preserve an Anthropic API path prefix

- **WHEN** `base_url` 为带路径的 API 前缀并提交一轮文本输入
- **THEN** 系统在该前缀后追加 `messages`，不会回退到 host 根路径或隐式补充其他 wire 路径

#### Scenario: Send Anthropic authentication and version headers

- **WHEN** 系统建立 Messages SSE 请求
- **THEN** 请求使用配置的 secret 作为 `x-api-key` 并发送 `anthropic-version: 2023-06-01`
- **THEN** secret 不出现在请求错误、诊断摘要或测试快照中

#### Scenario: Produce stable request bytes

- **WHEN** model、输出 token 上限、原生历史和当前输入完全相同
- **THEN** 请求的 canonical JSON bytes 和对应 fingerprint 完全相同
- **THEN** API key、时间戳、随机 ID、TUI 状态和其他动态宿主数据不参与 fingerprint

### Requirement: Reduce Anthropic content blocks with an indexed state machine

系统 SHALL 独立处理 `message_start`、`content_block_start`、`content_block_delta`、`content_block_stop`、`message_delta` 和 `message_stop`，并按 block index 与 wire 顺序归并 content blocks。`text_delta` SHALL 产生 typed assistant text delta；`thinking_delta` 与 `signature_delta` SHALL 只更新匹配的 thinking block；`redacted_thinking` SHALL 作为不可解释的原生 block 保存。

已开始 block 的重复 index、未开始 block 的 delta/stop、delta 与 block 类型不匹配、损坏 JSON 或在未完成 block 存在时收到 `message_stop` MUST 终止为 stream protocol error。格式正确但当前切片未知的 event SHALL 被安全忽略且不得伪造文本、native item 或成功终态；未知扩展不得传播为无约束的共享 map。

#### Scenario: Stream ordered assistant text

- **WHEN** text block 依次收到多个 `text_delta` 并正常停止
- **THEN** 系统按接收顺序产生对应的 assistant text delta
- **THEN** 完成的原生 text block 只包含一次最终文本，不因 start 与 delta 数据重复而重复内容

#### Scenario: Preserve thinking and its signature

- **WHEN** thinking block 收到一个或多个 `thinking_delta`、有效 `signature_delta` 并正常停止
- **THEN** 系统按原顺序保存 thinking 文本和最终 opaque signature
- **THEN** signature 不作为 assistant 可见文本或 token 增量投影到 TUI

#### Scenario: Preserve redacted thinking

- **WHEN** `content_block_start` 携带 `redacted_thinking` block 并随后正常停止
- **THEN** 系统不解析、不改写其 opaque data，并将该 block 保留在 Anthropic 原生项中

#### Scenario: Reject a mismatched delta

- **WHEN** text block 收到 `thinking_delta`，或 delta 引用尚未开始的 block index
- **THEN** Provider 产生稳定的 stream protocol failure，且不提交本轮暂存历史

#### Scenario: Ignore an unknown well-formed event

- **WHEN** 服务端发送当前切片未知但 JSON 格式正确的 event type
- **THEN** Provider 不 panic、不产生伪造输出，并继续等待后续受支持事件或显式终态

### Requirement: Native Messages history commits transactionally

系统 SHALL 在会话级 Anthropic 原生历史中保持 user/assistant message 边界、content block 顺序和已支持的原生字段。当前用户 message、已完成 assistant blocks、message metadata、stop reason 和原始 usage SHALL 先进入 turn staging，并仅在所有 blocks 完成且收到合法 `message_stop` 后一次性提交。

后续请求 SHALL 从已提交的 Anthropic 原生历史直接编译，不得从 RuntimeEvent、TUI transcript、SemanticHistoryView 或 OpenAI item 反向构造。失败、取消、idle timeout、协议错误或 `message_stop` 前 EOF MUST 丢弃整个 staging。

#### Scenario: Commit a successful Anthropic turn

- **WHEN** 一轮包含用户输入、一个或多个完成的 assistant blocks，并以合法 `message_stop` 结束
- **THEN** user message 和 assistant message 按 wire 顺序一次性进入原生历史
- **THEN** Provider 在成功 terminal 前按顺序发布完成的 native blocks

#### Scenario: Compile a later turn from native history

- **WHEN** 已成功完成一轮后提交第二条用户文本
- **THEN** 第二轮请求依次包含首轮 user message、首轮 assistant 原生 blocks 和新的 user message
- **THEN** thinking signature 与 redacted thinking 在合法请求位置原样回放

#### Scenario: Discard a partial failed turn

- **WHEN** 已产生文本 delta 或完成 content block 后发生失败、取消、timeout 或提前 EOF
- **THEN** 本轮 user message 和所有 staged assistant blocks 均不进入已提交历史
- **THEN** 下一轮请求不包含该失败轮次的部分原生数据

#### Scenario: Preserve final message metadata without inventing usage

- **WHEN** `message_start` 和 `message_delta` 提供 stop reason 或 usage 字段
- **THEN** Provider 保存服务端给出的最终原始值，并将缺失字段保持为 unknown 而不是伪装为零

### Requirement: Completion and capabilities match implemented Anthropic behavior

Anthropic Provider MUST 仅在合法 `message_stop` 后产生且仅产生一次 completed terminal。服务端 error event、取消、idle timeout、oversized frame、解析失败、非法事件序列或 completed 前 EOF SHALL 映射为恰好一次 failed/cancelled terminal，并 MUST NOT 由 Provider 或 transport 自动重放请求。

Provider SHALL 只声明本切片实际实现并通过测试的 streaming 与 thinking-signature 保留能力；tools、并行工具、prompt cache control、prompt cache key、previous response 和未实现的 reasoning 展示能力 MUST 保持 false 或 unsupported。

#### Scenario: Complete only on message_stop

- **WHEN** 所有 content blocks 已正常停止并收到合法 `message_stop`
- **THEN** Provider 产生一次 completed terminal，随后关闭其拥有的输出 channel

#### Scenario: Reject EOF before message_stop

- **WHEN** HTTP SSE body 在 `message_stop` 前结束
- **THEN** Provider 产生 stream protocol failure，不把 channel 关闭解释为成功
- **THEN** 本轮原生 staging 被丢弃且请求不被自动重放

#### Scenario: Cancel or time out an Anthropic stream

- **WHEN** turn context 被取消或已建立流超过 idle timeout
- **THEN** Provider 关闭流资源、等待清理完成并产生一次可识别的 cancelled 或 timeout failure
- **THEN** 不遗留继续读取或发送事件的 goroutine

#### Scenario: Inspect Anthropic capabilities

- **WHEN** Runtime 查询 Anthropic Provider capabilities
- **THEN** streaming 与 thinking-signature 保留能力被准确报告
- **THEN** 本切片延期的 tools、cache 和增量 continuation 能力未被声明为可用
