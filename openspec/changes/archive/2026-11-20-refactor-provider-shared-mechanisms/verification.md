# 实施验收

2026-11-19 在 Darwin arm64（Apple M1 Pro、Go 1.24.13）验证。对照源码为 `15e8e6aadb7c`。Provider 原生、Session current、请求及 UI 的固定 fixture 字节沿用基线，未替换预期输出。新回归覆盖架构分派、回放首错以及 UUIDv7 的固定时间和随机源。

- 双 Provider 定向回归通过。覆盖 live/restored canonical 请求、原生顺序与 opaque 数据、缓存指纹、普通/摘要请求、手动/自动压缩、WebSocket 增量与回退、工具和 Stop、用量、离线只读预览与 headless 帧。
- PreCompact 的真实 PTY 验收通过。Anthropic 与 OpenAI 分别完成手动、自动压缩四个场景，核对终端阶段、定向取消及请求。
- 最终 `make verify` 通过，81.109 秒。普通阶段执行 581 个入口，关键 race 阶段执行 61 个入口。包含格式、vet、Staticcheck、架构、跨进程 lease 与现有故障注入检查。测试结果缓存关闭。
- 变更严格校验、主规格严格校验和 `git diff --check` 通过。两项架构约束同步至 [single-current-contract](../../../specs/architecture/single-current-contract/spec.md)。新增逻辑未在 Linux 或 Windows 原生运行，本次 Provider 请求回归使用本地 fixture 与模拟端点。

## 基准及变动量

| 本地基准（中位数） | 基线 | 当前 |
| --- | ---: | ---: |
| Anthropic 回放 64 组工具结果 | 16.05 ms、158,311 次分配 | 4.96 ms、37,231 次分配 |
| Anthropic 回放 128 组工具结果 | 52.75 ms、554,776 次分配 | 9.80 ms、74,437 次分配 |
| Anthropic 目录解码一次 Read | 132.64 µs、139 次分配 | 1.34 µs、13 次分配 |
| OpenAI 目录解码一次 Read | 131.86 µs、138 次分配 | 1.33 µs、13 次分配 |

生产 Go 按 `internal` 和 `cmd` 下非测试源码统计，从 79,675 行降至 79,063 行，净少 612 行。其中 Provider 从 10,585 行降至 10,177 行，净少 408 行。全仓减少量中有 116 行函数定义迁入测试源码，作为迁移记录。实际减量低于评审阶段估算。

原始结果存放于忽略的 `.test-results/provider-refactor/` 和 `.test-results/provider-equivalence/`。首次全仓尝试遇到忽略目录中的 Go 基准源被格式检查扫描，移出 Go 源文件扫描范围后重新执行了完整质量门。

并发准入、恢复取消、计量对账收紧、停止后审批、Patch 取消分类、输出背压与 writer poison 等仍按 [设计中的独立评审清单](design.md#待单独评审的行为修复) 处理。

## 2026-11-20 全量回归

- Darwin arm64 重跑 `make verify`，581 个普通测试入口和 61 个关键 race 入口通过。`make test-race-full` 的 581 个入口及 1,328 个子测试全部通过，无跳过。测试运行器自检、分支规则、OpenSpec 全量严格校验 70/70 和 `git diff --check` 通过。
- 17 次 smoke 脚本全部通过，覆盖 67 个真实 PTY 场景和 8 个独立的正式 CLI 插件场景。覆盖双 Provider 压缩与取消、各类 Hook、推理与 effort、任务及文件引用、会话切换与恢复，以及 TUI 焦点与渲染。
- 用正式 CLI 对照基线 `15e8e6aadb7c` 验证两家 Provider 的 8 个压缩恢复场景。完整及尾部半行日志恢复后的请求逐字节相同。旧检查点损坏和模型不匹配均保持错误分类、拒绝请求并保留日志字节。
- 用同一基线执行两家 Provider 的工具调用与断流恢复流程，新旧 CLI 共发出 24 次本地 SSE 请求。核对真实文件读成败及顺序、原生推理和缓存字段、UTF-8 分片、同进程与跨进程恢复请求逐字节等价、断流后继续且不重放工具、跨 Provider 恢复拒绝及离线列表。基线对照排除每次运行生成的身份和按线程的缓存键后，请求摘要及稳定日志证据一致。
- Linux amd64/arm64 和 Windows amd64 的生产构建、CLI 构建、可用测试二进制编译及 vet 通过。Linux Staticcheck 通过。Windows Staticcheck 报告的 61 条 U1000 与基线一致。Linux 和 Windows 未原生执行，Provider 使用本地 SSE 模拟端点。

原始数据位于忽略的 `.test-results/full-regression/`。
