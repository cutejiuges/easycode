## MODIFIED Requirements

### Requirement: Automatic compaction runs only at complete sampling boundaries

系统 SHALL 在下一 sample 的统一准备阶段检查自动压缩；初始输入可在 turn started 后、首次输入提交前检查，工具续写必须等待全部 outputs 成功 Sync/finalize，再冻结本次已领取输入并检查候选或 committed 窗口；Stop 续轮必须等待 continued 冻结决定成功 Sync、一次 auto claim 已完成（可返回空前缀），才可准备反馈/用户输入合并候选并检查阈值；需要摘要时必须先 discard 候选，再只摘要旧 committed 窗口。同一 TurnID 和目标 sample index MUST 共用一次自动尝试额度，不能因工具结果与追加输入分别检查而重复摘要。任何 stream、待审批、未闭合工具配对、未闭合 Stop 检查计划/信任/执行或 prepared 候选尚未 discard 期间 MUST NOT 切窗。压缩 SHALL 由当前唯一工作 owner 执行，不启动并行摘要；摘要成功后继续同一任务，不增加普通 turn，不重置 sample/call/result 预算，不重跑已执行工具。

#### Scenario: Continue a long tool turn
- **WHEN** 工具组全部 outputs durable 后达到阈值
- **THEN** 摘要安装后在相同 turn 的下一 sample 继续，无重复执行或 orphan output

#### Scenario: Preserve exclusive ownership
- **WHEN** 自动压缩期间收到手动 compact
- **THEN** 手动请求以 session_busy 拒绝，查询与取消保持响应

#### Scenario: Compact after a closed completion block
- **WHEN** Stop continued 决定已保存但 native 交付未发生，合并候选达到阈值且已 discard
- **THEN** 最多一次摘要且只处理旧窗口，冻结反馈及真实追加输入不进入摘要；原预算不重置，摘要不运行 Stop

#### Scenario: Refuse compaction during a completion check
- **WHEN** Stop 检查尚无完整决定，或一个 prepared 交付候选尚未 discard
- **THEN** 不切窗、不开始摘要，不把检查中断变成已闭合状态

### Requirement: Automatic failures are bounded and durable

同一采样边界 SHALL 最多尝试一次。网络失败、超时、无缩减或摘要响应不合法，在失败已确定收口且 Provider 已完成清理时 SHALL 保持旧窗口并重新执行普通请求预算及编码预检；通过则继续，否则停止或按输入拒绝规则返还。用户取消 MUST 停止当前任务；writer durability 不确定或内部生命周期不变量破坏 MUST 停止后续工作，不得当作可降级失败。已保存输入不得返还后重新执行。

同一来源 context revision 自动失败后 SHALL 在当前进程抑制该来源的自动尝试；连续三次自动失败后 SHALL 在整个进程会话范围熔断，来源 revision 更新不得重置连续计数。成功的手动或自动压缩 SHALL 清零计数并解除抑制；手动压缩不受自动熔断限制，失败不消耗自动失败次数。阈值不满足、无内容、unknown、来源抑制不计失败；符合自动条件但摘要预算预检拒绝计一次失败且不写 started。状态不跨进程保存，恢复不联网重试。

恢复 SHALL 只在初始输入提交前、完整工具 outputs 后或Stop continued 冻结决定已保存、native 交付尚未发生且候选已 discard 的边界接受活动 turn 内自动压缩，验证真实 turn/sample 关联；压缩 started 到 terminal 之间 MUST NOT 插入普通持久化事实。悬空 started 先收口 interrupted compaction，再收口唯一活动 turn；成功 durable completed 必须恢复新窗口。所有摘要验证、120 秒期限、禁工具、Sync 后切窗、取消线性化与 poison 规则 SHALL 复用现有压缩契约。

#### Scenario: Avoid retry loops
- **WHEN** 摘要无缩减或断流且用户重提相同窗口上的任务
- **THEN** 当前进程不对相同来源 revision 重复自动摘要

#### Scenario: Restart after a crash
- **WHEN** 自动压缩 started 后进程退出
- **THEN** resume 在 lease 下收口，零网络、零工具重跑；只有完整 durable completed 能安装窗口

Stop 后的摘要 SHALL 继续使用原阈值、来源抑制、三次熔断、计量及取消/恢复契约；最新待交付反馈及本次真实输入必须保持在摘要之外，重建后原字节交付，不靠摘要文本的语义完整性保证它们存在。恢复先收口摘要，再补齐已接收的本地反馈交付，不运行旧 Stop 或联网重发。

#### Scenario: Recover a post-stop compaction
- **WHEN** continued 冻结决定已保存、native 交付缺失，而自动摘要 started 后中断
- **THEN** 同一 lease 下先收口摘要，再从最终冻结字节补齐本地反馈并关闭旧回合；零网络/Hook，已有交付不得重复

## ADDED Requirements

### Requirement: Stop continuation candidates survive compaction byte for byte

Stop continued 决定 SHALL 先冻结最终模型反馈与交付义务。随后只领取一次队首 auto，以真实 InputIDs/正文和冻结反馈准备同一完整候选，进行大小/阈值预检；摘要必须先 discard 候选，只处理旧 native 有效窗口，成功后重新准备相同候选，不重新渲染反馈、替换参数或读取来源。一次自动尝试后仍超限的真实输入须明确返还，允许重建反馈单独候选但不得再次摘要。反馈单独仍不满足普通预算时只完成本地交付并失败，不发普通网络请求。

交付 Sync/finalize 后至目标 sample 首次请求前 MUST NOT 再次摘要该反馈或已交付的新用户输入；下一请求必须含有它们的完整原生字节。后续普通 sample/工具安全点恢复现有策略，所有计数、熔断和取消契约不重置。该行为不引入局部摘要、保留尾部 checkpoint 或另一个 decoder。

#### Scenario: Preserve feedback even when the summary omits it
- **WHEN** 合法摘要完全省略 Stop reason，而 pending reason 和用户纠正有明确哨兵文本
- **THEN** 摘要请求不含这些 pending 文本，下一普通请求仍逐字节包含反馈和真实纠正，顺序为反馈后用户

#### Scenario: Reject only an oversized steering candidate
- **WHEN** 一次摘要后反馈单独可请求，但反馈与已领取用户输入的合并候选不可请求
- **THEN** 用户 claim 确定返还、原 InputID 不保存，反馈单独交付后继续；不做第二次摘要

#### Scenario: Do not compact a just-delivered correction
- **WHEN** 一/三记录交付已 Sync 而目标 sample 尚未开始
- **THEN** 只进行普通请求守卫，不再把新反馈或用户纠正替换成摘要
