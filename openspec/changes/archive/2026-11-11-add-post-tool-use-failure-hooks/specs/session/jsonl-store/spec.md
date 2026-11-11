## MODIFIED Requirements

### Requirement: Post hook facts preserve results and explicitly freeze disclosed feedback

唯一当前 Session vocabulary SHALL 使用 tool_post_hook_planned、tool_post_hook_completed，并使 hook_execution_started/finished 的当前身份显式包含事件/plan/ordinal。所有 kind MUST 有专属 typed draft/strict decoder/validator；Pre 的统计事实及未授权字段不能承载正文。不得新增内部格式版本、历史 reader、optional record 或迁移。

Post plan SHALL 单记录 Sync；有匹配 Pre 时必须在其 continue 后，无匹配 Pre 时不要求或伪造 Pre plan/continue，始终位于候选/普通批准前。它保存有序声明引用、披露策略及调用预算 ticket，不保存命令。真实 tool result SHALL 只写一次并先确定 Sync，Post started 单独 Sync 且仅在输入/剩余期限前检通过后接收；started 表示执行尝试准入，不保证 OS 启动成功。每项 finished 与 content/code/empty 反馈选择及预算增量原子保存，中间 finished 独立 batch，最后正常项 finished 与 completed terminal 同批。

terminal SHALL 保存 invocation/route/plan digest、origin=execution/recovery、disposition、code、processed、unstarted_from、suffix_feedback、最终预算事实、绑定最终规范反馈段的 feedback_digest 与绑定完整终态的 digest，不重复前缀正文。有对应选择分支时，N 为该分支长度，processed 等于已 finished 前缀数量，unstarted_from=processed，未启动后缀为 [processed,N)；无后缀时二者=N。completed 要求前缀完整且无 pending started，允许普通错误项，code 为 ok 或最早错误项 code；有选择分支的 skipped 必须存在未启动后缀，适用原因为 hook_input_limit 或 hook_execution_budget_exhausted。route=none 的 skipped 可使用 tool_not_successful、hook_trust_denied、hook_trust_required；success/failure 缺少对应分支的 skipped 使用 hook_event_unmatched。这两类无可运行分支的终态均要求 processed/unstarted_from=0、全部 Hook started/finished/反馈为空，不要求存在未运行后缀。未启动后缀只写 terminal，不伪造 started/finished/exit/timed_out/cleanup 统计；terminal 可独立 batch 或与尚未保存的原拒绝/取消 result 同批。

当前取消发生在信任阶段、首项前或项间时以 cancelled terminal 收口，原因为 cancelled 或 session_interrupted_before_post_hook；已 started 的取消项必须先有其 cancelled finished，并与 terminal 同批。选择为 none 的信任取消允许 cancelled/cancelled，但没有运行或反馈；success/failure 有对应分支时保留选择并取消检查，缺少对应分支时固定 skipped/hook_event_unmatched，turn 取消仍由外层处理。uncertain 要求存在最后 uncertain finished，并与 terminal 同批，后缀未启动；不得将纯粹尚未 started 的下一项标记 uncertain。原结果始终只写一次。origin=execution、选择分支存在且与原 durable 事实一致、Post 全部信任、对应声明为 model 且提示预算可用时，suffix_feedback 只允许为该分支未启动后缀冻结有序来源/code，不允许正文或运行统计，processed 不因此增加；在 terminal Sync 时计入反馈实际 bytes。origin=recovery、route=none、unmatched、未完成信任、feedback=none 或 budget_unavailable 的 suffix_feedback 必须为空，不能借恢复补发内容。

预算终值 SHALL 验证原 A/R、base allowance=A-R、真实结果 bytes、最终规范反馈段 bytes、released=R-feedback bytes；原 A SHALL 从同 sample/turn 已有事实重建，不能仅信任记录自述；反馈增量绑定单调 durable sequence，拒绝重复计费。分段只在确定 Sync 后计费，整个 call 在 terminal durable 后 close 一次。已保存前缀反馈不能为后续错误提示让位；所有提示槽须提前预留，delivery=budget_unavailable 时不得存在正文或模型提示。

唯一当前后置计划 SHALL 只在外层保存一次 invocation_id/capability，并保存 branches、allowance、reservation、digest。一至两个非空分支恰好包含 event、hooks、delivery，顺序为 PostToolUse、PostToolUseFailure，不嵌套完整单事件计划或重复保存分支级 invocation/capability/plan digest。缺失事件不保存空分支；分支事件唯一，引用事件必须与分支一致，同一来源在不同分支的 revision 必须一致。整个计划共享 A/R 和 digest，每分支 delivery 以同一 R 和该分支声明计算。结果未知的计划只列候选分支，不得保存选择或预设所选事件。旧单分支 plan/delivery、重复归属字段、未知/缺失字段 MUST 在 repair/append 前拒绝，不保留 reader、版本标记或迁移。

terminal 的 route SHALL 为 success/failure/none，并替代旧的单事件字段。route 必须从同一调用已验证的执行尝试准入/原结果交叉验证：准入后 ResultSuccess 为 success，准入后确定 error/conflict 为 failure，无准入或原结果 denied/cancelled/outcome_uncertain 为 none；Exec 不允许 conflict。tool_execution_started 不证明进程启动或文件改变；准入后的确定启动失败仍可选择 failure。none 不表示原工具没执行。尚无确定 durable 原结果时不得选择或构造后置运行事实；当前 context、配置、预算、恢复模式均不得改变选择。

原 error 已 Sync 后当前 turn 才取消时，terminal 必须保留 failure；对应分支存在时按 cancelled 收口，不能写 none。原 result 自身 cancelled 才是 none。只有实际合法接收的 Hook started 才能有对应 finished；原结果 cancelled/denied/outcome_uncertain 时不能存在已运行后置项。success/failure 无匹配分支时仅接受 skipped/hook_event_unmatched 且所有运行/反馈为空；另一分支不得生成运行事实或模型提示。

所有 run/finished/反馈 SHALL 绑定实际 event、全计划 digest 和所选分支 ordinal。即使两个分支第 0 项具有相同命令，也不得互换 started/finished；同调用只允许一个 terminal 和一次 budget close。上述固定当前结构覆盖纯成功、纯失败、双分支及 unmatched，不以 optional record 绕过完整校验。

#### Scenario: Persist output separately from its check
- **WHEN** 工具 success 已 Sync 随后后置 finished 的 Sync 失败
- **THEN** 原 result 保留，反馈不被宣称 durable，Runtime poison 且不发起下一 sample

#### Scenario: Validate the current fixed fixture bytes
- **WHEN** 加载三事件正常、跳过、反馈关闭、每崩溃点、伪造内容与越限 current fixtures
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

#### Scenario: Reject a failure run without execution evidence
- **WHEN** 失败分支 started 出现在原 result 之前，或原工具没有 started、结果被拒绝/取消/不确定
- **THEN** 当前回放拒绝，journal 不修复或追加，不通过错误字符串猜测资格

#### Scenario: Reject another branch with valid checksums
- **WHEN** 原工具 error 而 finished 指向成功事件，或 terminal 的 route/digest/预算与前序事实不符
- **THEN** 即使 checksum 正确且文件另有半行，也在 repair 前拒绝且 bytes 不变

#### Scenario: Load the replacement current branch fixtures
- **WHEN** 加载仓库固定的成功、失败、双分支、unmatched 及各崩溃切点 fixture
- **THEN** 生产 decoder/replay 严格验证单路由、原结果只一次、预算只关闭一次；测试运行时不生成这些输入 bytes

#### Scenario: Reject duplicated call ownership inside a branch
- **WHEN** 当前计划分支带 invocation_id/capability/plan digest，或引用事件/来源 revision 与外层分支关系冲突
- **THEN** strict decoder/validator 拒绝，不将重复归属解释为第二条合法选择来源

#### Scenario: Keep failure selection in a cancellation terminal
- **WHEN** 已准入 error result 保存后 turn 取消，匹配失败分支没有 started
- **THEN** 接受 failure/cancelled 的零运行终态，拒绝将同一原结果伪写为 none；原错误 bytes 不变

#### Scenario: Keep an unmatched terminal free of fake suffixes
- **WHEN** 只配置失败分支而原工具已准入成功，或只配置成功分支而原工具已准入确定失败
- **THEN** 保留 success/failure 选择并接受 skipped/hook_event_unmatched、零计数和空反馈，不要求未配置分支有后缀
