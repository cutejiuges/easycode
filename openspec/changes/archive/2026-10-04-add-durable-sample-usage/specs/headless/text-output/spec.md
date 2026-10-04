## MODIFIED Requirements

### Requirement: JSON mode exposes a separate versioned event protocol

`--json` SHALL 将内部 RuntimeEvent 单向投影为 headless JSONL v1，不得直接序列化内部事件、timestamp、opaque payload、Provider wire 或 Session record。每个输出对象 SHALL 包含整数 `version: 1` 和字符串 `type`，v1 只允许以下强类型事件：

- `thread.started`：包含 `session_id`、`thread_id` 和布尔 `resumed`。
- `turn.started`：包含相同 `session_id`、`thread_id` 和本次 `turn_id`。
- `assistant.text.delta`：包含相同三种身份和非空 `text`。
- `turn.completed`：包含相同三种身份和 normalized `usage`；usage 必须完整包含五个公共指标及其显式状态，不得包含 Provider raw usage、成本或上下文占用。
- `turn.failed`：包含相同三种身份和 `error`；`error` 必须包含稳定英文 `code`、安全英文 `message` 和布尔 `cancelled`。
- `error`：表示 turn 建立前的启动错误或无法继续投影的本地基础设施错误，只包含安全 `error` 对象且结束当前机器流。

每次已建立的 turn MUST 先输出一次 `turn.started`，随后输出零个或多个 delta，并最终输出恰好一次 `turn.completed` 或 `turn.failed`。`thread.started` MUST 在资源创建或恢复成功后、当前 turn 事件前输出一次。启动失败 MUST 只输出一个 `error`，不得伪造 thread/turn 身份或成功事件。

JSONL v1 SHALL 支持字段级加法演进：生产者只能在已知事件中增加有文档、可选读取的字段，不得在冻结后删除、重命名或改变既有字段类型与语义；消费者 MUST 忽略已知事件中不认识的字段，但仍 MUST 拒绝未知事件类型。由于当前协议尚未稳定发布，本变更直接重写并冻结完整 v1：每个 `turn.completed` MUST 包含 `usage`，即使所有可表达指标状态为 unknown；缺少 usage 的开发期旧 shape 不属于当前 v1 契约。

#### Scenario: Emit a successful new-thread sequence

- **WHEN** 新 Session 的 Runtime 产生两个文本 delta 后以 normalized turn usage 成功完成
- **THEN** JSONL 依次包含 `thread.started(resumed=false)`、`turn.started`、两个 `assistant.text.delta` 和一个 `turn.completed`
- **THEN** 全部 turn 事件携带一致且有效的 session/thread/turn ID，完成事件携带相同的 durable turn usage

#### Scenario: Emit a failed turn sequence

- **WHEN** Runtime 在 turn 建立后产生安全的 Provider 失败终态
- **THEN** JSONL 以包含对应稳定 code、message 和 `cancelled=false` 的 `turn.failed` 结束
- **THEN** 流中不出现 `turn.completed` 或伪造 usage

#### Scenario: Emit a startup error

- **WHEN** JSON 模式因配置无效而无法创建或恢复 thread
- **THEN** stdout 只包含一个版本化 `error` 对象，进程退出码为 `1`
- **THEN** 对象不包含虚构 ID、配置 secret、路径或底层 cause

#### Scenario: Read a historical v1 completion without usage

- **WHEN** v1 completion schema 校验一个不含 `usage` 的 `turn.completed`
- **THEN** 该对象被拒绝为不完整的当前 v1 completion，且不得补造全零 usage

#### Scenario: Ignore an additive field on a known v1 event

- **WHEN** v1 消费者读取已知事件类型且对象中存在其不认识的新增字段
- **THEN** 消费者忽略该字段并继续处理既有字段和事件顺序

#### Scenario: Do not expose unsupported event kinds

- **WHEN** 当前实现没有完整支持 reasoning、tool 或其他未来 RuntimeEvent kind
- **THEN** JSONL v1 不声明、不输出空占位，也不把内部 payload 透传为未知机器事件
