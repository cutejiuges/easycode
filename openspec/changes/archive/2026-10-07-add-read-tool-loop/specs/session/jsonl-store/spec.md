## MODIFIED Requirements

### Requirement: Session records use a versioned extensible envelope

每条JSONL记录 SHALL使用既有强类型、版本化envelope，至少包含schema/payload revision、replay requirement、单调seq、UTC timestamp、session/thread/turn identity、event kind、batch边界、checksum和受控payload。未知payload只可作为有界opaque JSON传递，不得展开为跨层 `map[string]any`。

影响Provider原生历史、turn lifecycle、usage、权限、工具副作用、幂等或恢复决策的记录 MUST标记 `required`。本变更 SHALL在当前记录词汇中新增 `tool_call_ready`、`tool_execution_started` 和 `tool_call_result`，三者全部为required并具有专属typed constructor、strict decoder与validator。数值payload revision只存在于record envelope，不进入Go业务类型或构造器名称。

`tool_call_ready` SHALL保存invocation ID、Provider call ID、sample/call index、capability及input revision和完整typed Read input；`tool_execution_started` SHALL引用同一invocation并表示executor接收线性化点；`tool_call_result` SHALL保存终态status、稳定code、result codec revision、完整有界模型preview与非敏感结果metadata。共享ledger不得嵌入Provider wire item，Provider call/output仍保存在 `provider_native_commit`。

completed sample的Provider entry仍作为native commit并紧邻配对 `sample_usage`。tool results编码为tool-output native commit时不生成sample usage。高频参数delta、进度、UI状态和文件系统瞬时信息 MUST NOT进入JSONL。

#### Scenario: Encode the initial record vocabulary
- **WHEN** 一个root thread完成一次Read Tool Loop
- **THEN** JSONL复用同一envelope记录metadata、turn边界、sample commits/usages、三类ledger records和tool-output commit
- **THEN** 每个已知kind/revision均可独立校验且标记为required

#### Scenario: Preserve an unknown optional record
- **WHEN** 程序读取结构和checksum合法但未知且标记optional的记录
- **THEN** loader有界保留opaque payload并允许恢复消费者忽略，且payload不进入日志或错误

#### Scenario: Represent multiple samples in one future turn
- **WHEN** Tool Loop在同一turn记录多次Provider samples
- **THEN** 每个sample按seq追加配对native commit和sample usage，无需改变envelope或改写既有records

#### Scenario: Commit a future tool result before the next sample
- **WHEN** tool results已durable且下一sample尚未发起
- **THEN** tool-output native commit单独持久化且不伪造sample usage
- **THEN** 恢复无需重复工具I/O即可继续

#### Scenario: Preserve a future native tool pair without conflating execution state
- **WHEN** sample native commit包含原生tool calls
- **THEN** Provider codec保留原生内容，而ready/started/result状态只由独立ledger records表达

### Requirement: Replay validation enforces record state transitions

Loader完成字节、envelope和batch校验后，ReplayPlanner SHALL在创建Provider Conversation前对全部known required records执行强类型语义回放。首个batch MUST恰好建立唯一 `session_meta`与root `thread_meta`；同一root thread最多有一个活动turn。

文本turn仍允许 `turn_started` 后以 `[provider_native_commit, sample_usage, turn_completed]` 完成，或以单个 `turn_failed` 失败。Tool Loop中，一个含calls的sample batch MUST严格为 `[provider_native_commit, sample_usage, tool_call_ready...]`，ready records的sample/call index连续且call identity唯一；正常执行按 `ready -> execution_started -> tool_call_result` 单向转换。取消在Executor接受前线性化时，唯一允许的旁路是 `ready -> tool_call_result(status=cancelled)`；ready直接进入success、error或outcome-uncertain result MUST被拒绝。全部results存在后，恰好一个tool-output `provider_native_commit`关闭该call组，之后才可出现下一sample或turn failure。最终无calls sample必须以 `[provider_native_commit, sample_usage, turn_completed]` 收口。

ReplayPlanner MUST拒绝孤立/重复result、started早于ready、call index乱序、ready后直接开始下一sample、outputs数量不匹配、sample usage缺失/重复、tool-output commit携带usage、terminal后records或新turn覆盖活动turn。只有journal末尾的已承诺活动状态可作为显式reconciliation计划返回；它不是可由Loader截断的文件损坏。

#### Scenario: Build a valid text replay plan
- **WHEN** journal包含metadata、一个失败文本turn和一个三记录完成文本turn
- **THEN** ReplayPlanner产生当前schema定义的native commits、usage和terminal投影

#### Scenario: Build a valid tool replay plan
- **WHEN** journal包含call sample、两个顺序invocations、tool outputs和最终sample completion
- **THEN** ReplayPlanner按seq返回native commits、sample usages、ledger状态和completed turn

#### Scenario: Reject a checksum-valid illegal transition
- **WHEN** JSON、seq、batch和checksum合法但usage配对、ledger顺序、call index或terminal placement非法
- **THEN** resume以稳定Session corruption错误失败且不创建Conversation或调用executor

#### Scenario: Report a reconcilable ready tail
- **WHEN** journal在合法ready batch后结束且没有started
- **THEN** ReplayPlanner保留committed records并返回待本地取消补偿的ready状态，不截断或补造result

#### Scenario: Accept cancellation before executor start
- **WHEN** journal包含ready后直接追加的matching cancelled result
- **THEN** ReplayPlanner接受该终态，并拒绝同一invocation后续出现started或第二个result

#### Scenario: Reject non-cancelled result before executor start
- **WHEN** journal包含ready后直接追加的success、error或outcome-uncertain result
- **THEN** ReplayPlanner以稳定Session corruption错误失败

#### Scenario: Report an uncertain execution tail
- **WHEN** journal在合法execution-start后结束且没有result
- **THEN** ReplayPlanner返回必须补偿的uncertain状态且不得把调用重新归类为ready

#### Scenario: Report a committed interrupted tail
- **WHEN** 文本journal以完整committed `turn_started`结束且没有后续事实
- **THEN** ReplayPlanner保留该record并返回既有interrupted-tail状态而不截断

## REMOVED Requirements

### Requirement: Known record revisions expose typed construction and decoding

**Reason**: 该requirement把尚未发布的开发期Provider payload和fixture当作永久历史契约，迫使Tool Loop为sample、tool outputs和continuation维护多套版本结构。

**Migration**: 当前没有线上用户或需保留journal；本变更一次性替换开发期fixture和codec，不提供旧payload reader。record envelope仍保留单一当前revision及未知required revision失败关闭能力。

## ADDED Requirements

### Requirement: Current record schema exposes typed construction and decoding

每个当前已知Session `event_kind`/`payload_version`组合 SHALL具有专属强类型draft constructor、strict decoder和validator。公共record draft、writer、replay和lifecycle接口 MUST NOT接受或返回无约束动态值；未知扩展只允许作为有大小边界的opaque JSON保留。构造和解码都 SHALL拒绝字段缺失、未知字段、尾随JSON和不满足revision语义的payload。业务类型和构造器不得用版本后缀复制当前schema。

当前十种record codec SHALL深拷贝preview/input等可变bytes，并验证identity、index、status与大小边界。由于当前没有线上用户，本变更 SHALL一次性重写仓库开发期fixture，以一套当前fixture固定canonical JSON、envelope、checksum和ReplayPlan；实现不得保留旧Provider payload reader、旧fixture兼容分支或混合revision恢复路径。

#### Scenario: Construct every tool record through a typed API
- **WHEN** 调用方创建ready、execution-started或result draft
- **THEN** constructor只接收该kind/revision的强类型值且立即验证
- **THEN** draft不能被改造成kind、revision与payload不匹配的记录

#### Scenario: Construct every current record through a typed API
- **WHEN** 调用方创建任一当前required record
- **THEN** 对应constructor只接收该kind的强类型字段且立即验证当前revision

#### Scenario: Strictly decode a tool result
- **WHEN** checksum合法的result payload包含未知字段、非法status、超限preview或尾随JSON
- **THEN** decoder在replay前拒绝记录且错误不回显preview正文

#### Scenario: Strictly decode a known payload revision
- **WHEN** 任一checksum合法的known payload包含未知字段、尾随JSON或非法语义
- **THEN** revision专属decoder在replay前拒绝且错误不包含payload正文

#### Scenario: Replay the current immutable tool fixture
- **WHEN** 当前实现加载仓库内固定的Tool Loop fixture
- **THEN** 生产Loader、registry、codec和ReplayPlanner产生预期ledger及native commit摘要
- **THEN** fixture不是测试运行时由当前encoder生成

#### Scenario: Reject a superseded development fixture
- **WHEN** Loader读取已被本变更替换的旧Provider payload shape或revision
- **THEN** 恢复在repair、append、executor或Provider网络调用前失败，不进入兼容分支

## MODIFIED Requirements

### Requirement: Sample usage is committed atomically with its sample

每个成功Provider sample SHALL在一个durable batch中依次写入 `provider_native_commit`、`sample_usage`以及该sample的后续边界。无tool call的最终sample后续边界为 `turn_completed`；含tool calls的sample后续边界为一个或多个 `tool_call_ready`。同一batch MUST使用相同session/thread/turn和batch identity并获得连续seq；只有整个batch成功Sync后才能确认任一事实。

tool-output native commit MUST在全部对应result facts durable后单独提交且不得带 `sample_usage`。`sample_usage` payload继续完整包含五个normalized metrics，不得包含Provider raw usage、价格、请求正文或tool result。

#### Scenario: Commit a new sample usage batch
- **WHEN** completed sample不含tool calls
- **THEN** journal原子追加native commit、sample usage和turn completion

#### Scenario: Commit a call sample
- **WHEN** completed sample按顺序包含两个Read calls
- **THEN** journal原子追加native commit、sample usage和两个ready records
- **THEN** executor只在整个batch Sync后才可接收调用

#### Scenario: Fail an incomplete new usage batch
- **WHEN** 尾部sample batch缺少usage、ready或completion中的任一必需记录
- **THEN** Loader按既有batch repair规则移除整个未完成batch，不恢复部分history、usage或call

#### Scenario: Fail closed on the superseded development baseline
- **WHEN** Loader读取缺少sample usage或使用旧Provider payload shape的开发期fixture
- **THEN** resume在repair、append、executor或Provider调用前失败，不保留兼容reader或原地改写journal

#### Scenario: Reject usage on a tool-output commit
- **WHEN** journal把tool-output commit与sample usage配对
- **THEN** ReplayPlanner拒绝该状态而不是把output误计为Provider sample
