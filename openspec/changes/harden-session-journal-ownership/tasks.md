## 1. 固定 v1 兼容基线

- [ ] 1.1 在 `internal/session/testdata/migrations/v1/` 提交覆盖六种 v1 record kind、成功/失败 turn、完整 batch 和合成 Provider opaque commit 的不可变 JSONL 与预期 ReplayPlan 摘要，人工确认无 secret、base URL、本机路径且 fixture 不是测试运行时由当前 encoder 生成，并用 `go test ./internal/session -run 'Test.*V1.*Fixture'` 验证当前 Loader/ReplayPlanner 可读取。
- [ ] 1.2 增加 fixture 续写与较新 required revision 的只读失败回归：把 fixture 复制到临时数据根后追加连续 seq，断言原 prefix bytes 不变；对不支持 revision 断言 repair/append 前后 bytes 相同且无 Provider 副作用，并运行对应 `internal/session` 测试。

## 2. Journal lease 与平台锁

- [ ] 2.1 将 `golang.org/x/sys` 提升为直接依赖，增加统一的非阻塞 exclusive journal lock adapter、Darwin/Linux/Windows 实现和 unsupported fail-closed fallback；使用 typed busy 分类而非字符串匹配，并以平台单测及目标平台编译检查验证 busy 与普通系统错误可区分。
- [ ] 2.2 在 `internal/session` 实现字段不导出的 `JournalLease` 和单向状态转换，使 Repository 的新建/打开可写入口只返回已校验、已锁定的 lease，锁失败关闭临时 handle，handle 关闭或进程退出释放所有权；以两个独立 Repository 实例、重复 Close、非法 transfer 和权限/路径回归测试验证，不保留生产可调用的裸 `*os.File` 写入旁路。
- [ ] 2.3 使用当前 test binary 的 helper-process、pipe ready/control 握手实现真实子进程测试，覆盖第二 owner 获取失败、失败路径 journal bytes 不变、正常关闭后重新获取和强制终止后自动释放；测试不得以 `time.Sleep` 作为锁状态证明。

## 3. 连续 load/repair/write 所有权

- [ ] 3.1 将 Loader 改为纯内存构造并借用 `JournalLease` 的同一 handle 完成 seek、全量校验和可选 repair，移除 Loader 自行 open/close 的路径；覆盖有效 load、两类尾部 repair、错误后 lease 仍由调用方关闭，以及 helper 持锁时父进程不能 repair、释放后只 repair 一次的测试。
- [ ] 3.2 让 JournalWriter 只通过生产级 `StartJournalWriter(lease, identity, nextSeq)` 接管 lease，删除 `ReopenJournalWriter` 和生产裸文件构造旁路；为 fault injection 保留包内窄测试接缝，并以启动失败不消费 lease、成功 transfer 后只有 writer 能关闭、poison/重复 Close 最终释放 handle 的测试验证。
- [ ] 3.3 重构 AppendBatch admission：draft 准备与 context 初检在锁外，锁内只做状态判断和非阻塞 channel send，队列满在 seq 分配前失败；accepted 请求等待确定 response。重构 Close 为先线性化 closing、再由 context 限制等待但不放弃 owner drain/Sync/close，并以 queue-full、admission 前取消、admission 后取消、Close/Append 竞态、已取消 Close 和 `go test -race ./internal/session` 验证。

## 4. 应用恢复装配与错误映射

- [ ] 4.1 在 `internal/fault` 增加稳定英文 `session_busy`，调整 Session service 的 create 路径为“创建 lease → writer 接管 → durable metadata”，调整 resume 路径为“取得 lease → 同 handle load/repair → replay/config/Provider restore → writer 接管 → 可选 interrupted-tail 补偿”，并以应用单测验证 busy、not-found、corruption、incompatible 与普通 write error 不混淆。
- [ ] 4.2 为 resume 的每个 ownership 分支补 fault-injection：ReplayPlanner 失败、非 root/config mismatch、Provider restore 失败、writer start 失败、interrupted-tail append/Sync 失败均释放正确 owner且不泄漏 handle；busy 必须零 Provider restore/network、零 TUI 暴露、零 journal 修改，运行 `go test -race ./internal/app` 验证。

## 5. 跨模块与 Provider 回归

- [ ] 5.1 增加应用级双进程 resume 集成测试：第一个进程持有活动 writer 时第二个得到 `session_busy` 且 bytes 不变，前者退出后后者恢复并追加更大 seq；使用确定性进程握手并运行目标集成测试验证。
- [ ] 5.2 运行并按需补充 OpenAI 与 Anthropic “不中断会话 vs v1 fixture/JSONL 恢复”回归，逐项断言下一请求的 native item/message 顺序、canonical bytes、cache fingerprint 和可见 HistoryProjector 语义不变，并运行 `go test ./internal/provider/... ./internal/app`。
- [ ] 5.3 对当前开发平台执行真实 lease/repair/race 测试，并对 Darwin、Linux、Windows lock adapter 执行可用的交叉编译检查；无法在本机执行的目标运行测试必须记录到交付风险，不得把仅编译通过声称为平台行为已验证。

## 6. 文档与质量门

- [ ] 6.1 更新总体架构、Roadmap 和 pitfall-log：明确单 writer 是跨进程 lease，修正不存在的 `user_input`、`turn_interrupted` 与测试路径，区分“v1 compatibility fixture 已建立”和“未来 schema 转换未实现”，并记录 advisory lock 不能约束旧版/非协作进程的限制；用仓库搜索确认无失效词汇和引用。
- [ ] 6.2 运行 `gofmt`、`go mod tidy -diff`、`git diff --check`、全部目标测试与一次完整 `make verify`，确认无 data race、goroutine/handle 泄漏、secret 或无用依赖；随后执行 `openspec validate harden-session-journal-ownership --strict` 并核对 proposal/spec/design/tasks 与实现一致。
