## MODIFIED Requirements

### Requirement: Only one active turn mutates a conversation

同一个会话级 Runtime SHALL 在任意时刻最多允许一个活动 turn 修改 provider 原生历史。并发提交 MUST 由唯一控制 owner 排队或拒绝，不能并行写入同一 conversation history。

#### Scenario: Submit while a turn is active

- **WHEN** 一个 turn 尚未结束时再次提交输入
- **THEN** 第二次提交不会启动并发 provider stream
- **THEN** 已运行 turn 的原生历史顺序保持不变

### Requirement: Runtime events carry stable Session identity

Session-bound Runtime SHALL 在接受 turn 前获得有效的 `session_id` 与 `thread_id`，并为每次新 turn 分配新的 `turn_id`；同一 turn 消费的自动输入共享该标识，但每条输入保持独立 InputID。该 turn 产生的 `turn_started`、assistant 语义事件和唯一终态事件 MUST 携带相同的 session/thread/turn 标识；Provider 产生的语义 payload 不得自行决定或覆盖这些标识。进程内 RuntimeEvent SHALL 使用唯一当前强类型 kind/payload，不携带 format version，也不提供 JSON decoder；headless adapter 负责把它单向投影为外部 JSONL v1。Runtime MUST NOT 接受包含未收口 interrupted-tail turn 的恢复计划；应用必须先 durable 记录对应失败边界。

#### Scenario: Decorate a successful turn
- **WHEN** Runtime 在一个 root thread 中执行成功文本 turn
- **THEN** 从 `turn_started` 到 `turn_completed` 的全部事件携带相同 session/thread/turn ID
- **THEN** 下一 turn 保留 session/thread ID 并获得不同的 turn ID

#### Scenario: Decorate a failed turn
- **WHEN** Provider 请求失败、取消、timeout 或协议错误结束 turn
- **THEN** `turn_failed` 与此前事件携带相同 session/thread/turn ID

#### Scenario: Reject an unresolved resumed turn
- **WHEN** 应用尝试用仍包含 interrupted-tail 活动 turn 的恢复计划构造或调用 Runtime
- **THEN** Runtime 在启动 Provider 或分配新 turn ID 前拒绝该状态

#### Scenario: Project an external version at the adapter
- **WHEN** headless adapter 接收一个已验证的当前进程内 RuntimeEvent
- **THEN** adapter 生成带 `version: 1` 的外部 JSONL 对象，而内部 event 保持无版本强类型值

用户输入 committed 事件 SHALL 携带已保存正文、InputID 和实际 turn identity；仅在对应输入 Sync/finalize 后发布，不能由队列 admission 推断。

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在首次 Provider 请求前依次提交 turn_started、纯内存预检候选输入、durable 提交 initial 用户输入 batch 并 finalize；任一步失败时不得建立网络流。每个completed sample的`provider_native_commit`、`sample_usage`和全部ready-call facts MUST在同一batch append/Sync后finalize，随后才允许候选准备或executor I/O。

Runtime SHALL按call index与调度屏障完成policy及所需approval。Patch候选只能在ready durable且前序批次结果durable后读取目标；ask decision只有在匹配`tool_approval_decided(allow*)`成功Sync后才可进入执行准入，deny decision必须与denied result同batch durable且零executor I/O。Runtime SHALL按调度批次逐一durable`tool_execution_started`：连续`parallel_read`批次的全部准入完成后最多启动8个workers，每个`exclusive` Patch在前序批次完成后准备候选、请求approval并独占执行。worker/executor只返回固定call slot的typed result，不得写Session或Provider history；协调owner按call index durable result facts。全部results durable后，匹配的Provider-native tool outputs MUST在下一Provider sample前append/Sync并finalize。最终无calls sample的native commit、sample usage和`turn_completed` SHALL在同一batch durable；携带聚合usage的`turn_completed` RuntimeEvent只有在finalizer成功后才能发布。

Provider/工具/approval失败、取消、协议错误或append/Sync失败 MUST通过合法result/output配对与唯一`turn_failed`收口。任何durability状态不确定的Session MUST poisoned并拒绝新turn或新副作用，直到重新加载验证。若后续started admission失败，更早已经accepted的invocation仍由Runtime拥有并恰好执行一次；未accepted的invocation不得执行。approval等待、worker和executor均由同一turn owner持有明确取消与最终完成路径。

上述 Patch 的 exclusive、approval、started/result/output 顺序 SHALL 同样适用于 Write。Write 的发布成功后取消不能覆盖已经确定的文件结果：成功 Sync/cleanup 的 Write result 保持 success，turn 仍可独立取消。Read 观察只能在整组原生 outputs 成功 durable/finalize 后生效。文件结果不确定导致 Runtime poisoned 时，禁止后续工具执行/Provider 请求；若 journal 仍健康，owner SHALL 继续保存已取得结果及配对 output 并收口失败，journal 自身不健康则留待本地恢复补偿。

#### Scenario: Persist before starting a Provider request
- **WHEN** Session无法durable append`turn_started`
- **THEN** Runtime返回Session错误且Provider、候选准备和任一executor均不被调用

#### Scenario: Complete a durably committed sample
- **WHEN** 最终sample不含calls且其native commit、usage和completion成功Sync
- **THEN** Conversation finalize该sample，随后Runtime发布一次带聚合usage的完成事件

#### Scenario: Fail while persisting a completed sample
- **WHEN** sample已完整归并但ready batch append或Sync失败
- **THEN** Runtime不finalize sample、不准备patch候选、不调用任何tool executor并将当前进程Session poisoned

#### Scenario: Cancel before Provider commit
- **WHEN** 首个sample在成功terminal前取消
- **THEN** Runtime不写入 sample native commit 或 sample usage，已经 durable 的用户输入保持不变，并以唯一失败边界收口

#### Scenario: Persist a call before Read
- **WHEN** Provider完成包含合法Read call的sample
- **THEN** native commit、usage、ready与execution-start依次durable后，Read才可接收调用

#### Scenario: Persist approval before Patch
- **WHEN** Provider完成包含合法Patch call的sample且用户批准
- **THEN** native commit、usage、ready、allow decision与execution-start依次durable后，Patch executor才可接收调用

#### Scenario: Persist all admissions before parallel I/O
- **WHEN** Provider完成含Glob、Grep、Read的合法连续只读批次
- **THEN** native commit、usage和全部ready facts先在一个batch成功Sync
- **THEN** started facts再按call index逐个成功Sync，之后workers才可并行接收调用

#### Scenario: Enforce an exclusive patch barrier
- **WHEN** 一个已批准Patch位于两个只读批次之间
- **THEN** Runtime等待前序workers完成再准入并执行Patch，Patch结果durable后才准入后序批次

#### Scenario: Persist results in model order
- **WHEN** 多个workers以与call index不同的顺序完成
- **THEN** Runtime按call index写入results并构造outputs，Session writer只由协调owner调用

#### Scenario: Continue only after durable outputs
- **WHEN** tool results已生成但tool-output native commit尚未Sync
- **THEN** Runtime不得建立下一Provider stream

#### Scenario: Retain ownership after partial admission failure
- **WHEN** 一个早期started已accepted而后续started append返回失败
- **THEN** Runtime恰好执行早期invocation、禁止后续未accepted invocation并等待所有已启动工作结束
- **THEN** Session进入失败或poisoned状态且不发起下一Provider sample

#### Scenario: Separate committed Write from turn cancellation
- **WHEN** Write 发布并完成 Sync 后 turn 被取消
- **THEN** 文件 result 保存 success，后续未执行调用按取消收口，turn 只有一个取消终态

#### Scenario: Stop after an uncertain Write
- **WHEN** Write 发布或清理的结果无法确认
- **THEN** Runtime poisoned，等待所拥有清理完成，不执行后续 sibling 或发起下一 sample

中途输入 SHALL 只在整组工具 outputs 成功 Sync/finalize 后提交。writer admission 前取消不得写入；接收后必须返回确定提交结果，成功则 finalize 一次，即使取消也不得返还，无法确认则 poison 并禁止后续副作用。

#### Scenario: Cancel around user input durability
- **WHEN** 取消分别发生在 writer 接收前、接收后 Sync 成功或结果不确定
- **THEN** 输入分别确定返还、保留为 committed 或标记 uncertain；每条只有一种结果，后两者不得自动重试

### Requirement: Runtime plans context before Provider side effects

Runtime SHALL 在 `turn_started` 已 durable、每次 Provider stream之前，使用同一不可变项目指令与Tool Catalog snapshots、当前已提交Conversation history以及本轮输入状态生成上下文计划。首轮和中途用户输入均先以候选历史预检，通过后独立提交；实际 sample 只从已提交 Provider-native 用户输入和 tool outputs 继续，不得把result、RuntimeEvent或SemanticHistoryView伪造成新的共享user文本。

规划失败或预算明确 `over_limit` 时，Runtime MUST durable收口当前turn且不得建立该Provider stream。Tool Catalog segment必须参与stable-prefix fingerprint和估算，但catalog、workspace绝对路径、invocation identity、ledger和结果正文不得新增RuntimeEvent或独立prompt文本。

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

#### Scenario: Plan the first tool sample
- **WHEN** 一个新turn开始
- **THEN** 计划按固定顺序包含Provider profile、Tool Catalog、可选项目指令、历史和当前输入

#### Scenario: Plan after tool outputs
- **WHEN** Read outputs已durable并finalize到原生history
- **THEN** 下一sample从更新后的Provider history编译，且不把同一tool result再作为共享user输入附加

用户输入 SHALL 在 Sync 前经过纯内存候选上下文预算与请求编码大小预检；本地超限必须 discard 并返还本次输入，不改变已提交历史。初始预检失败收口 turn，中途预检失败可以继续未追加输入的原循环。提交后的网络失败保留输入；当前采样状态不携带额外用户正文。
