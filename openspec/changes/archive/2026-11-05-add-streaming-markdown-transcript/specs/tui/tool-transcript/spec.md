## MODIFIED Requirements

### Requirement: Ctrl O opens the detailed conversation transcript

Ctrl+O SHALL 切换当前会话详细 transcript，包含用户、assistant 与全部七工具的稳定位置；不再打开独立命令列表。进入时定位当前主线锚点或最新活动，退出恢复草稿、光标和主线浏览位置。方向键用于浏览，Enter 仅在活动具有详情时打开二级详情；Esc 从详情回 transcript，再回主线。完整命令与冻结结果可以查看，历史 Patch/Write 仅展示已知摘要，不重读当前文件生成历史 diff。

完整助手正文详情 SHALL 支持局部 r 切换 Markdown/安全原文，并尽量保持源位置锚点；分页片段固定安全原文。resize 和审批返回 SHALL 保持阅读锚点，无法保持时说明原因并定位最近保留位置；模式切换不修改草稿或触发查询。

#### Scenario: Inspect an older search and command
- **WHEN** 用户在恢复会话中按 Ctrl+O 并浏览旧 Search 与 Shell 活动
- **THEN** 两者位于相应文本之间，均可查看已有结果，命令保存输出可进一步分页

#### Scenario: Preserve the draft
- **WHEN** 用户在有多行草稿时进入、滚动、resize 并退出 transcript
- **THEN** 草稿正文、光标及发送状态不变，不产生新的用户输入

#### Scenario: Preserve a source anchor when toggling text mode
- **WHEN** 用户在完整助手正文中部切换 Markdown/原文或 resize
- **THEN** 显示仍对应相同源范围，原文和输入状态不变


### Requirement: Detail browsing is bounded and subordinate to approval

列表每页 SHALL 最多 50 个逻辑条目，正文每页最多 64 KiB UTF-8；artifact 单页最多 64 KiB raw/256 KiB escaped。主线与详情 SHALL 共享最多四个查询缓存页，列表页及额外工具输出页均计入，不能复制到展示缓存绕过限制；分页、选择与关闭绑定 generation，迟到回复不得覆盖新状态。Tab 只在命令双流详情切换流；View/resize MUST 无外部 I/O，用户浏览 MUST 不触发模型、executor、审批或 ledger。

审批 SHALL 优先占据焦点并保存浏览位置；Ctrl+O 不得绕过审批。浏览层普通字符/paste/Enter 不得泄漏给 composer；Ctrl+C 对活动 turn 定向中断，idle 保持既有退出规则；Esc 在浏览层只执行返回，不触发主线取回或 stop。查询关闭/取消后资源仍由唯一 owner 等待最终清理。

正文加载 SHALL 优先当前锚点与预算内仍可见的已加载项，离屏页可回收；超额候选显式折叠，不形成加载/淘汰循环。解析和布局派生状态 MUST 独立有界且在来源淘汰时清理。resize 不直接启动补齐，后续导航或事实更新可调度新增可见项。

#### Scenario: Reject a stale page
- **WHEN** 用户切换活动或关闭详情后旧页面返回
- **THEN** 回复被丢弃，当前页面和草稿不变，查询资源最终关闭

#### Scenario: Approval interrupts browsing
- **WHEN** 浏览历史时新审批到达
- **THEN** 键盘由审批接管，浏览输入不能成为 allow；审批结束后可恢复原位置

#### Scenario: Share the budget across main and detail views
- **WHEN** 主线已加载正文，用户打开工具详情并查询额外输出
- **THEN** 合计不超过四个缓存页，优先当前阅读位置，折叠或淘汰不改写模型结果


## ADDED Requirements

### Requirement: Main transcript has a bounded reading position

普通主线 SHALL 支持 PgUp/PgDn 在当前最多 50 个逻辑项的窗口内回看。PgUp 退出自动跟随，PgDn 到底部恢复跟随；回看时新输出只提示有新内容，不抢阅读位置。更早记录沿用 Ctrl+O 浏览。已有 overlay、详情、审批的按键规则优先，composer 方向键和正文编辑语义保持不变。

#### Scenario: Return to live following
- **WHEN** 用户翻屏回看后继续向下到达主线底部
- **THEN** 恢复跟随新输出，未提交草稿保持原样

#### Scenario: Lose an old anchor from the bounded window
- **WHEN** 新提交使回看锚点离开最近 50 项窗口
- **THEN** 提示旧位置已离开当前窗口，定位最早保留项，并保留 Ctrl+O 历史入口
