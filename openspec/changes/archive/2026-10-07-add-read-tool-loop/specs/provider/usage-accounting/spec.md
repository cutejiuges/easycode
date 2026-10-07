## MODIFIED Requirements

### Requirement: Usage aggregation is deterministic and state-aware

turn usage SHALL 按 sample 顺序逐指标聚合。对于同一指标，任一 sample 为 `unknown` 时结果 MUST 为 `unknown`；否则所有 `known` 值 SHALL 以溢出检查相加，`not_applicable` 不贡献数值；全部 sample 均为 `not_applicable` 时结果 SHALL 为 `not_applicable`。聚合空 sample 集 MUST 被拒绝，聚合不得改变输入对象。

单 sample 文本 turn 的 turn usage SHALL 与该 sample normalized usage 完全相同。Tool Loop 的多 sample turn MUST 聚合从首次 sample 到最终无工具 sample 的全部 durable `sample_usage`，不得用最后一次 sample 覆盖此前用量。若 turn 在一个或多个 sample durable 后失败，既有 sample usage MUST 保留为 Session 事实且不得被删除、改写或伪造成成功 `turn_completed`；当前失败事件仍不对宿主暴露部分 turn usage。

#### Scenario: Aggregate known samples
- **WHEN** 一个 turn 的两个 sample 对同一指标分别报告 `known: 10` 和 `known: 4`
- **THEN** turn 指标为 `known: 14`

#### Scenario: Propagate unknown through aggregation
- **WHEN** 一个 turn 的任一 sample 对某指标为 `unknown`
- **THEN** turn 的该指标为 `unknown`，即使其他 sample 给出已知值

#### Scenario: Aggregate known and inapplicable samples
- **WHEN** 某指标在一个 sample 中为 `known: 7`，在另一个 sample 中为 `not_applicable`
- **THEN** turn 的该指标为 `known: 7`

#### Scenario: Keep an entirely inapplicable aggregate
- **WHEN** 一个 turn 的所有 sample 对某指标均为 `not_applicable`
- **THEN** turn 的该指标为 `not_applicable`

#### Scenario: Aggregate a complete tool turn
- **WHEN** 一个 Read turn 依次 durable 两个含工具调用的 samples 和一个最终文本 sample
- **THEN** `turn_completed` usage 按顺序聚合全部三个 sample，而不是只使用最终 sample

#### Scenario: Preserve usage before a later failure
- **WHEN** 首个 sample usage 已 durable，但 Read 或后续 Provider sample失败
- **THEN** Session 保留该 sample usage且 turn 以失败终态收口，不发布伪造的成功聚合 usage

