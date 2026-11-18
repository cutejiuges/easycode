# headless/stream-control Specification

## Purpose

定义自动化宿主通过 stdin/stdout 与单个 EasyCode 进程进行多轮会话控制的有界 NDJSON 协议，使输入、interrupt、shutdown、上下文与进程 effort 控制、Runtime 事件和退出状态可以被可靠关联并保持机器可解析。

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
- `context.query`：不包含额外 payload。
- `cache.query`：不包含额外 payload，只查询本进程最近 committed 缓存观测。
- `tasks.query`：不包含额外 payload，只读当前线程最近成功 durable 任务状态。
- `context.compact`：必需 instructions 字符串，可为空，合法 UTF-8 且去除外围空白后最多 16 KiB。
- `context.cancel`：必需 expected_compaction_id，合法 UUIDv7，不接受 TurnID。
- `input.append`：包含 text，使用自动投递语义。
- `input.retract`：包含 1 至 64 个合法、非重复领域 InputIDs 的 input_ids。
- `turn.stop`：包含 expected_turn_id，停止并返还未消费自动输入。
- `effort.query`：不包含额外 payload，读取 process scope 的 effort 状态。
- `effort.set`：必需 mode=auto/reset/value；只有 value 分支必须且只允许携带一个合法小写 level，其余分支不得带 level。

decoder SHALL 支持任意读取 chunk、跨 chunk 的 JSON 行、UTF-8 多字节边界和 EOF 前没有换行的最后一行，同时在累积内容超过有界行限制前停止读取。它 MUST 拒绝重复字段、未知字段、未知 type/revision、空行以外的非 JSON、多个 JSON value、非法 UTF-8、非法 ID 和超限输入，且错误不得回显原始命令、prompt、配置或 secret。

#### Scenario: Decode fragmented input
- **WHEN** 一个合法 input.submit 在 JSON token、换行和 UTF-8 多字节字符中间被任意拆成多个读取 chunk
- **THEN** decoder 只产生一条完整命令，解码后的文本与原始文本相同

#### Scenario: Decode the final line at EOF
- **WHEN** stdin 以一个合法但没有尾部换行的 session.shutdown 结束
- **THEN** decoder 处理该命令后再关闭输入，而不是丢弃最后一行

#### Scenario: Reject an oversized line before unbounded allocation
- **WHEN** 当前行超过协议配置的硬上限且尚未出现换行
- **THEN** reader 停止累积、报告安全 invalid_input，并进入协议失败清理流程

新增字段 SHALL 严格拒绝 null、重复、未知字段与非法组合；input.submit 保持严格下一 turn，turn.interrupt 保持后续队列。

effort.query/effort.set/tasks.query 的完整编码行 SHALL 额外限制为 4 KiB，在类型判明后、专属 payload 解码前拒绝超限；既有 reader 总上限和其他命令不变。TUI 的 current/status/unset/help 等别名不扩展外部 mode 或 type。

#### Scenario: Decode a manual compaction command
- **WHEN** context.compact 带 version=1、合法 request_id 和 instructions
- **THEN** 产生专属压缩命令，不解析成普通 input.submit；null、重复/未知字段或混合 turn 身份被拒绝

#### Scenario: Keep literal slash text literal
- **WHEN** input.submit/input.append 的 text 包含 /compact、/context、/cache、/effort 或 /tasks
- **THEN** 仍作为用户正文，不执行本地命令

#### Scenario: Decode a strict effort choice
- **WHEN** effort.set 带合法 request_id、mode=value 和 level=high
- **THEN** 构造唯一当前的类型化设置命令；auto/reset 携带 level、value 缺少 level、null 或未知字段均在 admission 前拒绝

#### Scenario: Decode a strict tasks query
- **WHEN** tasks.query 包含 version=1、合法 request_id 且完整编码行不超过 4 KiB
- **THEN** 构造专属只读任务查询；text、payload、null、重复/未知字段、非法 request_id 或超限编码行在 admission 前拒绝，不提交用户输入

### Requirement: Every command receives one ordered control response

每个成功解码的入站命令 SHALL 产生恰好一个 `control.response`，其中包含 `version: 1`、相同 `request_id`、命令 `type`、`status` 以及与结果匹配的强类型字段。submit 被接受时 status SHALL 为 `accepted` 且 disposition 为 `starting` 或 `queued`；interrupt 被接受时 disposition SHALL 为 `interrupting`；shutdown 被接受时 disposition SHALL 为 `closing`。拒绝响应 SHALL 使用 `status: rejected` 和稳定安全的英文 error code/message。

`control.response` 只确认命令 admission 或拒绝，不得伪装成 durable turn start 或 terminal。对严格 input.submit 的 `starting`/`queued` submit，后续对应的全部 `turn.*` 和 `assistant.text.delta` 事件 SHALL 携带相同 `input_id`；只有 `turn.started` 才表示对应 `turn_started` 已 durable。显式 shutdown 丢弃已接受但未启动的输入时，系统 SHALL 为每个严格输入输出一次携带其 `input_id` 和 `session_shutdown` error 的 `input.discarded`；自动输入使用 input.status returned。

#### Scenario: Acknowledge a queued input before it starts

- **WHEN** `input.submit` 在另一个 turn 活跃时被接受
- **THEN** stdout 先输出该 request identity 的 `control.response(status=accepted, disposition=queued)`
- **THEN** 它真正开始时，`turn.started` 携带相同 `input_id` 和新 `turn_id`

#### Scenario: Report a stale interrupt

- **WHEN** `turn.interrupt` 的 expected turn ID 与当前活动 turn 不匹配
- **THEN** stdout 输出一次 `control.response(status=rejected)`，error code 为 `turn_mismatch`
- **THEN** 当前 turn 和 follow-up queue 不受影响

#### Scenario: Report a discarded queued input

- **WHEN** shutdown 被接受时存在尚未开始的严格 queued input
- **THEN** stdout 在结束前为该 input identity 输出一次 `input.discarded`
- **THEN** 该输入不产生 `turn.started` 或 Provider 请求

自动 append 的 accepted response SHALL 额外包含领域 entry_id；turn.* 的 input_id 仍为启动该 turn 的首条请求 RequestID。自动输入共享 turn 时不重复输出 delta，其余输入通过 input.status 关联。新增 input.status 包含 version=1、type、request_id、entry_id、claimed/committed/returned/uncertain state、适用的 turn_id 和固定安全 reason，不含正文。新命令 response 先于相应状态，初始 turn.started 先于 committed，committed 先于后续请求 delta。retract/stop 接受 disposition 分别为 retracted/stopping，拒绝沿用稳定英文错误。严格输入和一次性输出必须过滤新增内部输入事件，保持原有字段/序列。

#### Scenario: Correlate a batched automatic turn
- **WHEN** 两条 input.append 一起作为初始输入提交
- **THEN** 各自先收到带 entry_id 的 accepted，再以各自 committed 关联同一 turn；turn 事件只使用首条 RequestID，正文不出现在 status

#### Scenario: Retract only requested pending inputs
- **WHEN** input.retract 与 claim 并发
- **THEN** 先被 owner 接受者决定结果；全量取回成功或 input_not_queued，不能部分取回

上下文 query 被接受时 disposition SHALL 为 queried，随后输出一次携带相同 request_id 的 context.snapshot；compact 为 compacting 并携带 compaction_id；cancel 为 cancelling。拒绝使用 session_busy、nothing_to_compact、compaction_budget_unknown、compaction_budget_exceeded、no_active_compaction、compaction_mismatch 等匹配的安全英文代码。压缩 admission 不等于 durable started，取消 admission 不等于操作已结束。

#### Scenario: Correlate compaction independently of turns
- **WHEN** context.compact 被接受
- **THEN** response 先包含 compaction_id，后续操作事件使用发起 request_id 和同一 compaction_id，不携带 input_id/turn_id

#### Scenario: Cancel an old compaction
- **WHEN** expected_compaction_id 不匹配当前活动压缩
- **THEN** 一次 rejected response，不取消当前 worker 或后续 turn

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

### Requirement: Context output is explicit ordered and isolated from one-shot output

stream-json v1 SHALL 增加 context.snapshot 与 compaction.started/completed/failed，由唯一 stdout writer 编码。snapshot SHALL 只包含有界数值、source kinds/revisions、配置/估算状态、活动状态与 committed scope，不包含正文；completed SHALL 只包含估算、normalized usage 和状态，不包含摘要正文、原生 payload 或权限数据。started 只在 durable start 后、completed 只在成功 Sync/finalize 后输出；每个 accepted compact 必须获得一个成功或失败终态，未获得 durable started 的失败不能伪造 started。durability 不确定时失败输出 MUST 显式标记 durable=false，不宣称已持久化收口。

context.query response MUST 先于 snapshot，compact response MUST 先于对应所有操作事件；独立手动压缩的成功/失败 terminal 和 cleanup 必须先于 queued 输入启动；自动压缩的 terminal 和 cleanup 必须先于原 turn 下一 sample，原 turn 结束前不启动另一 turn。自动压缩 SHALL 以 trigger=auto、原 submit 关联及真实 TurnID/sample index 输出 started/completed/failed，不生成虚构 compact response；manual 路径保持 response 先于事件。未启用自动压缩且没有显式新上下文命令时不得新增压缩控制输出。一次性 --print/--json 的词汇、slash 正文、退出规则保持原样。每个新 kind 必须有严格解码/校验与 golden，不直接序列化内部结构。

#### Scenario: Query during an active operation
- **WHEN** 客户端在普通 turn 或压缩中请求 context.query
- **THEN** response 后得到最近 committed snapshot，active 状态可见，查询不触发网络或窗口变换

#### Scenario: Fail before started became durable
- **WHEN** compact accepted 后 started 的写入确定失败
- **THEN** 输出唯一 compaction.failed(durable=false)，不输出虚假的 started/completed

#### Scenario: Preserve old headless output
- **WHEN** 客户端只使用既有命令或一次性输出模式
- **THEN** 既有字段不变；自动压缩可在 stream-json 普通 turn 中增加关联压缩事件，一次性模式不出现 context/compaction 控制帧

#### Scenario: Correlate an automatic summary
- **WHEN** stream-json 已接受的 submit 在工具续写前自动压缩
- **THEN** 压缩事件关联该 submit 和活动 turn，成功终态在下一普通采样前输出，不产生额外 control response

#### Scenario: Continue after an automatic failure
- **WHEN** 自动压缩 failed 已确定收口且旧窗口仍通过普通请求预检
- **THEN** 同一 submit 可继续产生普通采样事件，不额外发送 control response，不伪造 turn.failed；最终 turn 仅有一个终态

### Requirement: Compaction participates in stream drain and shutdown

EOF SHALL 排空已接受压缩与输入，显式 shutdown、协议失败或 stdout 故障 SHALL 取消压缩并等待原有唯一 owner 清理，writer/lease 不提前释放。普通压缩失败作为操作结果报告，不单独使健康控制进程退出为基础设施错误。正常清理保持退出码 0，协议/输出失败保持 1，用法错误保持 2；已损坏 stdout 不再尝试追加错误帧。

#### Scenario: Drain a compact followed by input
- **WHEN** EOF 时压缩活动且存在 queued input
- **THEN** 按操作成功/失败队列规则处理，所有需交付终态和清理完成后关闭输出

#### Scenario: Break stdout during a summary
- **WHEN** stdout 短写或断管且摘要网络仍活动
- **THEN** 停止输出、取消并等待 transport 与 Session cleanup，退出 1，期间 lease 保持

### Requirement: Cache queries have strict correlated bounded output

stream-json v1 SHALL 接受 cache.query，并先输出恰好一次关联 control.response；成功时 disposition=queried，随后输出恰好一次相同 request_id 的 cache.snapshot。snapshot SHALL 使用 context/cache-diagnostics 的有界纯数值、枚举、source revision/fingerprint、process scope 与活动状态，不包含 prompt、原生 payload、key 值或 header。查询不得生成 input_id、turn_id、虚构用户输入或 Provider 请求；最近普通/摘要观测各自保留真实归属，不将查询 request_id 冒充模型操作身份。

新命令与输出 SHALL 有严格 decoder、validator 和 golden；未知/重复字段、null、非法 identity 和超过 64 KiB 的 snapshot MUST 被拒绝。唯一 stdout writer SHALL 保持 response 在 snapshot 前，并沿用 EOF drain、显式 shutdown、背压和断管清理语义。只有显式 cache.query 才输出 cache.snapshot，不主动向普通 stream 插入缓存帧；一次性 --print/--json 的词汇、字段和退出语义不变。

#### Scenario: Query during an active tool sample
- **WHEN** 合法 cache.query 在工具审批或网络等待中被接受
- **THEN** response 后返回最近 committed 快照与 activity，不等待模型完成，也不发新模型请求

#### Scenario: Keep slash input literal
- **WHEN** input.submit 或 input.append 的 text 为 /cache
- **THEN** 继续作为真实用户正文，不执行本地缓存查询

#### Scenario: Reject extra query payload
- **WHEN** cache.query 包含 text、null 字段、重复 request_id 或其他未知字段
- **THEN** 按现有 strict 协议失败/重复身份规则处理，无缓存或 Provider 副作用

#### Scenario: Preserve existing output modes
- **WHEN** 用户不发送 cache.query 或使用一次性输出
- **THEN** 不新增 cache.snapshot；stdout 故障仍由既有唯一 owner 取消并等待所有资源

### Requirement: Effort controls return ordered process-local snapshots

成功解码的 effort.query/effort.set SHALL 经同一输入控制 owner 产生恰好一次关联 control.response。查询成功 disposition=queried，设置成功 disposition=updated；随后 SHALL 输出恰好一次相同 request_id 的 effort.snapshot，包含 version=1、session_id、thread_id 及 runtime/effort-control 定义的有界非敏感快照，最大编码 16 KiB。设置 response 仅确认本地选择，不宣称当前请求已切换、已持久化或远端接受。按 Provider 不支持的合法公共 level 返回 invalid_effort，配置/预算组合冲突返回 effort_conflict；拒绝不得输出成功 snapshot。

新命令、response 和 snapshot SHALL 有严格 decoder、validator 和固定 golden；effort.snapshot 的字段为唯一当前 shape，拒绝重复/未知/null、非法枚举、错误的活动操作身份及不一致 pending。唯一 stdout writer SHALL 保持 response 先于快照，活动模型等待期间仍可查询或接受下一操作选择。只有显式 effort.query 或成功 effort.set 才输出该快照；不主动向普通 turn 插入新控制帧。

snapshot SHALL 按 runtime/effort-control 携带必需整数 selection_revision 以及唯一当前 active 联合：turn_starting 使用既有输入 request_id，不能伪造 TurnID；durable turn 与独立手动摘要分别使用真实 TurnID/CompactionID，phase 与 cancel_requested 不表示已经发送或清理完成。active 捕获序号不得大于当前序号。设置序号与 current/native/format revision 无关，新进程输出流的 thread.started SHALL 重置客户端观测水位，不能跨重启去重或比较。

EOF drain、显式 shutdown、背压及断管清理 SHALL 沿用既有 owner。既有 context.snapshot 的外部字段和一次性 --print/--json 的词汇、版本、字段及退出语义 MUST 保持；内部增强的 context 设置不得直接序列化为旧外部 snapshot。所有 slash 正文仍为真实用户输入，不以文本模拟控制命令。

#### Scenario: Set effort during a running request
- **WHEN** high 回合活动期间 effort.set(value, low) 被接受
- **THEN** response(updated) 先于 effort.snapshot，快照显示 active high、selected low、pending=true；当前请求和 journal 不变

#### Scenario: Inspect settings without starting work
- **WHEN** 空闲时 effort.query 被接受
- **THEN** 一次 queried response 后一次 snapshot，无新 turn、input identity、Provider 请求或磁盘写入

#### Scenario: Preserve old output shapes
- **WHEN** 客户端只发 context.query、普通输入或使用一次性输出
- **THEN** 不出现 effort.snapshot，context.snapshot 不增加内部 effort 字段，旧固定 golden 保持

#### Scenario: Close after accepting a process-only setting
- **WHEN** effort.set 已 accepted，随后 EOF 或 session.shutdown 到达
- **THEN** 已接收结果仍确定，输出按序排空或按既有断管规则停止；不把设置写入 journal，不丢失活动工作清理 owner

#### Scenario: Query a starting turn
- **WHEN** input 已接收而 durable turn.started 尚未发布，effort.query 到达
- **THEN** queried response 后返回 turn_starting 快照及发起输入 request_id，不伪造 input/turn 生命周期事件或 durable 身份

#### Scenario: Bound the new control payload
- **WHEN** effort 命令的编码行超过 4 KiB，即使超长部分只是空白或字段值
- **THEN** 在专属 payload 解码与 admission 前安全失败，无选择变化；reader 仍使用既有总内存上限

#### Scenario: Lose a snapshot after its response
- **WHEN** updated response 已输出，随后 effort.snapshot 的写入短写或断管
- **THEN** 不回滚已接收选择，不补发控制或模型请求；沿原故障路径取消并等待实际资源 owner

#### Scenario: Start a new process with a lower observation revision
- **WHEN** 恢复同一 ThreadID 的新进程发布 thread.started，随后查询返回 selection_revision=0
- **THEN** 客户端按新输出流重新建立观测水位，不把它当作旧进程高序号的过期消息

### Requirement: Task queries return ordered bounded committed snapshots

stream-json v1 SHALL 接受严格 `tasks.query`，成功时先输出恰好一次关联 `control.response(status=accepted, disposition=queried)`，随后输出恰好一次相同 request_id 的 `tasks.snapshot`。任务 snapshot SHALL 为严格唯一当前外部 shape，包含 version=1、type、request_id、真实 session_id/thread_id，以及 tool/task-progress 定义的 none/clear/snapshot、已提交成功来源与完整有界规范 domain.TaskUpdate；派生完成计数与状态必须和条目一致。外部快照不携带 tool.TaskInput、facade profile、TodoWrite/update_plan 原始参数包装或 Provider 内部元数据；原输入 profile 的验证不因此取消。任务文字属于显式查询结果，不写 diagnostic，不返回原生 payload、opaque reasoning、绝对存储路径、未提交 draft 或其他 thread 的任务。

任务查询 SHALL 在 idle、普通模型请求、工具/Question/批准等待、Stop 检查及压缩期间读取最近 committed 快照，不等待模型或压缩结束，不启动 Provider 请求、不执行工具/Hook、不写 Session/配置、不产生 input_id、新 turn、CompactionID 或虚构调用身份。查询 identity 只关联本查询 response/snapshot，不替代任务本来的成功结果来源。快照遵守 tool/task-progress 的 20 项、16 KiB 文字及 64 KiB canonical 上限；外部完整 snapshot 编码额外不得超过 96 KiB，必须预检实际编码，不以截断、缺项或部分成功规避限制。

新命令和 snapshot SHALL 具有严格 decoder、validator 及固定 golden，拒绝 null、重复/未知字段、非法三态/来源、状态计数不一致、超过对应上限及混入 Provider opaque 数据。tasks.query 的 4 KiB 完整编码行上限在判明类型后、专属 payload 解码与 admission 前执行；超长空白也计入，不能只限制字段值。原有 request identity 去重、session_closing 拒绝、唯一 stdout writer、response-before-snapshot、背压、EOF drain、shutdown 与断管清理规则保持。

只有显式 tasks.query 才 SHALL 输出 tasks.snapshot，不主动向普通流插入任务控制帧；一次性 --print/--json 不增加专用任务帧。headless 投影必须显式识别并按既有策略忽略只供宿主/TUI 的任务更新事件，不能把新增已知事件当基础设施错误；任务模型调用和结果仍可正常完成。普通 input.submit/input.append 中的 /tasks SHALL 保持真实用户正文，不模拟本地查询。外部协议继续为 v1，内部 TaskUpdate、native/Session/checkpoint 均不增加格式版本或兼容 reader。

#### Scenario: Query before any task was created
- **WHEN** idle thread 从未成功提交任务更新并收到合法 tasks.query
- **THEN** queried response 先于返回 none 的 snapshot，没有虚构成功来源、模型请求或 journal 写入

#### Scenario: Query while the next task update is not durable
- **WHEN** 原清单 A 已成功保存，更新 B 的 Sync 尚未完成且收到 tasks.query
- **THEN** response 后的快照只含 A；B 仅在自己的成功结果确定 durable 后可供后续查询读取

#### Scenario: Keep the original source in a query
- **WHEN** 恢复的成功任务结果在另一个用户回合形成，当前活动操作中收到 tasks.query
- **THEN** 快照保留任务真实成功来源，request_id 只关联本查询，不伪造当前 turn 创建了该任务

#### Scenario: Reject task query payload and excessive whitespace
- **WHEN** tasks.query 含额外 text/null/重复字段，或完整行因空白而超过 4 KiB
- **THEN** 沿既有严格协议失败规则安全收口，不回显原命令，不改变任务或 journal，也不启动网络

#### Scenario: Preserve one-shot task execution
- **WHEN** 一次性 --print/--json 正常执行任务模型工具
- **THEN** 原工具结果照常提交，已知任务展示事件不成为错误，stdout 不出现 tasks.snapshot、control.response 或内部 TaskUpdate 原样序列化

#### Scenario: Keep a slash prompt literal
- **WHEN** stream-json input.submit/input.append 的 text 为 /tasks
- **THEN** 按普通用户输入提交并正常建模，不输出任务查询 snapshot

#### Scenario: Drain or fail after the response
- **WHEN** tasks.query 的 response 已输出，随后 EOF 到达或 snapshot 写入短写/断管
- **THEN** EOF 按序排空已接受快照，输出故障则停止写流并沿唯一 owner 取消/等待实际活动资源，不重试查询、回滚任务或释放仍持有的 lease

任务查询 snapshot SHALL 仅披露规范 domain 更新、三态、真实成功结果来源及派生计数，result_sequence 必须等于 live 成功 Append 返回或 replay 还原的实际 result record seq，不允许 ready/detail/native seq 代替。domain 更新/状态和外部查询 shape 必须拒绝 facade profile 等未知字段，不因其文字值合法而忽略透传；profile 仅属于模型工具输入事实。查询不生成或更新任务内容 stamp，其前后对应窗口的规划/请求内容身份不变。

#### Scenario: Keep task queries independent of facade input profiles
- **WHEN** 两家已通过各自严格 TaskInput 校验的任务成功保存并被 tasks.query 查询
- **THEN** 快照只返回规范任务值、实际 result 来源与计数，不暴露 facade profile 或原参数包装；注入这些字段的快照被当前 decoder 拒绝

#### Scenario: Report the durable result position rather than readiness
- **WHEN** 一个任务的 ready/start/result/native seq 各不相同，用户分别在 live 和恢复后查询
- **THEN** 两份 snapshot 的来源均指向实际成功 result seq，查询 request_id 不替代任务来源，原生 bytes 和内容 stamp 不变
