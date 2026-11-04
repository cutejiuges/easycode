# 交互式会话选择器验收记录

日期：2026-11-04。变更：`add-interactive-session-picker`。

## 范围与选择理由

现有 Catalog、连续 lease 恢复和双 Provider 原生历史已经具备可靠接缝；启动选择器解决用户辨认和续接会话的直接问题，复用这些基础而不引入新 Session vocabulary。交互采用 Claude Code sourcemap 2.1.88 的搜索、Ctrl+A、Ctrl+V 与分层 Esc；异步边界参考 Codex `6f8bf683c` 的请求身份和过期结果隔离。参考路径与逐文件取舍见 [design.md](design.md)，参考工程未修改或成为构建依赖。

## 质量门

- `openspec validate add-interactive-session-picker --strict`：通过。
- 最终 `make verify`：通过（退出 0），包含 gofmt、go vet、Staticcheck、架构守卫、全量测试与 race test。
- `git diff --check`：通过。

## 实际终端验收

平台：Darwin arm64，macOS 27.0.1，真实 POSIX PTY，`TERM=xterm-256color`；默认 100×28，resize 至 30×12，包含 `NO_COLOR=1`。通过 `python3 scripts/session-picker-smoke.py` 可重现。使用明确 fake 配置和固定 input-only fixture，不访问外网；没有声称做过 Terminal.app 人工观感检查或 Linux 终端实测。

| 路径 | 应用退出码 | 终端恢复 |
|---|---:|---|
| 搜索 → 预览 → 返回 → 跨目录默认返回 → resize → Ctrl+C | 130 | 通过 |
| 列表 Esc | 0 | 通过 |
| 无色 Ctrl+C | 130 | 通过 |
| 明确跨目录继续 → 同一 Program Chat → 空闲退出 | 0 | 通过 |

核对 termios 的输入、输出、控制、本地模式和控制字符。Darwin 返回时的唯一差异为 `PENDIN=0x20000000`，系统 SDK `sys/termios.h` 将其定义为待重绘输入的内核状态，单独记录并从模式等价比较中排除；canonical、echo、signal 等模式均恢复。PTY 暴露并推动修复了输出 wrapper 丢失 `term.File` 导致 resize 失效，以及关闭通道与正常退出竞争导致错误退出 1；均有回归证据。

## 契约与文档核对

- TUI 仅依赖自身 DTO 与 domain，不导入 Session/Provider/Runtime；app 组合存储、离线 reader 与既有聊天装配。无新依赖、全局 registry、后台 helper waiter 或第二个终端输入 owner。
- 正式/离线恢复共享私有严格校验，下一请求 canonical bytes、native 顺序和 fingerprint 沿用双 Provider golden 回归；预览不构造 Provider 请求。
- 没有新增/改名 Session record、schema version、SQLite 字段、缓存格式或迁移 reader。预览无 journal Sync/Repair/补偿；正式恢复保留连续 lease 和现有 durable 语义。
- 安全英文诊断不传播底层存储或 Provider cause；正文只进入显式预览内存，不写索引、日志或 artifact。终端控制字符安全转义且受预算限制。
- README、架构、Roadmap、pitfall 与实际入口一致；修正列表 repair 误述。未把 Markdown、reasoning 面板、正文检索、fork/worktree 或整个 P5 写成已实现。
- 既有 `session/resume` 与 `session/catalog` 主规格的旧 schema revision/显式版本描述未在本功能中扩张为格式治理；已有单契约架构守卫和实际代码为当前基线，旧文档措辞另行收敛。

## 规格场景到证据

以下路径以仓库根为基准。Provider 矩阵使用仓库内既有固定 golden/current fixture，不以运行时 encoder 生成 Session 验收输入。双 Provider 表示 `internal/provider/openai` 和 `internal/provider/anthropic` 的同名测试文件。

### provider/history-projection

| 场景 | 证据 |
|---|---|
| Project both provider histories without connection configuration | 双 Provider `offline_history_test.go:TestOfflineHistorySourceValidation`；app `TestSessionPreviewCurrentFixturesAreReadOnly` |
| Reject inconsistent native or compaction records | 双 Provider `restore_test.go`、`compaction_test.go` 经 `testRestoreAfterPreview` 同时断言正式/离线拒绝；`testOfflineUsageEvidence` 校验用量和 provenance |
| Cancel an offline projection | 双 Provider `offline_history_test.go:recordCancelContext` 对每个 record 边界取消并断言无正文 |
| Compare uninterrupted restored and previewed requests | 双 Provider `restore_test.go`、`steering_test.go`、`compaction_test.go`、`exec_restore_test.go`、`artifact_restore_test.go` 的既有 canonical request golden、opaque 顺序和 fingerprint 断言 |
| Mutate a preview projection | 双 Provider `offline_history_test.go:testRestoreAfterPreview`；app `TestSessionPreviewCurrentFixturesAreReadOnly` |

### session/preview

| 场景 | 证据 |
|---|---|
| Preview another supported provider without its credentials | `internal/app/session_preview_test.go:TestSessionPreviewCurrentFixturesAreReadOnly`，预览函数无配置/凭据参数，两家 current fixture |
| Reject a busy or replaced journal | `internal/app/session_picker_process_test.go:TestPreviewBusyProcessCrashAndNoRepair`；`session_preview_test.go:TestPreviewDescriptorBoundPathReplacement` |
| Release ownership before showing the result | `session_preview_test.go:TestSessionPreviewCurrentFixturesAreReadOnly` 返回后重新取得 lease；真实子进程崩溃释放回归 |
| Inspect an interrupted tool conversation | `session_preview_test.go:TestPreviewCommittedBatchPrefixesStayReadOnly` 覆盖双 Provider 工具/artifact/压缩的每个完整 batch 前缀；input-only current fixture |
| Inspect a recoverable half-line tail | `session_preview_test.go:TestSessionPreviewCurrentFixturesAreReadOnly` 对每个合法 fixture 附加半行并比较全部 bytes |
| Reject invalid checkpoint evidence | `internal/app/compaction_fixture_test.go` 的 source、summary、非法结构矩阵；双 Provider `testOfflineUsageEvidence` 校验 checkpoint 与失败 provenance 用量不一致 |
| Bound a long multilingual preview | `session_preview_test.go:TestSessionPreviewBudget`；`internal/tui/session_picker_test.go:TestPickerSafeRenderingAndBudget` |
| Exclude opaque and tool content | 双 Provider 恢复矩阵的 ProjectHistory 等价断言与 request opaque 断言；app 合法 current fixture 预览与原生投影对照 |
| Cancel while native validation is in progress | 双 Provider `offline_history_test.go:recordCancelContext`；app `TestSessionPreviewFailureAndCancellationKeepBytes`；owner `TestPickerOwnerParentCancellationWaitsForWorker` |
| Change the journal after preview | `internal/app/session_picker_test.go:TestPickerRecoveryReloadsFactsAndClassifiesMutation`；`session_picker_process_test.go` 在预览后恢复并检验补偿前缀和 seq |

### session/resume

| 场景 | 证据 |
|---|---|
| Resume an explicitly selected root thread | `session_picker_test.go:TestPickerRecoveryReloadsFactsAndClassifiesMutation` normal；`TestPickerLateRealRecoveryReleasesLease`；PTY 恢复后聊天退出 |
| Race selection with another owner | `session_picker_process_test.go:TestPreviewBusyProcessCrashAndNoRepair` 在真实进程占用期间执行 picker 恢复、失败 bytes 不变，崩溃后恢复 seq 单调 |
| Decline a different-directory target | `internal/tui/session_picker_test.go:TestPickerCompatibilityAndCrossDirectory`；PTY 默认 Return |
| Confirm recovery in the current workspace | 同上明确 Continue；`internal/app/resume_e2e_test.go:TestProvidersPersistResumeReplayAndContinueEndToEnd`；PTY 同一程序进入 Chat |
| Cancel before recovery admission | `internal/app/picker_owner_test.go:TestPickerOwnerCancelledBeforeStart` |
| Cancel after a successful recovery before handoff | `picker_owner_test.go:TestPickerOwnerResourceHandoffAndLateSuccess`；`session_picker_test.go:TestPickerLateRealRecoveryReleasesLease` |
| Fail after a legitimate durable repair | `session_picker_test.go:TestPickerRecoveryReloadsFactsAndClassifiesMutation` repair_failure；`session_service_test.go` 的 interrupted compensation/append 故障注入；owner fatal admission 回归 |
| Reject invalid history before repair | `session_picker_test.go` corrupt/mismatch/removed；`session_preview_test.go:TestPreviewRejectsUnknownContractWithoutRepair`；原有 Session 校验/lease 回归 |

### tui/session-picker

| 场景 | 证据 |
|---|---|
| Open the picker with valid configuration | `internal/app/session_picker_test.go:TestPickerDiscoveryScopeAndRefresh`；`session_picker_terminal_test.go:TestPickerTerminalHelper` 与 PTY 脚本 |
| Reject usage before resource acquisition | `cmd/easycode/resume_test.go:TestResumeCLIValidation` |
| Preserve a headless prompt | `cmd/easycode/resume_test.go:TestHeadlessResumePromptKeepsOriginalRoute` |
| Search a bounded mixed-provider list | `internal/tui/session_picker_test.go:TestPickerSearchEditingAndIdentity`；复用 `internal/session/catalog/list_test.go` 的 200/HasMore/排序测试，固定 picker golden |
| Distinguish no matches from incomplete discovery | `TestPickerStaleScopePreviewAndResume`；`internal/app/session_picker_test.go:TestPickerDiscoveryRetriesCancelledReconcileAndKeepsWarning` |
| Reject excessive pasted search text | `TestPickerSearchEditingAndIdentity` 的 Unicode、256 边界、整次拒绝、字素删除、decoded paste |
| Return from preview without losing the selection | `TestPickerStaleScopePreviewAndResume`；PTY 搜索后预览返回 |
| Clear search and then exit | `TestPickerSearchEditingAndIdentity`；`TestPickerNormalExitSurvivesOwnerClosedRace`；真实 Program Escape 0 |
| Ignore repeated confirmation while resuming | `TestPickerStaleScopePreviewAndResume`；`TestPickerResumingDoesNotSubmitBufferedInputToChat` |
| Render hostile metadata and narrow screens | `TestPickerSafeRenderingAndBudget`、`TestPickerSnapshots`、`TestPickerTinyDetailsExposeFullIdentity`；`testdata/picker_*.golden`；真实 PTY resize/NO_COLOR |
| Show compatibility without claiming availability | `TestPickerCompatibilityAndCrossDirectory`；固定正常/窄屏 golden；metadata 中没有 live busy 标志 |
| Ignore an old scope response | `TestPickerStaleScopePreviewAndResume`；`internal/app/picker_owner_test.go:TestPickerOwnerCoalescesAndWaitsForCancellation` |
| Fail a refresh without faking an empty result | `TestPickerStaleScopePreviewAndResume`；`TestPickerDiscoveryRetriesCancelledReconcileAndKeepsWarning` |
| Exit during preview or terminal failure | `internal/app/session_picker_terminal_test.go:TestPickerProgramExitAndOutputFailure`；owner 取消/迟到成功/cleanup failure；PTY Ctrl+C 130 |

## 故障注入与测试边界

- 一部分 `session/testdata/current` fixture 仅证明日志层 ledger/replay 语法，不包含严格 Provider restore 所需的完整 usage。预览测试显式将这些样本归为拒绝分支，断言无正文、无修复、bytes 不变；没有放宽 native decoder 或删除原测试。
- owner 使用受控 channel 验证最大一个在途操作、最新意图覆盖、迟到成功关闭、显式单次接收、父 context 取消后等待 worker、cleanup 错误和 fatal 后停止 admission。正式资源关闭复用已有 shutdown timeout/阻塞 stream/真实 lease 回归。
- 旧 `sessions list`、`--resume`、`--continue`、headless、工具和聊天面板继续通过全量质量门验证；新增代码不增加 Provider capability、Session 格式或 Tool schema。


## 最终完成状态

28/28 项完成，37 个规格场景已映射到上述真实验证。最终 `make verify` 退出 0；`openspec validate --specs --strict` 为 44 passed / 0 failed。四份 delta 已逐份与主规格核对一致，然后使用 CLI 归档到 `2026-11-04-add-interactive-session-picker`。归档时 `--skip-specs` 仅避免重复同步，未跳过验证。

CLI 对 proposal 给出超过 10 个 delta 可考虑拆分的非阻塞提示；本变更保持已确认的单一选择器/预览/恢复功能范围，没有延期实现项。终端实测范围与历史文档措辞边界见上文。

## 2026-11-04 复核更正

上文场景映射是首轮证据索引，不代表所有边界均已被直接断言。后续 `harden-session-picker-review` 发现并修复父取消接收窗口、双层预算及加载/窄屏显示缺陷；补齐另一 writer 在预览后合法追加、原生验证中 barrier 取消，以及真正无日志变更的装配失败。首轮 clean_failure 实际包含输入尾补偿，已更名并增加独立 clean_failure。详见后续复核变更的 verification.md；后续质量门结果以该报告为准。
