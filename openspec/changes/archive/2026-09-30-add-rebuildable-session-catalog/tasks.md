## 1. Catalog 边界与数据路径

- [x] 1.1 在 `internal/session/catalog` 定义强类型 `Config`、`Entry`、`Selector`、reconciliation report 与纯内存 Projector，保持无 `any`、无包级可变状态，并用单测验证非法 identity、时间、相对路径和非 root ReplayPlan 被拒绝。
- [x] 1.2 在 app 中引入默认 `~/.easycode/sessions`/`~/.easycode/state.sqlite` 的内部 data paths，并允许测试分别注入 Session 根与 Catalog path；验证仅解析配置或运行普通新会话/显式 resume 不会创建 SQLite。
- [x] 1.3 增加稳定英文 `session_catalog_failed` fault code 和安全摘要路径，并用错误测试验证路径、secret、SQLite 原始错误和 opaque payload 不会进入对外 message。
- [x] 1.4 更新架构守卫以允许 `internal/session/catalog -> internal/session|domain` 且继续禁止其依赖 runtime/app/TUI/headless/具体 Provider，并运行对应 architecture tests。

## 2. 安全 journal 枚举

- [x] 2.1 为 Unix secure root 实现固定 `YYYY/MM/DD/<uuidv7>.jsonl` 深度的 descriptor-relative、no-follow 枚举，返回排序后的 thread identity/相对定位，并用 Repository 单测验证空根、多日期和确定顺序。
- [x] 2.2 让枚举忽略非候选普通项、拒绝候选层级中的 symlink/类型/权限/日期与 UUIDv7 不一致问题，并用确定性替换 hook 覆盖目录和 journal TOCTOU，证明数据根外 bytes 未被读取或修改。
- [x] 2.3 为尚无等价 secure walker 的平台提供明确 fail-closed 实现，并验证所有目标可编译且不会回退到 `filepath.WalkDir`、`Lstat` 后打开或字符串 containment。

## 3. SQLite 安全 adapter 与 schema v1

- [x] 3.1 引入并锁定纯 Go、无 CGO SQLite driver，通过独立 adapter 配置 rollback journal、短 transaction 和 context-aware busy handling；用最小集成测试验证 build、open、commit、rollback 与幂等 Close。
- [x] 3.2 实现私有 data home、`state.sqlite.lock` 实际 handle 与跨进程 advisory lock，确保 schema/rebuild/reconciliation 有唯一 owner；用真实子进程测试验证竞争、取消、正常关闭和进程崩溃后的锁释放。
- [x] 3.3 通过 driver flags 或受控 VFS 对数据库、rollback journal 和必需 sidecar 实施 no-follow、普通文件和 `0600` 校验，并在 macOS/Linux 使用确定性 symlink/替换测试证明目标不会被跟随；driver 无法满足时实现 fail closed 而不是弱化测试。
- [x] 3.4 实现 `PRAGMA user_version=1` 的最小 `threads` schema 与 continue 组合索引，验证 schema 不含 title/tag/prompt/native payload 等延期字段，且数据库扫描不发现测试 secret。
- [x] 3.5 提交不可变的损坏数据库和不兼容 schema fixture，实现 Catalog 锁下的临时数据库重建、Sync 与原子替换，并验证缺失/损坏/不兼容数据库均从 JSONL 恢复相同 rows，失败时 journal bytes 不变。
- [x] 3.6 实现强类型 upsert/delete/latest-compatible 查询与严格 row decoding，用集成测试验证 cwd/family/wire/model 精确过滤、`updated_at_ns DESC, thread_id DESC` 排序、事务回滚和无候选结果。

## 4. ReplayPlan 投影与 reconciliation

- [x] 4.1 将 Loader 的 committed records 与 ReplayPlan 投影为 Catalog Entry，使用最后 record timestamp/seq/checksum 而非 mtime，并用单测覆盖 mtime 冲突、同 timestamp 平局、failed turn、interrupted tail 和两种 Provider metadata。
- [x] 4.2 实现 `Repository.Open -> Loader -> ReplayPlanner -> Project -> lease.Close` 的逐 journal协调，确保任一时刻只持有一个 lease，且 SQLite transaction 外完成全部文件 I/O；用故障注入验证 lease 和 handle 在每条失败路径释放。
- [x] 4.3 实现 `valid`、`busy`、`invalid`、`present` 与 `busy_unindexed` reconciliation 集合及单次短 transaction，验证新增/更新、真正缺失行删除、busy 行保留、invalid 行不再可查询和 transaction 失败原子回滚。
- [x] 4.4 覆盖 Loader 允许尾修复和中段损坏：验证 repair 只在 exclusive lease 下 truncate+Sync、Catalog 仅使用修复后 committed records，而 busy 或中段损坏路径 journal bytes 不变。
- [x] 4.5 增加真实子进程回归，验证活动 writer 期间 Catalog 不读取或 repair journal、已有 busy row 保留、未索引 busy thread 阻止自动选择，以及 owner 退出后下一次 reconciliation 能正常投影。

## 5. `--continue` 与宿主装配

- [x] 5.1 在 CLI 增加 `--continue`、帮助文本和 `--resume` 互斥校验，确保冲突在配置、prompt 外部副作用、Catalog、Session 和 Provider 前以退出码 `2` 失败，并更新 `cmd/easycode` 测试。
- [x] 5.2 在 app 中从当前配置纯函数取得 family/wire/model，使用 `NormalizeCreationCWD` 规则构造 Selector，按“Open Catalog -> reconcile -> latest-compatible -> close Catalog”的顺序解析 thread ID；验证无候选为 `session_not_found`、基础设施错误为 `session_catalog_failed`、未索引 busy 为 `session_busy`。
- [x] 5.3 将自动选择出的 ID 交给现有 `sessionService.resume`，不得转交 Catalog lease或信任 row 代替校验；用竞态 hook 验证选择后 journal 被替换、损坏、改为不兼容或被占用时安全失败且不回退旧 thread。
- [x] 5.4 让资源记录实际 `resumed` 状态，使交互、`--print --continue` 和 `--json --continue` 共享恢复路径；验证文本只输出本次回答，JSON 首事件为原 identity 且 `resumed=true`，启动失败不伪造 thread/turn。

## 6. 端到端与回归验证

- [x] 6.1 增加 app/cmd 端到端矩阵，覆盖多个 cwd、family、wire、model、updated_at 与 thread ID 平局，验证只选择当前 cwd 中最近的完全兼容 root Session，普通启动仍创建新 Session。
- [x] 6.2 对 OpenAI Responses 与 Anthropic Messages 分别比较 uninterrupted、显式 `--resume` 和 `--continue` 的下一请求 canonical bytes、native item 顺序与 cache fingerprint，验证 Catalog metadata、路径和 timestamp 不进入 Provider request。
- [x] 6.3 验证删除 `state.sqlite` 后 session 列表 metadata 与选择结果可重建，SQLite/锁/sidecar 不包含 API key、Authorization、base URL、prompt 或 Provider response 正文，且显式 resume 仍只依赖 JSONL。
- [x] 6.4 增加代表性多 journal reconciliation benchmark 或受控性能测试，记录全量前台扫描的时间与分配基线，确认无 goroutine 泄漏、无同时多 lease 和无 SQLite transaction 内文件 I/O。

## 7. 文档与质量门

- [x] 7.1 更新 `docs/architecture/overall-architecture.md`，记录 Catalog 包边界、schema v1、前台 reconciliation、Catalog 锁、descriptor-safe SQLite opener 和选择后重新 resume 的数据流，并核对实际文件/类型名称。
- [x] 7.2 更新 `docs/roadmap/product-roadmap.md` 与 `docs/roadmap/pitfall-log.md`，准确标记 SQLite Catalog 与 `--continue` 已交付、session picker/worktree/search/实时索引仍延期，并记录 mtime、字符串 head/tail 与 DB-as-truth 的规避理由。
- [x] 7.3 运行 `go mod tidy`、gofmt、目标包测试、跨进程测试、fixture 回归和 `openspec validate add-rebuildable-session-catalog --strict`，修复所有失败且不删除或弱化断言。
- [x] 7.4 运行完整 `make verify`，确认 go vet、Staticcheck、架构边界、全量测试与 race test 全部通过，并在交付说明中列出任何无法执行的真实平台 SQLite/VFS 检查及风险。
