## Context

参见 `proposal.md` 的动机与范围。当前双 Provider 已经具备会话级 `Conversation`、Provider 原生历史和单向 `HistoryProjector`，但存在三个会影响持久化接缝的现状：

- OpenAI/Anthropic 都在各自的 stream consumer 收到成功终态时直接修改内存历史，随后才向 Runtime 发出 completed；Runtime 无法在内存提交与成功事件之间插入 durable Session 写入。
- Runtime 目前只忽略式接收 native item、转发语义事件，事件没有稳定 session/thread/turn identity；`internal/session` 只有 `Record`/`Store` 占位契约，应用每次启动都创建空 Conversation。
- TUI 只投影 live RuntimeEvent，没有接收恢复后语义历史的初始化入口；CLI 也没有 resume 参数。

架构要求 JSONL 是事实源，Provider-native history 是续写事实依据，SQLite 和 RuntimeEvent 都不能替代二者。Session 还必须为 P3 Tool、P6 MCP 和 P7 Subagent 留出演进空间，但本次不能凭空定义尚未实现的权限、执行或幂等语义。

参考实现只作为事实边界和故障模式样本，不作为 EasyCode 的存储协议：

- 对 `claude-code-sourcemap` v2.1.88 的只读检查表明，其 transcript 把 `thinking`、`text`、`tool_use` 保存为 assistant message content blocks，把配对 `tool_result` 保存为后续 user message；MCP 复用相同 pair 并附带不发给模型的 metadata，高频 progress 不再进入 transcript，大结果可外置为 tool-result 文件。它使用 UUID/parentUuid 消息树、100ms 延迟 append 队列并跳过任意坏行，还会在恢复时删除未配对 tool call 或插入 synthetic error result。该机制保证下一次 Anthropic 请求大多可接受，但不能证明工具副作用是否发生，也不提供统一 seq、batch、checksum 或 Sync 确认边界。
- 对 `codex` `1cc7e2361237` 的只读检查表明，其 rollout 以 `timestamp/ordinal/type/payload` 保存 `response_item`、turn/context/usage/world-state、compaction、inter-agent 和筛选后的 `event_msg`。Responses reasoning、function/custom tool call/output 都是原生 response items，MCP attribution 是 host metadata；delta、approval、MCP begin/progress 等大量事件明确不持久化。它在调度 tool future 前等待 rollout append/flush，但先修改内存 history，文件只执行 flush 而非 `sync_all`，持久化错误也不会阻止工具继续执行；加载器会跳过坏行、允许 ordinal gap，并为缺失 tool output 合成稳定的 `aborted` 结果。

两者共同证明：Provider 原生 thinking/tool pair 必须无损保留，宿主 metadata 与原生 wire 应分层，UI progress 不应污染事实源；也共同暴露出“让下一次请求格式合法”与“证明副作用不会重复”是两个不同问题。因此本设计保留其 native-history 边界和调用顺序意图，不复制消息树、宽松坏行跳过、synthetic tool repair 或缺少副作用 ledger 的恢复方式。

## Goals / Non-Goals

**Goals:**

- 固定一套可版本化、严格校验、只追加的 JSONL envelope 和单 writer durable batch 机制，使记录词汇可以独立演进。
- 分离字节/批次校验、Session lifecycle 语义回放与 Provider-native codec 校验，使 checksum 合法但状态转换非法的 journal 也不能进入可调用 Runtime。
- 把 Provider stream 的“完整 sample 已准备好”与“提交到内存历史”拆开，在成功终态对宿主可见前先持久化相同的原生历史增量；公共提交形状同时允许未来在下一次采样前提交 input-only tool result。
- 以事务式全量校验恢复双 Provider 原生历史，并保证恢复前后下一请求的 native 顺序、canonical bytes 和 cache fingerprint 一致。
- 让 root thread 的创建、显式恢复、语义 transcript 回放、取消和 shutdown 都通过明确的应用装配边界完成。
- 保存足以重建 Session cwd 索引、但不隐式改变进程 cwd 或提前承担工具 workspace 权限语义的创建元数据。
- 明确未来工具循环的落盘顺序和归属：Provider wire 事实、共享执行事实、瞬态 UI progress、大结果 artifact 分别处理。

**Non-Goals:**

- 不把 Session payload 设计成跨 Provider 的统一 Message，也不让 Session package 导入具体 Provider 类型。
- 不在当前 change 中实现工具、MCP、permission、artifact store 或其 record payload；只保证它们无需替换日志内核即可加入。
- 不保证一个 turn 只有一次 Provider sample，不用 turn 作为 Provider commit 的序列化单元。
- 不实现 SQLite、session picker、最近会话发现、跨 Provider/model fork、headless 输出或失败 turn 的完整 transcript 回放。

## Decisions

### 1. 日志信封与记录词汇分层版本化

`internal/session` 将拆成边界清晰的组件：

- `Repository`：在受限数据根内创建/定位 thread journal，校验 ID、路径、类型和权限。
- `Loader`：有界读取、严格校验 bytes/envelope/batch、尾部修复并返回通用 records 与 repair report；不解释 turn 或 Provider 语义。
- `ReplayPlanner`：消费 Loader 的完整结果，按 registry 校验 metadata cardinality、turn lifecycle 和 record placement，输出 Provider commits、可忽略 records 与可选 interrupted-tail 的不可变恢复计划。
- `JournalWriter`：单 owner goroutine 持有文件和下一 seq，串行处理 append batch、Sync 与 close。
- `RecordCodec`：负责 envelope 的确定性编码、checksum 和各 Session payload revision 的强类型 registry。

每行 envelope v1 包含：

- `schema_version`：公共 envelope 版本。
- `payload_version`、`event_kind` 与 `replay_requirement`：该记录 payload 的独立版本、种类，以及未知消费者能否安全忽略它；v1 只允许 `required`/`optional`。
- `seq`、UTC `timestamp`、`session_id`、`thread_id`，以及按语义出现的 `parent_thread_id`、`turn_id`。
- `batch_id`、`batch_index`、`batch_size`：定义逻辑 batch 边界；单记录也是大小为 1 的 batch。
- 有界 `payload` 和 `checksum`。

`batch_id` 由 writer 根据该 batch 的首个 seq 确定，不引入新的随机状态。`checksum` 使用 SHA-256，覆盖移除 checksum 后的强类型 canonical envelope bytes，包括 opaque payload 的持久化表示；编码仍统一经过工程 `codec.MarshalStable`。checksum 用于发现可解析但 bytes 已改变的记录，不承担对抗恶意本地用户的签名职责。

首版 record kind 只有 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`turn_completed`、`turn_failed`，且全部是 `required`。registry descriptor 固定 kind、payload revision、replay requirement、codec、cardinality 和允许的 lifecycle 位置；已知记录的 wire 声明与 descriptor 不一致即属于 corruption。未知 `required` kind/revision 拒绝恢复；未知 `optional` 记录只在 envelope、大小和 checksum 合法时以有界 opaque payload 保留并由 ReplayPlanner 忽略。

v1 envelope 采用严格字段集合：token scan 先拒绝重复顶层 member、未知顶层 member、尾随 JSON value 和非法 UTF-8，再解码强类型结构并重算 checksum。这样不会让 Sonic 的重复字段覆盖或默认忽略行为决定事实源语义。公共 envelope 新字段必须升级 `schema_version`；Provider-owned payload 的内部未知字段仍由相应 codec 有界保留和解释。

未选择“直接序列化全部 RuntimeEvent”，因为 text delta、progress 和 UI 状态既高频又不能重建 Provider 历史。也未选择“一个 JSON 对象保存整个 turn”，因为工具循环会让一个 turn 包含多次 sample 和中间副作用。

### 2. Batch 提供恢复原子性，Sync 提供确认边界

调用方提交的是一组未分配 seq 的强类型 record drafts。`JournalWriter` 的 owner goroutine依次完成：

1. 校验 writer 状态和 batch 总大小。
2. 分配连续 seq、timestamp 和 batch fields。
3. 编码所有完整 JSON 行到单个有界 buffer。
4. 完整写入 buffer，并对文件执行 `Sync`。
5. 只有 Sync 成功才回复 durable success 并推进可确认的下一 seq。

文件首次创建时还要同步父目录（平台支持时），使目录项创建进入持久边界。writer 请求 channel 有明确 owner，`Close` 停止接收、处理已接收请求、Sync、关闭文件并等待 goroutine；不在锁内执行磁盘 I/O。任何短写、append 或 Sync 错误都会把 writer 标为 poisoned，所有后续 append 直接失败。系统不会猜测该 batch 是否落盘，而是要求重启后由 Loader 根据 bytes 决定。

“batch 原子”是恢复语义，不是假设文件系统提供多行物理事务。Loader 保存每个 batch 起始 offset，只有 batch ID、index、size、seq 和 checksum 全部闭合才把它加入已提交结果。EOF 处的半行或未闭合 batch 可以截断到该 batch 起点；同样问题出现在文件中段、已闭合 batch 或后面仍有 bytes 时则属于 corruption，不能自动跳过。修复会先截断、Sync 文件，再返回不包含 payload 正文的结构化 report；第二次加载不再产生相同 repair。

完整 committed batch 即使只包含一个尚未终结的 `turn_started` 也不能由 Loader 截断，因为它是合法写入的业务事实而非 torn write。Loader 返回它，ReplayPlanner 再把仅位于 EOF 的单个活动 turn 标记为 interrupted-tail；checksum 合法但 record placement 或 lifecycle 非法的其他情况直接拒绝，不借用 repair 删除。

首版限制定为单行 canonical bytes 不超过 16 MiB、单 batch 不超过 64 MiB、单文件读取通过有界 reader 完成。限制是 codec 常量并进入 boundary tests；未来大工具结果必须转入 artifact，不通过无限提高 JSONL 上限解决。

未采用“每次 Append 都启动 goroutine”或“用进程级全局 mutex”，前者没有可证明的关闭路径，后者会让互不相关的 thread 相互阻塞并形成全局可变状态。

### 3. ID 和路径由 Session Repository 管理

新建会话生成独立 UUIDv7 `session_id` 与 root `thread_id`，turn 同样使用 UUIDv7。root thread 的 journal 位于注入的数据根下 `YYYY/MM/DD/<thread-id>.jsonl`；日期由 thread UUIDv7 的时间部分确定，所以 `--resume <thread-id>` 无需 SQLite 或全目录扫描即可定位。默认数据根只在应用装配层通过 `os.UserHomeDir` 计算，测试始终显式注入临时根目录。

Go 1.24 的受限 filesystem root 能力用于将所有相对路径限制在数据根内；每个 ID 在拼接前按 UUIDv7 canonical form 校验。Repository 拒绝 symlink、非普通文件、越界路径和已有的宽松文件权限。支持 Unix 权限位的平台创建目录使用 `0700`、文件使用 `0600`，不会通过静默 chmod 掩盖不安全的既有文件。

创建 root thread 的第一个 batch 同时包含 `session_meta` 和 `thread_meta`。metadata 只记录 root/parent 关系、创建时间、Provider family、wire、model、revision 和应用装配层取得的规范化绝对 `creation_cwd`，不记录 base URL、API key、header 或配置路径。Session path、ID、timestamp 和 creation cwd 都不进入 Provider request 或 cache fingerprint。

`creation_cwd` 用于未来从 JSONL 重建 cwd/project 索引，不具备 sandbox capability，也不触发自动 `chdir`。本次显式 resume 即使从不同 cwd 发起也只按 family/wire/model 判断 Provider 兼容性；P3 在引入文件/命令副作用前另行定义 canonical workspace root、symlink containment、目录移动和用户 override 语义，不能把这个展示/索引字段直接当作授权边界。

未采用 thread ID 到路径的旁路索引文件；那会在 SQLite 尚未实现时引入第二事实源和一致性窗口。未来 SQLite 只从这些 metadata 重建定位/列表投影。

### 4. Provider 返回 prepared sample，Runtime 决定 durable commit

共享 Provider 边界增加受控 `NativeCommitEnvelope`，只暴露 `family`、`wire`、`payload_version` 和克隆后的 `json.RawMessage`。它代表一个有序 native history 增量，不承诺每个增量都同时包含输入和输出。OpenAI/Anthropic 分别拥有 payload 的强类型 codec；Session、Runtime 和 TUI 只能复制公共 envelope，不能解释 payload。

Conversation 的流式生命周期改为两阶段：

1. reducer 在 Provider 成功终态到达时完成 staging，校验原生不变量并生成 `PreparedSample`。此时不修改 Conversation history。
2. completed terminal 携带 prepared sample。Runtime 把它编码为 `provider_native_commit`，与相应边界组成 batch 并 durable append。
3. durable success 后，Runtime 调用 prepared sample 的一次性内存 finalizer；finalizer 只克隆并追加此前已校验的 staging，不执行 I/O、不重新解码、没有可恢复失败分支。
4. 内存历史提交完成后，Runtime 才发布成功终态。

`PreparedSample` 由共享 provider package 封装 envelope 和一次性 finalizer，避免 Runtime 获取具体 wire 类型或自行构造 staging。重复 finalization 在测试中被视为协议错误；Runtime 每个 sample 只能消费一次 completed terminal。Provider 失败、取消、timeout、提前 EOF 都丢弃 staging。

OpenAI payload v1 的 commit shape 是 `text_sample`，保存当前输入 message item 与按 Responses wire 顺序排列的完整 output items；Anthropic payload v1 同样是 `text_sample`，保存当前 user message、完整 assistant message和最终 metadata/usage known 状态。commit shape 位于 Provider-owned payload 内而非公共 envelope。OpenAI reasoning summary/encrypted content/phase 与 Anthropic thinking/signature/redacted thinking 已随这些 native items 持久化，不额外复制成 Session thinking event；流式 reasoning delta 仍是 RuntimeEvent。

两者的 raw/unknown JSON 都进行深拷贝和有界校验。Factory restore 接缝只接受 ReplayPlanner 产出的有序 native envelopes：先全部解码到临时 native history、校验 family/wire/revision/shape/角色和边界，全部成功后才构造 Conversation，绝不逐条修改一个可调用的半恢复对象。Provider codec 不通过插入 synthetic tool result、删除原生 item 或从 RuntimeEvent 推断缺失内容来修复非法历史；未来工具副作用是否可重放只能由独立 ledger records 决定。

未采用“Provider 先提交内存、Session 随后抓 snapshot”，因为磁盘失败后当前进程的下一请求会包含从未 durable 的 history。也未采用“从 SemanticHistoryView 重建”，因为它故意丢失 signature、encrypted reasoning、phase、unknown item 和 usage 状态。

### 5. Turn lifecycle 与 sample lifecycle 分离

ReplayPlanner 对当前文本 revisions 使用确定状态机：首个 batch 恰好是 `session_meta + thread_meta`；`turn_started` 打开唯一活动 turn；`provider_native_commit` 只能属于活动 turn；成功必须以同 batch、固定顺序的 `[provider_native_commit, turn_completed]` 关闭；失败以唯一 `turn_failed` 关闭且该 turn 没有 native commit。terminal 后才能开始下一 turn。唯一例外是 EOF 处完整 committed `turn_started`，它产生 interrupted-tail plan；其他重复 metadata、turn 外 commit、重复 terminal、成功 batch 缺项或活动 turn 后出现新 turn 都是不可修复的语义 corruption。未来工具 revisions 可注册包含多个 sample/tool states 的扩展转换，但不能改变旧 revision 的规则。

`Runtime` 构造时获得稳定 session/thread identity、turn ID generator 和该 thread 的窄 `Journal` 接口。每次 `RunTurn` 的顺序为：

1. 生成 turn ID，durable append `turn_started`；失败则不调用 Provider。
2. 发布带 session/thread/turn ID 的 `turn_started`，启动 Provider stream。
3. 转发语义事件前由 Runtime 覆盖/补齐相同 identity；Provider payload 无权指定 Session identity。
4. Provider 成功时 durable append `[provider_native_commit, turn_completed]` batch，finalize 内存 sample，再发布 `turn_completed`。
5. Provider 失败或取消时 durable append `turn_failed`，不提交 staging，再发布唯一的 `turn_failed`。

如果完成 batch 写入状态不确定，writer 与 Runtime 都进入 poisoned 状态：UI 得到一次 session 类失败，本进程拒绝下一 turn，重启后严格 load/repair 决定事实。若连失败边界也无法写入，不尝试在同一个 poisoned writer 上追加第二次；RuntimeEvent 可以报告失败，但不能声称它已 durable。

当前文本 turn 恰好只有一个 sample，但 contract 不编码该假设。未来工具循环按以下顺序扩展：

1. Provider 返回含原生 tool call 的 prepared sample；先 durable `provider_native_commit`，再使该增量进入内存 native history。
2. 后续 change durable 记录 `tool_call_ready`、权限决策和幂等状态后，才允许 Executor 产生副作用。
3. 工具结果与 artifact 引用 durable 后，由 Provider ToolWireCodec 编成 input-only 原生增量；该增量在下一次网络请求前再次 durable `provider_native_commit` 并进入内存 history。
4. 下一次 prepared sample 只提交相对已有 history 新增的模型输出，不重复已提交 tool-result input。
5. 只有模型给出最终结束条件时才写 `turn_completed`。

因此 Provider 的 `tool_use/tool_result` 属于 native commit，权限和副作用事实属于共享工具 records；二者通过稳定 call ID 配对但不互相替代。MCP progress 默认只作为 RuntimeEvent，不能因 UI 更新频率污染 journal。这个顺序与参考实现“先记录 tool use 再执行”的安全意图一致，同时补足其没有独立幂等 ledger 的部分。

后续工具 change 必须区分“Provider 请求格式可修复”和“副作用结果可证明”：只有 native tool call 而没有 execution-ready/ledger 事实时不得执行；执行状态在崩溃后不确定时不得自动重放非幂等工具；结果与 artifact 引用已 durable 时不得再次执行，而应补齐或复用对应 input-only native commit。Claude Code/Codex 式删除 call 或合成 `aborted` output 可以作为请求投影策略讨论，但不能替代这些持久化决策。具体 ledger payload、租约和人工处置状态仍由 P3 change 定义。

### 6. Resume 在应用进入 TUI 前完成

应用装配新增 Session service，负责两条互斥路径：

- 无 `--resume`：验证配置，创建 metadata batch、新空 Provider Conversation、JournalWriter 和带 identity 的 Runtime。
- 有 `--resume <thread-id>`：按以下顺序完成恢复，任一步失败都不进入 TUI 或调用 Provider 网络：
    1. 定位 journal 并由 Loader 完整读取、严格校验和执行允许的尾部 repair。
    2. 由 ReplayPlanner 验证 metadata cardinality 与 lifecycle，得到有序 native commits、optional records 和可选 interrupted-tail。
    3. 校验 root thread 与当前 family/wire/model；配置不匹配在任何补偿写入前失败。
    4. Provider factory 在临时状态中事务式解码 ReplayPlan 的 native commits。
    5. 从最大 seq 后打开 writer；若计划包含当前文本 interrupted-tail，先以原 turn ID durable 追加 required `turn_failed(code=session_interrupted)` 并 Sync。
    6. 只有补偿成功或无需补偿时，才发布恢复后的 Conversation、构造 Runtime 并向 TUI 提供语义投影。

base URL 与 API key 始终来自当前配置；model/family/wire 必须与 metadata 完全一致。本切片不尝试同 family 换模型或跨 Provider 转换。任何损坏、不支持记录或配置不匹配都发生在 TUI 启动和网络请求之前。

interrupted-tail 补偿只关闭 durable lifecycle，不把失败 turn 输入加入 native history、不自动重发用户请求，也不调用 Provider。若 append/Sync 失败，writer 进入 poisoned 状态且 resume 整体失败；下次进程重启重新从 journal bytes 判断是否已完整落盘，因此重复恢复仍具备幂等结果。

恢复完成后应用调用 `HistoryProjector` 得到独立 `SemanticHistoryView`，把已完成 turn 的可见 user/assistant 文本作为初始 projection 传给 TUI。TUI 构造器只接收共享语义数据，不读文件、不解析 Provider payload，也不在 render/getter 中执行 I/O。live 事件继续走现有 ChatSession facade。

关闭顺序是：ChatSession 拒绝新 turn并取消/等待 active turn，JournalWriter Sync/close，最后关闭 Provider transport。这样不会在仍可能产生日志请求时提前关闭 writer。

### 7. 版本演进只追加 record、codec 和 checkpoint

首版没有需要迁移的既有 on-disk Session，当前 `internal/session.Record` 也从未被实现或发布，因此可以直接替换占位字段（例如 `kind` 改为明确的 `event_kind` 并加入 batch/checksum）。一旦 v1 写入，后续版本遵守以下规则：

- 兼容的新 record kind 增加强类型 registry descriptor、独立 payload codec 和 golden/migration fixture，不回写旧行。只有省略后不改变 Provider request、lifecycle 或任何副作用的记录才能声明为 `optional`；其余记录必须为 `required`。
- 修正 metadata 或派生状态使用新记录表达 last-wins/compensating 语义，不采用 Claude Code 式 tombstone、任意位置删除或整文件重写。
- Provider 新增 native tool item 时优先升级对应 Provider payload revision；旧 decoder 看到未知 required revision 时安全拒绝。
- 大结果通过受限 artifact 文件及 hash/size/reference record 扩展，不把二进制或无限文本塞入 JSONL。
- 若公共 envelope 必须升级，Loader 保留显式版本 dispatch；不通过字段猜测或宽松默认值解释旧数据。

未来 compaction 采用 required replay checkpoint：payload 必须指明覆盖到的 source seq，并包含对应 Provider 可直接恢复的 replacement native history 和完整性信息。恢复选择最新受支持且校验通过的 checkpoint 加后缀 records，旧事实仍保留在 journal 中；checkpoint 不能依赖从 `SemanticHistoryView` 反向构造 native history。未来 fork/subagent 的 child `thread_meta` 通过新的 payload revision 引用不可变 parent cursor（至少 parent thread、exclusive seq 与完整性锚点），child 使用自己的 seq 空间，不复制或转换 parent opaque reasoning。这些 payload 不在本 change 实现，但现有 envelope、batch 与 registry 足以承载。

该策略承认未来会发生“schema/记录词汇变化”，但把变化限制在版本化 payload、descriptor 和 decoder registry，JSONL 的单 writer、seq、batch、Sync、repair、权限与路径机制保持稳定。

### 8. 验证按边界分层

实现阶段需要以下测试，而不是只验证 happy path：

- Session codec/writer 单测：canonical bytes/checksum、重复/未知顶层 member、尾随 JSON、required/optional registry 一致性、连续 seq、并发 append 串行化、短写/Sync fault、poison、行/batch 大小、权限、UUID/path traversal、symlink、close/shutdown。
- Loader/ReplayPlanner fixture：合法 v1、多 batch、尾部半行、尾部完整行但 batch 未闭合、中段坏 JSON、checksum 变化、seq 跳变、ID 漂移、未知 required/optional revision、重复 metadata、turn 外 commit、重复 terminal、完成 batch 缺项和 committed interrupted-tail；验证仅 torn tail 可修复、语义错误不截断且第二次加载幂等。
- Provider round-trip/golden：thinking/signature/redacted thinking、reasoning/encrypted content/phase、unknown raw、usage unknown；恢复后下一请求 canonical bytes 与 cache fingerprint 和不中断路径完全一致。
- Runtime fault-injection：`turn_started` 写失败时零网络调用，完成 batch 写失败时不 finalize、单一失败终态并禁止下一 turn，取消/EOF 不产生 native commit，cancel/completed race 仍只有一个终态。
- 应用/TUI 集成：新建、多轮、重启显式 resume、无 resume 新会话、不同 cwd 恢复但不自动 chdir、配置不匹配时 journal 不变、interrupted-tail 补偿成功/Sync 失败、repair 摘要、初始 transcript snapshot、secret 扫描。
- race test：writer actor、ChatSession shutdown、Provider finalizer 与取消交错。

Provider request golden 和 cache regression 必须证明 session path、timestamp、IDs、creation cwd 和 repair report 不进入稳定请求 bytes。Apply 完成前执行全量 `make verify`。

## Risks / Trade-offs

- [每个关键 batch 执行 Sync 会增加延迟] → 首版优先正确性，只把生命周期边界而非流式 delta 写入 journal；保留 batch 接口，后续必须以基准和崩溃语义证明后才能调整策略。
- [JSONL batch 不是文件系统物理事务] → 每行携带显式 batch 边界与 checksum，Loader 只承认闭合 batch并只修复尾部。
- [单条 Provider commit 可能较大] → 设置行/batch 上限并在 Provider codec 前校验；未来工具大结果强制 artifact 化，而不是无限增大内存 buffer。
- [成功响应已从网络到达但磁盘失败，用户看到失败且该回复不会进入当前历史] → poison 当前 Runtime 并拒绝继续；重启由 journal 唯一决定是否 batch 完整，避免臆测和分叉。
- [较新程序写入未知 required record 后旧程序无法 resume] → 旧程序安全拒绝未知 revision；每次演进提供 migration fixture，不静默忽略。
- [新 record 被错误标为 optional，旧程序忽略后改变恢复行为] → registry review 和测试必须证明省略它不改变 Provider request、lifecycle、权限或副作用；无法证明时一律标记 required。
- [显式 resume 为 interrupted-tail 追加补偿记录会改变文件] → 只在全量校验、配置兼容和 Provider 事务恢复成功后写入，使用原 turn ID 和固定 code；Sync 不确定即 poison 并由下次加载重新判定。
- [creation cwd 失效、移动或指向 symlink] → 本切片只将其用于索引/展示，不自动 chdir、不用于路径授权；P3 另行定义 workspace capability。
- [UUIDv7 时间决定日期路径会受异常系统时钟影响] → 路径只要求与 ID 自身时间一致，不依赖当前日期；创建和恢复使用同一纯函数并覆盖跨日/异常时间 fixture。
- [本次未实现工具 ledger，不能验证完整崩溃后副作用幂等] → 明确禁止当前 Tool 副作用；P3 必须在执行任何工具前新增 ledger records，并复用本设计的 durable ordering。

## Migration Plan

1. 先实现 Session codec/loader/repository/writer、强类型 registry、ReplayPlanner 及 fault-injection tests，不接入 Provider。
2. 为两个 Provider 增加 prepared sample、native commit codec 和 restore factory，使用 round-trip/request golden 验证；此阶段仍可由测试中的内存 journal 驱动。
3. 接入 Runtime identity 与 durable lifecycle，删除 Provider stream 内部直接 commit 的旧路径，并更新所有 fake Conversation。
4. 接入 app create/resume/interrupted-tail compensation/close 与 CLI 参数，再让 TUI 接收初始语义 projection。
5. 执行双 Provider 多轮/重启/损坏/取消集成、secret 扫描、race 和 `make verify`，同步 roadmap/pitfall 文档后再归档 change。

当前没有已发布 Session 文件需要原地迁移。回滚到旧二进制时，旧版本不会读取这些文件；回滚不得删除或重写已生成 journal。若实现阶段发现 envelope 或提交顺序无法满足测试，应先更新本 change 的 artifacts，而不是加入永久兼容分支。
