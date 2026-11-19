## Context

[提案](proposal.md)确定了聊天内同目录恢复的范围。启动选择器把 `StartupModel` 与一次性交付的 `pickerOwner` 组合起来。普通聊天独立运行 `tui.Model`。两个入口最后都调用同一个聊天装配函数。选择器已具备 Catalog 发现、只读预览和显式恢复。目前启动 owner 接受资源后持有到进程退出，候选 ChatSession 和文件查询也继承恢复请求 context。复用界面和恢复路径时，需要把可取消的请求、交付和活动会话的生命周期分开。[启动入口](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/session_picker.go#L77) · [选择器 owner](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/picker_owner.go#L80) · [聊天装配](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/app.go#L841)

基线为 `f6998f6`，设计中的源码行号引用该提交。已合入的任务进度和文件引用直接使用各自既有恢复链路。会话命名还需要 title 持久化和 Catalog 投影。Fork 需要线程谱系及原生历史继承规则。MCP 需要新能力的发现、信任、执行和缓存契约。聊天内切换在现有模型和存储边界上就能补齐一个完整流程。[能力清单](../../../../docs/roadmap/capability-status.md) · [路线图](../../../../docs/roadmap/product-roadmap.md)

### 两个参考实现

| 参考 | 观察到的实现 | EasyCode 取舍 |
| --- | --- | --- |
| Claude Code 2.1.88，sourcemap `a8a678c` | `restored-src/src/commands/resume/resume.tsx:107–210` 发现同仓库和 worktree 会话，列表排除当前和 sidechain，允许取消和扩大范围。外部项目分支提示另行启动并尝试复制命令。第 213–273 行先加载同仓库日志，日志非空时优先解析 UUID 并提供直接文件查找，之后才匹配标题。`restored-src/src/components/LogSelector.tsx:1127–1131,1227–1247` 提供 Ctrl+V 预览，`restored-src/src/components/SessionPreview.tsx:74–97` 支持在预览内确认切换。`restored-src/src/screens/REPL.tsx:1799–1935` 重建目标状态及可见对话。 | 采用无参数选择器、完整 ID 优先、列表排除当前、取消返回和跨目录另行启动的工作流。EasyCode 的完整 ID 直接定位 journal，预览沿用现有只读键位，目录按规范化 creation cwd 精确匹配，跨目录提示仅显示在终端。 |
| Codex `c0cc9fc820` | `codex-rs/tui/src/chatwidget/slash_dispatch.rs:146–166,312–314,1120–1122` 区分忙碌拦截、选择器入口和有参查找。`codex-rs/tui/src/resume_picker.rs:510–663,741–849,1106–1122` 在启动和聊天内复用同一个终端的 picker 和屏幕恢复。`codex-rs/tui/src/app/session_lifecycle.rs:1692–1734,1783–1801,1482–1484,1545–1555` 准备目标，再脱离旧会话，用目标历史重建聊天，切换不带初始 prompt。`codex-rs/tui/src/app/app_server_events.rs:204–232` 检查同一 thread 的旧关闭事件。 | 采用单终端、忙碌时明确拒绝、先准备再接管、目标历史先于输入和激活代次隔离。EasyCode 额外按自身控制契约等待 worker 清理，要求先处理未送达输入，并用现有双 Provider 原生恢复与独占 lease 验证目标。 |

## Goals / Non-Goals

- 在一个 Bubble Tea Program 和一个终端输入 reader 内支持正常启动、从启动选择器进入以及反复切换。
- 把空闲判断、取消、候选清理、接管和旧 lease 释放落实到明确的所有权边界。两种交互入口复用选择、预览、恢复和关闭逻辑。
- 复用当前 journal、Catalog schema、Provider wire 与 headless 协议。标题匹配、同时保留多个活动会话、休眠草稿池和跨目录接管属于独立设计。

## Decisions

### 1. 终端外层组合共享选择器和当前聊天

从 `StartupModel` 分离可复用的选择、预览、过滤和渲染逻辑。外层模型持有当前聊天与独占键盘焦点的选择器，根据启动或聊天内入口决定取消结果和目录政策。聊天内默认当前目录，`Ctrl+A` 扩展范围，`Ctrl+R` 刷新，`Ctrl+V` 只读预览并由 Esc 返回列表。沿用最多 200 条的原始查询预算，在显示前排除当前 thread。空范围仍保留切范围与刷新入口。每次重新打开先协调，以便刚关闭的来源再次出现。列表与预览阶段的可恢复错误保留查询及选择供重试。已经接收的正式恢复失败和接管前取消，清理确认后返回原聊天并显示原因。

跨目录时共用现有详情的安全转义和滚动容器，展示创建目录和完整 `easycode --resume <thread-id>` 提示。窄屏通过换行或滚动查看完整 ID，源与目标的 Provider 差异同样可查。指定 ID 的权威拒绝使用现有安全错误码和通用兼容说明。入口参数有专门配置时，提示需使用兼容配置即可。启动选择器保持既有跨目录确认、退出码与预览键位。[现有详情与单行诊断](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/tui/session_picker_view.go#L66)

`/resume` 在 composer 的显式提交路由中同步建立本地切换意图，先于文件和 Skill 准备。参数接受零项或一个按现有 identity parser 校验的完整 Thread ID。完整 ID 进入同一个目标操作，独立于列表的协调和页大小。无效 ID 或额外参数产生安全英文提示。交互命令、首词补全、文件候选识别与 `/skills` 共享同一 TUI 路由政策。同名 Skill 可用 `$resume` 或插件限定名。共享输入准备层和 headless 的同名 Skill 语义沿用当前规则。粘贴只编辑草稿，现有换行键继续换行，普通提交才处理恢复意图。取消或可恢复失败按当前尺寸还原提交前正文和光标，包括使用补全前的原稿。命令、查询、诊断与提示由宿主消费。[共享 Skill 准备](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/extension/skill/local.go#L223)

请求身份在一个终端生命周期内单调，候选结果绑定操作与目标。每次创建 ChatModel 分配独立激活代次，隔离异步输出订阅、查询和 hydration。嵌套 Batch 逐个绑定子命令结果。子模型的退出意图由外层校验代次后发出，避免框架先于模型消费旧的 Quit 消息。窗口变化由外层传递。选择器打开时继续投影来源必要的非输入消息，事件循环交付给选择器及交接进度的按键只在当前焦点消费，不缓存为目标输入。A → B → A 时第一次 A 的关闭或结果仍属于旧代次。[选择器路由](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/tui/session_picker.go#L79) · [聊天异步消息](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/tui/model.go#L228)

### 2. 由会话控制 owner 判断真正空闲

TUI 在申请接纳时拦住新编辑提交。授权、信任审查或问题界面保持原来焦点。存在待提交、取回、排队或返还输入时保留编辑内容并提示先处理。只读界面可关闭后发起切换。

在 `ChatSession` 和 `AgentLoop` 的本地生命周期边界申请强类型临时许可。会话的接纳记账覆盖 Append 开始准备 Skill 或文件附件到 owner 返回确定结果的整个区间。AgentLoop 串行确认仍可接纳、Runtime/journal 健康、队列及 claim 为空，并且普通 turn 和独立摘要 worker 已真实退出。自动摘要、工具和 Stop 随所属 turn 结束。手动摘要、effort 修改、Hook 信任或 reset、审批等变更共享冻结判据，包括直接发给 loop 的控制路径。只读投影和关闭继续可用。

许可在 owner 安装时同时判定取消，避免发送选择中的竞争让已取消请求取得许可。安装成功后返回确定结果。若 UI 已开始退出本次尝试，宿主仍收取实际结果并释放对应许可，等待释放确认后才恢复源输入。目标准备完成后，接管检查绑定源实例、许可和候选，确认源未终止、目标仍就绪以及终端尚未退出。源意外关闭走宿主失败清理；候选在接管前失效时，清理完成后仍可返回健康来源。接管成功后，许可持续到原实例关闭。[控制接收点](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/runtime/agent_loop.go#L158) · [直接 Hook 控制路径](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/runtime/hook_host.go#L30)

`CloseInput` 会永久进入排空。可释放的许可复用原输入控制 owner 和取消规则，只由交互宿主消费。[输入接纳](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/runtime/session.go#L54) · [回合 worker 收口](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/runtime/agent_loop.go#L683)

### 3. 目录校验与目标恢复共用连续 lease

发现、刷新和预览沿用既有安全读取、范围限制和串行取消。宿主在取租约前拒绝当前 thread。选择目标或直接输入 ID 后，持目标 lease 读取并验证 root identity、creation cwd、Provider family、wire 和 model。聊天内入口在同一个恢复服务上附加目标 cwd 约束，使用冻结启动路径的规范形式。目录和配置的权威校验先于 repair、补偿和 writer 启动。

活动终端冻结已经打开的工作区和私有数据根，包括隔离布局绑定的实际目录身份。发现、预览和候选装配复用固定根的句柄或与之绑定的描述符。必须重新打开时，核对实际拿到的句柄与冻结身份后从同一已验证句柄使用，拒绝路径指向新目录、symlink 或身份不一致。分别按路径打开 Repository、Workspace 和 Layout 即使得到相同路径字符串，也无法用于证明三者绑定同一目录。拆分长期根与每次恢复的结果记录，避免复用 Repository 时共享可变尝试状态。根的真正关闭在所有借用者退出之后。[现有装配顺序](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/app.go#L375) · [布局检查](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/session/artifact/layout_unix.go#L61) · [descriptor-bound journal](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/session/repository.go#L135)

复用 `sessionService.resume`、NativeHistory 恢复、usage 和 checkpoint 验证，以及现有 interrupted-tail、tool 与 Stop 补偿。目标 writer 持有由同一个 lease 转移的实际 handle。候选装配在目标 lease 内也可能创建产物目录并回收已验证的目标孤儿产物。该维护仅作用于目标，保留已引用内容。候选准备不采样，不重跑旧工具或 Hook。取消不回滚已经提交的 repair、补偿与合法产物维护，提示只根据实际证据描述其结果。[恢复服务](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/session_service.go#L99) · [目标产物维护](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/app.go#L456)

以类型区分恢复写入结果和最终关闭结果。恢复状态以实际接纳点、已确认的 Sync 与失败证据为准，区分写入尚未接收、存在确认提交、已接收但结果不确定。候选可能同时有较早的确认提交和后来一次不确定写入。关闭状态独立表示最终 Sync、实际文件关闭和资源释放的结果。writer 当前会把先前的 poisoned 状态与最终关闭错误合并返回，复用 Close 的现有聚合行为时需要提供结构化结果给交互 owner。此前 append 不确定且目标最终成功释放时，源可以继续，但仍须报告目标写入不确定。最终 Sync、文件关闭或其他资源清理失败进入错误退出，不能用先前 poison 状态掩盖。启动入口按原有失败与退出规则投影。差异不通过解析错误消息判断。[repair 接纳点](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/session/repair.go#L71) · [writer 关闭结果](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/session/writer.go#L274) · [现有恢复阶段](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/recovery_attempt.go#L3)

### 4. 一个交互宿主持有来源和最多一个候选

复用串行 picker worker 的请求代次、取消和清理机制，由终端宿主持有当前 `chatResources` 和至多一个候选。选中前仅使用发现或预览所需的短期资源。目标通过权威恢复、活动控制器就绪及首屏准备后才可交付。候选恢复使用可取消的操作 context。同步回放或原生恢复阶段在边界检查取消，已经接收的 durable I/O 等待确定结果。候选控制循环和文件 worker 使用由终端 owner 持有的独立会话 context。放弃候选时由 owner 取消并等待退出，接受成功后选择器的操作取消不传播至长期控制器。

复用 `TranscriptMain` 有界第一页查询与同一投影逻辑，在交付前验证目标初始可见内容。合法空会话也有确定的初始状态。活动源仍在时，候选控制器未就绪、已经关闭或必需首页读取/校验失败，按候选失败清理和返回。首屏与目标身份随接受后的模型切换展示，之后才启用目标输入。可展开正文与历史翻页沿用现有按需异步方式。[现有初始 transcript 查询](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/tui/transcript_state.go#L32) · [现有分页入口](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/transcript_queries.go#L134)

| 阶段 | 来源 | 目标与结果 |
| --- | --- | --- |
| 请求未接收或只读浏览 | 当前来源持有 lease，返回聊天时释放临时许可。 | 接收前取消不打开正式恢复。浏览仅协调目录或读取预览。 |
| 已接收并准备目标 | 保留原实例，暂停可变操作。 | 持连续目标 lease 验证并恢复。Esc 或 Ctrl+C 请求取消，等待已提交结果和资源清理。 |
| 目标不可用、准备失败或接管前取消 | 清理确认后恢复来源输入。 | 已提交的修复及补偿保留。可能发生写入时报告实际状态。写入不确定时关闭目标 writer，待后续显式恢复重新检查。 |
| 接管被接受 | 原控制器、文件准备、查询、Provider 和 writer 依次关闭，等待旧 lease 释放。 | 持有目标 lease，输入继续关闭。源关闭成功后挂载目标及首屏。进度页的 Esc 继续等待，Ctrl+C 或真实退出信号才进入宿主退出及清理。 |
| 清理报错或终端退出 | 停止接收，原 owner 跟踪实际完成。 | 如实报告资源错误，沿既有超时升级路径等待最终完成。源一旦开始关闭就不重新挂载，关闭失败时不开放目标输入。 |

来源和目标的 lease 在准备和交接中可短暂并存。每个 thread 保持单 writer。接管判断只进行有界内存转换，关闭和等待都在锁外。writer.Close 使用的等待 context 可能在最终 Sync 或 FD 关闭前超时。最终清理 owner 保留初次超时错误，继续等待同一 writer 的真实完成，包括 lease 释放，然后才关其余根句柄并发布错误退出或关闭结果。交接需要实际完成信号，不能用等待返回替代。交互入口复用终端输出错误监测与 `term.File` 能力，只有一个信号与终端生命周期 owner。外部退出、输出失败和当前会话异常均沿同一个显式关闭流程等待，接管过程中的其他结果不能重新启用输入。[现有关闭顺序](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/app.go#L753) · [writer 等待边界](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/session/writer.go#L171) · [终端输出与清理](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/session_picker.go#L56)

### 5. 启动来源共享，授权与历史按激活隔离

终端宿主冻结配置、规范化启动 cwd 和已绑定实际目录身份的根。项目指令在首次创建或恢复前建立一次，启动选择器在用户选择前仍无需读取指令。Hook 声明、Skill 目录和显式 Plugin 来源按初次装配的时机，在首次历史回放与补偿后冻结。分开 Hook 声明的加载与每次激活的 runner、信任实例装配。已加载 Skill/Plugin 目录只读复用，后续实际读取沿用来源重验。插件句柄的源校验依赖私有数据和工作区隔离布局，二者由同一个终端生命周期覆盖。结束时先关闭借用来源的会话与文件准备，再关闭插件来源和布局。每次激活分别拥有文件搜索、writer、Provider、对话、工具 ledger 与查询。共享装配入口也供现有非交互路径使用。[项目指令快照](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/app.go#L320) · [来源装配](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/app.go#L580) · [Hook 装配](https://github.com/cutejiuges/easycode/blob/f6998f693befab395835b66b8a26609f3ae177de/internal/app/hooks.go#L38)

目标任务状态、文件引用、产物授权和 transcript 从目标已验证事实恢复。输入召回来自该目标的已提交历史。Live cache 观测、WebSocket continuation、普通审批、Hook 信任、先前 Write 所需的 Read 资格和在途 UI 状态随原实例关闭。每次进入目标都从冻结配置和 revision 0 初始化 effort，`scope=process` 表示选择只在内存存在。取消保留原选择和观测。若工作区或私有存储的实际身份与冻结边界不符，在动作前失败关闭。

## Risks / Trade-offs

- 当前会话占用 lease 时 Catalog 可能报告 busy。保留带作用范围的提示，用实际恢复校验确定目标可用性。每次进入列表重新协调已关闭的来源。
- 取消可能晚于目标修复、补偿或产物维护。通过 journal 字节、seq 和重复恢复验证提交有且只有一次，验证源及目标已引用产物保持完整。
- 两次相同 thread ID 可以属于不同激活。通过 A → B → A 和确定性延迟的旧 close、查询结果验证消息隔离。
- 用确定性屏障区分终态发布与 worker 真正完成。分别在接收前、目标写入后及交接后验证取消、接管和 lease 释放。
- 以本地 mock Provider 对比等价启动恢复的完整逻辑请求 canonical bytes、原生项顺序、请求及稳定前缀 fingerprint。OpenAI 按现有规则检查 WebSocket 恢复的首次完整请求。运行 `make verify`、严格 OpenSpec 校验和 Darwin 真实 PTY，记录各平台已实际完成的编译及原生验收范围。

## Migration Plan

现有入口继续加载唯一当前格式。Session、Native、Checkpoint 和 Catalog 使用同一契约，无数据迁移步骤。
