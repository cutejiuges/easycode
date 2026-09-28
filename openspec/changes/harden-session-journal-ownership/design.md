## Context

参见 `proposal.md` 的问题说明。当前 `Repository.Open` 返回普通 `*os.File`，`Loader.Load` 打开、可能截断并关闭它，应用完成 ReplayPlanner、配置校验和 Provider 恢复后，再由 `ReopenJournalWriter` 第二次打开同一路径。`JournalWriter` 只能串行化进入同一实例的请求，不能阻止另一个 Repository 或进程同时加载、repair 和续写。

现有 JSONL v1 已经固定 envelope、checksum、batch、六种 required payload、Provider opaque native commit 和 durable-before-memory 顺序。本次不能改变这些 bytes，也不能把 SQLite、RuntimeEvent 或 Provider 语义引入 Session 所有权机制。Provider restore 是纯内存、无网络的事务式操作，因此可以安全地位于 lease 持有窗口内。

当前 writer 使用容量 64 的请求 channel。`AppendBatch` 在 mutex 内执行可能阻塞的 channel send；当队列已满且 owner 卡在 `Write`/`Sync` 时，Close 无法取得 mutex。另一方面，一旦 durable 请求已被接受，单纯响应调用方 context 并不能安全撤销正在进行的文件写入。

## Goals / Non-Goals

**Goals:**

- 让一个 thread 的所有可变生命周期由同一跨进程 lease 保护，消除 load/repair 与 writer 启动之间的所有权空窗。
- 使用明确的 handle 所有权转移，使每条失败路径都能证明 lease 和文件最终由谁关闭。
- 保留单 owner goroutine 和现有有界 channel 模型，同时移除锁内阻塞并定义 admission、取消、poison 与 Close 的线性化语义。
- 用不可变 v1 fixture 固定已发布 bytes 的读取、回放和原位续写兼容性，而不提前实现没有来源版本的通用迁移框架。
- 通过真实子进程验证操作系统锁语义、异常退出释放和占用期间零修改。

**Non-Goals:**

- 不修改 JSONL envelope、payload revision、checksum、batch 或 Provider native commit 形状。
- 不实现 schema v2、journal 原地改写、备份工具、SQLite、session picker、`--continue`、Tool ledger 或 artifact。
- 不在本 change 处理 Repository 构造器磁盘 I/O、配置文件 symlink TOCTOU、ChatSession/app shutdown reaper、RuntimeEvent/RecordDraft 强类型重构或未使用脚手架；这些分别进入后续 change。
- 不尝试阻止恶意的同用户进程绕过 advisory lock 直接修改文件；对非协作写入继续依靠权限、checksum、seq 和完整回放校验发现损坏。

## Decisions

### 1. 以 journal 文件 handle 本身作为 exclusive lease

`internal/session` 增加平台适配的非阻塞独占锁，并由一个字段不导出的 `JournalLease` 持有 thread ID、`*os.File` 和所有权状态。Repository 只通过“创建并取得所有权”或“打开既有文件并取得所有权”返回 lease；现有直接暴露可写 `*os.File` 的生产入口删除或降为包内测试接缝，避免绕过 lease。

既有 journal 的顺序为：安全定位并打开 handle、校验打开对象的类型和权限、尝试非阻塞锁、成功后才读取 record。锁冲突转换为可用 `errors.Is`/typed predicate 识别的内部 busy error，应用再映射到 `fault.CodeSessionBusy`；核心流程不得用字符串匹配识别冲突。未取得锁的 handle立即关闭。

Unix 类目标使用 `golang.org/x/sys/unix` 的非阻塞 exclusive file lock，Windows 使用 `golang.org/x/sys/windows` 的 `LockFileEx` 非阻塞独占区域锁；`x/sys` 成为直接依赖。平台文件只暴露统一的 `tryLockJournal` 语义，锁冲突与其他系统错误分开分类。当前无法提供可靠实现的平台必须 fail closed 并返回明确不支持错误，不能无锁继续。

lease 通过关闭 handle 释放，不在最终 Close 前单独 unlock。这样进程异常退出也由操作系统回收，不需要持久化 PID、TTL 或 lockfile。独立 Repository 实例和真实子进程都必须在测试中互斥。

未选择旁路 `.lock` 文件，因为它会增加清理、路径和 stale metadata 语义，而且锁住旁路文件不能天然证明当前打开的 journal inode 与所有权一致。未选择进程级 mutex，因为它既是全局可变状态，也无法保护其他进程。

### 2. Loader 借用 lease，writer 接管 lease

Loader 改为纯内存构造，不再持有 Repository。其 mutating load 接口只接受有效 `JournalLease`，从 lease 的 handle seek/read，并在同一 handle 上执行允许的 tail repair；Loader 返回 `LoadResult`，但不关闭或转移 lease。

应用 resume 使用单一所有权链：

```text
Repository acquire existing lease
              |
              v
Loader load/repair same handle
              |
              v
ReplayPlanner + config validation
              |
              v
Provider transactional restore (memory only)
              |
              v
Start writer by transferring the same lease
              |
              v
optional interrupted-tail durable compensation
              |
              v
interactive Runtime -> writer Close -> handle close/release
```

在 writer 启动前，应用函数保留一个明确的 deferred lease Close；任一 replay、配置或 Provider restore 错误都走该路径。writer 启动执行一次所有权转移，成功后原 lease 句柄在逻辑上失效，只能由 writer owner 关闭。writer 启动失败不得消费 lease，调用方仍负责关闭。`ReopenJournalWriter` 被删除，不保留兼容旁路。

新 root thread 使用同一模型：Repository 以 `O_CREATE|O_EXCL` 创建私有文件并取得 lease，writer 接管后 durable 写入唯一 metadata batch，成功后才向应用暴露 Session。新建路径仍同步父目录项；metadata 写入失败会关闭 writer/lease且不返回可交互会话。本 change 不新增自动删除失败空文件的契约，避免把创建清理与 append-only 修复混在一起。

### 3. lease 覆盖完整 resume 决策而不进入 Provider 协议

lease 从读取首个 record 前一直保持到活动 writer 关闭。配置不匹配、非 root thread、损坏、未知 required revision 和 Provider restore 失败只释放 lease，不写补偿记录。只有所有校验与 Provider 事务式恢复成功后，writer 才接管 lease并按既有规则 durable 收口 interrupted tail。

Provider codec、Conversation、ReplayPlan 和 Runtime 不接收 lease 类型；所有权只存在于 Session Repository/Loader/Writer 与应用装配边界。这样 Provider 请求、native history 和 cache fingerprint 不会包含锁、路径、进程或时间状态。

持锁期间执行完整文件读取和 Provider restore 会延长互斥时间，但这是获得一致快照并防止 repair/append 竞态的必要代价。当前恢复本来需要全量 JSONL；后续 SQLite/index 优化只能缩短定位时间，不能绕过 required records 的完整校验。

### 4. busy 是独立、稳定且无副作用的应用错误

`internal/fault` 增加 `CodeSessionBusy = "session_busy"`。应用只在识别到 lease conflict 时使用该错误；权限、损坏、不存在、平台锁失败和普通 I/O 错误继续保持各自现有分类。CLI/TUI 错误不得包含绝对 journal 路径、锁 owner PID 或 payload。

busy 采用 fail-fast，不在 CLI 内轮询等待。交互进程通常会长期持有 writer；隐式等待既无法给出可靠完成时间，也会使 shutdown 和用户取消语义复杂化。未来若需要显式等待，应作为带 context、timeout 和 UI 状态的新契约增加，而不是改变本错误路径。

### 5. 保留 actor channel，但让 admission 永不在锁内阻塞

JournalWriter 保留单 owner goroutine和容量 64 的 requests channel。`AppendBatch` 在锁外完成 draft 校验/编码准备和初始 context 检查；进入 mutex 后只检查 `closing`/`poisoned`，并使用带 `default` 的非阻塞 channel send。发送成功是 admission 线性化点，发送失败立即返回 queue-full 错误，不分配 seq、不写文件，也不 poison writer。

请求一旦 admission 成功，调用方不再以 context 取消等待 response，而是等待 owner 给出确定的 Sync 成功或失败。文件 `Write`/`Sync` 没有可移植、安全的中途撤销；在接收后返回 context error会产生“调用方认为失败但后台可能已提交”的歧义。Runtime 继续在 durable 结果前不启动后续 Provider 副作用。

`Close(ctx)` 总是先在 mutex 内把状态线性化为 closing，并以不会阻塞的方式通知 owner；传入 context 只限制调用方等待 `done` 的时间，即使 context 已取消，owner 仍继续 drain 已接受请求、最终 Sync 和关闭 handle。后续 Close 复用同一个 `done`/`closeErr`。owner 收到 close 后可以安全 drain channel，因为 closing 状态保证不会再有新 admission。

未选择无界 slice/cond 队列，因为它会移除现有背压并扩大内存风险；未选择在锁外直接阻塞发送，因为 Close 与 admission 的先后无法形成单一线性化点。

### 6. v1 fixture 固定读取兼容性，不引入空迁移框架

增加 `internal/session/testdata/migrations/v1/`，至少包含一个覆盖六种 v1 record kind、成功与失败 turn、完整 batch 和 Provider opaque commit 的固定 JSONL，以及独立的期望 replay 摘要。fixture 在实现期间一次性生成并人工审阅后提交；测试运行时只能读取，不得调用当前 encoder 重建或更新它。

测试把 fixture 复制到临时受限数据根，以生产 Loader/ReplayPlanner 加载并比较 identity、metadata、turn 状态、native commit 顺序和 `NextSequence`。继续写入测试先保存原始 prefix，append/Sync 后断言 prefix 逐字节不变且新 seq 连续。另以固定或手工封装的较新 required revision 验证失败前后 bytes 相同。

当前只有 v1，没有真实的来源版本转换，因此不创建 `Migrator`、版本图或通用转换 registry。未来首次升级 schema/payload 时，必须新增旧 fixture到当前内部 replay model 的版本专属 decoder 与 regression；磁盘原始 records 默认保持不变。

### 7. 子进程测试使用确定性握手

跨进程测试通过当前 test binary 的 helper-process 模式启动子进程。子进程取得 lease 后经 pipe/stdout 发出 `ready`，再阻塞等待父进程关闭控制 pipe；父进程只有收到 ready 后才尝试第二次获取。异常退出用进程终止后 `Wait` 作为释放前置条件，不使用 `time.Sleep` 推测锁状态。

占用期间禁止 repair 的用例预先构造 torn tail，让 helper 只取得 lease而不加载；父进程 resume 必须返回 busy，且前后 bytes 相同。helper 退出后，父进程再次加载才允许执行一次 repair。另保留同进程并发 append、fault-injection 与 race 测试，分别验证 actor 顺序和内存同步，不能用它们替代子进程锁测试。

### 8. 文档只声明已经验证的边界

总体架构将“单 writer”明确为跨进程 lease，并修正当前 v1 vocabulary：没有独立 `user_input` record，interrupted tail 使用 `turn_failed(code=session_interrupted)`。Roadmap 区分“v1 compatibility fixture 已建立”与“未来 schema 转换尚未实现”；pitfall-log 使用实际存在的测试路径并记录 advisory lock 的混合版本限制。

## Risks / Trade-offs

- [advisory lock 只能约束协作进程，旧版 EasyCode 不会主动取锁] → 新版测试覆盖所有当前入口并删除绕过 lease 的可写 API；发布说明明确禁止新旧二进制同时写同一 thread，非协作修改继续由严格 Loader 判损。
- [平台锁错误码和同进程语义不同] → 锁操作封装在 build-tag adapter，通过 typed 分类统一 busy；至少覆盖当前 Darwin、Linux 和 Windows 构建，真实运行测试按所在平台执行，unsupported fallback 必须 fail closed。
- [完整恢复期间长期持锁会让第二个进程快速失败] → 这是保持 repair、replay 与续写一致性的必要选择；使用明确 `session_busy`，不隐藏等待。Provider restore 保持纯内存且不得联网。
- [accepted append 在 context 取消后仍可能等待慢磁盘] → admission 后必须取得确定 durable 结果；调用方可通过上层 shutdown timeout停止等待，但 writer owner 继续完成清理，不能把未知提交伪装为取消成功。
- [fail-fast queue-full 会暴露新的瞬时写入错误] → 保留固定容量并在 admission 前拒绝，避免锁阻塞和无界内存；Runtime 将其作为未产生 Session 副作用的 session write failure 处理。
- [lease 所有权转移错误可能导致双 close 或泄漏] → `JournalLease` 使用显式状态和单向 transfer，所有失败分支做 fault-injection/重复 Close 测试，并由 race test 验证。
- [fixture 被当前 encoder 自动重写会掩盖兼容性破坏] → fixture 只读提交，测试不得提供 update 模式；变更 fixture 必须作为显式 schema change 评审。

## Migration Plan

1. 先提交并验证不可变 v1 fixture，建立重构前的兼容基线。
2. 增加平台 lock adapter、busy 分类和 JournalLease，在 Repository 层验证独立实例与子进程互斥。
3. 将 Loader 切换为借用 lease，并在 Session 包测试中删除 load/close/reopen 路径。
4. 将 JournalWriter 切换为接管 lease，完成非阻塞 admission、确定 response 和 Close drain 状态机。
5. 原子调整应用 create/resume 装配与 `session_busy` 映射；不存在同时支持旧、新内部 API 的永久兼容分支。
6. 运行两家 Provider 的 resume/request/fingerprint 回归、Session 子进程与 race 测试，再同步架构、Roadmap 和 pitfall 文档。

本变更不修改磁盘 schema，因此升级后直接读取既有 v1 journal，不执行数据重写。回滚到旧二进制在 bytes 层仍兼容，但旧二进制不会遵守新 lease；回滚或混合版本运行前必须确保没有其他进程持有或写入同一 thread。若实现失败，可回退代码与直接依赖而无需回滚 Session 数据，前提是失败版本没有绕过现有 v1 编码规则写入新 revision。
