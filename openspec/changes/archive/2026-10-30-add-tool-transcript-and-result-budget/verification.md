# 合并验收记录

日期：2026-10-30。环境：Darwin arm64，Go 1.24.13。UI 与预算在同一变更内实现，不拆分延期；change 已于 2026-10-30 归档。

## 实现与参考边界

产品参考 Claude Code 2.1.88 的紧凑工具主线、Read/Search 分组、Shell 展示与展开交互；内核参考 Codex `6f8bf683c` 的身份关联、有界材料、顺序提交与冻结恢复。调研文件与取舍见 [design](design.md)，长期理由见 [ADR-0006](../../../../docs/architecture/adr/0006-tool-transcript-and-result-budget.md)。两个参考工程仅只读，没有添加其依赖或大段移植代码。

实际复用本工程的严格 codec、Provider 原生文本提取、FD 安全打开、artifact 发布/配额、命令头尾缓冲、审批指纹和 durable 提交骨架。命令双流 capture 与搜索文本 capture 分别持有 owner；共享的是同一生命周期语义的包内发布机制。

## 任务证据

以下路径均相对仓库根；同组列出的测试共同覆盖该组任务。

| 任务 | 实现与验证证据 |
| --- | --- |
| 1.1 | `internal/domain/transcript.go`、`transcript_test.go`：封闭文本/调用锚点、顺序位置、复制隔离、无 opaque 推断。 |
| 1.2 | `internal/protocol/tool_activity_test.go`、`transcript_query_test.go`：七工具当前活动、Exec-only progress、严格 decode、上限与身份；terminal 保存引用校验 thread/source/digest 并隔离可变指针。 |
| 1.3 | `internal/tool/result_budget_test.go`、`result_budget_codec_test.go`：单调用/sample/turn 限额、64 call 预留、实际 bytes 计费、耗尽锁定与拒绝非法状态转换。 |
| 1.4 | `internal/tool/readonly_material.go`、`search_execution_test.go`、`artifact_page_test.go`：typed 材料、来源联合、按实际额度 finalization。 |
| 2.1–2.3 | 双 Provider 的 `transcript_test.go`、`steering_test.go` 及 transcript golden；`internal/provider/history_projection_test.go`：text/call/text、追加输入、隐藏 reasoning、原生顺序与 canonical request 等价。真实消费者是 app transcript owner。 |
| 3.1–3.3 | `internal/session/artifact/text_capture_unix_test.go`、`quota_process_test.go`、`quota_validation_test.go`：UTF-8 上限、短写、发布失败、严格 reservation、混合真实进程竞争、崩溃占用、symlink/hardlink/同长损坏。 |
| 3.4 | `internal/runtime/search_owner_test.go`、`internal/tool/builtin/artifact_owner_test.go`、`internal/app/host_lease_test.go`：阻塞 Write/Finish、取消和失败期间不提前释放 capture/store/thread lease。 |
| 4.1 | `internal/runtime/result_budget_test.go`、`internal/tool/builtin/read_test.go`：超额 Read 返回错误，旧完整观察失效，不能用拒绝的 Read 授权覆盖。 |
| 4.2 | `internal/tool/artifact_page_test.go`、`artifact_preview_test.go`、`internal/app/transcript_projection_test.go`：实际页头/转义预算、UTF-8 边界、前后分页及无零进度。 |
| 4.3–4.4 | `internal/tool/builtin/search_execution_test.go`、`internal/tool/search_execution_test.go`、`internal/session/result_budget_test.go`：保存裁剪前原行、limit 与保存覆盖分离、首尾 matches 有界、JSONL 不隐藏被省略正文。 |
| 4.5 | `internal/tool/exec_test.go`、`exec_output_test.go`、`internal/tool/builtin/process_execution_test.go`：实际 allowance 进入审批 fingerprint，32 KiB 输出在 8 KiB 额度下保存，非零 exit 不被保存故障覆盖。 |
| 4.6 | `internal/tool/result_finalize.go`、`internal/tool/result_budget_codec_test.go`、Runtime Patch/Write/Exec 回归：缩短摘要保留真实终态与 candidate，安全 fallback 有界。 |
| 5.1–5.2 | `internal/runtime/result_budget_test.go`、`artifact_scheduler_test.go`、`exec_barrier_test.go`：8 worker 正反完成顺序相同；正文耗尽停止新准入，完成配对及 outputs 后失败；同轮追加不重置账本。 |
| 5.3 | `internal/runtime/artifact_commit_test.go`、`read_observations_test.go`：result Sync 后才授权，outputs finalize 后才激活 Read 观察，失败不重复副作用。 |
| 5.4 | `internal/session/result_budget_test.go`、`current_fixture_test.go`、`artifact_fixture_test.go`、`testdata/current/`：固定 bytes/checksum、必需预算/保存字段、旧 canary/shape 失败关闭。fixture 离线更新，测试不自行生成期望。 |
| 5.5 | `internal/session/exec_fixture_test.go`、`internal/app/artifact_resume_darwin_test.go`、`tool_transcript_e2e_darwin_test.go`：异构组恢复、结果后 outputs 前补偿、对象丢失不改变冻结输出或下一请求。 |
| 5.6 | `internal/protocol/tool_activity_test.go`、Runtime 活动及执行回归：durable 后发布 terminal、progress 不进 journal、headless 不输出内部活动。 |
| 6.1–6.2 | `internal/session/tool_history_test.go`、`internal/app/transcript_projection_test.go`、`transcript_owner_test.go`：entry ordinal/seq/ready 严格连接；缺失/重复/错配拒绝；50 项与 64 KiB 查询；Close 取消并等待 reader。 |
| 6.3 | `internal/tui/transcript_state_test.go`：provisional 恰好一次替换、generation 丢弃迟到页、未提交文本只标 unfinished，恢复不伪造。 |
| 6.4 | 删除旧 `command_history.go`、`command_queries.go`、`command_browser.go`；生产引用扫描、架构测试及 headless golden 验证统一入口与外部 v1 不变。 |
| 7.1–7.2 | `internal/tui/transcript_state_test.go`、`command_detail_test.go`、`snapshot_test.go` 与固定 `testdata/*.golden`：七工具、Read/Search 去重分组、异常断组、Shell 三行结果、无持续 Status。 |
| 7.3–7.5 | `internal/tui/transcript_browser_test.go`、`command_detail_test.go`、composer/input/terminal 回归：Ctrl+O/Enter/Esc、四页缓存、超过四页后的反向分页、切流偏移、审批焦点、草稿/光标隔离、取消与 View 零 I/O。Shell 详情保留完整命令与冻结结果，异步预取保存页；输入召回独立保留完整 100 条/8 MiB 历史。 |
| 8.1–8.2 | 双 Provider 七工具纵向测试及下表固定工作负载；浏览前后 journal/request 不变，恢复前后 canonical bytes、item 顺序与 fingerprint 等价。 |
| 8.3 | macOS 本机真实子进程运行；Linux 无 runner，依任务明确记录未验收项，见平台范围。 |
| 8.4 | 已同步总体架构、tool-system、Roadmap、pitfall、ADR-0006 及 12 项主 specs，核对路径和当前 record vocabulary。 |
| 8.5 | 质量门与契约审查见下文。 |

## 固定工作负载

以下为本次本地脚本化测试记录，不是线上 token 或性能估算。request bytes 是序列化请求正文长度，与 tool preview bytes 不同。

| 工作负载 | preview bytes | request bytes | saved bytes | 保存分页 / 模型额外回读 |
| --- | ---: | ---: | ---: | --- |
| OpenAI 七工具混合（含定点 Read、8 路搜索、批准 Patch/Write、Exec exit 7、拒绝 Write） | 523872 | 2135800（前 5 次请求合计） | 3875936 | 64 页 / 1 次 ReadArtifact |
| Anthropic 相同任务 | 523872 | 2128798（前 5 次请求合计） | 3875936 | 64 页 / 1 次 ReadArtifact |
| Runtime 正文耗尽，8 samples/8 calls，2 条 steering | 2039808 | 不适用：使用 Runtime scripted Provider，不序列化 HTTP | 0 | 0 / 0 |

其余边界工作负载：

- `TestOrderedBudgetIndependentOfEightWorkerCompletionOrder`：8 个 Read worker 正序与反序释放，逐个 preview/预算相等，sample 合计不超过 524288 bytes，成功和预算拒绝均发生，恰好 8 条终态。
- `TestArtifactResumeKeepsNativeRequestAndReadOnlyHistory`：双 Provider 执行 17000000-byte stdout 加 7-byte stderr；stdout 保存 16777216 bytes、状态 partial，stderr 完整保存 7 bytes。回读中部 20 bytes，定点 Read，拒绝未授权对象；宿主检查尾部和 stderr，缺失/损坏后仍能恢复相同下一请求。
- `TestRunTurnToolCallLimitAllowsFinalSampleAndRejectsNextCall`：64 calls 执行后允许最终文本 sample；65 calls 的组在执行前拒绝，0 次执行。Runtime fake 不产生 HTTP request bytes。
- `TestBrowserReversePaginationBeyondFourCachedPages`：向前 8 页再反向 8 页，区间连续回到 offset 0；缓存始终不超过 4 页。真实宿主另验证 UTF-8/转义内容的反向范围。

## 格式与契约审查

Session/native/headless 当前分别为 6/2/v1。没有兼容 reader、可变全局 codec、Provider 反向重建、额外可执行 facade 或新 Session 展示记录。JSONL 词汇为 `session_meta`、`thread_meta`、`turn_started`、`turn_inputs_committed`、`provider_native_commit`、`sample_usage`、`tool_call_ready`、`tool_approval_decided`、`tool_execution_started`、`tool_call_result`、`turn_completed`、`turn_failed`。

新 UI 事件只用于单向展示。源码核对中，构造/getter/View 不执行 I/O；Tool Executor 不依赖 Provider/TUI；查询只通过当前 thread durable 引用授权；搜索保存不会改变扫描范围；stable catalog 与缓存回归、不中断/恢复请求等价测试保留。不存在新增依赖或参考工程构建耦合。

## 质量门

- `openspec validate add-tool-transcript-and-result-budget --strict`：通过。
- `openspec validate --specs`：36 项通过，0 项失败；保留 CLI 的非阻塞长需求提示。
- `git diff --check`：通过。
- `make verify`：通过（退出码 0）；包含 branch policy、gofmt、vet、Staticcheck、全量测试（含架构边界）和 race。

## 平台范围与限制

Darwin arm64 已运行真实 lease 竞争、崩溃释放、占用期间禁止 repair、seq 续写、混合配额竞争、sandbox 前台命令及确定性 Sync/阻塞清理故障测试。Linux 本次没有可用原生 runner，尚未验收 Linux 的实际 FD/文件系统、跨进程 lease、配额和取消故障矩阵；不以交叉编译替代运行证据。仓库现有 `.github/workflows/verify.yml` 在 ubuntu 执行同一 `make verify`，本次没有发起或声称该远程 CI 通过。Exec 仍按既有契约仅 Darwin 可执行。

主 specs 已同步，change 已归档。当前全量 native replay/projection 的内存模型保持不变；分页不为每页复制或渲染全部历史。artifact 读取为保证同 FD digest 校验仍会扫描保存对象，返回正文材料和界面缓存有界。
