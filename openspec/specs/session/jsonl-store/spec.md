# session/jsonl-store Specification

## Purpose

定义可长期演进的 append-only JSONL Session 事实源，使当前文本对话与未来 Tool、MCP、Hook、Compaction 和 Subagent 记录共享稳定日志机制，而不把 SQLite 或 UI 事件变成恢复依据。

## Requirements

### Requirement: Session records use a versioned extensible envelope

每条JSONL记录 SHALL使用既有强类型、版本化envelope，至少包含schema/payload revision、replay requirement、单调seq、UTC timestamp、session/thread/turn identity、event kind、batch边界、checksum和受控payload。未知payload只可作为有界opaque JSON传递，不得展开为跨层 `map[string]any`。

影响Provider原生历史、turn lifecycle、usage、权限、工具副作用、幂等或恢复决策的记录 MUST标记 `required`。本变更 SHALL在当前记录词汇中新增 `tool_call_ready`、`tool_execution_started` 和 `tool_call_result`，三者全部为required并具有专属typed constructor、strict decoder与validator。数值payload revision只存在于record envelope，不进入Go业务类型或构造器名称。

`tool_call_ready` SHALL保存invocation ID、Provider call ID、sample/call index、capability及input revision和完整typed Read input；`tool_execution_started` SHALL引用同一invocation并表示executor接收线性化点；`tool_call_result` SHALL保存终态status、稳定code、result codec revision、完整有界模型preview与非敏感结果metadata。共享ledger不得嵌入Provider wire item，Provider call/output仍保存在 `provider_native_commit`。

completed sample的Provider entry仍作为native commit并紧邻配对 `sample_usage`。tool results编码为tool-output native commit时不生成sample usage。高频参数delta、进度、UI状态和文件系统瞬时信息 MUST NOT进入JSONL。

#### Scenario: Encode the initial record vocabulary
- **WHEN** 一个root thread完成一次Read Tool Loop
- **THEN** JSONL复用同一envelope记录metadata、turn边界、sample commits/usages、三类ledger records和tool-output commit
- **THEN** 每个已知kind/revision均可独立校验且标记为required

#### Scenario: Preserve an unknown optional record
- **WHEN** 程序读取结构和checksum合法但未知且标记optional的记录
- **THEN** loader有界保留opaque payload并允许恢复消费者忽略，且payload不进入日志或错误

#### Scenario: Represent multiple samples in one future turn
- **WHEN** Tool Loop在同一turn记录多次Provider samples
- **THEN** 每个sample按seq追加配对native commit和sample usage，无需改变envelope或改写既有records

#### Scenario: Commit a future tool result before the next sample
- **WHEN** tool results已durable且下一sample尚未发起
- **THEN** tool-output native commit单独持久化且不伪造sample usage
- **THEN** 恢复无需重复工具I/O即可继续

#### Scenario: Preserve a future native tool pair without conflating execution state
- **WHEN** sample native commit包含原生tool calls
- **THEN** Provider codec保留原生内容，而ready/started/result状态只由独立ledger records表达

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

Loader完成字节、envelope和batch校验后，ReplayPlanner SHALL在创建Provider Conversation前对全部known required records执行强类型语义回放。首个batch MUST恰好建立唯一 `session_meta`与root `thread_meta`；同一root thread最多有一个活动turn。

文本turn仍允许 `turn_started` 后以 `[provider_native_commit, sample_usage, turn_completed]` 完成，或以单个 `turn_failed` 失败。Tool Loop中，一个含calls的sample batch MUST严格为 `[provider_native_commit, sample_usage, tool_call_ready...]`，ready records的sample/call index连续且call identity唯一；正常执行按 `ready -> execution_started -> tool_call_result` 单向转换。取消在Executor接受前线性化时，唯一允许的旁路是 `ready -> tool_call_result(status=cancelled)`；ready直接进入success、error或outcome-uncertain result MUST被拒绝。全部results存在后，恰好一个tool-output `provider_native_commit`关闭该call组，之后才可出现下一sample或turn failure。最终无calls sample必须以 `[provider_native_commit, sample_usage, turn_completed]` 收口。

ReplayPlanner MUST拒绝孤立/重复result、started早于ready、call index乱序、ready后直接开始下一sample、outputs数量不匹配、sample usage缺失/重复、tool-output commit携带usage、terminal后records或新turn覆盖活动turn。只有journal末尾的已承诺活动状态可作为显式reconciliation计划返回；它不是可由Loader截断的文件损坏。

#### Scenario: Build a valid text replay plan
- **WHEN** journal包含metadata、一个失败文本turn和一个三记录完成文本turn
- **THEN** ReplayPlanner产生当前schema定义的native commits、usage和terminal投影

#### Scenario: Reject a checksum-valid illegal transition
- **WHEN** JSON、seq、batch和checksum合法但usage配对、ledger顺序、call index或terminal placement非法
- **THEN** resume以稳定Session corruption错误失败且不创建Conversation或调用executor

#### Scenario: Report a committed interrupted tail
- **WHEN** 文本journal以完整committed `turn_started`结束且没有后续事实
- **THEN** ReplayPlanner保留该record并返回既有interrupted-tail状态而不截断

#### Scenario: Build a valid tool replay plan
- **WHEN** journal包含call sample、两个顺序invocations、tool outputs和最终sample completion
- **THEN** ReplayPlanner按seq返回native commits、sample usages、ledger状态和completed turn

#### Scenario: Report a reconcilable ready tail
- **WHEN** journal在合法ready batch后结束且没有started
- **THEN** ReplayPlanner保留committed records并返回待本地取消补偿的ready状态，不截断或补造result

#### Scenario: Accept cancellation before executor start
- **WHEN** journal包含ready后直接追加的matching cancelled result
- **THEN** ReplayPlanner接受该终态，并拒绝同一invocation后续出现started或第二个result

#### Scenario: Reject non-cancelled result before executor start
- **WHEN** journal包含ready后直接追加的success、error或outcome-uncertain result
- **THEN** ReplayPlanner以稳定Session corruption错误失败

#### Scenario: Report an uncertain execution tail
- **WHEN** journal在合法execution-start后结束且没有result
- **THEN** ReplayPlanner返回必须补偿的uncertain状态且不得把调用重新归类为ready

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

### Requirement: Current record schema exposes typed construction and decoding

每个当前已知Session `event_kind`/`payload_version`组合 SHALL具有专属强类型draft constructor、strict decoder和validator。公共record draft、writer、replay和lifecycle接口 MUST NOT接受或返回无约束动态值；未知扩展只允许作为有大小边界的opaque JSON保留。构造和解码都 SHALL拒绝字段缺失、未知字段、尾随JSON和不满足revision语义的payload。业务类型和构造器不得用版本后缀复制当前schema。

当前十种record codec SHALL深拷贝preview/input等可变bytes，并验证identity、index、status与大小边界。由于当前没有线上用户，本变更 SHALL一次性重写仓库开发期fixture，以一套当前fixture固定canonical JSON、envelope、checksum和ReplayPlan；实现不得保留旧Provider payload reader、旧fixture兼容分支或混合revision恢复路径。

#### Scenario: Construct every tool record through a typed API
- **WHEN** 调用方创建ready、execution-started或result draft
- **THEN** constructor只接收该kind/revision的强类型值且立即验证
- **THEN** draft不能被改造成kind、revision与payload不匹配的记录

#### Scenario: Construct every current record through a typed API
- **WHEN** 调用方创建任一当前required record
- **THEN** 对应constructor只接收该kind的强类型字段且立即验证当前revision

#### Scenario: Strictly decode a tool result
- **WHEN** checksum合法的result payload包含未知字段、非法status、超限preview或尾随JSON
- **THEN** decoder在replay前拒绝记录且错误不回显preview正文

#### Scenario: Strictly decode a known payload revision
- **WHEN** 任一checksum合法的known payload包含未知字段、尾随JSON或非法语义
- **THEN** revision专属decoder在replay前拒绝且错误不包含payload正文

#### Scenario: Replay the current immutable tool fixture
- **WHEN** 当前实现加载仓库内固定的Tool Loop fixture
- **THEN** 生产Loader、registry、codec和ReplayPlanner产生预期ledger及native commit摘要
- **THEN** fixture不是测试运行时由当前encoder生成

#### Scenario: Reject a superseded development fixture
- **WHEN** Loader读取已被本变更替换的旧Provider payload shape或revision
- **THEN** 恢复在repair、append、executor或Provider网络调用前失败，不进入兼容分支

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

每个成功Provider sample SHALL在一个durable batch中依次写入 `provider_native_commit`、`sample_usage`以及该sample的后续边界。无tool call的最终sample后续边界为 `turn_completed`；含tool calls的sample后续边界为一个或多个 `tool_call_ready`。同一batch MUST使用相同session/thread/turn和batch identity并获得连续seq；只有整个batch成功Sync后才能确认任一事实。

tool-output native commit MUST在全部对应result facts durable后单独提交且不得带 `sample_usage`。`sample_usage` payload继续完整包含五个normalized metrics，不得包含Provider raw usage、价格、请求正文或tool result。

#### Scenario: Commit a new sample usage batch
- **WHEN** completed sample不含tool calls
- **THEN** journal原子追加native commit、sample usage和turn completion

#### Scenario: Fail closed on the superseded development baseline
- **WHEN** Loader读取缺少sample usage或使用旧Provider payload shape的开发期fixture
- **THEN** resume在repair、append、executor或Provider调用前失败，不保留兼容reader或原地改写journal

#### Scenario: Fail an incomplete new usage batch
- **WHEN** 尾部sample batch缺少usage、ready或completion中的任一必需记录
- **THEN** Loader按既有batch repair规则移除整个未完成batch，不恢复部分history、usage或call

#### Scenario: Commit a call sample
- **WHEN** completed sample按顺序包含两个Read calls
- **THEN** journal原子追加native commit、sample usage和两个ready records
- **THEN** executor只在整个batch Sync后才可接收调用

#### Scenario: Reject usage on a tool-output commit
- **WHEN** journal把tool-output commit与sample usage配对
- **THEN** ReplayPlanner拒绝该状态而不是把output误计为Provider sample
