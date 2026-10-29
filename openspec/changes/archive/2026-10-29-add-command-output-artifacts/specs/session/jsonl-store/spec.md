## MODIFIED Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含一个 schema canary、一个 payload canary、单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、Provider wire kind、sample/call index、capability，以及与 `Read`、`Glob`、`Grep`、`Patch`、`Write`、`Exec` 或 `ReadArtifact` 匹配的完整 typed input；`tool_approval_decided` SHALL 保存 request/invocation identity、candidate fingerprint、typed outcome、decision source、仅适用于 deny 的有界反馈与不含绝对路径、正文、环境值或命令输出的安全 metadata；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。Exec metadata SHALL只包含相对workdir、exit/timeout/cancelled/truncated状态、duration、stdout/stderr计数及各流 typed 保存状态与 descriptor，不得包含PID、绝对路径或环境。工具 records MUST NOT 保存 input revision、result codec revision、执行器额外采集的完整 base/final 文件快照、完整 diff、临时文件路径或可执行 Provider wire item。

schema/payload canary 只用于精确验证当前格式，不得选择历史 decoder。completed sample 的 Provider entry 仍作为 native commit 并紧邻配对 `sample_usage`；tool results 编码为 tool-output native commit 时不生成 sample usage。高频参数 delta、approval request/焦点/按键、命令输出progress、elapsed tick、UI 状态和文件系统瞬时信息 MUST NOT 进入 JSONL。

Write 的必要 typed input SHALL 保存 file_path/content，并与原生调用参数一致；该 content 是调用事实，不是额外采集的磁盘文件快照。Write result SHALL 只保存冻结的有界摘要与 typed metadata，不回显正文。Read observation、目录/file identity、暂存路径与 grant MUST NOT 保存。

#### Scenario: Encode the current record vocabulary
- **WHEN** 一个 root thread 完成包含 Read、Glob、Grep、获批 Patch 与获批 Exec 的 Tool Loop
- **THEN** JSONL 复用同一当前 envelope 记录 metadata、turn 边界、sample commits/usages、四类 ledger records 和 tool-output commit
- **THEN** 每个已知 kind 均由唯一当前 typed codec 校验

#### Scenario: Encode the initial record vocabulary
- **WHEN** 一个root thread完成包含搜索、Patch与Exec的当前Tool Loop
- **THEN** 当前envelope记录全部恢复必需facts，且不存在optional或版本分派字段

#### Scenario: Encode a denied patch
- **WHEN** 用户拒绝一个需要 approval 的 Patch
- **THEN** journal 在该 call slot 保存 matching approval decision 与 denied result，且不存在 execution-started record
- **THEN** approval overlay、完整 diff 和按键不会写入 journal

#### Scenario: Encode a denied command
- **WHEN** 用户拒绝一个需要approval的Exec
- **THEN** journal在该call slot保存matching approval decision与denied result，且不存在execution-started record或进程信息

#### Scenario: Reject an unknown record
- **WHEN** 程序读取 checksum 合法但 event kind 或 payload canary 未知的 record
- **THEN** loader 在 repair、append、executor 和 Provider 副作用前失败
- **THEN** payload 不进入日志、错误或 optional 透传列表

#### Scenario: Preserve an unknown optional record
- **WHEN** journal包含旧开发期`optional` record或当前程序未知的record
- **THEN** loader不再保留或忽略它，而是在任何副作用前拒绝整个journal

#### Scenario: Represent multiple samples in one turn
- **WHEN** Tool Loop 在同一 turn 记录多次 Provider samples
- **THEN** 每个 sample 按 seq 追加配对 native commit 和 sample usage，无需改变当前 envelope

#### Scenario: Represent multiple samples in one future turn
- **WHEN** Tool Loop在同一turn记录多次Provider samples
- **THEN** 每个sample复用唯一当前envelope和codec，并保持seq与usage配对

#### Scenario: Commit a tool result before the next sample
- **WHEN** 当前 tool results 已 durable 且下一 sample 尚未发起
- **THEN** tool-output native commit 单独持久化且不伪造 sample usage
- **THEN** 恢复无需重复文件或进程I/O即可补齐或验证输出

#### Scenario: Commit a future tool result before the next sample
- **WHEN** 当前tool results已durable且下一sample尚未发起
- **THEN** tool-output native commit单独持久化且不伪造sample usage
- **THEN** 恢复无需重复工具I/O即可补齐或验证output

#### Scenario: Keep native tool pairs separate from execution state
- **WHEN** sample native commit 包含原生 function、custom 或 tool-use calls
- **THEN** Provider codec 保留原生内容与 wire kind，而 ready/approval/started/result 状态只由独立 typed ledger records 表达

#### Scenario: Keep native tool pairs separate from approval and execution state
- **WHEN** sample native commit 包含原生 function、custom 或 tool-use calls
- **THEN** Provider codec 保留原生内容与 wire kind，而 ready/approval/started/result 状态只由独立 typed ledger records 表达

#### Scenario: Preserve a future native tool pair without conflating execution state
- **WHEN** sample native commit包含原生tool calls
- **THEN** Provider codec保留原生内容，而当前typed ledger独立表达approval与执行状态

#### Scenario: Persist the requested Write content only
- **WHEN** Write ready 与 result 写入当前 journal
- **THEN** ready 包含完整必要参数，result 只含有界摘要与结果 metadata
- **THEN** 不另写磁盘旧文件、Read observation、approval preview 或暂存资源名

ReadArtifact result SHALL 保存冻结页 preview、artifact ID、实际 offset/next_offset、stored EOF 和 source_complete；不得保存宿主路径。Exec 保存状态 MUST 是必需当前字段，complete/partial 必须匹配合法 descriptor，not_needed/unavailable 不得伪造引用。

#### Scenario: Reject inconsistent artifact metadata
- **WHEN** checksum 合法但 descriptor 的 thread/invocation/stream、digest、长度或状态与结果不一致
- **THEN** 当前 decoder/replay 拒绝记录，不访问 artifact 或修改 journal

### Requirement: Record vocabulary evolves without logging UI deltas

JSONL 事实源 SHALL 只持久化恢复、幂等和审计所需的业务边界，不得默认记录 assistant text delta、spinner、窗口尺寸、工具 progress 或其他 UI 瞬态。发布前新增 Tool、MCP、permission、usage、hook、compact 或 subagent 记录时，变更 SHALL 更新唯一当前强类型 event kind 集合并直接替换当前 fixture；不得预留未知 optional record、历史 payload decoder 或 migration fixture。

大工具结果超过行大小边界时，命令输出 SHALL 使用权限受控且具备完整性信息的 artifact，并在 JSONL 中保存有界 typed 引用，而不是放宽所有记录的上限。搜索结果仍只保存 64 KiB 内的冻结模型 preview 和有界 typed metadata，不引入通用 artifact。

#### Scenario: Stream many text deltas
- **WHEN** Provider 在一个 sample 中产生大量 assistant 文本增量后成功完成
- **THEN** Session 保存最终 Provider 原生提交和生命周期边界，而不是为每个文本 delta 追加一条事实记录

#### Scenario: Extend the pre-release vocabulary
- **WHEN** 后续已确认变更引入新的 required fact
- **THEN** 新 kind 复用 envelope、seq、batch、Sync、修复和权限契约，并成为唯一当前 typed vocabulary 的一部分
- **THEN** 仓库不同时保留变更前后的 decoder 或 fixture 树

#### Scenario: Add a future tool result record
- **WHEN** 发布前后续 change 引入新的工具事实
- **THEN** 该事实进入唯一当前 typed vocabulary，并复用 envelope、seq、batch、Sync 与修复契约
- **THEN** 被替换的开发期 reader 和 fixture 不再保留

#### Scenario: Ignore transient tool progress
- **WHEN** 搜索工具在一次执行中访问大量条目
- **THEN** Session 不逐条持久化扫描 progress，只保存最终有界 result fact

#### Scenario: Ignore transient MCP progress
- **WHEN** MCP 工具在一次执行中产生大量仅用于界面的 progress
- **THEN** Session 不逐条持久化这些瞬态更新，只记录未来已批准的最终 typed fact

#### Scenario: Keep output bodies outside records
- **WHEN** Exec 产生大输出并发布 artifact
- **THEN** journal 仅保存有界 preview/descriptor 与原有业务事实，不记录管道 chunk、页读取 UI 状态或整个文件

### Requirement: Current record schema exposes typed construction and decoding

每个当前已知 Session `event_kind` SHALL 具有专属强类型 draft constructor、strict decoder 和 validator。公共 record draft、writer、replay 和 lifecycle 接口 MUST NOT 接受或返回无约束动态值；构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON、canary 不匹配和不满足当前语义的 payload。业务类型和构造器不得用版本后缀复制当前 schema，decoder registry 不得以 payload version 选择实现。

所有当前 record codecs SHALL 深拷贝 preview/input/feedback 等可变 bytes，并验证 identity、wire kind、index、candidate fingerprint、decision、status 与大小边界。本变更 SHALL 一次性重写仓库开发期 fixture，以一套固定当前 fixture 覆盖 text turn、异构并行读取、获批/拒绝 Patch、获批/拒绝/中断Exec、exclusive 屏障、各崩溃补偿与损坏拒绝；实现不得保留旧 Provider payload reader、旧 Tool revision reader、optional record 分支、旧 fixture 兼容或混合 revision 恢复路径。

当前固定 fixture SHALL 覆盖 Write 创建/覆盖成功、准备失败、拒绝、ready/allow/started/result 中断与混合 exclusive 顺序。新增 Write 使用唯一当前 input/result 联合并精确验证 canary；不得给旧 shape 增加 optional 分支。

#### Scenario: Construct every tool record through a typed API
- **WHEN** 调用方创建 Read、Glob、Grep、Patch、Write、Exec、ReadArtifact 的 ready、approval、execution-started 或 result draft
- **THEN** constructor只接收与kind和capability匹配的当前强类型值并立即验证
- **THEN** draft不能被改造成capability、wire kind与payload不匹配的record

#### Scenario: Construct every current record through a typed API
- **WHEN** 调用方创建任一当前record
- **THEN** 对应constructor只接收该kind的强类型字段并立即验证当前canary与语义

#### Scenario: Strictly decode an approval decision
- **WHEN** checksum合法的approval payload包含未知字段、非法outcome、超限反馈或与allow决定并存的反馈
- **THEN** 唯一当前decoder在replay前拒绝记录且错误不回显反馈正文

#### Scenario: Strictly decode a tool result
- **WHEN** checksum合法的result payload包含未知字段、非法status、超限preview或尾随JSON
- **THEN** 唯一当前decoder在replay前拒绝记录且错误不回显preview正文

#### Scenario: Strictly decode a known payload revision
- **WHEN** 任一checksum合法的当前payload包含未知字段、尾随JSON或非法语义
- **THEN** 唯一当前decoder在replay前拒绝且错误不包含payload正文

#### Scenario: Strictly decode an Exec result
- **WHEN** checksum合法的Exec result包含PID、绝对workdir、环境值、超限preview、未知字段或非法状态组合
- **THEN** 唯一当前decoder在replay前拒绝记录且错误不回显payload正文

#### Scenario: Replay the current immutable tool fixture
- **WHEN** 当前实现加载仓库内固定的含Exec异构Tool Loop fixture
- **THEN** 生产Loader、codec和ReplayPlanner产生预期approval、ledger、顺序、wire kind与native commit摘要
- **THEN** fixture不是测试运行时由当前encoder生成

#### Scenario: Replay the current immutable patch fixture
- **WHEN** 当前实现加载仓库内固定的获批Patch与中断补偿fixture
- **THEN** 生产Loader、codec和ReplayPlanner产生预期ledger、顺序、wire kind与native commit摘要
- **THEN** fixture不是测试运行时由当前encoder生成

#### Scenario: Reject a superseded development fixture
- **WHEN** Loader读取已被本变更替换的envelope、Provider payload或Tool ledger shape
- **THEN** 恢复在repair、append、executor或Provider网络调用前失败，不进入兼容分支

#### Scenario: Reject a mismatched Write result
- **WHEN** checksum 合法但 Write result 带 Read/Patch metadata、错误 wire kind 或缺失当前必需字段
- **THEN** strict decoder/replay 拒绝，不进入兼容路径

#### Scenario: Load fixed Write bytes
- **WHEN** 测试读取仓库保存的 Write current fixtures
- **THEN** 使用生产 loader/replay 验证预期调用、审批与结果；测试运行时不调用当前 encoder 生成 fixture

本变更 SHALL 更新唯一当前 canary 并逐字节固定 Exec 保存状态与 ReadArtifact 的 current fixtures；缺少保存字段的旧开发 shape MUST 在 repair/append 前失败，不保留兼容 reader。

#### Scenario: Load fixed artifact fixtures
- **WHEN** 加载仓库内七工具、partial/unavailable 和恢复中断的固定 bytes
- **THEN** 生产 decoder/replay 校验通过；测试运行时不使用当前 encoder 生成输入 fixture

### Requirement: Sample usage is committed atomically with its sample

每个成功 Provider sample SHALL 在一个 durable batch 中依次写入 `provider_native_commit`、`sample_usage` 以及该 sample 的后续边界。无 tool call 的最终 sample 后续边界为 `turn_completed`；含 tool calls 的 sample 后续边界为一个或多个按 call index 排列的 `tool_call_ready`，输入联合覆盖Read、Glob、Grep、Patch、Write、Exec与ReadArtifact。同一 batch MUST 使用相同 session/thread/turn 和 batch identity并获得连续seq；只有整个batch成功Sync后才能确认任一事实或读取Patch目标。

tool-output native commit MUST 在全部对应 result facts 已按 call index durable 后单独提交且不得带 `sample_usage`。`sample_usage` payload继续完整包含五个normalized metrics，不得包含Provider raw usage、价格、请求正文、tool result、approval或diff。

Write 的完整参数 SHALL 在该原子 batch 内验证并保存。多个 Write 的聚合原生记录或 ready record 超过既有上限时，系统 MUST 在任何 workspace I/O 前失败，不扩大通用 Session 限额。

#### Scenario: Commit a heterogeneous call sample
- **WHEN** completed sample按顺序包含Glob、Patch与Read
- **THEN** journal原子追加native commit、sample usage和三个ready records
- **THEN** candidate preparation与executor只在整个batch Sync后才可发生

#### Scenario: Commit a new sample usage batch
- **WHEN** completed sample不含tool calls
- **THEN** journal原子追加native commit、sample usage和turn completion

#### Scenario: Fail an incomplete usage batch
- **WHEN** 当前尾部sample batch缺少usage、ready或completion中的任一必需record
- **THEN** Loader按既有batch repair规则移除整个未完成batch，不恢复部分history、usage或call

#### Scenario: Fail closed on the superseded development baseline
- **WHEN** Loader读取缺少sample usage或使用旧Provider/Tool payload shape的开发期fixture
- **THEN** resume在repair、append、executor或Provider调用前失败，不保留兼容reader

#### Scenario: Fail an incomplete new usage batch
- **WHEN** 当前尾部sample batch缺少usage、ready或completion中的任一必需record
- **THEN** Loader按batch repair规则移除整个未完成batch，不恢复部分事实

#### Scenario: Commit an heterogeneous call sample
- **WHEN** completed sample按顺序包含Glob、Grep、Read与Patch
- **THEN** journal原子追加native commit、sample usage和四个ready records
- **THEN** 任一candidate preparation或executor只在整个batch Sync后发生

#### Scenario: Commit a call sample
- **WHEN** completed sample按顺序包含两个Read calls
- **THEN** journal原子追加native commit、sample usage和两个ready records
- **THEN** executor只在整个batch Sync后才可接收调用

#### Scenario: Reject usage on a tool-output commit
- **WHEN** journal把tool-output commit与sample usage配对
- **THEN** ReplayPlanner拒绝该状态而不是把output误计为Provider sample

#### Scenario: Reject an oversized Write sample before I/O
- **WHEN** 多个各自合法 Write 的聚合 native commit 超过当前 record 预算
- **THEN** sample 无成功提交，零候选读取与零文件/目录写入
