# runtime/input-control Specification

## Purpose

定义会话级 Runtime 在同一进程中持续接收输入、顺序执行多个 durable turn、定向中断并安全关闭的控制契约，同时保持 Provider 原生历史单 writer 和明确的 admission 线性化语义。

## Requirements

### Requirement: Runtime commands are versioned and strongly typed

内部输入控制面 SHALL 为 `submit_input`、`interrupt` 和 `shutdown` 提供唯一当前的强类型 command、专属 constructor 和 validator；内部 command 不携带 format revision，也不存在按 revision 选择的 decoder。每条 command MUST 携带非空 request identity；`submit_input` MUST 携带合法 UTF-8、去除外围空白后非空且不超过 4 MiB 的文本，`interrupt` MUST 携带预期活动 `turn_id`，`shutdown` MUST NOT 接受无关 payload。

command kind 与 typed payload 不匹配、非法 ID、非法 UTF-8 和不适用于该 kind 的字段 MUST 在产生排队、turn ID、Session 写入或 Provider 副作用前失败。command、提交结果与拒绝原因 MUST NOT 使用或暴露无约束 `any`。外部 headless stream-json 的 `version: 1` 由 headless adapter 严格解码后投影为内部 command，不得把外部 wire version 复制进 Runtime 核心类型。

#### Scenario: Accept a valid submit command
- **WHEN** 宿主提交具有唯一 request identity 和合法文本的当前 `submit_input`
- **THEN** 控制面返回强类型提交结果，并且后续生命周期可以通过同一 request identity 关联

#### Scenario: Reject an invalid command without side effects
- **WHEN** command 包含未知 kind、非法 target turn、空白文本、超限文本或 kind/payload 不匹配
- **THEN** 控制面返回稳定英文 `invalid_command` 或 `invalid_input` 错误
- **THEN** command 不进入队列、不分配 turn ID、不写 Session 且不调用 Provider

#### Scenario: Adapt the current external protocol once
- **WHEN** headless adapter 成功解码一个 stream-json `version: 1` command
- **THEN** adapter 构造不含 version 的内部强类型 command
- **THEN** Runtime 不暴露外部 JSON decoder 或第二套版本化 command DTO

### Requirement: One owner decides admission and turn execution

每个活动 thread SHALL 只有一个输入控制 owner 串行决定命令 admission、活动 turn 身份、follow-up queue 转换和关闭状态。空闲时接受的 `submit_input` SHALL 得到 `starting` disposition；存在活动 turn 时接受的输入 SHALL 得到 `queued` disposition，并进入该 Session 实例持有的 FIFO follow-up queue。

并发调用方不得通过先读取状态再自行启动 turn 的方式绕过 owner。任意时刻最多一个 `RunTurn` 可以修改该 Conversation；先被 owner 接受的输入 MUST 先开始，输入不得合并、重排或并行执行。活动 turn 期间接受的输入 SHALL 形成后续独立 turn，不得注入当前 Provider stream，也不得被称为 same-turn steer。

#### Scenario: Serialize concurrent submissions

- **WHEN** 两个 submit 在空闲 thread 上并发到达
- **THEN** owner 按唯一顺序接受它们，恰好一个得到 `starting`，另一个得到 `queued`
- **THEN** Provider 任意时刻只存在一个活动 stream

#### Scenario: Preserve one input per turn

- **WHEN** 活动 turn 期间连续接受两个 follow-up 输入
- **THEN** 当前 turn 先收口，两个输入再按 FIFO 各自启动新的独立 turn
- **THEN** 系统不拼接两个文本，也不把它们加入当前 turn 的 Provider 请求

#### Scenario: Close the lost-wakeup window

- **WHEN** 新输入在 owner 观察到队列为空与活动 turn 状态切换为空闲之间到达
- **THEN** owner 重新观察已接受工作并最终启动该输入
- **THEN** 输入不会滞留到下一条无关命令到达后才执行

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
