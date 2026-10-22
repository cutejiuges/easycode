## Why

EasyCode 已具备可恢复的 Read/Glob/Grep Tool Loop，但权限策略仍只有固定 allow/deny，TUI 也无法在副作用前展示并等待用户确认；直接继续增加写工具会使文件修改缺少可审计的批准线性化点。当前应以一个真实文件 patch 消费者建立 approval、durable ledger、双 Provider patch wire 和 Claude Code 风格 diff 交互的完整闭环，同时保持 Codex 式“批准不等于 sandbox/文件安全能力”的内核边界。

## What Changes

- 将 Tool Policy 扩展为强类型 `allow/ask/deny` 决策，并增加由 Runtime 单 owner 管理的 approval request/response 生命周期、候选 fingerprint、可用决定集合和 exactly-once resolution。
- 增加 `fs.patch` 能力：Anthropic 暴露 structured `Edit` facade，OpenAI 暴露 freeform custom `apply_patch` facade；两家分别归并原生输入，但只在完整 strict decode 和语义验证后产生共享 typed patch call。
- 首版 patch 只修改 workspace 内已存在的常规 UTF-8 文件，不创建、删除或移动文件；执行前从实际打开的安全句柄生成 base evidence 和有界 diff，批准后再次核验 evidence，并通过同目录临时文件和原子替换完成写入。
- 增加 Claude Code 风格文件审批 overlay：显示相对路径、完整有界 diff、风险说明和 `Yes`、`Yes, allow all edits during this session`、`No`；聚焦 `No` 时可用 Tab 附加“应该怎么做”的反馈，Esc/Ctrl+C 中断当前 turn，overlay 期间输入不得泄漏到聊天 composer。
- 会话级允许只在当前进程 Runtime 实例内生效，不跨重启恢复；每个具体调用仍在副作用前写入 durable approval fact。内核决定 UI 可呈现的选项，TUI 不自行扩展权限范围。
- 显式区分拒绝和中断：拒绝产生无副作用的 typed tool result并允许模型在下一 sample 调整方案；中断关闭当前未完成调用并以唯一取消终态结束 turn。
- 对 headless 宿主中的 `ask` 失败关闭，返回稳定拒绝结果，不隐式批准或扩大文件系统能力。
- 扩展 Session 当前唯一 record vocabulary、ReplayPlanner 和 current fixtures，使正常路径遵循 `ready -> approval_decided -> execution_started -> result`；拒绝不产生 execution started，恢复绝不重新询问或自动执行 patch。
- 扩展有序调度，在现有 `parallel_read` 批次之间加入真实 `exclusive` patch 屏障；并发完成、批准等待和拒绝均不得改变 Provider call/output 顺序。
- 更新双 Provider request/stream/native/result golden、cache fingerprint、uninterrupted/restored 等价、Session 崩溃点、headless fail-closed、文件安全、TUI 固定尺寸 snapshot 和端到端测试，并同步 Tool 架构、Roadmap 与 pitfall 文档。
- 本变更不实现整文件 Write、文件创建/删除/移动、Bash/进程、网络权限、持久 user/project permission rules、hooks/远程审批、MCP/Subagent、IDE diff 编辑、通用 artifact store或完整跨平台 sandbox。

## Capabilities

### New Capabilities

- `tool/approval`: 定义 allow/ask/deny、可用决定、durable approval fact、会话级授权、拒绝/中断、headless 失败关闭和恢复语义。
- `tool/file-patch`: 定义双 Provider patch facade、typed patch plan、base evidence、有界 diff、安全原子替换、exclusive 调度和确定性结果。

### Modified Capabilities

- `tool/invocation-loop`: 将 `fs.patch` 加入当前可执行 Catalog 与 typed call/result 联合，并在既有 ordered multi-sample loop 中加入审批阶段和 exclusive 屏障。
- `provider/anthropic-messages-text`: 增加 structured `Edit` request、stream decode 和匹配的 `tool_result` wire。
- `provider/openai-responses-text`: 增加 custom freeform `apply_patch` request、stream decode 和匹配的 custom tool output wire。
- `provider/native-history-persistence`: 原生 history、restore 和 output preparation 增加两家 patch item，同时保持 opaque 数据、顺序和恢复请求等价。
- `runtime/chat-turn`: 增加 approval 等待、拒绝、中断和 patch 执行在单 turn 生命周期中的 durable 顺序及取消所有权。
- `runtime/input-control`: 增加关联到活动 turn 和 approval request 的强类型 resolve command，并保持单 owner admission、去重和关闭语义。
- `session/jsonl-store`: 增加唯一当前 approval record 与 patch typed payload，收紧 ledger 状态机和当前 fixture。
- `session/resume`: 定义 approval/patch 中断尾部的本地补偿、零重放和 Provider pairing 修复。
- `tui/basic-chat`: 增加 Claude Code 风格 diff/permission overlay、按键隔离、拒绝反馈输入和固定尺寸投影行为。

## Impact

- 主要涉及 `internal/tool`、`internal/tool/builtin`、`internal/provider/anthropic`、`internal/provider/openai`、`internal/runtime`、`internal/protocol`、`internal/session`、`internal/tui` 与 `internal/app` 的组合根。
- Tool Catalog schema/fingerprint、Provider request golden、Provider-native 当前 payload、Session 当前 envelope/payload canary 和 `testdata/current` 将直接替换；产品尚未稳定发布，不保留旧开发 shape reader或兼容分支。
- RuntimeCommand/Event、Tool schema/result、Session record 和双 Provider native item 均发生契约变化，必须在同一 change 中完成实现、验证和文档同步。
- 不新增第三方运行时依赖；文件安全继续建立在平台专属 handle-relative 实现上，不以 approval 替代 workspace containment、symlink 或文件类型检查。
