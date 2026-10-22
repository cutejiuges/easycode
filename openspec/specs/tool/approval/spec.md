# tool/approval Specification

## Purpose

定义工具调用在产生副作用前的权限决策、交互批准、durable 事实和恢复边界，使交互式宿主能够安全询问用户，而 headless 与异常恢复路径始终失败关闭。

## Requirements

### Requirement: Policy decisions are typed and bound to an immutable candidate

Tool Policy SHALL 对完整、已验证且已经 durable 的调用输出封闭的 `allow`、`ask` 或 `deny` 决策。`ask` SHALL 只在系统已从安全句柄生成可执行候选后产生；approval request MUST 携带合法 request、session、thread、turn、invocation 和 Provider call identity、capability、稳定英文 reason、规范化 workspace 相对目标、候选 fingerprint、受控展示数据及内核允许的有序 decisions。

候选 fingerprint MUST 由 capability、规范化 typed input、目标范围、base evidence 和将要写入的确定 bytes 派生，不得包含时间、随机 approval ID、UI 状态或 Provider wire。宿主只能展示 request 中的 decisions，不得自行添加持久规则、扩大路径或把批准解释为绕过 workspace/file safety。

当前 `fs.read`、`fs.glob` 与 `fs.grep` SHALL 继续自动允许；每个 `fs.patch` 候选 SHALL 在没有匹配进程内 session grant 时返回 `ask`。非法调用 SHALL `deny`，且不得产生 approval request。

#### Scenario: Ask for a validated patch
- **WHEN** 一个已 durable 的 `fs.patch` 调用已从安全打开的目标文件生成 base evidence、最终 bytes 和 diff，且没有 session grant
- **THEN** Runtime 发布包含相同 invocation ID、规范化目标、candidate fingerprint 和允许决定集合的 typed approval request
- **THEN** 文件内容保持不变且没有 `tool_execution_started`

#### Scenario: Keep read-only calls allowed
- **WHEN** Policy 评估合法的 Read、Glob 或 Grep ready call
- **THEN** 决策为 `allow` 且不创建 approval request

#### Scenario: Deny an invalid candidate
- **WHEN** patch 目标、base evidence、结果 bytes 或 diff 无法通过验证
- **THEN** 决策失败关闭且系统不发布可批准的请求

### Requirement: Approval resolution is correlated and exactly once

同一活动 approval request SHALL 由 Runtime 的单一 owner 管理。response MUST 包含匹配的 request ID、turn ID、invocation ID、candidate fingerprint 和 `allow_once`、`allow_session`、`deny` 或 `abort` 中一个当前 request 允许的决定；`deny` MAY 携带合法 UTF-8、去除外围空白后不超过 16 KiB 的反馈，其他决定 MUST NOT 携带反馈或修改后的工具输入。

owner MUST 原子接受至多一个 response。未知、过期、重复、identity 不匹配、fingerprint 不匹配、当前 request 未提供的决定或超限反馈 SHALL 在写 approval fact、生成 result 或启动 executor 前被拒绝。首版宿主不得原地修改候选；用户希望修改时必须拒绝并让模型产生新的 Provider tool call，新调用必须重新 strict decode、validate、准备候选、policy 和 approval。

#### Scenario: Resolve an approval once
- **WHEN** 活动 request 首次收到 identity、fingerprint 和决定均匹配的 `allow_once`
- **THEN** owner 接受该 response 并关闭 request 的等待状态
- **THEN** 后续重复 response 不产生第二个 approval fact或执行

#### Scenario: Reject a stale candidate response
- **WHEN** response 携带旧 candidate fingerprint 或其他 invocation 的 identity
- **THEN** owner 返回稳定英文拒绝且文件、Session ledger 和 session grant 均不改变

#### Scenario: Ask the model for a revised patch
- **WHEN** 用户选择 `deny` 并附加修改建议
- **THEN** 当前 patch 不执行，反馈进入匹配调用的有界拒绝结果
- **THEN** 模型只有通过新的 tool call 才能提交修改后的候选，并接受完整重验

### Requirement: Final approval facts precede execution or denial output

每个进入 `ask` 的调用在终态 result 前 SHALL 具有唯一 `tool_approval_decided` fact。fact MUST 保存 approval request ID、invocation ID、candidate fingerprint、`allow_once`、`allow_session`、`allow_from_session`、`deny` 或 `abort` outcome、decision source、可选有界拒绝反馈以及不含绝对路径或文件正文的安全 metadata。

`allow_once`、`allow_session` 或 `allow_from_session` fact 必须成功 append/Sync 后才可尝试 `tool_execution_started`。`deny` fact 与对应 `tool_call_result(status=denied)` MUST 在同一 batch durable，且不得出现 execution started。`abort` SHALL 使当前调用及同一 turn 尚未执行的调用按既有取消规则获得配对 output，并以唯一取消终态关闭 turn。

approval request RuntimeEvent、焦点、倒计时、按键和 diff 展开状态属于瞬态 UI，不得进入 JSONL。批准事实不替代路径、文件类型、base evidence 或平台 capability 检查。

#### Scenario: Persist allow before starting execution
- **WHEN** 用户批准一个 patch
- **THEN** 匹配 candidate fingerprint 的 allow fact 先成功 Sync
- **THEN** 只有随后 `tool_execution_started` 也成功 Sync 时 executor 才能被调用

#### Scenario: Persist denial without execution
- **WHEN** 用户拒绝一个 patch
- **THEN** deny fact 与 denied result 原子 durable，并生成匹配 Provider call ID 的拒绝 output
- **THEN** executor 调用次数为零且模型可以在下一 sample 选择替代方案

#### Scenario: Approval does not bypass file safety
- **WHEN** 用户批准后目标文件或平台能力无法通过执行前安全验证
- **THEN** 调用返回 typed error或conflict result且不修改 workspace 外对象

### Requirement: Session grants are narrow and process-local

`allow_session` SHALL 只建立当前 Runtime 实例持有的 `fs.patch` workspace-wide grant；grant MUST NOT 写入用户或项目配置，不得跨进程、重启、resume、Provider 或 workspace 传播。匹配 grant 的后续 patch SHALL 跳过交互，但仍需生成新的候选、重新验证路径和 base evidence，并在副作用前 durable 写入 `allow_from_session` fact及该候选 fingerprint。

grant registry MUST 由 Runtime 实例持有，不得使用包级可变状态。关闭 Runtime、切换 workspace 或重新恢复 Session SHALL 清空 grant。

#### Scenario: Reuse a grant in the same Runtime
- **WHEN** 用户选择允许当前 Runtime 会话中的全部 workspace patch，随后模型提交另一个合法 patch
- **THEN** 第二个调用不发布 approval request，但仍在 started 前写入自己的 `allow_from_session` fact

#### Scenario: Do not restore a session grant
- **WHEN** 带有历史 `allow_session` fact 的 thread 在新进程中 resume
- **THEN** 首个新 patch 仍需要交互批准或按 headless 规则拒绝

### Requirement: Headless approval fails closed

无交互 approval responder 的 headless 宿主遇到 `ask` SHALL 自动产生 `deny`，使用稳定英文 code `approval_required` 和不含文件正文的安全消息；不得等待 stdin、隐式批准、创建 session grant、启动 patch executor或改变 Tool Catalog。拒绝结果 SHALL 保持 Provider call/output pairing，并允许模型在剩余 sample 预算内响应；持续提出必须批准的 patch 仍受既有 tool-loop 上限约束。

#### Scenario: Reject a patch in print mode
- **WHEN** `--print` 或非交互 stream 模式中的模型请求一个需要批准的 patch
- **THEN** 系统生成 `approval_required` denied result且文件 bytes 不变
- **THEN** 进程不会无限等待用户输入

### Requirement: Cancellation and recovery never replay approval or patch effects

活动 approval 等待 SHALL 响应定向 interrupt、Runtime shutdown 和 turn context 取消，并由同一 owner 完成唯一 resolution与清理。取消不得遗留等待 goroutine、可再次消费的 response channel 或跨 turn 的活动 request。

恢复 SHALL NOT 重新显示旧 approval、恢复进程内 grant或自动执行任何 patch。只有 ready、没有 approval fact 的调用关闭为 `cancelled/session_interrupted_before_approval`；已有 allow fact但没有 started 的调用关闭为 `cancelled/session_interrupted_before_execution`；started 而没有 result 的 patch关闭为 `outcome_uncertain`。每种补偿都必须生成匹配原 Provider call ID 的 output并以 `turn_failed` 关闭旧 turn。

#### Scenario: Interrupt a pending approval
- **WHEN** 用户在 approval overlay 活跃时定向中断当前 turn
- **THEN** request 只被解决一次、patch executor 不运行、全部 durable calls 获得配对取消 output
- **THEN** TUI 返回可输入状态且下一 turn 不会消费旧 response

#### Scenario: Recover before an approval decision
- **WHEN** 进程在 patch ready durable 后、approval fact 前退出
- **THEN** resume 不弹出 approval且不调用 patch executor
- **THEN** 调用以 `session_interrupted_before_approval` 补偿并关闭旧 turn

#### Scenario: Recover after approval but before execution
- **WHEN** journal 包含 allow fact但没有 execution started
- **THEN** resume 不复用批准执行 patch，而以 `session_interrupted_before_execution` 关闭调用
