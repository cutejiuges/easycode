## Why

EasyCode 已能通过双 Provider 读取、搜索并批准后修改代码，但修改后无法自行运行构建或测试，因而尚未形成可验证的 coding loop。当前 Tool Loop、durable Session、approval 与 exclusive 调度已具备承载进程副作用的前置边界，现在适合补齐前台命令执行，并同时固定“交互体验趋近 Claude Code、执行能力兼顾 Claude Code 与 Codex 优势”的产品原则。

## What Changes

- 新增共享 `process.exec` 能力：Anthropic 以 structured `Bash` facade 暴露，OpenAI 以 structured `exec_command` facade 暴露，两者使用同一强类型语义、policy、sandbox plan、executor 和结果模型，同时保留各自 Provider-native call/output。
- 提供非交互、前台、completion-only 的命令生命周期：冻结 shell、workspace 相对 cwd、最小环境、timeout、sandbox 与输出预算；并发排空 stdout/stderr，取消或超时时终止整个进程组，由唯一 owner 等待进程退出、输出关闭和清理完成。
- 默认对每个合法命令请求交互批准；approval 绑定规范化候选与 fingerprint，先 durable 决策和 execution admission，再启动进程。headless 遇到 `ask` 失败关闭，崩溃恢复不重新批准或执行，started 无 result 的调用收口为 `outcome_uncertain`。
- 增加有界命令进度与结果投影。TUI 采用 Claude Code 风格的紧凑命令、工作目录、Waiting/Running、近期输出、耗时、输出规模及终态展示；内部采用 Codex 风格的 owner、sandbox 分层和确定性 head/tail 输出预算。无交互 headless 保持 approval 失败关闭且不输出内部进度。
- 新增 Ctrl+O 命令详情视图：复用审批中的完整命令，只在 command terminal 投影完整有界结果，支持滚动和 resize；不把完整结果加入 progress，也不扩展 headless JSONL。
- 将 `process.exec` 作为 `exclusive` 屏障接入现有有序多 sample Tool Loop，并将稳定 facade 定义纳入 Tool Catalog、上下文估算和 cache fingerprint 回归。
- 增加双 Provider golden/native restore、Session current fixture、进程组取消/超时/阻塞输出故障注入、TUI snapshot、headless 和“读取 -> Patch -> 执行测试 -> 总结”端到端测试。
- 本变更不提供后台进程、自动后台化、PTY/TTY、`write_stdin`、可复用 process handle、通用 artifact store、持久命令 allowlist、任意 shell 选择或未经验证平台的宽松降级；这些能力需要后续独立 change。

## Capabilities

### New Capabilities

- `tool/command-execution`: 定义双 Provider 前台命令 facade、强类型输入/结果、approval、sandbox、进程 owner、取消/超时、输出预算和安全展示契约。

### Modified Capabilities

- `tool/invocation-loop`: 将 `process.exec` 加入唯一当前 catalog、typed ready-call/result 联合、exclusive 调度、durable ledger、恢复和 Provider output 配对。
- `tool/approval`: 将不可变候选批准从 Patch 扩展到命令，定义命令专用展示、决策范围、headless 失败关闭和恢复行为。
- `runtime/chat-turn`: 增加命令执行的 durable admission、进度事件、取消后 cleanup ownership 和 exclusive 屏障语义。
- `session/jsonl-store`: 将 Exec typed input/result metadata 纳入唯一当前 tool records、strict decoder、replay validator 与 current fixture。
- `provider/anthropic-messages-text`: 增加 structured `Bash` request、stream decode、native persistence/restore 和 `tool_result` 编码。
- `provider/openai-responses-text`: 增加 structured `exec_command` request、stream decode、native persistence/restore 和 `function_call_output` 编码。
- `context/planning`: 将新增稳定 facade 定义纳入 tool catalog 估算、cache segment 与 fingerprint 变更诊断。
- `tui/basic-chat`: 增加 Claude Code 风格命令审批、排队/运行进度、有界输出和终态呈现。

## Impact

- 影响 `internal/tool`、`internal/tool/builtin`、`internal/runtime`、`internal/session`、`internal/provider/{anthropic,openai}`、`internal/protocol`、`internal/tui`、`internal/headless` 与 `internal/app` 的强类型契约和装配。
- Session envelope/payload、RuntimeEvent、Tool schema/result 和 Provider native item 均发生当前契约变更；稳定发布前直接替换唯一 current fixture，不引入 V2/Legacy reader 或兼容分支。
- Tool Catalog canonical bytes、context token 估算、cache segment 与 stable-prefix fingerprint 将因真实新增 facade 而变化，必须更新双 Provider golden/cache regression。
- Darwin 首版需要可确定测试的进程组与系统 sandbox 能力；不能提供等价强制边界的平台必须明确 unsupported 并失败关闭。
- Darwin cleanup保证限于直接子进程、原进程组及owner持有的输出资源；主动通过setsid/setpgid脱离的进程不属于可保证回收的集合。系统sandbox继续约束其文件/网络能力，但不是进程树容器。审批明确说明不支持daemon化，检测到输出或进程组无法收口时返回outcome_uncertain并停止后续执行。
- 退出观察与最终回收分离：进程组信号和探测期间保留未回收leader身份，全部信号操作结束后才执行唯一Wait；回收后禁止再使用旧PID/PGID，避免编号复用导致误杀无关进程。
- 不新增外部 Go 依赖，优先使用标准库进程原语与现有架构边界；参考工程仅作为只读行为和设计依据。
