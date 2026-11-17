# 测试执行优化验收

## 当前结论

**整体目标尚未完成。** 本机完整质量门连续两轮为 **239.377 / 212.452 秒**，Linux 最短完整对照仍为 **359.663 秒**。原有业务测试、race、安全与恢复断言全部保留；不能用 600 秒诊断运行的成功状态代替五分钟验收。Hook 编码复用已通过本机与原生 Linux 定向回归；新的 Linux 全量 300 秒运行仍超时。

已同步 `origin/main` 的 `b0387c4bb3b833082beb1d077767885ecf46acdd`，开发分支为 `fix/test-execution-performance`。当前实现提交 `a923dae75962ca1f8f99e67809bb922c0d0a5d14`，执行输入摘要为 `8b9d45fd47f8ff4c6911867129c56b8d515a7b7d7a563f042bf9466b548a815d`。最终并发参数及验收任务保持未完成，变更未归档。

## 计时与覆盖口径

参考本机为 Darwin/arm64、Apple M1 Pro、10 核、32 GiB、Go 1.24.13；原生 Linux runner 为 amd64、4 核、约 16 GiB、Go 1.24.13。工具链与依赖准备不计入；计时包含静态检查、枚举、测试编译/链接、全量普通及 race。允许正常构建缓存，实际测试全部强制 `-count=1`。阶段时间包括该阶段的枚举及构建。

| 环境与运行 | 普通 | race | 完整质量门 | 判定 |
|---|---:|---:|---:|---|
| main 独立源码快照 | 150.704 s | 961.605 s | 1114.245 s | 基线通过 |
| 当前本机 pre-commit | 60.931 s | 155.750 s | 239.377 s | 通过，低于 300 s |
| 当前本机 pre-push | 57.819 s | 148.265 s | 212.452 s | 通过，低于 300 s |
| Linux，2 命令 / 1 分片 | 39.729 s | 386.643 s | 444.570 s | 完整通过，性能未达标 |
| Linux，4 命令 / 2 分片 | 33.876 s | 306.810 s | 359.663 s | 完整通过，性能未达标 |
| Linux，4 命令 / 4 分片 | 38.788 s | 449.121 s | 512.676 s | 完整通过，性能未达标 |
| Linux，4 命令 / 8 分片 | 49.353 s | 429.551 s | 525.382 s | 完整通过，性能未达标 |

本机两轮使用相同源码摘要，未观察到其他重测试套件竞争；期间继续进行只读分析与文档整理。两轮各执行普通/race **1608 个顶层入口、4764 条父子测试结果**；基线每阶段 **1597 个入口、4707 条结果**的通过/跳过状态逐项一致。两轮最大单个子进程 RSS 分别为 1155743744 bytes（pre-commit），这不是并发进程内存之和。当前两轮原始报告为 `/tmp/easycode-hook-full-1`、`/tmp/easycode-hook-full-2`，基线 4707 条结果逐项对照均无缺失、无状态变化。历史轮次见 [local-verification.json](local-verification.json)、[coverage-comparison.json](coverage-comparison.json)、[baseline.json](baseline.json)。

Linux 对照基于 `32efe87`；这些属于 Hook 编码复用前的历史对照，不代表当前实现。对照均为独立原生 runner，不假定不同 VM 的绝对速度完全一致。8 命令 / 8 分片在 300.258 秒触及预算，未跑完。完整配置、资源、慢测试和运行链接见 [ci-low-shards.json](ci-low-shards.json)、[ci-v4-complete-slow.json](ci-v4-complete-slow.json)、[ci-v4-timeout.json](ci-v4-timeout.json)。

## 根因证据与已实施处理

1. **严格解码重复扫描和分配。** 原 `UnmarshalRecord` 重复执行基础校验、严格解码与两套形状检查，嵌套字段又经 `ObjectMembers` 重复验证及物化。现用调用局部的已验证字节范围减少重复工作，纯校验不物化数组；最终强类型解码仍用固定 Sonic 配置。保持 Unicode、重复/未知字段、null、必需字段、自定义 decoder、未知字段优先级和公开返回值所有权。两个大预览 fixture 的 race 累计分配从 1330.34 降至 269.45 MB，进程时间从 11.150 降至 3.056 秒；见 [targeted-final.json](targeted-final.json)、[preview-after-alloc.txt](preview-after-alloc.txt)。
2. **Session payload 重复编码。** 经用户补充批准，`EncodeRecord` 复用 checksum envelope 已编码字节，计算原 SHA-256 后附加唯一当前 checksum 字段，避免对相同大 payload 再编码。全部校验及完整 record 大小检查保留。相同大 record/batch race profile 从 34.556 降至 23.804 秒，累计分配降至 2453.88 MB；见 [session-encoding-profile.txt](session-encoding-profile.txt)。
3. **race 放大频繁编码的内存成本。** 原生 Linux 的输入重写恢复 profile 为 39.19 秒、46.04 CPU 采样秒，累计分配 12761.37 MB，其中 Sonic 编码栈池为 10546.50 MB（82.64%）；TSAN 内存范围检查占 CPU 样本约 49.93%。嵌套 `MarshalJSON` 和重复校验继续触发编码。Sonic race 路径额外用标准库读取值以检测竞态，Go race 又会随机丢弃池对象，放大频繁申请编码栈的成本。Hook 编码复用后同一 Linux 矩阵累计分配为 9478.08 MB（降低 25.73%），采样时长为 29.99 秒；见 [优化后原生 profile](linux-hook-after.txt)。本次完整性能验收尚未结束。见 [原生 Linux profile](linux-hook-profile.txt) 与 [本机 Provider profile](hook-encoding-before.txt)。alloc_space 为累计分配；CPU cumulative 含祖先重叠，不能相加。
4. **分片必须有界，不能只增加并发。** GOMAXPROCS 单独调整的五轮对照未显著改善重矩阵；Linux 更多分片也没有稳定收益。当前 runner 在普通/race 各自枚举实际 Test、Example、Fuzz seed，确定性生成互斥完备清单，每阶段保留并复用已链接二进制，避免每个分片重新链接。默认候选为 Darwin 最多 8 个活动命令 / app 16 分片，Linux 最多 4 个活动命令 / app 2 分片，每进程 2 并行；Linux 默认配置尚待完成性能验收。没有证据表明慢路径来自业务死锁，现有证据支持重复工作、内存与 race 开销及并发放大。

`make test`、`test-race`、`test-all`、`verify` 共用同一执行器；普通/race 不受外层 `make -j` 影响而无界叠加。hooks/CI 执行完整 `make verify`。运行失败、超时或取消由唯一 owner 停止派发、取消并回收已启动进程，保留日志和未完成范围；直接执行二进制保留标准 Go 的所选 GOROOT/PATH、包工作目录和 TestMain 语义。诊断 profile 是独立手动模式，不代替默认质量门。

## 回归与契约核对

- 原业务测试未删除、合并、跳过或弱化；没有修改 GORACE，也没有 race 专用替代 codec。447 个固定 testdata 文件与 main 逐字节一致。[测试审核](test-audit.md)、[fixture-integrity.json](fixture-integrity.json)。
- scanner differential fuzz 首轮 1261464 次；补充 10001 层边界后的最终轮 18721 次、总语料 289，通过。codec 固定乱序两轮 race 通过。[fuzz-final.txt](fuzz-final.txt)。
- Session 新编码与原 Sonic 整体编码逐字节表格对照、完整 16 MiB / 超一字节边界通过；fuzz 30 秒共 819146 次、122 份语料通过。固定 golden、双 Provider native/cache、uninterrupted/restored 请求、checksum 和 fingerprint 由完整普通/race 继续验证。
- runner 13 项回归及乱序验证通过，覆盖完整/互斥清单、不同构建标签、编译二进制复用、选中工具链传递、并发上限、失败停止排队、取消、强制升级和真实后代/孤儿回收。真实 race 用 15 秒预算触发取消，在 15.058 秒以预算错误退出，临时二进制目录清理，无本次残留测试进程。[runner-shuffled.txt](runner-shuffled.txt)、[cancellation-final.json](cancellation-final.json)。
- Linux 首轮暴露两个测试 fixture 的时序假设：关闭顺序测试改用已有 Stream started 信号建立前置条件；取消 fixture 在清理后返回 `ctx.Err()`。原关闭 deadline、错误码与六项顺序断言保留，相关三项测试 30 轮乱序 race 通过。另修正 runner 的 GOROOT/PATH 传递，CLI 离线嵌套构建已随 Linux 全量对照通过。历史失败证据保留在相应 `ci-*.json`。
- 当前 `make verify` 包含分支策略、gofmt、vet、Staticcheck、架构守卫及全量普通/race；`make -j8 test test-race` 的阶段互斥已验证。workflow 经 actionlint 1.7.12 校验通过。全部 OpenSpec strict 校验 **64 项通过、0 失败**；主规格已同步已批准范围；`git diff --check` 通过。

## 尚未完成与平台限制

需选定可复跑且兼顾内存的默认参数，并在最终源码上重新取得连续两轮低于 300 秒的完整质量门及原生 Linux 证据，之后才能完成任务 3.4、5.1、5.2、5.4。当前不可声称 CI 五分钟目标已实现。

Darwin 覆盖现有真实 sandbox/Exec 集成；Linux 已真实执行上述完整对照。Windows 未原生执行，POSIX 进程组回归只在 POSIX 运行，本次不扩大产品平台支持承诺。变更保持未归档，未创建 PR。

额外预览采样 [43917045557](https://github.com/cutejiuges/easycode/actions/runs/43917045557) 曾长时间没有进入平台队列、取消 API 返回 409；平台后来完成执行，已下载真实 profile，见 [linux-preview-profile.txt](linux-preview-profile.txt)。此结果来自 Hook 优化前的提交，不能作为新实现验收。重复触发的 [43918045098](https://github.com/cutejiuges/easycode/actions/runs/43918045098) 已确认取消。

## 进程树内存测量与后续调度

用户追加内存排查后，以 0.25 秒间隔采样整个测试进程树：原 8 命令配置为 246.440 秒 / 3086.4 MiB 峰值 RSS；只降为 6 命令为 252.368 秒 / 3633.0 MiB；分离构建上限并限制两个高 RSS 包交错执行后为 257.232 秒 / 3038.7 MiB。主要峰值来自大包同时膨胀，单纯降低总进程数不能保证降低峰值。上述测量期间另一 worktree 运行 race，耗时不得视作隔离验收；RSS 之和会重复计算共享页。完整数据见 [memory-comparison.json](memory-comparison.json)。

新增调度回归共 15 项通过，验证重包限流仍允许轻包执行、无遗漏、失败停止派发、最终回收、硬链接保留及跨文件系统回退。七个独立 Hook/Stop 矩阵的子用例现使用既有包内并发上限；两轮固定乱序 race 125.817 秒通过，所有断言及单用例内部崩溃点顺序保留，见 [isolated-matrix-race.txt](isolated-matrix-race.txt) 和 [审核映射](test-audit.md)。新调度及子用例并行的完整普通/race 与原生 Linux 验收仍待完成。
