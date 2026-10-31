## MODIFIED Requirements

### Requirement: Streaming stdin uses a strict bounded NDJSON protocol

流式 stdin v1 SHALL 每行包含一个完整 UTF-8 JSON object，并至少包含整数 `version: 1`、字符串 `type` 和非空 `request_id`。v1 只允许：

- `input.submit`：包含 `text`，其语义和 4 MiB 上限与 Runtime submit input 一致。
- `turn.interrupt`：包含 `expected_turn_id`。
- `session.shutdown`：不包含额外 payload。
- `context.query`：不包含额外 payload。
- `context.compact`：必需 instructions 字符串，可为空，合法 UTF-8 且去除外围空白后最多 16 KiB。
- `context.cancel`：必需 expected_compaction_id，合法 UUIDv7，不接受 TurnID。
- `input.append`：包含 text，使用自动投递语义。
- `input.retract`：包含 1 至 64 个合法、非重复领域 InputIDs 的 input_ids。
- `turn.stop`：包含 expected_turn_id，停止并返还未消费自动输入。

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

新增字段 SHALL 严格拒绝 null、重复、未知字段与非法组合；input.submit 保持严格下一 turn，turn.interrupt 保持后续队列。

#### Scenario: Decode a manual compaction command
- **WHEN** context.compact 带 version=1、合法 request_id 和 instructions
- **THEN** 产生专属压缩命令，不解析成普通 input.submit；null、重复/未知字段或混合 turn 身份被拒绝

#### Scenario: Keep literal slash text literal
- **WHEN** input.submit/input.append 的 text 包含 /compact 或 /context
- **THEN** 仍作为用户正文，不执行本地命令

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


## ADDED Requirements

### Requirement: Context output is explicit ordered and isolated from one-shot output

stream-json v1 SHALL 增加 context.snapshot 与 compaction.started/completed/failed，由唯一 stdout writer 编码。snapshot SHALL 只包含有界数值、source kinds/revisions、配置/估算状态、活动状态与 committed scope，不包含正文；completed SHALL 只包含估算、normalized usage 和状态，不包含摘要正文、原生 payload 或权限数据。started 只在 durable start 后、completed 只在成功 Sync/finalize 后输出；每个 accepted compact 必须获得一个成功或失败终态，未获得 durable started 的失败不能伪造 started。durability 不确定时失败输出 MUST 显式标记 durable=false，不宣称已持久化收口。

context.query response MUST 先于 snapshot，compact response MUST 先于对应所有操作事件；成功/失败 terminal 和 cleanup 必须先于 queued 输入启动。没有显式新上下文命令时不得新增压缩控制输出。一次性 --print/--json 的词汇、slash 正文、退出规则保持原样。每个新 kind 必须有严格解码/校验与 golden，不直接序列化内部结构。

#### Scenario: Query during an active operation
- **WHEN** 客户端在普通 turn 或压缩中请求 context.query
- **THEN** response 后得到最近 committed snapshot，active 状态可见，查询不触发网络或窗口变换

#### Scenario: Fail before started became durable
- **WHEN** compact accepted 后 started 的写入确定失败
- **THEN** 输出唯一 compaction.failed(durable=false)，不输出虚假的 started/completed

#### Scenario: Preserve old headless output
- **WHEN** 客户端只使用既有命令或一次性输出模式
- **THEN** 既有字段与事件序列不变，一次性模式不出现 context/compaction 控制帧

### Requirement: Compaction participates in stream drain and shutdown

EOF SHALL 排空已接受压缩与输入，显式 shutdown、协议失败或 stdout 故障 SHALL 取消压缩并等待原有唯一 owner 清理，writer/lease 不提前释放。普通压缩失败作为操作结果报告，不单独使健康控制进程退出为基础设施错误。正常清理保持退出码 0，协议/输出失败保持 1，用法错误保持 2；已损坏 stdout 不再尝试追加错误帧。

#### Scenario: Drain a compact followed by input
- **WHEN** EOF 时压缩活动且存在 queued input
- **THEN** 按操作成功/失败队列规则处理，所有需交付终态和清理完成后关闭输出

#### Scenario: Break stdout during a summary
- **WHEN** stdout 短写或断管且摘要网络仍活动
- **THEN** 停止输出、取消并等待 transport 与 Session cleanup，退出 1，期间 lease 保持
