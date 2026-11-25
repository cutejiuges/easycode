# tui/rewind Specification

## Purpose

定义终端如何从当前会话中选择历史回合、检查切点并确认创建独立分支。成功进入分支后将该轮首条用户输入放回编辑框，用户可以修改草稿并决定是否提交，也可以恢复原会话继续原有对话。

## Requirements

### Requirement: Rewind opens a bounded picker for the current conversation

用户 SHALL 通过空闲交互会话中的无参数 `/rewind` 打开只读选择器。活动回合、压缩、等待输入或交接期间的请求 MUST 按现有会话切换准入拒绝，保留原草稿和光标并显示安全英文反馈。打开选择器、浏览和取消阶段不得创建分支、读取工作区附件、调用 Provider、工具或 Hook，也不得将命令加入输入队列或输入回顾。

列表 SHALL 展示 `session/branch` 判定合格的最近最多 200 个回合，按已提交的回合起始顺序从早到晚排列。每个回合展示其首条已提交的初始用户输入摘要及由提交顺序决定的回合编号，编号在列表和确认页一致，并能区分摘要相同的候选。列表末尾 SHALL 提供默认高亮的 `Current` 行，选择该行即返回当前会话。界面有更早的合格回合时 SHALL 说明当前上限。空列表与可恢复读取失败 SHALL 显示各自的安全英文反馈，清理完成后可返回健康源。已证实的源损坏 SHALL 沿用会话分支的停止写入与故障退出语义。

#### Scenario: Open the picker with the current conversation selected
- **WHEN** 用户在空闲会话提交 `/rewind`，当前线程有超过 200 个合格回合
- **THEN** 只显示最近 200 个合格回合，按提交顺序排列并提示范围，默认选中 `Current`，尚未创建分支或发起模型请求

#### Scenario: Expose only eligible top-level prompts
- **WHEN** 线程有继承回合、无已提交初始输入的回合、包含多条初始输入的完整回合及同轮追加输入
- **THEN** 仅合格的本线程回合可选，每个回合对应一行并显示首条初始输入，其余初始输入和同轮追加输入没有独立候选

#### Scenario: Return from an empty or unavailable picker
- **WHEN** 当前会话没有合格回合或发生可恢复的候选读取失败，用户按 Esc
- **THEN** 退出选择器并还原原草稿和光标，没有创建分支或发起模型请求

### Requirement: Historical selection and confirmation preserve terminal input ownership

选择器 SHALL 支持上下键、PageUp/PageDown 和普通 Enter，使用与现有终端面板一致的按当前尺寸滚动和指示方式。历史项上的普通 Enter SHALL 进入确认页。确认页 SHALL 显示同一回合编号及所选输入预览，说明将在该轮之前创建并进入独立分支，并提示分支使用当前工作区文件。选中回合曾同时提交多条初始输入时，确认页 SHALL 说明只回填首条。只有用户选中创建动作并用普通 Enter 确认后，系统才 SHALL 申请创建。确认页 Esc 返回列表并保留所选回合，列表 Esc 返回原会话并恢复打开前的草稿和字素光标。

显示正文 MUST 使用现有终端安全文本处理和列宽裁剪，列表使用有界单行摘要，确认预览保持有界并适配窗口。确认默认选中返回动作，窗口无法完整展示分支范围和工作区提示时 MUST 禁用创建动作。缩放和无色模式 SHALL 保持当前选中身份并有可见选择指示。粘贴和换行编辑键不得触发浏览选择、确认或模型输入。过渡期间交付的按键由相应的过渡界面消费。

#### Scenario: Confirm a selected prompt after browsing
- **WHEN** 用户在相同摘要的历史回合间移动，按普通 Enter 确认其中一项，随后调整终端尺寸
- **THEN** 确认页保留所选回合的编号和安全预览，显示分支范围及当前工作区提示。按 Esc 返回原高亮行，再按 Esc 恢复原草稿和光标

#### Scenario: Keep paste and newline editing inert during rewind
- **WHEN** 用户把换行和类似选择按键的内容粘贴到列表或确认页，或按 Alt+Enter、Ctrl+J
- **THEN** 保持同一面板和选择，未请求创建、未向会话提交文本

### Requirement: Activate only the selected and verified branch

列表、确认和终态 SHALL 绑定当前源线程、同一浏览请求及其合格回合身份。确认申请 MUST 具有独立请求身份，并交付匹配的浏览、源和所选回合。禁止使用列表位置推断日志切点。返回的晚到、过期或身份不匹配结果不得切换会话。创建和接管 SHALL 遵循 `session/branch` 的原子发布、取消、过期校验、退出和清理语义。重复确认只能接纳一次。如果取消或可恢复失败，健康的原会话 SHALL 在确认清理和许可释放后恢复原草稿和光标，并按真实发布结果显示反馈。

接管成功且源资源关闭后，系统 SHALL 显示新分支标题及返回原会话的完整 `/resume <源 ThreadID>` 命令。此时 SHALL 将选中回合首条已提交初始输入的原始用户正文按常规编辑框换行规则填入草稿，并沿用粘贴的补全抑制。回填草稿仅属于本次激活后的编辑框。它不提交新回合，也不沿用旧的冻结文件或 Skill 附件。取消接管后另行恢复已发布分支，或在重启后恢复该分支时，编辑框沿用普通恢复行为。

#### Scenario: Edit the original prompt in the independent branch
- **WHEN** 用户确认从含多行文本与 `@` 引用的合格回合前创建分支，且源资源关闭与分支接管成功
- **THEN** 新分支显示标题和原会话恢复入口，编辑框包含选中回合首条初始输入的用户原文，没有附带旧文件或 Skill 内容。用户显式提交或修改后提交才进入常规输入流程

#### Scenario: Reject stale selection and duplicate confirmation
- **WHEN** 列表已过期、来源身份不匹配，或等待已接纳的创建时收到重复 Enter
- **THEN** 过期或不匹配的结果无法切换会话，重复 Enter 不产生另一次创建，可安全返回时还原原草稿。若分支已经发布，反馈沿用发布判定和完整目标 ID

### Requirement: Recalled drafts require an intentional submission

历史草稿在实际编辑前，原本用于提交的 Enter SHALL 打开原样发送确认并默认选中返回编辑。现有续行键语义优先。用户 MUST 主动选择 `Send unchanged` 后再确认才能提交原样文本，Esc 或选择返回时保存草稿和光标。连续 Enter、光标移动及窗口变化不视为已编辑。草稿发生实际编辑后，提交键 SHALL 沿用常规规则。正常输入准备与接纳失败时，保留可编辑草稿和适用的首词意图。

回填中带 `/` 或 `$` 的原首词保持已提交输入的正文或显式 Skill 意图。首词仍保留时，原本的正文 MUST 作为对话文本提交，即使该首词在当前会话命中内置命令或 Skill。曾携带显式 Skill 的输入 MUST 经当前 Skill 来源重新准备，来源缺失或当前不可调用时报告安全错误、保留草稿且不发起模型请求。用户实际改变首词后，后续分派使用当前普通命令和 Skill 规则。真实提交时按现有文件语法、当前工作区和授权准备引用。现有跨宿主保留命令之后的 `@` 仍作为普通文字。

#### Scenario: Keep delayed Enter from sending a restored prompt
- **WHEN** 重复 Enter 中至少一个按键在目标已激活、历史草稿尚未编辑时交付
- **THEN** 草稿保持可编辑，连按 Enter 不触发输入、Hook 或请求。用户编辑正文后普通提交，或主动选择原样发送并确认，才提交一次

#### Scenario: Keep restored command-like text as conversation input
- **WHEN** 已提交初始输入是 headless 保存的字面 `/rewind` 或 `/compact @src/x`，目标交互会话有相应本地命令
- **THEN** 保留首词并明确提交时作为对话文本处理。`/compact` 后的 `@` 保持字面含义，`/rewind` 即使有同名 Skill 也不加载该 Skill。用户改变首词后按当前分派处理

#### Scenario: Retain a recalled Skill draft if its source is unavailable
- **WHEN** 用户明确提交曾携带显式 Skill 的历史草稿，仍保留原首词，但当前来源已不存在或不可调用
- **THEN** 报告安全的准备错误，保留草稿及首词意图，没有普通文本回退、模型请求或已提交输入
