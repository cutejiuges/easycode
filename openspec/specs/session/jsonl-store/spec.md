# session/jsonl-store Specification

## Purpose

定义 append-only JSONL Session 事实源，使已交付的文本对话、Tool、Hook、Compaction 与未来 MCP、Subagent 记录共享稳定日志机制，而不把 SQLite 或 UI 事件变成恢复依据。

## Requirements

### Requirement: Session records use one strict unversioned envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、Provider wire kind、sample/call index、capability，以及与 `Read`、`Glob`、`Grep`、`Patch`、`Write`、`Exec` 、`ReadArtifact`、`Skill` 或 `Question` 匹配的完整 typed input；`tool_approval_decided` SHALL 保存 request/invocation identity、candidate fingerprint、typed outcome、decision source、仅适用于 deny 的有界反馈与不含绝对路径、正文、环境值或命令输出的安全 metadata；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。Exec metadata SHALL包含相对workdir、exit/timeout/cancelled/truncated状态、duration、stdout/stderr计数及各流 typed 保存状态与 descriptor 和必需预算元数据，不得包含PID、绝对路径或环境。工具 records MUST NOT 保存 input revision、result codec revision、执行器额外采集的完整 base/final 文件快照、完整 diff、临时文件路径或可执行 Provider wire item。

Session envelope MUST NOT 包含 schema_version、payload_version、canary、format revision 或同义版本标记，不得按版本选择 decoder。completed sample 的 Provider entry 仍作为 native commit 并紧邻配对 `sample_usage`；tool results 编码为 tool-output native commit 时不生成 sample usage。高频参数 delta、approval request/焦点/按键、命令输出progress、elapsed tick、UI 状态和文件系统瞬时信息 MUST NOT 进入 JSONL。

Write 的必要 typed input SHALL 保存 file_path/content，并与原生调用参数一致；该 content 是调用事实，不是额外采集的磁盘文件快照。Write result SHALL 只保存冻结的有界摘要与 typed metadata，不回显正文。Read observation、目录/file identity、暂存路径与 grant MUST NOT 保存。

当前词汇 SHALL 增加 tool_hook_gate_planned、hook_execution_started、hook_execution_finished、tool_hook_gate_decided；四类事实均使用当前 envelope、真实 turn/invocation 身份及各自 typed constructor/strict decoder/validator，不能以 opaque payload、optional record 或版本字段代替。计划只保存有序 1..8 个来源种类/revision、Hook id、命令 fingerprint、timeout、能力及 plan digest；started/finished 保存匹配 ordinal 与 HookRunID。finished SHALL 保存封闭 status/code、exit known/value、timeout/cancelled、输出字节数、cleanup confirmed 及 execution/recovery origin；决定保存聚合状态、完成数量和适用时的阻断 ordinal。

新增事实 MUST NOT 保存完整 command、配置路径、stdin、stdout/stderr、自由文本 reason、绝对 cwd、环境实际值、PID、信任响应或 grant。source revision、fingerprint、plan digest 是内容关联，不是格式版本。无匹配时没有 Hook facts 是合法当前路径；实际存在的新 facts 全部是必须理解的恢复事实。

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

### Requirement: A single writer assigns order and durably appends batches

每个活动 thread SHALL 在跨 goroutine、跨 Repository 实例和跨进程范围内只有一个 Session writer。进程在创建首个 metadata batch，或对既有 journal 执行可能 repair、续写的 load 前，MUST 先取得绑定到该 journal handle 的 exclusive lease；同一 lease MUST 连续保持到 writer 完成最终 Sync、关闭文件并停止接受 append。系统不得在 load 后释放所有权再重新打开文件续写。

当其他进程已经持有 lease 时，竞争者 MUST 在读取、截断、Provider 恢复或其他可见副作用前以稳定英文 `session_busy` 错误快速失败，且 journal bytes MUST 保持不变。lease MUST 随持有 handle 关闭或进程终止自动释放；系统不得依赖 PID 文件、超时删除或进程级全局 mutex 猜测所有权。

writer MUST 分配连续递增的 `seq` 与记录时间，调用方不得自行选择或回退序号。一个逻辑 batch 中的记录 MUST 按调用顺序编码，并 SHALL 以全有或全无的恢复语义追加；系统只有在该 batch 已写入并对文件执行成功 Sync 后才能向上层确认 durable success。

append admission MUST 有明确线性化点：在请求被接受前发生的 context 取消或有界队列背压 MUST 在分配 seq、写盘或产生其他副作用前拒绝请求；请求一旦被接受，writer MUST 返回确定的 durable success 或失败结果，不得因调用方随后取消而留下无人确认的后台提交。Close MUST 拒绝新的 admission、完成所有已接受请求，然后执行最终 Sync、关闭 handle 并释放 lease。状态检查与 admission 不得在锁内执行阻塞 channel 操作、磁盘 I/O 或等待其他 goroutine。

写入或 Sync 结果不确定时，writer MUST 进入不可继续写入的失败状态；同一进程不得在该 writer 上继续分配序号或启动新的 Provider 副作用。

#### Scenario: Acquire one writer across processes

- **WHEN** 两个进程同时尝试恢复并续写同一个 thread journal
- **THEN** 恰好一个进程取得 exclusive lease 并成为活动 writer
- **THEN** 另一个进程以 `session_busy` 失败，且不得读取后 repair、截断或追加该 journal

#### Scenario: Release ownership after process termination

- **WHEN** 持有 thread lease 的进程在未执行应用级 Close 的情况下终止
- **THEN** 操作系统释放该 lease，后续进程可以重新取得所有权并从完整校验后的下一 seq 继续
- **THEN** 系统不通过删除 stale PID 文件或等待租约 TTL 恢复所有权

#### Scenario: Append a successful batch

- **WHEN** writer 追加包含 Provider 原生提交和 turn 完成边界的 batch
- **THEN** 记录获得连续 seq、保持给定顺序并在 durable success 返回前完成文件 Sync

#### Scenario: Serialize concurrent append attempts

- **WHEN** 同一 writer 同时收到多个 append 请求
- **THEN** writer 串行处理已接受请求且最终文件中的 seq 严格递增，不出现重复、倒序或交错 payload

#### Scenario: Reject before append admission

- **WHEN** append context 在 admission 前已经取消，或有界 admission 队列没有容量
- **THEN** writer 在分配 seq 或写入 bytes 前拒绝请求
- **THEN** 既有 journal、后续 seq 和 writer 的可继续状态不受该拒绝影响

#### Scenario: Close races with append admission

- **WHEN** Close 与多个 append 请求并发发生
- **THEN** 每个请求都被明确归类为 Close 前已接受并获得确定结果，或 Close 后未接受且零副作用
- **THEN** Close 只在已接受请求处理完成、最终 Sync 和 handle 关闭后返回

#### Scenario: Stop after an ambiguous write failure

- **WHEN** append 或 Sync 返回无法确认 durable 状态的错误
- **THEN** writer 返回安全英文 session 错误并拒绝后续写入
- **THEN** 上层不会继续当前 conversation 的模型调用或其他副作用

### Requirement: Loading validates the complete journal before replay

Session loader SHALL 按行和 batch 顺序校验记录大小、JSON、完整性校验值、seq 连续性、标识一致性和 batch 闭合性。当前 envelope MUST 拒绝重复顶层 member、未知顶层 member、尾随 JSON value、非法 UTF-8、非 canonical ID/时间字段和任一未知 event kind。checksum SHALL 覆盖移除 checksum 字段后的强类型 canonical envelope bytes；Provider payload 内部语义仍由对应 Provider 当前 codec 校验。

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

Load SHALL 只读扫描并返回完整 committed 前缀和私有待修复凭据，不执行 Truncate/Sync。恢复协调者 MUST 在全部 replay、Provider 兼容和原生/checkpoint 校验通过后才执行显式 Repair。Catalog SHALL 只读扫描，不隐式修复。

#### Scenario: Reject semantic corruption with a torn tail
- **WHEN** committed 前缀语义非法或 Provider/checkpoint 校验失败，且末尾另有半行或未完成 batch
- **THEN** 恢复拒绝，原 journal 全部 bytes 不变，不启动 writer 或补偿

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

### Requirement: Session storage is private and path-safe

Session 根目录和日期目录在支持权限位的平台上 MUST 使用仅当前用户可访问的权限，thread JSONL 文件 MUST 使用用户私有权限创建且不得因已存在的宽松权限而静默继续。thread ID 与派生路径 MUST 经过严格格式校验和 containment 检查；路径穿越、symlink 逃逸和非普通文件 MUST 在读写前拒绝。

Session records、错误、诊断和 fixture MUST NOT 包含 API key、Authorization、Cookie、敏感 Header、base URL 或配置文件路径。用户输入和 Provider 原生历史只允许存在于权限受控的 Session 事实源，不得复制到诊断日志。

#### Scenario: Create a private thread journal

- **WHEN** 系统在默认 Session 根创建新的日期目录和 thread 文件
- **THEN** Unix 类平台上的目录权限为 `0700` 且文件权限为 `0600`

#### Scenario: Reject an unsafe thread path

- **WHEN** resume 标识可解析为目录穿越、symlink 逃逸或非普通文件目标
- **THEN** 系统在打开文件前返回安全英文 session 错误

#### Scenario: Keep credentials out of the journal

- **WHEN** 使用包含 API key、base URL 和敏感 Header 的配置创建并运行 Session
- **THEN** JSONL、错误、fixture 和诊断中均不包含这些值

### Requirement: Record vocabulary evolves without logging UI deltas

JSONL 事实源 SHALL 只持久化恢复、幂等和审计所需的业务边界，不得默认记录 assistant text delta、spinner、窗口尺寸、工具 progress 或其他 UI 瞬态。发布前新增 Tool、MCP、permission、usage、hook、compact 或 subagent 记录时，变更 SHALL 更新唯一当前强类型 event kind 集合并直接替换当前 fixture；不得预留未知 optional record、历史 payload decoder 或 migration fixture。

大工具结果超过行大小边界时，命令输出 SHALL 使用权限受控且具备完整性信息的 artifact，并在 JSONL 中保存有界 typed 引用，而不是放宽所有记录的上限。搜索结果仍只保存 64 KiB 内的冻结模型 preview 和有界 typed metadata，不引入通用 artifact。

#### Scenario: Stream many text deltas
- **WHEN** Provider 在一个 sample 中产生大量 assistant 文本增量后成功完成
- **THEN** Session 保存最终 Provider 原生提交和生命周期边界，而不是为每个文本 delta 追加一条事实记录

#### Scenario: Extend the pre-release vocabulary
- **WHEN** 后续已确认变更引入新的 required fact
- **THEN** 新 kind 复用 envelope、seq、batch、Sync、修复和权限契约，并成为唯一当前 typed vocabulary 的一部分
- **THEN** 仓库不同时保留变更前后的 decoder 或 fixture 树

#### Scenario: Add a future tool result record
- **WHEN** 发布前后续 change 引入新的工具事实
- **THEN** 该事实进入唯一当前 typed vocabulary，并复用 envelope、seq、batch、Sync 与修复契约
- **THEN** 被替换的开发期 reader 和 fixture 不再保留

#### Scenario: Ignore transient tool progress
- **WHEN** 搜索工具在一次执行中访问大量条目
- **THEN** Session 不逐条持久化扫描 progress，只保存最终有界 result fact

#### Scenario: Ignore transient MCP progress
- **WHEN** MCP 工具在一次执行中产生大量仅用于界面的 progress
- **THEN** Session 不逐条持久化这些瞬态更新，只记录未来已批准的最终 typed fact

#### Scenario: Keep output bodies outside records
- **WHEN** Exec 产生大输出并发布 artifact
- **THEN** journal 仅保存有界 preview/descriptor 与原有业务事实，不记录管道 chunk、页读取 UI 状态或整个文件

### Requirement: Current record schema exposes typed construction and decoding

每个当前已知 Session `event_kind` SHALL 具有专属强类型 draft constructor、strict decoder 和 validator。公共 record draft、writer、replay 和 lifecycle 接口 MUST NOT 接受或返回无约束动态值；构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON、非法附加版本字段和不满足当前语义的 payload。业务类型和构造器不得用版本后缀复制当前 schema，decoder registry 不得以 payload version 选择实现。

所有当前 record codecs SHALL 深拷贝 preview/input/feedback 等可变 bytes，并验证 identity、wire kind、index、candidate fingerprint、decision、status 与大小边界。本变更 SHALL 一次性重写仓库开发期 fixture，以一套固定当前 fixture 覆盖 text turn、异构并行读取、获批/拒绝 Patch、获批/拒绝/中断Exec、exclusive 屏障、各崩溃补偿与损坏拒绝；实现不得保留旧 Provider payload reader、旧 Tool revision reader、optional record 分支、旧 fixture 兼容或混合 revision 恢复路径。

当前固定 fixture SHALL 覆盖 Write 创建/覆盖成功、准备失败、拒绝、ready/allow/started/result 中断与混合 exclusive 顺序。新增 Write 使用唯一当前 input/result 联合并严格验证当前字段结构；不得给旧 shape 增加 optional 分支。

#### Scenario: Construct every tool record through a typed API
- **WHEN** 调用方创建 Read、Glob、Grep、Patch、Write、Exec、ReadArtifact 的 ready、approval、execution-started 或 result draft
- **THEN** constructor只接收与kind和capability匹配的当前强类型值并立即验证
- **THEN** draft不能被改造成capability、wire kind与payload不匹配的record

#### Scenario: Construct every current record through a typed API
- **WHEN** 调用方创建任一当前record
- **THEN** 对应constructor只接收该kind的强类型字段并立即验证当前字段结构与语义

#### Scenario: Strictly decode an approval decision
- **WHEN** checksum合法的approval payload包含未知字段、非法outcome、超限反馈或与allow决定并存的反馈
- **THEN** 唯一当前decoder在replay前拒绝记录且错误不回显反馈正文

#### Scenario: Strictly decode a tool result
- **WHEN** checksum合法的result payload包含未知字段、非法status、超限preview或尾随JSON
- **THEN** 唯一当前decoder在replay前拒绝记录且错误不回显preview正文

#### Scenario: Strictly decode a known payload shape
- **WHEN** 任一checksum合法的当前payload包含未知字段、尾随JSON或非法语义
- **THEN** 唯一当前decoder在replay前拒绝且错误不包含payload正文

#### Scenario: Strictly decode an Exec result
- **WHEN** checksum合法的Exec result包含PID、绝对workdir、环境值、超限preview、未知字段或非法状态组合
- **THEN** 唯一当前decoder在replay前拒绝记录且错误不回显payload正文

#### Scenario: Replay the current immutable tool fixture
- **WHEN** 当前实现加载仓库内固定的含Exec异构Tool Loop fixture
- **THEN** 生产Loader、codec和ReplayPlanner产生预期approval、ledger、顺序、wire kind与native commit摘要
- **THEN** fixture不是测试运行时由当前encoder生成

#### Scenario: Replay the current immutable patch fixture
- **WHEN** 当前实现加载仓库内固定的获批Patch与中断补偿fixture
- **THEN** 生产Loader、codec和ReplayPlanner产生预期ledger、顺序、wire kind与native commit摘要
- **THEN** fixture不是测试运行时由当前encoder生成

#### Scenario: Reject a superseded development fixture
- **WHEN** Loader读取已被本变更替换的envelope、Provider payload或Tool ledger shape
- **THEN** 恢复在repair、append、executor或Provider网络调用前失败，不进入兼容分支

#### Scenario: Reject a mismatched Write result
- **WHEN** checksum 合法但 Write result 带 Read/Patch metadata、错误 wire kind 或缺失当前必需字段
- **THEN** strict decoder/replay 拒绝，不进入兼容路径

#### Scenario: Load fixed Write bytes
- **WHEN** 测试读取仓库保存的 Write current fixtures
- **THEN** 使用生产 loader/replay 验证预期调用、审批与结果；测试运行时不调用当前 encoder 生成 fixture

本变更 SHALL 直接更新唯一当前无版本结构并逐字节固定 Exec 保存状态与 ReadArtifact 的 current fixtures；缺少保存字段的旧开发 shape MUST 在 repair/append 前失败，不保留兼容 reader。

#### Scenario: Load fixed artifact fixtures
- **WHEN** 加载仓库内基础七工具、partial/unavailable 和恢复中断的固定 bytes
- **THEN** 生产 decoder/replay 校验通过；测试运行时不使用当前 encoder 生成输入 fixture

### Requirement: Repository lifecycle and path safety are explicit

Session Repository 的纯内存配置与外部资源获取 SHALL 分离。任何创建数据根、打开目录句柄、创建 journal、取得 lease 或启动 writer 的操作 MUST 使用 `Open`、`OpenOrCreate`、`Create` 或 `Start` 等显式生命周期入口；纯构造器不得访问文件系统或启动 goroutine。

在支持安全目录句柄的平台上，数据根、每一级日期目录和 journal SHALL 相对于已校验的父目录句柄逐级打开或创建，且不得跟随 symlink。目录类型与权限、journal 普通文件类型与权限 MUST 由实际打开的句柄校验；无法提供等价保证的平台 MUST fail closed 或使用经过测试的平台专属安全实现。

#### Scenario: Pure construction has no filesystem effect

- **WHEN** 调用方仅创建 Repository 配置或其他纯内存 Session 值
- **THEN** 数据根、日期目录、journal、lease 和后台 goroutine 均不会被创建或打开

#### Scenario: Reject a swapped path component

- **WHEN** 测试在目录遍历过程中确定性地把任一级日期目录或 journal 替换为 symlink、非目录或非普通文件
- **THEN** 创建或恢复在读取、repair、truncate 或 append 前失败
- **THEN** Session 数据根之外的目标 bytes 保持不变

#### Scenario: Validate every opened directory handle

- **WHEN** 数据根或某一级已有日期目录在支持权限位的平台上不是用户私有目录
- **THEN** Repository 根据已打开目录句柄的实际 metadata 拒绝继续
- **THEN** 不会依赖早先的路径 `Lstat` 结果继续打开子项

### Requirement: Sample usage is committed atomically with its sample

每个成功 Provider sample SHALL 在一个 durable batch 中依次写入 `provider_native_commit`、`sample_usage` 以及该 sample 的后续边界。无 tool call 的最终 sample 后续边界为 `turn_completed`；含 tool calls 的 sample 后续边界为一个或多个按 call index 排列的 `tool_call_ready`，输入联合覆盖Read、Glob、Grep、Patch、Write、Exec与ReadArtifact。同一 batch MUST 使用相同 session/thread/turn 和 batch identity并获得连续seq；只有整个batch成功Sync后才能确认任一事实或读取Patch目标。

tool-output native commit MUST 在全部对应 result facts 已按 call index durable 后单独提交且不得带 `sample_usage`。`sample_usage` payload继续完整包含五个normalized metrics，不得包含Provider raw usage、价格、请求正文、tool result、approval或diff。

Write 的完整参数 SHALL 在该原子 batch 内验证并保存。多个 Write 的聚合原生记录或 ready record 超过既有上限时，系统 MUST 在任何 workspace I/O 前失败，不扩大通用 Session 限额。

#### Scenario: Commit a heterogeneous call sample
- **WHEN** completed sample按顺序包含Glob、Patch与Read
- **THEN** journal原子追加native commit、sample usage和三个ready records
- **THEN** candidate preparation与executor只在整个batch Sync后才可发生

#### Scenario: Commit a new sample usage batch
- **WHEN** completed sample不含tool calls
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

### Requirement: Replay never treats command progress as durable state

ReplayPlanner SHALL只从Exec ready、approval、started、result和Provider-native output判断命令状态。命令progress、PID、spinner、实时输出chunk与后台handle不属于当前record vocabulary；即使宿主曾观察这些瞬态数据，resume也不得据此推断命令仍运行或尝试连接进程。

#### Scenario: Resume after a process crash
- **WHEN** journal包含Exec started但没有result且进程已经退出或状态未知
- **THEN** replay生成`outcome_uncertain`补偿而不查找PID、不连接进程、不重新执行命令

以上约束 SHALL 扩展到已实现工具统一活动投影；ready、approval、started、result 与 native commits 是恢复依据，临时 sample 文本、工具进度、折叠组与浏览页不属于事实源。

#### Scenario: Restore without presentation state
- **WHEN** 进程曾显示搜索 running 或未提交文本后中断
- **THEN** 恢复仅从已验证事实生成活动，不保留伪完成状态或未提交文本

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
