## MODIFIED Requirements

### Requirement: Streaming stdin uses a strict bounded NDJSON protocol

流式 stdin v1 SHALL 每行包含一个完整 UTF-8 JSON object，并至少包含整数 `version: 1`、字符串 `type` 和非空 `request_id`。v1 只允许：

- `input.submit`：包含 `text`，其语义和 4 MiB 上限与 Runtime submit input 一致。
- `turn.interrupt`：包含 `expected_turn_id`。
- `session.shutdown`：不包含额外 payload。
- `context.query`：不包含额外 payload。
- `cache.query`：不包含额外 payload，只查询本进程最近 committed 缓存观测。
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
- **WHEN** input.submit/input.append 的 text 包含 /compact、/context 或 /cache
- **THEN** 仍作为用户正文，不执行本地命令

## ADDED Requirements

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
