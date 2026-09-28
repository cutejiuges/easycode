## Why

当前 Session writer 只在单个 `JournalWriter` 实例内串行化请求，`Loader.Load` 与 `ReopenJournalWriter` 之间还会关闭并重新打开 journal。两个 EasyCode 进程可以因此同时从相同 `NextSequence` 续写，或在另一个 writer 活跃时执行尾部截断，破坏 JSONL 事实源的单调顺序与 append-only 语义。

同时，首版 v1 schema 尚无不可变兼容 fixture，writer 在持锁状态下可能阻塞发送请求；这些缺口使当前测试无法证明跨进程恢复、未来版本演进和关闭竞态满足已经承诺的 Session 契约。

## What Changes

- 为每个活动 thread 建立跨进程 exclusive journal lease；lease 在 load/repair 前获得，并持续覆盖 replay、Provider 事务式恢复、interrupted-tail 补偿和 writer 关闭。
- 将 load 与 writer 启动改为共享同一已锁定 journal handle，移除 `load -> close -> reopen` 的无锁窗口；占用冲突以稳定英文错误码 `session_busy` 快速失败，且不得读取、repair 或修改 journal。
- 明确新建 thread 也必须在首个 metadata batch 前取得所有权；创建失败或 lease 失败不得暴露可交互 Session。
- 重构 writer admission/close 状态机，使锁内只执行有界内存状态转换；明确请求接收前取消、接收后确定完成、队列背压、poison 与 drain/close 语义。
- 增加仓库内不可变 v1 compatibility fixture，验证既有 bytes 可被当前 Loader/ReplayPlanner 恢复、可继续追加且原始 records 不被重写；为后续 schema/payload revision 固定 migration fixture 规则。
- 增加真实子进程的 lease 竞争、崩溃释放、占用期间禁止 repair、续写序号与 bytes 不变回归，并保持双 Provider 恢复前后 request bytes/fingerprint 等价。
- 同步架构、Roadmap 与踩坑文档中的 record vocabulary、单 writer 范围、migration 状态和实际测试路径。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `session/jsonl-store`: 将单 writer 明确为跨进程 journal 所有权，约束 lease 覆盖范围、writer admission/关闭线性化以及每个版本的不可变兼容 fixture。
- `session/resume`: 要求 resume 在任何读取、修复或 Provider 恢复前独占目标 thread；并发占用必须以 `session_busy` 安全失败且不修改事实源。

## Impact

- 主要影响 `internal/session` 的 Repository、Loader、lifecycle 和 JournalWriter 边界，以及 `internal/app/session_service.go` 的 create/resume 装配顺序。
- `internal/fault` 新增稳定的 `session_busy` 错误码；CLI/TUI 继续只展示安全英文摘要，不暴露 journal 内容或路径。
- 需要平台锁适配层；优先使用操作系统随 handle/进程退出自动释放的 advisory file lock，预计将 `golang.org/x/sys` 提升为直接依赖，不引入 PID lockfile 或进程级全局 mutex。
- JSONL envelope、现有 v1 record bytes、Provider native commit payload 和 cache fingerprint 不变；SQLite、Tool ledger、schema v2、Runtime shutdown 与协议强类型重构不在本 change 范围内。
