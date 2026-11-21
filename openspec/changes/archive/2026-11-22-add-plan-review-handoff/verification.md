# 方案审阅与同会话实施交接验收

2026-11-22 在 Darwin arm64、Go 1.24.13 上验收。

| 检查 | 结果 |
| --- | --- |
| `make verify` | 通过，62.574 秒。覆盖 gofmt、go vet、Staticcheck、架构检查、614 个普通入口和 63 个审计 race 入口。 |
| `make verify TEST_OPTIONS='--race-scope=full'` | 通过，115.268 秒。静态检查通过，614 个普通入口和 614 个 race 入口全部执行，默认 race 清单的 63 个入口均在全量清单中。 |
| `python3 scripts/plan-mode-smoke.py` | 通过，使用真实 Darwin PTY 和本地 SSE 夹具。OpenAI 与 Anthropic 各 10 次实际请求，覆盖继续规划、重开审阅、修订、逐字交接、独立写入审批与新进程恢复。 |
| 现有端到端脚本 | 16 套脚本全部通过，共 69 个 PTY 场景和 8 个插件 CLI 场景。覆盖方案审阅、推理与 effort、会话、补全、文件引用、Markdown、问答、任务、插件与已支持的命令 Hook。 |
| `openspec validate --all --strict` | 归档前 70/70 通过，归档后 69/69 主规格通过。Runtime 与 TUI 主规格逐项对账通过，4 项修订、3 项新增均已同步。 |
| `GOOS=linux GOARCH=amd64 go build ./cmd/easycode` | 交叉构建通过。原生终端验收覆盖 Darwin。 |

- 双 Provider 集成复用 `internal/app/plan_mode_test.go`。自动摘要刻意省略旧方案的区分性内容，实施请求仍带完整获批原文，`$plan` 和 `@` 路径以字面值交付。确认之后文件仍须单独批准。恢复与只读预览沿用普通提交事实，持续运行与恢复后的全量请求、原生项顺序和缓存指纹对账通过。
- Provider 请求 golden 覆盖固定指令与七项规划工具。Anthropic 验证 TTL 断点和显式截断过滤。OpenAI 验证独立的最终消息、相互矛盾的终结元数据，以及 WebSocket 的全量与增量请求。
- 默认 race 清单新增一项 `TestPlanApprovalStartsOneLiteralInput`。用确定性编码屏障核对接纳前取消和编码拒绝，检查接纳后取消确认仍保留唯一初始输入及原 InputID。该入口在默认 race 阶段实测 0.06 秒。
- 首次全量回归发现既有 Hook 故障测试的替身忽略注入的 stdin，导致超时与伪造的 I/O 失败竞争。修正现有替身的一行代码后，五种确定性故障场景的针对性 race 检查及完整质量门通过。
- 原始诊断和 PTY 捕获位于忽略的 `.test-results/plan-review-handoff/` 与 `.test-results/plan-handoff-final-e2e/`。Provider 验收使用本地协议夹具。

合入含 PostCompact 的 `main`（`fb926f0`）后，复用原有双 Provider 集成用例，确认 PostCompact 明确失败并关闭后，获批原文仍只提交一次且写入保持独立审批。集成用例耗时 0.96 秒。方案审阅与 PostCompact 两套双 Provider PTY 验收均重新通过，OpenSpec 严格校验通过 70 项主规格。合入后的终端证据位于忽略的 `.test-results/plan-handoff-merged/`。
