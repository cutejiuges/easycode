## MODIFIED Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在请求 provider 前产生一次 `turn_started`。Runtime MUST 为每次 Provider stream 创建派生 context，逐项验证事件，记录唯一 terminal，并持续消费直到生产者关闭 channel；只有 channel 关闭且此前恰好收到一个合法 completed terminal，Runtime 才能产生一次 `turn_completed`。provider channel 关闭本身 MUST NOT 被解释为成功。

零值或非法事件、channel 在 terminal 前关闭、多个 terminal，以及 terminal 后任何事件均 MUST 作为 stream protocol failure。Runtime MUST 在首次发现协议错误时立即取消派生 context，保留首个协议错误作为最终结果，停止转发后续语义事件，并同步排空 channel 直到生产者清理完成；后续 terminal 不得覆盖首个协议错误或恢复成功。

#### Scenario: Complete a successful turn

- **WHEN** provider 产生文本事件和唯一合法 completed terminal，随后完成清理并关闭 channel
- **THEN** Runtime 按顺序输出 `turn_started`、中间语义事件和一次 `turn_completed`
- **THEN** `turn_completed` 只在 channel 关闭证明生产者退出后发布

#### Scenario: Fail when stream closes before terminal

- **WHEN** provider channel 在 completed、failed 或 cancelled 终态之前关闭
- **THEN** Runtime 输出一次 `turn_failed`
- **THEN** RunTurn 返回 `stream_protocol_error`

#### Scenario: Propagate provider failure

- **WHEN** provider 产生唯一合法 failed 终态并随后关闭 channel
- **THEN** Runtime 输出一次 `turn_failed` 并返回对应错误
- **THEN** Runtime 不再输出 `turn_completed`

#### Scenario: Drain after an invalid stream event

- **WHEN** provider 发布非法事件、重复 terminal 或 terminal 后事件
- **THEN** Runtime 立即取消该 stream 的派生 context并停止转发新的语义事件
- **THEN** Runtime 排空至 channel 关闭后输出一次 `turn_failed`，并返回首个 `stream_protocol_error`

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
