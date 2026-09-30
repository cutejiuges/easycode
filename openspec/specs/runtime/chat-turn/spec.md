# runtime/chat-turn Specification

## Purpose

定义共享 Runtime 执行一次文本 turn 时的生命周期和终态语义，使 TUI 等宿主只消费稳定 RuntimeEvent，并能可靠区分成功、失败、取消和流意外关闭。

## Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在请求 provider 前产生一次 `turn_started`。Runtime MUST 仅在收到 provider 的显式 completed 终态后产生一次 `turn_completed`；provider channel 关闭本身 MUST NOT 被解释为成功。

#### Scenario: Complete a successful turn

- **WHEN** provider 产生文本事件并最终产生 completed 终态
- **THEN** Runtime 按顺序输出 `turn_started`、中间语义事件和一次 `turn_completed`

#### Scenario: Fail when stream closes before terminal

- **WHEN** provider channel 在 completed 或 failed 终态之前关闭
- **THEN** Runtime 输出一次 `turn_failed`
- **THEN** RunTurn 返回 `stream_protocol_error`

#### Scenario: Propagate provider failure

- **WHEN** provider 产生 failed 终态
- **THEN** Runtime 输出一次 `turn_failed` 并返回对应错误
- **THEN** Runtime 不再输出 `turn_completed`

### Requirement: Assistant text uses a typed semantic payload

RuntimeEvent SHALL 为 assistant 文本增量定义稳定的强类型 payload，至少包含本次追加的文本。宿主 MUST NOT 解析 OpenAI Responses event 或 provider-native item 才能显示文本。

#### Scenario: Project provider text delta

- **WHEN** provider 产生 assistant 文本增量 `hello`
- **THEN** Runtime 输出 `assistant_text_delta`，其 typed payload 的 text 为 `hello`
- **THEN** payload 不包含 OpenAI wire event 对象

#### Scenario: Reject invalid semantic payload construction

- **WHEN** Runtime 尝试构造缺少必需文本字段的 assistant 文本增量
- **THEN** 系统返回明确错误而不是产生不可消费的 RuntimeEvent

### Requirement: Cancellation has a single observable outcome

Runtime SHALL 将调用 context 的取消传播到 provider stream，并等待该 stream 的清理路径退出。用户取消 MUST 产生一次 `turn_failed` 或专用 cancellation 语义，但 MUST NOT 同时产生 completed；对外错误 SHALL 可由 `errors.Is(..., context.Canceled)` 或稳定错误码识别。

#### Scenario: Cancel an active turn

- **WHEN** 用户在 provider stream 活跃时取消 turn context
- **THEN** Runtime 停止继续转发新的文本增量
- **THEN** Runtime 产生一次可识别的取消结果并等待 provider 清理完成

#### Scenario: Cancel races with provider completion

- **WHEN** context 取消与 provider completed 几乎同时发生
- **THEN** Runtime 只发布一个最终终态
- **THEN** 不会同时发布 `turn_completed` 和 `turn_failed`

### Requirement: Only one active turn mutates a conversation

同一个会话级 Runtime SHALL 在任意时刻最多允许一个活动 turn 修改 provider 原生历史。并发提交 MUST 被拒绝或留在宿主层等待，不能并行写入同一 conversation history。

#### Scenario: Submit while a turn is active

- **WHEN** 一个 turn 尚未结束时再次提交输入
- **THEN** 第二次提交不会启动并发 provider stream
- **THEN** 已运行 turn 的原生历史顺序保持不变

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

### Requirement: Host submission carries an explicit operation context

所有可能启动 Provider stream 或等待异步清理的宿主提交操作 SHALL 接收显式 `context`，并将其取消传播到当前 turn。nil context MUST 返回稳定英文错误，不得替换为 background context；context 不得保存到长期 Runtime、Session facade 或 UI 状态结构中。

#### Scenario: Reject a nil submission context

- **WHEN** headless、TUI adapter 或其他宿主使用 nil context 提交 turn
- **THEN** Session 在创建 goroutine、写入 `turn_started` 或发起 Provider 请求前返回明确错误

#### Scenario: Cancel through the submitted context

- **WHEN** 宿主取消已接受 turn 的提交 context
- **THEN** 相同取消信号到达 Provider stream，Runtime durable 收口唯一失败终态并完成清理
- **THEN** Session 不遗留活动 goroutine 或可继续发送事件的 channel owner

#### Scenario: Application shutdown retains cleanup ownership

- **WHEN** 优雅 shutdown 的等待期限到达而 Provider stream 仍阻塞
- **THEN** 唯一资源 owner 强制关闭 transport 以解除阻塞，并继续等待既有清理完成信号
- **THEN** 超时返回不会使 writer、lease、stream 或 goroutine 失去 cleanup owner

### Requirement: Prepared completion is valid before durable persistence

Runtime SHALL 在写入 `provider_native_commit` 和 `turn_completed` 前验证 completed terminal 携带尚未 finalize、结构完整且可独立复制的 prepared sample。零值、已 finalize、family/wire/revision/payload 非法或复制失败的 sample MUST 作为 stream protocol failure 收口，且不得写入 native commit 或成功终态。

#### Scenario: Reject a zero-value prepared sample

- **WHEN** Provider 产生 completed terminal 但 prepared sample 为零值或无法返回有效 envelope
- **THEN** Runtime durable 记录 `turn_failed`，不记录 `provider_native_commit` 或 `turn_completed`
- **THEN** Conversation history 不执行 finalizer

#### Scenario: Revalidate before durable append

- **WHEN** Provider 提供的 prepared sample 在创建后不满足公共 native envelope 不变量
- **THEN** Runtime 在 Session append 前拒绝该 sample
- **THEN** journal 中不存在代表该 sample 成功的事实
