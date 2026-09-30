## MODIFIED Requirements

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
