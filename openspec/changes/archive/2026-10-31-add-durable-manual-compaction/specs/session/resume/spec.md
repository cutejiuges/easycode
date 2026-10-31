## ADDED Requirements

### Requirement: Resume restores checkpoints and locally closes interrupted compaction

resume/continue SHALL 在连续 exclusive lease、完整 Loader/replay 和 Provider 兼容/原生验证后恢复最新合法 checkpoint 及后续增量，同时保留原始 transcript 和工具详情。只存在 started 的压缩尾部 SHALL 在接收新输入前本地 Sync 唯一 compaction_failed(code=compaction_interrupted)，保持旧窗口；不得自动联网生成摘要、重读文件或执行工具。completion 已完整持久化但原进程未 finalize 时 SHALL 恢复新窗口，不再追加失败或重复 checkpoint。最新 checkpoint 损坏、来源错配或 Provider 不兼容 MUST 拒绝整个恢复，不回退旧 checkpoint，也不写补偿。

当前启动项目指令/工具目录 SHALL 继续作为外部上下文；checkpoint 不恢复旧 cwd、secret、配置、队列或 permission grant。Catalog 继续可从 JSONL 重建，以最新合法 seq/timestamp 表示最近活动，不保存摘要正文。

#### Scenario: Close an interrupted summary locally
- **WHEN** 合法 journal 以 compaction_started 结束
- **THEN** 本地收口失败后暴露旧窗口，Provider/工具/文件重读次数均为零

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

### Requirement: Resume validates before tail repair

resume/continue SHALL 使用连续 exclusive lease 只读加载，先完成 replay、Provider 兼容及原生/checkpoint 校验，再显式修复尾部并 Sync，然后启动 writer 和本地 reconciliation。校验失败 MUST 保持包括未完成尾部在内的原始 bytes 不变。Catalog 扫描 MUST NOT 执行尾部修复。修复失败或 durability 不确定 MUST 阻止 writer 和新副作用。

#### Scenario: Reject an incompatible session with a partial tail
- **WHEN** 当前模型与 Session 不兼容且文件末尾存在半行
- **THEN** 拒绝恢复，journal 全部 bytes 不变

#### Scenario: Repair before local reconciliation
- **WHEN** committed 前缀及 Provider 完整验证通过，尾部存在未完成 batch
- **THEN** 同一 lease 下先修复并 Sync，再从正确 next seq 本地收口，不释放后重新打开文件
