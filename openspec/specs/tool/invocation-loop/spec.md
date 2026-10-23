# tool/invocation-loop Specification

## Purpose

定义首个可恢复 Tool Loop 的目录、调用、持久化与多次采样行为，使模型能够安全调用真实能力，同时保持 Provider 原生历史、Session ledger、取消和崩溃恢复的一致性。

## Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade及其wire kind、validator、policy、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema或freeform定义，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前 snapshot SHALL 恰好包含 `fs.read`、`fs.glob`、`fs.grep`、`fs.patch` 与 `fs.write`；Anthropic SHALL 将 patch 暴露为 structured `Edit`，OpenAI SHALL 将同一能力暴露为 custom freeform `apply_patch`。facade 按稳定 capability ID、wire kind 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical definition bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation/approval ID、权限临时状态、base evidence、diff 和文件系统枚举顺序 MUST NOT 进入 definition bytes 或 fingerprint。

两个 Provider SHALL 都以 structured Write 暴露 fs.write。Write 的 request、strict decoder、native restore、result codec、候选、policy、executor 和审批展示缺一不可；创建/覆盖证据和 Read observation MUST NOT 进入稳定 definition bytes。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Read、Glob、Grep、Write、Edit和apply_patch描述与定义创建catalog snapshot
- **THEN** facade顺序、wire kind、canonical bytes、source revision和fingerprint完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog尝试注册只有schema或freeform描述而没有匹配executor、validator、policy或result renderer的facade
- **THEN** snapshot构造失败且该facade不会出现在任一Provider请求中

#### Scenario: Expose the complete current catalog
- **WHEN** Runtime为本变更编译一次Provider请求
- **THEN** catalog只声明Read、Glob、Grep、Write和对应Provider的Edit或apply_patch，不声明Bash、Skill、MCP或Agent

#### Scenario: Expose the complete read-only catalog
- **WHEN** 将当前catalog中的只读子集投影给审计或测试
- **THEN** 该子集仍恰好包含Read、Glob、Grep，Patch作为独立exclusive能力存在且不冒充只读工具

#### Scenario: Expose only Read
- **WHEN** 将当前catalog与最早只暴露Read的开发期catalog比较
- **THEN** 当前唯一catalog直接包含Read、Glob、Grep、Patch与Write且不保留旧catalog分支

#### Scenario: Expose Provider-specific patch wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者都路由到`fs.patch`，但Anthropic使用structured JSON而OpenAI使用custom freeform定义

#### Scenario: Expose structured Write beside native Patch
- **WHEN** 获取两家完整当前 catalog
- **THEN** 两者均有 structured Write，OpenAI 同时保留一个 custom apply_patch
- **THEN** Write 定义顺序与 fingerprint 不受目标存在性和 Read 证据变化影响

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析 capability 以及 `ReadInput`、`GlobInput`、`GrepInput`、`PatchInput` 或 `WriteInput` 封闭联合中的恰好一种；核心导出边界 MUST NOT 接受或返回 `any`、无约束动态 map 或 input revision router。

Anthropic patch参数必须由完整structured JSON strict decode；OpenAI patch输入必须由完整custom freeform parser消费。参数delta、未闭合原生item、未知工具、重复Provider call ID、capability与typed input不匹配、未知字段、尾随JSON/patch文本、未支持patch操作或超限输入 MUST 使sample失败，不得产生ready call、native commit、sample usage、approval或工具I/O。

Write SHALL 只由完整 structured JSON strict decode 产生 ready call；调用不得被转换成 Patch replacement 或 shell 命令。

#### Scenario: Prepare ordered heterogeneous calls
- **WHEN** Provider sample依次完成合法的Glob、Grep、Read与Patch调用
- **THEN** prepared sample同时包含对应原生calls、normalized usage和四个匹配的typed ready calls
- **THEN** ready calls的位置与原生模型调用顺序一致

#### Scenario: Prepare an ordered Read call
- **WHEN** Provider sample完成一个参数合法的Read调用
- **THEN** prepared sample同时包含对应原生call、normalized usage和一个typed Read ready call

#### Scenario: Prepare Provider-specific patch input
- **WHEN** Anthropic完成structured Edit或OpenAI完成custom apply_patch
- **THEN** 两条wire分别产生匹配同一`fs.patch`能力的typed Patch ready call

#### Scenario: Reject partial arguments
- **WHEN** stream在任一搜索、读取或patch参数尚未完整时结束，或参数无法strict decode
- **THEN** sample以协议错误失败，Session不写入成功sample facts且所有executor调用次数为零

#### Scenario: Reject an unadvertised tool
- **WHEN** Provider返回当前catalog snapshot中不存在的工具名称或wire kind
- **THEN** sample失败且系统不按名称猜测executor或降级到通用JSON/freeform工具

#### Scenario: Reject partial Write content
- **WHEN** stream 在 Write content 尚未闭合或合法解码前结束
- **THEN** 整个 sample 失败，零候选读取、零 approval、零文件写入

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

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample不含tool calls时 SHALL结束turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

当前Read、Glob、Grep属于`parallel_read`，Patch与Write属于`exclusive`。单个调用的普通执行错误、拒绝、冲突或无匹配结果不得取消independent sibling；`abort`、turn取消或poisoned状态按取消契约收口。goroutine完成顺序、approval响应时间和实际执行时间不得影响Session seq、Provider output顺序、preview bytes或后续请求。超过sample/call上限 SHALL以稳定英文`tool_loop_limit_exceeded`失败，并且已经durable的sample、approval、ledger和outputs不得被删除或改写。

本条的 exclusive 屏障和串行审批语义 SHALL 同样适用于 Write；不能将非 Patch 调用默认视作 parallel_read。

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

### Requirement: Execution ledger provides at-most-once automatic execution

自动允许调用的正常路径 SHALL 遵循`ready_durable -> execution_started_durable -> result_durable`；需要审批的调用 SHALL 遵循`ready_durable -> approval_decided_durable -> execution_started_durable -> result_durable`。拒绝路径 SHALL为`ready_durable -> [approval_decided(deny), result(denied)]_durable`且不得出现started。Runtime SHALL在启动任何worker前按调度顺序为每个仍可执行调用单独append/Sync`tool_execution_started`；该append被接受的成功点是该调用的执行接收线性化点。在它之前观察到取消的调用 SHALL被分类为cancelled且不得进入executor；在它之后Runtime MUST恰好调用一次匹配executor，即使turn context已取消。

每个exclusive Patch的started只能在其前序结果durable且matching allow fact写入后出现。Runtime SHALL按调度批次执行已started调用并在固定slot收集结果；到达denied/cancelled调用时，该终态也占据原call slot，最终result facts按call index写入。若某次approval、result或started append确定失败，该调用及后续调用不得执行；更早已accepted的调用仍 MUST恰好执行一次。无法确认approval、result或started是否durable的失败 MUST poison Session；恢复阶段不得猜测、重新询问或自动执行。

同一invocation ID MUST NOT自动执行两次。恢复阶段 MUST NOT执行任何invocation：patch ready且没有approval的调用关闭为`cancelled/session_interrupted_before_approval`；已有allow但没有started的调用关闭为`cancelled/session_interrupted_before_execution`；started但没有result的调用关闭为`outcome_uncertain`。Read、Glob、Grep与Patch均不声明可恢复重试例外。

Write SHALL 与 Patch 使用相同的审批 ledger、accepted started、至多一次执行与三种崩溃补偿边界。恢复不得扫描目录、查询文件存在性、清理暂存项或自动重试 Write。

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
