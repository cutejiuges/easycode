# provider/openai-responses-text Specification

## Purpose

定义 OpenAI Responses 在首个文本对话切片中的请求、流式归并和原生历史契约，使多轮续写不依赖 UI 事件重建并保留后续扩展所需的 provider 原生数据。

## Requirements

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

系统 SHALL 以单个 Responses sample 状态机归并事件：有效的 `response.created` 必须先建立唯一的非空 response ID；`response.output_text.delta` SHALL 投影为 assistant 文本增量；`response.output_item.done` SHALL 按到达顺序保存完成的原生 item；仅有 ID 与 created ID 一致且首次出现的 `response.completed` 才是成功终态。

系统 SHALL 将与当前 response ID 一致的 `response.failed` 和 `response.incomplete` 视为失败终态。重复 created、终态前缺少 created、response ID 冲突、重复终态和终态后的任何事件 MUST 作为 stream protocol failure；未知但格式正确且位于活动 sample 内的 event MUST NOT 导致 panic，系统 SHALL 可诊断地忽略当前切片不消费的事件，同时不得把未知事件伪装成已支持能力。

#### Scenario: Establish one response identity

- **WHEN** 服务端首先发送包含非空 ID 的 `response.created`
- **THEN** reducer 将该 ID 固定为当前 sample identity
- **THEN** 相同或不同 ID 的第二个 `response.created` 都被拒绝为协议错误

#### Scenario: Stream assistant text

- **WHEN** 活动 response 依次发送多个 `response.output_text.delta`
- **THEN** 系统按接收顺序产生对应的 assistant 文本增量事件

#### Scenario: Preserve completed native items

- **WHEN** 活动 response 发送 `response.output_item.done`
- **THEN** 系统按 wire 顺序保存该 item 的已知强类型字段
- **THEN** 未识别扩展仅保留在 OpenAI 包内受控的 opaque envelope 中

#### Scenario: Complete only on response.completed

- **WHEN** 服务端发送 response ID 与 created ID 相同的首个合法 `response.completed`
- **THEN** provider 产生且仅产生一次 completed 终态

#### Scenario: Reject a conflicting completion identity

- **WHEN** `response.completed`、`response.failed` 或 `response.incomplete` 的 response ID 缺失或不同于 created ID
- **THEN** provider 产生 stream protocol failure，不提交 staged native history

#### Scenario: Reject illegal event order

- **WHEN** 服务端在 `response.created` 前发送受支持的 sample event，或在任一终态后继续发送事件
- **THEN** reducer 返回 stream protocol failure且不产生第二终态

#### Scenario: Surface failed and incomplete responses

- **WHEN** 活动 response 发送 ID 匹配的 `response.failed` 或 `response.incomplete`
- **THEN** provider 产生失败终态和稳定英文错误码
- **THEN** 失败信息不包含请求正文或敏感 header

#### Scenario: Ignore unknown well-formed event

- **WHEN** 活动 response 发送当前切片未识别但 JSON 格式正确的 event type
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

### Requirement: Responses completion preserves and normalizes usage

OpenAI reducer SHALL 只从与活动 response ID 一致的首个合法 `response.completed.response.usage` 取得最终 sample usage，并将受支持的 raw usage 字段保存在 Provider-native commit 中。缺失整个 usage 或缺失可表达字段 MUST 保持 unknown，不得以默认零补齐；明确提供的零 SHALL 保存为已知零。

已提供的 token 计数 MUST 为非负整数，cached input MUST NOT 大于总 input，details 子字段与总量之间的关系必须通过语义校验。损坏结构、负值、不可能的计数关系或溢出 MUST 将 completed event 转为 stream protocol failure，并丢弃 sample staging。合法 completed response 产生的 prepared sample SHALL 同时携带保存 raw usage 的 native commit 和对应 normalized usage。

#### Scenario: Normalize a complete Responses usage object

- **WHEN** `response.completed` 报告 input、cached input、cache write、output 和 reasoning output
- **THEN** Provider 保留 raw usage，并产生通过统一映射得到的五项 normalized usage

#### Scenario: Complete with missing Responses usage

- **WHEN** 兼容服务返回合法 `response.completed` 但省略整个 usage
- **THEN** sample 仍可完成，raw usage 和所有可表达 normalized 指标保持 unknown
- **THEN** 系统不生成全零 usage

#### Scenario: Preserve explicit Responses zeros

- **WHEN** Responses usage 明确报告任一 token 字段为零且字段关系合法
- **THEN** 对应 raw 与 normalized 指标保存为已知零

#### Scenario: Reject cached input above total input

- **WHEN** Responses usage 报告 cached input 大于总 input
- **THEN** Provider 产生 stream protocol failure，不生成成功 prepared sample

#### Scenario: Bind usage to the active response

- **WHEN** completion response ID 与 `response.created` 建立的 ID 不同
- **THEN** Provider 拒绝该 completion 及其 usage，不提交任何当前 sample 事实
