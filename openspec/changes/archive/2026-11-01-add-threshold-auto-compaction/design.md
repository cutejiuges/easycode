## Context

动机见 proposal。已阅读 Roadmap P2/P3/P4/P5、总体架构、pitfall、context/compaction、tui/context-commands、runtime/chat-turn、config/json-file 与 headless/stream-control。当前活动 change 为 add-threshold-auto-compaction，实现与验证状态以 tasks.md 和 verification.md 为准。

现有落点：
- internal/runtime/runtime.go 的 runTurn 持有 active，commitInputs 后启动样本；工具输出提交后才 claim steering。
- internal/runtime/input_commit.go 的 prepared input 提供 Preview/CheckRequest，拒绝不写 native history。
- internal/runtime/compaction.go 的 runCompaction 自行取得 active；不能在 turn 内直接调用，否则自拒绝。其 durable start、候选缩减校验、Sync/finalize 和失败 provenance 可复用。
- internal/runtime/compaction_loop.go 仅为独立手动操作关联 RequestID；自动事件必须通过 turn worker 既有 publication 通道，不创建第二个 consumer。
- internal/context/planning.go 已有显式预算和 unknown；compaction.go 单独校验摘要真实输出预留。

## Goals / Non-Goals

目标是贯通候选输入、工具续写、安全窗口安装和三类宿主，保持原始 transcript、用户纠正和授权事实。策略只负责纯内存决策，Runtime 负责生命周期，Provider 负责原生编译。

不引入服务端 compact、reactive API 错误重试、局部裁剪、摘要时调用工具、模型切换、Hook 或后台候选。参考仓库只读，未修改或作为构建依赖。

## Decisions

### 1. 参考实现的取舍

参考根目录实际位于 /Users/cutejiuge/IdeaProjects，当前 worktree 的 ../ 下没有这两个仓库。

| 参考源码 | 观察到的行为 | 本方案采用与取舍 |
|---|---|---|
| claude-code-sourcemap/restored-src/src/services/compact/autoCompact.ts：getEffectiveContextWindowSize/getAutoCompactThreshold | 预留摘要输出和提前缓冲；支持关闭；连续失败熔断 | 采用提前留空间、开关及有界失败；不复制依赖模型目录的 13k/20k 常量、环境隐藏覆盖与全局状态 |
| 同文件 autoCompactIfNeeded | 无用户追问的自动摘要；独立失败计数；还含 SessionMemory/实验模式 | 采用无需追问的接续与连续三次失败熔断；query.ts 在失败后仍可继续采样。不引入未实现的 SessionMemory 或 feature flag |
| codex/codex-rs/core/src/session/turn.rs：run_auto_compact | 按 PreTurn/MidTurn/PostTurn 阶段执行；有本地与远程路径 | 采用输入前与工具续写阶段、内联执行；首版不在任务结束后自动摘要，只复用现有双 Provider 摘要路径 |
| codex/codex-rs/core/src/compact.rs：run_inline_auto_compact_task | 显式 Auto trigger、reason/phase，摘要输入独立于用户任务 | 保存 typed 触发来源及关联；不复制把摘要构造成 user message 的方式 |
| codex/codex-rs/core/src/state/auto_compact_window.rs | 窗口身份及服务观测/估算 baseline 分开 | 复用 EasyCode 已有 context revision；不把累计 usage 当窗口占用 |

相较 session picker，该能力直接保障正在进行的工具回合；picker 可继续复用已有 sessions list/--resume。相较扩展/Subagent，自动压缩已有真实消费者和持久化闭环，不需提前启用占位能力。

参考复核补充：Claude query.ts:453-545 在摘要失败时传递连续计数，未在该处分支结束普通采样；prompt.ts:337 的包装要求直接接续而不复述摘要。Codex session/turn.rs:628-739 仅在 needs_follow_up 时执行前台 MidTurn rollover，摘要错误结束本次工作。两者 compact.ts/compact.rs 都存在删旧历史后重试的路径，与本项目完整窗口契约冲突，不采用。Codex compact.rs 还保留用户消息；本项目首版只承诺未提交候选逐字保真，旧窗口通过摘要有损压缩，不声称等同其保留策略。

### 2. 纯策略与确定阈值

在 internal/context 增加值对象和封闭 decision kind：disabled、unknown、below_threshold、eligible、suppressed。字段由实例持有，不增加可变包级 var 或单实现策略接口。JSON auto_compact 仅布尔，默认随显式窗口启用；明确 true 无窗口报配置错误。

T=floor((W-S-max(R,C))*80/100)。采用商余分解计算百分比，避免乘法溢出；可用量不足则关闭自动路径并显示原因，不破坏已有小窗口启动。当前 byte_heuristic_v1 每四字节估一个 token；80% 是保守的估算缓冲，不保证任意大工具输出后仍能摘要；实际摘要必须继续通过 Provider PreflightCompaction 与 CheckCompactionRequest。unknown 不触发。软阈值、摘要真实请求准入、候选安装和下一普通请求准入分别判断；失败时普通请求仍须严格预检。

拒绝从模型名猜容量、直接复用 raw usage 或等到服务端报 prompt-too-long；这些方式不适配自定义 base_url，且失败后完整窗口可能已无法摘要。

### 3. 单 owner 的执行骨架

把 runCompaction 拆为独占手动入口与私有“调用方已持有 owner”的执行骨架；骨架仍持有 CompactionKernel 策略组合，不通过 struct embed/shadow 实现 Template Method。手动入口取得 active，自动路径在 runTurn 的同一 worker 内调用骨架。构造器不做 I/O、不启动 goroutine。

统一由私有采样准备协调函数串行处理候选、自动决策和重新预检，不将调度逻辑全部塞进 commitInputs。初始路径在 beginTurn 后、首次输入提交前进入；工具路径在全部 outputs Sync/finalize、stream 清理且确认还可继续 sample 后，由 AgentLoop 按现有 safe-point 规则冻结 claim，再进入同一准备阶段。没有领取输入时规划 committed 窗口，有输入时先创建候选并估算。压缩期间新增队列输入留待后续安全点，不修改本次冻结批次。

```text
Committed boundary --> Freeze claimed inputs --> Preview context
                                                   |
                              +--------------------+----------------+
                              |                                     |
                          Below threshold                        Eligible
                              |                                     |
                              |                            Discard candidate
                              |                                     |
                              |                         Compact committed window
                              |                                     |
                              |                            Rebuild candidate
                              +--------------------+----------------+
                                                   |
                                       Ordinary request preflight
                                                   |
                                       Commit inputs --> Sample
```

一次尝试额度由当前 turn worker 的 target sample index 持有；工具结果检查与输入检查共用它。成功后重建同 InputID/正文，只重检一次，即使仍高于软阈值也不再次摘要。编码大小错误不触发摘要；无历史时走普通预检，不能直接返还本来可发送的首条输入。初始输入预检拒绝结束 turn；steering 预检拒绝沿用现有返还后继续规则。prepared input 必须 Discard 后再调摘要，因为两家 CompactionKernel 都拒绝 pending 候选；Discard 失败属于内部故障，不能继续。

sampleIndex、seenInvocationIDs、totalCalls、resultBudget 和普通 usage 聚合不重建。摘要 usage 独立记录。自动摘要不增加普通 turn/sample 数，也不另起 worker。

### 4. 事件、取消与控制面

自动路径保持 AgentLoop.active，复用 turn publication channel；不同时设置独立手动 compaction worker。typed trigger 与自动 turn/sample association 在 domain/protocol/session 各自边界严格验证，禁止 any/map 路由。自动事件归属于原 submit 的关联，绝不生成虚构 compact control response。

ContextSnapshot 活动状态在自动摘要期间显示 compacting，同时 active TurnID 保持。控制 owner 根据开始/终态更新投影；过期 CompactionID 事件不能清除新状态。interrupt 定向取消 turn context，传递至摘要 deadline。独立手动 cancel_compaction 只操作手动 worker。shutdown/断管仍由原 owner 排空事件并等待 cleanup，lease 不提前释放。

TUI 当前任务状态和摘要状态分别投影；完成摘要只加入 compact boundary，不触发 turn completed。Ctrl+O 仍保留摘要与历史工具详情；View 无 I/O。一次性输出过滤压缩事件；stream-json 顺序输出关联原输入的 compaction 事件。

### 5. 失败和持久化

复用 compaction_started/completed/failed vocabulary，仅增加当前 typed 来源与关联，不另造一套 auto_* 记录。扩大 Session replay 允许压缩位于活动 turn 的完整采样边界，并严格拒绝未闭合工具对时出现压缩。手动路径仍要求 idle。

Session 明确允许自动摘要位于“turn started 后且初始输入尚未提交”或“完整工具 outputs 已提交、下一 sample 尚未开始”两种位置。不能只删除 compactionReplay.accept 的 turnActive 拒绝；必须验证 target sample index、工具闭合和 started/terminal 区间互斥。手动仍要求 idle，自动记录显式关联真实 turn，不改变既有 native ordinal。

可恢复失败（网络/超时、无缩减或摘要响应不合法）在确定 failed 收口且 Provider 清理完成后重新预检旧窗口，允许则继续同一任务；普通请求不允许则失败或返还未提交输入。用户取消必须停止；journal/Sync 不确定、prepared resolution 或 owner 不变量错误停止并按现有规则 poison，不降级。

Runtime 实例持有最近失败 source revision 和连续自动失败计数。相同来源不重复尝试；来源推进不重置计数；三次失败后熔断。成功手动/自动压缩清零；手动操作可越过自动熔断，其失败不增加自动次数。无内容、禁用、unknown 与被抑制不计失败；符合自动条件但摘要预算预检拒绝计一次失败，不写 started、不联网。以上调度状态仅进程内有效。

恢复继续复用 orderedRestoreRecords 按 seq 交错 source/checkpoint/native：只有 started 时先追加 interrupted compaction 再关闭活动 turn；completed 已 durable 则安装新窗口再收口 turn；不完整尾批次依现有规则修复，不安装候选。无论用户取消还是崩溃，completed 成功 Sync 后不能回滚旧窗口。新窗口安装而输入尚未保存时，恢复不能猜测该输入已提交。全部恢复均零网络、零工具重跑，使用同一 lease。

### 5.1 接续包装与权限

在 provider 的确定性摘要包装中加入自动接续语义，保留历史参考/非授权声明；自动模式要求直接接续，不复述摘要或询问如何继续。手动模式不注入自动续写要求。若包装 shape 变化，双 Provider 编译、checkpoint restore 和当前 native payload 验证必须同 change 更新，不能仅把展示层字符串改掉。摘要提示要求保留最近纠正、约束、验证结果和下一步，不要求披露推理过程。

不加入“读取完整 transcript 文件”的提示，当前并无安全的完整 Session 读取工具。工具 grant、Read 观察和 artifact 授权仍从真实账本/descriptor 取得；摘要里提到路径或批准不能产生权限。冻结的新输入在摘要之后逐字提交，不进入摘要。

### 6. 验证矩阵

| 层级 | 必测业务断言 |
|---|---|
| 策略/配置 | T-1/T/T+1、W 极值、溢出、unknown、开关、重复字段、小窗口和无窗口 |
| Runtime 输入 | 双 Provider 初始/steering 输入仅保存一次；摘要不含待提交正文；重建保留字节与 InputID；大输入返还 |
| Runtime 工具 | 完整 outputs 后才 compact；无审批中切窗；工具与 steering 共用一次尝试；sample/call/result 预算不重置；副作用不重跑 |
| 故障 | 工具调用/空摘要/refusal/incomplete/无缩减、断流、120 秒超时、旧窗口可继续/不可继续、跨 revision 三次熔断、取消 admission 前后、Sync 失败与 poison |
| Session/恢复 | 初始/工具边界所有截断点、同一 lease 修复；不可变当前 fixture；相同后续输入下恢复与不停机下一请求 canonical bytes、native 顺序、fingerprint 等价；恢复本身结束旧 turn 不自动执行 |
| 宿主 | TUI 固定尺寸/无色/队列/取消；stream-json 顺序关联、EOF drain、断管；一次性 text/JSON 无新增词汇 |
| 缓存 | 未触发路径 bytes/fingerprint 不变；触发后只有效历史改变，稳定 tools/profile/项目指令前缀不变 |

使用本地 mock SSE 和可控屏障/时钟/故障注入，不用真实 key、外网或 sleep 证明竞态。复用现有跨进程 lease 回归，新增自动压缩占用期间 repair 拒绝的真实子进程场景。最终 make verify 包括 gofmt、vet、Staticcheck、架构守卫、全量与 race。

## Risks / Trade-offs

- 估算与服务端容量偏差 → 留 20% 空间且保留真实摘要预算预检；超限明确失败，不静默裁历史。
- 提前压缩增加成本及摘要损耗 → 显式窗口才默认启用，可关闭，完整历史仍可查看。
- 自动摘要包含当前任务早先已提交指令 → 摘要提示保留最新纠正/未完成工作；未提交新输入保持逐字，不声称旧历史无损进入摘要。
- 活动 turn 内 replay 是主要复杂点 → 单独实现状态机与故障 fixture，未通过之前不接入宿主。
- 失败抑制与熔断不跨进程 → 明确进程范围；resume 不自动启动请求，用户显式新任务可重新评估。

## 当前格式维护

用户明确要求 Session 不存在格式版本号。删除 Record、checksumEnvelope、writer/draft、codec 和 typed payload decoder 中的 Session SchemaVersion/PayloadVersion/EnvelopeVersion；不以固定 canary、revision 或同义字段替代。seq、batch、checksum、字段严格校验与 replay 状态机继续验证唯一当前格式。带版本字段的记录作为未知字段直接拒绝，不增加旧格式读取、迁移或兼容分支。

在测试外重写唯一当前 fixture 的无版本信封和 checksum；版本门禁测试改为版本字段作为非法附加字段、缺失必需字段、类型/记录关联或 checksum/状态损坏回归。Provider-native/checkpoint 也不保留内部格式版本，只有已有 headless 外部协议保留其明确声明的版本。AGENTS 与架构/规范必须明确：未经用户明确指示，不得擅自设计多版本兼容或为未来兼容引入内部版本门禁；即使未来发布也不能自动推定授权。

归档前同步主 specs、架构和 Roadmap，记录故障经验，不把 P4 整体标为完成。

## 全工程兼容与版本门禁审计

按用户明确要求清理剩余无需求的内部门禁：NativeCommitEnvelope/CheckpointEnvelope/Session.NativeCommitPayload 的 payload_version 及精确当前版本判断；CompactionSource 固定 prompt_revision 的存在性/相等性判断；Catalog schemaVersion 和 PRAGMA user_version 的设置/门禁。Provider family、wire、model、typed payload、native pairing、来源 digest/前驱/实际窗口计数和 SQLite 实际结构继续校验，派生 Catalog 结构不正确时仍重建，不读取历史格式或迁移业务事实。

审计未发现生产历史 reader、迁移器、多版本分派或 fallback decoder。保留有明确消费者和需求的 headless 外部协议版本、Anthropic API header、macOS sandbox 语言版本、应用版本、UUIDv7，以及用于内容身份、并发窗口和预算比较的 revision、fingerprint、估算算法和安全策略身份。缓存目录与 renderer 的固定算法标识参与确定性 fingerprint，不选择历史 reader；不将这些内容身份误删。
