## ADDED Requirements

### Requirement: Configure a bounded compaction output budget

JSON SHALL 接受可选顶层整数 compaction_output_tokens，省略时默认 4096，显式值必须在 1..65536；类型错误、null、负数、零、重复和溢出 MUST 返回 invalid_configuration。字段只由 JSON 配置，不新增环境变量或按模型猜测值。无 context_window_tokens 时允许该字段但不推断容量；显式窗口和显式摘要输出值存在时，其与安全余量之和 MUST 严格小于窗口。省略字段产生的默认值不适配小窗口时只拒绝压缩预检，不使原本合法普通配置启动失败。该配置不得修改普通 reserved_output_tokens 或普通请求输出策略。

#### Scenario: Preserve default startup
- **WHEN** 既有合法配置没有 compaction_output_tokens
- **THEN** 普通启动语义不变，摘要默认上限为 4096

#### Scenario: Apply an explicit summary budget
- **WHEN** 配置合法摘要输出上限 8192
- **THEN** 两家摘要请求分别发送对应实际 wire 上限 8192，摘要预检使用同一值

#### Scenario: Reject impossible explicit budgets
- **WHEN** 显式摘要输出值不在范围内或加安全余量大于等于显式窗口
- **THEN** 在 Provider 请求前拒绝配置，错误不包含配置正文或 secret

#### Scenario: Keep a small ordinary window usable
- **WHEN** 窗口为 2048、普通预留合法且未配置摘要输出上限
- **THEN** 普通启动成功，手动压缩以预算不足拒绝且不写 started
