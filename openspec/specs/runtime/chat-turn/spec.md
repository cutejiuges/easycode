# runtime/chat-turn Specification

## Purpose

定义共享 Runtime 执行一次文本 turn 时的生命周期和终态语义，使 TUI 等宿主只消费稳定 RuntimeEvent，并能可靠区分成功、失败、取消和流意外关闭。

## Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在请求 provider 前产生一次 `turn_started`。Runtime MUST 仅在收到 provider 的显式 completed 终态后产生一次 `turn_completed`；provider channel 关闭本身 MUST NOT 被解释为成功。

#### Scenario: Complete a successful turn

- **WHEN** provider 产生文本事件并最终产生 completed 终态
- **THEN** Runtime 按顺序输出 `turn_started`、中间语义事件和一次 `turn_completed`

#### Scenario: Fail when stream closes before terminal

- **WHEN** provider channel 在 completed 或 failed 终态之前关闭
- **THEN** Runtime 输出一次 `turn_failed`
- **THEN** RunTurn 返回 `stream_protocol_error`

#### Scenario: Propagate provider failure

- **WHEN** provider 产生 failed 终态
- **THEN** Runtime 输出一次 `turn_failed` 并返回对应错误
- **THEN** Runtime 不再输出 `turn_completed`

### Requirement: Assistant text uses a typed semantic payload

RuntimeEvent SHALL 为 assistant 文本增量定义稳定的强类型 payload，至少包含本次追加的文本。宿主 MUST NOT 解析 OpenAI Responses event 或 provider-native item 才能显示文本。

#### Scenario: Project provider text delta

- **WHEN** provider 产生 assistant 文本增量 `hello`
- **THEN** Runtime 输出 `assistant_text_delta`，其 typed payload 的 text 为 `hello`
- **THEN** payload 不包含 OpenAI wire event 对象

#### Scenario: Reject invalid semantic payload construction

- **WHEN** Runtime 尝试构造缺少必需文本字段的 assistant 文本增量
- **THEN** 系统返回明确错误而不是产生不可消费的 RuntimeEvent

### Requirement: Cancellation has a single observable outcome

Runtime SHALL 将调用 context 的取消传播到 provider stream，并等待该 stream 的清理路径退出。用户取消 MUST 产生一次 `turn_failed` 或专用 cancellation 语义，但 MUST NOT 同时产生 completed；对外错误 SHALL 可由 `errors.Is(..., context.Canceled)` 或稳定错误码识别。

#### Scenario: Cancel an active turn

- **WHEN** 用户在 provider stream 活跃时取消 turn context
- **THEN** Runtime 停止继续转发新的文本增量
- **THEN** Runtime 产生一次可识别的取消结果并等待 provider 清理完成

#### Scenario: Cancel races with provider completion

- **WHEN** context 取消与 provider completed 几乎同时发生
- **THEN** Runtime 只发布一个最终终态
- **THEN** 不会同时发布 `turn_completed` 和 `turn_failed`

### Requirement: Only one active turn mutates a conversation

同一个会话级 Runtime SHALL 在任意时刻最多允许一个活动 turn 修改 provider 原生历史。并发提交 MUST 被拒绝或留在宿主层等待，不能并行写入同一 conversation history。

#### Scenario: Submit while a turn is active

- **WHEN** 一个 turn 尚未结束时再次提交输入
- **THEN** 第二次提交不会启动并发 provider stream
- **THEN** 已运行 turn 的原生历史顺序保持不变
