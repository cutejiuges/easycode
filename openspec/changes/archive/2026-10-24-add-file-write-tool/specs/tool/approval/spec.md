## MODIFIED Requirements

### Requirement: Policy decisions are typed and bound to an immutable candidate

Tool Policy SHALL 对完整、已验证且已经 durable 的调用输出封闭的 `allow`、`ask` 或 `deny` 决策。`ask` SHALL 只在系统已从安全句柄生成可执行候选后产生；approval request MUST 携带合法 request、session、thread、turn、invocation 和 Provider call identity、capability、稳定英文 reason、规范化 workspace 相对目标、候选 fingerprint、受控展示数据及内核允许的有序 decisions。

候选 fingerprint MUST 由 capability、规范化 typed input、目标范围、base evidence 和将要写入的确定 bytes 派生，不得包含时间、随机 approval ID、UI 状态或 Provider wire。宿主只能展示 request 中的 decisions，不得自行添加持久规则、扩大路径或把批准解释为绕过 workspace/file safety。

当前 `fs.read`、`fs.glob` 与 `fs.grep` SHALL 继续自动允许；每个 `fs.patch` 或 `fs.write` 候选 SHALL 在没有匹配进程内 session grant 时返回 `ask`。非法调用 SHALL `deny`，且不得产生 approval request。

Write 候选 SHALL 绑定 create/overwrite、完整最终内容、实际文件/祖先证据、目录清单与权限计划；完整 Read 是覆盖的独立前置条件。只有候选合法才能 ask；Write presentation 必须与其操作分支一致。

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

#### Scenario: Ask to create or overwrite
- **WHEN** 一个合法 Write 候选没有匹配的 Write grant
- **THEN** 仅在完整展示已准备后请求批准，并明确 create 或 overwrite 与目录范围
- **THEN** Write session allow 不能绕过完整 Read、preview 预算或执行前重验

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

无交互 approval responder 的 headless 宿主遇到 `ask` SHALL 自动产生 `deny`，使用稳定英文 code `approval_required` 和不含文件正文的安全消息；不得等待 stdin、隐式批准、创建 session grant、启动 Patch/Write executor或改变 Tool Catalog。拒绝结果 SHALL 保持 Provider call/output pairing，并允许模型在剩余 sample 预算内响应；持续提出必须批准的 patch 仍受既有 tool-loop 上限约束。

上述失败关闭契约 SHALL 同样适用于 Write 的 create/overwrite，包括本来需要创建父目录的请求。

#### Scenario: Reject a patch in print mode
- **WHEN** `--print` 或非交互 stream 模式中的模型请求一个需要批准的 patch
- **THEN** 系统生成 `approval_required` denied result且文件 bytes 不变
- **THEN** 进程不会无限等待用户输入

#### Scenario: Deny headless Write without filesystem changes
- **WHEN** --print、--json 或 stream-json 的合法 Write 候选需要 ask
- **THEN** 返回 approval_required 并保持原生配对，不等待 stdin，不创建目标、父目录或暂存资源

### Requirement: Cancellation and recovery never replay approval or patch effects

活动 approval 等待 SHALL 响应定向 interrupt、Runtime shutdown 和 turn context 取消，并由同一 owner 完成唯一 resolution与清理。取消不得遗留等待 goroutine、可再次消费的 response channel 或跨 turn 的活动 request。

恢复 SHALL NOT 重新显示旧 approval、恢复进程内 grant或自动执行任何 Patch 或 Write。只有 ready、没有 approval fact 的调用关闭为 `cancelled/session_interrupted_before_approval`；已有 allow fact但没有 started 的调用关闭为 `cancelled/session_interrupted_before_execution`；started 而没有 result 的 Patch/Write关闭为 `outcome_uncertain`。每种补偿都必须生成匹配原 Provider call ID 的 output并以 `turn_failed` 关闭旧 turn。

恢复 SHALL 清空 Write grant 和进程内 Read 证据，不检查旧候选文件或清理暂存树。

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

#### Scenario: Recover Write before execution
- **WHEN** Write 的 allow 已 durable 而 started 缺失
- **THEN** 本地补偿 session_interrupted_before_execution，不重询问、不创建目录或执行写入
