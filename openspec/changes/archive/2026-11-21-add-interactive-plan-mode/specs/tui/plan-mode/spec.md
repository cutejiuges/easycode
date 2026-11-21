## Purpose

定义终端里开始和离开工作区只读规划的入口、模式提示及计划阅读体验，使用户可以在同一对话中收集信息、修订方案，并由自己决定何时开始实施。

## ADDED Requirements

### Requirement: The composer offers explicit local planning controls

对于单行输入，`/plan` SHALL 进入 Plan，已在 Plan 时报告同一模式。`/plan off` SHALL 返回普通模式，已在普通模式时报告同一模式。其他参数和作为 `/plan` 参数的多行正文 MUST 在本地拒绝并保留草稿。当前焦点为没有弹层、补全选择或待提交操作的普通编辑器时，`Shift+Tab` SHALL 请求在这两个模式间切换并保留现有草稿、光标和输入历史。接纳的 slash 命令只消费命令草稿，无模型调用或聊天历史条目。

等待模式确认期间，普通编辑区 SHALL 冻结原草稿，拒绝编辑、粘贴、再次提交和会话交接，继续处理尺寸和既有控制消息。宿主只在收到匹配当前交互实例与请求的确认后更新模式。命令错误、忙碌拒绝或接纳前取消 MUST 保留当前已确认模式，并恢复操作前草稿、光标和 recall 位置。若由补全 Enter 触发，则以按键前的原稿为准。空闲待决时 Esc 和 Ctrl+C SHALL 请求取消模式控制，等待确定拒绝、接纳确认或控制流结束。已接纳的模式仍以关联确认显示。模式取消不得产生当前或下一回合的中断意图，已有活动回合、取消和退出继续使用原控制规则。

普通粘贴事件 MUST 仅编辑草稿。补全 Tab MUST 只填写命令名，普通 Enter 才走本地命令提交。问卷、审批、Hook 信任、历史及其他弹层保有其焦点和按键语义。用户 SHALL 能在普通编辑区通过纯文本识别 `Plan mode · read-only`，成功退出及新建或恢复交互会话时可在首次提交前识别 `Default`。可用空间足够时显示切换提示，较小布局优先保留输入区、模式短标签和必要操作。独占弹层优先保持原焦点和操作提示，有额外空间时显示模式，退回编辑区后恢复完整模式提示。极小终端按现有宽高守卫裁剪，禁色不影响辨识或停止操作。

#### Scenario: Enter planning without submitting work

- **WHEN** 空闲的普通会话接受单行 `/plan`
- **THEN** 命令离开编辑器且出现 Plan Mode 状态；请求计数和输入历史不增加

#### Scenario: Switch with an unfinished draft

- **WHEN** 空闲普通编辑器中已有正文和非末尾光标，用户按 `Shift+Tab`
- **THEN** 接纳后模式更新，正文、光标和 recall 位置保持原样，直到用户主动提交

#### Scenario: Preserve focus in a plan question

- **WHEN** Plan Mode 下结构化问卷取得焦点，用户按 `Shift+Tab`
- **THEN** 问卷沿用返回行为，Plan Mode 保持；问卷关闭后才可能从编辑器切换

#### Scenario: Keep paste and busy commands local

- **WHEN** 用户粘贴 `/plan off`、提交带无效参数的命令，或在有操作存续时尝试切换
- **THEN** 粘贴自身不触发切换，错误或忙碌拒绝保留草稿与原模式，不把该命令送给模型

#### Scenario: Preserve an edit across a delayed mode decision

- **WHEN** 用户从 `/pla off` 用补全 Enter 发起控制，结果延迟，窗口调整且用户粘贴文字或尝试打开恢复入口
- **THEN** 待决期间没有新草稿、模型提交或交接，实际拒绝恢复按键前的正文、光标和 recall，实际接受只消费原命令一次

#### Scenario: Cancel mode control without interrupting the next turn

- **WHEN** 空闲的待决控制收到 Esc 或 Ctrl+C，并分别在 owner 接纳之前及之后完成取消信号
- **THEN** 前者保持原模式和草稿，后者仍显示权威的已接纳模式；独立提交的下一回合不继承旧取消

#### Scenario: Show the effective mode after a handoff

- **WHEN** 用户恢复一份包含 Plan 正文的会话，或从 Plan 成功退出
- **THEN** 首次可提交输入前可识别当前 `Default`，保留既有转录阅读；迟到的旧宿主确认不得改变提示或草稿

### Requirement: Plans are read and revised as normal assistant Markdown

TUI SHALL 使用现有 Markdown 转录和可访问的原文/历史阅读路径展示模型计划。模型可以使用已支持的结构化问卷消除歧义，并在下一条用户输入后修订计划。失败或取消的流式片段沿用现有未完成展示。任务清单更新、模型声称计划已通过、流式片段或历史中已有方案 MUST NOT 成为模式变化或自动提交的信号。离开 Plan 后，新的实施回合只由用户再次明确提交正文发起。若用户要精确实施早先某一版，旧计划正文 SHALL 继续供转录阅读，用户可在实施正文中显式携带其所需部分。自动压缩按既有规则只处理旧窗口，本次真实实施输入不受摘要替换。

#### Scenario: Review and refine a plan in place

- **WHEN** 模型在探索与问答后返回 Markdown 方案，用户仍处于 Plan 并提交修改意见
- **THEN** 正文可按既有方式阅读，下一轮继续使用 Plan 策略并可给出修订方案

#### Scenario: Start implementation from a separate submission

- **WHEN** 用户先提交 `/plan off`，再输入并发送自己的实施要求
- **THEN** 退出动作本身不产生新回合；之后的正文沿普通模式进入同一会话，适用正常工具权限

#### Scenario: Submit exact implementation context after a summary

- **WHEN** 用户从转录阅读旧计划，把所需决定随实施指令提交，且首次普通采样前摘要省略了旧计划的这些文字
- **THEN** 这些文字仍从本次真实用户输入进入普通原生请求，旧计划仍可从转录阅读
