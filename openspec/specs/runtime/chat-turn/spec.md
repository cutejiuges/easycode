# runtime/chat-turn Specification

## Purpose

定义共享 Runtime 执行一次文本 turn 时的生命周期和终态语义，使 TUI 等宿主只消费稳定 RuntimeEvent，并能可靠区分成功、失败、取消和流意外关闭。

## Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在用户 turn 的首次 Provider 请求前产生一次 `turn_started`。对该 turn 的每个 Provider sample，Runtime MUST 创建独立派生 context，逐项验证事件，记录唯一 terminal，并持续消费直到生产者关闭 channel；channel 关闭且此前恰好收到一个合法 completed terminal时，该 sample 才成功。provider channel 关闭本身 MUST NOT 被解释为 sample 或 turn 成功。

成功 sample 含 ready calls 时，Runtime SHALL 在工具处理和原生 outputs durable 后开始下一 sample；成功 sample 不含 calls 时才能产生一次 `turn_completed`。任一 sample 的零值/非法事件、terminal 前关闭、多个 terminal或terminal 后事件 MUST 作为 stream protocol failure，并由同一 owner 取消、排空、等待生产者清理后收口。一个 turn 无论包含多少 samples 都只能产生一个成功或失败终态。

#### Scenario: Complete a successful turn
- **WHEN** 首个 sample 产生文本和唯一合法 completed terminal且不含 tool calls
- **THEN** Runtime 输出一次 `turn_started`、中间文本和一次 `turn_completed`

#### Scenario: Fail when stream closes before terminal
- **WHEN** 任一 Provider channel 在 completed、failed 或 cancelled 之前关闭
- **THEN** Runtime 输出一次 `turn_failed`并返回 `stream_protocol_error`

#### Scenario: Propagate provider failure
- **WHEN** 任一sample产生唯一合法failed terminal并完成清理
- **THEN** Runtime输出一次 `turn_failed`并且不输出 `turn_completed`

#### Scenario: Drain after an invalid stream event
- **WHEN** 任一 sample 发布非法事件、重复 terminal或terminal 后事件
- **THEN** Runtime 立即取消并排空该 sample至 channel关闭，随后只输出一次 `turn_failed`

#### Scenario: Complete a tool turn
- **WHEN** 首个 sample 请求 Read，结果提交后第二个 sample 返回最终文本且不含 calls
- **THEN** Runtime 在两个 Provider channel均完成清理后只发布一个 `turn_completed`

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

Runtime SHALL 将调用 context 的取消传播到每次 Provider stream 的派生 context，并等待该 stream 的输出 channel 关闭。用户取消、消费端协议错误或其他需要提前停止消费的失败 MUST 先请求取消，再由同一消费 owner 同步排空 channel 直到 Provider 清理路径退出；Runtime MUST NOT 通过停止读取遗留阻塞的生产 goroutine。

用户取消 MUST 产生一次 `turn_failed` 或专用 cancellation 语义，但 MUST NOT 同时产生 completed；对外错误 SHALL 可由 `errors.Is(..., context.Canceled)` 或稳定错误码识别。取消与 Provider terminal 竞态时，Runtime MUST 根据已验证的唯一 terminal、调用 context 和首个消费错误得出一个确定结果，并只发布一个 durable terminal。

#### Scenario: Cancel an active turn

- **WHEN** 用户在 provider stream 活跃时取消 turn context
- **THEN** Runtime 停止继续转发新的文本增量，取消派生 stream context并排空 channel
- **THEN** Runtime 在 Provider 清理完成后产生一次可识别的取消结果

#### Scenario: Cancel races with provider completion

- **WHEN** context 取消与 provider completed 几乎同时发生
- **THEN** Runtime 消费到 channel 关闭并只发布一个最终终态
- **THEN** 不会同时发布 `turn_completed` 和 `turn_failed`

#### Scenario: Protocol failure cancels the producer

- **WHEN** Runtime 在消费中发现非法事件且 Provider 正等待继续发送
- **THEN** Runtime 取消派生 context以解除生产者发送或 transport 阻塞，并继续读取直到 channel 关闭
- **THEN** RunTurn 返回前生产 goroutine 已退出，下一 turn 不会收到残留事件

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

Runtime SHALL 在首次 Provider 请求前 durable append 当前 `turn_started`；失败时不得建立网络流。每个 completed sample 的 `provider_native_commit`、`sample_usage` 和全部 ready-call facts MUST 在同一 batch append/Sync 后 finalize，随后才允许 executor I/O。每个 invocation 的 execution-start fact MUST 在 executor 接收前单独 durable。

全部 result facts durable 后，匹配的 Provider-native tool outputs MUST 在下一 Provider sample前 append/Sync并 finalize。最终无 calls sample 的 native commit、sample usage和 `turn_completed` SHALL 在同一 batch durable；携带聚合 usage 的 `turn_completed` RuntimeEvent 只有在 finalizer 成功后才能发布。

Provider/工具失败、取消、协议错误或 append/Sync 失败 MUST 通过合法 result/output配对与唯一 `turn_failed` 收口。任何 durability 状态不确定的 Session MUST poisoned并拒绝新 turn或新副作用，直到重新加载验证。

#### Scenario: Persist before starting a Provider request
- **WHEN** Session 无法 durable append `turn_started`
- **THEN** Runtime 返回 Session 错误且 Provider和Read均不被调用

#### Scenario: Complete a durably committed sample
- **WHEN** 最终sample不含calls且其native commit、usage和completion成功Sync
- **THEN** Conversation finalize该sample，随后Runtime发布一次带聚合usage的完成事件

#### Scenario: Fail while persisting a completed sample
- **WHEN** sample已完整归并但 ready batch append或Sync失败
- **THEN** Runtime 不finalize sample、不调用Read并将当前进程Session poisoned

#### Scenario: Cancel before Provider commit
- **WHEN** 首个sample在成功terminal前取消
- **THEN** Runtime不写入native commit或sample usage，并以唯一失败边界收口

#### Scenario: Persist a call before Read
- **WHEN** Provider完成合法 Read call sample
- **THEN** native commit、usage和ready fact先成功 Sync，execution-start再成功 Sync，之后Read才可接收调用

#### Scenario: Continue only after durable outputs
- **WHEN** Read results已生成但tool-output native commit尚未Sync
- **THEN** Runtime不得建立下一Provider stream

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

成功 `turn_completed` RuntimeEvent SHALL 携带强类型 normalized turn usage；payload 必须遵守统一指标状态和聚合规则，不得包含 Provider wire、raw usage、价格或上下文占用。单 sample turn的payload MUST等于该sample usage；多 sample Tool Loop MUST按durable sample顺序聚合所有usage。

失败或取消终态 MUST NOT 携带伪造的成功 turn usage。已经 durable 保存的 sample usage 即使发生在后续工具或 Provider 失败之前，也不得删除或改写；当前 `turn_failed` 仍不暴露部分 usage。

#### Scenario: Publish current text turn usage
- **WHEN** 单sample文本turn durable完成
- **THEN** `turn_completed` payload与该sample usage完全相同

#### Scenario: Do not publish usage before durability
- **WHEN** 最终完成batch尚未成功Sync
- **THEN** Runtime不发布 `turn_completed`或任何成功聚合usage

#### Scenario: Keep failed terminal shape unchanged
- **WHEN** turn在已有中间sample后失败或取消
- **THEN** Runtime只发布既有强类型 `turn_failed`，同时Session保留已提交sample usage

#### Scenario: Publish aggregated tool-turn usage
- **WHEN** 一个成功Read turn包含三个durable samples
- **THEN** `turn_completed` payload等于按顺序聚合三个sample usage的结果

### Requirement: Runtime revalidates complete prepared sample facts

Runtime SHALL 在构造 Session drafts 前同时复制并重新验证 prepared sample 的 native envelope 与 normalized usage。任何一部分为零值、缺失、已 finalized、复制失败或违反语义不变量时，Runtime MUST 以 stream protocol failure 收口，并不得 append native commit、sample usage 或成功终态。

#### Scenario: Reject invalid normalized usage before append

- **WHEN** completed terminal 的 prepared sample 含非法 metric state 或计数关系
- **THEN** Runtime durable 记录失败边界且不执行 Provider finalizer
- **THEN** journal 中不存在该 sample 的成功记录

### Requirement: Runtime plans context before Provider side effects

Runtime SHALL 在 `turn_started` 已 durable、每次 Provider stream之前，使用同一不可变项目指令与Tool Catalog snapshots、当前已提交Conversation history以及本轮输入状态生成上下文计划。首次sample包含真实用户输入；后续sample只从已提交Provider-native tool outputs继续，不得把result、RuntimeEvent或SemanticHistoryView伪造成新的共享user文本。

规划失败或预算明确 `over_limit` 时，Runtime MUST durable收口当前turn且不得建立该Provider stream。Tool Catalog segment必须参与stable-prefix fingerprint和估算，但catalog、workspace绝对路径、invocation identity、ledger和结果正文不得新增RuntimeEvent或独立prompt文本。

#### Scenario: Stop a confirmed over-limit turn before networking
- **WHEN** tool outputs加入history后下一sample规划明确over-limit
- **THEN** Runtime不发起网络请求并以 `context_limit_exceeded` durable收口turn

#### Scenario: Continue when no window is configured
- **WHEN** 任一sample计划的预算状态为 `not_enforced`
- **THEN** Runtime使用相同snapshots和原生history启动Provider stream

#### Scenario: Continue with an indeterminate estimate
- **WHEN** 已配置窗口但计划因必需估算unknown而为 `indeterminate`
- **THEN** Runtime不把不确定性转换为明确超限并继续既有生命周期

#### Scenario: Close a planning failure durably
- **WHEN** 已durable开始的turn在任一sample规划中失败
- **THEN** Runtime写入唯一失败边界且不建立该Provider stream

#### Scenario: Keep public event and journal vocabularies unchanged
- **WHEN** Tool Catalog规划成功或失败
- **THEN** Runtime不为ContextPlan本身新增RuntimeEvent或Session record，并且不持久化prompt正文

#### Scenario: Plan the first tool sample
- **WHEN** 一个新turn开始
- **THEN** 计划按固定顺序包含Provider profile、Tool Catalog、可选项目指令、历史和当前输入

#### Scenario: Plan after tool outputs
- **WHEN** Read outputs已durable并finalize到原生history
- **THEN** 下一sample从更新后的Provider history编译，且不把同一tool result再作为共享user输入附加
