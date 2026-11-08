# add-reasoning-transcript 实施验收

日期：2026-11-08。环境：Darwin arm64（macOS 27.0.1），Go 1.24.13 本地工具链；模型请求仅使用 httptest 和合成 key，不访问公网。实施验收阶段未执行 commit、PR 或归档；后续归档结果见本文末节。

## 已实现范围

仅被动展示 Anthropic thinking 和 OpenAI summary_text。主线显示当前活动/等待保存提示，成功提交后撤下；Ctrl+O 统一阅读活动和已保存推理。signature、redacted data、encrypted content、raw、未知扩展、Provider ID 和 phase 不展示；opaque-only 为 unavailable。请求不增加 thinking/effort/budget/summary 或 sequential_cutoff，ReasoningSummary/RawReasoning 仍为 false。

内部唯一事件为 `assistant_reasoning` + Started/Delta/Completed；Runtime 绑定真实 sample_index；`transcript_committed` 明确 input/sample；`sample_resolution` 只在内部 turn failure 表达 not_committed/durable/uncertain。SamplePresentation 绑定 block/native ordinal，不进入 Session/native/checkpoint，没有兼容 reader、迁移或版本字段。main/detail 查询模式必填，先筛选后分页。无新依赖、后台 owner 或 per-delta goroutine。

## 九项缺口证据

| 缺口 | 实际证据 |
| --- | --- |
| 取消/保存结果 | `internal/runtime/reasoning_test.go`：Provider 失败、多块协议失败解析、提交接收门闩中取消后等待真实结果、uncertain、durable 投影失败、已保存工具 sample 后取消不重标；`internal/app/reasoning_e2e_test.go`：取消后的真实 JSONL 事实及 lease 释放 |
| 活动到 durable 身份 | `internal/provider/reasoning_binding_test.go`、`internal/protocol/sample_resolution_test.go`：唯一/边界/来源与单次消费；`internal/tui/reasoning_test.go`：相同正文的不同块、非最新列表 cursor、偏移 100000、连续 commit、延迟/关闭后的回复；慢查询保留当前有界画面 |
| 高频重算 | `TestReasoningProducerAndConsumerReuseCommittedSnapshots`：1 与 10000 次增量，planner/projector/footprint 调用数和 committed 快照不变；控制状态测试在最终输出前处理排队输入、interrupt、shutdown；现有审批控制与三个 modal 焦点回归继续通过 |
| OpenAI 交付矩阵 | `internal/provider/openai/reasoning_stream_test.go`：初始 summary、段快照/part.done、相同重复、冲突、item-only、late done、关闭后 delta、未知/错配引用与输出 index、未知 part 的保真与零泄漏 |
| 多 part 顺序 | 同文件：后段先到、空前段/空中段、不连续 index、UTF-8/空白与双 LF；`reasoning_restore_test.go` 对最终虚拟正文和原生顺序逐字节核对；app 工具循环覆盖文本/工具/推理交错 |
| 两家 completion-only 区别 | Anthropic `reasoning_stream_test.go`：start 初值忽略、signature/redacted 只保留 native；OpenAI item-only 恰好 Started/Completed；`internal/provider/reasoning_test.go`：固定两个逻辑 draft、惰性 UTF-8 分片和逐片取消 |
| 资源边界 | source/scratch 各 16 MiB、4096 节点、1 MiB 元数据在 staging 追加/替换前检查；完整数组由 `codec.CountArrayElements` 预检。每 Delta 16 KiB、完成尾快照 64 KiB、serialized payload 512 KiB；escaping 预检及最大控制字符完成快照回归。Provider/AgentLoop 各 16 项；ChatSession 适配层直接背压。UI 全 sample 64 KiB/50 项，选中项优先，淘汰不影响 80 块校验与提交 |
| 默认体验 | `internal/tui/reasoning_test.go` 与固定 golden：紧凑状态、opaque-only、正常/窄屏正文、OSC/bidi、长围栏、片段原文、r、草稿/paste 隔离、审批/问卷/Hook 优先、准确失败保存状态与下一 turn/退出释放；真实 PTY 8 组合验证 |
| 主契约/宿主 | `internal/domain/reasoning_test.go`、`internal/protocol/reasoning_test.go`、`internal/protocol/sample_resolution_test.go`、`internal/protocol/transcript_query_test.go`：strict 当前 shape、null/重复/未知/跨 kind、旧 shape 失败关闭；headless 共用 projector 与 app 双 Provider × 三模式成功/取消/断管/EOF 矩阵，外部词汇不增加 |

正文虚拟读取的分配回归在 domain 测试中，对大于 1 MiB 的 parts 读取小页不拼接全文。所有 UI 详情与工具/列表沿现有四页缓存，不复制活动全文作为第五份正文。两处队列每项最大 512 KiB，共 16 MiB；发布、绑定、控制投影、宿主访问的处理中 payload 副本上界按八份核对，另计 4 MiB。原生 encoder 的既有预算独立保持。

## 原生恢复、请求与缓存

新增两家 `testdata/reasoning_restore.current.jsonl` 为仓库固定 bytes，测试不以当前 encoder 生成它们。原有 `internal/session/testdata/current` 未修改。未知 part 的类型名计入元数据预算，类型与正文分别保真；显示淘汰后的活动状态由完整协议 open 计数决定，不依赖剩余临时条目。两家 `reasoning_restore_test.go` 核对展示前后及 uninterrupted/restored 的完整 canonical request bytes、native item 顺序、opaque 字段和 fingerprint；Anthropic disabled/5m/1h 与 OpenAI omit/thread 独立比较。原有 cache/request golden、WebSocket full/incremental、restore/steering/compaction 测试保留。app 手动/自动压缩 fixture 返回推理，辅助推理不进入普通活动或 transcript。

## 执行记录

- 分层测试：domain、protocol、provider 全子包、runtime、headless、TUI 和 app 的 reasoning/transcript/compaction 定向测试均已实际执行并通过（exit 0）。
- `python3 scripts/reasoning-smoke.py`：首轮 exit 0，8/8；两家 Provider × 彩色/NO_COLOR × 成功/取消，成功路径按真实 commit seq/item 交接，终端属性全部恢复。保存不确定的 PTY 画面使用固定内部事实注入，真实提交结果的映射由 Runtime 门闩/故障注入验证；不把画面注入声称为真实磁盘 Sync 失败。
- 首轮 `make verify`：exit 2。gofmt、go vet、Staticcheck 和普通功能测试通过；架构测试拒绝 Provider 中直接 JSON decoder，故未进入 race。已将数组扫描移入统一 codec，补 `internal/codec/array_test.go`，架构与相关 Provider 测试复跑 exit 0；没有放宽守卫。
- 最终完整质量门、strict OpenSpec、链接/词汇审计及主规格同步结果在下方完成后记录。
- 末轮前完整 `make verify` 已两轮通过（exit 0，包含全仓 race）；随后补入完整 open 状态、未知 part 类型名预算和复制前校验回归，最终冻结代码再次完整执行。
- 当前 change strict 校验：exit 0，21 requirements/80 scenarios；五份已存在 capability 的 40 个原场景全部保留。修改/新增 Markdown 的 69 个本地链接检查通过，`git diff --check` exit 0。

## 已有 Hook race 基线

提案时 `make verify` 曾在 `TestHookProcessBoundsOutputAndPreservesExitClassification` 的 race 阶段失败：期望 hook_output_limit，实际 hook_outcome_uncertain，stdout=18000、CleanupConfirmed=false。随后单项隔离复跑通过，不能据此覆盖完整门失败；该事实完整保留于 design.md。当前 change 没有修改 Hook 契约、超时、输出断言或生产实现。最终完整 race 的实际结果另记，不以单项复跑代替。

## 平台与流程

真实 PTY 证据限 Darwin；未据本次宿主结果宣称其他平台原生验收。主规格在完整验收后同步；用户后续明确请求归档，归档状态见本文末节。


## 最终验收结果

最终交付代码及固定测试文件在质量门期间保持相同 SHA-256：`9111b9a00bba42f4aeb0bb6ccece671ad475ad6fdaab12de7102adade009b2b4`（排序后的 1010 个源码、测试/fixture 与 Go 模块文件的路径和 bytes）。下列检查均基于最终实现，之后只同步规格与验收文档：

| 命令/检查 | 结果 |
| --- | --- |
| `make verify` | exit 0；分支策略测试、gofmt、go vet、Staticcheck、架构检查、全量普通测试和全量 race 全部通过；app 普通测试 66.826s、race 341.489s，部分未变 package 使用 Go 的有效测试缓存 |
| `python3 scripts/reasoning-smoke.py` | exit 0；最终 8/8。两家真实本地 Provider、彩色/NO_COLOR、成功/取消；含阅读页取消优先、精确 cursor 交接、原文切换、resize、分页按键及终端恢复 |
| `openspec validate add-reasoning-transcript --strict --json --no-interactive` | exit 0；21 条 requirement、80 个场景，40 个既有场景完整保留 |
| `openspec validate --specs --strict --json --no-interactive` | exit 0；同步后 53/53 主规格通过 |
| `openspec validate --all --strict --json --no-interactive` | exit 0；同步后 55/55 通过（53 主规格、2 活动 changes），0 失败 |
| `git diff --check` / Markdown 本地链接 | exit 0；本次修改/新增文档链接均有效 |
| 当前 Session fixture 与 Go 依赖 | `git diff --exit-code -- go.mod go.sum internal/session/testdata/current` exit 0，没有新依赖、数据迁移或历史 reader |

已通过 openspec-sync-specs 同步全部 7 个 capability：新建 `provider/reasoning-display` 与 `tui/reasoning-transcript`，其余 5 个保留未涉及内容；共新增 13 条、修订 8 条 requirement，没有 TBD Purpose 或 delta 操作标题混入主规格。README、能力清单、Roadmap、架构、实际转发预算 pitfall 与任务状态已同步；该处记录实施验收完成时的状态，后续归档结果见本文末节。

最终完整质量门的 Hook/builtin race 包通过，提案阶段的 Hook race 基线问题本轮未复现；没有修改 Hook 实现或弱化断言，也不据通过结果声称已确定原失败根因。保存不确定的真实磁盘故障仍由既有 Session 故障注入测试覆盖，Runtime 新测试核对结果映射，PTY 只验证固定事实的交互投影。


## 归档前一致性复核（2026-11-08）

用户已明确请求归档。复核保留实施阶段的历史命令和 Hook race 基线记录，核对当前源码/fixture 指纹、34 项任务、7 份 delta 与主规格、架构/P5 Roadmap/能力清单/pitfall，以及实际测试文件和 record vocabulary。

本轮仅更正文档漂移：架构当前事件清单补入 `assistant_reasoning` 及实际小写阶段/提交/失败解析字段；Roadmap 当前进度和 Tool 文档准确区分已实现被动阅读与未实现主动推理配置；design 开头标明实施及历史记录的阶段，测试引用使用真实路径。归档完成后将验收链接指向归档位置。不修改生产代码、测试、固定 fixture、依赖或主规格需求。


## 归档结果（2026-11-08）

- schema：`spec-driven`；proposal/specs/design/tasks 均完成，任务 34/34，无未完成项或跳过实现。
- 7 份 delta 与主规格逐 requirement 正文和场景比较完全一致：21 条 requirement、80 个场景。主规格已在实施验收后同步，因此本次使用 `--skip-specs` 避免重复应用，未跳过归档校验。
- 47 个文档引用的内部测试路径/本地验收脚本存在，实际内部 vocabulary 与源码对应；`sample_resolution` 仅属于进程内 failure，Session `TurnFailedPayload` 仍只有 code/message/cancelled。
- 生产代码、测试、固定 fixture 与 Go 模块指纹仍为实施验收记录中的 `9111b9a00bba42f4aeb0bb6ccece671ad475ad6fdaab12de7102adade009b2b4`，本次仅修改状态说明与文档链接。
- 归档前 `make verify` exit 0：gofmt、go vet、Staticcheck、架构边界、全量普通及 race 测试通过，多数未变 package 复用 Go 有效测试缓存；归档前本 change strict 校验 exit 0。
- `openspec archive add-reasoning-transcript --skip-specs --yes --json` exit 0，归档至 `openspec/changes/archive/2026-11-08-add-reasoning-transcript/`，保留 `.openspec.yaml`；没有改动其他 change，也没有执行 Git commit/PR。

归档后复核完成：`openspec validate --all --strict --json --no-interactive` exit 0，54/54 通过（53 主规格、1 个其他活动 change）；21 条 requirement 再次与主规格完全匹配；README、docs 和本归档内 124 个本地 Markdown 链接全部有效，旧活动目录引用为零，归档元数据保留。归档后再次执行 `make verify` exit 0，含架构检查、全量普通及 race（多数未变 package 使用有效缓存）；`git diff --check` exit 0。源码/fixture/Go 模块 SHA-256 与实施验收一致，活动列表已无本 change。

## 推送前完整回归（2026-11-08）

按用户要求重新执行全仓验证，不以此前测试结果缓存代替本轮功能与 race 回归。源码、测试、fixture 与模块文件指纹仍为上述 `9111b9a00bba42f4aeb0bb6ccece671ad475ad6fdaab12de7102adade009b2b4`；本轮仅修正 Roadmap 总览中遗漏的旧状态，将被动推理阅读标为已交付，主动配置继续保留为未实现。

| 命令/检查 | 本轮实际结果 |
| --- | --- |
| `go test -count=1 ./...` | exit 0，全部包实际执行，app 70.585s |
| `go test -race -count=1 ./...` | exit 0，全部包实际执行，app 410.348s；Hook/builtin 通过，已有 Hook race 基线未复现，未改动其实现或断言 |
| `make verify` | exit 0，完整质量门通过；普通/race 的架构检查分别实际执行 2.110s/11.223s，其他包可复用有效缓存 |
| 分支检查、CLI 构建 | `make branch-check branch-policy-test fmt-check vet lint build BIN_DIR=/tmp/easycode-pr-bin` exit 0 |
| Linux 目标静态检查/构建 | 以宿主 Staticcheck 可执行文件在 `GOOS=linux` 下检查 `./...`、`GOOS=linux go build ./...` 均 exit 0；不等同于 Linux 原生运行验收 |
| 六套真实 PTY | `reasoning-smoke.py` 8、`markdown-transcript-smoke.py` 2、`completion-smoke.py` 2、`session-picker-smoke.py` 4、`user-question-smoke.py` 4、`smoke-hooks.py` 2，共 22 个场景，全部 exit 0；脚本均来自 `scripts/`，Hook 脚本使用本轮编译的 app 测试二进制 |
| 规格/文档/契约 | 全仓 strict OpenSpec 54/54；7 份 delta 的 21 条 requirement 与主规格完全一致；相关主规格 198 个既有场景全部保留；124 个本地 Markdown 链接有效；34/34 tasks 完成 |
| 当前数据与依赖 | 对 HEAD 比较 `go.mod`、`go.sum` 和 `internal/session/testdata/current` 无变化；暂存 diff 空白检查通过 |

本轮未发现新增功能或回归问题。模型侧使用本地 HTTP fixture 与合成 key，未访问真实模型服务；真实 PTY 范围仍限 Darwin。保存不确定的交互画面与真实提交结果故障注入证据继续按上文区分，不扩大平台或故障验收声明。提交与推送继续执行仓库 pre-commit/pre-push 的同一 `make verify`，远端 GitHub 检查结果由 PR 单独记录。
