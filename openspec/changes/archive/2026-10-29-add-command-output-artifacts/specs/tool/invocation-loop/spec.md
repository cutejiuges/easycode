## MODIFIED Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade及其wire kind、validator、policy、sandbox planner、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema或freeform定义，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前 snapshot SHALL 恰好包含 `fs.read`、`fs.glob`、`fs.grep`、`fs.patch`、`fs.write`、`process.exec` 与 `artifact.read`；Anthropic SHALL 将 patch/command 分别暴露为 structured `Edit`/`Bash`，OpenAI SHALL 将同一 patch/command 能力分别暴露为 custom freeform `apply_patch` 与 structured `exec_command`。facade 按稳定 capability ID、wire kind 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical definition bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation/approval ID、权限临时状态、base evidence、diff、命令环境实际值和文件系统枚举顺序 MUST NOT 进入 definition bytes 或 fingerprint。

两个 Provider SHALL 都以 structured Write 暴露 fs.write。Write 的 request、strict decoder、native restore、result codec、候选、policy、executor 和审批展示缺一不可；创建/覆盖证据和 Read observation MUST NOT 进入稳定 definition bytes。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Read、ReadArtifact、Glob、Grep、Write、Edit、apply_patch、Bash和exec_command描述与定义创建catalog snapshot
- **THEN** facade顺序、wire kind、canonical bytes、source revision和fingerprint完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog尝试注册只有schema或freeform描述而没有匹配executor、validator、policy、sandbox planner或result renderer的facade
- **THEN** snapshot构造失败且该facade不会出现在任一Provider请求中

#### Scenario: Expose the complete current catalog
- **WHEN** Runtime为本变更编译一次Provider请求
- **THEN** catalog只声明Read、ReadArtifact、Glob、Grep、Write、对应Provider的Edit或apply_patch以及Bash或exec_command，不声明Skill、MCP、Agent、后台进程或write_stdin

#### Scenario: Expose the complete read-only catalog
- **WHEN** 将当前catalog中的只读子集投影给审计或测试
- **THEN** 该子集仍恰好包含Read、ReadArtifact、Glob、Grep，Patch、Write与Exec作为独立exclusive能力存在且不冒充只读工具

#### Scenario: Expose only current contracts
- **WHEN** 将当前catalog与早期开发期catalog比较
- **THEN** 当前唯一catalog直接包含七项能力且不保留旧catalog、旧schema或兼容分支

#### Scenario: Expose Provider-specific mutation wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者分别路由到相同`fs.patch`与`process.exec`能力，但保持各自Edit/apply_patch与Bash/exec_command facade及wire

#### Scenario: Expose only Read
- **WHEN** 将当前catalog与最早只暴露Read的开发期catalog比较
- **THEN** 当前唯一catalog直接包含Read、Glob、Grep、Patch、Write、Exec与ReadArtifact且不保留旧catalog分支

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
- **THEN** 恰好七项能力，ReadArtifact 具有完整 request、reducer、native restore、policy、executor 和 result codec

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析 capability 以及 `ReadInput`、`GlobInput`、`GrepInput`、`PatchInput`、`WriteInput`、`ExecInput` 或 `ReadArtifactInput` 封闭联合中的恰好一种；核心导出边界 MUST NOT 接受或返回 `any`、无约束动态 map 或 input revision router。

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

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample不含tool calls时 SHALL结束turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch、Write 或 Exec 作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

当前Read、Glob、Grep、ReadArtifact属于`parallel_read`，Patch、Write与Exec属于`exclusive`。单个调用的普通执行错误、拒绝、冲突或无匹配结果不得取消independent sibling；`abort`、turn取消或poisoned状态按取消契约收口。goroutine完成顺序、approval响应时间和实际执行时间不得影响Session seq、Provider output顺序、preview bytes或后续请求。超过sample/call上限 SHALL以稳定英文`tool_loop_limit_exceeded`失败，并且已经durable的sample、approval、ledger和outputs不得被删除或改写。

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

#### Scenario: Order mixed artifact reads
- **WHEN** ReadArtifact、Exec、ReadArtifact 按此顺序 ready
- **THEN** 后一个读取等待 exclusive Exec 结果，所有 result/native output 按原 call index 提交

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
