## MODIFIED Requirements

### Requirement: SSE frames survive arbitrary byte boundaries

系统 SHALL 按 SSE 语义解析 LF 和 CRLF 行结束符、空行事件边界、注释行、`event`、`id` 和多行 `data` 字段。解析结果 MUST 与底层读取 chunk 的大小和边界无关，并 MUST 在受控的最大事件大小内工作。

HTTP 响应使用受支持的内容编码时，系统 MUST 将解码后的响应流交给同一分帧器。gzip MUST 在响应仍进行时增量交付已收到的完整事件，事件字节上限适用于解码后的数据。

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

#### Scenario: Decode and deliver gzip frames as they arrive

- **WHEN** 服务端通过 gzip 响应发送一条包含 UTF-8 的完整 SSE 事件，刷新输出并等待稍后发送后续事件
- **THEN** 系统在响应结束前交付首条事件，并按原顺序交付后续事件
- **THEN** gzip 和无压缩响应中同一段 SSE 产生相同的事件名称、ID 和数据

### Requirement: Stream termination is explicit and cancellable

系统 SHALL 支持 context 取消和 idle timeout，并在终止时关闭响应 body、停止所有相关 goroutine 并关闭由其拥有的输出 channel。已建立的流断开后系统 MUST NOT 在 transport 层自动重连或重放请求。

取消或已建立流的空闲超时 SHALL 解除实际 HTTP 读取与内容解码阻塞。读到 Provider 所需终态之前发生解码失败时，系统 MUST 走既有流失败路径。错误与诊断遵循既有安全输出规则。

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

#### Scenario: Release a stalled compressed stream

- **WHEN** 服务端已刷新一条 gzip SSE 事件，随后停止输出直到客户端取消请求，且已建立流达到空闲超时
- **THEN** 实际 HTTP 请求结束，内容解码和 reader 退出，响应体与输出 channel 完成关闭
- **THEN** 调用方收到 idle timeout，服务端只观察到一次请求
