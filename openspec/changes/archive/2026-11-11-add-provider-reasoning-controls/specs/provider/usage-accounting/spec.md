## MODIFIED Requirements

### Requirement: Provider mappings preserve wire semantics

Anthropic Messages SHALL 将 `input_tokens` 映射为 `input_uncached`、`cache_read_input_tokens` 映射为 `cache_read`、`cache_creation_input_tokens` 映射为 `cache_write`、`output_tokens` 映射为 `output`；`message_delta.usage.output_tokens_details.thinking_tokens` SHALL 在明确报告时映射为 reasoning_output 的 known 值（含零），缺失时为 unknown；不得继续固定为 not_applicable，也不得从 thinking 文本长度、display、模式或预算推断已消耗 token。output 是包含 thinking 的总量，不能把两项相加。

OpenAI Responses SHALL 将 `input_tokens` 解释为包含 cached input 的总输入，将 `input_tokens_details.cached_tokens` 映射为 `cache_read`，并仅在两者均为 `known` 时计算 `input_uncached = input_tokens - cached_tokens`。`cache_write_tokens` 和 `output_tokens_details.reasoning_tokens` SHALL 在 wire 明确提供时分别映射为 `cache_write` 与 `reasoning_output`；字段缺失时必须依据“可表达但缺失”与“wire 不适用”的规则保留状态。`output_tokens` SHALL 映射为 `output`。

归一化 MUST NOT 修改或替代 Provider-native raw usage，也不得从 normalized usage 反向构造后续 Provider 请求。

同一 wire 事件的计量事实 MUST 无歧义；重复的 usage/details 对象或 token 成员不得通过后值覆盖被接受，也不得先改变已归并计量再返回错误。合法的连续事件仍按各自 Provider 观测规则归并，不能把不同事件中的更新误判为同一对象的重复成员。

#### Scenario: Normalize an Anthropic sample

- **WHEN** Anthropic 完成响应明确报告 input、cache read、cache creation 和 output token
- **THEN** normalized usage 在对应四个公共指标中保存相同数值
- **THEN** 未报告 thinking_tokens 时 `reasoning_output` 为 `unknown`，不能补零或解释为不适用

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

#### Scenario: Normalize reported Anthropic thinking
- **WHEN** 最终 message_delta 报告 output 40、thinking_tokens 30
- **THEN** output=40、reasoning_output=30 均为 known，不能累计成 output=70；缺失其他指标仍保留各自状态

#### Scenario: Preserve a reported Anthropic thinking zero
- **WHEN** 合法最终 usage 显式报告 thinking_tokens 为零
- **THEN** reasoning_output 为 known:0；与字段缺失的 unknown 保持区别，即使 display omitted 也不改变

#### Scenario: Reject an impossible Anthropic breakdown
- **WHEN** thinking_tokens 为负数、非整数、溢出、损坏的已知字段，或最终 output 已知且 thinking_tokens 大于 output
- **THEN** Provider sample 以安全协议错误失败，不提交成功 native/usage，不夹取为 output

#### Scenario: Keep a known thinking count with an unknown total
- **WHEN** 合法最终 usage 提供 thinking_tokens 但没有总 output
- **THEN** reasoning_output 保持 known，output 保持 unknown，不制造总量或通过相减推导可见输出

#### Scenario: Reject duplicate usage before normalization
- **WHEN** 两家 wire 在同一事件重复 output_tokens_details、token 成员或总量，试图用合法后值覆盖 null、非法关系或此前计量
- **THEN** 在归并前拒绝整个事件，不能产生成功 sample、更新可信计量或默认选择其中一个值

## ADDED Requirements

### Requirement: Anthropic thinking usage uses one strict current native shape

Anthropic sample 的 raw usage 当前结构 SHALL 增加必需 thinking_tokens 状态字段；known=false 时不得带 value，known=true 时必须带非负整数 value（含零）。wire 缺失只编码 unknown 状态，不省略当前结构字段；缺字段、null、非法状态/数值、重复/未知字段及超限 MUST 按当前 codec 严格拒绝。该字段同样用于 checkpoint generation 与 failed compaction provenance；设置值、display 和用户预算不得存入 raw usage 冒充计量。

计量 SHALL 与对应 native sample 一起 prepare/Sync/finalize；恢复 MUST 交叉核对 raw 与已保存的五项 normalized usage，普通 turn 聚合和独立摘要计量均沿既有规则。实现时直接替换受影响固定 current fixtures，旧四字段开发 shape 失败关闭，不加版本、历史 reader、迁移或 resume 重写。外部五指标字段/版本保持，但 Anthropic reasoning_output 的状态/值按更新契约改变；读取计量不得改变请求、内容顺序或 fingerprint。

#### Scenario: Restore known and unknown thinking facts
- **WHEN** 从固定当前 bytes 恢复已报告零、正值，或 wire 未报告而持久化为 known=false 的样本及摘要 provenance
- **THEN** raw 状态、normalized 指标和归属逐项相等，不重读网络/配置，不推断正文或重跑工具

#### Scenario: Reject the old development usage shape
- **WHEN** native sample 或摘要 provenance 仍使用没有 thinking_tokens 的旧四字段 usage shape
- **THEN** 在 repair/追加之前按当前严格验证失败，原 journal bytes 不变，不补字段重写或使用兼容 reader

#### Scenario: Reject inconsistent saved reasoning usage
- **WHEN** raw thinking_tokens 为 known:30，而关联的 normalized reasoning_output 为 0、unknown 或 not_applicable
- **THEN** 恢复事务性失败，不用其中一份覆盖另一份，不返回部分可继续会话

#### Scenario: Aggregate thinking across samples and summaries
- **WHEN** 普通 turn 包含 known thinking 与缺失 thinking 的多个 durable samples，并另有成功或候选拒绝的摘要
- **THEN** 普通 turn reasoning_output 聚合为 unknown，摘要独立归属 CompactionID，双方不重复计量，不把失败 turn 伪装成功
