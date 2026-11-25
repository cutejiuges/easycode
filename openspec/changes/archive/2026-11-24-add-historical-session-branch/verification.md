# 历史回合分支验收

- Darwin arm64 上运行 `python3 -B scripts/session-picker-smoke.py`，10 个真实 PTY 场景通过。新增场景覆盖补全、历史预览、取消恢复原草稿、确认创建、回填和 `/resume` 返回原线程。连续 Enter 只创建一个分支，回填后等待主动发送，编辑提交后才观察到新的模型请求。固定终端尺寸和消息顺序回归覆盖同文提示、缩放、选择保持与原样发送。 [PTY 验收](../../../../scripts/session-picker-smoke.py) · [终端回归](../../../../internal/tui/rewind_test.go)
- 本地 Anthropic Messages 和 OpenAI Responses SSE 验证了历史切点、原生项及工具配对顺序、任务与标题独立、工作区及源 journal 字节一致。即时续写与移除祖先 journal 后的冷恢复使用相同的 canonical 请求、上下文 footprint 和稳定 fingerprint。覆盖 Anthropic TTL、OpenAI WebSocket 的完整首请求和新 cache key，以及源 Read 证据、审批与 artifact 原文访问在目标中的权限边界。 [双 Provider 回归](../../../../internal/app/session_branch_e2e_test.go) · [权限与输出回归](../../../../internal/app/session_branch_artifact_darwin_test.go)
- 保留选中回合前已完成的手动 checkpoint，排除该回合启动后、首条输入提交前生成的自动 checkpoint 和 SessionStart 事实。源 journal 继续使用当前记录契约，分支原样保留切点前缀并追加 `thread_forked`。回归验证了已同步读取边界、后缀篡改、陈旧选择、取消后归还浏览许可和重复确认。 [压缩与 Hook 回归](../../../../internal/app/session_start_e2e_darwin_test.go) · [Session 回归](../../../../internal/session/historical_cut_test.go) · [接管回归](../../../../internal/app/picker_branch_test.go)
- Darwin arm64 的 `make verify` 通过，用时 60.439 秒，覆盖全量普通测试、已审计 race 和静态检查。新增 race 用例分别验证 writer 接纳取消后的读取归属与物理尾部边界，以及浏览许可在取消或确认时的释放与转移，实测分别为 0.05 秒和 0.13 秒。 [Race 清单](../../../../scripts/race_policy.json)
- `openspec validate add-historical-session-branch --strict` 和同步后的 `openspec validate --all --strict` 通过，三项增量已同步。Linux amd64 通过 `GOOS=linux GOARCH=amd64 go build ./...`。运行验收使用 Darwin arm64 和本地 Provider 夹具，未在 Linux 或 Windows 原生运行，也未连接真实 Provider 服务。

## SSE 解压修复前的复验

- 修复了两处复验发现的草稿边界。同文历史召回保留原样发送确认和首词意图。运行时在输入持久化前退回或撤回队列时，取回草稿保留首条输入的正文或显式 Skill 意图。Darwin arm64 的 `make verify` 复跑通过，用时 88.551 秒。会话选择器 10 个 PTY 场景通过，会话命名和 Plan Mode 的双 Provider 冒烟回归通过。Linux amd64 重新交叉构建通过。 [同文召回回归](../../../../internal/tui/composer_history_test.go) · [退回和撤回回归](../../../../internal/tui/rewind_test.go)
- 通过 `go build -o <临时目录>/easycode ./cmd/easycode` 构建 CLI，在隔离工作区和真实 PTY 中对本地 Anthropic Messages 与 OpenAI Responses 服务各完成 5 次请求。空会话、取消、连续 Enter、编辑后提交、`/resume` 返回源线程、离线列表和无祖先 journal 的冷恢复均通过。按实际字节核对保留前缀、血缘序号与 SHA-256、源 journal 和工作区文件。两家 Provider 均只收到分支切点保留的输入及显式提交的新输入。
- 用当时配置的真实 OpenAI Responses 服务验证时，普通会话首轮报出 `stream_protocol_error`。从实现前的基线 `c995153` 重新构建 CLI 后，用同一服务的普通 headless 请求复现 `provider stream closed before response.completed`。当次未能完成真实上游的 `/rewind` 全链路。

## 两项变更联合复验

- 从当前 worktree 重新构建 CLI，10 个会话选择器 PTY 场景通过。使用两种本地 gzip SSE 夹具各完成 5 次请求。连接真实 Anthropic Messages 和 OpenAI Responses 时各完成 4 个持久化回合，验证了草稿回填、取消、延迟 Enter、编辑发送、原始前缀 SHA-256、返回源会话及独立冷恢复。 [联合验收记录](../2026-11-24-fix-sse-response-decompression/verification.md)
- 提交前全仓回归发现，源会话的父生命周期取消与子循环看到取消之间存在短暂的分支发布准入窗口。交接许可现在直接检查父生命周期的完成信号，并核对原有子循环状态。受控回归在子循环仍运行时取消父生命周期，确认许可无法通过发布检查。 [许可回归](../../../../internal/runtime/resume_permit_test.go) · [发布回归](../../../../internal/app/picker_branch_test.go)
