## Why

EasyCode 已能读取、搜索和修改已有文件，但现有 Patch 拒绝创建文件，也无法向空文件写入内容。新增单文件 Write 是 Roadmap P3 的下一项独立切片，可复用已完成的审批、exclusive 调度和 durable Tool Loop，并避开会话列表与命令执行的在途工作。

## What Changes

- 新增共享 `fs.write`，建议 Anthropic 与 OpenAI 均提供 structured `Write(file_path, content)`，支持创建、空文件及已有 UTF-8 文件的整文件覆盖。保留 OpenAI 原生 custom `apply_patch` 的现有局部修改能力；本提案不扩展其 Add/Delete/Move 语法。
- 覆盖要求同一 Runtime 中来自先前已提交工具输出的完整 Read 证据；使用原始 bytes 摘要和文件身份核验，截断读取、同一 sample 尚未回传的 Read 和恢复历史均不能冒充有效证据。
- 候选冻结 create/overwrite 操作、目标、基线、最终 bytes、父目录创建清单和完整有界预览。交互遵循 Claude Code 的 Create/Overwrite 区分、默认 No、拒绝反馈和会话授权；Write 授权与 Patch 授权独立，headless 失败关闭。
- 使用 descriptor-relative 安全路径和同文件系统暂存提交：创建采用禁止覆盖的原子发布，覆盖采用基线重验后的原子替换；缺失父目录链在私有暂存树中准备后整体发布。原子能力不可用时明确失败，不回退到直接覆盖。
- **BREAKING**：扩展唯一当前 Tool input/result、Read executor 返回值、Runtime approval presentation 和 Session typed ledger；旧开发 shape 失败关闭，不保留双 reader。新增 Read 证据只在进程内流转，Session 保存 Write 的必要模型参数与冻结结果，不另存磁盘旧文件快照。
- 补齐双 Provider request/stream/native restore、cache regression、文件提交与清理故障注入、Session 固定 current fixture、TUI snapshot 和端到端验收。

## Capabilities

### New Capabilities

- `tool/file-write`: 单文件创建/覆盖、完整读取证据、候选与冲突、目录暂存、原子发布、结果和失败边界。

### Modified Capabilities

- `tool/read-file`: 完整 Read 产生与实际读取句柄绑定的瞬态观察证据，不改变模型 preview 口径。
- `tool/invocation-loop`: 将 Write 纳入唯一当前 catalog、typed ready/result、exclusive 调度和恢复配对。
- `tool/approval`: Write 的候选批准、独立 session grant、headless 拒绝和不重放恢复。
- `runtime/chat-turn`: 管理完整读取证据的生效/失效、Write durable admission，以及 create/overwrite 的 typed presentation。
- `session/jsonl-store`: Write 当前 input/result 联合、合法状态转换和必要 content 的持久化边界。
- `provider/anthropic-messages-text`: structured Write request、完整参数归并及原生 tool_use/tool_result 持久化恢复。
- `provider/openai-responses-text`: structured Write function call/output 与已有 custom apply_patch 共存。
- `tui/basic-chat`: Create/Overwrite 审批标题、完整内容或 diff、父目录清单、Write 专属授权标签。

## Impact

- 影响 `internal/codec`（纯内存严格对象字段校验）、`internal/tool`、`internal/tool/builtin`、`internal/runtime`、`internal/session`、`internal/provider/{anthropic,openai}`、`internal/protocol`、`internal/tui` 和 `internal/app`；context 只更新实际 catalog 的估算与缓存回归。
- 不引入新 Go module 或版本；终端显示宽度复用依赖图中现有的 `github.com/charmbracelet/x/ansi`，列为直接依赖。macOS/Linux 分别验证禁止覆盖发布和目录 Sync；其他平台或不支持相应文件系统能力的路径明确 unsupported。
- 参考本机 Claude Code sourcemap `a8a678c` 与 Codex `044da98b4`；采纳其产品语义和职责分层，拒绝 symlink 跟随、原子失败后直接写入及 Add 覆盖已有目标等不适合本仓库的行为。
- 不实现命令、后台进程、会话列表、artifact、多文件事务、删除/移动、IDE 内联改写审批或持久权限规则。不修改两个参考工程及 `17ad2` 工作区。
- 与 `add-command-execution` 的公共接入点有文本/契约合并风险，design 明确集成方式；它不是本变更的功能依赖。
- 用户已通过 `$openspec-apply-change add-file-write-tool` 确认本提案，按双 Provider structured Write 的完整契约实施。
