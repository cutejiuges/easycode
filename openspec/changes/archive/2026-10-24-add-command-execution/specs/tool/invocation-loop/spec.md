## MODIFIED Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade及其wire kind、validator、policy、sandbox planner、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema或freeform定义，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前 snapshot SHALL 恰好包含 `fs.read`、`fs.glob`、`fs.grep`、`fs.patch` 与 `process.exec`；Anthropic SHALL 将 patch/command 分别暴露为 structured `Edit`/`Bash`，OpenAI SHALL 将同一 patch/command 能力分别暴露为 custom freeform `apply_patch` 与 structured `exec_command`。facade 按稳定 capability ID、wire kind 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical definition bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation/approval ID、权限临时状态、base evidence、diff、命令环境实际值和文件系统枚举顺序 MUST NOT 进入 definition bytes 或 fingerprint。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Read、Glob、Grep、Edit、apply_patch、Bash和exec_command描述与定义创建catalog snapshot
- **THEN** facade顺序、wire kind、canonical bytes、source revision和fingerprint完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog尝试注册只有schema或freeform描述而没有匹配executor、validator、policy、sandbox planner或result renderer的facade
- **THEN** snapshot构造失败且该facade不会出现在任一Provider请求中

#### Scenario: Expose the complete current catalog
- **WHEN** Runtime为本变更编译一次Provider请求
- **THEN** catalog只声明Read、Glob、Grep、对应Provider的Edit或apply_patch以及Bash或exec_command，不声明Write、Skill、MCP、Agent、后台进程或write_stdin

#### Scenario: Expose the complete read-only catalog
- **WHEN** 将当前catalog中的只读子集投影给审计或测试
- **THEN** 该子集仍恰好包含Read、Glob、Grep，Patch与Exec作为独立exclusive能力存在且不冒充只读工具

#### Scenario: Expose only current contracts
- **WHEN** 将当前catalog与早期开发期catalog比较
- **THEN** 当前唯一catalog直接包含五项能力且不保留旧catalog、旧schema或兼容分支

#### Scenario: Expose Provider-specific mutation wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者分别路由到相同`fs.patch`与`process.exec`能力，但保持各自Edit/apply_patch与Bash/exec_command facade及wire

#### Scenario: Expose only Read
- **WHEN** 将当前catalog与最早只暴露Read的开发期catalog比较
- **THEN** 当前唯一catalog直接包含Read、Glob、Grep、Patch与Exec且不保留旧catalog分支

#### Scenario: Expose Provider-specific patch wire
- **WHEN** 分别读取Anthropic与OpenAI catalog view
- **THEN** 两者都路由到`fs.patch`，但Anthropic使用structured JSON而OpenAI使用custom freeform定义

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析 capability 以及 `ReadInput`、`GlobInput`、`GrepInput`、`PatchInput` 或 `ExecInput` 封闭联合中的恰好一种；核心导出边界 MUST NOT 接受或返回 `any`、无约束动态 map 或 input revision router。

Anthropic patch与command参数必须由完整structured JSON strict decode；OpenAI patch输入必须由完整custom freeform parser消费，command参数必须由完整structured JSON strict decode。参数delta、未闭合原生item、未知工具、重复Provider call ID、capability与typed input不匹配、未知字段、尾随JSON/patch文本、未支持操作或超限输入 MUST 使sample失败，不得产生ready call、native commit、sample usage、approval或工具I/O。

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

## ADDED Requirements

### Requirement: Command execution is an exclusive durable invocation

`process.exec` SHALL作为`exclusive`屏障参与既有有序多sample循环。Runtime MUST等待前序parallel-read批次及其results durable后才准备命令候选和请求approval；matching allow fact与execution started依次durable后才可启动进程；命令result durable后才可准入后续调用或提交Provider-native output。进程执行不得与Patch、另一Exec或任意后序调用重叠。

恢复 SHALL NOT重新批准或执行命令：ready且没有approval的Exec关闭为`cancelled/session_interrupted_before_approval`；allow fact后没有started的Exec关闭为`cancelled/session_interrupted_before_execution`；started而没有result的Exec关闭为`outcome_uncertain`。每种补偿 MUST占据原call slot并产生匹配Provider output。

#### Scenario: Enforce an Exec barrier
- **WHEN** 一个Exec位于两个只读批次之间
- **THEN** Runtime等待前序结果durable，批准并独占执行Exec，再等待其result durable后准入后序批次

#### Scenario: Recover an accepted command without a result
- **WHEN** journal包含Exec execution started但没有result
- **THEN** resume不创建进程、不请求批准并以`outcome_uncertain`补偿原call slot
