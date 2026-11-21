## Context

基线为 EasyCode `4054a7e`。手动和阈值自动压缩共用 `internal/runtime/compaction.go` 的 `runCompactionOwned`，自动路径包括待提交输入、工具续轮和 Stop 待交付反馈。现有成功流程先 Sync `compaction_completed`，再 finalize Provider checkpoint、装配 Cache/Context 与摘要历史，最后发布公共终态。AgentLoop 保留 compaction worker 和定向取消直到终态及清理完成。回放用成功记录的真实序号安装 checkpoint。目前 TUI 的压缩审查只识别摘要请求之前的 PreCompact，且 Context 快照在公共压缩终态才更新。[压缩提交](../../../../internal/runtime/compaction.go#L278) · [控制 owner](../../../../internal/runtime/compaction_loop.go#L128) · [回放](../../../../internal/session/compaction.go#L263)

### 参考行为

| 参考 | 已核实的行为 | EasyCode 取舍 |
| --- | --- | --- |
| Claude Code 2.1.88，`a8a678cb6244` | 完整及部分压缩在构造摘要后调用 Hook，再把候选交给 manual/auto 调用方安装。传递未包装的 `compact_summary`，REPL 有独立阶段。手动命令把 `userDisplayMessage` 包装进本地命令转录消息；自动续写消费压缩后的消息集。 | 保留触发、未包装摘要及阶段展示，沿用 Ctrl+O 阅读摘要。检查结果采用安全分类与计数。 |
| Codex，`9f2b87495bf9` | 本地和 token-budget reset 先安装窗口再派发。remote v2 在外层安装尝试成功且未中断时派发，内部可以接纳后台候选并再做一次前台压缩；候选的生成或过期并不等于安装。PostCompact 输入是 typed 身份及 trigger 元数据。 | 采用安装屏障与独立运行身份，以一个真实成功 checkpoint 为派发边界。窗口必须先成功 Sync。 |

#### Claude Code 的路径和输出选择

- 完整压缩传入与 transcript 包装前相同的摘要，Hook 结果返回后，手动 slash 路径才构建替换消息，auto 路径才替换查询窗口。session-memory 快速路径另行返回，直接调用的是 `SessionStart('compact')`。核对位置包括 `restored-src/src/services/compact/compact.ts:587–624,719–747,1065–1075`、`utils/processUserInput/processSlashCommand.tsx:678–703`、`query.ts:528–535` 和 `services/compact/sessionMemoryCompact.ts:583–602`。
- 通用匹配支持字面、分支与正则。outside-REPL 启动并行处理器，命令支持配置式和首行 JSON 请求式异步。通用结果识别 exit 2 / `decision:block`，选择成功的 stdout 或非零时的 stderr。PostCompact 包装层只读取成功与显示文本。具体位置为 `utils/hooks.ts:995–1028,1112–1160,1346–1379,1630–1633,3083–3085,3279–3380,4034–4088`，以及手动显示 `commands/compact/compact.ts:120–124,230–247` 与 REPL `screens/REPL.tsx:2497–2507`。
- EasyCode 采用同步串行、字面 matcher、主动审查及输出丢弃。遵循现有 Hook 沙箱、证据顺序和授权披露契约。保留本地检查可见性与已保存摘要的独立阅读入口。

#### Codex 的安装和观察边界

- 本地 sample 压缩在 `compact.rs:456–477` 安装、计算并发布 completed，外层 `216–239` 才运行 PostCompact。`compact_token_budget.rs:49–74` 的本地 reset 也可以触发，无需生成模型摘要。remote v2 在 `compact_remote_v2.rs:234–275` 根据安装结果与中断决策，`compact_remote_v2_installation.rs:17–76,225–312` 可在同一外层尝试中先接纳 prefix、再安装前台结果。EasyCode 的承诺限定为当前手动和阈值压缩产生的一个成功 checkpoint，WebSocket 握手回退也只观察最终安装的 checkpoint。
- `session/mod.rs:5089–5154,5546–5568` 先替换实时窗口，再尝试写 rollout，安装调用不以 append 返回值作为成功条件。Hook 生命周期通知是瞬态投影。EasyCode 将已知成功的 checkpoint Sync、运行准入和事实关闭作为本地持久边界。`rollout/src/policy.rs:162–218`。
- `hooks/src/events/compact.rs:140–218,255–327` 传递 trigger、模型、路径等元数据，允许有控制权限的同步命令使用结构化 `continue:false` 停止续轮，普通非零退出归为失败。`hooks/src/engine/dispatcher.rs:32–77,159–203` 正则匹配及并行运行，结果恢复声明顺序。Codex 人类输出渲染独立 Hook 通知，exec JSONL 屏蔽它们。EasyCode 采用有界独立状态、保留原有 v1 投影，并以用户取消和会话健康决定续轮。相关实现位于 `exec/src/event_processor_with_human_output.rs:280–295` 和 `exec/src/event_processor_with_jsonl_output.rs:478`。

`UserPromptSubmit` 会横跨初次提交、同回合追加、附件及 Stop 复合交付，需先确定按输入的拦截和返还规则。`SessionStart` 要定义 startup/resume/compact 的一次性启动和动态上下文。`PermissionRequest` 涉及普通审批与 Plan Mode 权限。PostCompact 可先在一条共享的成功边界交付。

## Goals / Non-Goals

**Goals**

- 让脚本读取实际提交并安装的语义摘要，满足事件隔离、逐声明告知与有界运行。
- 用相互独立的压缩结果和检查结果表达真实状态，覆盖中断、清理不确定及重复恢复。
- 使 Provider 仅依据既有原生历史续写。相同 checkpoint 和后续输入产生等价的请求字节、原生顺序及缓存 fingerprint。

**Non-Goals**

- 本切片范围限定为 root 同步命令、退出状态与本地展示。模型反馈注入、脚本控制 turn、SessionStart、部分压缩、异步、可执行插件贡献和新的平台沙箱属于各自的变更。

## Decisions

### 在现有成功提交中冻结后置计划

在纯候选校验后，从启动冻结的 Hook 快照选择 PostCompact。两类快照读取共用现有严格配置解析、FD 来源归属、触发作用域、排序和数量上限；事件、披露策略和命令 fingerprint 仍分别校验。将必需的 `post_compact` 判别联合放入同一条 `compaction_completed`，保留既有 Provider checkpoint envelope 和计量。

| 计划分支 | 事实 |
| --- | --- |
| `none` | 两来源有效且无匹配。只有原压缩终态。 |
| `skipped` | 含一个封闭原因。`hook_match_limit` 表示超过匹配上限，`hook_trust_required` 表示限额内匹配但无交互，`hook_plan_limit` 表示完整计划超过成功记录编码预算且跳过形状可保存。 |
| `planned` | 交互路径有 1–8 项按来源顺序冻结的 PostCompact 引用及 matcher，并保存 plan digest。 |

plan digest 使用固定规范编码的事件、真实 CompactionID、scope、有序声明引用、规范 matcher 及完整 checkpoint 摘要。checkpoint 摘要绑定来源和已生成的原生 payload，不能只使用压缩前窗口的 source digest。复用不可变 checkpoint bytes 及现有安全引用；按 user/project、来源内唯一 ID 和同来源 revision 校验。把计划独立追加在成功记录之后会留下无法判断是否有待关闭检查的崩溃窗口。内嵌计划增加 Session 元数据，同时保留单记录成功批次和真实 checkpoint sequence。

共享坏来源继续遵循 PreCompact 的失败关闭规则。PostCompact 依次判断匹配上限、无匹配、交互模式和完整成功记录大小。计划大小使用与 writer 一致的单记录 envelope 与 canonical 上限，含 seq、batch、checksum。只对确认的大小溢出尝试以 `skipped/hook_plan_limit` 保存同一 checkpoint 一次；分支本身也必须通过相同预算。其他编码、身份或结构错误按既有压缩失败路径处理，不能降格为跳过。已有普通输入的 `ValidateDraftBatchSize` 使用模拟多记录信封，压缩记录必须使用合法 singleton 形状进行预检。

### 在 checkpoint 安装后才审查和执行

工作顺序为 `candidate validation → Sync(compaction_completed) → Finalize → transcript/cache projection → local checkpoint-installed phase → preflight → trust → run → local result → CompactionCompleted`。平台与实际 stdin 编码预检先于逐条信任，所有匹配项获信任后才启动第一项。非交互在冻结计划时收口跳过，避免本次后置阶段探测运行器或构造脚本输入。信任表以现有声明引用为键，响应还须绑定 active session/thread、compaction ID、scope、PostCompact plan digest 和 request ID。PreCompact 的审查和计划 ID 无法与后置交叉。拒绝任一项即可跳过整个计划，已有会话内信任继续按既有 reset 生命周期处理。

stdin 使用封闭 typed 分支。摘要取现有 prepared checkpoint 携带、成功后安装为 `ActiveContextView.Summary()` 的同一不可变值。双 Provider 都先投影可见的 generation 文本，再分别生成带任务状态的原生包装，Hook 读取的是前者。每项收到相同 UTF-8 内容，保留空白、Unicode 和换行。规范 JSON 按实际编码限制 512 KiB，摘要值使用已有 256 KiB 上限，转义后超限走 `hook_input_limit`。兼容字段取 Claude 的 `trigger` 和 `compact_summary`；身份使用与 PreCompact 一致的 session/thread/compaction/cwd，自动附真实 turn/sample。运行器只接收内存构造的 typed 输入，复用现有工作区 FD 身份、最小环境和沙箱。

复用只读禁网 launcher、进程组 owner、每项声明期限、双流各 16 KiB 捕获和安全执行分类。正常 exit 0 即成功，任何限额内 stdout/stderr 始终丢弃，普通非零包括 2 为 `hook_exit_failed`。确定的单项错误记录后可继续下一项，剩余执行额度用尽时收口未运行后缀。每次 PostCompact 用 `runTimedHook` 追踪累计 runner 耗时，最大执行预算 30 秒，独立于 PreCompact、信任、journal 和 Provider 摘要期限。进程或流的收口可能超过执行期限，仍由既有 owner 完成。复用行为一致的 matcher、受限执行与引用校验；事件专属策略负责输入和退出判定，工具后置路由的 `HookEvent.Post()` 继续只识别工具后置事件。

### 维持同一宿主操作和明确的取消边界

本地阶段使用封闭 typed 通知。安装态携带 session/thread、compaction ID/scope、实际 checkpoint sequence、committed revision。对于 `planned`，另携带 plan digest 和条数。对于交互 `skipped`，安装态同时表示跳过结果，携带稳定原因；此分支没有计划或运行身份。合法无匹配与 headless 跳过均无额外本地通知。安装态只能在对应 summarizing 后接纳一次，先更新手动和自动路径的 AgentLoop 快照，再对宿主转发。已有单 owner 的有序发布足以维持阶段顺序，无须等待 TUI 渲染。Context 切换到新 committed revision。Cache 发布摘要的 `LastSummary` 并保持上次普通采样的来源、读基线与指纹，自动失败计数在成功安装时清零。信任 modal 仍独占键盘；可通过宿主 typed query 校验等待期间的快照。

PostCompact 信任请求和 checking 复用现有活动协议，要求匹配已安装的计划、事件和活跃的操作。使用已有无缓冲的 worker 发布通道，首次审查及已有信任的首次 checking 都排在安装态之后；每项 checking 先有 durable started，并在交付 runner 前发布。关闭态携带同一 checkpoint/plan 关联及真实 `closed` 记录序号、结果码和完成数。写入或投影不确定使用无 durable close sequence 的安全结果。仅在当前操作展示检查状态，压缩历史沿用原摘要条目和读取权限。取消后仍接纳匹配安装和关闭事实，忽略迟到的审查与运行活动；显示「已压缩 · 正在取消 PostCompact」，公共 completed 合并独立结果后才清除操作状态。Ctrl+C 路由保持手动 cancel_compaction 与自动 cancel_turn。

主动推迟原有公共 `CompactionCompleted` 到后置阶段完成。AgentLoop 的手动终态会拒绝迟到事件，自动终态会清除 active compaction ID，因此保留一个公开终态可复用现有相关性和取消路径。安装通知只面向本地宿主，携带独立已提交快照，查询仍显示活动压缩。等待审查、执行和取消清理期间不释放 worker 或 lease，也不接纳会改变当前窗口的工作。

以 `checkpoint 是否已知 Sync` 与 `当前操作是否可继续` 两个事实决定 worker 结论。当前 `prepareAutomaticWindow` 对未观察到失败事件的非空返回也增加失败次数，空返回只检查 context 取消。采用显式内部 checkpoint 状态及独立 operation error，后置 poison 必须返回非空 Session 错误，优先于并发取消，自动路径在尝试输入、工具续轮或 Stop feedback 交付前统一拦截；已知提交不增加压缩失败计数。保留既有 pre-Sync 可降级失败和旧窗口预算规则。

| 边界 | 行为 |
| --- | --- |
| 完成记录尚未获 writer 接纳 | 取消保留旧窗口，沿用现有取消和未提交输入返还。 |
| writer 接纳完成记录 | 等待确定结果。Sync 不确定时 poison，零 PostCompact 审查或执行。 |
| 成功 Sync，但 finalize 或必要宿主投影安装失败 | 当前操作按既有非 durable `session_failed` 关闭，既有成功记录供下次恢复。零 PostCompact 审查或执行，journal 不追加 compaction_failed。 |
| checkpoint 已知 Sync 且投影已安装 | 后置拒绝、已确认错误或取消仍发一次公共 completed。用户取消独立停止下一 sample；健康的普通后置错误仍按新窗口执行原有预算检查。 |
| 投影安装后，后置事实写入失败或清理不确定 | 除未接纳 started 的普通取消外，以 Session 错误收口。保留已知成功，发布独立安全后置状态及唯一公共 completed，poison 阻止后续工作。未闭合尾部保留给恢复，禁止提前写入普通 turn terminal 或交付待保存的 Stop 反馈。 |

### 单独保存前缀并原序号恢复 checkpoint

`planned` 进入独立的后置尾部，沿用同一 compaction 作用域。每条 `post_compact_hook_started`、`post_compact_hook_finished`、`post_compact_hook_closed` 独立单记录 Sync。started 通过 `HookRunIdentity` 表示交付 runner 的意图。取消如果先于 writer 接纳，保持零 started；如果 started 已 Sync，待 runner 返回真实结论后保存 finished，即便它在启动 OS 进程前确认取消。started Sync 不确定时零 dispatch。finished 从真实执行获得，封闭校验清理、退出、取消、超时及双流统计。PostCompact 专属校验拒绝控制或反馈结果、未清理确认的成功、未知或非零退出的成功以及超输出上限的成功。对已经获得的运行事实，取消不阻止安全收口。必要的 finally 仍由同一 owner 等待，不产生独立后台运行。

关闭保存封闭 `state`、`code`、真实 finished 数与 `live` / `recovery` 来源。以下按优先级匹配已验证前缀。回放、实时收口和恢复共用同一事实判定，避免相同前缀产生冲突解释。

| 事实 | 关闭结果 |
| --- | --- |
| 最后一项 finished 为清理不确定，或者恢复见到孤立 started | `outcome_uncertain / hook_outcome_uncertain`。孤立 started 只允许 recovery 收口；终态保留其已准入而结果未知，不伪造 finished。 |
| 最后一项真实 finished 为取消 | `cancelled / cancelled`，含末项取消；禁止新增 started。 |
| 全部计划项含真实确定 finished | 全部通过为 `succeeded / ok`，其余为 `failed` 加首个确定错误。此时后到取消不改写已保存的结果，只影响后续 turn。 |
| live 无孤立 started，且仍有未准入后缀 | 优先处理已观察到的用户取消。零开始的前检或信任失败为 `skipped`，原因限 `unsupported_platform`、`sandbox_unavailable`、`hook_input_limit`、`hook_trust_denied`。预算耗尽为 `failed / hook_execution_budget_exhausted`，要求有确定结束的真实前缀和未准入后缀。 |
| recovery 前缀尚未跑完且有序，所有 started 有确定 finished | `interrupted / session_interrupted_before_hook`。没有 started 的 planned 响应走同一分支。 |

仅在 writer 接纳前选择关闭结论。接纳后等待确定 Sync 结果，后到取消不能替换已接纳的关闭，也不能把完整 finished 改为未完成。如果 writer 结果不明，保留可恢复前缀，运行实例 poison。由于进程清理不确定会立即终止后续接纳，后续恢复只反映已有证据，保留原 checkpoint。

完整行解码须在 Loader 判定半行或未完成 batch 前校验 `compaction_completed` 和新增记录的 singleton envelope（`batch_id=seq, batch_index=0, batch_size=1`，外层无 turn/parent），前缀回放再检查跨行顺序及关闭证据。成功记录自身的序号仍代表 Provider checkpoint、usage 校验和摘要 cursor。即使 Post 尾部未关闭，也须把成功压缩投影进 `ReplayPlan.Compactions`；后置待恢复状态与 `InterruptedCompaction` 分开，后者只代表没有成功 checkpoint 的悬空压缩。自动压缩的每条有效后置事实推进真实 turn 尾序号，元数据回放（`PlanMetadata`）也执行完整后置语法校验。未关闭的尾部不允许普通记录。恢复 owner 在完整校验及同一 exclusive lease 下先收口压缩或后置检查，再处理 Stop 待交付和 turn 中断。离线预览共用语法并保持只读。重复恢复复用已提交的关闭。

## Risks / Trade-offs

- 摘要可能含有原工作上下文。只对放行的命令传递该次规范摘要，审查明确披露。stdin、原始输出、脚本路径与自由文本原因从 Hook 记录及对外事件排除；已安装 checkpoint 仍使用原有私有摘要持久化。
- 同步后置检查会延长公开终态延迟。活动展示即时表明已提交窗口，支持定向取消，现有预算和 cleanup owner 限制运行阶段。
- 须防止旧 PreCompact 审查或迟到活动在阶段切换后被接纳。协议和宿主都以实际目标、阶段、事件和独立计划摘要校验，不能仅用相同 CompactionID 放行。
- 新后置事实及恢复不能改变 Provider 字节与缓存。利用既有双 Provider mock、固定 fixture 和真实 Darwin runner 验证共同边界，以故障注入校验 Sync 顺序、定向取消与清理。细化验收见 [tasks.md](tasks.md)。

## Migration Plan

当前 `compaction_completed` 唯一形状要求 `post_compact`，合法无匹配使用 `{"kind":"none"}`。在实现提交中替换受影响的固定当前 fixture 及 checksum，与 reader/writer 一起启用。缺少该字段的旧开发记录在完整验证阶段失败关闭。Provider checkpoint envelope 与 headless v1 继续使用各自当前协议。
