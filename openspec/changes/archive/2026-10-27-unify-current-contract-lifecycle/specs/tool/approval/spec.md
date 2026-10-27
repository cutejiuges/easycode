## ADDED Requirements

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
