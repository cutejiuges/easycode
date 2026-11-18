# runtime/conversational-input Specification

## Purpose

定义运行中自动追加输入的接收、顺序、安全点消费、取回和停止契约，使用户能够持续参与 coding turn，同时保证每条输入身份明确、持久化结果可判断、队列有界且恢复不会重复执行。

## Requirements

### Requirement: Automatic delivery preserves FIFO barriers

系统 SHALL 为自动输入分配独立 UUIDv7 InputID，并与 RequestID 分离。一个 owner 管理全部输入；仅可领取队首连续 auto 前缀，strict-next 为不可越过屏障。空闲 auto 前缀可共享新 turn，正文仍是多条独立消息；strict-next 每条独立 turn。

#### Scenario: Respect a strict barrier
- **WHEN** 队列依次为 auto A、strict B、auto C 且当前工具安全点可继续
- **THEN** 仅 A 可进入当前 turn，B 必须在其收口后独立执行，C 不可越过 B

#### Scenario: Preserve message boundaries
- **WHEN** 两条 auto 作为新 turn 初始输入领取
- **THEN** 按原正文顺序独立保存，共享 turn identity，不拼接文本

### Requirement: Claims have one durable resolution

自动输入 SHALL 经 queued、claimed、committing 到 committed，确定无写入可 returned，durability 不确定必须 uncertain 并 poison。每 turn 最多一个未解决 claim，claim 未解决不得丢弃所有权或启动新 turn。领取不是保存，也不代表远端处理。

#### Scenario: Resolve cancellation before admission
- **WHEN** writer 尚未接受且 turn 取消
- **THEN** discard staging 并确定返还，零输入写入

#### Scenario: Resolve cancellation after Sync
- **WHEN** writer 已接受并 Sync 成功但 turn 同时取消
- **THEN** finalize 一次并报告 committed，不返还、不重发

#### Scenario: Poison uncertain persistence
- **WHEN** 输入写入或 Sync 结果无法确认
- **THEN** 报告 uncertain 并停止后续副作用，保留 cleanup ownership，不当作 queued 重试

### Requirement: Input admission and claims obey encoding budgets

系统 SHALL 限制每条 4 MiB、queued 与 unresolved claims 合计 64 条/16 MiB 正文。admission 必须纯内存验证单条在 native/record 16 MiB 上限及完整 envelope 开销内可表示，否则 input_encoding_limit 且无写入。claim 取满足 native/record 16 MiB、batch 64 MiB 的最大连续前缀，批次过大分批；不得将可预判编码错误交给 writer 导致 poison。

#### Scenario: Reject escaped expansion safely
- **WHEN** 合法 4 MiB 正文转义后无法放入一个当前 record
- **THEN** 拒绝 input_encoding_limit，journal/队列不变且 writer 健康

#### Scenario: Retain claim quota
- **WHEN** worker 持有尚未解决 claim 时继续收到输入
- **THEN** 原 claim 仍占待处理额度，超过总量返回 input_queue_full

#### Scenario: Split an oversized prefix
- **WHEN** 多条单独可表示输入合并后超过 native 限额
- **THEN** 本次只领取最大可表示前缀，其余保持 FIFO，不永久堵塞队首

### Requirement: Retraction is atomic and identity targeted

取回 SHALL 仅针对明确指定、非重复且仍 queued 的 auto InputIDs，先验证全部再原子返还；任何已 claimed 或不存在的目标必须 input_not_queued，不能部分删除。取回不修改 Provider 或 Session。

#### Scenario: Retract wins the claim race
- **WHEN** owner 先接受合法取回再处理安全点
- **THEN** 指定输入仅返还一次，后续 claim 不包含它

#### Scenario: Claim wins the retract race
- **WHEN** owner 先 claim 任一目标再处理取回
- **THEN** 整个取回失败，其余 queued 项保持不变

### Requirement: Interactive stop returns automatic inputs

定向 stop SHALL 只作用于匹配活动 turn，并返还截至该次 admission 的未领取 auto，严格输入保留；停止处理中拒绝新的 auto。已 claim 项等待确定 durable resolution。idle 返回 no_active_turn，stale 返回 turn_mismatch，拒绝不改变任何输入。approval abort 同样返还 auto。现有 interrupt 保持队列，EOF 排空，shutdown 等待全部 owner 清理。

#### Scenario: Stop without restarting pending automatic work
- **WHEN** stop 接受时有 queued auto 和 strict-next
- **THEN** auto 返回不可执行状态，严格输入只能在前 turn 清理终态后运行

#### Scenario: Do not cancel a replacement turn
- **WHEN** stop 指向旧 turn 而新 turn 已运行
- **THEN** 返回 turn_mismatch，当前 turn 与队列不变

#### Scenario: Finish claims before shutdown
- **WHEN** shutdown 时 input commit 已获 writer admission
- **THEN** 等待 committed 或 uncertain 的最终 resolution 与资源清理，不提前释放 lease

### Requirement: Queue status is transient and ordered

系统 SHALL 以同一有序输出流报告 admission 和输入状态，committed 只能发生在 Sync/finalize 后。queue/claim/returned UI 状态不写 Session；进程重启只恢复原生已提交输入，不猜测未消费队列。状态与错误不得回显正文或 secret。

#### Scenario: Crash with pending inputs
- **WHEN** 进程退出前仅收到 queued 确认
- **THEN** 恢复不自动执行该输入，也不声称已保存

#### Scenario: Fail after saving an input
- **WHEN** 输入 Sync 后 Provider 请求失败
- **THEN** 保留该输入 committed 历史，同时报告 turn failure，不自动重提

### Requirement: Preflight rejects unsendable input without retaining it

初始和中途输入 SHALL 在 Sync 前完成候选上下文预算与 Provider 请求编码大小预检。确定的本地超限必须返还本次 claim，committed history 不变；初始失败结束当前 turn，中途失败继续原循环前仍必须验证原请求合法。网络/远端失败发生在成功提交之后时仍保留输入。

#### Scenario: Shorten a rejected input and continue
- **WHEN** 候选输入使原本可请求的历史超过预算或请求大小上限
- **THEN** 本次输入未保存且可取回，用户缩短后可在同一会话继续，恢复也不包含被拒绝的正文

### Requirement: Explicit Skill inputs retain original text and frozen attachments

显式 Skill 输入 SHALL 在 admission 前完成可取消准备，保存用户原文及最多一个强类型 Skill 附件；附件包含来源、名称、revision、参数和完整展开正文，不能替换用户原文或伪造第二个用户身份。admission 继续纯内存校验，原文和附件共同计入每条 4 MiB、64 条/16 MiB 队列、native/record 16 MiB 与 batch 64 MiB 限额。候选预算和请求预检必须包含附件；Sync/finalize 前不得报告 committed。

queued 后文件变化不得改变已准备内容；取回或 returned tray 只恢复用户原文，再次提交重新准备。取消和 uncertain 沿用既有 owner 与 durable resolution，不能为了读取 Skill 创建独立无界队列或重试提交。

#### Scenario: Freeze an admitted skill
- **WHEN** 输入已 queued，随后磁盘 Skill 改变
- **THEN** 该输入只使用已准备附件；取回再提交会重新读取并进行来源检查

#### Scenario: Reject a preparation or encoding failure
- **WHEN** Skill 读取失败或原文加附件无法满足编码/预算约束
- **THEN** 不写该输入、不请求 Provider，用户原文保持可编辑

#### Scenario: Resolve a Skill input after Sync
- **WHEN** 带附件输入 Sync 成功后取消
- **THEN** 仍报告 committed，不把它返还为可自动重试输入

### Requirement: Interactive file inputs freeze before ordinary admission

交互输入 SHALL 用强类型明确区分普通文字和显式文件引用意图；文件读取 SHALL 在普通 admission 前可取消地完成，携带引用必须与原文完整有序集合一致。用户原文、最多一个既有 Skill 附件及至多八个文件附件 MUST 独立保留且共同计入每条 4 MiB、64 条/16 MiB queue/claim、native/record 16 MiB 与 batch 64 MiB 限额。候选上下文、请求以及强制提交展示事件的编码预检 MUST 包含实际包装或摘要开销；摘要必须直接从同一冻结输入派生。非法组合或预判超限必须在该输入 batch 的 writer 接收前确定拒绝，不能在成功 Sync 后才发现展示编码失败并误报 uncertain。admission 仍为纯内存静态接纳；实际动态上下文/请求预检 MUST 在交付操作中使用其完整捕获策略，不能把文件准备时的启动配置当成后续操作策略。

准备失败或接收前取消 MUST 不入队、不写 Session、不为该输入请求 Provider，原文保持可编辑。queue admission 继续纯内存，正式 committed 仍只在 Sync/finalize 后报告；接收后的 durable 不确定沿用 uncertain/poisoned 与唯一 cleanup owner。

#### Scenario: Prepare a Skill and multiple files together
- **WHEN** 一条真实交互输入同时包含显式 Skill 及多个合法文件引用
- **THEN** 全部附件在同一次 admission 前冻结，原文和附件共用一个 InputID 和现有额度，不伪造第二次输入

#### Scenario: Reject a combined encoding overage
- **WHEN** 单独原文及各附件合法，但总量或 JSON 转义后无法编码为合法当前记录
- **THEN** 以确定错误拒绝且 writer 健康，journal 和队列不变

#### Scenario: Preflight the mandatory committed-input view
- **WHEN** 原生输入合法，但派生提交摘要或其事件无法满足当前严格编码约束
- **THEN** writer 接收前确定拒绝，不先保存输入再因可预判的展示错误进入 uncertain

#### Scenario: Cancel after a successful file-input Sync
- **WHEN** 带附件输入 Sync 已成功而取消同时到达
- **THEN** 输入仍为 committed，不返还为可重试草稿，不重新读取或重复发送

### Requirement: Frozen file inputs retain FIFO retraction and delivery semantics

queued/claimed 后的文件变更 MUST 不改变冻结附件；初始输入、工具安全点 steering 和 Stop 续轮的真实纠正 SHALL 使用同一冻结值与原身份。已有 FIFO、strict-next 屏障、samples/calls 限制、原子取回和停止返还规则 MUST 保持。取回或 returned tray SHALL 仅恢复原始用户文字，再次明确提交才重新准备；未 durable 的内存附件不得在重启时恢复或自动执行。

普通文字输入中的 @ MUST 不触发隐式文件准备；从历史恢复已有文件附件不等于获得未来文件读取意图。

#### Scenario: Preserve a queued file across a source edit
- **WHEN** 带文件输入已 queued，文件随后改变，输入在工具安全点领取
- **THEN** 领取原冻结正文；它不会越过前方 strict-next 或重置 turn 额度

#### Scenario: Retract and resubmit a file input
- **WHEN** 用户合法取回尚未 claim 的文件输入，修改文件后重新提交
- **THEN** 只返还原始文字，新提交读取新内容并使用新的正常输入身份

#### Scenario: Preserve a file correction through Stop compaction
- **WHEN** Stop 冻结反馈后的真实纠正带文件附件，候选需要 discard 后摘要旧窗口再重建
- **THEN** 原 InputID、文件包装和顺序保持，无来源重读；预检包含全部附件且只在原交付 batch Sync 后 finalize

#### Scenario: Stop an active turn while its next file input is preparing
- **WHEN** 当前 turn 活动且另一条交互输入仍在 pre-admission 文件准备，用户按 Ctrl+C
- **THEN** 同时取消该准备和按既有契约停止活动 turn，不遗留可在停止后自动入队的旧准备；已被接收的输入仍等待确定 durable resolution

### Requirement: File content freezing preserves operation-scoped inference capture

文件准备和排队 SHALL 只冻结输入内容，不在草稿或附件中捕获 effort、profile 或选择序号。普通 turn SHALL 继续由唯一 thread owner 在 worker 启动前捕获完整推理策略，已准备或已排队的 strict-next 输入在真实启动时使用最新已接受选择。被当前 turn 领取的自动输入、工具安全点/Stop steering、自动摘要及候选 discard/rebuild MUST 继续使用当前 turn 的同一策略；输入领取或文件完成不能成为新策略生效边界。独立手动摘要仍在接纳时捕获自己的策略。

输入预检、实际 request、properties 和 receipt MUST 从同一操作策略生成；文件来源、TUI 及历史投影不得另从启动 profile 或快照构造采样设置。准备失败/取消和取回不得撤回已接受的 effort；文件清理完成不能绕过活动操作的 finishing，旧 worker/stream 真正完成前不得启动下一操作、刷新为 idle 或提前释放 lease。选择及其序号 MUST 保持进程内状态，不因附件新增持久化事实。

#### Scenario: Select effort while a file is preparing
- **WHEN** 文件准备被屏障暂停，owner 接受 low 选择后准备完成，且该输入随后启动新 turn
- **THEN** 内容只读取一次，该操作的预检、body 和 receipt 全部使用 low，不沿用准备开始时的旧策略

#### Scenario: Deliver the same frozen files to different turn boundaries
- **WHEN** 当前 high 回合中一条文件输入被自动领取，另一条 strict-next 文件输入已排队，然后 owner 接受 low
- **THEN** 同轮输入及其工具/Stop/自动摘要保持 high，严格下一回合启动时使用 low，两条内容都不重读或重新截断

#### Scenario: Keep the captured policy while finishing
- **WHEN** 带文件操作已发布 terminal，但 worker 清理被屏障暂停，新的文件准备已完成且新的 effort 已接受
- **THEN** 活动身份及旧捕获策略仍为 finishing，下一操作等待真实完成；取消或来源完成都不能提前释放资源或应用新策略

#### Scenario: Preserve a selection when file preparation fails
- **WHEN** owner 已接受 low，而并发文件准备因来源变化或取消失败
- **THEN** 原草稿保持且该输入不入队，已接受的 low 不回滚、不重复提交设置或产生该输入的 Provider 请求
