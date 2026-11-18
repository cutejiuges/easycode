## ADDED Requirements

### Requirement: Task tool inputs and results restore as Provider-native facts

两家 Provider SHALL 分别保存自身完整任务调用与匹配原生输出：Anthropic 为 TodoWrite tool_use/tool_result，OpenAI 为 update_plan function_call/function_call_output。合法源参数、调用顺序、call identity、wire kind 和冻结成功/错误确认不得因共享归一化而丢失；共享层只消费已验证的工具层封闭 TaskInput、typed ready/result 与纯规范领域 TaskUpdate，不解析或重建 Provider opaque payload。TaskInput 的来源 profile 仅存在于工具输入及其 Session 工具事实/Provider facade 映射中，表达当前输入语法而非内部格式版本或备用兼容 reader；领域 TaskUpdate、TaskProgressState、checkpoint 的 task_progress 和 tasks.snapshot 均不得重复该 profile。

Provider 当前 decoder SHALL 将任务 native 输入与同组 ready/result 的 TaskInput profile、完整纯规范 TaskUpdate、能力、调用身份和来源交叉验证。实时参数解码显式绑定 family/profile，不按 JSON 字段猜测 facade；恢复、补齐输出与离线历史正文读者 SHALL 复用同一份已验证工具输入/result evidence，不从 preview、UI 或自由文本重新猜测已应用状态。只有完整成功结果构成任务生效事实；错误、取消、中断补偿、未完成 sample 和缺失成功 result 不得因原生参数含 completed 就更新清单。任务输出准备保持纯内存，使用已经冻结的最多 1 KiB 英文计数确认，遵守既有 result 配对、预算及成功 Sync 后 finalize 契约；不把 UI 任务事件作为 native history 或创建额外 user_inputs。

#### Scenario: Preserve both task facade inputs
- **WHEN** 两家分别恢复含合法任务调用、完整 typed TaskUpdate、成功结果和 native outputs 的固定 current facts
- **THEN** 原生参数、工具输入 profile/来源、条目文字、activeForm/explanation、call/output 顺序与 wire kind 完整保留，当前纯领域任务等于最近成功结果且不重复 profile

#### Scenario: Reject a mismatched task source or result
- **WHEN** 已保存 native 调用的任务、facade/call identity 或 wire kind 与 typed ready/result 的来源或完整任务内容冲突
- **THEN** 恢复整体失败，不提供部分 Conversation、当前任务投影、repair 或网络请求

#### Scenario: Restore a committed result whose output is missing
- **WHEN** task.progress 成功结果已 durable 但 native tool-output 未提交
- **THEN** 使用冻结确认纯内存补齐原 call 的输出并重建同一任务状态，不重跑工具、Hook、文件或进程

#### Scenario: Ignore a failed task update
- **WHEN** 原生调用输入包含新清单但其对应结果为错误、取消或恢复补偿
- **THEN** 保留真实错误 output 和原配对，当前任务仍来自此前最近成功结果


OpenAI 当前 request/result/native restore 路径 SHALL 保留 update_plan explanation 可省略、null 被拒绝的同一语义，实际请求明确 strict:false，工具层 strict decoder 不放宽。每 facade 的强类型服务端 schema 模式 SHALL 参与静态 catalog 和请求属性证据；既有 function strict 值和 custom/Anthropic 省略行为保持。原生调用的合法性不能依赖当前 UI、服务端是否遵循 schema 或恢复时另选默认 profile。

#### Scenario: Restore an omitted explanation under the explicit server mode
- **WHEN** 本地验证服务核验实际首请求中 update_plan 的 strict:false 后返回省略 explanation 的合法原生调用，随后完整保存并离线恢复
- **THEN** 实时与恢复得到相同 TaskInput 和纯规范 TaskUpdate，原生参数和输出 bytes 保持；explanation:null 或缺失显式服务端 false 不能用假服务无条件成功掩盖

#### Scenario: Reject forged task success in every evidence reader
- **WHEN** fixed current bytes 的 ready/result、profile 或原生参数被改为不一致但外层 checksum 合法
- **THEN** 正常恢复、缺 outputs 补齐及离线任务正文读取全部基于同一 evidence 拒绝，在任何 repair、append、网络请求或工具 I/O 前失败，不显示伪造已应用清单

### Requirement: Current checkpoints preserve exact task state and verified provenance

每个唯一当前 Provider checkpoint 的封闭当前 payload SHALL 含必需 `task_progress`，不在公共 envelope 重复存储。它以 none、clear、snapshot 三态保存 tool/task-progress 定义的任务状态；none 不得携带 source/update；clear/snapshot 必须携带不含 facade profile 的完整纯规范领域 TaskUpdate 及真实成功结果来源 source(result_sequence,turn_id,invocation_id)，clear 的 items 为空、snapshot 非空，来源指向相同线程的成功结果。工具输入的 facade profile 只从该成功结果对应的已验证 ready/native 事实核验，不在 checkpoint 或领域快照重复声明。缺失 task_progress、null、未知字段、旧开发 shape、非法来源或三态矛盾必须失败关闭，不用默认 none、历史 decoder 或内部版本标记补齐。

恢复 SHALL 针对每个 checkpoint 使用其自身 compaction_started 前的完整 Session ledger 前缀，找到最近成功任务结果，并逐字核验任务快照、状态和来源；尚无成功结果必须为 none，最近成功空清单必须为 clear，非空必须为 snapshot。不能只对比最新 checkpoint 与最终任务，也不能用之后的更新、模型摘要或 native 正文替换来源证据。共享层验证 typed metadata，Provider codec 验证自身原生摘要包装，二者必须在 repair 与任何外部副作用之前整体成功。

摘要包装 SHALL 在各自原生结构中确定性保存准确任务文字与明确的历史资料来源说明，包含于候选完整实际编码、footprint 和窗口预算；result_sequence、turn_id、invocation_id 和 CompactionID 只用于 metadata 校验，不进入模型包装正文或内容 fingerprint。合法任务包装不含未经授权的 system/developer 权限，不合成模型工具调用、真实 InputID 或 sample usage。正常请求直接续写已提交 native history，不在每个 sample 额外重复注入任务。摘要失败、取消或候选预算不足时保留旧窗口和任务事实，不裁 snapshot、不将有损摘要当成已验证清单。

#### Scenario: Restore explicit clear after compaction
- **WHEN** 最新成功空任务更新后压缩成功，journal 随后还存在普通回合
- **THEN** checkpoint 的 task_progress 保留 clear 与原成功来源，恢复后不把它变回 none 或更早的非空清单

#### Scenario: Validate multiple checkpoint prefixes
- **WHEN** journal 在任务 A 后压缩，再更新 B 后第二次压缩
- **THEN** 第一个 checkpoint 逐字匹配其 started 前的 A，第二个匹配 B，最终投影来自完整 ledger 的最近成功结果

#### Scenario: Reject an old or forged checkpoint before repair
- **WHEN** checkpoint 缺少必需 task_progress，伪造来源，或在 A 的 started 前缀中携带随后才成功的 B
- **THEN** 恢复在尾部 repair、写入、网络及工具执行前整体失败，既有 journal bytes 不变

#### Scenario: Do not replace exact tasks with summary prose
- **WHEN** 模型摘要遗漏、重写或误报当前任务，但准确包装满足预算
- **THEN** 下一请求和恢复仍包含来自已验证来源的准确任务快照，不依赖摘要文字推断

### Requirement: Task continuation preserves canonical restoration and cache equivalence

相同事实的不中断与恢复路径 SHALL 产生相同的逻辑完整 canonical request bytes、原生 item 顺序、opaque reasoning/signature/redacted/encrypted content/message phase、history footprint/revisions 与内容 fingerprints。验证 SHALL 分别覆盖成功替换、回退、全 completed、显式 clear、预算拒绝、结果已 durable/原生 output 缺失、手动/自动压缩和压缩失败。任务数据只进入真实调用/结果历史及 checkpoint 的动态摘要包装，不能污染稳定 provider_profile、tool_catalog、project_instructions、skill_catalog；查询或 TUI 折叠不改变任何请求字节。

Anthropic disabled/ephemeral_5m/ephemeral_1h 的 marker/TTL SHALL 分别由同一最终请求及冻结策略派生；OpenAI omit/thread-key 与 WebSocket full/incremental SHALL 分别验证各自 wire golden。恢复后的 full 请求与连续连接的 incremental 帧无需逐帧相同，但重组成的逻辑完整请求、任务来源、原生顺序及缓存内容指纹必须等价；WS 失败重建不能丢失或重复任务调用/结果及准确 checkpoint 包装。

#### Scenario: Compare both native continuations
- **WHEN** 两家分别以固定 current bytes 恢复任务更新、明确 clear、opaque reasoning 及任务 checkpoint
- **THEN** 下一完整请求、原生 item 顺序、footprint 与 fingerprint 等于相同事实的不中断路径，任务/Hook/文件/进程执行次数为零

#### Scenario: Keep query and folding outside cache identity
- **WHEN** 唯一变化为 tasks.query、只读 /tasks 或 Ctrl+T 折叠
- **THEN** native bytes、稳定各段 fingerprints 和下一模型请求字节均不变，不增加任务消息或内部版本字段

#### Scenario: Reconstruct a full request after WebSocket failure
- **WHEN** 连续连接已增量提交任务 calls/outputs，随后连接失败并以完整请求重建
- **THEN** 重建完整请求包含各已提交任务事实恰好一次；任务快照和 canonical 内容与正常不中断路径相同
