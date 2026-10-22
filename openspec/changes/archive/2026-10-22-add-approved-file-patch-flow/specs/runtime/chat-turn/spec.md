## MODIFIED Requirements

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL在首次Provider请求前durable append当前`turn_started`；失败时不得建立网络流。每个completed sample的`provider_native_commit`、`sample_usage`和全部ready-call facts MUST在同一batch append/Sync后finalize，随后才允许候选准备或executor I/O。

Runtime SHALL按call index与调度屏障完成policy及所需approval。Patch候选只能在ready durable且前序批次结果durable后读取目标；ask decision只有在匹配`tool_approval_decided(allow*)`成功Sync后才可进入执行准入，deny decision必须与denied result同batch durable且零executor I/O。Runtime SHALL按调度批次逐一durable`tool_execution_started`：连续`parallel_read`批次的全部准入完成后最多启动8个workers，每个`exclusive` Patch在前序批次完成后准备候选、请求approval并独占执行。worker/executor只返回固定call slot的typed result，不得写Session或Provider history；协调owner按call index durable result facts。全部results durable后，匹配的Provider-native tool outputs MUST在下一Provider sample前append/Sync并finalize。最终无calls sample的native commit、sample usage和`turn_completed` SHALL在同一batch durable；携带聚合usage的`turn_completed` RuntimeEvent只有在finalizer成功后才能发布。

Provider/工具/approval失败、取消、协议错误或append/Sync失败 MUST通过合法result/output配对与唯一`turn_failed`收口。任何durability状态不确定的Session MUST poisoned并拒绝新turn或新副作用，直到重新加载验证。若后续started admission失败，更早已经accepted的invocation仍由Runtime拥有并恰好执行一次；未accepted的invocation不得执行。approval等待、worker和executor均由同一turn owner持有明确取消与最终完成路径。

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
- **THEN** Runtime不写入native commit或sample usage，并以唯一失败边界收口

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

## ADDED Requirements

### Requirement: Approval requests are typed Runtime events and suspend the matching call

Runtime SHALL为交互式ask产生强类型`tool_approval_requested` RuntimeEvent。event必须携带稳定Session/thread/turn identity、request与invocation identity、capability、candidate fingerprint、相对目标、完整有界diff、reason和内核提供的decisions；不得包含Provider wire、workspace绝对路径、完整base/final文件、secret或可执行回调。

发布event后，Runtime SHALL保持当前turn活跃并等待关联response、interrupt或shutdown，不建立下一Provider sample，也不启动该patch或后续调用。event只是宿主投影，不得替代Session approval fact；同一request最多发布一个活动提示。response收口后Runtime可发布不含敏感正文的`tool_approval_resolved`事件，使宿主从overlay转回streaming状态。

#### Scenario: Suspend on an approval request
- **WHEN** patch policy返回ask并完成候选准备
- **THEN** Runtime发布一个typed approval request且保持turn active
- **THEN** response到达前没有Patch execution started、下一sample或turn terminal

#### Scenario: Keep approval presentation Provider-neutral
- **WHEN** 相同typed patch分别来自Anthropic Edit和OpenAI apply_patch
- **THEN** TUI消费相同RuntimeEvent payload而无需解析任一家native item

### Requirement: Approval wait has one cancellation owner

活动approval wait SHALL由当前turn owner创建、取消和完成。定向interrupt、turn context取消或shutdown必须解除等待，并等待decision/result/output与Provider stream清理完成后才能释放turn；不得为每次等待创建无人管理的helper goroutine。response与取消竞态时，owner只接受一个终态：已接受allow则继续其durable路径，取消先被接受则不得启动executor。

#### Scenario: Race approval response with interrupt
- **WHEN** 合法allow response与匹配turn interrupt并发到达
- **THEN** owner依据唯一admission顺序得到一个确定结果
- **THEN** patch要么在allow和started均durable后恰好执行一次，要么零执行并以取消收口

#### Scenario: Shutdown while awaiting approval
- **WHEN** Runtime在approval wait中接受shutdown
- **THEN** wait被同一owner取消，旧request不能影响后续turn，shutdown仍等待durable terminal与全部清理完成
