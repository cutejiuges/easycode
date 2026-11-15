## Context

本文件记录已确认方案；实现及后续复审状态以 tasks.md、verification.md 和 review.md 为准。动机与已确认范围见 [proposal.md](proposal.md)。调研基于 EasyCode `8492ec25c489605f7ef6a3b42f15b86842f8717e`，能力基线来自 `docs/roadmap/capability-status.md`、Roadmap、主规格及实际源码，而不是目录中的占位类型。

### 现有实现与接入点

| 现有位置 | 已观察到的行为 | 本次接入方式 |
| --- | --- | --- |
| `internal/domain/inference_settings.go` | 不可变双 Provider 设置、独立枚举、严格组合校验、确定性 RequestPolicyIdentity | 复用原值对象，以纯函数只替换 effort |
| `internal/config/inference.go` | 最终 family 与上下文预留的组合校验 | 将可共享的纯预算校验放到 context 边界，启动和运行中更新共用；Runtime 不依赖 config |
| `internal/context/planning.go` | ProviderProfile 绑定 InferenceSettings 并生成稳定 profile 身份 | 从本次操作设置派生 profile，保持与实际 wire 同源 |
| `internal/runtime/agent_loop.go` | 单 owner 接收、request 去重、FIFO、唯一 turn worker、接收后等待确定结果 | owner 持有后续选择，启动 worker 前按值捕获策略 |
| `internal/runtime/input_commit.go` | 输入候选编码与预算预检通过 samplingInput 统一装配 | 将相同操作策略绑定到候选和真实采样 |
| `internal/runtime/compaction_loop.go` | idle 时先纯预检，再接纳独立手动摘要并启动唯一 worker | 接纳时捕获设置，不在执行时重新读选择 |
| 两家 `provider/*/user_input.go` | samplingRequestPlan 当前从 Provider config.Inference 读取策略 | 改为读取采样输入携带的有效设置，保留原生编译器 |
| 两家 `provider/*/request_evidence.go` | 同一用途计划产生逻辑输入、properties、receipt | 继续从同一计划生成证据，不另建 effort 编码器 |
| `internal/provider/openai/websocket.go` | baseline 比较全部请求属性；不相同时 offset=0 且不携带旧 response ID | 复用属性比较，不在控制 owner 中关闭 socket |
| `internal/tui/inference.go` | `/context` 设置由 app 注入，当前显示 frozen at startup | 不变字段保持启动快照；effort 改用 owner 的权威快照 |
| `internal/headless/context_event.go` | 外部 context.snapshot 当前直接持有内部 ContextSnapshotPayload | 明确投影已有数值部分，避免内部新增设置泄漏到既有外部 shape |
| `internal/extension/skill/local.go`、`internal/tui/completion.go` | 保留 slash 名称的 switch 与六项内置补全 | 增加 effort，仍通过原 Skill 路径调用 `$effort` |

### 两个参考工程的实际源码依据

参考工程只读，不构建、不修改、不加入依赖，也不复制大段实现。当前 worktree 的 `../` 没有参考目录，本次从主 checkout 的邻居定位到以下实际根；快照与旧架构文档记录不同，以本次核对为准。

| 工程 | 实际位置与快照 |
| --- | --- |
| Claude Code | `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap`；Git `a8a678cb6244e6770e1e421767ff0987a1d95549`；`package/package.json` 确认 `2.1.88`；还原源码根 `restored-src/src` |
| Codex | `/Users/cutejiuge/IdeaProjects/codex`；Git `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`；以下路径相对该根 |

| 参考源码 | 实际观察 | 采用与取舍 |
| --- | --- | --- |
| Claude `commands/effort/index.ts`、`commands/effort/effort.tsx:16,62,107` | `/effort` 是本地命令；无参数查看，有参数修改，auto 清除覆盖；后续可能计算模型默认值；部分值保存用户配置 | 沿用命令形式和简短反馈；本次 auto 明确省略字段，reset 单独恢复启动值，全部为进程选择，不宣称与模型默认派发等价 |
| Claude `utils/immediateCommand.ts:10`、`query.ts:694` | 运行中推理配置命令的立即执行受实验开关控制；query 从 appState 取 effort | 不把实验条件当作固定产品契约；EasyCode 明确允许立即接受后续选择，但完整当前回合冻结 |
| Claude `utils/effort.ts:152` | 应用环境覆盖和模型默认，并可能将 max 降为 high | 保留本地显式值；不引入 Claude 环境变量、账号判断、模型名猜测或自动降档 |
| Codex `codex-rs/tui/src/chatwidget/reasoning_shortcuts.rs:99,145,161` | 展示选择与可用档位分开，通过 AppEvent 更新；档位依赖模型元数据 | 采用 UI 只投递类型化选择的边界；EasyCode 没有已实现的模型目录，只声明本地合法枚举，不增加快捷键或远端探测 |
| Codex `codex-rs/core/src/session/reasoning_effort.rs:30,96` | 区分用户选择、上下文窗口的 request effort pin、受信任 configuration_update；摘要失败不改变普通 pin | 吸收设置/请求/缓存基线分离和失败不改基线；不引入尚未实现的 configuration_update 原生项或专有 wire |
| Codex `codex-rs/core/tests/suite/reasoning_effort_override.rs:739,900,1018,1239,1596,1656` | 覆盖无变化不重复更新、WS 基线/重连、不支持更新的请求路径、恢复和摘要取值 | 采用相应验证维度，使用 EasyCode 自有 local server、native fixture 和生命周期门闩重新实现测试 |

### 二次源码审查与方案修订

本次继续追踪命令回调、模型循环、摘要调用、设置发布与宿主回执，没有只核对命令名称。参考快照保持上表固定提交；下列修订均落在现有六项能力内，不新增文件、存储结构或实现代码。

| 原方案遗漏或不足 | 本次核对的源码依据 | 修订决定与验证位置 |
| --- | --- | --- |
| 把快捷 `/effort` 设计成新面板，未说明 current/status、unset 和帮助行为 | Claude `commands/effort/effort.tsx:107,171` 使用 onDone 返回本地反馈，支持 current/status、unset、help/-h/--help | 快捷入口改为有界、无独占焦点的短反馈；详细值继续用 `/context`；TUI 规格及任务第 5 组覆盖 |
| 未充分区分选择、操作捕获和实际发送，可能把 accepted 标成已应用 | Codex `core/src/session/step_settings.rs:36` 保留 selected 与 effective 值；`tui/src/app/thread_settings.rs:240` 单独处理服务端设置回执 | 快照只陈述选定值及操作捕获值；没有请求事实时不显示已发送/远端接受；回执和短反馈单一消费 |
| 启动后、durable started 前没有 TurnID，原活动快照无法构造 | EasyCode `agent_loop.go` 的 startTurn/handleTurnEvent 与 `runtime.go` 的 beginTurn | 增加封闭 turn_starting 分支，只关联既有 request_id；收到 durable started 后才使用 TurnID；暂停 started Sync 时查询必须可用 |
| 收到 terminal 后，资源仍可能尚未清理 | EasyCode handleTurnEvent 只设 terminalSeen，handleTurnFinished 才释放 active | 增加 finishing 阶段与 cancel_requested；直到原 owner 完成清理才清除捕获策略，禁止提前按新选择伪造 idle |
| 未定义局部请求新旧与权威选择顺序的关系 | EasyCode queryRequestID 管理局部请求，tea.Cmd 与 ControlItem 各有返回；Codex 设置通知按 thread 路由权威值 | 保留 FIFO 与请求去重，明确成功单一消费；增加 process 选择序号作为机器观测/缓存语义标记，不声称现有输出通道发生乱序 |
| 没有定义状态已更新但快照编码/观测失败时的结果 | Codex `core/src/session/mod.rs:2162` 先验证候选再发布，拒绝无设置影响 | 更新前准备候选、控制结果和设置快照；context/cache 观测是独立失败域，不撤回已接受选择，不以旧数值冒充新 profile |
| 对参考工程的“回合冻结”和“缓存保留”容易过度概括 | Claude `query.ts:307,570,694` 每次循环重新取 appState；Codex effort override 测试 `:739` 保留 request pin，`:1239` 普通路径直接改变请求 effort | 明确选用 EasyCode 完整 turn 冻结与普通请求属性路径；专有 pin/update 不在已实现 wire 内，不能宣传相同缓存收益 |
| 未记录摘要路径的重要差异 | Claude `services/compact/compact.ts:1305,1324` 禁用 thinking、仍传 effortValue；Codex summary 使用独立 request-effort 路径 | EasyCode 继续遵守已有 thinking/display/effort 冻结与独立摘要额度；不借鉴关闭 thinking 或隐式降低 effort |
| 把 auto 的名称一致误当成发送语义一致 | Claude `utils/effort.ts:152,279` 的 resolveAppliedEffort 在覆盖清除后仍可计算并发送模型默认值 | EasyCode 没有模型目录，auto 始终只省略字段，reset 才恢复启动值；OpenAI 显式 none 也不等于 auto |
| no-op 缓存承诺未充分限定，缺少失败/重连后的边界 | 两家 RequestBaseline、OpenAI startRequest 与 Runtime 失败时 ResetContinuation | 只承诺未启动中间操作的同值选择不重置；操作失败、取消和摘要仍依现有生命周期处理；比较 fingerprint 时固定同一内容输入 |

本切片与参考产品的明确差异：Claude 部分值持久化、可按模型降档，并可在 auto 下发送其计算的模型默认值；EasyCode 全部为进程选择，保留显式值，auto 明确省略字段。Claude 在启用立即配置命令后可在后续循环读取新选择，EasyCode 以整个 turn 为冻结单位；Codex 专有更新路径可以在保持 request effort pin 时追加受信任原生项，EasyCode 使用当前已实现的普通原生请求字段。上述差异是本阶段明确取舍，不把尚未支持的 wire、模型目录、配置保存或同轮切换描述为等价实现。

### 本阶段能力选择

| 候选 | 当前前置条件及收益 | 决定 |
| --- | --- | --- |
| 会话内 effort | 启动配置、双 Provider 编译、profile、缓存、恢复及 composer 均已具备；可直接改善复杂任务与简单任务间的切换 | 本次推进，形成独立 P5/P1 闭环 |
| MCP 首个切片 | 需先建立 server owner、发现、工具结果、权限/信任和动态目录契约 | 后续独立变更 |
| 后台进程/Subagent | 当前 Exec 仅前台，child-thread 与 mailbox 仍是占位；需要新的资源、线程和恢复生命周期 | 后续 P3/P7 变更 |
| 跨 Provider fork | 需要新 thread 来源和 compacted fork 的存储/入口；不能转换 opaque 历史 | 后续 P4 变更 |
| 更多 Hook 事件 | 已有四类 Hook，而所有工具任务仍缺少运行中 effort 入口 | 本轮先完成通用交互缺口 |

## Goals / Non-Goals

**Goals:**

- 使用现有不可变设置和单 owner，将一次选择准确贯穿预检、请求、证据、查询和缓存，而不是只修改界面文字。
- 让用户准确区分活动请求、下一操作选择和服务端实际返回；两家各自保留原生枚举与 opaque 语义。
- 在不增加持久化结构、执行资源或后台 owner 的前提下完成可测试闭环。

**Non-Goals:**

- 不动态修改 thinking/display/summary、输出额度、模型、Provider、窗口或缓存配置；不探测模型、不添加配置文件保存。
- 不实现 Codex 专有 effort update item、上下文 effort pin、模型目录或新快捷键；不改变 Skill frontmatter 对 effort 等未支持行为字段的拒绝。
- 不增加模型工具、迁移、兼容 reader、内部格式版本、自动重试或新的并行 worker。

## Decisions

### 1. 采用三分支不可变选择，而不是直接修改 Provider 配置

在 domain 增加有真实消费者的封闭 effort 选择值：reset、auto、value。value 只接受已有两家枚举的有限并集，再按启动设置的 family 和组合规则验证。使用专属构造器、validator 和按值返回的解析结果；不接受 any，不建立包级可变枚举列表。

解析使用启动 InferenceSettings 创建完整新副本，只替换对应 effort。reset 重新取启动值；auto 只清除 effort，不删除 OpenAI summary 或 Anthropic thinking/display。已有 domain 构造器继续校验 family、manual budget 和 disabled/xhigh/max 关系。context 中提取现有纯预算组合校验供 config 和 runtime 复用，保持依赖方向；不把配置文件读取搬入 Runtime。

副本必须保留 output 的显式/缺省来源，不能将 MaxOutputTokens 的有效默认值反填成显式配置；仅复用固定窗口/预留的组合校验，不把当前历史过大、估算 unknown 或观测失败误当作 effort 选择非法。这些动态条件仍在后续真实操作的预检中处理。

在 owner 改状态前完成全部纯校验。已知公共档位不适用于当前 family 返回 `invalid_effort`，与冻结配置或预算冲突返回 `effort_conflict`。对外使用固定英文摘要，不回显原始参数。远端拒绝仍走已有 Provider typed failure；不会自动回退设置或重发。

替代方案是直接给 Conversation 加 setter；它会令输入预检闭包、摘要预检与真实发送读取不同设置，并与 stream/WS owner 竞争，故不采用。

### 2. 单 owner 保存未来选择，worker 捕获完整操作策略

owner 的有限状态为不可变启动设置、最新选择、解析设置及 selection_revision；活动 turn/手动摘要保存设置和序号副本。内存保持 O(1)，request 去重沿用既有有界 ledger；不为每次设置创建队列 obligation 或 goroutine。

| 时刻 | 操作 |
| --- | --- |
| effort.set 被 owner 接收 | 纯内存准备完整候选、结果及快照，通过后原子替换后续选择；不读写 Provider 活动状态 |
| 新 turn worker 启动前 | 捕获最新完整策略，供该 turn 生命周期使用 |
| worker 已启动、durable started 未到达 | 以 turn_starting/request_id 表示捕获策略，不制造 TurnID 或发送事实 |
| 活动 turn 内工具、自动输入、Stop、自动摘要 | 始终使用捕获值，即使后续选择变化 |
| 独立手动摘要纯预检及接纳 | 捕获最新策略并存入 admission/preview，之后不重新解析 |
| 收到普通 terminal | 只进入 finishing；保留当前捕获策略，仍等待 stream/worker 最终清理 |
| worker 完成并清理 | 清除活动身份，以后续选择刷新 idle 查询；再按原 FIFO 启动工作 |

```text
owner: selected=high --> accept low --> selected=low --> start B
                            |                           |
turn A: policy=high --> tools/Stop/summary=high --> done |
                                                        v
                                               turn B: policy=low
```

已经 queued 的 strict-next 在真正启动时取最新设置；auto 如果被当前 turn 的安全点领取，仍使用当前策略。选择与输入并发时由同一 owner 排序，而不是用调用时间或 TUI 推断先后。

接收前取消保持状态不变；接收后沿用 AgentLoop.Submit 的确定结果等待。closing/draining/poisoned 拒绝新更新。不得通过更改 effort 清零自动摘要来源抑制或三次熔断，也不得改变工具/Stop 的 sample、调用与输出预算。

接收是既有 channel handoff 线性化点；取消与接收竞争时由实际 handoff 决定，不依墙钟推断顺序。更新前必须构造并校验 accepted result、16 KiB 快照和关联 ControlItem；构造/编码失败保持旧选择及旧序号，以安全错误拒绝。接收成功后，UI 关闭、调用方取消或 stdout 失败不能回滚选择，也不产生持久化义务；原 owner 继续完成已有工作和关闭。

### 3. 以显式采样输入传递策略，保持预检与 wire 同源

将唯一当前 TurnInput 调整为显式持有有效 InferenceSettings，构造时校验，禁止零值或“没带就回读 config”的分支。所有真实采样、候选预检、Stop 交付、摘要与测试调用点同批更新。Provider 配置仍作为启动装配信息，但普通/摘要请求编译只读取本次输入的策略。

Runtime 保留启动 profile 作为不可变基础；由 worker 捕获策略纯内存派生操作 profile，并与采样输入一起用于规划。不得让 owner 在活动 worker 期间改写 runtime.profile 或 Provider config。可在现有 worker 生命周期中持有小型操作值，所有 helper 取同一值，并在退出时清理；不创建通用策略管理器。

两家 samplingRequestPlan 继续分别负责原生字段；输入候选预检闭包已有 TurnInput 参数，直接复用。CompactionInput 已携带 sampling，天然携带冻结设置；Anthropic 的 manual 摘要预算检查也从该输入读取。body、properties、input fingerprint、receipt 均从同一计划生成，避免另走设置序列化路径。

直接调用 Runtime 单 turn 的已有明确生命周期仍使用其启动设置，并显式构造相同操作值；它不是缺失策略时的 fallback。所有产品宿主继续通过 AgentLoop，不增加动态直接执行通道。Capabilities 中 summary 等仍由未改变的启动字段决定，不新增能力位。选择序号只留在 owner/宿主观测，不进入 Provider 编译、prompt 或请求身份。

### 4. 使用已有缓存属性比较，而不是设置命令触发资源重置

在固定相同 native 窗口和候选输入的对照中，effort 变化只改变对应 RequestPolicyIdentity、ProviderProfile 和请求属性；工具、项目指令、Skill 与 native 内容身份不变。正常新 turn 仍会追加真实输入，不能要求两个不同 turn 的完整 native fingerprint 相等。缓存诊断将实际策略变化定位到 provider_profile；来源、选择序号、查询和 UI 状态不进入稳定前缀。

OpenAI baseline 已比较完整 properties。下一请求不同则 full/offset=0，不携带旧 previous_response_id；后续同策略样本成功 Sync/finalize 后正常建立增量基线。控制 owner 不调用 ResetContinuation/Close，socket 仍由原 worker 管理。相同 wire 选择，或没有任何中间操作启动的 high→low→high，不能制造额外重置。若中间 low 操作已经开始、失败或取消，继续遵守既有失败清理和连接重置规则，不能靠切回 high 恢复旧连接资格。摘要沿用既有独立 full 连接和用途规则，失败不建立普通 baseline；不承诺失败摘要保留旧 socket。

Anthropic 原生 cache marker/TTL 编译路径保持不变；不能把相同输入或 fingerprint 解释成服务端保证命中。两家旧默认 golden 必须逐字节保持。

### 5. 查询快照明确表达当前与后续状态

增加类型化 EffortSnapshot，最大编码 16 KiB，字段边界如下；字段名称在实现时与此方案保持一致并由固定 golden 锁定。

| 字段 | 含义及约束 |
| --- | --- |
| scope | 固定 process，不表示 durable 设置 |
| family | 当前 Provider，所有 value 按该 family 验证 |
| startup、selected | `{state: omitted}` 或 `{state: explicit, value: <level>}`；不能用 null/空串代替省略 |
| source | startup/runtime/auto；reset 恢复 startup 来源 |
| selection_revision | 从 0 开始的 process 内 uint64 选择序号；不属于 Session revision 或格式版本 |
| activity | idle/turn/compacting，沿用现有操作状态 |
| active | idle 时缺省；活动时为下述封闭联合，必需 effort、selection_revision、cancel_requested 与 phase |
| pending | 存在活动操作且选定与活动 wire 值不同才为 true |
| allowed_levels | 按 Provider 固定次序排列、最多七项，通过本地组合校验的档位；不作为远端能力声明 |

active 的唯一当前分支如下；缺少 ID 是明确的启动状态，不是可选旧格式：

| kind | 身份 | 允许的 phase |
| --- | --- | --- |
| turn_starting | 必需发起输入原有 request_id，禁止 turn_id/compaction_id | starting；启动失败终态已到达但尚未 join 时为 finishing |
| turn | 必需已发布 durable started 的 turn_id，禁止其他分支身份 | running/finishing；自动摘要仍为该分支且 activity=compacting |
| manual_compaction | 必需已接纳的 compaction_id，禁止 turn_id/request_id | starting/running/finishing，分别以真实 started/terminal/cleanup 观察推进 |

每个分支有专属构造、strict decoder 和 validator，不能用 nullable IDs 或 any 表达。cancel_requested 只说明已请求取消，不说明清理已完成；active.selection_revision 必须不大于外层选择序号。启动失败即使生成过一个错误 TurnID，也不能把它当成 durable started 身份。terminal 到达只推进 finishing，worker 最终清理后才移除 active。

序号只在选择分支、档位或来源发生实际变化时递增；同样选择的新 request 仍 accepted，但不递增。startup high→显式 high 会递增且 pending=false；任何未启动中间操作的 high→low→high 都不能因此重置缓存。序号禁止 wrap；达到上限时新变化以既有 invalid_command 拒绝，同值查询/幂等选择和正常关闭仍可用。序号不进入 wire、profile、receipt 或 journal。

保留原有单 owner FIFO，不为序号新增重排序、多播或补发通道。只有 request/generation 也能保护单次 UI 交互，但无法独立表达已选择值与活动捕获值经历过多少次来源/选择变化；该小型序号供当前机器快照和本地权威缓存使用，不能替代请求身份，也不表示服务端应用次数。对于失效局部请求，至多安装较新的 selected/source/selection_revision，不安装其中过期的 active、数值、草稿或通知。

owner 依据自身活动身份和捕获策略合成快照；不访问活动 Conversation。活动窗口数值从 worker 发布的最近 committed 快照读取，idle 时按选定 profile 做纯规划；启动期间必须使用捕获策略对应的已有数值快照，不能用旧 profile 数值冒充新 profile。context 查询返回数值及 effort 的同一次 owner 视图，不拼接两个异步查询。窗口估算始终是 ordinary committed context，不代表正在发送的摘要输入、流式 staging 或已计费 usage。

设置验证/发布与 context/cache 观测分离：有效选择不依赖动态历史是否可估算。若纯观测重建失败，保留已接受选择及 effort.query 可用性，记录相应 contextSnapshotErr；context 查询按既有失败规则拒绝，不沿用错误 profile 的旧数值，也不能把成功 set 改报为 rejected。

TUI 独立处理权威选择缓存与局部交互：低于已见 selection_revision 的回复不能回退选择；同序号的活动阶段仍按 request/generation 和当前操作身份校验。旧局部请求即使承载新选择，也不能重新显示已结束通知、清除新草稿或抢占新面板。关闭已接受命令的反馈不会撤回 owner 选择；下一次查询仍能看到它。

内部 ContextSnapshotPayload 组合数值 payload 与 effort 状态。将已有数值字段抽成可验证的具名值供内部与 headless 复用，headless 的既有 context.snapshot 只映射该数值值。内部严格 decoder 只接受唯一新结构，更新全部消费者；不留旧 reader 或 optional fallback。这样保留外部字段，而不靠删除 map key 或直接序列化增强后的内部事件。summary/thinking 等不可变字段仍可由 app 启动快照供 TUI 展示。

### 6. 产品交互沿用本地 slash 和现有焦点模型

| 输入 | 行为 |
| --- | --- |
| `/effort`、`/effort current`、`/effort status` | 相同的本地查询；短反馈显示操作捕获值、后续选择、来源和生效边界，不新增面板 |
| `/effort high` 等 | 经 owner 设置，短反馈说明生效时点 |
| `/effort auto`、`/effort unset` | 相同的省略选择；后续操作不发送 effort，服务端默认未知 |
| `/effort reset` | 后续操作恢复启动值，不写配置文件 |
| `/effort help`、`/effort -h`、`/effort --help` | 本地静态用法和 auto/reset 区别，不提交控制命令、不改设置；合法档位以 /effort 的权威查询为准 |

参数归一化只在文本边界进行，模型无关逻辑使用封闭枚举；命令名仍精确匹配小写 `/effort`。参数错误、组合冲突保留原始草稿/光标/历史。control.response 表示接收，匹配的权威 snapshot 才供安装数值和成功反馈；tea.Cmd 只投递调用错误，成功返回值不能成为第二条 UI 更新通道。启动中显示 captured/starting，不说已发送；运行中显示类似 `Next effort: low; current turn keeps high` 的短反馈；清理中明确 finishing，不能暗示还会用新值续写旧回合。

短反馈只保留最近一份：状态最多两行、帮助最多六行，空间不足优先 composer 并裁剪/隐藏；不增长 transcript、不新增持久化消息。显示不拥有焦点，Esc/Ctrl+C 保持原有输入/取消优先级；下一次显式编辑、提交或其他本地命令清除旧通知，流式增量和 resize 不重复弹出。所有操作继续复用既有短暂提交与草稿暂存，异步结果只处理匹配的请求/草稿 generation，不覆盖后来编辑的输入。已出现的 Question/审批/Hook/详情优先，迟到结果不能关闭它们。

详细来源、阶段与额度留在既有 `/context`；复用其布局、generation、request identity 和迟到响应规则，不另建通用通知框架或命令 registry。帮助不沿用“low 减少测试、high 增加测试”等描述，effort 不改变 AGENTS 质量门。View 无 I/O；新增补全仍仅为首词，不扩展参数候选或快捷键。内置上限为 256+7，保留 `$effort`、`/skills` 和限定插件技能的原执行路径。所有别名仍只映射到原 query/auto/help 行为，不新增 runtime kind 或模型能力。

### 7. stream-json 使用专属命令与明确投影

外部仍为独立 v1，新增以下当前命令，不把版本字段带入进程内命令：

```json
{"version":1,"type":"effort.query","request_id":"effort-query-1"}
{"version":1,"type":"effort.set","request_id":"effort-set-1","mode":"value","level":"high"}
{"version":1,"type":"effort.set","request_id":"effort-set-2","mode":"auto"}
{"version":1,"type":"effort.set","request_id":"effort-set-3","mode":"reset"}
```

mode 和 level 在外部协议中严格区分大小写；value 必需 level，auto/reset 禁止 level。TUI 别名不扩展外部 wire。新 effort 命令的完整编码行上限为 4 KiB；在类型判明后、专属 DTO 解码前执行该上限，既有 NDJSON reader 总上限不变。未知枚举、混合分支、null、重复键、未知字段、非法 UTF-8/identity 和超限输入按原协议失败路径处理。公共已知档位与当前 family 不符等运行条件在 owner 返回 rejected，不能把未成功解码的输入伪造成一次 control.response。

内部增加 query_effort/set_effort 的专属构造、validator、合法 result disposition 和 effort_snapshot 事件。成功查询 response(queried)、成功设置 response(updated) 后各输出一次关联 effort.snapshot；拒绝没有成功 snapshot。允许复用现有 correlated query ControlItem 的有界通道，但必须扩展 kind/payload 的封闭校验，不增加 any 或通用 JSON payload。

effort.snapshot 显式投影为外部安全 DTO，带 request_id、session_id、thread_id 与完整 snapshot；selection_revision 为必需无符号整数，active 使用上节封闭联合。仅显式新命令触发新帧；不分配新输入或操作 ID。一次性输出过滤内部设置事件，slash 及帮助文字在 input.submit/input.append 中仍为正文。响应背压或快照写出失败不回滚已接收选择、不补发/重试生成；沿既有单 writer、EOF drain、断管、shutdown 和最终清理路径执行，保持 lease 到原 owner 退出。

“模型网络等待期间可控制”不意味着绕过 stdout 背压：输出尚未排空时 handoff 可以等待并在接收前取消。不得添加第二个控制 goroutine 绕开顺序。宿主 detachment/输出失败必须继续消费并排空已有有限 ControlItem，以解除新增 response/snapshot 的背压，让既有 worker、dispatcher 和 lease 按原 owner 最终收口；测试明确填满输出缓冲后关闭，而不是以增加容量掩盖问题。

### 8. 进程作用域与原生恢复保持现有分工

不增加 Session/native/checkpoint record 或格式标记；启动 JSON 仍是重启后的配置来源。override 和 selection_revision 不跨重启恢复，TUI 和文档明确这一点。新进程序号从 0 开始，外部客户端在新 stdout stream 的 thread.started 后重置观测水位，不能跨进程比较；TUI 使用新 Model 的空状态。该序号是设置顺序，不是内部格式版本、native revision 或持久化去重身份。

恢复过程只做原有校验/补偿，不读取旧设置 UI、Skill 或摘要中的文字，不联网、不重跑工具/Hook。比较 uninterrupted/restored 时必须明确两边显式选择相同设置：request canonical bytes、native item 顺序、request/native/profile fingerprint 等价；OpenAI 恢复仍 full，而不是假装恢复旧 socket。不同设置的请求不要求等价，已有 opaque/signature/phase 和 journal 前缀仍逐字节保留。

### 9. 测试与验收矩阵

| 层次 | 必须验证的业务结果 | 实际证据入口 |
| --- | --- | --- |
| domain/context | 两家枚举、auto/reset、原值及显式/缺省输出来源不变；固定预算冲突与动态观测失败分开 | `internal/domain/effort_test.go`、`internal/context/inference_test.go`、`internal/config/reasoning_test.go` |
| protocol/headless decoder | 严格分支、4 KiB 命令/16 KiB 快照、必需整数选择序号、turn_starting/turn/manual_compaction、phase、取消与 pending 一致性 | `internal/protocol/effort_test.go`、`internal/headless/effort_test.go` 及各自 testdata |
| owner | 并发排序、接收前后取消、候选构造失败原子性、序号/source/no-op/上限、观测失败隔离、poison/关闭 | `internal/runtime/effort_test.go`、`effort_stream_test.go`，使用门闩/channels |
| 生命周期快照 | started Sync 暂停、启动失败、terminal 后清理暂停、取消未完成；查询使用真实可得身份且不误报发送/idle | `internal/runtime/effort_test.go` 的 started、finishing、cancel 回归 |
| 完整回合 | 首 sample、只读工具、mutation 审批、Question、自动追加、strict FIFO、Stop continued 均保持原 effort，下一回合取最新值 | `internal/runtime/effort_test.go`、`internal/app/effort_e2e_test.go` |
| 摘要 | manual 预检后更新不改变请求；auto/Stop 后摘要继承原回合；输出额度、失败抑制/三次熔断不重置 | `internal/runtime/effort_test.go` 的 manual/automatic/Stop 组合回归 |
| Provider golden | 无更新时旧默认 bytes 不变；新选择只改正确原生字段；body/receipt/profile 一致；auto 保留其他字段 | 两家 `internal/provider/<family>/effort_test.go`、`reasoning_request_test.go` |
| 缓存/WS | 固定内容对照；属性变化 full，未启动中间操作的同值/high→low→high 不重置；真实 B 失败/取消/摘要/重连不能恢复旧资格；序号不进入身份 | `internal/provider/openai/effort_websocket_test.go`、`internal/runtime/effort_websocket_test.go` |
| native/restore | 固定 current bytes 保留 opaque/pairing/checkpoint；新进程序号清零，重新选择相同设置但不同序号仍请求/指纹等价，零重跑 | `internal/app/effort_e2e_test.go` 的 RestoredRequest/Restart；两家 `reasoning_restore_test.go` 及固定 current fixture |
| app/CLI | 两家本地 SSE 服务完整循环；控制 query/set 不增加请求；400 一次失败不降级/重试；EOF/断管等待唯一 cleanup | `internal/app/effort_e2e_test.go` |
| TUI | 有界短通知/帮助及别名；80x24/28x10/20x5/1x1/NO_COLOR；迟到选择序号、结果单一消费、新草稿/overlay 保护、补全/保留 Skill | `internal/tui/effort_test.go`、`context_test.go`、`internal/extension/skill/local_test.go` 与固定 snapshot |
| 输出清理 | 满 ControlItem buffer、已接收 set 后 snapshot 短写/断管、host detach、EOF、shutdown；无回滚/重发/泄漏/提前释放 lease | `internal/runtime/effort_stream_test.go`、`internal/app/host_lease_test.go`、`effort_e2e_test.go` |
| 真实终端 | 双 Provider 流式中切换、下一轮生效、context 对照、非模态键路由、帮助/别名、auto/reset、队列叠加/取消/退出 | `scripts/effort-smoke.py`、`internal/app/effort_terminal_darwin_test.go`，本地服务与明确 ACK |

故障、取消、审批和生效时点采用确定性注入，不用 sleep 或随机竞态作为正确性证明。新固定 fixture 由仓库保存，不在测试运行时从当前 encoder 生成。只有新协议边界的 fuzz 需要有界执行；全量/race 及架构门通过后，无新变更不重复扩大验证。

完成前运行 `make verify`、本 change 和全部主规格严格校验；若执行 Linux/Windows 构建或静态检查，记录实际范围，不把交叉编译当作原生执行。真实 endpoint 不是默认测试依赖，使用 local server 和假凭据。验收记录在 apply 阶段生成，分别列出新增功能证据与既有基线；规划阶段的基线不能冒充功能通过。

## Risks / Trade-offs

- 当前回合较长时用户期望立即改变 → accepted 与 pending 明确说明下一回合生效，完整回合冻结避免工具续写策略不一致。
- 自定义 endpoint 不接受某个合法枚举 → 原值显式发送、单次安全失败；选择保留供查询，由用户明确 reset/auto，不猜测或隐式降档。
- UI 数值与 effort 来自不同时间 → 同一 owner 查询视图，活动数值使用 worker 已发布快照，idle 派生选定 profile。
- 启动阶段没有 durable TurnID 或终态后仍有清理 → 明确的 active 联合和 phase，身份只来自真实生命周期事实，直到 join 才切换 idle。
- 迟到回执回退选择或覆写草稿 → process 选择序号与 UI request/generation 分工，成功只经 ControlItem 消费，局部反馈结束不撤回 owner 状态。
- 数值观测不可用影响设置接收 → 候选发布前完整预检、发布后分离观测错误，设置查询不依赖 context/cache 数值。
- 改变策略破坏增量资格 → 继续使用完整逻辑 properties 比较，变更首请求 full，成功 durable 后才建立基线。
- 内部设置扩展泄漏进外部 context.snapshot → 内部组合、外部显式数值投影，旧外部 golden 固定。
- 重启后 override 不保留 → 进程作用域在交互、快照和 README 一致说明；不通过新增存储掩盖范围差异。
- 快捷反馈过重或遮挡输入 → 使用单份短通知，无独占面板；详细信息留给 `/context`，极小屏优先 composer。
- 新 slash 遮蔽同名 Skill → 复用保留名规则，明确测试 `$effort` 和 `/skills` 的选择结果，保留既有 256 项容量。
- 新 snapshot 增加输出背压 → 复用原单 owner/consumer 的 detachment 和排空路径，确定性验证满缓冲后关闭，不新增控制执行器。

## Migration Plan

本次只有唯一当前进程内契约调整，无数据迁移。实现必须一次更新所有采样/预检/快照消费者和固定协议 fixture，Session/native/checkpoint 不改写旧 journal，不增加版本或历史 reader。回退未发布变更使用普通代码回退；本地 override 本来就不持久化，无需数据回滚。

实现完成后同步受影响主规格、README、能力清单、架构与 Roadmap，重点将“effort 启动冻结”改为“启动默认、每完整操作冻结”，其余 thinking/display/summary/输出配置仍保持启动冻结；历史归档保留原有时间范围。只有本 change 验收完成后才能归档，不据此宣布整个 P5 已完成。
