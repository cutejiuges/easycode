## MODIFIED Requirements

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

Stop 后的摘要 SHALL 继续使用原阈值、来源抑制、三次熔断、计量及取消/恢复契约；最新待交付反馈及本次真实输入必须保持在摘要之外，重建后原字节交付，不靠摘要文本的语义完整性保证它们存在。恢复依次收口摘要、适用的 PostCompact、已接纳的 SessionStart(compact)，再补齐已接收的本地 Stop 反馈交付，不运行旧 Hook 或联网重发。

#### Scenario: Recover a post-stop compaction
- **WHEN** continued 冻结决定已保存、native 交付缺失，而自动摘要 started 后中断
- **THEN** 同一 lease 下先收口摘要，再从最终冻结字节补齐本地反馈并关闭旧回合；零网络/Hook，已有交付不得重复

#### Scenario: Fall back after a durable automatic hook rejection
- **WHEN** 同一活动 turn 的自动压缩被 PreCompact 明确阻止，确定失败已经 Sync，Hook 清理完成且旧窗口仍满足普通预算
- **THEN** 不发送摘要请求，当前来源受到抑制，同一 turn 依原生顺序继续普通采样，已提交的输入及工具输出均不会重新执行

#### Scenario: Stop after an uncertain automatic hook fact
- **WHEN** 自动压缩的检查事实提交结果不确定，或检查未能确认进程清理
- **THEN** 停止后续副作用。不得把不确定状态当作普通请求回退依据，不得释放仍由原 owner 持有的资源

### Requirement: Automatic sampling waits for post-compaction observation

初始输入、工具续轮和 Stop 待交付边界 SHALL 在同一压缩 owner 内等待已安装 checkpoint 的 PostCompact 阶段及适用的 SessionStart(compact) 依次收口和清理，再提交后续 native facts 或发起普通 sample。检查经各自授权读取的输入受原事件契约约束，所有待交付正文和附件保持原有顺序、边界与字节。成功安装清零自动失败计数。拒绝、确定的检查错误和跳过 MUST 不计为自动压缩失败，不重新摘要，健康且未取消时以新窗口继续原有预算检查。已成功 checkpoint 后的取消 MUST 停止下一 sample，保留新窗口和既有输入返还规则；健康 Session 仍按已有契约本地交付待提交的 Stop 反馈。后置事实写入失败或清理不确定 MUST 以独立 Session 错误阻止后续 native 提交、Stop 反馈交付和下一 sample，保留已确认 checkpoint 并交由恢复处理未关闭尾部，不产生自动压缩失败计数。

#### Scenario: Continue after a known observer failure

- **WHEN** 自动 checkpoint 成功，PostCompact 已确认清理的退出失败发生在一次工具续轮之前
- **THEN** 先发布压缩成功，再从新窗口继续原任务；不增加摘要失败计数，也不重跑工具或摘要

#### Scenario: Interrupt a post hook for an automatic Stop continuation

- **WHEN** Stop 的已持久化反馈等待交付，自动压缩成功后 PostCompact 仍在清理时收到 turn 中断
- **THEN** 保留新 checkpoint，不启动普通 sample，待交付反馈和用户输入仍由既有 Stop 取消及恢复归属收口

#### Scenario: Stop all automatic continuation after an uncertain post check

- **WHEN** checkpoint 已成功安装，PostCompact 的关闭 Sync 或进程清理不确定
- **THEN** 保留唯一已知成功的压缩，既不增加自动压缩失败计数，也不提交尚待交付的输入或 Stop 反馈，不开始下一 sample，Session 进入失败关闭

## ADDED Requirements

### Requirement: Automatic continuation preserves ordering around the compact supplement

自动压缩后的 SessionStart(compact) SHALL 使用已安装窗口和真实压缩作用域执行。Runtime SHALL 将候选补充内容纳入普通编码及窗口预检，并验证带补充的窗口小于旧窗口。存在已知的下一普通候选时，预检 SHALL 覆盖该候选的原身份、原字节、Skill/文件附件及适用的冻结 Stop 反馈。当该候选无法在加入补充内容后通过确定的预算或编码校验时，关闭为跳过并沿用原有后续预检和归还规则。不得领取第二批输入或再次摘要。候选必须包含无 steering 时的下一工具采样。若裸候选本身超限，原有真实输入返还及 Stop feedback-only 预检仍按原规则处理。在首次输入场景中若另有待运行的 startup/resume，后者沿用应用 compact 内容之后的实际窗口进行独立的最终输入候选预检。

成功提交的原生顺序 SHALL 按真实安全点保持为摘要、compact 上下文、适用的 startup/resume 上下文、初始输入，或摘要、compact 上下文、适用的 steering、普通 sample。Stop 续轮使用摘要、compact 上下文、已冻结的 Stop 反馈、适用的 steering、普通 sample。跳过的内容不占原生位置。计量、工具结果配对、队列所有权、Stop 交付义务及自动压缩尝试预算保留既有归属。

#### Scenario: Order both context sources before an initial input
- **WHEN** 恢复后首个普通回合触发初始自动压缩，compact 与 resume 均有可交付的上下文
- **THEN** 两项原生提交按摘要、compact、resume、真实用户输入的顺序出现，只有一条原始输入身份且无额外摘要

#### Scenario: Preserve the next candidate at tool and Stop boundaries
- **WHEN** 已闭合工具组或 Stop 决定后成功自动压缩，compact 补充内容可应用或因合并候选超限而被省略
- **THEN** 原冻结的 Stop 反馈和已领取 steering 保持各自原字节。合法补充内容在两者之前，省略时只恢复原后续候选预检，无重复摘要、运行或领取
