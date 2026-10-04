## MODIFIED Requirements

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在发起 Provider 请求前 durable append 当前 `turn_started` 事实；该写入失败时 MUST NOT 建立网络流。Provider 收到显式成功 terminal 后，对应 `provider_native_commit`、`sample_usage` 和 turn 完成边界 MUST 按规定顺序通过同一 Session batch append/Sync，再进入 Conversation 已提交 native history；携带 turn usage 的 `turn_completed` RuntimeEvent 只有在该 lifecycle boundary 已 durable 且 Provider finalizer 成功后才能发布。

Provider 失败、取消、timeout、提前 EOF 或 completed sample usage 非法 SHALL 丢弃 sample staging，并记录不进入 native history的失败边界。任何 append/Sync 错误 MUST 产生一次 `turn_failed` 且不得同时产生 `turn_completed`；durability 状态不确定的 Session MUST 拒绝继续提交新 turn，直到进程重新加载并校验 journal。

#### Scenario: Persist before starting a Provider request

- **WHEN** Session 无法 durable append `turn_started`
- **THEN** Runtime 返回 session 错误且 Provider 不收到请求

#### Scenario: Complete a durably committed sample

- **WHEN** Provider 产生有效 completed terminal 且 Session 成功 Sync 原生 commit、sample usage 与完成边界
- **THEN** Conversation 提交相同 native sample，随后 Runtime 产生一次携带相同 turn usage 的 `turn_completed`

#### Scenario: Fail while persisting a completed sample

- **WHEN** Provider sample 已完整归并但 Session append 或 Sync 失败
- **THEN** Runtime 产生一次 session 类 `turn_failed`，不产生 `turn_completed`，且当前进程拒绝下一 turn
- **THEN** Provider staging 不被当作当前进程中可继续使用的已提交历史，sample usage 也不得单独发布

#### Scenario: Cancel before Provider commit

- **WHEN** turn 在 Provider 成功 terminal 前取消
- **THEN** Runtime durable 记录失败边界，但不写入 `provider_native_commit` 或 `sample_usage`
- **THEN** 下一次请求不包含该 turn 的用户输入或部分 assistant 输出

## ADDED Requirements

### Requirement: Turn completion exposes typed aggregated usage

成功 `turn_completed` RuntimeEvent SHALL 携带强类型的 normalized turn usage；payload 必须遵守统一指标状态和聚合规则，不得包含 Provider wire、raw usage、价格或上下文占用。当前单 sample 文本 turn 的 payload MUST 等于该 durable `sample_usage`；未来同一 turn 的多个 sample MUST 按 sample 顺序聚合后发布。

失败或取消终态 MUST NOT 携带伪造的成功 turn usage。已经 durable 保存的 sample usage 即使发生在未来 turn 的后续工具失败之前，也不得被删除或改写；失败事件是否展示部分 turn usage必须由后续 change 单独定义，本变更不在 `turn_failed` 中暴露它。

#### Scenario: Publish current text turn usage

- **WHEN** 当前单 sample 文本 turn durable 完成
- **THEN** `turn_completed` payload 包含与已提交 `sample_usage` 相同的 normalized usage

#### Scenario: Do not publish usage before durability

- **WHEN** Provider 已完成但包含 usage 的 Session batch 尚未成功 Sync
- **THEN** Runtime 不发布 `turn_completed` 或任何成功 usage 投影

#### Scenario: Keep failed terminal shape unchanged

- **WHEN** 当前 turn 在成功 sample durable 提交前失败或取消
- **THEN** Runtime 只发布既有强类型 `turn_failed`，不附加全零或估算 usage

### Requirement: Runtime revalidates complete prepared sample facts

Runtime SHALL 在构造 Session drafts 前同时复制并重新验证 prepared sample 的 native envelope 与 normalized usage。任何一部分为零值、缺失、已 finalized、复制失败或违反语义不变量时，Runtime MUST 以 stream protocol failure 收口，并不得 append native commit、sample usage 或成功终态。

#### Scenario: Reject invalid normalized usage before append

- **WHEN** completed terminal 的 prepared sample 含非法 metric state 或计数关系
- **THEN** Runtime durable 记录失败边界且不执行 Provider finalizer
- **THEN** journal 中不存在该 sample 的成功记录
