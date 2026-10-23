## MODIFIED Requirements

### Requirement: Policy decisions are typed and bound to an immutable candidate

Tool Policy SHALL 对完整、已验证且已经 durable 的调用输出封闭的 `allow`、`ask` 或 `deny` 决策。`ask` SHALL 只在系统已从安全句柄或冻结执行计划生成可执行候选后产生；approval request MUST 携带合法 request、session、thread、turn、invocation 和 Provider call identity、capability、稳定英文 reason、规范化 workspace 相对目标、候选 fingerprint、受控展示数据及内核允许的有序 decisions。

Patch候选 fingerprint MUST 由 capability、规范化 typed input、目标范围、base evidence 和将要写入的确定 bytes 派生；Exec候选 fingerprint MUST由capability、规范化command、相对workdir、timeout、shell/environment/sandbox profile和输出预算派生。两者均不得包含时间、随机 approval ID、UI 状态、Provider wire、绝对workspace或环境实际值。宿主只能展示 request 中的 decisions，不得自行添加持久规则、扩大路径、修改命令或把批准解释为绕过安全边界。

当前 `fs.read`、`fs.glob` 与 `fs.grep` SHALL 继续自动允许；每个 `fs.patch` 候选 SHALL 在没有匹配进程内 session grant 时返回 `ask`；每个 `process.exec`候选 SHALL始终返回`ask`且只允许`allow_once`、`deny`、`abort`。非法调用 SHALL `deny`，且不得产生 approval request。

#### Scenario: Ask for a validated patch
- **WHEN** 一个已 durable 的 `fs.patch` 调用已从安全打开的目标文件生成 base evidence、最终 bytes 和 diff，且没有 session grant
- **THEN** Runtime 发布包含相同 invocation ID、规范化目标、candidate fingerprint 和允许决定集合的 typed approval request
- **THEN** 文件内容保持不变且没有 `tool_execution_started`

#### Scenario: Ask for a frozen command
- **WHEN** 一个已durable的`process.exec`调用已冻结command、相对workdir、timeout、environment和sandbox profile
- **THEN** Runtime发布绑定完整命令展示和candidate fingerprint的typed approval request
- **THEN** 没有`tool_execution_started`且不存在子进程

#### Scenario: Keep read-only calls allowed
- **WHEN** Policy 评估合法的 Read、Glob 或 Grep ready call
- **THEN** 决策为 `allow` 且不创建 approval request

#### Scenario: Deny an invalid candidate
- **WHEN** patch或exec候选无法通过输入、目标、base evidence、sandbox或预算验证
- **THEN** 决策失败关闭且系统不发布可批准的请求

### Requirement: Headless approval fails closed

无交互 approval responder 的 headless 宿主遇到 `ask` SHALL 自动产生 `deny`，使用稳定英文 code `approval_required` 和不含文件正文、命令输出或环境值的安全消息；不得等待 stdin、隐式批准、创建 session grant、启动 patch/exec executor或改变 Tool Catalog。拒绝结果 SHALL 保持 Provider call/output pairing，并允许模型在剩余 sample 预算内响应；持续提出必须批准的 Patch 或 Exec 仍受既有 tool-loop 上限约束。

#### Scenario: Reject a patch in print mode
- **WHEN** `--print` 或非交互 stream 模式中的模型请求一个需要批准的 patch
- **THEN** 系统生成 `approval_required` denied result且文件 bytes 不变
- **THEN** 进程不会无限等待用户输入

#### Scenario: Reject a command in headless mode
- **WHEN** `--print`、一次性`--json`或stream-json中的模型请求Exec
- **THEN** 系统生成`approval_required` denied result且子进程启动次数为零
- **THEN** stdout不出现交互提示或未版本化命令输出

### Requirement: Cancellation and recovery never replay approval or patch effects

活动 approval 等待 SHALL 响应定向 interrupt、Runtime shutdown 和 turn context 取消，并由同一 owner 完成唯一 resolution与清理。取消不得遗留等待 goroutine、可再次消费的 response channel 或跨 turn 的活动 request。

恢复 SHALL NOT 重新显示旧 approval、恢复进程内 grant或自动执行任何 Patch/Exec。只有 ready、没有 approval fact 的副作用调用关闭为 `cancelled/session_interrupted_before_approval`；已有 allow fact但没有 started 的调用关闭为 `cancelled/session_interrupted_before_execution`；started 而没有 result 的Patch或Exec关闭为 `outcome_uncertain`。每种补偿都必须生成匹配原 Provider call ID 的 output并以 `turn_failed` 关闭旧 turn。

#### Scenario: Interrupt a pending approval
- **WHEN** 用户在 Patch 或 Exec approval overlay 活跃时定向中断当前 turn
- **THEN** request 只被解决一次、对应executor不运行、全部 durable calls 获得配对取消 output
- **THEN** TUI 仅在清理完成后返回可输入状态且下一 turn 不会消费旧 response

#### Scenario: Recover before an approval decision
- **WHEN** 进程在 Patch 或 Exec ready durable 后、approval fact 前退出
- **THEN** resume 不弹出 approval且不调用对应executor
- **THEN** 调用以 `session_interrupted_before_approval` 补偿并关闭旧 turn

#### Scenario: Recover after approval but before execution
- **WHEN** journal 包含 allow fact但没有 execution started
- **THEN** resume 不复用批准执行副作用，而以 `session_interrupted_before_execution` 关闭调用

#### Scenario: Recover after command acceptance
- **WHEN** journal包含Exec execution started但没有result
- **THEN** resume不启动命令并以`outcome_uncertain`关闭调用和旧turn
