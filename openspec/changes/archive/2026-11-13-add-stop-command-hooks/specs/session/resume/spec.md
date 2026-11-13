## ADDED Requirements

### Requirement: Resume closes completion checks using only frozen facts

resume SHALL 在连续 exclusive lease、全部当前记录/replay/Provider/checkpoint 校验及合法显式 repair 后，先本地关闭中断摘要，再收口未完成 Stop，最后才安装当前 Hook 来源和接受新输入。不得读取旧配置/脚本/artifact、请求信任、查询 PID、重新执行 Hook/工具或自动请求 Provider。允许纯内存 Provider codec 从已验证事实编码本地交付，不等于允许网络请求。

checks plan 未 started 或仅有成功前缀时 SHALL 同批追加 plan-scope、recovery-origin decided(cancelled, session_interrupted_before_stop_hook) 与 turn_failed，processed 保持前缀；不得给未启动项制造 finished。unavailable plan SHALL 使用冻结的配置/匹配错误收口，不重新推测当前配置。已 started 未 finished 的唯一项 SHALL 与 hook-scope、recovery-origin outcome_uncertain finished、decided(outcome_uncertain) 及 turn_failed 同批，来源指向该真实 ordinal，不宣称旧进程 cleanup 已确认。此前 sample、usage 和事实 bytes 保留。

#### Scenario: Close a check before it starts
- **WHEN** journal 以 sample/usage/checks plan 或合法成功前缀结束
- **THEN** 本地取消并关闭 turn，零 loader/trust/runner/executor/Provider 网络，未运行后缀没有假 finished

#### Scenario: Close an uncertain completion command
- **WHEN** Hook started 已保存而 finished 缺失
- **THEN** 仅该项补 uncertain，决定及 turn_failed 同批，不重跑、不查询进程或伪造 cleanup

#### Scenario: Preserve a frozen source failure
- **WHEN** unavailable plan 已保存而配置后来修好或删除
- **THEN** 恢复仍按原计划安全错误关闭，不重新加载配置或转为通过

### Requirement: Restored stop feedback is never resent or reauthorized

continued 决定 durable 但 native 交付缺失时，resume SHALL 以 prior 决定中最终冻结模型字节和来源纯内存准备 FeedbackOnly，并成功 Sync/finalize 恰好一条 native(hook_feedback)，再补唯一 session_interrupted turn_failed。不得重渲染 reason/wrapper、重新请求授权、增加决定/usage/计费或恢复未保存的用户队列；不得自动把本地交付发送给模型。已保存一记录或三记录交付时 SHALL 恢复实际 native entries 与真实用户关联，不再次交付、返还或重发已保存用户输入。已保存 passed/failed terminal batch 完全复用。

continued 后若有未完成 auto compaction，SHALL 先按原契约本地收口摘要，再完成 FeedbackOnly 交付并关闭 turn；成功 checkpoint 恢复被摘要的旧窗口，再交付冻结原文，不能把尚未保存的反馈或用户输入推定已经包含于摘要。feedback 交付后至其目标 sample 首次请求前的新摘要属于非法事实，不作为可重试恢复窗口；后续普通 sample/工具安全点仍沿既有规则。后续真实工具尾部仍按现有 Tool recovery 配对，不能因为 Stop 扩大执行授权。

下一次真实用户输入 SHALL 从已补偿 native history 续写；相同快照/config 下 canonical request、native 次序、opaque bytes、footprint 和 fingerprints 与同一已补偿历史不中断路径等价。历史信任不安装到新进程，当前来源只服务未来真正调用。纯内存交付编码失败必须失败关闭，durability 不确定必须 poison，不能放弃已接收义务而发布可用 Session。

#### Scenario: Resume after feedback without sampling
- **WHEN** continued 与一记录或三记录 native 交付完整保存后进程退出
- **THEN** 恢复全部真实条目且只关闭旧 turn；Provider 网络/Hook 次数为零，下一真实输入请求与同一事实不中断路径相等

#### Scenario: Recover the post-feedback checkpoint
- **WHEN** continued 后、反馈交付前的旧窗口 auto compaction completion 已保存而进程尚未继续
- **THEN** 恢复新窗口、交付原冻结反馈并关闭，不重新摘要、自动发请求或宣布旧回合成功；反馈后的再摘要不是此合法窗口

#### Scenario: Resume after the hook script disappears
- **WHEN** 有已保存检查/反馈的脚本、配置或输出对象消失
- **THEN** 恢复 bytes 前缀不变，历史只读详情和下一请求仍只由 native/frozen facts 生成

#### Scenario: Complete a pending local delivery
- **WHEN** continued 冻结决定已保存但没有 native feedback
- **THEN** 本地交付恰好一次原最终字节并关闭旧 turn，无新决定/计费、重渲染、配置读取或网络

#### Scenario: Close compaction before delivering frozen feedback
- **WHEN** pending delivery 期间 compaction_started 已保存而 completion 缺失
- **THEN** 先本地保存 compaction_interrupted，再交付反馈并关闭，不恢复未保存用户 claim 或重试摘要

#### Scenario: Recover an admitted user correction
- **WHEN** 三记录交付已 Sync，而内存 finalize/宿主通知前崩溃
- **THEN** 反馈和真实用户 InputIDs/Skill 附件均恢复一次，用户输入保持 committed；无反馈重复、队列返还或虚构用户事件

### Requirement: Completion recovery retains lease and repeated-crash idempotency

Stop reconciliation SHALL 与 load/repair/writer 共用同一连续文件所有权窗口，直到最终 Sync/Close；真实竞争进程在读后 repair、append 或 Provider 恢复前 session_busy，失败路径 journal bytes 不变。二次崩溃后的恢复不得重复 finished/decision/native feedback/user inputs/usage/terminal，seq 单调且原前缀不改写；只补实际缺失的本地交付或终态。任何更早语义/Provider 损坏即使另有半行，也必须在 repair/补偿前整体拒绝；Sync 不确定不得发布可用 Session。

#### Scenario: Compete with recovery from another process
- **WHEN** 持有者正在补偿 Stop 而真实子进程请求同 journal
- **THEN** 子进程 session_busy、零 repair/append，持有者释放或崩溃后才可重新获取 lease

#### Scenario: Crash after a recovery batch
- **WHEN** 首次补偿交付已 Sync、terminal 尚未保存时再次退出
- **THEN** 第二次恢复复用交付，只补真正缺失终态，不重复任何模型反馈、用户输入或预算扣账

#### Scenario: Reject corruption before repairing a suffix
- **WHEN** 合法外壳下的 Stop 身份/计量/反馈语义非法且文件另有半行
- **THEN** 拒绝且全部原 bytes 不变，零 repair/补偿/外部调用
