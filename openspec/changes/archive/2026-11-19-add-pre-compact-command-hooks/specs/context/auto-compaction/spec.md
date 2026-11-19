## MODIFIED Requirements

### Requirement: Pending input stays exact and outside the summary

输入候选触发自动压缩时 SHALL 丢弃未提交候选，摘要只处理旧 committed 窗口；成功后 SHALL 从新窗口重新准备相同 InputID 和逐字正文并重复全部预算、编码预检。当前输入不得被摘要代替。活动压缩尚无确定终态或仍在清理时，不得发布待交付输入的 UserInputCommitted。领取或排队的输入及尚未交付的 Stop 反馈均不得进入 PreCompact 输入、摘要重点或摘要请求。若无可压缩历史 SHALL 回到普通请求预检；若重建后仍超限 SHALL 返还未保存输入，不递归压缩。初始输入无法提交时结束 turn；steering 预检拒绝时返还该批输入，并按既有规则继续不含该批输入的工具续写。纯输入编码大小错误 MUST NOT 触发摘要。压缩前检查及摘要期间新增 queued 输入不参与检查或摘要，按既有 FIFO/safe-point claim 规则处理。

#### Scenario: Rebuild an oversized candidate
- **WHEN** 初始或 steering 输入候选达到阈值且旧历史可压缩
- **THEN** 摘要不含该候选正文，成功后该输入只保存一次且正文和 ID 不变

#### Scenario: A single input remains too large
- **WHEN** 一次压缩后候选仍无法编码或超限
- **THEN** 返还输入，不再次摘要，不留下该输入的 native commit

#### Scenario: Do not disclose pending input to a compaction hook
- **WHEN** 一条待保存输入触发自动压缩，或已冻结 Stop 反馈和用户纠正的合并候选使估算达到阈值
- **THEN** 丢弃 preflight 候选后才开始 PreCompact。输入、反馈及用户纠正的哨兵字节均不进入 Hook 输入或 Provider 摘要请求，后续真实交付沿原预算及原字节规则

### Requirement: Automatic failures are bounded and durable

同一采样边界 SHALL 最多尝试一次。网络失败、超时、无缩减、摘要响应不合法、PreCompact 拒绝或失败，以及检查后最终摘要预算失败，在失败已 durable 收口、当前 owner 已完成相应清理且会话健康时 SHALL 保持旧窗口并重新执行普通请求预算及编码预检。通过则继续，否则停止或按输入拒绝规则返还。未发出摘要请求的失败必须标记摘要 usage unavailable。用户取消 MUST 停止当前任务；writer durability 不确定或内部生命周期不变量破坏 MUST 停止后续工作，不得当作可降级失败。已保存输入不得返还后重新执行。

同一来源 context revision 自动失败后 SHALL 在当前进程抑制该来源的自动尝试；连续三次自动失败后 SHALL 在整个进程会话范围熔断，来源 revision 更新不得重置连续计数。成功的手动或自动压缩 SHALL 清零计数并解除抑制；手动压缩不受自动熔断限制，失败不消耗自动失败次数。阈值不满足、无内容、unknown、来源抑制不计失败；符合自动条件但基础摘要预算预检拒绝计一次失败且不写 started。已接纳自动压缩的 PreCompact 拒绝、失败或检查后预算拒绝同样计入一次自动失败，使用当前来源抑制和连续失败计数。状态不跨进程保存，恢复不联网重试。

`/hooks` 对一条声明的信任或拒绝执行 reset 时，自动失败计数、同源抑制及熔断 MUST 保持原值。用户可手动发起压缩重新审查。

恢复 SHALL 只在初始输入提交前、完整工具 outputs 后或Stop continued 冻结决定已保存、native 交付尚未发生且候选已 discard 的边界接受活动 turn 内自动压缩，验证真实 turn/sample 关联；压缩 started 到 terminal 之间 MUST NOT 插入普通持久化事实，只允许与同一压缩操作匹配的专属检查事实。恢复必须先收口带有完整或部分检查事实的悬空 started，再收口唯一活动 turn。成功 durable completed 必须恢复新窗口。所有摘要验证、120 秒期限、禁工具、Sync 后切窗、取消线性化与 poison 规则 SHALL 复用现有压缩契约。

#### Scenario: Avoid retry loops
- **WHEN** 摘要无缩减或断流且用户重提相同窗口上的任务
- **THEN** 当前进程不对相同来源 revision 重复自动摘要

#### Scenario: Restart after a crash
- **WHEN** 自动压缩 started 后进程退出
- **THEN** resume 在 lease 下收口，零网络、零 Hook 或工具重跑。只有完整 durable completed 能安装窗口

Stop 后的摘要 SHALL 继续使用原阈值、来源抑制、三次熔断、计量及取消/恢复契约；最新待交付反馈及本次真实输入必须保持在摘要之外，重建后原字节交付，不靠摘要文本的语义完整性保证它们存在。恢复先收口摘要，再补齐已接收的本地反馈交付，不运行旧 Stop 或联网重发。

#### Scenario: Recover a post-stop compaction
- **WHEN** continued 冻结决定已保存、native 交付缺失，而自动摘要 started 后中断
- **THEN** 同一 lease 下先收口摘要，再从最终冻结字节补齐本地反馈并关闭旧回合；零网络/Hook，已有交付不得重复

#### Scenario: Fall back after a durable automatic hook rejection
- **WHEN** 同一活动 turn 的自动压缩被 PreCompact 明确阻止，确定失败已经 Sync，Hook 清理完成且旧窗口仍满足普通预算
- **THEN** 不发送摘要请求，当前来源受到抑制，同一 turn 依原生顺序继续普通采样，已提交的输入及工具输出均不会重新执行

#### Scenario: Stop after an uncertain automatic hook fact
- **WHEN** 自动压缩的检查事实提交结果不确定，或检查未能确认进程清理
- **THEN** 停止后续副作用。不得把不确定状态当作普通请求回退依据，不得释放仍由原 owner 持有的资源

### Requirement: Automatic boundaries remain observable and cancellable

压缩事件与 Session 事实 SHALL 具有封闭 trigger manual/auto、自动路径关联的 TurnID 与 sample index，手动路径不得伪造普通 turn。TUI SHALL 展示简短 Compacting conversation 状态、成功边界和可展开历史；普通 turn 不因压缩完成而显示最终完成。活动 turn 的 Ctrl+C/interrupt SHALL 取消当前 PreCompact 等待或进程、当前摘要及任务。Esc 在 PreCompact 信任审查中按既有默认拒绝规则处理，其他情形保持先关闭详情/取回队列规则。手动 cancel_compaction 不得借用错误 ID 取消自动任务。所有输出 SHALL 不含摘要正文、raw usage 或 secret；摘要详情仅在已有授权历史中展示。

#### Scenario: Cancel during an automatic summary
- **WHEN** 用户在 PreCompact 等待、检查执行或摘要期间中断对应活动 turn
- **THEN** 取消该阶段并等待 cleanup 后收口 turn；completed 尚未 durable 时旧窗口保持，completed 已成功 Sync 时保留新窗口但不启动下一 sample；队列按已有失败规则返还

#### Scenario: Keep one-shot output stable
- **WHEN** 一次性 --print 或 --json 自动压缩后成功完成普通任务
- **THEN** 输出仍只包含原有普通任务投影，不混入摘要或新的控制帧

#### Scenario: Continue after a recoverable summary failure
- **WHEN** 自动摘要失败已确定收口，且旧窗口的下一普通请求仍通过预检
- **THEN** 继续同一任务且不重复执行工具，不将 compaction.failed 误当作 turn.failed

#### Scenario: Share one attempt across tool outputs and steering
- **WHEN** 工具 outputs 和领取的 steering 候选均达到软阈值
- **THEN** 下一 sample 前最多一次摘要，重建后不递归压缩

#### Scenario: Trip a process-local circuit breaker
- **WHEN** 三次自动失败之间有新的普通 native commit 推进 context revision
- **THEN** 连续失败计数仍达到三次并熔断，手动成功压缩后恢复自动调度

#### Scenario: Recover a completed checkpoint in an interrupted turn
- **WHEN** completed 已 durable 而下一普通输入或 sample 尚未提交时进程退出
- **THEN** 恢复新窗口并本地收口活动 turn，不把未保存输入伪造为已提交，不自动续写
