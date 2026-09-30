# provider/http-stream-transport Specification

## Purpose

定义 Provider 可复用的 HTTP 流式传输行为，确保自定义 API 前缀、稳定请求字节、SSE 分帧、取消和超时在不同服务地址下具有一致且可测试的语义。

## Requirements

### Requirement: Base URL is an API prefix

系统 SHALL 将 `base_url` 解释为 API 路径前缀，并将 provider endpoint 作为相对路径追加；系统 MUST 保留 `base_url` 已有路径，且不得因 endpoint 以斜杠表示而回退到主机根路径。

系统 SHALL 仅接受带有 `http` 或 `https` scheme 和有效 host 的绝对 URL。本阶段的 `base_url` MUST NOT 包含 userinfo、query 或 fragment；校验错误不得回显密钥或敏感 header。

#### Scenario: Resolve endpoint from host-only prefix

- **WHEN** `base_url` 为 `https://gateway.example` 且 endpoint 为 `responses`
- **THEN** 请求 URL 为 `https://gateway.example/responses`

#### Scenario: Preserve an existing path prefix

- **WHEN** `base_url` 为 `https://gateway.example/openai/v1/` 且 endpoint 为 `responses`
- **THEN** 请求 URL 为 `https://gateway.example/openai/v1/responses`

#### Scenario: Reject ambiguous base URL components

- **WHEN** `base_url` 包含 userinfo、query、fragment、不受支持的 scheme 或缺少 host
- **THEN** 系统在发起网络请求前返回 `invalid_configuration` 类错误
- **THEN** 错误文本不包含 API key、Authorization 或 URL userinfo 内容

### Requirement: Request bytes are stable and bounded

Provider RequestCompiler SHALL 使用确定性 JSON 编码生成完整且有大小边界的 canonical request bytes；transport SHALL 将调用方提供的 method、headers 和已经编译的正文 bytes 作为同一次 HTTP 请求发送，不得接收无约束结构化值或再次执行 JSON 序列化。transport 在产生网络副作用前 SHALL 验证正文非空、是合法 JSON、未超过上限，并取得不受调用方后续修改影响的独立快照。

相同 Provider 输入重复编译 MUST 产生相同正文 bytes；请求错误和诊断信息 MUST NOT 包含 Authorization、API key 或完整请求正文。该职责调整不得改变现有 Anthropic Messages 或 OpenAI Responses 的 golden request bytes。

#### Scenario: Equivalent input produces identical request bytes

- **WHEN** 使用相同的强类型 Provider 请求输入重复编译流式请求
- **THEN** 每次 RequestCompiler 产生的 canonical JSON 正文字节完全一致
- **THEN** transport 逐字节发送该正文而不重新编码

#### Scenario: Snapshot request bytes before sending

- **WHEN** 调用方在请求被接受后修改其原始 byte slice
- **THEN** transport 发送的正文仍等于接受时的独立快照

#### Scenario: Reject invalid or oversized compiled bytes

- **WHEN** 调用方提供空正文、malformed JSON 或超过请求上限的 canonical bytes
- **THEN** transport 在建立网络请求前返回安全 provider 错误

#### Scenario: Request failure is safely reported

- **WHEN** 服务端在流建立前返回非成功 HTTP 状态
- **THEN** 系统返回包含安全状态摘要的 provider 错误
- **THEN** 错误、日志和测试 snapshot 均不包含敏感 header 或请求正文

### Requirement: SSE frames survive arbitrary byte boundaries

系统 SHALL 按 SSE 语义解析 LF 和 CRLF 行结束符、空行事件边界、注释行、`event`、`id` 和多行 `data` 字段。解析结果 MUST 与底层读取 chunk 的大小和边界无关，并 MUST 在受控的最大事件大小内工作。

#### Scenario: Parse randomly chunked UTF-8 stream

- **WHEN** 同一组 SSE bytes 在任意字节位置切块，包括 UTF-8 编码单元中间
- **THEN** 系统产生与一次性读取相同且顺序一致的事件

#### Scenario: Parse LF and CRLF event streams

- **WHEN** 服务端分别使用 `\n\n` 和 `\r\n\r\n` 分隔事件
- **THEN** 系统对两种流产生相同的事件序列

#### Scenario: Join multiline data and ignore comments

- **WHEN** 一个事件包含多行 `data` 且流中穿插 SSE comment heartbeat
- **THEN** 系统用换行连接该事件的 data 行
- **THEN** comment heartbeat 不产生 provider 事件但计为流活动

#### Scenario: Reject oversized event

- **WHEN** 单个 SSE 事件超过配置的最大事件大小
- **THEN** 系统终止该流并返回明确的 stream protocol 错误

### Requirement: Stream termination is explicit and cancellable

系统 SHALL 支持 context 取消和 idle timeout，并在终止时关闭响应 body、停止所有相关 goroutine 并关闭由其拥有的输出 channel。已建立的流断开后系统 MUST NOT 在 transport 层自动重连或重放请求。

#### Scenario: Cancel an active stream

- **WHEN** 调用方取消正在等待 SSE 数据的 context
- **THEN** 读取在有限时间内退出并返回 cancellation
- **THEN** 不遗留继续读取或发送事件的后台 goroutine

#### Scenario: Idle stream times out

- **WHEN** 已建立连接在配置的 idle timeout 内没有任何 SSE 数据或 heartbeat 活动
- **THEN** 系统关闭流并返回 idle timeout 错误

#### Scenario: EOF is distinct from successful provider completion

- **WHEN** HTTP body 到达 EOF
- **THEN** transport 报告流已结束
- **THEN** transport 不自行判断 provider turn 是否成功，也不自动重新发起请求
