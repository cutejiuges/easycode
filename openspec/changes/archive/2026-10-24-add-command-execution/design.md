## Context

现有 Tool Loop 已把 Provider-native call、typed ready invocation、Session ledger、approval、execution admission、executor 和 Provider-native output 分开，并实现 Read/Glob/Grep 的有序并行与 Patch 的 exclusive 屏障。`process.exec` 可以复用这些事实边界，但进程同时引入 shell、环境、sandbox、子进程树、双输出流、timeout 和 shutdown ownership，不能把 `os/exec.Cmd` 直接塞进 Runtime 分支。

产品交互以 Claude Code 2.1.88 的 Bash 行为为基准：命令本身是用户关注对象，普通视图紧凑，approval、Waiting/Running、近期输出、耗时、输出规模和终态在同一时间线中清晰呈现。能力内核同时吸收 Claude Code 的命令语义/权限体验与 Codex 指定提交 `c248f6d48b` 的 approval/sandbox/runtime 分层、process owner、进程组终止和 head/tail 输出策略。参考工程均只读，不复制其单体 Tool 或 Responses 专用模型。

当前开发与验证平台是 macOS；仓库既有安全文件能力已采用“支持平台提供等价实现，否则 fail closed”的方式。命令首版沿用这个策略：Darwin 提供经过测试的最小 sandbox/process-group adapter，其他平台先返回明确 unsupported，不能退化为普通 `cmd.Run`。

## Goals / Non-Goals

**Goals:**

- 在不破坏双 Provider native history 的前提下增加一个共享、强类型、可恢复的前台命令能力。
- 让 approval、sandbox 和实际进程生命周期分别可测试，用户批准不能成为绕过内核边界的开关。
- 为每个 started 命令建立唯一 cleanup owner 和最终 done 信号，覆盖成功、非零退出、启动失败、取消、timeout、终止升级与不确定结果。
- 以固定预算冻结模型 preview 和 Session result，同时提供不进入事实源的低频有界进度。
- TUI 达到 Claude Code 风格的命令审批与运行时间线，不把 Codex/EasyCode 内部协议暴露给用户。

**Non-Goals:**

- 不实现后台进程、PTY、stdin 交互、process handle、自动后台化或 shell snapshot/fork。
- 不实现基于 shell 文本分类的自动允许、session grant、持久 allowlist或sandbox escalation retry。
- 不把被省略的完整输出落盘；通用 artifact 与恢复权限留给 `add-tool-result-budget`。
- 不承诺 Windows/Linux 执行；unsupported adapter保持可编译并失败关闭，后续 change 需证明等价 sandbox、进程树终止和故障测试。

## Decisions

### 1. 一个共享 capability，两个原生 facade

共享层新增 `process.exec`、`ExecInput`、`ExecInvocation`、`ExecCandidate`、`ExecResult` 与 `ExecMetadata`。Anthropic catalog 暴露 structured `Bash`，OpenAI catalog 暴露 structured `exec_command`；两者保持各自名称、schema、stream reducer、native call/output和golden，但 strict decode 后进入同一个 typed invocation。

选择这一方案是为了让产品心智贴近 Claude Code，同时让OpenAI请求继续使用Codex用户熟悉且语义明确的`exec_command`。统一成一个模型可见名称会损失Provider原生体验；为两边创建两个executor则会复制权限、sandbox与恢复逻辑。

### 2. 输入刻意小于两个参考工程的完整能力

首版输入只有`command`、可选相对`workdir`和`timeout_ms`。shell、login、tty、环境、网络、sandbox override、background、yield和output budget都由宿主策略冻结，不允许模型提供。

Claude Code 的`run_in_background`与Codex的`tty`、`yield_time_ms`、`write_stdin`都依赖长期进程身份和额外权限协议；当前把它们加入schema会违反“未实现能力不得提前暴露”。固定的non-login shell profile由应用Open生命周期解析并注入，`New*`构造器不读取环境或文件系统。

### 3. 候选准备纯内存，安全资源在显式Open/Start阶段取得

Exec ready durable后，Runtime使用已冻结的`ShellProfile`、`EnvironmentPolicy`、`SandboxProfile`与typed input生成纯内存candidate和fingerprint。候选不打开目录、不探测可执行文件、不创建进程。

用户批准并durable记录后，executor的显式`Run`阶段才打开workspace/workdir安全句柄、实例化sandbox命令并启动进程。approval overlay显示candidate中的完整命令、相对workdir、timeout和能力摘要；批准后若实际句柄或sandbox验证失败，执行失败而不是沿用字符串预检结果。

这保留了`New*`纯内存约束，也避免为了生成approval而提前产生外部副作用。

### 4. Approval默认每次询问，不做字符串“安全命令”推断

`process.exec` policy始终为`ask`，decision集合仅`allow_once/deny/abort`。Patch现有session grant不扩展到Exec。shell语法分析未来可以帮助生成风险说明或匹配用户明确配置的规则，但不能证明命令无副作用或代替sandbox。

Claude Code具备成熟的命令规则和模式体系，Codex也支持prefix rule与sandbox escalation；当前EasyCode尚无可信配置snapshot、规则作用域和审计契约。现在引入宽泛`Bash(*)`或字符串白名单会制造持久安全债，因此独立后续change再设计。

### 5. SandboxPlan与ProcessRunner分离

新增小接口：

```text
ExecCandidate
    |
    v
SandboxPlanner --pure--> SandboxPlan
    |
    v
ProcessRunner.Run(ctx, accepted execution)
```

`SandboxPlanner`只组合强类型能力计划；平台adapter把plan转换为实际启动约束。Darwin首版使用系统sandbox wrapper和独立进程组，默认禁止网络，只允许读取系统/toolchain路径、读取workspace并按明确最小范围写workspace与受控临时/缓存目录。profile文本确定生成并接受golden测试，绝对路径只存在于执行边界，不进入catalog、fingerprint、Session或用户错误。

若sandbox executable、profile能力或安全workdir绑定不可用，返回`sandbox_unavailable`/`unsupported_platform`，不自动unsandboxed重试。Codex的“sandbox denial后按policy升级重试”需要新的approval事实和候选fingerprint，本change不隐式采用。

### 6. 环境使用显式allowlist snapshot

应用Open阶段从宿主捕获一次允许的环境名称和值，创建不可变`EnvironmentSnapshot`。默认只保留工具发现与基本locale所需的`PATH`、`LANG`、`LC_*`、`TERM`、`TMPDIR`及经过明确评审的构建缓存变量；显式拒绝API/provider凭据、Authorization/Cookie、代理凭据、Session内部变量和未知变量。runner再覆盖`NO_COLOR=1`、`PAGER=cat`、`GIT_PAGER=cat`等确定值。

candidate fingerprint只包含environment policy revision，不包含动态环境值；实际snapshot也不写Session、日志或错误。这样既避免继承全部进程环境，也不使用完全静态PATH破坏本地toolchain发现。

### 7. ProcessExecution是唯一owner，不暴露OS PID

runner启动后创建一个`ProcessExecution` owner，持有`cmd`、process group、stdout/stderr pipe、reader goroutine、timeout timer、终止升级状态和单一`done` channel。Runtime只等待typed progress与最终result，不得到PID或可复用handle。

状态机为：

```text
prepared -> approval durable -> started durable
                                      |
                                      v
                                  spawning
                                  /      \
                         spawn_failed    running
                                           |
                         +-----------------+-----------------+
                         |                 |                 |
                       exited          cancelled          timeout
                         |                 |                 |
                         +---------- terminate/wait --------+
                                           |
                                      output drained
                                           |
                                     result frozen
```

stdout/stderr reader由owner创建并由owner等待；创建者关闭内部channel。取消或timeout先发送进程组TERM，在确定性可注入的grace boundary后升级KILL，并始终调用Wait。shutdown超时只能触发升级，不能让Runtime放弃owner。测试通过fake clock、fake process与显式barrier注入，不依赖sleep或概率竞态。

#### Darwin清理保证的修订

本机验证表明，即使Seatbelt使用deny-default且只开放process-exec/process-fork和基本读取，子进程仍可setsid脱离原进程组。进程组信号与sandbox继承不能证明任意子孙进程已经退出；遍历PID/PPID也存在重归属、PID复用和枚举竞态，因此不作为完成证明。

首版明确采用前台进程组生命周期保证：owner负责直接子进程的唯一Wait、原进程组的TERM/KILL升级及自身reader/pipe/timer清理。直接子进程正常退出后也检查并清理原组残留成员。原组退出与reader收口均完成后才发布正常终态。进程组探测只用于当前owner生命周期，恢复不探测PID。

主动setsid/setpgid、double-fork或daemon化不属于支持的执行模式，审批能力摘要明确告知“仅支持前台命令；主动脱离的进程可能继续运行”。关闭所有输出后脱离的进程无法被可靠检测，正常终态只表示直接命令及受控资源完成，不声称任意子孙均已结束；sandbox文件/网络限制仍由继承策略保证。这是首版公开限制，不通过shell字符串黑名单伪装成强制边界。需要任意进程树强制回收的场景须另行引入可验证的进程容器方案。

直接命令退出或KILL升级后，输出排空使用独立有界期限。期限到达由owner关闭其读端并等待reader退出，冻结已捕获字节，结果为outcome_uncertain（output_cleanup_incomplete），不得把主动关闭pipe解释为writer退出。原组存活、探测错误或终止失败同样进入outcome_uncertain并poison Session。结果与cleanup done分开：无法回收的直接子进程仍由唯一reaper持有，done未完成前不得关闭writer或释放资源所有权；shutdown期限只触发升级，不承诺不可中断内核调用有有限完成时间。

测试必须覆盖两种setsid子进程：保留输出导致排空超时，以及关闭输出后仍存活但处于明确不支持范围。测试夹具通过独立控制通道自行回收这两类子进程并Wait，避免测试泄漏，也不得将夹具的回收能力误记为产品保证。

#### 退出观察与身份保留

直接调用Wait会回收leader并释放PID；之后按旧PGID探测或发信号可能命中复用编号的无关进程。因此process handle将非回收的退出观察与最终Wait拆分。owner在所有组信号和组探测结束前持有未回收leader；即使其已退出，也不提前消费退出状态。最后执行唯一Wait并关闭观察器，之后绝不再使用旧PID/PGID。观察失败同样保持身份、尝试受控终止并最终Wait，结果失败关闭。

Darwin在内部helper的启动屏障放行前注册kqueue NOTE_EXIT观察器；helper只在收到放行字节后执行sandbox命令，避免极短命令先退出导致观察注册丢失。observer只报告退出、不回收进程。原组查询只用于确认该受控组是否还有非僵尸成员，查询期间leader身份仍被保留；不遍历PID/PPID寻找脱离子孙、不根据枚举结果逐PID发信号，也不声称完整进程树保证。TERM/KILL后采用有界、可注入时钟的组收口观察；无法确认则outcome_uncertain，不在回收后重试信号。

fake process必须在Wait时模拟编号立即复用，并断言此后组探测和信号次数为零；覆盖正常退出、取消、timeout、observer失败和组清理失败。真实Darwin测试验证启动屏障、退出观察先于Wait、Wait恰好一次及清理后句柄拒绝旧身份操作。

### 8. 输出分别截断，最终preview固定排序

stdout、stderr各使用512 KiB head/tail buffer，保留稳定前缀、稳定后缀、observed/retained/omitted bytes和UTF-8修复状态。两个reader并发排空；跨fd的操作系统观察顺序只用于瞬态TUI，不写Session，也不影响模型结果。

最终preview固定按stdout后stderr编码，总上限256 KiB，分别插入英文省略标记，附加稳定exit/timeout/cancelled摘要。result一旦生成即深拷贝并作为Session事实保存，Provider codec只编码冻结preview。该方案结合Claude Code的大输出可理解体验与Codex的head/tail预算，同时避免把调度不确定性带入resume bytes。

当前不保存完整输出artifact：超过1 MiB捕获预算的中间内容明确丢弃。以后artifact change必须在同一次结果归一化中写入权限受控对象，不能通过恢复时重跑命令补齐。

### 9. Progress使用单槽合并，不阻塞pipe drain

ProcessExecution维护受锁保护的有界progress snapshot和容量1的notify channel。reader更新buffer/计数后执行非阻塞notify；Runtime收到通知后复制snapshot并投影typed event。慢TUI只会合并中间帧，不会阻塞stdout/stderr排空。锁内不发送阻塞channel、不调用Emitter、不Wait、不Close。

Runtime保证started/progress/terminal顺序；terminal前读取最终snapshot，关闭progress owner并等待reader完成。Session只记录ready/approval/started/result，TUI tick、elapsed和output chunk不进入JSONL。headless因Exec approval失败关闭，不产生started/progress事件，也不扩展当前JSONL v1。

### 10. Runtime沿用existing exclusive barrier与ledger

Exec与Patch都属于exclusive。Scheduler不创建通用“并行进程”模式：前序parallel-read results durable后，Runtime顺序处理Exec candidate、approval与started；ProcessExecution done且result durable后才越过屏障。result按原call index与其他calls一起编码Provider outputs。

恢复规则沿用at-most-once：ready无approval、allow无started、started无result分别补偿为interrupted-before-approval、interrupted-before-execution和outcome-uncertain。Session永远不扫描PID或尝试连接崩溃前进程。

### 11. TUI复用approval控制面但使用命令专用presentation

Runtime approval envelope继续提供统一identity、fingerprint与decision集合，但presentation改为封闭联合：Patch携带diff，Exec携带command、relative workdir、timeout与能力摘要。TUI根据typed presentation选择overlay，不能靠capability字符串解析payload。

Exec timeline普通视图最多显示两行/160 code points命令；Waiting、Running、recent output、elapsed/bytes及终态遵循Claude Code信息层级。当前仓库没有现成命令详情，本变更新增Ctrl+O打开/关闭最近命令详情，支持PgUp/PgDn与resize；Esc关闭详情，Ctrl+C在命令活动时仍定向中断，详情内普通字符不修改composer。审批默认No，composer在modal或running期间保持草稿且不能提交。

详情的完整命令复用已校验approval候选，按invocation identity绑定，不反向读取Session、executor或Provider native数据。完整结果来自command terminal专属的typed result preview（UTF-8上限256 KiB），与durable tool result逐字节一致；started/progress不得携带该字段，运行中详情仅显示既有有界recent输出。终态preview不增加Session record、不扩展headless JSONL，也不改变Provider output或cache fingerprint。UI只对显示副本清理终端控制字符，不改动候选或冻结结果。固定尺寸snapshot覆盖normal/small、scroll、cancel、timeout、non-zero与truncated及详情键盘路由。

### 12. 当前schema直接替换，不保留兼容reader

`CapabilityID`、Invocation、ReadyCall、Result、Session payload与Provider native tool联合直接加入Exec分支；current fixture逐字节重写。没有`ExecV2`、legacy decoder、optional record或旧catalog分支。catalog变化必须同步context estimate、cache fingerprint与双Provider request golden。

## Risks / Trade-offs

- [macOS系统sandbox能力在未来系统版本变化] → adapter启动前验证能力，集成测试固定允许/拒绝场景；不可兑现时失败关闭，不自动裸跑。
- [最小环境导致部分项目工具找不到或缺少配置] → 首版明确记录allowlist并返回安全诊断；扩展环境必须走单独安全评审，不回退到`os.Environ()`。
- [允许workspace写意味着命令可以产生广泛修改] → 每次命令显示完整内容并单次批准，sandbox仍禁止workspace外写和网络；Git/文件审计由用户与后续能力承担。
- [TERM/KILL仍无法确认某些内核态进程结果] → 进入`outcome_uncertain`、poison Session并保留cleanup ownership，绝不自动重试。
- [progress更新过多拖慢TUI] → 单槽合并、固定最大snapshot和非阻塞notify；最终result不依赖交付了多少progress帧。
- [stdout/stderr跨流交错不可确定] → 瞬态UI可按观察顺序显示，durable preview固定stdout后stderr，恢复bytes保持稳定。
- [首版仅Darwin降低跨平台可用性] → build-tag unsupported adapter保证明确错误；P8或后续P3 change为Linux/Windows分别提供等价sandbox和进程树测试，禁止弱降级。

## Migration Plan

1. 在唯一当前Tool/Session/Provider契约中加入Exec并同步current fixture，不保留旧开发shape。
2. 先落地纯逻辑输入、catalog、candidate、result和buffer测试，再接双Provider wire与native restore。
3. 接入Darwin sandbox/process owner和确定性故障注入；unsupported平台保持编译与失败关闭测试。
4. 接入Runtime durable/approval/exclusive/progress，再接TUI presentation和应用装配。
5. 更新context/cache golden、双Provider请求/流式fixture、Session字节fixture、端到端与文档，最后执行`make verify`。

若apply无法完成sandbox强制、进程组cleanup或双Provider恢复等价中的任一项，回退方式是删除本change新增的Exec current shape并恢复原fixture；不得保留只暴露schema或会unsandboxed运行的半成品能力。
