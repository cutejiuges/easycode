## ADDED Requirements

### Requirement: Configure an explicit context token budget

JSON 配置 SHALL 接受可选的顶层整数 `context_window_tokens`、`reserved_output_tokens` 和 `context_safety_margin_tokens`。`context_window_tokens` 存在时 MUST 大于零；两个预留字段省略时 SHALL 默认为零，存在时 MUST 为非负整数，且其无溢出之和 MUST 严格小于 context window。预留字段在 context window 缺失时 MUST 被拒绝，避免产生看似生效但无法执行的配置。

这三个字段 SHALL 只由显式 JSON 配置提供；当前切片不新增环境变量覆盖。系统 MUST NOT 根据 Provider 或 model 名称填充缺失窗口。类型错误、负数、超出受支持整数范围、算术溢出或非法字段组合 MUST 返回稳定英文 `invalid_configuration`，且不得启动 Provider 请求。

#### Scenario: Load a complete context budget

- **WHEN** JSON 配置包含 `context_window_tokens: 200000`、`reserved_output_tokens: 20000` 和 `context_safety_margin_tokens: 4096`
- **THEN** 合并后的配置保留三个显式数值供上下文规划使用
- **THEN** 既有 Provider 字段合并和校验语义保持不变

#### Scenario: Default omitted reserves to zero

- **WHEN** JSON 配置只增加合法的 `context_window_tokens`
- **THEN** 输出预留和安全余量均为零
- **THEN** 系统不从 model 名称或远程服务补全预留值

#### Scenario: Keep an omitted window unenforced

- **WHEN** JSON 配置和环境变量启动方式均未提供 context window 或预留字段
- **THEN** 配置加载保持兼容且上下文预算为未启用
- **THEN** 现有环境变量启动不需要新增变量

#### Scenario: Reject reserves without a window

- **WHEN** JSON 配置包含 `reserved_output_tokens` 或 `context_safety_margin_tokens` 但没有 `context_window_tokens`
- **THEN** 应用返回英文 `invalid_configuration`
- **THEN** 应用不创建 Provider 或发起网络请求

#### Scenario: Reject an impossible or overflowing budget

- **WHEN** 任一预算字段为负数、非整数、超出受支持范围，或两个预留之和大于等于窗口或发生整数溢出
- **THEN** 应用返回英文 `invalid_configuration`
- **THEN** 错误不包含 API key、prompt、历史正文或完整敏感 URL 凭据

