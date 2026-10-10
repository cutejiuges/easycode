## MODIFIED Requirements

### Requirement: Resume locally reconciles an unfinished Tool Loop from durable ledger facts

在取得并持续持有目标 thread exclusive lease、完成当前 Loader/ReplayPlanner、Provider 配置兼容检查和事务式 native history 恢复后，resume SHALL 检查活动 tool turn 的完整有序 call group，并在向宿主暴露可用 Session 或接受新输入前完成确定性本地 reconciliation。

- ready 但未 started 的 invocation SHALL 按 call index 使用原 invocation ID 追加 `cancelled/session_interrupted_before_execution` result。
- result 已 durable 但对应 tool-output native entry 尚未 durable 时，系统 SHALL 从保存的 preview bytes 重建完整有序 output commit，不得重读或重新搜索文件。
- execution started 但 result 缺失时，系统 MUST 按 call index 追加 `outcome_uncertain` result，禁止自动重试。
- tool-output entry 已 durable 时，系统 MUST NOT 重复追加 output。
- 所有缺失 output 补齐后，系统 MUST 以且仅以一个 `turn_failed` 关闭旧 turn。
- 最终 sample 已 durable 完成时，resume 不得重新执行、追加 output 或重发 sample。

reconciliation MUST 保持原 call index、capability 与 Provider call identity，只允许纯内存 Provider output 编码和 Session append/Sync；MUST NOT 调用 Read、Glob、Grep executor，MUST NOT 发起 Provider 请求，也 MUST NOT 自动继续下一 sample。每次 append/Sync 失败 MUST 使 resume 失败并阻止新 turn 副作用。旧开发 schema、未知 record/canary 或混合 revision journal MUST 在 reconciliation、repair 和 Provider 恢复前 fail closed，不得选择兼容 reader。

#### Scenario: Locally cancel an ordered ready group
- **WHEN** journal 在含 Glob、Grep、Read 的 ready batch 后结束且没有 started
- **THEN** resume 以原 invocation 和 Provider call identity 按 call index 持久化三个 cancelled results 及 matching outputs
- **THEN** 所有 executor 与 Provider stream 调用次数均为零

#### Scenario: Locally cancel a ready Read
- **WHEN** journal 在单个 Read ready 后结束且没有 execution-start
- **THEN** resume 使用原 identity 持久化 cancelled result 与 matching output，且 Read executor 调用次数为零

#### Scenario: Resume durable parallel results
- **WHEN** 一个并行执行组的全部 result facts 已按 call index durable 但 tool-output native commit 缺失
- **THEN** resume 复用逐字节相同的有序模型 previews 提交 output且所有 executor 调用次数不增加
- **THEN** resume 追加一个 `turn_failed` 且不发起 Provider 请求

#### Scenario: Resume a durable Read result
- **WHEN** Read result 已 durable 但 tool-output native commit 缺失
- **THEN** resume 复用逐字节相同的 preview 提交 output，且 Read executor 调用次数不增加

#### Scenario: Fail closed on uncertain searches
- **WHEN** Glob 与 Grep 的 execution-start 已 durable 但 results 缺失
- **THEN** resume 不调用搜索，按 call index durable 记录 `outcome_uncertain` 补偿并停止自动推进该 turn

#### Scenario: Fail closed on an uncertain Read
- **WHEN** Read execution-start 已 durable 但 result 缺失
- **THEN** resume 不调用 Read，durable 记录 `outcome_uncertain` 并停止自动推进该 turn

#### Scenario: Do not duplicate a durable output commit
- **WHEN** calls、results 和 tool-output entry 均完整 durable 但尚无 terminal
- **THEN** resume 只追加一个 `turn_failed`，不重复 output且不启动下一 sample

#### Scenario: Reject a superseded development journal
- **WHEN** resume 目标使用本变更前的 Tool input/result revision shape 或 optional record envelope
- **THEN** 系统在 repair、append、executor、Provider 网络或 SQLite 写入前失败
- **THEN** journal bytes 保持不变且不加载旧 decoder

#### Scenario: Continue on the next user input
- **GIVEN** unfinished tool turn 已完成本地补偿
- **WHEN** 用户提交下一次输入
- **THEN** Provider 请求保持 tool call/output pairing 和原生 item 顺序
- **THEN** canonical bytes 与 fingerprint 等于从同一 reconciled history 不经重启构造的请求

#### Scenario: Preserve ownership during reconciliation
- **WHEN** 另一个进程竞争同一 unfinished tool thread
- **THEN** 只有持有连续 exclusive lease 的进程可以 reconcile，竞争者在读取后 repair、Session append 或任何外部副作用前以 `session_busy` 失败
