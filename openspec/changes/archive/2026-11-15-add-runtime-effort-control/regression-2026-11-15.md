# 再次回归、端到端验收与测试耗时排查

日期：2026-11-15（Asia/Shanghai）；分支：`feat/runtime-effort-control`。本轮接续 11 月 14 日完整复审，检查同一份实现，未修改生产代码、测试断言、超时、安全/恢复矩阵或 race 配置。新增 tasks 第 10 节跟踪本次工作，证据独立保存在 regression-2026-11-15。

最终状态：47/47 项任务完成。完整未缓存质量门、双 Provider 真实 CLI、24 组 PTY 和 OpenSpec 严格校验全部通过；本轮覆盖范围内未发现新的功能问题、死循环或无法退出的等待。实现及固定 fixture 与上轮复审相同，源码校验值未变化。

## 功能和端到端结果

| 范围 | 实际验收及结果 | 证据 |
| --- | --- | --- |
| 完整质量门 | `make verify` 通过，937.83 秒。两遍 `go test` 均强制 `-count=1` 并记录 JSON；格式、vet、Staticcheck、架构、全部普通/race 范围和 20 分钟 watchdog 保持。1,368 个输入执行前后 SHA-256 一致。 | make-verify.json、日志 |
| 未缓存普通全量 | `go test -json -count=1 -timeout=20m ./...` 通过，153.97 秒；app 包 153.041 秒，是该阶段的关键路径。 | test-timing.json、原始事件 |
| 未缓存全量 race | `go test -json -count=1 -timeout=20m -race ./...` 通过，781.18 秒；app 包 780.224 秒。app 的 230 个顶层/975 个子测试全部通过，零失败。 | test-race-timing.json、原始事件 |
| 独立 app 普通回归 | 230 个顶层测试、975 个子测试通过，包内用时 147.564 秒；外层含构建/启动 148.74 秒。 | app-normal-before-timing.json |
| 对照 race | 插件浏览/恢复、Exec 失败和真实编译 CLI 三个矩阵全部通过；包内 52.436 秒，外层 54.29 秒。 | app-race-before-timing.json |
| 真实 CLI effort | 编译 `cmd/easycode`，对双 Provider 本地 SSE 服务执行完整多进程会话：首个 high 请求暂停时设置 low；当前操作仍 high，queued 下一回合 low；auto 省略、reset 恢复 high；重复 request 被拒绝且选择不变。每家实际请求序列为 high/low/omitted/high/low/low。 | cli-e2e.json、可复跑脚本 |
| CLI 恢复和失败 | 独立进程仅查询恢复，启动值 high/序号 0，零额外请求，journal bytes 不变；配置始终不改写。再恢复后设置 low，400 恰好一次失败、无隐式重试；显式重试保持 low 并成功，旧 journal 前缀不变，健康流 EOF 退出 0。 | 同上；脚本验证真实 stdout 控制事件、HTTP body、进程退出和磁盘 bytes |
| effort PTY | 双 Provider×颜色×取消/正常，共 8 组通过；流中切换、捕获/后续值、窄屏队列叠加、help、补全、同名 Skill 和终端恢复。 | effort-pty.json |
| 其他真实终端 | 推理控制 8 组、补全 2 组、Markdown 2 组、插件 4 组通过；插件脚本另含 8 组真实编译 CLI。共 24 组 PTY，另有 10 组脚本化真实 CLI。 | reasoning-controls-pty.json、completion-pty.json、markdown-pty.json、plugins-pty.json |

CLI 脚本的两个编写期预期已按现有主规格校正，并保留原始失败日志：turn 事件用 `input_id` 关联，控制结果用 `request_id`；单 turn 已通过 `turn.failed` 报告后，健康 stream-json 在 EOF 清理完成时应退出 0。依据是 `headless/stream-control` 的响应及退出契约，而非放宽对实现的要求。最终脚本仍严格断言唯一失败、无重试、真实字段/次数、退出码和磁盘不变；两次脚本预期错误不计为产品功能缺陷。

## 为什么会超过十分钟

`make verify` 顺序执行分支策略、格式、vet、Staticcheck、全量普通测试和全量 race。`make test` 只执行普通测试；本轮这部分约 2 分 34 秒，race 则约 13 分 1 秒，合计质量门约 15 分 38 秒。超过十分钟的关键路径是 app 包的全量 race，而不是普通 `go test` 或某个单独挂起用例。

app 中名称包含 Effort 的 9 个顶层测试，普通累计 3.64 秒、race 累计 23.96 秒；约占 app race 用时 3.1%。它们不是本轮十分钟以上用时的主要来源。完整比较见 test-timing-summary.json；父测试已包含子测试耗时，不能再把两者相加。

同一完整质量门中的慢测试排名：

| app 顶层矩阵 | 普通秒数 | race 秒数 |
| --- | ---: | ---: |
| SessionPreviewCurrentFixturesAreReadOnly | 6.15 | 56.34 |
| WriteSSEChunksCancellationEOFAndLimitsPrecedeSideEffects | 3.91 | 48.30 |
| FailureBothProvidersRealHookSandboxAndFrozenRestore | 13.90 | 47.96 |
| StopCompactionKeepsFrozenFeedbackAndGenuineSkillCorrection | 3.59 | 35.61 |
| StopCompactionKeepsFrozenPluginCorrectionAfterSourceRemoval | 3.53 | 35.09 |
| PluginBrowserManualAndRestoredRequestsBothProviders | 8.85 | 34.22 |
| SevenToolTranscriptBudgetAndRestoredRequestForBothProviders | 2.97 | 33.82 |

本次测量期间，还观察到另一 worktree 同时运行 app 全量 race 和 CPU profile，存在 CPU/内存/磁盘资源竞争。未终止或修改其进程。因此以上是本轮实际验收用时，不是隔离性能基准；不能据此精确拆分竞争影响或承诺固定耗时。对照矩阵和源码仍明确显示：大量串行安全/恢复组合、重复 JSON 校验及 race 检测构成主要开销。

本轮给 `make verify` 的 Go 执行器增加观测包装，只给两条 test 命令附加 `-json -count=1`；其他包列表、race、20 分钟 watchdog、vet、Staticcheck 与实际 Makefile 完全保留。`-count=1` 确保重跑而非复用上轮缓存。包装源码和实际命令见 timing-wrapper.txt；普通和 race 的全部事件、包耗时、顶层耗时和子进程 CPU 分别保存。

已完成的同场景对照：

| 测试矩阵 | 普通秒数 | race 秒数 | 比率 |
| --- | ---: | ---: | ---: |
| PluginBrowserManualAndRestoredRequestsBothProviders | 8.30 | 34.54 | 4.16× |
| ExecFailurePathsEndToEndForBothProviders | 5.26 | 11.89 | 2.26× |

这些矩阵不是单条空断言：插件浏览矩阵为 3 种入口×2 Provider×2 manual 策略×2 字符输入，共 24 组，每组创建 5 次真实请求，比较手动/补全/重启后的 canonical bytes、cache/native fingerprints、原生顺序、附件和 journal。failure hook 为 2 Provider×3 capability×11 模式，共 66 组；PreToolUse hook 为 2×3×5，共 30 组。当前 app 包多数此类测试串行执行，复用进程级 HOME 环境隔离和完整故障/恢复装配，每组的开销会累加。

代表性 CPU/阻塞/互斥 profile 各采集了插件恢复、真实 Hook 和大型预览 fixture 三个场景。CPU 样本中可见 `__tsan`、`racecall`、runtime 内存/线程操作和系统调用；这是 race 与实际资源操作的可测开销。预览矩阵遍历 122 份 fixture，分别验证完整/半行输入、拒绝/只读 bytes，合法项再做离线投影及原生恢复对照。约 624 KB 的两个大型 fixture 在完整 race 中各约 5 秒；单独采样显示 `codec.UnmarshalRecord`、`ObjectMembers`、`encoding/json.Decoder.readValue` 及 race 读检查是具体热点。

阻塞主要位于 channel/select，包含测试等待结果、owner 等待输入、HTTP 流和明确清理完成信号。采样没有显示某个业务循环持续空转；阻塞 profile 是多个 goroutine 的累计等待，不能把 15 秒累计值误当成一个用例卡住 15 秒。profile 只支持代表性场景的判断，不把其百分比外推成整个套件的 CPU 构成。

- profile 汇总
- 插件 CPU、阻塞、互斥
- Hook CPU、阻塞、互斥
- 大型预览 CPU、累计调用、阻塞、互斥

## 死循环和长等待核对

本轮普通和 race 的 app 各 230 个顶层/975 个子测试均已退出；对照 race 矩阵和真实 CLI/PTY 也均有确定完成信号，未发现无法退出的业务循环。完整 race 最慢的顶层矩阵为 56.34 秒，内部包含 122 个 fixture 子测试，并非某个测试卡住十分钟。

源码与实际计时核对：

- `internal/app/effort_e2e_test.go` 的查询等待有 10 秒 context；idle 查询循环依赖既有 owner 的 worker 完成信号，本轮冻结/取消/失败/恢复用例均完成，未形成测试关键路径。
- `internal/app/completion_e2e_test.go` 每组具有 30 秒 context，真正执行的是有限组合循环及两条对照分支，后续输入等待 terminal 后退出。
- `internal/executor/process/owner.go` 的清理有 250 毫秒 grace、最多 40 次×25 毫秒 settle 和 1 秒 drain；没有无条件无限轮询。实际退出确认仍由原 owner 与进程事实完成，不能为缩短测试删除这些安全边界。
- Hook fixture 的 `/bin/sleep 5` 和 Exec fixture 的 `/bin/sleep 30` 用于验证超时/取消。普通超时子用例约 1.38～1.40 秒，race 约 1.73～1.81 秒即完成，并未完整等待 5 秒或 30 秒。
- `Makefile` 的 `TEST_TIMEOUT=20m` 是 Go 包级失败上限，不是每个测试固定等待 20 分钟。当前已通过包的实际时长远低于该上限。

没有为了缩短时间修改 `GORACE`、跳过恢复/安全矩阵、删除断言、放宽结果或增加更大的 timeout。不能直接给这些测试批量添加 `t.Parallel`：现有 `TestMain` 和多处 `t.Setenv` 有进程级环境语义，盲目并发会破坏隔离。若后续推进性能优化，应先根据本轮完整排名选择热点，保留全部业务断言，分别评估测试装配复用或实例化环境隔离；本次排查不扩大为新的运行时/缓存设计。

## 范围与限制

实际平台为 Darwin/arm64，使用 Go 1.24.13。全部 Provider 请求发往本地 HTTP/SSE/WebSocket 服务，使用假凭据；没有调用真实远端模型，也未执行 Linux/Windows 原生终端矩阵。本轮未发现需要修复的实现功能异常，不把通过的本地矩阵当成所有远端服务和平台的保证。

404 份固定 fixture bytes 保持，六份 delta 的已批准需求未变，全部主规格/changes 的 62 项严格校验通过。验收后于 2026-11-15 归档，收尾见 [archive.md](archive.md)；未提交或推送；未修改其他 worktree 的源码或进程。最终审计和状态见 final-audit.json。
