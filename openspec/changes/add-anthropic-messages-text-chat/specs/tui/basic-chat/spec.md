## MODIFIED Requirements

### Requirement: TUI starts with a validated OpenAI conversation

交互模式 SHALL 在进入可提交状态前加载并校验 provider family、`base_url`、API key 和 model。选择 `openai` 时 SHALL 创建 OpenAI Responses conversation；选择 `anthropic` 时 SHALL 创建 Anthropic Messages conversation。无效配置或未知 provider MUST 在网络请求前返回明确错误，应用不得静默切换 wire 或回退到另一 Provider。

#### Scenario: Start with valid OpenAI configuration

- **WHEN** 用户提供有效的 OpenAI family、API prefix、API key 和 model
- **THEN** TUI 使用 OpenAI Responses conversation 进入空闲且可输入状态

#### Scenario: Start with valid Anthropic configuration

- **WHEN** 用户提供有效的 Anthropic family、API prefix、API key 和 model
- **THEN** TUI 使用 Anthropic Messages conversation 进入空闲且可输入状态

#### Scenario: Reject incomplete configuration

- **WHEN** base URL、API key 或 model 缺失或无效
- **THEN** 应用在发起模型请求前返回 `invalid_configuration`
- **THEN** 输出不包含 API key

#### Scenario: Reject unsupported provider in this slice

- **WHEN** 配置包含 Anthropic 和 OpenAI 以外的 provider family
- **THEN** 应用在网络请求前返回明确的配置或 provider unavailable 错误
- **THEN** 应用不静默选择 OpenAI、Anthropic 或其他 wire

### Requirement: Conversation continues in memory

一次成功 turn 结束后，TUI SHALL 返回可输入状态并允许提交下一轮。后续 turn SHALL 使用同一个会话级 Runtime 和启动时所选 Provider 的原生历史；Anthropic 与 OpenAI 历史不得互相转换或共享。本阶段不要求跨进程持久化、resume 或运行中切换 Provider。

#### Scenario: Submit a second turn

- **WHEN** 任一已支持 Provider 的第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** 所选 Provider 使用第一轮已提交的自身原生历史构建请求

#### Scenario: Submit a second OpenAI turn

- **WHEN** 使用 OpenAI conversation 的第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** OpenAI Provider 使用第一轮已提交的 Responses 原生历史构建请求

#### Scenario: Submit a second Anthropic turn

- **WHEN** 使用 Anthropic conversation 的第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** Anthropic Provider 使用第一轮已提交的 Messages 原生历史构建请求

#### Scenario: Restart the process

- **WHEN** 用户退出并重新启动 EasyCode
- **THEN** 本切片不承诺恢复上一次内存会话

#### Scenario: Keep the selected provider fixed for a conversation

- **WHEN** 一个内存 conversation 已按某个 Provider 创建
- **THEN** 后续 turn 继续使用该 Provider，且应用不会把另一 Provider 的原生历史注入当前请求
