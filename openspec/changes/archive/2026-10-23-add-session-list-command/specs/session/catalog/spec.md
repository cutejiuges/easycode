## ADDED Requirements

### Requirement: Catalog supports bounded root session discovery independent of provider configuration

Catalog SHALL 提供当前规范化创建目录或全部目录的 root 会话列表查询，不要求 Provider family、wire、model selector；同目录下不同 Provider/model 的会话 SHALL 均参与候选集。列表 MUST 按 committed `updated_at` 降序、canonical `thread_id` 降序排序，先筛选再限制数量；数量范围为 1–200，非法范围或非法目录条件 MUST 在查询前拒绝。查询 SHALL 返回至多指定数量的条目及 `has_more`，仅当当前筛选条件下存在额外可查询条目时为 true。

列表 MUST 使用现有已验证元数据，不新增 prompt、response、标题、摘要或 opaque payload 索引；当前目录匹配 MUST 使用规范化目录精确相等，不得隐式匹配子目录、同名目录或其他 worktree。现有最近兼容会话查询 SHALL 保留四字段精确筛选语义。

#### Scenario: Discover different provider sessions in one directory

- **WHEN** 同一目录存在 OpenAI 与 Anthropic、不同模型的合法 root 会话
- **THEN** 当前目录列表按统一更新时间顺序返回这些会话，不依赖当前 Provider 配置
- **THEN** 最近兼容会话查询仍只返回其精确 selector 对应的会话

#### Scenario: Apply directory filter before limit

- **WHEN** 其他目录拥有更新的会话，而当前目录查询限制为 1
- **THEN** 返回当前目录最新条目，其他目录不占用返回数量
- **THEN** 子目录、同名前缀目录和兄弟 worktree 不被隐式纳入

#### Scenario: Return exact overflow information

- **WHEN** 筛选后分别存在 N 或 N+1 个可查询条目，调用方请求 N 个
- **THEN** 两次查询均最多返回 N 个，前者 `has_more` 为 false，后者为 true
- **THEN** 时间相同的条目按 canonical thread ID 降序排列，重建 SQLite 后结果等价

#### Scenario: Reject invalid query bounds

- **WHEN** 数量为 0、负数、超过 200，或当前目录条件不是规范化绝对目录
- **THEN** 查询失败，不返回部分条目，不执行写入或 journal 加载

### Requirement: Session discovery preserves reconciliation and recovery separation

宿主为列表执行协调时 SHALL 沿用既有 descriptor-bound 枚举、单 journal exclusive lease、严格 load/replay、短事务与 busy/invalid 处理语义。列表查询本身 MUST NOT 自动修复、启动 writer、补偿未完成 turn、恢复 Provider 或执行工具；协调阶段允许的尾部修复仍遵循既有契约。列表不是可恢复性承诺，实际 resume SHALL 重新取得 lease 并校验事实源及 Provider 配置。

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
