## MODIFIED Requirements

### Requirement: Cancellation has a single observable outcome

Runtime SHALL 将调用 context 的取消传播到每次 Provider stream 的派生 context，并等待该 stream 的输出 channel 关闭。用户取消、消费端协议错误或其他需要提前停止消费的失败 MUST 先请求取消，再由同一消费 owner 同步排空 channel 直到 Provider 清理路径退出；Runtime MUST NOT 通过停止读取遗留阻塞的生产 goroutine。

用户取消 SHALL 按既有终态选择和提交线性化得到一个结果，MUST NOT 同时产生 completed 和 failed。没有选中合法 completed 进入 sample 提交时，取消按可识别的 context.Canceled 或稳定错误码失败；已进入 sample 提交路径后 SHALL 屏蔽该操作取消并等待真实提交结果，不能以 Sync 返回前收到取消为由回滚或伪造未保存。最终 sample 提交成功时只有成功终态；工具 sample 提交后取消后续操作可以失败，但不能否认该 sample 已保存。提交结果不确定时沿既有 poisoned/cleanup 契约处理，不承诺恢复时无记录。取消与 Provider terminal 竞态仍根据唯一 terminal、调用 context 和首个消费错误选择结果，不产生两个 terminal。推理取消后仍校验并交付当前 scope 的 Started/Completed 边界以闭合宿主状态，只抑制后续 Delta；若随后 completed 被选中并提交成功，宿主不能因之前抑制增量遗留活动块，取消提示仍具有 UI 优先级。

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

#### Scenario: Cancellation arrives after commit admission
- **WHEN** 已选中合法 completed 进入 sample 提交路径，用户在 Sync 返回前取消，随后提交成功
- **THEN** 当前 sample 的推理与正文为已保存；最终 sample 只发布成功，工具 sample 后续取消不得倒退其保存状态

#### Scenario: Persistence cannot be confirmed
- **WHEN** sample 提交已接收但写入/Sync 结果不能确定
- **THEN** 当前 sample 解析为 uncertain，保留原 owner 与 poison 规则，UI 不声称 journal 无此项，恢复由实际批次验证决定

## ADDED Requirements

### Requirement: Sample presentation has explicit committed identity bindings

每个 prepared sample SHALL 在纯内存中提供已验证、独立且有界的展示块到 native item ordinal 绑定；零推理 sample 提供空表，不改变 native envelope。宿主提交通知 SHALL 明确区分 input 与 sample；input 禁止 sample 字段，sample 必须包含 SampleIndex、成功提交 Sequence 与绑定，不依赖工具 ready facts 推测。

绑定 SHALL 在提交 admission 前完成字段/数量/编码预算预检；只有 Sync/finalize 与宿主投影成功后发布普通 sample committed 通知。相同块和位置唯一，不能跨 sample，不能按正文相等去重。映射不写 Session、请求或 fingerprint；目标被列表淘汰也应能按精确 cursor 阅读。

#### Scenario: Commit steering input between samples
- **WHEN** sample 0 保存后提交同轮用户追加，再保存 sample 1
- **THEN** 三次通知来源明确，input 不冒充任一 sample，两个 sample 的块各自映射真实 Sequence/item

#### Scenario: Reject an invalid prepared binding before writes
- **WHEN** 绑定含重复块、重复位置、错误来源或超过 serialized 512 KiB
- **THEN** 提交 admission 前拒绝，不产生部分 journal 写入，不用持久化后丢弃展示数据掩盖错误

### Requirement: Sample failures resolve presentation using actual persistence facts

当前 sample 因失败需要解析时，内部 failure SHALL 携带强类型 SampleIndex 与 not_committed、durable 或 uncertain disposition；没有当前 sample 时不得伪造解析。not_committed 只能用于确定未提交；durable 必须有已确认 Sequence/绑定，用于 Sync 后 finalize/投影失败；uncertain 不得伪造已确认位置或声称未写入。正常 sample commit 与 failure resolution 不能重复解析同一个 sample。

真实失败/取消 SHALL 一次结束当前所有活动推理块，即使没有块 Completed；先前 committed sample 不改变，后续不得接受该失败 sample 的新内部 delta。不增加推理补偿记录、独立后台 owner 或自动重跑。

#### Scenario: Fail after Sync but before host projection
- **WHEN** Sync 已成功而 finalize 或宿主投影失败
- **THEN** 解析为 durable，界面标已保存且刷新失败，Runtime 按既有 poison/清理结束，不误标未保存

#### Scenario: Fail later tool work without resolving an old sample again
- **WHEN** 工具 sample 已正常发布 committed，后续工具拒绝或取消且没有活动 sample
- **THEN** failure 不重复解析旧 sample，不将其推理标未保存，外部仍只有真实 turn terminal

### Requirement: High frequency presentation reuses committed context snapshots

普通文字和推理增量 SHALL 复用最近 committed 的 context/cache 快照，不逐 delta 调用上下文规划、有效历史投影或 native footprint，不改变 committed revision。推理 start/completed 不改变上下文，也不需要重新规划；真实提交和上下文操作继续刷新。生产 publication 与控制 owner 消费端必须采用一致的封闭分类，未知事件仍失败。

新增推理传递 SHALL 保持每个输出队列最多 16 项、单个 serialized payload 最多 512 KiB，不新增无界队列、每 delta goroutine/tea.Cmd 或 timer。取消、审批、输入及 shutdown 继续由原 owner 处理；展示预算不改变模型成功性。

#### Scenario: Keep planning counts independent of delta fragmentation
- **WHEN** 相同历史/提交边界分别以 1 次和 10000 次推理 delta 到达
- **THEN** 上下文规划/投影/footprint 调用计数相同，不产生额外请求或 revision

#### Scenario: Process controls while output is active
- **WHEN** 推理仍在生成且插入中断、审批或草稿编辑事件
- **THEN** 通过确定性调度证明控制事件在最终输出前被处理，owner 最终完成，无 sleep 或概率竞态证明
