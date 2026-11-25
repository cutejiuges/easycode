## MODIFIED Requirements

### Requirement: Claims have one durable resolution

自动输入 SHALL 经 queued、claimed、committing 到 committed，确定无写入可 returned，durability 不确定必须 uncertain 并 poison。每 turn 最多一个未解决 claim，claim 未解决不得丢弃所有权或启动新 turn。领取不是保存，也不代表远端处理。

`UserPromptSubmit` 的结果由同一 owner 按完整原 claim 和各 `InputID` 一次性核对。被拒项按原 claim 顺序以固定原因返还，并立即结束各自队列占用。通过项保有原 claim 内的归属、顺序及额度，直到真实输入提交确定结果。每个输入只发布一个终态。最初承载 turn 的宿主请求仍持有 turn 完整生命周期。用户再次提交时分配新的输入身份。

初轮全拒 SHALL 结束新 turn，被拒正文不进入 transcript。工具输出已保存后，拒绝追加不改变先前工具结果或回合能力，以既有上下文和仍合格的输入继续。Stop 续轮中被拒的纠正不得影响已接纳反馈的耐久交付。取消时等待真实输入写入确定结果。确定已拒项不受通过项后续持久化不确定影响。

混合检查中接收取消时，已耐久的拒绝保持原原因。尚未进入原生写入的其余输入返回 cancelled，未检查项不获得通过或已执行状态。检查事实写入未知且可以确定原生输入未被接纳时，原输入可取回，当前实例按 writer 不健康收口。若原生输入 batch 已经被 writer 接纳，仍以真实 writer 结果裁定其输入状态。任何阻断、取消、关闭或恢复都不得自动重新入队。

#### Scenario: Resolve cancellation before admission
- **WHEN** writer 尚未接受且 turn 取消
- **THEN** discard staging 并确定返还，零输入写入

#### Scenario: Resolve cancellation after Sync
- **WHEN** writer 已接受并 Sync 成功但 turn 同时取消
- **THEN** finalize 一次并报告 committed，不返还、不重发

#### Scenario: Poison uncertain persistence
- **WHEN** 输入写入或 Sync 结果无法确认
- **THEN** 报告 uncertain 并停止后续副作用，保留 cleanup ownership，不当作 queued 重试

#### Scenario: Preserve siblings around a rejected steering input
- **WHEN** 一次安全点领取 A、B、C，B 被 Hook 拒绝，A 和 C 通过
- **THEN** B 以原 ID 可取回，A 和 C 在同一真实输入批次中按原顺序提交，已保存的工具结果和后续 FIFO 屏障保持有效

#### Scenario: Keep the starting request attached after returning its input
- **WHEN** A 和 B 共享一个初始 turn，承载回合的 A 被拒而 B 通过
- **THEN** A 只收到一次可取回状态且仍承载回合终态。只有 B 的原文可提交，B 保有自身输入身份和回执

#### Scenario: Keep accepted Stop feedback after denying a correction
- **WHEN** Stop 已作出耐久续轮决定，而同轮待交付的全部真实纠正被拦截
- **THEN** 反馈按既有 FeedbackOnly 交付路径收口，模型请求不含拦截的纠正
