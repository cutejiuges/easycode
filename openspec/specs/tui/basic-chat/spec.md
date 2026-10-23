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

空闲状态下，用户 SHALL 能编辑单行文本，并通过 Enter 提交非空输入。提交后 TUI SHALL 在 transcript 中显示用户消息，并按 RuntimeEvent 顺序增量显示 assistant 文本；活动 turn 结束前 MUST NOT 启动第二个并发 turn。

Runtime发布`tool_approval_requested`时，TUI SHALL暂停聊天composer的键盘路由并进入与该request identity绑定的modal approval overlay。overlay解决前的普通字符、Enter、方向键和Tab不得修改或提交聊天草稿；已有草稿必须原样保留，并在overlay关闭后恢复。TUI只通过强类型`resolve_tool_approval`或定向interrupt响应当前request，不得直接调用Policy、Session、Provider或Patch executor。

#### Scenario: Submit a non-empty prompt
- **WHEN** 用户在空闲状态输入文本并按Enter
- **THEN** transcript增加该用户消息并启动一个turn
- **THEN** 输入区进入活动turn状态

#### Scenario: Ignore an empty submission
- **WHEN** 输入仅为空白且用户按Enter
- **THEN** TUI不启动Provider请求且transcript不增加空消息

#### Scenario: Render ordered text deltas
- **WHEN** Runtime依次产生多个`assistant_text_delta`
- **THEN** TUI将增量按事件顺序追加到当前assistant消息

#### Scenario: Preserve the composer under an approval overlay
- **WHEN** 用户已有未提交草稿且当前turn打开approval overlay
- **THEN** overlay期间的按键只路由到approval状态机，聊天草稿逐字节保持不变
- **THEN** overlay解决后原草稿重新可见且不会被自动提交

#### Scenario: Prevent concurrent submission
- **WHEN** 当前turn正在streaming或等待approval且用户按Enter
- **THEN** TUI不启动第二个turn
- **THEN** 当前turn继续保持唯一活动turn

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

命令排队时TUI SHALL显示紧凑命令与`Waiting`；执行后显示`Running`、相对workdir、elapsed time、累计输出规模和有界近期输出；完成后显示exit code或timeout/cancelled/failed状态以及truncated提示。普通视图的命令摘要最多两行和160个Unicode code points。本变更新增Ctrl+O命令详情，复用approval候选的完整命令及terminal-only typed result preview；运行中只显示有界recent输出。

详情 SHALL支持PgUp/PgDn滚动与resize，并保持当前命令identity与内容不变。Ctrl+O或Esc关闭详情；命令活动时Ctrl+C仍定向中断，普通字符和Enter不得修改composer或提交并发turn。显示副本 SHALL清理终端控制字符，不改变候选和冻结结果。

#### Scenario: Inspect a completed command
- **WHEN** 用户在命令终态后按Ctrl+O
- **THEN** 详情展示approval中相同的完整命令和terminal中相同的完整有界结果
- **THEN** 滚动或resize不读取外部资源、不改变候选且不扩展输出预算

TUI只能消费typed RuntimeEvent，不得读取executor、Session、PID或Provider native item。活动Exec期间composer不得提交并发turn，定向interrupt保持可用；只有Runtime已经完成进程cleanup并发布terminal后，TUI才可恢复idle输入状态。

#### Scenario: Render deterministic command progress
- **WHEN** 固定进度序列包含command started、两个output progress和completed
- **THEN** 固定尺寸snapshot依次展示Waiting、Running、近期输出、elapsed/bytes和最终exit code

#### Scenario: Show a truncated result safely
- **WHEN** 命令结果报告omitted bytes
- **THEN** TUI显示稳定省略提示与保留头尾，不尝试读取未授权artifact或绝对临时路径

#### Scenario: Wait for cleanup after interrupt
- **WHEN** 用户中断一个有子进程的活动命令
- **THEN** TUI保持取消中状态直到Runtime terminal，并且下一turn不会与旧进程cleanup重叠
