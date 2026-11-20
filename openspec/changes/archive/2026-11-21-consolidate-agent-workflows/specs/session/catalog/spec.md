## ADDED Requirements

### Requirement: Catalog snapshot storage is explicit and private

Catalog SHALL 使用唯一当前 JSON 快照格式，只有已验证的强类型元数据可查询。条目 MUST 包含所需字段和规范类型，拒绝未知或重复字段、重复身份、错误排序与不完整快照。标题为空字符串表示未命名。缺失或结构损坏的私有快照 SHALL 从 JSONL 重建。派生格式不使用版本标记，不读取或迁移旧 SQLite。

Catalog 配置的构造只在内存中完成。持有资源的实例显式打开、发布和关闭。默认 Catalog SHALL 位于 `~/.easycode/catalog.json`，Session 根和快照路径可分别注入。父目录 MUST 为当前用户拥有的 `0700` 目录，快照、锁与暂存文件 MUST 为当前用户拥有的 `0600` 普通文件。通过同一已持有目录句柄打开，不跟随 symlink。类型、权限及身份以实际句柄核对。跨进程锁须覆盖加载、重建、协调和发布全过程。无法提供等价保证的平台失败关闭。

发布 SHALL 在同一目录创建独占暂存文件，写入并 Sync，再复核暂存和目标身份，原子替换并 Sync 目录。首次发布不得覆盖已出现的陌生目标。仅目录 Sync 成功才替换实例内可查询的快照。目标安全或身份校验错误 MUST 直接返回，不得按损坏重建或删除不认识的对象。发布前取消或失败 MUST 保留原快照；原子替换后同步失败须返回错误，后续打开重新核验所见状态。

#### Scenario: Pure construction has no external effect

- **WHEN** 调用方只创建 Catalog 配置
- **THEN** 系统不创建目录、快照或锁文件

#### Scenario: Rebuild a missing or invalid private projection

- **WHEN** 快照缺失、截断或不满足当前强类型结构，且合法有名及未命名 journal 仍存在
- **THEN** 本次协调以 JSONL 重建当前快照并恢复标题和筛选字段
- **THEN** journal 的 bytes、seq 和 checksum 保持不变

#### Scenario: Reject a substituted file before reading or publishing

- **WHEN** 快照为 symlink、非私有或非普通文件，或暂存及快照目录项在身份复核时与实际句柄不符
- **THEN** Catalog 失败关闭，不读取不安全对象内容，不发布或清理不认识的对象

#### Scenario: Publish only a complete snapshot

- **WHEN** Catalog 更新快照时遇到取消、Sync 失败或进程崩溃
- **THEN** 发布前保留旧代，之后的打开只从仍有效的完整快照或 JSONL 重建恢复
- **THEN** 无法确认持久化时不报告成功且不改写 journal

### Requirement: Catalog is a rebuildable projection of validated journals

Session Catalog SHALL 将私有快照仅作为 JSONL journal 的可重建只读投影；Session 身份、恢复顺序、Provider-native history、turn lifecycle 和 repair 决策仍 MUST 由现有 Loader 与 ReplayPlanner 校验后的结果决定。Catalog MUST NOT 保存 API key、base URL、Authorization、Cookie、敏感 Header、配置文件路径、用户 prompt 正文或 Provider opaque payload，也不得向 JSONL 反向写入索引状态。

删除、缺失或 schema 不兼容的 Catalog SHALL 能从仍受支持的 journal 重建。重建后的可查询 thread 集合、筛选字段和排序键 MUST 与同一组 journal 的 ReplayPlan 投影等价；快照损坏或重建失败不得删除、重写、重新编号或伪造任何 Session record。

#### Scenario: Rebuild after deleting the snapshot

- **WHEN** 用户删除 `catalog.json`，而 Session 根仍包含多个合法 root thread journal
- **THEN** 系统从 JSONL 重建 Catalog，得到与删除前相同的 thread 身份、兼容性字段和确定性排序
- **THEN** journal bytes、seq、checksum 和 Provider-native payload 保持不变

#### Scenario: Reject unsupported required history during projection

- **WHEN** 一个 journal 包含当前程序不支持的 record kind 或 payload 字段结构
- **THEN** Catalog 不把该 journal 投影为可恢复 thread
- **THEN** 系统不展开、不记录也不复制未知 opaque payload

#### Scenario: Preserve transcripts when catalog rebuild fails

- **WHEN** 快照无法安全打开、创建、校验或提交重建结果
- **THEN** 系统返回稳定英文 `session_catalog_failed` 错误且不调用 Provider
- **THEN** 全部 Session journal bytes 保持不变，显式 `--resume <thread-id>` 的事实源不变

## MODIFIED Requirements

### Requirement: Catalog metadata and ordering are deterministic

每个可查询 root thread 投影 SHALL 至少包含 `session_id`、`thread_id`、journal 相对定位、`created_at`、最后 committed record 的 `updated_at`、最后 committed `seq` 与 checksum、规范化 `creation_cwd`、Provider family、wire 和 model。所有字段 MUST 从已验证 ReplayPlan 及其 committed records 派生；文件 mtime、ctime、inode、扫描时间和 快照 写入时间 MAY 仅作为重扫提示，不得成为用户可见 recency 或兼容性事实。

Catalog SHALL 支持按规范化 `creation_cwd`、Provider family、wire 和 model 精确筛选 root thread。最近排序 MUST 使用 `updated_at` 降序，并以 canonical `thread_id` 降序打破时间相同的平局，使重复查询、重建和不同进程得到相同结果。

当前标题 SHALL 来自该 root journal 已验证的最后一条标题事实。没有标题事实的会话 SHALL 以未命名状态投影。标题事实成为 journal 最后一条 committed record 时，Catalog SHALL 以它的时间、seq 和 checksum 更新对应字段，继续用同一排序规则参与列表及最近兼容会话查询。重命名成功 SHALL 只以 journal 的持久提交为依据。Catalog 在后续协调取得 lease 并完成验证时更新标题。busy journal 的已索引标题 SHALL 与其他字段一同保留最后一次已验证的值。

一次协调发布的是各 journal 在各自 lease 下验证的快照。某个 journal 在释放 lease 后、快照发布前更新时，该轮查询仍使用本次已验证值。列表与最近兼容会话查询 SHALL 使用该快照的排序；下一次在新事实提交后获取该 journal lease 的成功协调 SHALL 看到更新。预览与实际恢复继续按 thread ID 独立校验 journal。

最后提交事实 SHALL 以 journal seq 确定，`updated_at` 使用该记录的实际 UTC 时间。系统墙钟回拨时，`updated_at` 可以早于 `created_at`，只按正常排序规则处理，不能因此排除可重放会话或改写 journal。

#### Scenario: Discover the same valid session after a wall clock rollback

- **WHEN** 时钟回拨后成功追加一条记录，最后记录时间早于会话创建时间
- **THEN** Catalog 继续投影该会话与最新 seq、标题和实际 UTC 时间，重建及再打开后仍可查询

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

- **WHEN** 一次协调读完目标并释放 lease，另一进程在该次 快照发布前完成改名
- **THEN** 已观察到的标题和排序证据可作为本次快照发布，下一次验证目标 journal 的协调取得新标题

### Requirement: Journal discovery preserves descriptor-bound safety and ownership

Catalog reconciliation SHALL 从已安全打开并校验的 Session 根开始，以 descriptor-relative、no-follow 的方式枚举日期目录与 canonical UUIDv7 journal；目录类型、权限、journal 普通文件类型和权限 MUST 绑定到实际参与枚举与加载的句柄。路径穿越、symlink、非普通文件、非法日期层级或文件名 MUST 在内容读取、repair 或 索引投影前拒绝。

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

### Requirement: Reconciliation never treats stale index rows as Session facts

一次 reconciliation SHALL 先完成有界 journal 枚举、逐 journal lease/load/replay 和强类型投影，再在 Catalog 锁下原子发布完整快照。发布期间 MUST NOT 获取 journal lease 或等候用户操作。

完整枚举证明 journal 已不存在时，Catalog SHALL 删除对应陈旧行；journal 仍存在但当前 busy 时 SHALL 保留已有行而不得用未验证的文件属性覆盖；journal 存在但无法通过完整校验时 MUST NOT 将旧行继续返回为可恢复候选。只有已完整校验的投影可以新增或更新可查询行。

#### Scenario: Remove a row for a deleted journal

- **WHEN** 一次完整安全枚举确认已索引 journal 不再存在
- **THEN** reconciliation 在同一 Catalog commit 中删除对应 thread 行

#### Scenario: Preserve an indexed busy row

- **WHEN** 已索引 journal 正由另一个进程持有且本次无法取得 lease
- **THEN** reconciliation 保留其最后一次已验证投影，不使用当前 mtime 或 size 修改业务字段

#### Scenario: Exclude a newly corrupted journal

- **WHEN** journal 仍存在但本次 Loader 或 ReplayPlanner 报告中段损坏或语义损坏
- **THEN** reconciliation 不把其旧投影作为可恢复候选返回
- **THEN** 原 journal 保持符合现有 repair 契约，不因索引协调而被删除或重写

### Requirement: Catalog supports bounded root session discovery independent of provider configuration

Catalog SHALL 提供当前规范化创建目录或全部目录的 root 会话列表查询，不要求 Provider family、wire、model selector；同目录下不同 Provider/model 的会话 SHALL 均参与候选集。列表 MUST 按 committed `updated_at` 降序、canonical `thread_id` 降序排序，先按范围及调用方提供的可选源 thread 排除条件筛选，再限制数量；数量范围为 1–200，非法范围或非法目录条件 MUST 在查询前拒绝。查询 SHALL 返回至多指定数量的条目及 `has_more`，仅当当前筛选条件下存在额外可查询条目时为 true。

列表 MUST 只使用已验证的会话元数据，包括已命名条目的显式标题。标题 SHALL 作为返回条目的展示字段。Catalog MUST NOT 为 prompt、response、标题、摘要或 opaque payload 新建搜索索引；当前目录匹配 MUST 使用规范化目录精确相等，不得隐式匹配子目录、同名目录或其他 worktree。现有最近兼容会话查询 SHALL 保留四字段精确筛选语义。

#### Scenario: Discover different provider sessions in one directory

- **WHEN** 同一目录存在 OpenAI 与 Anthropic、不同模型的合法 root 会话
- **THEN** 当前目录列表按统一更新时间顺序返回这些会话，不依赖当前 Provider 配置
- **THEN** 最近兼容会话查询仍只返回其精确 selector 对应的会话

#### Scenario: Apply directory filter before limit

- **WHEN** 其他目录拥有更新的会话，而当前目录查询限制为 1
- **THEN** 返回当前目录最新条目，其他目录不占用返回数量
- **THEN** 子目录、同名前缀目录和兄弟 worktree 不被隐式纳入

#### Scenario: Exclude the source thread before limiting candidates

- **WHEN** 恢复选择器从当前会话发起查询，当前会话位于上限内或恰好在上限之后
- **THEN** 仅合格目标计入 1–200 条上限和 overflow，无须为当前会话保留候选位置

#### Scenario: Return exact overflow information

- **WHEN** 筛选后分别存在 N 或 N+1 个可查询条目，调用方请求 N 个
- **THEN** 两次查询均最多返回 N 个，前者 `has_more` 为 false，后者为 true
- **THEN** 时间相同的条目按 canonical thread ID 降序排列，重建快照 后结果等价

#### Scenario: Reject invalid query bounds

- **WHEN** 数量为 0、负数、超过 200，或当前目录条件不是规范化绝对目录
- **THEN** 查询失败，不返回部分条目，不执行写入或 journal 加载

## REMOVED Requirements

### Requirement: SQLite catalog is a rebuildable projection of validated journals

**Reason**: Catalog 改为私有原子快照，恢复继续以 JSONL 为准。
**Migration**: 由现有 journal 重建当前派生快照。

### Requirement: Catalog lifecycle, schema, and storage are explicit and private

**Reason**: SQLite schema 与 sidecar 从当前 Catalog 移除。
**Migration**: 使用新快照的实际文件和结构校验。
