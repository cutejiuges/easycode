## MODIFIED Requirements

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL 在首次普通 Provider 请求前依次保存 turn_started、完成适用的自动压缩与 SessionStart 检查、纯内存预检最终候选输入、durable 提交 initial 用户输入 batch 并 finalize。初始批次应用 SessionStart 上下文时，applied 关闭必须与真实输入事实同批提交。必要步骤失败时不得建立普通采样流。每个completed sample的`provider_native_commit`、`sample_usage`和全部ready-call facts MUST在同一batch append/Sync后finalize，随后才允许候选准备或executor I/O。

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

Runtime SHALL 在 `turn_started` 已 durable、每次 Provider stream之前，使用同一不可变项目指令与Tool Catalog snapshots、当前已提交Conversation history以及本轮输入状态生成上下文计划。首轮和中途用户输入均先以候选历史预检，通过后独立提交；实际 sample 只从已提交的 Provider-native 用户输入、tool outputs、合法独立的 hook_feedback 与 session_start_context 继续，不得把result、RuntimeEvent或SemanticHistoryView伪造成新的共享user文本。

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

### Requirement: SessionStart admission preserves the first input and its cancellation boundary

新建或成功接管的交互根会话 SHALL 在首个符合条件的真实对话 turn 消费已准备的 startup/resume 来源，时机位于 `turn_started` 耐久提交及原初始自动压缩阶段之后、初始输入批次之前。已匹配的首个回合 SHALL 先验证真实输入自身可通过普通预算、请求编码和完整记录预检。预检失败保留待处理来源，零本次 SessionStart 审查及执行。未接受的输入、普通命令与只读预览 MUST 不触发 SessionStart 进程。计划接纳前的确定取消保留待处理来源。已 Sync 的计划消耗本次启动机会，即使后续无交付。无匹配时原始输入、原生请求和持久化行为保持等价。

Runtime SHALL 在全部冻结声明独立获得信任、执行前置条件通过、对应 started Sync 后才调用 runner。获准的有界上下文与真实原文和已有附件使用同一候选窗口完成普通预算、请求编码与批次预检。只有已知的窗口、Provider canonical、单记录或批次额度错误可以视为上下文导致的超限。此情形 SHALL 关闭为跳过，并以同一真实初始输入重新进行原有预检。该退回策略不重复运行脚本、摘要或准备新的附件。applied 批次接纳前的取消保留原有输入归还规则。接纳后等待确定结果，成功时上下文和真实输入一起 finalize 一次，durability 不确定则 poison 并停止新副作用。确认 Sync 后若原生 finalize 或必要宿主投影失败，保留同批历史事实供恢复，不执行后续采样或让宿主将输入标记为可重试。

#### Scenario: Keep genuine input when context is too large
- **WHEN** 用户输入自身通过预检，但合并合法 SessionStart 上下文后超过可用窗口或完整编码限制
- **THEN** 保存跳过状态并提交原始用户输入，候选上下文不进入模型历史，不补跑脚本或摘要

#### Scenario: Cancel on either side of combined admission
- **WHEN** 合并批次在 writer 接纳前被取消，或在接纳后已确定 Sync 成功时被取消
- **THEN** 前者保留未提交输入的归还资格且不交付上下文，后者保留两个已提交原生位置，取消不返还已提交输入
