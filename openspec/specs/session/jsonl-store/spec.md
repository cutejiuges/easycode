# session/jsonl-store Specification

## Purpose

定义可长期演进的 append-only JSONL Session 事实源，使当前文本对话与未来 Tool、MCP、Hook、Compaction 和 Subagent 记录共享稳定日志机制，而不把 SQLite 或 UI 事件变成恢复依据。

## Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL 记录 SHALL 使用强类型、版本化的 envelope，至少包含 `schema_version`、`payload_version`、`replay_requirement`、单调 `seq`、UTC `timestamp`、`session_id`、`thread_id`、`event_kind`、完整性校验值和受控 JSON `payload`；与记录语义相关时 SHALL 同时包含 `parent_thread_id` 与 `turn_id`。同一 thread 文件中的 session/thread 标识 MUST 保持一致，未知 payload 只能作为有边界的 opaque JSON 传递，不得展开为跨层 `map[string]any`。

`replay_requirement` v1 只能是 `required` 或 `optional`。影响 Provider 原生历史、turn lifecycle、权限、工具副作用、幂等或恢复决策的记录 MUST 标记为 `required`；只影响索引、展示或可丢失统计且被省略不会改变后续副作用和 Provider request 的记录才可标记为 `optional`。首版六种记录全部 SHALL 标记为 `required`。已知 kind/revision 的 requirement、cardinality、顺序约束和 payload codec SHALL 由强类型语义 registry 唯一声明，记录中的声明与 registry 不一致时 MUST 拒绝恢复。

本切片 SHALL 定义 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`turn_completed` 和 `turn_failed` 的首版 payload。`provider_native_commit` 表示一个由对应 Provider codec 定义的有序 native history 增量；当前首版增量是一次成功文本 sample 的输入与输出，不得把该形状或“一个 turn 只有一次 commit”固化到公共 envelope。

未来 Provider wire 中出现的 `tool_use`、`tool_result`、reasoning 或 MCP tool block SHALL 作为相应 sample 的 Provider 原生内容保留在 `provider_native_commit`；权限决策、工具副作用、幂等 ledger、MCP progress 和大结果 artifact 等共享执行事实不得伪装成 Provider 原生 payload，必须由后续 change 定义独立记录种类与持久化策略。

#### Scenario: Encode the initial record vocabulary

- **WHEN** 一个 root thread 完成一次文本 turn
- **THEN** JSONL 使用同一 envelope 记录 session/thread 元数据、turn 边界和该次 Provider sample 的原生提交
- **THEN** 每条记录都包含可独立校验的 schema 与 payload revision
- **THEN** 六种首版记录的 `replay_requirement` 均为 `required`

#### Scenario: Preserve an unknown optional record

- **WHEN** 当前程序读取一个 envelope 版本受支持、结构和 checksum 合法但 kind/revision 未知且标记为 `optional` 的记录
- **THEN** loader 有界保留其 opaque payload 并报告 kind/revision，resume 可忽略该记录继续
- **THEN** 未知 payload 不进入日志、错误或语义投影

#### Scenario: Represent multiple samples in one future turn

- **WHEN** 后续工具循环需要在同一个 turn 下记录多次 Provider sample
- **THEN** 每次 sample 可按 seq 追加独立的 `provider_native_commit`，无需改变既有 envelope 或把原有文本 turn 改写为新形状

#### Scenario: Commit a future tool result before the next sample

- **WHEN** 后续工具循环已经 durable 完成工具结果，但下一次 Provider sample 尚未发起或完成
- **THEN** 对应 Provider codec 可用新的 payload revision 追加 input-only native history commit
- **THEN** 恢复不需要重复工具副作用，也不需要等待下一次模型输出才能保留 Provider 原生 tool result

#### Scenario: Preserve a future native tool pair without conflating execution state

- **WHEN** 后续工具循环的 Provider sample 包含原生 tool call 或 tool result item
- **THEN** Provider codec 可在新的 payload revision 或 commit shape 中无损保存该原生内容，而 JSONL envelope 和写入机制保持不变
- **THEN** 权限、执行、幂等和 artifact 事实使用独立 record kind，不从 Provider 原生内容反推

### Requirement: A single writer assigns order and durably appends batches

每个活动 thread SHALL 只有一个 Session writer。writer MUST 分配连续递增的 `seq` 与记录时间，调用方不得自行选择或回退序号。一个逻辑 batch 中的记录 MUST 按调用顺序编码，并 SHALL 以全有或全无的恢复语义追加；系统只有在该 batch 已写入并对文件执行成功 Sync 后才能向上层确认 durable success。

写入或 Sync 结果不确定时，writer MUST 进入不可继续写入的失败状态；同一进程不得在该 writer 上继续分配序号或启动新的 Provider 副作用。

#### Scenario: Append a successful batch

- **WHEN** writer 追加包含 Provider 原生提交和 turn 完成边界的 batch
- **THEN** 记录获得连续 seq、保持给定顺序并在 durable success 返回前完成文件 Sync

#### Scenario: Serialize concurrent append attempts

- **WHEN** 同一 writer 同时收到多个 append 请求
- **THEN** writer 串行处理请求且最终文件中的 seq 严格递增，不出现重复、倒序或交错 payload

#### Scenario: Stop after an ambiguous write failure

- **WHEN** append 或 Sync 返回无法确认 durable 状态的错误
- **THEN** writer 返回安全英文 session 错误并拒绝后续写入
- **THEN** 上层不会继续当前 conversation 的模型调用或其他副作用

### Requirement: Loading validates the complete journal before replay

Session loader SHALL 按行和 batch 顺序校验记录大小、JSON、完整性校验值、schema revision、seq 连续性、标识一致性和 batch 闭合性。已知 v1 envelope MUST 拒绝重复顶层 member、未知顶层 member、尾随 JSON value、非法 UTF-8 以及非 canonical ID/时间字段。checksum SHALL 覆盖移除 checksum 字段后的强类型 canonical envelope bytes，包括 opaque payload 的持久化表示；Provider payload 内部语义仍由对应 Provider codec 校验。

loader MUST 将结构完整但上层未知的记录保持为有界 opaque payload 并报告其 kind/revision。未知 `required` kind/revision MUST 使 resume 失败；未知 `optional` kind/revision MAY 被恢复消费者忽略。文件中间的损坏、seq 跳变、标识漂移、重复元数据、requirement 与 registry 不一致或未知 required 记录 MUST 产生明确错误，系统不得静默跳过或用默认值补全。

#### Scenario: Load a valid journal

- **WHEN** thread 文件包含受支持且 seq 连续的完整 batches
- **THEN** loader 按 seq 返回所有记录，且不会重排或改写 payload

#### Scenario: Reject corruption in the middle

- **WHEN** 一个非尾部记录 JSON 损坏、超出大小上限、seq 不连续或标识与文件元数据不一致
- **THEN** loader 返回稳定英文 session corruption 错误并且不返回部分可恢复历史

#### Scenario: Reject an unsupported required record

- **WHEN** journal 包含当前程序不理解且标记为 `required` 的 kind/revision
- **THEN** resume 在发起网络请求或其他副作用前失败
- **THEN** 未知 payload 不进入日志或错误文本

#### Scenario: Reject ambiguous JSON envelope fields

- **WHEN** 一行包含重复 `seq`、未知 v1 顶层字段或一个合法对象后的尾随 JSON value
- **THEN** loader 将该行视为损坏并且不得依赖 JSON decoder 的覆盖或宽松默认行为

### Requirement: Replay validation enforces record state transitions

在 loader 完成字节、envelope 和 batch 校验后，恢复消费者 SHALL 在创建可调用 Provider Conversation 前对已知 required records 执行强类型语义回放。首个 committed batch MUST 恰好建立唯一的 `session_meta` 与 root `thread_meta`；后续 metadata 不得重复。root thread 同一时刻最多有一个活动 turn，`provider_native_commit` 和 terminal 必须引用当前活动 turn，terminal 关闭该 turn 后才能开始下一 turn。

对于当前文本 payload revisions，合法完成路径 SHALL 为独立 committed `turn_started`，随后是同一 batch 中按顺序出现的 `[provider_native_commit, turn_completed]`；合法失败路径 SHALL 为 `turn_started` 后的唯一 `turn_failed` 且不包含当前 turn 的 native commit。只有位于 committed journal 末尾且尚无后续记录的单个未闭合 `turn_started` 可被识别为 interrupted-tail replay state；它是需要显式收口的业务状态，不是可由 loader 截断的文件损坏。未来工具循环 MAY 通过新的 required kind/revision 扩展状态转换，但不得放宽旧 revision 的既有不变量。

#### Scenario: Build a valid text replay plan

- **WHEN** journal 包含唯一 metadata batch、一个失败文本 turn 和多个按规定完成的文本 turn
- **THEN** replay validator 按 seq 生成包含 committed native commits 与 terminal 状态的完整计划
- **THEN** Provider 只接收计划中通过语义校验的 native commits

#### Scenario: Reject a checksum-valid illegal transition

- **WHEN** journal 的 JSON、seq、batch 和 checksum 均合法，但出现 commit 位于 turn 之外、重复 terminal、完成边界缺少同 batch commit 或新 turn 覆盖未结束 turn
- **THEN** resume 以稳定英文 session corruption 错误失败且不创建可调用 Conversation

#### Scenario: Report a committed interrupted tail

- **WHEN** journal 以完整 committed `turn_started` 结束且没有该 turn 的 native commit 或 terminal
- **THEN** replay validator 返回显式 interrupted-tail 状态而不截断该记录、不补造成功历史

### Requirement: Tail repair never rewrites committed history

loader SHALL 只修复文件末尾的半行或未完成 batch：截断到最后一个完整 committed batch 边界，保留此前所有合法 bytes，并以结构化结果报告发生了修复。已经完整提交的 batch、文件中间的损坏和语义非法记录 MUST NOT 被自动删除、重排或重写。

修复后 writer SHALL 从最后一个保留记录的下一 seq 继续；反复加载同一已修复文件 MUST 是幂等的。

#### Scenario: Repair a truncated final line

- **WHEN** 进程中断导致 JSONL 最后一行只写入部分 bytes
- **THEN** loader 截断该半行并保留之前的全部 committed batches
- **THEN** 修复结果明确报告被截断的尾部而不回显其 payload

#### Scenario: Repair a trailing incomplete batch

- **WHEN** 尾部含有一个或多个完整 JSON 行但逻辑 batch 缺少 commit 边界
- **THEN** loader 将整个未完成 batch 从可恢复事实中移除并从其起始 offset 截断
- **THEN** 先前 committed batch 的原始 bytes 保持不变

#### Scenario: Reopen after repair

- **WHEN** 已修复的 thread 再次打开并追加记录
- **THEN** 新记录从保留历史的下一 seq 开始，第二次加载不再报告同一尾部修复

#### Scenario: Do not truncate a committed interrupted turn

- **WHEN** 最后一条 `turn_started` 所在 batch 已完整写入并通过 checksum，但进程在 terminal 前退出
- **THEN** loader 保留该 batch，由 replay validator 报告 interrupted-tail 状态

### Requirement: Session storage is private and path-safe

Session 根目录和日期目录在支持权限位的平台上 MUST 使用仅当前用户可访问的权限，thread JSONL 文件 MUST 使用用户私有权限创建且不得因已存在的宽松权限而静默继续。thread ID 与派生路径 MUST 经过严格格式校验和 containment 检查；路径穿越、symlink 逃逸和非普通文件 MUST 在读写前拒绝。

Session records、错误、诊断和 fixture MUST NOT 包含 API key、Authorization、Cookie、敏感 Header、base URL 或配置文件路径。用户输入和 Provider 原生历史只允许存在于权限受控的 Session 事实源，不得复制到诊断日志。

#### Scenario: Create a private thread journal

- **WHEN** 系统在默认 Session 根创建新的日期目录和 thread 文件
- **THEN** Unix 类平台上的目录权限为 `0700` 且文件权限为 `0600`

#### Scenario: Reject an unsafe thread path

- **WHEN** resume 标识可解析为目录穿越、symlink 逃逸或非普通文件目标
- **THEN** 系统在打开文件前返回安全英文 session 错误

#### Scenario: Keep credentials out of the journal

- **WHEN** 使用包含 API key、base URL 和敏感 Header 的配置创建并运行 Session
- **THEN** JSONL、错误、fixture 和诊断中均不包含这些值

### Requirement: Record vocabulary evolves without logging UI deltas

JSONL 事实源 SHALL 持久化恢复、幂等和审计所需的业务边界，不得默认记录每个 assistant text delta、spinner、窗口尺寸或其他 UI 瞬态。后续 Tool、MCP、permission、usage、hook、compact 或 subagent 能力 SHALL 通过新增强类型 event kind/payload revision 和 migration fixture 扩展记录词汇；只要 envelope 语义兼容，MUST NOT 改写已有记录或替换 append/Sync/repair 机制。

工具或 MCP 的流式 progress 与 UI delta 默认 SHALL NOT 持久化；只有被后续规约认定为恢复、幂等或审计必需的终态/检查点才能进入 Session。大工具结果超过行大小边界时，后续实现 SHALL 使用权限受控且具备完整性信息的 artifact，并在 JSONL 中保存有界引用，而不是放宽所有记录的上限。

#### Scenario: Stream many text deltas

- **WHEN** Provider 在一个 sample 中产生大量 assistant 文本增量后成功完成
- **THEN** Session 保存最终 Provider 原生提交和生命周期边界，而不是为每个文本 delta 追加一条事实记录

#### Scenario: Add a future tool result record

- **WHEN** 后续 change 引入工具执行结果和幂等 ledger 记录
- **THEN** 新记录复用相同 envelope、seq、batch、Sync、修复和权限契约
- **THEN** 已有 Session records 的语义与 bytes 不被回写

#### Scenario: Ignore transient MCP progress

- **WHEN** MCP 工具在一次执行中产生大量仅用于界面反馈的 progress 更新
- **THEN** Session 不逐条持久化这些瞬态更新
- **THEN** 后续定义的最终工具结果、幂等状态和必要 artifact 引用仍按 durable 事实记录
