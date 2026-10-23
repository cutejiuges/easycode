## Purpose

定义双 Provider 共用的前台命令执行能力，使模型能够在明确批准和强制 sandbox 边界内运行构建、测试与诊断命令，并获得可取消、可恢复、输出有界且适合 Claude Code 风格交互展示的确定结果。

## ADDED Requirements

### Requirement: Command execution has Provider-native deterministic facades

当前 Tool Catalog SHALL 增加唯一共享 capability `process.exec`。Anthropic facade MUST 名为 structured `Bash`，OpenAI facade MUST 名为 structured `exec_command`；两者 SHALL 路由到同一强类型命令输入、policy、sandbox planner、executor 与结果语义，但各自 request compiler、stream reducer、native persistence/restore 和 result codec MUST 保留 Provider 原生 call/output。

输入 SHALL 包含必需的非空 `command`，以及可选的 workspace 相对 `workdir` 和 `timeout_ms`。`command` UTF-8 bytes MUST 不超过 64 KiB；`workdir` MUST 经过规范化且不能为绝对路径、包含 `..`、NUL 或逃逸 workspace；timeout 默认 120,000 ms，合法范围为 1,000 至 600,000 ms。未知字段、重复字段、尾随 JSON、非法 UTF-8、空命令和超限值 MUST 在 approval、进程或文件系统 I/O 前失败。

Facade description、schema canonical bytes、排序和 fingerprint MUST 确定稳定，不得包含 cwd 绝对路径、shell 路径、环境变量、sandbox 临时状态、approval/invocation identity 或时间。只有双 Provider 全链路、policy、sandbox、executor、result codec、presenter 和测试全部存在时才可暴露该能力。

#### Scenario: Expose Provider-specific command facades
- **WHEN** 分别读取 Anthropic 与 OpenAI catalog view
- **THEN** 两者都路由到 `process.exec`，Anthropic 名为 `Bash`，OpenAI 名为 `exec_command`
- **THEN** 两个 facade 都使用严格 structured JSON 且具有稳定 canonical definition 和 fingerprint

#### Scenario: Reject invalid command input before side effects
- **WHEN** 命令输入为空、含未知字段、使用绝对 workdir、目录穿越或超限 timeout
- **THEN** sample 以安全英文验证错误失败，且系统不创建 approval、不打开目录、不启动进程

#### Scenario: Hide an incomplete command implementation
- **WHEN** 任一 Provider 缺少 request、reducer、native restore、result codec 或真实 executor
- **THEN** 当前 catalog 构造失败且模型看不到对应命令 facade

### Requirement: Every command is approved as an immutable candidate

合法 ready call durable 后，系统 SHALL 形成包含 capability、规范化 command、workspace 相对 workdir、冻结 timeout、shell profile identity、environment policy revision、sandbox profile 和输出预算的不可变候选。candidate fingerprint MUST 由这些稳定字段的 canonical bytes 派生，不得包含绝对 workspace、实际环境值、随机 identity、时间或 UI 状态。

每个 `process.exec` 候选 SHALL 返回 `ask`，当前 change 不提供自动允许、session grant、持久 allowlist 或按命令字符串推断只读的例外。approval request MUST 只允许 `allow_once`、`deny` 和 `abort`，并提供完整命令、相对 workdir、timeout 与安全能力摘要；用户不能原地修改命令。批准只证明用户同意候选意图，MUST NOT 扩大 sandbox、环境、网络或路径能力。

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

### Requirement: Execution freezes shell, environment, workspace and sandbox

执行准入前 SHALL 冻结非交互 shell profile、workspace 句柄、规范化相对 workdir、最小环境、timeout、输出预算和 sandbox plan。模型不得指定 shell 路径、login shell、任意环境变量、网络权限、sandbox override 或宿主绝对 cwd。子进程环境 MUST 默认排除 API key、Authorization、Cookie、Provider/base URL 凭据、Session 路径及 EasyCode 内部控制变量，并设置确定性的 non-interactive/no-color/pager-disabled 值。

workdir 的 workspace containment、目录类型和 symlink/reparse point 判断 MUST 绑定实际打开的目录句柄；单独字符串规范化或 `Lstat` 后再按路径 `Open` 不构成最终安全检查。首个实现只可在具有确定性进程组终止和已验证 sandbox adapter 的平台执行；其他平台 SHALL 返回 `unsupported_platform`。

#### Scenario: Start with a minimal environment
- **WHEN** 已批准命令通过 sandbox 与 workdir 验证
- **THEN** 子进程只收到策略允许的最小环境且不继承 Provider secret 或 Session 内部路径

#### Scenario: Reject a workdir symlink escape
- **WHEN** 测试在 workdir 检查与进程启动之间把路径组件替换为 workspace 外 symlink
- **THEN** 系统失败或继续使用先前取得的安全目录句柄，子进程不能在外部目录启动

#### Scenario: Fail closed without an enforceable sandbox
- **WHEN** 当前平台或配置无法兑现候选所需的文件、进程或网络限制
- **THEN** executor 不启动命令并返回稳定安全的 typed error

### Requirement: One owner controls process acceptance, output and cleanup

命令 SHALL 使用 `exclusive` 调度。只有 matching approval fact 和 `tool_execution_started` 依次 durable 后，唯一 process owner 才可恰好一次接受并启动命令。owner MUST 同时排空 stdout 与 stderr、等待进程退出、观察 timeout/cancellation、终止整个受控进程组并等待输出 reader 与 cleanup 完成；不得在锁内启动、等待、终止进程或阻塞 channel。

started 被接受前的取消 MUST 产生 cancelled result 且零进程副作用；started 被接受后，即使 context 已取消，owner MUST 调用 executor 恰好一次并返回确定结果。取消或 timeout SHALL 先请求温和终止，再沿有界升级路径强制终止原进程组；正常终态前 MUST 确认直接子进程Wait、原组退出、stdout/stderr reader和owned cleanup完成。正常退出也必须清理原组残留。无法确认终止或 durable result 时 SHALL 返回 `outcome_uncertain` 并 poison 当前 Session，不得宣称失败后可安全重试。

Darwin的进程组与Seatbelt SHALL NOT被声明为任意子孙进程的强制容器。主动setsid/setpgid脱离、double-fork和daemon化不属于支持模式，审批摘要 MUST说明脱离进程可能继续运行；关闭输出且脱离的进程无法可靠检测，正常结果仅代表直接命令与受控资源结束。MUST NOT用PID/PPID扫描或shell文本匹配宣称完整进程树清理。脱离不扩大继承的文件/网络sandbox权限。

直接命令退出或KILL后输出排空 MUST具有独立有界期限；超期由owner关闭读端、等待reader退出并冻结已捕获输出，使用`outcome_uncertain/output_cleanup_incomplete`收口，MUST NOT把关闭pipe视为writer退出。无法Wait的直接进程仍由唯一reaper持有；结果不等于cleanup done，done前不得释放writer或接受新turn。

#### Scenario: Detached writer keeps an output pipe open
- **WHEN** 子进程setsid脱离后保留输出句柄，直接命令退出且输出排空期限到达
- **THEN** owner关闭自身读端并等待reader完成，返回outcome_uncertain且Session失败关闭
- **THEN** 不宣称脱离进程已终止，测试通过独立控制通道回收夹具

#### Scenario: Preserve process identity until group signaling ends
- **WHEN** leader退出但owner尚需探测或终止原进程组
- **THEN** owner只观察退出而不回收leader，直到所有组信号和探测结束后才执行唯一Wait
- **THEN** Wait之后禁止按旧PID/PGID探测或发信号，即使编号立即复用也不会影响无关进程

#### Scenario: Register exit observation before command release
- **WHEN** Darwin adapter启动一个可能立即退出的命令
- **THEN** 内部helper在启动屏障等待，退出观察器注册成功后才允许执行sandbox命令
- **THEN** 注册或放行失败不得执行用户命令，并由唯一owner清理helper和资源

#### Scenario: Detached process closes its output
- **WHEN** 测试子进程setsid并关闭继承输出后仍存活
- **THEN** 测试记录该进程不在原组清理保证内，审批说明与正常结果不得宣称任意子孙退出
- **THEN** 夹具验证继承sandbox限制且自行回收，不将夹具清理当作executor能力

#### Scenario: Complete a foreground command
- **WHEN** 已准入命令正常退出且两个输出流关闭
- **THEN** owner 返回一次包含 exit code、duration、stdout/stderr 统计和终态的 typed result
- **THEN** Runtime durable result 后才把匹配 output 提交给下一 Provider sample

#### Scenario: Cancel before process acceptance
- **WHEN** turn 在 execution started 被接受前取消
- **THEN** invocation 获得 cancelled result且 executor 与 OS process 启动次数均为零

#### Scenario: Kill a timed-out process group
- **WHEN** 命令及其子进程超过冻结 timeout
- **THEN** owner 终止整个受控进程组、排空已产生输出、等待所有 owned cleanup 后返回 `timed_out`

#### Scenario: Fail closed when termination cannot be confirmed
- **WHEN** timeout 或取消后无法确认进程组退出
- **THEN** result 为 `outcome_uncertain`，Session 禁止新的 Provider 或工具副作用且命令不自动重试

### Requirement: Command output is bounded once and projected safely

stdout 与 stderr SHALL 独立计数并保留流 identity；TUI progress可按owner观察到的chunk顺序展示瞬态近期输出，但该跨流交错顺序不得持久化或影响模型preview。每个流的内存捕获硬上限 SHALL 为 512 KiB，聚合最多1 MiB；超过预算时分别采用确定性 head/tail 保留并记录 observed bytes、retained bytes、omitted bytes 和 truncated 状态。模型 preview MUST按固定stdout后stderr顺序在冻结结果时一次生成，UTF-8 bytes 不超过 256 KiB，并以稳定省略标记明确各流中间丢弃字节；resume 不得按新预算重新计算。

成功 result SHALL 区分 exit code 0 与非零退出；非零退出属于已确定命令结果而不是基础设施失败。错误和 metadata 不得包含绝对路径、完整环境、底层 cause、secret 或未授权输出。当前 change 不创建 artifact；超过硬捕获预算的中间 bytes SHALL 丢弃并明确报告，而不是无界写入内存或 Session。

#### Scenario: Preserve the start and end of large output
- **WHEN** stdout/stderr 聚合输出超过 1 MiB
- **THEN** result 确定性保留稳定头尾、报告 omitted bytes，内存和模型 preview 均不超过各自预算

#### Scenario: Distinguish a non-zero exit from executor failure
- **WHEN** sandbox 中的命令正常退出且 exit code 为 2
- **THEN** result 表示已完成的非零退出并保留有界输出，而不伪造 `process_start_failed`

#### Scenario: Restore frozen preview bytes
- **WHEN** 命令 result 已 durable 但 Provider-native output commit 前进程退出
- **THEN** resume 从 Session 中逐字节恢复相同 preview 与非敏感 metadata，不重新执行命令或读取 workspace

### Requirement: Command activity supports Claude-style host presentation

Runtime SHALL 发布强类型、瞬态的 command queued、approval requested/resolved、started、progress 和 terminal 投影；除既有 approval/result durable facts外，高频 progress 不得写入 JSONL Session。progress MUST 绑定合法 session/thread/turn/invocation identity，携带规范化命令摘要、相对 workdir、elapsed time、累计行/字节、truncated 状态和有界近期输出，不得携带完整环境、绝对路径、PID、Provider wire 或未截断输出。

TUI SHALL 以 Claude Code 风格显示紧凑命令、Waiting/Running、相对工作目录、近期输出、耗时、输出规模和最终 exit/timeout/cancelled/truncated 状态；普通视图不得显示 ledger、sandbox 内部类型或 invocation ID。无交互headless SHALL按approval契约拒绝Exec；`--print`继续只在turn durable完成后输出最终assistant文本，JSONL模式不输出命令progress或内部RuntimeEvent。

#### Scenario: Render an active command
- **WHEN** 已批准命令正在输出内容
- **THEN** TUI 显示命令摘要、Running、相对 workdir、elapsed time 和有界近期输出
- **THEN** composer 不允许开始并发 turn但定向取消仍可用

#### Scenario: Keep print output clean
- **WHEN** `--print` turn 执行一个命令后得到最终 assistant 回答
- **THEN** stdout 只包含 durable 完成的最终回答，不包含 approval、progress、命令输出或内部事件

#### Scenario: Do not persist progress frames
- **WHEN** 长命令产生多个 progress 更新
- **THEN** Session 只保存既有 ready、approval、started 和最终 result facts，不保存 spinner、elapsed tick 或输出 delta
