## MODIFIED Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL record SHALL 使用唯一当前、强类型的 envelope，至少包含一个 schema canary、一个 payload canary、单调 seq、UTC timestamp、session/thread/turn identity、event kind、batch 边界、checksum 和受控 typed payload。所有当前 records 均为恢复必需事实；envelope MUST NOT 携带 `required`/`optional` 选择字段，也不得透传未知 kind 或未知 payload 为 opaque Session record。

当前记录词汇中的 `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 和 `tool_call_result` SHALL 具有专属 typed constructor、strict decoder 与 validator。`tool_call_ready` SHALL 保存 invocation ID、Provider call ID、Provider wire kind、sample/call index、capability，以及与 `Read`、`Glob`、`Grep`、`Patch` 或 `Exec` 匹配的完整 typed input；`tool_approval_decided` SHALL 保存 request/invocation identity、candidate fingerprint、typed outcome、decision source、仅适用于 deny 的有界反馈与不含绝对路径、正文、环境值或命令输出的安全 metadata；`tool_execution_started` SHALL 引用同一 invocation 并表示 executor 接收线性化点；`tool_call_result` SHALL 保存终态 status、稳定 code、完整有界模型 preview 与 capability 对应的非敏感 typed metadata。Exec metadata SHALL只包含相对workdir、exit/timeout/cancelled/truncated状态、duration及stdout/stderr计数，不得包含PID、绝对路径或环境。工具 records MUST NOT 保存 input revision、result codec revision、完整 base/final 文件、完整 diff、临时文件路径或可执行 Provider wire item。

schema/payload canary 只用于精确验证当前格式，不得选择历史 decoder。completed sample 的 Provider entry 仍作为 native commit 并紧邻配对 `sample_usage`；tool results 编码为 tool-output native commit 时不生成 sample usage。高频参数 delta、approval request/焦点/按键、命令输出progress、elapsed tick、UI 状态和文件系统瞬时信息 MUST NOT 进入 JSONL。

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

### Requirement: Current record schema exposes typed construction and decoding

每个当前已知 Session `event_kind` SHALL 具有专属强类型 draft constructor、strict decoder 和 validator。公共 record draft、writer、replay 和 lifecycle 接口 MUST NOT 接受或返回无约束动态值；构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON、canary 不匹配和不满足当前语义的 payload。业务类型和构造器不得用版本后缀复制当前 schema，decoder registry 不得以 payload version 选择实现。

所有当前 record codecs SHALL 深拷贝 preview/input/feedback 等可变 bytes，并验证 identity、wire kind、index、candidate fingerprint、decision、status 与大小边界。本变更 SHALL 一次性重写仓库开发期 fixture，以一套固定当前 fixture 覆盖 text turn、异构并行读取、获批/拒绝 Patch、获批/拒绝/中断Exec、exclusive 屏障、各崩溃补偿与损坏拒绝；实现不得保留旧 Provider payload reader、旧 Tool revision reader、optional record 分支、旧 fixture 兼容或混合 revision 恢复路径。

#### Scenario: Construct every tool record through a typed API
- **WHEN** 调用方创建 Read、Glob、Grep、Patch、Exec 的 ready、approval、execution-started 或 result draft
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

## ADDED Requirements

### Requirement: Replay never treats command progress as durable state

ReplayPlanner SHALL只从Exec ready、approval、started、result和Provider-native output判断命令状态。命令progress、PID、spinner、实时输出chunk与后台handle不属于当前record vocabulary；即使宿主曾观察这些瞬态数据，resume也不得据此推断命令仍运行或尝试连接进程。

#### Scenario: Resume after a process crash
- **WHEN** journal包含Exec started但没有result且进程已经退出或状态未知
- **THEN** replay生成`outcome_uncertain`补偿而不查找PID、不连接进程、不重新执行命令
