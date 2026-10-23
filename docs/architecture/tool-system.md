# EasyCode Tool 系统架构设计

> 状态：P3 增量实现中；Read、Glob、Grep、单文件 Patch 与 Darwin 前台 Exec 已实现
> 更新时间：2026-10-24
> 适用范围：Tool Catalog、Provider Tool Wire、Agent Tool Loop、权限与 sandbox、执行调度、结果预算、Session 恢复和宿主展示
> 前置阅读：[总体架构设计](overall-architecture.md)、[产品阶段路线图](../roadmap/product-roadmap.md)、[踩坑与经验记录](../roadmap/pitfall-log.md)

## 1. 文档定位

本文固定 Tool 系统的完整大图、长期边界和分阶段实现顺序，避免多个长周期 change 各自发明一套调用、权限、恢复或结果协议。

本文同时记录已实现基线与后续目标。当前 `internal/tool` 与 `internal/tool/builtin` 提供 Read、Glob、Grep、单个已有 UTF-8 工作区文件 Patch，以及 Darwin 前台 Exec。五项能力具有强类型输入/结果、双 Provider facade 和有序调度；Patch/Exec 都是 exclusive 屏障。Anthropic 使用 structured `Edit`/`Bash`，OpenAI 使用 custom freeform `apply_patch`/function `exec_command`。交互式批准默认聚焦 No，Exec 始终只允许单次批准；headless `ask` 以 `approval_required` 失败关闭。Write、PTY、后台进程控制、artifact、跨能力通用 SandboxPlanner 和完整 Tool UI 尚未实现；非 Darwin 命令执行明确返回 `unsupported_platform`，不得无 sandbox 降级。新增 Tool schema、Session record、权限协议或跨包接口仍须独立 OpenSpec change。

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

- 当前强类型输入、结果与 validator。
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

`ToolCatalog` 在应用启动或显式刷新时组合 built-in 与未来扩展工具，并生成不可变 snapshot。每个 sampling 只绑定一个内容 fingerprint，至少包含：

- 稳定排序后的可见 Facade。
- Capability 与 executor route 的封闭映射。
- schema canonical bytes 与 fingerprint。
- policy/sandbox 所需的静态 metadata。
- 由稳定 facade 名称、描述和 canonical schema bytes 派生的 fingerprint，不包含密钥、绝对临时路径和易变 world state。

模型只能调用本次 snapshot 中可见的工具。动态刷新只能在 sampling safe point 切换 snapshot；已经收到的调用继续使用其原 snapshot 路由，避免同名工具被静默替换。

### 5.4 InputDecoder 与 Validator

`ToolInputStreamDecoder` 归并 Provider-native delta，仅产生无副作用 draft。完整 item 到达后，strict decoder 生成 typed input，再由 Validator 检查语义、边界、大小和组合约束。

禁止导出的核心构造器接受 `any` 或无约束 `map[string]any`。每个当前 capability 必须具有 typed constructor、strict decoder 和 validator。

### 5.5 ToolPolicy 与 Approval

Policy 输入是不可变调用描述和当前安全上下文，输出强类型决策：

- `allow`：无需交互即可继续。
- `ask`：生成权限请求，等待宿主答复。
- `deny`：生成稳定的机器错误码和 Provider-native 拒绝结果。

用户批准可以是本次允许、会话内规则或配置变更，但持久范围必须由独立 OpenSpec 明确定义。若宿主允许修改命令、路径或 patch，修改后的输入视为新候选，必须重新 decode、validate、policy 和 sandbox plan，不能继承旧批准。

### 5.6 SandboxPlanner

SandboxPlanner 的目标是把验证后的调用转换为实际执行能力计划，Approval 只能接受或拒绝，不能扩大计划。当前仅实现 Exec 专用的纯内存 `SandboxPlanner`：冻结经过 descriptor 校验的 workspace、相对 workdir、读写范围、临时/缓存范围、禁止网络的 profile revision 与 candidate fingerprint；Darwin adapter 将其渲染为系统 sandbox profile。跨能力、可配置的通用 planner 尚未实现。

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

#### 当前前台 Exec 契约

- 输入：合法 UTF-8 command 至多 64 KiB；workdir 是 workspace 内相对目录；timeout 默认 120,000 ms，范围 1,000–600,000 ms。固定非交互、non-login `/bin/sh`，不提供任意 shell、PTY、stdin 写入或后台 handle。
- 权限：完整命令、相对目录、timeout 与能力摘要一起展示；仅 `allow_once/deny/abort`，不继承 Patch session grant。三种 headless 模式均拒绝需要交互的命令，不扩张外部 JSONL 事件词汇。
- 环境：app 显式读取一次宿主环境，只保留 `PATH/LANG/LC_ALL/LC_CTYPE/LC_MESSAGES/LC_NUMERIC`，并确定覆盖 `NO_COLOR/TERM/PAGER/GIT_PAGER/GIT_TERMINAL_PROMPT`。Darwin 将 `HOME/TMPDIR` 固定到 workspace、`GOCACHE` 固定到 workspace 下 `.cache/go-build`；其他宿主变量（包括 API、Authorization、Cookie、代理和内部变量）不继承。
- Sandbox：workdir 检查绑定实际 fd，系统只读资源与 workspace 可读，写入限定 workspace 和 `/dev/null`，网络禁用。系统 sandbox 不可用或初始化失败时失败关闭；初始化 stderr 不进入用户输出或 progress。
- Owner：启动屏障保证退出观察建立后才运行命令；先非回收地观察 leader，再处理原进程组，最后唯一 `Wait`。取消/timeout 使用 TERM、250 ms 宽限、必要时 KILL、有界组收口及 1 s 输出排空；最终回收后不再向旧 PID/PGID 探测或发信号。阻塞的最终 Wait 仍由原 owner 持有，不能靠提前返回释放下一 turn。
- 限制：保证原进程组清理，不承诺任意进程树或后台任务管理。`setsid`/脱离原组的后代可能继续存在，但仍继承 sandbox；保留输出管道导致排空超时则返回 `outcome_uncertain/output_cleanup_incomplete` 并 poison 当前 Runtime。关闭输出后脱离的进程不能据此检测，前台成功不等于任意后代已终止。
- 输出：stdout/stderr 各保留 512 KiB 确定性头尾窗口，模型 preview 至多 256 KiB，固定 stdout 后 stderr 并保证 UTF-8；中间省略内容不另存 artifact。Session 冻结 preview/字节数/截断和退出 metadata，恢复不重新读取或截断。
- TUI：普通命令摘要最多两行、160 code points；运行态只显示有界近期输出。`Ctrl+O` 打开可滚动详情，完整命令复用批准信息，终态预览来自 result durable 后的 typed terminal payload；进度不进 Session。`Ctrl+C` 定向中断后保持取消中，直到 cleanup 与 turn terminal 完成才恢复输入。

该切片选择 Exec 优先于整文件 Write，是为了补齐已有“读取/搜索 → Patch → 执行验证 → 总结”的真实闭环。Claude Code 2.1.88 的 Bash 批准、默认 No、命令摘要/展开详情指导产品交互；Codex `c248f6d48b` 的 unified_exec、Seatbelt 与进程 owner 分层指导内核边界。两者均只读参考：不引入 Claude Code 的流内提前副作用，也不提前复制 Codex 的 PTY/后台句柄；当前保留双 Provider 原生 call/output 语义，并以 durable started 作为执行接收点。具体证据和范围取舍见 `add-command-execution` 的 proposal/design。

回归入口：`internal/tool/builtin/exec_darwin_test.go`、`exec_detached_darwin_test.go`、`process_execution_test.go`、`internal/runtime/exec_fault_test.go`、`internal/session/exec_writer_test.go`、`exec_fixture_test.go`、`internal/app/exec_interactive_darwin_test.go`、`exec_failures_darwin_test.go` 和 `internal/tui/command_detail_test.go`。Darwin 已执行真实命令及 race 测试；Linux/Windows 已交叉构建且共享 unsupported adapter 在本机经过行为测试，尚未进行两平台原生运行测试。

## 10. 结果预算与 Artifact

每个 capability 选择显式预算策略，而不是全局粗暴截断：

- Read 优先保留请求行范围和定位信息。
- Grep/Glob 保留稳定排序后的前后命中，并报告省略计数。
- Exec 可保留头尾窗口、退出码和 stderr 摘要。
- Patch/Write 返回确定性 diff/摘要，不回显整个文件。

未来 artifact change 落地后，完整结果超限时的目标流程是：

1. 先把完整结果按策略保存为权限受控 artifact。
2. 生成确定性的模型预览和替换原因。
3. 将“模型实际收到的预览字节 + artifact descriptor + 截断元数据”写入 durable fact。
4. 恢复时复用已保存预览，不根据新预算重新截断。

当前工具均不创建 artifact：搜索工具持久化最多 64 KiB 的确定性模型预览，Exec 持久化最多 256 KiB 的预览及 typed metadata；恢复复用冻结字节而不重搜或重执行。省略内容当前不可恢复，UI 必须明确截断。artifact 后续须先定义受控存储、预算、清理所有权和失败语义；它不是 Session 事实源，JSONL 只保存 descriptor、摘要和完整性信息，SQLite 仍只做可重建索引。

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
| 模型预览字节与截断元数据 | 是 | 可投影 | 恢复必须精确复用 |
| UI 展开、焦点、spinner | 否 | 本地状态 | 不进入协议与缓存 |

当前 Session envelope canary 为 `3`，仍只有一个 reader。`tool_approval_decided` 保存 typed `decision_source` 和安全展示 metadata（capability、相对目标、Exec timeout），不保存 PID、绝对路径、宿主环境、输出 chunk 或底层 cause。Exec 恢复时，未决定批准为 `session_interrupted_before_approval`，allow 但未 started 为 `session_interrupted_before_execution`，started 无结果为 `outcome_uncertain`；均不重试命令。

稳定发布前新增或修改 record kind 时，直接替换唯一 current fixture，并让旧开发 shape fail closed。稳定发布后若引入真实 revision，必须同时提交 typed constructor、strict decoder、validator、不可变历史 fixture、兼容窗口和 replay migration test；既有 append-only records 不得在 resume 时原地改写。

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

- Read、Glob、Grep：已实现强类型输入/结果、双 Provider facade、Unix 安全 executor、固定只读策略和最多 8 路有序并行。
- Edit/structured patch 与 OpenAI `apply_patch` freeform facade：已实现单文件 Patch、批准及 Unix 原子替换。
- Exec：已实现 Darwin 前台执行、单次批准、sandbox、取消/timeout、输出预算、进度与详情；非 Darwin 失败关闭。
- Write、`write_stdin`、PTY、后台进程控制：待各自定义输入、授权和资源所有权后实现。
- allow/ask/deny、approval：已实现 Patch/Exec 闭环；SandboxPlan 当前只覆盖 Exec，不等于通用策略引擎。
- ledger 与配对恢复：五项能力均已实现；通用结果预算和 artifact 仍未实现。
- 工具输出后的 sampling safe point 已存在；same-turn steer 尚未接入。

### 13.2 后续阶段能力

- Skill：属于 P6 的上下文与指令装载能力，不在 P3 提前伪装成可执行工具。
- MCP tools：属于 P7 的远程 catalog、trust、认证和动态 schema 能力。
- Agent/Subagent：属于 P7 的 thread、budget、completion envelope 和取消传播能力。
- hooks/plugins：属于 P6，不能借 Tool callback 绕过版本化 manifest 和权限边界。
- 完整跨平台 sandbox 矩阵：P8；P3 只实现已验证平台能力并对其他平台失败关闭。

## 14. 推荐的 OpenSpec 纵向切片

下列名称是纵向切片；批准与 Patch 已合并交付，Exec 为补齐测试验证闭环先于 Write 实现。后续项仍须单独提出。

| 顺序 | 推荐 change | 主要闭环 | 明确排除 |
| --- | --- | --- | --- |
| 1（已实现） | `add-read-tool-loop` | 单个 Read 的 catalog、双 Provider wire、durable ready/result、ledger、一个多 sample 回合 | 并发、写入、shell、通用动态 registry |
| 2（已实现） | `add-search-tools-and-ordered-parallelism` | Glob/Grep、连续只读并发、按调用顺序提交 | 写工具和复杂调度 DAG |
| 3–4（已实现） | `add-approved-file-patch-flow` | allow/ask/deny、headless 失败关闭、完整 diff、双 Provider Patch、base evidence、原子替换与恢复 | diff artifact、任意文件写入 facade |
| 5 | `add-file-write-tool` | 新文件/整文件写入、原子替换、权限与冲突策略 | 命令执行 |
| 6（已实现） | `add-command-execution` | Darwin 前台 exec、单次批准、进程 owner、取消、超时、输出预算、sandbox 与 TUI 详情 | PTY、后台控制、artifact、非 Darwin 执行 |
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
| 进程安全 | 启动失败、取消、阻塞 stdout、原进程组清理、脱离组限制、shutdown timeout 和唯一 owner |
| 结果 | 工具特定截断、artifact 失败、secret redaction、恢复复用相同模型预览字节 |
| Session | 不可变 fixture、尾部半行、lease 竞争、repair、live/restored 下一请求等价 |
| 宿主 | headless approval 失败关闭、TUI 固定尺寸 snapshot、RuntimeEvent 不改变请求事实 |

任何实现完成前必须通过 `make verify`，并核对 OpenSpec、总体架构、Roadmap、踩坑记录和实际 record vocabulary 一致。

## 16. 尚需由具体 Change 冻结的参数

当前已冻结每 turn 最多 16 个 sample、64 个 tool call、最多 8 路只读并行，以及搜索 64 KiB preview、walker 深度 64/目录项 200,000、Grep 50,000 文件/256 MiB 累计扫描预算。Exec 的输入、超时、输出、环境与 Darwin sandbox 预算见 9.3。以下尚未实现的参数由首次消费者所在 change 给出默认值、配置面和测试：

- Write、交互/后台进程的输入、模型预览，以及通用 artifact 和累计上下文预算。
- approval 规则的持久范围和过期方式。
- Darwin 之外的命令 sandbox capability matrix 与原生平台验证。
- Read/Patch/Write 的编码、换行、权限保留、冲突和二进制文件策略。
- 可配置 shell/argv、网络授权、CPU/内存配额及任意后代进程 containment。
- `outcome_uncertain` 的用户恢复命令与宿主交互。

在对应 change 批准前，不得通过隐藏默认、占位配置或未消费的 event kind 抢先实现这些选择。
