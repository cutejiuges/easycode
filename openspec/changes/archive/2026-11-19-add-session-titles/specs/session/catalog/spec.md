## MODIFIED Requirements

### Requirement: Catalog metadata and ordering are deterministic

每个可查询 root thread 投影 SHALL 至少包含 `session_id`、`thread_id`、journal 相对定位、`created_at`、最后 committed record 的 `updated_at`、最后 committed `seq` 与 checksum、规范化 `creation_cwd`、Provider family、wire 和 model。所有字段 MUST 从已验证 ReplayPlan 及其 committed records 派生；文件 mtime、ctime、inode、扫描时间和 SQLite 写入时间 MAY 仅作为重扫提示，不得成为用户可见 recency 或兼容性事实。

Catalog SHALL 支持按规范化 `creation_cwd`、Provider family、wire 和 model 精确筛选 root thread。最近排序 MUST 使用 `updated_at` 降序，并以 canonical `thread_id` 降序打破时间相同的平局，使重复查询、重建和不同进程得到相同结果。

当前标题 SHALL 来自该 root journal 已验证的最后一条标题事实。没有标题事实的会话 SHALL 以未命名状态投影。标题事实成为 journal 最后一条 committed record 时，Catalog SHALL 以它的时间、seq 和 checksum 更新对应字段，继续用同一排序规则参与列表及最近兼容会话查询。重命名成功 SHALL 只以 journal 的持久提交为依据。Catalog 在后续协调取得 lease 并完成验证时更新标题。busy journal 的已索引标题 SHALL 与其他字段一同保留最后一次已验证的值。

一次协调发布的是各 journal 在各自 lease 下验证的快照。某个 journal 在释放 lease 后、SQLite 发布前更新时，该轮查询仍使用本次已验证值。列表与最近兼容会话查询 SHALL 使用该快照的排序；下一次在新事实提交后获取该 journal lease 的成功协调 SHALL 看到更新。预览与实际恢复继续按 thread ID 独立校验 journal。

#### Scenario: Order by durable record time

- **WHEN** 两个兼容 thread 的文件 mtime 与最后 committed record timestamp 顺序不同
- **THEN** Catalog 按 record `updated_at` 而不是文件 mtime 选择更新的 thread

#### Scenario: Break equal-time ties deterministically

- **WHEN** 两个候选 thread 具有相同 `updated_at`
- **THEN** Catalog 按 canonical `thread_id` 降序返回稳定顺序

#### Scenario: Filter exact compatibility fields

- **WHEN** 同一 `creation_cwd` 下存在不同 Provider family、wire 或 model 的 root Session
- **THEN** 精确筛选只返回四个条件全部匹配的候选

#### Scenario: Reconcile the latest durable title and record time

- **WHEN** 已索引会话先后完成两次重命名，第二条标题事实是最新的 committed record
- **THEN** 取得 journal lease 并成功协调后，Catalog 投影第二次标题及该记录的时间、seq 和 checksum
- **THEN** 列表和精确兼容查询仍按该更新时间及 canonical thread ID 排序

#### Scenario: Preserve validated title while the journal is busy

- **WHEN** 另一个进程持有已索引 journal 的 writer lease 并已持久化新标题，而协调无法取得该 lease
- **THEN** 查询可返回上次已验证的标题及该行原有的时间、seq 和 checksum
- **THEN** 后续能取得租约并验证最新 journal 的协调才发布新标题

#### Scenario: Publish a validated observation after the journal changes

- **WHEN** 一次协调读完目标并释放 lease，另一进程在该次 SQLite 发布前完成改名
- **THEN** 已观察到的标题和排序证据可作为本次快照发布，下一次验证目标 journal 的协调取得新标题

### Requirement: Catalog lifecycle, schema, and storage are explicit and private

Catalog SHALL 不设置或校验 PRAGMA user_version，不使用 schemaVersion 或其他固定格式门禁。合法结构中无关的版本元数据 MUST NOT 触发重建；只按完整性、实际表字段和所需索引校验当前结构，不实现历史迁移。

Catalog 的纯内存配置与数据库资源获取 SHALL 分离；打开、创建、重建、提交和关闭 SQLite 必须使用显式生命周期操作。数据库连接、codec、schema 状态、statement 和缓存 MUST 由实例持有，不得使用可变包级 registry、可替换全局 codec、隐藏 goroutine 或进程级内存单例协调多个进程。

默认 Catalog SHALL 位于 `~/.easycode/state.sqlite`，Session 根和 Catalog 路径 MUST 能在测试与宿主装配时分别注入。支持权限位的平台上，父目录 MUST 为当前用户私有且数据库及其 sidecar MUST 使用用户私有权限；打开数据库不得跟随 symlink，文件类型与权限 MUST 根据 SQLite 实际使用的对象校验，单独 `Lstat` 后再按路径打开不满足该契约。无法提供等价保证的平台 SHALL fail closed。

当前 Catalog schema SHALL 用 `title TEXT NOT NULL` 保存已验证标题投影，空字符串表示未命名。派生 schema 缺失、损坏或实际结构不匹配时 SHALL 从 JSONL 重建，包括同名标题列的声明类型或可空性错误。只有完整当前结构的投影可以参与查询。对外错误 SHALL 只包含稳定英文 code 和安全英文 message。

#### Scenario: Pure construction has no external effect

- **WHEN** 调用方只创建 Catalog 配置值
- **THEN** 系统不创建目录或 SQLite 文件、不打开连接且不启动 goroutine

#### Scenario: Reject a symlinked state database

- **WHEN** `state.sqlite` 或其必需 sidecar 被替换为 symlink 或非普通文件
- **THEN** Catalog 在查询或写入前以安全错误失败，且不修改 symlink 目标

#### Scenario: Rebuild an incompatible derived schema

- **WHEN** 现有 SQLite 表或索引结构不符合当前结构，或完整性校验失败
- **THEN** 系统不使用其中的 thread 行作恢复决策，并从 JSONL 构建当前 schema

#### Scenario: Ignore unrelated SQLite version metadata

- **WHEN** 当前表与索引结构完整合法，但 user_version 为任意值
- **THEN** 打开 Catalog 保留已验证投影行，不因该值重建，也不执行历史迁移

#### Scenario: Rebuild a derived table that lacks the title field

- **WHEN** 现有派生表缺少当前标题字段或同名字段不符合要求，且 Session 根存在合法的有名及未命名 journal
- **THEN** 协调从已验证 journal 重建当前结构并恢复标题投影及原有兼容性字段
- **THEN** 各 journal 的 bytes、seq 和 checksum 保持不变

### Requirement: Catalog supports bounded root session discovery independent of provider configuration

Catalog SHALL 提供当前规范化创建目录或全部目录的 root 会话列表查询，不要求 Provider family、wire、model selector；同目录下不同 Provider/model 的会话 SHALL 均参与候选集。列表 MUST 按 committed `updated_at` 降序、canonical `thread_id` 降序排序，先筛选再限制数量；数量范围为 1–200，非法范围或非法目录条件 MUST 在查询前拒绝。查询 SHALL 返回至多指定数量的条目及 `has_more`，仅当当前筛选条件下存在额外可查询条目时为 true。

列表 MUST 只使用已验证的会话元数据，包括已命名条目的显式标题。标题 SHALL 作为返回条目的展示字段。Catalog MUST NOT 为 prompt、response、标题、摘要或 opaque payload 新建搜索索引；当前目录匹配 MUST 使用规范化目录精确相等，不得隐式匹配子目录、同名目录或其他 worktree。现有最近兼容会话查询 SHALL 保留四字段精确筛选语义。

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
