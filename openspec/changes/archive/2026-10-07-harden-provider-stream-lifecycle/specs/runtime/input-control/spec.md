## MODIFIED Requirements

### Requirement: Durable terminal precedes the next turn

owner SHALL 复用既有 Runtime 单 turn durable 生命周期。当前 turn 只有在 Provider 输出 channel 已关闭、生产者清理路径退出，并且 Runtime 发布唯一 `turn_completed` 或 `turn_failed` 后，owner 才能从 follow-up queue 启动下一输入；下一次 `turn_started` MUST 晚于前一 turn 的 durable terminal 和 stream cleanup completion。单个 turn 失败只影响该输入；如果 Runtime 和 Session 仍可继续，owner SHALL 继续处理 FIFO 中的后续输入，poisoned 或 durability 不确定状态则 MUST 拒绝全部尚未启动输入并停止 Provider 副作用。

#### Scenario: Start a follow-up after durable completion

- **WHEN** 活动 turn 成功完成、Provider stream 已关闭且队列中存在 follow-up
- **THEN** 前一 `turn_completed` 已 durable、按序发布且生产者清理完成后，owner 才为 follow-up 分配并 durable 写入新的 `turn_started`

#### Scenario: Continue after an isolated turn failure

- **WHEN** 当前 turn 以普通 Provider failure durable 收口、stream 清理完成且 Session 未 poisoned
- **THEN** owner 保留该失败终态，并继续启动 FIFO 中的下一输入

#### Scenario: Stop after an uncertain durable failure

- **WHEN** 当前 turn 的 append 或 Sync 失败使 Session 进入 poisoned 状态
- **THEN** owner 不再启动 Provider stream，并以稳定 session 错误拒绝全部尚未启动输入

#### Scenario: Isolate a protocol failure from the next turn

- **WHEN** 当前 turn 因非法 stream event 取消 Provider 并排空输出 channel
- **THEN** owner 在生产者退出前不启动排队输入
- **THEN** 前一 stream 的 goroutine、事件和 active 状态不会进入或阻塞下一 turn

### Requirement: Graceful input close and explicit shutdown are distinct

输入关闭 SHALL 停止新的 admission，但允许活动 turn 和所有已接受 follow-up 按 FIFO 完成；队列排空、最后一个 Provider 输出 channel 关闭且对应 turn 清理结束后，owner SHALL 发布最终完成信号。显式 `shutdown` SHALL 原子停止 admission、请求取消匹配的活动 turn，并以稳定 `session_shutdown` 结果终止所有尚未开始的排队输入；它 MUST 等待活动 turn 的 durable terminal、Provider channel 关闭、Runtime 清理和 owner 最终完成信号后才确认关闭完成。

shutdown 等待 context 到期可以使调用方停止等待，但 MUST NOT 放弃资源所有权、关闭仍可能被发送的 channel 或留下无 owner goroutine。关闭开始后的新命令 SHALL 以稳定 `session_closing` 拒绝，重复 shutdown MUST 幂等地观察同一最终完成结果。

#### Scenario: Drain accepted work after input closes

- **WHEN** 输入源 EOF 时一个 turn 活跃且队列仍有两个输入
- **THEN** owner 拒绝后续新 admission，但按顺序完成活动 turn 和两个 follow-up
- **THEN** 最终完成信号只在最后一个 Provider channel 关闭且 turn 清理结束后关闭

#### Scenario: Shutdown an active session

- **WHEN** 显式 shutdown 在一个 turn 活跃且队列非空时被接受
- **THEN** owner 取消活动 turn、为每个未启动输入产生 `session_shutdown` 结果并保持它们不被执行
- **THEN** shutdown 只在活动 turn durable 收口、Provider channel 关闭并完成清理后确认完成

#### Scenario: Retain cleanup ownership after timeout

- **WHEN** shutdown 调用方的等待 context 到期但 Provider 清理仍阻塞
- **THEN** 调用方获得 timeout，唯一 owner 仍继续执行 transport 强制取消或升级路径并等待最终完成
- **THEN** Session writer、lease、stream 和事件 channel 不失去 cleanup owner
