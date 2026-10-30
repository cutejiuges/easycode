## MODIFIED Requirements

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

预算不足、保存失败及恢复补偿 SHALL 同样获得有界 typed result 与匹配 output；预算元数据与最终 preview/保存决定在 result Sync 前冻结。已有 results 恢复时不重新分配预算或访问保存对象，补偿只为缺失 result 使用当前安全有界策略。七工具与 search_result 均遵守上述规则。

#### Scenario: Recover a reduced search result
- **WHEN** 搜索缩短结果与保存引用已 durable 而 native output 未提交，保存文件随后丢失
- **THEN** 恢复只使用冻结 bytes，canonical request、native item 顺序与 fingerprint 与不中断路径等价
