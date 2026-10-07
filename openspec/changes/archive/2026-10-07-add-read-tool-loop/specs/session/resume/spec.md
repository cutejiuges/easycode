## ADDED Requirements

### Requirement: Resume locally reconciles an unfinished Tool Loop from durable ledger facts

在取得并持续持有目标thread exclusive lease、完成Loader/ReplayPlanner、Provider配置兼容检查和事务式native history恢复后，resume SHALL检查活动tool turn的ledger状态，并在向宿主暴露可用Session或接受新输入前完成确定性本地reconciliation。

- ready但未started的invocation SHALL使用原invocation ID追加 `cancelled/session_interrupted_before_execution` result。
- result已durable但对应tool-output native entry尚未durable时，系统SHALL从保存的preview bytes重建output commit，不得重读文件。
- execution started但result缺失时，系统MUST追加 `outcome_uncertain` result，禁止自动重试。
- tool-output entry已durable时，系统MUST NOT重复追加output。
- 所有缺失output补齐后，系统MUST以且仅以一个 `turn_failed`关闭旧turn。
- 最终sample已durable完成时，resume不得重新执行、追加output或重发sample。

reconciliation MUST保持原call index顺序，只允许纯内存Provider output编码与Session append/Sync；MUST NOT调用Tool Executor，MUST NOT发起Provider请求，也MUST NOT自动继续下一sample。每次append/Sync失败 MUST使resume失败并阻止新turn副作用。系统不得从原生tool item推断执行状态，也不得从RuntimeEvent、TUI transcript或文件当前内容重建result。

#### Scenario: Locally cancel a ready Read
- **WHEN** journal在ready batch后结束且没有execution-start
- **THEN** resume以相同invocation和Provider call identity持久化 `session_interrupted_before_execution` cancelled result及matching output
- **THEN** Read executor与Provider stream调用次数均为零

#### Scenario: Resume a durable Read result
- **WHEN** result fact已durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的模型preview提交output且Read executor调用次数不增加
- **THEN** resume追加一个 `turn_failed`且不发起Provider请求

#### Scenario: Fail closed on an uncertain Read
- **WHEN** execution-start已durable但result缺失
- **THEN** resume不调用Read，durable记录 `outcome_uncertain`补偿并停止自动推进该turn

#### Scenario: Do not duplicate a durable output commit
- **WHEN** call、result和tool-output entry均完整durable但尚无terminal
- **THEN** resume只追加一个 `turn_failed`，不重复output且不启动下一sample

#### Scenario: Continue on the next user input
- **GIVEN** unfinished tool turn已完成本地补偿
- **WHEN** 用户提交下一次输入
- **THEN** Provider请求保持tool call/output pairing和原生item顺序
- **THEN** canonical bytes与fingerprint等于从同一reconciled history不经重启构造的请求

#### Scenario: Preserve ownership during reconciliation
- **WHEN** 另一个进程竞争同一unfinished tool thread
- **THEN** 只有持有连续exclusive lease的进程可以reconcile，竞争者在读取后repair、Session append或任何外部副作用前以 `session_busy`失败
