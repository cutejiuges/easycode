# tui/tool-transcript Specification

## Purpose
定义 Claude Code 风格的已实现工具紧凑会话主线、详细 transcript 和按需保存输出查询，使用户能辨认执行进展、失败与模型可见范围，同时保持草稿、审批、原生历史及资源所有权隔离。

## Requirements

### Requirement: All current tools have consistent user-facing activity

Read、Glob、Grep、Patch、Write、Exec、ReadArtifact SHALL 有稳定身份、可辨认名称、操作对象和等待/审批/执行/终态展示。相同能力在两家 Provider 下 MUST 使用一致交互；Exec 展示 Shell，不因 wire facade 名称宣称 Bash 专属语法。普通主线 SHALL 使用紧凑工具行与缩进结果，隐藏 seq、invocation ID、ledger、descriptor、fingerprint 及持续调试状态行。用户与 assistant 正文保持原有顺序与内容。

终态 SHALL 依据成功持久化的结果；非零 exit、权限拒绝、冲突、取消和 outcome_uncertain 必须可区分。未确定 cleanup 完成不能恢复新 turn admission。运行进度允许合并，不能冒充完整历史输出或覆盖终态。

#### Scenario: Show equivalent provider activity
- **WHEN** 两家 Provider 分别请求同样的读取、搜索、修改和命令
- **THEN** 主线呈现相同能力名称和状态规则，仅实际输入/结果不同

#### Scenario: Show nonzero command completion
- **WHEN** 命令确定退出且 exit code 为 1
- **THEN** 用户看到失败退出与退出码，不显示为成功测试或进程启动失败

### Requirement: Read and search grouping preserves meaningful boundaries

主线 SHALL 折叠相邻正常 Read/Glob/Grep，区分规范相对路径去重后的文件数与搜索调用次数。用户输入、非空 assistant 正文、修改、命令、审批和异常 MUST 打断分组；ReadArtifact 独立显示保存输出读取且不计为 workspace 文件。分组内异常 MUST 展示明确异常项，不伪装成成功计数。分组只改变显示，不影响调度、调用位置或模型历史。

#### Scenario: Count repeated reads correctly
- **WHEN** 同一文件被读取两次，随后另一个文件被读取并执行两次搜索
- **THEN** 文件数为 2、搜索数为 2，展开仍保留全部五次调用

#### Scenario: Expose an error within a group
- **WHEN** 同一只读批次有一个结果失败而其他成功
- **THEN** 用户能直接辨认失败项，不能只显示绿色成功汇总

### Requirement: Ctrl O opens the detailed conversation transcript

Ctrl+O SHALL 切换当前会话详细 transcript，包含用户、assistant 与全部已实现工具的稳定位置；不再打开独立命令列表。进入时定位当前主线锚点或最新活动，退出恢复草稿、光标和主线浏览位置。方向键用于浏览，Enter 仅在活动具有详情时打开二级详情；Esc 从详情回 transcript，再回主线。完整命令与冻结结果可以查看，历史 Patch/Write 仅展示已知摘要，不重读当前文件生成历史 diff。

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

### Requirement: Disclosure distinguishes presentation model and storage omissions

界面折叠、模型预览缩短、查询采集范围不完整与保存不完整 SHALL 分别表达。只有内容实际可取回时才提供保存输出入口；missing/corrupt 仍保留原冻结预览。普通主线 SHALL 使用用户语言，避免 frozen preview、canary 等实现词。控制字符只在显示副本中净化，布局按终端显示宽度，窄屏、无色和 resize MUST 不改变正文或授权。

#### Scenario: Show a completely saved limited search
- **WHEN** 搜索因输入 limit 只采集部分匹配，但这些内容已全部保存
- **THEN** 明确本次范围有限且已保存内容完整，不宣称搜完整个仓库

#### Scenario: Missing saved output
- **WHEN** 用户展开的已授权对象缺失或损坏
- **THEN** 显示保存内容不可用，原冻结模型结果仍可读，继续会话不改变历史

### Requirement: Main transcript has a bounded reading position

普通主线 SHALL 支持 PgUp/PgDn 在当前最多 50 个逻辑项的窗口内回看。PgUp 退出自动跟随，PgDn 到底部恢复跟随；回看时新输出只提示有新内容，不抢阅读位置。更早记录沿用 Ctrl+O 浏览。已有 overlay、详情、审批的按键规则优先，composer 方向键和正文编辑语义保持不变。

#### Scenario: Return to live following
- **WHEN** 用户翻屏回看后继续向下到达主线底部
- **THEN** 恢复跟随新输出，未提交草稿保持原样

#### Scenario: Lose an old anchor from the bounded window
- **WHEN** 新提交使回看锚点离开最近 50 项窗口
- **THEN** 提示旧位置已离开当前窗口，定位最早保留项，并保留 Ctrl+O 历史入口

### Requirement: Question activity preserves committed decisions in history

主线 SHALL 以独立提问活动打断 Read/Search 折叠，准确显示等待回答、提交中、已回答、跳过、取消、不可用与不确定；终态来自 durable 结果。Ctrl+O SHALL 能查看原问题、全部选项及完整冻结回答，恢复后仍可回看但不可重新提交旧问卷。回答显示为该工具活动的结果，不伪装成普通用户消息或审批。

问卷 modal SHALL 优先于 transcript 并保存原阅读锚点；已 durable 的历史详情通过既有 typed 查询分页，复用最多四页及 64 KiB 原始正文页预算，不能为问卷复制无界历史缓存。浏览、折叠、resize、无色和安全转义不改变模型 preview、原生历史或授权。

#### Scenario: Inspect a restored answer
- **WHEN** 恢复后打开 Ctrl+O 中较早的提问详情
- **THEN** 显示与提交时一致的问题/回答/跳过状态，零模型请求、零 ledger 写入、零重新提问

#### Scenario: Suspend and resume a history page
- **WHEN** 浏览旧工具详情时出现问卷，用户提交后返回浏览
- **THEN** 问卷期间浏览按键不能泄漏到聊天或授权，结束后恢复原锚点并遵守共享页预算

### Requirement: Tool details distinguish post checks from the original outcome

工具主线和 Ctrl+O 详情 SHALL 将 Post 检查关联到原 invocation 的稳定工具位置，不生成假的工具调用或把真实成功改写成失败。详情按确定顺序展示检查事件、来源、合法 id、稳定 code、统计及明确授权的 frozen feedback；未披露 stdout/stderr 不存在正文页。反馈只通过 typed 宿主内存查询获得，复用四页缓存/每页最多 64 KiB 及现有异步取消/generation 规则，不直接读取 Session、脚本或 Provider native。后置正文使用独立 post_hooks source；list/body 绑定最新可见 durable detail_sequence（journal seq，不是格式版本）。查询要求期望 sequence，与当前快照不符则返回 transcript_changed，不混合不同修订页面。后置批次 Sync 并安装快照后发布进程内更新，cache key 绑定 cursor/source/detail_sequence/offset；即使用户停留在详情也失效对应旧页、推进 generation、重新加载该 source 首屏并保持原工具锚点。旧回复不能覆盖新详情，不维护无界历史快照。

界面 SHALL 对显示副本转义控制字符，resize/NO_COLOR/分页/折叠不改变 frozen bytes、模型预算、原生请求或 fingerprint。恢复后的同一工具结果、检查前缀与反馈在可见语义上等价；恢复补偿的 interrupted/uncertain 明确显示而不伪装成已完成检查。

#### Scenario: Inspect a restored successful change with feedback
- **WHEN** 恢复后打开原 Write/patch 的详情
- **THEN** 显示同一成功结果与独立 Post 状态及原授权反馈，零脚本执行、零模型请求

#### Scenario: Render terminal controls safely
- **WHEN** 反馈含有效 UTF-8 的终端控制字符，界面在固定/窄屏/无色尺寸间切换
- **THEN** 只显示安全转义，缓存有界且原反馈/输出 bytes 不变

#### Scenario: Refresh an open detail when post feedback arrives
- **WHEN** 用户在工具成功后、Post 完成前已打开同一工具详情，而新的反馈 durable
- **THEN** 对应旧后置页失效，详情保持原工具并显示新 sequence 的内容；在途旧回复丢弃，四页缓存上限不变

#### Scenario: Do not assemble pages from different detail sequences
- **WHEN** 第二页查询期间后置事实发生更新
- **THEN** 返回 transcript_changed 并从新 sequence 首屏恢复，不用旧 TotalBytes/offset 拼接新正文

### Requirement: Durable history refresh preserves the post check summary

工具列表查询 SHALL 携带不含正文及单项统计的强类型后置摘要，绑定同 invocation 和非零 detail_sequence。工具 outputs 提交后的主线重建及恢复 MUST 保留原工具状态与后置 completed/skipped/cancelled/uncertain 状态，不依赖瞬态 HookActivity 缓存。摘要不得跨调用、使用 Pre 引用或附带原始输出。

#### Scenario: Retain post failure after the native output commits
- **WHEN** Write 成功且后置检查失败，工具 outputs 提交并刷新主线，随后恢复同一会话
- **THEN** 两次投影均显示原成功、同一后置失败和原工具锚点，不重跑 Hook 或读取 artifact
