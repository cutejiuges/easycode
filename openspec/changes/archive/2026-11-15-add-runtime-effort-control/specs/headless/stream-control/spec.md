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

effort.query/effort.set 的完整编码行 SHALL 额外限制为 4 KiB，在类型判明后、专属 payload 解码前拒绝超限；既有 reader 总上限和其他命令不变。TUI 的 current/status/unset/help 等别名不扩展外部 mode 或 type。

#### Scenario: Decode a manual compaction command
- **WHEN** context.compact 带 version=1、合法 request_id 和 instructions
- **THEN** 产生专属压缩命令，不解析成普通 input.submit；null、重复/未知字段或混合 turn 身份被拒绝

#### Scenario: Keep literal slash text literal
- **WHEN** input.submit/input.append 的 text 包含 /compact、/context、/cache 或 /effort
- **THEN** 仍作为用户正文，不执行本地命令

#### Scenario: Decode a strict effort choice
- **WHEN** effort.set 带合法 request_id、mode=value 和 level=high
- **THEN** 构造唯一当前的类型化设置命令；auto/reset 携带 level、value 缺少 level、null 或未知字段均在 admission 前拒绝

## ADDED Requirements

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
