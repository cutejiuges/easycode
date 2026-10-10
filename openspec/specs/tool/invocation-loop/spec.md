# tool/invocation-loop Specification

## Purpose

定义首个可恢复 Tool Loop 的目录、调用、持久化与多次采样行为，使模型能够安全调用真实能力，同时保持 Provider 原生历史、Session ledger、取消和崩溃恢复的一致性。

## Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联唯一当前、已注册且可执行的 capability、强类型 input/result、Provider facade、validator、executor 和结果 renderer；系统 MUST NOT 暴露缺少任一真实消费者的 schema，也 MUST NOT 通过 input revision、result codec revision 或版本后缀类型选择实现。

当前 snapshot SHALL 恰好包含 `fs.read`、`fs.glob` 与 `fs.grep`，并按稳定 capability ID 与 facade name 确定性排序。snapshot SHALL 具有非空内容派生 source revision、canonical schema bytes 和 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation ID、权限临时状态和文件系统枚举顺序 MUST NOT 进入 schema bytes 或 fingerprint。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Read、Glob、Grep 描述和 schema 创建 catalog snapshot
- **THEN** facade 顺序、canonical bytes、source revision 和 fingerprint 完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog 尝试注册只有 schema 而没有匹配 executor、validator 或 result renderer 的 facade
- **THEN** snapshot 构造失败且该 facade 不会出现在任一 Provider 请求中

#### Scenario: Expose the complete read-only catalog
- **WHEN** Runtime 为本变更编译一次 Provider 请求
- **THEN** catalog 只声明 Read、Glob、Grep，不声明 Edit、Write、Bash、Skill、MCP 或 Agent

#### Scenario: Expose only Read
- **WHEN** 将本变更后的 catalog 与旧开发期 Read-only catalog 比较
- **THEN** 当前 catalog 不再只暴露 Read，而是完整暴露 Read、Glob、Grep 且不保留旧 catalog 分支

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析 capability 以及 `ReadInput`、`GlobInput` 或 `GrepInput` 封闭联合中的恰好一种；核心导出边界 MUST NOT 接受或返回 `any`、无约束动态 map 或 input revision router。

参数 delta、未闭合原生 item、未知工具、重复 Provider call ID、capability 与 typed input 不匹配、未知字段、尾随 JSON 或超限输入 MUST 使 sample 失败，不得产生 ready call、native commit、sample usage 或工具 I/O。

#### Scenario: Prepare ordered heterogeneous calls
- **WHEN** Provider sample 依次完成合法的 Glob、Grep 与 Read 调用
- **THEN** prepared sample 同时包含对应原生 calls、normalized usage 和三个匹配的 typed ready calls
- **THEN** ready calls 的位置与原生模型调用顺序一致

#### Scenario: Prepare an ordered Read call
- **WHEN** Provider sample 完成一个参数合法的 Read 调用
- **THEN** prepared sample 同时包含对应原生 call、normalized usage 和一个 typed Read ready call

#### Scenario: Reject partial arguments
- **WHEN** stream 在任一搜索或读取参数尚未完整时结束，或参数无法 strict decode
- **THEN** sample 以协议错误失败，Session 不写入成功 sample facts且所有 executor 调用次数为零

#### Scenario: Reject an unadvertised tool
- **WHEN** Provider 返回当前 catalog snapshot 中不存在的工具名称
- **THEN** sample 失败且系统不按名称猜测 executor 或降级到通用 JSON 工具

### Requirement: Durable sample facts precede tool execution

Runtime MUST 在任何工具 I/O 前，将当前 sample 的 Provider-native commit、sample usage 与全部 `tool_call_ready` facts 作为同一 Session batch 成功 `Sync`，随后执行 prepared sample finalizer。每个 ready fact SHALL 分配全局唯一、格式可验证的 EasyCode invocation ID，并保存 Provider call ID、sample index、call index、capability 和匹配的完整 typed input；不得保存或分派 capability/input revision。

batch 构造、append、Sync 或 finalizer 失败 MUST 使 turn 进入失败或 poisoned 状态，且不得调用任何 executor。sample 成功 durable 后，网络重连、UI 重连和重复 Runtime 消费不得创建新的 invocation ID 或重复执行。

#### Scenario: Fail before executor admission
- **WHEN** 含任意只读调用的 sample 无法 durable 写入 native commit、usage 与全部 ready facts
- **THEN** Runtime 产生唯一失败终态且所有 executor 调用次数为零

#### Scenario: Persist all calls atomically
- **WHEN** 一个 sample 按顺序包含 Glob、Grep 与 Read
- **THEN** native commit、sample usage 和三个 ready facts 在同一 committed batch 中获得连续 seq
- **THEN** 任一记录缺失时整个 batch 都不能成为可恢复事实

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户 turn SHALL 支持最多 16 个 Provider samples 和累计最多 64 个 tool calls。completed sample 不含 tool calls 时 SHALL 结束 turn；含 calls 时，Runtime SHALL 先按 call index 完成执行准入，再以最大 8 个并发任务执行当前 `parallel_read` 调用，收集到固定 call slot，并按 call index durable 提交全部 results 和匹配的 Provider-native outputs；outputs durable 后才可开始下一 sample。

当前 Read、Glob、Grep 均属于 `parallel_read`，单个调用的执行错误或无匹配结果不得取消 sibling。goroutine 完成顺序不得影响 Session seq、Provider output 顺序、preview bytes 或后续请求。超过 sample/call 上限 SHALL 以稳定英文 `tool_loop_limit_exceeded` 失败，并且已经 durable 的 sample、ledger 和 outputs 不得被删除或改写。

#### Scenario: Complete a two-sample search turn
- **WHEN** 首个 sample 请求 Glob 与 Grep，结果提交后第二个 sample 返回最终文本且不再调用工具
- **THEN** turn 只产生一个开始和一个成功终态，并在两次 sample 之间保持合法 native call/output 顺序

#### Scenario: Complete a two-sample Read turn
- **WHEN** 首个 sample 请求 Read，结果提交后第二个 sample 返回最终文本且不再调用工具
- **THEN** turn 只产生一个开始和一个成功终态，并保持合法 native call/output 顺序

#### Scenario: Execute multiple calls sequentially
- **WHEN** 同一 sample 产生两个 Read calls
- **THEN** 两个调用先按 call index 完成 durable admission，执行可以重叠但 results 与 outputs 始终按原 index 提交

#### Scenario: Preserve call order across parallel completion
- **WHEN** 同一 sample 的 call 0 执行较慢而 call 1 与 call 2 先完成
- **THEN** 三个 result facts 与 Provider outputs 仍按 call 0、1、2 顺序提交
- **THEN** 并行完成时间不进入 durable metadata 或模型 preview

#### Scenario: Bound worker concurrency
- **WHEN** 同一 sample 包含超过八个合法 `parallel_read` calls
- **THEN** 任意时刻至多八个 executor 正在运行，全部结果仍占据其原始 call slot

#### Scenario: Stop an infinite tool loop
- **WHEN** 模型持续请求工具并达到 sample 或 call 上限
- **THEN** Runtime 不发起超限 sample或 invocation，并以 `tool_loop_limit_exceeded` durable 收口 turn

### Requirement: Execution ledger provides at-most-once automatic execution

每个 invocation 的正常路径 SHALL 遵循 `ready_durable -> execution_started_durable -> result_durable` 的单向 ledger。Runtime SHALL 在启动任何 worker 前按 call index 为每个仍可执行调用单独 append/Sync `tool_execution_started`；该 append 被接受的成功点是该调用的执行接收线性化点。在它之前观察到取消的调用 SHALL 被分类为 cancelled 且不得进入 executor；在它之后 Runtime MUST 恰好调用一次匹配 executor，即使 turn context 已取消。

所有准入决定完成后，Runtime SHALL 执行已 started 的调用并在固定 slot 收集结果；cancelled 调用也占据原 call slot，最终 result facts 按 call index 写入。若某次 started append 确定失败，该调用及后续调用不得执行；更早已 accepted 的调用仍 MUST 恰好执行一次。无法确认 started 是否 durable 的失败 MUST poison Session；恢复阶段不得猜测或自动执行。

同一 invocation ID MUST NOT 自动执行两次。恢复阶段 MUST NOT 执行任何 invocation：`ready_durable` 且没有 started 的调用关闭为 `cancelled/session_interrupted_before_execution`；started 但没有 result 的调用关闭为 `outcome_uncertain`。当前 Read、Glob、Grep 均不声明可恢复重试例外。

#### Scenario: Replay a completed invocation
- **WHEN** Runtime 再次遇到已有 durable result 的 invocation ID
- **THEN** 系统复用已保存结果且 executor 调用次数不增加

#### Scenario: Cancel before execution admission
- **WHEN** ready batch durable 后 turn context 在某调用的 started admission 前取消
- **THEN** 该调用获得原 call slot 的 cancelled result且 executor 调用次数为零

#### Scenario: Recover before execution starts
- **WHEN** 进程在 ready fact durable 后、execution started 前退出
- **THEN** resume 使用相同 invocation ID 追加 cancelled result，且对应 executor 调用次数为零

#### Scenario: Cancellation races with executor admission
- **WHEN** ready 已 durable 且 turn context 与 started admission 竞态
- **THEN** admission 未接受则记录 cancelled 且不调用 executor；admission 已接受则恰好调用一次 executor

#### Scenario: Cancellation follows accepted admission
- **WHEN** 某调用的 started append 已被接受后 turn context 取消
- **THEN** Runtime 恰好调用一次匹配 executor并传入已取消 context
- **THEN** sibling 的 result 最终仍按原 call index durable

#### Scenario: Fail during ordered admission
- **WHEN** call 0 的 started 已确定 durable，而 call 1 的 started append 确定失败
- **THEN** call 0 仍恰好执行一次，call 1 及其后 calls 不执行
- **THEN** Session 以失败或 poisoned 状态阻止新的 Provider 副作用

#### Scenario: Recover an uncertain invocation
- **WHEN** journal 包含 execution started 但没有对应 result
- **THEN** resume 不调用任一 executor，并把 invocation 收口为 `outcome_uncertain`

### Requirement: Every durable call receives a durable Provider output

每个 durable tool call 最终 MUST 具有一个确定 result 状态和一个匹配原 Provider call ID 的 Provider-native output。成功、验证后执行错误、取消和 `outcome_uncertain` SHALL 使用 typed result status 与稳定安全的英文 code；Provider-specific codec SHALL 将按 call index 冻结的模型 preview 编码为合法原生 output。

result fact MUST 在 next sample 前 durable，并保存模型实际接收的完整有界 preview bytes 与 capability 对应的非敏感 typed metadata；不得保存或分派 result codec revision。resume MUST 从该 fact 重建 output，不得重新执行工具、重新读取或搜索文件，也不得按新预算重算。若 results 已 durable 而 output commit 尚未 durable，恢复 SHALL 从已保存的完整 call group 按 call index 生成 output commit；若 output commit 已 durable，则不得重复追加。补偿完成后 resume MUST 以一个 `turn_failed` 结束旧 turn，且不得自动继续 Provider sample。

#### Scenario: Resume between results and output commit
- **WHEN** 一个异构调用组的全部 results 已 durable 但进程在 Provider-native output commit 前退出
- **THEN** resume 使用逐字节相同的有序 preview 完成一个 output commit，且不再次读取或搜索文件

#### Scenario: Resume between result and output commit
- **WHEN** 单个 Read result 已 durable 但进程在 Provider-native output commit 前退出
- **THEN** resume 使用完全相同的 preview bytes 完成 output commit，且不再次读取文件

#### Scenario: Pair an execution error
- **WHEN** Grep executor 返回安全的 typed error result
- **THEN** Provider history 得到与原 call ID 匹配的错误 output，并允许下一 sample 决定如何响应

#### Scenario: Cancel a committed call group
- **WHEN** turn 在 ready calls durable 后被取消
- **THEN** 每个尚无结果的 call 按原 index 获得取消或不确定补偿 output，随后 turn 只产生一个失败终态

#### Scenario: Cancel a committed call
- **WHEN** turn 在单个 ready call durable 后被取消
- **THEN** 该 call 获得取消或不确定补偿 output，随后 turn 只产生一个失败终态
