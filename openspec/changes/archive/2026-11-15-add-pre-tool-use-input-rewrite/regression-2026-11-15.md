# 2026-11-15 再次回归与测试耗时排查

范围：已批准的 `add-pre-tool-use-input-rewrite`，当前 `feat/pre-tool-use-input-rewrite` 工作区。运行环境 Go 1.24.13、Darwin/arm64。全部命令重新运行，本轮回归没有修改生产代码、业务契约、测试断言或超时；随后按用户明确请求于 2026-11-15 完成归档，见 [归档记录](archive.md)。

本轮覆盖范围内未发现功能失败、数据竞争、业务死循环或死锁；49 项任务保持完成，change 已于 2026-11-15 归档。

## 回归与端到端结果

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| 完整 `make verify`，附 CPU 采样 | 通过，exit 0；1196.635 秒；全部包正常及 race 均加 `-count=1` | [命令与退出码](evidence/reverification-2026-11-15/make-verify-profiled.json)、[实际 Go 命令](evidence/reverification-2026-11-15/go-commands.jsonl) |
| 独立应用包无缓存全量测试 | 通过；命令 157.948 秒，1267 条 pass（包含父测试、子场景和显式 helper 入口） | [命令](evidence/reverification-2026-11-15/app-profile.json)、[逐项耗时](evidence/reverification-2026-11-15/app-normal-test-timings.json) |
| 应用包完整 race | 通过；包测试 857.771 秒，1267 条 pass，无失败 | [逐项耗时](evidence/reverification-2026-11-15/app-race-test-timings.json)、[CPU profile](evidence/reverification-2026-11-15/app-race.pprof) |
| 核心及双 Provider 专项 race | 10 包、随机顺序、重复 3 轮，654 条 pass，exit 0 | [命令](evidence/reverification-2026-11-15/focused-race.json)、[汇总](evidence/reverification-2026-11-15/focused-race-summary.json) |
| 双 Provider 真实 PTY | 重新构建并运行，两个 Provider 各 33 阶段通过 | [驱动命令](evidence/reverification-2026-11-15/pty.json)、[OpenAI](evidence/reverification-2026-11-15/pty/openai-input-rewrite.json)、[Anthropic](evidence/reverification-2026-11-15/pty/anthropic-input-rewrite.json) |
| 契约和差异检查 | change strict、全库 strict 62/62、`git diff --check` 均通过 | [完整结果](evidence/reverification-2026-11-15/contract-checks.json) |

质量门只为分析把 app 的 race 与另外 32 包分开运行，33 包全部保留；未减少断言、跳过测试或改变 race 检测配置。[包集合清单](evidence/reverification-2026-11-15/race-package-manifest.json) 给出拆分前后集合等价；[profile-go.py](evidence/reverification-2026-11-15/profile-go.py) 是本次采样适配器，不接入应用或默认质量门。采样导致本轮质量门按阶段运行，不能把其总耗时当作默认并行 `make verify` 的耗时。

端到端断言包含双 Provider 的真实 Read/Patch/Write/Exec、独立 Hook 信任和普通审批、原目标/命令未执行且 E 确实执行、缺失 Read 资格、base 冲突、symlink 交换、非零 Exec、启动失败、600000-byte artifact、原生配对、后置输入及删除旧配置后的离线恢复。恢复与同一调用重入均不重跑外部副作用。PTY 再次检查 80×24、28×10、8×2、NO_COLOR、Unicode、控制字符、resize、默认拒绝、小屏停用、Ctrl+O 审计/锚点和取消清理。

以上测试使用本地模拟 Provider 服务与合成数据，没有连接商业 API。本轮原生执行环境为 Darwin；Linux/Windows 的此前构建及测试编译证据保持有效，本轮没有新增其原生执行结论。

## 为什么会超过十分钟

慢的是完整质量门中的 race 应用包，而普通全量应用测试约 2 分 38 秒。完整质量门本来就依次执行正常测试和 race；`make test` 只执行正常测试，见 [Makefile](../../../../Makefile#L47)。

实施前留存的应用包基线为正常 **133.683 秒**、race **704.947 秒**，约 11 分 45 秒；因此十分钟以上的现象在本功能之前已存在。历史记录只作参考，不是同负载的受控性能基准，见 [基线来源及摘录](evidence/reverification-2026-11-15/historical-baseline.json)。

本次把完整应用包逐项计时并采集 CPU profile：应用包的 **226 个顶层测试**及其子场景全部结束，未出现某个业务测试一直不前进。最慢的单个顶层测试约 76 秒；耗时由许多有限矩阵累加。本轮没有发现业务死循环、死锁或无退出条件的轮询。

### 1. race 的运行时与内存成本占主要部分

正常 profile 时长 156.43 秒、CPU samples 72.68 秒；race profile 时长 855.87 秒、CPU samples 1291.42 秒。CPU samples 是多个线程的累计采样，可超过墙钟时间，不能直接与单个测试的 Elapsed 相加。

race 的 flat 热点包括 `runtime.pthread_kill` 15.32%、`runtime.usleep` 14.58%、`runtime.madvise` 12.56%、TSAN 的 `MemoryRangeSet`/`__tsan_read`/`MemoryAccessRangeT`，以及调度、GC 和堆内存处理。热点以运行时抢占、内存管理和检测器标记为主，没有指向某个不断执行同一业务逻辑的循环。

证据：[正常 flat](evidence/reverification-2026-11-15/app-normal-flat.txt)、[正常 cumulative](evidence/reverification-2026-11-15/app-normal-cumulative.txt)、[race flat](evidence/reverification-2026-11-15/app-race-flat.txt)、[race cumulative](evidence/reverification-2026-11-15/app-race-cumulative.txt)。Cumulative 行存在调用祖先重叠，不能累加当成独立占比；`_ExternalCode` 无法在此归入单一业务来源。

### 2. 全量 fixture、恢复与大输出矩阵放大了有限工作量

会话预览测试逐一处理当前 fixtures，并检查正常/半行、只读 bytes、lease 释放、离线 HistoryProjector 与真实 native restore 等价；Hook 恢复矩阵逐一覆盖 batch 切点与重复崩溃。这些场景有真实 FD、journal、SQLite、进程和多轮严格校验，不是简单纯逻辑单测。

| 顶层测试 | 普通 profile / 秒 | race profile / 秒 | 本次比值 |
| --- | ---: | ---: | ---: |
| `TestSessionPreviewCurrentFixturesAreReadOnly` | 7.72 | 75.51 | 9.78× |
| `TestInputRewriteBothProvidersRealEffectsAndOfflineRestore` | 8.60 | 48.25 | 5.61× |
| `TestFailureBothProvidersRealHookSandboxAndFrozenRestore` | 12.18 | 47.01 | 3.86× |
| `TestWriteSSEChunksCancellationEOFAndLimitsPrecedeSideEffects` | 3.82 | 46.67 | 12.22× |
| `TestStopCompactionKeepsFrozenPluginCorrectionAfterSourceRemoval` | 3.59 | 34.55 | 9.62× |
| `TestStopCompactionKeepsFrozenFeedbackAndGenuineSkillCorrection` | 3.86 | 34.53 | 8.95× |
| `TestSevenToolTranscriptBudgetAndRestoredRequestForBothProviders` | 3.00 | 32.44 | 10.81× |
| `TestPluginBrowserManualAndRestoredRequestsBothProviders` | 9.21 | 31.31 | 3.40× |
| `TestStopBothProvidersRealRunnerSameTurnAndRestore` | 4.25 | 28.84 | 6.79× |
| `TestHookBothProvidersRealSandboxAndThreeCapabilities` | 12.90 | 28.34 | 2.20× |

上表来自两次采样运行，受系统负载及采样影响，用于定位量级，不是性能预算或严格加速比。完整对照见 [逐项比较](evidence/reverification-2026-11-15/app-test-comparison.json)。本轮新增改写相关顶层测试在普通采样中合计 **11.92 秒，约占包时长 8%**，其余时间来自完整仓库已有场景及共享路径。

严格 JSON 的多次扫描是可测量的优化候选：`UnmarshalRecord` 先校验，再调用同样校验的 `UnmarshalStrict`，随后对字段形状递归检查，`ObjectMembers` 又解码子对象。race profile 中 `codec.UnmarshalRecord` cumulative 为 **47.52 CPU 秒 / 3.68%**，还会间接产生分配和 GC，不能把整个十分钟都归因于该函数。对应源码：[shape.go](../../../../internal/codec/shape.go#L14)、[json.go](../../../../internal/codec/json.go#L93)、[object_fields.go](../../../../internal/codec/object_fields.go#L11)。

### 3. 正常退出的 race 辅助进程还有固定等待

本机 race runtime 的 `atexit_sleep_ms` 默认值为 **1000**。同一测试二进制、`-test.run=^$`、各 3 次的辅助诊断中，默认退出约 **1.02 秒**，`atexit_sleep_ms=0` 约 **0.02 秒**，见 [选项帮助](evidence/reverification-2026-11-15/race-options.txt)、[对照命令与实测](evidence/reverification-2026-11-15/race-exit-delay.json)。

正常退出的 Go race helper 会累积这项开销，例如跨进程 lease 探针；它不能解释被 kill 或通过 exec 替换的所有子进程。此处只做诊断，不把空进程对照当作功能验收。完整质量门 `GORACE` 未设置，保留默认检测配置，见 [实际环境](evidence/reverification-2026-11-15/race-environment.json)。

### 4. 暂停的并行测试不是死锁

逐项 JSON 事件里，部分资源关闭测试收到 `pause`，等串行部分结束后才收到 `cont`，随后正常完成。这是 `t.Parallel` 的调度语义。分析时将暂停与正在执行分开，不能把一直未输出 pass 的测试直接判作卡住。

## 优化边界

本次完成定位及再次验收，不为提速调低质量门、减少 fixture、移除真实进程或放宽 JSON/恢复校验。后续若优化，应优先给共享严格解码的重复扫描/分配建立独立 benchmark，再保持坏格式拒绝、固定 canonical bytes、原生恢复与 race 断言等价；独立 fixture 的有限并行也应先核对隔离、内存峰值和 FD owner。不能以跳过 race 或更改退出等待代替这些工作。

复现本轮采样时将输出放入独立临时目录，保留归档证据原字节：

```sh
rewrite_profile_dir="$(mktemp -d /tmp/easycode-rewrite-profile.XXXXXX)"
cp openspec/changes/archive/2026-11-15-add-pre-tool-use-input-rewrite/evidence/reverification-2026-11-15/profile-go.py "$rewrite_profile_dir/profile-go.py"
make verify GO="python3 $rewrite_profile_dir/profile-go.py"
go tool pprof -top "$rewrite_profile_dir/app-race.pprof"
```

所有命令、包集合、退出码、逐项时间、CPU profile 及 PTY 原始输出保留在本目录证据中。完整质量门已结束，exit 0；主规格未因本轮验证改变，change 已于 2026-11-15 归档。

原始 test2json 轨迹以 gzip 无损保存： [完整质量门](evidence/reverification-2026-11-15/make-verify-profiled.log.gz)、[独立应用采样](evidence/reverification-2026-11-15/app-profile.jsonl.gz)、[三轮随机 race](evidence/reverification-2026-11-15/focused-race.jsonl.gz)。[归档清单](evidence/reverification-2026-11-15/trace-archive.json) 保存原始大小与 SHA-256；[质量门汇总](evidence/reverification-2026-11-15/quality-summary.json) 保留逐包状态和两阶段断言统计。
