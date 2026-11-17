# 测试执行与内存优化验收

## 当前验收状态

2026-11-18 用户最终批准：全量普通测试保留，race 按并发风险精简，撤销默认总耗时硬限制，验证通过后推送并创建 PR。`scripts/race_policy.json` 登记 161 个精确排除入口及理由；新增入口默认参加 race，手动 `make test-race-full` 不应用清单。补全/插件浏览矩阵保留两家 Provider 和所有入口的真实生命周期代表场景，复用原 helper；新增集中 codec 并发及返回值所有权回归。原普通用例和断言未删除。

执行提交 `6a87a03` 的真实 pre-commit/pre-push 两轮质量门已通过，输入摘要均为 `5aa4a462d7d22e90a5fdacb43dd1778697e94e6bd1597160d362bed223511040`；普通均 1611 个入口、4786 条父子结果，race 均选中并完成 1450 个入口、4383 条结果，无失败或 skip。精简前 4772 条普通结果逐项零缺失、零状态变化；新增 14 条来自代表场景和 codec 并发回归。首次原生 Linux [44134522709](https://github.com/cutejiuges/easycode/actions/runs/44134522709) 在 134.200 秒因清单引用 Darwin 专用必保留入口而失败；普通阶段已通过，race 尚未执行。引用已改为现有跨平台 `TestPreInputRewriteResumeReconcilesEveryBatchBeforeLoadingNewSnapshot`，以两平台真实枚举验证：排除项仍为 161，所选集合未变。最终配置正在重新验证。调度器 20 项回归、集中 codec 两轮乱序 race、64 项 OpenSpec strict 与 actionlint 已通过。[当前计时与覆盖证据](focused-race-verification.json)。

以下为精简前参考记录：已同步 main `b0387c4bb3b833082beb1d077767885ecf46acdd`，执行提交 `18863cb15784dc677fca6ef07c20cadcc402c4f3`，摘要 `84f8c4bceeff66a1a72952677b72e0d8e71062025f9238afcaf5f323aecf9dbc`。两轮本机完整质量门通过 **238.270 / 234.915 秒**；普通/race 各 **1609 个顶层入口、4772 条父子结果**，原始 4707 条结果无缺失，447 个固定 testdata 文件与 main 一致。Linux 在 **360.074 秒**被旧预算取消，未完成 12 个 race 入口，历史失败状态保持。[精简前记录](final-verification.json)。

## 计时与覆盖口径

本机为 Darwin/arm64、Apple M1 Pro、10 核、32 GiB；原生 Linux 为 GitHub Actions amd64、4 核、约 16 GiB，均使用 Go 1.24.13。工具链和依赖准备之外，计时包含测试编译/链接、清单枚举、静态检查、全量普通及所选 race。允许正常构建缓存，实际测试强制 `-count=1`。静态检查与普通测试在同一有界调度中交错，普通阶段已包含重叠区间，不能再相加静态命令耗时。

| 执行源码与环境 | 普通阶段 | race 阶段 | 完整质量门 | 结果 |
|---|---:|---:|---:|---|
| main 独立源码快照 | 150.704 s | 961.605 s | 1114.245 s | 基线通过 |
| f79c12a，本机第一轮 | 63.626 s | 176.392 s | 240.200 s | 通过 |
| f79c12a，本机第二轮 | 63.825 s | 197.446 s | 261.441 s | 通过；观测到其他 worktree 竞争 |
| 81507f6，本轮内存优化 | 62.235 s | 182.589 s | 244.996 s | 通过；当时预算 300 s |
| 18863cb，pre-commit | 64.738 s | 173.359 s | 238.270 s | 通过；预算 360 s |
| 18863cb，pre-push | 62.277 s | 172.469 s | 234.915 s | 通过；预算 360 s |
| 18863cb，Linux 原生 | 64.520 s | 295.509 s（未完成） | 360.074 s | 总预算超时 |
| 6a87a03，精简 race / pre-commit | 65.918 s | 171.593 s | 237.676 s | 通过；无总时限 |
| 6a87a03，精简 race / pre-push | 64.359 s | 171.569 s | 236.102 s | 通过；无总时限 |

精简前两轮测试执行窗口（不含枚举/构建）普通 57.698 / 57.425 秒，race 159.411 / 159.123 秒，合计 **217.109 / 216.548 秒**。原始报告 `/tmp/easycode-budget-full-1`、`/tmp/easycode-budget-full-2`。未观察到其他重测试套件竞争；期间仅进行文档整理和轻量只读核对。

历史 Linux 最短完整对照为 359.663 秒，来自 Hook 复用前的 `32efe87`，不代表最终代码。其后 f79c12a 的 [44119606565](https://github.com/cutejiuges/easycode/actions/runs/44119606565) 在旧的 300 秒预算下超时：普通 89.185 秒、race 已运行 210.835 秒；未出现业务断言失败。不同原生 VM 的观测耗时差异明显，不能只凭核数归因或把一次快运行推广到所有机器。历史配置对照保留在 [ci-low-shards.json](ci-low-shards.json)、[ci-hook-comparison.json](ci-hook-comparison.json)、[ci-resource-comparison.json](ci-resource-comparison.json)。

精简后本机整体时长与精简前相近，不能宣称有显著额外提速。两轮分别观测到其他 `01b8` worktree 的 Runtime/Context/Headless race 和 Session race，均未终止；这些进程不计入本次 RSS 树。测试用例和并发分配的变化同样会影响墙钟，不把差异单独归因于竞争。

## 根因与实现

- 严格解码原先重复执行基础、形状和嵌套对象校验，并物化大子树。`internal/codec` 现在复用调用局部的已验证字节范围，强类型解码仍使用固定 Sonic。两个大预览 fixture 的 race 累计分配从 1330.34 降至 269.45 MB，进程时间从 11.150 降至 3.056 秒。[定向结果](targeted-final.json)、[分配采样](preview-after-alloc.txt)。
- Session 原先为 checksum envelope 和最终 record 两次编码同一 payload；现复用 envelope bytes 后附加唯一当前 checksum 字段，保留校验、字段顺序和大小边界。大 record/batch race profile 从 34.556 降至 23.804 秒。[采样证据](session-encoding-profile.txt)。
- Hook 的嵌套 MarshalJSON 和重复校验反复调用 Sonic。race 下 Sonic 额外读取对象以检测竞态，Go 的池随机丢弃又放大大编码栈的分配。调用局部复用后，同一 Linux 恢复矩阵累计分配从 12761.37 降至 9478.08 MB（减少 25.73%），profile 时长从 39.19 降至 29.99 秒。[原生对照](linux-hook-after.txt)。最新 HookFinished 继续复用相等目标的合法编码字节，Result/Feedback 其余字段仍分别编码；超限 SSE fixture 直接发送原事件，避免整段缓冲副本。两项合并的本机定向累计分配从 22.03 降至 20.73 GiB，耗时从 29.33 降至 28.24 秒。[本轮证据](hook-target-memory.txt)。
- 同时启动重包会叠加内存。单独改变 GOMAXPROCS 或总进程数不能稳定解决。当前默认 Darwin 6 个活动命令、app 16 分片；Linux 4 个命令、app 2 分片；每进程并行度 2，高 RSS 的 Provider/Runtime/Session/Tool/TUI 包额外共享并发上限 2。构建上限单独控制，编译二进制复用，静态检查与普通测试交错；七个独立矩阵只并行各自拥有资源且不修改环境的子用例。[断言及隔离审核](test-audit.md)。

没有观测到支持业务死锁的证据，主要证据支持重复编解码、分配和 race 成本及并发资源叠加。普通阶段没有删除、跳过或弱化原业务断言，没有修改 GORACE、第三方池或加入 race 专用 codec。

## 内存口径与结果

以 0.25 秒间隔采样本次测试进程树的 RSS 之和。共享页可能重复计数，短峰值可能漏采；这不是独占物理内存。pprof alloc_space 是累计分配，不能当成峰值或把调用祖先 cumulative 相加。结束时的小 Go live heap 也不能证明不存在所有形式的泄漏或代表 TSAN 内存。

既有采样中，只把 8 个活动命令降为 6 个，峰值反而从 3086.4 增至 3633.0 MiB；增加重包限额并交错执行后观测为 3038.7 MiB。上述轮次有其他 worktree 竞争，不能声称为隔离的耗时对照。f79c12a 两轮为 2873.4 / 3065.8 MiB；81507f6 本轮为 **3117.7 MiB（约 3.04 GiB）**。最终 360 秒两轮为 **3021.2 / 3109.2 MiB（约 2.95 / 3.04 GiB）**。精简后两轮采样为 **2445.8 / 2603.7 MiB（约 2.39 / 2.54 GiB）**；本次观测较前两轮低，但运行负载不同，不能等同严格隔离的内存基准或保证所有机器同比下降。[内存对照](memory-comparison.json)。

本次 runner 只负责自己启动的进程树，其他 worktree 的旧 `go test -race ./...` 不计入本分支采样且未被终止。测试结束后的资源回收单独核对；系统文件缓存与其他应用占用不等于测试泄漏。

## 质量与契约检查

- 所有原基线测试、Example、fuzz seed 自动纳入两种构建模式的清单，分片互斥且完备，父子结果逐项对照。普通阶段全部执行，race 依据精确清单执行并单列排除理由；固定 fixture、双 Provider native/cache、恢复请求、checksum 与 fingerprint 的普通断言完整保留，必要并发/生命周期仍参加 race。
- scanner differential fuzz、10001 层边界和 codec 乱序 race 已通过；Session 的原 Sonic 整体编码逐字节对照、16 MiB/超一字节及 fuzz 已通过。[scanner fuzz](fuzz-final.txt)、[Session 对照](session-encoding-profile.txt)。
- Hook 新旧嵌套编码逐字节及错误类型对照包含全部统计字段、不同身份、不同合法目标与非法目标；domain 两轮乱序 race 通过，受限 fuzz 30 秒、100174 次执行通过。本轮新增回归曾使用未重建 RunID 的 Post 身份，完整质量门正确拒绝；已修正 fixture 后重跑，没有删除断言。[定向证据](hook-target-memory.txt)。
- runner 20 项回归通过，涵盖清单、二进制复用、工具链传递、总并发及重包上限、失败/取消/超时/强制升级、真实后代及孤儿清理、硬链接回退和计时；新增精确 race 筛选、新入口默认纳入、失效/重复排除、缺失替代覆盖、显式平台范围、手动全量及无默认总预算的回归。实际取消实验在 15.058 秒退出并清理临时二进制。[取消证据](cancellation-final.json)。
- 本次修正两个 fixture 的时序假设，保留原 deadline、错误码与完整清理顺序断言；相关三项 30 轮乱序 race 通过。七个独立矩阵两轮乱序 race 通过。[矩阵结果](isolated-matrix-race.txt)。
- `make verify` 继续包含分支策略、gofmt、vet、Staticcheck、架构守卫、runner 自检及全量普通与经审计的必要 race，pre-commit/pre-push/CI 使用相同入口。actionlint 通过；全部 OpenSpec strict 64 项通过、0 失败；主规格已同步；`git diff --check` 通过。

## 精简前的 Linux 失败与平台限制

360 秒预算的最终两轮本机已通过；[原生 Linux 验证](https://github.com/cutejiuges/easycode/actions/runs/44126561787) 因总预算失败。普通测试及静态检查完整通过，race app 第二分片完整通过（244.185 秒），第一分片运行 265.742 秒后被预算取消，完成 95 个入口、剩余 12 个。没有观察到业务断言失败，不能将未完成部分算作通过。CI 最大单个子进程 RSS 为 1229512704 bytes；未采集 Linux 进程树峰值，不能与本机进程树之和直接比较。此次失败保留原始记录；当前选择性 race 的验证状态见报告开头。Windows 未原生执行，本次不扩大平台支持承诺；变更保持未归档。
