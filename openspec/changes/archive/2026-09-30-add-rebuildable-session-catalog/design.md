## Context

参见 [proposal.md](./proposal.md) 的动机。当前 `internal/session` 已提供 UUIDv7 日期定位、descriptor-relative secure path walker、exclusive journal lease、会修复允许尾损坏的 Loader，以及产出强类型 `ReplayPlan` 的 ReplayPlanner；`internal/app/session_service.go` 已能在一个连续 lease 下显式恢复 root thread。CLI 目前只有 `--resume <thread-id>`，`Options.SessionDataRoot` 只描述 `~/.easycode/sessions`，仓库没有 SQLite 依赖或会话枚举 API。

设计同时受以下边界约束：JSONL 必须继续是唯一恢复事实源；SQLite 文件与 sidecar 也必须满足实际打开对象上的 no-follow、类型和权限校验；Catalog 不得进入 Runtime 或 Provider；不能用包级数据库、registry、隐藏 goroutine或后台 best-effort 写入掩盖失败。

参考工程提供了两个互补信号。Claude Code 的当前目录 `--continue`、稳定最近排序和先筛选后加载语义适合作为产品行为；Codex 的 rollout 事实源、SQLite 快照、文件回源和 read-repair 适合作为数据边界。EasyCode 不采用 Claude Code 的 mtime 事实与字符串 head/tail 解析，也不引入 Codex 当前包含 remote、archive、sections、project 和 spawn graph 的大型 state runtime。

## Goals / Non-Goals

**Goals:**

- 以不改变 JSONL record vocabulary、Provider native codec 或 Runtime 提交协议的方式建立最小 SQLite Catalog。
- 让 Catalog 删除、损坏或 schema 不兼容时可以通过现有 Loader/ReplayPlanner 重建。
- 让 `--continue` 只负责确定 thread ID，随后完整复用显式 resume 的安全与恢复路径。
- 保持所有外部资源具有显式、可注入、可测试的生命周期，并使跨进程竞争有确定结果。
- 为未来 picker 保留小而稳定的查询边界，但不提前实现没有消费者的搜索或展示字段。

**Non-Goals:**

- 不实现 TUI picker、预览、标题、tag、全文搜索、archive、Git repository/worktree 身份或 project 表。
- 不实现 parent-child graph、fork、subagent、远程 Session 或跨 Provider/model 恢复。
- 不为实时索引增加 writer observer、后台 worker、文件监听或进程间通知。
- 不让 Catalog 保存 prompt、SemanticHistoryView、Provider opaque payload 或任何连接 secret。
- 不承诺首版在超大 Session 根上的常量时间启动；正确性与可重建性优先。

## Decisions

### 1. 新增 `internal/session/catalog`，由 app 单向装配

新增独立子包 `internal/session/catalog`，它可以依赖 `internal/session` 的公开强类型结果和 `internal/domain`，持有 SQLite adapter、schema 与查询逻辑。`internal/session` 本体只增加安全枚举 journal identity 的能力，不反向依赖 Catalog；`internal/app` 同时装配 Repository、Catalog 和现有 `sessionService`。

Catalog 不进入 `internal/runtime`、Provider、TUI 或 headless facade。这样 Runtime 仍只提交 Session 事实，TUI/headless 仍只消费宿主 facade，SQLite 失败不会成为 Provider-native history 的一部分。

备选方案是把 SQLite 直接加入 `session.Repository`。拒绝该方案，因为 Repository 当前职责是 journal 路径、handle 与 lease 所有权；查询投影具有不同的事实性、生命周期和故障语义，会扩大核心类型并诱导 resume 直接信任数据库。

### 2. Repository 提供固定深度、descriptor-relative 的 identity 枚举

Repository 增加一个 context-aware 枚举操作，只返回验证通过的 `domain.ThreadID` 与规范化 journal 相对定位，不返回任意绝对路径或已经读取的 JSON。实现从已打开 secure root 的目录 handle 出发，按 `YYYY/MM/DD/<uuidv7>.jsonl` 固定深度读取并排序；每一级只打开匹配的目录项且不跟随 symlink，最终文件仍由现有 `Repository.Open` 取得实际 handle、校验权限并竞争 lease。

非匹配的普通目录项不作为 Session 候选且不被打开；匹配层级中的 symlink、非目录/非普通文件或 identity 与日期不一致作为安全扫描问题报告。枚举结果排序后再逐一处理，reconciliation 任一时刻只持有一个 journal lease。

备选方案是 `filepath.WalkDir` 加 `Lstat`。它不能把检查绑定到后续加载使用的对象，也无法满足当前 Session 的 TOCTOU 契约，因此拒绝。

### 3. Catalog projector 只接受完整 ReplayPlan 与 committed records

每个未占用 journal 的协调流程固定为：`Repository.Open -> Loader.Load -> ReplayPlanner.Plan -> Project -> lease.Close`。Projector 接收 `ReplayPlan` 和 Loader 已验证的 committed records，生成不可变 `Entry`；不自行解析 JSON、不读取 Provider payload 内容，也不从文件名、mtime 或不完整首行猜测业务字段。

`updated_at` 取最后一条 committed record 的 UTC timestamp，`last_sequence` 和 `last_checksum` 取同一记录；`created_at`、cwd 和 Provider 三元组来自 `SessionMetaPayload`。当前只接受 `ThreadMetaPayload.Root=true` 的根 thread。Loader 如发现允许修复的尾部损坏，会继续在该独占 lease 下 truncate、Sync，并由 Projector 使用修复后的完整结果。

备选方案是像 Claude Code 一样只读 head/tail 并通过字符串提取字段。它不能证明 batch、checksum、required revision 或 lifecycle 合法，还会形成第二套解析规则，因此拒绝。

### 4. 首版每次 `--continue` 前台协调，不引入实时索引写入

`--continue` 启动时同步执行一次完整枚举和 reconciliation，然后从已提交 Catalog 查询候选。首版不在 Session writer 上添加 observer，也不在正常 create/append/close 路径异步更新 SQLite。这样不会让一次已 durable 的 JSONL append 因派生索引失败而被改判为 turn 失败，也无需新增 goroutine owner、重试队列或 shutdown 协议。

Reconciliation 先在 SQLite transaction 外构建以下集合：

- `valid`: 已取得 lease 并完成 load/replay 的强类型 Entry。
- `busy`: journal 存在但 lease 被占用；保留已有行。
- `invalid`: journal 存在但完整校验失败；删除或屏蔽旧行，使其不再成为候选。
- `present`: 安全枚举发现的全部 canonical thread ID，用于删除真正缺失的旧行。
- `busy_unindexed`: busy 且没有既有投影的 thread；本次自动选择返回 `session_busy`，因为无法在不读活动 journal 的前提下证明其 cwd、兼容性和顺序。

随后在一个短 transaction 中 upsert `valid`、保留 `busy`、排除 `invalid`，并删除不在 `present` 中的行。transaction 中不执行文件 I/O 或等待 lease。提交失败返回 `session_catalog_failed`，但不修改 journal。

备选方案是先查 DB、只有 miss 才扫描。由于当前版本没有实时维护索引，它会漏掉后来创建的 journal并选择旧会话；等未来 picker 引入可靠增量维护后，可以在不改变 Catalog 事实边界的情况下增加快路径。

### 5. SQLite v1 schema 只保存当前选择所需字段

使用显式 `PRAGMA user_version = 1`，首版表结构为：

```sql
CREATE TABLE threads (
    thread_id       TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    journal_path    TEXT NOT NULL UNIQUE,
    created_at_ns   INTEGER NOT NULL,
    updated_at_ns   INTEGER NOT NULL,
    last_sequence   INTEGER NOT NULL,
    last_checksum   TEXT NOT NULL,
    creation_cwd    TEXT NOT NULL,
    provider_family TEXT NOT NULL,
    provider_wire   TEXT NOT NULL,
    model            TEXT NOT NULL
);

CREATE INDEX threads_continue_lookup
ON threads (
    creation_cwd,
    provider_family,
    provider_wire,
    model,
    updated_at_ns DESC,
    thread_id DESC
);
```

`journal_path` 是相对于 Session 根的 canonical 路径，不保存绝对数据根。时间以经过范围校验的 Unix 纳秒保存，查询返回后重新构造 UTC time。schema 不包含 `title`、`tag`、preview、token、archive、parent、project 或扫描 hint；未来真实消费者出现时再通过新的 schema version 增加。

缺失数据库创建 v1；`quick_check`/schema 校验失败或 `user_version` 不兼容时，在 Catalog 独占锁下构建临时 v1 数据库、完成重建和 Sync 后原子替换派生文件。旧数据库不能部分读取或原地猜测迁移。由于 SQLite 可重建，不为 v1 之前不存在的 schema伪造 migration fixture；测试保存损坏和不兼容数据库 fixture，证明当前重建路径。

### 6. SQLite 使用私有 data home、跨进程锁和受控 opener/VFS

默认布局为 `~/.easycode/sessions` 与 `~/.easycode/state.sqlite`。app 引入内部 `dataPaths` 值，并允许测试分别注入 Session 根和 Catalog path；普通不使用 Catalog 的启动不应仅因配置值解析就创建数据库。

Catalog 通过 `Open` 获取资源，通过幂等 `Close` 释放。使用同一私有 data home 下的 `state.sqlite.lock` 实际文件 handle 与 OS advisory lock 串行化 schema 初始化、损坏替换和 reconciliation；它不是 PID 文件，不通过删除或超时猜测 owner。SQLite 自身使用短 transaction 和有界、context-aware busy 等待处理连接级竞争。

SQLite adapter 必须使用纯 Go、无 CGO 的 driver，并通过 driver open flags 或受控 VFS 对数据库、rollback journal 和其他必需 sidecar 强制 no-follow，在实际使用对象上校验普通文件与 `0600`；父目录为 `0700`。单独 `Lstat` 后再把字符串路径交给普通 opener 不合格。应用阶段先用确定性 symlink/替换测试验证候选 driver；不能满足时必须 fail closed，不能降级安全契约。低写入量首版使用 rollback journal 和短事务，不启用需要额外长期 sidecar 生命周期的 WAL。

备选方案是内存 SQLite 每次重建或自制二进制索引。前者不交付 Roadmap 要求的持久索引，后者重复实现事务、并发与查询能力，均拒绝。

### 7. `--continue` 解析为 thread ID 后复用现有 resume

CLI 增加布尔 `--continue`，局部变量使用非关键字名称；在配置加载、prompt 读取以外的外部资源操作前检查其与 `--resume` 互斥。app 在 headless prompt 验证成功后加载当前配置，使用与 `NormalizeCreationCWD` 相同的逻辑绝对路径规则构造 selector，并从配置纯函数取得 family/wire/model，不必先创建 Provider conversation。

选择查询固定 `LIMIT 1 ORDER BY updated_at_ns DESC, thread_id DESC`，精确匹配 cwd、family、wire、model。无候选返回现有 `session_not_found`；Catalog 基础设施失败使用新增 `session_catalog_failed`；选择或恢复竞争使用现有 `session_busy`。选出 ID 后关闭 Catalog reconciliation 所持资源，再调用现有 `sessionService.resume`，由它重新取得目标 lease 并完成全部校验。不得把 Catalog 打开的 journal lease转交给 writer，因为选择与恢复之间必须以现有 resume 边界重新确认目标。

`chatResources` 保存实际是否恢复的结果，而不是通过 `Options.ResumeThreadID != ""` 推断；因此显式和自动恢复在 headless JSON 中都产生 `thread.started.resumed=true`，且旧 transcript 不重新输出。

### 8. 测试按事实源、安全边界与用户行为分层

- Catalog 纯逻辑测试覆盖 ReplayPlan 到 Entry、精确筛选、record timestamp 排序和 thread ID 平局。
- SQLite 集成测试覆盖 schema、事务回滚、删除/损坏/不兼容数据库重建、陈旧行删除、invalid 屏蔽和 secret 排除。
- Repository 测试使用确定性 hook 覆盖枚举中的 symlink/目录替换、非法层级、权限与类型；不得依赖概率竞态。
- 真实子进程测试覆盖 Catalog 锁竞争、journal lease busy、崩溃释放、busy 期间不读不 repair、未索引 busy 阻止自动选择，以及失败路径 journal bytes 不变。
- app/cmd 测试覆盖互斥参数、无候选、cwd/provider 筛选、交互与 headless continue、JSON resumed 标志和无替代 Session。
- 双 Provider e2e 回归比较 `--continue` 与显式 resume 后的 canonical request bytes、native item 顺序和 cache fingerprint，证明 Catalog 未参与请求构造。

## Risks / Trade-offs

- [每次 `--continue` 全量解析 journal，Session 很多时启动变慢] -> 首版优先正确性并记录基准；枚举排序、逐个释放 lease、短事务控制资源，未来只有在引入可靠实时维护后才增加 DB-first 快路径。
- [Catalog reconciliation 会触发现有 Loader 的允许尾修复，超出用户对“索引”的直觉] -> 只在 exclusive lease 下复用已有 repair 契约，保留 RepairReport，并用回归证明只截断 EOF 半行/未完成 batch。
- [SQLite driver/VFS 的 no-follow 与 sidecar 行为具有平台差异] -> adapter 封装 driver，macOS/Linux 分别做真实替换测试；任一目标无法证明时 fail closed，Windows 继续遵循当前 P8 平台策略。
- [多个进程同时首次重建会争用 Catalog 锁] -> 使用 OS handle lock 与 context-aware 有界等待，锁内不持有 journal lease，不使用 PID 清理或无限重试。
- [未索引 busy journal 可能与当前 selector 无关，却阻止 `--continue`] -> 这是不读取活动 journal前提下避免错误续写的保守选择；显式 `--resume` 仍可给出精确目标，未来实时索引可消除该限制。
- [record timestamp 受系统时钟回拨影响] -> 它仍是 durable Session 事实并优于可外部触碰的 mtime；thread ID 平局规则保证相同事实集的结果确定。

## Migration Plan

1. 引入 Catalog v1、私有 data path 与 fault code，但保持普通新 Session 和显式 `--resume` 不打开 SQLite。
2. 首次 `--continue` 在 Catalog 锁下创建或校验 `state.sqlite`，从既有 JSONL 完整 reconciliation 后再选择。
3. 现有 JSONL 无需迁移或重写；旧版本忽略 `state.sqlite`，回滚时可删除派生数据库而不影响显式 resume。
4. 后续 Catalog schema 不兼容时继续采用重建而非原地改写 Session；只有出现不可重建的 SQLite-owned 用户数据后，才需要另行 OpenSpec 定义真正 migration。

