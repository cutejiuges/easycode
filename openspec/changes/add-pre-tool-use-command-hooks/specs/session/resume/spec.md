## MODIFIED Requirements

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


已规划 Post 的恢复 SHALL 区分真实结果与检查状态：原工具未 started/result 时按既有状态补偿并收口 skipped；tool started 无 result 时仍为原工具 outcome_uncertain，零 Post；成功 result 无 Post started 时保留成功并收口 Post interrupted；Post started 无 finished 时追加 recovery-origin uncertain finished 和对应 Post terminal，不声称旧进程 cleanup 已确认。已 durable 的前缀反馈保持不变，未开始项不执行。部分 finished 后下一项尚未 started 时只追加 cancelled/session_interrupted_before_post_hook terminal，processed/unstarted_from 等于已有 finished 数量，不能生成 uncertain finished；下一项确实 started 未 finished 时，才为该项追加 recovery-origin uncertain finished 并与 uncertain terminal 同批，剩余后缀不伪造执行。已收口 skipped 后缀完全复用其原因和已冻结 suffix_feedback，不重新判断输入或预算。恢复新建的 terminal 明确 origin=recovery、suffix_feedback 为空；只组合已经 durable 的前缀/后缀模型 bytes，不为未开始项补发提示。完整 Post terminal 而 native outputs 缺失时只用真实结果与冻结反馈编码；outputs 已存在时只关闭旧 turn。所有状态均不读当前配置/脚本/artifact，不重建信任，不查询旧 PID，不重跑工具/Hook、不自动采样。第二次恢复不得重复任何已保存事实。

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
- **WHEN** result=success 已 durable，Post plan 存在而无 Post started
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
