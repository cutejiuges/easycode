## MODIFIED Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含一个 schema canary、一个 payload canary、单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、Provider wire kind、sample/call index、capability，以及与 `Read`、`Glob`、`Grep`、`Patch`、`Write`、`Exec` 或 `ReadArtifact` 匹配的完整 typed input；`tool_approval_decided` SHALL 保存 request/invocation identity、candidate fingerprint、typed outcome、decision source、仅适用于 deny 的有界反馈与不含绝对路径、正文、环境值或命令输出的安全 metadata；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。Exec metadata SHALL包含相对workdir、exit/timeout/cancelled/truncated状态、duration、stdout/stderr计数及各流 typed 保存状态与 descriptor 和必需预算元数据，不得包含PID、绝对路径或环境。工具 records MUST NOT 保存 input revision、result codec revision、执行器额外采集的完整 base/final 文件快照、完整 diff、临时文件路径或可执行 Provider wire item。

schema/payload canary 只用于精确验证当前格式，不得选择历史 decoder。completed sample 的 Provider entry 仍作为 native commit 并紧邻配对 `sample_usage`；tool results 编码为 tool-output native commit 时不生成 sample usage。高频参数 delta、approval request/焦点/按键、命令输出progress、elapsed tick、UI 状态和文件系统瞬时信息 MUST NOT 进入 JSONL。

Write 的必要 typed input SHALL 保存 file_path/content，并与原生调用参数一致；该 content 是调用事实，不是额外采集的磁盘文件快照。Write result SHALL 只保存冻结的有界摘要与 typed metadata，不回显正文。Read observation、目录/file identity、暂存路径与 grant MUST NOT 保存。

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
- **WHEN** 程序读取 checksum 合法但 event kind 或 payload canary 未知的 record
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

Session 当前 canary SHALL 为 7，Provider native 当前 canary 为 3；只允许精确匹配，旧开发 shape 在 repair/append 前失败关闭，不保留兼容 reader。新增 turn_inputs_committed 必须使用强类型 payload，仅含 initial/steering placement、target_sample_index 与有序非重复 InputIDs；不含正文、RequestID 或 UI 状态。正文仅由同 batch native user_inputs 保存，输入 batch 不产生 sample_usage。

#### Scenario: Persist input identity without duplicating text
- **WHEN** 一组初始或中途用户输入提交
- **THEN** journal 原子记录输入关联及 native 正文，纯接收/排队/claimed 状态不进入 Session

每个 tool_call_result SHALL 保存必需 typed 预算元数据：分配上限、最终 preview UTF-8 bytes 与封闭处理原因；validator 核对实际 bytes、能力上限与保存归属。搜索 metadata SHALL 只保存有界模型所见 matches、统计/覆盖原因与 search_result 保存状态/引用，被预览省略的正文不得另藏于 metadata。旧 canary 5、缺失预算字段或旧 reservation shape MUST 失败关闭，不提供双 reader 或 resume 原地迁移。展示条目/分组/正文副本 MUST NOT 成为新 Session records；headless 外部 JSONL v1 保持独立且不变。

#### Scenario: Reject inconsistent result budget
- **WHEN** checksum 合法但记录实际 preview bytes 超过声明 allowance 或缺失预算字段
- **THEN** strict decoder/replay 在恢复副作用前拒绝，不按新策略补齐字段或裁剪正文

当前词汇 SHALL 增加 compaction_started、compaction_completed、compaction_failed；它们使用同一 envelope 的 session/thread 身份、空 TurnID 与 payload 内独立 CompactionID。每个新 kind MUST 有专属构造/strict decoder/validator，不添加 optional record、任意 payload 或展示状态。普通 turn 身份规则不放宽。当前 schema/payload canary 均为 7，旧 6 和更早 shape 在 repair/append 前拒绝。

#### Scenario: Encode a scoped compaction fact
- **WHEN** 压缩事实写入 journal
- **THEN** payload 含合法 CompactionID，TurnID 为空，不复制 RequestID、排队输入、spinner 或摘要 delta

#### Scenario: Reject superseded current fixtures
- **WHEN** journal 使用 Session canary 6 或 native canary 2
- **THEN** 在 repair/append/网络前失败关闭，不保留旧 reader，不迁移既有 bytes

### Requirement: Replay validation enforces record state transitions

Loader 完成字节、envelope 和 batch 校验后，ReplayPlanner SHALL 在创建 Provider Conversation 前对全部当前 typed records 执行语义回放。首个 batch MUST 恰好建立唯一 `session_meta` 与 root `thread_meta`；同一 root thread 最多有一个活动 turn。

文本 turn SHALL 在 turn_started 后先完成 initial 输入 batch，才可通过 [provider_native_commit(sample), sample_usage, turn_completed] 完成；单个 turn_failed 可在输入提交前或后收口，保留此前全部已提交事实。Tool Loop 中，一个含 calls 的 sample batch MUST 严格为 `[provider_native_commit, sample_usage, tool_call_ready...]`，ready records 的 sample/call index 连续且 identity 唯一。ReplayPlanner SHALL 按 capability 验证每个 call slot：自动允许的读取正常为 `ready -> execution_started -> result`；Patch 可为 `ready -> approval_decided(allow*) -> execution_started -> result`、`ready -> [approval_decided(deny), result(denied)]`，或在候选准备失败/取消时从 ready 直接进入匹配的 error/cancelled result。`abort` SHALL 使当前及尚未完成 slots 按取消契约收口。approval identity、candidate fingerprint、outcome 和 decision source MUST 与同一 invocation 一致，且每个 Patch 至多一个 approval fact。

调度 records SHALL 符合连续 `parallel_read` batch 与 `exclusive` Patch 屏障：一个 read batch 的 starts 按 call index先于该 batch 的 results，Patch 的 started/result 位于前后 batch 之间；全部 result records仍按原 call index单调提交。全部 results 存在后，恰好一个按 call index与原 wire kind 配对的 tool-output `provider_native_commit` 关闭该 call group，之后才可出现可选 steering 输入 batch、下一 sample 或 turn failure。

ReplayPlanner MUST 拒绝孤立/重复 approval、result 或 started、allow 后缺失 candidate fingerprint、deny 后出现 started、started 早于必需 allow、started/result index 非法、越过 exclusive 屏障、ready 后直接开始下一 sample、outputs 数量/wire kind/call identity 不匹配、sample usage 缺失/重复、tool-output commit 携带 usage、terminal 后 records 或新 turn 覆盖活动 turn。只有 journal 末尾已承诺的活动状态可作为显式 reconciliation 计划返回；它不是可由 Loader 截断的文件损坏。

本条中 Patch 的 approval/started/result、exclusive 屏障和恢复规则 SHALL 同样适用于 Write。Write 准备失败可从 ready 进入 error/conflict/cancelled，但 success 或 outcome_uncertain 必须具有合法 started；write_no_changes 为 error。Write input/result 联合与 operation 的已知/未知状态必须严格验证，恢复不通过文件系统补齐。

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

普通 turn grammar 保持以上规则；压缩是与 turn 互斥的独立生命周期。compaction_started 必须为 idle 时单记录 batch，绑定当前来源 revisions/前驱/digest；其后恰好一个同身份 completed 或 failed 才能开始下一操作。completed 必须含有界 checkpoint envelope、normalized usage、前后估算并原子提交；failed 必须含 typed 原因和 observed/unavailable 计量状态。普通 terminal 后合法开始新的压缩不是“terminal 后同 turn 记录”；压缩不能插入活动 turn、不能带 TurnID，也不能冒充普通 native sample/usage。

#### Scenario: Replay a compacted thread
- **WHEN** 完整 turn 之后记录 started/completed，再记录新 turn
- **THEN** replay 返回有序普通 commits、checkpoint 和独立操作事实，原 native ordinal 不变

#### Scenario: Reject overlapping operations
- **WHEN** 活动 turn 中出现 compaction_started，或活动压缩中出现新 turn/第二个 started
- **THEN** 语义回放失败，journal 不进入可继续状态

#### Scenario: Reject invalid compaction terminals
- **WHEN** completion 无 started、身份错配、重复 terminal、缺失 checkpoint/usage 或 failed 含可安装窗口
- **THEN** strict decoder/replay 拒绝，不通过默认值修复

### Requirement: Loading validates the complete journal before replay

Session loader SHALL 按行和 batch 顺序校验记录大小、JSON、完整性校验值、当前 schema/payload canary、seq 连续性、标识一致性和 batch 闭合性。当前 envelope MUST 拒绝重复顶层 member、未知顶层 member、尾随 JSON value、非法 UTF-8、非 canonical ID/时间字段和任一未知 event kind。checksum SHALL 覆盖移除 checksum 字段后的强类型 canonical envelope bytes；Provider payload 内部语义仍由对应 Provider 当前 codec 校验。

任何旧、较新或未知 schema/payload canary、未知 kind、文件中间损坏、seq 跳变、标识漂移或重复 metadata MUST 产生明确安全错误。loader MUST NOT 返回部分可恢复历史、fallback 到旧 decoder、保留 optional opaque record、执行 migration 或用默认值补全。

#### Scenario: Load a valid current journal
- **WHEN** thread 文件包含当前 canary、受支持 kind 且 seq 连续的完整 batches
- **THEN** loader 按 seq 返回所有强类型 records，且不会重排或改写 payload

#### Scenario: Load a valid journal
- **WHEN** thread 文件包含当前 schema 的连续完整 batches
- **THEN** loader 返回全部强类型 records 和下一 seq，不修改 journal bytes

#### Scenario: Reject corruption in the middle
- **WHEN** 一个非尾部 record JSON 损坏、超出大小上限、seq 不连续或标识与文件元数据不一致
- **THEN** loader 返回稳定英文 session corruption 错误并且不返回部分可恢复历史

#### Scenario: Reject any unsupported record shape
- **WHEN** journal 包含未知 kind、未知 canary 或被替换的开发期 payload shape
- **THEN** resume 在 repair、append、executor 或 Provider 副作用前失败
- **THEN** 原始 journal bytes 保持不变且未知 payload 不进入日志或错误文本

#### Scenario: Reject an unsupported required record
- **WHEN** journal 包含当前程序不支持的 event kind 或 payload canary
- **THEN** resume 在任何 repair、append 或外部副作用前失败，原始 bytes 保持不变

#### Scenario: Reject ambiguous JSON envelope fields
- **WHEN** 一行包含重复 `seq`、未知当前顶层字段或一个合法对象后的尾随 JSON value
- **THEN** loader 将该行视为损坏并且不得依赖 JSON decoder 的覆盖或宽松默认行为

Load SHALL 只读扫描并返回完整 committed 前缀和私有待修复凭据，不执行 Truncate/Sync。恢复协调者 MUST 在全部 replay、Provider 兼容和原生/checkpoint 校验通过后才执行显式 Repair。Catalog SHALL 只读扫描，不隐式修复。

#### Scenario: Reject semantic corruption with a torn tail
- **WHEN** committed 前缀语义非法或 Provider/checkpoint 校验失败，且末尾另有半行或未完成 batch
- **THEN** 恢复拒绝，原 journal 全部 bytes 不变，不启动 writer 或补偿

### Requirement: Tail repair never rewrites committed history

显式 Repair SHALL 在完整语义及 Provider 校验后只修复文件末尾的半行或未完成 batch：截断到最后一个完整 committed batch 边界，保留此前所有合法 bytes，并以结构化结果报告发生了修复。已经完整提交的 batch、文件中间的损坏和语义非法记录 MUST NOT 被自动删除、重排或重写。

修复后 writer SHALL 从最后一个保留记录的下一 seq 继续；反复加载同一已修复文件 MUST 是幂等的。

#### Scenario: Repair a truncated final line

- **WHEN** 进程中断导致 JSONL 最后一行只写入部分 bytes
- **THEN** 只读 Load 返回待修复凭据，验证通过后 Repair 截断该半行并保留之前的全部 committed batches
- **THEN** 修复结果明确报告被截断的尾部而不回显其 payload

#### Scenario: Repair a trailing incomplete batch

- **WHEN** 尾部含有一个或多个完整 JSON 行但逻辑 batch 缺少 commit 边界
- **THEN** 只读 Load 排除未完成 batch，验证通过后 Repair 将整个未完成 batch 从可恢复事实中移除并从其起始 offset 截断
- **THEN** 先前 committed batch 的原始 bytes 保持不变

#### Scenario: Reopen after repair

- **WHEN** 已修复的 thread 再次打开并追加记录
- **THEN** 新记录从保留历史的下一 seq 开始，第二次加载不再报告同一尾部修复

#### Scenario: Do not truncate a committed interrupted turn

- **WHEN** 最后一条 `turn_started` 所在 batch 已完整写入并通过 checksum，但进程在 terminal 前退出
- **THEN** loader 保留该 batch，由 replay validator 报告 interrupted-tail 状态

修复凭据 MUST 绑定原始 lease、handle、文件大小和内容 digest，拒绝伪造、跨 lease、扫描后内容变化及重复消费。修复接收前取消 MUST 保持 bytes 不变；接收 Truncate 后 MUST 等待 Sync 确定结果，不因取消提前返回。Truncate/Sync 失败 MUST poison lease 并禁止 writer transfer，唯一 owner 保持到关闭。并发 Close 必须等待修复完成，不在锁内执行 I/O 或等待。无损 journal 不需要截断，但不得用零值凭据伪造已修复报告。

#### Scenario: Reject stale repair authority
- **WHEN** 修复凭据来自其他 lease、已消费或扫描后文件内容改变
- **THEN** Repair 拒绝且不截断、不启动 writer

#### Scenario: Cancel a repair before admission
- **WHEN** 截断接收前 context 已取消
- **THEN** 返回取消且原始 bytes 不变

#### Scenario: Finish an admitted repair
- **WHEN** Truncate 已接收后取消与 Sync 竞争
- **THEN** 等待 Sync 确定结果；失败 poison，Close 等待最终 I/O 完成且 writer 不得接管

## ADDED Requirements

### Requirement: Compaction facts preserve durable window installation

compaction_started SHALL 在摘要网络请求前成功 Sync；completion SHALL 以单记录原子 batch 保存安装所需来源、Provider checkpoint、normalized usage 与估算，成功 Sync 前不得切换内存窗口。failed SHALL 保留安全原因和适用计量 provenance，不安装窗口；原始历史 append-only，不将全文复制进 checkpoint。writer admission、lease、poison、编码大小和尾部修复继续适用。完整 checksum 合法但语义非法的 checkpoint MUST NOT 当成可删除尾部。

#### Scenario: Recover a partial completion batch
- **WHEN** 文件以不完整 completion batch 结束
- **THEN** 仅按现有尾部规则修复，started 保留为未完成操作，不安装半个 checkpoint

#### Scenario: Preserve a source journal prefix
- **WHEN** 压缩成功或普通失败后继续会话
- **THEN** 原 journal prefix 逐字节不变，新增 seq 单调，成功窗口事实只来自已完成 batch

#### Scenario: Retain lease during cleanup
- **WHEN** 压缩清理阻塞且另一个进程尝试 resume/repair
- **THEN** 竞争进程 session_busy 且 journal bytes 不变，lease 直到最终 Sync/Close 才释放
