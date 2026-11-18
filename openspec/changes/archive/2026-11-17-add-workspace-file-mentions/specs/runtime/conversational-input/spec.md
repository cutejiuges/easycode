## ADDED Requirements

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
