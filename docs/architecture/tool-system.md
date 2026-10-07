# EasyCode Tool 系统架构设计

> 状态：P3 目标架构，尚未实现
> 更新时间：2026-10-07
> 适用范围：Tool Catalog、Provider Tool Wire、Agent Tool Loop、权限与 sandbox、执行调度、结果预算、Session 恢复和宿主展示
> 前置阅读：[总体架构设计](overall-architecture.md)、[产品阶段路线图](../roadmap/product-roadmap.md)、[踩坑与经验记录](../roadmap/pitfall-log.md)

## 1. 文档定位

本文固定 Tool 系统的完整大图、长期边界和分阶段实现顺序，避免多个长周期 change 各自发明一套调用、权限、恢复或结果协议。

本文不是“能力已实现”的声明。当前 `internal/tool` 与 `internal/tool/builtin` 仍是经批准的 TODO 占位；任何 Tool schema、Session record、Runtime command/event、权限协议或跨包接口落地前，仍必须建立独立 OpenSpec change，经过 `propose -> review/confirm -> apply -> verify -> archive`。

参考工程的使用原则如下：

- Claude Code `@anthropic-ai/claude-code@2.1.88` sourcemap 用于理解产品行为、工具展示、权限交互、工具并发和大结果体验。该目录不是官方完整源码，文件组织和内部实现不作为可复制契约。
- Codex 参考提交 `c248f6d48b` 用于理解 Responses tool wire、工具路由、sandbox、进程生命周期、结果截断和 rollout 恢复。
- EasyCode 的产品体验优先对齐 Claude Code；内核边界选择可测试、可恢复、能支持双 Provider 的方案。两者存在冲突时，不以照搬任一参考工程为目标。

### 1.1 为什么当前选择 Tool 模块

Tool 是当前最适合推进的能力模块，原因不是 Roadmap 顺序本身，而是已有基线刚好满足它的关键前置条件：

1. 双 Provider 已建立原生 history、request compiler、stream reducer 和 prepared sample 边界，可以在不压平 Anthropic/OpenAI 语义的前提下接入两套 tool wire。
2. Session 已具备 append-only JSONL、exclusive lease、durable batch、typed record 和恢复校验，可以承载“副作用前必须持久化”的调用事实。
3. Runtime 已具备 turn owner、取消、输入队列和 Provider stream 清理路径，可以自然演进为多 sample Tool Loop，而不需要另建第二套 agent lifecycle。
4. ContextPlanner、项目指令和 usage 已有稳定来源与 fingerprint 规则，Tool schema 可以作为新的明确来源加入，而不是混进动态 prompt 文本。
5. Tool 是从文本对话走向真实 coding agent 的最短产品闭环，也是 P4 tool schema cache、P6 hooks/skills、P7 MCP/subagent 的共同前置；继续先做上层扩展会迫使它们依赖空接口。
6. 风险可以通过 Read 首个纵向切片控制：先验证双 Provider、多 sample、durable、ledger 和配对，再引入写文件与进程的不可逆副作用。

因此选择整个 Tool 系统作为下一能力模块，但实现单位不是“大而全 Tool change”，而是第 14 节定义的分散纵向 change。这个选择同时满足产品价值、依赖顺序和风险可控三项条件。

## 2. 参考工程结论与 EasyCode 取舍

| 关注点 | Claude Code 可观察行为 | Codex 可借鉴内核 | EasyCode 决策 |
| --- | --- | --- | --- |
| 工具定义 | 工具对象同时承载 schema、校验、权限、执行、展示等能力，产品闭环完整 | router、handler、sandbox、approval 的职责更显式 | 拆分 Capability、Facade、Validator、Policy、Scheduler、Executor、ResultCodec 和 Presenter，禁止上帝 Tool 类型 |
| 工具目录 | 工具按模式、权限、实验能力和动态来源过滤后暴露给模型 | 静态 handler 与动态工具统一路由 | 每次 sampling 使用不可变 `CatalogSnapshot`；可见性过滤在请求前完成，执行时仍二次校验 |
| Provider wire | 原生使用 Anthropic `tool_use`/`tool_result`，JSON 参数支持增量展示 | 原生使用 Responses function/custom tool call/output | 保留两套原生 wire 和输入 decoder；只在参数完整后形成共享 `ToolCallReady` |
| 执行时机 | 可在响应尚未完全结束时启动已经完整的工具 block，降低延迟 | 通常先完成 response item 归并，再由 turn loop 执行 | 首版选择“完整 sample durable 后再执行”；以后若引入流内提前执行，必须单独证明 durable、取消和恢复语义 |
| 并发 | 连续且安全的只读工具可并发，结果展示与模型配对稳定 | 并发和串行策略由工具类型显式决定 | 调度可并发，提交给模型的结果严格保持调用顺序；首版只提供 `parallel_read` 与 `exclusive` 两类 |
| 权限 | 交互式 allow/ask/deny、输入修改和模式切换是核心产品体验 | approval 与 sandbox enforcement 分离 | Policy 只做决策，SandboxPlan 约束实际执行；用户修改输入后必须从解析开始重新验证 |
| 大结果 | 对模型展示有预算和替换文案，完整内容可供用户查看 | 截断策略按工具定制，命令输出支持分段和限制 | 区分完整结果、模型预览、artifact 和 UI 摘要；模型实际收到的字节必须持久化，恢复时不得重新计算 |
| 恢复 | transcript 保存工具调用与结果，产品上维持合法配对 | rollout 记录 response item 和 tool output | JSONL 保存 Provider-native item 与共享执行事实；恢复时补齐未闭合调用，不从 RuntimeEvent 反推历史 |
| 幂等 | 调用 ID 用于关联与去重 | invocation ID、turn state 和 rollout 共同约束 | ledger 只承诺自动执行的至多一次；崩溃后的不确定外部副作用不得自动重放 |

最重要的取舍是：EasyCode 采用 Claude Code 的产品闭环，而不复制其单体工具对象；采用 Codex 的安全边界，而不把 Responses 专有数据模型扩散到共享执行器。

## 3. 核心不变量

1. 只有完整、严格解码且通过语义验证的参数才能形成 `ToolCallReady`；draft、delta 和展示中的预览不得产生副作用。
2. Provider-native history 是下一次 sampling、resume 和配对修复的事实依据；RuntimeEvent 只用于宿主投影。
3. 当前模型 sample 的 Provider-native commit、usage 和全部 ready call 必须先作为一个 durable batch 成功 `Sync`，之后才允许开始工具副作用。
4. 每次工具调用都具有 Provider call ID 和 EasyCode invocation ID；执行 ledger 由 invocation ID 唯一定位，不能依赖工具名或参数哈希猜测身份。
5. 自动执行至多一次不等于外部世界严格 exactly-once。`execution_started` 已 durable、结果却未 durable 的调用恢复为 `outcome_uncertain`，默认禁止自动重试。
6. 并发只改变实际完成时间，不改变模型可见的 call/output 顺序。
7. 每个已提交的 tool call 最终必须产生合法的 Provider-native output，包括成功、拒绝、取消、验证失败和恢复补偿；不得留下悬空配对。
8. Approval 表示“用户同意意图”，Sandbox 表示“内核能强制的能力边界”，两者不能互相替代。
9. 文件类型、权限、symlink 和 workspace containment 必须绑定到实际打开的句柄；路径字符串预检只能作为早期拒绝，不能作为最终安全证明。
10. 完整工具结果、模型预览、artifact 和 UI 展示是四种不同表示；它们必须来自同一次结果归一化，不能在恢复时按新配置重算。
11. Catalog 顺序、schema 序列化和稳定描述必须确定性生成并参与 fingerprint；动态 cwd、时间、随机 ID 和权限临时状态不得污染稳定前缀。
12. 未实现的工具、事件、配置和 capability flag 不得提前暴露给模型、Runtime 或用户。

## 4. 总体架构

```text
                         +------------------------------+
                         |         Agent Runtime        |
                         | sample -> durable -> tools   |
                         +--------------+---------------+
                                        |
              +-------------------------+-------------------------+
              |                         |                         |
      +-------v---------+       +-------v--------+        +-------v--------+
      | Provider Kernel |       | Tool Catalog   |        | Session Writer |
      | native wire     |       | snapshot/schema|        | facts/ledger   |
      +-------+---------+       +-------+--------+        +----------------+
              |                         |
      facade/result codec               | resolved capability
              |                         |
      +-------v-------------------------v--------------------------+
      |                 Tool Invocation Pipeline                  |
      | decode -> validate -> policy -> approval -> sandbox plan |
      |       -> durable start -> schedule -> execute             |
      +--------------------------+--------------------------------+
                                 |
                    +------------v------------+
                    | Result Budget/Artifact |
                    | native output/presenter|
                    +-------------------------+
```

依赖规则：

- `ToolExecutor` 不导入 Provider、Runtime、Session、TUI 或具体终端实现。
- Provider 只消费无 I/O 的 Tool schema snapshot、validated input/output value 和 facade codec，不持有 executor。
- Runtime 是 Tool 与 Provider 的组合根，负责 durable 边界、多 sample 循环和取消所有权。
- Session 只保存版本化事实，不调用 executor，也不根据记录自行恢复副作用。
- TUI/headless 只消费 RuntimeEvent 和受控 metadata，不读取 Provider SDK 类型或 executor 私有状态。
- 动态 registry、codec、策略、缓存和 map 必须由实例持有；禁止使用生产代码包级可变状态。

## 5. 组件边界

### 5.1 ToolCapability

稳定的共享能力标识，例如 `fs.read`、`fs.search`、`fs.patch`、`fs.write`、`process.exec`。它描述权限和调度语义，不直接等于模型可见名称。

Capability 必须声明：

- 输入和输出的内部 revision。
- 副作用分类与调度分类。
- 所需 sandbox capability。
- 默认结果预算类别。
- 是否支持 executor 级幂等键。

### 5.2 ToolFacade

面向指定 Provider/模型的名称、描述、输入 schema 和 wire mode。同一 Capability 可以存在多个 Facade，例如：

```text
fs.patch
  Anthropic: Edit        / structured JSON
  OpenAI:    apply_patch / custom freeform
  Executor:  PatchExecutor
```

Facade 不执行工具，也不自行放宽 Capability 的安全约束。

### 5.3 Catalog 与 CatalogSnapshot

`ToolCatalog` 在应用启动或显式刷新时组合 built-in 与未来扩展工具，并生成不可变 snapshot。每个 sampling 只绑定一个 snapshot revision，至少包含：

- 稳定排序后的可见 Facade。
- Capability 与 executor route 的封闭映射。
- schema canonical bytes 与 fingerprint。
- policy/sandbox 所需的静态 metadata。
- 来源 revision，但不包含密钥、绝对临时路径和易变 world state。

模型只能调用本次 snapshot 中可见的工具。动态刷新只能在 sampling safe point 切换 snapshot；已经收到的调用继续使用其原 snapshot 路由，避免同名工具被静默替换。

### 5.4 InputDecoder 与 Validator

`ToolInputStreamDecoder` 归并 Provider-native delta，仅产生无副作用 draft。完整 item 到达后，strict decoder 生成 typed input，再由 Validator 检查语义、边界、大小和组合约束。

禁止导出的核心构造器接受 `any` 或无约束 `map[string]any`。每个 capability/revision 必须具有 typed constructor、strict decoder 和 validator。

### 5.5 ToolPolicy 与 Approval

Policy 输入是不可变调用描述和当前安全上下文，输出强类型决策：

- `allow`：无需交互即可继续。
- `ask`：生成权限请求，等待宿主答复。
- `deny`：生成稳定的机器错误码和 Provider-native 拒绝结果。

用户批准可以是本次允许、会话内规则或配置变更，但持久范围必须由独立 OpenSpec 明确定义。若宿主允许修改命令、路径或 patch，修改后的输入视为新候选，必须重新 decode、validate、policy 和 sandbox plan，不能继承旧批准。

### 5.6 SandboxPlanner

SandboxPlanner 把验证后的调用与 policy decision 转换为执行能力计划，例如：只读目录、可写目录、网络、环境变量、进程组和资源上限。Executor 只在该计划内工作。

批准不能绕过平台不支持或安全 fallback。某平台无法提供声明的强制隔离时，应失败关闭或明确降级为更严格能力，不能仅用提示文本模拟 sandbox。

### 5.7 ToolScheduler

首版只定义两类可证明的调度语义：

- `parallel_read`：无外部写入且声明并验证为并发安全。
- `exclusive`：文件写、patch、进程和任何不明确安全的工具。

Scheduler 可以并发执行同一连续批次中的 `parallel_read`，遇到 `exclusive` 时建立屏障。完成和进度可以即时投影给 UI，但交给 Provider 的 outputs 必须按原始 call index 排序。

### 5.8 ToolExecutor

Executor 接收 typed input、受限执行能力和显式生命周期依赖，返回 typed result。它不负责：

- Provider wire 编码。
- TUI 渲染。
- 权限询问。
- Session 写入。
- call 顺序收集。
- 从全局变量查找配置或 registry。

长运行 executor 必须支持取消，拥有唯一 cleanup owner、最终完成信号和可测试的强制终止路径。

### 5.9 ResultBudget、ArtifactStore 与 ResultCodec

结果归一化一次产生：

1. 完整 typed result。
2. 确定性的模型预览或替换结果。
3. 可选 artifact descriptor。
4. 面向 UI 的受控摘要 metadata。

`ToolResultCodec` 只负责把已冻结的模型预览编码为 Anthropic 或 OpenAI 原生 output。artifact 路径不得把宿主绝对敏感路径、密钥或未授权内容泄漏给模型。

### 5.10 ToolPresenter

Presenter 将 RuntimeEvent 渲染为 TUI/headless 输出。它可以显示 draft、diff、进度和 artifact 提示，但不得参与执行决策，也不得成为恢复事实源。

## 6. 双 Provider Tool Wire

### 6.1 Anthropic Messages

- RequestCompiler 将 snapshot 编译为 Anthropic tools。
- StreamReducer 归并 `tool_use` block 和 `input_json_delta`，只在 block 完整且严格校验后产生 ready call。
- 工具结果编码为对应 call ID 的原生 `tool_result` content block，并保留错误语义。
- thinking、signature、redacted thinking 与 tool block 的原始顺序必须由 native history 无损保存。

### 6.2 OpenAI Responses

- RequestCompiler 根据 Facade 编译 function tool 或 custom tool。
- StreamReducer 分别处理 function arguments 与 custom/freeform input delta，不能套用 Anthropic JSON 假设。
- 输出编码为与原始 call 类型匹配的 function/custom tool output item。
- reasoning、encrypted content、message phase 和 tool item 的原始顺序必须由 native history 无损保存。

### 6.3 共享边界

Provider reducer 最终交付 `PreparedSample`：原生 envelope、usage、按模型顺序排列的 ready calls 和一次性 finalizer。Runtime 在写入前重新验证整个 sample，随后将原生 commit、usage 与 ready call facts 原子 `Sync`。

draft update 可即时发布给 UI，但不进入 JSONL 高频事实流。任何 stream 失败、取消或不完整 item 都不得生成可执行调用。

## 7. 多 Sample Agent Tool Loop

一个用户 turn 可以包含多个 Provider sample：

```text
durable turn_started
  -> Provider sample N
  -> validate PreparedSample
  -> durable native_commit + sample_usage + tool_call_ready[]
  -> execute calls
  -> durable tool result/rejection/cancellation facts
  -> append Provider-native tool outputs
  -> sampling safe point
  -> Provider sample N+1 或 durable turn terminal
```

规则如下：

- 每个 sample 独立记录 usage，turn usage 只是投影，不替代原始 sample facts。
- Provider finalizer 只在对应 durable batch 成功后执行一次。
- tool outputs 全部 durable 后才能开始下一次 Provider stream。
- follow-up queue 默认仍形成后续独立 turn；same-turn steer 只能在 tool outputs 后、下一 sample 前的 safe point 注入，并必须通过独立 change 定义历史与缓存语义。
- 每个 turn 必须限制最大 sample 数、最大 tool call 数和累计结果预算；达到限制时产生可恢复的明确终态，不能无限循环。

## 8. 执行 Ledger 与崩溃语义

### 8.1 状态机

```text
ready_durable
     |
     +-- policy denied --------------> denied_durable
     +-- approval rejected ----------> rejected_durable
     +-- cancelled before acceptance -> cancelled_durable
     |
     +-- execution_started_durable
                |
                +-- result_durable --> completed
                +-- failure_durable -> failed
                +-- crash/no result -> outcome_uncertain
```

`execution_started_durable` 是副作用接收线性化点：

- 在它之前取消，直接从ready写入cancelled result，不得启动副作用。
- started append被接受后，即使context同时取消，owner也必须恰好调用一次executor并取得确定结果；若进程崩溃导致无法确认，恢复为 `outcome_uncertain`。
- `outcome_uncertain` 默认生成补偿 output 并停止自动推进，要求用户检查或显式处理；不得把未知当失败后自动重试。

### 8.2 幂等承诺

ledger 可以保证同一 invocation 不被 EasyCode 自动执行两次，但无法消除“外部副作用已经发生、结果尚未 durable”的崩溃窗口。只有 executor 对目标系统提供可靠 idempotency key 或可验证提交协议时，特定 capability 才能声明更强保证。

因此文档和 UI 禁止笼统宣称“所有工具 exactly-once”。首版目标是：自动执行至多一次、结果可审计、不确定状态失败关闭。

## 9. 权限、安全与文件系统

### 9.1 Approval 与 Sandbox 分层

权限请求至少应展示：工具能力、规范化目标、风险摘要、预计写入范围和 sandbox 限制。Approval 结果是 policy fact；SandboxPlan 是 executor 的实际强制边界。即使用户批准，未授权网络、workspace 外写入、敏感环境变量和平台不支持能力仍保持禁止。

headless 无交互宿主遇到 `ask` 时默认失败关闭。未来若支持预授权规则，应由配置快照显式提供，不能在后台隐式批准。

### 9.2 文件工具

- workspace containment、类型、权限和 symlink 判断绑定实际打开的 fd/handle。
- Read/Search 使用受限 handle 或从可信目录 handle 相对打开。
- Write/Patch 在目标目录内创建临时文件，验证后原子替换；权限保留、换行、编码和冲突策略由具体 change 定义。
- Patch 必须验证 base evidence，至少能够检测读取后目标变化；静默覆盖并发修改不可接受。
- artifact 写入使用独立受控根目录和 descriptor，不复用任意模型输入路径。

### 9.3 进程工具

- shell/argv 形式、cwd、环境变量、网络、超时、输出预算和 sandbox 在启动前冻结。
- 启动后由唯一 process owner 管理 stdin、stdout/stderr、取消、进程组终止与最终 wait。
- `write_stdin` 只引用受控 process handle，不直接持有 OS PID 作为授权凭证。
- shutdown 超时不等于放弃 owner；必须有明确的强制取消或升级路径，并等待最终完成信号。

## 10. 结果预算与 Artifact

每个 capability 选择显式预算策略，而不是全局粗暴截断：

- Read 优先保留请求行范围和定位信息。
- Grep/Glob 保留稳定排序后的前后命中，并报告省略计数。
- Exec 可保留头尾窗口、退出码和 stderr 摘要。
- Patch/Write 返回确定性 diff/摘要，不回显整个文件。

当完整结果超限时：

1. 先把完整结果按策略保存为权限受控 artifact。
2. 生成确定性的模型预览和替换原因。
3. 将“模型实际收到的预览字节 + artifact descriptor + 预算 revision”写入 durable fact。
4. 恢复时复用已保存预览，不根据新预算重新截断。

artifact 不是 Session 事实源。JSONL 保存 descriptor、摘要和完整性信息；SQLite 只做可重建索引。敏感结果可以选择不落 artifact，此时必须生成明确的不可恢复说明。

## 11. Session 事实与 RuntimeEvent

| 信息 | Session durable fact | RuntimeEvent | 说明 |
| --- | --- | --- | --- |
| Provider 原生 sample | 是 | 可投影摘要 | 下一请求与恢复事实 |
| ToolCallReady | 是 | 是 | 副作用前置事实 |
| 权限请求与决定 | 决定是；瞬时 UI 状态否 | 是 | 决定影响恢复，动画不影响 |
| execution started | 是，且先于副作用 | 是 | ledger 线性化点 |
| 结果/拒绝/取消/不确定 | 是 | 是 | 保证 call/output 配对 |
| 高频参数 delta | 否 | 可节流发布 | 不作为恢复事实 |
| 高频 stdout/stderr chunk | 默认否 | 可节流发布 | 完整内容按 artifact 策略处理 |
| 模型预览字节与预算 revision | 是 | 可投影 | 恢复必须精确复用 |
| UI 展开、焦点、spinner | 否 | 本地状态 | 不进入协议与缓存 |

未来引入任何 record kind/revision 时，必须同时提交 typed constructor、strict decoder、validator、不可变历史 fixture 和 replay migration test。既有 append-only records 不得在 resume 时原地改写。

## 12. 配对修复与恢复

恢复器按 Provider-native history 和 ledger 判断：

- `ready_durable` 且从未开始：本地写入 `cancelled/session_interrupted_before_execution`，不调用policy或executor。
- 已有确定结果：只重建 Provider-native output，不重复执行。
- `execution_started_durable` 无确定结果：标记 `outcome_uncertain`，生成确定的补偿 output，禁止自动重跑。
- 权限拒绝、验证失败和取消：生成与 Provider call 类型匹配的错误 output。
- output 已 durable 但旧turn没有terminal：不重复output，只补写失败终态。
- 所有未完成工具turn在补齐call/output pairing后写入且只写入一个 `turn_failed`。

恢复阶段只允许纯内存Provider output编码与Session append/Sync，不得调用executor、不得建立Provider stream，也不得自动继续旧turn的下一sample。只有后续真实用户输入可以发起新请求；该请求必须保持native item顺序和tool call/output pairing，并与相同reconciled history不经重启构造的canonical bytes及fingerprint等价。无法等价时必须停止推进，不能通过扁平消息或RuntimeEvent猜测补齐。

## 13. 能力分期

### 13.1 P3 核心能力

- Read：已由 `add-read-tool-loop` 实现；当前仅支持顺序执行和固定只读策略。
- Glob 与 Grep。
- Edit/structured patch 与 OpenAI `apply_patch` freeform facade。
- Write。
- exec 与 `write_stdin`，随后补后台进程控制。
- allow/ask/deny、approval、sandbox plan。
- ledger 与配对恢复：Read 已实现；有序并发、通用结果预算和 artifact 仍未实现。
- 工具回合后的 sampling safe point，以及建立在该 safe point 上的 same-turn steer。

### 13.2 后续阶段能力

- Skill：属于 P6 的上下文与指令装载能力，不在 P3 提前伪装成可执行工具。
- MCP tools：属于 P7 的远程 catalog、trust、认证和动态 schema 能力。
- Agent/Subagent：属于 P7 的 thread、budget、completion envelope 和取消传播能力。
- hooks/plugins：属于 P6，不能借 Tool callback 绕过版本化 manifest 和权限边界。
- 完整跨平台 sandbox 矩阵：P8；P3 只实现已验证平台能力并对其他平台失败关闭。

## 14. 推荐的 OpenSpec 纵向切片

下列名称是推荐 change 名称，不代表目录已经创建。每个 change 必须只在前一项归档或其契约稳定后再提出，避免长线 change 同时修改 Provider、Session、Runtime、Tool 和 UI。

| 顺序 | 推荐 change | 主要闭环 | 明确排除 |
| --- | --- | --- | --- |
| 1 | `add-read-tool-loop` | 单个 Read 的 catalog、双 Provider wire、durable ready/result、ledger、一个多 sample 回合 | 并发、写入、shell、通用动态 registry |
| 2 | `add-search-tools-and-ordered-parallelism` | Glob/Grep、连续只读并发、按调用顺序提交 | 写工具和复杂调度 DAG |
| 3 | `add-tool-approval-protocol` | allow/ask/deny、headless 失败关闭、输入修改后重验、approval event/fact | 平台 sandbox 具体实现 |
| 4 | `add-file-patch-tools` | Anthropic structured Edit、OpenAI freeform apply_patch、base evidence、diff artifact | 任意文件写入 facade |
| 5 | `add-file-write-tool` | 新文件/整文件写入、原子替换、权限与冲突策略 | 命令执行 |
| 6 | `add-command-execution` | exec、进程 owner、取消、超时、输出预算和最小 sandbox | 后台进程复用 |
| 7 | `add-background-process-control` | process handle、write_stdin、最终 wait 和 shutdown | 通用任务系统 |
| 8 | `add-tool-result-budget` | 各工具预算、artifact、持久化模型预览、恢复等价 | 远程 artifact 服务 |
| 9 | `add-same-turn-steering` | tool output 后 safe point 注入、缓存与历史语义 | 任意时刻抢占 Provider stream |

首个 change 选择 Read，而不是先搭建“完整通用框架”，原因是：

- Read 能最小闭环验证 catalog、两套 Provider wire、多 sample Runtime、Session durable batch、ledger 和 result codec。
- Read 的副作用风险低，便于先证明恢复与配对语义，再引入写入和进程不可逆性。
- 一个真实消费者足以驱动必要接口，避免提前制造空 Facade、万能 registry 或没有退出条件的抽象。
- 首个 change 仍必须包含拒绝、取消、重放、崩溃点和双 Provider request golden，不能退化成只会调用 `os.ReadFile` 的演示。

## 15. 测试与验收矩阵

每个相关 change 至少覆盖其触及的行：

| 维度 | 必须验证 |
| --- | --- |
| Provider wire | Anthropic/OpenAI request golden、随机 SSE chunk、半包、UTF-8、未知事件、取消和断线 |
| Schema/cache | catalog 排序、canonical schema bytes、filesystem/registration 顺序扰动后的 fingerprint 稳定性 |
| 输入 | partial delta 不执行、strict decode、大小上限、未知字段、修改后重新验证 |
| Ledger | 拒绝、取消、重复 invocation、started 后崩溃、结果 Sync 失败和 `outcome_uncertain` |
| 并发 | 只读并发、exclusive 屏障、乱序完成但顺序提交、无 sleep 的确定性调度测试 |
| 文件安全 | traversal、symlink、TOCTOU 故障注入、实际 handle 类型/权限检查、原子替换失败 |
| 进程安全 | 启动失败、取消、阻塞 stdout、进程树清理、shutdown timeout 和唯一 owner |
| 结果 | 工具特定截断、artifact 失败、secret redaction、恢复复用相同模型预览字节 |
| Session | 不可变 fixture、尾部半行、lease 竞争、repair、live/restored 下一请求等价 |
| 宿主 | headless approval 失败关闭、TUI 固定尺寸 snapshot、RuntimeEvent 不改变请求事实 |

任何实现完成前必须通过 `make verify`，并核对 OpenSpec、总体架构、Roadmap、踩坑记录和实际 record vocabulary 一致。

## 16. 尚需由具体 Change 冻结的参数

本文只固定边界，不提前固定没有实现证据的数值。以下参数由首次消费者所在 change 给出默认值、配置面和测试：

- 每 turn 最大 sample 数与 tool call 数。
- 每个 capability 的输入、模型预览、artifact 和累计上下文预算。
- approval 规则的持久范围和过期方式。
- P3 支持平台及各平台 sandbox capability matrix。
- Read/Patch/Write 的编码、换行、权限保留、冲突和二进制文件策略。
- exec 的 shell/argv 模式、环境变量白名单、网络与资源限制。
- `outcome_uncertain` 的用户恢复命令与宿主交互。

在对应 change 批准前，不得通过隐藏默认、占位配置或未消费的 event kind 抢先实现这些选择。
