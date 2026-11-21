## Context

当前基线为 EasyCode `bc01e8a`。AgentLoop 已为每次操作捕获推理设置并等待 worker 完全收口，ChatSession 在交互输入进入队列前冻结 Skill 和文件引用。工具循环先提交完整样本和调用集合，再按顺序执行与配对。TUI 已有 Markdown 转录、问卷、任务清单、补全及聊天内会话交接。选择依据见 [proposal.md](proposal.md)。对应接缝在 `internal/runtime/{agent_loop,input_commit,resume_permit,tool_scheduler}.go` 与 `internal/tui/model.go`。

| 候选 | 现有前置条件与取舍 |
| --- | --- |
| **交互式 Plan Mode（选定）** | 组合现有只读工具、结构化问答和同会话历史，增加一个能实际约束模型能力的日常工作流。它需要贯通双 Provider 的请求与准入，可沿用当前持久化结构。 |
| PostCompact Hook | 能直接复用 PreCompact 的 runner 和信任，但收益主要集中在用户配置了压缩后检查的场景。 |
| 同 Provider 会话分支 | 与 `/resume` 的价值相邻，但需要定义 thread lineage、Catalog 可发现性、继承窗口、scope-bound artifact 及既有授权归属。 |
| MCP 或 Subagent | 分别需要外部服务 owner 与动态目录，或 child thread、预算、取消和完成投递，适合独立推进。 |

Roadmap 对照为 P3 工具与安全执行、P5 Claude 风格 TUI、P6 扩展及 P7 子代理。 [当前能力](../../../docs/roadmap/capability-status.md) · [产品 Roadmap](../../../docs/roadmap/product-roadmap.md) · [Session/Thread ADR](../../../docs/architecture/adr/0003-session-thread-tree.md)

### 固定参考快照

| 参考 | 已核对行为 | 本次采用的机制 |
| --- | --- | --- |
| Claude Code 2.1.88 sourcemap，`a8a678cb6244e6770e1e421767ff0987a1d95549` | `/plan` 进入规划。权限循环包括 Plan，footer 显示当前模式并按宽度缩减键位提示。主线程 ExitPlanMode 使用本地计划文件和交互批准，可继续规划或在普通审批下实施。 | `/plan`、Shift+Tab、持续的只读提示、同一对话内阅读和修订。实施由用户退出后另行提交，沿用本仓库的普通工具审批。 |
| Codex，`9f2b87495bf979f6c1953cf00c98143981664914` | 空闲时 Shift+Tab 在 Default 和 Plan 之间切换。Plan 的 developer 指令要求先查证、用问卷澄清产品选择，再输出决策完整的 Markdown 方案。`<proposed_plan>` 投影和 `update_plan` 分开。实施入口以有效模式发起一个新的用户回合。 | 冻结模式并把指令作为 Provider 请求属性。沿用结构化问卷和清晰的方案正文，用显式新回合划分规划和实施，避免将任务进度误当成方案。 |

Claude Code 证据位于本地 `claude-code-sourcemap` 的 `restored-src/src/`。`commands/plan/plan.tsx:64–119` 还支持在 Plan Mode 中查看当前文件方案及通过 `/plan open` 打开文件。EasyCode 首期在现有转录中阅读方案，`/plan` 在计划中报告模式。键位见 `keybindings/defaultBindings.ts:27,64`，footer 见 `components/PromptInput/PromptInputFooterLeftSide.tsx:336–354`，退出和人工决定见 `tools/ExitPlanModeTool/ExitPlanModeV2Tool.ts:185–253` 与 `components/permissions/ExitPlanModePermissionRequest/ExitPlanModePermissionRequest.tsx:635–745`。其 `utils/permissions/filesystem.ts:244–254,1483–1494` 给计划文件特殊写入权限。

Codex 证据位于本地 `codex` 的 `codex-rs/`。模式键和 `/plan` 见 `tui/src/chatwidget/interaction.rs:352–365` 与 `tui/src/chatwidget/slash_dispatch.rs:987–1010`，指令见 `collaboration-mode-templates/templates/plan.md` 与 `core/src/context/world_state/collaboration_mode.rs:157–175`，计划投影见 `core/src/session/turn.rs:2996–3048`，下一回合见 `tui/src/chatwidget/plan_implementation.rs:22–101`。该快照的工具注册路径仍可暴露 shell 和 apply_patch，依靠规划指令表达工作方式，执行器继续走通用策略。见 `core/src/tools/spec_plan.rs:1225–1262,1419–1422` 与 `core/src/tools/handlers/apply_patch.rs:305–336`。

Claude Code 的快捷键走权限循环，可立即重查待决审批。EasyCode 采用 Codex 的空闲切换边界，并将本轮模式冻结到收口。两份参考都支持带任务描述进入，首期仍以本地模式命令和独立正文提交组成流程。Claude Code 压缩后分别附上计划文件正文和模式提醒。Codex 恢复同一线程时可取回持久模式，同线程实施提交普通指令，只有新会话实施专门携带核对过的计划文件或有界原文。EasyCode 保留现有转录阅读与显式实施输入。见 Claude Code `components/PromptInput/PromptInput.tsx:1525–1547` 和 `services/compact/compact.ts:541–555,1470–1485`，以及 Codex `core/src/session/mod.rs:987–1029` 和 `core/src/session/approved_plan.rs:24–99`。

## Goals / Non-Goals

**Goals**

- 用户在同一交互会话中调查、询问、阅读和修订方案，之后清楚地决定何时回到普通模式。
- 模式同时决定普通请求的工具声明和调用准入。两家 Provider 使用已有原生历史、工具、缓存和压缩基础设施。
- 入口和退出为本地控制，原生历史只记录真实用户正文、助手输出与正常工具事实。

**Non-Goals**

本次边界为两个由用户选择的模式、普通 Markdown 计划和同会话独立提交。模型发起模式控制、专用计划文件及审阅确认、单键实施、清上下文、切换普通审批政策、启动模式设置和 headless 控制需要分别定义产品与权限契约。

## Decisions

### 1. 控制 owner 与接纳点

`domain` 定义封闭的两种模式，`protocol` 提供类型化的显式目标设置，以及带 Session/Thread/Request 关联的内存确认。`Shift+Tab` 根据最近确认值发出相反的显式目标。每次独立操作使用新 RequestID，去重作用于现有活动及有界近期请求账本，宿主不自动重发。无变化的同值请求按空闲接纳规则报告现值。TUI 持有用于显示的最近确认值和至多一个独立待决请求，采用 `internal/tui/chat_shell.go` 的宿主代次过滤迟到消息。`default` 仅描述工作流，具体普通工具行为仍由原 Policy 决定。

`ChatSession` 在现有 `appendAdmissions` 和 resume permit 的互斥边界串行化“输入开始准备”“预留交接”“请求模式控制”。控制发出后短暂保留同一入口的接纳权，直到 AgentLoop 返回确定结论。互斥锁内只读写内存。先获得准备许可的输入完成在先，先获得切换接纳的命令保证后续可接纳输入看到新模式。AgentLoop 独立检查 accepting、空队列、无 starting/running/finishing worker、无独立压缩及无交接许可。若自动启动队列或压缩先到达 owner，切换返回忙碌。准备或状态事件只表示已观察事实，不能替代该检查。

以 owner 执行内存交换为线性化点。先准备可验证的确认，再在同一个 owner 检查调用方取消、宿主关闭、loop 关闭、journal 已不可继续和空闲状态，随后交换。接纳后的调用方取消仍观察确定结果。控制流的关联模式确认是 TUI 唯一成功投影来源，提交返回用于报告拒绝或不可交付的失败，不再作为一次成功投影。待决状态在确定拒绝、匹配确认或控制流结束时收口。关闭的宿主继续沿用排空和清理路径。打开普通模式时接入原交互审批，进入 Plan 时无需销毁已有的窄范围 grant，其执行资格由下面的模式准入优先限制。切换本身没有审批、网络或磁盘发布，复用已有异步控制路径和收口 owner。

### 2. 冻结操作与共享准入

复用 effort 的选择与捕获接缝。AgentLoop 在当前 `ProviderProfile` 中保存工作模式和实际推理设置，两种控制各自更新本维度，`controlEffort` 仍从启动配置计算实际推理设置。选定策略投影或接纳新操作时，基于该不可变 profile 选择规划指令和工具目录，由 `TurnInput` 和工具准入使用。活动回合中成功的 `/effort` 仍只影响后续操作，模式保持冻结。捕获值贯穿普通预检、候选、续采样、自动压缩后的普通请求与 Stop 纠正。独立压缩接纳冻结该选择用于替换窗口验证。模式只改变已有状态机的输入，复用同一规划、工具执行和恢复路径。接缝见 `internal/runtime/effort_loop.go` 的 `controlEffort` 和 `internal/runtime/agent_loop.go` 的 `startTurn`。

完整 `tool.Binding` 继续要求两家各十项注册完备，历史 native decoder 继续识别全部当前能力。工具层从同一不可变定义按类型化允许集合派生两家各七项的 Plan 快照，确定性排序、schema、内容指纹及严格解码共用现有实现。允许 Read、Glob、Grep、ReadArtifact、Skill、已有问卷和任务进度。Question 和 Task 仍是原独占屏障，任务清单只表达模型进度。Codex Plan 专门拒绝 `update_plan`（`core/src/tools/handlers/plan.rs:87–91`），EasyCode 保留双 Provider 已有进度工具。普通采样输入在构造或验证边界核对模式、指令、目录和 Provider 家族的一致性，错配在发请求前失败。摘要继续使用专用输入和已有完整注册，目录派生不改变恢复校验。接缝见 `internal/tool/catalog.go` 的 `ForPlanMode` 和 `internal/provider/provider.go` 的 `WithPlanMode`。

普通采样的 request compiler 与 reducer 使用同一个冻结目录。`commitCallSample` 在 `Policy.Decide` 所属的样本准入阶段用同一许可集合检查 typed `ReadyCall`。这个边界处理绕过 reducer 的程序输入，正常流式 decoder 先拒绝未声明 facade。任一不允许的调用使未提交样本整体失败，故先前已 Sync 的结果仍正确配对，失败样本不产生调用或配对义务。选择该边界还避免普通授权、Pre/Post 工具 Hook、候选准备和只读执行器读取同一违规样本。保持原有错误类型和安全英文摘要，测试同时核对零执行与回合收口。

Shell 的命令文本可以调用任意脚本，因此 Plan 整体排除 `Bash`/`exec_command`，文件调查通过专门的读取和搜索工具完成。用户已独立信任的工作区只读 Stop 和 PreCompact 仍按其 sandbox、取消及恢复契约执行。它们可以改变检查结果和反馈，运行中的工具白名单与模式保持冻结。正常的 journal、搜索 artifact 和任务更新仍可使用受控私有存储。

### 3. 模型指令、预算与缓存

共享一段固定规划指令供两家编译。先核对能从工作区查到的事实，再用结构化问卷解决仍会改变范围、接口或验收的选择。关键决定未解决时继续澄清。定稿以普通 Markdown 提供可独立阅读的完整方案，写明目标、主要改动、应复用的接缝、明确假设和端到端验证，区分已核实事实与待执行验证。修订发布完整替代稿并标明变化。任务清单和问卷回答只表达进度或选择，在 Plan 中的实施要求继续按规划处理。指令只用固定内容，不含当前项目、会话、时间、许可详情或 UI 状态。写作要求借鉴 Claude Code `utils/messages.ts:3156–3163,3278–3290` 与 Codex `collaboration-mode-templates/templates/plan.md:41–58,92–128`。

Anthropic 普通请求以顶层 `system` 传输指令，缓存开启后的 `cachedMessagesRequest` 使用同一值。OpenAI 的顶层 `instructions` 放在 HTTP 和 WebSocket 都复用的 `responsesInputFields`，增量请求仍显式发送，因为 `previous_response_id` 不继承先前指令。NativeHistory 和真实输入仍按原顺序编译，保留历史中的原生调用、结果与推理字节。普通模式省略该属性。为了保持单一事实来源，同一指令参与 `provider_profile` 的确定性内容身份和一次 token 估算，派生快照的定义进入 `tool_catalog`。当前 source 顺序及可见历史 footprint 不承担系统指令计量。 [Anthropic Messages API](https://platform.claude.com/docs/en/api/messages/create) · [OpenAI Responses API](https://developers.openai.com/api/reference/resources/responses/methods/create) · [OpenAI WebSocket 请求](https://developers.openai.com/api/docs/guides/websocket-mode)

进入或退出后更新 `/context` 对下一普通操作的估算。`/cache` 沿用最近成功普通采样的收据与对应来源，未采样的选择、失败和单次摘要保持该普通采样基线。当前模式指令与目录一起变化时，按既有顺序首个变化来源是 `provider_profile`，另可单独对账 `tool_catalog` 的 revision 和 fingerprint。OpenAI 在同一可续写连接上按实际属性变化报告 `request_properties_changed` 并走 full，已发生恢复、摘要、断线或失败时沿现有重置契约。使用同一 thread key 和连接 owner，正常同模式续写与未发送中间请求的往返按现有证据判断。Anthropic 在该模式实际目录的最后合格工具上选择既有 TTL marker，稳定项目/技能来源及原生窗口可分别对账。

复用摘要请求的专用 prompt、完整现有工具定义和 `tool_choice=none`，按实际摘要 bytes 独立执行预算及响应拒调用。Plan 普通策略用于前后有效窗口比较和之后恢复的普通采样。摘要 purpose 的实际请求 receipt 保持独立。`CompactionSource` 提取真实原生窗口、scope 和内容摘要的规则保持原样，供后续对账的原生 checkpoint 不存模式或指令身份。

### 4. 交互与恢复

在普通 composer 焦点接线 `Shift+Tab`，保留问卷返回、各 modal 和补全的键位优先级。`/plan` 是单行 TUI 本地命令，`/plan off` 明确退出。同一 TUI 识别规则用于补全、命令提交和 `@` 搜索排除。沿用 `/resume` 的宿主限定做法，避免把 `plan` 加入 `domain.IsReservedSkillSlashName`，因此 headless 仍可按原规则展开同名 Skill。正常 Tab 只填入名称，物理 Enter 触发本地控制。等待结果时冻结编辑、粘贴、再次提交和 `/resume`，窗口重绘继续。已显示真实 turn 或压缩的取消仍走现有入口。空闲待决时的 Esc 或 Ctrl+C 只取消本地模式命令等待接纳，保留接纳后的确认并且不设置 `interruptNext`。成功的 slash 操作清空命令草稿而不增加 recall，键盘切换保留正文与光标。校验拒绝、忙碌或接纳前取消统一恢复触发 Enter 前的编辑状态，包括通过补全提交的原命令前缀、参数、光标和 recall。

Plan footer 将 `Plan mode · read-only` 纳入现有 composer 行预算，有空间时提示 Shift+Tab。成功退出和新宿主启动或恢复进入会话时，在可提交前显示当前 `Default`。极窄布局可用 `Plan`、`Default` 短标签，优先给输入区和现有问卷、信任、取消操作留空间。独占弹层有空间时附模式标签，焦点和操作键不变，关闭后回到带当前模式的 composer。实际转录复用 Markdown 和原文阅读。失败或取消的流式片段沿用已有未完成显示，正文与任务进度都只作展示。

在普通模式输入 `/plan` 后，用户可以提交“调查会话恢复并给出改造方案”。模型以 Read/Grep 等收集证据，必要时用问卷确认，返回可阅读的 Markdown。用户继续发送修改意见会保持 Plan。发出 `/plan off` 只改变模式。若要精确实施某一版方案，用户可从转录或 Ctrl+O 原文取回该版，把关键决定或所需完整正文放进自己提交的实施要求。普通首采样前的自动压缩只摘要旧窗口，用户本次的独立输入按既有提交与预算规则保持原样。既有权限证据和审批适用于后续真实调用。本地验收让摘要故意省略旧方案中的区分性文本，再核对它由真实实施输入逐字带入，避免夹具恰好保留旧方案而掩盖缺口。接缝见 `internal/runtime/auto_compaction.go:14–58`。

以新的 ChatSession owner 作为模式生命周期边界。新会话、进程启动恢复和成功交接均从普通模式开始，失败或取消交接保留源会话状态。转录正文仍随当前 Session 原生数据恢复，可重入 Plan 继续工作。压缩后旧方案仍可从完整转录阅读，有效窗口只拥有既有摘要及其后的历史。headless 继续按既有 Skill 或普通正文规则解释输入，外部 JSONL 版本和消息词汇保持原样。

## Risks / Trade-offs

| 风险 | 验证与处理 |
| --- | --- |
| TUI 显示空闲时 Runtime 仍持有 worker，或草稿正在准备附件 | 用可控闸口覆盖 finishing、准备、队列和交接恢复。以实际控制 owner 的结果驱动界面。 |
| 保留旧 grant、读取证据或工具 Hook 时出现越界调用 | 用两家真实本地 SSE 夹具发出未声明 facade，并在 Runtime prepared sample 接缝注入已解码调用。检查整样本没有 native commit、ready、started、审批、Hook 或执行器副作用。 |
| 过滤工具后丢失历史 mutation 或缓存编译丢失模式指令 | 用普通工具成功的历史进入 Plan，对账原生调用/输出与 opaque 内容。双 Provider golden 覆盖实际 wire，两家重复编译、恢复后重选同模式以及 OpenAI WS 属性变化与原合法增量分别断言。 |
| 配置的上游拒绝含既往工具调用与较小声明目录的请求 | 本地协议服务验证实际请求与本地准入，证据范围不外推至自定义上游。上游拒绝沿现有失败收口，保留有效模式与已提交事实，继续请求仍使用 Plan 快照。 |
| 压缩或 Stop 反馈误进入普通模式 | 在确定顺序下断言摘要的独立 wire、前后窗口估算和回到普通采样的 Plan 白名单；检查用户确认前没有普通模式请求。 |
| 用户把 Plan 当成 shell dry run 或把退出看成一次性工具批准 | README 说明可用工具、退出后独立提交和既有授权规则。用 Plan 状态与实际请求计数直接验证转场。 |

## Migration Plan

按 [tasks.md](tasks.md) 实现并验收后同步主 specs、能力清单、Roadmap 与架构说明。部署沿用当前配置和 journal，首轮启动使用普通模式。原生正文随当前 Session 格式读写，用户可重新选择 Plan。验收记录保存实际环境和结论，原始请求、PTY 捕获与中间输出留在被忽略的本地目录。
