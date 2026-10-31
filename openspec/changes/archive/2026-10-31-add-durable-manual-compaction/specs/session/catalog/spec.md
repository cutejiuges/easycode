## MODIFIED Requirements

### Requirement: Journal discovery preserves descriptor-bound safety and ownership

Catalog reconciliation SHALL 从已安全打开并校验的 Session 根开始，以 descriptor-relative、no-follow 的方式枚举日期目录与 canonical UUIDv7 journal；目录类型、权限、journal 普通文件类型和权限 MUST 绑定到实际参与枚举与加载的句柄。路径穿越、symlink、非普通文件、非法日期层级或文件名 MUST 在内容读取、repair 或 SQLite 投影前拒绝。

对 journal 执行只读 Loader 前，reconciliation MUST 取得该 journal handle 的跨进程 exclusive lease，并在 Loader、ReplayPlanner 和该 journal 投影完成前保持所有权；同一时刻不得持有多个 journal lease。协调 SHALL 只投影完整 committed 前缀，不执行半行或未完成 batch 的截断/Sync；尾部修复留给完整语义及 Provider 校验后的显式 resume，中段损坏不得因 Catalog 重建被删除或跳过后伪装为合法历史。

#### Scenario: Reject a symlink during catalog scan

- **WHEN** 日期目录或 journal 在枚举边界被替换为指向 Session 根外的 symlink
- **THEN** reconciliation 不读取、repair、截断或索引根外目标
- **THEN** 系统失败或继续使用替换前已经安全取得的目录句柄

#### Scenario: Repair a recoverable tail under one lease

- **WHEN** 未被占用的 journal 仅包含 EOF 半行或未完成尾 batch
- **THEN** reconciliation 在同一 exclusive lease 下只读扫描，原始 journal 全部 bytes 不变
- **THEN** Catalog 只投影只读 ReplayPlan 中的 committed records

#### Scenario: Do not inspect an active journal

- **WHEN** 另一个进程持有目标 journal 的 writer lease
- **THEN** reconciliation 不读取、不 repair、不截断该 journal，也不等待超时猜测 owner 状态

### Requirement: Session discovery preserves reconciliation and recovery separation

宿主为列表执行协调时 SHALL 沿用既有 descriptor-bound 枚举、单 journal exclusive lease、严格 load/replay、短事务与 busy/invalid 处理语义。列表查询本身 MUST NOT 自动修复、启动 writer、补偿未完成 turn、恢复 Provider 或执行工具；协调阶段也 MUST NOT 修复，只有 resume 完整验证后才能显式修复。列表不是可恢复性承诺，实际 resume SHALL 重新取得 lease 并校验事实源及 Provider 配置。

#### Scenario: Keep indexed busy metadata without inspecting the journal

- **WHEN** 真实其他进程持有已索引 thread 的 lease
- **THEN** 协调不读取或修复该 journal，列表可返回最后一次已验证元数据
- **THEN** busy 未索引 thread 不凭文件属性创建列表条目，协调报告保留不完整信息

#### Scenario: Rebuild and remove stale rows

- **WHEN** 索引缺失或损坏，或已有条目的 journal 已删除或无法通过校验
- **THEN** 协调按现有规则重建、删除或排除相应投影，不把损坏 journal 当作可恢复条目
- **THEN** 合法完整 journal 的 bytes、record 顺序和 native payload 保持不变

#### Scenario: Leave interrupted turns for explicit resume

- **WHEN** 列表协调读取一个结构合法但 turn 未结束的 journal
- **THEN** 列表只使用已提交事实，不追加 terminal、tool result 或 native output
- **THEN** 后续显式 resume 继续使用既有恢复校验和补偿路径
