## Context

动机见 [proposal.md](proposal.md)。本轮从最新 `origin/main` 的 `b0387c4` 开始；工作区原为干净的 detached HEAD，已建立 `fix/test-execution-performance`。以下为提案阶段诊断；最终实现与验收见 [verification.md](verification.md)。

已验证的事实：

1. `make test` 只运行普通测试，`make verify` 顺序执行普通与 race；pre-commit、pre-push、Linux CI 都调用后者。`TEST_TIMEOUT=20m` 是失败上限，不是正常用例固定等待。
2. [main CI 43876390977](https://github.com/cutejiuges/easycode/actions/runs/43876390977/job/133330446870) 的验证步骤为 13 分 18 秒：app 普通 69.132 秒、race 682.821 秒；race 的 runtime/session/两个 Provider 包各约 165–198 秒。不能把所有问题归到 app 的少数新功能。
3. 本地为 Darwin/arm64、Go 1.24.13、10 核、32 GiB。诊断时另一 worktree 正在运行完整 race，未终止其进程，当前数据不能称为隔离基准。
4. 对同一已编译的 race 二进制，顺序执行 Stop 压缩与七工具恢复两个矩阵，`GOMAXPROCS=10/2/4/1/10` 分别为 `70.584/72.088/68.343/81.295/69.360` 秒，每轮均通过相同 30 条父子测试。限制 CPU 数本身没有明显收益。
5. 对 `post_hook_failure_cancelled.jsonl` 与 `post_hook_input_limit.jsonl` 两个预览子测试采样，进程共 11.150 秒、退出 0，累计分配 1330.34 MB。其中 704.82 MB 来自 Sonic optdec parser pool，236.86 MB 来自标准库 JSON decoder refill，108.34 MB 来自 RawMessage 拷贝；`UnmarshalRecord` 累计关联 1158.59 MB。累计分配不是峰值 RSS，调用祖先的 cumulative 不能相加。
6. 本地依赖源码显示 Sonic optdec 每个新 parser 分配约 1 MiB padded buffer 和 1 MiB node buffer；Go 1.24.13 `sync.Pool.Put` 在 race 下约四分之一的调用丢弃对象。这解释了 arm64 中频繁解码的分配放大，不能直接假定 amd64 使用同一 decoder backend。
7. `UnmarshalRecord` 先 `ValidateJSON`，再经过 `UnmarshalStrict` 的重复校验与非 required 形状检查，然后再次 required 形状检查。形状遍历中的 `ObjectMembers` 又验证和物化字段。大文本被反复扫描/复制，是当前可直接优化的共享调用路径。
8. app 有 237 个测试函数声明、20 处 `t.Parallel`、30 处 `t.Setenv`，没有 `time.Sleep` 调用。完整计时中的慢测试大多是有限矩阵累计，不是无限等待；本轮抽样的 mutex 累计仅 17.43 ms，block 主要是测试父子等待，尚无死锁证据。

已有完整套件 profile 和断言解释保留在 `../archive/2026-11-15-add-runtime-effort-control/regression-2026-11-15.md` 与 `../archive/2026-11-15-add-pre-tool-use-input-rewrite/regression-2026-11-15.md`。本轮紧凑证据见 diagnosis.json，原始临时输出路径也记在其中；不把大型二进制/profile 加入仓库。

2026-11-18 用户明确接受当前耗时压缩结果，并要求本轮收尾后将总时限由 300 秒调整为 360 秒。随后用户明确批准保留全量普通测试、按并发风险精简 race，并移除默认总耗时硬限制。以下目标以最新批准范围为准；历史诊断报告保持原始预算与失败状态，不把旧超时记录改写为通过。

## Goals / Non-Goals

**Goals:**

- 最终 `make verify` 运行全量普通测试与审核后的必要 race，报告完整和分阶段耗时；不再以固定总时限判定成功。
- 优化共享重复工作，建立可定位回归的耗时与执行集合报告；完整检查项及业务断言保持。
- 限制活动进程总量，隔离环境敏感测试，明确失败、取消、超时与资源回收。

**Non-Goals:**

- 不以测试代码占比或行数为删除指标，不关闭 race 或删除真实并发和进程生命周期覆盖，不更改 GORACE 以减少检测工作。
- 不改变业务协议、固定 fixture、JSON 严格规则、操作级 deadline 或生产调度。
- 不修改 Go/Sonic 的全局池、依赖缓存或第三方源码，不增加仅 race 构建使用的宽松替代 codec。

## Decisions

### 1. 先消除重复解析，再确定调度参数

在 `internal/codec` 内部统一一次调用所需的严格校验与字段遍历。`UnmarshalRecord` 直接使用 required 模式的形状检查，避免先执行一套非 required 检查再重复执行 required 检查。一次校验过程中可以复用已验证的局部字段视图，避免 `ObjectMembers` 对相同子树反复做完整验证和 Sonic map 解码。

所有复用限定在当前调用中，不创建包级可变缓存。最终 typed 解码继续使用 Sonic 固定配置；自定义 decoder 和 RawMessage 的边界责任不转移。公开入口仍验证未受信任输入，内部已验证路径不能成为可由业务层调用的跳过校验接口。对外返回的数据继续具备原有所有权与拷贝语义。

为字段扫描选择单次严格遍历或已验证字节范围索引，避免为每个字段复制整个子树。若新增内部扫描器，必须覆盖 JSON 语法、转义、重复键、深度、数字、尾随值、Unicode，并做 differential/fuzz 验证；不能仅用更快但宽松的解析器替换。

替代方案：只调低 `GOMAXPROCS` 已被本轮对照否定为主要解法；扩展 timeout 不减少工作量；更换第三方库或修改其全局池会扩大范围。当前方案优先减少调用次数和临时数据，保留项目既定 JSON 边界。

### 1.1 复用 Session checksum envelope 的编码结果

2026-11-17 用户补充批准此内部扩展并继续 apply。Linux 原生验收仍超时，单独对 `TestInputDraftBudgetIncludesCanonicalEnvelopeAndNewlines` 采样发现约 44% CPU 在 JSON 压紧路径、累计分配约 3.35 GB；`EncodeRecord` 先通过 checksum envelope 编码 payload，再编码最终 Record，race 下 Sonic 同时用标准库检测数据竞争，两次调用重复处理同一大 payload。

保留 `validateRecord` 的全部拒绝规则和公开接口，将 checksum envelope 作为单次序列化来源。计算 SHA-256 后，在该完整 JSON 对象的结尾添加当前契约唯一的末尾 checksum 字段，避免再次编码已序列化的 payload。checksum 仍是原 envelope 的散列；新增后缀只包含固定字段名及 64 位小写十六进制值。最终仍检查完整 record 大小。读取路径、draft 验证、durable 生命周期、字段顺序、omitempty 与持久化词汇保持。

必须用固定 golden、带父 thread/turn 可选字段与 escaped/Unicode/嵌套 RawMessage 的表格测试、受限 fuzz 验证，与保留在测试中的原 Sonic 整体编码路径逐字节等价，并校验 checksum 相同及输入所有权。不得更改 Sonic 的 race 检查、降低 record/batch 边界、信任未经校验的公共输入或增加历史格式。`recordChecksum` 与写入方共享同一内部 envelope 编码函数，避免形成两套字段映射。

### 1.2 在 Hook 值对象内部复用单次编码

2026-11-17 用户批准更新此范围并继续 apply。原生 Linux 输入重写恢复 profile 的累计分配为 12761.37 MB，其中 Sonic 编码栈池为 10546.50 MB（82.64%），TSAN 内存范围检查占 CPU 样本约 49.93%；本机 Provider 恢复矩阵也出现约 14.8 GB 累计分配。证据见 `linux-hook-profile.txt` 与 `hook-encoding-before.txt`。这些是累计分配与采样占比，不是峰值内存，也不能将 cumulative 祖先相加。

范围限定为 HookToolInput、HookInputEffect、HookFinished 内部的重复编码。HookToolInput 的校验与 Digest 通过私有函数复用一次输入编码；HookInputEffect 的校验同时复用这些字节及 digest；HookInputEffect、HookFinished 在自身嵌套编码中先取得子对象的合法编码字节，再以局部 RawMessage 写入同一 Sonic envelope，避免父层多次回调子对象的 MarshalJSON。所有校验和大小限制保持原含义，错误分类、字段顺序、escaping、digest 与公共接口不变。

复用只存在于当前调用，不持久化缓存、不创建全局状态，不改变 Sonic 或 Go race 的检测机制，不更换 encoder/backend。新增表格对照和受限 fuzz，保留原 Sonic 嵌套编码结构作为测试参考，逐字节比较输入、effect、finished 与 digest；覆盖三种能力、Pre/Post/Stop、非法值、Unicode/HTML/转义、边界及返回字节所有权。后续再次执行固定 fixture、双 Provider 恢复/cache、完整普通/race 和原生 Linux 验收，不能用单项 profile 改善代替完整验收。

### 2. 使用同一执行入口管理测试阶段与有界分片

保留 `make test` 为普通测试、`make test-race` 为 race，增加清晰的两阶段完整测试入口供 `make verify` 复用。完整质量门不受 `make -j` 影响而将普通/race 两阶段无界重叠。正常构建缓存允许复用，完整验收通过 `-count=1` 强制测试执行。

先对优化后的重包计时，再设置保守的活动测试进程上限和每进程并行度。重包优先按顶层测试在独立进程中分片，保持子测试、TestMain、真实 helper、环境修改和资源 owner 的原有语义。对 app 不批量添加 `t.Parallel`。只对逐项审核、各自持有目录/Session/HTTP 服务/runner 且不修改进程环境的 Hook 恢复、Stop 压缩和 Stop 上限矩阵子用例，复用既有包内并行上限；同一恢复用例内的崩溃检查点继续顺序执行。轻包仍使用标准 Go 测试执行方式。

分片按当前平台实际枚举的 Test、Example、Fuzz seed 入口生成确定性清单，精确匹配名称；新测试自动加入，清单互斥且完备。不使用会静默遗漏新测试的手写正向名单。编译工作复用当前阶段枚举时链接的二进制，以 Go 的 exec 包装器保留文件，再由标准 test2json 执行分片。普通/race 分别构建，路径按完整包目录的摘要隔离；保留包工作目录、TestMain 与参数，阶段结束或失败后清理文件，不把大型二进制加入 CI 诊断产物。不能用多次重复链接抵消分片收益。同一文件系统优先硬链接保留二进制，跨设备或不支持链接时复制，阶段退出统一删除。

构建/静态分析和实际测试分别设置有界 GOMAXPROCS；降低测试并行度不应额外串行化编译。对实测高 RSS 的两个 Provider、Runtime、Session、Tool、TUI 包设置共享的额外并发上限，提前与 app 分片交错，避免尾段同时膨胀。达到重包限额时继续派发可运行的轻包；所选入口仍只执行一次；普通集合全量保留，race 的排除规则另行审计。峰值内存采用测试进程树的 RSS 采样，注明共享页重复计数，不能将其等同独占物理内存或累计分配。

静态检查与普通测试允许在同一调度 owner 和总命令上限内交错，避免完整质量门中的静态分析成为额外串行等待。静态检查单独使用构建 CPU 上限；任何静态失败仍立即停止后续派发并使整体失败，race 只能在普通阶段及静态检查全部成功后开始。计时报告的普通阶段包含该重叠区间，不能把静态命令时间再次相加或从总墙钟中扣除。并行子用例的父测试时间按标准事件的 run/pause/cont/pass 重建，包含等待子用例而排除父测试自身暂停排队，不能将 Go 的父测试 Elapsed=0 误解为零成本。

只有一个调度 owner 启动和回收子进程，输出阶段、包/分片、退出状态、测试事件和耗时；失败或取消停止派发并清理所有已启动的工作。避免引入通用任务框架，脚本/辅助工具只承担本仓库质量门所需职责。

替代方案：把 app 拆成多个生产包会改变依赖边界；对现有进程环境测试开启同进程并行会破坏隔离；所有测试串行会延长关键路径。进程分片保留现有测试语义，同时允许独立控制资源。

### 3. 以断言映射审核测试装配，保留高价值大数据边界

优先移除相同场景中可证明重复的初始化与解码，不先砍测试。若共享 fixture 装配，测试可变状态、私有路径和资源 owner 仍须独立。纯逻辑矩阵可下沉到所属包，但保留两家 Provider、真实 I/O、恢复与副作用边界的代表集成路径，并逐条列出保留测试如何覆盖原断言。

超限 fixture 与大输出测试用于验证真实边界，不能通过降低数据量绕过阈值。Session 固定字节禁止在运行时改由当前 encoder 生成。需要合并的测试在证据中解释原因和覆盖映射；没有充分证据就保留。

### 3.1 按并发风险筛选默认 race

用户已批准此测试契约调整，同时修改 AGENTS 第 7 节与工程主规格。普通测试保持全量；默认 race 使用精确的包名与顶层测试名排除清单，每组列出代码位置、理由及保留的相关 race 覆盖，不使用正向白名单或按名称关键词猜测。所有新入口默认进入 race。清单失效、重复或替代入口被排除必须失败；当前平台不存在的入口须有明确平台理由。报告同时保存完整清单、所选集合和排除原因。

首批审计纯值对象、协议、编解码、静态架构、串行预算及离线 golden 矩阵；不排除真实 writer、共享历史、取消/关闭、lease、故障注入及 stream 生命周期。codec 增加集中并发编解码及所有权回归。插件浏览矩阵沿用同一 helper：普通完整保留文本/策略组合，race 保留两个 Provider 和每种入口的真实启动、交互、恢复及关闭；不得用空桩替代副作用。手动 `make test-race-full` 可执行所有 race 入口。

### 4. 计时覆盖完整流程但不设置默认总预算

参考本地环境为本轮 10 核、32 GiB、Darwin/arm64、Go 1.24.13；Linux 以仓库 GitHub Actions 原生 runner 实际配置为准。工具链与依赖下载属于环境准备，验收计时包含后续编译/链接、静态检查、普通和 race。不使用缓存测试结果报告收益。默认 TEST_BUDGET=0 禁用总时限，正值仅供主动诊断；保留每测试进程 20 分钟超时、业务 deadline、取消及失败清理。CI 移除短总预算和对应 job timeout，沿用平台自身运行上限。

记录阶段和最慢顶层用例，父测试包含子测试时不重复求和；记录实际执行清单、峰值资源和其他 worktree 竞争。最终同一实现连续两轮完整质量门必须通过，并报告精简前后的 race 与完整耗时；Linux 需要原生运行证据，交叉编译不能算性能验收。没有可用 Linux runner 时保持未验证状态，不声称 CI 达标。

## Risks / Trade-offs

- [严格 JSON 行为漂移] → 保留坏格式回归、固定 fixture、双 Provider canonical/native/fingerprint 等价，并对新扫描路径做 differential/fuzz 验证。
- [只改善 arm64 的 Sonic pool 开销] → amd64 单独计时，不把本地 backend 的根因比例推广到 Linux；共享重复扫描和串行矩阵仍分别测量。
- [并发使峰值内存与 FD 增加] → 从有界进程数开始实测，同时记录资源峰值；不能只根据核数盲目扩大并发。
- [分片漏测、重复测或失去清理] → 用自动枚举集合和故障注入测试覆盖分片、退出码、信号、超时及所有子进程回收。
- [新 runner 维护成本] → 保持小入口、使用标准 Go 测试语义，避免独立维护业务测试列表和新的通用框架。
- [目标尚无实现证据] → 当前诊断不能替代通过证据；以最新批准的覆盖契约验收，历史超时不追认成功。

## Migration Plan

无数据迁移。先加入可重复测量与局部性能回归，再优化 codec，最后接入有界调度与统一质量门。任何失败均可回退工程入口或内部优化，现有唯一当前 fixture/协议不变。审阅确认后才能开始 apply；完成后同步工程主规格，归档依照单独归档流程。
