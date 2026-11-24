## MODIFIED Requirements

### Requirement: Interactive session handoff reserves a truly idle source before preparing a target

交互宿主 SHALL 在打开聊天内选择器、直接准备恢复目标或读取分支创建的源切点之前，向当前交互控制实例申请强类型交接许可。恢复与分支共享一个交接许可，源实例同一时刻最多持有一个。强类型交接意图区分两条宿主路径，许可只对本次交接请求与源实例有效。唯一输入控制 owner 只在 accepting、Runtime 和 journal 健康、输入队列及 claim 为空、所有普通 turn 和手动压缩 worker 已真实结束，且无进行中的可变更控制操作时授予许可。turn worker 启动但 durable started 尚未发布、已发布 terminal 但 worker 或 stream 仍在收口，以及同轮自动压缩期间均为忙。恢复和分支创建不得排队等待许可。宿主还 SHALL 在本地存在未确认输入、回收中输入或待处理的返还文本时阻止切换。

接纳前输入准备和交接许可 SHALL 有唯一顺序。已进入交互接纳路径的输入，包括正在准备的 Skill 或文件附件，使切换以 `session_busy` 拒绝，输入按原契约继续。交接许可先成功时，源侧新输入与状态变更 SHALL 在准备正文、排队或改变状态之前被拒绝。检查覆盖所有源侧入口，包括直接送达控制 owner 的工具审批、Hook 信任响应和信任重置，以及取回、会话命名、effort 设置和手动压缩。许可成立后可继续纯只读观测和关闭。

接收方 owner SHALL 在同一次有界状态转换中检查取消信号并决定是否安装许可。接收前已取消 MUST 不建立许可。许可接收后 MUST 交付确定的许可结果，或在同一尝试撤销时完成释放并确认释放结果。界面离开当前焦点或调用方取消等待时，宿主仍负责处理该结果。已撤销尝试只有在确认未安装许可或确认许可已释放后才可结束。释放和确认绑定源实例与尝试身份，旧结果不得释放后来取得的许可或恢复旧焦点。

宿主接受接管时 SHALL 在同一次状态转换中核对源仍为当前健康实例且持有本次许可，目标为同一次尝试已准备完成并仍可用的候选，宿主和尝试均未取消。分支目标 ID 由宿主生成，必须与本次请求绑定且不同于源 ID。原请求身份在生成目标 ID 后保持不变。接管前取消或目标失败，且源健康、该尝试清理完成时，释放匹配许可并让源继续使用原控制实例、已提交输入历史、草稿和运行设置。源意外故障、关闭或失去可用状态 SHALL 进入宿主终态清理，禁止把源重新开放为可用聊天。接管成功后许可保持至源侧最终关闭。接管后关闭、取消或故障 SHALL 沿宿主终态路径处理。目标仍可用且源 worker、控制流及 journal writer/lease 均已完成关闭后，才允许目标接收输入。

此许可仅供进程内交互宿主使用。分支 journal 的创建、发布结果和清理由宿主的分支生命周期按 `session/branch` 契约负责。它 MUST NOT 创建 RuntimeCommand/Event kind、Session record 或 headless wire 操作，也不得发起 Provider、Tool 或 Hook 调用。

#### Scenario: Reject while an operation is actually finishing
- **WHEN** 源回合或手动摘要的 terminal 已发布，但其 worker 或 stream 清理被确定性屏障暂停，用户请求切换
- **THEN** 不发放许可、不开始目标准备，源操作按原生命周期完成。完成清理后可重新申请许可

#### Scenario: Serialize a resume with input preparation
- **WHEN** 交接申请与带 Skill 或文件引用的输入接纳竞争
- **THEN** 已进入接纳路径的输入照常继续并阻止切换。许可先成立时该输入在附件准备或排队前被拒绝，且源控制实例保持空闲

#### Scenario: Reject a permit cancelled before the receiving owner's decision
- **WHEN** 交接请求在到达 owner 后被确定性屏障暂停，取消在 owner 检查接收条件之前发生
- **THEN** owner 确定拒绝该请求，源继续接纳输入而无需等待释放任何许可

#### Scenario: Resolve a permit accepted before the caller stops waiting
- **WHEN** owner 安装许可后，结果交付被确定性屏障暂停，用户已取消该尝试，随后又发起新尝试
- **THEN** 宿主以原身份取得确定结果并完成释放确认后才结束原尝试。晚到的旧确认无法释放新许可、重新打开选择器或恢复旧焦点

#### Scenario: Reject a direct mutation while a permit is held
- **WHEN** 源持有交接许可，Hook 信任重置或其他可变操作直接到达控制 owner
- **THEN** 原信任与运行状态保持不变，交接继续等待目标结果或确认取消

#### Scenario: Retain original controls after a cancelled or rejected resume
- **WHEN** 已持有交接许可且源仍健康，用户在接管前取消或目标在准备中被可恢复地拒绝
- **THEN** 该尝试完成清理后释放自身许可，原输入和运行设置保持可用，迟到结果无法锁定或切换源会话

#### Scenario: Prevent handoff when either instance closes before acceptance
- **WHEN** 在目标就绪和宿主接管之间注入实例关闭
- **THEN** 目标关闭时拒绝接管并在清理后恢复健康的源。源关闭时走宿主终态清理，候选不挂载，源不再接收输入

#### Scenario: Commit one handoff and close the original ingress
- **WHEN** 已就绪目标成功获得一次接管，同一请求再次确认或上一尝试的释放消息随后到达
- **THEN** 源侧持续拒绝输入或变更，关闭原资源后才放行目标输入，重复与过期操作不影响任一会话

#### Scenario: Give resume and branch one source admission order
- **WHEN** 当前源实例先后或并发接收聊天内恢复与分支请求，已有一次成功取得交接许可
- **THEN** 只有持有许可的尝试可以继续准备目标。另一尝试不得读切点、创建 journal、发动切换或接管同一许可

#### Scenario: Bind a generated branch target without retargeting the request
- **WHEN** 分支请求已获许可，且宿主生成新 thread ID 后收到过期、错误目标 ID 或已撤销尝试的候选
- **THEN** 请求身份保持原值，仅本次匹配源实例、许可、已验证目标 ID 的健康候选可接管。宿主仍交付匹配尝试的确定清理与发布结果

## RENAMED Requirements

- FROM: `### Requirement: In-chat resume reserves a truly idle source before preparing a target`
- TO: `### Requirement: Interactive session handoff reserves a truly idle source before preparing a target`
