## ADDED Requirements

### Requirement: Installed compaction remains committed during observation

成功 checkpoint SHALL 在 PostCompact 审查或执行前 Sync、finalize 并发布已安装的 committed Context/Cache 快照。Context 更新至实际新 revision，Cache 更新摘要计量并继续用已有普通采样基线表示最近采样。公共 completed SHALL 在后置阶段及其 owned cleanup 结束后发布一次。同一压缩 ID 及 scope 保持活动，命令、输入与恢复继续服从现有互斥和定向取消规则。确定的后置失败、拒绝或取消 SHALL 保留新窗口、usage 和成功终态。已安装之后的后置结果不确定 SHALL 关闭后续接纳，同时保持已知的压缩成功事实。

成功 checkpoint 已 Sync 但当前进程未能 finalize 或安装必要的宿主投影时，SHALL 以既有未安装的 Session 错误结束，禁止派发 PostCompact、宣称已切窗或追加 `compaction_failed`。恢复 SHALL 验证并安装该成功 checkpoint。

#### Scenario: Query the new window while the post hook waits for trust

- **WHEN** checkpoint 已安装，匹配 PostCompact 正等待用户审查
- **THEN** 宿主 typed context/cache 查询返回新 committed revision 和已确认的摘要计量，最近普通采样基线不变，活动压缩仍可按真实 ID 取消；尚未发布最终 completed，信任弹窗继续独占键盘

#### Scenario: Preserve a checkpoint after an in-process installation failure

- **WHEN** 成功记录已 Sync，而当前进程在 finalize 或必要的宿主安装时发生 Session 错误
- **THEN** 保留已持久化成功记录，零审查、零脚本、零后续 sample 或失败记录；恢复从该记录安装窗口
