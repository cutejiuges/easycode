## Context

EasyCode 基线 `51c7dc3` 的事件定义、配置解析、能力大纲及当前主规格列出七种同步命令 Hook。`PreToolUse` 可门禁和改写三种 mutation 参数，`PostToolUse` 与 `PostToolUseFailure` 分别检查确定成功和执行失败，`Stop` 可有界续轮，`PreCompact` 与 `PostCompact` 检查压缩边界，`SessionStart` 按 `startup`、`resume`、`compact` 注入模型上下文。实际命令 sandbox 当前由 Darwin 提供。异步执行、插件命令贡献、热加载和其余生命周期事件在路线图中仍待交付。仓库有受限 Hook 信任与命令执行 owner，`internal/runtime/runtime.go` 和 `internal/runtime/stop_delivery.go` 已有可定位的真实输入安全点。

### 两个参考实现

| 参考快照 | 观察到的行为 | 本切片采用的行为 |
| --- | --- | --- |
| Claude Code `a8a678c`，`restored-src/src/utils/handlePromptSubmit.ts:213`、`restored-src/src/utils/processUserInput/processUserInput.ts:174`、`restored-src/src/utils/messages.ts:2893` | 提交分派先展开粘贴文本。发起查询的路径把分派文本或文本块提取结果传给 Hook。该事件不按 matcher 筛选。`decision: block` 以含原因和原文的本地警告替换待查询输入。`continue:false` 是另一种停止，原用户消息仍可留在上下文。 | 普通输入与显式 Skill 保持查询前检查。以冻结的用户输入为准，阻断后通过 EasyCode 的 returned 列表取回。 |
| Codex `9f2b87495b`，`codex-rs/core/src/hook_runtime.rs:709`、`codex-rs/core/src/session/turn.rs:1174`、`codex-rs/core/tests/suite/hooks.rs:2909` | 从真实用户输入提取文本。按输入检查，保留同批合格项及工具输出持久化屏障。同批追加全拒时结束当前循环。启动检查先于首条提交检查。 | 采用逐条裁决。已保存工具输出后的健康回合在追加全拒时继续，Stop 继续履行已接纳的反馈。首轮把提交门禁放在自动压缩和激活检查之前。 |

- **追加与 Plan**。Claude 的活动回合直接把队列输入转成附件，清空上下文后的 Plan 实施另走直接查询。EasyCode 使用 Codex 的逐条检查思路覆盖工具追加，并将人工确认后实际领取的 Plan 实施文本纳入门禁。批准动作仍走自己的控制路径。参考 Claude `restored-src/src/query.ts:1555`、`restored-src/src/screens/REPL.tsx:3102`。
- **运行与退出**。Claude 并行运行声明并合并完成结果，先解释结构化输出，再按退出码分支。Codex 也并行执行并归并结果，对 `decision:block` 要求非空 reason，对退出码 `2` 要求非空 stderr。两家普通失败通常只报告诊断。EasyCode 串行执行并按 runner 结果优先级裁决，允许干净退出 `2` 无 stderr，确定失败时拦截该输入。这样配置的门禁在脚本故障时仍发挥作用。参考 Claude `restored-src/src/utils/hooks.ts:2499`、`:2647`、`:2743`，Codex `codex-rs/hooks/src/events/user_prompt_submit.rs:158`、`:229`、`codex-rs/hooks/src/engine/output_parser.rs:266`。
- **信任与附加上下文**。Claude 使用工作区信任，SDK 路径在该快照中可隐式运行。Codex 选择受信任或托管的声明，且可为阻断输入保存后续模型上下文。EasyCode 复用逐声明默认拒绝，要求非交互入口报告匹配而无授权的失败，本切片只接受本地阻断控制。参考 Claude `restored-src/src/utils/hooks.ts:268`，Codex `codex-rs/hooks/src/engine/discovery.rs:909`、`codex-rs/core/tests/suite/hooks.rs:2806`。

`PermissionRequest` 的实际效果涉及人工审批与政策重检，参见 Codex `codex-rs/core/src/tools/approvals.rs:600`。`SessionEnd` 和 `Notification` 首先提供观测能力，其中 `SessionEnd` 涉及终端卸载、关闭时限和已有信任，参见 Claude `restored-src/src/utils/gracefulShutdown.ts:406` 与 Codex `codex-rs/core/src/session/handlers.rs:429`。异步 Hook 要额外定义任务 owner、退出和交付。当前阶段优先交付输入门禁，它在本地队列与现有 turn owner 内即可为正常对话建立完整闭环。

## Goals / Non-Goals

**Goals**

- 在现有三类真实输入安全点使用同一检查语义，保留输入归属、普通工具批准、TurnID 和 Provider commit 的原有职责。
- 在一条记录链中证明哪些进程获准启动、哪些输入通过检查以及哪些输入真正提交，使进程中断后仅执行本地结算。
- 让声明审查准确说明明文披露范围，让拒绝的输入可取回编辑。

**Non-Goals**

- 输出向模型注入上下文、改写提示词、自动修复、后台执行、插件或持久信任由独立变更承载。`SessionStart` 已能承载项目级上下文。本事件的上下文注入需要另行定义如何与 SessionStart、Stop、自动压缩和原生历史原子交付。

## Decisions

### 1. 配置、目标和进程协议

`internal/extension/hook/config.go` 的 `Stop` 组已建立无 matcher 严格配置形状，复用同一组解码逻辑。`HookSnapshot` 返回现有冻结排序和诊断，声明引用沿用现有来源与 fingerprint。配置有效且无匹配时快速返回，保持原始调用路径和日志。坏来源或匹配超限以固定错误返还本批输入，仅完整的匹配引用形成耐久计划。无交互时写计划和无运行的结算，直接返回 `hook_trust_required`，不走事件级 stdin 编码、平台前检及审查。宿主原有启动可用性探测仍可运行。交互中平台或沙箱不可用时，在披露审查前报告固定原因，单条 stdin 超限时只结算当前条目。配置纠正通过重新激活读取冻结快照，重置信任只影响当前实例内的声明选择。

配置示例适用于 `.easycode/hooks.json`：

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "id": "check-submission",
            "type": "command",
            "command": "prompt=$(/usr/bin/plutil -extract prompt raw -expect string -o - -- -) || exit 1; case \"$prompt\" in *DRAFT_ONLY*) exit 2;; esac"
          }
        ]
      }
    ]
  }
}
```

`HookTarget` 增加可比较的 `TurnID + InputID + sample index` 目标，索引 `0` 对应初始输入。计划和交付使用现有 `InputAssociation` 保持初始和追加语义。声明信任绑定声明引用和会话实例，当前审查响应另绑定目标、计划摘要和请求身份。记录 envelope 和事件 TurnID 都须与目标一致。共享 runner 接收当前强类型 `HookInput` 的新分支。事件级 preflight、逐输入编码与限额验证均先于披露审查。使用现有 `runTimedHook`，由 turn worker 在各安全点共享剩余执行时间。每项 deadline 为声明值与剩余额度中的较小值。审查等待不计入命令额度，进程实际清理仍等待原 owner。信任或拒绝的声明在同一快照与实例中沿用既有状态。一个输入出现确定拒绝或失败后跳过其剩余声明。预算耗尽拒绝尚未通过的输入。取消及运行结果未知还阻止下一次 Provider 请求。

分类层先复用进程状态、stdin 双流处理及清理边界，再解释该事件的正常退出。可选 `reason` 只按严格字符串成员验证。复用 `HookRunResult` 时把干净退出 `2` 约束为本事件的 `hook_denied`，不扩大其他事件的 `HookFinished` 形状。前检或信任拒绝是从未启动的输入决策，禁止伪造运行统计。诊断仅记录已有有界元数据。脚本示例完整消费 stdin，以免关闭管道造成 I/O 失败先于退出码分类。

### 2. 唯一 turn worker 检查与 claim 结算

| 安全点 | 检查位置 | 未通过输入的处理 |
| --- | --- | --- |
| 新 turn 初始输入 | `runtime.go` 的 `beginTurn` 后，`prepareAutomaticSample` 和 `commitInitialInputs` 前 | 仅用通过项预检、评估压缩和提交。无通过项时以既有失败路径收口，待执行的激活 Hook 保持待执行。 |
| 工具后的追加 | 成组 native tool outputs 已提交、`input_queue.go` 已领取追加输入之后，下一次自动压缩或提交之前 | 返还拒绝项，使用通过项及已提交窗口继续。无通过项时原回合可执行与当前窗口独立相关的维护和下一次采样。 |
| Stop 的真实纠正 | `stop_delivery.go` 作出耐久续轮决定并领取纠正之后，在构造复合交付候选之前 | 用通过项构造原 Stop 复合提交。无通过项时发送原 `FeedbackOnly`。阻断或取消不得绕过反馈的本地收口义务。 |

Plan 确认复用初轮入口。确认后的实际实施文本已经获得用户明确选择，并由现有 TUI 保留独立的 `inputPreview`。门禁可以返还该文本，用户可以编辑后重新提交。默认模式切换和已完成的确认保留，普通工具批准仍单独执行。只读 slash、模式切换、批准或问卷响应、系统生成的 Stop 反馈、压缩摘要与历史回放均无新 `UserInput` claim。TUI 将显式 Skill 或 `@file` 解析为原文及冻结附件，Hook 只拿同一 `UserInput.Text()`，正文保真按入口原有换行规则。stderr 与独立文件或 Skill 展开值从不作为模型反馈。

队列有唯一 claim owner。owner 先发布已领取状态，worker 再发出检查活动或信任请求。worker 只交付一条有序筛选消息，包含完整已领取 IDs 和按 ID 绑定的拒绝原因。owner 核对当前控制握手、真实 turn 和整条 claim 后，在一次有界状态转换中发布拒绝项的 `returned` 并释放其额度，保留合格输入的有序子序列及额度。后续沿用现有 resolve/Sync 确认。最初承载 turn 的请求即使其输入被拦，也等待该 turn 实际终态才释放请求标识。正常关闭且全部被拒的首轮用第一条输入的固定失败码。取消、运行未知及 writer 失败按各自终态收口。单个输入结果由 returned 状态链呈现。队列新输入、取回和清理继续由同一循环串行处理。

### 3. 持久化记录与恢复

- 使用独立的 `user_prompt_submit_hook_planned`、`user_prompt_submit_hook_decided`、`user_prompt_submit_hook_closed`，运行事实复用现有 `hook_execution_started` 和 `hook_execution_finished`。该事件五类事实都按单记录 batch 保存，Loader 检查完备行的 arity，不能把错误形状当成未完成批次截断。相同采样安全点只容许一个计划，计划绑定真实 turn、有序 claim 和声明引用。摘要覆盖关联和引用。运行时使用同一冻结输入供检查和后续原生提交，日志只验证这两条链路的身份、顺序和运行证据。
- `planned` 之后按输入和声明顺序接受 `started → finished`。`started` 必须先 Sync，正常 `finished` 使用同一运行身份与空反馈。`decided` 仅在全部声明已成功时记录通过，首个终止运行的结果直接决定对应拒绝码。未启动原因的有限组合由前检、信任、剩余额度及已验证的成功前缀决定。正常 `closed` 要求完整逐项决定。取消、运行未知或 recovery `closed` 封闭尚未决定的有序后缀并禁止用户输入交付，同时保留已有决定。恢复可以补写已接受 `started` 对应的未知 `finished`，若在两次恢复之间再次崩溃，只追加仍缺失的事实。
- 新计划仅在初始、工具输出已提交后或 Stop 待交付纠正的采样边界接纳，并验证自动压缩、激活或真实输入提交尚未发生。未关闭计划独占对应 replay 前缀，每个接受的事实推进活动 turn 尾序号。合法关闭允许已通过输入经自动压缩和 `SessionStart` 后按原路径提交。关闭计划内如有后续输入关联，只允许通过项的完整有序子序列按原位置一次交付。这个校验落在既有普通、SessionStart 与 Stop 共用的 Session 输入关联边界。检查过的 ID 必须保留独立的回放占用，含已拒和未提交身份。接受/终态由当前完整 Sync 批次确定，因压缩、预算、取消或失败未交付的通过项可以没有原生输入。
- 已通过但未交付的资格在推进 target sample、Stop `FeedbackOnly` 或 turn 终态时失效。检查账本在 `Plan` 与 `PlanMetadata` 中共用同一语义验证，分支继承检查占用集合和已验证历史。回放必须先识别 Stop 待交付期间的合法检查前缀，再派发 Stop 原生交付，并在同一 writer lease 下先收口中断检查才本地补偿 Stop、Tool Loop 或 turn 终态。恢复零脚本和零请求。
- Writer 接纳原生用户批次之前，命令结果未知、检查写入未知或取消都能确定没有这批用户输入被写入。需要停止当前路径并由 owner 关闭已接受的检查，状态仍可取回。检查清理与收口确认且 writer 健康时，可本地完成已接纳 Stop `FeedbackOnly` 并报告取消或失败。journal poison 时把义务交给既有恢复。在原生批次已被接纳后等待 Sync/finalize。只有该次原生提交结果不确定才报告输入 `uncertain`。

### 4. 投影、交互和双 Provider

在 Runtime 中编排检查，domain 定义身份与强类型契约，extension 解析配置和 stdout，现有 tool/process owner 执行命令，Session 验证事实，app/TUI 投影事件。复用声明信任、进程 timeout、严格 codec 和既有 `HookActivity`。本地 `InputStatus` 的 `hook_rejected` 携带封闭 `HookCode` 与可选安全声明来源和 ID。TUI 仅在实际 turn 与 claimed 输入匹配时投影，取消开始即关闭该检查的审查。复用当前窄屏限制及 `/hooks` reset。普通文本沿用接纳后的内存召回，Plan 实施沿用提交后的召回。被拒文本可在本地查看和取回，只有真实提交才进入 transcript。

一次性 `--print` 使用 stderr 固定摘要与退出码 `1`，`--json` 使用已有 `turn.failed`。stream-json v1 的 `input.submit` 走现有严格路径，`input.append` 用原有 auto `input.status` 关联各条返还，外部 `reason` 仍为 `session_failed`，同一 `turn_id` 的 `turn.failed.error.code` 为 `hook_trust_required`。在既有外部投影处完成映射，交互专用活动和声明诊断不交给严格的 headless projector。会话级退出遵循原 EOF drain 和 shutdown 规则。

门禁通过后的正文、文件和 Skill 内容仍走现有 `PreparedUserInputs` 或已存在的 SessionStart/Stop 复合提交。比较开启门禁与相同通过序列下的原完整逻辑请求，核对 canonical bytes、原生顺序、缓存指纹、来源和稳定前缀。恢复等价以完整逻辑请求为准，OpenAI 已连接的增量传输帧仍按原契约单独检查。

## Risks / Trade-offs

- **同批第一个请求被拦截** → 保留 turn 承载请求直到终态，分开处理其输入返还和 turn 输出，按有序 `InputID` 发布其他项目的接纳状态。
- **检查或复合交付中断** → 测试分别暂停在命令启动事实、原生 batch 接收前和 Sync 后，并覆盖 Stop 先行决策的恢复顺序。
- **超限或脚本错误阻断正常工作** → 使用确定的英文代码和现有取回编辑路径。无匹配配置的大输入维持现有 4 MiB 规则。当前无 stdin 可安全交给匹配命令时，匹配门禁返回输入限制或需要信任。
- **增加交互等待** → 默认拒绝的声明信任继续复用会话内决策。运行共用每 turn 30 秒，取消先于下一个进程启动。只在匹配时写入本事件事实。

## Migration Plan

用户启动新会话实例加载冻结声明，移除声明后在下一次激活应用配置。新增 journal 事实使用唯一当前 grammar，既有记录形状继续使用原构造。启用新事件的 journal 需要识别这些事实的构建。发布没有内部版本字段、历史 reader 或迁移器。
