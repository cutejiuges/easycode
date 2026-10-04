# session/jsonl-store Specification

## Purpose

定义可长期演进的 append-only JSONL Session 事实源，使当前文本对话与未来 Tool、MCP、Hook、Compaction 和 Subagent 记录共享稳定日志机制，而不把 SQLite 或 UI 事件变成恢复依据。

## Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL 记录 SHALL 使用强类型、版本化的 envelope，至少包含 `schema_version`、`payload_version`、`replay_requirement`、单调 `seq`、UTC `timestamp`、`session_id`、`thread_id`、`event_kind`、完整性校验值和受控 JSON `payload`；与记录语义相关时 SHALL 同时包含 `parent_thread_id` 与 `turn_id`。同一 thread 文件中的 session/thread 标识 MUST 保持一致，未知 payload 只能作为有边界的 opaque JSON 传递，不得展开为跨层 `map[string]any`。

`replay_requirement` v1 只能是 `required` 或 `optional`。影响 Provider 原生历史、turn lifecycle、usage 归属、权限、工具副作用、幂等或恢复决策的记录 MUST 标记为 `required`；只影响索引、展示或可丢失统计且被省略不会改变后续副作用、Provider request 或已发布用量事实的记录才可标记为 `optional`。七种已知记录的当前 revision 全部 SHALL 标记为 `required`。已知 kind/revision 的 requirement、cardinality、顺序约束和 payload codec SHALL 由强类型语义 registry 唯一声明，记录中的声明与 registry 不一致时 MUST 拒绝恢复。

本切片 SHALL 定义 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`sample_usage`、`turn_completed` 和 `turn_failed` 的已知 payload。`provider_native_commit` 表示一个由对应 Provider codec 定义的有序 native history 增量；`sample_usage` 表示紧邻的同一 completed sample 的 normalized usage。当前首版 native 增量是一次成功文本 sample 的输入与输出，不得把该形状或“一个 turn 只有一次 commit”固化到公共 envelope。

未来 Provider wire 中出现的 `tool_use`、`tool_result`、reasoning 或 MCP tool block SHALL 作为相应 sample 的 Provider 原生内容保留在 `provider_native_commit`；权限决策、工具副作用、幂等 ledger、MCP progress 和大结果 artifact 等共享执行事实不得伪装成 Provider 原生 payload，必须由后续 change 定义独立记录种类与持久化策略。

#### Scenario: Encode the initial record vocabulary

- **WHEN** 一个 root thread 完成一次文本 turn
- **THEN** JSONL 使用同一 envelope 记录 session/thread 元数据、turn 边界、该次 Provider sample 的原生提交和 normalized usage
- **THEN** 每条记录都包含可独立校验的 schema 与 payload revision
- **THEN** 七种已知记录的当前 revision 均标记为 `required`

#### Scenario: Preserve an unknown optional record

- **WHEN** 当前程序读取一个 envelope 版本受支持、结构和 checksum 合法但 kind/revision 未知且标记为 `optional` 的记录
- **THEN** loader 有界保留其 opaque payload 并报告 kind/revision，resume 可忽略该记录继续
- **THEN** 未知 payload 不进入日志、错误或语义投影

#### Scenario: Represent multiple samples in one future turn

- **WHEN** 后续工具循环需要在同一个 turn 下记录多次 Provider sample
- **THEN** 每次 completed sample 可按 seq 追加配对的 `provider_native_commit` 和 `sample_usage`，无需改变既有 envelope 或把原有文本 turn 改写为新形状

#### Scenario: Commit a future tool result before the next sample

- **WHEN** 后续工具循环已经 durable 完成工具结果，但下一次 Provider sample 尚未发起或完成
- **THEN** 后续 change 必须定义独立、强类型的 input-only native history commit shape；只有无法在已冻结 v1 中加法表达且必须兼容既有数据时才可引入新的 payload revision
- **THEN** input-only commit 不伪造 `sample_usage`，恢复不需要重复工具副作用，也不需要等待下一次模型输出才能保留 Provider 原生 tool result

#### Scenario: Preserve a future native tool pair without conflating execution state

- **WHEN** 后续工具循环的 Provider sample 包含原生 tool call 或 tool result item
- **THEN** Provider codec 在相应的强类型 commit shape 中无损保存该原生内容，而 JSONL envelope 和写入机制保持不变；新增 revision 必须满足冻结契约不兼容且需要并存的版本门槛
- **THEN** 权限、执行、幂等和 artifact 事实使用独立 record kind，不从 Provider 原生内容反推

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

Session loader SHALL 按行和 batch 顺序校验记录大小、JSON、完整性校验值、schema revision、seq 连续性、标识一致性和 batch 闭合性。已知 v1 envelope MUST 拒绝重复顶层 member、未知顶层 member、尾随 JSON value、非法 UTF-8 以及非 canonical ID/时间字段。checksum SHALL 覆盖移除 checksum 字段后的强类型 canonical envelope bytes，包括 opaque payload 的持久化表示；Provider payload 内部语义仍由对应 Provider codec 校验。

loader MUST 将结构完整但上层未知的记录保持为有界 opaque payload 并报告其 kind/revision。未知 `required` kind/revision MUST 使 resume 失败；未知 `optional` kind/revision MAY 被恢复消费者忽略。文件中间的损坏、seq 跳变、标识漂移、重复元数据、requirement 与 registry 不一致或未知 required 记录 MUST 产生明确错误，系统不得静默跳过或用默认值补全。

#### Scenario: Load a valid journal

- **WHEN** thread 文件包含受支持且 seq 连续的完整 batches
- **THEN** loader 按 seq 返回所有记录，且不会重排或改写 payload

#### Scenario: Reject corruption in the middle

- **WHEN** 一个非尾部记录 JSON 损坏、超出大小上限、seq 不连续或标识与文件元数据不一致
- **THEN** loader 返回稳定英文 session corruption 错误并且不返回部分可恢复历史

#### Scenario: Reject an unsupported required record

- **WHEN** journal 包含当前程序不理解且标记为 `required` 的 kind/revision
- **THEN** resume 在发起网络请求或其他副作用前失败
- **THEN** 未知 payload 不进入日志或错误文本

#### Scenario: Reject ambiguous JSON envelope fields

- **WHEN** 一行包含重复 `seq`、未知 v1 顶层字段或一个合法对象后的尾随 JSON value
- **THEN** loader 将该行视为损坏并且不得依赖 JSON decoder 的覆盖或宽松默认行为

### Requirement: Replay validation enforces record state transitions

在 loader 完成字节、envelope 和 batch 校验后，恢复消费者 SHALL 在创建可调用 Provider Conversation 前对已知 required records 执行强类型语义回放。首个 committed batch MUST 恰好建立唯一的 `session_meta` 与 root `thread_meta`；后续 metadata 不得重复。root thread 同一时刻最多有一个活动 turn，`provider_native_commit`、`sample_usage` 和 terminal 必须引用当前活动 turn，terminal 关闭该 turn 后才能开始下一 turn。

当前 v1 的唯一合法完成路径 SHALL 为独立 committed `turn_started(v1)`，随后是同一 batch 中严格按顺序出现的 `[provider_native_commit(v1), sample_usage(v1), turn_completed(v1)]`；`sample_usage` 与前一 native commit 配对，不能缺失、重复或脱离 completed sample。本变更前的 `[provider_native_commit(v1), turn_completed(v1)]` 开发期结构不再是合法回放路径，loader MUST 在 repair、append 或 Provider 请求前 fail closed，且不得补写 usage 或把它作为同版本的历史变体继续运行。合法失败路径 SHALL 为 `turn_started` 后的唯一 `turn_failed` 且不包含当前 turn 的成功 sample facts。只有位于 committed journal 末尾且尚无后续记录的单个未闭合 `turn_started` 可被识别为 interrupted-tail replay state；它是需要显式收口的业务状态，不是可由 loader 截断的文件损坏。未来工具循环 MAY 通过新的 required kind 扩展状态转换；只有冻结后的 v1 无法加法表达且旧数据必须并存时才可新增 revision。

#### Scenario: Build a valid text replay plan

- **WHEN** journal 包含唯一 metadata batch、一个失败文本 turn，以及一个当前 v1 三记录完成 batch
- **THEN** replay validator 按 seq 生成包含 committed native commits、sample usage 和 terminal 状态的完整计划
- **THEN** Provider 只接收计划中通过语义校验的 native commits

#### Scenario: Reject a checksum-valid illegal transition

- **WHEN** journal 的 JSON、seq、batch 和 checksum 均合法，但 v1 completion 缺少配对 usage、usage 顺序错误、存在重复 usage、commit 位于 turn 之外、重复 terminal 或新 turn 覆盖未结束 turn
- **THEN** resume 以稳定英文 session corruption 错误失败且不创建可调用 Conversation

#### Scenario: Report a committed interrupted tail

- **WHEN** journal 以完整 committed `turn_started` 结束且没有该 turn 的 native commit、sample usage 或 terminal
- **THEN** replay validator 返回显式 interrupted-tail 状态而不截断该记录、不补造成功历史或 usage

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

### Requirement: Published Session revisions remain replay-compatible through immutable fixtures

每个已发布的 envelope schema revision 和已知 payload revision MUST 在仓库中保留由固定历史 bytes 构成的不可变 compatibility fixture，以及对应的预期 ReplayPlan 摘要。fixture MUST 独立于当前 encoder 生成，MUST 使用与生产相同的 Loader、checksum、batch、registry 和 ReplayPlanner 路径验证，并不得包含 secret、敏感 Header、base URL 或本机路径。

当前程序 MUST 能读取所有仍受支持的历史 fixture，并在其末尾继续追加更大 seq，而不重写、重新编码或复制 fixture 中的既有 records。版本升级 MUST 在写入新 revision 前增加从所有受支持旧 fixture 到当前 replay model 的 migration regression；默认迁移语义是版本专属解码与只读投影，不是在 resume 时原地改写 journal。

当前程序遇到不受支持的较新 required schema/kind/payload revision 时 MUST 在任何 repair、append 或 Provider 副作用前失败，并保持原始 bytes 不变。

#### Scenario: Replay the immutable v1 baseline

- **WHEN** 当前程序加载仓库内固定的 v1 root-thread JSONL fixture
- **THEN** Loader 与 ReplayPlanner 产生预期 identity、metadata、turn boundaries、native commits 和下一 seq
- **THEN** fixture 不是由测试运行时调用当前 encoder 临时生成

#### Scenario: Continue a historical fixture without rewriting it

- **WHEN** 程序从受支持的历史 fixture 恢复并 durable 追加一个新 batch
- **THEN** 新记录从 fixture 的下一 seq 开始
- **THEN** 追加前的全部 fixture bytes 保持逐字节不变

#### Scenario: Reject an unsupported newer required revision read-only

- **WHEN** journal 包含当前程序不支持的较新 required schema、kind 或 payload revision
- **THEN** 加载或恢复以稳定英文 Session 错误失败
- **THEN** journal 不被 repair、迁移、截断或追加，且不会发起 Provider 请求

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

JSONL 事实源 SHALL 持久化恢复、幂等和审计所需的业务边界，不得默认记录每个 assistant text delta、spinner、窗口尺寸或其他 UI 瞬态。后续 Tool、MCP、permission、usage、hook、compact 或 subagent 能力 SHALL 通过新增强类型 event kind/payload revision 和 migration fixture 扩展记录词汇；只要 envelope 语义兼容，MUST NOT 改写已有记录或替换 append/Sync/repair 机制。

工具或 MCP 的流式 progress 与 UI delta 默认 SHALL NOT 持久化；只有被后续规约认定为恢复、幂等或审计必需的终态/检查点才能进入 Session。大工具结果超过行大小边界时，后续实现 SHALL 使用权限受控且具备完整性信息的 artifact，并在 JSONL 中保存有界引用，而不是放宽所有记录的上限。

#### Scenario: Stream many text deltas

- **WHEN** Provider 在一个 sample 中产生大量 assistant 文本增量后成功完成
- **THEN** Session 保存最终 Provider 原生提交和生命周期边界，而不是为每个文本 delta 追加一条事实记录

#### Scenario: Add a future tool result record

- **WHEN** 后续 change 引入工具执行结果和幂等 ledger 记录
- **THEN** 新记录复用相同 envelope、seq、batch、Sync、修复和权限契约
- **THEN** 已有 Session records 的语义与 bytes 不被回写

#### Scenario: Ignore transient MCP progress

- **WHEN** MCP 工具在一次执行中产生大量仅用于界面反馈的 progress 更新
- **THEN** Session 不逐条持久化这些瞬态更新
- **THEN** 后续定义的最终工具结果、幂等状态和必要 artifact 引用仍按 durable 事实记录

### Requirement: Known record revisions expose typed construction and decoding

每个已知 Session `event_kind`/`payload_version` 组合 SHALL 具有专属的强类型 draft constructor、严格 decoder 和语义 validator。公共 record draft、writer、replay 与生命周期接口 MUST NOT 接受或返回无约束动态值；未知扩展只允许作为有大小边界的 opaque JSON 保留。构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON 和不满足该 revision 语义不变量的 payload。

新增 `sample_usage(v1)` MUST 使用专属 typed codec；`turn_completed` 保持 payload v1，并由 v1 ReplayPlanner 校验其配对 usage。七种当前 payload 的 canonical JSON、envelope 和 checksum SHALL 通过重写后的单一 v1 fixture 固定；实现不得保留旧两记录完成结构的 decoder/replay 成功分支，也不得为本能力引入 v2 codec。

#### Scenario: Construct every v1 record through a typed API

- **WHEN** 调用方创建 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`sample_usage`、`turn_completed` 或 `turn_failed` 的受支持 draft
- **THEN** 对应 constructor 只接收该 kind/revision 的强类型字段或 payload
- **THEN** draft 不能被调用方改造成 kind、revision 与 payload 不匹配的记录

#### Scenario: Strictly decode a known payload revision

- **WHEN** checksum 合法的已知 record payload 含未知字段、尾随 JSON 或非法 usage 语义值
- **THEN** 该 kind/revision 的 decoder 在 replay 前拒绝记录
- **THEN** 错误不包含 opaque payload 正文

#### Scenario: Preserve historical v1 bytes

- **WHEN** 当前实现加载本变更重写并冻结的 v1 fixture，并以相同固定 identity、时间和 batch 参数编码等价记录
- **THEN** 解码后的 typed replay model 与当前 v1 期望一致
- **THEN** canonical record bytes 和 checksum 与重写后 fixture 完全一致

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

当前 v1 成功文本 sample SHALL 在一个 durable batch 中依次写入 `provider_native_commit(v1)`、`sample_usage(v1)` 和 `turn_completed(v1)`。三条记录 MUST 使用相同 session/thread/turn 与 batch identity并获得连续 seq；只有整个 batch 成功 Sync 后才能确认任一事实。`sample_usage` payload MUST 完整包含五个 normalized metrics 及合法状态，不得包含 Provider raw usage、价格或请求正文。

本变更前的 `[provider_native_commit(v1), turn_completed(v1)]` 开发期 batch SHALL 被拒绝为不兼容的旧基线，不得继续回放、补写全零 `sample_usage`、原地升级或恢复后追加新 turn。原 journal bytes MUST 保持不变并可供人工检查。

#### Scenario: Commit a new sample usage batch

- **WHEN** 当前 writer 收到合法 prepared sample 并完成文本 turn
- **THEN** journal 以连续顺序原子追加三个 v1 record：native commit、normalized sample usage 和 completion
- **THEN** durable success 只在三条记录均 Sync 后返回

#### Scenario: Fail closed on the superseded development baseline

- **WHEN** loader 读取本变更前生成的 v1 两记录完成 batch
- **THEN** resume 在 repair、append 或 Provider 请求前以稳定英文不兼容错误失败
- **THEN** journal bytes 不被修改且不生成全零 usage或兼容 replay plan

#### Scenario: Fail an incomplete new usage batch

- **WHEN** 尾部新 completion batch 只写入 native commit 或 native commit 加 sample usage而未完整提交
- **THEN** loader 按既有 batch repair 规则移除整个未完成 batch
- **THEN** 不恢复部分 native history 或孤立 usage
