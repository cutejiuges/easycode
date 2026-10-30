## Purpose

定义 Claude Code 风格的七工具紧凑会话主线、详细 transcript 和按需保存输出查询，使用户能辨认执行进展、失败与模型可见范围，同时保持草稿、审批、原生历史及资源所有权隔离。

## ADDED Requirements

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

Ctrl+O SHALL 切换当前会话详细 transcript，包含用户、assistant 与全部七工具的稳定位置；不再打开独立命令列表。进入时定位当前主线锚点或最新活动，退出恢复草稿、光标和主线浏览位置。方向键用于浏览，Enter 仅在活动具有详情时打开二级详情；Esc 从详情回 transcript，再回主线。完整命令与冻结结果可以查看，历史 Patch/Write 仅展示已知摘要，不重读当前文件生成历史 diff。

#### Scenario: Inspect an older search and command
- **WHEN** 用户在恢复会话中按 Ctrl+O 并浏览旧 Search 与 Shell 活动
- **THEN** 两者位于相应文本之间，均可查看已有结果，命令保存输出可进一步分页

#### Scenario: Preserve the draft
- **WHEN** 用户在有多行草稿时进入、滚动、resize 并退出 transcript
- **THEN** 草稿正文、光标及发送状态不变，不产生新的用户输入

### Requirement: Detail browsing is bounded and subordinate to approval

列表每页 SHALL 最多 50 个逻辑条目，正文每页最多 64 KiB UTF-8；artifact 单页最多 64 KiB raw/256 KiB escaped。界面 SHALL 最多缓存四个查询页；分页、选择与关闭绑定 generation，迟到回复不得覆盖新状态。Tab 只在命令双流详情切换流；View/resize MUST 无外部 I/O，用户浏览 MUST 不触发模型、executor、审批或 ledger。

审批 SHALL 优先占据焦点并保存浏览位置；Ctrl+O 不得绕过审批。浏览层普通字符/paste/Enter 不得泄漏给 composer；Ctrl+C 对活动 turn 定向中断，idle 保持既有退出规则；Esc 在浏览层只执行返回，不触发主线取回或 stop。查询关闭/取消后资源仍由唯一 owner 等待最终清理。

#### Scenario: Reject a stale page
- **WHEN** 用户切换活动或关闭详情后旧页面返回
- **THEN** 回复被丢弃，当前页面和草稿不变，查询资源最终关闭

#### Scenario: Approval interrupts browsing
- **WHEN** 浏览历史时新审批到达
- **THEN** 键盘由审批接管，浏览输入不能成为 allow；审批结束后可恢复原位置

### Requirement: Disclosure distinguishes presentation model and storage omissions

界面折叠、模型预览缩短、查询采集范围不完整与保存不完整 SHALL 分别表达。只有内容实际可取回时才提供保存输出入口；missing/corrupt 仍保留原冻结预览。普通主线 SHALL 使用用户语言，避免 frozen preview、canary 等实现词。控制字符只在显示副本中净化，布局按终端显示宽度，窄屏、无色和 resize MUST 不改变正文或授权。

#### Scenario: Show a completely saved limited search
- **WHEN** 搜索因输入 limit 只采集部分匹配，但这些内容已全部保存
- **THEN** 明确本次范围有限且已保存内容完整，不宣称搜完整个仓库

#### Scenario: Missing saved output
- **WHEN** 用户展开的已授权对象缺失或损坏
- **THEN** 显示保存内容不可用，原冻结模型结果仍可读，继续会话不改变历史
