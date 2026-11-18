## Context

动机和用户已确认的范围见 [proposal.md](proposal.md)。本变更处于 propose 阶段；以下设计描述待实现行为，不表示能力已经交付。

### 当前实现与接入点

| 已核对位置 | 现状 | 本次使用方式 |
| --- | --- | --- |
| `docs/roadmap/product-roadmap.md:434,463,484` | 要求压缩后保留计划、Claude 风格任务交互和 task 状态 | 补充单线程进度清单，不推定 P5 或 P7 整体完成 |
| `internal/tool/tool.go:70`、`catalog.go:96,325` | 九工具封闭联合、固定 facade 身份、确定性目录 | 增加一个真实 capability 和两家 facade，不引入动态 registry |
| `internal/runtime/tool_scheduler.go:12-65` | 只读并发批次和按模型调用顺序的独占屏障 | 任务更新采用无批准的独占 slot |
| `internal/runtime/runtime.go:1140-1182` | 冻结结果、写入并 Sync、预算提交、通知 | 在这条提交路径安装任务投影，不先修改 UI 状态 |
| `internal/session/tool_history.go:183`、`replay.go:300` | 验证后的完整工具 ledger 可形成恢复计划 | 从成功任务结果推导当前状态，不增加另一套任务存储 |
| `internal/app/compaction_restore.go:16-91` | 按 seq 合并 native facts、压缩来源和 checkpoint | 为每个 checkpoint 关联其来源前缀的期望任务状态 |
| `internal/provider/{anthropic,openai}/compaction.go` | Provider 生成摘要、确定性包装、候选窗口、durable 后安装 | 包装加入精确任务块，保持 generation 与宿主资料的来源区别 |
| `internal/context/planning.go` | 固定来源顺序，语义估算与原生 footprint 采用覆盖关系 | 任务资料只作为 committed_history 计一次 |
| `internal/app/transcript_queries.go`、`internal/tui/transcript_*` | 只读宿主查询、四页缓存、稳定历史位置 | 展示冻结任务输入/结果，复用详情生命周期 |

### 两个参考工程的证据与取舍

参考工程只读检查，没有修改、构建依赖或复制其实现。证据绑定本地 revision，不能据此宣称上游当前行为永远不变。

Claude Code 参考：`claude-code-sourcemap`，revision `a8a678cb6244e6770e1e421767ff0987a1d95549`，恢复源码根为 `restored-src/src/`。

| 来源 | 观察 | 采用或调整 |
| --- | --- | --- |
| `utils/tasks.ts:133-138`、`tools.ts:218-220`、`tools/TodoWriteTool/TodoWriteTool.ts:52-54` | 默认交互态使用 TaskCreate/Get/Update/List；TodoWrite 是另一启用分支 | 清楚说明本次不是完整 Claude Tasks；保留轻量整表替换，不导入协作任务体系 |
| `utils/todo/types.ts:4-19`、`tools/TodoWriteTool/TodoWriteTool.ts:58-99` | 三态、content/activeForm、无普通批准、完整列表更新；全完成后清理活动列表 | 采用熟悉的 Anthropic facade；全完成只折叠，不删除事实 |
| `components/TaskListV2.tsx:48,128-189,220-239` | 有界清单、完成/进行中/待办数量、状态图标及隐藏数量 | 借鉴紧凑视觉；采用确定性顺序和高度预算，不使用时间排序 |
| `hooks/useGlobalKeybindings.tsx:50-85`、`components/Spinner.tsx:280-296`、`screens/REPL.tsx:4606-4608` | Ctrl+T 折叠、运行时和输入框邻近清单 | 使用相同主要交互，保证审批与 composer 优先 |
| `hooks/useTasksV2.ts:123-170`、`utils/sessionRestore.ts:73-90,138-147` | 默认 Tasks 有独立文件和完成后清理；TodoWrite 恢复会查调用 | 不创建任务文件，也不从仅有调用推断成功；使用 Session 结果事实 |

Codex 参考：`codex`，revision `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`，源码根为 `codex-rs/`。

| 来源 | 观察 | 采用或调整 |
| --- | --- | --- |
| `protocol/src/plan_tool.rs:8-29`、`core/src/tools/handlers/plan.rs:65-111` | update_plan 的 explanation + plan 完整更新；与 Plan Mode 区分 | 采用 OpenAI 原生习惯和可解释的批量更新 |
| `core/src/tools/handlers/plan_spec.rs:7-54` | 最多一个 in_progress 主要是 schema 描述；可选 explanation 的工具明确 strict=false | 本地真正校验状态与大小，并为该 OpenAI facade 显式保留可选字段的服务端 schema 模式 |
| `rollout/src/policy.rs:81-99,161-227` | 原生调用/输出保存，PlanUpdate 事件本身不持久化 | RuntimeEvent 只投影；当前状态来自成功 typed result |
| `tui/src/chatwidget/turn_runtime.rs:610-624`、`tui/src/history_cell/plans.rs:234` | 当前计划进度与简短历史单元 | 吸收完成比例与紧凑活动，不以 UI 内存作为恢复依据 |
| `tui/src/chatwidget/session_flow.rs:394`、`core/src/compact.rs:381-397,609-633,716-832` | UI 进度初始化会清空，压缩依靠用户消息与自然语言摘要 | 本次明确补齐恢复重建和压缩后精确保留 |

Claude 的部分状态规则也只存在于提示中。本次的数量、字节、重复标签和单一进行中限制是 EasyCode 自己的当前契约，不归因于参考工程已有保证。

## Goals / Non-Goals

**Goals:**

- 以一个 capability 覆盖模型更新、用户阅读、headless、恢复和压缩的完整路径。
- 明确区分模型报告的进度、真实工具执行结果和用户授权；completed 不改变任何安全或回合完成判断。
- 只保留一个当前状态和一套严格格式；所有投影可从原有 append-only 事实重建。
- 保持 Provider-native opaque 数据、调用顺序、稳定缓存前缀和唯一资源 owner。

**Non-Goals:**

- 不把任务项变成可执行 job，不增加 ID/owner/依赖图、共享任务目录、定时隐藏或恢复后自动执行。
- 不实现 Enter/Exit Plan Mode，不把展示状态作为修改权限或 Stop 检查通过的证据。
- 不扩大九项现有工具的权限、Hook 覆盖范围或平台支持承诺。

## Decisions

### 1. 单一任务能力，保留两家输入习惯

能力 ID 使用 `task.progress`。Anthropic `TodoWrite` 接收 `todos`，每项必须有 `content/activeForm/status`；OpenAI `update_plan` 接收 `plan`，每项必须有 `step/status`，可带 `explanation`。两条 wire 独立严格解码；不会创建一个同时接受两家字段的宽松 schema。

domain 只保存不可变 `TaskUpdate`、`TaskStep` 与三态 status 值，校验公共文字、数量和状态规则，允许规范 active_form 为空。`tool.TaskInput` 另外持有 `todo_write` 或 `update_plan` 的封闭来源 profile 与规范 TaskUpdate，使工具、Session 和原生证据验证能约束 Anthropic 的 activeForm 必需、OpenAI 的 activeForm 为空以及 explanation 的归属。profile 是工具输入契约的一部分，不是格式版本；domain 不解析 facade 名称或 Provider DTO。当前状态、checkpoint、任务内容 stamp 与外部 tasks.snapshot 只保存规范更新和成功来源，不重复 profile；来源对应的已验证 ready/result 仍完整保留它。规范值的字段完整存在，getter/clone 不共享 slice。

当前 `decodeFacadeInput` 仅接 capability 和 wire kind，无法区分两个同为 structured 的任务 facade。目录解码、`ValidateNativeCallInput`、结果预检以及联网/离线恢复必须显式传入经过核验的 family/facade profile，共用两条严格 decoder；不能检查 JSON 中出现 todos 或 plan 再猜测来源。

| 限制 | 选择 |
| --- | --- |
| 清单项数 | 0–20；0 表示明确清除 |
| content/step、activeForm | 每字段最多 512 UTF-8 bytes；必需文字非空、无外围空白、无控制字符 |
| explanation | 仅 OpenAI 输入可选，省略归一化为空；最多 2 KiB，允许换行和 Tab，拒绝其他控制字符；不改写原文字 |
| 总文字 | content/step、activeForm 和 explanation 合计最多 16 KiB |
| JSON 与规范值 | 原始工具输入与规范任务快照各最多 64 KiB，并继续服从 record/native/batch 实际编码上限 |
| 状态 | 仅 pending/in_progress/completed，最多一项 in_progress；允许没有进行中 |
| 标签唯一性 | 相同完整标签不重复；不执行 Unicode 归一化或模糊匹配 |
| 更新语义 | 有序完整替换，允许重新组织、删除、状态回退和全完成，不做增量合并 |

拒绝重复键、未知字段、null、缺字段、尾随内容、非法 UTF-8、跨 facade 字段和超限。Provider completed sample 才能产生 ready；非法输入仍按当前协议使整个 sample 失败，不能提前更新清单或让其他工具产生副作用。

首个真实请求中的工具/schema description 必须简明披露全部硬限制：最多 20 项、字段 512 UTF-8 bytes、说明 2 KiB、总文字 16 KiB、原始/规范编码 64 KiB、标签唯一、无外围空白/控制字符、至多一个进行中但允许零个、空数组清除、完整替换及跨 turn 保留。JSON Schema 的 maxLength 只限制字符数，不能替代字节校验；以 512/513 bytes 的中文组合验证两者区别。说明同时表达多步骤用途、及时更新、成功后无需重复全文以及 completed 不等于测试证明。两家均直接曝光真实工具，不引入 deferred ToolSearch 或参考工程的协作提示；简单任务不强制先调用清单工具。

### 1.1 服务端 schema 模式与本地严格校验分别定义

当前 OpenAI `request.go:89` 为 structured function 固定 strict=true，`responseTool.Strict` 还是带 omitempty 的 bool。这与可选、非 null 的 explanation 不相容：[OpenAI strict-mode 文档](https://developers.openai.com/api/docs/guides/function-calling#strict-mode)要求 strict=true 时所有 properties 都列为 required，且明确关闭需要 strict=false。Codex 参考的 plan_spec.rs:49 也显式选择 false。

保留本次已定义的可选 explanation，不将它改为必需字段或接受 null。不可变 Facade 保存强类型服务端 function schema 模式：OpenAI 任务为 best_effort，其他现有 function 保持 strict，Anthropic 与 custom freeform 为 not_applicable；每一当前分支都有真实消费者。Provider 编译器消费该模式，update_plan 在 HTTP、WS、普通与摘要请求中都明确输出 `strict:false`，其他现有 function 的 true 和 custom 的字段省略保持原 wire。使用可表达 true/false/不适用的 DTO，不能让 omitempty 吞掉 false，也不在请求编译时按名称临时特判。

该静态模式进入 catalog canonical definition、source revision、fingerprint 与实际请求属性证据；改变它必须产生可解释的目录变化。服务端 best_effort 不改变本地 strict decoder、validator、Session 或恢复的失败关闭规则，不引入重试、探测或自动降级。真实 CLI 假服务必须先校验提交的工具 schema 和 strict 值，再返回 SSE；仅无条件回放工具调用不能证明请求可接受。

备选方案：直接采用 Claude 四工具体系会引入 task ID、独立 CRUD/并发和协作契约；两家统一一个外部 JSON 会丢失熟悉的 facade；每一步单独 patch 列表增加冲突和恢复复杂度。本切片选择完整快照替换。

### 2. 结果 ledger 是唯一事实，执行器不持有清单

`TaskExecutor` 只接收完整 typed invocation 并构造冻结结果，没有文件、网络、进程、终端或共享状态副作用。任务更新是 `exclusive`，不需要 approval，也不触发仅覆盖 Patch/Write/Exec 的 Pre/Post/Failure Hook。沿用 `tool_call_ready -> tool_execution_started -> tool_call_result`，不新增专用任务 JSONL record。

成功结果保存完整规范更新，且必须与 ready input 相等；失败结果不得包含已应用更新。success preview 使用固定英文的完成数量确认，清空使用固定确认，均不回显任务正文且最多 1 KiB。完整 metadata 不是可截断 preview；单项上限 1 KiB 继续计入 sample 512 KiB、turn 2 MiB 和既有每 call 安全预留。到达 slot 后先检查冻结确认、metadata、record 与 Provider output 的实际编码；不足则保存配对 `result_budget_exceeded`，旧清单不变，不切分列表或另存 artifact。

提交顺序：

```text
native sample + usage + all ready --Sync--> finalized sample
    --> exclusive admission --> started --Sync-->
    --> pure task result --> result admission --Sync-->
    --> install current projection --> publish UI update
    --> later calls --> native outputs --Sync--> next sample
```

Task 的业务变化发生在成功 result durable 后，不等待整组 outputs 才展示。result 与 outputs 之间崩溃时，该更新仍然有效，恢复仅补原 outputs。前序任务结果 Sync 失败时不得继续后序工具；同 sample 两次更新按 call index 各自生效，最终取最后一个成功结果。

Task success 写入使用可取消的 writer admission：接收前取消不应用成功，复用原预算 ticket 按原调用持久化唯一 cancelled 结果，清除成功任务 metadata，不二次 reserve/close，也不把确定尚未提交的正常取消当作 writer poison；接收后等待确定 resolution，Sync 成功即应用，即使随后 turn 取消也不回滚。不能直接沿用副作用已发生后无条件 `WithoutCancel` 的假设来提交尚未接收的 Task success。实际共享 writer/预算路径，增加窄的结果 admission 策略，不另造 writer 或持锁等待 Sync。真正写入/Sync 失败或无法确定 durability 时沿既有 poisoned 路径停止工作；恢复只认可实际验证完整的 journal。

### 3. 显式三态与来源，跨 turn 保留

当前 `TaskProgressState` 是封闭联合：

- `none`：从未出现成功任务更新；不得携带 source 或 update。
- `clear`：最近成功更新是空列表；必须携带完整 source 和空列表 update，保留合法 explanation。
- `snapshot`：最近成功更新非空；必须携带完整 source 和 update，所有项完成仍为此状态。

source 包含成功 result 的 `result_sequence`、所属 TurnID 和 invocation ID。sequence 是 journal 次序，不是格式版本。现有 `ToolHistoryEntry.Sequence` 是 ready seq，不能误用为结果来源；replay 投影应保留明确的 `ResultSequence`，live 路径从实际 result Append 返回的记录取得同一 seq，不能拿 detail/native seq 代替。每个分支严格校验必需与禁止字段，禁止 null 或缺字段按旧格式降级。

live 的 `ToolHistoryObserver.ToolResult` 目前也不携带 result seq。提交路径应将 Append 返回的实际结果序号传到 observer 与当前投影；replay 从相同 result record 取得该值，不能从 ready/detail 或 UI 事件推导。

当前状态由 Runtime 实例拥有的有界内存投影持有；唯一活动 worker 在 Sync 后替换，查询只取得不可变副本。同步只覆盖有界内存读写，通知与回调在锁外执行，不启动 goroutine。初值由经过完整验证的 ReplayPlan 注入，构造器不读取 journal。

用户下一条输入、turn 结束、Stop 续轮、Question 回答、取消与压缩都不隐式清除或完成清单。模型可以通过下一次完整更新替换它。UI 应表明进度由模型报告，不把上一回合未完成项自动当作新用户请求。

不新增 `task.json`、SQLite 表、磁盘 watcher、全局 map 或可变 codec。历史使用现有工具 ledger，内存只保存一份当前状态。

### 4. 先验证再恢复，失败不生成更新事实

ready/result 的 typed 封闭联合新增 Task 输入和 metadata，配套纯构造器、strict decoder、validator、clone、预算验证和结果/原始调用一致性校验。两个 Provider 的 tool-result codec 和 native restore 都识别相应 facade，不能只为实时请求添加名称。

完整 replay 按 durable seq 归约成功 Task result；不解析自然语言输出、模型摘要、终端事件或单独调用来猜状态。恢复情况如下：

| 已保存事实 | 恢复行为 | 当前任务 |
| --- | --- | --- |
| ready，未 started/result | 补既有 before_execution 中断结果 | 旧状态 |
| started，无 result | 保守补 outcome_uncertain；不重新计算成功 | 旧状态 |
| 失败/取消/预算拒绝 result | 复用冻结结果及预算 | 旧状态 |
| 成功 result，缺 native outputs | 安装该快照，补原 call ID、顺序和冻结确认 bytes | 新状态 |
| 成功 result 和 outputs | 只重建投影 | 新状态 |

为了跨层验证，app 组装 restore records 时把 Task ready/result evidence 与 Question 一样显式传入相应 sample；Provider 校验原生调用与 typed 事实一致、结果状态正确、确认 bytes 可复现。共享层不从语义历史重建 wire，Provider 不读取 Session records。

具体消费者包括 `orderedRestoreRecords`、RestoreRecord 的结果联合、两家 ready/result 原生核验及离线 HistoryReader。`previewSession` 也使用这条装配进入 ProjectStoredHistory，不能只改联网 resume。离线 picker 必须能验证合法任务 checkpoint，并拒绝重算 checksum 后仍伪造 ready/detail/native seq、另一合法或未来结果、跨 profile 参数及 native wrapper 的记录；所有失败都不 repair、不联网且 journal bytes 不变。

所有 Task grammar、result/input、checkpoint source 和 Provider 校验必须先于 repair 或本地补偿；损坏文件的 bytes 保持不变。只替换仓库内固定 current fixture，不在测试运行时用当前 encoder 生成期望历史 bytes。

### 5. 压缩只保留精确快照一次，不重复注入普通请求

普通 sample 已经通过原生调用保存完整输入与结果，不额外注入当前清单。否则会重复计量，并让 OpenAI WebSocket 的增量前缀失配。任务状态、source seq 和 UI 折叠也不能进入稳定 profile/tool/skill/project 前缀。

每次压缩 admission 显式冻结当前 `TaskProgressState` 到 `CompactionInput`；普通 `TurnInput` 不增加动态任务附件。来源是该 `compaction_started` 之前的 committed journal 前缀，而不是恢复完成时的最终列表。压缩期间既有 grammar 禁止新工具事实，因此 source 不会中途改变。

两家唯一当前 checkpoint payload 增加必需 `task_progress`，包含该冻结三态及 provenance。保留真实模型 `generation` 和从它验证得到的自然语言摘要；宿主另以确定性函数生成任务参考块，并和现有摘要 wrapper 一起形成 Provider 原生 summary item/message：

```text
model generation --> verified context summary ----+
                                                 +--> deterministic native handoff
source task state --> verified task reference ----+
```

任务块说明这些是历史中模型报告的当前清单，不是新请求、许可、工具执行证明或自动续跑指令。snapshot 保留完整标签、activeForm、状态、顺序与 explanation；clear 明确没有当前步骤，防生成摘要把旧列表复活；none 不添加任务正文。任务文本按确定性 JSON 编码作为资料，不插入可混淆标签的原文。source seq、TurnID、invocation ID、CompactionID 不进入模型正文或内容 fingerprint。相同清单不同来源身份产生相同参考正文。

Provider codec 独立验证 checkpoint payload 与自己的 native wrapper。Session/app 从每个 started 前缀计算 expected task state，通过窄 typed restore evidence 交给 checkpoint 恢复校验；必须同时匹配 branch、source 和规范 update canonical bytes。不能用恢复终点的最新快照比较历史 checkpoint，也不能只校验一个自洽但伪造来源的 payload。

原模型 generation 仍是摘要详情投影的事实，不把宿主任务块伪称为模型生成。原生 footprint 覆盖完整摘要加任务包装；规划沿既有语义/原生覆盖规则计一次，不新增 task source，也不把恢复投影作为 current_input 再加一遍。候选完整编码、估算、缩减和窗口预算都包含任务块；任何超限或无缩减失败保留旧窗口，不截断清单、不扩大原有限额。压缩安装不改变当前任务状态。

### 5.1 任务内容 stamp 补足历史规划指纹

现有 buildCachePlan 的 historyJSON 只包含真实 summary、排除工具调用的语义消息、footprint revision 和估算数；等长 A/B 任务可能产生相同规划指纹，即使真实 Provider request 不同。数值 footprint 不能充当内容身份，也不能把宿主任务 JSON 伪装成语义摘要来修补。

PlanningInput 显式接纳由对应已验证 TaskProgressState 纯内存派生的有界内容 stamp：none 不添加 stamp，clear/snapshot 对 `{state, update}` 的规范 bytes 计算 SHA-256，将结果编码成 64 位小写十六进制字符串，作为既有 committed_history canonical metadata 的 `task_content_digest`。update 只包含 domain 任务资料，排除 facade profile、source seq/turn/invocation、CompactionID、折叠和查询身份。保持来源顺序与原生 footprint 类型不变；它不增加估算、Provider 正文、TurnInput 附件或新 source。省略 none 的元数据不是历史 reader 或格式版本分支。

普通、候选窗口、手动/自动摘要及恢复规划都使用与该窗口对应的已验证任务内容；失败更新不推进 stamp，压缩前后内容相同则 stamp 相同。测试固定 revision、summary、token 数，仅替换等字节任务文字或 activeForm/explanation，断言历史指纹不同；仅改变 provenance 时相同，none/clear 也须可区分。该 stamp 只标识当前任务内容，不替代 Provider 对完整原生请求和 opaque 数据计算的字节证据。

### 6. 展示采用 Claude 的清单反馈，历史与当前状态分开

普通聊天在 composer 邻近、spinner 下方显示完成数和当前项。新成功非空更新自动展开；全完成自动折叠成完成统计，保留事实和详情，无定时删除。Ctrl+T 只切换当前清单的展示；无清单时无操作。展开保留模型列表顺序，空间不足时以进行中项及邻近项为窗口并明确隐藏数量；不依赖时钟或重排业务列表。每项预览只占一物理行，安全转义后按显示宽度在字素边界截断，不换行；任务行最多 6 行，正常空间争取 3–6 行。数量标题最多一行，有隐藏项时另有最多一行隐藏数量，因此展开区域总计最多 8 行；折叠只有一行统计。composer、错误与审批高度优先，极窄/极矮只显示数量或隐藏辅助清单。

Anthropic 当前项优先显示 activeForm，OpenAI 显示 step；完整面板保留原 content/step 和解释。纯渲染转义终端控制内容，NO_COLOR 不依赖颜色表达状态；resize 不做 I/O。

任务、补全、effort 通知与主线必须使用同一可用高度预算。固定 composer、队列、状态和高优先 modal 先分配；正在操作的 completion 其次，保证所选候选可见；任务区域最后分配。补全可见时任务降为一行统计，空间不足则隐藏，Ctrl+T 不得挤掉候选或改变其选择。任务实际行数纳入共享布局与缓存 key，禁止任务和 completionSpace 各自按整窗重复分配。组合 snapshot/PTY 覆盖 20 项任务、5 项候选、多行草稿、队列/状态、resize 和 Ctrl+T。

普通尺寸的示意如下；状态和文字均来自已提交快照，完整内容不受预览截断影响：

```text
Tasks: 1/4 completed                         Ctrl+T
  [x] Inspect existing behavior
  [>] Implementing task progress
  [ ] Run regression checks
  [ ] Review results

> Continue with the current change
```

只读 `/tasks` 不接受参数，查询并打开可滚动完整面板，包括 none/clear/全完成状态。它不是发给模型的消息，不进 recall/Session。补全内置目录增加到八项；同名 Skill 保持 `$tasks`。活动回合可查询，沿 effort 等现有本地控制优先路由，不能排队成用户正文。面板通过 Esc 返回并还原草稿/光标/主线位置。面板打开期间的新成功 sequence 原子替换当前内容；原锚点的完整任务标签仍存在时保留该项的阅读位置，否则把滚动位置夹到新内容范围，不新增任务 ID。明确 clear 显示清除状态，不自动关闭面板。

任务更新主线使用统一的紧凑工具活动，打断相邻 Read/Glob/Grep 分组。Ctrl+O 在原调用位置读取完整冻结输入与结果；不能把“当前最新列表”塞回历史调用。详情复用现有 input/result 正文查询和四页缓存，当前面板只持有同一有界当前快照，不复制完整历史。

Tool result Sync 后发布带来源 sequence 的 typed progress update；TUI 初始/resume hydration 和 `/tasks` 通过 typed query 取 coherent snapshot。迟到回复按 thread、来源 sequence 和 UI generation 丢弃；查询同一状态不再次触发自动展开。审批、问卷、Hook 信任等既有高优先焦点不被 Ctrl+T 或异步回复关闭，浏览层输入不泄漏给 composer。查询失败保留可识别旧状态并允许重查，不伪造新进度。

UI 本地另持 pending/failed/ready 的 hydration 状态，不写 Session，也不扩展业务 none/clear/snapshot。首次成功查询或有效更新事件前只显示加载/不可用，不能把尚未取得的结果显示成 none；首次成功未全部完成时展开，全完成时折叠，后续重复回复保持用户选择。恢复旧 none 回复晚于新成功事件时丢弃旧回复。

相同 result_sequence 只有完整 source、规范 TaskUpdate 与派生计数都相同才是幂等重复。同序号的来源、文字、状态、说明或计数矛盾应显示安全协议错误并保留已验证状态，不覆盖也不静默当作正常重复；较旧结果仍按既有迟到规则丢弃。none 也必须满足自己的唯一当前空分支。

### 7. Headless 与控制协议共用业务能力

任务执行不依赖交互宿主，`--print`、一次性 `--json` 和 stream-json 都能通过同一工具路径更新清单。一次性输出不增加专用任务事件帧；现有工具活动继续按原投影输出，新增内部任务投影明确过滤。

stream-json 外部 v1 增加无 payload 的 `tasks.query`，完整行最多 4 KiB；重复/未知/null 字段或混入 turn/input 参数在 admission 前拒绝。合法查询不创建 turn、不联网、不写 Session，支持 idle 和活动回合。按既有输出 owner 先输出一次 `control.response`，成功时再输出具有相同 request_id 和真实 Session/thread 的 `tasks.snapshot`，不伪造 query turn。快照完整表达三态、来源、清单、解释和派生数量；字段有严格 decoder/validator，完整外部编码最多 96 KiB，为 64 KiB 规范任务更新之外的来源和输出 envelope 留出有界空间，实际编码仍须预检且不截断。

只有显式查询输出专用快照，不新增 unsolicited 外部任务帧。普通 `input.submit/input.append` 中的 `/tasks` 仍为字面正文。新增能力不改变外部 v1 的身份与清理规则，也不引入任何内部 schema_version。

### 8. 分层与复用边界

| 层 | 职责 |
| --- | --- |
| domain | 不可变规范任务步骤、更新、三态与成功来源值；纯公共校验/clone，不识别 facade profile 或 Provider DTO |
| protocol | typed 查询、快照/更新投影与严格构造解码，只依赖 domain/codec |
| tool | 带 profile 的 TaskInput、两家来源约束与输入规范化、服务端 schema 模式、invocation/result、policy、budget、Activity/ResultCodec |
| tool/builtin | 纯任务结果执行器，不保存当前列表 |
| provider | 原生参数/输出与 checkpoint 编码、严格 restore、golden 和请求证据 |
| session | ready/started/result grammar、seq/Sync、恢复事实与 expected checkpoint 来源 |
| runtime | 有序 admission、提交后安装唯一当前投影、查询、压缩冻结 |
| app | 组装 executor、replay 和跨层 restore evidence；只读 transcript 投影 |
| tui/headless | 命令/按键/输出适配和纯展示，不拥有业务真相 |

沿现有策略组合和依赖倒置扩展，不用 struct embed/shadow 模拟动态分派，不创建通用 manager、可变全局 registry 或为尚未支持能力预留空 facade。纯构造器不访问外部资源。新的业务逻辑放入职责明确的任务文件，避免继续把解析、状态和渲染堆进 Runtime 主文件。

## Risks / Trade-offs

- [模型进度与实际验证不一致] → 界面与工具说明表明来源；完成数不改变执行结果、Stop、权限或回合终态。
- [取消与 Sync 竞争导致“界面先完成”] → 明确 writer admission 线性化点，使用门控 writer/Sync 故障注入验证两侧，发布仅在确定成功后。
- [共享结果成功但 native outputs 尚未完成] → 任务事实仍有效；恢复补配对输出，不重跑、不重新计费。
- [checkpoint 携带一份自洽的伪造列表] → 同时校验对应 source 前缀的最新成功 result、来源 identity、内容和 Provider wrapper；repair 前拒绝。
- [任务块让摘要候选超过上限] → 以完整候选做所有既有编码/窗口/缩减检查，失败保留旧窗口，不裁任务资料。
- [当前展示挤压聊天或打断审批] → 固定项数与终端高度预算、高优先 overlay、snapshot/PTY 覆盖，完整内容经只读面板阅读。
- [参考工程功能面远大于本切片] → 文档明确首切片与 Claude 当前默认 Tasks 的差异，不暴露 owner/依赖、计划权限或后台能力。
- [旧开发 checkpoint 不再可读] → 遵循唯一当前结构，更新固定 current bytes 并显式验证旧 shape 拒绝；不提供未授权迁移。
- [可选字段与 OpenAI strict 模式冲突] → 单 facade 显式 false、不可变目录身份与真实请求 schema 检查，本地 decoder 继续严格。
- [等长任务在数值 footprint 中无法区分] → 当前任务内容 stamp 纳入既有历史指纹，不改变稳定前缀或计量。
- [初次加载失败被展示为空，或同序号内容被覆盖] → 纯 UI hydration 状态与同序号规范内容一致性检查，失败保留已验证快照。

## Migration Plan

不存在业务数据迁移。apply 时直接修改唯一当前 typed 工具与 checkpoint 结构，替换仓库固定 fixture 和 golden；旧格式在 load/Provider 验证阶段失败关闭，禁止 resume 原地重写。先实现纯值/工具，再接通提交与恢复，再实现压缩和 UI/控制适配，最后运行质量门与交互矩阵。回退使用代码回退，不能让旧程序尝试改写新格式 journal。

## Verification

2026-11-18 合并 main 后，按用户要求采用远端“轻、准、关键、快”的测试原则。下表保留原始风险清单；当前以代表业务链和确定性故障回归验证，删除重复矩阵与逐值穷举，不宣称与早期全集等覆盖。实际保留入口、fixture 和本轮结果以 `verification.md` 为准。

| 范围 | 必须验证的业务断言 |
| --- | --- |
| 纯值和解码 | 边界 0/20/21、字节/转义膨胀、空白/控制/非法 UTF-8、重复标签/键、多进行中、null/未知/跨 facade、clone 隔离、允许回退及明确清除 |
| 有序工具循环 | Read→Task→Read、同 sample 两次更新、started/ready/result 顺序、无审批/Hook、无额外 I/O、后序工具等待前序结果 |
| 预算 | 确认文本按真实 bytes 计费、累计低额度、完整 metadata 不截断、失败不更新、turn 耗尽仍正确配对并沿既有收口 |
| 取消与 durability | writer 接收前取消、接收后取消、Sync 失败/不确定、结果通知失败、后序不得越过 poison；均用确定性门控，不用 sleep 证明 |
| Session | 固定 current 完成/清除/ready-only/started-only/result-tail/坏来源 fixtures；真实子进程 lease 竞争/崩溃释放和 repair 前零改写，继续 seq 单调 |
| Provider | 两家 request/stream/result/native golden，保留 thinking/signature/redacted/encrypted/phase/opaque；uninterrupted/restored canonical bytes、item 顺序、指纹等价 |
| 压缩 | 手动/自动/重复 compact；none/clear/部分完成/全完成；每个历史 source 独立核验；摘要忘记或虚构任务仍不改真相；完整包装超限/无缩减保留旧窗口 |
| 缓存 | 更新列表只改变历史；UI 折叠/查询/尺寸不改变请求；Anthropic disabled/5m/1h；OpenAI HTTP、WS 增量、checkpoint 后预期 reset 与重建 |
| TUI | 80×24、120×40、40×12、13×6、NO_COLOR、长中文/字素、Ctrl+T、/tasks、$tasks 冲突、Ctrl+O、草稿、resize、活动审批/问卷/Hook、迟到查询 |
| CLI/headless | 两家真实 CLI + 本地假服务，无真实 API key/外网；stream-json 分片/重复键/4KiB/响应顺序/活动查询/EOF/背压/关闭；一次性不混入任务专用帧 |
| 质量与文档 | `make verify`、change 和全量 OpenSpec strict 校验；README、能力清单、架构/Tool 文档、Roadmap、pitfall 与实际 vocabulary 一致 |

apply 的最终 `verification.md` 必须记录命令、结果、固定 fixture 与故障证据、实际平台及未运行范围。当前 Darwin 的真实 PTY 验收不扩大 Linux/Windows 原生支持承诺。验收后再按明确请求同步主规格和归档，不在提案阶段提前勾选任务或更新能力清单为已实现。

### 提案阶段已执行的基线检查

2026-11-18 在现有代码 `b0387c4`、分支 `feat/task-progress` 上执行 `make verify`，退出 0；格式、vet、Staticcheck、架构测试、全量测试和 race 均通过。`internal/app` 普通全量为 182.548 秒，race 为 928.458 秒，未缩减检查范围或放宽 20 分钟包级期限。

首轮提案检查时，`openspec validate add-task-progress --strict --no-interactive` 及全量 strict 63/63 项通过；当时 13 份 delta、46 项 requirements、56 项未勾选任务的对应关系及 `git diff --check` 通过。上述结果是现有代码与规划 artifacts 的检查，不是尚未实现的新任务能力验收。

### 二轮方案审查与闭合（2026-11-18）

重新只读核对两个相同 revision 的参考工程，并按用户确认修订现有 artifacts。以下是本轮发现的方案缺口及已选定的闭合方式；优先级表示实现前处理顺序，不是声称当前生产代码已存在未交付功能的故障。

| 项目 | 证据与失败场景 | 方案闭合与验收任务 |
| --- | --- | --- |
| R1 / P1：OpenAI schema 模式 | EasyCode `openai/request.go:89` 固定 strict=true，Codex `plan_spec.rs:49` 为 false；可选 explanation 配 true 会被支持该规则的端点拒绝 | 仅任务 facade 明确 false，本地保持严格，目录身份包含模式；2.5、3.2、3.5 在首个真实请求核验 |
| R2 / P1：等长任务内容身份 | `context/planning.go:481-496` 排除了工具正文，仅靠相同 revision/估算无法区分同长度 A/B | 同一历史 metadata 的 source-free 内容 digest；6.7–6.8 固定其余输入验证差异，不以长度变化代替内容测试 |
| R3 / P2：双 facade 的共享解码 | `tool/catalog.go:530`、`native_input_evidence.go:22` 只按 capability/wire 解码，两个 structured 任务入口缺来源选择 | profile 留 TaskInput，显式 family/profile，domain 为纯规范值；2.1、3.3、4.1 验证所有入口及跨 profile 拒绝 |
| R4 / P2：恢复证据消费者 | `app/compaction_restore.go:31-36`、`provider/restore.go:138-158` 原只附 Question，`session_preview.go:95-102` 也消费这条路径 | 实际 result seq 传 observer，联网与两家离线 picker 共用证据；4.3、4.6 验证伪来源/包装重算 checksum 仍拒绝 |
| R5 / P2：共享高度预算 | `tui/completion_view.go:35-36` 与 `chat_space.go:32-35` 已有高度预算，新任务最多 8 行不能再独立分配 | 补全选择优先、任务降级、同一布局缓存；8.8 组合任务/候选/多行草稿/队列/状态/resize/Ctrl+T |
| R6 / P2：首次加载状态 | 原规则只描述查询失败保留旧状态，首次恢复没有旧快照 | UI 独立 pending/failed/ready；8.9 验证阻塞、失败重试及初始过期 none |
| R7 / P2：同序号冲突 | 原“同来源幂等”未约束同序号不同内容，现有 `tui/effort.go:166-179` 有相同 revision 不同值拒绝范式 | 完整 source、规范更新和计数一致才幂等；8.10 逐字段冲突拒绝且保留已验状态 |
| R8 / P2：模型可见约束 | Claude `utils/api.ts:169-177` 实际发送 tool.prompt，详细 Todo 规则在 prompt.ts:144-178；maxLength 无法独立表示 UTF-8 bytes | 首请求 description 披露真实限制；2.7 验证规则曝光与 512/513 bytes 中文边界 |

额外复核确认无需 TaskRead：本切片只有模型写入者，普通回合保留原生调用/结果，压缩后保留精确当前快照；不因此扩大为 Claude 协作 CRUD。1 KiB cap 也不会自然触发整轮 BodyExhausted，2.6 仍增加 Task 后继续 Read 的业务回归。现有 13 份 delta 保持范围，实施任务为 64 项且全部未勾选。本轮只修订规划文件，未修改实现或新增依赖，上一节 make verify 仍只是同一代码基线的验证记录。

二轮修订后的规划检查：4/4 artifacts complete；全量 OpenSpec strict 63/63 项通过；13 份 delta 共 47 项 requirements、442 个 scenarios（包含完整继承场景），能力声明逐项匹配，所有 MODIFIED 块保留原主规格场景，64 项任务编号唯一且全部未完成。文件编码/换行/尾空白、模板占位与 `git diff --check` 检查通过。代码基线仍为 b0387c4，本轮未重复运行没有代码变化的 make verify，也未把规格检查当作功能验收。
