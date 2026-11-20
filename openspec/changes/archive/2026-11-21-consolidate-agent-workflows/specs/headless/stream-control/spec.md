## MODIFIED Requirements

### Requirement: Every command receives one ordered control response

每个成功解码的入站命令 SHALL 产生恰好一个 `control.response`，其中包含 `version: 1`、相同 `request_id`、命令 `type`、`status` 以及与结果匹配的强类型字段。submit 被接受时 status SHALL 为 `accepted` 且 disposition 为 `starting` 或 `queued`；interrupt 被接受时 disposition SHALL 为 `interrupting`；shutdown 被接受时 disposition SHALL 为 `closing`。拒绝响应 SHALL 使用 `status: rejected` 和稳定安全的英文 error code/message。

`control.response` 只确认命令 admission 或拒绝，不得伪装成 durable turn start 或 terminal。对严格 input.submit 的 `starting`/`queued` submit，后续对应的全部 `turn.*` 和 `assistant.text.delta` 事件 SHALL 携带相同 `input_id`；只有 `turn.started` 才表示对应 `turn_started` 已 durable。显式 shutdown 丢弃已接受但未启动的输入时，系统 SHALL 为每个严格输入输出一次携带其 `input_id` 和 `session_shutdown` error 的 `input.discarded`；自动输入使用 input.status returned。

外部 v1 的命令类型与接受后的 disposition MUST 对应同一条已验证的内部命令契约。例如 `context.query` 成功只允许 `queried`。不匹配的类型、状态、disposition 或关联身份在写帧前失败关闭。

已解码 `input.submit` 的可预期正文或附件准备失败 SHALL 经同一命令 owner 产生一个关联的 `rejected/invalid_input`。同一请求身份继续应用当前去重和关闭优先级。拒绝不得入队、分配 input ID、产生 Provider 请求或终止后续流控。解码错误和 transport 或 owner 失败沿对应终态收口。

#### Scenario: Reject one input and keep the command stream open

- **WHEN** 合法 input.submit 引用未知 Skill，之后收到合法 cache.query 或普通输入
- **THEN** 前者收到同 request ID 的一次拒绝，后者按顺序取得正常响应，拒绝不产生 turn 或泄漏用户正文

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

上下文 query 被接受时 disposition SHALL 为 queried，随后输出一次携带相同 request_id 的 context.snapshot；compact 为 compacting 并携带 compaction_id；cancel 为 cancelling。拒绝使用 session_busy、nothing_to_compact、compaction_budget_unknown、compaction_budget_exceeded、no_active_compaction、compaction_mismatch 等匹配的安全英文代码。压缩 admission 不等于 durable started，取消 admission 不等于操作已结束。

#### Scenario: Correlate compaction independently of turns
- **WHEN** context.compact 被接受
- **THEN** response 先包含 compaction_id，后续操作事件使用发起 request_id 和同一 compaction_id，不携带 input_id/turn_id

#### Scenario: Cancel an old compaction
- **WHEN** expected_compaction_id 不匹配当前活动压缩
- **THEN** 一次 rejected response，不取消当前 worker 或后续 turn

#### Scenario: Reject a response with the wrong disposition before encoding
- **WHEN** 输出侧收到 context.query 的 accepted 响应但 disposition 为 updated
- **THEN** 拒绝该帧且无部分输出，合法 query 响应保持 v1 的字段和顺序

### Requirement: Protocol and output failures have deterministic cleanup

无法解码的非空输入行 SHALL 在 stdout 可用时产生一个终止性 `error`，停止 admission，按显式 shutdown 语义取消活动 turn、终止尚未开始输入并等待 cleanup，然后以退出码 `1` 结束。stdout 序列化错误、短写或断管 SHALL 停止继续写该流，请求 shutdown 并保持 cleanup ownership，最终以退出码 `1` 结束；不得尝试向已损坏 stdout 追加错误 JSON。

流式会话在 EOF drain 或显式 shutdown 下完成全部协议和资源清理时 SHALL 返回退出码 `0`；单个 turn 的 `turn.failed` 或命令级 rejected 已通过机器事件报告时，不得单独把健康控制进程变成基础设施失败。CLI 用法错误仍为退出码 `2`。所有对外错误必须是稳定、安全的英文 code/message，不得包含输入正文、底层 cause、secret、Provider 正文或 Session opaque payload。

输出 transport owner SHALL 持续拥有底层 stdout 直到终态帧完成或确定无法交付。最后一帧被背压阻塞期间发生取消或强制关闭，MUST 解除该写入，等待同一 writer、Runtime 和 Session 完成，沿用输出不可用失败分类。在可用 stdout 上仍发布单个安全终止性错误。

#### Scenario: Fail a malformed command safely

- **WHEN** reader 收到一个无法严格解码的非空 JSON 行
- **THEN** stdout 可用时输出一个安全终止性 `error`，不回显原始行
- **THEN** 应用取消并清理活动工作后以退出码 `1` 结束

#### Scenario: Clean shutdown returns success despite a reported turn failure

- **WHEN** 一个 turn 已通过 `turn.failed` 完整报告，随后宿主发送 shutdown 且清理成功
- **THEN** 进程以退出码 `0` 结束，因为该 turn 结果已由流式协议表达且控制基础设施保持健康

#### Scenario: Broken pipe retains cleanup ownership

- **WHEN** stdout 在活动 turn 中发生短写或断管
- **THEN** 应用不再写 stdout，请求取消并等待 Runtime、Session writer 和 stream cleanup
- **THEN** 进程以退出码 `1` 结束且不把部分输出提交为成功历史

#### Scenario: Cancel while writing the final error frame
- **WHEN** 未读取的 stdout 确定性阻塞最终 error 帧，宿主随后取消
- **THEN** 同一个 transport owner 解除写入并完成清理，进程退出且 journal lease 可重新获取，不再尝试第二个终态帧
