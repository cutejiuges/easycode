## Purpose

定义运行中自动追加输入的接收、顺序、安全点消费、取回和停止契约，使用户能够持续参与 coding turn，同时保证每条输入身份明确、持久化结果可判断、队列有界且恢复不会重复执行。

## ADDED Requirements

### Requirement: Automatic delivery preserves FIFO barriers

系统 SHALL 为自动输入分配独立 UUIDv7 InputID，并与 RequestID 分离。一个 owner 管理全部输入；仅可领取队首连续 auto 前缀，strict-next 为不可越过屏障。空闲 auto 前缀可共享新 turn，正文仍是多条独立消息；strict-next 每条独立 turn。

#### Scenario: Respect a strict barrier
- **WHEN** 队列依次为 auto A、strict B、auto C 且当前工具安全点可继续
- **THEN** 仅 A 可进入当前 turn，B 必须在其收口后独立执行，C 不可越过 B

#### Scenario: Preserve message boundaries
- **WHEN** 两条 auto 作为新 turn 初始输入领取
- **THEN** 按原正文顺序独立保存，共享 turn identity，不拼接文本


### Requirement: Claims have one durable resolution

自动输入 SHALL 经 queued、claimed、committing 到 committed，确定无写入可 returned，durability 不确定必须 uncertain 并 poison。每 turn 最多一个未解决 claim，claim 未解决不得丢弃所有权或启动新 turn。领取不是保存，也不代表远端处理。

#### Scenario: Resolve cancellation before admission
- **WHEN** writer 尚未接受且 turn 取消
- **THEN** discard staging 并确定返还，零输入写入

#### Scenario: Resolve cancellation after Sync
- **WHEN** writer 已接受并 Sync 成功但 turn 同时取消
- **THEN** finalize 一次并报告 committed，不返还、不重发

#### Scenario: Poison uncertain persistence
- **WHEN** 输入写入或 Sync 结果无法确认
- **THEN** 报告 uncertain 并停止后续副作用，保留 cleanup ownership，不当作 queued 重试


### Requirement: Input admission and claims obey encoding budgets

系统 SHALL 限制每条 4 MiB、queued 与 unresolved claims 合计 64 条/16 MiB 正文。admission 必须纯内存验证单条在 native/record 16 MiB 上限及完整 envelope 开销内可表示，否则 input_encoding_limit 且无写入。claim 取满足 native/record 16 MiB、batch 64 MiB 的最大连续前缀，批次过大分批；不得将可预判编码错误交给 writer 导致 poison。

#### Scenario: Reject escaped expansion safely
- **WHEN** 合法 4 MiB 正文转义后无法放入一个当前 record
- **THEN** 拒绝 input_encoding_limit，journal/队列不变且 writer 健康

#### Scenario: Retain claim quota
- **WHEN** worker 持有尚未解决 claim 时继续收到输入
- **THEN** 原 claim 仍占待处理额度，超过总量返回 input_queue_full

#### Scenario: Split an oversized prefix
- **WHEN** 多条单独可表示输入合并后超过 native 限额
- **THEN** 本次只领取最大可表示前缀，其余保持 FIFO，不永久堵塞队首


### Requirement: Retraction is atomic and identity targeted

取回 SHALL 仅针对明确指定、非重复且仍 queued 的 auto InputIDs，先验证全部再原子返还；任何已 claimed 或不存在的目标必须 input_not_queued，不能部分删除。取回不修改 Provider 或 Session。

#### Scenario: Retract wins the claim race
- **WHEN** owner 先接受合法取回再处理安全点
- **THEN** 指定输入仅返还一次，后续 claim 不包含它

#### Scenario: Claim wins the retract race
- **WHEN** owner 先 claim 任一目标再处理取回
- **THEN** 整个取回失败，其余 queued 项保持不变


### Requirement: Interactive stop returns automatic inputs

定向 stop SHALL 只作用于匹配活动 turn，并返还截至该次 admission 的未领取 auto，严格输入保留；停止处理中拒绝新的 auto。已 claim 项等待确定 durable resolution。idle 返回 no_active_turn，stale 返回 turn_mismatch，拒绝不改变任何输入。approval abort 同样返还 auto。现有 interrupt 保持队列，EOF 排空，shutdown 等待全部 owner 清理。

#### Scenario: Stop without restarting pending automatic work
- **WHEN** stop 接受时有 queued auto 和 strict-next
- **THEN** auto 返回不可执行状态，严格输入只能在前 turn 清理终态后运行

#### Scenario: Do not cancel a replacement turn
- **WHEN** stop 指向旧 turn 而新 turn 已运行
- **THEN** 返回 turn_mismatch，当前 turn 与队列不变

#### Scenario: Finish claims before shutdown
- **WHEN** shutdown 时 input commit 已获 writer admission
- **THEN** 等待 committed 或 uncertain 的最终 resolution 与资源清理，不提前释放 lease


### Requirement: Queue status is transient and ordered

系统 SHALL 以同一有序输出流报告 admission 和输入状态，committed 只能发生在 Sync/finalize 后。queue/claim/returned UI 状态不写 Session；进程重启只恢复原生已提交输入，不猜测未消费队列。状态与错误不得回显正文或 secret。

#### Scenario: Crash with pending inputs
- **WHEN** 进程退出前仅收到 queued 确认
- **THEN** 恢复不自动执行该输入，也不声称已保存

#### Scenario: Fail after saving an input
- **WHEN** 输入 Sync 后 Provider 请求失败
- **THEN** 保留该输入 committed 历史，同时报告 turn failure，不自动重提


### Requirement: Preflight rejects unsendable input without retaining it

初始和中途输入 SHALL 在 Sync 前完成候选上下文预算与 Provider 请求编码大小预检。确定的本地超限必须返还本次 claim，committed history 不变；初始失败结束当前 turn，中途失败继续原循环前仍必须验证原请求合法。网络/远端失败发生在成功提交之后时仍保留输入。

#### Scenario: Shorten a rejected input and continue
- **WHEN** 候选输入使原本可请求的历史超过预算或请求大小上限
- **THEN** 本次输入未保存且可取回，用户缩短后可在同一会话继续，恢复也不包含被拒绝的正文
