## Purpose

定义首个可用的 Bubble Tea 文本 Chat/REPL 交互，使用户能够通过已配置的 OpenAI Responses 服务连续提交输入、观察流式回复、取消当前 turn 并从错误中恢复。

## ADDED Requirements

### Requirement: TUI starts with a validated OpenAI conversation
交互模式 SHALL 在进入可提交状态前加载并校验 provider family、`base_url`、API key 和 model。当前切片仅支持 OpenAI Responses；无效配置或选择未实现的 provider MUST 在网络请求前返回明确错误。

#### Scenario: Start with valid OpenAI configuration
- **WHEN** 用户提供有效的 OpenAI family、API prefix、API key 和 model
- **THEN** TUI 进入空闲且可输入状态

#### Scenario: Reject incomplete configuration
- **WHEN** base URL、API key 或 model 缺失或无效
- **THEN** 应用在发起模型请求前返回 `invalid_configuration`
- **THEN** 输出不包含 API key

#### Scenario: Reject unsupported provider in this slice
- **WHEN** 用户配置 Anthropic 或其他尚未实现的 provider
- **THEN** 应用返回明确的 unsupported/provider unavailable 错误
- **THEN** 应用不静默切换 wire 或 provider

### Requirement: User can submit and observe a streaming text turn
空闲状态下，用户 SHALL 能编辑单行文本，并通过 Enter 提交非空输入。提交后 TUI SHALL 在 transcript 中显示用户消息，并按 RuntimeEvent 顺序增量显示 assistant 文本；活动 turn 结束前 MUST NOT 启动第二个并发 turn。

#### Scenario: Submit a non-empty prompt
- **WHEN** 用户在空闲状态输入文本并按 Enter
- **THEN** transcript 增加该用户消息并启动一个 turn
- **THEN** 输入区进入活动 turn 状态

#### Scenario: Ignore an empty submission
- **WHEN** 输入仅为空白且用户按 Enter
- **THEN** TUI 不启动 provider 请求且 transcript 不增加空消息

#### Scenario: Render ordered text deltas
- **WHEN** Runtime 依次产生多个 `assistant_text_delta`
- **THEN** TUI 将增量按事件顺序追加到当前 assistant 消息

#### Scenario: Prevent concurrent submission
- **WHEN** 当前 turn 正在 streaming 且用户再次按 Enter
- **THEN** TUI 不启动第二个 turn
- **THEN** 当前 turn 继续保持唯一活动 turn

### Requirement: Conversation continues in memory
一次成功 turn 结束后，TUI SHALL 返回可输入状态并允许提交下一轮。后续 turn SHALL 使用同一个会话级 Runtime 和 OpenAI 原生历史，但本阶段不要求跨进程持久化或 resume。

#### Scenario: Submit a second turn
- **WHEN** 第一轮 completed 后用户提交第二条输入
- **THEN** TUI 启动下一轮并继续在同一 transcript 中显示消息
- **THEN** Provider 使用第一轮已提交的原生历史构建请求

#### Scenario: Restart the process
- **WHEN** 用户退出并重新启动 EasyCode
- **THEN** 本切片不承诺恢复上一次内存会话

### Requirement: Cancel and quit behavior is unambiguous
活动 turn 期间，Esc 或 Ctrl+C SHALL 取消当前 turn 而不直接退出进程。空闲且输入为空时，Ctrl+C SHALL 退出应用。普通字符 `q` MUST 作为输入处理，不能作为全局退出快捷键。

#### Scenario: Interrupt a streaming turn
- **WHEN** assistant 正在 streaming 且用户按 Esc 或 Ctrl+C
- **THEN** TUI 请求取消当前 turn并保留已经显示的 transcript
- **THEN** 清理结束后 TUI 返回可输入状态

#### Scenario: Exit while idle
- **WHEN** TUI 空闲、输入为空且用户按 Ctrl+C
- **THEN** 应用正常退出并恢复终端状态

#### Scenario: Type the letter q
- **WHEN** 输入框聚焦且用户输入 `q`
- **THEN** `q` 出现在草稿中且应用不退出

### Requirement: Failure is visible and recoverable
provider、stream protocol、timeout 或配置以外的 turn 错误 SHALL 以安全英文错误摘要显示。失败不得清除已有 transcript，且清理完成后用户 SHALL 能提交新的 turn。

#### Scenario: Display provider failure
- **WHEN** Runtime 产生 `turn_failed`
- **THEN** TUI 显示不含 secret 和请求正文的错误摘要
- **THEN** 已完成和已显示的消息仍保留

#### Scenario: Submit after failure
- **WHEN** 失败 turn 已完成清理且用户提交新输入
- **THEN** TUI 可以启动新的 turn
- **THEN** 失败 turn 的部分 assistant output 不进入 Provider 已提交历史
