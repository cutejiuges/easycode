## Context

动机见 proposal.md。2026-11-08 用户已确认本提案范围；reasoning 展示由另一隔离进程推进，本 change 不接入该模块。EasyCode 基线为 `d7af3c9`，当前九项模型工具、本地 Skills 与 PreToolUse 可执行。唯一活动的既有 `add-pre-tool-use-command-hooks` tasks 已全部完成，但尚未归档；本提案消费已同步的主规格，不代为归档或修改它。

### 实现接缝与冲突

| 现有位置 | 已观察行为 | 本次设计影响 |
| --- | --- | --- |
| `internal/extension/hook/config.go` | 只接受 PreToolUse，规范内容产生 source revision | 同一严格 loader 增加第二个真实事件，不新建并行配置系统 |
| `internal/domain/hooks.go` | plan 固定 PreToolUse；run ID 仅为 invocation:ordinal | 引入封闭事件与披露策略；run identity 必须包含事件和 plan digest，避免前后检查碰撞 |
| `internal/runtime/exclusive.go`、`hook_gate.go` | Pre 门禁先于候选/审批；执行骨架最终 Sync 唯一 tool result | 后置处理由同一 exclusive owner 编排，真实结果先 durable，再运行 Post |
| `internal/runtime/hooks.go` | trust 与 completed map 由实例持有 | 信任包含事件/披露策略；Pre 的缓存不能错误短路尚未完成的 Post |
| `internal/tool/builtin/hook_process.go` | 复用进程 owner；parser 只返回安全分类，不传播正文 | 统计结果继续无原始输出；新增受控、显式披露的候选内容分支 |
| `internal/provider/provider.go` | PrepareToolOutputs 只接收 frozen InvocationResult | 改为接收强类型模型输出值，组合不可变结果与已冻结反馈 |
| 两家 `tool_output_preflight.go` | 对冻结结果重新验证配对、编码与大小 | 继续验证原结果；附加反馈单独验证后进行完整 output 编码检查 |
| `internal/session/hooks.go` | 每调用至多一个执行前 gate，恢复缺失 finished 不重跑 | 保留独立 Pre 状态，新增 Post 生命周期；结果存在后仍可能存在待收口 Post |
| `internal/app/transcript_queries.go`、`internal/tui/hooks.go` | Hook 详情只包含安全统计；UI 文案固定 PreToolUse，关闭工具后丢弃 checking | 分开维护两阶段状态，Post checking 不受工具终态抑制，已授权反馈通过 typed 详情读取 |
| `internal/app/transcript_owner.go`、`internal/session/tool_history.go` | 历史按单一 invocation/ordinal 数组或 plan map 关联 | 关联键升级为 invocation/event/plan digest/ordinal，不用 Pre 数量校验 Post ordinal |
| `internal/tool/result_budget.go` | Commit 同时扣 bytes 并退休一次调用 | 改为调用级 reserve/base/feedback/close，不能对反馈重复调用旧 Commit |
| `internal/tool/exec_output.go`、`internal/tui/transcript_browser.go` | Exec preview 内含 artifact 引用；详情页按稳定游标缓存 | Hook 不接收该 preview；正文缓存另绑定最新 durable detail sequence |

### 两个参考工程的只读证据

以下是本地快照观察，不声称代表服务最新版本；不引入参考仓库依赖或大段代码。

| 参考与路径 | 观察 | 吸收与取舍 |
| --- | --- | --- |
| Claude Code 2.1.88 / sourcemap `a8a678c`：`restored-src/src/services/tools/toolExecution.ts`、`toolHooks.ts` | 成功工具执行与 PostToolUse 分开；后置信息使用工具关联的附件，避免重复展示 block reason | 用户体验保持“工具结果 + 后置检查”，不把已发生的修改显示成没执行 |
| 同上：`src/types/hooks.ts`、`src/utils/hooks.ts` | PostToolUse 支持 hookSpecificOutput.additionalContext；还支持更宽的 decision、continue 和 MCP output 改写 | 本次只接受严格 additionalContext 子集，不支持输出替换、自动批准或隐式控制流 |
| Codex `6f8bf683c`：`codex-rs/core/src/hook_runtime.rs` | 工具 handler 提供已适配的稳定 PostToolUsePayload，而不是传递任意内部工具对象 | 输入使用强类型结果投影，Provider wire、artifact 存储路径与环境不外泄 |
| 同上：`hooks/src/events/post_tool_use.rs` | 匹配别名与真实 tool_name 分开；run identity 关联 tool_use_id；状态、反馈、additional contexts 分离 | 保留真实 facade、调用关联和各结果职责；本地无约束 Value 不进入 EasyCode 核心协议 |
| 同上：`core/src/context/hook_additional_context.rs` | Hook context 有独立来源语义，与普通助手输出分离 | 反馈标明来源，但放在原工具配对 output 中作为工具数据，不提升为 system/developer 权限 |

选择 PostToolUse 而不是扩大 Skills 的理由：本地 Skill 主文档、同来源资源、显式附件与补全已经闭环；allowed-tools、fork、hooks frontmatter 分别引出权限范围、子线程和声明生命周期，本次不能假定这些前置能力已存在。PostToolUse 已有实际执行和检查消费者，能较集中地复用现有边界。

## Goals / Non-Goals

**Goals:** 工具真实结果与检查结果独立；用户在任何修改前理解并批准脚本及内容披露；脚本检查可取消、有界且无隐式写权限；模型收到可回归的反馈 bytes；从任一合法崩溃尾部恢复均不重跑副作用。

**Non-Goals:** 采用 proposal.md 的排除项。实现时不得为未来事件增加空策略、通用动态 registry、兼容 reader、文件 watcher、额外 Tool facade 或更高权限沙箱。若必须增加其他事件、原生消息角色或自动写入，先更新本提案并确认范围。

## Decisions

### 1. 扩展唯一当前配置，事件与披露纳入身份

继续只从两处安全配置 FD 建立冻结快照。新增 `hooks.PostToolUse` 数组，与 PreToolUse 使用同一 matcher/command schema；每条 Post command 新增 `feedback`，仅接受 `none` 或 `model`，缺省规范化为 `none`。PreToolUse 禁止该字段，避免无声扩大原门禁的输出语义。

```json
{
  "hooks": {
    "PostToolUse": [{
      "matcher": "Edit|apply_patch|Write",
      "hooks": [{
        "id": "check-format",
        "type": "command",
        "command": "./scripts/check-format-readonly.sh",
        "timeout": 5,
        "feedback": "model"
      }]
    }]
  }
}
```

示例脚本必须已经存在、只读且不依赖写缓存；本 change 不自动创建或执行工程内脚本。执行平台沿用 Darwin。不得把自动运行 gofmt -w、测试缓存写入或联网检查声称为支持能力。

每来源 64 KiB/32 commands、两来源总计 64 commands 均按两个事件合计；id 在同一来源所有事件中唯一。每事件/调用至多匹配 8 项，运行顺序仍为 user 后 project、各数组原序。规范序列显式为 PreToolUse、PostToolUse，保持对象键顺序无关；同一来源 revision 覆盖完整配置。

事件为仅含 PreToolUse/PostToolUse 的强类型枚举。reference、plan、run identity 与 fingerprint 必须包含事件；command fingerprint 还包含 feedback、matcher、timeout 和原 shell/env/sandbox policy。Post wrapper digest 同时绑定预算 A/R、delivery 与引用，run ID 由 invocation、事件、该计划 digest、ordinal 确定，不用随机 ID 或文本匹配区分 Pre/Post。旧开发 shape 直接拒绝并替换 current fixtures，无版本标记。

选择共享配置/执行骨架，而不是新增 PostHookManager：文件安全、排序、计时与 cleanup 的语义完全相同；Pre 的 deny parser 和 Post 的反馈 parser 语义不同，由骨架显式持有小策略，不通过 embed shadow、字符串核心分派或全局注册表扩展。

### 2. 工具前审查完整后置计划

在原 exclusive slot 中先按现有规则完成 Pre gate，再冻结 Post plan；没有匹配 Pre 时直接进入 Post 规划，不伪造 Pre plan/continue fact。plan 包含 invocation、能力、事件、有序声明引用、feedback 策略、反馈预算预留和内容 digest；不保存命令正文、cwd 或凭据。plan Sync 后，逐项完成 Post 的独立信任，之后才进入候选准备、普通批准和 tool started。

Pre/Post 相同命令也不能共享授权。审查明确“在操作成功后运行”“工作区只读/禁网”“feedback=model 时该字段将发送给服务端并保存到 Session”。信任仍仅限当前 Runtime/workspace 实例，默认拒绝、可取消、过期响应拒绝；不从历史 facts 重建授权。配置无效、匹配超限、平台不支持或信任拒绝在原工具副作用前收口，不能等修改完才询问信任。

仅命中 Post 的 headless 调用也在候选准备前得到 `hook_trust_required`。未匹配时不增加 post facts、检查、预算扣费或 output 字段。无论 Hook 是否通过，Patch/Write 的 grant、完整 Read 证据、候选重验及 Exec 每次批准均保持原语义。

### 3. 同一 owner 编排，先保存真实结果

```text
sample/native/ready Sync + finalize
  --> preceding call and its post phase complete
  --> existing PreToolUse gate
  --> Post plan Sync --> independent Post trust
  --> existing candidate/approval/started Sync
  --> executor and cleanup --> frozen tool result Sync
  --> successful executed result? -- no --> Post skipped Sync
  --> Post started Sync --> run + join --> finished Sync
  --> next Post or Post completed Sync
  --> next call --> complete native outputs Sync + finalize
  --> steering / next sample
```

Post admission 需要真实 `tool_execution_started` 和 `ResultSuccess`。普通拒绝、准备失败、conflict、取消、不确定、预算拒绝均只收口已存在 plan 为 skipped，零 Post 进程。Exec 的正常非零 exit 仍是当前工具的 ResultSuccess，因此运行 Post 并把真实 exit_code 传给脚本；不能用字符串或 exit==0 偷换已有成功定义。

实现以 executeApproved 的 exclusive 编排骨架持有具体工具策略，保证每个实际执行分支先持久化真实 result，再执行或跳过 Post；不在 persistToolResult、getter、通知回调或 View 中启动进程。成功终态通知可先发布，随后单独发布后置检查活动，但 exclusive 屏障保持关闭。后续 sibling 不因普通 Post 检查失败被取消；turn 取消、writer poison 或 cleanup 不确定仍停止后续工具。

真实 result 只冻结/扣账/持久化一次。不得把 Hook 信息写回它，重写 record 或再生成同 invocation 的第二个 result。Pre completed 缓存只表示已知真实 result，不能说明 Post 完成；分别保存 plan/result/post terminal 状态，重复编排重用 durable 结果且不重复启动。

### 4. 稳定有界 stdin 与受限输出

Post stdin 为下列唯一当前对象，所有列出的字段必填；严格拒绝未知/重复键、null、错误类型和尾随 JSON，不接受省略字段的旧形状：

| 字段 | 类型与约束 |
| --- | --- |
| hook_event_name | 固定字符串 PostToolUse |
| session_id / thread_id / turn_id | 当前真实合法身份，分别使用既有领域校验 |
| tool_use_id | 原 Provider call ID，非 invocation ID；沿用 Pre 的非空 UTF-8/256 bytes 上限 |
| tool_name | 当前真实 Edit/apply_patch、Write、Bash/exec_command，与能力严格匹配 |
| cwd | 仅发给本地脚本的安全工作区根绝对路径；Hook 进程仍在该根运行 |
| tool_input | 沿用 Pre 的三分支 typed 输入，Exec 的相对 workdir 保持原值 |
| tool_response | 下列由 capability 判别的封闭元数据联合，只从已 durable 的真实结果构造 |

三个 tool_response 的完整形状如下；示例计数只是说明，不是输出模板中的默认值：

```json
{"capability":"fs.patch","status":"success","code":"ok","metadata":{"file_path":"src/example.go","replacement_count":1,"added_lines":2,"removed_lines":1}}
{"capability":"fs.write","status":"success","code":"ok","metadata":{"file_path":"src/example.go","operation":"create","bytes":12,"lines":1,"created_directories":0}}
{"capability":"process.exec","status":"success","code":"ok","metadata":{"workdir":".","exit_known":true,"exit_code":1,"stdout_bytes":120,"stderr_bytes":0,"output_truncated":false,"timed_out":false,"cancelled":false}}
```

status/code 固定 success/ok，能力与 tool_name/tool_input 一致；所有计数为非负整数并遵守现有真实结果 validator，不能接受浮点数、填入推测值或从输入重算操作结果。Patch 路径/计数来自 PatchResultMetadata，Write operation 仅 create/overwrite，bytes/lines/created_directories 来自 WriteResultMetadata；Exec workdir 为规范相对目录，exit_known=false 时 exit_code 必须为零，known 时保留真实整数，stdout/stderr bytes 为 observed bytes。其余 flags 从 ExecResultMetadata 原样获取。

所有分支均不提供 preview、stdout/stderr 正文、artifact ID/存储路径、PID、transcript 路径、完整历史、环境或 opaque reasoning，也不把整个 InvocationResult 序列化。现有 Exec preview 内含 artifact 提示，因此不能透传后再用字符串删除；本次选择 metadata-only 投影而非新增输出捕获/持久化通道。秘密凭据不进入宿主生成字段；已有 tool_input 仍是明确授权给本地脚本的用户工具参数，不宣称能自动识别其中任意自由文本秘密。

同一纯内存构造验证输入最多 512 KiB。输入过大时保留成功 result，不启动该 Hook，记录 `hook_input_limit`；不得截断参数、重新读取文件或把工具改为失败。stdout/stderr 沿用各 16 KiB 上限，单项默认 5 秒/最大 30 秒；Pre/Post 对同 invocation 共用原有 30 秒执行预算，信任等待不计时但可取消。执行前剩余预算不足的项不写 Hook started，以 `hook_execution_budget_exhausted` 收口剩余未启动后缀；只有已准入运行确实到达期限时才使用 `hook_timed_out`。

Post exit 0 只接受空白、严格空对象或下列完整对象：

```json
{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"Format check found trailing whitespace in src/example.go."}}
```

additionalContext 为非空有效 UTF-8，最多 4 KiB，不含 NUL；未知/重复字段、null、尾随值、事件错配、decision、continue、updatedInput、updatedMCPToolOutput、async 均为 `hook_output_invalid`。feedback=none 时不接收任何非空 additionalContext，报告披露未授权且丢弃正文，不暗自发送或保存。原始普通 stdout 不是默认反馈，stderr 从不变成模型正文。exit 2 与其他普通非零退出均为 Post `hook_exit_failed`，不能伪造 Pre deny 或撤销工具。

运行 adapter 继续使用共享 process owner 的启动、stdin writer、双流 readers、Wait、TERM/KILL 和 Done。统计 HookRunResult 不含自由文本；Post 输出策略只向当前 owner 返回显式反馈候选。只有完整通过模式、事件、身份、编码和预算检查，才能转为可持久化的 FrozenHookFeedback。Pre parser 仍只返回安全分类，不接受 additionalContext。

输出分类和结果 validator 按事件判别：Pre 的 exit 2 仍为 denied/hook_denied；Post 的 exit 2 为 error/hook_exit_failed，必须接受其 exit_known=true/exit_code=2。不能全局放宽 Pre，也不能只改 parser 而让共享 validator 将合法 Post 误判为 uncertain。两事件均先处理 cleanup 不确定，再处理取消、实际超时、输出超限/I/O、普通退出及 exit 0 解析；未启动后缀没有 exit/timeout/cleanup 执行统计。

### 5. 明确披露契约与独立预算

feedback=model 是明确披露授权，不代表自动脱敏任意工作区正文。UI 必须直说内容会进入 Provider 请求和 Session。脚本作者不得输出秘密；不把任意 stderr 作为所谓“安全诊断”。只有受信任声明明确输出的 additionalContext 可以进入这条通道，其余 stdout/stderr/reason 的合成 secret 哨兵应在 Session、模型、错误、diagnostic 和 artifact 中完全缺席。已授权反馈正文不得额外进入诊断日志或自动 artifact。

新增稳定分类 `hook_feedback_not_allowed`、`hook_feedback_budget_exceeded`，分别表示未授权正文和整块反馈超预算；它们是检查错误，不改变工具 status。`hook_execution_budget_exhausted` 只表示未启动后缀的检查预算不足，不伪装成进程实际超时。恢复中成功工具而 Post 尚未 started 使用 cancelled/`session_interrupted_before_post_hook`；Post started 无 finished 使用 uncertain/`hook_outcome_uncertain`。这些均进入封闭枚举和 strict validator，不按字符串匹配驱动分支。

每 invocation 最多接收 16 KiB 的**最终冻结反馈段 bytes**，包含来源标签、结构包装与安全失败提示；每项 additionalContext 原文至多 4 KiB。按当前工具额度 A，在执行前预留 `R=min(16 KiB,max(0,A-1 KiB))`，但仅当计划含 feedback=model。原结果 allowance 使用 A-R，仍保留后续 calls 的 1 KiB 安全空间。

**字节不变的精确范围：** 无匹配或全部 none 时不预留，原预览/输出 bytes 保持既有行为。model 模式即使返回空对象，也可能已把原预览缩短；释放 R 不能重读输出、扩展已保存 preview 或追认“与无 Hook 相同”。例如 A=256 KiB、原预览 250 KiB、R=16 KiB，原预览须按 240 KiB 额度冻结。实际为空时仅省略反馈段，不承诺还原为 250 KiB；状态/计数及既有 artifact 保存策略仍准确。

现有 ResultBudget.Commit 同时扣 bytes、递增 processed、递减 sampleRemaining，且要求 metadata allowance 与未分割 A 相等，不能直接以 A-R 重复调用。由同一 turn owner 持有调用预算 ticket：

```text
reserve(invocation,A,R)
  --> base frozen and durable --> charge base bytes
  --> ordered feedback durable --> charge actual feedback bytes
  --> Post terminal durable --> close call once; release unused reservation
```

reserve 固定 invocation/call index/A/R/base allowance，不推进 processed/sampleRemaining；原结果的 budget allowance 为 A-R，ticket 验证其与原 A 的关系。base/feedback 每一分段只在对应事实成功 Sync 后计入已提交使用量，重复接受同一事实必须拒绝；无法确定 Sync 时 poison，不准入后序调用。只有 close 才递增 processed、递减 sampleRemaining 一次；没有 Post 的调用在结果 durable 后立即 close。BeginSample 和后序 admission 必须拒绝未关闭 ticket。Post completed 固定保存原 A/R、base 实际 bytes、feedback 实际 bytes 和 released=R-feedback bytes；回放交叉核验原 result 和反馈事实，不将原结果重新冻结或把反馈算成第二个 call。被拒绝或取消的原工具同样关闭 ticket 并释放未用预留。

冻结原结果与最终反馈段之和仍受原能力单次上限、sample 512 KiB/turn 2 MiB 约束。计费包含规范反馈包装和分隔符，但不包括 Provider 外层 JSON；每次候选接收还必须纯内存验证两家的实际 output 编码上限。

**失败提示先占位，不追溯驱逐：** 以每条 model 声明 512 bytes 为宿主 code 项最坏上限，并在开始接收正文前为所有尚未解决的 model 项及固定反馈包装预留空间。固定事件/来源/id/ordinal/封闭 code 必须用最大合法值测试证明单项不超过 512 bytes；超过即实现验证失败，不能在运行时缩掉已 durable 内容。若 R 连最小提示集合及包装都容纳不了，该调用不接收任何反馈正文或模型提示，仍可运行只读检查，记录 feedback delivery=budget_unavailable 与有界 typed 分类，最终释放 R。

有足够最小空间时，处理当前项：完整候选加已保存段、最终包装及后续提示预留仍能放入 R，才接收整块；否则拒绝全文并使用该项已经预留的安全 code 项。成功空对象释放本项提示预留，不强制产生模型文字；none 项不占模型提示槽。已保存正文/提示不因后续项失败而修改、删除或重新排序。每次 finished 在同一事实中冻结本项的 content/code/empty 选择及预算增量；符合第 7 节条件的未启动后缀 code 在 terminal.suffix_feedback 中一次性冻结。Post terminal 校验前缀与后缀提示的最终合计，输出为有序已冻结段的确定组合。恢复只复用这些 bytes 和事实，不重裁剪、补发提示、重新扣账或重新决定预算分配。

### 6. 模型输出组合与 Provider-native 一致性

新增由 `internal/tool` 拥有的强类型模型输出值，组合已冻结 InvocationResult 和独立的 FrozenHookFeedback；domain 中的 Hook 引用/反馈值不依赖 tool。Provider ToolResultPreparer 接收此有序值，继续只做纯内存配对/编码。无匹配或全部 feedback=none 时编码**逐字节等于既有输出**；model 模式空反馈只省略反馈段，其原预览可能受预留影响。有反馈时在该调用已冻结 preview 后附加单一规范反馈段，两家得到相同正文语义，分别维持 tool_result、function_call_output 或 custom_tool_call_output。原 status/code、Anthropic is_error、call ID 与 item 顺序不变。

冻结反馈段由固定 canonical codec 产生，包住 event/source/id/ordinal/content 或宿主稳定 code。宿主生成的 provenance 字段不含绝对路径、命令、运行 ID、时间或实际环境；明确授权的 content 是脚本正文，不宣称能自动清除其中所有路径或秘密。反馈是工具来源数据，不伪装为用户输入，不分配 InputID，也不提升到 system/developer 角色；不增添假的 Hook tool call。它不能成为 Provider 请求的反向历史来源。

Native restore record 的 tool-output 分支必须带同组已验证真实结果及 Post 收口/反馈证据。两家恢复路径用同一组合器纯内存重编预期 output，与存储 native bytes/配对校验；不能仅凭可伪造标签从 preview 中猜测反馈。无 Post 的调用也使用当前强类型组合值的空反馈，禁止双接口或历史 reader。离线预览与正式恢复共用 strict decoder，ProjectHistory 不额外拼入反馈或泄漏到 Hook 上下文。

Hook 配置、trust 和披露开关不进入稳定 tool catalog 或 skill metadata；工具尚未执行时请求/stable-prefix 不变。执行后 model 预留导致的原预览/保存引用变化，以及实际反馈，都只改变 committed_history 尾部，后续 canonical request 与 native digest 按实际内容改变，来源可追溯到配置 revision/Hook 引用。WebSocket 仅使用已 durable finalize 的新输出建立增量基线，恢复、取消及压缩保持既有 full/reset 规则。

### 7. 唯一当前 durable 词汇与状态机

保留 Pre gate 的 plan/decision；新增两个有实际消费者的事实 `tool_post_hook_planned`、`tool_post_hook_completed`。两阶段复用 `hook_execution_started/finished`，但其唯一当前 identity 必须显式绑定事件与 plan digest。finished payload 分离统计和冻结反馈：Pre/none 反馈必须为空；Post/model 才可有经过验证的冻结块。所有形状具有 typed constructor、strict decoder 和 validator。

Post planned 单记录 batch；有匹配 Pre 时位于其 continue 后，无匹配 Pre 时不要求该 fact，始终位于候选/普通批准前。它携带 refs、预算 ticket 和受控身份，不证明已获得信任。原结果以既有 tool_call_result 先确定 Sync；Post started 单独 Sync，且只能在对应执行成功 result 之后、stdin/剩余期限前检通过时接收。started 表示执行尝试已准入，不保证 OS 启动成功；启动失败仍有匹配 finished，exit unknown、零输出并 cleanup confirmed。每项 finished 与其 content/code/empty 披露选择原子 Sync。

**选择连续已运行前缀加未启动后缀，不新增伪造运行记录。** terminal 字段包括 invocation/event/plan digest、origin=execution/recovery、disposition、code、processed、unstarted_from、suffix_feedback、budget 终值和内容 digest。processed 是 finished 数量，unstarted_from 必须等于 processed，未启动集合固定为 [processed,N)；没有后缀时二者为 N。正常中间 finished 为单记录 batch；最后项 finished 与 completed terminal 同批。以下规则区分生命周期终态与检查通过：

| disposition | 前缀/后缀条件及 code | 合法批次 |
| --- | --- | --- |
| completed | processed=N，无 pending started；全部通过 code=ok，否则 code 为最早普通错误项 code | 最后 finished + terminal |
| skipped | processed<N，无 pending started；未成功工具/信任拒绝、共享 stdin 超限或执行预算耗尽；后缀统一采用 tool_not_successful/hook_trust_denied/hook_trust_required/hook_input_limit/hook_execution_budget_exhausted 中匹配原因 | terminal 单记录；原 result 尚未保存时也可 result + terminal 同批 |
| cancelled | 无 pending started；包括项间取消或恢复时下一项未 started，保留前缀；code=cancelled 或 session_interrupted_before_post_hook | 项间 terminal 单记录；活动项已取消则 cancelled finished + terminal 同批 |
| uncertain | 必须有最后一项 uncertain finished，剩余后缀未启动，code=hook_outcome_uncertain | uncertain finished + terminal 同批 |

输入超限或前置已用尽检查预算时 processed=0，零 Post started/finished；若某项已 finished 后再发现预算不足，只追加 skipped terminal，不能写 timeout=true 或假 finished。普通执行错误/实际超时/解析错误允许下一 ordinal 继续，不能沿用 Pre 的“前项必须 continue”规则；cleanup uncertain 或取消才截断后缀。completed 只表示已全部处理，不代表每项检查通过，详情须保留各错误项。真实 result 不为任何 terminal 重写。正常 origin=execution 且真实工具成功、Post 已全部信任、model 披露和提示预算可用时，terminal 可在 suffix_feedback 中冻结未启动 model 后缀各项的宿主 code；只允许有序来源引用与 code，不允许 content 或运行统计，不改变 processed。它们是新冻结的提示，不是伪造 finished，也不重复前缀正文；计费在 terminal Sync 时完成。origin=recovery、原工具不成功、信任未完成、none 或 budget_unavailable 时 suffix_feedback 必须为空；恢复不补发模型提示。terminal 的 code、计数、前缀、后缀和 budget 均必须可由先前事实及匹配结果验证，不能凭缺省值修补。

Replay 分别持有 Pre 状态和 Post 状态。结果存在并不自动解除已计划 Post 屏障；后序 call start、outputs、steering、下一 sample 均要求前序 Post terminal。完整记录中重复 plan、事件错配、无成功 result 的 post started、无执行 started 的 success、ordinal 重复/乱序、越过预算或缺失终态反馈，全部作为 corruption 拒绝，不能用 repair 删除。

所有新的 Session/native/checkpoint 字段属于唯一当前结构，不增加 schema_version/payload_version/canary 等门禁。更新全部受身份变化影响的 immutable current fixtures，包括原 Pre fixtures；测试运行时不得用当前 encoder 生成这些输入。

### 8. 取消、Sync 失败与离线恢复矩阵

执行前取消保持原 admission 线性化；已接受 executor/Hook started 后的 durable resolution 由同一 owner 等待，不能把超时当作未提交。真实结果 Sync 失败时零 Post 进程且 poison；Post finished/terminal Sync 失败时保留真实结果并 poison，不开始新 sample，不假装反馈已保存。cleanup 未确认时禁止释放 owner/lease，继续既有强制取消和最终 Done 路径。

| durable 尾部 | 恢复动作 | 不得发生 |
| --- | --- | --- |
| Post planned，无 tool started/result | 按原 ready/approval 状态补偿原调用，并原子收口 skipped Post | 信任对话、候选读取、执行或反馈 |
| tool started，无 result | 原工具 outcome_uncertain；Post skipped | 把后置计划当作成功证据 |
| 成功 result，无 Post started | 保留成功，收口 Post interrupted，补齐 outputs | 重跑工具或第一次执行旧 Hook |
| Post started，无 finished | 追加 recovery-origin uncertain finished 与 Post uncertain；保留成功，已完成前缀反馈不变 | 查询旧 PID、假称清理确认、重跑脚本 |
| 部分 finished，下一项尚未 started | 保留前缀，追加 cancelled/session_interrupted_before_post_hook terminal，后缀明确未启动 | 给未启动项写 uncertain finished 或声称进程运行过 |
| 部分 finished，下一项已 started 无 finished | 仅该活动项追加 recovery-origin uncertain finished，再与 uncertain terminal 同批；后缀未启动 | 给后缀伪造 started/finished、重新解析旧 stdout 或重算预算 |
| 最后 finished + completed，缺 native outputs | 用真实结果及全部 frozen feedback 补齐原生输出 | 读取当前配置、脚本或 artifact |
| native outputs 已存在，缺 turn terminal | 只关闭旧 turn | 重复 output、反馈或自动采样 |

活动 turn 恢复始终以一个既有 turn_failed 结束；不会因已保存成功结果而继续自动调用模型。比较对象是“相同已补偿历史”的不中断/恢复请求，不能声称丢失未提交反馈后仍等于从未崩溃的成功执行。二次恢复复用补偿事实，不重复 finished/completed/result/output/terminal。

完整 Loader/replay/native/feedback evidence 校验必须先于尾部 repair，在同一 exclusive lease 下完成；旧 turn 补偿仍先于新来源 Load/install。损坏前缀后即使有半行也必须保持全部 bytes 不变。配置/脚本消失、改动或当前预算不同不影响旧恢复；未来新调用重新审查当前快照。

### 9. Claude 风格交互与有界历史

`/hooks` 同时列出 PreToolUse/PostToolUse、来源、matcher、信任和 feedback 策略；详情支持现有滚动/窄屏/reset，不新增会隐式开启披露的按钮。Post 信任 overlay 出现在原工具批准前，明确时机与内容用途，普通 approval 和问卷仍互斥。

工具主线显示原成功及紧凑的后置检查中/通过/检查失败；不能从 Hook 错误回写工具 Error。非零命令 exit 维持现有命令终态。Ctrl+O 详情按原工具位置展示每项事件/来源/code/bytes，并通过 typed 有界正文查询提供已授权反馈；所有身份用于关联而非在普通主线泄漏。采用现有四页缓存与每页最多 64 KiB，不另存一份无限 transcript。

未授权原始输出无正文页。控制字符只在显示副本转义，反馈 frozen bytes 不变。resize、NO_COLOR、分页和折叠不读磁盘、不执行进程、不修改 Session 或 cache。定向 Ctrl+C 始终可取消检查并等待 cleanup；成功结果已存在时 UI 仍说明检查被取消，返回原草稿/阅读锚点。

**历史关联与刷新接缝：** session/tool_history 与 app/transcript_owner 的 plan 查找键改为 invocation/event/plan digest，每项完成再关联 ordinal；前后置前缀分别校验，不能以 len(Pre+Post) 约束 Post ordinal。历史中另保存已验证 Post terminal，包括无运行的后缀；不得从 finished 列表猜测“全部通过”。工具 success 与 Post checking 是正交状态，删除仅按 toolClosed 丢弃 checking 的旧假设，只接受同 turn/invocation/事件/plan 的合法更新，仍拒绝 Pre 在工具结束后重开。

新增实际使用的进程内 typed 事件 post_hook_committed，在 Post 的 plan/started/finished/terminal 批次 Sync 并安装查询快照后发布，每批一次；payload 为当前 session/thread/turn、invocation、固定 PostToolUse、plan digest、最新 detail_sequence 和 planned/started/finished/completed 阶段。它不是新模型工具或外部 headless 事件；headless 明确校验后忽略，不能按未知事件失败。现有 HookActivity 仍只携带受控状态/统计，Post checking 不受原工具 terminal 抑制；completed/skipped 表达聚合终态，取消与不确定保持独立，不覆盖真实工具结果。

detail_sequence 是该调用最新可见 durable 事实的 journal seq，不是格式版本。typed list/detail 查询增加该值；list 另携带不含正文或单项运行统计的 typed PostHook 聚合摘要，主线在 native commit 刷新及恢复时重建同一检查状态；后置正文使用独立 post_hooks source，要求请求携带期望 detail_sequence。app 从同一内存快照读取，发生变化则返回 transcript_changed 和安全重试语义，不拼接两个修订的页面、不保留无限历史快照。cache key 为 cursor/source/detail_sequence/offset，后置更新使对应旧页失效并推进查询 generation；旧回复不能覆盖新详情。用户停在详情时同样刷新，重置该 source 页偏移到首屏并保持原工具/阅读锚点；保留四页与 64 KiB 上限，其他工具正文页不无谓失效。恢复后从事实序列重建同一关联，不读取脚本。

用户示例在 README 交付一个可直接复制的只读 shell 包装：检查发现也返回 exit 0 的严格 additionalContext JSON，实际执行异常才非零。示例同时给出 metadata-only stdin 字段、feedback=none/model 区别、exit 2 非 Claude block 兼容含义及不能自动 gofmt -w/写缓存；Darwin PTY 与 mock transport 使用同一示例验证。

### 10. 验收与实现落点

| 实际测试落点 | 必须证明 |
| --- | --- |
| `internal/domain/hook_event_test.go`、`post_hook_codec_test.go`、`post_hook_input_test.go`、`hooks_test.go` | event/feedback identity、strict decode、复制隔离、前后 run ID 不碰撞 |
| `internal/extension/hook/{config,post_config,output,post_output}_test.go` | 两事件/默认 none/上限/未知字段、Post 严格输出、Pre 拒绝反馈、合成 secret 哨兵 |
| `internal/tool/*feedback*_test.go`、`internal/runtime/post_hooks_test.go` | ticket 分阶段计费/一次退休；model 空反馈的大预览变化；后项失败不驱逐已保存正文；metadata-only stdin 无 artifact；mixed-call 屏障 |
| `internal/runtime/post_hooks_test.go`、`post_tool_outcome_test.go`、`internal/app/hook_host_owner_test.go` | result/started/finished/completed 各 Sync 失败；取消线性化；错配 runner 与 cleanup 不确定 |
| 两家 `post_output_test.go` 与 `testdata` golden、OpenAI `post_websocket_test.go` | Edit/custom apply_patch、Write、非零 exit Exec；output 配对、opaque 数据、bytes/fingerprint、HTTP/WS continuation |
| `internal/session/post_hook_replay_test.go`、`testdata/current` | 固定字节的正常/拒绝/未启动后缀/各崩溃点，前缀后未 started 与已 started 分开恢复；suffix_feedback 与预算证据拒伪造 |
| `internal/app/post_hook_e2e_darwin_test.go`、`hook_resume_test.go`、`exec_headless_e2e_test.go` | 两家真实 transport 与三能力闭环，配置删除/脚本改变后零重跑，headless 失败关闭 |
| `internal/tui/hooks_test.go`、`post_hook_commit_test.go`、`internal/app/post_hook_query_test.go` | Pre/Post ordinal 各自从零；工具成功后显示 Post checking；详情打开期间刷新/旧回复丢弃/混页拒绝；默认拒绝、modal/无色/resize |
| `scripts/smoke-hooks.py --event PostToolUse` | mock Provider + 真实 PTY，信任/普通审批/后置检查/模型收到反馈/中断/终端恢复；不需要真实 key 或外网 |
| 现有 process/localfile/架构测试 | 原 owner/FD/sandbox invariants 不变；真实 Darwin 只读/禁网，其他平台不伪装支持 |
| 真实子进程 lease 测试 | 持有 Post 活动期间跨进程竞争失败、崩溃释放、占用时不 repair、续写 seq 单调及拒绝 bytes 不变 |

实施完成必须运行 `make verify`、本 change `openspec validate --strict` 和全部主规格校验；验收报告列出命令、平台、退出码及对应业务证据。同步 README、总体/Tool 架构、capability-status、Roadmap 和真实踩坑；核对所有测试路径与实际 record vocabulary 后，才能由后续流程同步 specs 并归档。规划基线测试不能替代新功能验收。

终态的 `feedback_digest` 绑定最终 canonical 反馈段，`digest` 绑定完整终态；Session 从 durable 前缀重算二者，并从同 sample/turn 的既有事实重建 ticket 额度和按 sequence 计费，不能仅信任记录自述预算。

## Risks / Trade-offs

- [用户主动反馈包含敏感工作区正文] → 默认关闭、逐条披露信任，只收结构化字段，明确告知会发送及持久化；不宣称任意正文可自动脱敏。
- [工具成功后脚本失败造成误判或重复操作] → 真实 result 先保存，独立检查状态，不执行自动重试，恢复不重跑。
- [Post 占满模型输出额度] → 执行前从原 allowance 预留，完整块接收，含包装计费，保留 accepted result 与后续安全空间。
- [Pre/Post 相同 ordinal 碰撞] → 事件和计划 digest 进入 reference/fingerprint/run identity，固定 fixture 和交叉配对拒绝覆盖。
- [串行检查增加延迟] → 沿用同调用共计 30 秒预算，限制八项/事件；不以 async 换取失去 owner 的生命周期。
- [只读沙箱无法运行自动格式化或写缓存的 lint] → 示例限定已有的只读检查；不临时放宽写权限，后续扩大须另行 OpenSpec。
- [与 reasoning 隔离工程最终合并冲突] → 本工作区只实施此 change；合并前重新核对目标分支及主规格，并在共享 protocol/restore 接缝按语义解决冲突，不复制另一模块实现。

## Migration Plan

没有存量数据迁移或兼容窗口。apply 时一次性更新唯一当前构造器/decoder/validator、所有消费者和固定 current fixtures；替换旧开发形状，禁止 resume 原地改写日志。代码回退使用对应提交的代码与测试快照，不为回退新增历史 reader。本次已按明确 apply 请求实施当前契约；主规格已按本次明确请求同步，archive 核对见 `archive.md`。
