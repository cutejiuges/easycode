# 交互式 Plan Mode 验收记录

2026-11-21 在 Darwin arm64、Go 1.24.13 下验证。选择该能力是因为只读调查、结构化提问、任务清单、Markdown 转录和审批已经具备，可以复用它们补齐先规划再实施的交互流程。模式入口与只读提示沿用 Claude Code 的交互习惯，双 Provider 保留各自的原生请求、工具名与恢复语义。

## 实际执行

- `make verify` 通过。执行 gofmt、go vet、Staticcheck、架构检查、607 个普通测试入口和 62 个选定 race 入口。新增模式接纳与取消用例进入 race 清单，既有交接用例同时覆盖模式控制。
- `openspec validate --all --strict --json` 通过 70 项，包含 69 份主规格及本变更。七份增量已同步，`openspec validate --specs --strict --json` 通过 69 份主规格。`git diff --check` 通过。
- `python3 -B scripts/plan-mode-smoke.py` 通过两个真实 CLI 场景，每家 Provider 11 次请求。检查工具声明和历史调用的配对顺序，覆盖上游拒绝后的继续规划、已授予权限下的写入拒绝、流式未完成标记、问答焦点、Markdown 修订、忙碌与取消、恢复时默认模式、草稿与窄屏布局、退出后独立提交并审批写入，以及退出时终端设置恢复。PTY 输出保存在忽略的 `.test-results/plan-mode/`。

## 关键对账

- `internal/runtime/work_mode_test.go` 验证接纳、取消和交接互斥，以及混合样本在任何事实落盘、Hook 或执行之前整批拒绝。`internal/runtime/stop_hooks_test.go` 验证同轮追加和已授权 Stop Hook 续请求保持七项工具与规划规则。`internal/tui/work_mode_test.go` 覆盖补全草稿、接纳前后取消和旧宿主消息过滤。
- 双 Provider 的 `plan_mode_exec_continuation.golden.json` 验证同模式原生恢复的 canonical bytes、调用次序和 fingerprint。实际 WebSocket 验证跨模式完整请求、同模式增量、空切换保留续写资格和摘要重置。Anthropic 请求与缓存副本保留相同规划指令。
- `internal/app/plan_mode_test.go` 验证 headless 同名 Skill、`/context` 的空闲估算、`/cache` 的普通基线、失败和独立摘要收据，以及手动摘要后恢复的原生请求等价。自动压缩场景验证旧 Markdown 仍可逐字阅读，本次实施输入显式携带的决定进入普通请求。

## 归档前复验（2026-11-21）

- `make verify TEST_OPTIONS='--output=.test-results/plan-mode-archive/verify'` 通过，耗时 53.3 秒，执行 607 个普通测试入口和 62 个选定 race 入口。
- Darwin arm64 终端回归通过 51 个场景。使用现有 `scripts/` 下的 Plan Mode、补全、文件引用、问卷、会话选择、Markdown、effort、PreCompact、Stop Hook、任务进度、推理控制和会话标题烟测，共 49 个场景。另用真实 CLI 和两个本地模拟服务临时验证了 2 个聊天内交接场景，取消保留源 Plan 状态，两次成功交接恢复 Default，交接期间模型请求数和 journal bytes 保持原值，返回源会话仍可阅读旧计划。
- 修正 `scripts/plan-mode-smoke.py` 对绘制顺序的假设。在同一次越界请求的终端输出中核对未完成标记、部分正文和协议错误。复跑 `python3 -B scripts/plan-mode-smoke.py --output .test-results/plan-mode-archive/terminal/plan-mode-rerun`，双 Provider 各 11 次请求通过。复用现有测试用例。临时验收场景与原始输出留在忽略的 `.test-results/plan-mode-archive/`。
- Linux amd64 与 Windows amd64 分别执行 `GOOS=<目标系统> GOARCH=amd64 go build -p=3 ./...`，均通过。协议验证使用本地模拟 HTTP/SSE 与 WebSocket 服务，原生终端验收仅覆盖 Darwin arm64。
- 归档前对账 17 项任务及 7 份增量规格，主规格已包含 5 项新增要求及 8 项修改，原有场景均保留。`openspec validate --all --strict --json` 归档前通过 70 项，归档后通过 69 份主规格，`openspec list --json` 确认活动变更为空。

## 归档后测试精简（2026-11-21）

- 测试源码净增量从 1,738 行降至 1,473 行，减少 265 行。合并同名 Skill 和文件引用场景，复用 Stop Hook 和 WebSocket 的现有流程，以及手动压缩、journal 和真实 SSE 夹具。保留样本越权阻断、接纳取消与交接、两家 Provider 请求 golden 和原生恢复等价。
- `python3 -B scripts/plan-mode-smoke.py --output .test-results/plan-mode-test-trim/terminal` 通过，双 Provider 各 9 次请求。在同一个阻塞请求验证忙碌拒绝和取消，保留实际问卷焦点、失败片段标记、Default 恢复及退出后审批写入。上游失败时保持缓存基线由应用层验证。
- `make verify TEST_OPTIONS='--output=.test-results/plan-mode-test-trim/verify'` 通过，执行 604 个普通测试入口和 62 个选定 race 入口，实测 59.8 秒。本轮较归档前多 6.5 秒，其中 race 用例清单收集多 5.2 秒。`openspec validate --all --strict --json` 通过 69 份主规格，`git diff --check` 通过。
