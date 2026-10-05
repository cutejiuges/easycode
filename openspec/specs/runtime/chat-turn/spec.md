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

Runtime SHALL 在发起 Provider 请求前 durable append 当前 `turn_started` 事实；该写入失败时 MUST NOT 建立网络流。Provider 收到显式成功 terminal 后，对应 `provider_native_commit`、`sample_usage` 和 turn 完成边界 MUST 按规定顺序通过同一 Session batch append/Sync，再进入 Conversation 已提交 native history；携带 turn usage 的 `turn_completed` RuntimeEvent 只有在该 lifecycle boundary 已 durable 且 Provider finalizer 成功后才能发布。

Provider 失败、取消、timeout、提前 EOF 或 completed sample usage 非法 SHALL 丢弃 sample staging，并记录不进入 native history的失败边界。任何 append/Sync 错误 MUST 产生一次 `turn_failed` 且不得同时产生 `turn_completed`；durability 状态不确定的 Session MUST 拒绝继续提交新 turn，直到进程重新加载并校验 journal。

#### Scenario: Persist before starting a Provider request

- **WHEN** Session 无法 durable append `turn_started`
- **THEN** Runtime 返回 session 错误且 Provider 不收到请求

#### Scenario: Complete a durably committed sample

- **WHEN** Provider 产生有效 completed terminal 且 Session 成功 Sync 原生 commit、sample usage 与完成边界
- **THEN** Conversation 提交相同 native sample，随后 Runtime 产生一次携带相同 turn usage 的 `turn_completed`

#### Scenario: Fail while persisting a completed sample

- **WHEN** Provider sample 已完整归并但 Session append 或 Sync 失败
- **THEN** Runtime 产生一次 session 类 `turn_failed`，不产生 `turn_completed`，且当前进程拒绝下一 turn
- **THEN** Provider staging 不被当作当前进程中可继续使用的已提交历史，sample usage 也不得单独发布

#### Scenario: Cancel before Provider commit

- **WHEN** turn 在 Provider 成功 terminal 前取消
- **THEN** Runtime durable 记录失败边界，但不写入 `provider_native_commit` 或 `sample_usage`
- **THEN** 下一次请求不包含该 turn 的用户输入或部分 assistant 输出

### Requirement: Host submission carries an explicit operation context

所有直接执行单个 turn 或等待异步清理的操作 SHALL 接收显式 `context`，并将其取消传播到当前 turn。nil context MUST 返回稳定英文错误，不得替换为 background context；context 不得保存到长期 Runtime、Session facade、输入队列或 UI 状态结构中。

长期输入控制器 SHALL 由显式生命周期 context 拥有，使用该 context 或其子 context 驱动每个已开始 turn 和最终清理。命令提交的 context 只控制 admission 等待：在 owner 接受前取消 MUST 无副作用；一旦命令被接受，调用方 context 的后续取消不得隐式撤回已排队或已开始的输入，宿主必须使用定向 interrupt 或 shutdown。owner MUST NOT 将命令调用方 context 保存进 follow-up queue。

#### Scenario: Reject a nil submission context

- **WHEN** headless、TUI adapter 或其他宿主使用 nil context 直接提交 turn
- **THEN** Session 在创建 goroutine、写入 `turn_started` 或发起 Provider 请求前返回明确错误

#### Scenario: Cancel through the submitted context

- **WHEN** 宿主取消已接受的直接单 turn 提交 context
- **THEN** 相同取消信号到达 Provider stream，Runtime durable 收口唯一失败终态并完成清理
- **THEN** Session 不遗留活动 goroutine 或可继续发送事件的 channel owner

#### Scenario: Cancel a long-lived command before admission

- **WHEN** 长期控制器的命令提交 context 在 owner 接受前取消
- **THEN** 命令无副作用失败，且 context 不进入 follow-up queue

#### Scenario: Caller cancellation does not retract accepted input

- **WHEN** 长期控制器已接受输入后，提交调用方取消其 admission context
- **THEN** owner 继续拥有该输入，调用方取消不等价于 interrupt
- **THEN** 后续 turn 使用控制器生命周期的子 context，而不是保存的调用方 context

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

### Requirement: Turn completion exposes typed aggregated usage

成功 `turn_completed` RuntimeEvent SHALL 携带强类型的 normalized turn usage；payload 必须遵守统一指标状态和聚合规则，不得包含 Provider wire、raw usage、价格或上下文占用。当前单 sample 文本 turn 的 payload MUST 等于该 durable `sample_usage`；未来同一 turn 的多个 sample MUST 按 sample 顺序聚合后发布。

失败或取消终态 MUST NOT 携带伪造的成功 turn usage。已经 durable 保存的 sample usage 即使发生在未来 turn 的后续工具失败之前，也不得被删除或改写；失败事件是否展示部分 turn usage必须由后续 change 单独定义，本变更不在 `turn_failed` 中暴露它。

#### Scenario: Publish current text turn usage

- **WHEN** 当前单 sample 文本 turn durable 完成
- **THEN** `turn_completed` payload 包含与已提交 `sample_usage` 相同的 normalized usage

#### Scenario: Do not publish usage before durability

- **WHEN** Provider 已完成但包含 usage 的 Session batch 尚未成功 Sync
- **THEN** Runtime 不发布 `turn_completed` 或任何成功 usage 投影

#### Scenario: Keep failed terminal shape unchanged

- **WHEN** 当前 turn 在成功 sample durable 提交前失败或取消
- **THEN** Runtime 只发布既有强类型 `turn_failed`，不附加全零或估算 usage

### Requirement: Runtime revalidates complete prepared sample facts

Runtime SHALL 在构造 Session drafts 前同时复制并重新验证 prepared sample 的 native envelope 与 normalized usage。任何一部分为零值、缺失、已 finalized、复制失败或违反语义不变量时，Runtime MUST 以 stream protocol failure 收口，并不得 append native commit、sample usage 或成功终态。

#### Scenario: Reject invalid normalized usage before append

- **WHEN** completed terminal 的 prepared sample 含非法 metric state 或计数关系
- **THEN** Runtime durable 记录失败边界且不执行 Provider finalizer
- **THEN** journal 中不存在该 sample 的成功记录

### Requirement: Runtime plans context before Provider side effects

Runtime SHALL 在当前 `turn_started` 已 durable append 并发布、但调用 Provider stream 之前，以当前 Conversation 已提交历史和本轮输入生成上下文计划。规划失败或预算状态为明确 `over_limit` 时，Runtime MUST durable append 当前 turn 的唯一失败边界并发布既有 `turn_failed`，且 MUST NOT 建立 Provider stream 或修改 Provider native history。

明确超限 SHALL 返回稳定英文错误码 `context_limit_exceeded`，错误消息只可包含预算数值和估算状态，不得包含用户输入、历史正文、opaque Provider data、API key 或其他 secret。`not_enforced`、`within_limit` 和 `indeterminate` 预算状态 SHALL 允许既有 Provider 生命周期继续；本变更不得为计划新增 RuntimeEvent kind、Session record 或 headless JSONL 字段。

#### Scenario: Stop a confirmed over-limit turn before networking

- **WHEN** `turn_started` 已 durable 且上下文计划得到 `over_limit`
- **THEN** Runtime durable 记录一次失败边界并发布一次携带 `context_limit_exceeded` 的 `turn_failed`
- **THEN** Provider 不收到 Stream 调用且 Conversation native history 保持不变

#### Scenario: Continue when no window is configured

- **WHEN** 计划完整生成但预算状态为 `not_enforced`
- **THEN** Runtime 使用既有本轮输入启动 Provider stream
- **THEN** 请求继续由 Provider 的原生历史编译，而不是由上下文计划或语义视图重建

#### Scenario: Continue with an indeterminate estimate

- **WHEN** 用户配置了窗口但计划因为必需估算 unknown 而标识 `indeterminate`
- **THEN** Runtime 不把不确定性转换为 `context_limit_exceeded`
- **THEN** Provider 生命周期按既有规则继续

#### Scenario: Close a planning failure durably

- **WHEN** 已 durable 开始的 turn 因非法 footprint、family 不匹配或计划构造错误而失败
- **THEN** Runtime durable 记录唯一 `turn_failed` 边界且不建立 Provider stream
- **THEN** Session append 失败仍遵守既有 poisoned journal 语义

#### Scenario: Keep public event and journal vocabularies unchanged

- **WHEN** 任一计划结果完成或失败
- **THEN** Runtime 只使用既有 `turn_started`、`turn_failed` 及正常 Provider 生命周期事件
- **THEN** journal 不写入 context plan、prompt 正文或新的 record kind
