# tool/invocation-loop Specification

## Purpose

定义首个可恢复 Tool Loop 的目录、调用、持久化与多次采样行为，使模型能够安全调用真实能力，同时保持 Provider 原生历史、Session ledger、取消和崩溃恢复的一致性。

## Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade及其wire kind、validator、policy、适用的 sandbox planner、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema或freeform定义，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前 snapshot SHALL 恰好包含 `fs.read`、`fs.glob`、`fs.grep`、`fs.patch`、`fs.write`、`process.exec`、`artifact.read`、`skill.read` 与 `user.question`；Anthropic SHALL 将 patch/command 分别暴露为 structured `Edit`/`Bash`，OpenAI SHALL 将同一 patch/command 能力分别暴露为 custom freeform `apply_patch` 与 structured `exec_command`。facade 按稳定 capability ID、wire kind 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical definition bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation/approval ID、权限临时状态、base evidence、diff、命令环境实际值和文件系统枚举顺序 MUST NOT 进入 definition bytes 或 fingerprint。

两个 Provider SHALL 都以 structured Write 暴露 fs.write。Write 的 request、strict decoder、native restore、result codec、候选、policy、executor 和审批展示缺一不可；创建/覆盖证据和 Read observation MUST NOT 进入稳定 definition bytes。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Skill、Read、ReadArtifact、Glob、Grep、Write、Edit、apply_patch、Bash和exec_command描述与定义创建catalog snapshot
- **THEN** facade顺序、wire kind、canonical bytes、source revision和fingerprint完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog尝试注册只有schema或freeform描述而没有匹配executor、validator、policy、sandbox planner或result renderer的facade
- **THEN** snapshot构造失败且该facade不会出现在任一Provider请求中

#### Scenario: Expose the complete current catalog
- **WHEN** Runtime为本变更编译一次Provider请求
- **THEN** catalog只声明Read、ReadArtifact、Glob、Grep、Write、对应Provider的Edit或apply_patch以及Bash或exec_command，额外声明 structured Skill 与对应 Provider 的 structured 提问 facade，不声明MCP、Agent、后台进程或write_stdin

#### Scenario: Expose the complete read-only catalog
- **WHEN** 将当前catalog中的只读子集投影给审计或测试
- **THEN** 该子集仍恰好包含Read、ReadArtifact、Glob、Grep、Skill，Patch、Write与Exec作为独立exclusive能力存在且不冒充只读工具

#### Scenario: Expose only current contracts
- **WHEN** 将当前catalog与早期开发期catalog比较
- **THEN** 当前唯一catalog直接包含九项能力且不保留旧catalog、旧schema或兼容分支

#### Scenario: Expose Provider-specific mutation wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者分别路由到相同`fs.patch`与`process.exec`能力，但保持各自Edit/apply_patch与Bash/exec_command facade及wire

#### Scenario: Expose only Read
- **WHEN** 将当前catalog与最早只暴露Read的开发期catalog比较
- **THEN** 当前唯一catalog直接包含Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill与Question且不保留旧catalog分支

#### Scenario: Expose Provider-specific patch wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者都路由到`fs.patch`，但Anthropic使用structured JSON而OpenAI使用custom freeform定义

#### Scenario: Expose structured Write beside native Patch
- **WHEN** 获取两家完整当前 catalog
- **THEN** 两者均有 structured Write，OpenAI 同时保留一个 custom apply_patch
- **THEN** Write 定义顺序与 fingerprint 不受目标存在性和 Read 证据变化影响

两家 Provider SHALL 使用 structured ReadArtifact 暴露 artifact.read；保存路径、ID、计数、动态配额使用量 MUST NOT 进入 catalog definition 或 fingerprint。

#### Scenario: Expose executable artifact reading
- **WHEN** 构造两家 Provider 当前 catalog
- **THEN** 恰好九项能力，ReadArtifact 具有完整 request、reducer、native restore、policy、executor 和 result codec

两家 Provider SHALL 以 structured Skill 暴露 skill.read，具备完整读取、validator、policy、来源范围约束、renderer 与恢复实现。目录为空仍保留真实 executor，未知名称返回安全错误，不暴露未来来源能力。

#### Scenario: Expose executable Skill loading
- **WHEN** 两家 Provider 构造当前目录
- **THEN** 均恰好九项能力，Skill 的主文档与资源分支都有 typed input/result，定义不包含实际目录正文或绝对路径

user.question SHALL 具有完整 typed input/result、strict decoder、policy、实际交互执行消费者、renderer 和 native restore。Anthropic facade 为 AskUserQuestion，OpenAI facade 为 request_user_input，均为 structured wire。该能力不操作文件、进程或网络，不生成虚构 sandbox 候选或副作用批准；无交互宿主由真实执行路径返回配对不可用结果。目录定义中不得出现动态题目、回答、请求身份、结果余额或 UI 状态。

#### Scenario: Expose executable user questions
- **WHEN** 两家 Provider 构造完整当前 catalog
- **THEN** 均恰好九项真实能力，提问 facade 按既有规则排序，不能只提供 schema 而没有交互、拒绝及恢复消费者

#### Scenario: Keep the read-only subset precise
- **WHEN** 检查可并行读取子集
- **THEN** 仍为 Read、Glob、Grep、ReadArtifact、Skill，提问不因没有文件写入而被放入并行读批次

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析 capability 以及 `ReadInput`、`GlobInput`、`GrepInput`、`PatchInput`、`WriteInput`、`ExecInput`、`ReadArtifactInput` 或 `SkillInput` 封闭联合中的恰好一种；核心导出边界 MUST NOT 接受或返回 `any`、无约束动态 map 或 input revision router。

Anthropic patch与command参数必须由完整structured JSON strict decode；OpenAI patch输入必须由完整custom freeform parser消费，command参数必须由完整structured JSON strict decode。参数delta、未闭合原生item、未知工具、重复Provider call ID、capability与typed input不匹配、未知字段、尾随JSON/patch文本、未支持操作或超限输入 MUST 使sample失败，不得产生ready call、native commit、sample usage、approval或工具I/O。

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
- **WHEN** Provider返回当前catalog snapshot中不存在的工具名称或wire kind
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

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample不含tool calls时 SHALL结束turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch、Write、Exec 或 Question 作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

当前Read、Glob、Grep、ReadArtifact、Skill属于`parallel_read`，Patch、Write、Exec与Question属于`exclusive`。单个调用的普通执行错误、拒绝、冲突或无匹配结果不得取消independent sibling；`abort`、turn取消或poisoned状态按取消契约收口。goroutine完成顺序、approval响应时间和实际执行时间不得影响Session seq、Provider output顺序、preview bytes或后续请求。超过sample/call上限 SHALL以稳定英文`tool_loop_limit_exceeded`失败，并且已经durable的sample、approval、ledger和outputs不得被删除或改写。

本条的 exclusive 屏障和串行审批语义 SHALL 同样适用于 Write；不能将非 Patch 调用默认视作 parallel_read。

存在匹配 PreToolUse 时，Patch/Write/Exec SHALL 在同一 exclusive 屏障内先冻结并 durable 保存完整 Hook 计划，串行完成检查，再准备候选与普通审批。Hook 的计划、信任、进程及结果不得越过前序 result 或与后序工具重叠；只读调用不触发本切片 Hook。无匹配时 SHALL 保持原调度、九能力目录与 Provider facade 不变。单个 Hook denied/error 只阻断原 call slot，后序 independent sibling 仍按原序执行；取消、cleanup 不确定或 writer poison 才停止整个 turn。


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

自动追加安全点 SHALL 仅在整组 results 与匹配 native outputs 均 durable/finalize 后开放；上限、取消、poison 或无 calls 最终决策后不得领取。输入提交不占用 sample、不重置 16/64 上限，不改变工具执行权限与 pairing。

#### Scenario: Consume steering after complete outputs
- **WHEN** 并行调用乱序完成且存在排队自动输入
- **THEN** 全部结果仍按原 call index 提交，outputs 成功后才可提交追加输入并开始下一 sample

#### Scenario: Leave late input for the next turn
- **WHEN** 无 calls sample 已选择提交最终边界后输入到达
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

### Requirement: Command execution is an exclusive durable invocation

`process.exec` SHALL作为`exclusive`屏障参与既有有序多sample循环。Runtime MUST等待前序parallel-read批次及其results durable后才准备命令候选和请求approval；matching allow fact与execution started依次durable后才可启动进程；命令result durable后才可准入后续调用或提交Provider-native output。进程执行不得与Patch/Write、另一Exec或任意后序调用重叠。

恢复 SHALL NOT重新批准或执行命令：ready且没有approval的Exec关闭为`cancelled/session_interrupted_before_approval`；allow fact后没有started的Exec关闭为`cancelled/session_interrupted_before_execution`；started而没有result的Exec关闭为`outcome_uncertain`。每种补偿 MUST占据原call slot并产生匹配Provider output。

#### Scenario: Enforce an Exec barrier
- **WHEN** 一个Exec位于两个只读批次之间
- **THEN** Runtime等待前序结果durable，批准并独占执行Exec，再等待其result durable后准入后序批次

#### Scenario: Recover an accepted command without a result
- **WHEN** journal包含Exec execution started但没有result
- **THEN** resume不创建进程、不请求批准并以`outcome_uncertain`补偿原call slot
