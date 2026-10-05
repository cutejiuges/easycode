## MODIFIED Requirements

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
