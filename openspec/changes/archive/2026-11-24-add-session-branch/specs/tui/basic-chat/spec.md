## MODIFIED Requirements

### Requirement: Command progress and result use a compact Claude-style timeline

命令排队时TUI SHALL显示紧凑命令与`Waiting`；执行后主线显示`Running`和至多三行近期输出，相对 workdir、elapsed time 与累计输出规模在详情查看；完成后显示exit code或timeout/cancelled/failed状态以及truncated提示。普通视图的命令摘要最多两行和160个Unicode code points。Ctrl+O SHALL 打开完整对话 transcript，定位当前锚点或最新活动，↑/↓ 选择、Enter 进入详情，复用完整命令和冻结 preview；运行中只显示有界 recent 输出。

详情 SHALL 支持 Tab 切换 stdout/stderr、PgUp/PgDn 有界分页与 resize；Esc 从详情返回列表，再次 Esc 返回主线，Ctrl+O 退出 transcript 并恢复主线位置与草稿。命令活动时 Ctrl+C 仍定向中断，普通字符和 Enter 不得修改 composer 或提交并发 turn。显示副本 SHALL清理终端控制字符，不改变候选和冻结结果。

#### Scenario: Inspect a completed command
- **WHEN** 用户在命令终态后按 Ctrl+O 并选择该命令进入详情
- **THEN** 详情立即展示相同完整命令和冻结有界结果，存在当前线程已授权的保存引用时通过 typed 宿主查询异步获取首个输出页
- **THEN** View/resize 不读取外部资源，分页 I/O 只在异步查询执行，不改变候选或冻结模型输出预算

TUI 只能消费 typed RuntimeEvent 和宿主 typed transcript/detail 查询结果，不得直接读取 executor、Session、文件系统、PID 或 Provider native item。活动Exec期间composer不得提交并发turn，定向interrupt保持可用；只有Runtime已经完成进程cleanup并发布terminal后，TUI才可恢复idle输入状态。

#### Scenario: Render deterministic command progress
- **WHEN** 固定进度序列包含统一 tool started、两个 Exec progress 和 completed
- **THEN** 固定尺寸snapshot依次展示Waiting、Running、至多三行近期输出和最终 exit code，详情包含 elapsed/bytes

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
- **WHEN** 恢复后打开列表并查看属于当前线程的较早命令 stderr
- **THEN** 按 seq 找到历史命令、独立切流翻页，零 Provider 请求、零 tool ledger 写入

#### Scenario: Show partial storage
- **WHEN** artifact 只保存前段而预览包含末尾
- **THEN** 详情明确标注保存范围，stored EOF 不被显示为完整输出结束

主线 SHALL 按 tui/tool-transcript 覆盖全部已实现工具，提供确定性 Read/Search 分组和明显异常状态，并移除持续调试式 Status 行。审批焦点高于 transcript；浏览不得改变 composer、原生请求或工具执行事实。列表中的正文按 64 KiB UTF-8 页查询，不为折叠或 resize 构建全部历史行。旧 Ctrl+O 命令列表入口 SHALL 被本交互替换，不并存第二套浏览状态。

#### Scenario: Preserve draft across transcript and approval
- **WHEN** 用户带草稿浏览 Read/Search 组时出现审批
- **THEN** 审批获得焦点，浏览按键不处理；审批完成后恢复原浏览位置，退出后草稿不变

#### Scenario: Show a saved ancestor command without a local read action
- **WHEN** 用户在分支按 Ctrl+O 查看某个祖先线程已保存的 stdout 或 stderr
- **THEN** 详情保留冻结预览与保存状态，并给出生成该项的完整线程 ID。没有针对祖先的首屏原文查询或分页入口；选择本线程已授权的命令仍可分页。

### Requirement: Conversation continues in memory

一次成功 turn 结束后，TUI SHALL 返回可输入状态并允许提交下一轮。后续 turn SHALL 使用同一个会话级 Runtime 和启动时所选 Provider 的原生历史；Anthropic 与 OpenAI 历史不得互相转换或共享。

用户显式恢复兼容根线程或分支，或完成交互式分支切换时，TUI SHALL 在接受新输入前使用 `HistoryProjector` 的语义视图按 turn 顺序初始化 transcript，并继续使用已恢复的 Provider 原生历史。TUI MUST NOT 解析 JSONL payload、Provider wire、signature 或 encrypted reasoning。未指定 resume 的普通启动 SHALL 创建新 Session，不得隐式恢复最近历史；运行中切换 Provider 仍不属于本能力。

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

- **WHEN** 用户退出、重新启动 EasyCode 并显式选择兼容的根线程或分支
- **THEN** TUI 在进入 idle 前显示已完成 turn 的用户与 assistant 可见文本
- **THEN** 后续 turn 使用恢复后的对应 Provider native history

#### Scenario: Initialize the branch before enabling input

- **WHEN** 新分支完成交接，或者用户重启并恢复已有分支
- **THEN** TUI 在可提交下一输入前初始化已验证的目标 transcript，按原顺序展示继承及分支自身的可见历史
- **THEN** 下一 turn 使用分支对应 Provider 的原生历史，创建分支的命令与源草稿不会成为分支输入

#### Scenario: Restart without resume

- **WHEN** 用户退出并在未指定 resume 的情况下重新启动 EasyCode
- **THEN** TUI 进入一个新的空 Session，不自动选择旧历史

#### Scenario: Keep the selected provider fixed for a conversation

- **WHEN** 一个新建或恢复的 conversation 已按某个 Provider 创建
- **THEN** 后续 turn 继续使用该 Provider，且应用不会把另一 Provider 的原生历史注入当前请求

### Requirement: Composer editing preserves grapheme boundaries

聊天 composer SHALL 支持在光标处插入文本、按扩展字素移动左右光标、用 Backspace 删除前一字素和 Delete 删除后一字素。光标 MUST 始终处于合法字素边界。Home/End 与 Ctrl+A/E SHALL 移到逻辑行首/尾，Ctrl+B/F SHALL 等价于左/右。输入补全激活时上下键 SHALL 选择候选；未激活补全时，上下键 SHALL 优先在软折行后的视觉行之间移动并保留目标显示列；较短行不得使光标越界或落在字素中间。水平移动、修改文本、resize 和历史切换 SHALL 重置目标列。

Alt+Enter 与 Ctrl+J SHALL 插入 LF。普通 Enter 且光标前一字素为反斜杠时 SHALL 以 LF 替换该反斜杠，不提交。交互式 `/branch` 或 `/fork` 的明确标题末尾是例外，普通 Enter 提交时 SHALL 保留该反斜杠作为标题字符。输入中的 CRLF 和独立 CR SHALL 规范化为 LF，不做其他 Unicode 规范化。输入补全可见且激活时手动 Tab SHALL 只接受候选；当前合法目标因 Esc、粘贴或 modal 返回而被抑制且有可显示匹配时，手动 Tab SHALL 只重新打开列表；其他未激活补全时手动 Tab SHALL 保持无操作。粘贴中的 Tab 属于正文，不接受候选。空闲 Esc SHALL 先关闭激活补全并保留草稿；无激活补全的空闲 Esc 和空闲非空草稿 Ctrl+C SHALL 保持草稿，空闲空草稿 Ctrl+C 的退出语义不变。

普通 Enter 的反斜杠续行和显式换行 SHALL 在补全提交或内置 slash 命令分派之前处理，明确分支标题末尾的反斜杠按前述规则处理。仅实际提交意图可执行内置命令或沿用补全提交，续行保留草稿其余内容。

#### Scenario: Edit inside a Unicode line
- **WHEN** 用户在中文、组合重音或 ZWJ emoji 之间移动、插入和前后删除
- **THEN** 操作以完整字素为单位，结果为合法 UTF-8，其他正文保持不变

#### Scenario: Preserve a preferred column
- **WHEN** 无激活补全，用户从长视觉行向下经过短行后继续进入长行
- **THEN** 短行光标停在合法可达边界，下一长行恢复原目标显示列

#### Scenario: Insert newlines without submitting
- **WHEN** 用户按 Alt+Enter、Ctrl+J，或在普通正文的反斜杠后按普通 Enter
- **THEN** 只插入规定的换行，不产生提交、历史条目或 Provider 请求

#### Scenario: Keep multiline editing separate from exit
- **WHEN** 用户空闲且无 queued 输入时在非空草稿按 Esc 或 Ctrl+C
- **THEN** 草稿保持原样，应用不退出且不提交

#### Scenario: Complete only with an active visible candidate
- **WHEN** 聊天 composer 收到手动 Tab
- **THEN** 有激活可见候选时只完成当前选择；合法被抑制目标存在可显示匹配时只重开列表；其他无候选或候选因极小尺寸不可见时保持无操作；不提交或加载 Skill

#### Scenario: Keep newline keys literal while completing
- **WHEN** 补全期间按 Alt+Enter 或 Ctrl+J
- **THEN** 按既有规则插入 LF，不接受候选、不提交

#### Scenario: Continue a slash draft without executing it
- **WHEN** composer 输入 `/rename demo\` 且光标紧跟反斜杠，用户按普通 Enter
- **THEN** 反斜杠换成 LF，保留草稿，Session title 和 journal 不发生变化

#### Scenario: Keep a trailing backslash in an explicit branch title
- **WHEN** composer 输入 `/branch demo\` 或 `/fork demo\`，且光标位于标题末尾，用户按普通 Enter
- **THEN** 按本地分支规则提交包含原样反斜杠的标题。Alt+Enter 或 Ctrl+J 则只编辑草稿，不请求创建分支
