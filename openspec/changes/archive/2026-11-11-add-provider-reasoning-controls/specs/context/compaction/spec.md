## ADDED Requirements

### Requirement: Summary requests preserve frozen reasoning with independent output budgets

手动及自动摘要 SHALL 保留会话冻结的 Anthropic thinking/display/effort 或 OpenAI reasoning.effort/summary，与普通采样使用相同的显式/省略语义，不因用途变为 summary 删除字段、夹取 budget 或降低 effort。摘要保持当前模型、原生有效窗口、项目/Skill、稳定工具目录和缓存策略；tool choice 必须禁用，输出上限只取 compaction_output_tokens，不使用普通 max_output_tokens。策略一致只维护本地请求语义，不保证服务端缓存命中或摘要必然产生正文。

摘要预检、估算、实际发送和 receipt MUST 来自同一摘要计划，其输入证据包含独有指导后缀、属性包含工具禁用和真实摘要上限；不从普通请求再次生成 evidence。准备/预检/失败不安装普通 baseline，completed Sync 后才切换有效窗口；摘要 generation 的推理与 raw usage 按当前 provenance 保存，但不发布普通推理正文或外部推理事件。

#### Scenario: Preserve adaptive display and effort during compaction
- **WHEN** 普通配置为 adaptive/display summarized/high，摘要上限 4096 且预检合法
- **THEN** 摘要保留该 thinking/display/effort，并发送独立 max_tokens 4096；成功后下一普通请求仍使用原普通上限

#### Scenario: Preserve OpenAI reasoning controls
- **WHEN** 普通请求 effort high、summary detailed、上限 32768，自动摘要上限 4096
- **THEN** 摘要保留同一 reasoning，只用 4096 上限与 tool choice none；不能删 summary 或降 effort 追求成功

#### Scenario: Keep a failed summary inert
- **WHEN** 摘要取消、截断或完成 Sync 结果不确定
- **THEN** 分别沿既有取消/失败/poisoned 契约收口，不安装部分摘要、不修改冻结策略、不重跑工具；取得可信终态但候选失败时单独保存真实摘要 usage

### Requirement: Manual thinking conflicts reject compaction before admission

Anthropic enabled 模式的 budget_tokens MUST 严格小于实际摘要输出上限。相等或更大时 SHALL 在 started、网络和工具副作用前返回 compaction_budget_exceeded；错误不回显正文或 secret，不自动改变任何设置，也不把合法普通配置判为无效。无论是否配置 context window，此约束均适用；普通输入和窗口估算仍按既有独立预算规则。

自动摘要预检遇到同类预算拒绝 SHALL 使用既有预算失败/来源抑制路径，不误分类为 stream_protocol_error；不写 started/failed 事实。旧窗口仍能通过普通预检时继续同一任务，不能通过时沿既有失败及输入返还语义收口，不忽略预算或重复尝试同一来源。相容性检查不等于有可见摘要额度保证；服务端仍可用完全部输出，届时按真实终态失败。

#### Scenario: Reject a manual budget above the summary cap
- **WHEN** 普通 budget 8192、上限 16384、摘要上限 4096，用户请求压缩
- **THEN** 在任何 started/网络之前返回 compaction_budget_exceeded，旧窗口、普通策略和 journal bytes 不变

#### Scenario: Enforce the strict manual boundary
- **WHEN** manual budget 为 8192，摘要上限分别是 8192 和 8193，其他预检约束满足
- **THEN** 前者本地拒绝，后者允许发起请求但不承诺可见摘要足够；两者均不夹取 budget

#### Scenario: Continue safely after an automatic preflight rejection
- **WHEN** 工具 outputs durable 后自动摘要因 manual budget 不相容而被拒绝，旧窗口仍可请求
- **THEN** 不写摘要事实、不改模式，按同一冻结设置继续下一 sample，相同来源按既有机制抑制，不重跑工具

#### Scenario: Reject a blocked continuation without losing input
- **WHEN** 自动摘要预检拒绝且旧窗口的普通预检也失败
- **THEN** 沿既有任务失败/返还规则保留未提交输入，不发送超预算请求、不尝试降低 thinking 或另一个摘要分支

### Requirement: Tool durability is not an inference mode transition boundary

完整工具结果成功 Sync SHALL 只表示本地安全点，不证明 Provider assistant turn 已结束。普通多 sample 工具循环、同轮追加及工具后的摘要 MUST 保持冻结 thinking/display/effort；原 thinking/signature/redacted、OpenAI opaque items 和工具配对仍按原序回放。成功摘要建立新的有效窗口，但不能成为自动改变设置或重放工具的许可。

#### Scenario: Compact between a tool result and the final answer
- **WHEN** 已有 thinking、工具调用和 durable 工具结果，尚未获得最终无工具回答时触发摘要
- **THEN** 摘要保留冻结策略且全部来源完整，成功后原任务继续；不能仅因为工具已完成就关闭 thinking 或删原生块
