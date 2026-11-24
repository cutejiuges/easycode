## ADDED Requirements

### Requirement: A branch journal preserves an exact committed prefix and a single owner chain

分支 journal SHALL 逐字节保留源线程从首行至分支切点的全部 committed JSONL bytes，包括每行末尾的 LF。每次创建 SHALL 在该前缀之后追加恰好一条新的 `thread_forked` 事实。继承前缀中的原始标识、序号、时间、batch、checksum 与 payload MUST 保持原样，包括此前的分支事实。分支标记不推进原生历史 ordinal、sample、usage、checkpoint 或任务序号。分支自己的后续事实使用当前线程标识并从标记的下一 seq 单调追加。

`thread_forked` SHALL 使用现有无版本 envelope，具备专属 typed constructor、strict decoder 和 validator。外层 `session_id` MUST 等于根 Session 标识，`thread_id` 为本链从未出现的有效子线程标识，`parent_thread_id` 为前一段的已提交线程标识，`timestamp` 为子线程创建的 UTC 时间，`turn_id` MUST 缺席。其完整行 MUST 声明独立单记录 batch，`batch_id=seq`、`batch_index=0`、`batch_size=1`，并遵守当前 16 MiB 行及 64 MiB batch 上限。

标记 payload SHALL 仅含三个必需字段 `source_seq`、`source_sha256`、`title`。`source_seq` 为正整数，MUST 等于前一完整 batch 的最后 seq 且等于标记 seq 减一。`source_sha256` MUST 是紧邻标记之前全部原始 bytes 的 SHA-256，表示为 64 个小写十六进制字符。`title` MUST 满足 session/titles 的非空规范标题约束，未显式命名时的默认标题仍以同一字段写入。缺失必需字段、重复或未知字段、null、尾随 JSON 及错配证据 MUST 拒绝。标记 MUST NOT 保存配置、路径、会话内容副本、批准、权限或格式标记。

同一文件 SHALL 只有一条继承链。文件首 batch 仍是源根线程的唯一 `session_meta` 和 `thread_meta`。工作目录、Provider family、wire 和 model 沿用根 metadata，分支的创建时间和初始标题来自其自身标记。每个 `thread_forked` 只可从当时的已提交线程标识过渡到未见过的子线程标识。其他 records MUST 属于当前段的线程，并沿用其现有外层标识规则。验证 SHALL 按文件顺序检查整个链，只依赖当前 journal 中的事实，不读祖先 journal 或使用 Catalog 代替证明。验证整个链时对已读前缀的字节完整性检查 SHALL 随文件大小线性增长，不能随标记数反复扫描继承前缀。

#### Scenario: Retain the exact prefix through a recursive branch
- **WHEN** 根线程的分支追加完成的回合，此后从该分支继续派生
- **THEN** 新 journal 逐字节保留源分支的完整提交前缀及既有标记，仅追加一个新的合法标记，后续本地事实从其下一 seq 起写入
- **THEN** 即使祖先 journal 不可访问，当前 journal 仍可完整验证与恢复

#### Scenario: Reject a forged or misplaced fork marker
- **WHEN** 标记 checksum 正确，但父线程、Session、子线程唯一性、前缀 seq、batch 或 payload 不符，或普通行冒用其他线程身份
- **THEN** 在 repair、writer 接管和恢复副作用前拒绝，原 journal bytes 保持不变

#### Scenario: Verify the digest against exact bytes
- **WHEN** 继承记录的字节改动仍能通过单行 canonical checksum，但其原始行或 LF 与已保存的前缀摘要不符
- **THEN** 拒绝整个 journal，即使在末尾另外存在可识别的半行

## MODIFIED Requirements

### Requirement: Loading validates the complete journal before replay

Session loader SHALL 按行和 batch 顺序校验记录大小、JSON、完整性校验值、seq 连续性、标识一致性和 batch 闭合性。标识一致性涵盖同一 Session 内经验证的分支标记转换，文件其余位置不允许线程标识漂移。当前 envelope MUST 拒绝重复顶层 member、未知顶层 member、尾随 JSON value、非法 UTF-8、非 canonical ID/时间字段和任一未知 event kind。checksum SHALL 覆盖移除 checksum 字段后的强类型 canonical envelope bytes；Provider payload 内部语义仍由对应 Provider 当前 codec 校验。

任何含非法版本字段的 envelope、未知 kind、文件中间损坏、seq 跳变、标识漂移或重复 metadata MUST 产生明确安全错误。loader MUST NOT 返回部分可恢复历史、fallback 到旧 decoder、保留 optional opaque record、执行 migration 或用默认值补全。

#### Scenario: Load a valid current journal
- **WHEN** thread 文件包含当前字段结构、受支持 kind 且 seq 连续的完整 batches
- **THEN** loader 按 seq 返回所有强类型 records，且不会重排或改写 payload

#### Scenario: Load a valid journal
- **WHEN** thread 文件包含当前 schema 的连续完整 batches
- **THEN** loader 返回全部强类型 records 和下一 seq，不修改 journal bytes

#### Scenario: Reject corruption in the middle
- **WHEN** 一个非尾部 record JSON 损坏、超出大小上限、seq 不连续或标识与文件元数据不一致
- **THEN** loader 返回稳定英文 session corruption 错误并且不返回部分可恢复历史

#### Scenario: Reject any unsupported record shape
- **WHEN** journal 包含未知 kind、非法附加版本字段或不符合当前字段结构的 payload
- **THEN** resume 在 repair、append、executor 或 Provider 副作用前失败
- **THEN** 原始 journal bytes 保持不变且未知 payload 不进入日志或错误文本

#### Scenario: Reject an unsupported required record
- **WHEN** journal 包含当前程序不支持的 event kind 或 payload 字段结构
- **THEN** resume 在任何 repair、append 或外部副作用前失败，原始 bytes 保持不变

#### Scenario: Reject ambiguous JSON envelope fields
- **WHEN** 一行包含重复 `seq`、未知当前顶层字段或一个合法对象后的尾随 JSON value
- **THEN** loader 将该行视为损坏并且不得依赖 JSON decoder 的覆盖或宽松默认行为

Load SHALL 只读扫描并返回完整 committed 前缀和私有待修复凭据，不执行 Truncate/Sync。恢复协调者 MUST 在全部 replay、Provider 兼容和原生/checkpoint 校验通过后才执行显式 Repair。Catalog SHALL 只读扫描，不隐式修复。对于非空 committed 前缀，loader MUST 先证明最后完整 committed batch 确认的线程标识与 journal 文件名的期望标识相同，才可签发尾部修复凭据。完整的非法标记不能作为未完成 batch 丢弃。

#### Scenario: Reject semantic corruption with a torn tail
- **WHEN** committed 前缀语义非法或 Provider/checkpoint 校验失败，且末尾另有半行或未完成 batch
- **THEN** 恢复拒绝，原 journal 全部 bytes 不变，不启动 writer 或补偿

#### Scenario: Bind the filename to the final committed owner
- **WHEN** 分支文件的最后完整 committed batch 对应的线程标识与文件名指定的线程标识不同，包括目标标记尚未完整写入或只复制了源前缀
- **THEN** loader 在发出任何待修复凭据前拒绝，原文件 bytes 保持不变，且不会以祖先身份返回该文件或为其启动 writer

#### Scenario: Repair only the final branch suffix
- **WHEN** 分支的最后合法标记及其已有合法 committed batches 共同确定了与文件名相符的线程标识，但其后存在半行或未完成的本地 batch
- **THEN** loader 验证全部 committed 事实和继承证据后，仅允许按当前规则显式修复未完成本地尾部，并完整保留标记及其继承前缀

### Requirement: Replay validation enforces record state transitions

Loader 完成字节、envelope 和 batch 校验后，ReplayPlanner SHALL 在创建 Provider Conversation 前对全部当前 typed records 执行语义回放。首个 batch MUST 恰好建立唯一 `session_meta` 与 root `thread_meta`；同一已验证线程段最多有一个活动 turn。分支标记仅可置于已有提交用户输入、所有 turn、工具调用与原生 outputs、Stop、Compaction 与 PostCompact 生命周期均已闭合的边界，且标记 MUST NOT 关闭、补偿或重新开始此前操作。只有最终线程段的合法活动尾部可生成恢复计划。

文本 turn SHALL 在 turn_started 后先完成 initial 输入 batch；无 Stop 时才可通过 [provider_native_commit(sample), sample_usage, turn_completed] 完成，有 Stop 时必须先按独立完成检查 grammar 保存 sample/usage/plan 再收口；单个 turn_failed 可在输入提交前或后收口，保留此前全部已提交事实。Tool Loop 中，一个含 calls 的 sample batch MUST 严格为 `[provider_native_commit, sample_usage, tool_call_ready...]`，ready records 的 sample/call index 连续且 identity 唯一。ReplayPlanner SHALL 按 capability 验证每个 call slot：自动允许的读取正常为 `ready -> execution_started -> result`；Patch 可为 `ready -> approval_decided(allow*) -> execution_started -> result`、`ready -> [approval_decided(deny), result(denied)]`，或在候选准备失败/取消时从 ready 直接进入匹配的 error/cancelled result。`abort` SHALL 使当前及尚未完成 slots 按取消契约收口。approval identity、candidate fingerprint、outcome 和 decision source MUST 与同一 invocation 一致，且每个 Patch 至多一个 approval fact。

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

普通 initial/工具 steering 输入 batch SHALL 严格为 [turn_inputs_committed, provider_native_commit(user_inputs)]；Stop 的真实追加输入仅可作为其三记录交付 batch 内紧邻的同一 pair。initial/target=0 只能在 start 后首 sample 前出现一次；steering/target=n 只能在 sample n-1 完整工具 outputs 闭合后，或匹配 Stop continued 冻结决定的受控交付中、sample n 前出现至多一次。sample 索引从 0 连续递增，输入提交不增加 sample 计数。同一 InputID 不得重复提交；回放必须给 native restore 提供真实 turn、期望角色和关联，不得按 batch 长度猜测 opaque kind。

#### Scenario: Reject illegal input placement
- **WHEN** checksum 正确但输入在未闭合工具组内、terminal 后、重复 target 或 initial 缺失时出现
- **THEN** 回放在 Provider 恢复和副作用前失败，不截断合法 committed bytes

#### Scenario: Preserve an input-only interrupted tail
- **WHEN** journal 在完整 initial 或 steering 输入 batch 后中断
- **THEN** 回放保留输入及关联，并返回本地 lifecycle reconciliation，不请求模型或执行工具

普通 turn grammar 保持以上规则；手动压缩是与 turn 互斥的独立生命周期。compaction_started 必须为单记录 batch，绑定当前来源 revisions/前驱/digest 和 scope。manual 仅在 idle 开始；auto 仅在关联 turn 的首次输入提交前、完整工具 outputs 已提交，或 Stop continued 冻结决定已保存、native 交付缺失且 prepared 候选已 discard、下一 sample 尚未开始时允许，target sample_index 必须精确匹配。同一 sample 最多一次 auto started，未闭合调用、已保存当前 sample 输入或重复自动摘要必须拒绝。started 之后恰好一个同身份、同 scope 的 completed 或 failed 才允许普通事实继续，期间不能插入 turn/input/sample/tool 记录。completed 必须含有界 checkpoint envelope、normalized usage、前后估算并原子提交；failed 必须含 typed 原因和 observed/unavailable 计量状态。compaction record 的 envelope TurnID 始终为空，普通 turn 关联只在 scope 中表达，不冒充普通 native sample/usage。

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

Stop 候选 SHALL 消耗真实 sample index；continued 后的下一 sample 必须使用连续 index，不能用空工具组假装闭合。Stop plan 开放时不得插入新 sample、输入、工具或压缩。完整 Stop 计划/运行/决定按新增 grammar 验证，不复用 tool slot 的 approval/output 配对，也不允许凭 batch 长度推断 native role。

#### Scenario: Reject a premature checked completion
- **WHEN** checks plan 存在未完成项却出现 turn_completed、新 sample 或 auto compaction
- **THEN** 作为语义 corruption 拒绝，不能以半行修复或默认通过放行

Task 的正常语法 SHALL 为 ready -> started -> result，并与其他 exclusive slot 按模型 call index 串行；它无需且禁止普通 approval，也不允许 PreToolUse/PostToolUse/PostToolUseFailure facts。started 必须晚于前序 result 和调用预算关闭；后序调用不得早于 Task result 的确定 durable。ready 可直接收口为取消或预算/验证错误；success/outcome_uncertain 必须具有合法 started，失败不含应用状态。只有完整合法 success result 推进任务投影，状态回退或空列表是显式完整更新，不是 corruption。

任务当前状态 SHALL 从全部完整有序 facts 中最新成功 Task result 推导，保存实际 result_sequence、turn_id、invocation_id 与完整规范更新。无成功更新为 none，最新成功空数组为 clear，最新成功非空为 snapshot；三态及字段为闭合联合。Task ready/started、失败、取消、预算错误、未提交 batch、UI 事件或模型摘要自述都不是应用事实。完整 checksum 下的原生 facade/profile 与 TaskInput 不符、规范 result 与输入 update 不符、重复 source、结果顺序非法、越过屏障或审批/Hook 旁路 MUST 整体拒绝。

#### Scenario: Replay successive task replacements in call order
- **WHEN** 合法 journal 在同一 sample 按 Task A、Task B 保存两个成功结果，B 含状态回退或空数组
- **THEN** replay 接受显式完整替换，当前状态来自 B 的真实 result sequence，A 仍可在历史详情读取

#### Scenario: Reject approval or hook facts for task progress
- **WHEN** journal 为 task.progress 保存普通 approval、mutation Hook plan，或后序调用早于任务 result 完成
- **THEN** 语义校验整体拒绝，不以“没有文件写入”为由放入并行只读批次或跳过屏障

#### Scenario: Retain the previous task state after an uncertain update
- **WHEN** 新 Task started 而 result 缺失，或保存 error/cancelled/outcome_uncertain，之前已有成功列表
- **THEN** replay 当前状态仍是此前列表，新调用只形成原 slot 的恢复计划或失败事实，不安装候选

任务 source SHALL 只使用实际成功 tool_call_result 的 record sequence。live result observer 必须接收成功 Append 返回的真实 result sequence，回放投影必须保留同一 ResultSequence；既有 ready Sequence、detail sequence、native sequence、模型自述或预分配位置均不能替代。记录顺序、同 turn/invocation 和结果成功状态必须同时匹配，列表内容相同不免除来源核验。

#### Scenario: Preserve the actual result sequence across live and replay observers
- **WHEN** 同一任务的 ready、started、result、native output 具有不同 seq，live observer 接收成功 Append 的返回记录，随后恢复同一 journal
- **THEN** 两条路径都以真实 result record seq 形成 source，绝不使用 ready/detail/native seq，内容及来源完全一致

分支回放 SHALL 保留各继承事实的原始来源和顺序，保留 Provider 原生输入、样本与工具配对，并按原位置校验 checkpoint 前驱、来源 revisions 及任务 result 的原始 seq、turn 和 invocation 关联。任务当前状态和有效压缩窗口沿用已经验证的前缀并随本线程后续提交推进。工具结果及历史 ReadArtifact 页按其原线程归属验证，继承条目仍可提供已冻结的历史投影。切换记录归属只改变接受当前引用的线程，必须保留继承段已发现的首个引用错误，即使其后的标记和后续记录均有效。运行中的 artifact 授权及清理引用 MUST 仅来自最终线程段内与其线程归属一致的已提交结果。继承记录中的历史批准、运行和结果事实只用于验证已经结束的调用，不能接纳本线程的新副作用或补偿祖先操作。

#### Scenario: Reject a branch boundary with an open operation
- **WHEN** 有效 checksum 与前缀摘要的标记出现在未完成 turn、尚未交付的工具或 Stop 结果、悬空压缩或未关闭 PostCompact 检查之后
- **THEN** 语义回放拒绝，原文件 bytes 保持不变，不对祖先执行恢复补偿或修复

#### Scenario: Restore inherited context without inheriting artifact authority
- **WHEN** 源线程成功提交工具结果、artifact 引用、读取页、任务更新及匹配 checkpoint 后创建分支，随后分支另有自己的工具结果或压缩
- **THEN** 回放使用原线程身份分别验证已冻结历史、任务来源和连续 checkpoint 链，保持原生顺序与有效上下文
- **THEN** 运行中的 artifact 引用和清理范围只来自分支自己已提交的拥有者事实，继承条目不产生执行或读取授权

#### Scenario: Keep an ancestor artifact error across a valid marker
- **WHEN** 继承段的 ReadArtifact 结果无该段的 matching durable 引用，后接 checksum 和前缀摘要均合法的分支标记及合法子线程结果
- **THEN** 完整及元数据回放都拒绝继承段的错误，分支不能通过发现、恢复或尾部修复检查

#### Scenario: Reject a branch created from metadata alone
- **WHEN** 分支文件的继承前缀含初始元数据或本地标题，标记的父子、seq、digest 与 batch 结构有效，但切点前没有已提交用户输入
- **THEN** 语义回放拒绝该分支，保留原 bytes，不签发可用于修复或接管的已验证结果
