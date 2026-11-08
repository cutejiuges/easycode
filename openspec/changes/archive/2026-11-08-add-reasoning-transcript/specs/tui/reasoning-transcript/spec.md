## Purpose

为双 Provider 已返回的可展示推理提供活动提示与统一详细记录阅读，采用接近 Claude Code 的默认主线行为，并在有界分页、取消、提交、恢复和输入焦点之间保持准确身份与保存状态。

## ADDED Requirements

### Requirement: Active reasoning is compact and completed reasoning stays in detail history

主线 SHALL 仅显示真实当前推理的紧凑 Thinking/Reasoning 提示，块结束未提交可显示一次等待保存；sample 保存后撤下活动提示，不逐块累积永久推理行，不挤占主线非推理 50 项窗口。工具、审批、问卷和取消状态优先于推理提示，迟到更新不能覆盖它们。没有推理项不得伪造活动。

Ctrl+O SHALL 使用现有统一记录入口，详情包含全部可翻页推理及保留的活动临时项；上下选择、Enter 阅读、Esc 从正文到列表再返回、Ctrl+O 关闭。unavailable 无正文不可读；完整正文支持弱化 Markdown 与 r 安全原文，缺上下文片段固定原文。标签由来源和事实决定，不分析 Markdown 标题推测状态。

#### Scenario: Inspect reasoning without crowding the main transcript
- **WHEN** 多个 sample 的推理已完成并保存
- **THEN** 主线没有逐块永久行，Ctrl+O 仍可翻页阅读对应推理，普通回答/工具的主线窗口不被隐藏项挤占

#### Scenario: Keep active reasoning subordinate to a modal
- **WHEN** 推理期间问卷、Hook 信任或普通审批获得焦点
- **THEN** 提示不冒充工具正在执行或抢占 modal，Enter/r/Tab/paste 不修改背后草稿或阅读状态

### Requirement: Reading identity survives sample commit and list eviction

活动阅读 SHALL 使用有类型的临时键，不伪造 durable cursor。sample 提交后依据显式映射升级为准确 cursor，保留正文源偏移和选择；input 提交不能解析当前推理完成。连续 commit、查询延迟或目标离开最新列表时仍按真实范围交接，不能按正文/来源名猜测去重。旧 generation 回复不得覆盖新选择或重新打开已关闭页面。

#### Scenario: Commit while projection is delayed
- **WHEN** sample 已保存而列表/正文查询被确定性阻塞，下一 sample 开始
- **THEN** 有界画面不闪空，旧 sample 恰好替换一次，新 sample 独立保留

#### Scenario: Preserve a selection with repeated text and eviction
- **WHEN** 两个块文字相同，正在阅读的块提交后离开最新 50 项列表
- **THEN** 选择按映射保持在原块与源范围，不跳到相同文字的其他块

### Requirement: Saved state follows cancellation linearization and actual commit results

block Completed 不等于已保存。没有进入 sample 提交的失败/取消 SHALL 收口当前所有块并标未保存；已经进入提交路径的取消 SHALL 等待真实结果，不按取消时刻回滚。成功提交的推理保留已保存状态，Sync 已确认但投影失败须显示已保存/刷新失败；提交结果不确定须显示保存状态不确定，不声称 journal 无记录。之后工具阶段失败不能将先前 durable sample 改为未保存。

失败/取消允许块没有 Completed，由唯一 turn/sample 解析结束全部当前活动状态，不生成第二终态。至多保留一个失败 sample 的有界快照，开始下一 turn 或退出时释放并提示未保存快照已结束；恢复只读取实际验证成功的事实，不重放工具。

#### Scenario: Cancel before a sample enters commit
- **WHEN** 推理块完成后 stream 取消，且没有选中合法 sample completed 进入提交路径
- **THEN** 当前块标未保存，恢复不伪造它，先前 committed 内容保留

#### Scenario: Cancel during an accepted commit
- **WHEN** 已进入 sample 提交路径后取消，随后 Sync/finalize 成功
- **THEN** 推理标已保存，最终 sample 不因该取消伪造失败；工具 sample 后续取消只影响后续操作

#### Scenario: Preserve uncertainty or a durable projection failure
- **WHEN** 提交结果不能确认，或 Sync 已确认但 finalize/投影失败
- **THEN** 分别显示保存状态不确定，或已保存且刷新失败；不冒充未保存、不重写 journal

### Requirement: Reading and live buffers remain bounded safe and independent of rendering

活动 sample 全部推理正文 SHALL 共享 64 KiB 和最近 50 个临时条目，选中项优先保留，其他按最近收到文字淘汰；仍超额时选中项保留 UTF-8 尾片段并标缺失范围。完成快照受同一预算，活动详情不复制另一份全正文。正文页最多 64 KiB，main/detail 列表、正文和工具页共用四页缓存，不为折叠或 resize 加载整篇历史。

既有净化、宽字素、NO_COLOR、Markdown 预算、源锚点和查询 owner 规则继续有效。未变正文及其他历史不重扫；New/getter/View 无 I/O、隐藏 goroutine、模型请求或提交。UI 淘汰不得停用协议验证或改变模型/工具结果。

#### Scenario: Read oversized reasoning safely
- **WHEN** 多块累计超 64 KiB，含中文、emoji、OSC/bidi 和长代码，用户打开详情并 resize/翻页
- **THEN** 显示/缓存均有界，片段范围准确，终端不受源控制序列控制，已提交全文仍可分页读取

#### Scenario: Preserve draft and discard a late reply
- **WHEN** 用户带草稿阅读、切换选中项或关闭页面，旧查询随后返回
- **THEN** 草稿/光标/回忆/队列不变，旧回复只清理不覆盖，浏览零模型请求和零控制提交
