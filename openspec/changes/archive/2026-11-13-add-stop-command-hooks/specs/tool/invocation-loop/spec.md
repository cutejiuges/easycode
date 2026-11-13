## MODIFIED Requirements

### Requirement: Tool turns use a bounded ordered multi-sample loop

一个用户turn SHALL支持最多16个Provider samples和累计最多64个tool calls。completed sample 不含 tool calls 时 SHALL 进入完成边界：合法无 Stop 沿原路径结束，有 Stop 时先保存 sample/usage/plan，检查通过才完成，正常授权阻断则按 Stop 契约继续同 turn；含calls时，Scheduler SHALL按call index把连续`parallel_read`调用组成最大8并发的批次，并把每个`exclusive` Patch、Write、Exec 或 Question 作为前后屏障。Runtime SHALL在到达每个调用或批次时完成其policy；Patch候选准备和所需approval只能在其前序批次结果durable后发生，matching decision收口后才可越过该屏障。结果收集到固定call slot并按call index durable提交全部results和匹配Provider-native outputs，outputs durable后才可开始下一sample。

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
