# 推理控制能力全面回归记录（2026-11-11）

本轮重新执行单元、集成、真实 CLI、真实 PTY、并发与模糊测试，发现并修复两家 Provider 的重复 JSON 字段覆盖问题。修复后全部验证通过，当前测试范围内没有未解决的失败。

基线为 `e38621d`，工作分支为 `fix/provider-reasoning-regression`。环境为 macOS 27.0.1 arm64、Go 1.24.13。协议测试使用本地 HTTP/SSE/WebSocket 服务与合成凭据；没有访问真实模型服务，也没有验证 Linux/Windows 终端。该记录补充原有 [验收记录](verification.md)，保留两轮结果的时间边界。

归档补充：本报告的修复与测试已纳入九份 delta/main specs 的对照和相关工程文档，后续同步、质量门复核及归档状态见 [归档核对](archive.md)。本文末尾“未执行同步或归档”描述全面回归结束当时的操作范围。

## 发现的问题与修复

两个 SSE reducer 原先直接反序列化完整帧，重复成员会被后值覆盖。例如 `output_tokens_details` 先包含 `null` 再包含合法计量，或 `output_tokens` 先为 20 再为 40，可能使异常帧成为可信样本。OpenAI 的重复响应 ID、缓存计量成员存在同类问题。

- Anthropic 3 个复现用例、OpenAI 5 个复现用例在修复前失败，原始输出保留在 [Anthropic 复现日志](regression-2026-11-11/anthropic-before-fix.log) 和 [OpenAI 复现日志](regression-2026-11-11/openai-before-fix.log)。这些是有意保存的缺陷证据。
- 修复复用 `codec.ValidateJSON`，在归并状态或接受终态之前检查整个事件帧，再交给既有类型解码。重复成员和有损 Unicode 被拒绝；合法的未知扩展字段仍可通过。
- 生产变更仅涉及两个 reducer。未修改请求编译、原生持久化格式、依赖或 Runtime kind；修复没有增加自动重试、回退或降级路径。
- [修复后 Provider 完整测试](regression-2026-11-11/providers-after-fix.log) 和后续全量质量门均通过。

## 新增自动化测试

| 层级 | 覆盖内容 | 实现与证据 |
| --- | --- | --- |
| Anthropic 计量单元测试 | 13 类非法值（null、空白 null、负数、浮点、指数、字符串、布尔、容器、溢出）；3 类重复成员；4 类缺失、正值和明确零的连续观测；合法未知扩展 | `internal/provider/anthropic/usage_validation_regression_test.go` |
| OpenAI 计量单元测试 | 5 类重复成员，包括响应 ID、缓存计量与推理计量；合法未知扩展不会阻断正常完成 | `internal/provider/openai/usage_validation_regression_test.go` |
| 应用端到端 | 双 Provider × text/json/stream-json，共 6 组：完整工具参数已经到达，但最终 usage 含重复成员时，没有成功样本、工具执行或成功终态；日志关闭为失败；仅一次请求；不泄露私有字段 | `internal/app/reasoning_malformed_stream_test.go`；[结果](regression-2026-11-11/malformed-stream-e2e.log) |
| 编译后二进制端到端 | 双 Provider × print/json/stream-json × 正常、重复 usage、HTTP 400 参数拒绝、非法配置，共 24 组真实子进程测试 | `internal/app/reasoning_cli_e2e_test.go`；[24 组明细](regression-2026-11-11/built-cli-e2e.log) |

真实 CLI 正常场景完成 Read → 最终回答，并在新进程恢复（print/json）或同一进程继续第二轮（stream-json）。测试逐字节比较三种模式的完整续写请求，检查已有 journal 前缀保持、工具只执行一次、三个 sample 的原生与归一化计量一致。对外首轮推理/输出计量为 60/80，流式第二轮为 30/40。

失败场景检查无隐式重试、无凭据或推理私有字段泄露；非法配置在请求及应用资源创建前失败。stream-json 依据已有长会话契约通过 `turn.failed` 交付单轮失败，进程可正常关闭；text/json 对对应失败返回非零退出码。测试没有把长会话退出码误当作单轮成功。

CLI 构建仅复用本机已有 Go 缓存，禁用依赖下载；应用子进程使用隔离 HOME 和工作目录，清理继承的 `EASYCODE_*` 配置。构建与应用执行均有超时。

## 功能与相邻能力回归

重新运行既有完整测试，覆盖启动冻结与 family 校验、独立 effort/summary/display 编译、默认请求字节、HTTP/WS 请求证据、允许与禁止的传输回退、摘要独立输出预算及自动拒绝、缓存规划、原生恢复、计量 known/unknown、取消和持久化失败。具体实现映射继续参见 [原有契约对照](verification.md#契约与实现对照)，本轮重新执行结果见下述完整质量门。

真实 PTY 共运行 **8 套、32 个场景**，全部通过。场景内还包含多个交互步骤，不将每个按键步骤单独计数。

| 脚本 | 场景数 | 回归重点与原始日志 |
| --- | ---: | --- |
| `reasoning-controls-smoke.py` | 8 | 双 Provider、NO_COLOR、完成/取消；`/context`、`/cache`、Ctrl+O/r、滚动、resize、工具续写、摘要与预算拒绝；[日志](regression-2026-11-11/reasoning-controls-smoke.py.log) |
| `reasoning-smoke.py` | 8 | 推理阅读、取消、保存状态、终端恢复；[日志](regression-2026-11-11/reasoning-smoke.py.log) |
| `completion-smoke.py` | 2 | 补全交互；[日志](regression-2026-11-11/completion-smoke.py.log) |
| `markdown-transcript-smoke.py` | 2 | 彩色/NO_COLOR 下 Markdown 与输入交互；[日志](regression-2026-11-11/markdown-transcript-smoke.py.log) |
| `session-picker-smoke.py` | 4 | 搜索预览、跨目录、resize、Esc、中断、恢复后聊天；[日志](regression-2026-11-11/session-picker-smoke.py.log) |
| `user-question-smoke.py` | 4 | 用户问题交互；[日志](regression-2026-11-11/user-question-smoke.py.log) |
| `smoke-hooks.py --event PreToolUse` | 2 | 双 Provider、审批/拒绝、取消与终端恢复；[日志](regression-2026-11-11/hooks-PreToolUse.log) |
| `smoke-hooks.py --event PostToolUse` | 2 | 双 Provider、工具结果详情、反馈、审批与取消；[日志](regression-2026-11-11/hooks-PostToolUse.log) |

全部脚本退出状态见 [PTY 汇总](regression-2026-11-11/pty-summary.json)。其中 session-picker 的中断场景应用退出 130 为预期结果，脚本自身退出 0，终端均已恢复。

## 并发、边界与模糊测试

- Provider 定向 race 通过：[日志](regression-2026-11-11/critical-race.log)。
- Provider/Runtime 关键恢复、计量、WebSocket、取消与摘要预算测试在 `-race -count=5 -shuffle=on` 下重复 5 轮通过：[日志](regression-2026-11-11/shuffled-race.log)。
- `FuzzThinkingUsageStrictInteger` 使用标准 JSON 检查与独立的十进制整数解析作为判定，不复用生产解码器。输入域为单个 JSON 值；重复对象成员由独立帧用例覆盖。30 秒模糊测试共执行 **421,051 次**，没有新失败：[日志](regression-2026-11-11/thinking-usage-fuzz.log)。
- 八个相关核心包使用 `-count=1` 重新测量，合计语句覆盖率 **81.9%**；包级结果见 [定向测试日志](regression-2026-11-11/focused-tests.log)，逐函数结果见 [覆盖率摘要](regression-2026-11-11/focused-coverage-summary.log)，原始数据见 [coverprofile](regression-2026-11-11/focused-coverage.out)。覆盖率用于识别覆盖范围，不代表所有输入或线上环境都已验证。

## 最终质量门与复跑入口

所有 Go 修改完成后先执行 `go clean -testcache`，随后完整运行 `make verify`，**退出 0**。分支策略回归、格式、`go vet`、Staticcheck、全量测试（含架构守卫和新增 CLI 测试）、全量 `go test -race ./...` 均通过，结果不是复用测试结果缓存。[完整日志](regression-2026-11-11/make-verify.log) 中 app 普通测试为 85.218 秒、race 为 433.454 秒，时长仅为本次执行记录。

OpenSpec `validate --all --strict --json --no-interactive` 为 **56/56 通过**（2 个 change、54 个主规格），0 失败；[原始结果](regression-2026-11-11/openspec-strict.json)。补充报告后再次校验文档链接、当前分支名和 `git diff --check`。最终质量门之后没有修改 Go 源码；没有修改请求 golden、主规格或依赖。

主要复跑命令：

```sh
go clean -testcache
make verify
go test -count=1 -v ./internal/app -run '^TestReasoningControlsBuiltCLIEndToEnd$'
go test -count=1 ./internal/app -run '^TestAmbiguousReasoningUsageNeverCommitsSampleOrRunsTool$'
go test -race -count=5 -shuffle=on ./internal/provider/anthropic ./internal/provider/openai ./internal/runtime -run 'Test(ThinkingUsage|ReasoningUsage|ReasoningWebSocket|WebSocketHandshake|WebSocketPostCreate|CompactionStreamCancellation|ReasoningCompactionBudget|ThinkingSummaryUsage)'
go test -run='^$' -fuzz='^FuzzThinkingUsageStrictInteger$' -fuzztime=30s -parallel=2 ./internal/provider/anthropic
python3 scripts/reasoning-controls-smoke.py
python3 scripts/reasoning-smoke.py
python3 scripts/completion-smoke.py
python3 scripts/markdown-transcript-smoke.py
python3 scripts/session-picker-smoke.py
python3 scripts/user-question-smoke.py
go test -c -o /tmp/easycode-regression-hooks-app.test ./internal/app
python3 scripts/smoke-hooks.py /tmp/easycode-regression-hooks-app.test --event PreToolUse
python3 scripts/smoke-hooks.py /tmp/easycode-regression-hooks-app.test --event PostToolUse
openspec validate --all --strict --json --no-interactive
git diff --check
```

本轮未执行 commit、主规格同步或归档。结论适用于本次代码与上述平台、协议 fixture 和测试覆盖范围；真实服务端行为仍需对应环境验证。
