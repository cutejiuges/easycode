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

新增上下文命令 SHALL 具有专属构造与严格 validator；compact instructions 为至多 16 KiB 合法 UTF-8，可为空，cancel 必须指定合法 expected CompactionID；query 不接受正文/目标身份。压缩事件必须有 session/thread/CompactionID 而没有 TurnID；查询快照为 thread scope；普通 turn 事件原有身份校验不放宽。

#### Scenario: Reject a mixed operation scope
- **WHEN** 压缩取消带 TurnID 或普通 turn 事件带压缩身份，或查询带正文
- **THEN** 在 admission/副作用前拒绝，不能补默认身份或借用最近 turn

### Requirement: One owner decides admission and turn execution

每个活动thread SHALL只有一个输入控制owner串行决定command admission、活动turn身份、活动approval、follow-up queue转换和关闭状态。空闲时接受的`submit_input` SHALL得到`starting` disposition；存在活动turn时接受的输入 SHALL得到`queued` disposition并进入该Session实例持有的FIFO follow-up queue。`resolve_tool_approval`只能路由到同一owner当前记录的一个活动request，不进入follow-up queue，也不得启动新turn。

并发调用方不得通过先读取状态再自行启动turn或完成approval的方式绕过owner。任意时刻最多一个`RunTurn`可以修改该Conversation；输入 MUST 按 admission FIFO 处理，正文不得拼接、重排或并行执行。严格 submit_input 保持每条一个独立 turn；自动输入只可领取队首连续自动前缀，在完整工具安全点进入当前 turn，或在空闲时形成一个含多条独立消息的新 turn。严格输入是不可跨越的 FIFO 屏障；任何输入均不得注入活动 stream 或 approval feedback。

#### Scenario: Serialize concurrent submissions
- **WHEN** 两个submit在空闲thread上并发到达
- **THEN** owner按唯一顺序接受它们，恰好一个得到`starting`，另一个得到`queued`
- **THEN** Provider任意时刻只存在一个活动stream

#### Scenario: Route approval without creating a turn
- **WHEN** 当前turn等待approval且匹配response到达
- **THEN** owner把response交给该turn并保持原turn identity
- **THEN** response不进入普通输入FIFO、不生成用户turn也不建立并发Provider stream

#### Scenario: Preserve one input per turn
- **WHEN** 活动turn期间连续接受两个严格 submit_input follow-up输入
- **THEN** 当前turn先收口，两个输入再按FIFO各自启动新的独立turn
- **THEN** 系统不拼接两个文本，也不把它们加入当前turn的Provider请求

#### Scenario: Close the lost-wakeup window
- **WHEN** 新输入或approval response在owner观察状态转换之间到达
- **THEN** owner重新观察已接受工作并最终处理它
- **THEN** 命令不会滞留到下一条无关命令到达后才执行

同一 owner SHALL 同时管理压缩与普通 turn，任意时刻最多一个活动 worker；压缩只在 idle 且输入队列为空时接受，不能跨越 FIFO。活动压缩期间接受的新输入只能 queued，不能 claim 或进入摘要；context.query 可读最近已提交快照。查询和控制不得被网络等待阻塞。

#### Scenario: Serialize a compact and a submit race
- **WHEN** idle 状态下 compact 与 input submit 并发到达
- **THEN** owner 先接受 compact 时输入 queued；先接受 submit 时 compact 被 session_busy 拒绝，任意时刻只有一个网络 worker

## ADDED Requirements

### Requirement: Compaction control preserves admission queue and cleanup ownership

compact 的纯内存预检失败 SHALL rejected 且零 durable 写入/网络；成功 admission SHALL 返回独立 CompactionID，RequestID 只作进程内关联。started 必须在 Sync 后发布，completed 必须在 Sync/finalize 后发布；每个 accepted compact SHALL 获得唯一终态，started 尚未持久化的确定失败也必须有失败响应但不得伪造 durable started。无法确定 durable 状态时 SHALL poison 并报告安全失败，不声称已完成持久化收口。

cancel 只作用于匹配的活动 CompactionID，idle 返回 no_active_compaction，目标不同返回 compaction_mismatch；重复请求不能重复执行，旧 turn interrupt 不取消压缩。取消交付只表示 cancelling；writer 已接受完成提交后必须等待确定结果。操作结束且 stream cleanup 完成后才执行 queued 工作。成功保留 FIFO；普通失败/取消返还 queued auto、保留 strict-next；poison/shutdown 停止全部后续副作用。EOF 排空，shutdown/断管取消并等待唯一 cleanup owner，超时不得提前释放 writer/lease。

#### Scenario: Cancel a stale operation
- **WHEN** cancel 指向旧 CompactionID，而另一个压缩或普通 turn 正在运行
- **THEN** 拒绝且当前操作/队列不变

#### Scenario: Keep queued text outside a summary
- **WHEN** 摘要期间接受新的 auto 和 strict-next 输入
- **THEN** 它们有界排队且不进入冻结来源窗口；成功后按原 FIFO 继续

#### Scenario: Return automatic work after failure
- **WHEN** 压缩普通失败且有 queued auto/strict-next
- **THEN** auto returned、strict-next 保持顺序；只在失败收口和清理后继续普通队列

#### Scenario: Own cleanup beyond a shutdown timeout
- **WHEN** 摘要 stream 在 shutdown 后清理阻塞
- **THEN** owner 继续强制取消/等待固定 done，lease 不释放、不得启动新 worker

#### Scenario: Reject a duplicate request identity
- **WHEN** 相同 RequestID 的 compact/query/cancel 在保留的去重范围内再次提交
- **THEN** 返回 duplicate_request，不重复压缩、查询结果发布或取消副作用
