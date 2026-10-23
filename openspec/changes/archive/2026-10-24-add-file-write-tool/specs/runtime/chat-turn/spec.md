## MODIFIED Requirements

### Requirement: Durable facts precede Provider side effects and completion

Runtime SHALL在首次Provider请求前durable append当前`turn_started`；失败时不得建立网络流。每个completed sample的`provider_native_commit`、`sample_usage`和全部ready-call facts MUST在同一batch append/Sync后finalize，随后才允许候选准备或executor I/O。

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

#### Scenario: Separate committed Write from turn cancellation
- **WHEN** Write 发布并完成 Sync 后 turn 被取消
- **THEN** 文件 result 保存 success，后续未执行调用按取消收口，turn 只有一个取消终态

#### Scenario: Stop after an uncertain Write
- **WHEN** Write 发布或清理的结果无法确认
- **THEN** Runtime poisoned，等待所拥有清理完成，不执行后续 sibling 或发起下一 sample

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

## ADDED Requirements

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
