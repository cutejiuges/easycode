## Context

动机与范围见 proposal.md。本提案基于 EasyCode `27be635`；首次提案时，除本 change 外仅有 `add-pre-tool-use-command-hooks` 活动 change，43/43 tasks 已完成，本次不修改或代为归档它。PostToolUse、本地 Skills、补全与被动推理阅读已归档并体现在当前实现中。以下记录用户确认的深入推演契约与设计基线；当前实现、验收证据及质量门状态见 verification.md。

### 四个方向的选择依据

| 方向 | 当前事实 | 下一步依赖及价值 | 本次判断 |
| --- | --- | --- | --- |
| Skills | `extension/skill/local.go` 已具备分层发现、FD 校验、冻结目录、参数展开、资源加载；TUI 有列表与补全 | allowed-tools 需要权限子集；context:fork 需要子线程；热加载需要安全快照替换 | 现有主要使用链路完整，下一项会引出其他边界 |
| Hooks | `runtime/exclusive.go` 已在 Pre 和成功 Post 间形成执行闭环；失败结果目前只让 Post skipped | 执行尝试失败诊断可复用已有信任、受限进程、预算和恢复，直接接入真实消费者 | 选择确定执行尝试失败后的 PostToolUseFailure，不包含命令语义分类 |
| Plugins | `extension/plugin/plugin.go`、`extension/extension.go` 仍为批准占位，没有装配消费者 | 至少需要来源/manifest/contribution、名称空间、启停和信任，不能只实现 manifest 空解析器 | 可作为后续独立 P6 切片，本次不提前暴露 |
| Subagent | `subagent/control.go` 是批准占位；当前 Catalog 只索引 root thread | 需要独立 native history/writer、thread tree、预算、取消、completion 和恢复 | 属于 P7，不宜用不可恢复的最小 spawn 绕过前置条件 |

### 两个参考工程的只读核对

参考目录不位于本 worktree 的 `..`，已定位到 `/Users/cutejiuge/IdeaProjects/`。以下结论限定于本地快照，不代表上游最新产品，不引入构建依赖或照搬代码。

| 快照和源码位置 | 已观察行为 | 本次吸收与明确取舍 |
| --- | --- | --- |
| Claude Code 2.1.88，sourcemap `a8a678c`；`restored-src/src/entrypoints/sdk/coreSchemas.ts:448`、`:854` | Failure 输入有 tool_name/input/use_id、error、可选 is_interrupt；输出有事件相关 additionalContext | 使用相同事件/字段名称；EasyCode 强制 is_interrupt=false，并只提供安全错误摘要，明确为支持子集 |
| 同上，`services/tools/toolExecution.ts:1696`、`toolHooks.ts:193` | catch 路径运行失败检查；原错误 tool_result 与 Hook 附件分别返回 | UI 保留原工具错误，检查与反馈关联同一个调用，不生成额外模型工具调用 |
| 同上，`utils/hooks.ts:3492` | 按失败事件选择匹配项，并沿用可取消 Hook 执行器 | 复用事件 matcher、输入身份及取消链，不建立第二套失败进程系统 |
| 同上，`tools/BashTool/BashTool.tsx:687`、`:714` | 通过 interpretCommandResult 判断错误，再抛 ShellError；并非简单地把所有非零退出一律判错 | 不复制命令字符串启发式。保持 EasyCode 已批准的正常非零退出 ResultSuccess 契约 |
| Codex `6f8bf683c`，`codex-rs/core/src/tools/registry.rs:936` | 基于工具输出 success_for_logging 决定 Post 请求；工具生命周期、Hook 结果和输出包装分离 | 使用真实结构化结果和明确阶段决定路由，不从 preview 或错误文字猜测执行是否发生 |
| 同上，`hooks/src/events/post_tool_use.rs:27`、`:139` | typed Request/Outcome；matcher aliases 与真实 tool_name 分离；run 与 tool_use_id 绑定；反馈分开收集 | 继续采用小类型、精确关联、真实 facade 和独立反馈；不引入其中的无约束 JSON Value |
| 同上，`hooks/src/engine/dispatcher.rs`、`core/src/tools/registry.rs:979` | 共用 handler 调度；区分执行事实与后置 block 对结果的作用 | 复用现有进程 owner；EasyCode 不采用后置 block/替换输出，原结果始终不变 |
| Claude `tools/BashTool/commandSemantics.ts:19`、Codex `core/src/tools/context.rs:448` | Claude 默认非零为错误，但 grep/rg/diff/test 等有例外；Codex ExecCommandToolOutput 正常输出的 success_for_logging 为 true | 保留 EasyCode 的正常非零退出契约；go test exit=1 仍走成功 Post，不将本能力宣传为测试失败自动诊断 |
| Claude `utils/hooks.ts:286`、`:1992`、`:2015` | 交互模式采用 workspace trust，非交互模式策略不同；父 signal 已取消则 executeHooks 直接返回 | EasyCode 仍采用逐项精确信任及 headless 默认拒绝；失败输入有 is_interrupt 字段不代表取消后一定运行脚本 |
| Claude `utils/hooks.ts:2142`、Codex `hooks/src/engine/dispatcher.rs:136` | 两者可并行执行匹配 Hook，Codex 完成后按配置顺序整理结果 | EasyCode 当前采用串行有界执行，维持连续 finished 前缀、确定预算与恢复；不把并发调度一并引入 |
| Codex `core/src/context/hook_additional_context.rs:25`、`hooks/src/output_spill.rs:57` | additionalContext 采用 developer 角色，大内容可截断并保存临时文件 | 只借鉴来源与生命周期分离；EasyCode 维持工具数据角色和完整接受/拒绝，不提升权限或自动转存正文 |

在该 Codex hooks/src 快照中搜索未发现独立 PostToolUseFailure 实现，不能把它描述成双方已有同名能力。本次主要借鉴其类型、执行与反馈边界；失败事件体验以 Claude 为依据。

Claude 的菜单说明提到 error_type/is_timeout，但实际 Failure builder 和 SDK schema 的共同字段是 error 与可选 is_interrupt。本方案以实际构造/消费链为依据，不把说明文案当作已兑现的 wire 契约。

### 当前必须修改的接缝

| 接缝 | 现状 | 必须实施的变化 |
| --- | --- | --- |
| `domain/hook_event.go`、`extension/hook/config.go` | 仅两个事件，feedback 只允许成功 Post | 增加第三个真实事件，保持 Pre 严格拒绝 feedback |
| `domain/post_hooks.go` | PostHookPlan 只持有单个 HookPlan，terminal event 固定成功 Post | 调用归属只保存一次，一至两个精简分支、一个预算、一个 terminal |
| `runtime/post_hooks.go`、`exclusive.go` | 所有非 success 直接 skipped；tool started 先于 executor，进程随后仍可能启动失败 | 同一 state 冻结两分支；只按准入/结果事实选择，取消和启动前检独立 |
| `tool/model_output.go`、`post_feedback.go` | 验证固定 PostToolUse | 接收所选后置事件，拒绝跨分支、混合 plan 与 ordinal |
| `session/post_hooks.go`、`runtime/post_hook_recovery.go` | started/finished 只能属于 success 后置 | 验证选择证据、两个分支及恢复补偿；禁止用失败结果启动恢复脚本 |
| `protocol/hooks.go`、`post_hook_commit.go`、`app/transcript_owner.go`、`tui/hooks.go` | 后置提交通知有固定成功事件字段，计划时尚不知道工具结果 | 提交通知移除固定事件，只携调用/计划/序列/阶段；实际运行事件从已验证快照读取 |

## Goals / Non-Goals

**Goals:** 同一 exclusive owner 编排前置检查、原工具和一个后置分支；失败检查完整接入真实工具、TUI、模型反馈和恢复；每个副作用与预算段都有明确 durable 线性化点；变更后只有一套严格当前结构。

**Non-Goals:** 不改变 Exec 成功定义，不增加命令语义分类器或测试失败自动修复；不处理 candidate preparation 的错误；不在用户取消后以 WithoutCancel 启动脚本；不实现 PermissionDenied、Stop、并发/async Hook、自动重试、错误改写、插件 Hook、热加载或子代理。只读工具仍不接入 Hook。未覆盖行为在交付文档明确列为后续能力。

## Decisions

### 1. 扩展原配置和事件，不新增 Hook 管理层

同一 hooks.json 加入下列声明；使用者自行提供只读诊断脚本，产品不会自动生成或修改它。

```json
{
  "hooks": {
    "PostToolUseFailure": [{
      "matcher": "Edit|apply_patch|Write|Bash|exec_command",
      "hooks": [{
        "id": "explain-failure",
        "type": "command",
        "command": "./scripts/explain-failure-readonly.sh",
        "timeout": 5,
        "feedback": "model"
      }]
    }]
  }
}
```

来源、FD 安全读取、顺序和数量限制沿用 command-hooks：每来源最多 64 KiB/32 项，合并最多 64 项，id 在来源三事件间唯一；单调用每事件最多 8 项。规范事件顺序为 PreToolUse、PostToolUse、PostToolUseFailure，JSON 对象键顺序无关。仅两个后置事件允许 feedback=none/model，默认 none；Pre 出现该字段仍无效。未知字段、null、重复键、尾随值、未支持事件使整个来源无效，mutation 不采用部分快照继续执行。

command fingerprint 与信任身份继续包含事件、完整 source revision、命令、matcher、timeout、披露和执行策略。相同命令在不同事件不能借用信任。采用现有实例快照与封闭 switch；不新增全局 registry、空策略或 embed shadow。

### 2. 冻结两个可能分支，但只预留和关闭一次

Pre 通过后，`executeApproved` 在候选准备前一次匹配两个后置事件。不存在任何匹配时保持原无 Post 路径；只配置成功或失败分支时也不构造空分支。

唯一当前 `PostHookPlan` 的必填字段为 invocation_id、capability、branches、allowance、reservation、digest。调用归属只在外层保存一次；每个 branch 恰好保存 event、hooks 引用数组和 delivery，不嵌套完整 HookPlan，不再保存分支级 invocation/capability/plan digest。branches 为一或两项，按成功、失败顺序，事件不得重复、Pre 不得出现，每分支至少一个合法 reference。引用自身的事件/来源身份继续保留并与分支事件交叉校验；同一来源在两分支中的 revision 必须一致。外层 digest 覆盖调用归属、全部分支和预算。

实施时抽取并复用现有 HookPlan 的引用校验、稳定排序及摘要规则；Pre 继续使用单事件 HookPlan，后置不为复用而保存重复的调用归属。每个新当前类型均提供 typed constructor、strict decoder、validator 和深拷贝，拒绝缺失/未知字段及不一致归属。

此形状替换单 `plan/delivery` 旧形状，不支持双 decoder。`PostHookRoute` 只有 success/failure/none：success 对应 PostToolUse，failure 对应 PostToolUseFailure，none 仅表示不选择后置事件，不能据此推断原工具未执行。terminal 保存 route，不重复保存可由 route 推导的 event。success/failure 可以因相应分支未配置而 skipped/hook_event_unmatched；none、unmatched 均不能有 Hook started/finished/反馈。原工具事实、后置选择和检查 disposition 是三个独立维度。

所有匹配声明按成功分支、失败分支及各自来源/数组顺序逐项审查，最多 16 项。两个分支不共享信任；任一拒绝或取消均不进入候选准备。headless 匹配任一后置事件返回 hook_trust_required，零脚本与目标执行。该保守选择让用户在任何修改前了解最坏情况下会运行哪些命令，也避免工具失败后才弹出新授权。

不采用两个独立 Post 生命周期：它会重复 reserve/close、使未命中分支生成伪运行状态，并增加恢复交错。一个 plan 一个 ticket 的方案保持既有调用级资源所有权。

### 3. 先按 durable 事实选择，再判断是否启动

路由是 tool 层共享的纯函数，只接收同一 invocation 的已验证执行尝试准入证据和已持久化原结果，输出封闭 route。`tool_execution_started` 表示 executor 执行尝试已准入，不证明 OS 进程已经启动或文件已改变；准入后的 process_start_failed 同样是确定失败结果。runtime、replay 和恢复必须使用同一语义，当前 context、当前配置、剩余预算、PID、时间及恢复模式都不能作为选择输入。共享值不依赖 runtime/session 包，domain 仍不依赖 tool。

| durable 工具事实 | 唯一选择 |
| --- | --- |
| 尝试准入 + ResultSuccess | success；包括 go test exit=1、rg exit=1 等正常非零退出 |
| 尝试准入 + 确定 ResultError/ResultConflict | failure；包括准入后的启动失败、确认清理的 Exec 超时；Exec 没有 conflict |
| 无尝试准入的准备错误/冲突、预算 admission 拒绝 | none，不把未准入错误当作执行尝试失败 |
| 原结果 ResultDenied/ResultCancelled/ResultOutcomeUncertain | none，不改变工具原本是否已执行的事实 |
| 尚无确定 durable result（包括 Sync 失败） | 不进行选择，不启动检查；按 writer/owner 失败边界处理 |

原 runtime 当前没有统一向 Post 传递 started 证据，实施时在 exclusive 返回值及重复编排缓存中显式保存同调用已接受 started 的强类型准入事实；replay 从已验证的 started sequence 重建，不增加第二条“进程已启动”事实。ModelOutput 组合边界需要接收经调用归属校验的选择/计划证据，不能仅将原先的事件常量替换成“任一合法事件”。准备错误仍保存原结果，但不伪造准入。核心控制流不比较自由文本 code/preview。

选择完成后才执行独立启动门禁：none 以原拒绝/取消/工具未成功等适用原因收口；success/failure 无对应分支固定 skipped/hook_event_unmatched，turn 取消仍由外层取消流程处理。有对应分支时先处理当前取消，再检查 stdin 和剩余期限；合法且 context 可用才 Sync Hook started 并交给 runner。writer/runtime poisoned 时停止，不能为了补终态而再启动脚本；只能走原持久化或离线恢复边界。

例如 error result Sync 后立即取消，route 仍是 failure，已有失败分支的 terminal 为 cancelled/cancelled，零脚本；恢复看到相同错误也仍选择 failure，但未开始检查以 recovery cancelled/session_interrupted_before_post_hook 收口。终态原因反映不同中断事实，选择不因 context 或恢复模式改变。原结果若本身是 cancelled，则 route=none；两种情况不得混淆。

```text
ready/native/usage Sync
  -> Pre 门禁
  -> 双分支计划 Sync + 全部精确信任
  -> candidate / 普通 approval / 执行尝试准入 Sync
  -> executor（可能启动失败）和 cleanup -> 唯一真实 result Sync
  -> 仅按事实选择 success / failure / none
  -> 选中分支存在性、当前取消、input 和剩余预算前检
  -> Hook started Sync -> run/join -> finished Sync
  -> terminal Sync -> budget close
  -> 后续 call -> 全部 native outputs Sync -> 下一采样安全点
```

普通失败检查错误不改变原工具错误，不递归触发失败 Hook，不阻断已允许的后续 sibling；取消、持久化失败和 cleanup 不确定继续停止该 turn。重复调用复用相同 result/plan/terminal，不因 failure 路由再执行原工具或检查。

### 4. 独立失败 stdin，只传安全分类

新增 typed failure 输入构造器、strict decoder 和 validator。通用 Hook 输入采用封闭事件分支，失败事件不使用成功 tool_response，也不通过 map[string]any 存储错误对象。暂态 stdin 必填字段恰好为：

| 字段 | 规则 |
| --- | --- |
| hook_event_name | PostToolUseFailure |
| session_id/thread_id/turn_id/tool_use_id | 原真实身份；tool_use_id 为 Provider call ID |
| tool_name/cwd/tool_input | 原真实 facade、安全 workspace 根与既有三分支 typed 输入 |
| error | 宿主生成英文摘要 `Tool execution failed: <code>`；code 仅接受 1..128 小写 ASCII 字母/数字/下划线，不能投影时使用固定 tool_execution_failed，不复制底层 error 或 preview |
| is_interrupt | 必填 false；不支持取消后执行检查 |

总量仍为 512 KiB，未知/重复/缺失/null/类型不符/尾随数据拒绝；error 最多 256 bytes，禁止 NUL。过大只保存 hook_input_limit 终态，零进程，不裁剪工具参数或改变原结果。只从冻结结果生成分类，不读取文件、artifact、历史、stderr/stdout；不传 artifact ID、绝对存储路径、PID、环境、Provider 凭据或 opaque reasoning。`error` 不是 Claude 原样错误字符串的完整兼容实现。

脚本 exit 0 的非空支持输出为 `{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"项目诊断建议"}}`，必须严格验证与执行事件相同。额外接受空白和严格空对象；additionalContext 最多 4 KiB、有效 UTF-8、非空且无 NUL。none 模式拒绝正文而不保存，model 模式也必须有精确信任。exit 2 与其他非零是 Hook 自身 error/hook_exit_failed，不阻断、改写或自动重试原工具。原 stderr/普通 stdout 不进入模型、Session、错误或 artifact。

与成功 Post 共用 parser 骨架、反馈 candidate/freezer 和进程 owner，只把封闭事件检查和输入投影作为策略；保留 Pre 特有 deny 语义。不要把 Pre 的 exit 2 判断全局放宽。

### 5. 两个互斥分支共享调用额度与执行期限

当前调用 allowance=A；任一分支存在 model 声明时只 reserve 一次 `R=min(16 KiB,max(0,A-1 KiB))`，否则 R=0。base allowance=A-R。按每分支 model 数和包装预先计算 delivery；两个互斥分支不相加保留提示槽。每个可能分支单独保证各未处理 model 项 512 bytes 的安全提示槽，最小集合放不下则该分支 budget_unavailable。

未选择的分支不执行、不产生反馈、不扣 bytes。选定分支的正文/来源/包装/提示共用 R，每块完整接受或拒绝，已 durable 前缀不可被后项挤出。真实结果、反馈各按 Sync 序列提交一次；唯一 terminal close 才释放余额和推进 call count，不向另一分支二次 close。

none 或 unmatched 没有所选可运行分支，processed/unstarted_from 均为 0，反馈为空；仍用同一 terminal 关闭原 ticket。原错误保存后才取消时不改 route，已选择分支的取消沿原反馈提示规则收口；不得切换到另一分支借用提示槽。重复编排与恢复都不能再次 reserve、提交同一段或 close。

若只有未选择分支配置 model，预留仍可能缩小原预览；terminal 释放未用 R，不重扩展已保存结果。这是前置保留最坏情况空间的明确代价，文档和回归测试必须覆盖，不能宣称与无 Hook 完全字节相等。

Pre 和实际选中的后置分支共享 30 秒执行预算；不为失败重新计时。单项默认 5 秒、最大 30 秒；两输出各 16 KiB；TERM/KILL、reader drain、done/lease 与 Darwin 安全 adapter 保持原行为。不支持的平台继续失败关闭，不复制第二套 process owner。

### 6. 严格当前事实与恢复

沿用 `tool_post_hook_planned`、`hook_execution_started`、`hook_execution_finished`、`tool_post_hook_completed`，不新增预留 event kind。计划的外层唯一 invocation/capability 与原 ready/调用序号/预算关联；分支仅保存 event/hooks/delivery，结果未知时计划不保存 route。Hook run identity 包含所选真实 event、外层 plan digest、ordinal，不能只靠 invocation:ordinal。terminal 绑定 route、origin、实际处理前缀/未开始后缀、预算和 feedback digest。

没有单独的选择 record：只有确定 result Sync 后才可选择；replay 用同调用的尝试准入/result 验证 route，不使用当前 context。原结果 error 后发生的后置取消不把 route 改为 none；原结果 cancelled 才属于 none。success/failure 分支存在时，以该分支长度 N 验证连续前缀及后缀；分支缺失或 route=none 时没有 N 项运行计划，processed/unstarted_from 必须为 0，suffix_feedback 和全部 started/finished 必须为空，不套用“skipped 必有未运行项”的一般规则。另一分支没有 fake finished、提示或统计。最后 finished 与 terminal 仍原子保存，原结果永远只保存一次。

恢复矩阵：

| 崩溃位置 | 本地补偿 |
| --- | --- |
| 分支计划后、原工具尝试准入前 | 沿既有 ready/approval 取消规则补原结果；route=none，后置 skipped/tool_not_successful，无反馈 |
| 原工具尝试准入、无 result | 补原工具 outcome_uncertain，route=none；不推断 failure，不启动检查 |
| 准入后确定 error/conflict result、存在失败分支、无 Hook started | 保留错误及 route=failure，只追加 recovery cancelled/session_interrupted_before_post_hook terminal |
| 准入后确定 success/error，所选事件未配置 | 保留 success/failure 选择，以 recovery skipped/hook_event_unmatched 收口，不借用另一分支 |
| error result 后实时取消，cancelled terminal 已保存 | 保留 route=failure 与原 terminal/code，不改为 none 或二次补偿 |
| 已 durable 第 0 项、下一项未 started | 保留前缀/反馈，只补 interrupted terminal，未开始项无 finished |
| 某项 started、无 finished | 仅该项补 recovery-origin uncertain finished，与 terminal 同批；不声明旧进程已清理 |
| terminal 已 durable、native outputs 缺失 | 从原 result 和冻结反馈补配对 outputs，不读取当前来源 |
| outputs 已 durable | 不重复 outputs，只按既有规则关闭旧 turn |

origin=recovery 的新后缀反馈必须为空，不为未开始项制造模型提示。补偿可以在已验证事实之后按原规则追加自己的 terminal origin/code，但不能修改事实选择；不恢复 trust、不请求 approval、不探测 PID、不执行任何工具或脚本、不读取配置/资源/artifact、不访问 Provider 网络。当前 Hook Load 仍晚于旧事实 reconciliation。二次中断恢复必须幂等；验证失败时在 repair/SQLite 更新前失败且 journal bytes 不变。

实现直接替换受影响 `testdata/current` 固定 bytes，拒绝旧单分支字段结构，不添加 schema_version、兼容窗口、migration 或原地历史重写。通用 lease、writer 和 repair 机制保持不变，但要为新恢复路径复跑真实子进程竞争和 Sync 故障验证。

### 7. 交互与双 Provider 输出

`/hooks` 展示三事件并按事件、来源、matcher 解释时机；失败分支审查明确“只在执行尝试确定失败后运行，包括准入后的启动失败”，同时说明正常非零退出仍走成功事件，model 还明确发送给 Provider 并保存 Session。默认拒绝、Esc 拒绝、Ctrl+C 取消，paste 与旧响应不能误授权；浏览、reset、View 无 I/O。

唯一当前 `PostHookCommittedPayload` 只携 invocation_id、plan_digest、detail_sequence、phase 四项，删除固定成功 event 字段；不加可空事件、伪 pending 事件或额外事件 kind。计划阶段通知只表示后置计划已保存，不能显示某个事件已选中或脚本已启动。实际 run 的 event 仍在精确 reference/run identity 中；宿主从相同计划和最新已验证结果/terminal 快照读取选择。若通知先于相应快照或序列过期，使用既有身份校验/刷新机制，不猜测事件。getter、构造器和 render 不进行查询 I/O。

主线保留原工具 Error/Conflict；随后检查的 checking/failed/completed/cancelled 与原工具终态正交。检查通过只能表示诊断脚本通过，不显示“工具已修复”。Ctrl+O 使用 invocation/event/plan/ordinal 与 durable detail_sequence 查询；旧分支、旧序列和旧 turn 消息不能刷新当前详情，锚点/草稿保持。等待 cleanup 时不能提前 idle。

ModelOutput 只组合已校验所选事件/计划的冻结反馈与原结果；none/unmatched 只能有空反馈。保留已有 `<post_tool_use_feedback>` 包装，内部 event 精确为实际事件。禁止混合 success/failure 内容，原 status/code 与 OpenAI function/custom output、Anthropic tool_result 配对和 is_error 语义保持。反馈仍是工具数据，不能采用 Codex 的 developer 注入或伪造用户消息。无匹配不改变原工具 schema/catalog 或稳定前缀；配置、信任、时间和活动身份不进入缓存前缀。恢复字节等价比较针对同一已冻结/已补偿历史，不把“稳定前缀未变”误写成“有无 model 预留时整个请求相同”。

共享进程 owner 对信号请求与最终清理事实分别判定：原组已确认停止时不再发信号；信号与自然退出并发时，仍依次完成有界原组探测、唯一 Wait 和流收口。信号调用失败且最终原组仍活跃、探测失败、Wait 或流清理不确定时继续失败关闭，不用单次信号返回值代替最终资源状态。确定性故障注入覆盖已停止及探测后退出两个窗口。

### 8. 实施与验收矩阵

| 层次 | 必须覆盖的业务断言及落点 |
| --- | --- |
| 配置/domain | 三事件顺序、来源合计/id/8 项限制、clone、digest、严格缺失/重复/null/错事件；扩展现有 domain/*hook*_test.go 与 extension/hook/*_test.go |
| 路由/runtime | 准入后的 error/conflict、进程未启动的确定错误、确认清理的 Exec 超时；go test/rg 正常 exit=1 仍走 success；error Sync 后取消保持 failure、原结果 cancelled 为 none；当前 context/恢复不影响纯选择；重复缓存保留准入证据 |
| 预算/tool | 单/双分支 model/none、JSON 转义膨胀、每项/整段超限、提示槽不足、未选分支预留释放、分段只计费一次、重复 close 拒绝；扩展 result_budget/post_feedback/model_output 测试 |
| 进程/安全 | Darwin 真实失败工具后运行只读脚本；工作区写/网络/私有数据根拒绝；假时钟/屏障注入 timeout、阻塞 stdin、输出超限、cleanup 不确定、shutdown timeout；复用 hook_process/process owner 故障设施 |
| Session | 固定 current fixture 覆盖精简分支、无事件选择、unmatched、取消/恢复各切点；拒绝跨分支第 0 项、重复调用归属、错误 route/digest/预算/前缀；corruption+半行不能 repair；真实子进程竞争和二次恢复幂等 |
| Provider/app | 两家 fixture server 验证失败反馈后继续下一 sample、原生顺序/配对；OpenAI apply_patch custom、Responses function、Anthropic tool_result；uninterrupted/restored canonical bytes、opaque bytes、footprint、cache fingerprint 等价；有 WS 路径复用现有 fallback 回归 |
| TUI | 结果未知时中性 planned 通知、四字段 strict payload、相同计划/序列快照刷新、窄屏审查、默认拒绝及过期响应；真实 PTY 覆盖原错误后取消、失败诊断独立显示和 Ctrl+O 草稿/锚点 |
| 工程 | 运行 make verify，覆盖 gofmt/vet/Staticcheck/架构/全量/race；OpenSpec strict 校验、文档链接和实际 vocabulary 交叉核对 |

专项测试均使用本地 fixture、合成 secret 哨兵和确定性故障注入，不使用真实 API key、默认外网或 sleep 证明竞态。实施验收报告必须区分基线验证与新增功能验证、真实 Darwin 执行与其他平台编译检查，记录命令、退出状态和不可执行项；未通过不得归档。

## Risks / Trade-offs

- 两个可能分支都需事前信任，可能增加交互次数 → `/hooks` 解释时机，保持精确信任复用，拒绝仍为默认；不在失败后追加意外授权。
- 用户可能把正常非零退出当作此失败事件 → 明确 go test/rg exit=1 仍走成功 Post；仅本切片不能兑现测试失败自动诊断，Claude 式命令解释应另开契约变更。
- started、事件选择和取消混为一谈 → started 仅为尝试准入，route 只读 durable 事实；检查启动门禁和 disposition 独立，none 不表示工具没执行。
- 真实失败可用信息少，尤其不提供 stderr → 仅允许安全分类，诊断脚本可读工作区；不以扩大隐私披露换取错误解释。
- 双分支计划影响恢复与 current fixtures → 使用一套当前结构、强校验和固定 bytes；不将兼容设计偷渡进来。
- 只配置另一个 model 分支也可能缩小预览 → 保留最坏情况预算并明确说明，不在结果保存后重算。
- 更广泛失败、取消、准备阶段和自动修复仍未覆盖 → 文档明示本切片边界，若实现必须扩大范围，先更新 artifacts 并重新确认。

## Migration Plan

本项目没有存量业务数据；这里不实施数据迁移。apply 时修改唯一当前结构、消费者、fixtures 和规格，旧 shape 失败关闭。完成专项验收与 make verify 后同步主规格、架构、能力状态、Roadmap、pitfall，再按归档流程核对。回退以回退同一完整代码/规格变更为单位，不将新结构 journal 喂给旧 reader，也不重写历史记录。

## 提案阶段核验（历史基线）

以下内容记录 apply 之前的规划核验，不代表当前任务或实现状态；最终验收和归档结论见 [archive.md](archive.md)。

首次提案阶段仅新增规划文件，当时现有代码基线 make verify 退出状态为 0，包含 gofmt、vet、Staticcheck、架构测试、全量测试和 race。当前修订仍只涉及本 change 已存在的 proposal/design/specs/tasks，不修改生产代码、测试或主规格；六项 capability 路径保持不变，全部实施任务保持未完成。

本次修订已通过 OpenSpec strict 校验及规划一致性检查：六项 capability 路径与 delta 一致、全部 modified requirement 名称可在主规格定位、原有 requirement 的场景均保留并补充新场景、33 个任务身份和未完成状态保持。已核对事实选择/启动门禁/检查终态、精简计划归属、取消与恢复、通知字段和对应验证任务；文件范围比较确认只修改本 change 九个既有规划文件，没有新增 artifact，全部 Git 已跟踪文件内容不变。

本次复跑 make verify 退出状态为 0，包含 gofmt、vet、Staticcheck、架构测试、全量测试和 race；现有代码未变，部分测试使用缓存。该质量门结果只证明现有代码基线，不能用来勾选新增能力任务；实现与专项验收仍须由后续明确 apply 请求触发。


### 2026-11-11 回归修正：完整命令的审查编码预算

独立回归中，4 KiB 的 `<` 诊断正文使合法 shell 命令的信任消息 JSON 超过原 decoder 的 16 KiB 假设，AgentLoop 因此拒绝合法控制事件并取消回合。原始 command 的 8 KiB 契约保持；审查请求编码预算按原始 command 的最坏六倍 JSON 转义加封闭身份/引用元数据余量计算。只调整携带完整声明的 hook_trust_requested decoder，响应、reset、统计活动及原始字段 validator 不放宽；不增加字段、kind、版本或兼容分支。三事件都以最大原始命令、转义扩张、未知字段和超限拒绝回归验证，应用端再验证实际预算拒绝不会取消原回合。
