## MODIFIED Requirements

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample不含tool calls时 SHALL结束turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch、Write、Exec 或 Question 作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

当前Read、Glob、Grep、ReadArtifact、Skill属于`parallel_read`，Patch、Write、Exec与Question属于`exclusive`。单个调用的普通执行错误、拒绝、冲突或无匹配结果不得取消independent sibling；`abort`、turn取消或poisoned状态按取消契约收口。goroutine完成顺序、approval响应时间和实际执行时间不得影响Session seq、Provider output顺序、preview bytes或后续请求。超过sample/call上限 SHALL以稳定英文`tool_loop_limit_exceeded`失败，并且已经durable的sample、approval、ledger和outputs不得被删除或改写。

本条的 exclusive 屏障和串行审批语义 SHALL 同样适用于 Write；不能将非 Patch 调用默认视作 parallel_read。

存在匹配 PreToolUse 时，Patch/Write/Exec SHALL 在同一 exclusive 屏障内先冻结并 durable 保存完整 Hook 计划，串行完成检查，再准备候选与普通审批。Hook 的计划、信任、进程及结果不得越过前序 result 或与后序工具重叠；只读调用不触发本切片 Hook。无匹配时 SHALL 保持原调度、九能力目录与 Provider facade 不变。单个 Hook denied/error 只阻断原 call slot，后序 independent sibling 仍按原序执行；取消、cleanup 不确定或 writer poison 才停止整个 turn。

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
