# tui/basic-chat Specification

## Purpose

定义首个可用的 Bubble Tea 文本 Chat/REPL 交互，使用户能够通过已配置的 OpenAI Responses 或 Anthropic Messages 服务连续提交输入、观察流式回复、取消当前 turn 并从错误中恢复。

## Requirements

### Requirement: TUI starts with a validated OpenAI conversation

交互模式 SHALL 在进入可提交状态前加载并校验 provider family、`base_url`、API key 和 model。选择 `openai` 时 SHALL 创建 OpenAI Responses conversation；选择 `anthropic` 时 SHALL 创建 Anthropic Messages conversation。无效配置或未知 provider MUST 在网络请求前返回明确错误，应用不得静默切换 wire 或回退到另一 Provider。

#### Scenario: Start with valid OpenAI configuration

- **WHEN** 用户提供有效的 OpenAI family、API prefix、API key 和 model
- **THEN** TUI 使用 OpenAI Responses conversation 进入空闲且可输入状态

#### Scenario: Start with valid Anthropic configuration

- **WHEN** 用户提供有效的 Anthropic family、API prefix、API key 和 model
- **THEN** TUI 使用 Anthropic Messages conversation 进入空闲且可输入状态

#### Scenario: Reject incomplete configuration

- **WHEN** base URL、API key 或 model 缺失或无效
- **THEN** 应用在发起模型请求前返回 `invalid_configuration`
- **THEN** 输出不包含 API key

#### Scenario: Reject unsupported provider in this slice

- **WHEN** 配置包含 Anthropic 和 OpenAI 以外的 provider family
- **THEN** 应用在网络请求前返回明确的配置或 provider unavailable 错误
- **THEN** 应用不静默选择 OpenAI、Anthropic 或其他 wire

### Requirement: User can submit and observe a streaming text turn

空闲状态下，用户 SHALL 能编辑多行文本，并通过普通 Enter 提交合法非空白输入；换行快捷键与反斜杠换行规则 SHALL 优先于提交。正文 SHALL 保留输入规范化后的缩进、内部换行、尾部空格和尾换行，不得以 trim 改写；空白判断仅用于拒绝空输入。等待提交接收及活动 turn 结束前 MUST NOT 发起第二次提交或把输入排入后续 turn。

提交接收成功后 TUI SHALL 在 transcript 中显示恰好一条用户消息，并按 RuntimeEvent 顺序增量显示 assistant 文本。接收仅表示进程内 admission，不得声称 Session 已 durable。接收失败 SHALL 恢复原草稿、光标和历史浏览状态，且不增加已接收用户消息或输入历史。接收成功后的 turn 失败 MUST NOT 自动恢复 pending 草稿、重新提交或重试工具。

Runtime发布`tool_approval_requested`时，TUI SHALL暂停聊天composer的键盘路由并进入与该request identity绑定的modal approval overlay。overlay解决前的普通字符、Enter、方向键、Tab和粘贴不得修改或提交聊天草稿；已有草稿、光标和历史浏览状态必须保留，并在overlay关闭后恢复。TUI只通过强类型`resolve_tool_approval`或定向interrupt响应当前request，不得直接调用Policy、Session、Provider或Patch executor。

#### Scenario: Submit a non-empty prompt
- **WHEN** 用户在空闲状态输入文本并按普通 Enter，且不匹配换行规则
- **THEN** 输入进入等待接收状态，最多发起一次提交
- **THEN** 接收成功后 transcript 增加该用户消息并开始消费 turn 事件

#### Scenario: Ignore an empty submission
- **WHEN** 输入仅为空白且用户按普通 Enter
- **THEN** TUI 不启动 Provider 请求且 transcript 不增加空消息，草稿保持原样

#### Scenario: Render ordered text deltas
- **WHEN** Runtime依次产生多个`assistant_text_delta`
- **THEN** TUI将增量按事件顺序追加到当前assistant消息

#### Scenario: Preserve the composer under an approval overlay
- **WHEN** 用户已有未提交草稿且当前turn打开approval overlay
- **THEN** overlay期间的按键和粘贴只路由到approval状态机，聊天草稿逐字节保持不变
- **THEN** overlay解决后原草稿、光标和浏览状态恢复且不会自动提交，resize只允许调整显示布局

#### Scenario: Prevent concurrent submission
- **WHEN** 提交正在等待接收、当前turn正在streaming或等待approval且用户按Enter
- **THEN** TUI不发起第二次提交、不排队，当前输入保持唯一执行路径

#### Scenario: Preserve multiline content exactly after normalization
- **WHEN** 草稿包含首行缩进、多行代码、尾部空格和尾换行并被接收
- **THEN** 提交的正文与草稿内容一致，不进行外围空白删除或 Unicode 规范化
- **THEN** 两种 Provider 都接收同一正文，终端尺寸与光标位置不进入模型请求

#### Scenario: Recover a rejected submission
- **WHEN** 提交接收返回错误
- **THEN** TUI 恢复提交前草稿、光标和历史浏览状态并显示安全英文错误
- **THEN** transcript 和输入历史中不增加该次输入，也不自动重试

#### Scenario: Keep an accepted failed turn distinct from rejection
- **WHEN** 提交已被接收，随后 Provider 失败、事件流异常或用户取消
- **THEN** 已显示的用户消息和输入历史保留，不自动恢复为待提交草稿或启动第二次执行

#### Scenario: Interrupt while admission is pending
- **WHEN** 提交接收尚未返回时用户按 Esc 或 Ctrl+C
- **THEN** TUI 保留取消意图，若接收成功则沿用当前会话中断路径并等待清理
- **THEN** 若接收失败则恢复草稿并清除取消意图，不误取消后续 turn

### Requirement: Conversation continues in memory

一次成功 turn 结束后，TUI SHALL 返回可输入状态并允许提交下一轮。后续 turn SHALL 使用同一个会话级 Runtime 和启动时所选 Provider 的原生历史；Anthropic 与 OpenAI 历史不得互相转换或共享。

用户显式恢复 root thread 时，TUI SHALL 在接受新输入前使用 `HistoryProjector` 的语义视图按 turn 顺序初始化 transcript，并继续使用已恢复的 Provider 原生历史。TUI MUST NOT 解析 JSONL payload、Provider wire、signature 或 encrypted reasoning。未指定 resume 的普通启动 SHALL 创建新 Session，不得隐式恢复最近历史；运行中切换 Provider 仍不属于本能力。

#### Scenario: Submit a second turn

- **WHEN** 任一已支持 Provider 的第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** 所选 Provider 使用第一轮已提交的自身原生历史构建请求

#### Scenario: Submit a second OpenAI turn

- **WHEN** 使用 OpenAI conversation 的第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** OpenAI Provider 使用第一轮已提交的 Responses 原生历史构建请求

#### Scenario: Submit a second Anthropic turn

- **WHEN** 使用 Anthropic conversation 的第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** Anthropic Provider 使用第一轮已提交的 Messages 原生历史构建请求

#### Scenario: Restart the process

- **WHEN** 用户退出、重新启动 EasyCode 并显式选择兼容的 root thread
- **THEN** TUI 在进入 idle 前显示已完成 turn 的用户与 assistant 可见文本
- **THEN** 后续 turn 使用恢复后的对应 Provider native history

#### Scenario: Restart without resume

- **WHEN** 用户退出并在未指定 resume 的情况下重新启动 EasyCode
- **THEN** TUI 进入一个新的空 Session，不自动选择旧历史

#### Scenario: Keep the selected provider fixed for a conversation

- **WHEN** 一个新建或恢复的 conversation 已按某个 Provider 创建
- **THEN** 后续 turn 继续使用该 Provider，且应用不会把另一 Provider 的原生历史注入当前请求

### Requirement: Cancel and quit behavior is unambiguous

活动 turn streaming期间，Esc或Ctrl+C SHALL取消当前turn而不直接退出进程。approval overlay活跃时，Esc或Ctrl+C SHALL发送与显示中turn identity匹配的定向interrupt，并立即禁用重复决定；TUI必须等待Runtime的resolved/terminal事件后关闭overlay，不得本地伪造批准、拒绝或文件结果。空闲且输入为空时，Ctrl+C SHALL退出应用。普通字符`q` MUST作为当前聚焦控件的输入处理，不能作为全局退出快捷键。

#### Scenario: Interrupt a streaming turn
- **WHEN** assistant正在streaming且用户按Esc或Ctrl+C
- **THEN** TUI请求取消当前turn并保留已经显示的transcript
- **THEN** 清理结束后TUI返回可输入状态

#### Scenario: Interrupt an approval
- **WHEN** approval overlay活跃且用户按Esc或Ctrl+C
- **THEN** TUI发送匹配turn的interrupt并阻止再次提交decision
- **THEN** Runtime收口后overlay关闭且Patch未被TUI直接执行

#### Scenario: Exit while idle
- **WHEN** TUI空闲、输入为空且用户按Ctrl+C
- **THEN** 应用正常退出并恢复终端状态

#### Scenario: Type the letter q
- **WHEN** 聊天输入框聚焦且用户输入`q`
- **THEN** `q`出现在草稿中且应用不退出

### Requirement: Failure is visible and recoverable

provider、stream protocol、timeout 或配置以外的 turn 错误 SHALL 以安全英文错误摘要显示。失败不得清除已有 transcript，且清理完成后用户 SHALL 能提交新的 turn。

#### Scenario: Display provider failure

- **WHEN** Runtime 产生 `turn_failed`
- **THEN** TUI 显示不含 secret 和请求正文的错误摘要
- **THEN** 已完成和已显示的消息仍保留

#### Scenario: Submit after failure

- **WHEN** 失败 turn 已完成清理且用户提交新输入
- **THEN** TUI 可以启动新的 turn
- **THEN** 失败 turn 的部分 assistant output 不进入 Provider 已提交历史

### Requirement: File approval uses a Claude-style deterministic overlay

TUI SHALL将typed Patch approval request投影为居中的modal overlay，按稳定顺序显示workspace相对路径、稳定英文风险摘要、完整有界unified diff和内核提供的三个选择：`Yes`、`Yes, allow all edits during this session`、`No`。初始焦点 SHALL为`No`；上下方向键在允许选择间循环，Enter提交当前选择，TUI不得根据终端尺寸或Provider来源改变决定语义。

只有`No`聚焦时Tab SHALL切换到拒绝反馈编辑器，提示用户说明“应该怎么做”；反馈为合法UTF-8，去除外围空白后至多16 KiB。反馈编辑器的Enter提交deny，Esc先退出反馈编辑器并保留overlay，再次Esc按中断语义处理。`Yes`与session allow不接受反馈，也不得提供候选内联编辑。Runtime声明的decision集合缺失、未知或与当前overlay identity不一致时，TUI必须失败关闭而不是猜测默认批准。

固定尺寸不足以完整显示diff时，overlay SHALL在自身viewport中滚动并持续显示路径、选择和当前位置；不得截断diff后仍允许批准。颜色不可用时仍须用文本、边框和焦点标记区分新增、删除和当前选择。resize只改变布局，不改变request、草稿、反馈或焦点语义。

Write approval SHALL 沿用同一 modal、默认 No、反馈、键盘隔离、滚动与 resize 契约，但使用明确的 Create file / Overwrite file 标题。create 展示完整新内容或全新增 diff、空文件说明、行/字节数和缺失父目录清单；overwrite 展示完整变化 diff。内容中控制字符必须可见转义而不执行终端控制。

Write 的 session allow 标签 SHALL 明确仅授权本 Runtime 的创建和覆盖，不沿用 Patch 的 all edits 标签，也不能授权 Patch/Exec。TUI MUST 仅渲染已验证的封闭 presentation，不访问文件系统、不修改候选、不自行推断操作或扩充 decisions。approval resolved 只表示决议已收口，不表示文件写入成功。

#### Scenario: Render an approval at a normal terminal size
- **WHEN** TUI在固定尺寸收到合法Patch approval request
- **THEN** snapshot包含相对路径、完整diff、三个有序选择和默认聚焦的`No`
- **THEN** snapshot不包含workspace绝对路径、完整未变文件或Provider wire

#### Scenario: Allow one patch
- **WHEN** 用户聚焦`Yes`并按Enter
- **THEN** TUI提交匹配identity和fingerprint的`allow_once`response且禁用重复提交

#### Scenario: Allow edits for the current Runtime session
- **WHEN** 用户聚焦session选项并按Enter
- **THEN** TUI提交`allow_session`，其标签明确限定为当前session
- **THEN** TUI不写用户配置、项目配置或持久权限规则

#### Scenario: Deny with guidance
- **WHEN** 用户在`No`上按Tab、输入合法反馈并提交
- **THEN** TUI提交deny与该反馈，聊天composer内容不变
- **THEN** overlay关闭后模型可通过新的tool call提出修订方案

#### Scenario: Scroll a large bounded diff
- **WHEN** 完整合法diff超过当前overlay viewport但未超过内核大小边界
- **THEN** 用户可以滚动查看全部diff且选择始终可达
- **THEN** resize后显示内容仍来自同一candidate fingerprint

#### Scenario: Fail closed on an unsupported decision set
- **WHEN** approval event缺少`No`、含未知decision或identity与当前活动turn不匹配
- **THEN** TUI不发送allow response并以安全错误请求中断当前turn

#### Scenario: Render a new empty file
- **WHEN** Write create 候选 content 为空并需要创建父目录
- **THEN** 固定 snapshot 包含 Create file、空文件说明、全部目录、Write 专属选择和默认 No

#### Scenario: Render overwrite distinctly
- **WHEN** Write 覆盖一个已有空文件或非空文件
- **THEN** 标题为 Overwrite file，显示同一候选的完整 diff，不能将空 base 显示成 create

#### Scenario: Preserve draft while scrolling Write preview
- **WHEN** 在小窗口中滚动、resize 或输入拒绝反馈
- **THEN** 聊天草稿保持原样且按键不会提交给模型，identity 和 fingerprint 不改变

#### Scenario: Keep rendering free of filesystem effects
- **WHEN** 多次渲染同一个 Write approval
- **THEN** 不读取目标、不探测目录、不重新生成候选或授予权限
### Requirement: Command approval follows Claude Code interaction semantics

Exec安全摘要 SHALL简洁说明仅支持前台命令，主动脱离的进程可能继续运行；不得将sandbox或进程组描述成任意进程树的回收保证。

TUI SHALL将typed Exec approval request投影为modal overlay，显示完整命令、workspace相对workdir、timeout与安全能力摘要，并只显示内核提供的`Yes`、`No`和`Abort turn`。初始焦点 SHALL为`No`；方向键在允许选择间循环，Enter提交匹配identity与fingerprint的决定，普通字符不得修改聊天composer。TUI不得提供session-wide命令授权、内联修改命令、sandbox override或模型未请求的环境设置。

命令超过viewport时overlay SHALL在自身区域滚动，并持续显示workdir、timeout、选择和当前位置；resize只改变布局，不改变候选、焦点或决定语义。无法验证decision集合、identity或fingerprint时 MUST失败关闭并请求中断当前turn。

#### Scenario: Render a command approval
- **WHEN** TUI在固定尺寸收到合法Exec approval request
- **THEN** snapshot包含完整命令、相对workdir、timeout、三个有序选择和默认聚焦的`No`
- **THEN** snapshot不包含绝对workspace、PID、环境值或Provider wire

#### Scenario: Allow one command
- **WHEN** 用户聚焦`Yes`并按Enter
- **THEN** TUI提交matching `allow_once` response并禁用重复提交

#### Scenario: Deny without mutating the command
- **WHEN** 用户选择`No`
- **THEN** 当前命令不执行，聊天composer保持原样，模型只能通过新tool call提出新命令

### Requirement: Command progress and result use a compact Claude-style timeline

命令排队时TUI SHALL显示紧凑命令与`Waiting`；执行后显示`Running`、相对workdir、elapsed time、累计输出规模和有界近期输出；完成后显示exit code或timeout/cancelled/failed状态以及truncated提示。普通视图的命令摘要最多两行和160个Unicode code points。Ctrl+O SHALL 打开历史命令列表并默认选中最新项，↑/↓ 选择、Enter 进入详情，复用完整命令和冻结 preview；运行中只显示有界 recent 输出。

详情 SHALL 支持 Tab 切换 stdout/stderr、PgUp/PgDn 有界分页与 resize；Esc 从详情返回列表，再次 Esc 关闭，Ctrl+O 直接关闭覆盖层。命令活动时 Ctrl+C 仍定向中断，普通字符和 Enter 不得修改 composer 或提交并发 turn。显示副本 SHALL清理终端控制字符，不改变候选和冻结结果。

#### Scenario: Inspect a completed command
- **WHEN** 用户在命令终态后按 Ctrl+O 并选择该命令进入详情
- **THEN** 详情立即展示相同完整命令和冻结有界结果，存在保存引用时通过 typed 宿主查询异步获取首个输出页
- **THEN** View/resize 不读取外部资源，分页 I/O 只在异步查询执行，不改变候选或冻结模型输出预算

TUI 只能消费 typed RuntimeEvent 和宿主 typed 命令查询结果，不得直接读取 executor、Session、文件系统、PID 或 Provider native item。活动Exec期间composer不得提交并发turn，定向interrupt保持可用；只有Runtime已经完成进程cleanup并发布terminal后，TUI才可恢复idle输入状态。

#### Scenario: Render deterministic command progress
- **WHEN** 固定进度序列包含command started、两个output progress和completed
- **THEN** 固定尺寸snapshot依次展示Waiting、Running、近期输出、elapsed/bytes和最终exit code

#### Scenario: Show a truncated result safely
- **WHEN** 命令结果报告omitted bytes
- **THEN** TUI显示稳定省略提示与保留头尾，不尝试读取未授权artifact或绝对临时路径

#### Scenario: Wait for cleanup after interrupt
- **WHEN** 用户中断一个有子进程的活动命令
- **THEN** TUI保持取消中状态直到Runtime terminal，并且下一turn不会与旧进程cleanup重叠

列表每页 SHALL 不超过 50 项，保存内容单页请求不超过 64 KiB 原始 bytes，最多缓存 4 页、每页展示文本不超过 256 KiB。查询 MUST 绑定当前 thread 与选择 generation，切换或关闭后丢弃旧回复。保存完整性与预览省略 SHALL 分别显示；missing/corrupt 仍保留原冻结 preview，用户查看 MUST NOT 触发模型请求或工具 ledger。

#### Scenario: Ignore a late page
- **WHEN** 用户从命令 A 切换到 B 后 A 的页回复到达
- **THEN** B 的内容与游标不受影响，旧请求被取消并由 owner 收尾

#### Scenario: Browse restored commands
- **WHEN** 恢复后打开列表并查看较早命令 stderr
- **THEN** 按 seq 找到历史命令、独立切流翻页，零 Provider 请求、零 tool ledger 写入

#### Scenario: Show partial storage
- **WHEN** artifact 只保存前段而预览包含末尾
- **THEN** 详情明确标注保存范围，stored EOF 不被显示为完整输出结束

### Requirement: Composer editing preserves grapheme boundaries

聊天 composer SHALL 支持在光标处插入文本、按扩展字素移动左右光标、用 Backspace 删除前一字素和 Delete 删除后一字素。光标 MUST 始终处于合法字素边界。Home/End 与 Ctrl+A/E SHALL 移到逻辑行首/尾，Ctrl+B/F SHALL 等价于左/右。上下键 SHALL 优先在软折行后的视觉行之间移动并保留目标显示列；较短行不得使光标越界或落在字素中间。水平移动、修改文本、resize 和历史切换 SHALL 重置目标列。

Alt+Enter 与 Ctrl+J SHALL 插入 LF。普通 Enter 且光标前一字素为反斜杠时 SHALL 以 LF 替换该反斜杠，不提交。输入中的 CRLF 和独立 CR SHALL 规范化为 LF，不做其他 Unicode 规范化。手动 Tab SHALL 保持无操作；粘贴中的 Tab 属于正文。空闲 Esc 和非空草稿 Ctrl+C SHALL 保持草稿，空草稿 Ctrl+C 的退出语义不变。

#### Scenario: Edit inside a Unicode line
- **WHEN** 用户在中文、组合重音或 ZWJ emoji 之间移动、插入和前后删除
- **THEN** 操作以完整字素为单位，结果为合法 UTF-8，其他正文保持不变

#### Scenario: Preserve a preferred column
- **WHEN** 用户从长视觉行向下经过短行后继续进入长行
- **THEN** 短行光标停在合法可达边界，下一长行恢复原目标显示列

#### Scenario: Insert newlines without submitting
- **WHEN** 用户按 Alt+Enter、Ctrl+J，或在反斜杠后按普通 Enter
- **THEN** 只插入规定的换行，不产生提交、历史条目或 Provider 请求

#### Scenario: Keep multiline editing separate from exit
- **WHEN** 用户在非空草稿按 Esc 或 Ctrl+C
- **THEN** 草稿保持原样，应用不退出且不提交

### Requirement: Explicit text paste is atomic and bounded

对终端已解码并显式标识为 paste 的文本，TUI SHALL 在光标处原子插入规范化后的整个文本，内部换行、Tab、Esc、Ctrl+C 等内容 MUST NOT 被解释成操作键。随后到达的独立普通 Enter SHALL 按普通提交规则处理。空 paste SHALL 无操作。

草稿 MUST 不超过 4 MiB UTF-8 字节；插入或粘贴使规范化后的正文超限时 SHALL 整次拒绝、保持此前草稿及光标并显示不含正文的安全英文错误，不得截断后假装成功。

支持说明 MUST 明确：安全粘贴保证基于显式 bracketed paste/已解码事件；无此标记的输入按普通按键处理。不得声称 Shift+Enter 在所有终端可用、原始终端字节无损或草稿限额覆盖解码前缓冲。输入区帮助 SHALL 提供 Alt+Enter/Ctrl+J 等明确支持的换行方式。

#### Scenario: Paste code containing newlines and control characters
- **WHEN** 空闲 composer 收到含 CRLF、独立 CR、LF、Tab、Esc 和 Ctrl+C 的显式 paste
- **THEN** 换行规范化后一次插入，控制字符作为正文保留且不触发提交、取消或审批
- **THEN** 只有后续独立的操作键才能产生相应行为

#### Scenario: Reject an oversized paste atomically
- **WHEN** 新 paste 会使草稿超过 4 MiB
- **THEN** 原草稿、光标和历史浏览状态不变，显示安全错误，不生成部分新正文

#### Scenario: Accept the exact input limit
- **WHEN** 规范化后的输入恰好为 4 MiB 且不是全空白
- **THEN** 草稿可被编辑与提交，不因字符数或显示转义长度被错误拒绝

#### Scenario: Paste into a modal or an active turn
- **WHEN** 审批、命令详情、提交等待或活动 turn 占据输入焦点时收到 paste
- **THEN** 聊天草稿不变，不提交、不排队，也不把 paste 内部控制字符解释为批准或中断

### Requirement: Session input recall is bounded and preserves the working draft

TUI SHALL 提供当前 thread 的内存输入历史：以已有恢复语义视图中的用户文本初始化，并加入本进程接收成功的输入。不得访问全局输入历史、另写历史文件或从 UI 历史反向构建 Provider 请求。种子输入 SHALL 使用相同换行规范化；单条超限或全空白种子不进入 recall，不影响既有 transcript。

历史 SHALL 同时限制为最多 100 条及正文合计 8 MiB；相邻完全相同的输入折叠，超过任一预算按最旧优先淘汰。历史淘汰不影响 Session 事实。

在首视觉行按 Up SHALL 进入较旧历史，在末视觉行按 Down SHALL 进入较新历史；普通上下移动优先。首次浏览 SHALL 保存当前草稿与光标，包括纯空白草稿。越过最新条目 SHALL 恢复保存的草稿与光标。上翻召回后光标放在首视觉行最后一个可停靠位置，下翻召回后放在正文末尾。编辑召回文本 SHALL 结束浏览并将其作为当前工作草稿，不改写历史条目；这时之前保存的浏览前草稿被放弃。无条目、最旧边界以及非浏览状态底部 Down SHALL 无操作。

#### Scenario: Return from history to the original draft
- **WHEN** 用户浏览历史且没有修改召回文本，然后下翻越过最新条目
- **THEN** 原草稿包括空白、光标位置均恢复，不自动提交

#### Scenario: Adopt an edited recalled prompt
- **WHEN** 用户修改一个召回条目
- **THEN** 修改后的内容成为当前工作草稿并结束历史浏览，已存历史不变
- **THEN** 该修改只有在新的提交接收成功后才进入输入历史

#### Scenario: Evict old entries without changing the journal
- **WHEN** 新条目使条数或总字节预算超限
- **THEN** 最旧条目被淘汰直到同时满足预算，Session records 不变

#### Scenario: Seed recall from a resumed thread
- **WHEN** 应用已提供恢复后的语义历史视图
- **THEN** TUI 从其中用户文本生成有界 recall，不读取 journal 或 Provider native payload

### Requirement: Composer layout is safe deterministic and cursor aware

输入区 SHALL 根据终端宽高软折行，最多显示 8 行正文并在尺寸受限时进一步收缩，光标始终在可见区域。resize MUST 不改变正文、字节光标或历史条目。窄屏 SHALL 优先显示光标和正文，再按可用空间显示提示；尚未取得尺寸时按 80x24 渲染。光标移动与显示 SHALL 使用一致的字素/显示列映射，纯 render 不执行 I/O 或修改业务状态。

LF SHALL 显示为换行，Tab SHALL 按 4 列制表位展示；C0（LF/Tab 除外）、DEL、C1、双向控制字符 SHALL 显示为可见转义，不得执行原始终端控制。相同安全显示规则 SHALL 用于已提交用户消息。展示副本 MUST NOT 回写正文、Provider 请求、Session 或 cache fingerprint。无色模式 SHALL 仍提供可见的插入位置，转义显示和光标标记不得造成正文光标落在字素中间。

#### Scenario: Resize a long draft
- **WHEN** 多行或长单行草稿在正常、窄屏和极小尺寸之间切换
- **THEN** 正文和字节光标不变，视口调整使插入位置可见且不出现负尺寸或越界

#### Scenario: Render a terminal escape safely before and after submission
- **WHEN** 草稿包含终端控制字符并随后被接收
- **THEN** 输入区与用户 transcript 均显示可见转义，不输出可执行的原始控制字符
- **THEN** 提交正文保留输入规范化后的原值

#### Scenario: Keep UI state out of requests
- **WHEN** 同一正文经过不同尺寸、光标移动或历史浏览后被提交
- **THEN** 双 Provider 的对应请求 canonical bytes 与稳定前缀不受这些 UI 操作影响
- **THEN** 同一已提交历史在 uninterrupted 与 restored 路径生成等价的后续请求、原生 item 顺序与 fingerprint

#### Scenario: Render without color
- **WHEN** 输入区以无色方式显示含中文、emoji 和软折行的草稿
- **THEN** 仍能识别插入位置，显示列计算与光标移动一致
