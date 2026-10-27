# runtime/input-control Specification

## Purpose

定义会话级 Runtime 在同一进程中持续接收输入、顺序执行多个 durable turn、定向中断并安全关闭的控制契约，同时保持 Provider 原生历史单 writer 和明确的 admission 线性化语义。

## Requirements

### Requirement: Runtime commands are versioned and strongly typed

内部输入控制面 SHALL为`submit_input`、`resolve_tool_approval`、`interrupt`和`shutdown`提供唯一当前的强类型command、专属constructor和validator；内部command不携带format revision，也不存在按revision选择的decoder。每条command MUST携带非空request identity；`submit_input` MUST携带合法UTF-8、去除外围空白后非空且不超过4 MiB的文本；`resolve_tool_approval` MUST携带expected active turn ID、approval request ID、invocation ID、candidate fingerprint、允许的typed decision和仅适用于deny的可选16 KiB反馈；`interrupt` MUST携带预期活动`turn_id`，`shutdown` MUST NOT接受无关payload。

command kind与typed payload不匹配、非法ID/fingerprint/decision、非法UTF-8、超限内容和不适用于该kind的字段 MUST在产生排队、approval fact、turn ID、Session写入或Provider/工具副作用前失败。command、提交结果与拒绝原因 MUST NOT使用或暴露无约束`any`。外部headless stream-json的`version: 1`仍只投影当前已支持的外部command；本变更不向无交互外部协议暴露可阻塞的approval response wire，也不得把外部wire version复制进Runtime核心类型。

#### Scenario: Accept a valid submit command
- **WHEN** 宿主提交具有唯一request identity和合法文本的当前`submit_input`
- **THEN** 控制面返回强类型提交结果，并且后续生命周期可以通过同一request identity关联

#### Scenario: Accept a correlated approval command
- **WHEN** 交互式宿主提交与当前active turn、request、invocation和fingerprint匹配的`resolve_tool_approval`
- **THEN** 控制面返回强类型accepted结果并把decision交给唯一turn owner

#### Scenario: Reject an invalid command without side effects
- **WHEN** command包含未知kind、非法target turn、空白/超限文本、过期approval identity或kind/payload不匹配
- **THEN** 控制面返回稳定英文`invalid_command`、`invalid_input`或`stale_approval`错误
- **THEN** command不进入队列、不写approval fact、不分配turn ID且不调用Provider或executor

#### Scenario: Adapt the current external protocol once
- **WHEN** headless adapter成功解码一个stream-json`version: 1`command
- **THEN** adapter构造不含version的内部强类型command
- **THEN** Runtime不暴露外部JSON decoder或第二套版本化command DTO

### Requirement: One owner decides admission and turn execution

每个活动thread SHALL只有一个输入控制owner串行决定command admission、活动turn身份、活动approval、follow-up queue转换和关闭状态。空闲时接受的`submit_input` SHALL得到`starting` disposition；存在活动turn时接受的输入 SHALL得到`queued` disposition并进入该Session实例持有的FIFO follow-up queue。`resolve_tool_approval`只能路由到同一owner当前记录的一个活动request，不进入follow-up queue，也不得启动新turn。

并发调用方不得通过先读取状态再自行启动turn或完成approval的方式绕过owner。任意时刻最多一个`RunTurn`可以修改该Conversation；先被owner接受的输入 MUST先开始，输入不得合并、重排或并行执行。活动turn期间接受的普通输入 SHALL形成后续独立turn，不得注入当前Provider stream、approval feedback或称为same-turn steer。

#### Scenario: Serialize concurrent submissions
- **WHEN** 两个submit在空闲thread上并发到达
- **THEN** owner按唯一顺序接受它们，恰好一个得到`starting`，另一个得到`queued`
- **THEN** Provider任意时刻只存在一个活动stream

#### Scenario: Route approval without creating a turn
- **WHEN** 当前turn等待approval且匹配response到达
- **THEN** owner把response交给该turn并保持原turn identity
- **THEN** response不进入普通输入FIFO、不生成用户turn也不建立并发Provider stream

#### Scenario: Preserve one input per turn
- **WHEN** 活动turn期间连续接受两个follow-up输入
- **THEN** 当前turn先收口，两个输入再按FIFO各自启动新的独立turn
- **THEN** 系统不拼接两个文本，也不把它们加入当前turn的Provider请求

#### Scenario: Close the lost-wakeup window
- **WHEN** 新输入或approval response在owner观察状态转换之间到达
- **THEN** owner重新观察已接受工作并最终处理它
- **THEN** 命令不会滞留到下一条无关命令到达后才执行

### Requirement: Admission is bounded and has a linearization point

follow-up queue SHALL 同时限制消息数量和 UTF-8 文本总字节数，且每条输入仍受 4 MiB 上限约束。超过任一队列上限的 submit MUST 以稳定英文 `input_queue_full` 拒绝，不得阻塞 reader、分配 turn ID、写 Session 或调用 Provider。

提交操作的 context 只控制等待 owner admission 的过程：在 owner 接受前取消 MUST 无副作用失败；owner 一旦返回 `starting` 或 `queued`，控制器 SHALL 接管该输入的最终处理，调用方随后取消不得隐式撤回输入。排队状态是同一进程内的非 durable obligation；只有后续 `turn_started` 成功 Sync 才表示该输入已 durable 开始，进程重启不得从 Session 推测或重放尚未开始的排队输入。

#### Scenario: Cancel before admission

- **WHEN** submit context 在 owner 接受命令前取消
- **THEN** submit 返回可识别的取消结果，队列、Session 和 Provider 保持不变

#### Scenario: Retain ownership after admission

- **WHEN** owner 已返回 `queued` 后原调用方停止等待或取消其 context
- **THEN** 输入仍由控制器按 FIFO 启动，除非显式 shutdown 以规定的拒绝结果终止它

#### Scenario: Reject queue overflow

- **WHEN** 新输入会使消息数或总字节数超过配置上限
- **THEN** owner 返回 `input_queue_full`，既有队列内容和顺序保持不变

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

### Requirement: Interrupt is targeted and race-safe

`interrupt` SHALL 只在其 expected turn ID 与当前活动 turn 完全匹配时请求取消，并返回 `interrupting` 结果。空闲状态 SHALL 返回 `no_active_turn`，目标已被新 turn 替换 SHALL 返回 `turn_mismatch`；两种拒绝都不得取消任何 turn、改变队列或产生 Provider 副作用。重复 interrupt 和 interrupt 与 terminal 的竞态 MUST 保持当前 turn 唯一 durable terminal，且不得误取消随后启动的 turn。

#### Scenario: Interrupt the expected active turn

- **WHEN** interrupt 的 expected turn ID 与当前活动 turn 相同
- **THEN** owner 请求取消该 turn，并等待 Runtime 产生唯一 durable failure terminal
- **THEN** 队列保持 FIFO，后续输入只在被取消 turn 收口后启动

#### Scenario: Reject a stale interrupt

- **WHEN** interrupt 指向已经结束的 turn，而另一 turn 当前正在运行
- **THEN** owner 返回 `turn_mismatch` 且不取消当前 turn

#### Scenario: Ignore repeated cancellation side effects

- **WHEN** 同一活动 turn 收到重复的有效 interrupt
- **THEN** Provider cancellation 最多被有效触发一次，Runtime 仍只发布一个 terminal

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

### Requirement: Request identity prevents outstanding duplicate execution

控制器 SHALL 拒绝与当前正在 admission、排队、执行或等待终态的命令重复的 request identity，并且不得为重复 submit 创建第二个输入 obligation。已完成 identity 的保留可以有界，且本变更不承诺跨进程或进程重启去重；任何有界淘汰策略 MUST 是实例状态，不得使用可变包级 registry。

#### Scenario: Repeat an outstanding submit identity

- **WHEN** 相同 request identity 的 submit 在原输入仍排队或执行时再次到达
- **THEN** owner 返回稳定 `duplicate_request` 且只执行原输入一次

#### Scenario: Restart does not replay a queued identity

- **WHEN** 进程在一个输入仍仅位于内存队列时终止并随后恢复 Session
- **THEN** 恢复只依据已有 Session records，且不推测、执行或确认该排队输入

### Requirement: Approval response admission is correlated and race-safe

owner SHALL只在expected turn仍活动、request仍pending且invocation/fingerprint完全匹配时接受`resolve_tool_approval`，并返回`resolving` disposition。不存在活动approval SHALL返回`no_active_approval`；turn已替换 SHALL返回`turn_mismatch`；request或fingerprint过期 SHALL返回`stale_approval`。所有拒绝都不得改变grant、Session、队列或文件。

同一response request identity与同一approval的重复resolution MUST被去重。resolution一旦被owner接受，调用方context后续取消不得撤回decision；owner负责返回确定的durable decision/result或失败终态。interrupt/shutdown与resolution竞态必须由同一owner排序，不能误批准随后turn的request。

#### Scenario: Reject a response for an old turn
- **WHEN** approval response指向已结束turn而另一turn当前活跃
- **THEN** owner返回`turn_mismatch`且不解决当前request

#### Scenario: Deduplicate an approval response
- **WHEN** 相同response request identity被并发或重复提交
- **THEN** owner至多接受一次，且只产生一个durable approval outcome

#### Scenario: Caller cancellation follows accepted resolution
- **WHEN** owner已返回`resolving`后调用方context取消
- **THEN** owner继续完成decision的durable结果，调用方取消不能造成未知approval状态

### Requirement: Every chat host shares the current control lifecycle

TUI、一次性文本/JSON headless 和 stream-json SHALL 通过同一控制生命周期执行输入接收、定向中断、turn 调度和关闭。宿主 adapter MUST NOT 持有另一套直接启动 turn 的执行状态机。每个活动 thread 只有一个实际控制 owner 和一个有序输出消费者；应用 MUST 等待实际运行的 owner，而非另一个空闲 facade，才可释放其依赖资源。

宿主现有外部行为 SHALL 保持：一次性模式只提交一个输入且保留输出格式/退出码；TUI 活动期间禁止 composer 提交；stream-json 保持 FIFO、request identity 和独立外部 version。

#### Scenario: Run each host through one owner
- **WHEN** 相同文本分别通过 TUI、一次性 headless 和 stream-json 执行
- **THEN** 每个会话只有一个执行 owner，副作用与 durable terminal 都只发生一次
- **THEN** 各宿主仍产生原有格式的输出，不泄漏其他宿主的控制事件

### Requirement: Host detachment preserves cleanup ownership

宿主退出或输出失败后，事件分发 SHALL 能解除宿主发送背压并继续排空内核输出。正常消费时 MUST 保留事件顺序和 terminal；停止向已退出宿主投递不得省略 Session durable 收口。启动取消、输出分发和运行 owner SHALL 各有唯一清理责任与可等待的最终完成结果。

关闭等待超时 MUST NOT 释放仍被活动 turn 使用的 journal lease、workspace 或 repository。应用 SHALL 保留失败结果，执行明确的活动 stream 取消/强制清理路径，等待 worker、owner 与分发器退出，再按生命周期释放资源。重复关闭 SHALL 观察同一最终结果，不新建无 owner 的等待任务。

#### Scenario: Exit with a full host event buffer
- **WHEN** TUI 停止读取且事件 buffer 已满，随后请求关闭
- **THEN** 分发不再阻塞于已退出宿主，活动 turn 取消并完成唯一 durable terminal
- **THEN** worker、控制 owner 和分发器最终完成后，才关闭 writer 并释放 lease

#### Scenario: Cancel during startup
- **WHEN** 控制 owner 已启动而宿主启动 context 取消
- **THEN** 启动路径取消并等待已创建任务清理后返回，不能遗留孤立 owner

#### Scenario: Escalate a shutdown timeout
- **WHEN** 确定性注入使活动 stream 清理超出首次 shutdown 等待期限
- **THEN** timeout 被保留，应用请求解除真实 stream 阻塞并继续等待固定完成信号
- **THEN** 清理完成前其他进程仍不能取得活动 journal lease，完成后可以取得

#### Scenario: Repeat shutdown while cleanup is pending
- **WHEN** 多次关闭与 terminal 发布并发发生
- **THEN** 通道只由唯一 owner 关闭，不发生重复关闭、send-after-close 或第二次副作用
- **THEN** 所有关闭调用观察一致的最终清理结果
