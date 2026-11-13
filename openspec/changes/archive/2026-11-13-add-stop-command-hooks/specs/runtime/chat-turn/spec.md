## MODIFIED Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在用户 turn 的首次 Provider 请求前产生一次 `turn_started`。对该 turn 的每个 Provider sample，Runtime MUST 创建独立派生 context，逐项验证事件，记录唯一 terminal，并持续消费直到生产者关闭 channel；channel 关闭且此前恰好收到一个合法 completed terminal时，该 sample 才成功。provider channel 关闭本身 MUST NOT 被解释为 sample 或 turn 成功。

成功 sample 含 ready calls 时，Runtime SHALL 在工具处理和原生 outputs durable 后开始下一 sample；成功 sample 不含 calls 时才可进入回合完成边界；存在 Stop 时还 MUST 在 sample 已保存、全部 Stop 检查通过并且最终决定及 turn_completed 同批成功 Sync 后，才产生一次 `turn_completed`。任一 sample 的零值/非法事件、terminal 前关闭、多个 terminal或terminal 后事件 MUST 作为 stream protocol failure，并由同一 owner 取消、排空、等待生产者清理后收口。一个 turn 无论包含多少 samples 都只能产生一个成功或失败终态。

#### Scenario: Complete a successful turn
- **WHEN** 首个 sample 产生文本和唯一合法 completed terminal且不含 tool calls，并且无 Stop 或 Stop 全部通过
- **THEN** Runtime 输出一次 `turn_started`、中间文本和一次 `turn_completed`

#### Scenario: Fail when stream closes before terminal
- **WHEN** 任一 Provider channel 在 completed、failed 或 cancelled 之前关闭
- **THEN** Runtime 输出一次 `turn_failed`并返回 `stream_protocol_error`

#### Scenario: Propagate provider failure
- **WHEN** 任一sample产生唯一合法failed terminal并完成清理
- **THEN** Runtime输出一次 `turn_failed`并且不输出 `turn_completed`

#### Scenario: Drain after an invalid stream event
- **WHEN** 任一 sample 发布非法事件、重复 terminal或terminal 后事件
- **THEN** Runtime 立即取消并排空该 sample至 channel关闭，随后只输出一次 `turn_failed`

#### Scenario: Complete a tool turn
- **WHEN** 首个 sample 请求 Read，结果提交后第二个 sample 返回最终文本且不含 calls
- **THEN** Runtime 在两个 Provider channel均完成清理后只发布一个 `turn_completed`

#### Scenario: Keep a checked sample active
- **WHEN** 无 calls sample 与 Stop plan 已保存，但检查尚未通过
- **THEN** sample 已 committed，turn 仍 active，不发布 completed 或接收并发回合

### Requirement: Cancellation has a single observable outcome

Runtime SHALL 将调用 context 的取消传播到每次 Provider stream 的派生 context，并等待该 stream 的输出 channel 关闭。用户取消、消费端协议错误或其他需要提前停止消费的失败 MUST 先请求取消，再由同一消费 owner 同步排空 channel 直到 Provider 清理路径退出；Runtime MUST NOT 通过停止读取遗留阻塞的生产 goroutine。

用户取消 SHALL 按既有终态选择和提交线性化得到一个结果，MUST NOT 同时产生 completed 和 failed。没有选中合法 completed 进入 sample 提交时，取消按可识别的 context.Canceled 或稳定错误码失败；已进入 sample 提交路径后 SHALL 屏蔽该操作取消并等待真实提交结果，不能以 Sync 返回前收到取消为由回滚或伪造未保存。无 Stop 路径最终 sample 与 completed 同批提交成功时只有成功终态；有 Stop 时 sample+plan 成功仅保存 sample，取消仍可阻止检查或续轮并使 turn 失败，不能否认该 sample 已保存。工具 sample 提交后取消后续操作可以失败，但不能否认该 sample 已保存。提交结果不确定时沿既有 poisoned/cleanup 契约处理，不承诺恢复时无记录。取消与 Provider terminal 竞态仍根据唯一 terminal、调用 context 和首个消费错误选择结果，不产生两个 terminal。推理取消后仍校验并交付当前 scope 的 Started/Completed 边界以闭合宿主状态，只抑制后续 Delta；若随后 completed 被选中并提交成功，宿主不能因之前抑制增量遗留活动块，取消提示仍具有 UI 优先级。

#### Scenario: Cancel an active turn

- **WHEN** 用户在 provider stream 活跃时取消 turn context
- **THEN** Runtime 停止继续转发新的文本增量，取消派生 stream context并排空 channel
- **THEN** Runtime 在 Provider 清理完成后产生一次可识别的取消结果

#### Scenario: Cancel races with provider completion

- **WHEN** context 取消与 provider completed 几乎同时发生
- **THEN** Runtime 消费到 channel 关闭并只发布一个最终终态
- **THEN** 不会同时发布 `turn_completed` 和 `turn_failed`

#### Scenario: Protocol failure cancels the producer

- **WHEN** Runtime 在消费中发现非法事件且 Provider 正等待继续发送
- **THEN** Runtime 取消派生 context以解除生产者发送或 transport 阻塞，并继续读取直到 channel 关闭
- **THEN** RunTurn 返回前生产 goroutine 已退出，下一 turn 不会收到残留事件

#### Scenario: Cancellation arrives after commit admission
- **WHEN** 已选中合法 completed 进入 sample 提交路径，用户在 Sync 返回前取消，随后提交成功
- **THEN** 当前 sample 的推理与正文为已保存；无 Stop 的最终 sample 只发布成功，有 Stop 的候选及工具 sample 后续取消不得倒退其保存状态，回合仍按后续完成门禁决定

#### Scenario: Persistence cannot be confirmed
- **WHEN** sample 提交已接收但写入/Sync 结果不能确定
- **THEN** 当前 sample 解析为 uncertain，保留原 owner 与 poison 规则，UI 不声称 journal 无此项，恢复由实际批次验证决定

Stop passed/failed terminal batch 的接收 SHALL 决定唯一终态，接收后迟到取消不能撤回确定提交。continued 决定接收后形成必须完成的本地反馈交付义务；取消阻止摘要/下一请求，确定未写入的用户 claim 返还，反馈按冻结字节单独完成本地交付再关闭 turn。复合交付已被接收且 Sync 成功时，反馈及真实输入均保存、真实输入不返还；Sync 不确定 poison 且禁止重提。不得撤回或重复决定/terminal。Hook 清理未完成时取消不得提前报告 idle 或释放资源。

#### Scenario: Cancel between sample durability and check admission
- **WHEN** 最终候选 sample 已 Sync，而 Stop started 尚未接收便取消
- **THEN** sample 保留，零检查进程，turn 以唯一取消终态结束

#### Scenario: Cancel an admitted passing decision
- **WHEN** passed/completed batch 已接收且最终 Sync 成功，期间发生取消
- **THEN** 只有成功 terminal，不回滚答复或重复结束回合

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在首次 Provider 请求前依次提交 turn_started、纯内存预检候选输入、durable 提交 initial 用户输入 batch 并 finalize；任一步失败时不得建立网络流。每个completed sample的`provider_native_commit`、`sample_usage`和全部ready-call facts MUST在同一batch append/Sync后finalize，随后才允许候选准备或executor I/O。

Runtime SHALL按call index与调度屏障完成policy及所需approval。Patch候选只能在ready durable且前序批次结果durable后读取目标；ask decision只有在匹配`tool_approval_decided(allow*)`成功Sync后才可进入执行准入，deny decision必须与denied result同batch durable且零executor I/O。Runtime SHALL按调度批次逐一durable`tool_execution_started`：连续`parallel_read`批次的全部准入完成后最多启动8个workers，每个`exclusive` Patch在前序批次完成后准备候选、请求approval并独占执行。worker/executor只返回固定call slot的typed result，不得写Session或Provider history；协调owner按call index durable result facts。全部results durable后，匹配的Provider-native tool outputs MUST在下一Provider sample前append/Sync并finalize。无 Stop 的最终无 calls sample 的 native commit、sample usage 和 turn_completed SHALL 同批 durable；有 Stop 或来源不可判定时 SHALL 先同批保存 native sample、usage、Stop plan；通过/失败时决定与 terminal 同批，合法阻断时 finished 与 continued 决定同批冻结反馈义务，再经独立一/三记录 batch 原子交付 native feedback 及适用的真实用户输入；携带聚合usage的`turn_completed` RuntimeEvent只有在finalizer成功后才能发布。

Provider/工具/approval失败、取消、协议错误或append/Sync失败 MUST通过合法result/output配对与唯一`turn_failed`收口。任何durability状态不确定的Session MUST poisoned并拒绝新turn或新副作用，直到重新加载验证。若后续started admission失败，更早已经accepted的invocation仍由Runtime拥有并恰好执行一次；未accepted的invocation不得执行。approval等待、worker和executor均由同一turn owner持有明确取消与最终完成路径。

上述 Patch 的 exclusive、approval、started/result/output 顺序 SHALL 同样适用于 Write。Write 的发布成功后取消不能覆盖已经确定的文件结果：成功 Sync/cleanup 的 Write result 保持 success，turn 仍可独立取消。Read 观察只能在整组原生 outputs 成功 durable/finalize 后生效。文件结果不确定导致 Runtime poisoned 时，禁止后续工具执行/Provider 请求；若 journal 仍健康，owner SHALL 继续保存已取得结果及配对 output 并收口失败，journal 自身不健康则留待本地恢复补偿。

#### Scenario: Persist before starting a Provider request
- **WHEN** Session无法durable append`turn_started`
- **THEN** Runtime返回Session错误且Provider、候选准备和任一executor均不被调用

#### Scenario: Complete a durably committed sample
- **WHEN** 最终sample不含calls、没有 Stop 且其 native commit、usage 和 completion 成功 Sync
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

中途输入 SHALL 只在整组工具 outputs 成功 Sync/finalize 后，或 Stop continued 决定闭合后的受控交付安全点提交。writer admission 前取消不得写入；接收后必须返回确定提交结果，成功则 finalize 一次，即使取消也不得返还，无法确认则 poison 并禁止后续副作用。

#### Scenario: Cancel around user input durability
- **WHEN** 取消分别发生在 writer 接收前、接收后 Sync 成功或结果不确定
- **THEN** 输入分别确定返还、保留为 committed 或标记 uncertain；每条只有一种结果，后两者不得自动重试

Stop 检查 SHALL 在 sample/plan Sync/finalize、预检、全部信任及对应 started Sync 后执行。模型续轮 SHALL 在 continued 决定、候选预检与独立 native 交付全部确定后开始；交付有真实追加输入时两种 native entries 必须同批保存并 finalize，随后经过普通采样守卫；不得将冻结反馈放入当前输入旁路、从展示反建请求或先发布 turn_completed。Stop 决定/反馈失败不得重新调用已运行脚本。

#### Scenario: Persist feedback before sampling
- **WHEN** Stop continued 决定已保存，而最终 native 交付 batch 尚未 Sync
- **THEN** 下一 Provider 请求次数不增加；只有确定 Sync/finalize 后才进入采样准备

### Requirement: Runtime plans context before Provider side effects

Runtime SHALL 在 `turn_started` 已 durable、每次 Provider stream之前，使用同一不可变项目指令与Tool Catalog snapshots、当前已提交Conversation history以及本轮输入状态生成上下文计划。首轮和中途用户输入均先以候选历史预检，通过后独立提交；实际 sample 只从已提交 Provider-native 用户输入、tool outputs 和合法独立 hook_feedback 继续，不得把result、RuntimeEvent或SemanticHistoryView伪造成新的共享user文本。

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

Stop 反馈 SHALL 先作为有界冻结交付义务保存，再与已领取真实输入准备合并候选。需要自动摘要时 discard 预备对象，只摘要旧 committed 窗口，再重建相同最终反馈字节及真实 InputIDs/正文；最多一次。交付之后、目标 sample 首次请求之前不得重新摘要该反馈。真实输入导致本地超限则明确返还该 claim、重建反馈单独候选，不额外摘要。反馈单独仍不可请求时完成其本地交付并以预算错误结束，零下一普通请求，不生成第二个 Stop 决定。

#### Scenario: Enforce the window after a stop block
- **WHEN** 候选最多一次旧窗口摘要后，反馈单独的下一普通请求仍 over_limit
- **THEN** 保留 sample/反馈，零下一普通请求，以明确预算失败结束，不伪造检查通过

## ADDED Requirements

### Requirement: Stop continuations preserve one task and its accounting

Runtime SHALL 以同一 turn owner 执行 Stop 状态机；continued 至多两次，不重置 16 sample/64 tool call、结果预算或 Stop 的同 turn 30 秒额度。正常 exit0/model 阻断尝试续轮而无下一普通 sample 时，必须在 continued 决定前以 tool_loop_limit_exceeded 关闭，仍允许当前候选通过。Hook 不伪造用户输入；自动续轮前须由既有输入 owner 领取一次队首 auto，保持 strict-next 屏障及每条真实 InputID。最终成功聚合全部 durable 普通 sample usage；失败保留 sample usage，但不伪造成功聚合。

#### Scenario: Continue within the original turn
- **WHEN** Stop 两次阻断后模型第三次候选通过
- **THEN** 一个 turn_started、同 TurnID 的三次答复、两段反馈、一个 completed；用量按实际普通 samples 顺序聚合

#### Scenario: Preserve queued user intent
- **WHEN** 检查期间队首为 auto A、strict-next B、auto C，随后阻断并允许继续
- **THEN** 只领取 A 并与反馈候选一起预检，B/C 保持 FIFO；A 的取消/Sync/返还沿真实输入生命周期
