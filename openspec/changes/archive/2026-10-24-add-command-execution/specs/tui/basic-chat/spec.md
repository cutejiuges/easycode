## ADDED Requirements

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
