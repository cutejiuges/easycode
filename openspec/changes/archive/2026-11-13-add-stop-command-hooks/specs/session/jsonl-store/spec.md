## MODIFIED Requirements

### Requirement: Session records use one strict unversioned envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、Provider wire kind、sample/call index、capability，以及与 `Read`、`Glob`、`Grep`、`Patch`、`Write`、`Exec` 、`ReadArtifact`、`Skill` 或 `Question` 匹配的完整 typed input；`tool_approval_decided` SHALL 保存 request/invocation identity、candidate fingerprint、typed outcome、decision source、仅适用于 deny 的有界反馈与不含绝对路径、正文、环境值或命令输出的安全 metadata；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。Exec metadata SHALL包含相对workdir、exit/timeout/cancelled/truncated状态、duration、stdout/stderr计数及各流 typed 保存状态与 descriptor 和必需预算元数据，不得包含PID、绝对路径或环境。工具 records MUST NOT 保存 input revision、result codec revision、执行器额外采集的完整 base/final 文件快照、完整 diff、临时文件路径或可执行 Provider wire item。

Session envelope MUST NOT 包含 schema_version、payload_version、canary、format revision 或同义版本标记，不得按版本选择 decoder。completed sample 的 Provider entry 仍作为 native commit 并紧邻配对 `sample_usage`；tool results 编码为 tool-output native commit 时不生成 sample usage。高频参数 delta、approval request/焦点/按键、命令输出progress、elapsed tick、UI 状态和文件系统瞬时信息 MUST NOT 进入 JSONL。

Write 的必要 typed input SHALL 保存 file_path/content，并与原生调用参数一致；该 content 是调用事实，不是额外采集的磁盘文件快照。Write result SHALL 只保存冻结的有界摘要与 typed metadata，不回显正文。Read observation、目录/file identity、暂存路径与 grant MUST NOT 保存。

当前词汇 SHALL 增加 tool_hook_gate_planned、hook_execution_started、hook_execution_finished、tool_hook_gate_decided；四类事实均使用当前 envelope、真实 turn 与封闭 Hook target 身份（工具目标仍必须含真实 invocation/capability）及各自 typed constructor/strict decoder/validator，不能以 opaque payload、optional record 或版本字段代替。计划只保存有序 1..8 个来源种类/revision、Hook id、命令 fingerprint、timeout、能力及 plan digest；started/finished 保存匹配 ordinal 与 HookRunID。finished SHALL 保存封闭 status/code、exit known/value、timeout/cancelled、输出字节数、cleanup confirmed 及 execution/recovery origin；决定保存聚合状态、完成数量和适用时的阻断 ordinal。

新增事实 MUST NOT 保存完整 command、配置路径、stdin、stdout/stderr、未授权自由文本 reason、绝对 cwd、环境实际值、PID、信任响应或 grant。仅 Stop continued 决定可按 extension/stop-hooks 保存已授权、有界且有来源的规范模型反馈，并与后续唯一 native 交付的角色/来源/最终字节交叉验证；工具 gate 统计事实不承载该正文。source revision、fingerprint、plan digest 是内容关联，不是格式版本。无匹配时没有 Hook facts 是合法当前路径；实际存在的新 facts 全部是必须理解的恢复事实。

#### Scenario: Encode the current record vocabulary
- **WHEN** 一个 root thread 完成包含 Read、Glob、Grep、获批 Patch 与获批 Exec 的 Tool Loop
- **THEN** JSONL 复用同一当前 envelope 记录 metadata、turn 边界、sample commits/usages、四类 ledger records 和 tool-output commit
- **THEN** 每个已知 kind 均由唯一当前 typed codec 校验

#### Scenario: Encode the initial record vocabulary
- **WHEN** 一个root thread完成包含搜索、Patch与Exec的当前Tool Loop
- **THEN** 当前envelope记录全部恢复必需facts，且不存在optional或版本分派字段

#### Scenario: Encode a denied patch
- **WHEN** 用户拒绝一个需要 approval 的 Patch
- **THEN** journal 在该 call slot 保存 matching approval decision 与 denied result，且不存在 execution-started record
- **THEN** approval overlay、完整 diff 和按键不会写入 journal

#### Scenario: Encode a denied command
- **WHEN** 用户拒绝一个需要approval的Exec
- **THEN** journal在该call slot保存matching approval decision与denied result，且不存在execution-started record或进程信息

#### Scenario: Reject an unknown record
- **WHEN** 程序读取 checksum 合法但 event kind 未知或 payload 不满足当前字段结构的 record
- **THEN** loader 在 repair、append、executor 和 Provider 副作用前失败
- **THEN** payload 不进入日志、错误或 optional 透传列表

#### Scenario: Preserve an unknown optional record
- **WHEN** journal包含旧开发期`optional` record或当前程序未知的record
- **THEN** loader不再保留或忽略它，而是在任何副作用前拒绝整个journal

#### Scenario: Represent multiple samples in one turn
- **WHEN** Tool Loop 在同一 turn 记录多次 Provider samples
- **THEN** 每个 sample 按 seq 追加配对 native commit 和 sample usage，无需改变当前 envelope

#### Scenario: Represent multiple samples in one future turn
- **WHEN** Tool Loop在同一turn记录多次Provider samples
- **THEN** 每个sample复用唯一当前envelope和codec，并保持seq与usage配对

#### Scenario: Commit a tool result before the next sample
- **WHEN** 当前 tool results 已 durable 且下一 sample 尚未发起
- **THEN** tool-output native commit 单独持久化且不伪造 sample usage
- **THEN** 恢复无需重复文件或进程I/O即可补齐或验证输出

#### Scenario: Commit a future tool result before the next sample
- **WHEN** 当前tool results已durable且下一sample尚未发起
- **THEN** tool-output native commit单独持久化且不伪造sample usage
- **THEN** 恢复无需重复工具I/O即可补齐或验证output

#### Scenario: Keep native tool pairs separate from execution state
- **WHEN** sample native commit 包含原生 function、custom 或 tool-use calls
- **THEN** Provider codec 保留原生内容与 wire kind，而 ready/approval/started/result 状态只由独立 typed ledger records 表达

#### Scenario: Keep native tool pairs separate from approval and execution state
- **WHEN** sample native commit 包含原生 function、custom 或 tool-use calls
- **THEN** Provider codec 保留原生内容与 wire kind，而 ready/approval/started/result 状态只由独立 typed ledger records 表达

#### Scenario: Preserve a future native tool pair without conflating execution state
- **WHEN** sample native commit包含原生tool calls
- **THEN** Provider codec保留原生内容，而当前typed ledger独立表达approval与执行状态

#### Scenario: Persist the requested Write content only
- **WHEN** Write ready 与 result 写入当前 journal
- **THEN** ready 包含完整必要参数，result 只含有界摘要与结果 metadata
- **THEN** 不另写磁盘旧文件、Read observation、approval preview 或暂存资源名

ReadArtifact result SHALL 保存冻结页 preview、artifact ID、实际 offset/next_offset、stored EOF 和 source_complete；不得保存宿主路径。Exec 保存状态 MUST 是必需当前字段，complete/partial 必须匹配合法 descriptor，not_needed/unavailable 不得伪造引用。

#### Scenario: Reject inconsistent artifact metadata
- **WHEN** checksum 合法但 descriptor 的 thread/invocation/stream、digest、长度或状态与结果不一致
- **THEN** 当前 decoder/replay 拒绝记录，不访问 artifact 或修改 journal

Session envelope SHALL 不携带格式版本字段；Provider native/checkpoint 同样不带格式版本字段，按自身封闭结构与原生语义校验。当前字段结构不合法时在 repair/append 前失败关闭，不保留兼容 reader。新增 turn_inputs_committed 必须使用强类型 payload，仅含 initial/steering placement、target_sample_index 与有序非重复 InputIDs；不含正文、RequestID 或 UI 状态。正文仅由同 batch native user_inputs 保存，输入 batch 不产生 sample_usage。

#### Scenario: Persist input identity without duplicating text
- **WHEN** 一组初始或中途用户输入提交
- **THEN** journal 原子记录输入关联及 native 正文，纯接收/排队/claimed 状态不进入 Session

每个 tool_call_result SHALL 保存必需 typed 预算元数据：分配上限、最终 preview UTF-8 bytes 与封闭处理原因；validator 核对实际 bytes、能力上限与保存归属。搜索 metadata SHALL 只保存有界模型所见 matches、统计/覆盖原因与 search_result 保存状态/引用，被预览省略的正文不得另藏于 metadata。缺失预算字段或旧 reservation shape MUST 失败关闭，不提供双 reader 或 resume 原地迁移。展示条目/分组/正文副本 MUST NOT 成为新 Session records；headless 外部 JSONL v1 保持独立且不变。

#### Scenario: Reject inconsistent result budget
- **WHEN** checksum 合法但记录实际 preview bytes 超过声明 allowance 或缺失预算字段
- **THEN** strict decoder/replay 在恢复副作用前拒绝，不按新策略补齐字段或裁剪正文

当前词汇 SHALL 增加 compaction_started、compaction_completed、compaction_failed；它们使用同一 envelope 的 session/thread 身份、空 TurnID 与 payload 内独立 CompactionID 和必需强类型 scope。manual scope 不含普通 turn 关联；auto scope 包含真实 turn_id 和 target sample_index，completed checkpoint 保存同一 scope；scope 缺失、未知 trigger 或关联错配必须拒绝。每个新 kind MUST 有专属构造/strict decoder/validator，不添加 optional record、任意 payload 或展示状态。普通 turn 身份规则不放宽。系统只保留唯一当前无版本字段结构；缺少 scope 等必需字段或含非法附加版本字段时在 repair/append 前拒绝，不提供版本门禁、迁移或兼容 reader。

#### Scenario: Encode a scoped compaction fact
- **WHEN** 压缩事实写入 journal
- **THEN** payload 含合法 CompactionID，TurnID 为空，不复制 RequestID、排队输入、spinner 或摘要 delta

#### Scenario: Reject superseded current fixtures
- **WHEN** journal 使用 非法附加版本字段、缺失必需 scope 字段的记录，或 native/checkpoint 包含非法 payload_version 字段
- **THEN** 在 repair/append/网络前失败关闭，不保留旧 reader，不迁移既有 bytes

提问 SHALL 复用 tool_call_ready、tool_execution_started 与 tool_call_result，不新增问卷 UI/回答 admission record 或 approval_decided。ready 保存完整题目/选项和对应 Provider call；result 保存完整冻结回答 preview 与必要的强类型关联/计数 metadata。模型预填答案、未知字段、非当前 shape、题目/回答错配及超限内容必须拒绝。答案正文不额外复制到 SQLite、diagnostic 或 telemetry；焦点、按键、未提交文字和临时 request identity 不成为恢复事实。

#### Scenario: Persist a question without approval
- **WHEN** 用户提交一个合法问卷
- **THEN** 当前唯一记录结构保存 ready/started/result 和匹配 native outputs，不出现该调用的 approval record 或 UI 草稿

#### Scenario: Encode a complete hook gate
- **WHEN** 合法检查在一个真实 mutation 调用前完成
- **THEN** 四类 facts 使用同一当前 envelope 与独立 typed payload，保留调用身份和检查顺序，不保存脚本输入输出正文

#### Scenario: Reject an unrecognized hook shape
- **WHEN** Hook record 缺失当前必需字段、包含版本标记、未知 kind 或 raw output
- **THEN** strict 校验在 repair/append/副作用前失败，不跳过记录或使用历史 reader

当前词汇 SHALL 增加 turn_stop_hook_planned、turn_stop_hook_decided，复用带 tool_call/turn_stop 目标联合的 Hook started/finished；每个当前 kind 都具有 typed constructor/strict decoder/validator。Stop 计划为 checks/unavailable 封闭联合，包含真实 turn/sample、续轮次数、digest，以及有序声明引用（含 Stop 事件及 none/model 披露策略）或明确来源/匹配错误，不保存命令或伪 invocation。决定包含同 target/plan、processed、disposition/code、origin 与续轮计量；仅 continued 含冻结模型文本及来源/bytes/digest。旧 invocation-only 运行身份直接失败，不增加格式版本、optional record 或兼容 reader。

#### Scenario: Encode a current stop target
- **WHEN** 成功无 calls sample 进入 Stop 检查
- **THEN** 当前 typed facts 使用真实 turn/sample target，无 tool capability、Provider call ID 或假 invocation

#### Scenario: Reject unapproved text in a stop fact
- **WHEN** plan/运行统计或非 continued 决定含脚本原文，或反馈缺少 model 来源/预算
- **THEN** 当前 decoder/replay 在 repair/append 前拒绝，不透传正文

### Requirement: Replay validation enforces record state transitions

Loader 完成字节、envelope 和 batch 校验后，ReplayPlanner SHALL 在创建 Provider Conversation 前对全部当前 typed records 执行语义回放。首个 batch MUST 恰好建立唯一 `session_meta` 与 root `thread_meta`；同一 root thread 最多有一个活动 turn。

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

### Requirement: Sample usage is committed atomically with its sample

每个成功 Provider sample SHALL 在一个 durable batch 中依次写入 `provider_native_commit`、`sample_usage` 以及该 sample 的后续边界。无 tool call 的最终 sample 在无 Stop 且来源合法时后续边界为 turn_completed，有 Stop 或来源不可判定时为 turn_stop_hook_planned；含 tool calls 的 sample 后续边界为一个或多个按 call index 排列的 `tool_call_ready`，输入联合覆盖Read、Glob、Grep、Patch、Write、Exec与ReadArtifact。同一 batch MUST 使用相同 session/thread/turn 和 batch identity并获得连续seq；只有整个batch成功Sync后才能确认任一事实或读取Patch目标。

tool-output native commit MUST 在全部对应 result facts 已按 call index durable 后单独提交且不得带 `sample_usage`。`sample_usage` payload继续完整包含五个normalized metrics，不得包含Provider raw usage、价格、请求正文、tool result、approval或diff。

Write 的完整参数 SHALL 在该原子 batch 内验证并保存。多个 Write 的聚合原生记录或 ready record 超过既有上限时，系统 MUST 在任何 workspace I/O 前失败，不扩大通用 Session 限额。

#### Scenario: Commit a heterogeneous call sample
- **WHEN** completed sample按顺序包含Glob、Patch与Read
- **THEN** journal原子追加native commit、sample usage和三个ready records
- **THEN** candidate preparation与executor只在整个batch Sync后才可发生

#### Scenario: Commit a new sample usage batch
- **WHEN** completed sample 不含 tool calls 且无 Stop
- **THEN** journal原子追加native commit、sample usage和turn completion

#### Scenario: Fail an incomplete usage batch
- **WHEN** 当前尾部sample batch缺少usage、ready或completion中的任一必需record
- **THEN** Loader按既有batch repair规则移除整个未完成batch，不恢复部分history、usage或call

#### Scenario: Fail closed on the superseded development baseline
- **WHEN** Loader读取缺少sample usage或使用旧Provider/Tool payload shape的开发期fixture
- **THEN** resume在repair、append、executor或Provider调用前失败，不保留兼容reader

#### Scenario: Fail an incomplete new usage batch
- **WHEN** 当前尾部sample batch缺少usage、ready或completion中的任一必需record
- **THEN** Loader按batch repair规则移除整个未完成batch，不恢复部分事实

#### Scenario: Commit an heterogeneous call sample
- **WHEN** completed sample按顺序包含Glob、Grep、Read与Patch
- **THEN** journal原子追加native commit、sample usage和四个ready records
- **THEN** 任一candidate preparation或executor只在整个batch Sync后发生

#### Scenario: Commit a call sample
- **WHEN** completed sample按顺序包含两个Read calls
- **THEN** journal原子追加native commit、sample usage和两个ready records
- **THEN** executor只在整个batch Sync后才可接收调用

#### Scenario: Reject usage on a tool-output commit
- **WHEN** journal把tool-output commit与sample usage配对
- **THEN** ReplayPlanner拒绝该状态而不是把output误计为Provider sample

#### Scenario: Reject an oversized Write sample before I/O
- **WHEN** 多个各自合法 Write 的聚合 native commit 超过当前 record 预算
- **THEN** sample 无成功提交，零候选读取与零文件/目录写入

Stop 反馈 native commit SHALL 在 continued 冻结决定后的独立一/三记录交付中保存，不带 sample_usage；检查进程和反馈输入不伪造 Provider 计量。sample 与 plan 成功 Sync/finalize 后即可报告 sample 已保存，但成功 turn usage 只能在 passed/turn_completed terminal batch 确定提交后发布。无 Stop 路径保留已有三记录 batch。

#### Scenario: Commit a candidate without completing its turn
- **WHEN** 成功 sample 无 calls 且需要 Stop
- **THEN** sample/usage/plan 同批保存，无 turn_completed；后续失败仍保留该 sample usage

## ADDED Requirements

### Requirement: Stop facts atomically separate sample commitment feedback and turn completion

Stop 候选 batch SHALL 严格为 native(sample)、sample_usage、turn_stop_hook_planned，身份和连续 sample index 一致。checks plan 有序 1..8 refs；unavailable plan 仅表达 hook_config_invalid/hook_match_limit，不伪造空 checks 或运行。每项 started 单独 Sync，中间成功 finished 单独保存；只有连续成功前缀可启动下一 ordinal。每个计划恰好一次 decided，target/event/plan/run/ordinal 全部匹配。

全部通过 SHALL 将最后 finished、decided(passed) 和 turn_completed 同批；执行后阻断/错误/取消/不确定将 finished、decided 和 turn_failed 同批；预检/信任/预算拒绝仅 decided+turn_failed。正常授权 block 则将 finished 与 decided(continued, frozen feedback) 同批，冻结最终模型字节及本地交付义务，不同时保存 native feedback。续轮及文本额度在决定 Sync 时计入一次，交付或恢复不重复计费。第三次 continued 或 next sample index 超过普通上限必须拒绝；无 sample 余量不阻止当前检查通过或其他真实错误；只有正常 model 阻断尝试续轮时才以 tool_loop_limit_exceeded 关闭，none/exit2 保持 hook_denied。

决定 disposition SHALL 仅为 passed/continued/blocked/error/cancelled/outcome_uncertain；运行统计仍使用 continue/denied/error/cancelled/outcome_uncertain。passed 仅 code=ok 且全部项通过；continued 仅 execution-origin、code=hook_denied、有 sample/续轮额度及合法 model 冻结反馈；blocked 仅 hook_denied/hook_trust_denied/hook_trust_required/stop_hook_limit_exceeded/tool_loop_limit_exceeded；cancelled 仅 cancelled/session_interrupted_before_stop_hook；outcome_uncertain 仅 hook_outcome_uncertain 且有真实未决项。error 仅接受设计表中封闭的配置/匹配/输入/平台/运行/反馈/额度错误 code。processed 等于真实 finished 前缀数量，含终结项；未启动项不计数。真实运行结束后的取消或编码失败可改变最终决定，但不能改写已确认 finished。

决定 SHALL 保存封闭的 plan/hook 来源范围；hook 分支必须给出计划内真实 ordinal，plan 分支不得混入 ordinal。continued 的来源必须恰好是 processed-1 的最后实际阻断项，该 finished 必须 denied/hook_denied、exit_known=true/exit_code=0、cleanup_confirmed=true、非取消/超时，且该项声明为 model；不能借用同计划另一 model 声明或 exit 2 的拒绝伪造授权反馈。trust_denied 可以指向审查被拒项，但 processed=0 且无运行事实；恢复取消为 plan 范围，真实未决恢复为对应 hook 范围。全部通过、来源/共同输入/平台/headless/运行前总预算错误和项目间取消使用 plan 范围；实际运行或反馈错误、终结阻断及实际结果后的取消使用最后 finished 的 hook 范围。其他 source/code/ordinal 组合必须拒绝。

#### Scenario: Save one continued decision atomically
- **WHEN** 已信任 model 检查正常 exit 0 block 且全部额度有效
- **THEN** finished/continued 冻结意图同批；尚无 native 反馈，下一 sample 必须等待后续交付

#### Scenario: Reject a split passing terminal
- **WHEN** 完整 committed 记录有最后通过的 finished，但决定或 turn_completed 被拆到另一 batch
- **THEN** 拒绝语义损坏，不当作可补偿的成功检查

#### Scenario: Reject an over-limit feedback decision
- **WHEN** checksum 正确但第三次 continued、普通 sample 余量耗尽、反馈 bytes/digest 错误或实际来源项不是 model
- **THEN** 整体回放拒绝且 journal bytes 不变

#### Scenario: Recover only the genuinely started item
- **WHEN** 第 0 项通过、第 1 项 started 未 finished、第 2 项未 started
- **THEN** 只把第 1 项标为未决，不为第 2 项生成运行统计

#### Scenario: Cancel after a definite block
- **WHEN** Hook 已确定 denied，但 continued 决定接收前用户取消
- **THEN** finished 保持真实 denied，决定为 cancelled 且无交付义务，不伪造 cancelled 运行或第二个决定

#### Scenario: Reject feedback borrowed from another declaration
- **WHEN** 真实阻断项 feedback=none，但决定引用同计划另一个 model 项
- **THEN** 即使正文/校验和合法也拒绝，不能把另一声明的披露授权转移过来

#### Scenario: Reject an exit-two continuation
- **WHEN** exit 2 已确定 denied，却伪造为 continued 并携带 model 文本
- **THEN** 严格拒绝；exit 2 仍只能是无原文的终止拒绝

### Requirement: Pending stop feedback has one explicit native delivery

continued 决定后 SHALL 进入 feedback_pending，允许对已丢弃 prepared 候选的旧 committed 窗口执行至多一次自动摘要；plan 未闭合、候选未 discard 或交付后至目标 sample 首次请求前不得摘要。该状态只接受 [native(hook_feedback)] 或 [native(hook_feedback), turn_inputs_committed(steering), native(user_inputs)] 两种交付 batch，不携带 sample_usage。后者反馈在前、真实纠正在后，用户 pair 不能拆批或单独先保存。无实际 claim/合法输入关联不得生成 user_inputs。

交付 SHALL 绑定唯一 prior continued 决定及其最终冻结字节/来源/digest，所有 native roles 和真实 InputIDs/附件通过 Provider codec 交叉校验；Session 依赖已验证 pending 状态路由，不用 batch 长度猜 opaque role。交付 Sync/finalize 后清除待交付义务、推进实际 native entries 的 revisions，但不重复决定/反馈计费。重复交付、漏反馈而开始下一 sample/terminal、错误 target/role/顺序、正文重新渲染或跨目标摘要均为 corruption；合法 pending 尾部可本地补偿。

continued 接收后即使取消或请求预检失败，健康 journal 仍须以冻结字节完成反馈单独本地交付再关闭旧 turn；不能因该义务强制保存未接收的用户输入。复合交付 admission 前取消返还 claim；admission 后 Sync 成功则真实输入 committed 不返还；durability 不确定 poison，不再写替代 batch 或发请求。用户输入本地拒绝可重建反馈单独候选，不重做摘要。内存 finalize/宿主投影失败不能伪造历史回滚或再次交付。

#### Scenario: Deliver a correction with feedback
- **WHEN** continued 后实际领取合法 auto 输入
- **THEN** 三记录交付同批保存两种来源，反馈不伪造 InputID、真实输入保留原 ID/附件，Sync 后再采样

#### Scenario: Reconcile a pending feedback obligation
- **WHEN** 冻结决定完整保存但 native 交付不存在
- **THEN** 这是合法待交付尾部，恢复可纯内存生成一条反馈再关闭，不重跑或丢弃已接收义务

#### Scenario: Reject a torn compound meaning
- **WHEN** 完整 committed 记录含单独真实 steering 而遗漏本应同批的反馈，或重复反馈/错角色/错顺序
- **THEN** 回放失败，即使另有半行也不先修复；不能以输入已保存推断交付完整

#### Scenario: Avoid re-compacting fresh delivery
- **WHEN** 一/三记录交付已保存而目标 sample 尚未开始，随后又有 auto compaction_started
- **THEN** 拒绝该状态，最新反馈/纠正不能在模型首次消费前被摘要替换
