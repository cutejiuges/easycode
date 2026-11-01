## Purpose

定义双 Provider 在长 coding 会话中的阈值自动压缩行为，使模型在输入与采样安全点保留可继续工作的窗口，并确保用户正文、工具授权、原始历史和 durable 恢复的一致性。

## ADDED Requirements

### Requirement: Automatic compaction has explicit deterministic eligibility

系统 SHALL 接受 JSON 布尔 auto_compact，省略时在显式窗口存在时启用，无窗口时不启用；显式 true 而无窗口 MUST 返回 invalid_configuration。null、非布尔、重复字段 MUST 拒绝。设 W 为窗口，S 为安全余量，R 为普通输出预留，C 为摘要输出预留；若 W-S 小于等于 max(R,C)，自动压缩 SHALL 不可用但既有合法配置仍可启动。否则阈值 T SHALL 等于 floor((W-S-max(R,C))*80/100)，使用不溢出的整数运算。仅完整、可比较的当前规划估算达到 T 时触发；unknown 不当作零、不猜模型容量、不改变既有普通预算策略。禁用自动压缩不影响手动压缩。上下文查询 SHALL 公布自动调度状态、不可用原因和可用时的数值阈值，保持只读且不推断服务端容量。

#### Scenario: Reach the threshold
- **WHEN** 完整估算等于阈值且存在可压缩历史
- **THEN** 系统在下一安全点尝试一次自动压缩

#### Scenario: Keep unknown explicit
- **WHEN** 窗口未配置或估算 unknown
- **THEN** 不发起自动摘要，普通采样继续遵循既有预算规则

### Requirement: Automatic compaction runs only at complete sampling boundaries

系统 SHALL 在下一 sample 的统一准备阶段检查自动压缩；初始输入可在 turn started 后、首次输入提交前检查，工具续写必须等待全部 outputs 成功 Sync/finalize，再冻结本次已领取输入并检查候选或 committed 窗口。同一 TurnID 和目标 sample index MUST 共用一次自动尝试额度，不能因工具结果与追加输入分别检查而重复摘要。任何 stream、待审批或未闭合工具配对期间 MUST NOT 切窗。压缩 SHALL 由当前唯一工作 owner 执行，不启动并行摘要；摘要成功后继续同一任务，不增加普通 turn，不重置 sample/call/result 预算，不重跑已执行工具。

#### Scenario: Continue a long tool turn
- **WHEN** 工具组全部 outputs durable 后达到阈值
- **THEN** 摘要安装后在相同 turn 的下一 sample 继续，无重复执行或 orphan output

#### Scenario: Preserve exclusive ownership
- **WHEN** 自动压缩期间收到手动 compact
- **THEN** 手动请求以 session_busy 拒绝，查询与取消保持响应

### Requirement: Pending input stays exact and outside the summary

输入候选触发自动压缩时 SHALL 丢弃未提交候选，摘要只处理旧 committed 窗口；成功后 SHALL 从新窗口重新准备相同 InputID 和逐字正文并重复全部预算、编码预检。当前输入不得被摘要代替，不得在压缩成功前发布 UserInputCommitted。若无可压缩历史 SHALL 回到普通请求预检；若重建后仍超限 SHALL 返还未保存输入，不递归压缩。初始输入无法提交时结束 turn；steering 预检拒绝时返还该批输入，并按既有规则继续不含该批输入的工具续写。纯输入编码大小错误 MUST NOT 触发摘要。压缩期间新增 queued 输入不参与摘要，按既有 FIFO/safe-point claim 规则处理。

#### Scenario: Rebuild an oversized candidate
- **WHEN** 初始或 steering 输入候选达到阈值且旧历史可压缩
- **THEN** 摘要不含该候选正文，成功后该输入只保存一次且正文和 ID 不变

#### Scenario: A single input remains too large
- **WHEN** 一次压缩后候选仍无法编码或超限
- **THEN** 返还输入，不再次摘要，不留下该输入的 native commit

### Requirement: Automatic failures are bounded and durable

同一采样边界 SHALL 最多尝试一次。网络失败、超时、无缩减或摘要响应不合法，在失败已确定收口且 Provider 已完成清理时 SHALL 保持旧窗口并重新执行普通请求预算及编码预检；通过则继续，否则停止或按输入拒绝规则返还。用户取消 MUST 停止当前任务；writer durability 不确定或内部生命周期不变量破坏 MUST 停止后续工作，不得当作可降级失败。已保存输入不得返还后重新执行。

同一来源 context revision 自动失败后 SHALL 在当前进程抑制该来源的自动尝试；连续三次自动失败后 SHALL 在整个进程会话范围熔断，来源 revision 更新不得重置连续计数。成功的手动或自动压缩 SHALL 清零计数并解除抑制；手动压缩不受自动熔断限制，失败不消耗自动失败次数。阈值不满足、无内容、unknown、来源抑制不计失败；符合自动条件但摘要预算预检拒绝计一次失败且不写 started。状态不跨进程保存，恢复不联网重试。

恢复 SHALL 只在初始输入提交前或完整工具 outputs 后接受活动 turn 内自动压缩，验证真实 turn/sample 关联；压缩 started 到 terminal 之间 MUST NOT 插入普通持久化事实。悬空 started 先收口 interrupted compaction，再收口唯一活动 turn；成功 durable completed 必须恢复新窗口。所有摘要验证、120 秒期限、禁工具、Sync 后切窗、取消线性化与 poison 规则 SHALL 复用现有压缩契约。

#### Scenario: Avoid retry loops
- **WHEN** 摘要无缩减或断流且用户重提相同窗口上的任务
- **THEN** 当前进程不对相同来源 revision 重复自动摘要

#### Scenario: Restart after a crash
- **WHEN** 自动压缩 started 后进程退出
- **THEN** resume 在 lease 下收口，零网络、零工具重跑；只有完整 durable completed 能安装窗口

### Requirement: Automatic boundaries remain observable and cancellable

压缩事件与 Session 事实 SHALL 具有封闭 trigger manual/auto、自动路径关联的 TurnID 与 sample index，手动路径不得伪造普通 turn。TUI SHALL 展示简短 Compacting conversation 状态、成功边界和可展开历史；普通 turn 不因压缩完成而显示最终完成。活动 turn 的 Ctrl+C/interrupt SHALL 取消当前摘要及任务，Esc 保持先关闭详情/取回队列规则；手动 cancel_compaction 不得借用错误 ID 取消自动任务。所有输出 SHALL 不含摘要正文、raw usage 或 secret；摘要详情仅在已有授权历史中展示。

#### Scenario: Cancel during an automatic summary
- **WHEN** 用户中断对应活动 turn
- **THEN** 取消摘要并等待 cleanup 后收口 turn；completed 尚未 durable 时旧窗口保持，completed 已成功 Sync 时保留新窗口但不启动下一 sample；队列按已有失败规则返还

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

### Requirement: Automatic continuation keeps task and authority distinct

自动压缩 SHALL 使用确定性接续包装，要求直接继续原任务而不复述摘要或询问如何继续；摘要要求保留最新纠正、约束、任务状态、验证结果和未完成工作。该包装 MUST 标识摘要为历史参考，不构成新的用户请求、授权、文件观察或 artifact 访问权。系统 MUST NOT 提示模型使用尚未提供的完整 Session JSONL 读取能力。

#### Scenario: Preserve a correction without inventing authority
- **WHEN** 用户先纠正旧方案并撤销某执行方向，随后自动压缩
- **THEN** 摘要请求包含该纠正，接续包装不覆盖它；模型后续工具调用仍经过真实权限与观察校验
