## MODIFIED Requirements

### Requirement: Conversation continues in memory

一次成功 turn 结束后，TUI SHALL 返回可输入状态并允许提交下一轮。后续 turn SHALL 使用同一个会话级 Runtime 和启动时所选 Provider 的原生历史；Anthropic 与 OpenAI 历史不得互相转换或共享。

用户显式恢复 root thread 时，TUI SHALL 在接受新输入前使用 `HistoryProjector` 的语义视图按 turn 顺序初始化 transcript，并继续使用已恢复的 Provider 原生历史。TUI MUST NOT 解析 JSONL payload、Provider wire、signature 或 encrypted reasoning。未指定 resume 的普通启动 SHALL 创建新 Session，不得隐式恢复最近历史；运行中切换 Provider 仍不属于本能力。

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

- **WHEN** 用户退出、重新启动 EasyCode 并显式选择兼容的 root thread
- **THEN** TUI 在进入 idle 前显示已完成 turn 的用户与 assistant 可见文本
- **THEN** 后续 turn 使用恢复后的对应 Provider native history

#### Scenario: Restart without resume

- **WHEN** 用户退出并在未指定 resume 的情况下重新启动 EasyCode
- **THEN** TUI 进入一个新的空 Session，不自动选择旧历史

#### Scenario: Keep the selected provider fixed for a conversation

- **WHEN** 一个新建或恢复的 conversation 已按某个 Provider 创建
- **THEN** 后续 turn 继续使用该 Provider，且应用不会把另一 Provider 的原生历史注入当前请求
