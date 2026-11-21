## MODIFIED Requirements

### Requirement: Automatic boundaries remain observable and cancellable

压缩事件与 Session 事实 SHALL 具有封闭 trigger manual/auto、自动路径关联的 TurnID 与 sample index，手动路径不得伪造普通 turn。TUI SHALL 展示简短 Compacting conversation 状态、成功边界和可展开历史；普通 turn 不因压缩完成而显示最终完成。活动 turn 的 Ctrl+C/interrupt SHALL 取消当前 PreCompact/PostCompact 等待或进程、当前摘要及任务。Esc 在压缩 Hook 信任审查中按既有默认拒绝规则处理，其他情形保持先关闭详情/取回队列规则。手动 cancel_compaction 不得借用错误 ID 取消自动任务。所有输出 SHALL 不含摘要正文、raw usage 或 secret；摘要详情仅在已有授权历史中展示。

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

## ADDED Requirements

### Requirement: Automatic sampling waits for post-compaction observation

初始输入、工具续轮和 Stop 待交付边界 SHALL 在同一压缩 owner 内等待已安装 checkpoint 的 PostCompact 阶段收口及清理，才准备后续 native 提交或普通 sample。检查只读取摘要及压缩身份，所有待交付正文和附件保持原有顺序、边界与字节。成功安装清零自动失败计数。拒绝、确定的检查错误和跳过 MUST 不计为自动压缩失败，不重新摘要，健康且未取消时以新窗口继续原有预算检查。已成功 checkpoint 后的取消 MUST 停止下一 sample，保留新窗口和既有输入返还规则；健康 Session 仍按已有契约本地交付待提交的 Stop 反馈。后置事实写入失败或清理不确定 MUST 以独立 Session 错误阻止后续 native 提交、Stop 反馈交付和下一 sample，保留已确认 checkpoint 并交由恢复处理未关闭尾部，不产生自动压缩失败计数。

#### Scenario: Continue after a known observer failure

- **WHEN** 自动 checkpoint 成功，PostCompact 已确认清理的退出失败发生在一次工具续轮之前
- **THEN** 先发布压缩成功，再从新窗口继续原任务；不增加摘要失败计数，也不重跑工具或摘要

#### Scenario: Interrupt a post hook for an automatic Stop continuation

- **WHEN** Stop 的已持久化反馈等待交付，自动压缩成功后 PostCompact 仍在清理时收到 turn 中断
- **THEN** 保留新 checkpoint，不启动普通 sample，待交付反馈和用户输入仍由既有 Stop 取消及恢复归属收口

#### Scenario: Stop all automatic continuation after an uncertain post check

- **WHEN** checkpoint 已成功安装，PostCompact 的关闭 Sync 或进程清理不确定
- **THEN** 保留唯一已知成功的压缩，既不增加自动压缩失败计数，也不提交尚待交付的输入或 Stop 反馈，不开始下一 sample，Session 进入失败关闭
