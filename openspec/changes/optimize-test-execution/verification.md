# 测试执行优化验收

## 实现与测量范围

从已同步的 `origin/main` `b0387c4bb3b833082beb1d077767885ecf46acdd` 开始，开发分支为 `fix/test-execution-performance`。参考机器：Darwin/arm64、Apple M1 Pro、10 核、32 GiB、Go 1.24.13。计时从 `make verify` 启动到结束，包含静态检查、测试枚举及编译/链接、完整普通和 race；工具链/依赖准备不计入。使用正常 Go 构建缓存，所有实际测试强制 `-count=1`。

最终默认设置为最多 `min(8, 2 × CPU 数)` 个活动命令、每个进程 `GOMAXPROCS=2`/`-parallel=2`，app 在 Darwin 自动分为 16 个互斥分片、其他平台为 4 个，普通/race 顺序执行；其他包同样共享该并发上限。两个阶段分别按实际构建模式枚举 Test/Example/Fuzz，执行集合必须精确匹配。`make test` 是普通测试，`make test-race` 是 race，`make test-all` 顺序执行两者，hooks/CI 保持调用 `make verify`。

## 根因与处理

基线完整验证为 **1114.245 秒**，普通 **150.704 秒**、race **961.605 秒**。主要成本是严格 JSON 的重复扫描、字段物化与多轮形状校验；在 arm64 Sonic optdec 与 race 的池丢弃机制下产生大量临时分配。单独改变 GOMAXPROCS 的五组对照没有显著收益，profile 没有提供死锁证据。该 backend 归因不直接推广到 amd64。诊断期间与隔离基线的竞争条件分别见 [diagnosis.json](diagnosis.json)、[baseline.json](baseline.json)。

`UnmarshalRecord`/`UnmarshalStrict`/`ObjectMembers` 复用当前调用的字段范围，纯验证不物化数组，公开返回值保持独立所有权；typed 解码仍使用固定 Sonic 配置，保留未知字段优先于缺失字段的错误次序、自定义 decoder 与严格 Unicode/重复键/null 规则。超过标准快速检查深度上限的合法 JSON 继续使用既有流式语法语义。没有共享可变缓存或 race 专用替代实现。

调度由 `scripts/test_suite.py` 的单一 owner 管理，失败在 worker 交还执行槽之前停止派发；取消识别独立进程组和同 session 孤儿，处理父进程退出而后代仍持有 stdout 的情况。预算超限返回失败，并保存未完成范围。真实 race 套件用 12 秒预算故意触发取消，在 **12.144 秒**返回明确预算错误，退出后没有遗留本次 Go/测试进程。

## 全流程计时

| 运行 | 普通阶段 | race 阶段 | 总墙钟 | 结果 |
|---|---:|---:|---:|---|
| main 隔离源码快照 | 150.704 s | 961.605 s | 1114.245 s | 通过 |
| 最终参数试跑：8 命令、2 并行、16 分片 | 66.996 s | 167.409 s | 238.204 s | 通过 |
| 首版实现：pre-commit | 65.821 s | 167.239 s | **237.077 s** | 通过 |
| 首版实现：pre-push | 66.950 s | 168.715 s | **239.901 s** | 通过 |

阶段数据包含当前模式的测试枚举/构建。试跑的最大单个子进程 RSS 为 1202601984 bytes，累计子进程 CPU 为 1144.871 秒；RSS 不是并发进程内存总和。试跑过程中无其他 worktree 重测试，只有文档/证据整理与只读检查。4 命令/4 分片及 6 命令/1 并行/12 分片均触及 300 秒预算，不能作为通过结果；具体见 [concurrency-comparison.json](concurrency-comparison.json)。

首版实现连续两轮 hook 验收通过，两轮源码摘要均为 `8a876c089981fdfad940bdcafdc7facee1a727f947d4d3d8852400b874f237fa`，覆盖同一实现提交 `06e0ad3` 的实际代码。每轮均执行完整静态检查、1602 个顶层入口及 4730 条父子结果，普通/race 分别核对清单；原有 4707 条结果的通过/跳过状态完全一致。完整环境、阶段、资源及清单摘要见 [local-verification.json](local-verification.json)，逐项覆盖差异见 [coverage-comparison.json](coverage-comparison.json)。

第二轮测试本身通过，但 SSH 连接在长钩子期间被远端关闭，Git push 以 141 退出；这是传输失败。后续推送使用 SSH 保活重试，继续执行完整 pre-push，在 **240.163 秒**通过后推送成功。Linux 原生 CI [43895186714](https://github.com/cutejiuges/easycode/actions/runs/43895186714) 已触发，发现关闭顺序测试的调度假设，普通阶段通过，race 阶段失败；不作为验收通过证据。详见 [ci-first-failure.json](ci-first-failure.json)。

## 覆盖与专门验证

- 原有业务测试未删除、跳过、移动或弱化，未修改 GORACE。基线每阶段有 1597 个顶层入口、4707 条含子测试结果；最终集合由报告逐项比较。[重测试审核](test-audit.md)说明每个重路径保留的业务边界。
- 仓库内 **447 个 testdata 文件**与 main 逐字节相同，见 [fixture-integrity.json](fixture-integrity.json)。完整质量门保留 native/golden/cache、恢复请求 canonical bytes、fingerprint、有序副作用、真实 lease 竞争和架构守卫。
- 独立 token 参考实现的第一轮差异 fuzz 执行 1261464 次；补充 10001 层深度边界后最终 fuzz 通过，执行 18721 次、总语料 289，见 [fuzz-final.txt](fuzz-final.txt)。codec 用固定乱序种子 `20261116` 执行两轮 race，通过。
- 调度器 12 项回归覆盖完整/互斥分片、Example 无后缀名称、普通/race 不同构建标签清单、峰值并发、失败/启动失败停止排队、外部取消、可控时钟强制升级、独立子进程组与失败父进程的孤儿清理。11 项进程/清单测试另以乱序执行通过，随后新增的构建模式集成回归通过。
- `make -j8 test test-race` 用可观测 runner 验证顺序为 normal start/end、race start/end，没有重叠。
- 两个大预览 fixture 的 race profile：累计分配 **1330.34 → 269.45 MB**，进程时间 **11.150 → 3.056 秒**。七工具恢复＋Stop 压缩两组重测试同样保留 30 条父子结果，乱序复测 **31.549 秒**（原五轮约 68–81 秒）。早期探针存在另一 worktree 的 race，绝对时间只能作为诊断对照，不替代隔离全量验收；累计分配也不能当作峰值 RSS。见 [targeted-final.json](targeted-final.json) 与 [分配 profile](preview-after-alloc.txt)。
- benchmark 保持相同输入和 100 ms 采样参数：race 小对象 536201 → 76367 ns/op，嵌套 12358392 → 326019 ns/op，大文本 434508750 → 25432719 ns/op；嵌套累计 B/op 77799508 → 561003。结果见 `benchmark-before-*.txt` 和 `benchmark-after-*.txt`，单次短 benchmark 存在池采样波动。
- change strict 校验通过；同步后的 **62 项 main specs** strict 校验全部通过；全部 OpenSpec 的最终 strict 验证为 **64 项通过、0 失败**；`git diff --check` 通过。

## 平台边界

Darwin 运行包含现有真实 sandbox/Exec 集成场景。Linux 需要原生 CI 实测，尚未完成时不以交叉编译代替。Windows 未做原生执行；调度器的 POSIX 进程组回归仅在 POSIX 主机运行，本次不扩大产品平台支持承诺。OpenSpec 已同步主规格，变更保持未归档。

## Linux 时序回归与后续验收

首轮 Linux 为 4 核、约 16 GiB、amd64、Go 1.24.13。`TurnStarted` 事件早于输入提交与 Provider Stream；关闭测试立即触发 shutdown 时允许在输入提交前取消，原测试却要求已存在 stream 时间线。改为等待 fixture 已有的 `started` 信号后再关闭，保留原 1 秒关闭 deadline 与全部六条顺序断言，不修改生产行为。两项关闭/强制回收测试以固定乱序种子执行 20 轮 race，通过。

Linux 的 16 分片还放大了多个进程的独立初始化。CI 将比较 4/8 活动命令及 4/8 分片；手动 workflow 提供有界参数和 600 秒诊断预算，但默认及验收阈值始终为 300 秒，诊断超时放宽不算达标。后续同一最终源码的两轮本地与 Linux 验收待补充。
