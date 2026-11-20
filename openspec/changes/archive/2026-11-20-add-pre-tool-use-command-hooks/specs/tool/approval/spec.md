## MODIFIED Requirements

### Requirement: Exclusive tools share durable admission invariants

Patch、Write 与 Exec SHALL 共享批准事实、执行接收和结果持久化的不变量。deny MUST 优先于已有 session grant；grant 仅可满足同 capability 的 ask，且 Patch/Write grant 均在 allow_session fact 成功 Sync 后生效。批准来源 MUST 反映实际决策来源。Exec SHALL 保持每次 ask、禁止 session grant，不能因流程共享而扩大权限。

执行 started 接收前已观察到 turn 取消时 MUST 不接收 executor，且产生匹配原调用的取消结果。started 请求接收后 SHALL 按 writer 线性化返回确定提交结果；只有 started 成功 Sync 才能调用 executor。已知取消不得 poison 健康会话，无法确认 durable 结果才进入 poisoned 状态。批准 durable 与执行 durable 仍为不同事实。

Hook 命令信任 SHALL 是独立前置授权，不属于普通工具 approval outcome、decision source 或 session grant。对于命中 PreToolUse 的调用，所有 Hook 的 durable gate continue MUST 先于候选准备和普通审批；Hook deny/error 优先于已有 Patch/Write grant，且不得伪造普通 tool_approval_decided。只有真正进入普通 ask 的调用继续使用既有 approval facts。信任 Hook 不授权文件写入或 Exec，普通 allow 也不能授权 Hook；Exec 仍必须每次普通 ask。

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

#### Scenario: Block a granted Write before preparing a candidate
- **WHEN** 已有 Write session grant，但本次检查 deny
- **THEN** 不读取目标准备候选、不写普通 allow/started、不执行 Write，原 call slot 得到匹配拒绝结果

#### Scenario: Continue to ordinary Exec approval
- **WHEN** Exec 的全部 Hook 已信任且继续
- **THEN** 仍请求普通单次审批；用户拒绝时 Exec 不执行，Hook 信任不能成为 Exec grant

### Requirement: Headless approval fails closed

无交互 approval responder 的 headless 宿主遇到 `ask` SHALL 自动产生 `deny`，使用稳定英文 code `approval_required` 和不含文件正文、命令输出或环境值的安全消息；不得等待 stdin、隐式批准、创建 session grant、启动 Patch/Write/Exec executor或改变 Tool Catalog。拒绝结果 SHALL 保持 Provider call/output pairing，并允许模型在剩余 sample 预算内响应；持续提出必须批准的 Patch、Write 或 Exec 仍受既有 tool-loop 上限约束。

上述失败关闭契约 SHALL 同样适用于 Write 的 create/overwrite，包括本来需要创建父目录的请求。

在配置、输入与匹配数量前检合法的 headless 路径中，若调用命中 Hook，系统 SHALL 在候选准备或普通 ask 前返回 denied/hook_trust_required，零 Hook/工具进程且不等待 stdin；该信任拒绝先于执行平台前检，不因 headless 所在平台改变为可执行信任流程。配置/输入/匹配前检本身失败时使用对应 error code。没有匹配 Hook 且到达普通 ask 的调用 SHALL 继续使用本条 approval_required 行为，不增加 headless 信任 flag、持久规则或外部协议命令。

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

#### Scenario: Reject hook trust in every headless entry
- **WHEN** print、一次性 JSON 或 stream-json 的合法 mutation 调用匹配 Hook
- **THEN** 原 call slot 得到 hook_trust_required，Hook/工具/候选/交互调用次数为零，stdout 不出现内部请求或进度

#### Scenario: Preserve ordinary headless denial
- **WHEN** headless mutation 调用没有匹配 Hook
- **THEN** 原有 approval_required、配对 output 和工具零执行语义不变
