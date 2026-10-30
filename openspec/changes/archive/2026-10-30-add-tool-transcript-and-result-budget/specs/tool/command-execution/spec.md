## MODIFIED Requirements

### Requirement: Every command is approved as an immutable candidate

合法 ready call durable 后，系统 SHALL 形成包含 capability、规范化 command、workspace 相对 workdir、冻结 timeout、shell profile identity、environment policy revision、sandbox profile、内存/模型预算、保存策略 identity 与每流/调用/thread/root 保存上限的不可变候选。candidate fingerprint MUST 由这些稳定字段的 canonical bytes 派生，不得包含绝对 workspace、实际环境值、随机 identity、时间或 UI 状态。

每个 `process.exec` 候选 SHALL 返回 `ask`，当前 change 不提供自动允许、session grant、持久 allowlist 或按命令字符串推断只读的例外。approval request MUST 只允许 `allow_once`、`deny` 和 `abort`，并提供完整命令、相对 workdir、timeout、私有输出保存与配额摘要及安全能力摘要；用户不能原地修改命令。批准只证明用户同意候选意图，MUST NOT 扩大 sandbox、环境、网络或路径能力。

#### Scenario: Ask before running a command
- **WHEN** 一个合法命令 ready fact 已 durable 且候选构造成功
- **THEN** Runtime 发布绑定 invocation、candidate fingerprint 和完整命令展示的 approval request
- **THEN** approval decision 与 execution started durable 前不存在子进程

#### Scenario: Reject a stale command approval
- **WHEN** response 的 request、invocation 或 candidate fingerprint 与活动命令不匹配
- **THEN** Runtime 在写 approval fact 或启动进程前拒绝 response，活动候选保持不变

#### Scenario: Do not treat approval as sandbox bypass
- **WHEN** 用户批准命令但当前平台不能满足候选声明的 sandbox profile
- **THEN** 命令以 `sandbox_unavailable` 失败且没有 unsandboxed fallback

#### Scenario: Bind output saving to approval
- **WHEN** 审批后保存策略或任一预算改变
- **THEN** 原 candidate fingerprint 不再匹配，系统 MUST 拒绝用旧批准执行新候选

Exec 独占候选 SHALL 在审批前取得本次实际 allowance，将其纳入模型预算与 fingerprint；审批后不可因其他调用预算重新分配而修改。预算不再允许新准入时 MUST 不创建审批或子进程。

### Requirement: Command output is bounded once and projected safely

stdout 与 stderr SHALL 独立计数并保留流 identity；TUI progress可按owner观察到的chunk顺序展示瞬态近期输出，但该跨流交错顺序不得持久化或影响模型preview。每个流的内存捕获硬上限 SHALL 为 512 KiB，聚合最多1 MiB；超过预算时分别采用确定性 head/tail 保留并记录 observed bytes、retained bytes、omitted bytes 和 truncated 状态。模型 preview MUST按固定stdout后stderr顺序在冻结结果时一次生成，UTF-8 bytes 不超过 256 KiB，并以稳定省略标记明确各流中间丢弃字节；resume 不得按新预算重新计算。

成功 result SHALL 区分 exit code 0 与非零退出；非零退出属于已确定命令结果而不是基础设施失败。错误和 metadata 不得包含绝对路径、完整环境、底层 cause、secret 或未授权输出。系统 SHALL 从首字节暂存各流；输出可完整内联时不长期保存，否则按 session/output-artifacts 发布有界连续前缀。模型预览 SHALL 包含保存状态、可用 artifact ID、保存长度及 partial 提示，并把这些信息计入 256 KiB 上限；内存省略不等于磁盘丢失，保存失败不改变确定的进程 exit。

#### Scenario: Preserve the start and end of large output
- **WHEN** stdout/stderr 聚合输出超过 1 MiB
- **THEN** result 确定性保留稳定头尾、报告 omitted bytes，内存和模型 preview 均不超过各自预算

#### Scenario: Distinguish a non-zero exit from executor failure
- **WHEN** sandbox 中的命令正常退出且 exit code 为 2
- **THEN** result 表示已完成的非零退出并保留有界输出，而不伪造 `process_start_failed`

#### Scenario: Restore frozen preview bytes
- **WHEN** 命令 result 已 durable 但 Provider-native output commit 前进程退出
- **THEN** resume 从 Session 中逐字节恢复相同 preview 与非敏感 metadata，不重新执行命令或读取 workspace

#### Scenario: Report independently bounded output
- **WHEN** 一条命令输出超过内存预算但未超过磁盘预算
- **THEN** 预览省略中间内容且引用 complete artifact；ReadArtifact 可读取内存已省略的内容

#### Scenario: Keep process outcome on disk failure
- **WHEN** 输出写入达到上限或发生磁盘错误而命令 exit 为 0
- **THEN** 结果保留 exit 0，单独报告 partial 或 unavailable，继续排空且预览不超限

256 KiB SHALL 仅是单调用硬上限，最终渲染使用候选中冻结的实际 allowance，头尾、省略说明和 artifact 引用一并计入。capture needed SHALL 按该实际额度决定，不得先以硬上限判定 not_needed 并删除输出，再因 sample/turn 聚合预算丢失内容。

#### Scenario: Save output reduced by aggregate budget
- **WHEN** 命令输出 32 KiB 而候选实际 allowance 为 8 KiB
- **THEN** 最终 preview 不超 8 KiB，省略内容按保存策略发布，不因低于 256 KiB 宣称 not_needed

### Requirement: Command activity supports Claude-style host presentation

Runtime SHALL 发布强类型的统一 tool ready/started/progress/terminal 活动投影及独立 approval requested/resolved 投影，替换旧命令专用活动事件；除既有 approval/result durable facts外，高频 progress 不得写入 JSONL Session。progress MUST 绑定合法 session/thread/turn/invocation identity，携带规范化命令摘要、相对 workdir、elapsed time、累计行/字节、truncated 状态和有界近期输出，不得携带完整环境、绝对路径、PID、Provider wire 或未截断输出。

TUI SHALL 以 Claude Code 风格显示 Shell、紧凑命令、Waiting/Running、至多三行近期输出和最终 exit/timeout/cancelled/truncated 状态；完整命令、相对工作目录、耗时与精确输出规模可在详情查看；普通视图不得显示 ledger、sandbox 内部类型或 invocation ID。无交互headless SHALL按approval契约拒绝Exec；`--print`继续只在turn durable完成后输出最终assistant文本，JSONL模式不输出命令progress或内部RuntimeEvent。

#### Scenario: Render an active command
- **WHEN** 已批准命令正在输出内容
- **THEN** TUI 主线显示命令摘要、Running 和有界近期输出，详情展示相对 workdir、elapsed time 与精确规模
- **THEN** composer 不允许开始并发 turn但定向取消仍可用

#### Scenario: Keep print output clean
- **WHEN** `--print` turn 执行一个命令后得到最终 assistant 回答
- **THEN** stdout 只包含 durable 完成的最终回答，不包含 approval、progress、命令输出或内部事件

#### Scenario: Do not persist progress frames
- **WHEN** 长命令产生多个 progress 更新
- **THEN** Session 只保存既有 ready、approval、started 和最终 result facts，不保存 spinner、elapsed tick 或输出 delta

terminal SHALL 携带冻结的各流保存状态和授权 descriptor，不携带磁盘路径；用户可通过宿主只读查询获取详情。headless 外部 JSONL v1 MUST 保持原输出边界。

#### Scenario: Present saving separately
- **WHEN** 命令完成且 stdout 仅保存前段
- **THEN** 主线明确显示部分保存，详情展示保存长度；不把保存失败显示为命令执行失败

progress SHALL 是仅对 Exec 合法的有界变体；ready/started/terminal 以对应 durable 事实为依据。显示名 Shell 不随 Provider facade 改变。完整 transcript 快捷键与七工具主线遵守 tui/tool-transcript，不维护旧命令浏览入口。
