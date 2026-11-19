## MODIFIED Requirements

### Requirement: Resume restores checkpoints and locally closes interrupted compaction

resume/continue SHALL 在连续 exclusive lease、完整 Loader/replay 和 Provider 兼容/原生验证后恢复最新合法 checkpoint 及后续增量，同时保留原始 transcript 和工具详情。合法的中断压缩即使已有部分或全部 PreCompact 检查事实，仍 SHALL 在接收新输入前本地 Sync 唯一 `compaction_failed`（code 为 `compaction_interrupted`，usage_state 为 `unavailable`），保持此次压缩前的窗口。恢复只从已验证的事实判断是否已经存在终态。不得补造 PreCompact finished、决定、脚本清理结论或摘要用量，不得重读旧 Hook 配置和脚本、请求信任、查询旧进程、重新运行检查、执行工具或发起摘要网络请求。completion 已完整持久化但原进程未 finalize 时 SHALL 恢复新窗口，不再追加失败或重复 checkpoint。最新 checkpoint 损坏、来源错配或 Provider 不兼容 MUST 拒绝整个恢复，不回退旧 checkpoint，也不写补偿。

当前启动项目指令/工具目录 SHALL 继续作为外部上下文；checkpoint 不恢复旧 cwd、secret、配置、队列或 permission grant。Catalog 继续可从 JSONL 重建，以最新合法 seq/timestamp 表示最近活动，不保存摘要正文。

#### Scenario: Close an interrupted summary locally
- **WHEN** 合法 journal 以 compaction_started、其任一已提交检查前缀或已通过的 PreCompact 决定结束
- **THEN** 只为本次压缩追加一个中断失败终态，使用旧窗口，Provider 网络、历史脚本/工具运行和信任收集次数均为零
- **THEN** 已提交检查事实逐字不变，缺失 finished 或 decided 继续如实保持缺失

#### Scenario: Resume after Sync before finalize
- **WHEN** completion 已 durable 而进程在内存切换前退出
- **THEN** 重启直接恢复新窗口，下一请求等于 uninterrupted，操作不重复计量或执行

#### Scenario: Fail compensation safely
- **WHEN** interrupted compaction 的补偿 append/Sync 失败
- **THEN** resume 失败并阻止新输入副作用，不放弃 cleanup ownership

#### Scenario: Validate before compensation
- **WHEN** journal 有未完成 started 但更早 checkpoint 来源错配或当前 Provider 配置不兼容
- **THEN** 拒绝恢复，不写 compaction_failed、不改变已有 journal bytes

#### Scenario: Keep history readable after compaction
- **WHEN** 用户恢复压缩过的会话且旧 artifact 文件缺失
- **THEN** 原历史及冻结工具 preview 仍可显示，续写只需原生窗口；按需读 artifact 才报告 unavailable

恢复所有 checkpoint SHALL 验证其必需 task_progress 与各自 compaction_started source 前缀的最新成功任务事实逐字一致。none 只能来自从无成功更新的前缀；clear/snapshot 必须匹配完整规范更新及真实 source result_sequence、turn_id、invocation_id。未来 Task 结果不参与旧 checkpoint 的期望值；恢复最终当前列表继续取完整 ledger 的最新成功结果，不仅取最后摘要自述或 checkpoint 当时列表。

Session/replay 只计算窄 typed 期望状态，Provider 在自身 codec 中验证当前 payload、generation 和完整确定性包装。当前 source identity 不进入模型包装或内容 fingerprint；ProjectCheckpoint 继续投影真实模型 generation，原生 footprint 覆盖其完整任务包装。缺失 task_progress 的旧开发 shape、来源/内容/原生包装错配以及伪造 clear/none MUST 在 repair、补偿、writer 接管和任何网络之前整体拒绝，不回退旧 checkpoint 或加默认字段。

#### Scenario: Restore a task-preserving checkpoint before later updates
- **WHEN** checkpoint 保存甲，后续 native 调用成功更新乙，之后进程退出
- **THEN** checkpoint 用旧 source 验证甲，当前列表从后续成功 result 恢复乙；请求只含有效原生窗口和后续增量，不额外注入第三份列表

#### Scenario: Validate an explicit clear against its saved result
- **WHEN** checkpoint 声称 clear，但 source 前缀最新成功更新为非空列表，或 source 指向失败结果
- **THEN** 恢复失败且 journal bytes 不变，不从模型摘要或当前 UI 推测修复状态

#### Scenario: Reject a missing task checkpoint field with a torn tail
- **WHEN** checkpoint 缺少必需 task_progress 且尾部另有半行
- **THEN** 在 repair/补偿之前拒绝整个恢复，所有旧 bytes 不变，不使用兼容 reader

#### Scenario: Resume task state after completion Sync before finalize
- **WHEN** 含精确任务快照的 completion 已 Sync，进程在内存窗口 finalize 前退出
- **THEN** 恢复新原生窗口及完整规范清单，generation 仍是真实模型文本，零摘要网络、任务执行、重复更新来源或重复计费

Session picker 的离线 reader SHALL 与在线恢复共用完整 Task ready/result 和 checkpoint expected evidence 装配；即使预览只返回文本也必须执行同等来源校验。合法任务 checkpoint 应可离线预览；source 指向 ready/detail/native seq、另一合法结果、未来成功结果，或原生任务 wrapper 被修改后重算 checksum，仍必须整体拒绝，不 repair、不补偿、不联网，原 bytes 不变。每个 checkpoint 的规范更新/状态和查询不携带 facade profile，原输入 profile 的验证不能因离线预览绕过。

恢复后普通、输入候选、摘要候选的规划 SHALL 分别使用对应已验证状态派生的 kind 与规范 domain.TaskUpdate 内容 digest，none 省略；它仅进入同一 committed_history canonical 元数据，不增加 token 或重复网络注入。完整来源 identity 必须校验，但不进入该 digest 或原生摘要包装。

#### Scenario: Preview a valid task checkpoint without model configuration
- **WHEN** Session picker 使用离线 StoredHistoryReader 预览两家合法的任务更新与 checkpoint
- **THEN** 原生输入/结果及 checkpoint 来源全部验证后返回有界预览，不要求模型连接、零网络、零 repair/追加且 bytes 不变

#### Scenario: Reject a forged result sequence in picker preview
- **WHEN** checkpoint 的 source result_sequence 被换成 ready/detail/native seq、其他合法结果或未来结果，checksum 已重新计算
- **THEN** 离线预览与在线恢复均整体拒绝，原文件包括半行尾部的全部 bytes 不变，零 repair/补偿/网络

#### Scenario: Reject a rewritten task wrapper in stored history
- **WHEN** checkpoint 任务 metadata 未变但原生 wrapper 的标签/状态/说明被修改，外层 checksum 已重新计算
- **THEN** 离线 reader 仍按同一 typed expected state 和 Provider codec 拒绝，不能因原生 JSON 自洽而返回部分预览

压缩检查事实及其顺序 SHALL 在 repair、writer 接管和补偿前完整校验。离线预览 SHALL 复用同一检查验证且始终只读，合法中断最多呈现不完整历史。自动压缩发生在 Stop continued 交付前时，SHALL 先关闭压缩，再完成已有的本地 Stop 反馈交付和旧 turn 收口。已成功补偿后再次启动 SHALL 复用已有终态，不重复中断失败、Stop 交付或 turn terminal。没有完整压缩成功事实时不得因检查通过或疑似曾发请求而安装新窗口。

#### Scenario: Recover checked auto compaction before Stop delivery
- **WHEN** 自动压缩已经保存 PreCompact 部分结果或通过决定，发生退出时仍无压缩终态，并且前缀存在待交付的 Stop continued 决定
- **THEN** 同一 lease 下依次关闭压缩、在本地交付已冻结的 Stop feedback、关闭旧 turn，保留压缩前窗口，零 Hook 和 Provider 调用

#### Scenario: Do not repeat local compensation after a second crash
- **WHEN** 首次恢复已成功 Sync compaction_interrupted 后、尚未完成旧 turn 收口便再次退出
- **THEN** 下次恢复只补实际缺失的 Stop 交付或 turn 终态，序号继续递增且已有检查和压缩事实保持原字节

#### Scenario: Reject invalid check evidence before repairing a suffix
- **WHEN** 完整 PreCompact 记录的 CompactionID、scope、plan、ordinal 或生命周期造假，末尾另有半行
- **THEN** 在线恢复与离线预览整体失败，全部 journal bytes 不变，无 repair、补偿、旧配置读取或外部调用

#### Scenario: Restore a completed checked checkpoint without reissuing its request
- **WHEN** 已通过 PreCompact 的新 checkpoint 成功 Sync，随后在内存安装之前退出
- **THEN** 恢复已保存的窗口而不重复检查、摘要、用量或计费
- **THEN** 相同配置和真实下一输入下，普通请求 canonical bytes、原生顺序和 fingerprint 与不中断路径一致
