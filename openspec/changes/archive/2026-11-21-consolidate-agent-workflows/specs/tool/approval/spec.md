## MODIFIED Requirements

### Requirement: Approval resolution is correlated and exactly once

同一活动 approval request SHALL 由 Runtime 的单一 owner 管理。response MUST 包含匹配的 request ID、turn ID、invocation ID、candidate fingerprint 和 `allow_once`、`allow_session`、`deny` 或 `abort` 中一个当前 request 允许的决定；`deny` MAY 携带合法 UTF-8、去除外围空白后不超过 16 KiB 的反馈，其他决定 MUST NOT 携带反馈或修改后的工具输入。

owner MUST 原子接受至多一个 response。未知、过期、重复、identity 不匹配、fingerprint 不匹配、当前 request 未提供的决定或超限反馈 SHALL 在写 approval fact、生成 result 或启动 executor 前被拒绝。首版宿主不得原地修改候选；用户希望修改时必须拒绝并让模型产生新的 Provider tool call，新调用必须重新 strict decode、validate、准备候选、policy 和 approval。

Stop、interrupt 或 shutdown 先被接纳后，迟到的响应 MUST 被拒绝。审批 owner SHALL 在同一个接纳边界冻结终态、决定来源和是否发布过请求。内部中断的 abort 来源为 `interruption`，经允许选项提交且被接纳的显式 Abort 来源为 `interactive`。后到的取消不能改写已接纳响应，执行仍须满足 started 接收及 Sync 的独立门禁。Resolved 事件仅对应已发布的关联审批请求。

durable allow 后、started 接纳前已确定取消或超时的 Patch、Write、Exec SHALL 写入关联的 `cancelled_before_execution` 结果。未收到 started 的调用不得执行。恢复 MUST 接受相同的已提交事实，同时禁止重放审批或启动执行器。

#### Scenario: Cancel after an allowed approval before started
- **WHEN** Patch、Write 或 Exec 的 allow 已 durable，started 接纳前发生确定取消
- **THEN** journal 保留原审批及匹配的取消结果，执行器零调用，重新加载及回放后可继续会话

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

#### Scenario: Do not install a grant from a late approval
- **WHEN** 活动工具等待审批且 Stop 已被接纳，随后收到原请求的 allow_session
- **THEN** 响应被拒绝，不写 allow_session、不安装 grant、不启动 executor，原调用按取消结果配对收口

#### Scenario: Associate an interactive abort with the offered decisions
- **WHEN** Exec 以关联匹配的显式 Abort 收口，另一次活动 Patch 或 Write 请求未提供 Abort
- **THEN** Exec 的 durable 审批来源为 interactive，Patch 或 Write 的未提供选项被拒绝

#### Scenario: Reuse a grant without a phantom resolution
- **WHEN** 已有同能力 grant 满足本次合法 ask，且没有发布新的交互请求
- **THEN** 该候选仍需 durable approval fact，宿主不收到未匹配请求的 resolved
