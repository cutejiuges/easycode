# provider/stream-lifecycle Specification

## Purpose

定义 Provider 共享流事件从构造、消费到关闭的强类型生命周期，确保非法字段组合无法进入正常路径，并让取消、失败和协议错误都具有唯一且可等待的清理完成边界。

## Requirements

### Requirement: Stream events are sealed typed values

Provider `StreamEvent` SHALL 只允许通过 semantic、native item、completed、failed 和 cancelled 五类强类型构造入口创建，事件字段 MUST 对调用方只读，并提供按 kind 读取 payload 的类型安全访问入口。每个事件 MUST 能通过纯内存验证；零值、未知 kind、nil 必需 payload、kind 与 payload 冲突、多余 payload、nil failure、非法 native item，以及零值、已 finalized 或不满足公共不变量的 prepared sample MUST 被拒绝。

semantic 事件 MUST 携带当前协议版本下 Provider 可发布的已知语义事件，并通过共享 `protocol.Event` 验证；该验证不得执行 I/O。completed MUST 只携带一个尚未 finalized 的有效 prepared sample；failed 和 cancelled MUST 各自只携带一个非 nil 错误。任何构造和只读访问都不得修改 prepared sample 状态或执行其 finalizer。

#### Scenario: Construct every legal event kind

- **WHEN** 调用方分别以合法语义事件、合法 native item、有效 prepared sample、非 nil failure 和非 nil cancellation error 构造五类事件
- **THEN** 每个构造均成功，事件验证通过，kind 与对应只读 payload 可被准确读取
- **THEN** 不适用于该 kind 的访问入口不会暴露其他 payload

#### Scenario: Reject invalid field combinations

- **WHEN** 验证零值、未知 kind、缺少必需 payload、包含冲突 payload 或 error 为 nil 的终态事件
- **THEN** 验证返回明确错误，事件不得被当作合法 Provider 输出消费

#### Scenario: Reject an invalid semantic event

- **WHEN** semantic 构造接收零值、未知版本、未知 kind、非法 typed payload 或 Provider 不可发布的生命周期事件
- **THEN** 构造返回错误且不产生合法 stream event

#### Scenario: Reject a consumed prepared sample

- **WHEN** completed 构造接收已经 finalized 或违反 native envelope、usage、finalizer 不变量的 prepared sample
- **THEN** 构造返回错误且不会再次执行 finalizer

### Requirement: A provider stream has exactly one terminal

每次成功建立的 Provider stream MUST 在关闭输出 channel 前发布恰好一个合法 terminal。terminal 只能是 completed、failed 或 cancelled；semantic 与 native item 只能出现在 terminal 之前。Provider MUST NOT 在 terminal 后发布任何事件，且 channel 提前关闭、多个 terminal 或 terminal 后事件均属于 stream protocol failure。

Provider 生产者 MUST 是输出 channel 的唯一关闭者。调用方观察到 channel 关闭时，Provider transport 消费路径、Conversation active 状态和该 stream 的生产 goroutine MUST 已完成清理；调用方不得代替生产者关闭 channel。

#### Scenario: Complete a successful stream

- **WHEN** Provider 成功归并完整响应并准备好有效 sample
- **THEN** Provider 在全部 semantic 和 native item 后发布一次 completed terminal
- **THEN** Provider 完成 transport 与 active 状态清理后关闭输出 channel，且不再发布事件

#### Scenario: End a failed or cancelled stream

- **WHEN** Provider 请求失败、被取消、发生 idle timeout 或检测到 wire protocol error
- **THEN** Provider 发布恰好一次携带非 nil 错误的 failed 或 cancelled terminal
- **THEN** Provider 不发布 completed，并最终完成清理和关闭输出 channel

#### Scenario: Detect an invalid terminal sequence

- **WHEN** stream 未发布 terminal 就关闭、发布多个 terminal，或在 terminal 后继续发布事件
- **THEN** 消费方将该 stream 视为 protocol failure，而不把任何 terminal 解释为成功

### Requirement: Cancellation has an owned completion signal

Provider stream MUST 观察传入 context 的取消，并沿唯一 owner 路径停止 transport、排空 transport 输出、发布唯一 terminal、释放 Conversation active 状态，最后关闭输出 channel。取消请求本身 MUST NOT 被当作清理已完成；输出 channel 关闭才是该生产者生命周期的完成信号。

若 Provider 未遵守取消后最终关闭 channel 的契约，应用已有的 transport 强制关闭路径 MUST 负责解除阻塞并继续等待同一完成信号；系统 MUST NOT 为单次等待创建失去 owner 的超时 goroutine。

#### Scenario: Cancel an active provider stream before terminal

- **WHEN** stream context 在生产者仍活跃且尚未发布 terminal 时被调用方取消
- **THEN** Provider 停止发布新的非终态事件，取消并排空 transport，发布一次 cancelled terminal
- **THEN** Conversation active 状态释放后由生产者关闭输出 channel

#### Scenario: Force-close a non-responsive transport

- **WHEN** 上层等待期限到达而 Provider transport 尚未因 context 取消退出
- **THEN** 既有资源 owner 强制关闭 transport 以解除阻塞，并继续等待原输出 channel 关闭
- **THEN** 清理职责不会转移给新的后台 waiter，也不会遗留无 owner goroutine
