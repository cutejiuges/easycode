## ADDED Requirements

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
