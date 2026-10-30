## MODIFIED Requirements

### Requirement: Runtime commands are versioned and strongly typed

内部输入控制面 SHALL为`submit_input`、`submit_conversational_input`、`retract_inputs`、`stop_turn`、`resolve_tool_approval`、`interrupt`和`shutdown`提供唯一当前的强类型command、专属constructor和validator；内部command不携带format revision，也不存在按revision选择的decoder。每条command MUST携带非空request identity；`submit_input` MUST携带合法UTF-8、去除外围空白后非空且不超过4 MiB的文本；`resolve_tool_approval` MUST携带expected active turn ID、approval request ID、invocation ID、candidate fingerprint、允许的typed decision和仅适用于deny的可选16 KiB反馈；`interrupt` MUST携带预期活动`turn_id`，`shutdown` MUST NOT接受无关payload。

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

自动输入 SHALL 使用与严格输入相同的正文校验；retract 必须携带 1 至 64 个合法、非重复 InputIDs，stop 必须携带 expected turn ID。新增命令同样具有专属强类型构造与严格验证，不能复用无约束 payload。

### Requirement: One owner decides admission and turn execution

每个活动thread SHALL只有一个输入控制owner串行决定command admission、活动turn身份、活动approval、follow-up queue转换和关闭状态。空闲时接受的`submit_input` SHALL得到`starting` disposition；存在活动turn时接受的输入 SHALL得到`queued` disposition并进入该Session实例持有的FIFO follow-up queue。`resolve_tool_approval`只能路由到同一owner当前记录的一个活动request，不进入follow-up queue，也不得启动新turn。

并发调用方不得通过先读取状态再自行启动turn或完成approval的方式绕过owner。任意时刻最多一个`RunTurn`可以修改该Conversation；输入 MUST 按 admission FIFO 处理，正文不得拼接、重排或并行执行。严格 submit_input 保持每条一个独立 turn；自动输入只可领取队首连续自动前缀，在完整工具安全点进入当前 turn，或在空闲时形成一个含多条独立消息的新 turn。严格输入是不可跨越的 FIFO 屏障；任何输入均不得注入活动 stream 或 approval feedback。

#### Scenario: Serialize concurrent submissions
- **WHEN** 两个submit在空闲thread上并发到达
- **THEN** owner按唯一顺序接受它们，恰好一个得到`starting`，另一个得到`queued`
- **THEN** Provider任意时刻只存在一个活动stream

#### Scenario: Route approval without creating a turn
- **WHEN** 当前turn等待approval且匹配response到达
- **THEN** owner把response交给该turn并保持原turn identity
- **THEN** response不进入普通输入FIFO、不生成用户turn也不建立并发Provider stream

#### Scenario: Preserve one input per turn
- **WHEN** 活动turn期间连续接受两个严格 submit_input follow-up输入
- **THEN** 当前turn先收口，两个输入再按FIFO各自启动新的独立turn
- **THEN** 系统不拼接两个文本，也不把它们加入当前turn的Provider请求

#### Scenario: Close the lost-wakeup window
- **WHEN** 新输入或approval response在owner观察状态转换之间到达
- **THEN** owner重新观察已接受工作并最终处理它
- **THEN** 命令不会滞留到下一条无关命令到达后才执行

### Requirement: Admission is bounded and has a linearization point

follow-up queue SHALL 同时限制为最多 64 条和 16 MiB UTF-8 正文字节，queued 与未解决 claim 共用预算，且每条输入仍受 4 MiB 上限约束。超过任一队列上限的 submit MUST 以稳定英文 `input_queue_full` 拒绝，不得阻塞 reader、分配 turn ID、写 Session 或调用 Provider。

提交操作的 context 只控制等待 owner admission 的过程：在 owner 接受前取消 MUST 无副作用失败；owner 一旦返回 `starting` 或 `queued`，控制器 SHALL 接管该输入的最终处理，调用方随后取消不得隐式撤回输入。排队状态是同一进程内的非 durable obligation；`turn_started` 成功 Sync 只表示生命周期开始，输入正文只有配对的输入提交 batch 成功 Sync 才 durable，进程重启不得从 Session 推测或重放尚未开始的排队输入。

#### Scenario: Cancel before admission

- **WHEN** submit context 在 owner 接受命令前取消
- **THEN** submit 返回可识别的取消结果，队列、Session 和 Provider 保持不变

#### Scenario: Retain ownership after admission

- **WHEN** owner 已返回 `queued` 后原调用方停止等待或取消其 context
- **THEN** 输入仍由控制器按 FIFO 启动，除非显式撤回、交互停止或 shutdown 按相应契约终止该输入

#### Scenario: Reject queue overflow

- **WHEN** 新输入会使消息数或总字节数超过配置上限
- **THEN** owner 返回 `input_queue_full`，既有队列内容和顺序保持不变

### Requirement: Every chat host shares the current control lifecycle

TUI、一次性文本/JSON headless 和 stream-json SHALL 通过同一控制生命周期执行输入接收、定向中断、turn 调度和关闭。宿主 adapter MUST NOT 持有另一套直接启动 turn 的执行状态机。每个活动 thread 只有一个实际控制 owner 和一个有序输出消费者；应用 MUST 等待实际运行的 owner，而非另一个空闲 facade，才可释放其依赖资源。

宿主现有外部行为 SHALL 保持：一次性模式只提交一个输入且保留输出格式/退出码；TUI 普通活动状态支持自动输入，审批与详情保持焦点隔离；stream-json 保持 FIFO、request identity 和独立外部 version。

#### Scenario: Run each host through one owner
- **WHEN** 相同文本分别通过 TUI、一次性 headless 和 stream-json 执行
- **THEN** 每个会话只有一个执行 owner，副作用与 durable terminal 都只发生一次
- **THEN** 一次性宿主与严格 input.submit 仍产生原有格式的输出，自动输入宿主投影显式新增状态，不泄漏其他宿主的控制事件
