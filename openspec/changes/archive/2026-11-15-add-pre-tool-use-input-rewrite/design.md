## Context

动机与功能范围见 [proposal.md](proposal.md)。本设计建立在 2026-11-14 的 EasyCode `8492ec25c489605f7ef6a3b42f15b86842f8717e` 上；调研阶段没有修改生产代码。

### 当前实现接缝

| 接缝 | 当前事实 | 本切片的变化 |
| --- | --- | --- |
| `internal/extension/hook/output.go` | Pre 仅解析空输出与严格 deny | 增加完整参数替换分支，保留独立事件校验 |
| `internal/domain/hook_input.go` | 三能力已有 Provider 无关的封闭输入 | 复用规范形状，补齐必要 getter 与完整替换校验 |
| `internal/tool/builtin/hook_process.go` | Runner 组合进程 owner 与纯输出策略 | 只增加 typed 输出传递，不改进程、沙箱和清理所有权 |
| `internal/runtime/hook_gate.go` | 按声明顺序逐项 Sync、信任、运行、保存 finished/gate | finished 后更新下一项的检查候选，gate continue 后才选择工具执行输入 |
| `internal/runtime/exclusive.go` | Pre、后置信任、候选/审批、执行与 Post 共用独占骨架 | 将最终有效调用贯穿全部消费者 |
| `internal/session/replay.go`、`hooks.go` | 审批 metadata、result 均以 ready 调用核验 | 增加原始调用与有效调用的显式证据，不能直接替换 ready |
| `internal/session/tool_history.go` | 历史从 ready 与 result 构造 | 展示原始请求、有效参数与逐项来源，不读脚本 |
| `internal/tool/model_output.go`、Provider restore | 冻结结果、后置反馈及原生配对共同校验 | 增加改写执行说明及其证据，不改原生调用参数 |

### 两份只读参考与实际取舍

参考根位于主 checkout 的同级目录；当前 worktree 的 `../claude-code-sourcemap`、`../codex` 不存在，实际只读来源分别是 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap` 和 `/Users/cutejiuge/IdeaProjects/codex`。不修改、不构建依赖、不复制其中实现。

| 参考 | 已核对源码 | 采用与差异 |
| --- | --- | --- |
| Claude Code 2.1.88 | `restored-src/src/types/hooks.ts:66`、`services/tools/toolHooks.ts:436-567` | 采用 `hookSpecificOutput.updatedInput`、无权限决定的透传改写及普通审批分离；保留 EasyCode 三能力规范输入，不承诺完整外部脚本兼容 |
| Claude Code 2.1.88 | `services/tools/toolExecution.ts:773-838`、`:1130-1215`；`toolHooks.ts:333-433` | 采用原始可观察输入与实际调用输入分离、无决定改写进入正常权限路径；其 allow 分支可跳过普通提示但仍检查 deny/ask，EasyCode 继续使用独立普通批准 |
| Codex `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee` | `codex-rs/core/src/tools/registry.rs:194-275`、`:1114-1153` | 采用每能力正反向适配、保留原调用身份与不可修改执行范围；不采用通用动态 JSON 作为进程内协议 |
| 同一 Codex 快照 | `core/src/tools/handlers/apply_patch.rs:370`、`handlers/unified_exec/exec_command.rs:336` | freeform patch 与 function command 各自处理，说明不能把 Hook 对象直接当成 Provider wire；EasyCode 继续使用已有规范 Patch/Exec 输入 |
| 同一 Codex 快照 | `hooks/src/events/pre_tool_use.rs:144-176`、`hooks/src/engine/output_parser.rs:441-481`；`core/src/tools/registry.rs:883-934` | 此快照用 allow+updatedInput 表达替换，随后仍进入原生 review/工具权限；不能把这个 allow 误解为自动批准。竞争改写取最后完成项，EasyCode 明确采用串行候选链 |
| Claude Code 2.1.88 | `utils/hooks.ts:2120-2170`、`:2744-2878` | 同批 Hook 共享不变 stdin 并并行收集结果。EasyCode 后项读取前项候选是明确的行为差异，不宣称按 Claude 原样兼容；文档必须提示检查/改写顺序影响语义 |
| 同一 Codex 快照 | `core/tests/suite/hooks.rs:4113-4178`、`:5250-5328` | 测试同时断言原目标未发生副作用、改写目标确实变化及输出配对。EasyCode 采用相同业务断言思想，保留当前单已有文件 Patch 范围 |

Claude 恢复源码的 SHA-256：`toolExecution.ts=77aae23a5b8b68aa1ee32b99eb25c6360071d7a4c4c46e6d2c95d584fb6e25e0`；`toolHooks.ts=a2c02fca0f84cfa8995a5c6dfa11f8527ad5bc3cfe39b355a98d2f166c3e034a`；`types/hooks.ts=d64920bda327934c117546b3ffc55cefb1cf567656e8ea3e8f2fbaeefb6ff6c1`。版本与源码只描述本地参考快照，不代表上游最新行为。

### 复核证据与方案补强

本轮重新核对上述两个源码快照及调用后的消费者，确认 Codex HEAD 和三份 Claude 文件摘要均未变化。下面的规则已并入后文与对应规格，不能只作为实现时可选建议。

| 原方案不足 | 直接证据 | 最终处理 |
| --- | --- | --- |
| 只限制原始 stdout，未约束规范编码膨胀 | `internal/codec/json.go:84-85,141-143` 使用 EscapeHTML=true；使用同一 Sonic 配置的离线探针中，含 12,000 个 `<` 的 Write 输出为 12,108 bytes，规范输入为 72,039 bytes，已经超过当前 finished 的 65,536 bytes decoder 上限 | 规范 updatedInput 独立限制 16 KiB，并核验完整 finished 编码；超限不接纳，不提高通用上限 |
| 同能力验证不能锁定来源语义 | `internal/tool/tool.go` 的 Patch ready 只验证联合与一般输入；`patch_decode.go` 中 Edit 只有一个子串 replacement，而 apply_patch 为 LF 行替换 | 用源调用导出的封闭匹配 profile 校验数量、line_based、replace_all 与 LF；普通 PatchInput.Validate 只是其中一步 |
| 中间候选与可执行参数混用 | Codex registry 在 Hook 后重新 review 并清除原 payload 的 serving_assessment；当前 EasyCode 在 gate 前禁止候选与普通批准 | 明确 O/C/E 三种身份；只有 continue 才产生 E，中途阻断沿原调用的非执行结果收口 |
| 要求所有正常结果都附完整参数，遗漏低预算拒绝与取消 | `runtime/exclusive.go` 可在 Post 信任失败、candidate 准备或 started 前直接保存结果，此时没有 ExecutionAdmission | 以真实准入及恢复事实决定完整/短说明，不能根据错误字符串或 success 猜测 |
| 准入证据若只从 Post 状态取得，无 Post 会丢来源 | `runtime/exclusive.go` 的准入先是局部值，仅在 Post 存在时复制到其 state，result 却在函数返回前保存 | started Sync 后将唯一准入值写入当前调用执行证据，早于 executor/结果冻结；Post 和说明引用同一值 |
| “以后扩展投影”缺少实际提交路径 | 当前 HookHistoryObserver 只交付 ref/result，无 effect/sequence；HookActivity 上限 16 KiB | 以有返回值的 Pre 已提交快照替换该窄回调，先安装后通知，并给投影单独的编码预算 |
| 取消的交界未写清 | `hook_gate.go` 用不可取消 context 保存已完成项，最后成功 finished 与 gate continue 同批；`session/hooks.go` 不接受全通过后的 gate cancelled | 保留真实 finished/最终 continue，晚到取消阻止后续普通执行，不改写为未运行或取消的 Hook |
| 缓存身份与退役时机不足 | `runtime/hooks.go:94-101` 在 turn 边界清空运行状态；当前 completed 仅持有 result | 持有完整原调用与所选输入证据，同 turn 重入只复用，跨 turn 清除；不恢复 grant 或沿 ID 重新执行 |

离线编码探针只使用临时目录及已安装 Sonic，网络关闭，没有运行任何脚本或工具副作用，也未改动项目代码。它证明了编码大小遗漏，不代表新功能已经实现。

### 为什么当前优先选择这一块

| 候选 | 当前基础与收益 | 本轮判断 |
| --- | --- | --- |
| PreToolUse 参数改写 | 已有三能力 typed 输入、串行 Hook、durable gate、普通审批和四类事件；直接补齐 P6 明确的 block/rewrite 验收 | 选择，形成有限且完整的用户闭环 |
| UserPromptSubmit | 可以复用进程 owner，但要重新定义初始/追加/strict-next 输入、拒绝返还和上下文交付 | 保留为独立输入生命周期变更 |
| MCP | 当前只有批准占位，尚无动态 catalog、远端调用准入、server owner 或结果恢复消费者 | 用户收益高，但新增边界更多，宜独立完整设计 |
| 运行中推理切换 | 已有启动冻结设置，另一 worktree 已使用 `feat/runtime-effort-control` 分支 | 避免重复选择；分支名不作为其已实现的证明 |

## Goals / Non-Goals

目标是保证同一真实调用具有可审计的原始参数、确定的有效执行参数、与后者相符的批准和结果，以及零外部重跑的恢复。所有新增公共类型使用强类型构造、严格 decoder、深拷贝与 validator，依赖继续由下至上。

不扩张 Hook 支持事件、模型工具目录、平台沙箱、会话迁移或现有权限范围。不允许 Hook 修改 invocation/call ID、capability、wire kind、Provider、workspace、shell、环境策略、sandbox、预算和普通审批决定。不在 approval response 中编辑参数。

## Decisions

### 1. 只接受三能力的完整规范参数对象

Pre 的 exit 0 继续支持空白、严格空对象和现有严格 deny；新增且仅新增以下独立分支：

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "updatedInput": {
      "command": "go test ./...",
      "workdir": "",
      "timeout_ms": 120000
    }
  }
}
```

`updatedInput` 是当前 stdin 中 `tool_input` 的完整替换，不是模型 facade 原始 wire，也不做浅合并/深合并或默认值补齐：

| 能力 | 必需字段 | 再验证 |
| --- | --- | --- |
| Patch | `file_path`、非空有序 `replacements`；每项含 `old_text/new_text/replace_all/line_based` | 复用 Patch 输入语义，并校验下述来源匹配 profile；保留单已有文件与 wire kind |
| Write | `file_path/content` | 复用 Write 限制；新目标仍按实际 create/overwrite 及完整 Read 资格处理 |
| Exec | `command/workdir/timeout_ms` | 复用 Exec 命令、工作目录、1000..600000 ms 限制；不改变固定执行策略 |

未知/缺失/重复字段、null、错误类型、跨能力形状、非法 UTF-8、尾随 JSON，以及 deny 与 updatedInput 混用均失败关闭。allow/ask、additionalContext、旧 decision 和 async 继续拒绝。先按现有 cleanup/取消/超时/输出/I/O/退出码优先级分类，只有正常 exit 0 才接纳替换。

当前输出策略回调只接收 event/policy/exit/stdout，不具备解码三能力替换所需的输入语义。apply 时让它接收来自已验证 stdin 的强类型输入上下文，由其中真实能力选择 decoder；不能让 stdout 自报 capability，也不能先传播未校验 RawMessage 到 Runtime 后再猜测。Stop 和两个 Post 保留自身分支，不构造伪工具能力。

该上下文还必须由原调用的强类型 wire kind 导出封闭 Patch profile，不能按脚本声明或字符串启发式选择。Edit profile 只接受一个 replacement，line_based=false，replace_all 可按现有 Edit 语义修改；apply_patch profile 只接受 1..1024 个完整 LF 行 replacement，全部 line_based=true、replace_all=false，old_text/new_text 不含 CR，并满足当前行终止符规则。line_based 是来源匹配模式，不是可自由切换的参数；Hook 不能借规范中间形状获得源 facade 没有的执行模式。Write 的 content 允许空串、Exec 的根 workdir 允许空串，但成员仍必须存在。

profile 的封闭值与纯校验归 domain，语义使用子串/行替换等输入约束，不依赖 Provider wire。tool 从真实 ReadyCall 的 wire kind 纯映射并绑定该值，随已验证输入上下文交给输出策略；extension 不导入 tool、Provider 或自行匹配 wire 名称。保存与恢复复用同一映射和校验，不能形成第二套来源语义。

stdout/stderr 各 16 KiB、stdin 512 KiB、每调用全部 Hook 总执行 30 秒保持不变。规范编码后的 updatedInput 还必须独立不超过 16 KiB；原始输出合法但规范对象超限属于不支持的参数形状，以 hook_output_invalid、unchanged effect 收口，不伪报实际 stdout 超限。完整替换因此不能覆盖任意 1 MiB Write；对象和规范文本都不自动截断。被接纳的检查候选作为必要事实保存，gate 选中的参数按第 6 节向模型提供完整或短说明；信任审查明确用途，不保存原始双流或自由文本原因。

各表示分别校验实际编码：原始双流 16 KiB、规范替换 16 KiB、含身份/空反馈/effect 的 finished 沿用当前 64 KiB、后项完整 stdin 512 KiB、模型说明按最终 base allowance、宿主改写投影按第 10 节预算。检查必须包含 JSON 转义和 framing；不能拿原始长度、rune 数或另一个表示的额度代替。合格替换和有界身份应能生成合法 finished；若 runner 绕过边界返回错身份、错 profile 或超限 typed 候选，按既有 runner 错配归一为 Hook 不确定并 poison，不当作普通脚本解析错误继续。

选择完整对象是为了让脚本、审查、持久化和重新验证共享唯一语义。部分对象合并会把缺失字段、默认化及 Provider 别名引入第二套行为。

### 2. 区分原始调用、检查候选与选中执行输入

在 `tool` 边界提供纯内存的调用到 Hook 输入投影及反向适配，供 Runtime 和 Session 共用。反向适配只构造同一能力的 typed input，保留 invocation ID、Provider call ID、wire kind 与真实 call index；不生成新的 Provider call，不序列化或改写 native item。

| 值 | 产生与含义 | 消费边界 |
| --- | --- | --- |
| O：original | 唯一 ready 中的原始 typed 调用，与 native sample 对照 | 原生配对、原始请求审计、Pre 未通过时的非执行结果 |
| C：checking candidate | 从 O 开始，按已 Sync 的 effect 推导；可能在后续检查被拒绝 | 仅下一项 Hook stdin、检查候选详情和审计 |
| E：selected execution | 只有完整 gate continue 才从最终 C 选择；身份仍属于 O | 普通候选、policy/approval、执行、结果 metadata 与 Post |

共享执行证据使用同一当前契约的 unhooked/gate_rejected/gate_selected 封闭分支。unhooked 绑定 O，只表示没有 Pre gate facts，包含既有建 plan 前失败的非执行路径，不代表绕过来源或权限检查；gate_rejected 保留候选链但没有 E，结果 metadata 按 O 的非执行规则验证；gate_selected 绑定真实 plan、完整 finished 前缀、continue 决定及推导的 E。最终 E 即使等于 O，也不能丢弃已检查证据。是否添加改写说明由 E 与 O 的规范内容比较派生，不能用是否存在 effect 或指针身份猜测。

未配置 Pre 的九项工具消费 unhooked 证据；只有三项 mutation 消费另外两个实际分支。它们不是 nil 回退、旧格式 reader 或发布兼容选择，不把未选中的 C 塞进可执行 Invocation。domain 的候选值与 effect 不依赖 tool；tool 持有共享纯适配，Runtime 和 Session 使用相同规则。

使用一个组合值而不是到处覆盖 `Invocation`，因为恢复时仍要证明 ready 与 native sample 原文一致；审批和结果 metadata 又必须对应实际参数。相同转换逻辑不能在 Runtime、Session 和两家 Provider 中分别实现。

### 3. 按已有顺序形成确定性串行处理

匹配集合、引用与计划在运行前冻结，依旧是 user 来源、project 来源、各数组位置；改写不重新匹配或新增 Hook。第一项读取 O 的规范输入；以后每项读取前一步已成功 Sync 的 C。每次接纳都再次执行完整能力及来源 profile 校验。

输出对象经过规范编码后与当前 C 相同，视为 unchanged；不增加伪变化。替换只在本项 finished 成功 Sync 后安装为 C；下一 started 不得早于该提交。中间阻断、错误、取消或清理不确定立即停止，候选前缀只作审计，不产生 E，不调用工具，也不退回原始参数执行。该 gate 拒绝/取消结果继续关联 O 的安全 metadata，避免把检查中的新 workdir 等字段误报成实际执行范围。

如果多项真实变化最后将参数改回原始值，逐项 effect 仍保留审计，但最终不加改写模型说明；候选和执行按最终原值处理，不把中间值误作实际执行参数。

选择顺序组合是因为当前实现已经串行，下一条验证脚本可以检查前一步的结果。代价是顺序影响语义；配置浏览和文档必须明确，不能声称早先脚本自动重检了后来改写。

### 4. 在现有 finished 中冻结必要证据，不新增日志事件

复用 `tool_hook_gate_planned`、`hook_execution_started`、`hook_execution_finished`、`tool_hook_gate_decided`，不增加 record kind。计划和 gate 的含义与现有原子 batch 顺序保持。

Pre 的 finished 增加必需封闭 `input_effect`：

- unchanged：只有 `kind/before_digest/after_digest`，两个摘要相等；表示该次未产生被接纳变化。
- replaced：包含同样关联及一个完整的三能力 typed input；其摘要与 after 相符且不同于 before。只允许正常 execution-origin、exit 0、continue/ok、cleanup confirmed、非取消/超时的实际项。

摘要统一从 capability 与规范输入计算，不含随机运行身份、路径宿主前缀、时间、信任状态或环境实际值。before 必须等于此前 ready/已接纳前缀推导的 C 摘要；保存和恢复均重新验证规范 16 KiB 与来源 profile。字段根据真实事件严格分支解码：Pre 必须带 effect，Post/Failure/Stop 不允许混入它；后者保留现有反馈语义。失败与 recovery finished 只能 unchanged，不能把尚未接纳输出保存为变化。

原始 ready 始终与 native sample 相同；C 由 ready 加连续 effect 前缀纯内存推导，不增加一份易漂移的最终输入 record。最后成功 finished 与 gate continue 同批，中间成功 finished 单批，阻断/错误的 finished、gate/result 同批。只有完整 continue 决定才建立 E；坏 gate/缺失 effect 不能按“没有改写”退回 O 放行。

这会替换当前 Pre finished shape 及相关固定 fixture。不得增加版本字段、可选兼容字段、历史 decoder 或 resume 原地改写。`input_effect` 中的 typed 参数是必要执行事实，不是整个 stdin 的快照，也不是任意脚本 stdout 的保存通道。

### 5. 最终有效参数贯穿候选、审批、执行、后置与展示

```text
native sample + usage + original ready
  --> frozen Pre plan
  --> trust / started / finished(input_effect) + checking candidate C
  --> gate continue --> selected execution input E
  --> Post branch plan/trust + result budget preflight
  --> prepare candidate + policy(effective input)
  --> approval(effective candidate) + Sync
  --> tool started + Sync --> register shared execution admission
  --> execute once --> frozen real result + Sync
  --> selected Post checks(effective input + real result)
  --> native outputs paired with original call IDs
```

三个候选 preparer 继续复用原有实现。改写路径要重新接受 FD 路径与 symlink 校验、base evidence、完整内容/diff、Write Read 资格、目录计划、sandbox 和预算检查；不能借 Hook 读取生成 Write Read 证据。最终目标改变时，旧目标的读取资格不能用于覆盖新目标。

普通 candidate fingerprint 与 approval metadata 从有效参数生成。Hook 来源作为独立的受控审计说明，不能替代真实权限 source。Patch/Write session grant 仍是当前 capability/workspace 范围；即使复用 grant 也重新准备有效候选、验证 deny 并 durable 保存相应 fingerprint。Exec 仍每次普通单次批准。

实际 executor 与候选 metadata、结果校验、已选参数活动 summary、命令详情、Post/Failure stdin、artifact 归属均使用 E。检查期间的 C 只能以检查候选展示。所有由输入派生的临时候选、权限评估及待展示数据必须对应当前选中值；不能复用原参数的推测授权或给 C 安装普通 grant。现有 Read 资格和 session grant 保留其原有作用范围，不能因改写一律清空后再制造隐式降级。

ExecutionAdmission 属于当前工具调用，独立于是否存在 Post 计划。独占骨架在 tool_execution_started 成功 Sync 后、调用 executor 与冻结结果之前，向该调用共享证据登记唯一真实 sequence；Post 路由、结果说明和宿主结果快照引用同一值。无 Post 不得等 executeExclusive 返回后才补准入，也不能把 hook_execution_started、结果 status 或已有批准当作工具准入。恢复只从唯一合法工具 started 重建该值。

重入项绑定当前 Session/thread/turn、真实 sample/call index、原始 invocation/call ID、capability、wire kind、完整 O 的规范内容，以及已保存 plan/effect/gate/result 摘要。不能用候选参数替代原始输入 fingerprint，也不能用零索引 surrogate 或只比 ID/能力认定同一请求。完成后同证据重入只返回冻结状态，零脚本/候选/批准/执行；在途重复不能新建 owner，错输入、错来源、错 turn 或 poisoned 状态失败关闭。新增缓存由现有 turn owner 持有，至多当前工具调用上限，随 resetHookTurn/最终关闭退役；恢复只重建事实，不恢复缓存授权。

### 6. 冻结结果说明实际参数，并使用原有结果预算

模型已经看到 O。说明必须让模型分清被选择、已准入尝试、确定结果和恢复不确定，避免把“Hook 改写过”说成“原命令执行成功”。由执行证据、真实 ExecutionAdmission 及合法恢复结果决定模式，不能按 code 字符串、stdout 标签或 result success 猜测。

| 条件 | 模型说明 |
| --- | --- |
| unhooked、gate_rejected，或 gate_selected 但 E 等于 O | 保持既有结果 bytes，不附改写说明；候选审计在宿主详情中保留 |
| gate_selected、E 不同于 O、没有工具准入 | 短说明：参数曾改写但工具未执行，带最终输入摘要和稳定状态；不传完整参数 |
| 同上且有已 Sync 的工具准入、正常保存实际 result | 完整说明：这是准入尝试采用的 E，含完整 canonical 参数和来源；具体执行/启动/错误仍以真实结果为准 |
| 为旧 started 缺口生成合法恢复补偿 | 短说明：结果无法确认且本次未重跑；不据 started 声称 OS 已启动，不重传大对象 |

短说明整体最多 256 UTF-8 bytes，使用固定英文说明、固定格式摘要和封闭状态，不含自由 reason、命令或正文。完整说明的来源元数据只含最终 input_digest 与按计划顺序排列的实际 replaced 项 sources，每项仅有 source/id，最多 1 KiB；不含绝对路径、RunID、时间、当前配置或授权。既有最多 8 个匹配、每项最多 64 个 ASCII 字符的 id 能使该封闭对象完整容纳，不截断或丢失来源，formatter 用最大合法形状回归并核对实际编码。完整参数独立最多 16 KiB，另核对 framing 的实际字节。说明是原调用结果的数据，不是额外消息或 additionalContext。

说明作为冻结 `tool_call_result.preview` 的受控前缀计入原 base allowance，Post 仍使用既有单 ticket。Post 信任失败/取消等早期结果直接走短说明，不能为了生成完整说明而继续信任、准备候选或申请额外空间。Post 计划/信任成功后，对未来完整说明执行 `完整说明 bytes + SafetyResultBytes <= base allowance` 预检；没有 Post 时从同一预算 owner 取得该次真实 allowance，不需要伪造 Post plan 或开启第二个 ticket。不足返回 error/hook_rewrite_budget_exceeded 和短说明，零候选/普通批准/工具准入，所选 Post 路由为 none 并正常关闭原 ticket。

候选准备失败、普通 deny/abort、allow 后 started 接收前取消，均因无准入使用短说明。成功准入后的启动失败或普通 error 仍使用完整说明，不能把未启动 OS 进程说成已执行；实际 failure/success 选择保持原规则。短或完整前缀先保留，再按剩余额度截断普通正文，确保安全终态仍在既有 1 KiB 空间内。artifact 只保存原输出，Post stdin 只从 typed 结果构造。

prefix 的模式和来源从事实推导；validator 先确定应有模式，再逐字节核对期望前缀，不从正文查找标签来决定控制流。普通 stdout 可以包含看似相同的标签，不能影响准入、授权或说明模式。已有 durable result 原字节复用，只验证不重新包装/计费；恢复补偿短说明只能用于当前合法缺口和恢复状态，不能把完整实际结果伪装成补偿来隐藏 E。

### 7. 取消按已接收事实收口，不改写真实检查结果

| 取消位置 | 必须保存和阻止的行为 |
| --- | --- |
| Hook started 请求接收前 | 不调用 runner；按既有未开始门禁取消，无伪 finished |
| started 已接收且 Sync 成功 | 已拥有的检查必须返回确定结果及 cleanup；runner 自身确认 cancelled 时只允许 unchanged，不接纳输出 |
| runner 已返回确定 continue/replaced，finished 尚未 Sync | 这是已准入检查的必要完成事实，沿现有不可取消收口保存真实 result/effect；不得改写为脚本未运行、丢变化或重新执行 |
| 中间 finished 已保存后 | 取消阻止下一 ordinal；只在未运行后缀处关闭 gate/call，已保存 C 保留但不产生 E |
| 最后一项确定通过后或最后 batch 接收期间 | 最后 finished 与 gate continue 仍同批；成功 Sync 后 E 已选，取消在普通执行路径收口。禁止追加另一个 gate cancelled 或伪造最后 Hook cancelled |
| 普通 allow 后、tool started 接收前 | 零 executor，结果采用 E 的非执行 metadata 与短说明，不能重用批准启动工具 |
| tool started 接收后 | 等待确定 Sync 结果再交给原 executor/cleanup owner；说明只宣称准入尝试，结果与 Post 路由仍依据真实事实 |

用户取消优先于尚未开始的下一项、candidate、普通批准和工具准入；必要完成/补偿写入不授予新的外部执行权限。输入预算错误不能覆盖已经观察到的取消，晚到取消也不能覆盖已 durable 的实际 result 或改变其 Post success/failure 选择。所有提交不确定路径仍 poison，不通过重试 finished 或重跑脚本确认结果。

### 8. 恢复严格验证全部事实，再允许尾部 repair

Session replay 保留 O、候选链 C 及 gate 选择事实。只有 continue 才能派生 E 并用于普通审批/result/Post 校验；未选中路径的 gate 结果按 O 非执行语义校验，候选链仅作审计。所有 effect 的事件、target、plan、ordinal、规范大小、来源 profile、摘要和批次校验均早于 repair；两家 Provider 消费相同共享证据，不解析 Session 原始 JSON。

| 崩溃边界 | 本地恢复行为 | 禁止行为 |
| --- | --- | --- |
| Pre plan 后、下一项尚未 started | 保留已保存 C，按原 Hook interrupted 语义关闭 gate/call，未产生 E | 不运行剩余检查、不准备工具 |
| Hook started 后、finished 前 | 仅该项补 recovery uncertain/unchanged，再补 gate 和工具取消 | 不猜测尚未保存输出、不安装替换 |
| 中间 replaced finished 后 | 从 fixed bytes 重建检查候选 C，按 O 的非执行规则关闭旧调用 | 不丢变化、不把 C 当成 E、不退回原参数执行 |
| gate continue 后、批准前 | 使用有效调用补 cancelled/session_interrupted_before_approval | 不重新询问或恢复 grant |
| allow 后、tool started 前 | 补 cancelled/session_interrupted_before_execution | 不使用旧批准执行 |
| tool started 后、result 前 | 按既有工具规则补 outcome_uncertain | 不重新执行、不查询 PID/文件证明成功 |
| result 后、Post/output 尚未闭合 | 复用冻结结果/说明模式、既有后置补偿及原 call 顺序补输出 | 不重跑检查、不重排或再截断 |

跨进程 lease 从 load/验证/repair 一直持有到 writer cleanup/Sync/Close。Sync 无法确认时 poison，不推进 C/选择 E、不开始下一 Hook/普通候选、不宣称保存成功。关闭超时继续持有唯一 cleanup owner 与 lease，不引入 helper goroutine 或第二条执行路径。

### 9. 原生历史和稳定缓存保持原有事实来源

Anthropic Edit/Write/Bash 原始 tool_use 与 OpenAI custom apply_patch/function 调用的参数保持模型原文；有效执行输入只存在于共享执行证据及冻结结果说明中。输出仍使用原 Provider call ID、wire kind、数量与次序。有效参数不能反向重建原生调用，HistoryProjector 只读。

Provider tool-output 构造和恢复使用同一纯内存结果编码规则及 unhooked/gate_rejected/gate_selected 证据，核对选择、真实准入、说明模式、结果 metadata、后置反馈与完整 stored output。Pre 拒绝不允许 C 改写结果 metadata；正常准入结果不允许冒用恢复短说明隐藏 E；stdout 中看似说明的文本不能选择模式。即使 checksum 合法，也拒绝错参数、错来源或输出正文伪造。

Hook 配置、trust、effect 身份、UI 和私有内容摘要不加入 provider_profile/tool_catalog/project_instructions/skill_catalog。实际说明只进入历史尾部；无变化路径请求必须逐字节不变。相同配置、catalog、项目/Skill 快照与已补偿事实下，比较 uninterrupted/restored 的完整 canonical request、native 顺序、opaque bytes、footprint/revisions、分段与请求 fingerprint。Anthropic cache marker/TTL、OpenAI key 和 WebSocket full/incremental 分别验证，不能声称不同 wire 帧完全相同。

### 10. 先安装有序快照，再显示检查候选与实际输入

继续使用 `/hooks`、现有独立信任 overlay、普通工具批准和 Ctrl+O；不增加专用 slash command。Pre 审查说明脚本可能提出参数替换、替换要重新验证、有效参数会保存并向模型说明，普通权限仍独立；默认拒绝。

普通批准展示 E 的完整目标、命令、内容/diff，增加紧凑“参数已由 Hook 调整”及来源标记，并绑定相同输入摘要/候选 fingerprint。检查期间主线只能说 C 是“检查候选”；通过 gate 后才能显示已选参数；tool started Sync 后才是 Running。Ctrl+O 区分 O、每项已保存 C 和最终是否选中 E；不能把部分变化或未开始后缀画成已执行步骤。

当前 HookHistoryObserver.HookFinished(ref,result) 缺少 effect 与 sequence，不能满足该规则。将它替换为有错误返回的 PreHooksCommitted(invocation,detail_sequence,typed_snapshot)，与现有 Post 快照接缝保持相同职责；快照包含原调用、plan、已保存 finished 前缀、实际 pending/decision，C/E 由共享纯规则推导。真实 record sequence 来自成功 AppendBatch 的返回值；gate batch 使用 gate 的真实 sequence，不能使用 ordinal、时间或 native revision 冒充。

每次 Pre plan/started/finished/gate 的事实更新先安装独立不可变快照，再通知宿主。校验、深拷贝与派生展示在锁外完成，锁内只做有界状态替换；getter/render 不解析整个 journal 或执行 I/O。Sync 成功但内存或宿主快照安装失败时，不回滚或重跑，Runtime poison 并按既有失败路径收口，不让后续候选/批准/执行使用旧投影。

ToolHistoryEntry 的原始 Invocation 必须继续保存 O，不能为了刷新目标直接覆盖它；选参和准入由共享执行证据单独提供。工具 started/terminal、结果 observer 与恢复投影从相同证据生成实际展示，结果通知不能只传 preview 再从文字推断准入。无 Pre 的消费者显式提供直接证据，读写/提问等原始结果语义保持。

继续复用 HookActivity 及既有批准/工具活动，不新增 event kind。Pre 活动的改写投影采用封闭 none/candidate/selected/rejected 分支：none 不携带其他字段，后三者仅含真实 detail_sequence、候选/所选摘要、最多 160 rune 的摘要及有界统计；不携带完整参数或 4 KiB 原路径。新投影块完整 canonical 编码最多 4 KiB，整个 HookActivity 仍须满足既有 16 KiB。检查活动可带 none；无真实执行的 gate 拒绝仅发布 gate 相位，不能伪造 started/finished/输出计数。其他事件不接收该 Pre 分支。

通知以相同 invocation/plan、递增 sequence 触发既有 generation 更新和页缓存失效；迟到 checking、旧 candidate、gate 前快照和旧查询页不能覆盖 selected/rejected。详情继续复用四页缓存与有界分页，在内存中从 typed facts 生成安全文本；原始输入大于一页时分页而不是塞进活动事件。正常批准自带完整合法展示，不依赖一个可能过期的 query 页补足。原始脚本双流和自由文本原因仍不展示。

28×10、8×2、80×24、NO_COLOR、resize、长 Unicode/控制字符、Esc、Ctrl+C、paste 和过期响应必须覆盖。窄屏不能把截断参数冒充完整批准；草稿、阅读锚点及取消所有权保持现有规则。headless 匹配依旧 hook_trust_required，零 Hook/工具执行；外部 JSONL v1 不增加字段或事件。

### 11. 复用边界与设计模式

| 边界 | 职责及复用 |
| --- | --- |
| domain | 封闭变化值、摘要、严格输入/effect 校验；不依赖 tool、文件或 Provider |
| extension/hook | 外部 stdout 子集适配到 typed 输出；不做普通权限决定 |
| tool | O/C/E 纯适配、选择/准入证据、确定性模型说明及预算冻结 |
| executor/process、builtin runner | 复用现有进程 owner 和输出策略；无第二套进程生命周期 |
| runtime | 既有串行 Pipeline 编排；独占骨架持有各能力策略，不使用 embed shadow |
| session | 保存与回放严格 State 转换；Repository 继续独占 lease |
| provider | 原生编译/配对/恢复策略，消费共享证据，不读取 Hook 来源 |
| protocol/app/tui | 只读 Observer 投影与真实审批消费，无 render I/O |

仅提取 Runtime 和 Session 确实共用的纯适配、摘要与冻结规则。禁止新增通用 manager、可变全局 codec、registry、兼容 facade、伪工具或提前为 MCP/Subagent 设计输入分支。

## Risks / Trade-offs

- 完整对象与已有 16 KiB 输出上限限制了大 Write 改写：文档明确支持子集，超限失败关闭，不扩大进程/结果额度。
- 串行改写使配置顺序可见：固定并展示顺序，后项检查当前候选输入；不承诺此前策略对后来输入自动成立。
- 同时保留 O/C/E 增加关联成本：唯一纯适配与封闭 gate 选择证据供三层复用，固定 fixture、伪造 metadata、摘要链和完整原调用重入测试约束漂移。
- 参数中的终端控制文本可能欺骗审查：使用现有安全文本/原文渲染和固定尺寸快照，不执行或解释其内容。
- 改写说明占用结果预算：副作用前完整预检，未准入/恢复短说明独立有界，准入结果完整说明优先保留，原 artifact 规则保持。
- JSON 转义可使原始长度失真：已用同配置探针确认；分别限制规范输入、finished 和宿主投影实际编码，不提高通用上限或截断对象放行。
- 旧开发 Pre facts 将不再可读：这是仓库唯一当前契约的直接替换；明确报错，保留旧 journal bytes，不引入迁移。

## Migration Plan

本项目尚未发布，不实施数据迁移。apply 中直接调整当前 typed shape，替换受影响的 `testdata/current` 固定 bytes，并在旧 shape/缺失字段/未知版本字段下证明 repair 前拒绝。回滚代码时不能宣称新 facts 仍可由旧二进制读取，也不重写 append-only 历史。不得为回滚增加兼容窗口。

实施前先复核本提案、六份 delta 与当前工作区基线；如发现设计必须扩大输入权限、反馈通道或持久化范围，暂停代码变更并回到提案更新。实现与验收后同步主规格，再按用户后续指令归档。

## Validation Plan

| 层次 | 必须证明的业务断言 |
| --- | --- |
| parser/domain | 完整对象与空值；缺失/重复/未知/null/UTF-8/混合权限拒绝；来源 profile；独立编码/转义边界；无变化先校验；effect 深拷贝和摘要链 |
| tool/runtime | O/C/E 选择及拒绝 metadata；真实调用来源、独立于 Post 的准入、缓存退役；声明顺序；Write 证据；晚到取消与最后 batch；原目标零副作用 |
| 预算与隐私 | 完整/未准入/恢复说明模式；低预算 Post 拒绝与无 Post；真实 error 和标签伪造；单 ticket；原始双流/原因哨兵不泄漏 |
| Session | 所有崩溃切点、固定 current bytes、语义伪造与半行共存时零 repair；真实子进程 lease 竞争、崩溃释放、占用不 repair、seq 单调、失败 bytes 不变 |
| Provider | 两家三个 facade 的 request/output golden；三分支选择及真实准入核验；O 与实际说明并存；opaque、配对、footprint、canonical/fingerprint 恢复等价 |
| app/CLI | 本地模拟服务驱动完整 Read/改写/批准/执行/Post/续答；headless 三入口失败关闭；离线恢复移除旧配置/脚本后零重跑 |
| TUI/PTY | O/C/E 与真实准入相位；先装快照后通知/失败 poison；4 KiB/16 KiB 编码、旧 checking/页拒绝；完整批准、窄屏/无色/resize/草稿及真实 Darwin PTY |
| 质量门 | `make verify`；change 与受影响主规格 strict 校验；必要的 Linux/Windows 编译，并分别记录不能原生运行的原因 |

故障测试采用注入 writer/Sync、runner、进程 handle、clock 和明确接收屏障，不依赖 sleep 或概率竞态。验收报告区分本阶段基线结果、将来的功能验收及实际平台范围；不得用基线通过证明未实现功能正常。

### 本提案阶段的实际检查

2026-11-14 在当前 worktree、Darwin arm64、Go 1.24.13 上执行 `make verify`，退出码为 0，涵盖分支策略回归、gofmt、vet、Staticcheck、架构守卫、全量普通测试及 race。此前被会话中断的运行不作为完整通过证据；本次完整日志为 `/tmp/easycode-pre-tool-use-input-rewrite-0fb5-baseline.log`，退出结果记录在同名前缀的 `.status.json` 文件。

首版提案的 change 与全工程 strict 校验通过（61 项、0 失败）。本轮对照两份固定参考快照及当前消费者进一步复核后，现有六份 delta 共覆盖 88 个场景、49 个待实施任务；每项任务均给出验证方式。首版的场景/任务数量不作为当前方案的覆盖清单。

本轮实际执行了关闭网络的 Sonic 编码边界探针，并对更新后的 change 和全工程重新 strict 校验；同时核对 capability 路径、rename、任务编号/验证方式、全部场景结构与规划文件范围。生产代码未改变，因此不重复运行前述完整 Go 质量门；该结果继续只表示同一代码基线，不是新功能验收。

本阶段改动限现有九份 planning 文件，没有创建新 artifact 或修改主规格/实现；49 项任务均未勾选。实施后仍须完成上表及 tasks 的全部对应验证，并重新运行完整 `make verify`。
