# headless/stream-control Specification

## Purpose

定义自动化宿主通过 stdin/stdout 与单个 EasyCode 进程进行多轮会话控制的有界 NDJSON 协议，使 submit、interrupt、shutdown、Runtime 事件和退出状态可以被可靠关联并保持机器可解析。

## Requirements

### Requirement: Streaming control is an explicit headless mode

CLI SHALL 仅在 `--json --input-format stream-json` 组合下启用长期流式控制模式。该模式 MUST 拒绝 `--print`、位置 prompt、显式 `-` 和终端 stdin，并 SHALL 允许与 `--config`、`--resume` 或 `--continue` 组合；`--resume` 与 `--continue` 仍互斥。参数错误 MUST 在读取或创建 Session、打开 Catalog、生成 turn ID 或调用 Provider 前以退出码 `2` 失败，stdout 保持为空。

一次性 `--print` 和 `--json` 未指定 `--input-format stream-json` 时 SHALL 保持原有 prompt 解析、单 turn 和退出行为。未指定 headless 输出模式时 SHALL 保持 TUI 默认行为。

#### Scenario: Start a streaming JSON session

- **WHEN** 用户以非终端 stdin 执行 `easycode --json --input-format stream-json`
- **THEN** 应用创建或恢复一个 thread、持续读取 NDJSON 命令并保持进程存活直到输入关闭、显式 shutdown 或协议失败

#### Scenario: Reject a prompt in streaming mode

- **WHEN** 用户同时指定 stream-json 输入和位置 prompt
- **THEN** CLI 在 Session 或 Provider 副作用前以退出码 `2` 失败，stdout 保持为空

#### Scenario: Preserve one-shot JSON behavior

- **WHEN** 用户执行既有 `easycode --json "hello"`
- **THEN** 应用仍只提交一个 turn、收口资源并按既有退出语义结束

### Requirement: Streaming stdin uses a strict bounded NDJSON protocol

流式 stdin v1 SHALL 每行包含一个完整 UTF-8 JSON object，并至少包含整数 `version: 1`、字符串 `type` 和非空 `request_id`。v1 只允许：

- `input.submit`：包含 `text`，其语义和 4 MiB 上限与 Runtime submit input 一致。
- `turn.interrupt`：包含 `expected_turn_id`。
- `session.shutdown`：不包含额外 payload。

decoder SHALL 支持任意读取 chunk、跨 chunk 的 JSON 行、UTF-8 多字节边界和 EOF 前没有换行的最后一行，同时在累积内容超过有界行限制前停止读取。它 MUST 拒绝重复字段、未知字段、未知 type/revision、空行以外的非 JSON、多个 JSON value、非法 UTF-8、非法 ID 和超限输入，且错误不得回显原始命令、prompt、配置或 secret。

#### Scenario: Decode fragmented input

- **WHEN** 一个合法 `input.submit` 在 JSON token、换行和 UTF-8 多字节字符中间被任意拆成多个读取 chunk
- **THEN** decoder 只产生一条完整命令，解码后的文本与原始文本相同

#### Scenario: Decode the final line at EOF

- **WHEN** stdin 以一个合法但没有尾部换行的 `session.shutdown` 结束
- **THEN** decoder 处理该命令后再关闭输入，而不是丢弃最后一行

#### Scenario: Reject an oversized line before unbounded allocation

- **WHEN** 当前行超过协议配置的硬上限且尚未出现换行
- **THEN** reader 停止累积、报告安全 `invalid_input`，并进入协议失败清理流程

### Requirement: Every command receives one ordered control response

每个成功解码的入站命令 SHALL 产生恰好一个 `control.response`，其中包含 `version: 1`、相同 `request_id`、命令 `type`、`status` 以及与结果匹配的强类型字段。submit 被接受时 status SHALL 为 `accepted` 且 disposition 为 `starting` 或 `queued`；interrupt 被接受时 disposition SHALL 为 `interrupting`；shutdown 被接受时 disposition SHALL 为 `closing`。拒绝响应 SHALL 使用 `status: rejected` 和稳定安全的英文 error code/message。

`control.response` 只确认命令 admission 或拒绝，不得伪装成 durable turn start 或 terminal。对 `starting`/`queued` submit，后续对应的全部 `turn.*` 和 `assistant.text.delta` 事件 SHALL 携带相同 `input_id`；只有 `turn.started` 才表示对应 `turn_started` 已 durable。显式 shutdown 丢弃已接受但未启动的输入时，系统 SHALL 为每个输入输出一次携带其 `input_id` 和 `session_shutdown` error 的 `input.discarded`。

#### Scenario: Acknowledge a queued input before it starts

- **WHEN** `input.submit` 在另一个 turn 活跃时被接受
- **THEN** stdout 先输出该 request identity 的 `control.response(status=accepted, disposition=queued)`
- **THEN** 它真正开始时，`turn.started` 携带相同 `input_id` 和新 `turn_id`

#### Scenario: Report a stale interrupt

- **WHEN** `turn.interrupt` 的 expected turn ID 与当前活动 turn 不匹配
- **THEN** stdout 输出一次 `control.response(status=rejected)`，error code 为 `turn_mismatch`
- **THEN** 当前 turn 和 follow-up queue 不受影响

#### Scenario: Report a discarded queued input

- **WHEN** shutdown 被接受时存在尚未开始的 queued input
- **THEN** stdout 在结束前为该 input identity 输出一次 `input.discarded`
- **THEN** 该输入不产生 `turn.started` 或 Provider 请求

### Requirement: One stdout writer preserves cross-protocol order

streaming stdout SHALL 由唯一顺序 writer 写入命令响应、`input.discarded` 和既有 headless RuntimeEvent 投影，每个 UTF-8 JSON object 独占一行并以 `\n` 结束。writer MUST 施加背压，且同一输入的 accepted response MUST 先于其 `turn.started`；一个 turn 的 terminal MUST 先于下一输入的 `turn.started`。字符串编码、诊断隔离、短写和断管处理 SHALL 遵守既有 headless JSONL 安全约束。

内部 RuntimeCommand、RuntimeEvent、Session record、Provider wire 或 opaque payload MUST NOT 被直接序列化。streaming v1 可复用既有 `thread.started`、`turn.started`、`assistant.text.delta`、`turn.completed`、`turn.failed` 和 `error` 的字段语义，但本能力新增的 control 类型只属于流式协议，不得出现在一次性 `--json` 输出中。

#### Scenario: Preserve response and turn ordering

- **WHEN** 一个 idle submit 被接受并快速 durable 开始
- **THEN** stdout 中该 submit 的 `control.response(starting)` 严格先于带相同 `input_id` 的 `turn.started`

#### Scenario: Serialize control and runtime output

- **WHEN** Runtime delta 与另一个入站命令响应并发产生
- **THEN** stdout 仍由单 writer 输出两个完整 JSON 行，不交织字节、不产生半行或无效 JSON

#### Scenario: Keep one-shot JSON vocabulary unchanged

- **WHEN** 应用运行一次性 `--json` 模式
- **THEN** stdout 不输出 `control.response`、`input.discarded` 或 `input_id`

### Requirement: EOF drains while shutdown cancels

stdin EOF SHALL 被解释为 graceful input close：应用停止 admission，继续输出活动 turn 和全部已接受 follow-up 的事件，并仅在队列排空、最终 terminal 交付和 Runtime cleanup 完成后关闭 stdout。`session.shutdown` SHALL 输出 accepted response，然后执行显式 shutdown：取消活动 turn、输出 queued input 的 discard 结果、等待 durable terminal 和清理完成后关闭 stdout；shutdown 之后已经读取但尚未接受的命令 SHALL 得到 `session_closing` 拒绝或不再被读取，不得执行。

#### Scenario: Drain on EOF

- **WHEN** stdin 在一个活动 turn 和一个 queued input 存在时关闭
- **THEN** 两个输入都按顺序获得 terminal，stdout 在最后 terminal 后才关闭

#### Scenario: Cancel on explicit shutdown

- **WHEN** `session.shutdown` 在 turn 活跃时被接受
- **THEN** stdout 先输出 shutdown accepted response，再输出活动 turn 的唯一取消 terminal和所有 queued input 的 discard 结果
- **THEN** cleanup 完成后输出流关闭

### Requirement: Protocol and output failures have deterministic cleanup

无法解码的非空输入行 SHALL 在 stdout 可用时产生一个终止性 `error`，停止 admission，按显式 shutdown 语义取消活动 turn、终止尚未开始输入并等待 cleanup，然后以退出码 `1` 结束。stdout 序列化错误、短写或断管 SHALL 停止继续写该流，请求 shutdown 并保持 cleanup ownership，最终以退出码 `1` 结束；不得尝试向已损坏 stdout 追加错误 JSON。

流式会话在 EOF drain 或显式 shutdown 下完成全部协议和资源清理时 SHALL 返回退出码 `0`；单个 turn 的 `turn.failed` 或命令级 rejected 已通过机器事件报告时，不得单独把健康控制进程变成基础设施失败。CLI 用法错误仍为退出码 `2`。所有对外错误必须是稳定、安全的英文 code/message，不得包含输入正文、底层 cause、secret、Provider 正文或 Session opaque payload。

#### Scenario: Fail a malformed command safely

- **WHEN** reader 收到一个无法严格解码的非空 JSON 行
- **THEN** stdout 可用时输出一个安全终止性 `error`，不回显原始行
- **THEN** 应用取消并清理活动工作后以退出码 `1` 结束

#### Scenario: Clean shutdown returns success despite a reported turn failure

- **WHEN** 一个 turn 已通过 `turn.failed` 完整报告，随后宿主发送 shutdown 且清理成功
- **THEN** 进程以退出码 `0` 结束，因为该 turn 结果已由流式协议表达且控制基础设施保持健康

#### Scenario: Broken pipe retains cleanup ownership

- **WHEN** stdout 在活动 turn 中发生短写或断管
- **THEN** 应用不再写 stdout，请求取消并等待 Runtime、Session writer 和 stream cleanup
- **THEN** 进程以退出码 `1` 结束且不把部分输出提交为成功历史

### Requirement: Streaming request deduplication is process-local

流式宿主 SHALL 将 request identity 传递给 Runtime 控制面，并拒绝仍处于 admission、queued、active 或 terminal delivery 状态的重复 identity，防止同一入站 submit 在未完成期间重复执行。该保证只覆盖当前进程和控制器保留窗口；协议 v1 MUST NOT 宣称跨重启 exactly-once，恢复 Session 时也不得把外部 input identity 伪造为已 durable 接受。

#### Scenario: Reject a duplicated outstanding request

- **WHEN** 宿主在原 submit 仍 queued 时再次发送相同 request identity
- **THEN** stdout 输出 `duplicate_request` rejection，原输入继续且只执行一次

#### Scenario: Do not infer acceptance after restart

- **WHEN** 客户端在进程重启后重发一个此前仅收到 queued response、但从未产生 `turn.started` 的 identity
- **THEN** 新进程不从 Session 推断该 identity 已执行
- **THEN** 跨重启重试语义由调用方根据 durable `turn.started`/terminal 事件决定
