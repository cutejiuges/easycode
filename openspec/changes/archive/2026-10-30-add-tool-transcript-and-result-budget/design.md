## Context

动机与合并范围见 proposal.md。当前没有其他活动 change。现有 `tool_scheduler.go` 按只读批次/独占屏障执行；`runtime.go` 先保存 result 再提交 native outputs；双 Provider 的 `history.go` 忽略工具并连接 sample 文本；Session 的 `CommandHistoryEntry` 和 app 的 `commandQueries` 只覆盖 Exec；TUI 用 role/text 记录主线，Ctrl+O 打开命令列表。命令存储已有安全 FD、跨进程 reservation、双流 capture、冻结 preview 与私有分页。

### 参考证据与采用范围

参考仓库均只读，不作为构建依赖。以下路径相对于实际参考仓库根；当前 worktree 的 `../` 不含参考工程，调研使用 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap` 与 `/Users/cutejiuge/IdeaProjects/codex`。

| 来源 | 读取路径 | 采用与调整 |
|---|---|---|
| Claude Code 2.1.88 | `restored-src/src/tools/{FileReadTool,GrepTool,GlobTool}/UI.tsx` | Read 行数、Search 数量、弱化技术字段；展示名与 Provider facade 解耦 |
| Claude Code | `components/messages/CollapsedReadSearchContent.tsx`、`utils/collapseReadSearch.ts` | 相邻只读分组、活动提示与稳定计数；不引入 memory/MCP/REPL 分类或 shell 文本猜测 |
| Claude Code | `components/CtrlOToExpand.tsx`、`screens/REPL.tsx`、`components/shell/OutputLine.tsx`、`utils/terminal.ts` | Ctrl+O 详细 transcript、默认少量输出；不照搬 feature 分支与非安全 ANSI 处理 |
| Claude Code | `utils/toolResultStorage.ts`、`tools/FileReadTool/{FileReadTool.ts,limits.ts}`、`query.ts` | 新结果决策冻结、Read 不递归保存、工具差异预算；不采用绝对路径引用、保存失败回退大正文或异步持久化替换 |
| Codex `6f8bf683c` | `codex-rs/tui/src/history_cell/{mod.rs,activity_preview.rs}`、`exec_cell/{live_output.rs,transcript.rs}` | 稳定活动身份、紧凑/详细同源、有界实时输出与独立省略信息；不复制大型 trait、Any 或快捷键 |
| Codex | `codex-rs/core/src/tools/{context.rs,context_tests.rs}`、`context_manager/history.rs`、`utils/output-truncation/src/lib.rs` | 头尾/元数据预算、Unicode 与序列化回归；恢复不重新按 policy 截断 |

调研基于源码与既有测试，不宣称已对两个产品每种开关做逐屏验证。目标是固定的 Claude 风格交互，不复刻全部实验分支。

## Goals / Non-Goals

**Goals:**
- 一次 apply 完成全部七工具的 live/restored transcript 与预算，最终提供一个可验收的纵向闭环。
- 用户展开记录、模型读取结果、Provider 续写分别拥有明确且可验证的数据边界。
- 预算进入 result durable 之前；展示终态来自 durable 事实；同轮追加不重置预算。
- 继续使用一个 Session writer、一个查询 owner 和一个当前协议，所有新增结构在本 change 有真实消费者。

**Non-Goals:**
- 不新增后台/PTY、自动 compact、跨 Provider fork、完整 Markdown 渲染、鼠标选择体系或通用插件 UI。
- 不新增持久化 diff/base 快照；审批仍展示完整当前候选，历史修改展示已保存计数/摘要。
- 不增加远程 token API、模型窗口猜测、CLI 预算配置或动态实验开关。预算是当前内核确定策略，参数调整须更新当前契约与回归。

## Decisions

### 1. 统一展示模型，保留原生事实源

新增小型 `domain` 封闭展示投影值（名称可按实现统一）：可见文本段、调用锚点、输入关联和提交位置。每个 kind 都有 typed constructor/validator；不引入 `any`、可变全局 registry 或可执行 opaque UI 数据。

Provider 提供纯内存 transcript projector，与既有 `ProjectHistory()` 共用各自私有文本提取函数，但不把工具记录塞入 token 估算用的 SemanticHistoryView。投影保持 native entry ordinal 和 entry 内 item/block 顺序；调用锚点含受控 call identity，不能包含原始参数 JSON、thinking/signature/encrypted data 或 phase。

app 将 native entry ordinal 与已验证 `NativeCommitRecord.Sequence` 对应，再将 call anchor 与 ready 的 ProviderCallID、sample/call index、invocation 连接。用户关联沿用 InputID。锚点缺失、重复、能力或顺序不一致失败关闭，不按时间戳或显示字符串补配。以 commit seq/entry 内序号形成稳定游标；调用状态更新不移动其锚点。工具结果放在所属活动详情，不把后发生的执行完成伪装成模型早先已见。

采用单独投影，是因为现有 SemanticHistoryView 丢失同 sample 的 text/tool 交错；直接拼接文本列表与命令列表无法恢复位置。拒绝在 TUI 或 session 中解码 Provider wire，也不持久化第二份正文/transcript 日志。

### 2. live 与恢复使用相同事实合成器

七工具的 ready、approval、started、result 经成功 Sync 后通知宿主投影。Runtime 输出统一的 typed 活动更新，Exec 进度作为仅 Exec 合法的有界变体；替换现有命令独占的 started/progress/terminal 结构，不长期维护两套消费者。approval request/resolved 继续保留其独立授权契约。

sample streaming 文本暂存为当前 sample 的 provisional 展示区域。sample durable/finalize 后以该 sample 的有序 committed 投影替换，且恰好一次；工具锚点只在完整 ready 后出现，partial args 不产生工具状态。stream 失败时未提交文本仅在当前界面标注“未完成输出”，恢复不伪造它已保存。已经独立提交的输入/前序 samples 保留。

Runtime owner 是唯一活动更新生产者；worker 不直接向 TUI 或 Session 发布结果。待审批、已准入、执行中、已完成、拒绝、取消、冲突、不确定是合法状态；非零命令 exit 在 UI 显示失败退出，但不改写内核确定执行状态。快任务可以跳过肉眼可见 running，不用 sleep 延迟 durable terminal。

headless 继续消费原有外部协议；新增进程内展示更新在宿主分流，不能使 `--print`、JSONL v1 或 stream-control 出现未知工具消息。

### 3. 宿主查询与 UI 复用

将 `session/command_history.go` 的投影扩为七工具事实，复用 ready/input 与 frozen result，不复制大正文。`app/command_queries.go` 演进成唯一 transcript/detail 查询 owner，保留取消、最终完成信号、generation 与私有 artifact 授权。旧命令专用 facade/observer/browser 状态在消费者迁移后删除；spec 路径 `session/command-history` 保留为既有能力的演进，不创建平行历史服务。

`protocol` 定义封闭的显示条目与有界查询，不能依赖 `tool` 或 `session`。app 执行转换。列表每页最多 50 个逻辑条目；正文页最大 64 KiB UTF-8，按逻辑身份和正文偏移继续；保存输出沿用最多 64 KiB raw/256 KiB escaped 的页。TUI 最多保留四个查询页与当前可见页索引，旧页按稳定游标重查；不得为了折叠先构造全历史所有渲染行。原始记录仍归已有会话 owner，本 change 不宣称 Session 已支持无限历史流式恢复。

主线使用独立 transcript state、grouping、tool presenter、viewport、detail panel，Model 仅路由，不增加万能 ToolManager。纯渲染函数只处理已取得数据，I/O 只经 tea.Cmd 到 app 查询；New/getter/View 不访问资源。

### 4. Claude 风格交互与边界

- 主线保留用户/assistant 正文，工具使用状态标记、短名称和缩进结果，移除持续的调试式 `Status:` 行。沿用 EasyCode 名称，不复制参考品牌。
- 正常相邻 Read/Glob/Grep 聚合；文件数按规范相对路径去重，搜索按调用数。ReadArtifact 独立显示为“读取保存输出”，不计入 workspace 文件数。用户输入、assistant 非空正文、Write/Patch/Exec、审批和异常打断分组。
- 一旦组内调用失败/拒绝/取消/不确定，显示明确异常项和结果，不藏成成功计数；组状态由已知事实算出，不按动画时钟决定。
- Exec 默认展示至多三行结果摘要；完整命令在详情，非零 exit 明确显示。显示名使用 Shell，不随 OpenAI/Anthropic facade 改变，不谎称 Bash 语法。
- Ctrl+O 进入/退出详细 transcript，初始定位当前主线锚点或最新活动；方向键滚动/选择，Enter 在有详情的活动打开二级详情。Esc 先关闭详情、再返回主线；恢复原草稿和滚动位置。详情 Tab 只在双流命令切换 stdout/stderr，搜索结果不伪装成 stdout。
- PgUp/PgDn 对逻辑页翻页，resize 保持活动 identity 与正文偏移；generation 丢弃迟到回复。用户浏览不产生模型或 ledger 副作用。
- approval 拥有最高焦点，暂停浏览输入并保存位置；Ctrl+O 在 approval 中不抢焦点。浏览中 Ctrl+C 对活动 turn 定向取消，idle 按既有退出行为；浏览 Esc 不触发主线取回/stop。普通字符/paste 不泄漏到 composer。
- 主线显示“结果已缩短”“仅保存部分输出”等用户语言；完整详情可展示精确 bytes、范围与“模型收到的内容”，不默认展示 fingerprint、seq、descriptor 或 ledger。
- 保留无色与极窄终端降级、控制字符净化和按显示宽度布局。历史 Patch/Write 展示真实路径/计数/状态；没有保存的历史 diff 明确不可用，禁止重读当前文件生成。

### 5. 预算口径与默认值

本合并提案明确选择一套可直接实施、需通过固定场景验收的初始策略，而不把默认值留给实现者猜测：单次沿用 Read/Exec/ReadArtifact 256 KiB、Glob/Grep 64 KiB；每 sample 总 preview 512 KiB，每 turn 2 MiB。这些是资源上限，不是 Provider token window 或经真实模型测量的最优值。若固定场景证明不足，应先更新本 change 数值和证据，再修改代码，不以延期预算或隐式放宽替代。

账本计量最终 `ModelPreview` UTF-8 bytes，包括行号、状态说明、省略标记、artifact 引用和分页头。Provider JSON escaping/framing 不计入此共享数值，仍须由实际 canonical request 编译与既有请求/Context 检查覆盖；禁止把 512 KiB 宣称为网络请求上限。添加引号/反斜杠/控制字符放大回归。

纯内存 ResultBudget 由 turn owner 持有。每个当前及未来允许调用预留 1 KiB 安全结果空间（总 call 上限仍为 64）；安全 fallback 不含无界路径/输入。按 call index 处理每个完整结果时：

```
allowance = min(capability_cap,
                sample_remaining - 1024 * later_calls_in_sample,
                turn_remaining - 1024 * remaining_turn_call_slots)
```

这里 remaining_turn_call_slots 排除当前 call，最多为 `64 - processed_calls - 1`，所有减法校验下溢。提交实际结果 bytes 后扣账，不按预留额扣账。小结果保留原样；不平均切正文，不按 worker 完成时间分配，也不修改既有 durable result。样本包含超过 call 上限仍走既有拒绝路径，不扩容预算。

Read/ReadArtifact 的可用 allowance 在 owner 按序 finalization 时应用；只读 worker 返回有界的 typed execution material（Read 的当前原预览+观察，ReadArtifact 至多 64 KiB raw window 和边界字节，搜索的有界预览材料+保存 owner），不提前声称这是 durable result。finalizer 不再读取 workspace/artifact。Exec 独占，allowance 在 candidate 准备前已确定并加入 fingerprint，审批后不改变。

当 turn 可用正文预算已不足（除安全结果预留外为零），不再准入新的 executor/approval；已经 accepted 的调用必须执行一次、完成 cleanup 并提交真实结果，未 accepted 的 ready calls 以 `result_budget_exceeded` 配对。完成当前 outputs 后 durable `turn_failed`（`tool_result_budget_exceeded`），不再采样/领取 steer。sample 局部不足只限制本 sample 结果，turn 尚有额度时允许下一 sample 缩小范围重试。初始预算及使用计数不进入 stable catalog，steering 不重置。resume 关闭旧 turn，新真实输入才新建账本。

### 6. 各工具 finalization

| 工具 | 决策 |
|---|---|
| Read | 原有范围/行长/硬上限渲染后若仍超当前 allowance，返回有界 `result_budget_exceeded`，提示缩小 offset/limit；不在事后默默裁短，不转存；观察 unavailable 并失效对应旧观察 |
| ReadArtifact | 从执行时取得的 raw window 依据 allowance 渲染实际消费范围；页头与转义算入额度；非 EOF 必须前进，否则安全预算错误；不产生 artifact/Write 观察 |
| Glob/Grep | 从本次确定性采集的范围生成稳定首尾预览和省略信息；超出最终内联预算时发布文本结果；无保存额度仍保持有界预览并报告 unavailable |
| Exec | 用冻结 allowance 计算头尾预览，并以最终预览是否完整决定 capture needed；不能先按 256 KiB 删除暂存、再因聚合预算省略正文 |
| Patch/Write | 只保留有界状态/路径/计数，不回显全文；必要时缩短显示路径，元数据保留真实相对路径；审批完整候选不受此正文额度影响 |

错误、拒绝、冲突、取消与恢复补偿均计入预算，不因缩短改变 status、call identity 或 wire kind。原始完整 Read 证据只有最终成功且完整的模型 preview 配对 native outputs Sync/finalize 后才激活。保存了正文不等于模型看过；ReadArtifact 永远不产生覆盖证据。

### 7. artifact 的有限泛化

扩展 `ArtifactStream` 为封闭的输出来源：stdout、stderr、search_result；仅 Exec 使用前两项，仅 Glob/Grep 使用后者。descriptor 继续绑定 thread/invocation/ID/digest/长度，新增来源 coverage 明确本次查询范围，不把 search_result 冒充命令流。

复用 `session/artifact` 私有根、FD 校验、no-replace、Sync、quota accounting、完整 digest 读取与清理。提取包内 shared captured-object/reservation 骨架，CommandOutputCapture 仍负责双流生命周期，新增 TextResultCapture 仅负责单结果；消费端小接口由 tool 定义、app 适配，不让 builtin 导入 session。

Exec 预留保持 32 MiB，搜索每 invocation 预留 16 MiB；单对象仍最多 16 MiB、thread 256 MiB、root 1 GiB。reservation 文件以当前严格格式保存实际来源与预留 bytes，不接受任意额度。只读批次中的搜索保存准入由 owner 在 started 成功后按 call index 完成，然后交给 workers，避免并发完成时序决定谁得到最后一份 thread 配额。其他进程的竞争可改变保存可用性，最终状态必须冻结，而非声称跨进程世界状态相同。

搜索 artifact 保存本次调用在既有 traversal/scan/input limit 下选中结果的确定性文本，包含相对路径和行号，位于 preview 的单行/总预算裁剪之前；不为了“完整”继续越过扫描上限。采集完整性（是否覆盖查询）与保存完整性（是否保存采集输出）分离：完整保存一个受 limit 限制的结果不能声称整个搜索空间完整。需增量写入并限制内存，禁止累积全仓库命中大字符串。UTF-8 文本对象保存至完整字符边界。

搜索 metadata 不再把被预览省略的全部正文重复写入 JSONL；保留有界模型所见 matches/统计、覆盖原因、保存状态和引用，完整采集内容只在 artifact。取消的未完成扫描丢弃未发布材料，返回确定取消；不能按文件系统竞态生成随机 partial 搜索结果。

调用 owner 持有暂存到 publish/finalization/cleanup；只读批次退出、result Sync 失败和 shutdown 均有唯一 cleanup 路径。达到磁盘额度或确定写失败可转 unavailable，不回退无界内存/原始大输出。阻塞操作未完成必须保留 reservation、store 和 thread lease。文件/目录 Sync 成功后才可产生 descriptor，result Sync 后才授权；无法确认 journal 提交则 poison，孤儿按既有安全恢复清理。

### 8. durable 与恢复

```
native sample + usage + ready Sync / finalize
  -> started Sync -> bounded execution material
  -> budget finalization + needed artifact publish/Sync
  -> frozen result Sync -> authorize reference / update transcript
  -> ordered native outputs Sync / finalize
  -> steering safe point -> next sample
```

保留只读有序提交和 exclusive 屏障。每个 result 增加必需的 typed 预算元数据（实际 preview bytes、分配上限、封闭处理原因）；它不是旧版本路由，不进入 native output 除非形成实际模型提示。恢复补偿使用当前安全结果策略并标明补偿原因，不能猜测缺失的历史正文。已有 result 始终直接使用已冻结 bytes，不重算预算、不访问 artifact。校验实际 bytes、能力上限、合法保存归属与 metadata 相互一致。

不新增 UI 日志 records。`tool_call_result` 扩展当前 payload；Session envelope/payload canary 5 -> 6，Provider native shape 不变仍为 2；外部 headless v1 不变。snapshot/fixture 使用唯一 `testdata/current` 固定 bytes。旧开发 journal 及旧 reservation shape 失败关闭，不自动迁移或删除；支持清理的当前 storage 必须在持有 thread lease 并校验 journal 后才能操作。

### 9. 单一完成标准与实现顺序

先实现投影/协议与预算强类型，再接 store/executors/runtime，随后统一宿主与 TUI，最后跑双 Provider 三种宿主与恢复/故障矩阵。此顺序只是同一 change 内部任务依赖，不是分期交付或允许单独归档。

必须验证：同 sample text/call/text、同轮 steering、乱序完成、Read->Write 证据、ReadArtifact 分页、搜索保存完整性、Exec 小输出因聚合额度转存、审批拒绝、非零退出、取消、配额/Sync 故障及崩溃结果/outputs 边界。UI 操作前后 journal/request bytes/fingerprint 不变；存储对象删除后恢复仍等价；source revision 仅随实际静态 schema/description 变化。

固定工作负载覆盖单文件定点读取、八工具并行搜索、大命令日志、64 call 耗尽与中途追加，记录请求 bytes、预览 bytes、保存 bytes、页数和额外模型读取次数（使用脚本化 Provider fixtures，不访问真实 API）。性能断言以有界材料/页缓存/算法计数为主，不以机器相关毫秒阈值证明正确性。真实 API 任务完成率不在当前离线验收中伪造。

## Risks / Trade-offs

- [跨层范围较大] -> 全部归入一个 tasks 清单与双 Provider 纵向验收，任何契约变化先更新本 artifacts，不静默删减。
- [有界预算可能增加定点读取次数] -> Read 明确可操作错误，固定工作负载对比；不平均分配或反复改写历史。
- [provisional 文本提交时布局调整] -> 按 sample 区域和逻辑锚点替换，保留用户浏览位置，不从文本内容匹配身份。
- [当前完整历史已驻内存] -> 本 change 限制查询页、布局和额外副本，不声称解决无限会话的全部内存问题。
- [存储失败与模型输出不同] -> 冻结真实保存状态与同一次最终预览，恢复不重试保存；跨运行等价以同一 durable 事实为准。
- [历史 diff 未持久化] -> 只展示可证明摘要，审批展示保持完整；不新增 diff 快照或伪装历史比较。
- [Linux 缺少原生 runner] -> 真实 Linux artifact/lease 路径为交付验证要求；若无法执行，明确阻塞或记录未验收限制，交叉构建不替代运行通过。

## Migration Plan

实施阶段一次替换当前协议及 fixture；同步 architecture、tool-system、Roadmap、pitfall 和 ADR（已有 ADR 的历史决策保留，新增决策说明能力演进）。先通过 strict OpenSpec 校验、契约/架构审查与 `make verify`，再同步 delta specs、核对词汇/路径后归档。禁止只更新数值而不更新固定 bytes/checksum。

现有开发 Session 不迁移，使用新存储或保留旧数据供旧匹配版本只读使用；回滚使用匹配旧 canary 的独立开发数据。不得在 resume 原地重写已提交 records，不能靠双 reader 保留旧命令 UI。此设计没有需要实现阶段自行决定的范围问题。

长期决策记录：[ADR-0006](../../../../docs/architecture/adr/0006-tool-transcript-and-result-budget.md)。
