## Context

`/branch` 已在 Runtime 空闲许可下复制源 writer 已同步的完整日志，校验子线程的 Session 语法和 Provider 原生证据，然后以独占租约发布并接管。Loader 在消费原始行时已经维护物理位置、批次边界和前缀 SHA-256。历史切点需要从这条链路取得当时的有效前缀，并将选中输入的原文交给新线程的 composer。 [现有分支](../../../../internal/app/session_branch.go) · [日志 Loader](../../../../internal/session/loader.go) · [终端接管](../../../../internal/app/picker_owner.go)

### 两个参考工程的取舍

- Claude Code 2.1.88 的 `restored-src/src/commands/rewind/rewind.ts:4–12` 打开本地选择器并跳过日志消息。`components/MessageSelector.tsx:59–70,155–175,328–357,767–789` 筛选用户消息、默认停在当前项，启用文件历史时用预览、时间和操作选项辅助确认，关闭文件历史时可直接恢复。`screens/REPL.tsx:3656–3737` 按选中消息截短活动历史，并独立回填输入、粘贴图片与权限模式。`utils/messages.ts:2873–2885` 在回填时恢复输入模式并去除展示用包装。EasyCode 采用 Claude 的提示选择和编辑流程，使用回合编号区分同文提示，沿用已提交用户正文和新的 Default 权限。
- Claude Code 的 rewind 轮换 REPL 的 conversation ID 并重接同一持久 Session 中的消息链。日志写入仍通过 `getSessionId()`，冷恢复从该 Session 最新叶子查找消息链。它的独立持久分支、切入分支和返回源提示来自单独的 `/branch`。EasyCode 将两套体验组合为显式确认后发布的独立历史分支。参考 `restored-src/src/hooks/useLogMessages.ts:48–65,91–117`、`utils/sessionStorage.ts:1408–1448,2316–2331`、`commands/branch/branch.ts:229–281`。Claude 的双 Esc 与空输入、空队列及活动状态有关，EasyCode 首版使用独立命令，保留既有 Esc 路由。参考 `components/PromptInput/PromptInput.tsx:1915–1955`。
- Codex `9f2b87495bf979f6c1953cf00c98143981664914` 的 `codex-rs/app-server-protocol/src/protocol/v2/thread.rs:589–600` 定义包含所选回合的 `lastTurnId` 与排除所选回合的 `beforeTurnId`。App-server 将二者映射为互斥存储边界，复制路径使用真实 TurnStarted，分页路径在源生命周期预约下解析物理位置。分页的 writer 锁只保护获取边界，生命周期预约继续保留至子线程物化。EasyCode 在原有独占 writer 上冻结源，并采用同样明确的排他回合身份。参考 `codex-rs/app-server/src/request_processors/thread_processor.rs:5611–5655,5824–5840`、`thread-store/src/local/paginated_fork.rs:24–61,92–113,141–201`、`core/src/thread_manager.rs:1919–1947`。
- Codex 的普通 `/fork` 取最新状态，历史提示编辑走同线程 revert，并根据已持久化的首条输入核对选中内容、排除 steering 和运行中回合。它在部分分支快捷入口丢弃待处理按键，并在历史编辑后抑制队列自动发送。EasyCode 采用身份检查和显式发送语义，使用编辑框内的发送确认应对无代次的终端 Enter。Codex 的分支创建和 TUI 挂载也可能分别失败，EasyCode 继续保留已发布的目标 ID。参考 `codex-rs/tui/src/app_backtrack.rs:568–688`、`tui/src/app/event_dispatch.rs:765–875,944–1027`、`app-server/src/request_processors/thread_processor.rs:5929–5975`。
- Codex 的引用式存储能分别保存请求来源和祖先历史基底。EasyCode 将物理前缀放在一个独立 journal 中，源 ID 必须与前缀最后有效段的所有者一致。选择祖先中的切点会越过当前线程自身的标记。沿用当前线程段范围可以保留这一可独立验证的性质。参考 Codex `codex-rs/thread-store/src/types.rs:253–269`。 [EasyCode 血缘校验](../../../../internal/session/loader.go#L145)

## Goals / Non-Goals

**Goals**

- 让选中提示、原生历史切点和持久分支对应同一次用户确认。保持完整源线程可恢复，并为新线程提供来自历史首条输入的可编辑草稿。
- 延续双 Provider 的原生恢复与缓存策略。将候选浏览、分支准备与已提交结果的资源生命周期交给现有 owner。

**Non-Goals**

- 文件或 Git 状态恢复、worktree、对话原地截断和局部摘要各自需要独立的工作区或历史契约。
- 根线程首轮前的空对话前缀、跨祖先片段切点、回合内切点、跨 Provider 分支与 headless 创建属于其他切片。

## Decisions

### 从回合开始前建立切点

宿主用 `TurnID` 及首个 `InputInitial` 的 `InputID` 表示候选，从已验证的记录定位 `turn_started` 批次首行。切点等于该批次开始前最后一个完整批次的结尾，按 Loader 消费的原始 LF 计算字节位置。Runtime 在自动压缩、SessionStart 和初始输入提交之前单独同步 `turn_started`。按输入记录所在批次裁切，可能留下目标回合的自动压缩或未闭合的 Hook 生命周期。选中回合之前已收口的手动压缩、标题和任务结果属于保留前缀。 [回合起始](../../../../internal/runtime/runtime.go#L273) · [原生输入提交](../../../../internal/runtime/input_commit.go#L42) · [SessionStart 提交](../../../../internal/runtime/session_start_input.go#L97)

利用 `ReplayPlanner` 已验证的状态收集候选，沿实际普通完成、Stop 完成和失败关闭三条路径纳入已结束回合。在同一回放中的活动回合上绑定起始位置与首条提交输入，避免把相同 `TurnID` 的前置失败定位成选中回合的开始。失败或取消的回合在真实首条输入已同步时可用于重试。只在开始记录属于当前线程段、前缀已有已提交用户输入时纳入。回合含多个初始输入时 `InputIDs[0]` 是唯一选择对象，余项与后续 steering 归本轮。根首轮的前缀缺少提交输入，分支的首个自有回合可使用继承前缀满足条件。仅有前置失败或缺少可验证原生用户输入的回合不能供选择。已提交的实施交接文本与其他已提交用户输入使用同一规则。展示编号从当前段已提交的回合开始顺序派生，先计数再筛选和裁剪。读写边界继续使用持久身份。 [回放状态机](../../../../internal/session/replay.go#L562) · [有序输入关联](../../../../internal/domain/user_input.go#L96)

### 浏览和确认共用源许可

`StartupModel` 通过 `pickerOwner` 向 Runtime 预约现有 `HandoffBranch` 许可。仅在未准备或提交输入、未排队或取回、无活动回合或压缩且当前实例健康时接纳。浏览和发布沿用分支的源创建目录核对。浏览持有一份 owner 状态，绑定已接受的浏览 ID、源实例、许可与验证后的候选。创建请求有独立递增的 ID，只有匹配浏览 ID、源、选中身份与当前阶段的确认才能转为既有分支准备操作。转移检查和发布准入分别在线性化点完成，确认复用浏览取得的许可。普通最新分支的准入继续单独预约。已发布结果的匹配、接管和 CLI 交付使用确认 ID。 [现有提交和接管许可](../../../../internal/app/picker_owner.go#L74) · [结果交付](../../../../internal/app/session_picker.go#L133)

从加载阶段取消、选中 `Current`、退出浏览或确认失败时，走会释放许可的交接取消路径。只取消列表/预览 I/O 的 `CancelPending` 保留许可，不能用来结束整个历史浏览。终端保留最后接受的请求 ID，确认提交被拒时仍能关联正确的释放请求。目标准备后的接管拒绝同样收口已取得的资源和许可。取消或可恢复失败等到源事件与控制结果正常消费、候选清理及许可释放获确认后，才恢复原聊天。成功接管和宿主退出复用原 owner 关闭路径并丢弃浏览状态。所有 I/O、取消等待与关闭在锁外。 [取消与许可释放](../../../../internal/app/picker_owner.go#L145) · [源事件消费](../../../../internal/tui/chat_shell.go#L376)

发现请求通过 writer 串行队列从持有租约的 descriptor 读取。用有界 section reader 和既有 Loader 解析已同步范围，读取前后检查实际长度与 writer 记录，验证最后序号、身份、批次和血缘。根线程空历史直接返回无候选。复用原有只读 `sessionService.restore` 的完整 ReplayPlanner、两家原生历史和 usage/checkpoint 校验，发现任何待 repair 或未收口的操作都结束浏览。这里使用源已绑定的 Provider 进行纯内存恢复。启动新的目标、修复或追加补偿由各自的后续入口接纳。读取、逐批回放、原生校验、提取及复制在可取消的阶段检查取消，若已进入既有同步操作则等待原 owner 确认结束。 [写入与复制](../../../../internal/session/writer.go#L344) · [日志边界](../../../../internal/session/loader.go#L89) · [只读恢复](../../../../internal/app/session_service.go#L221)

使用上述恢复已经取得的历史语义投影，以 `TurnID`/`InputID` 对最近 200 个合格回合获取单独的用户正文和已提交 Skill 标记。原生提交序号与首条输入位置来自同一回放，确认正文时以它们查现有转录索引，核对身份、正文 SHA-256 和 Skill 标记。浏览无需再次投影完整转录。摘要独立、有界分配，避免一个短字符串引用保留整个输入。离开验证阶段后释放完整回放及其投影，owner 只保留已同步末尾序号、字节长度、全量 SHA-256 和有界候选。通常用来读展示详情的正文分页会拼接冻结附件，草稿用索引中单独的用户正文。呈现层收到展示所需的摘要，以及成功激活时的一份完整正文。 [原生用户语义投影](../../../../internal/provider/native_projection.go#L50) · [转录索引](../../../../internal/app/transcript_projection.go#L69) · [正文阅读](../../../../internal/app/transcript_queries.go#L205)

### 复用一次分支发布

历史确认向现有 `PickerBranch` 提供已验证的切点，与最新分支使用相同的候选准备、同步、排他发布、目录与 inode 复核和接管流程。确认时 writer 复核已同步末尾，流式计算全源范围的 SHA-256，并将保留区域的原始字节复制给私有暂存。全量见证包含将被排除的日志后缀。有效提交已推进、选择失配或仅展示索引关联不再可用时，终止这次尝试并提示重新选择。已确认范围长度失配、同一尾端的全量摘要变化、已证实源事实损坏或无法完整取得已同步范围时，停止源 writer 新写入并走已有故障诊断。取消、可恢复查询和目标暂存故障确认清理后可返回健康源。仅在可归因于源的证据成立时使源失效。 [现有复制证据归因](../../../../internal/session/branch_stage.go#L200)

目标追加一个既有 `thread_forked`，其中 `source_seq` 与 `source_sha256` 绑定实际复制的原始前缀。默认标题按该前缀的最新有效标题派生，之后的改名只保留在源线程。目标完整原生恢复校验和 Sync 早于发布接纳点，发布确认早于写入目标线程资源。接管与发布各自沿用既有线性化点。取消发生在发布确认之后时仍交付目标 ID，终端关闭后由现有 CLI 诊断交付已知结果。持久性确认后，新分支可独立恢复。 [分支暂存与发布](../../../../internal/session/branch_stage.go) · [分支装配](../../../../internal/app/session_branch.go) · [取消和报告](../../../../internal/app/picker_owner.go#L455-L480)

### 输入与终端投影

`/rewind` 经现有首词补全和交互命令分派打开面板，和其他宿主专属命令共用保留名判断。同名 Skill 使用 `$rewind` 或插件限定名。普通 `/rewind` 提交使用现有续行键语义，标题命令的字面反斜杠规则仍由 `/branch` 与 `/fork` 分支处理。列表默认选择 `Current`，按提交顺序显示编号和单行摘要，可用方向键和翻页键移动。摘要使用最多 512 字节的有效 UTF-8 原文前缀，再经终端安全转义和列宽裁切。确认显示同一回合编号、预览、切点和当前工作区提示。源回合有多个初始输入时提示只回填首条。默认停在返回动作，确认中必须看见范围与后果才能建立分支。Esc 从确认返回列表，再返回源对话，恢复此前草稿、光标和模式。面板消费粘贴和换行键，选择保持原样。

匹配的 `PickerActivated` 结果到达且旧 Runtime、writer 和租约关闭后，使用现有编辑路径填入首条输入正文，遵守换行规范化、UTF-8、字素光标、大小与终端显示规则，并在首次渲染前抑制命令与文件补全。该草稿只在本次激活后存在，接管失败后另行恢复目标及冷启动沿用普通空编辑框。新分支通过完整 `/resume <源 ThreadID>` 返回源。 [composer](../../../../internal/tui/composer.go) · [激活事件](../../../../internal/tui/chat_shell.go#L236)

旧聊天消息有代次约束，终端 `KeyMsg` 由当前界面接收且不携带该代次。接管前处理的 Enter 可按阶段消费，已缓冲的重复 Enter 可能在激活后才到达。现有粘贴和续行键编辑优先。历史草稿实际编辑后可正常按提交键。未编辑时本来用于提交的 Enter 只打开原样发送确认，默认选中返回编辑。Esc 和返回动作回到同一草稿，用户明确选择 `Send unchanged` 才能提交。此处使用实际文字编辑或明确的发送选择判断意图，光标、resize、自动补全预览和时间流逝不会放行。 [按键路由](../../../../internal/tui/chat_shell.go#L127) · [聊天代次](../../../../internal/tui/chat_shell.go#L410)

带 `/` 或 `$` 的草稿仍保留原首词时，原输入中已验证的 Skill 标记决定该首词提交为正文还是显式 Skill。历史中的字面 `/rewind`、`/compact` 或与当前 Skill 重名的词可作为对话文本发送。显式 Skill 使用当前配置重新准备，包括命中交互保留名的历史 headless Skill。原意图要求 Skill，但当前解析不到可调用来源时，以现有安全错误保留草稿，避免缺失裸名的普通文本回退。首词实际改变后恢复普通分派。现有提交入口先确定宿主首词意图，类型化输入草稿在内存中区分普通分派、保留正文、要求当前 Skill，再由既有 `Append`、输入准备和准入承接。 [首词分派](../../../../internal/tui/model.go#L548) · [输入准备](../../../../internal/app/file_source.go#L158) · [Skill 解析](../../../../internal/extension/skill/local.go#L223)

保留正文或显式 Skill 意图的交互专属首词（如 `/rewind`）按现有交互文件规则处理 `@`，文件候选的宿主 gate 与提交共用已解析意图。现有跨宿主保留名进入文件语法本身及持久化附件序列校验，例如 `/compact @src/x` 的 `@` 保持普通文字。这样重提历史输入时保留当前原生输入与文件附件校验。新文件和 Skill 走当前来源及正常权限，真实新输入重新分配身份。 [文件引用语法](../../../../internal/domain/file_mentions.go#L189) · [持久附件校验](../../../../internal/domain/file_attachment.go#L279)

### 原生续写

历史切点按实际日志顺序继承所保留的工具结果预算、opaque reasoning、已交付 Hook 上下文、任务状态和最近有效 checkpoint。启动设置、工作区、Provider family/wire/model 与源绑定一致。新线程沿用现有分支的 Default 模式、读写与 artifact 授权规则，首次真实对话按 `resume` 触发 SessionStart。新线程的第一笔 OpenAI WebSocket 请求使用本线程缓存 key 策略并走全请求，Anthropic 继续使用保留窗口对应断点与 TTL。验收比较同一分支的即时请求与冷恢复请求，并比较源切点和目标之间应继承的原生内容与稳定段。 [现有分支规格](../../../specs/session/branch/spec.md#requirement-inherited-native-history-preserves-continuation-with-fresh-execution-authority)

## Risks / Trade-offs

- 日志很长时，打开面板需要一次完整校验，确认还需流式验证同一日志范围。展示复用恢复中已有的投影，摘要数量与大小有硬上限。进入等待用户选择的阶段前释放全量快照，确认只复制目标前缀。
- 选择错误的重名提示或包含自身 Hook/压缩的中间批次会污染续写。使用有序 `TurnID`、首条 `InputID`、完整批次和源快照绑定，按独立分支回放验收。
- 过去的对话上下文可能和目前的工作区内容不同。确认页提示使用当前文件，分支停止继承执行授权，重新提交草稿时按普通路径准备附件。

## Migration Plan

沿用当前 journal、Catalog、Provider 和外部 headless 记录形状。实现验收后同步三项主规格、README、能力清单和路线图，记录历史切点的证据和实际平台范围。归档时按归档目录深度重新核对提案与设计的相对链接。
