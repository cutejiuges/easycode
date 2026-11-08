## Context

动机和范围见 [proposal.md](proposal.md)。本文保留 2026-11-08 二次推演后的设计及规划阶段历史；当前实施、主规格同步与归档验收状态见 [verification.md](verification.md)。

### 现状与已核对的接缝

| 当前来源 | 实际行为 | 本 change 的设计约束 |
| --- | --- | --- |
| 两家 reducer/native/commit | 原生推理已保存；semantic 目前只有普通文字；16 MiB commit 校验在最终编码处 | 新增展示不得改变原生保存；累计上限必须在追加前实施，不能借最终编码上限假称 staging 已有界 |
| `protocol/transcript_commit.go` | 只有 Sequence，输入和 sample 都会发布此事件 | 增加明确提交来源与 sample 映射，不能靠最近一次 commit 推测身份 |
| `runtime/runtime.go` | 合法 terminal 后的 sample 提交使用 WithoutCancel，先 Sync 再 finalize | 完成块、请求取消、持久化完成是不同事实，保持既有取消线性化 |
| `runtime/compaction_loop.go`、`agent_loop.go` | 只有 assistant text delta 跳过 contextSnapshot | 推理 delta 同样复用 committed 快照，覆盖生产与消费两个分支 |
| `app/transcript_projection.go` | native ordinal 对接 journal seq，call 精确连接 ready fact | 新增推理不改变 call/sample 索引，不解析 Provider wire |
| `protocol/transcript_query.go`、TUI browser | 50 项列表、64 KiB 正文页、四页共享缓存 | 主线和详情使用有类型的不同列表模式，隐藏推理不能仍挤占主线窗口 |
| `headless/projector.go` | 未知内部事件失败，已知非输出事件单独消费 | 新增事件/解析须严格校验后消费，不能默认忽略未知事件 |

### 两个参考工程的证据与取舍

参考工程实际位于 `/Users/cutejiuge/IdeaProjects/`，只读核对，没有修改或作为构建依赖。Claude 是 2.1.88 sourcemap；Codex 提交为 `6f8bf683cee83ed4d580a2e106b587d8451db936`，不据本地快照声称上游最新行为。

| 参考路径 | 核对结果 | 采用与取舍 |
| --- | --- | --- |
| Claude `restored-src/src/components/Message.tsx:539`、`Messages.tsx:391` | 调用层会在普通模式隐藏 thinking，详细记录可只保留最近块；不能只看子组件断言主线每块一行 | 采用活动提示和详情阅读；完成推理不累积主线独立行。EasyCode 详情保留全部可翻页历史，这是明确的自身选择 |
| Claude `components/messages/AssistantThinkingMessage.tsx`、`AssistantRedactedThinkingMessage.tsx` | 允许文字用弱化 Markdown；redacted 没有正文 | 复用既有净化和 Markdown；opaque-only 不可阅读 |
| Claude `services/api/claude.ts:2030`、`:2148` | start 的 thinking/signature 清空，再由 delta 累计，避免重复 | 保持同样原生累计口径；不为展示新增“从 start 恢复正文”分支 |
| Claude `utils/messages.ts:2976` | 流式到完成消息的交接在同批更新完成，避免空隙和重复 | EasyCode 在更严格的 durable 边界交接，不把 block stop 当成已保存 |
| Codex `codex-api/src/sse/responses.rs:381`、`core/tests/suite/items.rs:1023` | summary delta/done/raw 分离；协商 sequential_cutoff 后忽略 partial、消费 done，已关闭项的迟到 done 不接到新消息 | 本切片不协商该模式；固定默认增量/快照规则，区分 Provider 边界的可忽略迟到 done 与内部非法事件 |
| Codex `tui/src/app/reasoning_replay.rs`、`app/tests/buffered_replay.rs:39` | 活动 item 快照与完成历史分开，回放依赖明确身份和边界 | 采用身份驱动交接，避免把活动快照当完成历史或按正文匹配 |
| Codex `tui/src/history_cell/messages.rs:826` | 完成推理默认只在展开记录，活动标题不累积 scrollback | 活动提示与历史正文分离，Ctrl+O 复用单一浏览器 |
| Codex `core/src/client.rs:998`；Claude `services/api/claude.ts:1596` | 主动请求推理受独立设置/模型支持约束 | 保持被动展示范围，不照搬模型名单、默认启用或预算配置 |

选择推理展示的理由仍是：native 保存、提交、Markdown 和分页接缝已存在，可直接推进 P5；MCP、后台任务及 Subagent 还需要更大的权限与资源生命周期契约。新增范围是闭合该展示能力所需的契约，不扩大主动请求能力。

## Goals / Non-Goals

**Goals:**

- 允许文字、不可展示项、普通回答和保存状态各有封闭类型，Provider 决定白名单，共享层不解析 wire。
- 临时阅读身份明确连接 durable cursor，连续提交、列表淘汰和取消不会错配或重复。
- 高频增量不重算上下文、不阻塞交互；新增状态和每一条排队副本具有确定上限。
- 保持 canonical bytes、原生顺序、签名/encrypted content、fingerprint、工具授权和执行事实不受展示影响。

**Non-Goals:**

- 不新增 thinking/effort/budget/summary 请求选项，不保证模型返回可阅读文字，不协商 sequential_cutoff。
- 不展示 raw/encrypted/redacted/signature，不额外请求或生成摘要，不新增工具、权限、诊断正文或后台 owner。
- 不修改 Session/native/checkpoint schema，不增加 reader/version gate/migration，不将展示正文加入语义历史或压缩请求。

## Decisions

### 1. 白名单、正文与投影复用

Anthropic 只提取 thinking block 的 Thinking；redacted 或零字节 thinking 为 unavailable。OpenAI 只提取非零字节 summary_text parts，保留原序和各段所有空白，段间为两个 LF，零字节 part 不产生分隔；没有非空允许段则 unavailable。Signature、RedactedData、EncryptedContent、raw Content、Raw、Provider ID、Phase 和未知 part/扩展永远不进入展示值。

每家 Provider 内使用小的纯内存提取函数，供 reducer 完成核对和 TranscriptProjector 共用，不引入 registry/通用 manager。SemanticHistoryView 继续完全排除推理，既有 native 续写/压缩使用方式不变。

domain 新增 reasoning kind 与封闭 source/availability；其他 kind 出现 reasoning 字段拒绝。reasoning 正文以不可变有序 parts 值持有，不把多段正文反复拼成整篇字符串：复制集合、共享不可变字符串，通过有界范围读取计算双 LF 的虚拟偏移。Anthropic 为单 part。现有 TranscriptQueries 仍返回 text 页，app 在纯内存中取范围，UI 不获得完整 parts。这样不会因每次翻页重新拼接整篇 summary，分页字节与完整展示语义一致。

### 2. 一个推理事件种类与有界事件序列

新增 `EventAssistantReasoning`，分别提供 Started、Delta、Completed typed constructor、strict decoder 和 validator。Runtime 按真实 turn/sample 绑定 scope；Provider 阶段的同一结构只允许未绑定 scope，不接受伪造已绑定身份。payload 的 stage 决定必需/禁止字段，无 any、任意 metadata 或兼容 shape，不启用预留 ItemID/CallID。Provider 产生的当前事件保留现有未绑定 Session/Thread/Turn scope 规则，SampleIndex 固定为零，仅表示此时尚未绑定采样序号；Runtime 在当前 stream 边界显式覆盖真实 SampleIndex 并绑定完整身份，再由宿主严格校验，不用缺字段识别旧 shape。

| 阶段 | 必需数据 | 含义 |
| --- | --- | --- |
| Started | 本 sample 内 block 序号、source | 已观察到真实推理项，尚不判定正文可用性 |
| Delta | 同一 block、非空文字、连续原文偏移 | Provider 已确定的有序展示前缀；含必要的双 LF 分隔，不含 part ID 或 wire ID |
| Completed | block、availability、正文总字节数及有界保留范围/快照 | 块已结束，快照替换而非追加；仍没有证明已保存 |

block 序号在当前 sample 内单调分配、不复用，最多 4096。Provider 内部保留 raw ID/index 到该序号的关联；与 native item ordinal 的对应在 item 完成后确定。普通 text delta 与 reasoning 生命周期交错时，TUI 必须结束此前临时 assistant 段再接新段，防止 text A / reasoning / text B 被拼成 A+B 后再追加推理。

reducerResult 的推理输出采用最多两个强类型逻辑 draft 的固定序列：通常一个，OpenAI 完整 item 首次出现时为 Started/Completed 两个；已有块在最终 item 补齐缺失文字时为前缀文字/Completed。draft 仅在 Provider 内引用已受预算约束的不可变 source，不是已经构造的超限 protocol.Event。Provider 发布器按 UTF-8 将文字 draft 惰性拆成至多 16 KiB 的 Delta，逐个构造/校验/发送，不把全部分片物化为数组；每次发送前检查取消。完成快照按 UI 保留范围裁至 64 KiB 后构造。随后发 native item，最后才有 sample terminal。不存在为了 done-only 放宽内部“未开始就完成”的隐式路径；两个逻辑 draft 不等于两个 wire 字节块，也不限制合法大 delta 只能输出两个分片。两家 Provider 消费各自有界 draft，保持唯一 owner。

内部校验状态以 `{turn, sample, block}` 为键。正常 sample 成功必须没有未关闭块；Provider 失败/取消时允许仍有活动块，由真实 sample resolution 收口全部当前块，不能把缺 Completed 再报成第二个 turn failure。先前已提交 sample 不受影响。取消后 Runtime 仍验证并转发当前作用域的 Started/Completed 生命周期边界，只停止转发新的 Delta，避免已选中的 completed 随后提交成功却给宿主遗留未关闭块；TUI 的取消状态优先，边界不能重启 Thinking 提示。

### 3. Anthropic 保持原生 start/delta/stop 口径

thinking start 产生 Started，正文只由 thinking_delta 累计；signature_delta 只更新原生 signature。stop 产生 Completed 和 native item；redacted start/stop 走同样生命周期，但无正文。继续忽略 start 中的 thinking/signature 初值；不新增“start-only thinking 正文”接受规则。既有合法 native 历史恢复仍可直接投影其已保存 Thinking。

因此 completion-only 的服务端路径仅指下述 OpenAI 完整 item；不能写成两家都会从完成事件取得一个额外完整正文。Anthropic 的重复 index、错误 block 类型、未停止块和 message_stop 条件仍按原契约失败。

### 4. OpenAI 默认增量、段快照与完整 item 的交付矩阵

不新增交付模式配置，不发送 `reasoning_summary_delivery=sequential_cutoff`。采用下列唯一当前接受规则，Provider 内部先核对引用，再产生共享事件：

| 收到的情况 | 规则 |
| --- | --- |
| reasoning item added，含空/初始 summary | 建立 item/block 与 part 状态；非空初始文字计入该 part；后续从真实偏移继续，不重复累计 |
| part added + delta | 按 summary_index 保存独立 parts；只有引用可证明的 item/part 可追加；禁止猜测“最近的 reasoning” |
| 较后 part 先有文字 | 暂存，只有前面 part 已确定完成或最终 item 确认空段后，才发布连续展示前缀；新分隔只在下一个非空 part 首字节发布时加入 |
| text.done，没有任何该段文字 | 接受其 text 为完整段快照；不要求此前出现 part added/delta，但 item 必须已知且引用一致 |
| text.done，已有初值/delta | 快照必须逐字节等于已完整累计值；相同重复 done 幂等消费，冲突失败；done 后该段 delta 失败 |
| part.done | 与已知段核对并标记完成；没有 text.done 时完整 part 快照可完成该段；重复相同完成幂等，冲突失败 |
| output_item.done，没有任何 reasoning added/delta | 以完整原生 item 提取一次快照，发 Started/Completed；不要求原本未要求的 output_index/ID 字段，展示身份用本地序号 |
| output_item.done，已有活动 item | 引用及所有已收到文字与最终 parts 一致；有 delta 的段不得用最终快照静默纠正，未收到的段可从最终快照补齐；最终原生 item 决定完整正文 |
| 已关闭的已知 item 的迟到 text.done/part.done | Provider 边界丢弃，不发布内部事件、不修改已保存 item、不挂接新 item；直到 sample 结束保留有界关闭身份 |
| 未知引用、错配 output index、已关闭 item 的新 delta | 安全协议错误；不因引用异常去猜测或自动重放 |
| sequential_cutoff 的 partial 与 done 不同 | 本切片未协商该模式，不承诺其特殊语义；按当前冲突规则失败，不能声称支持 Codex 的这项交付模式 |

summary_index 必须为非负且小于 4096；超范围和同一活动引用内重复身份冲突失败。完整 item 允许跳过没有发出的中间空 parts；不能用不存在的空 part 修复已经冲突的数据。raw reasoning text 不进入展示。

原生 output items 沿现有完成路径及 wire 顺序保存，绝不为界面排序重排 native。展示块的完成时 native ordinal 用于第 5 节映射，活动到达顺序不冒充持久化顺序。

### 5. PreparedSample 的纯内存映射与明确提交来源

在 PreparedSample 增加不可变 typed 展示绑定表，每项为 `{block ordinal, native item ordinal, source, availability}`，最多 4096 项。Provider 在原生 item 完成且快照核对后构造；强类型构造器验证 block/位置唯一、边界及来源。零推理 sample 也使用当前结构的空表，不保留旧构造重载。该表与 envelope 一起被复制和验证，Finalize/Discard 后不可再次消费；不进入 envelope、请求、fingerprint 或 Session。

扩展唯一当前 `TranscriptCommittedPayload` 为判别联合：

- input 来源：只携带合法 Sequence，禁止 sample/bindings 字段；不解析活动推理的完成。
- sample 来源：携带 Sequence、SampleIndex 与必需的绑定表；只在 Sync/finalize 且宿主投影成功后发布。Runtime 在绑定表 native ordinal 前加 journal Sequence 得到 cursor。

不以“下一次 sequence”推测 sample；同轮追加输入和 sample 提交必须区别对待。普通 assistant 完成不一定有工具 ready facts，因此不能依赖工具 SampleIndex 间接补齐映射。

TUI 在 commit 到达时立即将活动选择转换为精确 cursor，并以同一范围交接；正文查询迟到不改变映射。目标不在最新 50 项时直接按已知 cursor 取正文，不退回按正文/名称搜索。连续 commit 合并 through/generation 时保持当前选择的一份有界绑定；其余已完成映射可释放。

### 6. 取消线性化、保存不确定与失败收口

本切片沿用 Runtime 现有决策：Provider completed 被选为当前合法结果并进入 sample 提交路径后，提交使用 WithoutCancel 并等待确定结果；不把“取消发生在 Sync 返回前”当作回滚指令。没有选中合法 completed 的活动 stream 取消则取消、排空并 Discard。工具 sample 已保存后取消工具，推理仍是已提交历史；最终 sample 提交成功后只能报告一个成功终态。

内部 turn failure 增加可选但强类型的当前 sample resolution；只在确有当前 sample 需要解析时出现，包含 SampleIndex 和封闭 disposition。无当前 sample 时禁止伪造该字段。它是当前语义联合的合法分支，不是旧 shape 兼容 reader；三个构造入口分别表达：

| disposition | 依据 | UI 与恢复 |
| --- | --- | --- |
| not_committed | 未进入提交路径、Provider 失败/取消，或已知接收前拒绝且无写入 | 所有当前块停止活动，标未保存；不伪造 durable cursor，恢复只读已有事实 |
| durable | Sync 已确认，随后 finalize 或宿主投影失败；附已知 Sequence/绑定 | 标已保存但显示刷新失败，不继续显示“未保存”；Runtime 按现有 poison/owner 规则停止，查询不可用不冒充成功 |
| uncertain | 已接收提交后写入/Sync 结果不能确认 | 标保存状态不确定，不声称 journal 无此项、不重写记录；重启后由既有严格恢复验证实际完整批次 |

正常 sample commit 使用上一节事件解析，不再在 failure 中重复解析。失败发生在之后的工具阶段且没有活动 sample 时，先前 committed 推理不能重新标记未保存。失败/取消释放全部当前校验 map 和 owner 状态，最多保留一份 UI 64 KiB 未保存/不确定快照供用户查看，开始下一 turn 或退出时释放并提示未保存快照已结束；poisoned Runtime 不接受下一 turn。

不增加独立“取消推理”命令或后台清理者；现有 turn terminal 是活动块失败收口信号。headless 校验 resolution 后继续只输出原有安全 code/message。

### 7. 高频事件不触发上下文重算

在 runtimePublication 生产和 AgentLoop 消费两处使用同一封闭的事件分类：普通文字及推理 Delta 为高频展示增量，沿用最近成功 committed 的 context/cache 快照，不调用 contextPlan、ProjectActiveContext 或 HistoryFootprint，不发布新的 committed revision。Started/Completed 也不改变 committed context，可以复用该快照；真正提交和上下文操作继续刷新。

不增加 timer/goroutine。Provider、Runtime 与宿主之间保持有界 channel 和背压，禁止按 delta 启动 tea.Cmd/后台解析任务。TUI 默认紧凑提示不解析正文；打开活动详情时复用既有有界 Markdown 增量缓存，只有文字变化才失效，未变正文和其他历史不重扫。

验收用 fake planner/projector 的确定计数证明：同一 native 历史和提交边界下，1 次与 10000 次推理 delta 具有相同上下文规划调用次数；事件处理中插入 interrupt、审批及草稿编辑，断言它们在最终输出前完成，不用 wall-clock/sleep 证明响应性。

### 8. 交互、列表模式与阅读交接

默认主线只显示当前活动推理的紧凑提示，块结束未提交时显示一次等待保存；sample 已提交后撤下该活动提示，不为每块推理保留永久独立行。工具/审批/问卷/取消状态优先，不能被迟到推理状态覆盖。没有真实推理项时不显示 Thinking；opaque-only 提示不可阅读，不据模型名推断。

扩展 TranscriptListQuery 为两个必需的封闭模式：main 排除 reasoning 后再取最近 50 项，detail 包含全部逻辑项并按真实 cursor 翻页。两个模式的排序、过滤和缓存键独立；只有这两个真实消费者，不添加自由筛选器。所有当前调用者/strict decoder 同时更新，不默认把缺模式解释为旧查询。这样隐藏条目不会仍挤掉主线可见消息。

Ctrl+O 仍是现有统一列表入口；活动临时项以 `{turn,sample,block}` 判别键并入 detail，不能塞进只允许 durable cursor 的协议列表值。Enter 阅读，Esc 从正文到列表再返回，Ctrl+O 关闭；r 切完整正文的 Markdown/安全原文，片段固定安全原文。活动详情引用同一受预算控制的缓冲，不复制另一整份正文。

commit 到达时使用绑定表升级键，保持该正文源偏移和选择；即使列表页被淘汰，也按 cursor 继续读取。用户关闭/改选后，旧 generation 回复只能释放，不能重新打开。查询失败时保留有界交接画面和准确保存状态，不按文字猜测映射。手动/自动压缩的辅助推理不发普通展示事件，完整历史 cursor 不因压缩重编号。

### 9. 各层预算与分配检查

| 资源 | 硬约束与检查点 |
| --- | --- |
| Provider 推理累计 | 每 sample 全部推理 source 的已解码逻辑文字/opaque 字段合计最多 16 MiB；delta 追加、part 创建或完整 item 替换前检查增量，不把重复 done 重计为新来源。最终 encoded native commit 的既有 16 MiB 限额仍单独校验 |
| reasoning 元数据 | 每 sample reasoning items/blocks 与 parts 合计最多 4096，任一 index 小于 4096；含关闭身份和待排序 part。新增元数据总预算 1 MiB，分配前检查，失败安全返回，不无限保留 tombstone |
| raw 与快照暂存 | 新增完整 wire/item/part 的副本也计入新增 source/scratch 的 16 MiB 暂存预算；完成核对使用已有 staging 和增量字节计数/摘要，不保留第二套全量推理正文；展示提取以不可变 parts/范围工作 |
| 单推理事件 | 每个 Delta 最多 16 KiB UTF-8；较大 wire delta 在 Provider 边界分成合法范围；Completed 快照最多 64 KiB。序列化 payload 硬上限 512 KiB，计入 JSON escaping，并在复制/发布前校验 |
| 提交映射事件 | 最多 4096 个小型绑定，序列化 payload 最多 512 KiB；只包含整数/枚举，无正文。超过预算在提交 admission 前预检失败，不能在 Sync 后才发现展示元数据无法编码 |
| 队列副本 | 两家 Provider 输出及 AgentLoop 输出各保留现有最多 16 项；每条受上述 512 KiB 限额，因此两个边界排队的新增展示 payload 合计不超过 16 MiB，生产/消费中的新增展示 payload 临时副本另限 4 MiB，在复制/编码前预留并核对实际峰值，不以队列限额代替。其余转发只用现有有界通道，不新增无限聚合 |
| 活动 UI | 全 sample 共用 64 KiB 正文和最近 50 个临时条目；按最近收到文字淘汰非选中项，选中项优先保留；仍超额时该项保留 UTF-8 尾片段并标真实范围。完成快照也受同一预算，不得每块额外分配 64 KiB |
| 查询与缓存 | 正文页 64 KiB，main/detail 的列表及正文与工具页共用四页缓存，活动详情不复制成第五份全正文；每页既有展示上限 256 KiB，Markdown 派生步骤/节点/容量预算不放宽 |
| 交接/回放 | 仅保留当前可见画面、当前选择映射及至多一份失败 sample 的有界快照；开始新 turn/关闭阅读/退出时按所有权释放，不随 commit 次数增长 |

完整事件流中的状态验证不能因 UI 淘汰而停用；纯显示预算耗尽只降为片段，不改变请求结果。Provider 硬资源限额耗尽则是明确协议资源失败，与 UI 宽度、展开状态和颜色无关。当前源码没有前述累计/条目保障，本 change 必须实际实现并测试，不能引用最终 commit 上限替代。

### 10. 能力声明与 headless

当前 Capabilities 的 ReasoningSummary 明确定义为可主动编译 summary 请求的能力，本切片没有实现该请求策略，保持 false；RawReasoning 保持 false。ThinkingSignature/EncryptedReasoning 继续仅声明已实现的原生保存能力。被动展示由已实现的封闭事件与 TranscriptProjector 契约表达，不新增空 capability 位、空策略或服务端支持推断。两家主规格同步澄清，不再将 thinking 文字与 signature 共同笼统称为不可见。

print/json/stream-json 共用 projector 校验推理 stage/scope、commit origin/bindings 和失败 resolution。可忽略的 Provider wire 迟到 done 必须已在 Provider 边界消费，内部迟到/非法事件仍失败。推理正文、范围、映射、保存状态均不新增外部事件或混入最终回答/诊断；外部字段、顺序、EOF drain、断管背压和唯一 cleanup owner 保持原样。

### 11. 验证矩阵与九项缺口追踪

下列文件的实际实施证据与命令结果记录于 verification.md。全部模型请求用本地 httptest、合成 key 和固定 fixture，不访问公网。

| 推演缺口 | 修订决策 | 必须交付的证据 |
| --- | --- | --- |
| 取消/保存状态过强 | 第 6 节 | `internal/runtime/reasoning_test.go`、`internal/app/reasoning_e2e_test.go`：提交前取消、提交接收后成功、Sync uncertain、Sync 后投影失败、工具阶段取消，状态与 journal/恢复事实一致 |
| 活动到 durable 身份缺失 | 第 5、8 节 | `internal/provider/prepared_lifecycle_test.go`、`internal/app/transcript_projection_test.go`、`internal/tui/transcript_state_test.go`：输入 commit、多 sample、相同正文、目标被 50 项窗口淘汰、延迟/连续 commit 的精确交接 |
| delta 反复重算快照 | 第 7 节 | `internal/runtime/reasoning_test.go`：planner/projector 调用计数相同、控制事件先于最终输出被处理；TUI 未变历史重绘不重扫 |
| OpenAI 交付模式混用 | 第 4 节 | `internal/provider/openai/reasoning_stream_test.go`：交付矩阵每行一个固定 fixture；未协商 sequential_cutoff、合法 late done 与内部非法事件分开验证 |
| 多 part 有序前缀缺失 | 第 4 节 | 后段先到、前段空/有 delta、空 part、不连续 index、文本/工具交错；最终虚拟正文与 committed 投影逐字节一致 |
| 两家 completion-only 混淆 | 第 2、3、4 节 | Anthropic start 忽略/重复保护；OpenAI item-only 恰好两个语义事件一个条目；max-two 逻辑 draft 与大文字惰性分片的终态/取消测试 |
| 全链路预算缺失 | 第 9 节 | 两家累计/元数据/escaping/队列/快照/显示/四页边界值测试；检查发生在分配前，超显示预算不影响 sample 成功性 |
| 默认体验对齐不准 | 第 8 节 | `internal/tui/reasoning_test.go`、固定 golden/PTY：完成推理不累积主线，main/detail 模式、活动详情、r/窄屏/NO_COLOR、问卷/Hook/审批优先 |
| 主契约与失败状态遗漏 | 第 6、10 节 | 七份 delta 全场景核对；`internal/protocol/reasoning_test.go`、`internal/headless/reasoning_test.go` 覆盖唯一结构、跨 kind/null/重复/未知字段、失败闭合与三模式零泄漏 |

保留现有 SemanticHistoryView 隐私负断言，为每种 signature/redacted/encrypted/raw/未知字段分别注入合成哨兵。更新展示 golden 不等于删除原隐私测试。双 Provider uninterrupted/restored 比较完整 canonical bytes、native 顺序和 fingerprint；Anthropic marker/TTL 与 OpenAI WebSocket full/incremental 按已有口径独立核验。

Sync、提交接收点、迟到查询、背压和 shutdown 用确定性门闩/故障注入，不用 sleep 或概率竞态。补 `scripts/reasoning-smoke.py` 的 Darwin 双 Provider PTY；其他平台结果只按实际命令披露。实施结束执行最终 make verify，并保留已有 Hook race 基线失败的真实记录。

## Risks / Trade-offs

- [被动展示时服务端可能完全没有正文] → 保持批准范围并明确说明；不借展示自动开启推理或增加成本。
- [新增 strict summary 规则拒绝未协商交付模式] → 请求不协商该模式，接受矩阵与拒绝场景显式固定，不假称支持参考工程全部能力。
- [取消发生在持久化过程中] → 服从提交接收线性化与真实结果，uncertain 不冒充未保存，先前 durable sample 不倒退。
- [新增绑定/失败解析与旧内部结构不同] → 同一 change 更新全部 typed 构造/解码/消费者和固定 fixture，不保留双 reader。
- [长 summary 的派生拼接与排队膨胀] → 不可变 parts 范围读取、分层预算、发布前编码预检和现有有界背压。
- [默认主线隐藏完成推理后不易发现历史] → 活动提示包含 Ctrl+O 入口，详细记录保留全部允许项；main/detail 分别过滤后分页。
- [全量验证已有失败] → 原样记录并在实施启动时先核实，不以隔离复跑通过覆盖完整门失败，不擅自扩大为 Hook 契约修复。

## Migration Plan

没有数据迁移。直接修改唯一当前的进程内事件、prepared 附属值和查询结构，替换相关展示/协议 golden；不改变 Session/native/checkpoint schema，不新增版本标记或持久化展示 ID，resume 不重写 append-only records。

完成实施和验收后才同步主规格、README、能力清单、Roadmap 与架构，并按独立流程归档。实施进度以 tasks.md 为准，实际验收记录于 verification.md；下方提案/修订验证记录保留其原始阶段语境。

## 提案阶段验证记录（2026-11-08）

以下为初次提案阶段的历史记录，当时 27 项任务均未完成；后续修订已重排任务。记录时只创建规划 artifacts，没有修改生产代码、测试或主规格。这些结果不能视为本次修订后执行的检查或新能力验收：

- `openspec validate add-reasoning-transcript --strict`：通过。
- `openspec validate --specs`：51 项通过、0 失败；长 requirement 仅为信息提示。
- `openspec status --change add-reasoning-transcript`：proposal/specs/design/tasks 四类 artifact 全部齐备。
- `git diff --check` 与新增 Markdown 的本地链接/行尾空白检查：通过；工作区改动仅为本 change 目录。
- `make verify`：退出码 2。分支策略、gofmt、go vet、Staticcheck、架构检查和全量普通测试通过；race 阶段 `internal/tool/builtin` 的 `TestHookProcessBoundsOutputAndPreservesExitClassification` 失败。该用例输出超限分支期望 `hook_output_limit`，实际为 `hook_outcome_uncertain`，stdout 计数 18000、CleanupConfirmed=false；其余 package 的 race 测试通过。
- `go test -race ./internal/tool/builtin -run '^TestHookProcessBoundsOutputAndPreservesExitClassification$' -count=1 -v`：单次隔离复跑通过（exit 0）。不能据此抹去全量质量门失败，也尚不能证明根因是调度或环境负载。

实现启动和最终验收必须关注此已有 Hook/进程清理基线失败；若需要修改其契约或扩大本 change 范围，应先单独确认并更新相应 artifacts。不得删除/弱化断言、提高 timeout 掩盖问题，或把一次隔离复跑成功写成完整 `make verify` 已通过。本轮未修复该实现问题。


## 规划修订阶段一致性与严格校验历史（2026-11-08）

当次授权只修订规划：proposal/design/tasks、原四份 delta，以及明确补充的 Anthropic/OpenAI/Runtime 三份 delta。没有修改生产代码、测试、主规格、OpenSpec 配置或其他 change；34 项实施任务全部未勾选。

- proposal 的七个 capability 路径与七份 delta 一一对应；两项新 capability 具有 Purpose，其余使用现有路径。
- 共 21 条 delta requirements、80 个 scenarios；所有 MODIFIED requirement 名称匹配主规格，并保留原 40 个场景，完整条款未被片段替换。九项推演缺口在第 11 节逐项关联设计与验收目标。
- 已核对取消/提交解析、活动到 cursor 映射、两家交付矩阵、main/detail 行为、高频快照复用、惰性分片、各层预算、headless 和请求/缓存边界在各 artifacts 中一致。
- `openspec validate --all --strict --json --no-interactive`：exit 0，53 项通过、0 失败（2 个 change 与 51 个主规格），仅有既有长 requirement 的 INFO 提示，无 WARNING/ERROR。
- 本 change 的 strict 校验、Markdown 本地链接/行尾检查、任务编号/未完成状态检查和 `git diff --check` 均通过；变更范围仅当前 change 目录。
- 本轮不执行实现测试或重新运行 make verify；前述初次基线 race 失败记录仍保留，不能把规划校验通过当成功能或完整质量门通过。


## 实施边界核对（2026-11-08）

实际采用 `assistant_reasoning` 单一事件 kind 与 stage 判别联合、`TranscriptListQuery.Mode` 的 main/detail、`SamplePresentation` 纯内存绑定和 `TurnFailedPayload.sample_resolution`；没有 Session/native/checkpoint 新字段或兼容 reader。实现时发现 ChatSession 还有适配缓冲，已在本 change 的转发预算范围内改为直接背压，Provider/AgentLoop 仍各保留 16 项。提交查询交接会保留旧有界画面、raw 模式和源锚点，直到准确 cursor 的新正文页接管。

第 11 节的目标路径由以下实际文件补足：`internal/provider/reasoning_binding_test.go`、两家 `reasoning_stream_test.go` 和 `reasoning_restore_test.go`、`internal/protocol/sample_resolution_test.go`、`internal/app/reasoning_query_test.go`、`internal/runtime/reasoning_test.go`、`internal/tui/reasoning_test.go`、`internal/headless/reasoning_test.go`、`internal/app/reasoning_e2e_test.go` 与 `reasoning_terminal_darwin_test.go`。原定旧文件的回归继续保留；固定恢复 bytes 新增在两家 Provider testdata，现有 Session/current fixture 不变。最终检查以 verification.md 的实际记录为准。
