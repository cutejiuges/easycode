# provider/stream-lifecycle Specification

## Purpose

定义 Provider 共享流事件从构造、消费到关闭的强类型生命周期，确保非法字段组合无法进入正常路径，并让取消、失败和协议错误都具有唯一且可等待的清理完成边界。

## Requirements

### Requirement: Stream events are sealed typed values

Provider `StreamEvent` SHALL 只允许通过 semantic、completed、failed 和 cancelled 四类强类型构造入口创建，事件字段 MUST 对调用方只读，并提供按 kind 读取 payload 的类型安全访问入口。每个事件 MUST 能通过纯内存验证；零值、未知 kind、nil 必需 payload、kind 与 payload 冲突、多余 payload、nil failure，以及零值、已 finalized 或不满足公共不变量的 prepared sample MUST 被拒绝。

semantic 事件 MUST 携带当前契约允许 Provider 发布的已知语义事件，并通过共享 `protocol.Event` 的纯内存验证。completed MUST 只携带一个尚未 finalized 的有效 prepared sample。failed 和 cancelled MUST 各自只携带一个非 nil 错误。任何构造和只读访问都不得修改 prepared sample 状态或执行其 finalizer。

原生 item SHALL 由 Provider 私有归并状态持有。普通流以有效 prepared sample 承载完成的原生提交，保持原生顺序及 opaque 内容，不向 Runtime 单独发布 item。

#### Scenario: Construct every legal event kind

- **WHEN** 调用方分别以合法语义事件、有效 prepared sample、非 nil failure 和非 nil cancellation error 构造四类事件
- **THEN** 每个构造均成功，事件验证通过，kind 与对应只读 payload 可被准确读取
- **THEN** 不适用于该 kind 的访问入口不会暴露其他 payload

#### Scenario: Reject invalid field combinations

- **WHEN** 验证零值、未知 kind、缺少必需 payload、包含冲突 payload 或 error 为 nil 的终态事件
- **THEN** 验证返回明确错误，事件不得被当作合法 Provider 输出消费

#### Scenario: Reject an invalid semantic event

- **WHEN** semantic 构造接收零值、未知 kind、非法 typed payload 或 Provider 不可发布的生命周期事件
- **THEN** 构造返回错误且不产生合法 stream event

#### Scenario: Reject a consumed prepared sample

- **WHEN** completed 构造接收已经 finalized 或违反 native envelope、usage、finalizer 不变量的 prepared sample
- **THEN** 构造返回错误且不会再次执行 finalizer

### Requirement: A provider stream has exactly one terminal

每次成功建立的 Provider stream MUST 在关闭内部输出队列前发布恰好一个合法 terminal。terminal 只能是 completed、failed 或 cancelled，semantic 只能出现在 terminal 之前。提前结束、多个 terminal 或 terminal 后事件均属于 stream protocol failure。

Provider 生产者 MUST 是内部输出队列的唯一关闭者。调用方从 `StreamEvents` 自然迭代到结束时，Provider transport 消费路径、Conversation active 状态和生产 goroutine MUST 已完成清理。

#### Scenario: Complete a successful stream

- **WHEN** Provider 成功归并完整响应并准备好有效 sample
- **THEN** Provider 在全部 semantic 后发布一次 completed terminal
- **THEN** Provider 完成 transport 与 active 状态清理后关闭内部队列，调用方迭代至结束

#### Scenario: End a failed or cancelled stream

- **WHEN** Provider 请求失败、被取消、发生 idle timeout 或检测到 wire protocol error
- **THEN** Provider 发布恰好一次携带非 nil 错误的 failed 或 cancelled terminal
- **THEN** Provider 最终完成清理，迭代在唯一失败或取消 terminal 后结束

#### Scenario: Detect an invalid terminal sequence

- **WHEN** stream 未发布 terminal 就结束、发布多个 terminal，或在 terminal 后继续发布事件
- **THEN** 消费方将该 stream 视为 protocol failure，而不把任何 terminal 解释为成功

### Requirement: Cancellation has an owned completion signal

Provider stream MUST 观察传入 context 的取消，并沿唯一 owner 路径停止 transport、排空 transport 输出、发布唯一 terminal、释放 Conversation active 状态，最后关闭内部输出队列。调用方必须等待迭代自然结束来确认清理完成。

若 Provider 取消后仍不能完成清理，应用已有的 transport 强制关闭路径 MUST 负责解除阻塞并继续等待同一完成信号。系统 MUST NOT 为单次等待创建失去 owner 的超时 goroutine。

非终态输出的背压 SHALL 可被同一取消解除。若取消先于流终态接纳，owner MUST 停止接纳新的非终态输出、保留已发布事件的顺序，并保证即使消费方暂时停读也能发布一个 cancelled、清理 transport 并关闭队列。若合法终态已接纳，后到取消不得制造第二个流终态。Runtime 仍迭代到自然结束，并按已观察的推理范围和独立 durable 接纳点收口。

#### Scenario: Cancel an active provider stream before terminal

- **WHEN** stream owner 在 terminal 接纳之前观察到调用方取消
- **THEN** Provider 停止发布新的非终态事件，取消并排空 transport，发布一次 cancelled terminal
- **THEN** Conversation active 状态释放后由生产者关闭内部队列，调用方迭代至结束

#### Scenario: Force-close a non-responsive transport

- **WHEN** 上层等待期限到达而 Provider transport 尚未因 context 取消退出
- **THEN** 既有资源 owner 强制关闭 transport 以解除阻塞，并继续等待原迭代结束
- **THEN** 清理职责不会转移给新的后台 waiter，也不会遗留无 owner goroutine

#### Scenario: Cancel while a semantic publish is blocked
- **WHEN** 宿主暂停读取且流的非终态输出已饱和，调用方定向取消
- **THEN** 生产者停止新的非终态发布，清理 transport 后完成流收口，恢复迭代时恰好看到一个 cancelled terminal 并自然结束

### Requirement: Native operations have one conversation admission owner

同一 Conversation 的普通采样、独立摘要、会暂存原生提交的输入、工具结果与 Stop 交付，以及瞬态续写清理 SHALL 经过互斥的操作准入。活动操作与尚未完成的 finalize/discard、传输或续写清理不可向新操作同时交付所有权。冲突调用按对应入口的忙状态失败且无 I/O 或 native mutation。纯只读预检可以读取一致的已提交窗口，不能取得 pending 或改写续写状态。

成功 Sync 后才可完成原生 finalize。上一操作的流清理或已接纳 resolution 清理未完成时 MUST 阻止下一修改操作。自然迭代结束表示流生产者已清理，未收口的 prepared sample 仍须由自己的 resolver 完成。

#### Scenario: Compete for one conversation operation
- **WHEN** sample 与原生提交准备或独立摘要在同一个空闲 Conversation 竞争准入
- **THEN** 只有一个操作被接纳，另一个无传输开启、staging 或 history mutation

#### Scenario: Retain continuation ownership during discard cleanup
- **WHEN** OpenAI 未提交的 sample 正在清理旧连接，调用方尝试下一次采样
- **THEN** 新采样在清理完成前不能开始，清理后的合法下一请求按既有 reset 条件发送一次
