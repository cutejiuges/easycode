# runtime/chat-turn Specification

## Purpose

定义共享 Runtime 执行一次文本 turn 时的生命周期和终态语义，使 TUI 等宿主只消费稳定 RuntimeEvent，并能可靠区分成功、失败、取消和流意外关闭。

## Requirements

### Requirement: Turn lifecycle requires an explicit provider terminal

Runtime SHALL 在用户 turn 的首次 Provider 请求前产生一次 `turn_started`。对该 turn 的每个 Provider sample，Runtime MUST 创建独立派生 context，逐项验证事件，记录唯一 terminal，并持续消费直到生产者关闭 channel；channel 关闭且此前恰好收到一个合法 completed terminal时，该 sample 才成功。provider channel 关闭本身 MUST NOT 被解释为 sample 或 turn 成功。

成功 sample 含 ready calls 时，Runtime SHALL 在工具处理和原生 outputs durable 后开始下一 sample；成功 sample 不含 calls 时才能产生一次 `turn_completed`。任一 sample 的零值/非法事件、terminal 前关闭、多个 terminal或terminal 后事件 MUST 作为 stream protocol failure，并由同一 owner 取消、排空、等待生产者清理后收口。一个 turn 无论包含多少 samples 都只能产生一个成功或失败终态。

#### Scenario: Complete a successful turn
- **WHEN** 首个 sample 产生文本和唯一合法 completed terminal且不含 tool calls
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

### Requirement: Assistant text uses a typed semantic payload

RuntimeEvent SHALL 为 assistant 文本增量定义稳定的强类型 payload，至少包含本次追加的文本。宿主 MUST NOT 解析 OpenAI Responses event 或 provider-native item 才能显示文本。

#### Scenario: Project provider text delta

- **WHEN** provider 产生 assistant 文本增量 `hello`
- **THEN** Runtime 输出 `assistant_text_delta`，其 typed payload 的 text 为 `hello`
- **THEN** payload 不包含 OpenAI wire event 对象

#### Scenario: Reject invalid semantic payload construction

- **WHEN** Runtime 尝试构造缺少必需文本字段的 assistant 文本增量
- **THEN** 系统返回明确错误而不是产生不可消费的 RuntimeEvent

### Requirement: Cancellation has a single observable outcome

Runtime SHALL 将调用 context 的取消传播到每次 Provider stream 的派生 context，并等待该 stream 的输出 channel 关闭。用户取消、消费端协议错误或其他需要提前停止消费的失败 MUST 先请求取消，再由同一消费 owner 同步排空 channel 直到 Provider 清理路径退出；Runtime MUST NOT 通过停止读取遗留阻塞的生产 goroutine。

用户取消 MUST 产生一次 `turn_failed` 或专用 cancellation 语义，但 MUST NOT 同时产生 completed；对外错误 SHALL 可由 `errors.Is(..., context.Canceled)` 或稳定错误码识别。取消与 Provider terminal 竞态时，Runtime MUST 根据已验证的唯一 terminal、调用 context 和首个消费错误得出一个确定结果，并只发布一个 durable terminal。

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

### Requirement: Host submission carries an explicit operation context

所有直接执行单个 turn 或等待异步清理的操作 SHALL 接收显式 `context`，并将其取消传播到当前 turn。nil context MUST 返回稳定英文错误，不得替换为 background context；context 不得保存到长期 Runtime、Session facade、输入队列或 UI 状态结构中。

长期输入控制器 SHALL 由显式生命周期 context 拥有，使用该 context 或其子 context 驱动每个已开始 turn 和最终清理。命令提交的 context 只控制 admission 等待：在 owner 接受前取消 MUST 无副作用；一旦命令被接受，调用方 context 的后续取消不得隐式撤回已排队或已开始的输入，宿主必须使用定向 interrupt 或 shutdown。owner MUST NOT 将命令调用方 context 保存进 follow-up queue。

#### Scenario: Reject a nil submission context

- **WHEN** headless、TUI adapter 或其他宿主使用 nil context 直接提交 turn
- **THEN** Session 在创建 goroutine、写入 `turn_started` 或发起 Provider 请求前返回明确错误

#### Scenario: Cancel through the submitted context

- **WHEN** 宿主取消已接受的直接单 turn 提交 context
- **THEN** 相同取消信号到达 Provider stream，Runtime durable 收口唯一失败终态并完成清理
- **THEN** Session 不遗留活动 goroutine 或可继续发送事件的 channel owner

#### Scenario: Cancel a long-lived command before admission

- **WHEN** 长期控制器的命令提交 context 在 owner 接受前取消
- **THEN** 命令无副作用失败，且 context 不进入 follow-up queue

#### Scenario: Caller cancellation does not retract accepted input

- **WHEN** 长期控制器已接受输入后，提交调用方取消其 admission context
- **THEN** owner 继续拥有该输入，调用方取消不等价于 interrupt
- **THEN** 后续 turn 使用控制器生命周期的子 context，而不是保存的调用方 context

#### Scenario: Application shutdown retains cleanup ownership

- **WHEN** 优雅 shutdown 的等待期限到达而 Provider stream 仍阻塞
- **THEN** 唯一资源 owner 强制关闭 transport 以解除阻塞，并继续等待既有清理完成信号
- **THEN** 超时返回不会使 writer、lease、stream 或 goroutine 失去 cleanup owner

### Requirement: Prepared completion is valid before durable persistence

Runtime SHALL 在写入 `provider_native_commit` 和 `turn_completed` 前验证 completed terminal 携带尚未 finalize、结构完整且可独立复制的 prepared sample。零值、已 finalize、family/wire/revision/payload 非法或复制失败的 sample MUST 作为 stream protocol failure 收口，且不得写入 native commit 或成功终态。

#### Scenario: Reject a zero-value prepared sample

- **WHEN** Provider 产生 completed terminal 但 prepared sample 为零值或无法返回有效 envelope
- **THEN** Runtime durable 记录 `turn_failed`，不记录 `provider_native_commit` 或 `turn_completed`
- **THEN** Conversation history 不执行 finalizer

#### Scenario: Revalidate before durable append

- **WHEN** Provider 提供的 prepared sample 在创建后不满足公共 native envelope 不变量
- **THEN** Runtime 在 Session append 前拒绝该 sample
- **THEN** journal 中不存在代表该 sample 成功的事实

### Requirement: Turn completion exposes typed aggregated usage

成功 `turn_completed` RuntimeEvent SHALL 携带强类型 normalized turn usage；payload 必须遵守统一指标状态和聚合规则，不得包含 Provider wire、raw usage、价格或上下文占用。单 sample turn的payload MUST等于该sample usage；多 sample Tool Loop MUST按durable sample顺序聚合所有usage。

失败或取消终态 MUST NOT 携带伪造的成功 turn usage。已经 durable 保存的 sample usage 即使发生在后续工具或 Provider 失败之前，也不得删除或改写；当前 `turn_failed` 仍不暴露部分 usage。

#### Scenario: Publish current text turn usage
- **WHEN** 单sample文本turn durable完成
- **THEN** `turn_completed` payload与该sample usage完全相同

#### Scenario: Do not publish usage before durability
- **WHEN** 最终完成batch尚未成功Sync
- **THEN** Runtime不发布 `turn_completed`或任何成功聚合usage

#### Scenario: Keep failed terminal shape unchanged
- **WHEN** turn在已有中间sample后失败或取消
- **THEN** Runtime只发布既有强类型 `turn_failed`，同时Session保留已提交sample usage

#### Scenario: Publish aggregated tool-turn usage
- **WHEN** 一个成功Read turn包含三个durable samples
- **THEN** `turn_completed` payload等于按顺序聚合三个sample usage的结果

### Requirement: Runtime revalidates complete prepared sample facts

Runtime SHALL 在构造 Session drafts 前同时复制并重新验证 prepared sample 的 native envelope 与 normalized usage。任何一部分为零值、缺失、已 finalized、复制失败或违反语义不变量时，Runtime MUST 以 stream protocol failure 收口，并不得 append native commit、sample usage 或成功终态。

#### Scenario: Reject invalid normalized usage before append

- **WHEN** completed terminal 的 prepared sample 含非法 metric state 或计数关系
- **THEN** Runtime durable 记录失败边界且不执行 Provider finalizer
- **THEN** journal 中不存在该 sample 的成功记录

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

### Requirement: Approval requests are typed Runtime events and suspend the matching call

Runtime SHALL为交互式ask产生强类型`tool_approval_requested` RuntimeEvent。event必须携带稳定Session/thread/turn identity、request与invocation identity、capability、candidate fingerprint、相对目标、封闭的有界presentation、reason和内核提供的decisions；Patch/Write overwrite使用完整diff，Write create使用完整新内容或全新增diff并列出父目录。除了必要的批准展示，不得另带完整base/final快照、Provider wire、workspace绝对路径、secret或可执行回调。

发布event后，Runtime SHALL保持当前turn活跃并等待关联response、interrupt或shutdown，不建立下一Provider sample，也不启动该Patch/Write或后续调用。event只是宿主投影，不得替代Session approval fact；同一request最多发布一个活动提示。response收口后Runtime可发布不含敏感正文的`tool_approval_resolved`事件，使宿主从overlay转回streaming状态。

presentation MUST 区分 Patch、Write create 和 Write overwrite，具有专属 typed constructor、strict decoder 和 validator；混合字段/错误操作与 capability 组合失败关闭。事件只消费已经冻结的展示，不触发文件读取。

#### Scenario: Suspend on an approval request
- **WHEN** patch policy返回ask并完成候选准备
- **THEN** Runtime发布一个typed approval request且保持turn active
- **THEN** response到达前没有Patch execution started、下一sample或turn terminal

#### Scenario: Keep approval presentation Provider-neutral
- **WHEN** 相同typed patch分别来自Anthropic Edit和OpenAI apply_patch
- **THEN** TUI消费相同RuntimeEvent payload而无需解析任一家native item

#### Scenario: Project a new file approval
- **WHEN** 两家 Provider 分别提出相同 Write create 候选
- **THEN** 宿主收到等价的创建展示、目录清单和 identity，无需解析 Provider item

#### Scenario: Reject a mixed presentation
- **WHEN** approval payload 把 create 与 overwrite 字段混合或 capability 不匹配
- **THEN** 构造/解码失败，不能产生可批准请求

### Requirement: Approval wait has one cancellation owner

活动approval wait SHALL由当前turn owner创建、取消和完成。定向interrupt、turn context取消或shutdown必须解除等待，并等待decision/result/output与Provider stream清理完成后才能释放turn；不得为每次等待创建无人管理的helper goroutine。response与取消竞态时，owner只接受一个终态：已接受allow则继续其durable路径，取消先被接受则不得启动executor。

#### Scenario: Race approval response with interrupt
- **WHEN** 合法allow response与匹配turn interrupt并发到达
- **THEN** owner依据唯一admission顺序得到一个确定结果
- **THEN** patch要么在allow和started均durable后恰好执行一次，要么零执行并以取消收口

#### Scenario: Shutdown while awaiting approval
- **WHEN** Runtime在approval wait中接受shutdown
- **THEN** wait被同一owner取消，旧request不能影响后续turn，shutdown仍等待durable terminal与全部清理完成

### Requirement: Runtime activates and invalidates complete Read observations explicitly

Runtime SHALL 只将实际 Read executor 返回、与 invocation/result 匹配的 complete observation 暂存于对应 call slot；只有该组 results 和原生 outputs 均成功 Sync/finalize 后，才允许供后续 sample/turn 的 Write 使用。当前 sample 内尚未回传的观察不得满足 Write 前置条件。

观察表 MUST 由单一 Runtime owner 持有，最多 1024 个规范路径，按完整 Read 的确定提交顺序淘汰；不得使用包级状态、I/O getter 或 worker 完成时间驱动顺序。失败/截断的新 Read、冲突及该路径 Patch/Write 的 accepted started SHALL 使相关旧观察失效，且不能在该 sample outputs 提交后重新激活被后续写入否定的 pending 观察。若实际 catalog 中已有命令执行，其 accepted started SHALL 使整个 workspace 的当前及 pending 观察失效。

Runtime 关闭、workspace/Provider 改变、resume 或 poisoned 状态 SHALL 清空观察。不得从历史恢复、成功 Write 或模型自报字段制造完整 Read 证据。观察仅影响未来工具执行前置条件，不修改已持久化的模型请求历史。

#### Scenario: Do not promote before native durability
- **WHEN** Read result 已 durable 但 outputs Sync/finalizer 失败
- **THEN** 观察不生效且 Runtime 按既有失败/poisoned 契约收口

#### Scenario: Invalidate an earlier pending Read
- **WHEN** 一个 sample 先完整 Read(a)，再成功 Patch(a)，然后提交全部 outputs
- **THEN** 前一个 Read 证据不能在该次提交后重新生效

#### Scenario: Permit a later complete Read
- **WHEN** Patch(a) 之后在同一 sample 又有完整 Read(a)，且 outputs 提交成功
- **THEN** 只有后置 Read 对应的新观察可供下一 sample 使用

#### Scenario: Bound observation memory
- **WHEN** 按提交顺序读入第 1025 个不同文件的完整观察
- **THEN** 淘汰最早提交的路径；被淘汰路径下一次覆盖需要重新 Read

#### Scenario: Invalidate after an integrated command
- **WHEN** 已实际实现的命令调用取得执行准入
- **THEN** 所有旧及 pending Read 证据失效，不解析命令文本推断写入路径
### Requirement: Command execution preserves durable ordering and cleanup ownership

Runtime SHALL将每个`process.exec`作为exclusive调用处理：ready batch与Provider finalizer成功后冻结候选并请求approval，matching allow fact与execution started依次成功`Sync`后才可把进程交给executor。前序调用results未durable、approval未收口或started未accepted时不得启动进程；命令result与匹配Provider-native output未durable时不得启动后序调用或下一sample。

execution started是命令副作用接收线性化点。此前取消不得创建进程；此后Runtime与executor的唯一owner MUST即使在turn取消、timeout、approval abort或shutdown下仍持有进程组、stdout/stderr readers与最终done信号，直到终止、wait、输出关闭和cleanup完成。shutdown等待期限到达时可以升级终止手段，但不得释放资源所有权或关闭Session writer；无法确认命令结果 SHALL poison Session并以`outcome_uncertain`配对收口。

#### Scenario: Persist approval and admission before spawn
- **WHEN** 用户批准一个Exec调用
- **THEN** ready、allow fact和execution started按顺序durable后executor才可启动进程

#### Scenario: Cancel an accepted command
- **WHEN** execution started已accepted后turn context取消
- **THEN** Runtime恰好调用一次executor并等待进程组终止、两个输出reader关闭和最终result
- **THEN** turn在cleanup完成前不发布terminal也不接受下一turn

#### Scenario: Keep an Exec exclusive
- **WHEN** 同一sample按顺序包含Read、Exec和Grep
- **THEN** Read完成且result durable后Exec独占执行，Exec result durable后Grep才可准入

#### Scenario: Retain process identity through cleanup
- **WHEN** 直接子进程已退出但原组清理尚未结束
- **THEN** process owner保留未回收leader身份，完成组信号和探测后才最终Wait
- **THEN** Runtime仍等待cleanup done且不得提前开启下一turn，回收后的旧PGID不得用于shutdown升级

#### Scenario: Incomplete output cleanup poisons the session
- **WHEN** 直接命令退出后输出reader在排空期限内无法完成
- **THEN** owner关闭自身pipe并等待reader退出，Runtime保存outcome_uncertain并禁止后续Provider或工具副作用
- **THEN** result不能代替cleanup done，尚未Wait的直接子进程仍由唯一owner持有

### Requirement: Command progress is typed bounded and transient

RuntimeEvent SHALL提供命令started、progress和terminal的强类型语义投影，全部绑定当前session/thread/turn/invocation identity。progress SHALL只包含规范化命令摘要、workspace相对workdir、elapsed time、累计stdout/stderr行数与字节数、truncated状态和有界近期输出；payload MUST验证UTF-8与大小，不得包含PID、绝对路径、完整环境、secret、Provider wire、Session record或无界输出。

progress属于宿主瞬态视图，不得写入Session事实源，也不能替代最终tool result。Runtime可以合并或丢弃中间progress以施加背压，但 MUST保持started先于progress、terminal晚于最后已交付progress，且任何progress都不能在turn terminal之后出现。

command terminal SHALL额外携带与durable tool result逐字节一致的typed result preview，UTF-8 bytes上限256 KiB，供TUI命令详情展示。started/progress MUST拒绝携带result preview；terminal preview不得增加Session record或headless JSONL词汇，不得在result durable或cleanup完成前发布。

#### Scenario: Project a frozen result only at terminal
- **WHEN** 命令cleanup完成且tool result已durable
- **THEN** command terminal携带相同preview bytes，TUI无需读取Session或executor即可展示详情
- **THEN** started/progress中的result preview字段被strict decoder拒绝

#### Scenario: Project bounded process output
- **WHEN** 活动命令持续产生stdout与stderr
- **THEN** Runtime发布有界typed progress并累计正确的行数、字节数与elapsed time
- **THEN** Session journal不增加progress record

#### Scenario: Reject progress after terminal
- **WHEN** executor在命令terminal或turn terminal后尝试发布输出更新
- **THEN** Runtime拒绝该协议顺序且不把迟到内容交给TUI或headless
