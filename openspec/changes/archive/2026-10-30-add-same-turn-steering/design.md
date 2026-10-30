## Context

动机见 proposal.md。已观察的接缝：

- `internal/runtime/agent_loop.go` 的实例 owner 已持有 FIFO、request ledger 和唯一 turn worker；普通 submit 只能排队为独立 turn。
- `internal/runtime/runtime.go:RunTurn` 在 `commitToolOutputs` 后继续采样；无 calls sample 直接提交 terminal。这里适合作为唯一中途输入安全点。
- `internal/tui/model.go:handleKey` 在非 idle 状态忽略普通输入，`streamReadyMsg` 才清除原草稿；`ChatSession` 以单个提交的事件流适配 UI，必须扩展为长期控制输出消费，不能同时增加第二个消费者。
- `internal/session/replay.go` 将单记录 native batch 解释为 tool outputs；首轮输入依附成功 sample 保存。当前 Session canary=4、两家 native canary=1，外部 headless v1 是独立边界。
- `internal/provider/{openai,anthropic}/history.go` 依据 sample.Input 建立语义 turn；`domain.SemanticTurn` 只有 UserText/AssistantText，不能表达 turn 内交错消息。
- `context/planning.go` 分别估算 committed history 与 current input；原生 footprint 已覆盖可见正文，不能再次相加。

参考证据来自主仓库相邻只读目录，worktree 中的 `../` 不存在：Claude Code 2.1.88 的 `utils/handlePromptSubmit.ts:313`、`query.ts:1535`、`utils/queueProcessor.ts`、`utils/messageQueueManager.ts:428`；Codex 6f8bf683c 的 `core/src/session/turn_input.rs:850`、`session/turn.rs:466`、`tasks/mod.rs:690`、`tui/src/chatwidget/input_queue.rs`、`thread-store/src/store.rs:102` 和 `core/tests/suite/pending_input_persistence.rs`。Claude 的模块级队列、文本 trim 与 Codex 允许后台持久化的 SteeredUserInput 策略均不直接移植。架构文档中的旧 Codex 提交在实现后的文档同步中改为实际参考快照，并保留历史决策的原始版本语境。

## Goals / Non-Goals

**Goals:**

- 运行中继续输入、显示队列、取回编辑；系统自动选择当前工具安全点或下一 turn。
- 每条输入只归属一个可执行位置，每个 turn 最多一个 worker，每次持久化具有可证明的取消线性化点。
- 首轮与中途输入采用同一纯准备、候选预检、durable、finalize 生命周期，网络失败后已保存输入可恢复。
- Provider wire 私有；共享层只持有强类型文本、输入关联和 opaque envelope。

**Non-Goals:**

- 不抢占正在流式生成的 sample，不撤销已经执行的文件/进程副作用，不改变审批授权。
- 不支持图片、slash 命令框架、后台执行、自动 compact、持久化待执行队列或跨重启命令去重。
- 不把 sample 数限制升级为无限循环，不引入 app-server、额外 transport 或新依赖。

## Decisions

### 1. 一份 owner 队列，两种实际投递意图

现有 `submit_input` 表达 strict-next：空闲时开始，忙碌时形成后续独立 turn，一条输入一个 turn。新增 `submit_conversational_input` 表达 auto：TUI 默认使用；stream-json 通过显式 `input.append` 使用。auto 命令不要求 UI 猜测活动 turn；owner 在接收和领取时分别决定排队位置与实际目标。

每个实例只持有一个按 admission 顺序排列的 FIFO，元素包含领域 InputID、命令 RequestID、文本和 delivery。InputID 使用 UUIDv7，与 request 去重身份分离；ID 不进入模型 wire 或 cache 内容。现有 outstanding/recent ledger 复用，返回/消费完成后才释放对应 obligation；不承诺跨进程 request 去重。

- 活动 turn 只能领取队首连续 auto 前缀，遇到 strict-next 停止，不能跳过或重排。
- 空闲时 strict-next 只启动自己；auto 可把队首连续 auto 前缀作为一个新 turn 的多条有序用户消息。
- 一组 auto 初始输入的首条 RequestID 作为 turn 事件的控制面 origin；其余通过各自 committed 反馈关联到同一 turn，不能为一条 delta 输出多份副本。
- 无 calls sample 保持最终 sample 语义，先关闭当前 turn，再启动剩余队列；不采用 Codex 因 pending input 延长纯文本 turn 的策略。
- 常规 Provider 失败但 Runtime/journal 健康时继续现有 FIFO；编码不可表示的输入单独返还；durability 不确定时停止全部后续执行。

替代方案：统一修改 submit_input 会破坏既有自动化 FIFO 契约；在 UI 私自重试 steer 会产生重复接收窗口；多队列优先级会让输入越过严格屏障。均不采用。

### 2. 输入状态和反馈

状态为 `queued -> claimed -> committing -> committed`，未 durable 的确定失败可转为 `returned`；Sync 结果不确定转为 `uncertain` 并 poison，不得自动返还为可执行消息。初始自动输入也经 owner claim，不另设启动旁路。

控制面新增强类型 input status，携带 InputID、关联 RequestID、状态、可选真实 turn ID及固定英文原因；committed 只表示已保存，不宣称远端消费。运行时发布用户输入 committed 文本事件用于正式 transcript，控制面 status 用于接收结果和队列投影，两者由同一有序输出消费者处理，避免重复消息。一次性 headless 不投影这些新增控制事件。

队列预览使用 TUI 已保存的接收文本。返还事件按身份关联，stream-json 调用方保留原始正文，不在状态事件中再次输出正文；显式取回命令给 TUI 的强类型操作结果可以携带原正文副本。正文不得进入 diagnostic/error。

### 3. 安全点握手与最终边界

Runtime worker 只在整组 results 和 native tool outputs durable/finalize 后提出 `(turnID, nextSampleIndex)` 的领取请求；达到 sample/call 上限、取消、poison 或 terminal 决策后不再请求领取。请求和返回都为有界值，通过 loop owner 已拥有的固定通道通信，不暴露给 Provider。

owner 核对 active turn、边界序号和取消状态，冻结可消费的 FIFO 前缀，并返回 claim identity。每个 turn 同时最多一份 claim；worker 返回确定 resolution 后才可领取下一份。owner 在 worker 做 I/O 时继续消费控制命令与事件。

```text
sample Sync -> tools -> results/native outputs Sync
                                 |
                                 v
                       owner grants FIFO claim
                                 |
                                 v
                    prepare -> candidate budget/request preflight -> input Sync
                                 |
                                 v
                    finalize -> committed events
                                 |
                                 v
                       plan -> next sample
```

领取先于撤回则撤回失败；撤回先于领取则该输入不在 claim 中。最后一份无 calls sample 一经选择提交 terminal，所有晚到输入留在队列。worker 退出且 terminal 已输出、stream 清理结束后才能启动下一 turn。claim 未决不得以 finished 信号偷偷释放输入。

### 4. 统一用户输入提交和当前格式

引入 `provider.UserInputPreparer` 的小接口及强类型 `PreparedUserInputs`，接收有序 `(InputID, text)`、turn identity 和 initial/steering placement，纯内存编码并验证。提供 envelope、独立只读输入关联及候选历史/footprint 快照、Finalize/Discard；新增小的 Provider 请求预检接口，仅纯内存编译相同 snapshots 下的候选请求，不建立 stream。候选快照必须与本次 prepared 增量及 committed revision 绑定，禁止 finalize 前通过 ProjectHistory 暴露 staging；仅生命周期相同的无 usage 提交骨架允许与 PreparedToolOutputs 共享私有组合实现，不复用 tool result 类型或把用户输入伪装成工具结果。sample 保留自己的 usage/ready-call 校验。

当前原生 entry 为 `user_inputs`、`sample`、`tool_outputs`。user_inputs 保存原生用户消息及仅用于恢复的输入关联；sample 只保存模型输出、raw usage/metadata，不再包含 input 字段。用户输入不会在 sample 成功时再次追加。

Session 新增 `turn_inputs_committed`，payload 只含 placement、target_sample_index 和按顺序排列的 InputIDs，不复制正文、RequestID 或宿主 UI 状态；紧邻同 batch 的 `provider_native_commit` 保存正文事实。具体 grammar：

| batch | 合法位置与内容 |
|---|---|
| start | 单条 turn_started；没有网络副作用 |
| initial inputs | `[turn_inputs_committed(initial, target_sample_index=0), provider_native_commit(user_inputs)]`，仅在 start 后且首 sample 前出现一次 |
| steering inputs | `[turn_inputs_committed(steering, target_sample_index=n), provider_native_commit(user_inputs)]`，仅在第 n-1 sample 的整组 tool outputs 闭合后、sample n 前出现至多一次 |
| tool sample | `[provider_native_commit(sample), sample_usage, tool_call_ready...]` |
| final sample | `[provider_native_commit(sample), sample_usage, turn_completed]` |
| tool output | 单条 provider_native_commit(tool_outputs)，仍受完整 ledger/配对约束 |
| failure | 单条 turn_failed，已有工具调用先按原契约补齐结果配对 |

不是按“长度为 1/2”猜测所有 native kind；Session 按封闭 batch grammar 和输入关联记录识别提交角色，Provider 验证 opaque kind 与关联上下文一致。恢复接口改为强类型 restore record，包含 turn ID、期望提交角色/输入关联和 envelope；app 将 ReplayPlan 映射过去，Session 不解析 Provider payload。native user metadata 中的身份和顺序必须与外部 fact 一致，错配整体拒绝。

Session canary 从 4 替换为 5，两个 native canary 从 1 替换为 2；这些只做唯一当前精确匹配，不是 V2 类型或版本分派。外部 JSONL 仍为 v1，与 Session 无关。SQLite catalog schema 不变，可由新 journal 重建；旧 journal 不迁移、不改写，报告 incompatible。

首轮输入在纯内存预算和请求编码预检通过后、网络前保存，因此远端失败或提交后取消允许出现只有用户消息的 turn；确定的本地预检超限不得保存该输入。start 成功但 initial commit 未成功时允许本地 turn_failed，不伪造用户历史。输入提交不产生 usage，不计为模型 sample，也不重置 16 samples/64 calls 上限。

### 5. 取消线性化、崩溃和返还

命令 context 只控制 owner admission 前等待。领取不是 durable 接收；worker 在准备和 journal admission 前观察 turn cancellation。调用 AppendBatch 使用仍可取消的 turn context，不提前用 WithoutCancel 掩盖接收前取消；writer 一旦接受请求就等同现有线性化点，之后必须返回确定结果，不因 caller context 取消撤回。

- writer 未接受且确定无写入：discard staging，claim 中输入返还；可继续使用健康 Session。
- Sync 成功：即使同时取消也 finalize 一次，先发布 committed，再收口取消；不得返还或重发这些输入。
- 写入/Sync 结果无法确认：poison，输入状态 uncertain，关闭且保留 lease 到清理完成；只有新进程严格恢复能确认事实，不给自动重试建议。
- 恢复看到 initial 或 steering commit 但没有后续 sample：保留已提交用户历史，写入旧 turn 的本地失败终态，不建立网络流，不执行工具。
- 内存 queued/claimed、草稿和撤回文本没有 durable 承诺，进程崩溃后不恢复；UI 不得标为已保存。

现有 `turn.interrupt` 保留后续队列。新增交互式 stop 命令带 expected turn ID，原子停止该 turn 并返还截至该次 admission 的未领取 auto 输入；strict-next 保留。停止处理中拒绝新的 auto admission，避免用户刚停止又被自动输入立即启动；取消清理完成后恢复接收。claimed 输入由提交结果决定返还/committed/uncertain，不能在 stop 接收时先行猜测。无 active turn 时 stop 返回 no_active_turn；纯队列编辑用取回命令。

EOF 停止新 admission，但已接受输入继续按 FIFO 排空；shutdown 返还/终止未领取输入、取消活动 turn，等待 claims、worker、dispatcher、writer 清理。宿主断管后只停止投递，继续排空和清理，不能丢失 ownership。

### 6. 编码预算和上下文

延用 4 MiB 单条 UTF-8、64 条/16 MiB 待处理正文上限；queued 和未解决 claim 共享额度，claim 不能释放额度后无限接收。每条初始/自动输入在 admission 前还需纯内存检查 native/record 可表示性；4 MiB 原文不保证 JSON 转义后可写入 16 MiB record。拒绝使用稳定 input_encoding_limit，不回显正文。ID 和 record envelope 的固定最大开销需计入安全预算，不能只比较 payload bytes。

选择本次 claim 时按 FIFO 取满足 native envelope、record 16 MiB 和 batch 64 MiB 限制的最大前缀；单条可表示性已在 admission 检查，批次过大只分批，不永久堵队首。严格输入仍只有一条。精确预编码在交给 writer 前完成；新增路径不得把可预判大小错误交给 writer 后造成 poison。候选请求仍受 Provider 16 MiB 编译上限，必须在输入 Sync 前以相同 snapshots 编译验证；确定的本地超限返还本次输入，不污染已提交历史。

提交前以 committed history 加 prepared user_inputs 生成仅内存候选视图及 native footprint，并编译候选请求检查大小；不改动 committed history、不发网络。预算明确 over_limit 或编码超限时 discard 并返还全部本次 claim；初始输入失败收口本 turn，中途输入拒绝后继续原工具循环，前提是未追加时的请求本身合法。随后用户可编辑缩短再提交；不得自动重试。预检通过才 Sync/finalize，用户输入成功 finalize 后只存在于 committed history。规划的 current_input 表达“没有额外待发送文本”的纯采样状态；移除旧首 sample Text 与 continuation 二义。每次 request 从不可变 snapshots 和完整 committed native history 生成。可见估算遍历有序消息，原生 footprint覆盖其自身全部正文/opaque，取覆盖值而不相加。预算预检超限时输入未保存并可取回编辑；提交成功后的网络/远端失败仍保留输入且不自动重试。InputID/turn ID不进入语义 cache segment canonical 内容；为规划生成仅含角色/正文的纯投影视图，不能把展示关联字段直接序列化进 fingerprint。

### 7. 双 Provider wire 与历史投影

OpenAI：每条输入编码为独立 user input_text item，位于已闭合的 function/custom outputs 之后；InputID 不映射为 Provider item ID，不生成合成 system 指令。现有 reasoning/encrypted content/message phase 原样保留。

Anthropic：保留既有完整 tool_result user message，后面追加独立有序 user text messages；不把文本插进未闭合的 result 组，不修改已提交 tool-output entry，不自动合并历史 message。首轮、连续 user、tool_result 后追加输入的 wire 由固定 golden 验证。保持与既有临时项目指令 user message 的独立性。

SemanticHistoryView 按真实 turn 分组，SemanticTurn 持有强类型有序消息；user 消息带 InputID，assistant 文本按 sample 顺序排列。无可见文本的 native 项省略；关联数据不是 wire，不包括签名、encrypted 内容、usage 或原始 JSON。恢复上下文提供真实 turn identity，不能靠“遇到用户消息就新建 turn”推断。

TUI 在 user committed 时结束前一个 assistant 文本 cell，下一 delta 创建新 cell；同一 InputID 从队列移入 transcript 恰好一次。部分未提交 assistant 文本可以在 live UI 标为中断，但不作为已提交历史或恢复事实。recall 从有序用户消息种子初始化，沿用 100 条/8 MiB；运行中 accepted 输入可以进入 recall，committed 反馈不重复加入。

### 8. 取回编辑与键盘路由

新增 retract_inputs 命令携带有界、非重复 InputIDs；只允许自动队列中仍 queued 的目标，全部验证通过后原子返还。任一已 claimed/不存在则整个命令以 input_not_queued 拒绝，不取回部分内容。TUI 先用已有副本验证合并草稿的 4 MiB 限额，再发命令；等待撤回期间冻结该次草稿编辑，避免响应覆盖新输入。响应失败保持原草稿；新队列项不在指定 IDs 中，不能被一并取走。

- 普通运行状态允许编辑和显式 paste；Enter 使用自动输入命令。等待 admission 的当前草稿保留快照，成功才清除。
- 空草稿且在首视觉行按 Up：有 queued auto 则取回，否则按既有 recall；非空草稿继续视觉移动/recall。
- Esc：审批/详情/局部浮层先处理；普通聊天有可取回队列则取回，没有则定向 stop；idle 保持原草稿语义。
- Ctrl+C：普通活动任务定向 stop；idle 的非空草稿保留、空草稿退出。审批按已有 abort 语义，并使交互 auto 队列按 stop 规则返还。
- 取回多个输入时仅在编辑副本中按队列顺序以 LF 连接，再接原草稿；超限原子失败。正常消费不拼接正文。
- stop 返还的总正文可能大于 composer 上限：使用有界、不可执行的 returned tray 保留原条目，显式逐条取回；不截断、不自动覆盖新草稿。tray 与 queued/claimed 共用展示保留预算，未被用户取回/丢弃前不能无限接收新 auto 输入。该 tray 只属 UI，不是可执行队列，退出不持久化。

替代方案：UI 先删除再通知内核可能重复执行；直接恢复所有正文可能超限或覆盖用户新草稿；两者不采用。

### 9. 外部协议与宿主复用

新增 stream-json v1 命令：`input.append`(request_id,text)、`input.retract`(request_id,input_ids)、`turn.stop`(request_id,expected_turn_id)。均严格解码未知/重复/null/超限字段。既有 input.submit、turn.interrupt、session.shutdown shape 不改，不向内部 command 复制外部 version。

retract/stop 接受 disposition 分别为 retracted/stopping。input.append 的 control.response 包含新的 `entry_id` 与 starting/queued disposition；entry_id 是领域 InputID，既有 turn event 的 input_id 仍是首条请求 RequestID，避免重定义旧字段。新增 `input.status` 携带 version、type、request_id、entry_id、state、可选 turn_id 和固定 reason；状态为 claimed/committed/returned/uncertain。committed 必须在 Sync 后，返回消息的操作结果与逐条 status 保持一致。auto 启动 turn 的所有 turn events继续由首条 RequestID 关联，其他成员通过 input.status 关联到同一 turn。status 不含正文。新命令控制结果先于相应 status；初始 turn.started 先于 committed，committed 先于后续 request 产生的 delta。

只有自动输入获得这些新增外部状态；既有严格 input.submit 的输出序列/字段保持不变。内部初始输入 committed 事件由一次性 headless 和严格 stream 模式 adapter过滤，仍使用原有 text/JSON完成与 usage协议。session 当前 canary 不泄漏到外部 v1。

ChatSession 扩展小的长期控制接口，TUI 只保留一个 Outputs 消费链；移除仅用于旧 TUI 单次阻塞提交的重复路由，不另起 runtime 或 dispatcher。纯构造、读取快照、render 不做 I/O。

### 10. 验证与文档

在既有 agent_loop_test、current_contract_test、双 Provider request/restore/history/footprint golden、Session current fixtures、app composer/tool-loop/resume e2e、headless stream tests 和 TUI snapshots 上增加业务断言。新测试按职责放在 input_queue、input_commit、input_replay、input_delivery、input_queue_view 等明确文件中。race 以可控 channel/故障注入决定交错，不用 sleep 证明结果。

文档同步必须更新实际 vocabulary、当前 canary、首轮失败后用户消息保留、内存队列非 durable 与终端键盘限制。主 specs 中现有“失败 turn 投影完全不变”“sample 自带用户输入”“活动 TUI 禁止提交”等条款由本 change 的 MODIFIED deltas替换，不用新条款旁路旧条款。

## Risks / Trade-offs

- [统一输入生命周期扩大了首轮改动面] → 明确纳入 proposal，三种宿主和双 Provider 都使用真实路径；用未追加场景的 wire golden证明模型请求不重复且不无故变化。
- [已保存不等于远端已处理] → 状态命名为 committed；网络失败与用户输入事实并存，不输出 delivered/processed 承诺。
- [批量 auto 改变可见 turn 数量] → 仅 opt-in auto 生效；strict-next 保持屏障与独立 turn。
- [新 ID 污染缓存] → 关联元数据只用于存储/投影，request 与规划内容 fingerprint 都排除 ID，并做跨 ID 等价回归。
- [JSON 转义膨胀及返还草稿超限] → admission 可表示性校验、精确前缀预算、returned tray 与有界所有权。
- [中断期间仍发生已接收 durable 提交] → 清楚区分 writer admission 之前/之后，测试所有竞争结果，不能擅自返还已提交输入。
- [参考工程复杂机制外溢] → 不引入其全局队列、mailbox、后台持久化、图片或 slash 框架；所有新增类型有本 change 内消费者。

## Migration Plan

1. 在后续明确 apply 中逐层替换当前协议、native codec和Session grammar，同步current fixtures，禁止双 reader过渡实现留在完成状态。
2. 旧开发 journal/native canary在加载/repair/append前失败关闭；不原地转换。新建Session用于验收，旧数据目录保持只读原样。
3. 通过 openspec strict validate、delta/main契约对照、架构守卫、make verify；记录本机可运行平台范围，不宣称新增跨平台能力。
4. 同步 specs、架构、Roadmap、pitfall 后另行归档。若回滚代码，只能使用回滚版本兼容的独立数据目录；禁止旧二进制打开新canary journal。
