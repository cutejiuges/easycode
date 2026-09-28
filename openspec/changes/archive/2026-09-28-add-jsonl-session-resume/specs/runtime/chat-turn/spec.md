## ADDED Requirements

### Requirement: Runtime events carry stable Session identity

Session-bound Runtime SHALL 在接受 turn 前获得有效的 `session_id` 与 `thread_id`，并为每次提交分配新的 `turn_id`。该 turn 产生的 `turn_started`、assistant 语义事件和唯一终态事件 MUST 携带相同的 session/thread/turn 标识；Provider 产生的语义 payload 不得自行决定或覆盖这些标识。Runtime MUST NOT 接受包含未收口 interrupted-tail turn 的恢复计划；应用必须先 durable 记录对应失败边界。

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

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在发起 Provider 请求前 durable append 当前 `turn_started` 事实；该写入失败时 MUST NOT 建立网络流。Provider 收到显式成功 terminal 后，完整 sample commit MUST 先通过 Session batch append/Sync，再进入 Conversation 已提交 native history；`turn_completed` 只有在对应 lifecycle boundary 已 durable 后才能发布。

Provider 失败、取消、timeout 或提前 EOF SHALL 丢弃 sample staging，并记录不进入 native history 的失败边界。任何 append/Sync 错误 MUST 产生一次 `turn_failed` 且不得同时产生 `turn_completed`；durability 状态不确定的 Session MUST 拒绝继续提交新 turn，直到进程重新加载并校验 journal。

#### Scenario: Persist before starting a Provider request

- **WHEN** Session 无法 durable append `turn_started`
- **THEN** Runtime 返回 session 错误且 Provider 不收到请求

#### Scenario: Complete a durably committed sample

- **WHEN** Provider 产生有效 completed terminal 且 Session 成功 Sync 原生 commit 与完成边界
- **THEN** Conversation 提交相同 native sample，随后 Runtime 产生一次 `turn_completed`

#### Scenario: Fail while persisting a completed sample

- **WHEN** Provider sample 已完整归并但 Session append 或 Sync 失败
- **THEN** Runtime 产生一次 session 类 `turn_failed`，不产生 `turn_completed`，且当前进程拒绝下一 turn
- **THEN** Provider staging 不被当作当前进程中可继续使用的已提交历史

#### Scenario: Cancel before Provider commit

- **WHEN** turn 在 Provider 成功 terminal 前取消
- **THEN** Runtime durable 记录失败边界，但不写入 `provider_native_commit`
- **THEN** 下一次请求不包含该 turn 的用户输入或部分 assistant 输出
