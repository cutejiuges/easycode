# session/catalog Specification

## Purpose

定义从 append-only JSONL Session 事实源派生、可安全删除并重建的 SQLite 会话目录，使宿主能够确定性发现和筛选 Session，同时不让索引替代严格回放或 Provider-native history。

## Requirements

### Requirement: SQLite catalog is a rebuildable projection of validated journals

Session Catalog SHALL 将 SQLite 仅作为 JSONL journal 的可重建只读投影；Session 身份、恢复顺序、Provider-native history、turn lifecycle 和 repair 决策仍 MUST 由现有 Loader 与 ReplayPlanner 校验后的结果决定。Catalog MUST NOT 保存 API key、base URL、Authorization、Cookie、敏感 Header、配置文件路径、用户 prompt 正文或 Provider opaque payload，也不得向 JSONL 反向写入索引状态。

删除、缺失或 schema 不兼容的 Catalog SHALL 能从仍受支持的 journal 重建。重建后的可查询 thread 集合、筛选字段和排序键 MUST 与同一组 journal 的 ReplayPlan 投影等价；SQLite 损坏或重建失败不得删除、重写、重新编号或伪造任何 Session record。

#### Scenario: Rebuild after deleting SQLite

- **WHEN** 用户删除 `state.sqlite`，而 Session 根仍包含多个合法 root thread journal
- **THEN** 系统从 JSONL 重建 Catalog，得到与删除前相同的 thread 身份、兼容性字段和确定性排序
- **THEN** journal bytes、seq、checksum 和 Provider-native payload 保持不变

#### Scenario: Reject unsupported required history during projection

- **WHEN** 一个 journal 包含当前程序不支持的 required kind 或 payload revision
- **THEN** Catalog 不把该 journal 投影为可恢复 thread
- **THEN** 系统不展开、不记录也不复制未知 opaque payload

#### Scenario: Preserve transcripts when catalog rebuild fails

- **WHEN** SQLite 无法安全打开、创建、校验或提交重建结果
- **THEN** 系统返回稳定英文 `session_catalog_failed` 错误且不调用 Provider
- **THEN** 全部 Session journal bytes 保持不变，显式 `--resume <thread-id>` 的事实源不变

### Requirement: Catalog metadata and ordering are deterministic

每个可查询 root thread 投影 SHALL 至少包含 `session_id`、`thread_id`、journal 相对定位、`created_at`、最后 committed record 的 `updated_at`、最后 committed `seq` 与 checksum、规范化 `creation_cwd`、Provider family、wire 和 model。所有字段 MUST 从已验证 ReplayPlan 及其 committed records 派生；文件 mtime、ctime、inode、扫描时间和 SQLite 写入时间 MAY 仅作为重扫提示，不得成为用户可见 recency 或兼容性事实。

Catalog SHALL 支持按规范化 `creation_cwd`、Provider family、wire 和 model 精确筛选 root thread。最近排序 MUST 使用 `updated_at` 降序，并以 canonical `thread_id` 降序打破时间相同的平局，使重复查询、重建和不同进程得到相同结果。

#### Scenario: Order by durable record time

- **WHEN** 两个兼容 thread 的文件 mtime 与最后 committed record timestamp 顺序不同
- **THEN** Catalog 按 record `updated_at` 而不是文件 mtime 选择更新的 thread

#### Scenario: Break equal-time ties deterministically

- **WHEN** 两个候选 thread 具有相同 `updated_at`
- **THEN** Catalog 按 canonical `thread_id` 降序返回稳定顺序

#### Scenario: Filter exact compatibility fields

- **WHEN** 同一 `creation_cwd` 下存在不同 Provider family、wire 或 model 的 root Session
- **THEN** 精确筛选只返回四个条件全部匹配的候选

### Requirement: Journal discovery preserves descriptor-bound safety and ownership

Catalog reconciliation SHALL 从已安全打开并校验的 Session 根开始，以 descriptor-relative、no-follow 的方式枚举日期目录与 canonical UUIDv7 journal；目录类型、权限、journal 普通文件类型和权限 MUST 绑定到实际参与枚举与加载的句柄。路径穿越、symlink、非普通文件、非法日期层级或文件名 MUST 在内容读取、repair 或 SQLite 投影前拒绝。

对 journal 执行 Loader 或任何尾部 repair 前，reconciliation MUST 取得该 journal handle 的跨进程 exclusive lease，并在 Loader、ReplayPlanner 和该 journal 投影完成前保持所有权；同一时刻不得持有多个 journal lease。允许的半行或未完成 batch repair SHALL 继续使用现有 truncate、Sync 和 RepairReport 语义，中段损坏不得因 Catalog 重建被删除或跳过后伪装为合法历史。

#### Scenario: Reject a symlink during catalog scan

- **WHEN** 日期目录或 journal 在枚举边界被替换为指向 Session 根外的 symlink
- **THEN** reconciliation 不读取、repair、截断或索引根外目标
- **THEN** 系统失败或继续使用替换前已经安全取得的目录句柄

#### Scenario: Repair a recoverable tail under one lease

- **WHEN** 未被占用的 journal 仅包含 EOF 半行或未完成尾 batch
- **THEN** reconciliation 在同一 exclusive lease 下复用 Loader 截断并 Sync 到最后完整 batch
- **THEN** Catalog 只投影修复后 ReplayPlan 中的 committed records

#### Scenario: Do not inspect an active journal

- **WHEN** 另一个进程持有目标 journal 的 writer lease
- **THEN** reconciliation 不读取、不 repair、不截断该 journal，也不等待超时猜测 owner 状态

### Requirement: Reconciliation never treats stale index rows as Session facts

一次 reconciliation SHALL 先在 SQLite transaction 外完成有界 journal 枚举、逐 journal lease/load/replay 和强类型投影，再用短 transaction 原子应用成功结果。SQLite transaction 内 MUST NOT 执行文件 I/O、等待 lease、等待 goroutine 或调用用户回调。

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

### Requirement: Catalog lifecycle, schema, and storage are explicit and private

Catalog 的纯内存配置与数据库资源获取 SHALL 分离；打开、创建、重建、提交和关闭 SQLite 必须使用显式生命周期操作。数据库连接、codec、schema 状态、statement 和缓存 MUST 由实例持有，不得使用可变包级 registry、可替换全局 codec、隐藏 goroutine 或进程级内存单例协调多个进程。

默认 Catalog SHALL 位于 `~/.easycode/state.sqlite`，Session 根和 Catalog 路径 MUST 能在测试与宿主装配时分别注入。支持权限位的平台上，父目录 MUST 为当前用户私有且数据库及其 sidecar MUST 使用用户私有权限；打开数据库不得跟随 symlink，文件类型与权限 MUST 根据 SQLite 实际使用的对象校验，单独 `Lstat` 后再按路径打开不满足该契约。无法提供等价保证的平台 SHALL fail closed。

Catalog schema SHALL 具有显式版本。当前程序遇到缺失、损坏或不兼容的派生 schema 时 SHALL 从 JSONL 重建，而不是把部分旧表当作有效结果；任何对外错误只包含稳定英文 code 和安全英文 message。

#### Scenario: Pure construction has no external effect

- **WHEN** 调用方只创建 Catalog 配置值
- **THEN** 系统不创建目录或 SQLite 文件、不打开连接且不启动 goroutine

#### Scenario: Reject a symlinked state database

- **WHEN** `state.sqlite` 或其必需 sidecar 被替换为 symlink 或非普通文件
- **THEN** Catalog 在查询或写入前以安全错误失败，且不修改 symlink 目标

#### Scenario: Rebuild an incompatible derived schema

- **WHEN** 现有 SQLite schema version 不是当前程序支持的派生版本
- **THEN** 系统不使用其中的 thread 行作恢复决策，并从 JSONL 构建当前 schema
