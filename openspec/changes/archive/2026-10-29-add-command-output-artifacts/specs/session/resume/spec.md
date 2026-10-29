## MODIFIED Requirements

### Requirement: Resume replays semantic history and continues native history

恢复成功后，应用 SHALL 使用经过语义回放校验的 `provider_native_commit` 调用 Provider codec 重建 native history，并单独使用 `HistoryProjector` 生成 TUI 初始 transcript。TUI MUST NOT 解析 JSONL 中的 Provider payload。后续用户输入 SHALL 继续使用恢复后的 native history 编译请求，并向原 thread 追加更大的 seq；不得重写、复制或重新编号既有 records。

#### Scenario: Replay visible transcript

- **WHEN** 一个包含多个成功 turn 的 thread 被恢复
- **THEN** TUI 在接受新输入前按原顺序显示各 turn 的用户和 assistant 可见文本
- **THEN** transcript 不显示 thinking、signature、encrypted content、usage 或未知原生扩展

#### Scenario: Continue after resume

- **WHEN** 用户在恢复后的 TUI 提交下一条输入并成功完成
- **THEN** Provider 使用恢复后的 native history 续写
- **THEN** 新 Session records 追加到同一 thread 且 seq 延续既有最大值

应用 SHALL 从经过完整 replay 验证的命令 facts 单独恢复命令历史投影和当前 thread 授权 descriptor 集合，不改变 HistoryProjector 或 native request。恢复不得为重建请求读取 artifact；文件缺失仅在按需查看时报告。

#### Scenario: Resume command details without stored files
- **WHEN** 包含历史 Exec 的 thread 恢复时对应输出文件缺失
- **THEN** 历史命令、原 preview 和状态仍可展示，续写请求保持相同；按需读取显示 unavailable

### Requirement: Resume locally reconciles an unfinished Tool Loop from durable ledger facts

在取得并持续持有目标 thread exclusive lease、完成当前 Loader/ReplayPlanner、Provider 配置兼容检查和事务式 native history 恢复后，resume SHALL 检查活动 tool turn 的完整有序 call group，并在向宿主暴露可用 Session 或接受新输入前完成确定性本地 reconciliation。

- Patch ready但没有approval fact的invocation SHALL按call index使用原identity追加`cancelled/session_interrupted_before_approval` result。
- approval allow已durable但execution未started的Patch SHALL追加`cancelled/session_interrupted_before_execution` result；不得恢复grant或复用批准。
- ready但未started的自动允许Read/Glob/Grep/ReadArtifact SHALL追加`cancelled/session_interrupted_before_execution` result。
- result已durable但对应tool-output native entry尚未durable时，系统 SHALL从保存的preview bytes和wire kind重建完整有序output commit，不得重读、重新搜索或修改文件。
- execution started但result缺失时，系统 MUST按call index追加`outcome_uncertain` result，禁止自动重试；Patch不得根据当前文件内容猜测执行结果。
- tool-output entry已durable时，系统 MUST NOT重复追加output。
- 所有缺失output补齐后，系统 MUST以且仅以一个`turn_failed`关闭旧turn。
- 最终sample已durable完成时，resume不得重新执行、重新询问、追加output或重发sample。

reconciliation MUST保持原call index、capability、Provider call identity、wire kind和已durable candidate fingerprint，只允许纯内存Provider output编码和Session append/Sync；MUST NOT调用任一tool executor、候选准备器或approval presenter，MUST NOT访问Patch目标、恢复进程内session grant、发起Provider请求或自动继续下一sample。每次append/Sync失败 MUST使resume失败并阻止新turn副作用。旧开发schema、未知record/canary或混合revision journal MUST在reconciliation、repair和Provider恢复前fail closed，不得选择兼容reader。

#### Scenario: Close a patch before approval
- **WHEN** journal在Patch ready durable后、approval fact前结束
- **THEN** resume持久化`session_interrupted_before_approval`结果与matching output并关闭旧turn
- **THEN** approval presenter、候选准备器、Patch executor和Provider stream调用次数均为零

#### Scenario: Locally cancel an ordered ready group
- **WHEN** journal在含Glob、Grep、Read与Patch的ready batch后结束且没有任何started或approval
- **THEN** resume按原call index持久化只读调用的`session_interrupted_before_execution`与Patch的`session_interrupted_before_approval`结果及matching outputs
- **THEN** 所有executor、approval presenter与Provider stream调用次数均为零

#### Scenario: Locally cancel a ready Read
- **WHEN** journal在单个Read ready后结束且没有execution-start
- **THEN** resume使用原identity持久化`session_interrupted_before_execution` result与matching output，且Read executor调用次数为零

#### Scenario: Close an approved patch before execution
- **WHEN** journal包含matching allow fact但没有execution-started
- **THEN** resume持久化`session_interrupted_before_execution`结果且不恢复session grant或修改目标文件

#### Scenario: Fail closed on an uncertain patch
- **WHEN** Patch execution-started已durable但result缺失
- **THEN** resume追加`outcome_uncertain`结果与matching output，不读取目标或自动重试
- **THEN** 旧turn以一个失败终态关闭

#### Scenario: Resume a durable denied result
- **WHEN** deny decision与denied result均已durable但tool-output commit缺失
- **THEN** resume复用冻结preview和原wire kind生成逐字节相同的output
- **THEN** 不重新显示approval也不访问workspace

#### Scenario: Resume durable ordered results
- **WHEN** 异构调用组的全部result facts已按call index durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的有序模型previews提交output且所有executor调用次数不增加
- **THEN** resume追加一个`turn_failed`且不发起Provider请求

#### Scenario: Resume durable parallel results
- **WHEN** 一个并行只读组的全部result facts已按call index durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的有序模型previews提交output且所有executor调用次数不增加
- **THEN** resume追加一个`turn_failed`且不发起Provider请求

#### Scenario: Resume a durable Read result
- **WHEN** Read result已durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的preview提交output，且Read executor调用次数不增加

#### Scenario: Fail closed on uncertain searches
- **WHEN** Glob与Grep的execution-start已durable但results缺失
- **THEN** resume不调用搜索，按call index durable记录`outcome_uncertain`补偿并停止自动推进该turn

#### Scenario: Fail closed on an uncertain Read
- **WHEN** Read execution-start已durable但result缺失
- **THEN** resume不调用Read，durable记录`outcome_uncertain`并停止自动推进该turn

#### Scenario: Do not duplicate a durable output commit
- **WHEN** calls、approval、results和tool-output entry均完整durable但尚无terminal
- **THEN** resume只追加一个`turn_failed`，不重复output且不启动下一sample

#### Scenario: Reject a superseded development journal
- **WHEN** resume目标使用本变更前的Tool/approval/native payload shape或optional record envelope
- **THEN** 系统在repair、append、executor、Provider网络或SQLite写入前失败
- **THEN** journal bytes保持不变且不加载旧decoder

#### Scenario: Continue on the next user input
- **GIVEN** unfinished tool turn已完成本地补偿
- **WHEN** 用户提交下一次输入
- **THEN** Provider请求保持function/custom/tool-use call/output pairing和原生item顺序
- **THEN** canonical bytes与fingerprint等于从同一reconciled history不经重启构造的请求

#### Scenario: Preserve ownership during reconciliation
- **WHEN** 另一个进程竞争同一unfinished tool thread
- **THEN** 只有持有连续exclusive lease的进程可以reconcile，竞争者在读取后repair、Session append或任何外部副作用前以`session_busy`失败

#### Scenario: Do not repeat interrupted artifact reading
- **WHEN** ReadArtifact ready 或 started 后缺少 result
- **THEN** 恢复沿既有取消/不确定规则补偿，不访问文件或自动重新执行
