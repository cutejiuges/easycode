## Context

基线为 EasyCode `4054a7e` 和已归档的 `add-interactive-plan-mode`。Plan Mode 已为整轮冻结指令与七项允许工具。双 Provider 的 `ProjectAssistantSample` 可按样本引用读取已提交的助手可见正文，当前供 Stop 检查使用。AgentLoop 管理真实空闲、输入顺序和操作 profile。TUI 已有 Markdown、原文阅读、输入保留和会话代次过滤。[选择依据](proposal.md)

### 本地参考快照

| 参考 | 已核对的行为 | EasyCode 的选择 |
| --- | --- | --- |
| Claude Code 2.1.88 sourcemap，`a8a678cb6244e6770e1e421767ff0987a1d95549` | `ExitPlanMode` 等待用户确认，终端先展示 Markdown，再提供手动审批修改或继续规划。Esc 拒绝，继续规划可带修改意见。保留上下文的选项通过工具批准切回所选权限模式，启用相关开关时可同时提交模型建议的命令权限。工具可回读计划文件，编辑后的正文可随批准提供。 | 采用先读方案再决策的界面、固定选择区和继续规划。用户在现有输入区发送修改意见。由同一冻结正文驱动审阅与实施，确认只开始新回合，权限由普通策略按每次调用处理。 |
| Codex，`9f2b87495bf979f6c1953cf00c98143981664914` | 使用独立行 `<proposed_plan>`，按已完成的单条 assistant message 提取并单独投影。TUI 在实时回合结束且没有排队、待处理的同轮输入或冲突弹层时展示决策。同线程选项用短指令在 Default 发起新回合。新线程选项才使用核验过的计划附件或带上限的完整正文。 | 采用明确标记、单条消息边界、无后续输入时审阅及独立 Default 回合。同会话输入也携带获批原文，应对自动压缩。按 Provider 可用的终结证据核对候选，要求标记真实闭合且拒绝歧义。 |

Claude Code 证据在本地参考仓库的 `restored-src/src/`。`tools/ExitPlanModeTool/ExitPlanModeV2Tool.ts:185–238,243–259` 定义交互确认、模式门禁和计划文件读取。`components/permissions/ExitPlanModePermissionRequest/ExitPlanModePermissionRequest.tsx:56–74,194–217,425–450,475–506,512–555,727–744` 展示权限变更构造、当前计划、同上下文决策、反馈、取消和滚动时仍可操作的选择区。该文件的 `:332–393` 将所审正文带入清上下文后的初始输入。

Codex 证据在本地参考仓库的 `codex-rs/`。`collaboration-mode-templates/templates/plan.md:92–128` 定义标记，`core/src/session/turn.rs:2886–2913,2996–3048` 按单条消息的 `output_text` 单独投影计划并保存原生响应。`tui/src/chatwidget/input_submission.rs:603–606` 在新同轮输入到达时清除旧候选资格，`turn_runtime.rs:214–285` 决定是否弹出，`plan_implementation.rs:22–105` 接线实施选择。`core/src/session/approved_plan.rs:25–99,102–134` 核对新线程所用的完整计划字节。其提取器在输入结束时也可得到未闭合内容，且会剥离引用标记。EasyCode 的实施交接要求真实闭合并原样携带所审正文。见 `utils/stream-parser/src/tagged_line_parser.rs:84–106`、`proposed_plan.rs:93–115,183–195`。

## Goals / Non-Goals

**Goals**

- 让用户审阅一个确切版本，明确决定是否在当前交互会话中发起实施。
- 以已提交的助手样本和现有 owner 作为方案来源与接纳依据，保留模式冻结、工具准入、缓存和 Session 恢复的既有边界。
- 对两家 Provider 验证相同的交互与可靠性契约，保留各自的 wire、opaque 推理和缓存策略。

**Non-Goals**

本切片适用当前交互会话、当前 Provider、完整文本方案和普通审批。计划文件、编辑器、清上下文、新线程、跨 Provider、模型工具调用退出模式、权限模式选项和 headless 控制另行设计。

## Decisions

### 1. 从最终提交正文识别一份完整方案

在现有固定 `WorkModePlan.Instructions` 中说明标记规则。可实施的完整方案占用最终答复的全部正文。问题、阶段性调查和仍待决定的内容用普通回复，修订后的完整方案重新输出整块。候选来自成功终态所对应的最终采样，Provider 按以下原生事实选正文。

| Provider | 审阅正文来源 |
| --- | --- |
| Anthropic Messages | 无工具调用的已提交助手消息按原顺序读取可见 text blocks。若提供 `stop_reason`，要求 `end_turn`。`message_stop` 也会结束 `max_tokens` 或 `refusal` 的流，此类已提交回复保留转录阅读。省略 stop reason 的兼容端点仍须通过正常流终态与完整标记检查。 |
| OpenAI Responses | 在已验证 `response.completed` 的最终采样中，使用唯一有正文的 `final_answer` 助手消息。显式非完成状态或非空 `incomplete_details` 阻止该采样提供审阅，缺省状态的兼容端点仍可使用。终结证据只绑定本次已提交的进程内采样。其余消息保留原生顺序及普通展示。若可见助手消息全无 phase，只在恰有一条非空正文消息时使用它。只有 `commentary`、`partial_answer`、未知显式 phase 或多个最终正文均无法作为候选。仅拼接选中消息内部有序的 `output_text` parts；多条消息的标签不能拼成一个方案。 |

标记必须逐字独占一行，不带缩进、尾随空格或围栏。块外仅允许空白行。LF 与 CRLF 用于定位行，闭标签行可直接结束于 EOF。正文取开标签行结束之后到闭标签行开始之前的原字节，包括闭标签前的换行。任何额外的独立标记行，包括正文代码示例中的标记行，都会使整份候选歧义。空正文、未闭合块和超过 32 KiB 的正文留在转录。工作流使用最终提交的正文，SSE 分包、直播文本和渲染结果不参与候选识别。

AgentLoop 从带样本信息的 `EventTranscriptCommitted` 记住当前回合最后的样本引用；输入和 Hook 反馈的转录提交不能覆盖它。普通结束与 `StopPassed` 均须看到成功终态及 worker 退场。届时用只读的审阅投影读取并核对同一回合的最终原生条目，复用原生历史与现有文本提取，具体 phase 和停止原因留在适配器。现有 Stop 正文的 `ProjectAssistantSample` 会拼接 OpenAI 多条消息并复制历史；审阅投影应定位当前回合的最终条目，避免复制整个历史，保留 Stop 投影语义。读取失败只影响候选，已完成回合仍按其终态显示。接缝见 `internal/runtime/reasoning.go` 的 `emitSampleCommit`、`internal/runtime/stop_hooks.go` 的 `decideStop`、`internal/provider/{openai,anthropic}/history.go` 和 `internal/provider/native_history.go`。

### 2. AgentLoop 只持有当前可确认的候选

候选的身份由当前会话、线程及已提交的 `TurnID + SampleIndex` 确定。AgentLoop 在 `handleTurnFinished` 确认成功终态已交付、worker 返回成功、宿主仍可接纳且没有待排队输入后，发布一次内部审阅通知。通知走有序控制流，独立于已经结束的 turn event 序列，并带原回合关联。用 `loopWorker.cancelRequested` 区分已经接纳的 Interrupt 或 StopTurn，即使取消竞态最终产生了成功记录也不发审阅。正常退场本身会取消 worker context，是否还可接纳应查看会话的运行及关闭状态。最终采样之后仍在排队的追加输入阻止发出候选，已在工具或 Stop 边界成功提交的追加可以产生新的最终方案。TUI 另根据当前聊天代次过滤迟到消息。

待审阅状态至多持有一份有界 Markdown，复制提取后的正文时避免保留原始采样字符串的容量。通知和确认指令使用类型化构造器、校验器和严格解码，发布失败不影响已提交的原生历史。该内部命令及通知仅由当前交互宿主消费，外部 headless 的解析、事件投影和错误词汇沿用现有契约。

接纳普通聊天输入、独立压缩、实际退出 Plan 或确认实施都会使旧候选失效。拒绝的输入、取消或失败的模式切换以及只读查询不消耗候选。会话交接许可期间任何确认都返回忙碌，失败或取消后源实例可手动重新审阅，成功接管的目标从新实例和 Default 进入。无效、失效或已消费的身份在接纳前拒绝。采用复用 owner 的单候选状态，避免把计划或权限挂到任务清单、转录选中项或宿主的另一份可变状态。

### 3. 确认一次接纳 Default 与确切的新输入

TUI 的类型化确认只传当前方案身份和请求身份。ChatSession 沿用模式切换的短期入口预留，与文件或 Skill 准备、会话交接串行。`AgentLoop.submitRequest` 核对交互宿主来源，并将调用方的取消信号交给 owner，覆盖命令已投递但尚未接纳的时间段。AgentLoop 校验真实空闲、健康状态、Plan、候选匹配和请求去重。由 owner 持有的字面 Markdown 构造一条普通用户输入，固定前缀为 `Implement the following approved plan:` 加空行，后跟完整正文。跳过 Skill、slash 命令和 `@` 文件引用解析，保留方案中的例子和路径。接纳前沿用普通输入的队列上限和 `inputEncodingFits` 纯内存编码检查。接纳后，实际自动压缩、普通请求及上下文预算预检、原生准备和 Sync 沿用正常初始输入路径。

先验证带新 InputID 的接纳结果和关联 Default 模式快照，再检查调用取消、宿主关闭和 loop 停止。接纳时一起更新 Default profile、消费候选并接纳实施输入。按顺序发布确定结果和模式快照，在处理下一条命令前进入现有启动路径。实施输入走普通交互输入归属，且是新回合唯一的初始输入。后到的普通输入按该回合已启动后的规则处理。沿用会话的运行 context 启动 worker，并冻结 Default 与当时所选 effort。确认调用的 context 只用于接纳，接纳后的取消仍返回确定结果。

TUI 在清除审阅状态前把同一份文字关联已接纳的 InputID，后续按现有输入状态区分已提交、可返还或不确定。历史条目和实施输入的终端召回以 Sync 后关联的 `user_input_committed` 为依据；召回和取回是普通编辑，编辑器换行规则不参与已经接纳的字面输入。接纳前拒绝保留原模式和有效候选。接纳后的正常回合中断、预算或预检拒绝按输入归属反馈，所选模式为 Default。无法确定提交事实时沿现有 poisoned 和不确定状态收口。恢复只读取已提交事实或执行现有本地中断恢复，用户可显式开启后续回合。把模式切换和普通 Append 拆成两次命令会留下输入竞争窗口，因此由 owner 一次接纳。

确认方案仅决定开启哪个实施回合。普通 Tool Policy 继续在候选准备和执行阶段核对同一目标与调用身份，适用现有窄范围 grant、单次批准、Hook 信任和沙箱。计划正文中提出的权限要求仅作为该次输入的文本。Plan 回合自己的冻结工具许可一直维持到清理完成。

### 4. 终端焦点和准确审阅

以已有 composer、modal 键位路由和转录 Markdown/原文滚动为接缝。实时通知只在空闲、普通编辑器拥有焦点、草稿为空、主转录跟随尾部、当前没有未解决输入或控制、补全及其他弹层，且外层处于 `shellChat` 时自动打开一次。按当前作用域与最近成功终态核对通知，终态后的 `activeTurnID` 已清空。选择器打开时底层仍消费有序控制流并保存符合条件的源候选，延迟通知不得在选择器下打开审阅。取消切换后用户可显式打开，成功切换后丢弃源投影。读到旧聊天代次的消息直接忽略。

候选仍有效时，空闲用户可以用 `/plan review` 显式打开。空闲编辑器有空间时提供一条命令提示，未自动打开的审阅无需抢占焦点即可找到。手动打开也要求没有冲突的输入或控制，实施确认前应有空间保留新 InputID 的正文与归属。关闭弹层保留转录阅读位置。打开成功只消费命令草稿且不进入召回，占用或校验失败保留操作前编辑状态。继续沿用 `/plan` 的物理 Enter、首词补全和文件候选排除。普通输入、独立压缩和交接实施的接纳结果，以及离开 Plan 的模式快照，按同一控制流使该投影失效。接纳前失败保留仍有效的候选。

审阅弹层显示可滚动的完整正文，并在正文滚动时保留明确可达的决策区。Markdown 超预算或可能隐藏终端控制字符时，复用 `markdownRawWindow` 的分页机制呈现完整安全原文。原文视图显式表示 CRLF 中的 CR、制表符、终端控制字符和字面的反斜杠，区分真实控制字符和形似转义的字面文本。使用有界、已准备的 Markdown 视图，滚动和选择不重复解析完整正文。若视口只能显示截断或替代内容，或无法同时展示正文与明确选项，禁用实施。布局恢复后以继续规划为默认，用户须再次主动选择实施。

选项为 `Implement with normal approvals` 和 `Keep planning`。Esc 或 Ctrl+C 在选择前关闭弹层，修订走普通 composer。粘贴与 Shift+Tab 不作为确认。确认请求待决时冻结编辑，Esc 或 Ctrl+C 只取消当前确认，不设置针对未来回合的中断。从收到接纳到看到相同请求的 `TurnStarted`、失败或输入结算前，显示启动状态，这些按键不可退出宿主或产生待执行中断。新回合显示后，独立按键按实际 TurnID 走普通中断。控制流关闭时由外层路由交给当前模型，清理待决或启动状态并按已知的确定结果显示。参考现有 `internal/tui/{model,chat_shell,input,work_mode}.go`。

### 5. 原生请求及缓存

两个 Provider 从同一固定 Plan 指令和允许工具快照编译普通请求。标记说明进入 `provider_profile` 的内容指纹和一次 token 估算。审阅状态不参加指纹或请求编译，`tool_catalog` 仍来自当前模式选定的定义。自动压缩候选估算包括新的实施输入，摘要读取旧窗口，实际普通请求再送出完整的原文字面输入。原生历史和 checkpoint 只保存现有提交事实，恢复不构造审阅凭据。

双 Provider 的持续运行与恢复对账使用同一已提交 thread、相同配置、effort 及下一输入，比较完整 canonical 请求与内容指纹。将签名或加密推理及有序工具配对放在压缩后的实施回合，使它们确实进入被比较请求。压缩前历史则按完整原生回放逐项对账。WebSocket 恢复首发必走全量，持续运行时同属性请求可能只有增量后缀，因此单独检查 OpenAI 的模式变化和恢复回退，以及稳定 thread key。缓存未被其他原因重置时，Plan 转 Default 的属性变化走既有 full request 分支。Anthropic 按实际工具序列和现有缓存策略核对 TTL marker。

## Risks / Trade-offs

| 风险 | 验收 |
| --- | --- |
| 工具续采样、Stop 尚未完成、回合中断或旧会话事件误触发审阅 | 用确定性事件和 worker 闸口覆盖一条普通成功、一条 StopPassed 和代表性续轮、失败、晚取消及切换。记录实际候选身份与时序。 |
| 用户读到了截断或不同版本 | 验证 CRLF、中文与代码块、标记分包、畸形或超限块、终端滚动和安全原文，断言已展示的完整方案与批准输入中的正文一致。 |
| 重复确认、竞争输入或取消导致半切换 | 覆盖准备中、排队、压缩、resume、关闭及接纳两侧的取消。用输入身份、模式通知与持久事实断言零次或一次实施。 |
| 自动压缩抹去被审阅的细节 | 提供故意省略识别文本的摘要，分别检查 Anthropic Messages 和 OpenAI Responses 的实施原生请求、普通权限路径、恢复后请求 canonical bytes、item 顺序与 fingerprint。 |
| 将计划确认误当成提前批准写入 | 在同一集成链路让首个实施回合尝试新建文件，检查普通审批出现且此前文件不存在。拒绝和允许后的状态按原契约验收。 |
| 自定义上游不按固定格式返回或拒绝某种请求属性 | 缺少合格方案时仍使用原有转录和手动实施路径。双 Provider 验收用本地 wire 夹具，报告实际覆盖的平台和 transport。 |

## Migration Plan

Apply 改动使用当前 Session JSONL、native item、checkpoint 和外部 headless 契约。既有会话按当前规则恢复，含标记的历史方案按原转录阅读。实现验收后同步两个主规格，以及 README、能力清单、Roadmap 和架构说明。必要的新踩坑按实际验收记录。验证记录保留命令、环境和结论，原始 wire、日志与 PTY 捕获保存在忽略的本地输出。
