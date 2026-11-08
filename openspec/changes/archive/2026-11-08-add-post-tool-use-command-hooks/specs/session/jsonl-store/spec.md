## MODIFIED Requirements

### Requirement: Replay validation enforces record state transitions

Loader 完成字节、envelope 和 batch 校验后，ReplayPlanner SHALL 在创建 Provider Conversation 前对全部当前 typed records 执行语义回放。首个 batch MUST 恰好建立唯一 `session_meta` 与 root `thread_meta`；同一 root thread 最多有一个活动 turn。

文本 turn SHALL 在 turn_started 后先完成 initial 输入 batch，才可通过 [provider_native_commit(sample), sample_usage, turn_completed] 完成；单个 turn_failed 可在输入提交前或后收口，保留此前全部已提交事实。Tool Loop 中，一个含 calls 的 sample batch MUST 严格为 `[provider_native_commit, sample_usage, tool_call_ready...]`，ready records 的 sample/call index 连续且 identity 唯一。ReplayPlanner SHALL 按 capability 验证每个 call slot：自动允许的读取正常为 `ready -> execution_started -> result`；Patch 可为 `ready -> approval_decided(allow*) -> execution_started -> result`、`ready -> [approval_decided(deny), result(denied)]`，或在候选准备失败/取消时从 ready 直接进入匹配的 error/cancelled result。`abort` SHALL 使当前及尚未完成 slots 按取消契约收口。approval identity、candidate fingerprint、outcome 和 decision source MUST 与同一 invocation 一致，且每个 Patch 至多一个 approval fact。

调度 records SHALL 符合连续 `parallel_read` batch 与 `exclusive` 屏障：一个 read batch 的 starts 按 call index先于该 batch 的 results，Patch 的 started/result 位于前后 batch 之间；全部 result records仍按原 call index单调提交。全部 results 存在后，恰好一个按 call index与原 wire kind 配对的 tool-output `provider_native_commit` 关闭该 call group，之后才可出现可选 steering 输入 batch、下一 sample 或 turn failure。

ReplayPlanner MUST 拒绝孤立/重复 approval、result 或 started、allow 后缺失 candidate fingerprint、deny 后出现 started、started 早于必需 allow、started/result index 非法、越过 exclusive 屏障、ready 后直接开始下一 sample、outputs 数量/wire kind/call identity 不匹配、sample usage 缺失/重复、tool-output commit 携带 usage、terminal 后 records 或新 turn 覆盖活动 turn。只有 journal 末尾已承诺的活动状态可作为显式 reconciliation 计划返回；它不是可由 Loader 截断的文件损坏。

本条中 Patch 的 approval/started/result、exclusive 屏障和恢复规则 SHALL 同样适用于 Write。Write 准备失败可从 ready 进入 error/conflict/cancelled，但 success 或 outcome_uncertain 必须具有合法 started；write_no_changes 为 error。Write input/result 联合与 operation 的已知/未知状态必须严格验证，恢复不通过文件系统补齐。

对于有 PreToolUse Hook plan 的 call slot，ReplayPlanner SHALL 先验证独立 Hook 前缀：每调用至多一个 plan/decision，ordinal 连续且匹配计划，每项至多一个 started/finished，前项 continue 才可启动下一项。全计划成功完成才允许 gate continue；trust 拒绝可在未 started 的下一项处短路。任一 Hook 阻断、error 或 cancelled 只结束该 call slot；cleanup 不确定另行记录，目标工具不得出现 started。普通 approval/候选执行事实不得先于 gate continue，后序 call 不得越过当前 exclusive result。

计划和每项 started SHALL 单独 batch；中间成功 finished 单独 batch；最后成功 finished 与 gate continue MUST 同批；executed 阻断/错误/取消的 finished、gate decision 和冻结 tool result MUST 同批，trust 拒绝则 gate decision/result 同批，不伪造执行记录。正常 gate 终止状态与 result code/status MUST 一致。恢复补偿 Hook 不确定时，finished(origin=recovery, cleanup_confirmed=false, exit unknown)、gate outcome_uncertain 与工具 cancelled/hook_outcome_uncertain MUST 同批。完整 committed 记录违反上述组合为 corruption，不得当成合法活动尾部。

正常路径 started 后 cleanup 不确定或 runner 结果错配 MUST 记录 gate outcome_uncertain、工具 cancelled/hook_outcome_uncertain，并停止后续工具；健康 journal 按 call index 完成剩余取消配对，owner 未真正清理完成前不能释放 lease。缺失 finished 的合法末尾 SHALL 投影为独立 Hook recovery 状态，不得退化为普通 ready-before-approval 或目标工具 started。


存在 tool_post_hook_planned 时，call slot 的真实 result 与 Post 状态 SHALL 分别验证。前述每调用一个 gate/decision 及其执行前批次限制仅指 Pre；后置 plan/terminal 独立且至多一次。真实成功 result 后才可出现同事件/plan 的 Hook started；后序 call admission 和 outputs MUST 等待 Post terminal，不以 result 存在推断检查完成。后置 finished 只能携带已授权且符合预算的冻结反馈；普通后置 error 允许下一 ordinal 开始，不适用 Pre 的 continue-only 前缀规则；无成功 result、事件/ordinal 错配、缺少必要证据或后置 terminal 后再执行均作为 corruption。

#### Scenario: Build a valid approved patch replay plan
- **WHEN** journal 包含 Patch ready、matching allow、started、result、custom或tool-result output与最终 sample completion
- **THEN** ReplayPlanner 保留每个状态、candidate fingerprint、原生 wire kind和严格顺序

#### Scenario: Build a valid parallel tool replay plan
- **WHEN** journal包含异构只读batch、exclusive Patch、按index的results、tool outputs和最终sample completion
- **THEN** ReplayPlanner按seq返回native commits、sample usages、每个ledger状态和completed turn

#### Scenario: Build a valid text replay plan
- **WHEN** journal包含metadata、一个失败文本turn和一个完整文本turn
- **THEN** ReplayPlanner产生当前schema定义的native commits、usage与terminal投影

#### Scenario: Build a valid tool replay plan
- **WHEN** journal包含合法call sample、有序approval/ledger、tool outputs和最终sample completion
- **THEN** ReplayPlanner按seq返回当前native commits、sample usages、ledger状态和completed turn

#### Scenario: Build a valid denied patch replay plan
- **WHEN** journal 包含 Patch ready以及同 batch 的deny decision与denied result，且没有started
- **THEN** ReplayPlanner接受该终态并要求最终output匹配原Provider call kind和ID

#### Scenario: Reject execution without approval
- **WHEN** Patch ready后直接出现execution-started或allow fact的candidate fingerprint不匹配
- **THEN** resume以稳定Session corruption错误失败且不创建Conversation或调用executor

#### Scenario: Reject a checksum-valid illegal transition
- **WHEN** JSON、seq、batch和checksum合法但approval、usage、ledger顺序、call index或terminal placement非法
- **THEN** resume以稳定Session corruption错误失败且不创建Conversation或调用executor

#### Scenario: Enforce an exclusive scheduling barrier
- **WHEN** call group按Read、Patch、Read排列但后一个Read在Patch result前started
- **THEN** ReplayPlanner拒绝该journal而不调用任何工具

#### Scenario: Report an approval-interrupted tail
- **WHEN** journal在Patch ready后结束且没有approval fact
- **THEN** ReplayPlanner返回`session_interrupted_before_approval` reconciliation状态而不重新询问

#### Scenario: Report a committed interrupted tail
- **WHEN** 文本journal以完整committed`turn_started`结束且没有后续事实
- **THEN** ReplayPlanner保留该record并返回interrupted-tail状态而不截断

#### Scenario: Report a reconcilable ready tail
- **WHEN** journal在合法ready batch后结束且调用没有started或Patch没有approval
- **THEN** ReplayPlanner保留committed records并按capability与call index返回对应本地取消补偿状态

#### Scenario: Report an approved but unstarted tail
- **WHEN** journal包含Patch allow fact但没有execution-started
- **THEN** ReplayPlanner返回`session_interrupted_before_execution` reconciliation状态而不执行Patch

#### Scenario: Report an uncertain execution tail
- **WHEN** journal包含合法execution-started但缺少对应result
- **THEN** ReplayPlanner返回必须补偿的outcome-uncertain状态且不得自动重试

#### Scenario: Accept cancellation before executor start
- **WHEN** journal包含ready后直接追加的matching cancelled result
- **THEN** ReplayPlanner接受该终态，并拒绝同一invocation后续出现started、approval或第二个result

#### Scenario: Reject non-cancelled result before executor start
- **WHEN** journal包含ready后直接追加的success或outcome-uncertain result，且不是合法候选准备error、Write准备conflict或approval deny路径
- **THEN** ReplayPlanner以稳定Session corruption错误失败

#### Scenario: Reject completion-order persistence
- **WHEN** worker实际以call 2、call 0、call 1完成且journal也按该完成顺序写入results
- **THEN** ReplayPlanner以稳定Session corruption错误拒绝该journal

#### Scenario: Reject Write execution without approval
- **WHEN** Write ready 后直接出现 started 或 success，缺少 matching allow
- **THEN** replay 失败关闭且不修改 journal/workspace

#### Scenario: Accept a pre-approval Write conflict
- **WHEN** Write 在准备时发现完整读取证据与当前文件不匹配
- **THEN** ready 后的 conflict result 合法且不存在 started，后续配对 output 仍必须出现

输入 batch SHALL 严格为 [turn_inputs_committed, provider_native_commit(user_inputs)]。initial/target=0 只能在 start 后首 sample 前出现一次；steering/target=n 只能在 sample n-1 整组 outputs 闭合后、sample n 前出现至多一次。sample 索引从 0 连续递增，输入提交不增加 sample 计数。同一 InputID 不得重复提交。回放必须给 native restore 提供真实 turn、期望角色和关联，不得按 batch 长度猜测 opaque kind。

#### Scenario: Reject illegal input placement
- **WHEN** checksum 正确但输入在未闭合工具组内、terminal 后、重复 target 或 initial 缺失时出现
- **THEN** 回放在 Provider 恢复和副作用前失败，不截断合法 committed bytes

#### Scenario: Preserve an input-only interrupted tail
- **WHEN** journal 在完整 initial 或 steering 输入 batch 后中断
- **THEN** 回放保留输入及关联，并返回本地 lifecycle reconciliation，不请求模型或执行工具

普通 turn grammar 保持以上规则；手动压缩是与 turn 互斥的独立生命周期。compaction_started 必须为单记录 batch，绑定当前来源 revisions/前驱/digest 和 scope。manual 仅在 idle 开始；auto 仅在关联 turn 的首次输入提交前，或完整工具 outputs 已提交且下一 sample 尚未开始时允许，target sample_index 必须精确匹配。同一 sample 最多一次 auto started，未闭合调用、已保存当前 sample 输入或重复自动摘要必须拒绝。started 之后恰好一个同身份、同 scope 的 completed 或 failed 才允许普通事实继续，期间不能插入 turn/input/sample/tool 记录。completed 必须含有界 checkpoint envelope、normalized usage、前后估算并原子提交；failed 必须含 typed 原因和 observed/unavailable 计量状态。compaction record 的 envelope TurnID 始终为空，普通 turn 关联只在 scope 中表达，不冒充普通 native sample/usage。

#### Scenario: Replay a compacted thread
- **WHEN** 完整 turn 之后记录 started/completed，再记录新 turn
- **THEN** replay 返回有序普通 commits、checkpoint 和独立操作事实，原 native ordinal 不变

#### Scenario: Reject overlapping operations
- **WHEN** 活动 turn 的非法位置出现 auto compaction_started、活动 turn 出现 manual compaction_started，或活动压缩中出现新 turn/第二个 started
- **THEN** 语义回放失败，journal 不进入可继续状态

#### Scenario: Reject invalid compaction terminals
- **WHEN** completion 无 started、身份错配、重复 terminal、缺失 checkpoint/usage 或 failed 含可安装窗口
- **THEN** strict decoder/replay 拒绝，不通过默认值修复

独占调度 SHALL 与需要审批分开验证。Question 的正常语法为 ready -> started -> result，started 必须晚于前序结果且不需要 allow；Question 携带任何 approval fact 必须拒绝。ready 可直接关闭为取消或无交互/预算错误，但 success 和 outcome_uncertain 必须有 started。仅 ready 的提问恢复为 session_interrupted_before_execution；started 无结果恢复为 outcome_uncertain；已有 result 不重新生成回答。后序调用越过等待问卷或其 result、答案与题目不匹配、重复 resolution 或伪造权限都必须失败关闭。

#### Scenario: Validate a nonapproval exclusive call
- **WHEN** journal 为 Read result、Question started/result、Write allow/started/result 的合法有序事实
- **THEN** replay 接受提问无需 approval，但仍验证 Write 的完整批准前置条件

#### Scenario: Reject question approval facts
- **WHEN** journal 为 user.question 记录了 tool_approval_decided
- **THEN** 恢复整体拒绝且不 repair，不把提问选择视为副作用授权

#### Scenario: Reject a gate that skips a planned hook
- **WHEN** checksum 合法且 plan 有两项，但只有第一项 finished 后便记录 gate continue 或 tool approval
- **THEN** 整体恢复失败，journal bytes 不变，不读取配置或启动进程

#### Scenario: Require an atomic denial result
- **WHEN** 完整 committed batch 保存 gate deny 却没有同批 tool result，或拒绝后出现 tool started
- **THEN** 语义损坏被拒绝，不能由 repair 删除已提交记录

#### Scenario: Keep a hook-started tail distinct
- **WHEN** 合法 journal 恰好以 hook_execution_started 结束
- **THEN** 返回包含原计划和未完成 ordinal 的 recovery 子状态，不伪造 tool_execution_started

#### Scenario: Reject corruption even with a torn suffix
- **WHEN** 计划/ordinal/身份非法的完整前缀后另有半行
- **THEN** 恢复失败且全部 journal bytes 不变，禁止先截断再校验

## ADDED Requirements

### Requirement: Post hook facts preserve results and explicitly freeze disclosed feedback

唯一当前 Session vocabulary SHALL 新增 tool_post_hook_planned、tool_post_hook_completed，并使 hook_execution_started/finished 的当前身份显式包含事件/plan/ordinal。所有 kind MUST 有专属 typed draft/strict decoder/validator；Pre 的统计事实及未授权字段不能承载正文。不得新增内部格式版本、历史 reader、optional record 或迁移。

Post plan SHALL 单记录 Sync；有匹配 Pre 时必须在其 continue 后，无匹配 Pre 时不要求或伪造 Pre plan/continue，始终位于候选/普通批准前。它保存有序声明引用、披露策略及调用预算 ticket，不保存命令。真实 tool result SHALL 只写一次并先确定 Sync，Post started 单独 Sync 且仅在输入/剩余期限前检通过后接收；started 表示执行尝试准入，不保证 OS 启动成功。每项 finished 与 content/code/empty 反馈选择及预算增量原子保存，中间 finished 独立 batch，最后正常项 finished 与 completed terminal 同批。

terminal SHALL 保存 invocation/event/plan digest、origin=execution/recovery、disposition、code、processed、unstarted_from、suffix_feedback、最终预算事实、绑定最终规范反馈段的 feedback_digest 与绑定完整终态的 digest，不重复前缀正文。processed 等于已 finished 前缀数量，unstarted_from=processed，未启动后缀为 [processed,N)；无后缀时二者=N。completed 要求前缀完整且无 pending started，允许普通错误项，code 为 ok 或最早错误项 code；skipped 要求存在未启动后缀，原因为 tool_not_successful、hook_trust_denied、hook_trust_required、hook_input_limit 或 hook_execution_budget_exhausted。未启动后缀只写 terminal，不伪造 started/finished/exit/timed_out/cleanup 统计；terminal 可独立 batch 或与尚未保存的原拒绝/取消 result 同批。

取消发生在项间时以 cancelled terminal 单独收口，原因为 cancelled 或 session_interrupted_before_post_hook；已 started 的取消项必须先有其 cancelled finished，并与 terminal 同批。uncertain 要求存在最后 uncertain finished，并与 terminal 同批，后缀未启动；不得将纯粹尚未 started 的下一项标记 uncertain。原结果始终只写一次。origin=execution 且原工具成功、Post 全部信任、对应声明为 model 且提示预算可用时，suffix_feedback 只允许为未启动后缀冻结有序来源/code，不允许正文或运行统计，processed 不因此增加；在 terminal Sync 时计入反馈实际 bytes。origin=recovery、原工具不成功、未完成信任、none 或 budget_unavailable 的 suffix_feedback 必须为空，不能借恢复补发内容。

预算终值 SHALL 验证原 A/R、base allowance=A-R、真实结果 bytes、最终规范反馈段 bytes、released=R-feedback bytes；原 A SHALL 从同 sample/turn 已有事实重建，不能仅信任记录自述；反馈增量绑定单调 durable sequence，拒绝重复计费。分段只在确定 Sync 后计费，整个 call 在 terminal durable 后 close 一次。已保存前缀反馈不能为后续错误提示让位；所有提示槽须提前预留，delivery=budget_unavailable 时不得存在正文或模型提示。

#### Scenario: Persist output separately from its check
- **WHEN** 工具 success 已 Sync 随后后置 finished 的 Sync 失败
- **THEN** 原 result 保留，反馈不被宣称 durable，Runtime poison 且不发起下一 sample

#### Scenario: Validate the current fixed fixture bytes
- **WHEN** 加载两事件正常、跳过、反馈关闭、每崩溃点、伪造内容与越限 current fixtures
- **THEN** 使用仓库固定 bytes 和生产 decoder/replay 验证；不在测试运行时由 encoder 生成输入

#### Scenario: Reject a cross-event run identity
- **WHEN** 同 invocation/ordinal 的 Post finished 指向 Pre plan 或错误 digest
- **THEN** committed 前缀作为 corruption 拒绝，即使另有半行也不先 repair

#### Scenario: Record an unstarted suffix without fake execution
- **WHEN** Post 第 0 项已 finished，而剩余预算不足以准入第 1 项
- **THEN** 只追加 skipped terminal，processed/unstarted_from 均为 1，原因 hook_execution_budget_exhausted，后缀无 started/finished/实际超时统计

#### Scenario: Start no post runs for an oversized input
- **WHEN** 成功工具之后的共享 stdin 超限且 Post 共有两项
- **THEN** 保存 processed=0/unstarted_from=0 的 skipped terminal/hook_input_limit，原成功不变

#### Scenario: Continue after an ordinary post failure
- **WHEN** 后置第 0 项为普通解析错误且 cleanup confirmed，随后第 1 项成功
- **THEN** 回放接受两个已运行项的有序前缀及 completed terminal，不要求第 0 项 continue，不改写工具成功

#### Scenario: Reject inconsistent call budget facts
- **WHEN** terminal 的 A/R/base allowance、分段 bytes、释放余额或 delivery 与前序事实不符
- **THEN** 严格回放拒绝，不用当前配置重分配或削减已保存反馈
