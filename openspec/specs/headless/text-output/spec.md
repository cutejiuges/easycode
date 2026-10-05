# headless/text-output Specification

## Purpose

定义 EasyCode 单 turn headless 宿主的命令行输入、最终文本和版本化 JSONL 输出契约，使脚本与 CI 能在不依赖 TUI、Provider wire 或 Session 内部记录的前提下可靠运行、取消并恢复文本会话。

## Requirements

### Requirement: CLI selects one explicit headless output mode

CLI SHALL 提供 `--print` 文本模式和 `--json` JSONL 模式；未指定 `--input-format stream-json` 时，任一模式都 SHALL 在提交恰好一个新 turn 并收口资源后退出。两个输出模式 MUST 互斥，并 SHALL 与现有 `--config <path>`、`--resume <thread-id>` 或 `--continue` 组合使用。`--resume` 与 `--continue` MUST 互斥。`--json --input-format stream-json` SHALL 切换到独立的长期流式控制契约，而 `--print` MUST NOT 与 stream-json 输入组合。未指定任一 headless 输出模式时，现有 TUI 启动语义 MUST 保持不变，且 TUI 同样可以显式使用 `--resume` 或 `--continue`。

CLI 参数结构错误、同时指定两个 headless 输出模式、同时指定 `--resume` 与 `--continue`、在一次性模式提供超过一个位置参数，或违反流式输入模式的组合约束 SHALL 作为用法错误在发起配置外部副作用、创建或读取 Session、打开 Catalog 或调用 Provider 前失败，退出码为 `2`。`--version` SHALL 继续不要求 Provider 配置、Catalog 或 prompt。

#### Scenario: Run text headless mode

- **WHEN** 用户执行 `easycode --print "hello"`
- **THEN** 应用提交一次文本 turn、输出其文本结果并退出

#### Scenario: Run JSONL headless mode

- **WHEN** 用户执行 `easycode --json "hello"`
- **THEN** 应用提交一次文本 turn并只向 stdout 输出既有一次性 JSONL 协议事件

#### Scenario: Run streaming JSONL headless mode

- **WHEN** 用户执行 `easycode --json --input-format stream-json` 且 stdin 不是终端
- **THEN** CLI 使用 `headless/stream-control` 契约持续接收命令，而不尝试把完整 stdin 解析成一个 prompt

#### Scenario: Continue in headless mode

- **WHEN** 用户执行 `easycode --print --continue "hello"` 或 `easycode --json --continue "hello"`
- **THEN** CLI 允许该组合，并在 Session 自动选择和恢复成功后提交恰好一个新 turn

#### Scenario: Reject conflicting modes

- **WHEN** 用户同时指定 `--print` 和 `--json`
- **THEN** CLI 在加载 Provider、创建 Session 或发起网络请求前以退出码 `2` 失败
- **THEN** 错误写入 stderr，stdout 保持为空

#### Scenario: Reject stream input with text output

- **WHEN** 用户同时指定 `--print` 和 `--input-format stream-json`
- **THEN** CLI 在读取 Session、Catalog 或 Provider 配置前以退出码 `2` 失败
- **THEN** stdout 保持为空

#### Scenario: Reject conflicting Session selectors

- **WHEN** 用户同时指定 `--resume <thread-id>` 和 `--continue`
- **THEN** CLI 在打开 Catalog、读取 journal、加载 Provider 配置或创建 Session 前以退出码 `2` 失败
- **THEN** 错误写入 stderr，stdout 保持为空

#### Scenario: Preserve interactive default

- **WHEN** 用户未指定 `--print` 或 `--json`
- **THEN** CLI 按现有行为启动 TUI，且不把位置参数或 stdin 解释为 headless prompt 或流式控制协议

### Requirement: Headless prompt resolution is deterministic and bounded

Headless SHALL 接受一个可选位置参数作为 prompt。位置参数缺失或恰好为 `-` 时，应用 MUST 从 stdin 读取 prompt；位置参数缺失且 stdin 是终端时 MUST 作为用法错误失败。存在普通位置参数且 stdin 不是终端时，非空 stdin SHALL 作为附加上下文，以 `prompt + "\n\n<stdin>\n" + stdin + optional-newline + "</stdin>"` 的确定形状组成最终输入；stdin 为空时 SHALL 只使用位置参数。

组合后的输入 MUST 是合法 UTF-8、去除外围空白后非空，且 UTF-8 编码总长度不得超过 4 MiB。stdin 读取 MUST 有界，不能先无界分配后再检查。空输入、非法 UTF-8 或超限输入 SHALL 在创建或修改 Session、生成 turn ID、发起 Provider 请求前失败；输入结构错误退出码为 `2`，底层 stdin 读取错误退出码为 `1`。错误不得回显完整 prompt 或 stdin 内容。

#### Scenario: Read the prompt from piped stdin

- **WHEN** 用户未提供位置参数且通过非终端 stdin 输入 `hello`
- **THEN** Provider 收到的当前 turn 文本为 `hello`

#### Scenario: Force stdin with a dash

- **WHEN** 用户以 `-` 作为位置参数并提供 stdin
- **THEN** 应用读取 stdin 作为完整 prompt，即使入口将 stdin 标记为终端

#### Scenario: Append piped context to an explicit prompt

- **WHEN** 位置参数为 `summarize` 且非终端 stdin 为不带尾部换行的 `content`
- **THEN** Provider 收到 `summarize\n\n<stdin>\ncontent\n</stdin>`

#### Scenario: Reject missing terminal input

- **WHEN** 用户未提供位置参数且 stdin 是终端
- **THEN** CLI 以退出码 `2` 失败，不创建 Session 或调用 Provider

#### Scenario: Reject invalid or oversized input without side effects

- **WHEN** 组合输入不是合法 UTF-8、仅含空白或超过 4 MiB
- **THEN** CLI 返回安全英文用法错误且不创建 journal、不分配 turn、不调用 Provider

### Requirement: Text mode publishes only a durably completed final answer

`--print` SHALL 按事件顺序聚合本次 turn 的 `assistant_text_delta`，但 MUST 仅在收到对应的 durable `turn_completed` 后将聚合结果写入 stdout。输出 MUST 不包含 prompt、既有 transcript、Session ID、状态文本、日志或事件 envelope；结果若没有尾部换行 SHALL 追加一个换行，已有尾部换行不得重复追加，空成功结果 SHALL 输出一个换行。

turn 失败、取消、事件协议错误或在成功终态前关闭时，文本模式 MUST 丢弃已聚合的部分文本，stdout 保持为空，安全英文摘要写入 stderr，并以退出码 `1` 退出。最终 stdout 写入失败同样 SHALL 返回退出码 `1`，但若 Runtime 已 durable 完成，应用 MUST NOT 伪造失败记录、回滚或重放已提交 turn。

#### Scenario: Print a completed answer

- **WHEN** Runtime 依次产生文本 `hel`、`lo` 和 durable `turn_completed`
- **THEN** stdout 恰好输出 `hello\n`，进程退出码为 `0`

#### Scenario: Preserve an existing trailing newline

- **WHEN** 成功聚合文本已经以换行结束
- **THEN** 文本模式不再追加第二个换行

#### Scenario: Hide partial text on failure

- **WHEN** Runtime 在产生部分文本后产生 `turn_failed`
- **THEN** stdout 保持为空，stderr 只包含安全错误摘要，进程退出码为 `1`

#### Scenario: Fail delivery after durable completion

- **WHEN** Runtime 已 durable 完成 turn，但写入最终 stdout 失败
- **THEN** 进程以退出码 `1` 退出且 Session 保留已完成事实
- **THEN** 应用不追加 `turn_failed` 或再次提交相同输入

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

### Requirement: JSONL stdout remains machine-parseable

JSON 模式的 stdout MUST 只包含 UTF-8 JSON object，每个对象占一行并以 `\n` 结束；事件 SHALL 由一个顺序 writer 写入，保持 RuntimeEvent 顺序并对下游写入施加背压。字符串中的换行、U+2028、U+2029 和其他控制字符 MUST 被编码为不会破坏逐行解析的 JSON 表示。人类诊断、日志、配置摘要、旧 transcript 和 stdout guard 标记 MUST 写入 stderr 或不产生。

序列化错误、短写或断管 MUST NOT 产生半个后续 JSON 对象或回退为纯文本 stdout。若写失败发生在活动 turn 期间，宿主 SHALL 请求取消并继续拥有清理责任直到 Runtime 退出；进程最终返回退出码 `1`。若 stdout 已不可写，错误只向 stderr 报告，不尝试向同一损坏流追加 JSON。

#### Scenario: Keep every output line valid JSON

- **WHEN** assistant delta 包含换行、U+2028、U+2029、引号和反斜杠
- **THEN** 消费者仍能逐行解析全部 stdout，且解码后的 `text` 与原始 delta 相同

#### Scenario: Keep diagnostics out of stdout

- **WHEN** headless 运行同时产生可报告诊断
- **THEN** stdout 的每个非空行都能解码为一个已声明的 JSONL v1 event
- **THEN** 诊断只出现在 stderr 且不包含 secret 或请求正文

#### Scenario: Cancel after a broken pipe

- **WHEN** 活动 turn 的 JSON writer 遭遇短写或断管
- **THEN** 宿主请求取消、等待 Runtime 清理并以退出码 `1` 结束
- **THEN** Session 不把取消前的部分 assistant 输出提交为成功历史

### Requirement: Headless cancellation and stream closure have one outcome

当进程上下文在活动 turn 期间取消时，headless 宿主 SHALL 恰好一次请求中断，并继续消费或等待 Runtime 清理直到获得唯一终态或事件 channel 关闭。Runtime 的 durable terminal SHALL 决定成功或失败；取消与 completed 竞态不得让 headless 同时报告成功和失败。

事件 channel 在唯一终态前关闭、事件身份与当前 turn 不一致、payload 无法严格解码或出现首版不支持的 RuntimeEvent kind SHALL 被视为 `stream_protocol_error`，以退出码 `1` 结束且不得输出成功。JSON writer 仍可用时 SHALL 输出一个终止性的 `error`；文本模式 SHALL 保持 stdout 为空并向 stderr 输出安全摘要。

#### Scenario: Cancel an active JSON turn

- **WHEN** 根 context 在 JSON 模式的活动 turn 中取消且 Runtime 发布取消失败终态
- **THEN** JSONL 以 `turn.failed` 结束，其 code 为 `user_cancelled` 且 `cancelled=true`
- **THEN** 进程在清理完成后以退出码 `1` 退出

#### Scenario: Completion wins a cancellation race

- **WHEN** Runtime 已 durable 发布 `turn.completed` 后根 context 才被取消
- **THEN** headless 只报告成功终态并以退出码 `0` 结束

#### Scenario: Detect a stream closed before terminal

- **WHEN** RuntimeEvent channel 在没有 `turn.completed` 或 `turn.failed` 时关闭
- **THEN** headless 不输出成功，并以安全 `stream_protocol_error` 和退出码 `1` 结束

### Requirement: Exit status and public errors are stable

headless SHALL 在 turn durable 成功且所需输出完整交付时返回退出码 `0`，在配置、Session、Provider、stream protocol、取消、序列化、输出或清理失败时返回退出码 `1`，在 CLI 参数或 prompt 结构无效时返回退出码 `2`。JSON 模式的运行错误 SHALL 通过声明的机器事件报告；文本模式和 CLI 用法错误 SHALL 通过 stderr 报告。

所有对外错误 MUST 只使用稳定英文 code 和安全英文 message；底层 cause、API key、Authorization、Cookie、敏感 Header、base URL 凭据、完整 prompt、stdin、Provider 正文和 Session opaque payload 不得进入 stdout、stderr、fixture 或 snapshot。

#### Scenario: Return success only after output delivery

- **WHEN** turn durable 完成且所选模式的输出全部写入成功
- **THEN** 进程退出码为 `0`

#### Scenario: Classify a usage error

- **WHEN** 用户提供冲突模式、多个 prompt 参数或缺失必需输入
- **THEN** 进程退出码为 `2`，且不创建或修改 Session

#### Scenario: Redact a nested failure cause

- **WHEN** 底层错误链包含 API key、请求正文或敏感 URL
- **THEN** headless 对外输出只包含映射后的稳定 code 和安全 message
