## Purpose

定义 OpenAI Responses 在首个文本对话切片中的请求、流式归并和原生历史契约，使多轮续写不依赖 UI 事件重建并保留后续扩展所需的 provider 原生数据。

## ADDED Requirements

### Requirement: Compile a text-only Responses request
系统 SHALL 向解析后的 `responses` endpoint 发送 OpenAI Responses 请求。请求 MUST 包含配置的 model、完整会话原生 input 和当前用户输入，并 MUST 设置 `stream=true` 与 `store=false`。

本阶段请求 MUST NOT 使用 `previous_response_id`，MUST NOT 声明工具，并 SHALL 请求保留服务端返回的 encrypted reasoning 内容。Authorization SHALL 使用配置的 secret，且该 secret MUST NOT 进入错误、日志、fixture 或 snapshot。

#### Scenario: Compile the first turn
- **WHEN** 空会话收到第一条非空用户文本
- **THEN** 请求 input 只包含该用户输入对应的 OpenAI 原生 item
- **THEN** 请求启用 stream、禁用 store 且不包含 `previous_response_id`

#### Scenario: Compile a later turn from native history
- **WHEN** 已完成一轮对话后提交下一条用户文本
- **THEN** 请求 input 按原顺序包含已提交的用户 item、assistant 原生 output items 和新的用户 item
- **THEN** 请求不是从 RuntimeEvent、TUI transcript 或扁平共享 Message 反向构造

#### Scenario: Deterministic request compilation
- **WHEN** model、原生历史和当前输入完全相同
- **THEN** 编译结果的稳定 JSON bytes 完全相同

### Requirement: Reduce supported Responses stream events
系统 SHALL 将 `response.output_text.delta` 投影为 assistant 文本增量，将 `response.output_item.done` 保存为完成的原生 item，并仅将有效的 `response.completed` 视为 provider 成功终态。

系统 SHALL 将 `response.failed` 和 `response.incomplete` 视为失败终态。未知但格式正确的 event MUST NOT 导致 panic；系统 SHALL 可诊断地忽略当前切片不消费的事件，同时不得把未知事件伪装成已支持能力。

#### Scenario: Stream assistant text
- **WHEN** 服务端依次发送多个 `response.output_text.delta`
- **THEN** 系统按接收顺序产生对应的 assistant 文本增量事件

#### Scenario: Preserve completed native items
- **WHEN** 服务端发送 `response.output_item.done`
- **THEN** 系统保存该 item 的已知强类型字段
- **THEN** 未识别扩展仅保留在 OpenAI 包内受控的 opaque envelope 中

#### Scenario: Complete only on response.completed
- **WHEN** 服务端发送可解析的 `response.completed`
- **THEN** provider 产生且仅产生一次 completed 终态

#### Scenario: Surface failed and incomplete responses
- **WHEN** 服务端发送 `response.failed` 或 `response.incomplete`
- **THEN** provider 产生失败终态和稳定英文错误码
- **THEN** 失败信息不包含请求正文或敏感 header

#### Scenario: Ignore unknown well-formed event
- **WHEN** 服务端发送当前切片未识别但 JSON 格式正确的 event type
- **THEN** provider 不 panic、不产生伪造的文本或完成事件，并继续等待后续受支持事件

### Requirement: Native history commits transactionally
系统 SHALL 将当前用户 item 和本轮完成的 output items 暂存在 turn staging 中，并仅在收到 `response.completed` 后按 wire 顺序提交到会话级原生历史。失败、取消、idle timeout 或 completed 前 EOF MUST NOT 将部分 assistant output 提交为可续写历史。

#### Scenario: Commit a successful turn
- **WHEN** 一轮包含用户输入、一个或多个完成 output item 并以 `response.completed` 结束
- **THEN** 用户 item 和 output items 以确定顺序一次性提交到原生历史

#### Scenario: Discard a partial failed turn
- **WHEN** 已收到文本 delta 或 output item 后发生失败、取消、timeout 或提前 EOF
- **THEN** 本轮 staging 不进入已提交原生历史
- **THEN** 下一轮请求不会包含该部分 assistant output

#### Scenario: Preserve reasoning data without rendering it
- **WHEN** 完成的原生 item 包含 reasoning summary、raw reasoning 标识或 encrypted content
- **THEN** 系统在原生历史中无损保留本切片支持的字段
- **THEN** TUI 是否展示 reasoning 不影响后续请求中的原生历史

### Requirement: Advertised capabilities match implemented behavior
OpenAI Provider SHALL 只声明已经由本变更实现并通过测试的能力。工具、并行工具、prompt cache key、`previous_response_id` 和未实现的 reasoning 展示能力 MUST NOT 被声明为可用。

#### Scenario: Inspect capabilities after initialization
- **WHEN** Runtime 查询 OpenAI Provider capabilities
- **THEN** streaming 和本变更实际支持的原生保留能力被准确报告
- **THEN** 未实现能力被报告为 false 或 unsupported
