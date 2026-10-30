# 同轮输入验收记录

## 选择理由与落地范围

当前工程已经有双 Provider、多 sample 工具循环、durable 工具结果与 Session 恢复，因此完整 outputs 后的采样边界是可验证的输入追加位置。同轮输入直接改善用户在长任务中补充条件、纠正方向、取回草稿和停止排队任务的体验，也能复用已有权限、执行与持久化边界。相比同时引入后台进程、PTY 或 Subagent，它不需要增加新的副作用执行系统。

产品行为参考 Claude Code 2.1.88 的运行中输入与取回编辑；内核取舍结合 Codex 6f8bf683c 的身份、顺序和控制边界。具体参考路径与取舍保存在 design.md，两个参考工程未修改。本实现使用实例持有的单 FIFO、强类型输入身份及 Provider 原生消息；不复制参考工程的全局队列或额外可执行 UI 队列。

已采用用户确认的修正：先对独立候选历史执行上下文和请求编码预检，再 Sync 输入。确定超限返还输入而不污染历史；网络失败仍保留已提交输入。Session/native 当前 canary 为 5/2，旧开发格式失败关闭；外部 headless JSONL 保持 v1。

## 验证映射

| 范围 | 业务证据 |
|---|---|
| 领域与控制契约 | `internal/domain/user_input_test.go`、`history_test.go`、`internal/protocol/input_test.go`：身份、大小、拷贝、封闭状态组合与严格解码 |
| 双 Provider | 两家的 `steering_test.go`：准备/丢弃不可见、完整 outputs 前拒绝追加、多消息顺序、opaque reasoning 保真、关联错配拒绝、native golden、恢复前后 canonical request bytes 与 footprint 等价 |
| 上下文 | `internal/context/planning_test.go`：消息身份不改变计划/fingerprint，非法角色拒绝；既有规划回归覆盖可见/native 估算覆盖值、unknown、稳定 catalog 与项目快照 |
| 输入持久化 | `internal/runtime/input_commit_test.go`：预检返还后缩短重提、接收前/后取消、确定失败/uncertain、转义膨胀拒绝与最大前缀拆分 |
| owner 与 FIFO | `internal/runtime/steering_test.go`：auto/strict 交错、工具后追加、下一轮屏障、原子取回失败不部分删除、stop 返还且保留 strict、停止期间拒绝 auto、每条输入单次 resolution |
| Session grammar | `internal/session/input_commit_test.go`：strict payload、重复 initial/身份、terminal 后输入、未闭合工具边界拒绝、16 MiB record 信封开销与 64 MiB batch 换行预算 |
| 固定当前 fixture | `internal/session/testdata/current/input_only_{openai,anthropic}.jsonl` 与 `steering_tail_{openai,anthropic}.jsonl` 经生产 Loader/ReplayPlanner；既有文本/七工具 fixtures 同步到唯一当前契约，不在测试中生成 fixture |
| 真实宿主与 transport | `internal/app/steering_e2e_test.go`：两家 HTTP/SSE 工具请求期间追加两条输入，同 turn 下一次请求只提交一次，重启后 identity/正文/顺序一致且恢复不联网 |
| 恢复故障边界 | `internal/app/resume_e2e_test.go`：固定 input-only/steering 尾部本地收口、append-only 前缀保持；既有取消、失败、恢复补偿失败测试保留 |
| headless | `internal/headless/steering_test.go`：新命令、未知/重复/null/非法 IDs 拒绝、不含正文的状态 golden；原有分片/UTF-8、strict FIFO、EOF/shutdown、断管与一次性 golden 全量运行 |
| TUI | `internal/tui/input_test.go`：取回等待冻结、超限不丢内容、returned 逐条取回/丢弃不执行、uncertain 不重试、固定宽度状态快照、pending Esc 定向停止和拒绝清除取消意图；既有字素、recall、审批/详情优先级、窄屏/无色/resize 快照保留 |
| 复用的取消/资源所有权 | `internal/session/writer_test.go` 的接收线性化与 Sync 故障注入，`lease_test.go` 的真实子进程竞争/崩溃释放/禁止 repair；`internal/app/host_lease_test.go`、`artifact_shutdown_darwin_test.go` 的阻塞清理、超时升级与 lease 保留 |
| 架构 | `internal/architecture` 全量守卫：依赖方向、纯构造、强类型 API、包级变量、唯一当前契约及占位 allowlist |

## 质量门

- `openspec validate add-same-turn-steering --strict`：通过。
- `openspec validate --all --strict`：34 项通过，0 项失败；仅有长 requirement 的信息提示。
- `git diff --check`：通过。
- `make verify`：最终完整检查通过（退出码 0），包括 gofmt、go vet、Staticcheck、架构守卫、全量测试和 race test。
- `.githooks/pre-commit` 仍调用同一 `make verify`，未修改或弱化质量门。

验证范围为当前 macOS 工作环境、离线测试服务与故障注入；未使用真实 API key 或外网模型。未进行新增 Linux/Windows 原生运行或真实终端人工 smoke test，不声称这些验证已经完成。主 specs、架构、Roadmap、pitfall 和 README 已同步；已按用户明确请求于 2026-10-30 归档，13 份增量规范的 39 条需求均已核对同步至主规范。
