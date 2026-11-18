## MODIFIED Requirements

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


## ADDED Requirements

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
