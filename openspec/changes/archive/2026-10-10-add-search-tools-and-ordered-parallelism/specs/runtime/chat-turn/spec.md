## MODIFIED Requirements

### Requirement: Runtime events carry stable Session identity

Session-bound Runtime SHALL 在接受 turn 前获得有效的 `session_id` 与 `thread_id`，并为每次提交分配新的 `turn_id`。该 turn 产生的 `turn_started`、assistant 语义事件和唯一终态事件 MUST 携带相同的 session/thread/turn 标识；Provider 产生的语义 payload 不得自行决定或覆盖这些标识。进程内 RuntimeEvent SHALL 使用唯一当前强类型 kind/payload，不携带 format version，也不提供 JSON decoder；headless adapter 负责把它单向投影为外部 JSONL v1。Runtime MUST NOT 接受包含未收口 interrupted-tail turn 的恢复计划；应用必须先 durable 记录对应失败边界。

#### Scenario: Decorate a successful turn
- **WHEN** Runtime 在一个 root thread 中执行成功文本 turn
- **THEN** 从 `turn_started` 到 `turn_completed` 的全部事件携带相同 session/thread/turn ID
- **THEN** 下一 turn 保留 session/thread ID 并获得不同的 turn ID

#### Scenario: Decorate a failed turn
- **WHEN** Provider 请求失败、取消、timeout 或协议错误结束 turn
- **THEN** `turn_failed` 与此前事件携带相同 session/thread/turn ID

#### Scenario: Reject an unresolved resumed turn
- **WHEN** 应用尝试用仍包含 interrupted-tail 活动 turn 的恢复计划构造或调用 Runtime
- **THEN** Runtime 在启动 Provider 或分配新 turn ID 前拒绝该状态

#### Scenario: Project an external version at the adapter
- **WHEN** headless adapter 接收一个已验证的当前进程内 RuntimeEvent
- **THEN** adapter 生成带 `version: 1` 的外部 JSONL 对象，而内部 event 保持无版本强类型值

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在首次 Provider 请求前 durable append 当前 `turn_started`；失败时不得建立网络流。每个 completed sample 的 `provider_native_commit`、`sample_usage` 和全部 ready-call facts MUST 在同一 batch append/Sync 后 finalize，随后才允许任何 executor I/O。

Runtime SHALL 按 call index 为可执行调用逐一 durable `tool_execution_started`，所有准入完成后才可启动最大 8 个 `parallel_read` workers。worker 只返回其固定 call slot 的 typed result，不得写 Session 或 Provider history；协调 owner 等待全部已 started workers 结束，并按 call index durable result facts。全部 results durable 后，匹配的 Provider-native tool outputs MUST 在下一 Provider sample 前 append/Sync 并 finalize。最终无 calls sample 的 native commit、sample usage 和 `turn_completed` SHALL 在同一 batch durable；携带聚合 usage 的 `turn_completed` RuntimeEvent 只有在 finalizer 成功后才能发布。

Provider/工具失败、取消、协议错误或 append/Sync 失败 MUST 通过合法 result/output 配对与唯一 `turn_failed` 收口。任何 durability 状态不确定的 Session MUST poisoned 并拒绝新 turn 或新副作用，直到重新加载验证。若后续 started admission 失败，更早已经 accepted 的 invocation 仍由 Runtime 拥有并恰好执行一次；未 accepted 的 invocation 不得执行。

#### Scenario: Persist before starting a Provider request
- **WHEN** Session 无法 durable append `turn_started`
- **THEN** Runtime 返回 Session 错误且 Provider、Read、Glob、Grep 均不被调用

#### Scenario: Complete a durably committed sample
- **WHEN** 最终 sample 不含 calls且其 native commit、usage和completion成功 Sync
- **THEN** Conversation finalize 该 sample，随后 Runtime 发布一次带聚合 usage 的完成事件

#### Scenario: Fail while persisting a completed sample
- **WHEN** sample 已完整归并但 ready batch append或Sync失败
- **THEN** Runtime 不 finalize sample、不调用任何 tool executor 并将当前进程 Session poisoned

#### Scenario: Cancel before Provider commit
- **WHEN** 首个 sample 在成功 terminal 前取消
- **THEN** Runtime 不写入 native commit 或 sample usage，并以唯一失败边界收口

#### Scenario: Persist a call before Read
- **WHEN** Provider 完成包含合法 Read call 的 sample
- **THEN** native commit、usage、ready 与 execution-start 依次 durable 后，Read 才可接收调用

#### Scenario: Persist all admissions before parallel I/O
- **WHEN** Provider 完成含 Glob、Grep、Read 的合法 sample
- **THEN** native commit、usage和全部 ready facts先在一个batch成功 Sync
- **THEN** started facts再按call index逐个成功Sync，之后workers才可并行接收调用

#### Scenario: Persist results in model order
- **WHEN** 三个 workers 以与 call index 不同的顺序完成
- **THEN** Runtime 按 call index 写入 results 并构造 outputs，Session writer只由协调 owner 调用

#### Scenario: Continue only after durable outputs
- **WHEN** tool results 已生成但tool-output native commit尚未Sync
- **THEN** Runtime 不得建立下一 Provider stream

#### Scenario: Retain ownership after partial admission failure
- **WHEN** 一个早期 started 已 accepted 而后续 started append 返回失败
- **THEN** Runtime 恰好执行早期 invocation、禁止后续未 accepted invocation并等待所有已启动 workers 结束
- **THEN** Session 进入失败或 poisoned 状态且不发起下一 Provider sample
