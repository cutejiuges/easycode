## ADDED Requirements

### Requirement: Configure explicit native reasoning settings

JSON SHALL 接受可选字段 `anthropic_thinking`、`anthropic_effort`、`openai_reasoning_effort` 和 `openai_reasoning_summary`。anthropic_thinking MUST 是封闭判别对象：disabled 只允许 type；adaptive 允许 type 及可选 display；enabled 要求 type/budget_tokens 并允许可选 display。budget_tokens MUST 为 1024 至普通有效输出上限减一的整数，其他分支不得携带。display SHALL 仅接受 summarized/omitted；省略时不发字段，disabled 携带 display 必须拒绝，不支持 updates 或隐式补 mode。

anthropic_effort SHALL 仅接受 low/medium/high/xhigh/max；openai_reasoning_effort SHALL 仅接受 none/minimal/low/medium/high/xhigh/max；openai_reasoning_summary SHALL 仅接受 auto/concise/detailed。省略表示不发送对应字段，不等于关闭或某个固定强度。xhigh/max 的 Anthropic effort 与显式 disabled thinking MUST 被拒绝。

字段名、判别值和枚举 MUST 大小写精确匹配；未知、重复、跨分支字段、null、空串、错误类型、非整数、非法 Unicode、尾随内容及溢出 MUST 返回安全英文 invalid_configuration。配置只从既有 JSON 入口读取，不增加环境变量、CLI 推理开关或任意参数透传。

#### Scenario: Configure adaptive thinking
- **WHEN** Anthropic 配置包含 adaptive thinking 和 high effort
- **THEN** 配置成功且不产生 budget_tokens，不按模型名称改写设置

#### Scenario: Validate manual budget boundaries
- **WHEN** Anthropic 普通有效输出上限为 4096，manual budget 分别为 1023、1024、4095、4096
- **THEN** 仅 1024 与 4095 成功，不自动夹取或提高输出上限

#### Scenario: Distinguish omitted and disabled thinking
- **WHEN** 分别省略 anthropic_thinking 与显式设置 disabled
- **THEN** 前者不发送 thinking，后者发送明确 disabled，两者不被归一化为同一 wire

#### Scenario: Reject cross-branch and malformed settings
- **WHEN** adaptive/disabled 对象带 budget_tokens，enabled 缺 budget_tokens，或对象含 null、重复键、未知字段
- **THEN** 返回 invalid_configuration，零 Provider 请求和零 Session 创建

#### Scenario: Reject incompatible disabled effort
- **WHEN** Anthropic 显式 disabled 与 xhigh 或 max effort 同时存在
- **THEN** 配置在资源装配前失败，不静默降低 effort

#### Scenario: Request summarized or omitted thinking explicitly
- **WHEN** adaptive 或 enabled 分支显式设置 display 为 summarized 或 omitted
- **THEN** 合法配置精确保留该值，display 省略时不补默认值；正文是否返回仍由服务端决定

#### Scenario: Reject malformed display before opening resources
- **WHEN** disabled 带 display，或 display 是 updates、null、大小写别名、错误类型或重复字段
- **THEN** 返回 invalid_configuration，零请求与零 Session 创建，不删除 display 后重试

### Requirement: Configure bounded ordinary output independently from compaction

JSON SHALL 接受可选整数 `max_output_tokens`，范围 1..65536；该范围是本地实现边界，不表示远端模型上限。普通 Anthropic 请求 SHALL 映射为 max_tokens，省略时沿用 4096；OpenAI SHALL 映射为 max_output_tokens，省略时继续不发送该字段。不得修改 compaction_output_tokens 或从输出预留反向生成 wire 上限。

普通 manual budget 在普通有效上限内即为合法普通配置；与摘要独立上限不相容时 SHALL 由 context/compaction 在操作预检时拒绝，不将该问题误报为普通启动配置无效，不修改任何配置值。

配置窗口存在且显式 max_output_tokens 时，reserved_output_tokens MUST 大于等于该值。窗口存在且任一新增 Anthropic 推理字段存在时，预留 MUST 覆盖 Anthropic 普通有效输出上限；任一新增 OpenAI 推理字段存在时 MUST 同时显式配置 max_output_tokens 并由预留覆盖。违反时在资源装配前拒绝，不自动修改预留或推断窗口；所有新字段省略时维持原有预算行为。

#### Scenario: Apply an explicit ordinary output cap
- **WHEN** 配置 max_output_tokens 为 16384 且预算组合合法
- **THEN** 两家普通请求使用各自 wire 名称发送 16384，摘要继续使用独立上限

#### Scenario: Reject an insufficient ordinary reserve
- **WHEN** 窗口已配置，max_output_tokens 为 16384 而 reserved_output_tokens 为 8192
- **THEN** 返回 invalid_configuration，不发起请求且不将任一字段自动改成 16384

#### Scenario: Require a known OpenAI cap for configured reasoning with a window
- **WHEN** OpenAI 配置窗口和显式 effort 或 summary，但未配置 max_output_tokens
- **THEN** 启动失败，不能声称已为未知输出上限充分预留

#### Scenario: Preserve existing small-window startup
- **WHEN** 所有新增字段省略且既有小窗口/预留配置合法
- **THEN** 启动和预算判定维持原行为，不因新增固定 Provider 默认上限而失败

#### Scenario: Keep an unconfigured window unknown
- **WHEN** 用户显式配置推理参数但未配置窗口
- **THEN** 允许结构合法配置，不推断容量、不要求窗口外的输出预留、不保证服务器接受

#### Scenario: Keep an ordinary manual configuration usable
- **WHEN** 普通 manual budget 为 8192、上限 16384，摘要默认上限为 4096，其他既有配置约束合法
- **THEN** 普通启动成功，后续摘要必须在本地预检拒绝，不自动扩大摘要额度或使普通启动失败

### Requirement: Freeze and validate reasoning after Provider environment overrides

既有 Provider 环境变量合并后，非 Anthropic family MUST 拒绝任何显式 Anthropic 推理字段，非 OpenAI family MUST 拒绝任何显式 OpenAI 推理字段，即使其语义类似缺省或关闭。校验 SHALL 在 Provider 网络、Session 创建及后台任务前完成，错误不得回显密钥、完整 URL、配置正文或非法值。合法设置 SHALL 在启动时冻结；配置文件后续变化不影响活动进程。resume/continue SHALL 使用本次启动的显式配置，不从旧 journal 猜测或恢复未保存的设置。

#### Scenario: Reject a family overridden by the environment
- **WHEN** JSON 配置 Anthropic thinking，环境变量把最终 family 改为 OpenAI
- **THEN** 合并后拒绝组合，零网络与零 Session 创建，错误不含配置中的合成 secret

#### Scenario: Ignore later file edits
- **WHEN** 合法启动后修改配置文件的推理字段
- **THEN** 活动进程后续请求维持冻结设置，render/getter 不重读文件

#### Scenario: Resume with current startup settings
- **WHEN** 同一 family/model 的会话用不同合法推理配置重新启动
- **THEN** 新请求使用当前设置，旧 append-only records 保持逐字节不变，不自动请求或重跑工具
