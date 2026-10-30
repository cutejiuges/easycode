## MODIFIED Requirements

### Requirement: Streaming stdin uses a strict bounded NDJSON protocol

流式 stdin v1 SHALL 每行包含一个完整 UTF-8 JSON object，并至少包含整数 `version: 1`、字符串 `type` 和非空 `request_id`。v1 只允许：

- `input.submit`：包含 `text`，其语义和 4 MiB 上限与 Runtime submit input 一致。
- `turn.interrupt`：包含 `expected_turn_id`。
- `session.shutdown`：不包含额外 payload。
- `input.append`：包含 text，使用自动投递语义。
- `input.retract`：包含 1 至 64 个合法、非重复领域 InputIDs 的 input_ids。
- `turn.stop`：包含 expected_turn_id，停止并返还未消费自动输入。

decoder SHALL 支持任意读取 chunk、跨 chunk 的 JSON 行、UTF-8 多字节边界和 EOF 前没有换行的最后一行，同时在累积内容超过有界行限制前停止读取。它 MUST 拒绝重复字段、未知字段、未知 type/revision、空行以外的非 JSON、多个 JSON value、非法 UTF-8、非法 ID 和超限输入，且错误不得回显原始命令、prompt、配置或 secret。

#### Scenario: Decode fragmented input

- **WHEN** 一个合法 `input.submit` 在 JSON token、换行和 UTF-8 多字节字符中间被任意拆成多个读取 chunk
- **THEN** decoder 只产生一条完整命令，解码后的文本与原始文本相同

#### Scenario: Decode the final line at EOF

- **WHEN** stdin 以一个合法但没有尾部换行的 `session.shutdown` 结束
- **THEN** decoder 处理该命令后再关闭输入，而不是丢弃最后一行

#### Scenario: Reject an oversized line before unbounded allocation

- **WHEN** 当前行超过协议配置的硬上限且尚未出现换行
- **THEN** reader 停止累积、报告安全 `invalid_input`，并进入协议失败清理流程

新增字段 SHALL 严格拒绝 null、重复、未知字段与非法组合；input.submit 保持严格下一 turn，turn.interrupt 保持后续队列。

### Requirement: Every command receives one ordered control response

每个成功解码的入站命令 SHALL 产生恰好一个 `control.response`，其中包含 `version: 1`、相同 `request_id`、命令 `type`、`status` 以及与结果匹配的强类型字段。submit 被接受时 status SHALL 为 `accepted` 且 disposition 为 `starting` 或 `queued`；interrupt 被接受时 disposition SHALL 为 `interrupting`；shutdown 被接受时 disposition SHALL 为 `closing`。拒绝响应 SHALL 使用 `status: rejected` 和稳定安全的英文 error code/message。

`control.response` 只确认命令 admission 或拒绝，不得伪装成 durable turn start 或 terminal。对严格 input.submit 的 `starting`/`queued` submit，后续对应的全部 `turn.*` 和 `assistant.text.delta` 事件 SHALL 携带相同 `input_id`；只有 `turn.started` 才表示对应 `turn_started` 已 durable。显式 shutdown 丢弃已接受但未启动的输入时，系统 SHALL 为每个严格输入输出一次携带其 `input_id` 和 `session_shutdown` error 的 `input.discarded`；自动输入使用 input.status returned。

#### Scenario: Acknowledge a queued input before it starts

- **WHEN** `input.submit` 在另一个 turn 活跃时被接受
- **THEN** stdout 先输出该 request identity 的 `control.response(status=accepted, disposition=queued)`
- **THEN** 它真正开始时，`turn.started` 携带相同 `input_id` 和新 `turn_id`

#### Scenario: Report a stale interrupt

- **WHEN** `turn.interrupt` 的 expected turn ID 与当前活动 turn 不匹配
- **THEN** stdout 输出一次 `control.response(status=rejected)`，error code 为 `turn_mismatch`
- **THEN** 当前 turn 和 follow-up queue 不受影响

#### Scenario: Report a discarded queued input

- **WHEN** shutdown 被接受时存在尚未开始的严格 queued input
- **THEN** stdout 在结束前为该 input identity 输出一次 `input.discarded`
- **THEN** 该输入不产生 `turn.started` 或 Provider 请求

自动 append 的 accepted response SHALL 额外包含领域 entry_id；turn.* 的 input_id 仍为启动该 turn 的首条请求 RequestID。自动输入共享 turn 时不重复输出 delta，其余输入通过 input.status 关联。新增 input.status 包含 version=1、type、request_id、entry_id、claimed/committed/returned/uncertain state、适用的 turn_id 和固定安全 reason，不含正文。新命令 response 先于相应状态，初始 turn.started 先于 committed，committed 先于后续请求 delta。retract/stop 接受 disposition 分别为 retracted/stopping，拒绝沿用稳定英文错误。严格输入和一次性输出必须过滤新增内部输入事件，保持原有字段/序列。

#### Scenario: Correlate a batched automatic turn
- **WHEN** 两条 input.append 一起作为初始输入提交
- **THEN** 各自先收到带 entry_id 的 accepted，再以各自 committed 关联同一 turn；turn 事件只使用首条 RequestID，正文不出现在 status

#### Scenario: Retract only requested pending inputs
- **WHEN** input.retract 与 claim 并发
- **THEN** 先被 owner 接受者决定结果；全量取回成功或 input_not_queued，不能部分取回
