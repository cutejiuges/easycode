## Context

[提案](proposal.md)覆盖根会话 `startup`、`resume` 与 `compact`。基线 `7ec7708` 在 `app` 完成离线校验、恢复和资源装配，首个普通回合由 Runtime 生成真实 TurnID，已有自动压缩决定先于初始输入提交。手动和自动压缩共用 `runCompactionOwned`，已有成功记录 Sync、checkpoint finalize、宿主快照安装、PostCompact 收口和公共完成事件。两条路径均已有 Hook 信任界面和可取消的只读命令 owner。 [首轮接入点](../../../../internal/runtime/runtime.go#L273) · [压缩接入点](../../../../internal/runtime/compaction.go#L316) · [会话接管](../../../../internal/app/picker_owner.go#L403)

### 参考工程与取舍

| 参考快照 | 已核对的行为 | EasyCode 决策 |
| --- | --- | --- |
| Claude Code 2.1.88，源映射快照 `a8a678cb6` | `restored-src/src/utils/sessionStart.ts:35,129–174` 按来源执行并汇总独立上下文附件。`hooks/useDeferredHookMessages.ts:4–45` 和 `screens/REPL.tsx:3488–3501` 让界面先显示并在首个查询前等待。`utils/conversationRecovery.ts:556–568` 反序列化完成后使用实际恢复目标。`services/compact/compact.ts:587–594,637–642,719–743` 把 `compact` 上下文纳入新窗口和估算，PostCompact 单独报告本地结果。 | 界面先可用，活动状态简洁，用来源选择 Hook。启动与恢复的提示延迟到真实首轮，压缩后上下文与本地检查分别审查和结算。 |
| Codex `9f2b87495bf9` | `codex-rs/hooks/src/events/session_start.rs:23–79,243–324` 定义 typed 来源、matcher 和输出。`core/src/session/session.rs:2173–2203` 记录历史后排队来源，`core/src/hook_runtime.rs:133–189,892–917` 在实际回合消费并写入原生上下文。`core/src/session/mod.rs:5141–5153` 与 `core/src/session/turn.rs:828–853` 将压缩来源安排在安装后和同轮后续采样前，手动压缩可等下一普通回合。 | 启动与恢复复用真实回合 owner，压缩来源立即在已安装的压缩 owner 中结算，覆盖手动压缩后立即退出的场景。正文使用结构化、显式授权的用户层数据，提交成功取决于 Session Sync。 |

两份参考各有额外输出语义。Claude 源码接受成功 stdout 并另处理 `initialUserMessage`、`watchPaths`。Codex 可接纳纯文本回退和停止控制，其 Responses 包装也使用 developer 角色。EasyCode 采用一个封闭的 `additionalContext` 形状和现有权限层级。压缩阶段的外部表现接近 Claude，安装后原生交付及真实的操作身份沿用 Codex 的分层思路。

本轮复核进一步沿参考工程的生命周期追踪了四处差异。Claude 启动时已建立 Hook promise，恢复时则在安装或显示目标前等待结果。完整压缩与消息选择器的部分压缩都先执行 `SessionStart(compact)`，再执行 PostCompact。快速 session-memory 路径甚至在候选阈值验收前执行它，拒绝后还可能回退到完整压缩。EasyCode 以实际接管、通过输入预检或已安装 checkpoint 为边界，避免搁置候选产生信任和命令副作用，沿现有 PostCompact 顺序附加新阶段。 证据见 Claude `restored-src/src/` 下的 `main.tsx:2432–2440`、`utils/conversationRecovery.ts:556–568`、`services/compact/sessionMemoryCompact.ts:583–619` 和 `services/compact/autoCompact.ts:287–321`。

Codex 的进程内 FIFO 在 resume 首轮自动摘要后按 `resume → compact` 派发，回归测试明确检查该顺序。EasyCode 采用 `compact → resume`，压缩 owner 在归还首轮之前完成自己的 checkpoint 补充。Codex 手动压缩把事件留到下一轮，EasyCode 可在手动压缩完成后立即退出并恢复已确认正文。Claude 的常规及完整转录都隐藏相关附件，EasyCode 让用户在 Ctrl+O 主动检查已保存的模型文本。 证据见 Codex `codex-rs/core/tests/suite/hooks.rs:2263–2332,2545–2629`，以及 Claude `restored-src/src/components/Messages.tsx:459,504` 和同目录树下的 `components/messages/nullRenderingAttachments.ts:4–16`。

## Goals / Non-Goals

**Goals**

- 使用真实会话或 checkpoint 身份，精确控制命令执行、已保存上下文和模型请求的因果顺序。
- 复用冻结来源、声明信任、进程资源清理、原生 staged 提交及有界宿主查询。增量协议具有真实消费者、可离线验证且覆盖两家 Provider。

**Non-Goals**

- `clear` 与 `fork` 对应未来会话入口。可执行插件声明、异步命令、配置热加载及新增平台 sandbox 属独立变更。

## Decisions

### 1. 共同执行策略和两种接纳锚点

`app` 只在当前会话装配成功后为 Runtime 提供纯内存的 `startup` 或 `resume` 来源。准备中的切换候选可携带冻结描述，只有接管的实例能提交真实对话回合。AgentLoop 仍持有回合和手动压缩的专有接纳权。首轮在 `beginTurn` 和既有初始自动压缩结束后，先校验不含激活上下文的真实输入能通过普通请求、预算和记录 preflight，再由原始 worker 消费来源。仅待派发且有合法匹配时需要这一步。纯检查结束后立即释放 prepared owner。预检失败的输入按现有规则收口，保留待派发来源。首轮自动摘要可以先发出 Provider 请求。激活上下文进入的是首个普通 sample。取消先于计划接纳时可以保留待消费来源，计划成功 Sync 后该实例的激活尝试即消耗。合法无匹配、冻结配置无效、匹配超限和非交互在选择边界结束本次激活机会。共享启动阶段建立的 runner availability 仍沿原边界装配，新增派发前检只发生在符合条件的交互尝试。单独执行 `/compact` 只消费该 checkpoint 的 `compact`，保留本实例待派发的 `startup` 或 `resume`。这样 `/hooks`、只读导航和草稿可在第一问前使用，额外的 idle worker 和空 turn 均无必要。 [首轮顺序](../../../../internal/runtime/runtime.go#L274) · [恢复装配](../../../../internal/app/app.go#L527)

`compact` 的派发由 `runCompactionOwned` 完成。成功记录 Sync、安装 checkpoint 和初次 Context/Cache 投影后，等 PostCompact 完成清理并确定收口或确认未选中。确定的 PostCompact 失败或拒绝仍允许新事件，当前取消、PostCompact 不确定性或失效 Session 会阻止派发。同一成功 checkpoint 只接纳一次。手动压缩在发布完成前结算，自动压缩在返回原安全点前结算。这里需要补充当前压缩 owner 的阶段和续写预算，底层 checkpoint、Hook runner、工具配对及 Stop 交付已具备。 [现有压缩 owner](../../../../internal/runtime/compaction.go#L160) · [自动摘要续写](../../../../internal/runtime/auto_compaction.go#L118) · [Stop 候选](../../../../internal/runtime/stop_delivery.go#L84)

`HookTarget` 扩展 `session_start` 强类型分支。激活锚点包含来源和真实 TurnID，压缩锚点包含 CompactionID、Scope、成功序号和已验证 checkpoint 来源。自动 turn/sample 取 Scope，手动路径无二者。UI response 必须同时绑定 Session/Thread、锚点、plan digest、引用和 request ID。声明信任仍绑定进程内实例、事件、来源 revision 和原有命令 fingerprint，按原语义覆盖本实例内该声明 matcher 的后续所有匹配来源。审查同时显示配置 matcher 和本次来源，明确 `*` 的信任或拒绝也适用于后续 compact 等触发。reset 只影响未来派发。对其他事件的授权互相独立。配置在事件固定顺序最后加入 `SessionStart`，matcher 字面值为三个来源及 `*`。

### 2. 运行中的数据与持久事实

采用现有只读禁网 runner、目录和进程安全边界，按回合或压缩 owner 的取消信号清理。预检只处理实际平台、边界和 typed stdin。stdin 使用有限的会话及来源元数据，PostCompact 仍是唯一读取本次压缩摘要的后置事件。全计划通过逐条信任再顺序执行。每次派发共享 30 秒进程阶段预算，信任等待可取消。已确认失败可继续有剩余 budget 的后续声明。预算耗尽的未启动后缀保留真实处置，已完成的合法候选仍可提交。取消或执行不确定发生在 applied 接纳前时丢弃本次所有候选。复用现有严格结构化 `additionalContext` 解析与反馈校验，固定事件值，允许成功空输出。在声明顺序上选择最多 16 KiB 的完整包装前缀，单项内容最多 4 KiB。保留已接纳 Unicode、空白和换行，转义只用于确定模型包装和终端显示。原始流和控制字段只留在进程执行边界。单项候选的字节数和摘要从该项确切的最终模型文本计算。合计预算只求和这些包装文本字节，外层 wire 和存储转义另做完整预检。选择所有成功且已授权候选中按声明顺序可容纳的最长前缀，首个无法容纳项及其后缀都不交付。回放从有界 finished 证据重算前缀和对账，拒绝跳过可容纳项后选择更短的后续项。 [受限解析](../../../../internal/extension/hook/post_output.go#L11) · [运行预算](../../../../internal/runtime/hooks.go#L21)

新增四种 Session 事实。各事实使用强类型 constructor、strict decoder 和 validator。共享层只检查封闭元数据与 opaque Provider 信封，必要的原生语义检查留给 Provider。

| 事实 | 内容与边界 |
| --- | --- |
| `session_start_hook_planned` | 绑定锚点、稳定顺序的声明引用与计划摘要。配置无效或匹配超限可直接报告安全跳过。合法匹配在审查前先 Sync 计划。 |
| `session_start_hook_started` | 计划摘要、目标和实际序位。该记录 Sync 是派发准入点。 |
| `session_start_hook_finished` | 真实执行分类和有界统计。合法、有授权的候选仅留下来源及确定性包装的摘要证据，正文此时仍在内存。 |
| `session_start_hook_closed` | 封闭结果包含安全原因、已完成前缀和来源。`applied` 另有有序披露引用、包装字节摘要和唯一 Provider-native `session_start_context` 信封。其真实序号就是原生游标。其余结论只保存安全元数据。 |

激活的 `applied` 物理批次固定为 `[session_start_hook_closed, turn_inputs_committed(initial), provider_native_commit(user_inputs)]`，同 turn 一次 Sync。压缩 `applied` 为嵌入 native 信封的单条关闭记录，独立于原 checkpoint，验证目标确为刚安装的同一 checkpoint。其他关闭使用单记录。现有普通首轮输入 pair 和一般 `provider_native_commit` 的真实 TurnID 规则可直接使用。嵌入信封比放宽普通 commit 规则更易校验手动压缩的无 turn 事实，且避免持久化多份正文。 [现有普通输入语法](../../../../internal/session/input_commit.go#L44) · [原生提交 draft](../../../../internal/session/draft.go#L37)

完整记录在 Loader 把尾批判为可修复之前即验证固定 arity、batch index 和作用域。计划、开始、结束、未交付关闭以及 compact applied 均为合法 singleton。激活 applied 是 index 0 的三记录批次首项，后续已经完整读到的行也必须符合该前缀。compact 外层 `turn_id` 和根记录 `parent_thread_id` 必须省略，拒绝显式空值。真正撕裂的合法激活批次整批回滚，已完整但伪造 arity、索引或前缀的尾记录拒绝且保留原 bytes。尺寸预检复用 writer 的实际 encoder，按本次真实 arity/index 和最大宽度序号、批次 ID、时间及校验值计算，不能使用会被 arity 门禁拒绝的模拟超大 batch_size。 [Loader 批次边界](../../../../internal/session/loader.go#L156) · [现有尺寸预检](../../../../internal/session/input_commit.go#L109)

回放的 compact 阶段在 checkpoint 与 PostCompact 校验之后、Stop 或工具待交付分支之前消费事实。关联 checkpoint 包含 planned PostCompact 时必须先有本次真实后置关闭，且其结果已知并允许继续。拒绝绕过后置关闭，或在取消、结果不确定和仅恢复收口之后伪造 compact 计划。每条前缀推进真实最后 seq，applied 同时推进 native revision，即使 Catalog 用不物化原生条目的 metadata 路径。`startup` 只能出现在尚无已提交初始输入、也无较早 startup 或 resume 计划的历史前缀。接纳前取消留下的空 turn 允许下一条合法 startup 计划。实际恢复激活是宿主接管事实，由当前实例保证只消费一次，离线回放只验证持久化位置和关联。 [回放分流](../../../../internal/session/replay.go#L398)

writer 接纳后等待确定结果，按下列边界收口。压缩 checkpoint 的公共成功与 worker 的取消结果独立传播。手动压缩已保存后收到取消仍按既有停止规则返还 auto 队列，已显式排入的 strict-next 保留 FIFO、在清理完成后按原规则继续。公共成功不能让待返还的 auto 输入进入下一轮。健康的自动路径取消后只履行既有本地 Stop 反馈义务，不启动后续 sample。 [手动压缩回收](../../../../internal/runtime/compaction_loop.go#L327) · [Stop 取消收口](../../../../internal/runtime/stop_delivery.go#L175)

| 取消位置 | 必须保留的事实与行为 |
| --- | --- |
| checkpoint 已安装，SessionStart plan 尚未接纳 | 保留 checkpoint 和已知 PostCompact 结论，SessionStart 零事实、零审查及零执行。激活路径在 plan 前取消仍保留待派发来源。 |
| plan 已确认但首项 started 未接纳 | 等待原 owner 关闭，保存 cancelled，命令启动为零。 |
| started 已 Sync，runner 发现 OS 启动前已取消 | 使用真实结果保存 cancelled、无 exit 证据，零 OS 进程。 |
| 命令已执行，applied 尚未接纳 | 等待清理、收口已知结果，接纳前取消丢弃未提交上下文。 |
| applied 已接纳 | Sync 确定成功便完成一次 finalize 并保留上下文与同批真实输入，取消阻止后续请求。结果不确定则停止接纳，等待恢复核对并阻止输入重试。 |

已确认 Sync 但原生 finalize 或必要宿主投影失败时，交由下次恢复验证真实批次，当前不发请求、已保存通知或可重试输入。记录身份、编码或证据错配为独立错误。写入或必要清理不确定时 poison，含 Stop 在内的后续 durable 提交和采样均停止。待关闭计划的恢复先于原 turn/tool/Stop 补偿。在原 lease 下校验全量事实及原生信封，再进行尾部修复与本地收口。只在恢复关闭中标记无完成证据的已接纳序位未知，脚本调用和网络请求次数均为零。落在 checkpoint 与计划之间的崩溃已保存压缩结果，下次显式续接按新的 `resume` 语义运行。 [按序恢复](../../../../internal/app/compaction_restore.go#L16) · [PostCompact 前缀计账](../../../../internal/session/post_compact_replay.go#L30)

### 3. 原生准备、窗口和预算

两家 adapter 各自生成一个用户层原生消息，其文本块按声明顺序对应已接纳项。每项的共同包装使用固定的内容类型、触发来源及转义后的原正文。冻结声明引用、目标 ID、运行统计和摘要只在原生存储元数据中使用。整条记录采用新增的封闭 native kind，恢复逐项校验包装实际 bytes 及有序摘要。激活原生项紧随旧窗口、紧邻实际 initial user inputs，独立来源和原生游标便于追踪回放。使用 Stop 复合提交已经证明的单 owner、纯候选预检与批量 append 模式，复用其实际共通 staging 动作。Stop 的助手采样锚点和续轮状态机保持专属。针对 `compact`，以刚安装的 checkpoint 为 base，追加一次真实 suffix。请求迭代顺序自然为新原生摘要、已接纳上下文、后续条目，底层摘要原文和计量仍来自原 checkpoint。 [复合准备](../../../../internal/provider/session_start.go#L154) · [原生窗口](../../../../internal/provider/native_history.go#L129)

回放在 journal 层恢复闭合批次与 checkpoint 对应关系，Provider 再按顺序比较实际角色、包装字节摘要及来源。compact 项作为实际原生 ordinal 增加历史 revision 和 request footprint，原生采样资格仍以真实用户输入、闭合工具输出和 Stop 交付决定。两家 adapter 的 sampling gate 与 reducer/sample commit TurnID 目前都直接读取物理尾项。改由同一个经过验证的逻辑采样承接状态同时提供资格、真实 TurnID 和 sample index。compact 内容是请求序列的实际成员，但不推进这个状态，也不覆盖 Stop 的前次 assistant sample 或 pending calls 配对。自动压缩后无 steering 的 tool outputs 承接是必要端到端回归。静态验证完整历史时，检查点关系由带 checkpoint 顺序的恢复阶段验证。手动 compact 的语义来源挂在真实压缩锚点上，宿主查询按实际 ordinal/seq 排序，避免制造一个普通回合。复合提交安装全部原生游标后一次发通知。 [原生顺序器](../../../../internal/provider/native_sequence.go#L58) · [宿主原生游标发布](../../../../internal/app/transcript_projection.go#L175)

激活上下文带入首个真实输入的 Provider 请求和 Journal 完整信封预检。只有 typed 硬预算、Provider canonical 编码额度、单记录或整批额度不足才丢弃全部上下文并按原输入路径提交。错误分类在各边界保留到回退决策后再转换为现有安全消息。结构错误、无效作用域、stale staged owner 和 Session 故障使用原有错误处理。自动压缩判断已经完成，回退只重做候选检查。

`compact` 先确认带上下文的已安装窗口仍满足相对旧窗口的缩减和硬预算，再用将要继续的准确候选验证普通采样预算。手动压缩只有已知窗口和硬预算，未来输入仍走原先入场检查。自动压缩由三个调用点传递封闭的只读续写描述，分别是初始输入、闭合工具输出后可选追加、以及原 Stop 反馈与可选追加。复用已冻结文本及既有原生编码和请求编译，摘要前候选照旧 discard，任何预检都只持有一个 prepared owner。用纯候选验证组合，下游仍按原有边界重新准备。已确认超限只舍弃可选 Hook 上下文。精确的普通候选包括无 steering 的工具续写及其冻结模式和 effort。如果原候选本身超限，仍交还原流程判断，例如有 steering 的 Stop 候选返回输入并尝试已冻结的 feedback-only。结论不确定通过既有 Session 错误退出。仅有刚安装 checkpoint 与本次 compact 补充时维持 `nothing_to_compact`，避免用户立即再次压缩同一份本地注入。真实历史新增后，它参与完整窗口、来源摘要和后续缩减判断。 [候选缩减校验](../../../../internal/context/compaction.go#L55) · [普通输入预检](../../../../internal/runtime/input_commit.go#L74)

| 自动压缩安全点 | 下一次普通请求的相关顺序 |
| --- | --- |
| 首条输入 | 新摘要、compact 上下文、该实例若待消费的 startup/resume 上下文、真实输入及原附件 |
| 工具结果已成对提交 | 新摘要、compact 上下文、合法的已领取追加（如有），再开始下一采样 |
| Stop 反馈待交付 | 新摘要、compact 上下文、原冻结 Stop 反馈、合法的真实纠正（如有），再开始下一采样 |

新上下文只改变原生 `committed_history`，现有工具、项目指令、Skill 目录与缓存 key 仍由原来源计算。上下文元数据保存可诊断的内容及来源摘要，缓存比较基于真实原生历史和可见内容，避免用展示信息、执行 ID、信任决定或运行时间形成稳定前缀。后续压缩的来源摘要和新窗口估算包含已经交付的上下文。没有选中或实际披露时，原有请求序列直接复用。Context 使用新的 committed revision 和 footprint，当前历史指纹在 planner 计算，供请求证据和缓存诊断使用。`/cache` 中最近真实 sample 或 summary 的 receipt、计量和 baseline 只有实际网络提交后才更新。保留 `context_compacted` 和 `restored_baseline` 的现有原因优先级。恢复后的首个 OpenAI WS 请求没有可复用的 response ID，使用完整请求。压缩后首个普通 sample 也按现有 reset 完整发送摘要、补充和后续条目，只有随后已确认的可复用前缀才允许 delta。以相同后续输入和同一组已经提交的上下文比较 uninterrupted/restored，单独验证一次新 `resume` 接纳造成的预期差异。 [现有缓存分段](../../../../internal/context/planning.go#L488)

### 4. 宿主展示与阶段顺序

以下项目配置示例让同一声明匹配三个来源。脚本可依据 stdin 的 `source` 选择要补充的工作区状态。

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "id": "workspace-context",
            "type": "command",
            "command": "sh .easycode/session-context.sh",
            "timeout": 5,
            "feedback": "model"
          }
        ]
      }
    ]
  }
}
```

一次合法空白退出表示无上下文。下面是脚本有内容时的成功输出示例。

```json
{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Build and test with make verify."}}
```

`/hooks` 增加事件和来源标签。复用默认拒绝的详情审查，说明完整命令、读取权、当前来源与授权覆盖范围。授权文本会保存，在有效历史中继续随请求提供，也可能进入后续摘要。未能完整展示的窄屏按既有拒绝机制。第一问发送后以短状态显示工作，Esc 拒绝信任，Ctrl+C 取消整个当前 owner 并等待清理。实际保存的上下文在 Ctrl+O 用既有分页文本查询，主线只显示原始输入和活动提示。详情游标绑定真实序号，模型尚未收到字节时使用「上下文已保存」，不把 Sync 描述为已发送。

压缩沿用已安装 checkpoint 锚点，并为 SessionStart 增加同 owner 的独立阶段。没有 PostCompact 声明时也能用实际安装事实呈现压缩阶段。PostCompact 的终态和压缩成功事实保持可见。`CompactionCompleted` 的原 revision 和估算描述 checkpoint 本身，较新的已披露上下文由 SessionStart 关闭通知与其原生 revision 发布，Context 查询消费最新已提交快照。已打开的 Ctrl+O 列表以实际关闭后的游标更新 Through 并逐代刷新缓存，保留合法阅读锚点，拒绝跨会话世代、错误计划及取消后迟来的审查。PostCompact 与 SessionStart 各有独立计划和计数。若安装已确认但 control stream 提前关闭，界面保留已知 checkpoint 成功和安全的不确定状态，并禁止提交下一输入。 [现有压缩投影](../../../../internal/tui/compaction_presentation.go#L90) · [详情有界读](../../../../internal/app/transcript_queries.go#L205)

## Risks / Trade-offs

- **命令失误混入模型上下文** → 默认拒绝、最小 stdin 与明确披露策略。runner 流上限、结构校验、关闭时整批交付及错误输出过滤共同约束实际可见内容。
- **已安装窗口被可选内容挤满** → 检查窗口缩减及下一普通候选的完整预算。失败以原成功窗口继续，自动路径仍只做一次摘要。
- **物理原生项改变工具/Stop 前驱** → 双 Provider 顺序器按真实业务状态检查，具体用例验证初始采样、tool/Stop 配对及 checkpoint 后的原生 bytes。
- **进程中断或收到迟到结果** → 计划、执行和关闭严格关联合适锚点，取消等待原 owner 清理，离线只收口记录。发生写入不确定时禁止再接纳。
- **首轮增加等待** → 界面先可用，非交互和零匹配直接跳过，执行总预算与既有短状态约束等待。

## Migration Plan

内部协议采用唯一当前的未版本化编解码。新形状只在真实 SessionStart 事实出现时使用。变更涉及的固定 `testdata/current` 用提交后的字节校验，原有合法的零事件场景继续使用普通输入与压缩批次。部署随功能代码和匹配的文档、规格及 fixture 一起完成。回退前使用支持已写入事实的构建导出必要信息，旧构建遇到新词汇按当前严格契约失败关闭。
