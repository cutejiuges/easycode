## MODIFIED Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含一个 schema canary、一个 payload canary、单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、Provider wire kind、sample/call index、capability，以及与 `Read`、`Glob`、`Grep`、`Patch`、`Write`、`Exec` 或 `ReadArtifact` 匹配的完整 typed input；`tool_approval_decided` SHALL 保存 request/invocation identity、candidate fingerprint、typed outcome、decision source、仅适用于 deny 的有界反馈与不含绝对路径、正文、环境值或命令输出的安全 metadata；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。Exec metadata SHALL只包含相对workdir、exit/timeout/cancelled/truncated状态、duration、stdout/stderr计数及各流 typed 保存状态与 descriptor，不得包含PID、绝对路径或环境。工具 records MUST NOT 保存 input revision、result codec revision、执行器额外采集的完整 base/final 文件快照、完整 diff、临时文件路径或可执行 Provider wire item。

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

Session 当前 canary SHALL 为 5，Provider native 当前 canary 为 2；只允许精确匹配，旧开发 shape 在 repair/append 前失败关闭，不保留兼容 reader。新增 turn_inputs_committed 必须使用强类型 payload，仅含 initial/steering placement、target_sample_index 与有序非重复 InputIDs；不含正文、RequestID 或 UI 状态。正文仅由同 batch native user_inputs 保存，输入 batch 不产生 sample_usage。

#### Scenario: Persist input identity without duplicating text
- **WHEN** 一组初始或中途用户输入提交
- **THEN** journal 原子记录输入关联及 native 正文，纯接收/排队/claimed 状态不进入 Session

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
