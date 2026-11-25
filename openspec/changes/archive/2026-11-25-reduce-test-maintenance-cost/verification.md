# 验收与归档记录

## 本地 hook 策略调整（2026-11-25）

用户明确要求取消远端 smoke，并将同一 `make smoke` 强制加入本地 pre-commit。该授权替代原任务 9.3 的远端原生 CI 条件；基础 verify、61 场景、3:2 比例及 4,933 行脚本预算保持。hook 依次执行分支检查、verify 与 smoke，失败即阻止提交；远端只保留 branch-governance 与 Linux verify。smoke 当前仅支持原生 macOS，其他平台明确失败，不自动跳过。

旧提交 `4f05e56` 的 [CI 运行](https://github.com/cutejiuges/easycode/actions/runs/45635748145) 已通过分支检查与原生 Linux verify（659 普通 / 67 关键 race，115.457 秒）；macOS arm64 因托管容量排队，两次运行均取消，未宣称通过。远端 smoke 的取消与本地强制验收是用户选择，不是将未执行结果改写为成功。

临时三例顺序探针已通过：verify 失败返回 7 且不调用 smoke；smoke 失败返回 9；两者成功时各执行一次。探针仅证明 hook 控制流。完整本地 hook 在真实 Git 定位环境下通过：verify 为 686 普通 / 68 关键 race，55.457 秒；原生 smoke 为 61/61 场景、三个构建产物，35.666 秒；hook 总计 91.534 秒，Git config 字节不变。源码摘要为 `d7948ead138c161149245fe767e35d574d5a9cee1660fbba8841052512a4352e`。两份主规格已同步，OpenSpec 严格校验 75/75 通过；按用户修订后的条件，任务 36/36 完成并归回原目录。原始回执只保存在忽略的 `.test-results/test-maintenance-local-hook/`。

## 提交阶段补验（2026-11-25）

用户授权提交并创建 PR 后，真实 pre-commit 暴露计数器自检的 Git 环境泄漏：临时 `git init` 继承 hook 的 GIT_DIR，误将真实仓库 core.bare 改为 true。已恢复原非 bare 配置；在原有自检内隔离 GIT_* 环境，避免临时仓库命令定位到用户仓库，未新增框架或测试矩阵。该关键隔离修复增加 3 行脚本，唯一预算相应由 4,930 显式调整为 4,933，不由运行时自动刷新。带真实 hook 定位变量的最小回归通过（0.189 秒），并逐字节确认真实 Git config 不变；首次提交 `4f05e56` 的完整 pre-commit 已通过（686 个普通入口、68 个关键 race，55.345 秒），该次尚未接入 smoke。

## 归档确认（2026-11-25）

用户在知悉任务为 35/36、最终提交的远端原生 macOS CI 尚未执行后，明确要求直接归档。两份主规格已同步；首次归档时保留任务 9.3 未勾选，不将本地验证替代为远端 CI 成功，也不宣称全部任务完成。归档位置为 `openspec/changes/archive/2026-11-25-reduce-test-maintenance-cost/`，归档当时未执行 Git 提交或推送。下方按阶段保存实际验收事实，早期“保留 change”的决定已由本次明确授权替代。

## 最新决定与 3:2 实现（2026-11-25）

用户先选择保留 change，随后将长期门槛修订为 3:2 并授权继续实现；最终归档决定见上文。下方旧 2:1 的失败记录仅为当时事实，不再是当前目标。

当前 P=88,048、T=58,485，P:T=1.5055:1，满足 `2P >= 3T`；T 上限 58,698，余量 213。相对 main `5b283ea` 的 T=63,333 净减 4,848 行，业务 Go 未改。第二轮净减 2,453：app 844、Provider 214、核心 1,087（含 TUI 7）、TUI 追加 308。Provider testsupport 的 2,097 行全部计入 T；fixture 仍为 323 个 / 5,130,297 bytes。

app 复用成功打开、文件读写、关闭与 ACK 描述符装配，不搬动 cleanup 时点；Provider 保留两家真实 wire/opaque/cache/native 恢复证据，合并固定 fixture 和生命周期装配；核心复用成功 fixture 及完全相同的错误检查，保留特殊分支、提交时序和固定 bytes。TUI 共享 whole/fragmented 多行提交链路，移除已被固定快照及 PTY 承接的普通展开、标签重排和重复高度/overlay 排列，不再声称穷举旧排列。

同一 `make test-ratio` 已前置接入 `make verify`、pre-commit 与 CI；脚本首次总预算按实际 4,930 行锁定，未设置自动刷新。AGENTS 和两份主规格已同步保守风险准入、整数门禁与独立原生交互验收。

计数器代表自检和真实 Makefile 的五个隔离推演均通过：刚好 3:2 才进入 runner；多一行测试、少一行业务、显示舍入接近 1.5000、脚本预算超额均非零退出且 runner 未启动，策略字节不变。在当前快照 P 不变时新增 214 行测试，或 T 不变时删除 321 行业务代码，都会跌破门槛；不是逐包/逐 PR 配额。

最终稳定快照验收：

- `make verify TEST_OPTIONS='--output=.test-results/test-maintenance-3to2-verify-final'` 退出 0：前置规模检查通过，runner 执行 686 个普通入口、68 个关键 race、静态检查和 Python 自检，runner 用时 61.845 秒（比例前置检查另计）。完整现存入口均执行，运行后源码摘要再核对一致。首轮被忽略目录中一次性 refactor.go 未格式化阻断；格式化临时工具后重跑，保留失败回执，未弱化 fmt-check。
- `make smoke SMOKE_OPTIONS='--output=.test-results/test-maintenance-3to2-smoke-final'`：61/61 场景、20 步、三个构建各一次通过，36.586 秒；本轮与跨平台检查并行，不据此声称性能变化。
- Linux/Windows amd64：全仓 34 包测试仅编译通过；Linux 全仓 Staticcheck 通过。Windows 15 个变更包仍有 46 项既有 U1000，与 `5b283ea` 基线的文件、行列及消息逐行一致，无新增。两平台未原生运行，全量手动 race 未运行。
- 正式 CLI 的两家各四轮真实会话、Hook 运行中取消和 CLI 交叉构建沿用上文之前已验证的同一生产源码；第二轮只改测试与工程门禁，不重复外网请求。真实会话产物无凭据，终端恢复且无进程残留。
- `openspec validate --all --strict` 75/75 通过；两份主规格逐要求复核已与 delta 一致，旧 composer 终端测试的当前文档引用已改为实际承接路径，历史 archive 保留当时记录。独立只读复审未发现规模门禁缺陷，diff 检查通过。

脚本总量已用满预算，Go 测试有 213 行余量；必要新增测试仍须说明独立关键风险。实际快照无生产代码修改，临时探针及原始报告全部保持忽略。当时 tasks 9.3 的最终提交原生 macOS CI 未执行，因此首次归档保留该未完成项；后续提交与用户调整见本文顶部。

## 2026-11-25 主线整合与复验

当前分支先保留既有修改，再从 `51c7dc3` 快进至 main 的 `5b283ea`，恢复修改后无文本冲突。合并前备份 stash 保留；本轮未提交、推送或归档。新增 #55/#56 的 SSE 解压、历史回合分支与 UserPromptSubmit 已做语义复审。

| 统计（全平台同一口径） | main 原始快照 | 精简后工作树 |
|---|---:|---:|
| 业务 Go P | 88,048 | 88,048 |
| 测试 Go T（含支持） | 63,333 | 60,938 |
| P:T | 1.3902:1 | 1.4449:1 |
| 距 2:1 需净减 | 19,309 | 16,914 |
| Python/Shell S | 4,604 | 4,924 |

相对合入的 main，累计净减测试 Go 2,395 行，包含本轮新增复审的 195 行：transport 净减 73，domain/hook/session 净减 47，app 净减 76，并在既有历史分支断言中增加 1 行拒绝重新读取变更后文件。去掉重复装配和 encoder 自我 roundtrip，保留 gzip 首帧/关闭、冻结切点与许可、提示门禁无副作用拒绝及输入归属。main 新建测试文件自身由 2,022 降至 1,949 行；这不是整个新增复审范围的净值。未跟踪 testsupport 的 1,634 行全部计入 T，没有扩大业务分母。

`make test-ratio` 如实失败：T 上限 44,024，超额 16,914；脚本暂定预算 4,572，超额 352。fixture 为 323 个 / 5,130,297 bytes，单列不参与比例。2:1、最终脚本预算和 `make verify` 的规模检查仍未完成，不能把下列功能通过当作整体变更通过。

- 原生 Darwin arm64 / Go 1.24.13：`make verify` 退出 0，74.213 秒，692 个普通入口、68 个关键 race、静态检查及脚本自检通过；运行后输入摘要再次核对一致。新 main 增加的四个 race 名称无重复或失效，既有项无移除。该门目前尚未接入比例检查，不与旧源码的 77.132 秒直接推断性能收益。
- 统一 `make smoke`：61/61 场景、20 步通过，37.169 秒，三个构建各一次；包含新 rewind 场景。额外双 Provider 正式 CLI/本地模拟服务的运行中 Hook 取消 2/2 通过，2.021 秒；先观察脚本及子进程 ACK，再 Ctrl+C，验证四个 PID 退出、journal 明确 cancelled/cleanup_confirmed、Provider 请求为零、CLI 退出 0 和 termios 恢复。
- 真实服务：使用记忆指定的凭据及现有 DeepSeek Responses/Messages 配置，在隔离 HOME/工作区中运行正式 CLI/PTY。两家各四轮、共八轮成功，验证默认拒绝、信任后 exit 2/JSON 阻断、返回文本取回编辑、连续对话、rewind 取消与编辑分支、source 全字节不变、prefix SHA、移走 parent 后 cold resume 独立续聊。OpenAI 复用本轮已成功的两轮，仅补两轮；续验 4.918 秒，Anthropic 完整验收 9.498 秒。成功路径退出 0、终端还原；31 个产物扫描无凭据、live CLI 无残留。临时 harness 的提示等待和按键假设失败已修正并保留日志，没有修改产品或新增常驻矩阵。
- Linux amd64：全仓 34 包测试编译、CLI 交叉构建及全仓 Staticcheck 通过。Windows amd64：全仓测试编译和 CLI 构建通过；15 个变更包 Staticcheck 的 46 项 U1000（Session 9、builtin 37）与 `5b283ea` 基线逐字一致，无新增。两平台未原生执行；`-exec=true` 只证明测试编译，手动全量 race 未执行。
- OpenSpec 严格校验 75/75 通过；单 pre-commit、实际 hooksPath 及分支策略回归通过。远端再次只读核实 rulesets 为空，main required status checks 为空；当前未提交快照没有原生 macOS CI 成功记录。

原始回执位于忽略的 `.test-results/test-maintenance-main-*`，规模比较位于 `.test-results/test-maintenance-merged-counts.json`。主规格同步评估：branch-governance 的单 hook 修改已同步；test-execution 的两项修改和三项新增尚未完整同步，其中 2:1 与实际 CI 完成条件仍未满足。tasks 7.1–7.3、9.1、9.3–9.4 保持未完成；归档操作另待按 skill 明确处理未完成项和同步选择，不能把未实现要求写成已完成。

## 2026-11-24 初轮验证

2026-11-24，分支 `test/reduce-test-maintenance-cost`，基线 `51c7dc3`。本轮实现停在 tasks 7.1，未达 2:1，未提交、未推送、未归档。目标仍为长期 `P >= 2T`，不能将下列功能验证当作规模约束已经完成。

## 实际规模

| 项目 | 基线 | 当前 |
|---|---:|---:|
| 业务 Go P | 85,784 | 85,784 |
| 测试 Go T（含测试支持） | 60,682 | 58,482 |
| P:T | 1.4137:1 | 1.4668:1 |
| 距 2:1 需净减 | 17,790 | 15,590 |
| Python/Shell 总量 | 4,572 | 4,892 |
| 固定 fixture | 319 / 5,100,458 bytes | 320 / 5,107,254 bytes |

Go 测试净减 2,200 行。脚本增加 320 行，来自统一 smoke 构建/回执/清理和规模计数及自检；未转移 Go 业务断言，预算仍是未验收的基线值。`make test-ratio` 返回非零，明确报告测试超额 15,590 和脚本超额 320；没有自动改预算或 warning-only 通过路径。按设计仅在达标后前置接入 `make verify`，当前尚未接入，AGENTS 的最终 2:1 条款亦未激活。

## 删除与承接

- Provider 净减 1,119：共用离线恢复、连续 checkpoint、文件/Stop 生命周期、工具 fixture 与 stream 编排；新增 testsupport 1,634 行全部计入 T，各家保留私有 native 接线、fixed bytes、opaque、canonical/fingerprint 和来源拒绝。70 个 compaction 组合缩至 16，40 个工具 SSE 组合缩至 16；顶层 Test 和 race 清单名称保留。两家 post output 伪造来源测试补有效 ThreadID，避免无关错误假通过。
- app 净减 622：headless 接续承接 resume 的只读 preview、真实 UI 历史、新会话隔离和元数据保密；Read→Question→Write 的双 Provider 成功代表补实际 Read 内容和 native call/output 配对。Skill/Plugin 30 组缩至 6 组；加载格式和交互由所属 loader/CLI/PTY 承接。请求记录与 cache/task 查询只共享装配，取消/崩溃/权限边界保留。
- TUI 净减 157：普通展示、滚动、编辑成功排列交由已通过的 PTY 承接；保留 hydration、旧结果、跨会话、审批焦点、取消、关闭和全部固定 golden。
- 核心及 headless 净减 305：Session 自制 reducer 改调用真实 replay，14 个固定坏前缀保留明确拒绝阶段与 journal 不变；artifact/journal fake 共享后仍有 Sync 前不可见和 poisoned 断言。Hook encoder 旧实现镜像改为三能力固定 canonical bytes/digest，明确不再保留开放输入下的旧实现 fuzz 对照。
- 架构守卫净增 3：测试支持不冒充生产包，正式生产依赖禁止导入它；CLI 依赖图确认不含 testsupport/testutil。没有生产代码或公共 API 改动。

本次不声称与旧排列逐断言等覆盖。第二次独立 review 发现的计数分类绕漏和 Read 内容承接缺口均已修正并验证。当前没有足够的已证实重复量能支持再净减 15,590 行；各剩余风险组及重新设计点见 design 的实施复核。

## 验证事实

- Darwin arm64 / Go 1.24.13：现有 `make verify TEST_OPTIONS='--output=.test-results/test-maintenance-verify'` 退出 0，93.511 秒，静态检查、661 个普通入口、64 个正向关键 race 和 Python 自检完成。基线 77.132 秒；本轮同时进行了跨平台编译/静态检查，不据这一耗时差声称性能改善或回退。
- `make smoke SMOKE_OPTIONS='--output=.test-results/test-maintenance-smoke-final'`：17 个脚本入口、20 步、60 个实际场景全部通过，34.790 秒，三个二进制各构建一次，合计 1.625 秒。runner 另用确定探针验证 ACK 后超时合作清理及父先退出后的同组后代清理；新 session 仍由对应 harness/Go owner 负责。
- 比例计数器与现有 runner 的自检通过；未跟踪 Darwin 测试、精确 2:1 边界、移动到支持目录不改善比例、未分类支持/脚本拒绝、预算只读均有代表性检查。
- Linux amd64：全仓 Staticcheck 通过；变更包测试编译通过。Windows amd64：变更包测试编译通过；变更包范围 Staticcheck 有 Session 9 项 U1000，使用同一工具链对 HEAD 基线复核，诊断逐字一致，未新增。Windows 静态检查不声明全通过，不把交叉编译称为原生执行。Linux/Windows 未原生运行，手动全量 race 未执行。
- `make install-hooks branch-policy-test` 通过，实际 hooksPath 为 `.githooks`，剩余 pre-commit 保留分支检查与完整质量门；未改用户 `.git/hooks`。当前规范、安装清单、Roadmap 与 PR 模板已对应单 hook。
- `openspec validate --all --strict`：73 项通过、0 失败；任务为 22/28，整体完成检查仍因规模目标与远端 CI 未完成而保持未勾选。

原始日志、JSON、探针与交叉平台结果保存在忽略的 `.test-results/test-maintenance-*` 或 `/tmp/easycode-test-maintenance-*`，不提交进归档。

## 未完成保障

1. 2:1 未达到；make verify 尚未接入规模硬门禁，脚本预算及 AGENTS 最终条款未锁定。主规格目前只同步已实现的单 hook 部分，完整测试策略同步/完成核对待目标实现。
2. 包含交互迁移的最终提交尚无原生 macOS CI 成功记录，不能按完整 change 交付；未为触发 CI 擅自提交或发布当前未达标快照。
3. 只读核实 GitHub rulesets 为空，main 分支保护的 required status checks 也为空；`branch-governance`、`verify`、`smoke` 均未由远端合并保护强制要求。没有修改托管保护。

后续需先确认更大范围测试分层/共享装配的修订方案及可兑现净成本；保留 2:1 与独立关键风险，不降低门槛或按目录整体删测试。
