# tool/invocation-loop Specification

## Purpose

定义首个可恢复 Tool Loop 的目录、调用、持久化与多次采样行为，使模型能够安全调用真实能力，同时保持 Provider 原生历史、Session ledger、取消和崩溃恢复的一致性。

## Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次普通 Provider sample SHALL 使用从当前完整工具注册目录派生的不可变声明快照。快照按回合捕获的模式筛选同一套当前定义，实际请求编译与流式参数解码 SHALL 使用同一冻结快照。完整目录中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade及其wire kind、validator、policy、适用的 sandbox planner、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema或freeform定义，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前完整注册目录 SHALL 恰好包含 `fs.read`、`fs.glob`、`fs.grep`、`fs.patch`、`fs.write`、`process.exec`、`artifact.read`、`skill.read`、`user.question` 与 `task.progress`；Anthropic SHALL 将 patch/command 分别暴露为 structured `Edit`/`Bash`，OpenAI SHALL 将同一 patch/command 能力分别暴露为 custom freeform `apply_patch` 与 structured `exec_command`。facade 按稳定 capability ID、wire kind 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical definition bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation/approval ID、权限临时状态、base evidence、diff、命令环境实际值和文件系统枚举顺序 MUST NOT 进入 definition bytes 或 fingerprint。

普通模式（含两个 headless 入口）SHALL 声明完整十项能力。TUI Plan Mode SHALL 恰好声明 `fs.read`、`fs.glob`、`fs.grep`、`artifact.read`、`skill.read`、`user.question` 与 `task.progress`。两种模式下同一能力的 facade 名称、wire kind、schema、description、服务端 schema 模式及本地参数与结果约束 SHALL 相同。声明快照的 canonical bytes、source revision 与 fingerprint MUST 对应实际声明的定义及顺序，普通模式的定义和指纹保持原样。模式筛选只适用于当次采样，Session 恢复及原生历史核验 SHALL 使用完整当前注册目录并保留先前全部已提交的调用与结果。

两个 Provider SHALL 都以 structured Write 暴露 fs.write。Write 的 request、strict decoder、native restore、result codec、候选、policy、executor 和审批展示缺一不可；创建/覆盖证据和 Read observation MUST NOT 进入稳定 definition bytes。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Skill、Read、ReadArtifact、Glob、Grep、Write、Edit、apply_patch、Bash和exec_command描述与定义创建catalog snapshot
- **THEN** facade顺序、wire kind、canonical bytes、source revision和fingerprint完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog尝试注册只有schema或freeform描述而没有匹配executor、validator、policy、sandbox planner或result renderer的facade
- **THEN** snapshot构造失败且该facade不会出现在任一Provider请求中

#### Scenario: Expose the complete current catalog
- **WHEN** Runtime 编译普通模式的 Provider 请求
- **THEN** catalog只声明Read、ReadArtifact、Glob、Grep、Write、对应Provider的Edit或apply_patch以及Bash或exec_command，额外声明 structured Skill、对应 Provider 的 structured 提问 facade 与任务进度 facade，不声明MCP、Agent、后台进程或write_stdin

#### Scenario: Expose the complete read-only catalog
- **WHEN** 将当前catalog中的只读子集投影给审计或测试
- **THEN** 该子集仍恰好包含Read、ReadArtifact、Glob、Grep、Skill，Patch、Write、Exec、Question 与 Task 作为独立 exclusive 能力存在且不冒充只读工具

#### Scenario: Expose only current contracts
- **WHEN** 将当前catalog与早期开发期catalog比较
- **THEN** 当前唯一完整注册目录包含十项能力，Plan Mode 从中派生声明快照，系统不保留旧 catalog、旧 schema 或兼容分支

#### Scenario: Expose Provider-specific mutation wire
- **WHEN** 分别读取 Anthropic 与 OpenAI 的普通模式声明快照
- **THEN** 两者分别路由到相同`fs.patch`与`process.exec`能力，但保持各自Edit/apply_patch与Bash/exec_command facade及wire

#### Scenario: Expose only Read
- **WHEN** 将当前catalog与最早只暴露Read的开发期catalog比较
- **THEN** 当前唯一完整注册目录包含 Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill、Question 与 Task，系统不保留旧 catalog 分支

#### Scenario: Expose Provider-specific patch wire
- **WHEN** 分别读取 Anthropic 与 OpenAI 的普通模式声明快照
- **THEN** 两者都路由到`fs.patch`，但Anthropic使用structured JSON而OpenAI使用custom freeform定义

#### Scenario: Expose structured Write beside native Patch
- **WHEN** 获取两家完整当前 catalog
- **THEN** 两者均有 structured Write，OpenAI 同时保留一个 custom apply_patch
- **THEN** Write 定义顺序与 fingerprint 不受目标存在性和 Read 证据变化影响

两家 Provider SHALL 使用 structured ReadArtifact 暴露 artifact.read；保存路径、ID、计数、动态配额使用量 MUST NOT 进入 catalog definition 或 fingerprint。

#### Scenario: Expose executable artifact reading
- **WHEN** 构造两家 Provider 的普通模式声明快照
- **THEN** 恰好十项能力，ReadArtifact 具有完整 request、reducer、native restore、policy、executor 和 result codec

两家 Provider SHALL 以 structured Skill 暴露 skill.read，具备完整读取、validator、policy、来源范围约束、renderer 与恢复实现。目录为空仍保留真实 executor，未知名称返回安全错误，不暴露未来来源能力。

#### Scenario: Expose executable Skill loading
- **WHEN** 两家 Provider 构造普通模式声明快照
- **THEN** 均恰好十项能力，Skill 的主文档与资源分支都有 typed input/result，定义不包含实际目录正文或绝对路径

user.question SHALL 具有完整 typed input/result、strict decoder、policy、实际交互执行消费者、renderer 和 native restore。Anthropic facade 为 AskUserQuestion，OpenAI facade 为 request_user_input，均为 structured wire。该能力不操作文件、进程或网络，不生成虚构 sandbox 候选或副作用批准；无交互宿主由真实执行路径返回配对不可用结果。目录定义中不得出现动态题目、回答、请求身份、结果余额或 UI 状态。

#### Scenario: Expose executable user questions
- **WHEN** 两家 Provider 构造完整当前 catalog
- **THEN** 均恰好十项真实能力，提问 facade 按既有规则排序，不能只提供 schema 而没有交互、拒绝及恢复消费者

#### Scenario: Keep the read-only subset precise
- **WHEN** 检查可并行读取子集
- **THEN** 仍为 Read、Glob、Grep、ReadArtifact、Skill，提问不因没有文件写入而被放入并行读批次

`task.progress` SHALL 是当前第十项真实能力，Anthropic facade 为 structured `TodoWrite`，OpenAI facade 为 structured `update_plan`。两家 SHALL 使用各自严格参数契约并归一化为同一有界完整清单替换；schema、decoder、validator、结果消费者、展示、持久化及恢复缺一不可。任务能力为无需普通副作用批准的 exclusive 调度屏障，不冒充并行读取或文件写入，不触发 PreToolUse/PostToolUse/PostToolUseFailure。任务标签、activeForm、状态、说明、来源身份与当前清单 MUST NOT 进入静态定义或 catalog fingerprint。

#### Scenario: Expose Provider-specific task progress facades
- **WHEN** 读取两家完整当前 catalog
- **THEN** 恰好十项真实能力，Anthropic 声明 TodoWrite，OpenAI 声明 update_plan，二者映射 task.progress
- **THEN** 改变当前任务清单不改变定义 bytes、source revision 或 fingerprint，五项并行只读子集保持不变


每个 facade SHALL 具有固定且强类型的服务端 schema 模式，参与不可变 catalog 的 canonical definition、source revision/fingerprint 与实际 Provider 请求属性证据。只有 OpenAI update_plan SHALL 显式编译 strict:false；既有 function facade strict 值和 wire bytes 不变，custom apply_patch 不增加该字段，Anthropic 不发送 OpenAI 专属字段。显式 false 必须在序列化中保留，不使用零值省略或服务端默认代替；本地 strict decoder/validator 不因该模式降低要求。服务端模式不属于领域 TaskUpdate 或动态任务状态，不能在请求编译时按字段猜测或只特判名称而让 catalog 证据遗漏策略。

任务工具的原生参数 SHALL 以显式 Provider family/facade profile 进入工具层封闭 TaskInput，持有纯规范领域 TaskUpdate；ready、result 和 native restore evidence SHALL 验证该映射。profile 不存入领域值、当前三态、checkpoint 或 tasks.snapshot，也不得作为版本/历史 reader 分派。静态 description 必须完整披露 tool/task-progress 的数量、UTF-8 字节、原始/规范编码、唯一性、状态、完整替换、清除、跨回合及验证证据边界；schema 字符 maxLength 不能代替 UTF-8 bytes 校验。

#### Scenario: Preserve the explicit nonstrict server mode for update_plan only
- **WHEN** 两家实际请求编译完整十能力目录
- **THEN** OpenAI update_plan 恰好显式 strict:false，其他既有 function strict 值及定义 bytes 保持，custom 与 Anthropic 不带不适用字段；同一模式进入目录和实际请求属性证据

#### Scenario: Reject a facade mode omitted from catalog identity
- **WHEN** facade schema 模式变化但 catalog canonical definition、source revision 或 fingerprint 没有对应变化
- **THEN** 当前目录验证或相应回归断言失败，不让请求编译器与目录证据各自维护矛盾定义

#### Scenario: Validate actual first request definitions before emitting fixtures
- **WHEN** 两家真实 CLI 连接本地协议验证服务并发送首个请求
- **THEN** 服务先核对真实 schema/description、update_plan 的显式 false 与其他工具原模式，再决定是否返回 SSE；错误定义不能借预置成功响应通过验收

#### Scenario: Advertise the seven planning facades without changing their contracts
- **WHEN** Anthropic 与 OpenAI 分别编译同一已冻结 Plan Mode 回合的普通采样请求
- **THEN** 两者都只声明 Read、Glob、Grep、ReadArtifact、Skill 及各自现有的 AskUserQuestion 和 TodoWrite，或 request_user_input 和 update_plan，沿用各自参数、wire 和本地验证规则
- **THEN** 共同 facade 保持原有定义和相对顺序，OpenAI update_plan 保留显式 strict:false，规划快照的定义证据与请求中的七项声明一致

#### Scenario: Keep history independent of advertised tools
- **WHEN** 同一会话已在普通模式提交文件修改或命令调用及配对原生输出，随后在 Plan Mode 续写或退出后重新装载该会话
- **THEN** 当前声明的筛选只影响新采样准入，已经提交的历史调用和配对输出保留完整内容与顺序，恢复按完整当前注册目录验证

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析 capability 以及 `ReadInput`、`GlobInput`、`GrepInput`、`PatchInput`、`WriteInput`、`ExecInput`、`ReadArtifactInput`、`SkillInput`、`QuestionInput` 或任务进度更新封闭联合中的恰好一种；核心导出边界 MUST NOT 接受或返回 `any`、无约束动态 map 或 input revision router。

Anthropic patch与command参数必须由完整structured JSON strict decode；OpenAI patch输入必须由完整custom freeform parser消费，command参数必须由完整structured JSON strict decode。参数delta、未闭合原生item、未在当前回合的采样快照声明的工具、重复Provider call ID、capability与typed input不匹配、未知字段、尾随JSON/patch文本、未支持操作或超限输入 MUST 使sample失败，不得产生ready call、native commit、sample usage、approval或工具I/O。

Write SHALL 只由完整 structured JSON strict decode 产生 ready call；调用不得被转换成 Patch replacement 或 shell 命令。

#### Scenario: Prepare ordered heterogeneous calls
- **WHEN** Provider sample依次完成合法的Glob、Grep、Read、Patch与Exec调用
- **THEN** prepared sample同时包含对应原生calls、normalized usage和五个匹配的typed ready calls
- **THEN** ready calls的位置与原生模型调用顺序一致

#### Scenario: Prepare an ordered Read call
- **WHEN** Provider sample完成一个参数合法的Read调用
- **THEN** prepared sample同时包含对应原生call、normalized usage和一个typed Read ready call

#### Scenario: Prepare Provider-specific patch input
- **WHEN** Anthropic完成structured Edit或OpenAI完成custom apply_patch
- **THEN** 两条wire分别产生匹配同一`fs.patch`能力的typed Patch ready call

#### Scenario: Prepare Provider-specific command input
- **WHEN** Anthropic完成structured Bash或OpenAI完成structured exec_command
- **THEN** 两条wire分别产生匹配同一`process.exec`能力的typed Exec ready call

#### Scenario: Reject partial arguments
- **WHEN** stream在任一搜索、读取、patch或command参数尚未完整时结束，或参数无法strict decode
- **THEN** sample以协议错误失败，Session不写入成功sample facts且所有executor调用次数为零

#### Scenario: Reject an unadvertised tool
- **WHEN** Provider返回当前冻结声明快照中不存在的工具名称或不匹配的 wire kind
- **THEN** sample失败且系统不按名称猜测executor或降级到通用JSON/freeform工具

#### Scenario: Reject partial Write content
- **WHEN** stream 在 Write content 尚未闭合或合法解码前结束
- **THEN** 整个 sample 失败，零候选读取、零 approval、零文件写入

#### Scenario: Decode a complete artifact call
- **WHEN** Provider 完成包含 ReadArtifact 的 sample
- **THEN** 完整参数以唯一 typed input 验证，未 ready 前不打开输出文件

SkillInput SHALL 是主文档加载与资源读取的封闭联合；skill 必填，args/resource 互斥，所有字段 strict decode。

#### Scenario: Reject an ambiguous Skill call
- **WHEN** Skill 同时提供 args 与 resource、重复键、未知字段或不完整参数
- **THEN** sample 失败且 Skill 读取次数为零

TodoWrite 与 update_plan SHALL 仅在完整 sample 通过各自 strict decoder 后产生 typed 任务调用；不得将两家字段混用、透传未知字段或以文字、Bash、用户输入代替任务调用。两家均支持至多 20 项、pending/in_progress/completed、至多一个 in_progress、完整替换与显式空数组清空；其余文本与字节约束遵循 tool/task-progress。状态回退是合法显式替换，重复标签或多个进行中不是合法更新。

#### Scenario: Decode both task facades strictly
- **WHEN** Anthropic 返回合法 TodoWrite，或 OpenAI 返回合法 update_plan
- **THEN** prepared sample 包含原生调用、usage 与同一 task.progress 能力的完整 typed ready call，顺序不变

#### Scenario: Reject a mixed or invalid task update
- **WHEN** 任务参数混用两家字段、重复标签、超过二十项、含两个 in_progress、未知字段或超出文本/编码上限
- **THEN** sample 失败，零成功 native/usage/ready facts，当前任务清单不变


任务参数解码 SHALL 按不可变 catalog 绑定的 family/profile 执行，不根据参数中出现 todos、plan 或其他字段猜测来源。工具层封闭 TaskInput 的 profile 校验与纯领域 TaskUpdate 的公共约束分别执行，非法 wire shape 使当前整个 sample 失败，不制造部分 ready、成功任务结果或新的清单。

#### Scenario: Reject mixed task arguments without profile guessing
- **WHEN** 已绑定 TodoWrite 的调用携带 plan/explanation，或已绑定 update_plan 的调用携带 activeForm/todos
- **THEN** 显式匹配的 facade decoder 拒绝整个 sample，不尝试另一 facade、不产生任何可执行 ready 或副作用

Runtime SHALL 在整个调用采样的 native commit、usage 和 ready 同批持久化前，按与声明快照相同的模式准入规则核验全部 typed ready calls。任一个调用违反 Plan Mode 的白名单时，整个采样 MUST 失败关闭。该采样 SHALL 无已提交的 native commit、usage 或 ready facts，无配对的虚构拒绝结果，无工具准备、审批、工具 Hook 或工具 I/O。此前已提交的调用和输出 SHALL 保持有效。

#### Scenario: Reject an unadvertised mutation before any member of the planning sample runs
- **WHEN** Plan Mode 中同一个 sample 同时返回合法 Read 和未声明的 Edit/apply_patch、Write 或 Bash/exec_command，即使该操作已获会话级权限或命中工具 Hook
- **THEN** 整个采样失败，所有调用均不产生已提交的 native/usage/ready facts、工具候选、审批、工具 Hook、结果或 I/O，先前采样的已提交事实仍可读取和恢复

#### Scenario: Reject a typed mutation at Runtime admission
- **WHEN** Provider 适配器向 Runtime 交付了含完整 typed Patch、Write 或 Exec 的 Plan Mode prepared sample
- **THEN** 与该回合声明相同的准入规则在持久化前拒绝整个采样，无法通过完整注册目录或既有授权绕过

### Requirement: Durable sample facts precede tool execution

Runtime MUST 在任何工具I/O前，将当前sample的Provider-native commit、sample usage与全部`tool_call_ready` facts作为同一Session batch成功`Sync`，随后执行prepared sample finalizer。每个ready fact SHALL分配全局唯一、格式可验证的EasyCode invocation ID，并保存Provider call ID、sample index、call index、capability和匹配的完整typed input；不得保存或分派capability/input revision。

patch候选准备所需的文件读取只能发生在ready batch durable与finalizer成功之后。任何patch写入还必须等待匹配candidate fingerprint的allow fact与`tool_execution_started`依次durable。batch构造、append、Sync或finalizer失败 MUST 使turn进入失败或poisoned状态，且不得调用任何executor。sample成功durable后，网络重连、UI重连和重复Runtime消费不得创建新的invocation ID、approval identity或重复执行。

Write 的目标/目录检查与候选读取 SHALL 遵循上述 Patch 的 ready durable/finalizer 边界；Write 的任何暂存文件/目录创建和发布 MUST 等待 matching allow 与 execution_started 依次 durable。

#### Scenario: Fail before executor admission
- **WHEN** 含任意调用的sample无法durable写入native commit、usage与全部ready facts
- **THEN** Runtime产生唯一失败终态且所有候选准备和executor调用次数为零

#### Scenario: Persist all calls atomically
- **WHEN** 一个sample按顺序包含Glob、Patch与Read
- **THEN** native commit、sample usage和三个ready facts在同一committed batch中获得连续seq
- **THEN** 任一记录缺失时整个batch都不能成为可恢复事实

#### Scenario: Read patch evidence only after readiness is durable
- **WHEN** patch sample的ready batch和finalizer成功
- **THEN** Runtime才可安全读取目标生成candidate与diff，但仍不得修改目标

#### Scenario: Persist before creating any directory
- **WHEN** Write 指向不存在的目录链
- **THEN** ready 前零文件读取，allow 与 started durable 前零暂存或正式目录创建

任务进度执行与其他能力一样 MUST 等待整个 ready batch 成功 Sync/finalize；只有 ready 或 started facts 不得使任务列表生效。任务执行器只计算并验证候选结果；其成功 result 在独立提交边界确定 Sync 前，宿主清单、后续请求来源与可见成功通知 MUST 仍使用旧状态。

#### Scenario: Fail a task ready batch without applying progress
- **WHEN** 含任务更新的 sample 无法成功 Sync，或 prepared sample finalize 失败
- **THEN** 任务执行器不接收调用，清单及其来源保持旧值，不发布任务更新成功

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample 不含 tool calls 时 SHALL 进入完成边界：合法无 Stop 沿原路径结束，有 Stop 时先保存 sample/usage/plan，检查通过才完成，正常授权阻断则按 Stop 契约继续同 turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch、Write、Exec、Question 或 Task 作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

当前Read、Glob、Grep、ReadArtifact、Skill属于`parallel_read`，Patch、Write、Exec、Question 与 Task 属于 `exclusive`。单个调用的普通执行错误、拒绝、冲突或无匹配结果不得取消independent sibling；`abort`、turn取消或poisoned状态按取消契约收口。goroutine完成顺序、approval响应时间和实际执行时间不得影响Session seq、Provider output顺序、preview bytes或后续请求。超过sample/call上限 SHALL以稳定英文`tool_loop_limit_exceeded`失败，并且已经durable的sample、approval、ledger和outputs不得被删除或改写。

本条的 exclusive 屏障和串行审批语义 SHALL 同样适用于 Write；不能将非 Patch 调用默认视作 parallel_read。

存在匹配 PreToolUse 时，Patch/Write/Exec SHALL 在同一 exclusive 屏障内先冻结并 durable 保存完整 Hook 计划，串行完成检查，再准备候选与普通审批。Hook 的计划、信任、进程及结果不得越过前序 result 或与后序工具重叠；只读调用不触发本切片 Hook。无匹配时 SHALL 使用该回合模式声明的目录并保持原调度与 facade 契约。单个 Hook denied/error 只阻断原 call slot，后序 independent sibling 仍按原序执行；取消、cleanup 不确定或 writer poison 才停止整个 turn。


匹配 PostToolUse 的调用 SHALL 在同一 exclusive slot 内先冻结计划并审查信任，再沿用普通候选/审批/执行。真实结果先冻结并成功 Sync；成功且真实执行的调用随后串行收口 Post，其他结果将已存在计划标记 skipped。前序 result 已 durable 但 Post 尚未 terminal 时，后序工具、整组 native outputs、steering 与下一 sample MUST 继续等待。Post 普通检查失败不能改写原结果或取消独立 sibling；取消、cleanup 不确定或 writer poison 按既有强制收口规则处理。Pre/Post 对同调用共享 30 秒检查执行预算。未开始的后缀用明确 skipped/cancelled disposition 收口，不伪造超时运行；已准入的普通 Post 错误可继续下一项，未关闭的调用预算 ticket 与未 terminal 的 Post 一样阻止后序 admission。

#### Scenario: Complete a two-sample search turn
- **WHEN** 首个sample请求Glob与Grep，结果提交后第二个sample返回最终文本且不再调用工具
- **THEN** turn只产生一个开始和一个成功终态，并在两次sample之间保持合法native call/output顺序

#### Scenario: Complete a two-sample Read turn
- **WHEN** 首个sample请求Read，结果提交后第二个sample返回最终文本且不再调用工具
- **THEN** turn只产生一个开始和一个成功终态，并保持合法native call/output顺序

#### Scenario: Complete an approved patch turn
- **WHEN** 首个sample请求Patch，用户批准且结果提交后第二个sample返回最终文本
- **THEN** turn只产生一个开始和一个成功终态，并保持合法native patch call/output顺序

#### Scenario: Execute multiple calls with an exclusive barrier
- **WHEN** 同一sample依次产生Read、Grep、Patch与Read calls
- **THEN** 前两个只读调用可并行，Patch独占，最后一个Read等待Patch完成
- **THEN** results与outputs始终按原index提交

#### Scenario: Execute multiple calls sequentially
- **WHEN** 同一sample产生两个Read calls
- **THEN** 两个调用在同一parallel-read批次按call index完成durable admission，执行可以重叠但results与outputs始终按原index提交

#### Scenario: Preserve call order across parallel completion
- **WHEN** 同一只读批次的call 0执行较慢而call 1先完成
- **THEN** result facts与Provider outputs仍按call 0、call 1顺序提交
- **THEN** 并行完成时间不进入durable metadata或模型preview

#### Scenario: Bound worker concurrency
- **WHEN** 同一连续批次包含超过八个合法`parallel_read` calls
- **THEN** 任意时刻至多八个executor正在运行，全部结果仍占据其原始call slot

#### Scenario: Stop an infinite tool loop
- **WHEN** 模型持续请求工具并达到sample或call上限
- **THEN** Runtime不发起超限sample或invocation，并以`tool_loop_limit_exceeded`durable收口turn

#### Scenario: Order Write between read and Patch calls
- **WHEN** 同一 sample 依次产生 Read、Write、Patch、Grep
- **THEN** Write 与 Patch 各自独占且按 index 串行处理，所有 results/outputs 保持原顺序
- **THEN** Write 若缺少先前已提交输出的完整 Read 证据，则在自己 slot 返回错误，不冒用本 sample 的 Read

#### Scenario: Order mixed artifact reads
- **WHEN** ReadArtifact、Exec、ReadArtifact 按此顺序 ready
- **THEN** 后一个读取等待 exclusive Exec 结果，所有 result/native output 按原 call index 提交

自动追加安全点 SHALL 在整组 results 与匹配 native outputs 均 durable/finalize 后，或无 calls sample 的 Stop continued 冻结决定已闭合且下一 sample 仍有余量后开放。只有前者要求工具 outputs，不为后者伪造调用或结果。两种安全点均由原唯一输入 owner 尝试领取一次队首连续 auto，允许空前缀，保持 strict-next 屏障；取消、poison、无采样余量或已选择结束 turn 的最终决定后不得领取。输入提交不占用 sample、不重置 16/64 上限及工具结果预算，不改变工具执行权限与 pairing。Stop 通过或终止拒绝后未续轮的队列仍等待后续 turn；无 calls sample/plan 已保存本身不表示已选择成功终态。

#### Scenario: Consume steering after complete outputs
- **WHEN** 并行调用乱序完成且存在排队自动输入
- **THEN** 全部结果仍按原 call index 提交，outputs 成功后才可提交追加输入并开始下一 sample

#### Scenario: Leave late input for the next turn
- **WHEN** 无 calls sample 已选择提交结束 turn 的最终 terminal（含 Stop 通过或终止拒绝）后输入到达
- **THEN** 当前 turn 正常完成，输入保留 FIFO 等待新 turn

Runtime SHALL 在同一 turn owner 持有 tool/result-budget 的纯内存账本，sample 512 KiB、turn 2 MiB，按调用 index 最终分配而非 worker 完成时间。每个当前及后续允许调用保留 1 KiB 安全结果空间，已冻结实际 preview bytes 扣账。steering 不重置额度；只读 worker 返回有界执行材料，由 owner 按序 finalization、result Sync 和 native outputs 提交；exclusive 屏障前不得延迟前序 result durable。

#### Scenario: Do not replenish budget with steering
- **WHEN** 同轮用户追加输入在 outputs 安全点提交
- **THEN** 后续 sample 继续使用同一 turn 剩余额度，16/64 上限和权限不改变

#### Scenario: Stop admission when turn body budget is exhausted
- **WHEN** turn 除安全结果预留外已无正文额度
- **THEN** 不再创建新审批或准入 executor；accepted 调用仍执行并清理一次，未 accepted ready 获得 result_budget_exceeded 配对；当前 outputs 完成后以 tool_result_budget_exceeded 失败收口且不领取 steer

提问 exclusive 屏障 SHALL 仅约束调度；是否需要副作用审批必须独立判定。前序结果 durable 后才可准入提问，完整回答或跳过结果 durable 后才能准入后序调用；只有整组 matching outputs durable/finalize 后才开放 steering 和下一 sample。提问计入原有 16 samples/64 calls 上限；不新增执行循环或重置预算。

#### Scenario: Preserve a question barrier
- **WHEN** sample 按 Read、Question、Write、Question 排列
- **THEN** 两个问题逐个处理且不重叠，Write 仍需单独批准，所有结果和 outputs 保持原 index

#### Scenario: Keep a hook inside the exclusive barrier
- **WHEN** 调用组为 Read、Write、Read，Write 命中检查
- **THEN** 前序 Read result durable 后才可写 Hook plan；Write result durable 前后序 Read 不开始
- **THEN** Hook 阻断不会创建额外模型 tool call，普通 denied/error 不取消后序独立调用

#### Scenario: Never hook a parallel read
- **WHEN** 同一 sample 包含 Read、Glob、Grep、Skill 或 ReadArtifact
- **THEN** 它们保持原并发与来源规则，Hook 计划和进程启动次数为零

#### Scenario: Hold the barrier after a successful result
- **WHEN** Read、Write、Read 依次 ready，Write 已保存 success 且 Post 仍运行
- **THEN** 后序 Read、native outputs 和 steering 均不开始，检查收口后仍按原 index 继续

#### Scenario: Preserve success after a failed check
- **WHEN** Write 成功后 Post 普通非零退出或输出无效
- **THEN** 只记录检查失败，原 result/status/code 不变且后序独立调用可继续

#### Scenario: Skip post processing for a declined operation
- **WHEN** 已规划 Post 的调用被普通审批拒绝或 executor 返回 conflict
- **THEN** 零 Post 进程，计划明确 skipped，再允许后序工具继续

#### Scenario: Finish a post phase without running its suffix
- **WHEN** 成功调用的前置检查已经消耗全部共用检查预算
- **THEN** 后置零 started/finished，以 hook_execution_budget_exhausted 记录未启动后缀并关闭预算 ticket，原成功保留

#### Scenario: Publish only durable post details
- **WHEN** Post plan/started/finished/terminal 批次成功 Sync 并安装查询快照
- **THEN** 发布匹配 invocation/event/plan 和递增 detail_sequence 的进程内更新；失败 Sync 不发布，headless 校验后忽略且外部协议不扩张

Stop 自动续轮 SHALL 先按真实阻断项检查独立续轮与下一普通 sample 余量，continued 决定只冻结最终反馈及交付义务；可选自动摘要只处理旧 committed 窗口。反馈单独或与真实用户纠正同批交付 Sync/finalize 后才可继续，首次请求前不再摘要新反馈。第 16 个 sample 的检查仍可通过；正常 model 阻断且没有下一 sample 时以 tool_loop_limit_exceeded 结束，无虚假 continued/claim/摘要。已累计 64 calls 本身不禁止剩余额度内的纯文本 sample；任何新 ready call 仍按原上限拒绝。

#### Scenario: Continue a checked answer without a fake tool
- **WHEN** 无 calls 答复命中正常 model 阻断且队列为 auto A、strict-next B、auto C
- **THEN** 原 turn 只领取 A，原子交付反馈与真实 A 后再采样；不制造工具 output，B/C 保持 FIFO，原调用/结果额度不重置

#### Scenario: Check the final allowed sample
- **WHEN** 第 16 个 sample 无 calls 且 Stop 检查通过或正常 model 阻断
- **THEN** 通过可完成；需要续轮则以 tool_loop_limit_exceeded 关闭，不保存 continued、不领取或摘要

#### Scenario: Preserve a text continuation after the call limit
- **WHEN** 64 个已配对 calls 后仍有 sample 余量和其他正常预算，Stop 请求续轮且后续 sample 只有文本
- **THEN** 文本采样可继续同 turn，但若出现新 ready call 则在调用准入前拒绝，不能重置 calls 上限

任务进度 SHALL 占据一个无需普通副作用批准的 exclusive slot。前序 results 成功 Sync 后才可准入 Task，Task 的唯一 result 确定 Sync 并关闭预算后才可准入后序工具；同一 sample 的多个 Task 按 call index 逐个完整替换，不能并行覆盖或合并。任务调用计入原有 16 samples/64 calls 及结果预算，不新增调度器、后台工作或额外循环；不运行工具 Hook、不产生 approval、候选文件或 sandbox 资源。只报告模型维护的清单状态不自动完成 turn，也不阻止模型在仍有 pending/in_progress 时返回普通最终答复。

#### Scenario: Order task updates as nonapproval exclusive barriers
- **WHEN** 同一 sample 按 Read、Task A、Write、Task B、Grep 排列
- **THEN** 每个 Task 等待前序 result durable 后逐个处理，B 成功保存后成为当前清单，Write 仍单独批准
- **THEN** Task 的批准、文件/进程/网络与工具 Hook 次数为零，所有结果/native outputs 保持原顺序

#### Scenario: Keep tasks within the existing turn limits
- **WHEN** 同轮多次更新任务并接受 steering
- **THEN** 任务调用仍累计到既有 16/64 及输出额度，不因清空列表或折叠界面重置预算或结束回合

Plan Mode 已准入的五项读取 SHALL 继续遵循既有只读批次、结果预算及输出配对规则。Question 和 Task SHALL 继续作为各自的 exclusive 屏障并保留既有交互、提交、取消与恢复边界。Stop 与 PreCompact SHALL 按各自的用户配置、信任、只读工作区和禁网沙箱执行。它们独立于模型工具目录，使用原有续轮或压缩边界，不改变回合捕获的模式。

#### Scenario: Order reads around planning questions and task updates
- **WHEN** Plan Mode 的合法采样按 Read、Question、Task、Grep 排列
- **THEN** Question 与 Task 分别作为 exclusive 屏障，调用结果及配对原生输出保持原顺序，既有任务与提问行为不改变当前模式

### Requirement: Execution ledger provides at-most-once automatic execution

自动允许调用的正常路径 SHALL 遵循`ready_durable -> execution_started_durable -> result_durable`；需要审批的调用 SHALL 遵循`ready_durable -> approval_decided_durable -> execution_started_durable -> result_durable`。拒绝路径 SHALL为`ready_durable -> [approval_decided(deny), result(denied)]_durable`且不得出现started。Runtime SHALL在启动任何worker前按调度顺序为每个仍可执行调用单独append/Sync`tool_execution_started`；该append被接受的成功点是该调用的执行接收线性化点。在它之前观察到取消的调用 SHALL被分类为cancelled且不得进入executor；在它之后Runtime MUST恰好调用一次匹配executor，即使turn context已取消。

每个exclusive Patch的started只能在其前序结果durable且matching allow fact写入后出现。Runtime SHALL按调度批次执行已started调用并在固定slot收集结果；到达denied/cancelled调用时，该终态也占据原call slot，最终result facts按call index写入。若某次approval、result或started append确定失败，该调用及后续调用不得执行；更早已accepted的调用仍 MUST恰好执行一次。无法确认approval、result或started是否durable的失败 MUST poison Session；恢复阶段不得猜测、重新询问或自动执行。

同一invocation ID MUST NOT自动执行两次。恢复阶段 MUST NOT执行任何invocation：patch ready且没有approval的调用关闭为`cancelled/session_interrupted_before_approval`；已有allow但没有started的调用关闭为`cancelled/session_interrupted_before_execution`；started但没有result的调用关闭为`outcome_uncertain`。Read、Glob、Grep与Patch均不声明可恢复重试例外。

Write SHALL 与 Patch 使用相同的审批 ledger、accepted started、至多一次执行与三种崩溃补偿边界。恢复不得扫描目录、查询文件存在性、清理暂存项或自动重试 Write。

本条中的 execution started SHALL 专指目标工具，不得兼作 Hook started。已选 Hook MUST 在独立 plan/started/finished/gate facts 中表达；有计划的调用只有 gate continue 后才允许准备候选、普通 approval 或 tool started。Hook started 与工具 started 各有独立的 writer admission/Sync 线性化点：接收前取消零对应 runner 调用，接收后等确定提交结果，只有成功 Sync 才恰好调用一次对应 runner，并传入当前 context。

上述 ready/approval/tool-started 恢复矩阵 SHALL 在没有未收口 Hook 门禁时适用；存在 Hook 子状态时，系统 MUST 优先按冻结计划补偿，禁止重新选择或执行检查。Hook started 无 finished 不能被解释为没有外部操作，也不能伪造目标工具 started：必须记录 Hook outcome_uncertain，并让尚未执行工具得到 cancelled/hook_outcome_uncertain 配对结果。正常运行中无法确认 Hook cleanup 或 runner 返回错配结果时 MUST poison Runtime、停止后续副作用并保留唯一清理 owner。

已有 durable tool result 的重复消费 MUST 直接复用 frozen result，不得再次请求信任、运行 Hook、重分配预算或执行工具。Hook 的安全摘要 SHALL 进入原有结果预算且最多 1 KiB；预算已不足时不得先运行 Hook 再以丢弃反馈掩盖事实。

#### Scenario: Replay a completed invocation
- **WHEN** Runtime再次遇到已有durable result的invocation ID
- **THEN** 系统复用已保存结果且executor调用次数不增加

#### Scenario: Cancel before execution admission
- **WHEN** ready或allow fact durable后turn context在某调用started admission前取消
- **THEN** 该调用获得原call slot的cancelled result且executor调用次数为零

#### Scenario: Recover before approval
- **WHEN** 进程在patch ready fact durable后、approval decision前退出
- **THEN** resume使用相同invocation ID追加`session_interrupted_before_approval`结果且不请求用户或调用executor

#### Scenario: Recover before execution starts
- **WHEN** 进程在allow fact durable后、execution started前退出
- **THEN** resume追加`session_interrupted_before_execution`结果且executor调用次数为零

#### Scenario: Cancellation races with executor admission
- **WHEN** ready已durable且turn context与started admission竞态
- **THEN** admission未接受则记录cancelled且不调用executor；admission已接受则恰好调用一次executor

#### Scenario: Cancellation follows accepted admission
- **WHEN** 某调用的started append已被接受后turn context取消
- **THEN** Runtime恰好调用一次匹配executor并传入已取消context
- **THEN** sibling的result最终仍按原call index durable

#### Scenario: Fail during ordered admission
- **WHEN** call 0的started已确定durable，而call 1的started append确定失败
- **THEN** call 0仍恰好执行一次，call 1及其后calls不执行
- **THEN** Session以失败或poisoned状态阻止新的Provider副作用

#### Scenario: Recover an uncertain invocation
- **WHEN** journal包含execution started但没有对应result
- **THEN** resume不调用任一executor，并把invocation收口为`outcome_uncertain`

#### Scenario: Do not replay a published Write after a crash
- **WHEN** Write 已 started 且可能已发布文件，但 result 缺失
- **THEN** 恢复补偿 outcome_uncertain，文件和目录 executor 调用次数均为零

#### Scenario: Cancel before hook admission
- **WHEN** Hook plan 已 durable，但 Hook started 尚未被 writer 接收时取消
- **THEN** 对应 Hook runner 与目标工具 executor 调用次数为零，门禁与原 call slot 被取消收口

#### Scenario: Cancel after accepted hook admission
- **WHEN** Hook started append 已接收后 context 取消，且最终 Sync 成功
- **THEN** Hook runner 恰好接收一次已取消 context，返回确定取消或清理结果；不要求强行创建 OS 进程
- **THEN** 目标工具不 started，清理完成前 owner 和 lease 不释放

#### Scenario: Reuse a hook-denied invocation
- **WHEN** 同一 invocation 已有 durable Hook 阻断 result，被重复消费
- **THEN** result bytes 与 code 不变，信任请求、Hook 和工具调用次数均不增加

#### Scenario: Recover an uncertain hook without inventing a tool start
- **WHEN** 只有 Hook started，没有 Hook finished、tool approval 或 tool started
- **THEN** 本地补偿 Hook 不确定与工具 cancelled/hook_outcome_uncertain，零重跑，旧 turn 失败关闭

任务进度正常路径 SHALL 为 ready_durable -> execution_started_durable -> result_durable，不要求或允许普通 approval。started 的执行接纳只允许计算候选结果，不代表清单已生效。成功 result 请求在 writer 接收前取消时 MUST 不安装新清单，并按原 slot 保存有界取消结果；writer 接收后必须等待确定提交结果，成功 Sync 才应用完整更新，不能因随后取消把已提交结果改为失败或应用两次。未知 durability MUST poison，不能发布可用新状态。错误、取消、预算拒绝与 outcome_uncertain 都不含 applied state。

恢复 Task ready 无 started 时 SHALL 补 cancelled/session_interrupted_before_execution；started 无 result 时 SHALL 保守补 outcome_uncertain；二者都不得推测候选已经生效。唯一成功 result 已 durable 时 SHALL 仅投影它的完整规范更新，不再调用执行器，不因缺少 native outputs 回退列表，也不重复更新来源。

#### Scenario: Cancel a task result before writer acceptance
- **WHEN** Task 候选计算完成但成功 result 尚未被 writer 接收便取消
- **THEN** 新清单不生效，原 call 获得配对取消结果，旧列表及来源不变

#### Scenario: Confirm an accepted task result despite late cancellation
- **WHEN** 成功 Task result 已接收，随后取消到达且 Sync 成功
- **THEN** 完整列表恰好一次生效，返回确定成功事实，不伪造回滚或第二个 result

#### Scenario: Recover an admitted task without its result
- **WHEN** Task started 已保存但没有 result，候选是否曾在旧进程计算未知
- **THEN** 本地补 outcome_uncertain、保留此前成功清单，执行器与外部调用次数为零

### Requirement: Every durable call receives a durable Provider output

每个durable tool call最终 MUST具有一个确定result状态和一个匹配原Provider call ID及call kind的Provider-native output。成功、拒绝、验证/执行错误、冲突、取消和`outcome_uncertain` SHALL使用typed result status与稳定安全的英文code；Provider-specific codec SHALL将按call index冻结的模型preview编码为合法原生function、custom或Anthropic output。

result fact MUST在next sample前durable，并保存模型实际接收的完整有界preview bytes与capability对应的非敏感typed metadata；不得保存或分派result codec revision。resume MUST从该fact重建output，不得重新执行工具、读取/搜索文件、应用patch、生成diff或按新预算重算。若results已durable而output commit尚未durable，恢复 SHALL从已保存的完整call group按call index生成output commit；若output commit已durable，则不得重复追加。补偿完成后resume MUST以一个`turn_failed`结束旧turn，且不得自动继续Provider sample。

Write SHALL 复用以上 result/output 冻结语义。完整 Write 参数由 ready/native 调用保存；result 只保存紧凑结果。Read observation 不进入 result、原生 output 或恢复逻辑。

#### Scenario: Resume between results and output commit
- **WHEN** 一个异构调用组的全部results已durable但进程在Provider-native output commit前退出
- **THEN** resume使用逐字节相同的有序preview完成一个output commit，且不再次访问workspace

#### Scenario: Resume between result and output commit
- **WHEN** 单个Read或Patch result已durable但进程在Provider-native output commit前退出
- **THEN** resume使用完全相同的preview bytes和wire kind完成output commit，且不再次访问workspace

#### Scenario: Pair a patch conflict
- **WHEN** Patch executor返回`patch_conflict`
- **THEN** Provider history得到与原call ID和custom/function kind匹配的错误output，并允许下一sample决定如何响应

#### Scenario: Pair an execution error
- **WHEN** Grep executor返回安全的typed error result
- **THEN** Provider history得到与原call ID匹配的错误output，并允许下一sample决定如何响应

#### Scenario: Cancel a committed call group
- **WHEN** turn在ready calls durable后被取消
- **THEN** 每个尚无结果的call按原index获得拒绝、取消或不确定补偿output，随后turn只产生一个失败终态

#### Scenario: Cancel a committed call
- **WHEN** turn在单个ready call durable后被取消
- **THEN** 该call获得取消或不确定补偿output，随后turn只产生一个失败终态

#### Scenario: Recover Write outputs byte for byte
- **WHEN** 有序 Write results 已 durable 而 native output 未提交
- **THEN** 恢复仅从冻结结果补齐 function/tool_result outputs，preview bytes 和原生调用顺序与不中断路径完全一致

Exec artifact 状态/引用与 ReadArtifact 页结果 SHALL 随 preview 一次冻结。恢复补偿 MUST NOT 打开 artifact、重新校验文件或按新范围重读；文件丢失不改变已提交输出。

#### Scenario: Recover artifact outputs byte for byte
- **WHEN** Exec 与 ReadArtifact results 已 durable，native output 未提交且文件已删除
- **THEN** 恢复得到与不中断路径相同的 preview、native item 顺序、canonical request 与 fingerprint，不执行 artifact I/O

预算不足、保存失败及恢复补偿 SHALL 同样获得有界 typed result 与匹配 output；预算元数据与最终 preview/保存决定在 result Sync 前冻结。已有 results 恢复时不重新分配预算或访问保存对象，补偿只为缺失 result 使用当前安全有界策略。已实现工具与 search_result 均遵守上述规则。

#### Scenario: Recover a reduced search result
- **WHEN** 搜索缩短结果与保存引用已 durable 而 native output 未提交，保存文件随后丢失
- **THEN** 恢复只使用冻结 bytes，canonical request、native item 顺序与 fingerprint 与不中断路径等价

Task success SHALL 使用最多 1 KiB 的固定格式英文计数确认 preview，并在 typed metadata 中完整保存规范更新与必要来源关联；模型调用本身已保存完整参数，preview 无需重复回显整份清单。完整更新不得被 preview 裁剪替代，失败结果不得伪含 applied state。只有 status=success/code=ok 且完整 metadata 与调用一致的 durable result 能成为当前清单来源；完成数只是模型报告，不证明工作已验证或获得授权。

#### Scenario: Rebuild a task output without re-rendering its list
- **WHEN** 任务成功 result 已 durable 而对应 native outputs 缺失
- **THEN** 恢复保留已保存规范列表并仅使用原冻结英文确认 preview 编码配对 output，canonical request 和 fingerprint 与同一事实不中断路径相等
- **THEN** 不重新执行更新、不按当前 UI 或预算重渲染确认文本

#### Scenario: Preserve task metadata independently of the compact confirmation
- **WHEN** 完整合法任务更新包含多项长文本，而成功 preview 只报告计数
- **THEN** Session 恢复仍逐字保留完整清单、顺序、activeForm 与说明，不能把计数 preview 当成唯一任务数据


原生恢复、恢复补齐输出和离线历史正文读取 SHALL 复用相同已验证 TaskInput/任务结果 evidence，确认完整规范更新与原 native 调用相等。只读读者不得从固定计数 preview、调用名或自然语言摘要独立猜测已应用清单，也不能重新规范化文字或放宽来源 profile 约束；只有成功完整结果才能显示为已应用更新。

#### Scenario: Share task evidence with the offline history reader
- **WHEN** 同一固定 Session 分别用于正常恢复、补齐缺失 native output 与离线读取任务详情
- **THEN** 三者使用相同原调用/profile/result 关联并保留完整规范内容；篡改 ready/result 或原生参数时三者均拒绝，不能一个读者显示伪造成功而另一个拒绝

### Requirement: Command execution is an exclusive durable invocation

`process.exec` SHALL作为`exclusive`屏障参与既有有序多sample循环。Runtime MUST等待前序parallel-read批次及其results durable后才准备命令候选和请求approval；matching allow fact与execution started依次durable后才可启动进程；命令result durable后才可准入后续调用或提交Provider-native output。进程执行不得与Patch/Write、另一Exec或任意后序调用重叠。

恢复 SHALL NOT重新批准或执行命令：ready且没有approval的Exec关闭为`cancelled/session_interrupted_before_approval`；allow fact后没有started的Exec关闭为`cancelled/session_interrupted_before_execution`；started而没有result的Exec关闭为`outcome_uncertain`。每种补偿 MUST占据原call slot并产生匹配Provider output。

#### Scenario: Enforce an Exec barrier
- **WHEN** 一个Exec位于两个只读批次之间
- **THEN** Runtime等待前序结果durable，批准并独占执行Exec，再等待其result durable后准入后序批次

#### Scenario: Recover an accepted command without a result
- **WHEN** journal包含Exec execution started但没有result
- **THEN** resume不创建进程、不请求批准并以`outcome_uncertain`补偿原call slot
