## MODIFIED Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在用户 turn 的首次 Provider 请求前产生一次 `turn_started`。对该 turn 的每个 Provider sample，Runtime MUST 创建独立派生 context，逐项验证事件，记录唯一 terminal，并持续消费直到生产者关闭 channel；channel 关闭且此前恰好收到一个合法 completed terminal时，该 sample 才成功。provider channel 关闭本身 MUST NOT 被解释为 sample 或 turn 成功。

成功 sample 含 ready calls 时，Runtime SHALL 在工具处理和原生 outputs durable 后开始下一 sample；成功 sample 不含 calls 时才能产生一次 `turn_completed`。任一 sample 的零值/非法事件、terminal 前关闭、多个 terminal或terminal 后事件 MUST 作为 stream protocol failure，并由同一 owner 取消、排空、等待生产者清理后收口。一个 turn 无论包含多少 samples 都只能产生一个成功或失败终态。

#### Scenario: Complete a successful turn
- **WHEN** 首个 sample 产生文本和唯一合法 completed terminal且不含 tool calls
- **THEN** Runtime 输出一次 `turn_started`、中间文本和一次 `turn_completed`

#### Scenario: Complete a tool turn
- **WHEN** 首个 sample 请求 Read，结果提交后第二个 sample 返回最终文本且不含 calls
- **THEN** Runtime 在两个 Provider channel均完成清理后只发布一个 `turn_completed`

#### Scenario: Fail when stream closes before terminal
- **WHEN** 任一 Provider channel 在 completed、failed 或 cancelled 之前关闭
- **THEN** Runtime 输出一次 `turn_failed`并返回 `stream_protocol_error`

#### Scenario: Drain after an invalid stream event
- **WHEN** 任一 sample 发布非法事件、重复 terminal或terminal 后事件
- **THEN** Runtime 立即取消并排空该 sample至 channel关闭，随后只输出一次 `turn_failed`

#### Scenario: Propagate provider failure
- **WHEN** 任一sample产生唯一合法failed terminal并完成清理
- **THEN** Runtime输出一次 `turn_failed`并且不输出 `turn_completed`

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在首次 Provider 请求前 durable append 当前 `turn_started`；失败时不得建立网络流。每个 completed sample 的 `provider_native_commit`、`sample_usage` 和全部 ready-call facts MUST 在同一 batch append/Sync 后 finalize，随后才允许 executor I/O。每个 invocation 的 execution-start fact MUST 在 executor 接收前单独 durable。

全部 result facts durable 后，匹配的 Provider-native tool outputs MUST 在下一 Provider sample前 append/Sync并 finalize。最终无 calls sample 的 native commit、sample usage和 `turn_completed` SHALL 在同一 batch durable；携带聚合 usage 的 `turn_completed` RuntimeEvent 只有在 finalizer 成功后才能发布。

Provider/工具失败、取消、协议错误或 append/Sync 失败 MUST 通过合法 result/output配对与唯一 `turn_failed` 收口。任何 durability 状态不确定的 Session MUST poisoned并拒绝新 turn或新副作用，直到重新加载验证。

#### Scenario: Persist before starting a Provider request
- **WHEN** Session 无法 durable append `turn_started`
- **THEN** Runtime 返回 Session 错误且 Provider和Read均不被调用

#### Scenario: Persist a call before Read
- **WHEN** Provider完成合法 Read call sample
- **THEN** native commit、usage和ready fact先成功 Sync，execution-start再成功 Sync，之后Read才可接收调用

#### Scenario: Fail while persisting a completed sample
- **WHEN** sample已完整归并但 ready batch append或Sync失败
- **THEN** Runtime 不finalize sample、不调用Read并将当前进程Session poisoned

#### Scenario: Continue only after durable outputs
- **WHEN** Read results已生成但tool-output native commit尚未Sync
- **THEN** Runtime不得建立下一Provider stream

#### Scenario: Complete a durably committed sample
- **WHEN** 最终sample不含calls且其native commit、usage和completion成功Sync
- **THEN** Conversation finalize该sample，随后Runtime发布一次带聚合usage的完成事件

#### Scenario: Cancel before Provider commit
- **WHEN** 首个sample在成功terminal前取消
- **THEN** Runtime不写入native commit或sample usage，并以唯一失败边界收口

### Requirement: Turn completion exposes typed aggregated usage

成功 `turn_completed` RuntimeEvent SHALL 携带强类型 normalized turn usage；payload 必须遵守统一指标状态和聚合规则，不得包含 Provider wire、raw usage、价格或上下文占用。单 sample turn的payload MUST等于该sample usage；多 sample Tool Loop MUST按durable sample顺序聚合所有usage。

失败或取消终态 MUST NOT 携带伪造的成功 turn usage。已经 durable 保存的 sample usage 即使发生在后续工具或 Provider 失败之前，也不得删除或改写；当前 `turn_failed` 仍不暴露部分 usage。

#### Scenario: Publish aggregated tool-turn usage
- **WHEN** 一个成功Read turn包含三个durable samples
- **THEN** `turn_completed` payload等于按顺序聚合三个sample usage的结果

#### Scenario: Publish current text turn usage
- **WHEN** 单sample文本turn durable完成
- **THEN** `turn_completed` payload与该sample usage完全相同

#### Scenario: Do not publish usage before durability
- **WHEN** 最终完成batch尚未成功Sync
- **THEN** Runtime不发布 `turn_completed`或任何成功聚合usage

#### Scenario: Keep failed terminal shape unchanged
- **WHEN** turn在已有中间sample后失败或取消
- **THEN** Runtime只发布既有强类型 `turn_failed`，同时Session保留已提交sample usage

### Requirement: Runtime plans context before Provider side effects

Runtime SHALL 在 `turn_started` 已 durable、每次 Provider stream之前，使用同一不可变项目指令与Tool Catalog snapshots、当前已提交Conversation history以及本轮输入状态生成上下文计划。首次sample包含真实用户输入；后续sample只从已提交Provider-native tool outputs继续，不得把result、RuntimeEvent或SemanticHistoryView伪造成新的共享user文本。

规划失败或预算明确 `over_limit` 时，Runtime MUST durable收口当前turn且不得建立该Provider stream。Tool Catalog segment必须参与stable-prefix fingerprint和估算，但catalog、workspace绝对路径、invocation identity、ledger和结果正文不得新增RuntimeEvent或独立prompt文本。

#### Scenario: Plan the first tool sample
- **WHEN** 一个新turn开始
- **THEN** 计划按固定顺序包含Provider profile、Tool Catalog、可选项目指令、历史和当前输入

#### Scenario: Plan after tool outputs
- **WHEN** Read outputs已durable并finalize到原生history
- **THEN** 下一sample从更新后的Provider history编译，且不把同一tool result再作为共享user输入附加

#### Scenario: Stop a confirmed over-limit turn before networking
- **WHEN** tool outputs加入history后下一sample规划明确over-limit
- **THEN** Runtime不发起网络请求并以 `context_limit_exceeded` durable收口turn

#### Scenario: Continue when no window is configured
- **WHEN** 任一sample计划的预算状态为 `not_enforced`
- **THEN** Runtime使用相同snapshots和原生history启动Provider stream

#### Scenario: Continue with an indeterminate estimate
- **WHEN** 已配置窗口但计划因必需估算unknown而为 `indeterminate`
- **THEN** Runtime不把不确定性转换为明确超限并继续既有生命周期

#### Scenario: Close a planning failure durably
- **WHEN** 已durable开始的turn在任一sample规划中失败
- **THEN** Runtime写入唯一失败边界且不建立该Provider stream

#### Scenario: Keep public event and journal vocabularies unchanged
- **WHEN** Tool Catalog规划成功或失败
- **THEN** Runtime不为ContextPlan本身新增RuntimeEvent或Session record，并且不持久化prompt正文
