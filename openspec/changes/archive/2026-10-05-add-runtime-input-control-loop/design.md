## Context

参见 [proposal.md](./proposal.md) 的动机。当前 `Runtime.RunTurn` 已经正确实现单 turn 的 durable start、Provider staging/finalize、唯一 terminal 和 poisoned journal 保护；`runtime.ChatSession` 只在锁下维护一个 active flag，活动期间的新提交直接失败。一次性 headless runner 绑定一个 prompt 和一个 per-turn event channel，CLI 会在装配前把 stdin 整体解析为最多 4 MiB 的 prompt。

本变更受以下边界约束：

- `Runtime.RunTurn` 的单活动 turn 和 Provider-native history 事实边界保持不变。
- 新控制能力必须由实例持有，不能增加可变包级 queue、registry、codec 或 cache。
- 新 goroutine 必须有显式 `Start`/`Run` 入口、唯一 owner、最终完成信号和可测试的强制解除阻塞路径；`New*` 仍为纯内存构造。
- 外部 headless wire、进程内 RuntimeCommand、RuntimeEvent 和 Session records 是不同协议，不得互相直接序列化。
- queue acceptance 不是 durable Session 事实；本变更不能修改 JSONL/SQLite schema，也不能从 queue 构造 Provider history。
- 当前一个 turn 只有一次 Provider sampling，没有可以安全插入用户输入的工具结果边界。

## Goals / Non-Goals

**Goals:**

- 用一个 session-bound owner 原子决定输入启动、排队、定向取消、drain 和 shutdown。
- 在不改变 `RunTurn` durable 语义的前提下连续执行多个独立 turn。
- 为自动化宿主提供有界、严格、可关联且单 writer 的长期 NDJSON 控制模式。
- 保留现有 TUI、`--print` 和一次性 `--json` 的接口与行为，并为未来 TUI 接入同一控制器保留清晰边界。
- 让取消、EOF、显式 shutdown、输入/输出阻塞和 cleanup escalation 都有唯一 owner 与确定性测试点。

**Non-Goals:**

- 不在活动 turn 内注入新用户输入，不实现 `now/next/later`、优先级、prompt batching 或 Tool Loop safe point。
- 不把未启动 queue 持久化，也不承诺跨进程 exactly-once 或自动重放外部 request identity。
- 不修改 Provider capability、request compiler、stream reducer、native commit、usage 或 cache fingerprint。
- 不在本变更中改变 TUI 的按键行为；TUI 仍可继续使用现有单 turn facade。
- 不提供远程 socket、daemon、认证或多客户端并发协议。

## Decisions

### 1. 在 `Runtime.RunTurn` 之上增加独立 AgentLoop，而不是把 queue 塞进 Runtime

新增 session-bound `AgentLoop`，由它持有 admission 状态、活动 request/turn、FIFO queue、容量计数、近期 request identity ledger 和最终 `done`。构造器只校验并保存依赖；`Run(ctx)` 是唯一启动 owner loop 和 turn worker 的入口。

`Runtime.RunTurn` 继续只负责一个 turn，并保留原子 active guard作为最后一道防并发保护。AgentLoop 每次最多启动一个 turn worker；worker 只调用 `RunTurn` 并把 RuntimeEvent/最终返回送回 owner，绝不直接修改 queue 或写外部 stdout。owner 必须同时观察 terminal event 和 worker 完成信号，只有两者都满足后才启动下一输入，从而保证前一 turn 已 durable 收口且 goroutine 已退出。

选择这一层次是因为 queue、command admission 和 shutdown 属于宿主控制，而 durable sample 与 Provider-native history 属于 Runtime。备选方案“让 `RunTurn` 自身常驻并读取命令”会把多 turn 调度、headless 传输和单 turn 持久化揉成上帝状态机；“继续用 mutex 加每次 Submit 自行开 goroutine”则无法原子解决并发 submit、stale interrupt 和 lost wakeup。

### 2. owner 使用显式状态机，EOF 与 shutdown 走不同转换

核心状态只在 owner goroutine 内转换：

```text
accepting/idle --submit--> accepting/active
accepting/active --submit--> accepting/active + FIFO queue
accepting/* --EOF--> draining/* --queue empty + worker done--> closed
accepting/* --shutdown--> closing/* --worker done--> closed
accepting/active --matching interrupt--> cancelling/active --> accepting|draining|closing
```

- `draining` 拒绝新 admission，但执行全部已接受 queue。
- `closing` 拒绝新 admission、取消活动 turn，并把未启动 queue 转换为显式 discard output。
- 普通 turn failure 后，如果 Runtime/Journal 未 poisoned，则回到相应 accepting/draining 状态并继续；poisoned failure 直接进入 closing，拒绝后续 Provider 副作用。
- owner 在 worker 完成和 queue 空转换前重新检查 queue，不依赖一次性通知，消除“最后一次 dequeue 与 active=false 之间”的 lost-wakeup 窗口。

不采用 Claude Code 的全局三级优先队列：当前没有 mid-turn safe point，`next` 与 `later` 无法形成真实不同的模型语义，`now` 已由独立 interrupt 表达。

### 3. command handoff 是 admission 线性化点，调用方 context 不进入 queue

进程内 command 使用封闭的 v1 类型集合：submit、interrupt、shutdown。每个 kind 具有 typed constructor、getter、validator 和 strict decoder；payload 使用具体结构或受控 `json.RawMessage` 分派，不暴露 `any`。request identity 使用独立值类型，限制长度、空白和控制字符；外部 wire request ID 经验证后映射到该类型。生成内部兼容 ID 时使用现有 UUIDv7 设施的同类实现，但外部 ID 保持 opaque，不写入 Session。

AgentLoop 的提交入口使用无缓冲 request/reply handoff：

1. 调用方 context 只参与把请求交给 owner。
2. send 尚未成功时 context 取消，命令无副作用返回。
3. owner 接收后成为唯一处理者，调用方等待确定 admission result，不再以 context 取消撤回已接受工作。

这与 Journal writer 的取消线性化原则一致，也避免把 `context.Context` 保存到 queue。备选方案“每个排队输入保存调用方 context”会违反长期结构约束，并需要每条输入额外 watcher goroutine；“buffered command channel + caller cancel”会产生调用方已返回取消但命令稍后仍执行的模糊窗口。

### 4. command result 与 durable turn event 是两级确认

AgentLoop 输出封闭的强类型 control item：

- command result：`starting`、`queued`、`interrupting`、`closing` 或稳定 rejection。
- correlated turn event：request identity 加既有 `protocol.Event`。
- queued input discard：request identity 加 `session_shutdown` 或不可继续的 session failure。

`starting` 只表示 owner 已承担执行 obligation；它先进入有序输出，再启动 worker。只有 correlated `turn_started` 才表示 Session Sync 成功。queue 中每个输入独立执行，不合并文本；进程异常退出时没有 `turn_started` 的输入不属于 Session 恢复模型。

AgentLoop 不修改 RuntimeEvent envelope。关联信息由 control item 外层携带，避免把 headless request identity 污染 TUI、日志和 Session 通用事件。headless projector 再把它映射为 streaming DTO 的 `input_id`。

### 5. queue 和去重 ledger 都是实例级有界状态

AgentLoop 配置同时提供最大排队条数和 decoded UTF-8 总字节数；CLI 默认使用 64 条与 16 MiB，总是另行执行每条 4 MiB 校验。enqueue 在 owner 内完成“重复检查、容量检查、计数更新、插入”这一有界内存状态转换；dequeue/discard 同步回收字节计数。

identity ledger 至少覆盖正在 admission、queued、active 和 terminal delivery 的 request；已完成 identity 使用固定容量 FIFO/LRU 快照保留近期值，默认 4096 条。淘汰只削弱已明确不承诺的长期重试检测，不影响 outstanding exactly-once。ledger 不落盘、不跨 Session 实例共享。

不采用无界 map，也不把 request identity 加入 `turn_started(v1)`：后者会扩大 Session schema、fixture 和恢复语义，而当前本地 stdin 协议没有跨重连 exactly-once 需求。

### 6. 流式 headless 使用独立 wire DTO 和严格两阶段 decoder

CLI 增加 `--input-format`，默认 `text`；解析完成后将合法组合归一化为独立的 stream-json app mode，使应用装配不再重复判断 flag 组合。`--json --input-format stream-json` 不执行 `ResolvePrompt`，一次性模式完全沿用现有路径。

流式 decoder 先以有界逐行 reader 取得完整 frame，再扫描顶层 object members，显式拒绝重复 member、未知 envelope member、多个 JSON value和非法 UTF-8；随后根据 `type/version` 使用对应 concrete DTO 和统一 strict codec 解码，最后调用 `protocol.New*Command`。encoded line 上限使用 32 MiB，足以容纳 4 MiB decoded text 的最坏 JSON escape 和 envelope 开销，同时阻止无界累积。

输出另建 sealed streaming event 集合：`thread.started`、`control.response`、带 `input_id` 的 turn/delta/terminal、`input.discarded` 和 `error`。它复用已有 projector 的校验与 usage/error 转换逻辑，但不复用或扩宽一次性 `headless.Event` 接口，因而一次性 `--json` 不会意外输出 control vocabulary。

不直接把进程内 Command/Result/Event JSON tag 当作 wire schema。这样内部状态可以重构，外部 v1 仍由专属 constructor、validator、encoder 和 fixture 固定。

### 7. stdin reader、AgentLoop 和 stdout writer 具有一个顶层协调 owner

`StreamRunner.Run` 是显式生命周期入口，协调三个角色：

- reader：增量解码一条命令后通过 request/reply handoff 提交；收到 shutdown 后停止再读，EOF 调用 graceful close。
- AgentLoop：唯一控制状态 owner，并产生已经排序的 control items。
- writer：唯一调用 stdout 的 goroutine，严格编码完整 JSONL frame。

reader 不会无限预读：它必须等当前 command result 被 owner 接受并排入输出顺序后才读取下一条，因此 control response backlog 有常数上界。Runtime delta 通过小型有界 channel 施加背压；owner 不在 mutex 内发送、等待或执行 I/O。

stream transport 显式接收可关闭的 input handle，以及仅用于失败升级的 output closer。正常 EOF/shutdown 不关闭调用方 stdout；发生 output 阻塞、根 context 取消或 runner 失败时，协调 owner 关闭相应 handle 解除 read/write，取消 AgentLoop，等待 reader、writer 和 turn worker 的固定 `done` 信号。不得为每次 Wait 或每条命令创建 helper goroutine。

备选方案“reader 与 Runtime goroutine各自直接写 stdout”无法保证 control response 和 turn event 的全局顺序；“先把整个 stdin 读完”无法支持 interrupt；“对不可取消的任意 io.Reader 启动后台 Read”会在 shutdown 时遗留 goroutine。因此 streaming app 装配必须提供拥有明确关闭语义的 transport，测试用 `io.Pipe` 和故障注入证明解除阻塞。

### 8. app 继续承担最终资源关闭与 transport escalation

正常 stream shutdown 先由 AgentLoop 取消活动 turn并等待 Runtime durable terminal。若根 shutdown deadline 到期，`app` 保持现有唯一资源 owner：先关闭 Provider transport 解除阻塞，再以不受原 deadline 取消的 context 等待 AgentLoop/Runtime 完成，随后依次关闭 Journal writer、Repository 和 Provider。stream runner 在 cleanup 未完成前不能声明成功或释放其完成信号。

为避免 headless 反向依赖具体 Provider，runner 只依赖最小 lifecycle/escalation 接口，由 app 适配现有 `chatResources`。所有回调在锁外执行，且 Close 必须幂等。显式 `session.shutdown` 的正常成功不关闭 Session JSONL 之前的 stdout terminal；异常 escalation 则通过 stream-level error/退出码 1 表达，不能伪造业务 terminal。

### 9. 流式退出码表达控制进程健康，而不是累积业务结果

一次性 headless 仍以单 turn 结果决定退出码。长期流式模式已经为每个 turn 和 command输出强类型结果，因此 clean EOF 或显式 shutdown 在协议、输出和 cleanup 都成功时返回 0，即使其中某个 turn 曾产生 `turn.failed`。输入协议错误、stdout 失败或清理失败返回 1；CLI 用法错误返回 2。

若把任一历史 turn failure 累积成进程失败，长会话将无法在失败后恢复继续，也会让显式 interrupt 的预期取消导致最终非零退出。调用方应按 `input_id` 消费业务结果，进程退出码只判断 transport/control 生命周期。

## Risks / Trade-offs

- [Risk] `queued` 已确认但进程在 `turn.started` 前崩溃时输入会丢失 → 文档和 wire 明确两级确认；只把 durable `turn.started` 作为可恢复事实，不提供虚假 exactly-once。
- [Risk] stdout backpressure 会减慢 Provider stream 读取并推迟普通 command response → 使用小型有界 pipeline、同步 command handoff 和可关闭 transport；interrupt/shutdown 由 owner 优先处理，但不丢弃或重排既有输出。
- [Risk] 新旧 headless JSON 都使用部分相同 type 名称，消费者可能混淆 → stream 模式必须显式开启，control vocabulary 和 `input_id` 由独立 sealed DTO/fixture 固定，一次性 encoder 不扩展。
- [Risk] shutdown、terminal 和下一 turn 启动竞态可能误取消新 turn → interrupt 强制携带 expected turn ID，owner 在同一 goroutine中比较并调用当前 cancel，worker 完成前不得替换 active identity。
- [Risk] Reader/Writer 对任意 `io.Reader/io.Writer` 无法保证可取消 → streaming 装配要求显式 close/unblock capability，并通过真实 pipe 与确定性阻塞 fixture 测试；一次性 API 不受影响。
- [Risk] 近期 identity 淘汰后旧请求可再次执行 → v1 只承诺 outstanding/保留窗口去重；跨重启或无限期 exactly-once 需要未来 Session schema change。
- [Risk] AgentLoop 与现有 ChatSession 暂时并存可能形成重复 orchestration → 两者都只组合同一个 `Runtime.RunTurn`，本变更不抽象共享“manager”；后续 TUI 真正需要 queue 时再迁移并删除旧 facade，Roadmap 记录退出条件。

## Migration Plan

1. 增加新的 protocol command/result 类型、AgentLoop 和测试，不改变现有 Runtime、Provider 或 Session bytes。
2. 增加 streaming headless DTO、decoder/encoder/runner 和 fixture，再在 app 中装配为独立模式。
3. 最后增加 CLI `--input-format` 参数和合法组合校验；默认值保持一次性 text 输入，因此现有脚本无需迁移。
4. 同步架构、Roadmap 和 pitfall 文档，并通过 `make verify` 后交付。

本变更不需要数据迁移或 journal rewrite。回滚可以移除新 CLI 模式和 AgentLoop，而已经由它完成的 turns 仍是标准 v1 Session records，可由旧的一次性 resume 路径继续读取；不得回滚或重写这些 durable turns。
