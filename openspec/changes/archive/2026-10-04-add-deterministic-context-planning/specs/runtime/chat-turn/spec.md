## ADDED Requirements

### Requirement: Runtime plans context before Provider side effects

Runtime SHALL 在当前 `turn_started` 已 durable append 并发布、但调用 Provider stream 之前，以当前 Conversation 已提交历史和本轮输入生成上下文计划。规划失败或预算状态为明确 `over_limit` 时，Runtime MUST durable append 当前 turn 的唯一失败边界并发布既有 `turn_failed`，且 MUST NOT 建立 Provider stream 或修改 Provider native history。

明确超限 SHALL 返回稳定英文错误码 `context_limit_exceeded`，错误消息只可包含预算数值和估算状态，不得包含用户输入、历史正文、opaque Provider data、API key 或其他 secret。`not_enforced`、`within_limit` 和 `indeterminate` 预算状态 SHALL 允许既有 Provider 生命周期继续；本变更不得为计划新增 RuntimeEvent kind、Session record 或 headless JSONL 字段。

#### Scenario: Stop a confirmed over-limit turn before networking

- **WHEN** `turn_started` 已 durable 且上下文计划得到 `over_limit`
- **THEN** Runtime durable 记录一次失败边界并发布一次携带 `context_limit_exceeded` 的 `turn_failed`
- **THEN** Provider 不收到 Stream 调用且 Conversation native history 保持不变

#### Scenario: Continue when no window is configured

- **WHEN** 计划完整生成但预算状态为 `not_enforced`
- **THEN** Runtime 使用既有本轮输入启动 Provider stream
- **THEN** 请求继续由 Provider 的原生历史编译，而不是由上下文计划或语义视图重建

#### Scenario: Continue with an indeterminate estimate

- **WHEN** 用户配置了窗口但计划因为必需估算 unknown 而标识 `indeterminate`
- **THEN** Runtime 不把不确定性转换为 `context_limit_exceeded`
- **THEN** Provider 生命周期按既有规则继续

#### Scenario: Close a planning failure durably

- **WHEN** 已 durable 开始的 turn 因非法 footprint、family 不匹配或计划构造错误而失败
- **THEN** Runtime durable 记录唯一 `turn_failed` 边界且不建立 Provider stream
- **THEN** Session append 失败仍遵守既有 poisoned journal 语义

#### Scenario: Keep public event and journal vocabularies unchanged

- **WHEN** 任一计划结果完成或失败
- **THEN** Runtime 只使用既有 `turn_started`、`turn_failed` 及正常 Provider 生命周期事件
- **THEN** journal 不写入 context plan、prompt 正文或新的 record kind

