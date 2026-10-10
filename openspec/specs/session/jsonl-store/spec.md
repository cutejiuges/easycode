# session/jsonl-store Specification

## Purpose

定义可长期演进的 append-only JSONL Session 事实源，使当前文本对话与未来 Tool、MCP、Hook、Compaction 和 Subagent 记录共享稳定日志机制，而不把 SQLite 或 UI 事件变成恢复依据。

## Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含一个 schema canary、一个 payload canary、单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、sample/call index、capability，以及与 `Read`、`Glob` 或 `Grep` 匹配的完整 typed input；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。工具 records MUST NOT 保存 input revision、result codec revision 或 Provider wire item。

schema/payload canary 只用于精确验证当前格式，不得选择历史 decoder。completed sample 的 Provider entry 仍作为 native commit 并紧邻配对 `sample_usage`；tool results 编码为 tool-output native commit 时不生成 sample usage。高频参数 delta、进度、UI 状态和文件系统瞬时信息 MUST NOT 进入 JSONL。

#### Scenario: Encode the current record vocabulary
- **WHEN** 一个 root thread 完成包含 Read、Glob、Grep 的 Tool Loop
- **THEN** JSONL 复用同一当前 envelope 记录 metadata、turn 边界、sample commits/usages、三类 ledger records 和 tool-output commit
- **THEN** 每个已知 kind 均由唯一当前 typed codec 校验

#### Scenario: Encode the initial record vocabulary
- **WHEN** 一个 root thread 完成包含搜索的当前 Tool Loop
- **THEN** 当前 envelope 记录全部恢复必需 facts，且不存在 optional 或版本分派字段

#### Scenario: Reject an unknown record
- **WHEN** 程序读取 checksum 合法但 event kind 或 payload canary 未知的 record
- **THEN** loader 在 repair、append、executor 和 Provider 副作用前失败
- **THEN** payload 不进入日志、错误或 optional 透传列表

#### Scenario: Preserve an unknown optional record
- **WHEN** journal 包含旧开发期 `optional` record 或当前程序未知的 record
- **THEN** loader 不再保留或忽略它，而是在任何副作用前拒绝整个 journal

#### Scenario: Represent multiple samples in one turn
- **WHEN** Tool Loop 在同一 turn 记录多次 Provider samples
- **THEN** 每个 sample 按 seq 追加配对 native commit 和 sample usage，无需改变当前 envelope

#### Scenario: Represent multiple samples in one future turn
- **WHEN** Tool Loop 在同一 turn 记录多次 Provider samples
- **THEN** 每个 sample 复用唯一当前 envelope 和 codec，并保持 seq 与 usage 配对

#### Scenario: Commit a future tool result before the next sample
- **WHEN** 当前 tool results 已 durable 且下一 sample 尚未发起
- **THEN** tool-output native commit 单独持久化且不伪造 sample usage
- **THEN** 恢复无需重复工具 I/O 即可补齐或验证输出

#### Scenario: Keep native tool pairs separate from execution state
- **WHEN** sample native commit 包含原生 tool calls
- **THEN** Provider codec 保留原生内容，而 ready/started/result 状态只由独立 typed ledger records 表达

#### Scenario: Preserve a future native tool pair without conflating execution state
- **WHEN** sample native commit 包含原生 tool calls
- **THEN** Provider codec 保留原生内容，而当前 typed ledger 独立表达执行状态

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

### Requirement: Replay validation enforces record state transitions

Loader 完成字节、envelope 和 batch 校验后，ReplayPlanner SHALL 在创建 Provider Conversation 前对全部当前 typed records 执行语义回放。首个 batch MUST 恰好建立唯一 `session_meta` 与 root `thread_meta`；同一 root thread 最多有一个活动 turn。

文本 turn 仍允许 `turn_started` 后以 `[provider_native_commit, sample_usage, turn_completed]` 完成，或以单个 `turn_failed` 失败。Tool Loop 中，一个含 calls 的 sample batch MUST 严格为 `[provider_native_commit, sample_usage, tool_call_ready...]`，ready records 的 sample/call index 连续且 identity 唯一；全部 started records 和全部 result records 都 MUST 分别按原 call index 单调出现。正常状态按 `ready -> execution_started -> tool_call_result` 转换；取消在 executor 接受前线性化时，唯一旁路是 `ready -> tool_call_result(status=cancelled)`。全部 results 存在后，恰好一个按 call index 配对的 tool-output `provider_native_commit` 关闭该 call group，之后才可出现下一 sample 或 turn failure。

ReplayPlanner MUST 拒绝孤立/重复 result、started 早于 ready、started/result index 倒序、ready 后直接开始下一 sample、outputs 数量或 call identity 不匹配、sample usage 缺失/重复、tool-output commit 携带 usage、terminal 后 records 或新 turn 覆盖活动 turn。只有 journal 末尾已承诺的活动状态可作为显式 reconciliation 计划返回；它不是可由 Loader 截断的文件损坏。

#### Scenario: Build a valid parallel tool replay plan
- **WHEN** journal 包含一个异构 call sample、按 index 的 starts、按 index 的 results、tool outputs 和最终 sample completion
- **THEN** ReplayPlanner 按 seq 返回 native commits、sample usages、每个 ledger 状态和 completed turn

#### Scenario: Build a valid text replay plan
- **WHEN** journal 包含 metadata、一个失败文本 turn 和一个完整文本 turn
- **THEN** ReplayPlanner 产生当前 schema 定义的 native commits、usage 与 terminal 投影

#### Scenario: Reject a checksum-valid illegal transition
- **WHEN** JSON、seq、batch 和 checksum 合法但 usage 配对、ledger 顺序、call index 或 terminal placement 非法
- **THEN** resume 以稳定 Session corruption 错误失败且不创建 Conversation 或调用 executor

#### Scenario: Report a committed interrupted tail
- **WHEN** 文本 journal 以完整 committed `turn_started` 结束且没有后续事实
- **THEN** ReplayPlanner 保留该 record 并返回 interrupted-tail 状态而不截断

#### Scenario: Build a valid tool replay plan
- **WHEN** journal 包含合法 call sample、有序 ledger、tool outputs 和最终 sample completion
- **THEN** ReplayPlanner 按 seq 返回当前 native commits、sample usages、ledger 状态和 completed turn

#### Scenario: Reject completion-order persistence
- **WHEN** 执行实际以 call 2、call 0、call 1 完成且 journal 也按该完成顺序写入 results
- **THEN** ReplayPlanner 以稳定 Session corruption 错误拒绝该 journal

#### Scenario: Report a reconcilable ready tail
- **WHEN** journal 在合法 ready batch 后结束且部分或全部调用没有 started
- **THEN** ReplayPlanner 保留 committed records 并按 call index 返回待本地取消补偿的 ready 状态

#### Scenario: Report an uncertain execution tail
- **WHEN** journal 包含合法 execution-start 但缺少对应 result
- **THEN** ReplayPlanner 返回必须补偿的 uncertain 状态且不得把调用重新归类为 ready

#### Scenario: Accept cancellation before executor start
- **WHEN** journal 包含 ready 后直接追加的 matching cancelled result
- **THEN** ReplayPlanner 接受该终态，并拒绝同一 invocation 后续出现 started 或第二个 result

#### Scenario: Reject non-cancelled result before executor start
- **WHEN** journal 包含 ready 后直接追加的 success、error 或 outcome-uncertain result
- **THEN** ReplayPlanner 以稳定 Session corruption 错误失败

### Requirement: Tail repair never rewrites committed history

loader SHALL 只修复文件末尾的半行或未完成 batch：截断到最后一个完整 committed batch 边界，保留此前所有合法 bytes，并以结构化结果报告发生了修复。已经完整提交的 batch、文件中间的损坏和语义非法记录 MUST NOT 被自动删除、重排或重写。

修复后 writer SHALL 从最后一个保留记录的下一 seq 继续；反复加载同一已修复文件 MUST 是幂等的。

#### Scenario: Repair a truncated final line

- **WHEN** 进程中断导致 JSONL 最后一行只写入部分 bytes
- **THEN** loader 截断该半行并保留之前的全部 committed batches
- **THEN** 修复结果明确报告被截断的尾部而不回显其 payload

#### Scenario: Repair a trailing incomplete batch

- **WHEN** 尾部含有一个或多个完整 JSON 行但逻辑 batch 缺少 commit 边界
- **THEN** loader 将整个未完成 batch 从可恢复事实中移除并从其起始 offset 截断
- **THEN** 先前 committed batch 的原始 bytes 保持不变

#### Scenario: Reopen after repair

- **WHEN** 已修复的 thread 再次打开并追加记录
- **THEN** 新记录从保留历史的下一 seq 开始，第二次加载不再报告同一尾部修复

#### Scenario: Do not truncate a committed interrupted turn

- **WHEN** 最后一条 `turn_started` 所在 batch 已完整写入并通过 checksum，但进程在 terminal 前退出
- **THEN** loader 保留该 batch，由 replay validator 报告 interrupted-tail 状态

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

大工具结果超过行大小边界时，后续已批准能力 SHALL 使用权限受控且具备完整性信息的 artifact，并在 JSONL 中保存有界 typed 引用，而不是放宽所有记录的上限。本变更的搜索结果只保存 64 KiB 内的冻结模型 preview 和有界 typed metadata，不引入通用 artifact。

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

### Requirement: Current record schema exposes typed construction and decoding

每个当前已知 Session `event_kind` SHALL 具有专属强类型 draft constructor、strict decoder 和 validator。公共 record draft、writer、replay 和 lifecycle 接口 MUST NOT 接受或返回无约束动态值；构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON、canary 不匹配和不满足当前语义的 payload。业务类型和构造器不得用版本后缀复制当前 schema，decoder registry 不得以 payload version 选择实现。

所有当前 record codecs SHALL 深拷贝 preview/input 等可变 bytes，并验证 identity、index、status 与大小边界。本变更 SHALL 一次性重写仓库开发期 fixture，以一套固定当前 fixture 覆盖 text turn、异构并行 Tool Loop、补偿与损坏拒绝；实现不得保留旧 Provider payload reader、旧 Tool revision reader、optional record 分支、旧 fixture 兼容或混合 revision 恢复路径。

#### Scenario: Construct every tool record through a typed API
- **WHEN** 调用方创建 Read、Glob、Grep 的 ready、execution-started 或 result draft
- **THEN** constructor 只接收与 capability 匹配的当前强类型值并立即验证
- **THEN** draft 不能被改造成 kind、capability 与 payload 不匹配的 record

#### Scenario: Construct every current record through a typed API
- **WHEN** 调用方创建任一当前 record
- **THEN** 对应 constructor 只接收该 kind 的强类型字段并立即验证当前 canary 与语义

#### Scenario: Strictly decode a tool result
- **WHEN** checksum 合法的 result payload 包含未知字段、非法 status、超限 preview 或尾随 JSON
- **THEN** 唯一当前 decoder 在 replay 前拒绝记录且错误不回显 preview 正文

#### Scenario: Strictly decode a known payload revision
- **WHEN** 任一 checksum 合法的当前 payload 包含未知字段、尾随 JSON 或非法语义
- **THEN** 唯一当前 decoder 在 replay 前拒绝且错误不包含 payload 正文

#### Scenario: Replay the current immutable tool fixture
- **WHEN** 当前实现加载仓库内固定的异构并行 Tool Loop fixture
- **THEN** 生产 Loader、codec 和 ReplayPlanner 产生预期 ledger、顺序与 native commit 摘要
- **THEN** fixture 不是测试运行时由当前 encoder 生成

#### Scenario: Reject a superseded development fixture
- **WHEN** Loader 读取已被本变更替换的 envelope、Provider payload 或 Tool ledger shape
- **THEN** 恢复在 repair、append、executor 或 Provider 网络调用前失败，不进入兼容分支

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

每个成功 Provider sample SHALL 在一个 durable batch 中依次写入 `provider_native_commit`、`sample_usage` 以及该 sample 的后续边界。无 tool call 的最终 sample 后续边界为 `turn_completed`；含 tool calls 的 sample 后续边界为一个或多个按 call index 排列的 `tool_call_ready`。同一 batch MUST 使用相同 session/thread/turn 和 batch identity 并获得连续 seq；只有整个 batch 成功 Sync 后才能确认任一事实。

tool-output native commit MUST 在全部对应 result facts 已按 call index durable 后单独提交且不得带 `sample_usage`。`sample_usage` payload 继续完整包含五个 normalized metrics，不得包含 Provider raw usage、价格、请求正文或 tool result。

#### Scenario: Commit a new sample usage batch
- **WHEN** completed sample 不含 tool calls
- **THEN** journal 原子追加 native commit、sample usage 和 turn completion

#### Scenario: Fail an incomplete usage batch
- **WHEN** 尾部 sample batch 缺少 usage、ready 或 completion 中的任一必需 record
- **THEN** Loader 按既有 batch repair 规则移除整个未完成 batch，不恢复部分 history、usage 或 call

#### Scenario: Fail closed on the superseded development baseline
- **WHEN** Loader 读取缺少 sample usage 或使用旧 Provider/Tool payload shape 的开发期 fixture
- **THEN** resume 在 repair、append、executor 或 Provider 调用前失败，不保留兼容 reader

#### Scenario: Fail an incomplete new usage batch
- **WHEN** 当前尾部 sample batch 缺少 usage、ready 或 completion 中的任一必需 record
- **THEN** Loader 按 batch repair 规则移除整个未完成 batch，不恢复部分事实

#### Scenario: Commit an heterogeneous call sample
- **WHEN** completed sample 按顺序包含 Glob、Grep 与 Read
- **THEN** journal 原子追加 native commit、sample usage 和三个 ready records
- **THEN** executor 只在整个 batch Sync 后才可接收调用

#### Scenario: Commit a call sample
- **WHEN** completed sample 按顺序包含两个 Read calls
- **THEN** journal 原子追加 native commit、sample usage 和两个 ready records
- **THEN** executor 只在整个 batch Sync 后才可接收调用

#### Scenario: Reject usage on a tool-output commit
- **WHEN** journal 把 tool-output commit 与 sample usage 配对
- **THEN** ReplayPlanner 拒绝该状态而不是把 output 误计为 Provider sample
