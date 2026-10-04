## ADDED Requirements

### Requirement: Anthropic usage reduces to a validated sample fact

Anthropic reducer SHALL 分别消费 `message_start.message.usage` 与 `message_delta.usage`，并在合法 `message_stop` 前形成最终 raw usage。start 中明确提供的零 SHALL 保存为已知零；delta 中 input、cache creation 和 cache read 的零 MUST NOT 覆盖 start 中已观察到的正值，也不得在没有 start 值时把占位零解释为已知零；delta 中明确提供的 output 零 SHALL 保存为已知零。后续正值 SHALL 按字段更新最终值，缺失字段保持 unknown。

所有已提供 token 值 MUST 为非负整数。负值、超出公共计数范围的值或无法按统一规则归一化的 usage MUST 使当前 stream 以协议错误失败，不提交 native history、`sample_usage` 或成功终态。合法 `message_stop` 产生的 prepared sample SHALL 同时携带保留 raw usage 的 native commit 和对应 normalized usage。

#### Scenario: Preserve start usage across zero delta

- **WHEN** `message_start` 报告 input `12`、cache creation `3`、cache read `7`，随后 `message_delta` 对这些字段报告零并报告 output `9`
- **THEN** 最终 raw usage 保持 `12`、`3`、`7`、`9`
- **THEN** normalized usage 以相同数值产生对应公共指标

#### Scenario: Keep absent Anthropic usage unknown

- **WHEN** 合法 Anthropic sample 没有提供某个可表达 usage 字段
- **THEN** raw usage 与 normalized usage 中对应字段均保持 unknown，而不是已知零

#### Scenario: Preserve an explicit output zero

- **WHEN** `message_delta.usage.output_tokens` 明确为零
- **THEN** output usage 为 `known: 0`

#### Scenario: Reject negative Anthropic usage

- **WHEN** start 或 delta usage 包含负 token 值
- **THEN** Provider 产生 stream protocol failure 并丢弃当前 sample staging
