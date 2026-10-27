# tool/approval Specification

## Purpose

定义工具调用在产生副作用前的权限决策、交互批准、durable 事实和恢复边界，使交互式宿主能够安全询问用户，而 headless 与异常恢复路径始终失败关闭。

## Requirements

### Requirement: Policy decisions are typed and bound to an immutable candidate

Tool Policy SHALL 对完整、已验证且已经 durable 的调用输出封闭的 `allow`、`ask` 或 `deny` 决策。`ask` SHALL 只在系统已从安全句柄或冻结执行计划生成可执行候选后产生；approval request MUST 携带合法 request、session、thread、turn、invocation 和 Provider call identity、capability、稳定英文 reason、规范化 workspace 相对目标、候选 fingerprint、受控展示数据及内核允许的有序 decisions。

Patch候选 fingerprint MUST 由 capability、规范化 typed input、目标范围、base evidence 和将要写入的确定 bytes 派生；Exec候选 fingerprint MUST由capability、规范化command、相对workdir、timeout、shell/environment/sandbox profile和输出预算派生。两者均不得包含时间、随机 approval ID、UI 状态、Provider wire、绝对workspace或环境实际值。宿主只能展示 request 中的 decisions，不得自行添加持久规则、扩大路径、修改命令或把批准解释为绕过安全边界。

当前 `fs.read`、`fs.glob` 与 `fs.grep` SHALL 继续自动允许；每个 `fs.patch` 或 `fs.write` 候选 SHALL 在没有匹配进程内 session grant 时返回 `ask`；每个 `process.exec`候选 SHALL始终返回`ask`且只允许`allow_once`、`deny`、`abort`。非法调用 SHALL `deny`，且不得产生 approval request。

Write 候选 SHALL 绑定 create/overwrite、完整最终内容、实际文件/祖先证据、目录清单与权限计划；完整 Read 是覆盖的独立前置条件。只有候选合法才能 ask；Write presentation 必须与其操作分支一致。Exec 候选 SHALL 绑定规范化 command、相对 workdir、timeout 与 sandbox profile；其 presentation 必须与候选一致。

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

#### Scenario: Ask to create or overwrite
- **WHEN** 一个合法 Write 候选没有匹配的 Write grant
- **THEN** 仅在完整展示已准备后请求批准，并明确 create 或 overwrite 与目录范围
- **THEN** Write session allow 不能绕过完整 Read、preview 预算或执行前重验

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

`allow_session` SHALL 只建立当前 Runtime 实例持有的、以 capability 区分的 workspace-wide grant：Patch 请求只授予 `fs.patch`，Write 请求只授予 `fs.write`；grant MUST NOT 写入用户或项目配置，不得跨进程、重启、resume、Provider 或 workspace 传播。匹配 grant 的后续同 capability 调用 SHALL 跳过交互，但仍需生成新的候选、重新验证路径和 base evidence，并在副作用前 durable 写入 `allow_from_session` fact及该候选 fingerprint。

grant registry MUST 由 Runtime 实例持有，不得使用包级可变状态。关闭 Runtime、切换 workspace 或重新恢复 Session SHALL 清空 grant。

Write 与 Patch grants MUST NOT 互相授权或扩大到命令；Write grant SHALL 仍要求每次完整 Read/候选重验和 durable allow_from_session。grant 只在 allow_session fact 确定 durable 后生效。

#### Scenario: Reuse a grant in the same Runtime
- **WHEN** 用户选择允许当前 Runtime 会话中的全部 workspace patch，随后模型提交另一个合法 patch
- **THEN** 第二个调用不发布 approval request，但仍在 started 前写入自己的 `allow_from_session` fact

#### Scenario: Do not restore a session grant
- **WHEN** 带有历史 `allow_session` fact 的 thread 在新进程中 resume
- **THEN** 首个新 patch 仍需要交互批准或按 headless 规则拒绝

#### Scenario: Isolate Write permission from Patch
- **WHEN** Runtime 已有 Patch session grant，模型首次提出 Write
- **THEN** Write 仍请求批准或按 headless 规则拒绝；反向亦然

#### Scenario: Do not grant before durability
- **WHEN** Write allow_session decision 的 Sync 失败
- **THEN** 不安装 Write grant，且不准入 executor

### Requirement: Headless approval fails closed

无交互 approval responder 的 headless 宿主遇到 `ask` SHALL 自动产生 `deny`，使用稳定英文 code `approval_required` 和不含文件正文、命令输出或环境值的安全消息；不得等待 stdin、隐式批准、创建 session grant、启动 Patch/Write/Exec executor或改变 Tool Catalog。拒绝结果 SHALL 保持 Provider call/output pairing，并允许模型在剩余 sample 预算内响应；持续提出必须批准的 Patch、Write 或 Exec 仍受既有 tool-loop 上限约束。

上述失败关闭契约 SHALL 同样适用于 Write 的 create/overwrite，包括本来需要创建父目录的请求。

#### Scenario: Reject a patch in print mode
- **WHEN** `--print` 或非交互 stream 模式中的模型请求一个需要批准的 patch
- **THEN** 系统生成 `approval_required` denied result且文件 bytes 不变
- **THEN** 进程不会无限等待用户输入

#### Scenario: Deny headless Write without filesystem changes
- **WHEN** --print、--json 或 stream-json 的合法 Write 候选需要 ask
- **THEN** 返回 approval_required 并保持原生配对，不等待 stdin，不创建目标、父目录或暂存资源

#### Scenario: Reject a command in headless mode
- **WHEN** `--print`、一次性`--json`或stream-json中的模型请求Exec
- **THEN** 系统生成`approval_required` denied result且子进程启动次数为零
- **THEN** stdout不出现交互提示或未版本化命令输出

### Requirement: Cancellation and recovery never replay approval or tool effects

活动 approval 等待 SHALL 响应定向 interrupt、Runtime shutdown 和 turn context 取消，并由同一 owner 完成唯一 resolution与清理。取消不得遗留等待 goroutine、可再次消费的 response channel 或跨 turn 的活动 request。

恢复 SHALL NOT 重新显示旧 approval、恢复进程内 grant或自动执行任何 Patch、Write 或 Exec。只有 ready、没有 approval fact 的调用关闭为 `cancelled/session_interrupted_before_approval`；已有 allow fact但没有 started 的调用关闭为 `cancelled/session_interrupted_before_execution`；started 而没有 result 的 Patch/Write/Exec关闭为 `outcome_uncertain`。每种补偿都必须生成匹配原 Provider call ID 的 output并以 `turn_failed` 关闭旧 turn。

恢复 SHALL 清空 Write grant 和进程内 Read 证据，不检查旧候选文件、不查找进程 PID或清理暂存树。
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
- **THEN** resume 不复用批准执行 patch，而以 `session_interrupted_before_execution` 关闭调用

#### Scenario: Recover Write before execution
- **WHEN** Write 的 allow 已 durable 而 started 缺失
- **THEN** 本地补偿 session_interrupted_before_execution，不重询问、不创建目录或执行写入
- **THEN** resume 不复用批准执行副作用，而以 `session_interrupted_before_execution` 关闭调用

#### Scenario: Recover after command acceptance
- **WHEN** journal包含Exec execution started但没有result
- **THEN** resume不启动命令并以`outcome_uncertain`关闭调用和旧turn

### Requirement: Exclusive tools share durable admission invariants

Patch、Write 与 Exec SHALL 共享批准事实、执行接收和结果持久化的不变量。deny MUST 优先于已有 session grant；grant 仅可满足同 capability 的 ask，且 Patch/Write grant 均在 allow_session fact 成功 Sync 后生效。批准来源 MUST 反映实际决策来源。Exec SHALL 保持每次 ask、禁止 session grant，不能因流程共享而扩大权限。

执行 started 接收前已观察到 turn 取消时 MUST 不接收 executor，且产生匹配原调用的取消结果。started 请求接收后 SHALL 按 writer 线性化返回确定提交结果；只有 started 成功 Sync 才能调用 executor。已知取消不得 poison 健康会话，无法确认 durable 结果才进入 poisoned 状态。批准 durable 与执行 durable 仍为不同事实。

#### Scenario: Fail Patch grant persistence
- **WHEN** 用户选择 Patch allow_session，但批准事实 Sync 失败
- **THEN** grant 不安装，executor 不调用，会话进入既定 durable failure 收口

#### Scenario: Deny despite an existing grant
- **WHEN** 同 capability 已有 grant，但当前合法候选的 policy 决策为 deny
- **THEN** 不复用 grant 执行，记录与实际来源一致的拒绝事实和配对结果

#### Scenario: Cancel between approval and execution admission
- **WHEN** Patch、Write 或 Exec 的 allow 已 durable，而 started 请求被接收前 turn 取消
- **THEN** executor 调用次数为零，无 started fact，存在配对 cancelled result 和唯一 turn terminal
- **THEN** 已知取消不会把健康 journal 标记为 poisoned

#### Scenario: Cancel after started admission
- **WHEN** started 请求已被 writer 接收，随后调用方取消
- **THEN** owner 等待确定提交结果，只有成功 Sync 后才交给 executor 返回确定取消或执行结果
- **THEN** 不重试执行，也不把调用方超时当成未知提交结果

#### Scenario: Preserve capability restrictions
- **WHEN** 共用流程执行 Write 覆盖或 Exec 命令
- **THEN** Write 仍需有效完整 Read 证据与候选重验，Exec 仍需单次批准和平台 sandbox
- **THEN** Patch/Write grant 互不授权且均不能授权 Exec

### Requirement: Invalid post-execution results are uncertain outcomes

副作用 executor 返回的结果 SHALL 与原调用的 invocation ID、Provider call ID、capability、wire kind 和候选相关 metadata 一致，并通过当前结果 validator。执行已被接收后，非法或错配结果 MUST 归一为原调用的 `outcome_uncertain`，使用 `invalid_tool_result`，停止后续副作用并 poison Runtime；不得降级为可继续执行的普通错误。

健康 journal SHALL 保存该不确定结果并按模型调用顺序完成剩余调用的取消配对、原生 outputs 和唯一失败 terminal；journal 自身已失败时 MUST 保留 poisoned 状态，不声称已 durable 成功，也不得重试工具。

#### Scenario: Reject a result for a different call
- **WHEN** Patch、Write 或 Exec executor 返回结构合法但 Provider call ID、wire kind 或 capability 不匹配的结果
- **THEN** 当前调用记为 outcome_uncertain/invalid_tool_result，后续副作用执行次数为零
- **THEN** 在 journal 健康时，结果按原调用身份和顺序完成持久化与 Provider pairing

#### Scenario: Reject malformed metadata after execution
- **WHEN** executor 返回零值结果或不符合候选的成功 metadata
- **THEN** Runtime 不继续下一工具或 sample，不把失败转换为普通可重试 error
