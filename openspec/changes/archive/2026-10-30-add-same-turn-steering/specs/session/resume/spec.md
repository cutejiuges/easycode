## MODIFIED Requirements

### Requirement: Interrupted and failed turns cannot become fabricated history

resume SHALL 只把 durable 且通过语义回放校验的 `provider_native_commit` 恢复为 Provider 历史。仅含 `turn_started`、`turn_failed`、未完成 batch 或部分流式文本的 turn MUST NOT 被补全为成功提交；当前文本切片不要求重放失败 turn 的完整 transcript。尾部修复结果 SHALL 可诊断地报告，但不得把修复内容或原生 payload 输出到 TUI 错误中。

当当前 journal 以完整 committed turn_started、initial 用户输入 batch 或已闭合工具组后的 steering 输入 batch 结束且没有 terminal 时，应用 SHALL 在完成全量 load/repair、配置兼容检查和事务式 Provider 恢复之后、向 TUI 暴露可用 Session 或接受新 turn 之前，使用原 turn identity durable 追加唯一的 required `turn_failed`，其机器可读 code 为 `session_interrupted`。该补偿记录只收口 lifecycle，不得加入 native history、重放旧输入或调用 Provider；append/Sync 失败 MUST 使 resume 失败并阻止后续副作用。配置不匹配或其他恢复校验失败时不得写入该补偿记录。

#### Scenario: Resume after a cancelled turn

- **WHEN** 最后一个 turn 在任何用户输入或 sample native commit 前被取消或失败
- **THEN** 恢复后的 native history 与该 turn 开始前相同
- **THEN** 下一次请求不包含失败 turn 的用户输入或部分 assistant 文本

#### Scenario: Resume after repairing a tail

- **WHEN** loader 修复了最后一个未完成 batch
- **THEN** 应用只恢复此前 committed records，并以安全摘要报告 Session 已修复

#### Scenario: Close a committed interrupted turn before reuse

- **WHEN** 合法 journal 的最后状态是只有 `turn_started` 的 interrupted tail
- **THEN** 应用恢复此前 committed native history并在网络请求前 durable 追加 `turn_failed(code=session_interrupted)`
- **THEN** 新 turn 只能在该失败边界 Sync 成功后开始，旧用户输入不会被自动重发

#### Scenario: Fail while closing an interrupted turn

- **WHEN** interrupted-tail 补偿记录无法 append 或 Sync
- **THEN** resume 返回安全英文 session 错误且不进入可交互状态、不调用 Provider

恢复 SHALL 保留失败 turn 中已 durable 的用户输入和成功 sample，并以真实 turn 归属投影。内存 queued/claimed、草稿和返还托盘不属于恢复事实，不自动重放；input-only tail 在新输入前按原 turn identity 追加唯一 session_interrupted 失败终态。

#### Scenario: Resume committed steering without issuing a request
- **WHEN** steering 输入 Sync 后进程退出且没有下一 sample
- **THEN** 恢复保留用户文本，本地收口旧 turn，零 Provider/工具调用；用户之后提交的新任务携带完整原生历史
