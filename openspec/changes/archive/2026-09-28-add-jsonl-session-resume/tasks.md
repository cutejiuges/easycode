## 1. Session envelope 与安全存储基础

- [x] 1.1 在 `internal/domain` 增加 UUIDv7 Session/Thread/Turn ID 的生成、canonical 校验和时间提取边界，覆盖无效格式、跨日和异常时间 fixture，并运行 `go test ./internal/domain` 验证。
- [x] 1.2 重构 `internal/session` 的占位 `Record`/`Store` 为含 `replay_requirement` 的 v1 envelope、record draft 和首版六种 required 强类型 payload，加入声明 kind/revision/requirement/cardinality/placement 的 registry、16 MiB 行/64 MiB batch 限制及确定性 SHA-256 checksum；以 canonical golden、checksum 篡改、registry 声明漂移和未知 required/optional revision 单测验证。
- [x] 1.3 实现受限数据根下的 Session `Repository`，按 thread UUIDv7 定位 `YYYY/MM/DD/<thread-id>.jsonl`，创建 `0700` 目录和 `0600` 文件并拒绝 traversal、symlink、非普通文件及宽松既有权限；运行 `go test ./internal/session` 验证路径与权限用例。
- [x] 1.4 实现单 owner goroutine 的 `JournalWriter` 与 batch append/Sync/Close 生命周期，保证连续 seq、batch fields、短写/Sync 后 poison 和无锁内磁盘 I/O；用并发 append、fault-injection、重复 close 和 shutdown 测试验证后运行 `go test -race ./internal/session`。
- [x] 1.5 实现有界 `Loader`、严格 v1 顶层 token/envelope/batch 校验、结构化 repair report 和仅尾部半行/未闭合 batch 截断；添加重复/未知顶层 member、尾随 JSON、非法 UTF-8、合法 v1、中段损坏、checksum/seq/ID 漂移、两类 torn tail、完整 committed `turn_started` 不截断及二次加载幂等 fixture，并运行 `go test ./internal/session` 验证。
- [x] 1.6 实现独立于 Loader 和 Provider 的强类型 `ReplayPlanner`，校验首批 metadata、required record placement、当前文本 turn 状态机并输出有序 native commits、optional records 与可选 interrupted-tail；覆盖重复 metadata、turn 外 commit、重复 terminal、成功 batch 缺项、新 turn 覆盖活动 turn、未知 required 拒绝和未知 optional 跳过后运行 `go test ./internal/session`。
- [x] 1.7 实现新 root thread 的 `session_meta`/`thread_meta` 初始 batch 与恢复后从最大 seq 续写，保存规范化绝对 `creation_cwd` 并验证其不具备自动 chdir/workspace 授权语义；用 fixture 扫描确认 API key、base URL、敏感 Header 和配置路径未进入 journal 或错误。

## 2. Provider 原生历史提交与恢复

- [x] 2.1 扩展 `internal/provider` 小接口，引入受控 `NativeCommitEnvelope`、一次性 `PreparedSample` finalizer 和接收已排序 commit envelopes 的事务式 restore 接缝；更新测试替身并用重复 finalize、错误 family/wire/revision、buffer 深拷贝和全量失败不返回半恢复 Conversation 的单测验证公共层不解析 Provider payload或依赖 Session 类型。
- [x] 2.2 为 OpenAI Responses 实现 `text_sample` native commit codec，完整保存输入 item、顺序 output items、phase、reasoning summary、encrypted content 和 unknown raw JSON；添加 payload golden/round-trip/大小与损坏校验测试。
- [x] 2.3 重构 OpenAI stream consumer，使成功 terminal 只产生 prepared sample、durable finalization 后才修改内存历史，失败/取消/EOF 丢弃 staging；以成功一次提交、未 finalize、重复 terminal 和 cancel/completed race 测试验证。
- [x] 2.4 实现 OpenAI commits 的事务式 restore factory，并增加“不中断会话 vs JSONL 恢复会话”的下一 Responses request canonical bytes、item 顺序和 cache fingerprint 回归测试。
- [x] 2.5 为 Anthropic Messages 实现 `text_sample` native commit codec，完整保存 user/assistant message、content-block 顺序、thinking/signature、redacted thinking、metadata、usage known/unknown 和 unknown raw JSON；添加 payload golden/round-trip/大小与损坏校验测试。
- [x] 2.6 重构 Anthropic stream consumer，使成功 terminal 只产生 prepared sample、durable finalization 后才修改内存历史，失败/取消/EOF 丢弃 staging；以成功一次提交、未 finalize、重复 terminal 和 cancel/completed race 测试验证。
- [x] 2.7 实现 Anthropic commits 的事务式 restore factory，并增加“不中断会话 vs JSONL 恢复会话”的下一 Messages request canonical bytes、message 顺序和 cache fingerprint 回归测试。
- [x] 2.8 增加 Provider 边界回归，证明 Session/Runtime/TUI 不导入两种 wire 类型、`HistoryProjector` 恢复前后可见文本一致且 projection 变更不影响 native history，并运行 `go test ./internal/provider/...`。

## 3. Runtime durable lifecycle

- [x] 3.1 为 Runtime 注入稳定 session/thread identity、Turn ID generator 和窄 Journal 接口，由 Runtime 给 `turn_started`、assistant 事件及唯一终态绑定相同 IDs，并拒绝仍含 interrupted-tail 活动 turn 的恢复状态；更新协议 snapshot、非法恢复状态和 Runtime 顺序测试。
- [x] 3.2 在网络请求前 durable append `turn_started`，验证写入失败时零 Provider 调用、零未关闭 goroutine，并以 fault-injection Runtime 测试覆盖。
- [x] 3.3 在成功 sample 上按 `[provider_native_commit, turn_completed]` durable batch → prepared finalizer → `turn_completed` 的顺序接入，验证 append/Sync 失败时不 finalize、不发 completed、Runtime poison 且拒绝下一 turn。
- [x] 3.4 在 Provider 失败、取消、timeout 和提前 EOF 上 durable append `turn_failed` 并丢弃 staging；验证 journal 已 poison 时不进行第二次写入尝试，且所有路径只产生一个 Runtime terminal。
- [x] 3.5 调整 `ChatSession` shutdown/interrupt 生命周期以等待 active turn 和 Journal 请求完成，补充 writer/Provider close 次序、并发 Submit/Interrupt/Shutdown 与 race 回归并运行 `go test -race ./internal/runtime`。

## 4. 创建、显式恢复与 TUI 回放

- [x] 4.1 实现应用级 Session service 的 create/open 两条装配路径，按 Loader load/repair → ReplayPlanner → root/config 兼容检查 → Provider 临时恢复 → writer reopen 的顺序装配；对 interrupted-tail 以原 turn ID Sync `turn_failed(code=session_interrupted)` 后才暴露 Runtime，并用新建、未知/非 root thread、语义损坏、配置不匹配不改 journal、补偿成功和补偿 Sync 失败测试验证零网络副作用。
- [x] 4.2 为 CLI 和 `app.Options` 增加显式 `--resume <thread-id>`，保持未传参时始终创建新 Session，验证缺失/非法 ID 不回退、不创建替代文件且 `--resume` 不等价于 `--continue`。
- [x] 4.3 调整应用资源 owner 和关闭顺序为 ChatSession → JournalWriter → Provider transport，使用取消中的 turn、Sync 失败和超时测试验证资源均有 owner、退出条件与等待路径。
- [x] 4.4 让 TUI 接收由应用提供的 `SemanticHistoryView` 初始 projection，在进入 idle 前按 turn 顺序显示已完成的 user/assistant 文本且不显示 thinking/signature/encrypted reasoning/usage；更新固定尺寸 snapshot 并运行 `go test ./internal/tui`。
- [x] 4.5 为 OpenAI 与 Anthropic 分别增加 httptest 端到端用例：多轮聊天、退出、显式 resume、回放后继续，并断言请求 native history、required record、JSONL seq 和 transcript 都保持顺序；另验证无 `--resume` 启动为空会话。
- [x] 4.6 增加取消后恢复、interrupted-tail 收口、尾部 repair 后恢复、不同 cwd 恢复但不自动 chdir、刷新 API key/base URL 后兼容恢复以及 family/wire/model 不匹配拒绝的集成测试；断言 creation cwd/Session IDs/repair report 不污染 request/cache fingerprint，并扫描 JSONL、错误、snapshot 和 fixture 确认无 secret 泄漏。

## 5. 文档与质量门

- [x] 5.1 更新总体架构/roadmap 中本次实际落地范围与剩余 P2 工作，并在 `pitfall-log.md` 记录 Claude Code/Codex 参考差异、required/optional record、三层恢复校验、interrupted-tail 补偿、prepared native commit、batch repair 和未来 tool-result input-only commit 的经验及对应回归测试；注明 compaction/fork 只保留 append-only checkpoint/cursor 约束、未在本 change 实现。
- [x] 5.2 运行 `gofmt`、`go vet ./...`、固定版本 Staticcheck、`go test ./...` 和 `go test -race ./...`，修复所有问题后以一次成功的 `make verify` 作为完成证据。
- [x] 5.3 运行 `openspec validate add-jsonl-session-resume --strict`，逐项核对 proposal/spec/design/tasks 与实现和测试一致；若实现迫使契约或范围变化，先更新本 change artifacts 再重新验证。
