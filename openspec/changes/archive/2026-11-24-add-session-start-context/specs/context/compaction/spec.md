## MODIFIED Requirements

### Requirement: Installed compaction remains committed during observation

成功 checkpoint SHALL 在 PostCompact 审查或执行前 Sync、finalize 并发布已安装的 committed Context/Cache 快照。Context 更新至实际新 revision，Cache 更新摘要计量并继续用已有普通采样基线表示最近采样。公共 completed SHALL 在适用的 PostCompact 与 SessionStart(compact) 阶段依序收口且 owned cleanup 结束后发布一次。同一压缩 ID 及 scope 保持活动，命令、输入与恢复继续服从现有互斥和定向取消规则。确定的后置失败、拒绝或取消 SHALL 保留新窗口、usage 和成功终态。已安装之后的后置结果不确定 SHALL 关闭后续接纳，同时保持已知的压缩成功事实。

成功 checkpoint 已 Sync 但当前进程未能 finalize 或安装必要的宿主投影时，SHALL 以既有未安装的 Session 错误结束，禁止派发 PostCompact 或 SessionStart(compact)、宣称已切窗或追加 `compaction_failed`。恢复 SHALL 验证并安装该成功 checkpoint。

#### Scenario: Query the new window while the post hook waits for trust

- **WHEN** checkpoint 已安装，匹配 PostCompact 正等待用户审查
- **THEN** 宿主 typed context/cache 查询返回新 committed revision 和已确认的摘要计量，最近普通采样基线不变，活动压缩仍可按真实 ID 取消；尚未发布最终 completed，信任弹窗继续独占键盘

#### Scenario: Preserve a checkpoint after an in-process installation failure

- **WHEN** 成功记录已 Sync，而当前进程在 finalize 或必要的宿主安装时发生 Session 错误
- **THEN** 保留已持久化成功记录，零审查、零脚本、零后续 sample 或失败记录；恢复从该记录安装窗口

## ADDED Requirements

### Requirement: Compact session context supplements an already installed checkpoint

原有手动与自动压缩 owner SHALL 仅在成功 checkpoint 已 Sync、finalize 并安装宿主投影，以及适用的 PostCompact 已确定收口后，派发匹配的 SessionStart(compact)。目标绑定确切 checkpoint、CompactionID 与手动或自动 scope。失败、取消而未安装的摘要、非交互路径、只读预览和恢复旧 checkpoint MUST 不运行该检查。匹配为空时不存在额外的检查或提交。取消发生在 SessionStart 计划接纳前时，零新计划、审查和命令。已接纳计划按真实运行前缀收口。

获准上下文 SHALL 在已安装窗口之后作为独立原生补充预检，不修改 checkpoint 内容、旧记录、摘要用量、模型任务状态或压缩成功事实。带补充内容的有效窗口必须仍满足原窗口缩减条件、普通预算与编码边界；自动路径还须按对应安全点检查已冻结的真实后续候选。确定超限、拒绝、有确认清理的运行错误或取消 SHALL 按各自状态关闭并保留已安装窗口。成功 applied 关闭 Sync 后，才可更新原生修订与宿主内容。同一操作仅有一次摘要，不以新摘要弥补上下文超限。刚安装的 checkpoint 后仅有 compact 补充时，手动及自动仍视为无待压缩的新增历史。真实后续历史存在时，该补充 SHALL 参与完整窗口估算、压缩来源校验和摘要请求。后置持久化或结果不确定 SHALL 保留成功 checkpoint，poison 当前运行实例，并停止后续副作用。手动压缩在安装后收到当前取消时，公共终态仍报告一次已保存成功，操作内部 SHALL 以取消完成，按现有规则返还已排队 auto 输入并保留 strict-next。

#### Scenario: Apply a manual compact supplement
- **WHEN** 手动 checkpoint 已安装且 PostCompact 已收口，新的已获准上下文满足缩减、窗口和编码要求
- **THEN** 同一压缩操作保存补充上下文并更新有效窗口，之后报告一次压缩成功。下个真实对话请求按原生顺序使用已安装摘要、补充内容和用户输入

#### Scenario: Do not recompact only the just-applied supplement
- **WHEN** 手动压缩已保存 compact 补充，用户在任何真实后续历史提交前再次请求压缩
- **THEN** 返回原有无新内容状态，零额外摘要或 compact Hook。之后真实历史提交可按原规则再次压缩，成功摘要覆盖已提交补充

#### Scenario: Preserve success when a supplement would remove the reduction
- **WHEN** 已安装的摘要有缩减，追加候选上下文后的有效窗口达到或超过压缩前窗口
- **THEN** 状态报告预算跳过，原 checkpoint 和摘要计量保持，零第二次摘要
