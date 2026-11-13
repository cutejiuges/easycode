## Context

选型动机见 [proposal.md](proposal.md)。以下保留 PreToolUse 切片最初的设计依据与规划路径；实际实施落点及验收见文末说明与 [verification.md](verification.md)。2026-11-13 已将本变更覆盖的 delta 需求完整对齐当前主规格，后续已交付能力不再被旧 delta 覆盖；当前契约以同步后的规格及唯一当前实现为准。

### 现有接缝与必须修正的冲突

| 已观察到的实现 | 设计影响 |
|---|---|
| EasyCode `1a327e79d514a1456792336d526d2d8a11e02606`；无其他活动 change | 在单一当前契约上演进，不叠加历史 reader |
| `internal/runtime/exclusive.go` 分派 Patch/Write/Exec；只读批次在 `tool_scheduler.go` 中先收口 | Hook 放在此 exclusive 屏障内，不能在 Provider reducer 或工具 executor 中各自接入 |
| `runtime/exec.go` 已分离 candidate、approval、started、result | Hook 继续不能代替任何现有步骤；Hook 拒绝发生在 candidate 准备前 |
| `tool/builtin/process_execution.go` 已有 owner、退出观察、进程组清理与输出排空；candidate、artifact、renderer 与之耦合 | 仅抽取进程生命周期，禁止直接伪造 ExecCandidate/Invocation 来启动 Hook |
| Darwin launcher 有目录 FD、helper 启动屏障及退出观察注册；其他平台拒绝执行 | 保留这些保证并添加 stdin；不改成普通 `exec.CommandContext`，不扩大平台声明 |
| `extension/skill/secure_unix.go` 已逐段 no-follow 打开并由调用者检查 FD | 抽取实际共用的文件访问骨架；来源身份、优先级、digest 与错误映射仍由各消费者拥有 |
| `session/replay.go` 的 ToolRecoveryPlan 目前只有 ready/approval/started/result | 必须加入 Hook 子状态；旧逻辑将 Hook 已启动误判为工具尚无副作用，不能继续沿用 |
| `app/app.go` 目前创建 Runtime 后执行工具 reconciliation | 初始 Runtime 允许未安装当前 Hook 快照；先离线 reconciliation，再由同一 owner 显式 Load 并安装未来调用快照，最后 Start 接收输入 |
| `extension/hook/hook.go`、架构 AST allowlist 和总体架构第 12 章仍声明 Hook 占位；第 12 章也残留 Skills 未接入的旧描述 | 实施时移除 Hook 的占位 API 与对应 allowlist，修正文档，其他未实现事件不保留可执行占位 |

### 参考证据与吸收方式

参考工程位于 `/Users/cutejiuge/IdeaProjects/`，全部只读，不参与构建。以下结论仅针对固定本地快照。

| 参考快照及路径 | 已核对行为 | 本次取舍 |
|---|---|---|
| Claude sourcemap `a8a678cb6244e6770e1e421767ff0987a1d95549`，`restored-src/src/utils/hooks.ts`，`types/hooks.ts` | PreToolUse 封装、交互工作区信任、退出码 2、structured deny、普通非零退出继续；非交互分支隐含信任 | 保留事件名、交互入口和阻断概念；不继承 headless 隐式信任和普通执行错误继续的策略 |
| 同上，`utils/hooks/hooksConfigSnapshot.ts`、`components/hooks/HooksConfigMenu.tsx` | 启动快照、按事件/matcher/来源钻取，菜单以查看为主 | 使用实例持有的冻结快照与只读浏览，不复制全局可变状态、fallback 读取或热更新 |
| 同上，`skills/loadSkillsDir.ts`、`utils/plugins/pluginLoader.ts` | Skills 已有独立发现消费者；插件还承担多贡献、来源、安装与依赖 | 不重复当前本地 Skills，不把插件安装与信任一并带入本切片 |
| Codex `6f8bf683cee83ed4d580a2e106b587d8451db936`，`codex-rs/hooks/src/events/pre_tool_use.rs`、`engine/output_parser.rs` | 事件专用 request/outcome、matcher aliases、独立输出解析；有明确不支持字段检查 | 使用封闭 outcome、能力身份和边界适配；不复制 `serde_json::Value` 到 Go 核心协议 |
| 同上，`hooks/src/registry.rs`、`engine/command_runner.rs` | 独立执行运行期、取消/关闭和结果收集 | 采用唯一进程 owner 与显式生命周期，不复制环境捕获构造器、后台 Hook、MCP runner 或 legacy output |
| 同上，`ext/skills/src/sources.rs`、`utils/plugins/src/lib.rs` | 来源 authority、插件身份及命名空间是独立职责 | Hook 来源不与 Skill 权限混用；插件命名空间等待独立 change |

兼容目标是 Claude 风格产品行为与声明清楚的命令 Hook 子集，不是两家全部 settings、脚本输入或输出格式的无修改兼容。

## Goals / Non-Goals

**Goals:** 用一个可审计的门禁同时支持两家 Provider；信任、普通工具批准和 sandbox 各自有效；正常执行、取消、进程崩溃与 Sync 失败均有确定边界；共享代码可证明没有改变现有 Exec、Skill 语义。

**Non-Goals:** 范围见 proposal。设计上额外强调：本次 Hook 只用于检查，不承诺格式化、写缓存、联网、后台服务或任意子孙进程容器；没有独立的 Hook 调度器、数据库、权限配置写回或可执行脚本 artifact。

## Decisions

### 1. 两个显式来源，一份有界冻结快照

只加载用户 `~/.config/easycode/hooks.json` 和启动 workspace 根的 `.easycode/hooks.json`。workspace 沿用应用已确定的工具根，不为 Hooks 另行向祖先或 `.claude` 树扫描。缺失文件表示该来源为空；打开失败、symlink、非法类型/权限或解析失败表示该来源无效。

格式如下；这是 EasyCode 的原生封闭格式，不读取已有凭据配置，也不是完整 Claude settings importer。

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit|Write|Bash",
        "hooks": [
          {
            "id": "protect-generated",
            "type": "command",
            "command": "python3 .easycode/check-change.py",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
```

配置通过既有严格 JSON 入口解析，拒绝重复键、未知字段、非 UTF-8、大小写别名、null、尾随值和错误类型。id 为 1–64 字节小写 ASCII 字母/数字/单连字符、首尾字母数字，在单个来源内唯一。command 非空且最多 8 KiB，不插值工具参数或环境。timeout 单位秒，省略为 5，只接受 1..30 的整数。

matcher 必填，只支持 `*` 或由 `|` 分隔的非空字面名称；Edit/apply_patch 均映射 `fs.patch`，Write 映射 `fs.write`，Bash/exec_command 均映射 `process.exec`。空分支、未知名称、重复名称和正则语法拒绝；`*` 仅表示本切片三项能力。别名归一到能力集合，同一声明即使匹配多个别名也只运行一次。请求的 tool_name 仍是 ready call 的真实 facade，不伪造模型实际调用的名称。

每来源最多 64 KiB、32 条 Hook，合并最多 64 条；每调用最多匹配 8 条，超出时在任何 Hook 启动前返回 `hook_match_limit`。全部匹配项按用户来源、项目来源、配置数组位置执行，不按进程完成时刻排序。来源的对象键和 matcher 能力集合确定性编码，数组顺序是用户声明的执行语义。

source revision 对规范化完整来源内容求摘要；单条 command fingerprint 包含来源种类、id、规范 matcher、command、timeout 及 shell/environment/sandbox 策略身份。绝对 home/workspace、mtime、inode、随机 ID、信任状态不进入内容 revision。信任另行绑定当前 workspace 实例，不能因摘要相同跨工作区复用。

任一已存在来源无效时，快照报告来源与安全 code，所有三项 mutation 调用返回 `hook_config_invalid`；不使用其他合法来源形成部分放行，也不把文件损坏当成无配置。普通聊天、只读工具和 `/hooks` 仍可使用。此次没有旧快照回退：运行期只消费启动时捕获的内容，磁盘后续编辑须重启，不在 getter/View 中检查文件。

### 2. 文件访问与快照的安全边界

在 `internal/extension/localfile` 抽取 Skills 已使用的逐段 no-follow 打开及实际 FD 的常规文件/目录、有界读取骨架；不抽取来源优先级、catalog、parser、digest 或错误码。Skill 与 Hook 都是本 change 的真实消费者，Skill 继续保持现有 no-follow、系统路径别名、数量/bytes 和主文档变更语义。

Hook 配置从同一个打开 FD 校验类型、大小与权限；拒绝特殊文件、源文件或受信任来源目录被 group/other 写入、symlink 及路径替换。仅沿用现有 Darwin 系统别名特例，不通过任意 realpath 放宽规则。读取前后 metadata 和有界全文必须构成一致捕获；检测到变化则标记来源无效，不发布部分快照。关闭 FD 后快照独立拥有 bytes。

审核的是捕获的 command、matcher 和策略，不保证它引用的 workspace 脚本与依赖永远不变。运行期目录 FD/sandbox 仍限制脚本可以访问的范围；UI 必须说明“工作区脚本后续编辑会影响检查”。若要对脚本完整依赖进行内容锁定，需另行设计，不伪称当前信任已覆盖。

### 3. 信任授权与普通工具审批分离

用户和项目 Hook 均默认未信任，不按来源名、项目已打开、Skill 已使用或历史 approval 自动授权。trust key 为当前 Session 实例、workspace 实例、source identity、source revision、Hook id、command fingerprint、shell/environment/sandbox 身份。实例持有授权，重启、resume、关闭后清空，不写配置、不从 Session facts 恢复。

首次匹配时发布独立的 Hook trust request，显示完整命令、来源、matcher、cwd 相对范围、timeout、只读/禁网限制及脚本可变性；只提供“本会话信任此检查”和“拒绝”，默认拒绝。信任一条 Hook 不授权同来源的其他 Hook，也不授权它产生任意文件修改或普通 Exec。拒绝记为当前快照的进程内拒绝状态，后续调用直接拒绝，避免反复弹窗；可在 `/hooks` 对该项重新审查，但不自动重试已拒绝的工具调用。

Runtime 的唯一 owner 按 request/session/thread/turn/invocation/source revision/command fingerprint 关联响应；旧 turn、重复响应、伪造 fingerprint 或已取消请求在任何 grant 和 I/O 之前拒绝。只有接收匹配响应后才安装内存 grant。若随后 started Sync 失败，仍不得启动进程；grant 不表示该执行已发生。

协议使用有真实消费者的 `resolve_hook_trust`、`reset_hook_trust` commands 和 `hook_trust_requested`/`hook_trust_resolved` events，以及只含安全统计的 `hook_activity` 瞬态事件，各有 typed constructor、strict decoder、validator。reset 只在没有活动 turn 时，凭匹配的 Session/workspace/source revision/Hook id/fingerprint 清除该项内存授权或拒绝，不授予新权限；下一真实调用重新触发信任审查，不制造空 invocation。活动等待由 AgentLoop/ChatSession 的现有 owner 管理并接收定向取消；TUI 只路由命令和投影，不持有最终授权事实。用于审查的完整命令是瞬态本地 presentation，不进入 JSONL、诊断日志或 headless 输出。

Headless 不增加 trust flag 或外部信任协议：匹配到 Hook 时，返回 `denied/hook_trust_required`，不启动 Hook、不等待 stdin、不进入普通工具候选/审批；没有匹配 Hook 时，既有 `approval_required` 语义不变。恢复阶段优先按旧 durable facts reconciliation，不运行这套当前配置门禁。

### 4. 工具门禁的唯一插入点

在 `executeApproved` 完成既有 result budget 和取消前检后、进入 Patch/Write/Exec 专属准备前调用 Hook 门禁。无配置或无匹配时不写 Hook facts，也不改变旧路径。匹配时先冻结完整 plan 并 Sync，随后按该 plan 串行检查。

```text
sample/native/usage/ready Sync + finalize
  --> preceding call results Sync
  --> budget + cancellation + configuration checks
  --> hook gate planned Sync
  --> trust check
  --> hook execution started Sync
  --> run + join owned resources
  --> hook execution finished Sync
  --> next planned hook or gate decision Sync
  --> existing candidate preparation
  --> existing policy / tool approval Sync
  --> tool execution started Sync
  --> tool executor --> tool result Sync --> native outputs Sync
```

Hook denied/error 只终止当前调用，后续 independent sibling 仍按原顺序执行；turn 取消、writer poison 或清理不确定才终止整个 turn。blocked result 使用原 invocation/provider-call/wire kind，绝不创建一个模型并未发出的 Hook tool call。Hook 的副作用不能与前一个只读批次、另一个 mutation 或后序工具重叠。

读工具不触发 Hook。尤其 Skill tool、ReadArtifact 和普通 Read 均保持现有 authority 和并发方式；本次 Hook 信任不能扩张 Skills 资源资格。Patch/Write 的既有 session grant 每次都位于门禁之后，不能掩盖 Hook deny。

### 5. JSON stdin 与输出子集

输入为有界 UTF-8 JSON，最多 512 KiB，至少包含 `hook_event_name: PreToolUse`、session/thread/turn identity、tool_use_id、真实 tool_name 和强类型 tool_input。cwd 仅在送往本地受信任子进程的输入中出现，取安全工作区根；不提供 transcript_path、Provider 配置、完整历史、reasoning、环境或凭据。

tool_input 使用 EasyCode 已验证调用的唯一当前语义：Patch 为 file_path 与有序 replacements（old_text/new_text/replace_all/line_based），Write 为 file_path/content，Exec 为 command/workdir/timeout_ms。三种输入通过封闭联合构造；受控外部 JSON 序列化在边界完成，不让 map/any 或 Provider 原始 item 进入共享层。该 shape 明确写入示例，不声称 Claude 所有脚本可以无修改使用。超过 stdin 上限在 plan/进程开始前返回 `hook_input_limit`，不截断请求或读取目标文件补齐输入。

stdout/stderr 各最多捕获 16 KiB。退出 0 且 stdout 只有空白表示继续；非空必须是严格单个 JSON 对象，接受空对象或下列唯一 structured deny：

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Generated files are protected"
  }
}
```

deny reason 为非空 UTF-8、最多 4 KiB。allow/ask、updatedInput、additionalContext、旧 decision/approve shape、async、混合字段和未知字段都报 `hook_output_invalid`，不忽略或变相执行。在清理确认且 I/O 未超限/失败的前提下，退出码 2 为 deny，不解析 stdout JSON，也不依赖 stderr 可解析；其他普通非零退出为 error，阻断当前调用。完整分类优先级为 cleanup 不确定、turn 取消、timeout、输出超限/I/O 失败、exit 2、其他非零，最后才解析 exit 0 的 stdout；同时出现多个原因时按此前后顺序决定唯一状态/code。

**反馈隐私决定：** stderr 和 structured reason 可以来自任意 workspace 内容，无法可靠证明不含用户机密；本切片只向 UI 主线、Session 和模型结果发布宿主生成的安全摘要（来源种类、合法 Hook id、能力及稳定英文 code），不自动发布原始 reason/stdout/stderr，不保存 artifact，也不在错误中回显它们。reason 仅参与有效性校验并在清理后丢弃；UI 明确提示详细脚本输出未收录。阻断反馈仍可指明是哪项检查，模型可以选择替代操作；需要全文反馈时须另行设计显式内容披露契约，不偷偷恢复 stdout 日志。

输入只写 stdin，绝不拼进 shell command。stdout/stderr 分别捕获，不根据竞争到达顺序拼接；超过上限即请求取消和清理，输出截断不能变成成功解析的 JSON 前缀。

### 6. 生命周期复用与最小安全 adapter

新增 `internal/executor/process`，只持有一次进程的 Run/Done、并行 I/O、退出观察、计时、温和终止/强制升级及最终回收。它不导入 tool/provider/session/runtime/tui，不知道 approval、Hook verdict、artifact 或模型预算。

`Launcher.Start(ctx)` 返回窄 Handle（stdin/stdout/stderr、ObserveExit、Wait、Terminate、GroupAlive）；参数由具体不可变 launcher 实例持有，不使用 any 或任意参数字典。显式 Run 接收本次执行；New 只校验内存值，不启动 reader。输入 writer、两个输出 reader 和退出观察都有同一 owner 与等待路径。stdin 写满阻塞、子进程不读输入、提前关闭 stdin、输出管道不关闭都进入同一取消/清理流程；不能让写 stdin 的 goroutine 脱离 Done。

现有 `builtin.ProcessExecution` 改为组合共享 owner：Exec 专属 candidate、进度、512 KiB/流捕获、preview、artifact 写入和结果校验保留在 adapter。Hook adapter 使用同一 owner，但有 16 KiB/流、独立 verdict 和禁止输出保存的策略。不能改变 Exec 的 512 KiB 捕获、256 KiB preview、实际 allowance、非零退出正常结果、artifact 配额和审批 fingerprint。

Darwin 平台启动骨架仍复用现有 FD 目录绑定、helper 早期入口、kqueue 退出观察先注册后放行、Wait 前保持原组身份、Wait 后不再发信号的规则。为共享 launcher 提供中性不可变启动计划；Exec 用现有 workspace 可写/禁网 profile，Hook 用 `darwin-hook-workspace-readonly-offline`，只读工作区及现有系统运行根，禁止网络和文件写入（仅允许 `/dev/null` 必需行为）。不创建 scratch、cache 或真实 home 写入口，HOME/TMPDIR 绑定工作区但仍不可写。

Hook 的 workdir 固定安全 workspace 根；Exec 的相对 workdir 不受影响。最小环境复用既有 allowlist，排除 Provider key、Authorization/Cookie、Session 路径和内部控制变量。shell 为 `/bin/sh` 非 login；配置中的 shell 代码是明确受信任 command，模型参数只来自 stdin。

首批仅 Darwin 运行真实进程。交互运行路径的 unsupported 平台或 sandbox 不可用返回安全 typed error，禁止无 sandbox 回退。能力前检早于交互信任与执行，已知平台不可用时不弹出可执行的信任请求；headless 则先按无交互信任规则拒绝，不进入平台执行前检。

每 Hook wall timeout 为配置值；一条工具调用全部 Hook 共用 30 秒执行预算，信任等待不计入该预算但可被取消。剩余预算不足时不启动下一项。TERM 后 250 ms 升级 KILL，保留现有最多 40 次/25 ms 原组收口观察及独立 1 秒输出排空；这些只限制响应/升级路径，不能放弃仍未 Wait 的直接进程或卡住的 owner。Darwin 进程组不是任意子孙强容器，setsid/double-fork 不受完整树退出保证，sandbox 权限仍继承。

### 7. 失败与取消语义

优先级为：已有 durable result 直接复用；已知 result budget 不足或 turn 已取消不运行 Hook；配置无效/输入或匹配超限不发布可执行计划；headless 匹配项先以 hook_trust_required 拒绝；交互路径平台不可执行不请求信任；其余按冻结 plan、信任和执行顺序处理。headless 信任拒绝仍有计划和同批 gate/result，不产生任何执行 started。

| 情况 | 门禁/工具结果 | 后续行为 |
|---|---|---|
| 无匹配 | 无 Hook facts | 完整走原审批链 |
| 配置无效、输入超限、匹配超限 | error / hook_config_invalid、hook_input_limit、hook_match_limit | 无 Hook/候选/工具 I/O，后序 sibling 可继续 |
| 无交互信任、用户拒绝 | denied / hook_trust_required、hook_trust_denied | gate decision + result 同批 Sync，不启动对应 Hook，不弹普通审批 |
| exit 0 空输出或 `{}` | continue / ok | 检查下一项；全部完成才进入普通审批 |
| exit 2、合法 structured deny | denied / hook_denied | 不运行后续 Hook；不运行目标工具 |
| 其他非零、启动失败 | error / hook_exit_failed、hook_start_failed | 当前调用阻断，其他调用仍可继续 |
| 非法输出、输出超限、读写 stdin/stdout 错误 | error / hook_output_invalid、hook_output_limit、hook_io_failed | 清理完毕后阻断当前调用 |
| 单项或总执行预算超时 | error / hook_timed_out | 终止受控组并等待；无工具 started |
| unsupported 平台或 sandbox 缺失 | error / unsupported_platform、sandbox_unavailable | 零 Hook 命令启动，不提供绕过 |
| turn 取消且清理确认 | cancelled / cancelled | 冻结门禁取消、按顺序取消剩余调用，唯一 turn_failed |
| 无法确认清理或 runner 返回错配结果 | Hook outcome_uncertain；工具 cancelled / hook_outcome_uncertain | poison Runtime，停止后续副作用；健康 journal 收口原生配对，仍持有最终 cleanup owner |
| 任一计划/开始/完成/决定/结果 Sync 无法确认 | session_write failure / poisoned | 停止一切新副作用，不假造 durable success、不重试脚本 |

与 Claude 的两个显式差异是 headless 不隐式信任，以及普通非零/超时/解析错误不会放行。采用此决定是因为本切片定位为可阻断的检查门禁，脚本失效不能使它失去约束却继续修改；不提供隐含 fail-open 开关。

started append 接收前取消，拒绝该 admission，零 runner 调用。append 一旦接收，必须等确定的 Sync 结果；只有成功才恰好调用一次 runner，即使此时 ctx 已取消。runner 接到已取消 ctx 可在 OS 启动前返回 cancelled；“一次 runner 调用”不等于强制创建进程。Started 已 durable 但 Sync 确认丢失时不猜测，poison 并等待恢复。

关闭超时可以先向宿主返回取消/超时信息，但不能关闭最终 Done、释放 workspace/writer lease、允许下一 turn 或丢弃 owner；最终清理完成后再释放。禁止每次等待创建 WaitGroup helper goroutine，禁止锁内执行 I/O、回调或阻塞 channel。

### 8. 四类 durable 事实与合法批次

新增词汇只服务本次真实消费者，不为其他事件预留 kind：

| kind | 最小必需字段与用途 |
|---|---|
| `tool_hook_gate_planned` | invocation ID、event=PreToolUse、capability、plan digest；有序 1..8 条 Hook 引用，每条含 source kind、source revision、Hook id、command fingerprint、timeout；冻结本调用必须检查的顺序 |
| `hook_execution_started` | invocation ID、plan digest、ordinal、HookRunID；表示匹配 runner 已接收，成功 Sync 才能调用 runner |
| `hook_execution_finished` | 同一身份、status、code、exit known/value、timed_out/cancelled、stdout/stderr byte count、cleanup confirmed、origin=execution/recovery；不存输出/输入正文 |
| `tool_hook_gate_decided` | invocation ID、plan digest、continue/denied/error/cancelled/outcome_uncertain、稳定 code、已完成数量及阻断 ordinal（适用时）；表示门禁聚合结论 |

每个字段组合由封闭类型 validator 验证；code 使用有界枚举，不允许任意字符串传播核心控制流。HookRunID 从 invocation+ordinal 确定性构造并严格校验，不持久化 PID。recovery origin 是事实来源，不是格式版本；恢复结果没有伪造 exit，cleanup confirmed=false，明确表达进程结果无法确认。

source revision/fingerprint 与 plan digest 足以关联声明；三者均采用固定 canonical 编码后的 SHA-256 十六进制摘要，并在构造/解码时验证形状。它们不持久化完整 command、配置路径、绝对 cwd、stdin、stdout/stderr、自由文本 reason、信任响应或 grant。计划摘要不是将来可重新执行的脚本，恢复不需要取得源码。

正常路径：plan 单独 batch Sync；每项 started 单独 Sync；中间成功 finished 单独 Sync。最后成功 finished 与 gate continue 在同 batch；首个 executed deny/error/cancelled 的 finished、gate decision 与已冻结 tool result 在同 batch；信任拒绝则 gate decision 与 result 同 batch，不能伪造 started/finished。Tool result 继续复用当前 capability 的 typed error/denied/cancelled 构造和预算元数据。Hook 安全摘要不超过 1 KiB，并进入既有 sample/turn 预算；预算前检失败零 Hook 启动。

任一 call 只有一个 plan、一个 gate decision，每 ordinal 至多一个 started 和 finished，开始下一项必须以前一项 finished=continue 为前提。全成功才能 gate continue；拒绝/错误后的剩余项未执行，不生成虚构 skipped execution。普通 tool approval 只能出现在 gate continue 后，tool started 仍要求普通 allow。后序 call 必须等待当前 result durable，保持既有 exclusive 屏障。

无配置/无匹配时没有 Hook facts 是正常当前行为，不是旧格式兼容。实际存在的每个新 kind 都必须被严格理解；未知字段/kind、重复/错序/错配、伪造 plan digest 或 gate 继续时缺少某个 finished 都是完整语义损坏，不能通过 optional record 或尾部 repair 删除。

### 9. 恢复状态矩阵与不重跑保证

Loader 先验证完整 journal 与 Provider native/checkpoint，再允许尾部 repair；竞争者取得 lease 失败时不得读取、repair 或触碰配置。ToolRecoveryPlan 在原 call slot 持有封闭的 Hook 子状态，只读 JSONL，不反向解析 UI 或 tool preview。

| 最后已 durable 的状态 | 本地补偿；Hook/工具执行次数均为零 |
|---|---|
| ready，无 Hook plan | 沿用当前按 capability 的 before_approval/before_execution 补偿 |
| plan；尚未开始或只完成前缀 | 追加 gate cancelled + tool cancelled，code=session_interrupted_before_hook；不续跑剩余项 |
| Hook started，无 finished | 补 finished(outcome_uncertain, origin=recovery)、gate outcome_uncertain、工具 cancelled/hook_outcome_uncertain 为同 batch；不把目标工具标为已执行 |
| gate continue，无普通 approval | 工具 cancelled/session_interrupted_before_approval |
| gate continue，普通 allow，无 tool started | 工具 cancelled/session_interrupted_before_execution |
| gate deny/error/cancelled，与 result 同批 | 完整复用 frozen result，不再增加 gate 或重新分配预算 |
| tool started，无 result | 沿用现有工具 outcome_uncertain，Hook 已完成事实不重跑 |
| result 有，native outputs 无 | 仅用有序 frozen results 准备/Sync/finalize outputs |
| outputs 已提交，旧 turn 无 terminal | 只追加唯一旧 turn_failed，不追加重复 outputs |

最后成功 finished 与 gate continue、阻断 finished 与 gate/result 的原子批次规则，消除了恢复时从进程 stderr 或当前配置推测聚合结果的需要。不完整 batch 仍只按既有规则修复；只有 checksum/batch 完整的合法活动尾部才能补偿。恢复中再次崩溃时，以已提交的补偿 facts 继续，不重复任何完成记录、result、outputs 或 terminal。

未完成 Hook 的历史进程状态无法由新进程证明；恢复既不按 PID 探测也不宣称已清理，不恢复信任。它记录不确定事实并失败关闭旧 turn；完成离线补偿后，新的 turn 可以在新 owner 和重新审查的当前配置下开始。这不是对旧脚本的重试授权，也不承诺任意外部副作用 exactly-once。

在 app 启动中，把 Hook source Load/install 放在旧 turn reconciliation 之后、ChatSession.Start 之前；恢复旧门禁期间 source loader、trust responder、process launcher、workspace executor、Provider 网络调用计数必须全为零。新 snapshot 仅服务未来调用；加载失败作为诊断/阻断状态安装，不回写已恢复历史。

### 10. 原生历史、缓存与宿主展示

Hook facts 是执行审计，不是 provider history。Provider 只接收原 invocation 对应的冻结 tool result；不增 Hook schema，不将 Hook 输出伪装 assistant/user 消息，不经过 SemanticHistoryView 反向组请求。thinking/signature/encrypted reasoning/phase 完全保持 Provider 私有。

Hook 配置与 trust 状态不进入 system prefix、tool schema 或 skill catalog；因此只有配置变动且尚未调用工具时，请求 bytes 与稳定 segment fingerprint 不变。实际阻断安全摘要改变历史尾部，是可解释的 committed_history 变化。source revision 只用于本地门禁身份，不伪装成新的 prompt catalog segment。

TUI 提供事件 -> matcher/source -> 命令详情的 `/hooks` 浏览；显示 available/untrusted/trusted/denied/invalid/unsupported，支持窄屏滚动与 Esc 返回原草稿。查看不读盘、不运行 Hook、不调 Provider、不写 Session。详情可在空闲时请求 reset 清除该项拒绝或授权，下一真实调用重新审查；列表不能直接建立 trust 或重试工具。自动首次信任请求使用唯一 presentation 和 validator，不存在第二套授权代码。

活动工具主线先显示“检查中/被检查阻断/检查失败”；只有真正 tool started durable 才显示工具 Running。详情展示安全 code、Hook id、来源种类、timeout 和输出字节数，明确详细输出未收录。信任 overlay 与工具 approval overlay 互斥；Esc 拒绝信任，Ctrl+C 定向取消，旧请求不能覆盖 composer 或污染下一 turn。主线与固定 snapshot 不显示 ledger、绝对路径、完整命令输出、Provider wire 或内部 ID。

headless 继续遵守原外部版本和 stdout 单 writer：不外泄 trust/Hook progress/internal RuntimeEvent。print 只输出 durable 最终回答，机器协议只通过原调用结果影响模型和最终状态，不增加未声明的外部 Hook 命令。

### 11. 验证矩阵与完成条件

以下是实施任务的验收依据，测试文件名均为计划路径；已有入口必须保留并增加业务断言，不能以新测试替代或弱化既有断言。

| 验证层/计划入口 | 必须直接断言 |
|---|---|
| `internal/extension/hook/config_test.go`、`output_test.go` | 严格 JSON 负例、全部上限、matcher 别名一次匹配、数组顺序、无效来源全阻断、稳定 revision、getter 深拷贝；exit0/2/其他与 JSON 组合矩阵、stdout 前缀不能误成功 |
| `internal/extension/localfile/*_test.go`、既有 `skill/secure_unix_test.go` | 同 FD 校验、特殊文件、权限、逐段 symlink/TOCTOU、捕获中变化、取消；Skill 来源和 bytes/fingerprint 完全不变 |
| `internal/executor/process/*_test.go` | 不读 stdin/提前关闭、输出洪泛、半 UTF-8、双流、取消、timeout、TERM/KILL、退出观察注册失败、Wait 后无信号、脱离 writer、blocked cleanup；用假时钟和控制 channel，无 sleep/probabilistic race |
| `internal/tool/builtin/hook_process_darwin_test.go`、原 Exec 进程测试 | 真实 Hook 子进程能读 JSON；workspace 写入/网络/外部 home/Session 访问被拒绝；直接进程和受控组清理；Exec 既有写入/预算/artifact/进度 golden 不变 |
| `internal/runtime/hooks_test.go`、`hook_fault_test.go` | 三能力门禁位置、ready/finalizer 前零启动、trust grant 精确身份、deny 高于 session grant、partial args 零调用、读批次不触发、顺序、已 durable result 零重跑 |
| `internal/runtime/hook_shutdown_test.go` | started admission 两侧取消、每个 Hook Sync 故障、runner 错配、清理不确定、shutdown timeout 不释放资源或新 turn；解阻后唯一 Done |
| `internal/session/hook_replay_test.go`、`testdata/current/` | 四种 kind typed 构造/decoder/validator；固定 bytes 覆盖恢复表每个边界及重复恢复；checksum 合法的错序/错配/门禁绕过失败，带 torn tail 仍禁止 repair 合法前缀 |
| `internal/app/hook_resume_test.go`、现有 `session_process_test.go` | 真实子进程 lease 竞争、崩溃释放、占用期间禁止 repair、seq 单调、失败 journal bytes 不变；删除/破坏 hooks.json 后旧门禁恢复零读取零脚本启动 |
| `internal/provider/{openai,anthropic}/hook_test.go`、`internal/app/hooks_e2e_test.go` | continue 后普通审批、deny/exit/timeout/headless 配对；同一 call slot 的 function/custom/tool_result 编码、native 顺序与 opaque 内容 |
| `internal/app/hook_cache_test.go` | uninterrupted/restored 完整 canonical bytes、native item 顺序、footprint 和 segment/request fingerprint 等价；Anthropic cache 配置与 OpenAI full/incremental 分别验证，不混称传输帧相同 |
| `internal/tui/hooks_test.go`、`testdata/hooks_*.golden` | 正常/空/无效/不支持/窄屏、完整命令滚动审查、默认拒绝、重复/过期响应、overlay 按键隔离、Esc 草稿与取消；敏感哨兵不出现在主线/snapshot |
| `internal/architecture`、工程质量门 | 新下层包依赖、生产可变全局/any/构造副作用负例、Hook 占位移除；`make verify` 与 OpenSpec strict 校验 |

所有进程测试使用本地临时工作区、合成凭据哨兵和本地 fixture，不读真实 home、不连接模型服务。网络 sandbox 测试只用本机受控监听及子进程回报，不访问公网。真实手工 TUI smoke 在 Darwin 分别验证 OpenAI/Anthropic 本地模拟服务；无真实 API key 仍能覆盖交互闭环。Linux/Windows 仅验证本次 unsupported 路径，不据交叉编译宣称执行支持。

实施退出条件为上述业务矩阵、全量/race/架构质量门和文档一致性都通过；任何未执行项必须列明原因和风险，不能用已通过的基线测试声称 Hook 已验收。

## Risks / Trade-offs

- [行为子集与 Claude 不完全相同] → 明确列出字面 matcher、规范 tool_input、错误阻断、headless 拒绝和输出隐私策略；拒绝不支持字段，不宣传全兼容。
- [只读 sandbox 使依赖写缓存/联网的脚本失败] → UI 审查前明确限制、提供纯检查示例；不以隐式放宽权限换兼容。
- [信任命令引用的 workspace 脚本变化] → 授权说明准确限定为声明及 sandbox，不把配置摘要误称为整个脚本依赖锁定。
- [脱离进程、取消或故障无法证明完整结束] → 沿用现有受控组保证与唯一 owner，结果不确定时 poison；恢复不做 PID 推断。
- [抽取进程和文件打开骨架导致 Exec/Skill 回归] → 同 change 完成两个真实消费者，保留旧业务策略与既有测试，加入前后 golden/FD 安全/生命周期等价断言，不留双实现。
- [只发布安全摘要降低错误排查便利性] → 展示检查身份、来源、code 与字节统计；原始输出不自动进入模型或 Session，避免以所谓脱敏覆盖任意用户秘密的虚假保证。
- [多条 Hook 增加时延和 journal 数量] → 每调用 8 条、30 秒总执行预算、单项 1..30 秒、有界计划与批次；不会提前并发产生未知副作用。

## Migration Plan

本次仅修改开发期唯一当前契约：四类新 records、typed recovery 子状态与固定 current fixtures 一起更新，不添加 schema_version、历史 decoder、迁移器或 optional record。无 Hook facts 的普通调用仍是合法当前行为，不建立“旧会话模式”。

Apply 时同步 README 的配置样例/安全反馈说明、总体架构第 12 章与当前能力清单、Roadmap 的 P6 切片状态、pitfall 的信任与恢复边界，以及 AGENTS/AST exact allowlist 的 Hook 退出；其他占位保持原状。验收后先同步 delta specs，再按归档流程处理 change，不在提案阶段修改主 specs 或伪报交付。

回滚需连同本次代码、current fixtures 与规范一起回退，不承诺旧二进制读取新增 Hook records；不得原地改写既有 append-only journal。基线 `make verify` 已在前轮通过，但实施完成后必须重新执行全部质量门。


### 实施落点说明

`HookHistoryObserver` 只在 finished 所属 batch 成功 Sync 后更新 app 内存投影；离线 `session.HookHistoryEntry` 从四类事实重建相同统计。历史结果详情附加身份/code/timeout/字节数与输出未收录说明，不改写 frozen model preview，不添加原始输出 artifact。`hook_activity` 仅给交互宿主投影 checking/terminal，严格 decoder 拒绝任意原始输出字段；它既不是持久事实，也不授权工具或恢复执行。

验证矩阵中进程故障与资源 owner 用例落在 `executor/process/owner_test.go`，Hook Sync 故障用例集中于 `runtime/hooks_test.go`，宿主真实 lease/阻塞清理用例位于 `app/hook_host_owner_test.go`，固定恢复窗口位于 `session/testdata/current/hook_*.jsonl`。真实 PTY 入口为 `app/hook_terminal_test.go` 与 `scripts/smoke-hooks.py`；不引入生产依赖或新外部协议。

### 后续已交付能力的规格保留（2026-11-13）

旧 delta 覆盖的 11 项需求遗漏了后续 30 个场景，现以当前主规格完整需求块为基线恢复，并保持原有场景及 ADDED/MODIFIED 归属。共享架构规则同时明确四类已交付同步 command Hook，未实现事件或行为仍须失败关闭；不得再将已交付 Post/Failure/Stop 误称为未实现入口。

第 8 节的四类事实仍说明 Pre 的职责，不恢复 invocation-only 旧 shape。当前共享运行身份使用封闭的 tool_call/turn_stop target；Stop 另有独立计划、决定和交付事实。恢复、原生历史、压缩与自动输入安全点保留后续变更的现行约束，修订不增加 reader、迁移器、版本字段或执行消费者。原 43 项实施任务保持完成，本轮仅修复文档与 delta 的漂移。
