# provider/usage-accounting Specification

## Purpose

定义跨 Provider 的 sample 级 token usage 事实、字段状态、归一化映射和聚合代数，使持久化、恢复和宿主输出不把缺失值伪装成零，也不混淆计费用量与上下文占用。

## Requirements

### Requirement: Normalized sample usage has explicit metric states

每个成功 Provider sample SHALL 产生一个 normalized usage，固定包含 `input_uncached`、`cache_read`、`cache_write`、`output` 和 `reasoning_output` 五个指标。每个指标 MUST 显式处于 `known`、`unknown` 或 `not_applicable` 之一：`known` MUST 携带非负整数值，另外两种状态 MUST NOT 携带数值。

`known: 0` SHALL 表示 Provider 明确报告或可无损推导出的零；`unknown` SHALL 表示该 wire 能表达该指标但本次没有足够信息；`not_applicable` SHALL 仅表示该 Provider wire 对该 sample 没有这个独立概念。构造、解码和聚合 MUST 拒绝负数、非法状态、状态与数值不匹配及整数溢出，不得通过默认零修复。

#### Scenario: Preserve a reported zero

- **WHEN** Provider 明确报告某个可表达指标为零
- **THEN** normalized metric 为 `known` 且值为 `0`

#### Scenario: Preserve a missing metric

- **WHEN** Provider wire 支持某指标但本次响应没有提供推导该指标所需的数据
- **THEN** normalized metric 为 `unknown` 且不携带数值

#### Scenario: Mark an inapplicable metric

- **WHEN** Provider wire 对本次 sample 不存在可独立报告的某指标概念
- **THEN** normalized metric 为 `not_applicable` 且不携带数值

#### Scenario: Reject an invalid metric

- **WHEN** normalized metric 包含负值、未知状态，或在非 `known` 状态下携带数值
- **THEN** sample usage 校验失败且不得作为成功事实发布或持久化

### Requirement: Provider mappings preserve wire semantics

Anthropic Messages SHALL 将 `input_tokens` 映射为 `input_uncached`、`cache_read_input_tokens` 映射为 `cache_read`、`cache_creation_input_tokens` 映射为 `cache_write`、`output_tokens` 映射为 `output`；不能从 Anthropic usage 单独识别的 reasoning output SHALL 为 `not_applicable`。

OpenAI Responses SHALL 将 `input_tokens` 解释为包含 cached input 的总输入，将 `input_tokens_details.cached_tokens` 映射为 `cache_read`，并仅在两者均为 `known` 时计算 `input_uncached = input_tokens - cached_tokens`。`cache_write_tokens` 和 `output_tokens_details.reasoning_tokens` SHALL 在 wire 明确提供时分别映射为 `cache_write` 与 `reasoning_output`；字段缺失时必须依据“可表达但缺失”与“wire 不适用”的规则保留状态。`output_tokens` SHALL 映射为 `output`。

归一化 MUST NOT 修改或替代 Provider-native raw usage，也不得从 normalized usage 反向构造后续 Provider 请求。

#### Scenario: Normalize an Anthropic sample

- **WHEN** Anthropic 完成响应明确报告 input、cache read、cache creation 和 output token
- **THEN** normalized usage 在对应四个公共指标中保存相同数值
- **THEN** `reasoning_output` 为 `not_applicable`

#### Scenario: Normalize an OpenAI sample

- **WHEN** OpenAI 完成响应报告总 input 为 `120`、cached input 为 `40`、output 为 `30`、reasoning output 为 `10`
- **THEN** normalized usage 的 `input_uncached=80`、`cache_read=40`、`output=30`、`reasoning_output=10` 且均为 `known`

#### Scenario: Reject impossible OpenAI cache usage

- **WHEN** OpenAI 响应报告 cached input 大于总 input
- **THEN** Provider 将该 completed response 视为协议错误，不截断为零，也不产生成功 sample

#### Scenario: Keep raw and normalized usage separate

- **WHEN** 一个 sample 同时包含 Provider-specific raw usage 和 normalized usage
- **THEN** Provider-native codec 保留 raw 字段，共享消费者只读取 normalized usage
- **THEN** 后续请求仍只从 Provider-native history 编译

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

### Requirement: Usage accounting excludes cost and context occupancy

Normalized usage SHALL 只描述 Provider 对已完成 sample 报告的 token 用量，不得包含价格、币种、成本、限额、context window 容量、压缩阈值或估算 token。Usage 数据 MUST NOT 直接驱动 compaction；需要上下文占用时必须由独立的 ContextPlanner/token estimator 能力定义。

#### Scenario: Report usage without pricing

- **WHEN** sample usage 成功归一化并发布给宿主
- **THEN** usage 不包含单价、总成本或 Provider 订阅信息

#### Scenario: Avoid treating billed usage as live context

- **WHEN** Session 恢复了历史 sample usage
- **THEN** 系统不使用这些累计值直接判定当前请求是否需要 compaction
