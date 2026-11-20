## MODIFIED Requirements

### Requirement: Cancel and quit behavior is unambiguous

普通聊天中输入补全可见且激活时，Esc SHALL 仅关闭候选并保留草稿、队列和活动 turn；相同名称未编辑时不得由流式事件或 resize 立即重开。没有激活补全的普通活动 turn 期间，Esc SHALL 优先取回 queued auto；没有可取回输入时定向 stop。Ctrl+C SHALL 定向 stop 当前 turn 而不退出进程。局部浮层先按自身 Esc 规则处理。approval overlay活跃时，Esc或Ctrl+C SHALL发送与显示中turn identity匹配的定向 stop，并立即禁用重复决定；TUI必须等待Runtime的resolved/terminal事件后关闭overlay，不得本地伪造批准、拒绝或文件结果。空闲且输入为空时，Ctrl+C SHALL退出应用。普通字符`q` MUST作为当前聚焦控件的输入处理，不能作为全局退出快捷键。

有审批、问卷或 Hook 信任等待时，活动交互 SHALL 优先于普通只读面板接受渲染与按键。被覆盖的面板、位置和草稿保留，阻塞交互解除后按原规则恢复。活动 turn 在只读面板打开时收到 Ctrl+C 仍 SHALL 定向取消一次，适用的独立手动压缩沿用其实际 compaction ID。

#### Scenario: Interrupt a streaming turn
- **WHEN** assistant 正在 streaming，用户按 Ctrl+C，或没有激活补全及可取回队列时按 Esc
- **THEN** TUI请求取消当前turn并保留已经显示的transcript
- **THEN** 清理结束后TUI返回可输入状态

#### Scenario: Interrupt an approval
- **WHEN** approval overlay活跃且用户按Esc或Ctrl+C
- **THEN** TUI发送匹配 turn 的 stop并阻止再次提交decision
- **THEN** Runtime收口后overlay关闭且Patch未被TUI直接执行

#### Scenario: Exit while idle
- **WHEN** TUI空闲、输入为空且用户按Ctrl+C
- **THEN** 应用正常退出并恢复终端状态

#### Scenario: Type the letter q
- **WHEN** 聊天输入框聚焦且用户输入`q`
- **THEN** `q`出现在草稿中且应用不退出

stop SHALL 返还未消费 auto，保留严格下一 turn；claimed 输入等待确定 resolution。返还内容进入有界不可执行托盘，不覆盖现有草稿、不截断、不自动重提；托盘与 queued/claimed 共用展示预算，取回/丢弃前不能无限接收新 auto。

#### Scenario: Return more than one draft can hold
- **WHEN** 停止返还多条合计超过 4 MiB 的输入
- **THEN** 托盘保留每条完整文本，用户逐条取回；当前草稿不变，不自动执行

#### Scenario: Close completion before retracting or stopping
- **WHEN** 活动 turn 中有 queued auto 和可见补全，用户连续按两次独立 Esc
- **THEN** 首次只关闭补全，第二次才按既有规则取回 queued auto；首次不产生停止或取回请求

#### Scenario: Keep direct control C cancellation
- **WHEN** 活动 turn 中有可见补全，用户按 Ctrl+C
- **THEN** 直接沿用当前 turn 定向停止规则，不吞掉中断或退出请求

#### Scenario: Give approval focus over a skills or hooks browser
- **WHEN** 用户在活动 turn 打开只读 Skills 或 Hooks 浏览，随后收到工具批准请求
- **THEN** 审批可见并接收按键，面板按键不泄漏到审批或 composer，解除审批后恢复既有浏览位置和草稿

### Requirement: Composer editing preserves grapheme boundaries

聊天 composer SHALL 支持在光标处插入文本、按扩展字素移动左右光标、用 Backspace 删除前一字素和 Delete 删除后一字素。光标 MUST 始终处于合法字素边界。Home/End 与 Ctrl+A/E SHALL 移到逻辑行首/尾，Ctrl+B/F SHALL 等价于左/右。输入补全激活时上下键 SHALL 选择候选；未激活补全时，上下键 SHALL 优先在软折行后的视觉行之间移动并保留目标显示列；较短行不得使光标越界或落在字素中间。水平移动、修改文本、resize 和历史切换 SHALL 重置目标列。

Alt+Enter 与 Ctrl+J SHALL 插入 LF。普通 Enter 且光标前一字素为反斜杠时 SHALL 以 LF 替换该反斜杠，不提交。输入中的 CRLF 和独立 CR SHALL 规范化为 LF，不做其他 Unicode 规范化。输入补全可见且激活时手动 Tab SHALL 只接受候选；当前合法目标因 Esc、粘贴或 modal 返回而被抑制且有可显示匹配时，手动 Tab SHALL 只重新打开列表；其他未激活补全时手动 Tab SHALL 保持无操作。粘贴中的 Tab 属于正文，不接受候选。空闲 Esc SHALL 先关闭激活补全并保留草稿；无激活补全的空闲 Esc 和空闲非空草稿 Ctrl+C SHALL 保持草稿，空闲空草稿 Ctrl+C 的退出语义不变。

普通 Enter 的反斜杠续行和显式换行 SHALL 在补全提交或内置 slash 命令分派之前处理。仅实际提交意图可执行内置命令或沿用补全提交，续行保留草稿其余内容。

#### Scenario: Edit inside a Unicode line
- **WHEN** 用户在中文、组合重音或 ZWJ emoji 之间移动、插入和前后删除
- **THEN** 操作以完整字素为单位，结果为合法 UTF-8，其他正文保持不变

#### Scenario: Preserve a preferred column
- **WHEN** 无激活补全，用户从长视觉行向下经过短行后继续进入长行
- **THEN** 短行光标停在合法可达边界，下一长行恢复原目标显示列

#### Scenario: Insert newlines without submitting
- **WHEN** 用户按 Alt+Enter、Ctrl+J，或在反斜杠后按普通 Enter
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
