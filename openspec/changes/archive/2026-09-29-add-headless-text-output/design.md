## Context

参见 [proposal.md](proposal.md) 的动机。当前 `cmd/easycode` 已注册 `--print`，但 `app.Run` 在加载配置前直接返回 `not_implemented`；未消费的位置参数也没有输入语义。应用装配已经能创建或恢复一个绑定 Session/thread identity、exclusive journal lease、Provider Conversation 和 `runtime.ChatSession` 的资源组。

Runtime 已保证以下顺序：`turn_started` 在网络请求前 durable 写入；Provider 成功 sample 的 native commit 和 `turn_completed` 在同一 batch `Sync` 后才 finalize 到内存并发布成功事件；失败/取消发布 typed `turn_failed`。这条链必须继续作为 headless 成功与恢复事实的唯一依据，headless 不能直接读取 Provider stream 或自行提交 Session records。

内部 `protocol.Event` 包含 timestamp、opaque `json.RawMessage` payload 和多个尚未实现的 kind。它是进程内宿主协议，不适合作为外部 JSONL schema。现有 `ChatSession.Shutdown` 还会在每次等待时创建 helper goroutine，稳定 JSON codec 由可变包级变量持有；新宿主依赖这些边界前应同步收紧。

Claude Code 参考实现说明文本模式应等待最终 result，流式 JSON 必须保持 stdout 纯净且由单 writer 输出；Codex exec 进一步证明内部通知应先投影到独立、有限的外部事件 union，最终答案和诊断应分离到 stdout/stderr。EasyCode 采用这两个原则，但不引入 Claude 的双向控制协议或 Codex 的完整 item 模型。

## Goals / Non-Goals

**Goals:**

- 以单个可复用 headless 宿主消费现有 `ChatSession`，支持新 Session 和显式 resume 的一次 turn。
- 在任何 Session 或 Provider 副作用前完成 CLI 结构和 prompt 校验。
- 让文本成功输出严格晚于 durable `turn_completed`，让 JSONL 实时输出具有独立、版本化、可测试的 schema。
- 维持 stdout 单一 owner、stderr 诊断和稳定退出状态，覆盖取消、短写、断管、协议错误及关闭竞态。
- 保持 Provider-native history、RequestCompiler、JSONL Session schema 和 cache fingerprint 不变。

**Non-Goals:**

- 不设计 stdin JSON 消息、运行中追加 prompt、permission control response 或其他双向 SDK transport。
- 不增加 usage、reasoning、tool、patch、MCP、hook、subagent 或结构化模型输出事件。
- 不实现 `--continue`、session picker、SQLite 索引、fork 或 compaction。
- 不让 headless 读取或渲染旧 transcript，也不改变 TUI 的 live/history 投影。
- 不为信号分别承诺 130/143；当前稳定外部分类只有成功、运行失败和用法错误。

## Decisions

### 1. CLI 在进入 app 装配前解析完整 prompt

`cmd/easycode` 增加显式输出模式枚举，避免 `Headless bool + JSON bool` 产生非法组合。flag parser 完成后，入口最多接受一个位置参数，并在任何 `config.Load`、工作目录读取、Session 创建或 resume 之前调用纯输入解析器。

输入解析器接收：位置参数、`io.Reader`、由真实入口探测并由测试显式注入的 `stdinIsTerminal`。真实入口使用仓库已经间接依赖的跨平台 terminal helper；该 module 只从 indirect 提升为 direct，不引入新的 module。测试不读取真实 fd。

stdin 使用 `io.LimitedReader` 读取最多“剩余组合预算 + 1”字节。普通位置参数先扣除 UTF-8 字节、固定分隔符和闭合标签所需预算；参数为 `-` 时强制读取 stdin，不受 TTY 判断影响。读取后先验证 UTF-8 和上限，再检查 `TrimSpace` 后是否为空；传给 Provider 的内容保留原始非外围结构，由现有 Provider 输入校验继续拒绝无意义空白。

当显式 prompt 与管道 stdin 同时存在时使用固定边界：

```text
<prompt>

<stdin>
<stdin bytes>
</stdin>
```

若 stdin 自带尾部换行则不重复添加。这一选择保留 Codex 的输入来源边界，比无标记拼接更不易让附加文件内容伪装成用户指令。`<stdin>` 只是当前纯文本 prompt 的稳定分隔，不声明 XML 解析语义。

替代方案：

- 像 Claude Code 一样自动等待任意继承 pipe 并以普通换行拼接。拒绝，因为可能挂在未关闭的父进程 pipe 上，且丢失内容来源边界。
- 只在参数缺失时读取 stdin。拒绝，因为脚本常需要短指令加长文件内容，显式的附加边界可以安全支持该场景。
- 在 app 内解析输入。拒绝，因为配置和 Session 资源可能在无效输入前已经产生副作用。

### 2. `internal/headless` 是与 TUI 平级的宿主

新增 `internal/headless`，依赖 `domain`、`fault`、`protocol` 以及一个最小会话接口：

```go
type ChatSession interface {
    Submit(string) (<-chan protocol.Event, error)
    Interrupt()
}
```

宿主接收已解析 prompt、Session identity、是否 resume、输出模式和 writer，不创建 Provider、Journal 或 Runtime。`internal/app` 继续是唯一装配层：资源创建成功后，TUI 分支构造 Bubble Tea model，headless 分支调用 runner。恢复得到的 `SemanticHistoryView` 只传给 TUI，headless 不消费它。

runner 在调用 `Submit` 前输出 JSON `thread.started`，随后以单 goroutine 顺序消费事件、校验状态并写输出。除 Provider/Runtime 已有 worker 外，headless 不额外建立异步输出队列；下游 writer 的速度直接形成背压，避免无界缓冲和事件重排。

替代方案：

- 把 headless 分支直接写进 `app.Run`。拒绝，因为输入投影、协议状态机和输出测试会继续膨胀装配层。
- 复用 TUI model 后抓取 transcript。拒绝，因为会把终端渲染状态当业务结果，并绕过机器协议校验。
- 直接消费 Provider stream。拒绝，因为会复制 Runtime terminal/durability 逻辑并破坏双 Provider 边界。

### 3. app outcome 显式区分退出状态、已报告终态和 stdout 失效

当前 `app.Run error` 无法表达“JSON `turn.failed` 已经输出，但进程应返回 1”；若继续只返回 error，`cmd` 会再打印一遍纯文本并污染 stdout/stderr 契约。将顶层调用结果改为强类型 outcome，至少包含：

- exit class：success、runtime failure、usage failure；
- failure report state：尚未报告、已经由文本/JSON 宿主报告、stdout 已失效；
- 可选的安全 fault code/message，不携带底层 cause 到 renderer。

CLI 解析和 prompt 结构错误由 `cmd` 直接写 stderr 并返回 2。配置、Session 等发生在 headless runner 前的错误由 `cmd` 使用同一个 headless JSON encoder 输出单个 `error`，文本模式写 stderr。runner 收到 `turn.failed` 时已经输出终态，只返回非零 outcome，不再把它作为待渲染 error。stdout 写失败后只向 stderr 报告，禁止再次尝试写机器事件。

新增统一的 public failure projector，从 `fault.Error` 只提取 `Code` 和 `Message`；未知错误映射为通用稳定摘要。任何 renderer 都不得调用可能展开 `Cause` 的完整 `Error()` 作为外部内容。

替代方案：

- 使用 `reportedError` sentinel wrapper。拒绝，因为输出通道失效、terminal 已报告和安全错误摘要会被压进一个脆弱的 errors.As 分支。
- 所有错误都只写 stderr。拒绝，因为 JSON 消费者无法从 stdout 机器流识别启动失败。
- 让 app 直接调用 `os.Exit`。拒绝，因为会跳过 defer/cleanup 并使集成测试困难。

### 4. JSONL v1 使用封闭的强类型外部 union

`internal/headless` 定义独立 event type 和每种事件的具体 payload/constructor。外部名称使用点分层级，字段直接放在对象顶层，避免把当前内部 `kind + RawMessage payload` 固化为公共格式。所有已建立 turn 的事件都携带 session/thread/turn ID；不输出 timestamp 或 seq，因为一次 stdout pipe 已提供顺序，且本切片没有 reconnect/replay 协议。

示例：

```json
{"version":1,"type":"thread.started","session_id":"...","thread_id":"...","resumed":false}
{"version":1,"type":"turn.started","session_id":"...","thread_id":"...","turn_id":"..."}
{"version":1,"type":"assistant.text.delta","session_id":"...","thread_id":"...","turn_id":"...","text":"hel"}
{"version":1,"type":"turn.completed","session_id":"...","thread_id":"...","turn_id":"..."}
```

encoder 先把完整 object marshal 到独立 byte slice，验证单行 framing，再使用一个 `writeFull` 循环写入 bytes 和尾部换行。这样 short write 在开始下一条事件前被识别；已经由操作系统接收的当前行前缀无法撤回，因此写失败后 stdout 被永久标记为不可用，只做取消和 stderr 诊断。Sonic 标准兼容配置继续统一编码，golden 专门验证换行、U+2028/U+2029 和控制字符不会打断行。

内部事件 projector 是严格状态机：只接受 `turn_started`、`assistant_text_delta`、`turn_completed`、`turn_failed`，逐项验证版本、identity、顺序和 typed payload。尚未实现的内部 kind 不做“忽略”，而是协议失败；同时从内部 `EventKind` 删除当前没有 producer/consumer/validator 的预留常量，未来能力在同一 change 中增加 producer、consumer 和测试。

`error` 是 stream 级终止事件，用于 thread/turn 建立前失败，或 turn 事件流本身无法继续解释的本地协议故障。正常 Runtime 失败始终投影为 `turn.failed`。若 turn 已开始后出现 stream 级 `error`，它终止整个 JSON 流但不伪造 Session 中的 durable turn terminal；cleanup 仍等待 Runtime 写入真实失败边界。

替代方案：

- 直接 marshal `protocol.Event`。拒绝，因为会暴露 timestamp、内部命名、opaque payload 和未实现 kind，之后无法独立演进。
- 模仿 Claude 安装全局 stdout guard。拒绝，因为 Go 入口可以结构化地只把 stdout writer 交给 headless owner，全局 monkey patch 反而隐藏所有权错误。
- 一开始设计 Codex 风格完整 item union。拒绝，因为当前只有文本 delta，没有可验证的 item/tool/usage producer。

### 5. 文本模式以 terminal 为提交闸门

文本 runner 将已验证的 delta 追加到局部 `strings.Builder`，不在流中写 stdout。只有收到身份匹配的 `turn.completed` 后才一次性写最终文本和至多一个尾部换行。收到 `turn.failed`、stream-level error 或 channel 提前关闭时丢弃 builder；这使 shell 重定向不会把部分输出误当成功结果。

JSON 模式为了实时消费按事件写出 delta，但 `turn.completed` 仍只来自 Runtime durable terminal。两种模式都不从 channel close、已见文本或 Provider stop hint推断成功。

文本聚合会占用与本次可见 assistant 输出同量级的内存，这是“成功前不发布部分结果”的明确代价。当前 Provider 输出受模型和 transport 既有边界约束；若未来需要超大输出 artifact 或边流边提交，应由独立协议 change 设计，不能在本切片静默截断模型答案。

### 6. 取消、事件关闭和 cleanup 保持唯一 owner

runner 同时观察根 context 和 RuntimeEvent channel。context 首次取消时调用幂等 `Interrupt`，但不立即返回；它继续等待 Runtime 唯一 terminal 或 stream 关闭。若 durable completed 已经胜出，即使 context 随后可读仍报告成功；否则使用 Runtime 发布的取消失败终态。channel 在 terminal 前关闭映射为安全 `stream_protocol_error`。

`ChatSession` 将每个活动 turn 的 owner-held `done` channel 作为完成信号：创建 turn 时创建，`runTurn` 唯一关闭，任意数量的 Shutdown caller 只 select 同一信号。移除“每次 Shutdown 启动 goroutine 等 WaitGroup”的模式。锁内仍只做状态转换和复制 cancel/done，不等待 channel、Sync 或 Close。

应用关闭顺序保持：请求 ChatSession shutdown并等待 Runtime durable 收口，然后关闭 journal/lease，再关闭 Repository 和 Provider。正常关闭使用现有超时；超时不能让 `Run` 丢失资源所有权，装配层必须进入明确的强制取消/transport close 升级路径并等待最终完成信号，再释放 writer。故障注入测试用 channel 控制阻塞与释放，不用 sleep 证明正确性。

输出 writer 失败发生在活动 turn 时也走同一 Interrupt + drain 路径。输出失败不会改写已经 durable completed 的 Session；发生在成功前则由 Runtime 记录取消/失败边界，部分 delta 不进入 native history。

### 7. Session 和 Provider 语义保持不变

prompt 校验成功后，headless 复用 `newChatResources`：无 resume 时创建 root journal；resume 时在读取/repair 前获取 lease，事务式恢复全部 Provider commits，必要时先 durable 收口 interrupted tail，再创建 Runtime。`resumed` 只来自 CLI 是否显式提供有效 resume，不从历史长度猜测。

headless 不使用 `resources.history`，因此不会输出旧 transcript；新 prompt 仍由恢复后的 Conversation 编译，成功后 records 追加到同一个 journal。不会为 headless 新增 Session event kind、保存输出协议行或改变 native commit payload。双 Provider e2e 必须继续比较 uninterrupted 与 restored 的 canonical request bytes、原生 item 顺序和 fingerprint。

### 8. 内部协议和 codec 只做本切片所需的收紧

删除当前没有真实 producer/consumer 的预留 RuntimeEvent kind，保留文本切片实际使用的 `turn_started`、`assistant_text_delta`、`turn_completed`、`turn_failed`。移除导出的 `NewEventWithPayload(any)`；每个保留 kind 使用具体 constructor、strict decoder 和 validator。通用 marshal helper可以保持在 codec 边界，但核心协议导出 API 不接收或返回 `any`。

`internal/codec` 不再暴露可替换的 `StableJSON` 包级变量；marshal/unmarshal/valid 函数直接使用固定配置或由显式不可变实例实现。此重构必须保持现有 Provider request、Session checksum 和 cache fingerprint bytes 不变，并通过原 golden 回归。

这些修正不新增 RuntimeEvent 能力，也不改变 Session schema，因此与 headless 一起实施不会制造第二套迁移面。

## Risks / Trade-offs

- **[管道 stdin 可能等待未关闭的生产者]** → 只有非终端 stdin 或显式 `-` 才读取；文档明确需要 EOF，不引入基于时间的猜测和静默丢弃。
- **[4 MiB prompt 上限可能拒绝超大文件]** → 在副作用前返回明确错误；后续 coding tools 应通过 Read/artifact/context budget 处理大文件，而不是无限扩大初始 prompt。
- **[文本模式等待完成增加首字延迟和内存]** → 这是可组合 stdout 的必要保证；实时消费者使用 `--json`。
- **[JSONL 已写事件无法在后续失败时撤回]** → 强制唯一终止事件或 stream-level error，并用退出码表明整体失败；Session 成功仍只由 Runtime durable terminal 决定。
- **[输出 short write 可能留下无法解析的最后半行]** → 停止使用该 stdout、取消并在 stderr 报告；不追加第二种格式或假装可修复下游 pipe。
- **[删除预留内部 kind 会影响测试替身或未搜索到的消费者]** → 全仓 `rg`、编译、架构测试与全量 race test验证；未来能力必须随 producer/consumer 同时重新引入。
- **[关闭升级路径可能延长进程退出]** → 保留明确最终信号和 owner，宁可报告并等待可证明的 cleanup，也不在 writer/lease 仍活动时返回。
- **[外部 JSON v1 形成兼容负担]** → 首版字段保持最小、无 timestamp/usage/item 占位；为每种事件保存 golden，并要求后续字段/类型通过 OpenSpec 版本演进。

## Migration Plan

1. 先收紧 codec、RuntimeEvent kind/typed payload 和 ChatSession done 信号，使用现有 golden/race test证明行为不变。
2. 增加纯 prompt resolver、headless typed event/encoder/state machine 及故障注入单测，不接入真实 app。
3. 重构 app outcome 和 CLI 模式解析，将 headless runner接入既有资源装配与有序 cleanup。
4. 增加双 Provider httptest 集成：新 Session、显式 resume、失败、取消、输出故障和请求字节/cache 等价。
5. 更新 README、Roadmap、架构和 pitfall 文档，执行 `make verify` 后进入评审。

回滚时可以移除 `--json` 和恢复 `--print` 未实现分支，但必须同时移除尚未发布的 headless JSONL v1 artifacts；不得回滚或重写本 change 期间生成的合法 Session JSONL。内部协议/codec/ChatSession 的约束性修正若已验证兼容可独立保留。
