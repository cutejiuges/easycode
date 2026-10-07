# tool/invocation-loop Specification

## Purpose

定义首个可恢复 Tool Loop 的目录、调用、持久化与多次采样行为，使模型能够安全调用真实能力，同时保持 Provider 原生历史、Session ledger、取消和崩溃恢复的一致性。

## Requirements

### Requirement: Tool catalog snapshots are deterministic and executable

每次 Provider sample SHALL 使用一个不可变 Tool Catalog snapshot。snapshot 中每个 facade MUST 关联已注册且可执行的 capability、typed input revision、Provider facade 和结果 codec；系统 MUST NOT 暴露没有 executor、validator、result codec 或 Runtime 消费者的 schema。

当前 snapshot SHALL 只包含 `fs.read` 的 Read facade，并按稳定 capability ID 与 facade name 确定性排序。snapshot SHALL 具有非空 source revision、canonical schema bytes 和内容派生 fingerprint；调用方修改 getter 返回值不得改变 snapshot。时间戳、cwd、Session/thread/turn identity、随机 invocation ID、权限临时状态和文件系统枚举顺序 MUST NOT 进入 schema bytes 或 fingerprint。

#### Scenario: Build the same catalog twice
- **WHEN** 两次使用相同 Read facade revision、描述和 schema 创建 catalog snapshot
- **THEN** facade 顺序、canonical bytes、source revision 和 fingerprint 完全相同

#### Scenario: Reject a facade without an executor
- **WHEN** catalog 尝试注册只有 schema 而没有匹配 executor、validator 或 result codec 的 facade
- **THEN** snapshot 构造失败且该 facade 不会出现在任一 Provider 请求中

#### Scenario: Expose only Read
- **WHEN** Runtime 为本变更编译一次 Provider 请求
- **THEN** catalog 只声明 Read，不声明 Glob、Grep、Edit、Write、Bash、Skill、MCP 或 Agent

### Requirement: Completed samples produce typed ready calls

Provider completed sample SHALL 将原生 native commit、normalized usage 与按模型 item 顺序排列的 ready calls 作为一个不可分割的 prepared value 交给 Runtime。每个 ready call MUST 包含合法 Provider call ID、非空 facade identity、已解析的 capability、input revision 和经过 strict decode 与语义校验的 typed input；核心导出边界 MUST NOT 接受或返回 `any` 或无约束动态 map。

参数 delta、未闭合原生 item、未知工具、重复 Provider call ID、非法 schema、未知字段、尾随 JSON 或超限输入 MUST 使 sample 失败，不得产生 ready call、native commit、sample usage 或工具 I/O。

#### Scenario: Prepare an ordered Read call
- **WHEN** Provider sample 完成一个参数合法的 Read 调用
- **THEN** prepared sample 同时包含对应原生 call、normalized usage 和一个 typed ready call
- **THEN** ready call 的位置与原生模型调用顺序一致

#### Scenario: Reject partial arguments
- **WHEN** stream 在 Read 参数尚未完整时结束或参数无法 strict decode
- **THEN** sample 以协议错误失败，Session 不写入成功 sample facts且 Read executor 调用次数为零

#### Scenario: Reject an unadvertised tool
- **WHEN** Provider 返回当前 catalog snapshot 中不存在的工具名称
- **THEN** sample 失败且系统不按名称猜测 executor 或降级到通用 JSON 工具

### Requirement: Durable sample facts precede tool execution

Runtime MUST 在任何工具 I/O 前，将当前 sample 的 Provider-native commit、sample usage 与全部 `tool_call_ready` required facts 作为同一 Session batch 成功 `Sync`，随后执行 prepared sample finalizer。每个 ready fact SHALL 分配全局唯一、格式可验证的 EasyCode invocation ID，并保存 Provider call ID、sample index、call index、capability revision 和完整 typed input。

batch 构造、append、Sync 或 finalizer 失败 MUST 使 turn 进入失败或 poisoned 状态，且不得调用任何 executor。sample 成功 durable 后，网络重连、UI 重连和重复 Runtime 消费不得创建新的 invocation ID 或重复执行。

#### Scenario: Fail before executor admission
- **WHEN** 包含 Read call 的 sample 无法 durable 写入 native commit、usage 与 ready facts
- **THEN** Runtime 产生唯一失败终态且 Read executor 调用次数为零

#### Scenario: Persist all calls atomically
- **WHEN** 一个 sample 按顺序包含两个合法 Read calls
- **THEN** native commit、sample usage 和两个 ready facts 在同一 committed batch 中获得连续 seq
- **THEN** 任一记录缺失时整个 batch 都不能成为可恢复事实

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户 turn SHALL 支持最多 16 个 Provider samples 和累计最多 64 个 tool calls。completed sample 不含 tool calls 时 SHALL 结束 turn；含 calls 时，Runtime SHALL 按 call index 顺序执行并生成结果，durable 提交全部匹配的 Provider-native outputs 后才可开始下一 sample。

当前切片 MUST 顺序执行 Read calls，MUST NOT 声明或实现并行工具。执行完成顺序、Session result 顺序、Provider output 顺序和 call index SHALL 一致。超过任一上限 SHALL 以稳定英文 `tool_loop_limit_exceeded` 失败，并且已经 durable 的 sample、ledger 和 outputs 不得被删除或改写。

#### Scenario: Complete a two-sample Read turn
- **WHEN** 首个 sample 请求 Read，工具结果提交后第二个 sample 返回最终文本且不再调用工具
- **THEN** turn 只产生一个开始和一个成功终态，并在两次 sample 之间保持合法 native call/output 顺序

#### Scenario: Execute multiple calls sequentially
- **WHEN** 同一 sample 产生两个 Read calls
- **THEN** 第二个 invocation 只在第一个 invocation 得到 durable result 后开始
- **THEN** outputs 按原 call index 编码而不是按其他顺序重排

#### Scenario: Stop an infinite tool loop
- **WHEN** 模型持续请求工具并达到 sample 或 call 上限
- **THEN** Runtime 不发起超限 sample或 invocation，并以 `tool_loop_limit_exceeded` durable 收口 turn

### Requirement: Execution ledger provides at-most-once automatic execution

每个 invocation 的正常路径 SHALL 遵循 `ready_durable -> execution_started_durable -> result_durable` 的单向 ledger。Runtime MUST 在 executor 接收调用前单独 durable 写入 `tool_execution_started`；该append被接受的成功点是执行接收线性化点。在它之前取消 MUST直接写入cancelled result且不得产生工具I/O；在它之后Runtime MUST恰好调用一次Executor，即使传入context已取消，并不得将调用自动当作从未开始。

同一 invocation ID MUST NOT 自动执行两次。恢复阶段 MUST NOT执行任何invocation：`ready_durable`且没有started的调用关闭为 `cancelled/session_interrupted_before_execution`；started但没有result的调用关闭为 `outcome_uncertain`。只有 capability 自身另有已批准的可靠幂等协议时才能声明更强保证；当前 Read 不作该例外。

#### Scenario: Replay a completed invocation
- **WHEN** Runtime 再次遇到已有 durable result 的 invocation ID
- **THEN** 系统复用已保存结果且 executor 调用次数不增加

#### Scenario: Recover before execution starts
- **WHEN** 进程在 ready fact durable 后、execution started 前退出
- **THEN** resume使用相同invocation ID追加 `session_interrupted_before_execution` cancelled result
- **THEN** Read executor调用次数保持为零

#### Scenario: Cancellation races with executor admission
- **WHEN** ready已durable且turn context取消
- **THEN** started append未被接受时Runtime直接记录cancelled result且不调用Executor
- **THEN** started append已被接受时Runtime恰好调用一次Executor并传入已取消context

#### Scenario: Recover an uncertain invocation
- **WHEN** journal 包含 execution started 但没有对应 result
- **THEN** resume 不调用 Read executor，并把 invocation 收口为 `outcome_uncertain`

### Requirement: Every durable call receives a durable Provider output

每个 durable tool call 最终 MUST 具有一个确定 result 状态和一个匹配原 Provider call ID 的 Provider-native output。成功、验证后执行错误、取消和 `outcome_uncertain` SHALL 使用 typed result status 与稳定安全的英文 code；Provider-specific codec SHALL 将冻结的模型预览编码为合法原生 output。

result fact MUST 在 next sample 前 durable，并保存模型实际接收的完整有界 preview bytes 与 result codec revision。resume MUST 从该 fact 重建 output，不得重新执行工具、重新读取文件或按新预算重算。若 result 已 durable 而 output commit 尚未 durable，恢复 SHALL 从已保存 results 生成 output commit；若 output commit 已 durable，则不得重复追加。补偿完成后resume MUST以一个 `turn_failed`结束旧turn，且不得自动继续Provider sample。

#### Scenario: Resume between result and output commit
- **WHEN** Read result 已 durable 但进程在 Provider-native output commit 前退出
- **THEN** resume 使用完全相同的 preview bytes 完成 output commit，且不再次读取文件

#### Scenario: Pair an execution error
- **WHEN** Read executor 返回安全的 typed error result
- **THEN** Provider history得到与原 call ID 匹配的错误 output，并允许下一 sample决定如何响应

#### Scenario: Cancel a committed call
- **WHEN** turn 在 ready call durable 后被取消
- **THEN** 每个尚无结果的 call 获得取消或不确定补偿 output，随后 turn 只产生一个失败终态
