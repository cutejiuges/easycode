## MODIFIED Requirements

### Requirement: Resume replays semantic history and continues native history

恢复成功后，应用 SHALL 使用经过语义回放校验的 `provider_native_commit` 调用 Provider codec 重建 native history，并单独使用 `HistoryProjector` 生成 TUI 初始 transcript。TUI MUST NOT 解析 JSONL 中的 Provider payload。后续用户输入 SHALL 继续使用恢复后的 native history 编译请求，并向原 thread 追加更大的 seq；不得重写、复制或重新编号既有 records。

#### Scenario: Replay visible transcript

- **WHEN** 一个包含多个成功 turn 的 thread 被恢复
- **THEN** TUI 在接受新输入前按原顺序显示各 turn 的用户和 assistant 可见文本
- **THEN** transcript 不显示 thinking、signature、encrypted content、usage 或未知原生扩展

#### Scenario: Continue after resume

- **WHEN** 用户在恢复后的 TUI 提交下一条输入并成功完成
- **THEN** Provider 使用恢复后的 native history 续写
- **THEN** 新 Session records 追加到同一 thread 且 seq 延续既有最大值

应用 SHALL 从经过完整 replay 验证的命令 facts 单独恢复命令历史投影和当前 thread 授权 descriptor 集合，不改变 HistoryProjector 或 native request。恢复不得为重建请求读取 artifact；文件缺失仅在按需查看时报告。

#### Scenario: Resume command details without stored files
- **WHEN** 包含历史 Exec 的 thread 恢复时对应输出文件缺失
- **THEN** 历史命令、原 preview 和状态仍可展示，续写请求保持相同；按需读取显示 unavailable

恢复 SHALL 从完整已验证 Tool ledger 最新成功 task.progress result 单独重建当前任务状态及 source(result_sequence、turn_id、invocation_id)，不从自然语言摘要、计数 preview、RuntimeEvent、SQLite 或 UI 折叠状态猜测清单。TaskProgressState 为 none/clear/snapshot 封闭联合，clear/snapshot 保留完整规范更新；仅失败、取消、预算拒绝或未完成调用不能推进当前状态。恢复后的当前列表可以来自失败 turn 内已成功 durable 的任务结果，不因 turn 最后失败而回滚真实事实。

普通后续请求 SHALL 继续使用原生调用/结果及已验证 checkpoint 包装，不每次重复注入内存任务快照。/tasks 和任务 UI 只读该投影，不修改 native history、context revision、权限或 fingerprint；列表完成只说明模型报告，不证明工作通过验证。

#### Scenario: Restore the latest successful task state
- **WHEN** journal 曾成功保存列表甲，随后保存错误更新和未完成调用，旧 turn 最终失败
- **THEN** 在接受新输入前恢复完整列表甲及真实来源，不重新执行任务工具，不从错误或摘要复述安装新列表

#### Scenario: Restore an explicit clearing
- **WHEN** 最后成功 Task 更新为显式空数组，历史和摘要仍提及旧清单
- **THEN** 当前状态为有真实来源的 clear，UI 不复活旧列表，普通请求不重复注入新任务消息

恢复当前任务的 result_sequence SHALL 取自真实成功 result record 的 ResultSequence，并与 live observer 接收的成功 Append 返回 seq 一致；不得复用 ToolHistoryEntry 的 ready Sequence、detail/native seq。当前 domain 更新/状态不携带工具 facade profile；仅 TaskInput 原始事实及 native evidence 负责相应来源限制。恢复后的规划在同一 committed_history canonical 元数据中绑定 source-free 任务内容 stamp，不新增 source/token/TurnInput 附件或网络正文。

#### Scenario: Keep live and restored task content stamps equivalent
- **WHEN** 相同成功任务事实分别沿 live 和恢复路径产生当前状态及下一规划
- **THEN** 实际 result_sequence、规范更新和内容 stamp 一致；不同来源身份但相同规范更新得到相同内容 stamp，不把身份或 facade profile 注入请求

### Requirement: Resume locally reconciles an unfinished Tool Loop from durable ledger facts

在取得并持续持有目标 thread exclusive lease、完成当前 Loader/ReplayPlanner、Provider 配置兼容检查和事务式 native history 恢复后，resume SHALL 检查活动 tool turn 的完整有序 call group，并在向宿主暴露可用 Session 或接受新输入前完成确定性本地 reconciliation。

- Patch ready但没有approval fact的invocation SHALL按call index使用原identity追加`cancelled/session_interrupted_before_approval` result。
- approval allow已durable但execution未started的Patch SHALL追加`cancelled/session_interrupted_before_execution` result；不得恢复grant或复用批准。
- ready但未started的自动允许Read/Glob/Grep/ReadArtifact SHALL追加`cancelled/session_interrupted_before_execution` result。
- result已durable但对应tool-output native entry尚未durable时，系统 SHALL从保存的preview bytes和wire kind重建完整有序output commit，不得重读、重新搜索或修改文件。
- execution started但result缺失时，系统 MUST按call index追加`outcome_uncertain` result，禁止自动重试；Patch不得根据当前文件内容猜测执行结果。
- tool-output entry已durable时，系统 MUST NOT重复追加output。
- 所有缺失output补齐后，系统 MUST以且仅以一个`turn_failed`关闭旧turn。
- 最终sample已durable完成时，resume不得重新执行、重新询问、追加output或重发sample。

reconciliation MUST保持原call index、capability、Provider call identity、wire kind和已durable candidate fingerprint，只允许纯内存Provider output编码和Session append/Sync；MUST NOT调用任一tool executor、候选准备器或approval presenter，MUST NOT访问Patch目标、恢复进程内session grant、发起Provider请求或自动继续下一sample。每次append/Sync失败 MUST使resume失败并阻止新turn副作用。旧开发schema、未知record/canary或混合revision journal MUST在reconciliation、repair和Provider恢复前fail closed，不得选择兼容reader。

上述常规 ready/approval/started 矩阵 SHALL 在没有未收口 Hook 子状态时适用。有 Hook plan 时 MUST 先消费已验证的冻结计划，且不读取当前 hooks.json、不重新选择 matcher、不请求信任、不调用 Hook runner：plan 尚未开始或仅完成成功前缀时，追加 gate cancelled 与原工具 cancelled/session_interrupted_before_hook；Hook started 无 finished 时，以同 batch 追加 recovery finished(outcome_uncertain, exit unknown, cleanup_confirmed=false)、gate outcome_uncertain 与原工具 cancelled/hook_outcome_uncertain。系统 MUST NOT 将 Hook 不确定冒充目标工具已执行或宣称旧进程已清理。

gate continue 后的未审批/allow 未 tool-started 状态 SHALL 沿用本条常规取消；gate 阻断和 result 已 durable 时直接复用 result；tool started 无 result 则仍按目标工具 outcome_uncertain 补偿。所有缺失 outputs 只由 frozen results 与已收口 Post 的冻结反馈编码，结束旧 turn 后不自动采样。reconciliation 再次中断不得重复已 durable 的 Hook completion、gate、result、outputs 或 terminal。

旧 turn reconciliation SHALL 在当前 Hook 来源 Load/install 之前完成，并始终持有原连续 lease；当前配置只服务未来调用。已删除或损坏配置不得改变历史补偿或恢复 bytes；旧事实不建立当前 trust。完成补偿后新 turn 重新接受当前配置和信任检查。与同一 reconciled history 的不中断路径比较时，双 Provider canonical request、native item 顺序、opaque bytes、context footprint 与 cache fingerprints MUST 相同；旧门禁恢复的配置 loader、信任、进程、工具与 Provider 网络调用次数均为零。


已规划 Post 的恢复 SHALL 区分真实结果与检查状态：原工具未 started/result 时按既有状态补偿并收口 skipped；tool started 无 result 时仍为原工具 outcome_uncertain，零 Post；成功 result 且有对应分支、无 Post started 时保留成功并收口 Post interrupted；Post started 无 finished 时追加 recovery-origin uncertain finished 和对应 Post terminal，不声称旧进程 cleanup 已确认。已 durable 的前缀反馈保持不变，未开始项不执行。部分 finished 后下一项尚未 started 时只追加 cancelled/session_interrupted_before_post_hook terminal，processed/unstarted_from 等于已有 finished 数量，不能生成 uncertain finished；下一项确实 started 未 finished 时，才为该项追加 recovery-origin uncertain finished 并与 uncertain terminal 同批，剩余后缀不伪造执行。已收口 skipped 后缀完全复用其原因和已冻结 suffix_feedback，不重新判断输入或预算。恢复新建的 terminal 明确 origin=recovery、suffix_feedback 为空；只组合已经 durable 的前缀/后缀模型 bytes，不为未开始项补发提示。完整 Post terminal 而 native outputs 缺失时只用真实结果与冻结反馈编码；outputs 已存在时只关闭旧 turn。所有状态均不读当前配置/脚本/artifact，不重建信任，不查询旧 PID，不重跑工具/Hook、不自动采样。第二次恢复不得重复任何已保存事实。

上述 Post 本地恢复 SHALL 同样适用于执行尝试准入后原 error/conflict 已 durable、选择 PostToolUseFailure 的调用，保留原错误与 failure 选择且不执行任何脚本。恢复必须先按同调用的准入/原结果验证 success/failure/none；当前 context、配置、预算和恢复模式不得改变选择。tool_execution_started 只代表执行尝试准入，准入后确定的启动失败仍选 failure；准入前准备错误、原结果 denied/cancelled/outcome_uncertain 为 none。none 不表示原工具没执行。确定失败且存在失败分支、无 Hook started 时只补 recovery cancelled/session_interrupted_before_post_hook terminal；已有 finished 前缀保持原 bytes；只有 Hook started 无 finished 的项补 recovery uncertain，和 terminal 同批。未选中分支没有运行补偿或反馈。

恢复生成 terminal 的 suffix_feedback MUST 为空；既有 terminal 的冻结前缀/后缀完全复用。所有 current 结构验证、双 Provider 事务式恢复和本地 reconciliation 始终持有连续 lease，早于当前来源 Load 与新输入。再次恢复不得重复 result、finished、terminal、outputs 或计费；canonical request/native order/opaque bytes/footprint/cache fingerprint 须等价于同一已补偿历史的不中断路径。

恢复时 route=none SHALL 保持零 Hook started/finished/反馈，并按已验证原事实补适用终态；工具尝试准入但 result 缺失时先沿既有规则补原 outcome_uncertain，再按 none 收口。success/failure 的对应事件未配置时 SHALL 保留该选择并补 recovery skipped/hook_event_unmatched，processed/unstarted_from=0、反馈为空，不要求不存在的分支有后缀。已经 durable 的实时取消 terminal 只复用，不用恢复原因替换：原 error 后当前取消形成的 failure/cancelled 不得重分类成 none。

#### Scenario: Close a patch before approval
- **WHEN** journal在Patch ready durable后、approval fact前结束
- **THEN** resume持久化`session_interrupted_before_approval`结果与matching output并关闭旧turn
- **THEN** approval presenter、候选准备器、Patch executor和Provider stream调用次数均为零

#### Scenario: Locally cancel an ordered ready group
- **WHEN** journal在含Glob、Grep、Read与Patch的ready batch后结束且没有任何started或approval
- **THEN** resume按原call index持久化只读调用的`session_interrupted_before_execution`与Patch的`session_interrupted_before_approval`结果及matching outputs
- **THEN** 所有executor、approval presenter与Provider stream调用次数均为零

#### Scenario: Locally cancel a ready Read
- **WHEN** journal在单个Read ready后结束且没有execution-start
- **THEN** resume使用原identity持久化`session_interrupted_before_execution` result与matching output，且Read executor调用次数为零

#### Scenario: Close an approved patch before execution
- **WHEN** journal包含matching allow fact但没有execution-started
- **THEN** resume持久化`session_interrupted_before_execution`结果且不恢复session grant或修改目标文件

#### Scenario: Fail closed on an uncertain patch
- **WHEN** Patch execution-started已durable但result缺失
- **THEN** resume追加`outcome_uncertain`结果与matching output，不读取目标或自动重试
- **THEN** 旧turn以一个失败终态关闭

#### Scenario: Resume a durable denied result
- **WHEN** deny decision与denied result均已durable但tool-output commit缺失
- **THEN** resume复用冻结preview和原wire kind生成逐字节相同的output
- **THEN** 不重新显示approval也不访问workspace

#### Scenario: Resume durable ordered results
- **WHEN** 异构调用组的全部result facts已按call index durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的有序模型previews提交output且所有executor调用次数不增加
- **THEN** resume追加一个`turn_failed`且不发起Provider请求

#### Scenario: Resume durable parallel results
- **WHEN** 一个并行只读组的全部result facts已按call index durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的有序模型previews提交output且所有executor调用次数不增加
- **THEN** resume追加一个`turn_failed`且不发起Provider请求

#### Scenario: Resume a durable Read result
- **WHEN** Read result已durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的preview提交output，且Read executor调用次数不增加

#### Scenario: Fail closed on uncertain searches
- **WHEN** Glob与Grep的execution-start已durable但results缺失
- **THEN** resume不调用搜索，按call index durable记录`outcome_uncertain`补偿并停止自动推进该turn

#### Scenario: Fail closed on an uncertain Read
- **WHEN** Read execution-start已durable但result缺失
- **THEN** resume不调用Read，durable记录`outcome_uncertain`并停止自动推进该turn

#### Scenario: Do not duplicate a durable output commit
- **WHEN** calls、approval、results和tool-output entry均完整durable但尚无terminal
- **THEN** resume只追加一个`turn_failed`，不重复output且不启动下一sample

#### Scenario: Reject a superseded development journal
- **WHEN** resume目标使用本变更前的Tool/approval/native payload shape或optional record envelope
- **THEN** 系统在repair、append、executor、Provider网络或SQLite写入前失败
- **THEN** journal bytes保持不变且不加载旧decoder

#### Scenario: Continue on the next user input
- **GIVEN** unfinished tool turn已完成本地补偿
- **WHEN** 用户提交下一次输入
- **THEN** Provider请求保持function/custom/tool-use call/output pairing和原生item顺序
- **THEN** canonical bytes与fingerprint等于从同一reconciled history不经重启构造的请求

#### Scenario: Preserve ownership during reconciliation
- **WHEN** 另一个进程竞争同一unfinished tool thread
- **THEN** 只有持有连续exclusive lease的进程可以reconcile，竞争者在读取后repair、Session append或任何外部副作用前以`session_busy`失败

#### Scenario: Do not repeat interrupted artifact reading
- **WHEN** ReadArtifact ready 或 started 后缺少 result
- **THEN** 恢复沿既有取消/不确定规则补偿，不访问文件或自动重新执行

#### Scenario: Reconcile a partly completed plan offline
- **WHEN** 计划有两项，第一项已 continue，第二项未 started
- **THEN** 不续跑第二项，追加 gate/tool cancelled 和原生配对，关闭旧 turn；所有外部执行调用次数为零

#### Scenario: Record uncertain hook recovery
- **WHEN** Hook started 已 durable 但 finished 缺失，且没有目标 tool started
- **THEN** 同批补偿 Hook 不确定、gate 不确定和目标工具 cancelled/hook_outcome_uncertain；不查询 PID、不读取 workspace 或配置

#### Scenario: Recover after configuration disappears
- **WHEN** Hook result 已 durable 而 native output 缺失，配置随后删除或损坏
- **THEN** 仅用冻结结果补齐原 call slot 的输出，canonical bytes/原生顺序/fingerprint 等于同一已补偿历史的不中断路径

#### Scenario: Repeat recovery after a second crash
- **WHEN** 第一次恢复已提交 Hook 补偿 batch 后再次退出
- **THEN** 第二次恢复复用已提交事实，不重复 finished/gate/result/output，旧 turn 最多一个失败 terminal

#### Scenario: Retain lease while a competing process attempts repair
- **WHEN** 持有者正在补偿 Hook，真实竞争进程请求同一 journal
- **THEN** 竞争者 session_busy，不能读取后 repair 或追加；持有者续写 seq 单调，失败路径 bytes 不变

#### Scenario: Recover after success but before the post check
- **WHEN** result=success 已 durable，Post plan 存在且包含成功分支，而无 Post started
- **THEN** 保留唯一 success，Post 记 interrupted 并补齐输出；原工具及 Hook 执行次数为零

#### Scenario: Recover an uncertain post run
- **WHEN** 成功 result 之后只有 Post started 而无 finished
- **THEN** 只补偿 Post uncertain，保留工具 success 及已完成反馈前缀，关闭旧 turn

#### Scenario: Recover frozen feedback without its source
- **WHEN** 结果、Post finished/completed 和反馈已 durable 而 output 缺失，配置/脚本随后消失
- **THEN** 离线生成相同配对 output、canonical bytes/原生顺序/fingerprint，不重算预算

#### Scenario: Preserve lease and idempotency during repeated recovery
- **WHEN** 真实竞争进程在 Post 补偿期间尝试打开/repair，且持有者补偿后再次崩溃
- **THEN** 竞争者 session_busy、失败 bytes 不变；二次恢复不重复 result/finished/completed/output/terminal，seq 单调

#### Scenario: Recover between post items without uncertainty
- **WHEN** 第 0 项 finished 已保存，第 1 项从未 started 而进程退出
- **THEN** 保留第 0 项及反馈，仅追加 cancelled/session_interrupted_before_post_hook terminal；第 1 项没有补偿 finished 或 cleanup 声明

#### Scenario: Recover a started next item separately from its suffix
- **WHEN** 第 0 项已 finished，第 1 项 started 未 finished，第 2 项尚未 started
- **THEN** 只为第 1 项补 recovery-origin uncertain finished，和 uncertain terminal 同批；第 2 项仍是未启动后缀

#### Scenario: Reuse a durable skipped suffix and closed budget
- **WHEN** skipped terminal 及最终预算已保存而 native outputs 缺失
- **THEN** 原因/前缀/反馈/释放余额不变，离线补输出不重复关闭调用或扣账

#### Scenario: Recover a definite failure before its diagnostic starts
- **WHEN** 原 error/conflict result 与双分支计划均已 durable，失败项未 started
- **THEN** 保留原错误，仅补失败分支 interrupted terminal 和配对 output；工具、Hook、信任和 Provider 网络调用次数为零

#### Scenario: Recover a started failure diagnostic
- **WHEN** 失败分支第 0 项 finished、第 1 项 started 未 finished、第 2 项未开始
- **THEN** 保留第 0 项反馈，只为第 1 项补 uncertain finished 并与 terminal 同批，第 2 项与成功分支无伪运行或新提示

#### Scenario: Recover without failure configuration or artifacts
- **WHEN** 失败反馈已 durable、native output 缺失，原配置/脚本/artifact 已删除
- **THEN** 只用冻结 bytes 补 output，恢复后 canonical request、原生顺序和 fingerprint 等价，不访问消失来源

#### Scenario: Keep a competing process outside failure reconciliation
- **WHEN** 恢复正在追加失败检查补偿，真实子进程竞争同一 journal 并尝试 repair
- **THEN** 竞争者 session_busy、失败路径 bytes 不变；持有者 seq 单调，崩溃释放后再次恢复不重复事实

#### Scenario: Preserve a live cancellation after a durable error
- **WHEN** 原工具准入与 error 已保存，当前取消又保存了 failure/cancelled terminal，之后恢复
- **THEN** 保留原错误、failure 选择和原 terminal/code，只补缺失 outputs；二次恢复不重复 terminal、output 或计费

#### Scenario: Recover an unmatched selected event
- **WHEN** 已准入 success 只有失败分支，或已准入 error/conflict 只有成功分支，而 terminal 缺失
- **THEN** 保留 success/failure 并补 recovery skipped/hook_event_unmatched，零计数/反馈且不要求伪后缀，不借用另一分支

#### Scenario: Do not infer a failed process from attempt admission
- **WHEN** tool_execution_started 已保存但原 result 缺失，进程是否启动或文件是否修改未知
- **THEN** 补原 outcome_uncertain，选择 none 并零诊断脚本，不推测原操作失败或未发生

#### Scenario: Select failure for a saved launch error offline
- **WHEN** 准入后进程尚未启动便形成确定 process_start_failed，原 error result 已保存且存在失败分支
- **THEN** 恢复仍选 failure，仅补检查中断终态和配对输出，不尝试启动原命令或诊断脚本

Task 的恢复 SHALL 作为无需审批的 exclusive 调用按原 call slot 补偿：ready 无 started 追加 cancelled/session_interrupted_before_execution；started 无 result 追加 outcome_uncertain；二者不应用候选，保留此前成功清单。已有成功 result 时从保存的完整规范更新恢复状态，即使该组 native outputs 尚未 durable 也不回退列表；缺失 outputs 仅用原冻结计数确认 preview 和原 call identity/wire 编码。失败结果不得携带 applied state，也不得借补偿把旧 candidate 标为成功。

恢复 MUST NOT 调用任务执行器、请求审批、运行工具 Hook、重渲染列表为新结果、按当前预算重算或自动继续模型。部分 Task results 按序保存后崩溃时，仅已成功部分推进状态，后续缺失 result 按普通取消/不确定路径补齐；再次崩溃不得重复 result、更新来源、outputs 或 terminal。完整错误和 checkpoint 来源校验仍早于 repair/补偿。

#### Scenario: Reconcile task readiness without applying it
- **WHEN** Task ready 已 durable 但没有 started，之前已有成功任务列表
- **THEN** 按原 invocation 补 before_execution 取消及 matching output，旧列表保持，任务执行器调用为零

#### Scenario: Reconcile an uncertain task conservatively
- **WHEN** Task started 已 durable 而 result 缺失
- **THEN** 补 outcome_uncertain 与 matching output，不推断候选已应用、不清空旧列表、不重新执行

#### Scenario: Recover success before native task outputs
- **WHEN** Task success result 已 Sync，而原生 outputs 或内存投影通知前退出
- **THEN** 完整列表及来源恢复恰好一次，原冻结确认 bytes 生成 matching output；下一请求 canonical bytes、原生顺序和 fingerprint 等于同一已补偿历史不中断路径

#### Scenario: Resume after only the first task replacement was saved
- **WHEN** 同一 sample 两个 Task ready 中仅第一个成功 result durable，第二个未开始或 started 无结果
- **THEN** 只安装第一个成功更新，第二个按真实状态补偿；二次恢复不重复应用第一项或执行第二项

### Requirement: Resume restores checkpoints and locally closes interrupted compaction

resume/continue SHALL 在连续 exclusive lease、完整 Loader/replay 和 Provider 兼容/原生验证后恢复最新合法 checkpoint 及后续增量，同时保留原始 transcript 和工具详情。只存在 started 的压缩尾部 SHALL 在接收新输入前本地 Sync 唯一 compaction_failed(code=compaction_interrupted)，保持旧窗口；不得自动联网生成摘要、重读文件或执行工具。completion 已完整持久化但原进程未 finalize 时 SHALL 恢复新窗口，不再追加失败或重复 checkpoint。最新 checkpoint 损坏、来源错配或 Provider 不兼容 MUST 拒绝整个恢复，不回退旧 checkpoint，也不写补偿。

当前启动项目指令/工具目录 SHALL 继续作为外部上下文；checkpoint 不恢复旧 cwd、secret、配置、队列或 permission grant。Catalog 继续可从 JSONL 重建，以最新合法 seq/timestamp 表示最近活动，不保存摘要正文。

#### Scenario: Close an interrupted summary locally
- **WHEN** 合法 journal 以 compaction_started 结束
- **THEN** 本地收口失败后暴露旧窗口，Provider/工具/文件重读次数均为零

#### Scenario: Resume after Sync before finalize
- **WHEN** completion 已 durable 而进程在内存切换前退出
- **THEN** 重启直接恢复新窗口，下一请求等于 uninterrupted，操作不重复计量或执行

#### Scenario: Fail compensation safely
- **WHEN** interrupted compaction 的补偿 append/Sync 失败
- **THEN** resume 失败并阻止新输入副作用，不放弃 cleanup ownership

#### Scenario: Validate before compensation
- **WHEN** journal 有未完成 started 但更早 checkpoint 来源错配或当前 Provider 配置不兼容
- **THEN** 拒绝恢复，不写 compaction_failed、不改变已有 journal bytes

#### Scenario: Keep history readable after compaction
- **WHEN** 用户恢复压缩过的会话且旧 artifact 文件缺失
- **THEN** 原历史及冻结工具 preview 仍可显示，续写只需原生窗口；按需读 artifact 才报告 unavailable

恢复所有 checkpoint SHALL 验证其必需 task_progress 与各自 compaction_started source 前缀的最新成功任务事实逐字一致。none 只能来自从无成功更新的前缀；clear/snapshot 必须匹配完整规范更新及真实 source result_sequence、turn_id、invocation_id。未来 Task 结果不参与旧 checkpoint 的期望值；恢复最终当前列表继续取完整 ledger 的最新成功结果，不仅取最后摘要自述或 checkpoint 当时列表。

Session/replay 只计算窄 typed 期望状态，Provider 在自身 codec 中验证当前 payload、generation 和完整确定性包装。当前 source identity 不进入模型包装或内容 fingerprint；ProjectCheckpoint 继续投影真实模型 generation，原生 footprint 覆盖其完整任务包装。缺失 task_progress 的旧开发 shape、来源/内容/原生包装错配以及伪造 clear/none MUST 在 repair、补偿、writer 接管和任何网络之前整体拒绝，不回退旧 checkpoint 或加默认字段。

#### Scenario: Restore a task-preserving checkpoint before later updates
- **WHEN** checkpoint 保存甲，后续 native 调用成功更新乙，之后进程退出
- **THEN** checkpoint 用旧 source 验证甲，当前列表从后续成功 result 恢复乙；请求只含有效原生窗口和后续增量，不额外注入第三份列表

#### Scenario: Validate an explicit clear against its saved result
- **WHEN** checkpoint 声称 clear，但 source 前缀最新成功更新为非空列表，或 source 指向失败结果
- **THEN** 恢复失败且 journal bytes 不变，不从模型摘要或当前 UI 推测修复状态

#### Scenario: Reject a missing task checkpoint field with a torn tail
- **WHEN** checkpoint 缺少必需 task_progress 且尾部另有半行
- **THEN** 在 repair/补偿之前拒绝整个恢复，所有旧 bytes 不变，不使用兼容 reader

#### Scenario: Resume task state after completion Sync before finalize
- **WHEN** 含精确任务快照的 completion 已 Sync，进程在内存窗口 finalize 前退出
- **THEN** 恢复新原生窗口及完整规范清单，generation 仍是真实模型文本，零摘要网络、任务执行、重复更新来源或重复计费

Session picker 的离线 reader SHALL 与在线恢复共用完整 Task ready/result 和 checkpoint expected evidence 装配；即使预览只返回文本也必须执行同等来源校验。合法任务 checkpoint 应可离线预览；source 指向 ready/detail/native seq、另一合法结果、未来成功结果，或原生任务 wrapper 被修改后重算 checksum，仍必须整体拒绝，不 repair、不补偿、不联网，原 bytes 不变。每个 checkpoint 的规范更新/状态和查询不携带 facade profile，原输入 profile 的验证不能因离线预览绕过。

恢复后普通、输入候选、摘要候选的规划 SHALL 分别使用对应已验证状态派生的 kind 与规范 domain.TaskUpdate 内容 digest，none 省略；它仅进入同一 committed_history canonical 元数据，不增加 token 或重复网络注入。完整来源 identity 必须校验，但不进入该 digest 或原生摘要包装。

#### Scenario: Preview a valid task checkpoint without model configuration
- **WHEN** Session picker 使用离线 StoredHistoryReader 预览两家合法的任务更新与 checkpoint
- **THEN** 原生输入/结果及 checkpoint 来源全部验证后返回有界预览，不要求模型连接、零网络、零 repair/追加且 bytes 不变

#### Scenario: Reject a forged result sequence in picker preview
- **WHEN** checkpoint 的 source result_sequence 被换成 ready/detail/native seq、其他合法结果或未来结果，checksum 已重新计算
- **THEN** 离线预览与在线恢复均整体拒绝，原文件包括半行尾部的全部 bytes 不变，零 repair/补偿/网络

#### Scenario: Reject a rewritten task wrapper in stored history
- **WHEN** checkpoint 任务 metadata 未变但原生 wrapper 的标签/状态/说明被修改，外层 checksum 已重新计算
- **THEN** 离线 reader 仍按同一 typed expected state 和 Provider codec 拒绝，不能因原生 JSON 自洽而返回部分预览
