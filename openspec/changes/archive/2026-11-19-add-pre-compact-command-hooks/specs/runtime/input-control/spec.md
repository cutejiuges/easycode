## MODIFIED Requirements

### Requirement: Runtime commands are versioned and strongly typed

内部输入控制面 SHALL为`submit_input`、`submit_conversational_input`、`retract_inputs`、`stop_turn`、`resolve_tool_approval`、`interrupt`、`shutdown`、`query_context`、`compact_context`和`cancel_compaction`提供唯一当前的强类型command、专属constructor和validator；内部command不携带format revision，也不存在按revision选择的decoder。每条command MUST携带非空request identity；`submit_input` MUST携带合法UTF-8、去除外围空白后非空且不超过4 MiB的文本；`resolve_tool_approval` MUST携带expected active turn ID、approval request ID、invocation ID、candidate fingerprint、允许的typed decision和仅适用于deny的可选16 KiB反馈；`interrupt` MUST携带预期活动`turn_id`，`shutdown` MUST NOT接受无关payload。

command kind与typed payload不匹配、非法ID/fingerprint/decision、非法UTF-8、超限内容和不适用于该kind的字段 MUST在产生排队、approval fact、turn ID、Session写入或Provider/工具副作用前失败。command、提交结果与拒绝原因 MUST NOT使用或暴露无约束`any`。外部headless stream-json的`version: 1`仍只投影当前已支持的外部command；本变更不向无交互外部协议暴露可阻塞的approval response wire，也不得把外部wire version复制进Runtime核心类型。

#### Scenario: Accept a valid submit command
- **WHEN** 宿主提交具有唯一request identity和合法文本的当前`submit_input`
- **THEN** 控制面返回强类型提交结果，并且后续生命周期可以通过同一request identity关联

#### Scenario: Accept a correlated approval command
- **WHEN** 交互式宿主提交与当前active turn、request、invocation和fingerprint匹配的`resolve_tool_approval`
- **THEN** 控制面返回强类型accepted结果并把decision交给唯一turn owner

#### Scenario: Reject an invalid command without side effects
- **WHEN** command包含未知kind、非法target turn、空白/超限文本、过期approval identity或kind/payload不匹配
- **THEN** 控制面返回稳定英文`invalid_command`、`invalid_input`或`stale_approval`错误
- **THEN** command不进入队列、不写approval fact、不分配turn ID且不调用Provider或executor

#### Scenario: Adapt the current external protocol once
- **WHEN** headless adapter成功解码一个stream-json`version: 1`command
- **THEN** adapter构造不含version的内部强类型command
- **THEN** Runtime不暴露外部JSON decoder或第二套版本化command DTO

自动输入 SHALL 使用与严格输入相同的正文校验；retract 必须携带 1 至 64 个合法、非重复 InputIDs，stop 必须携带 expected turn ID。新增命令同样具有专属强类型构造与严格验证，不能复用无约束 payload。

新增上下文命令 SHALL 具有专属构造与严格 validator；compact instructions 为至多 16 KiB 合法 UTF-8，可为空，cancel 必须指定合法 expected CompactionID；query 不接受正文/目标身份。压缩事件必须有 session/thread/CompactionID 及经校验的 manual/auto scope。手动 scope 不含 TurnID，自动 scope 关联真实活动 TurnID 与目标 sample index。查询快照为 thread scope。普通 turn 事件原有身份校验不放宽。

#### Scenario: Reject a mixed operation scope
- **WHEN** 手动压缩或对应取消使用 TurnID、自动压缩缺失真实活动 scope、普通 turn 事件带压缩身份，或查询带正文
- **THEN** 在 admission/副作用前拒绝，不能补默认身份或借用最近 turn

### Requirement: Compaction control preserves admission queue and cleanup ownership

compact 的基础纯内存预检失败 SHALL rejected，零 durable 写入、Hook 和网络。成功 admission SHALL 返回独立 CompactionID，RequestID 只作进程内关联。原压缩 owner SHALL 持有从接纳到检查信任、执行、最终摘要、终态和 cleanup 的同一活动身份。started 必须在 Sync 后发布，completed 必须在 Sync/finalize 后发布；每个 accepted compact SHALL 获得唯一终态，started 尚未持久化的确定失败也必须有失败响应但不得伪造 durable started。无法确定 durable 状态时 SHALL poison 并报告安全失败，不声称已完成持久化收口。

cancel 只作用于匹配的活动 CompactionID，idle 返回 no_active_compaction，目标不同返回 compaction_mismatch；重复请求不能重复执行，旧 turn interrupt 不取消压缩。取消交付只表示 cancelling；writer 已接受完成提交后必须等待确定结果。操作结束且 Hook 与 stream cleanup 完成后才执行 queued 工作。成功保留 FIFO；普通失败/取消返还 queued auto、保留 strict-next；poison/shutdown 停止全部后续副作用。EOF 排空，shutdown/断管取消并等待唯一 cleanup owner，超时不得提前释放 writer/lease。

#### Scenario: Cancel a stale operation
- **WHEN** cancel 指向旧 CompactionID，而另一个压缩或普通 turn 正在运行
- **THEN** 拒绝且当前操作/队列不变

#### Scenario: Keep queued text outside a summary
- **WHEN** 压缩前检查或摘要期间接受新的 auto 和 strict-next 输入
- **THEN** 它们有界排队且不进入冻结来源窗口；成功后按原 FIFO 继续

#### Scenario: Return automatic work after failure
- **WHEN** 压缩普通失败且有 queued auto/strict-next
- **THEN** auto returned、strict-next 保持顺序；只在失败收口和清理后继续普通队列

#### Scenario: Own cleanup beyond a shutdown timeout
- **WHEN** PreCompact 命令或摘要 stream 在 shutdown 后清理阻塞
- **THEN** owner 继续强制取消/等待固定 done，lease 不释放、不得启动新 worker

#### Scenario: Reject a duplicate request identity
- **WHEN** 相同 RequestID 的 compact/query/cancel 在保留的去重范围内再次提交
- **THEN** 返回 duplicate_request，不重复压缩、查询结果发布或取消副作用

压缩前检查的信任请求、应答、活动与摘要准入通知 SHALL 具有严格类型并仅交付交互宿主。控制 owner MUST 根据 SessionID、ThreadID、CompactionID 和真实压缩 scope 校验当前操作。信任应答还必须精确匹配当前信任请求身份、冻结计划及声明。手动压缩使用独立 CompactionID 并且没有普通 TurnID。自动检查使用当前活动 TurnID 和 sample index 指定的真实压缩操作。错配、重复、已取消或已收口的应答 MUST 在更新授权状态或启动检查前拒绝。等待审查和进程清理 SHALL 保持控制和取消可响应。唯一压缩 owner 持有整个检查和摘要操作。

只有当前操作的真实终态可以结束相应的已启动压缩。Hook 及摘要准入通知必须在已 durable 的 started 之后出现，并保持当前 CompactionID、最近 committed 查询及窗口 revision。若当前自动压缩 worker 在 started 成功 Sync 前取消或失败，控制 owner MUST 接纳该真实作用域的唯一 `durable=false` 失败通知，不伪造 started，不关闭其他活动操作。若写入状态不确定，仍执行 poison 和关闭接纳。手动压缩保留 accepted response 和唯一终态。

#### Scenario: Resolve trust without fabricating a manual turn
- **WHEN** 手动压缩等待审查，收到精确匹配的当前声明及压缩身份的响应
- **THEN** 同一压缩 owner 唤醒当前检查等待，普通 turn 数、输入队列及普通工具审批均未变化

#### Scenario: Reject an unrelated or late compaction trust response
- **WHEN** 检查应答指向另一作用域、另一个 compaction、错误声明或已取消的等待
- **THEN** 信任状态和 Hook 启动次数保持原值，不得将该应答转用于当前或后续操作

#### Scenario: Cancel the owner of the current check
- **WHEN** 手动 PreCompact 收到匹配的 cancel_compaction，或自动 PreCompact 所属活动 turn 被定向 interrupt
- **THEN** 撤销当前等待或取消当前检查进程，等待现有 owner 完成清理。取消先于尚未生效的信任时，撤销等待即可，既不建立信任，也不要求额外的 resolved 通知

#### Scenario: Observe cancellation after a positive gate write
- **WHEN** writer 已接纳检查通过决定，取消在该决定 Sync 期间到达，尚未接纳 Provider 请求
- **THEN** 保留确定提交的决定，以 `compaction_cancelled` 无摘要计量收口，当前操作零摘要请求。检查通过不覆盖既有 checkpoint 完成 Sync 后的安装语义

#### Scenario: Correlate failure without a durable auto start
- **WHEN** 当前自动尝试在 started 提交前确定取消，或 started 的 Sync 结果不确定
- **THEN** 无 started 或检查通知，唯一失败关联原 submit 与真实作用域，前者保留取消语义，后者关闭接纳。两者均不误分类为压缩身份错误
