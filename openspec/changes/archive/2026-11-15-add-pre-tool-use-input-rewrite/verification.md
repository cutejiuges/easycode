# PreToolUse 参数改写验收记录

实现日期：2026-11-14；最终质量门：2026-11-15（Asia/Shanghai）。工作区 `/Users/cutejiuge/.codex/worktrees/0fb5/easycode`，分支 `feat/pre-tool-use-input-rewrite`，基线 `8492ec25c489605f7ef6a3b42f15b86842f8717e`。运行环境 Go 1.24.13、Darwin/arm64。

## 范围与选择理由

已完成现有 Patch、Write、Exec 的 PreToolUse 完整规范参数替换。该能力复用当前 Hook、独立普通审批、结果预算和 Session 恢复链路，直接补齐当前 P6 的 block/rewrite 闭环，适合当前阶段形成可验证的完整交付。

采用 Claude 的 `hookSpecificOutput.updatedInput` 和普通批准交互，结合 Codex 的真实工具重新审查、来源语义及输出配对要求。保留 EasyCode 的强类型内核、双 Provider 原生历史与确定性恢复。两份只读参考的版本、源码锚点、SHA-256 和行为差异见 [design.md](design.md)。未修改参考工程或将其用作构建依赖。

O 是不可变原始 ready/native 调用；C 仅为逐项已 Sync 的检查候选；完整 continue 后才选择 E。候选、policy、普通批准、执行、结果和 Post 共用 E。部分候选拒绝不产生 E，不执行 O。真实 started Sync 的准入独立于 Post；正常准入结果披露完整 E，未准入和真实恢复补偿使用有界短说明。artifact/Post stdin 不混入说明。最终 E 等于 O 时仍验证全部证据，模型结果不增加说明。

## 2026-11-15 再次回归与耗时排查

完整质量门重新通过：33 包正常及 race 全部以 `-count=1` 运行；双 Provider 核心专项随机顺序重复三轮通过，Darwin 真实 PTY 两个 Provider 各 33 阶段再次通过。在本轮覆盖范围内未发现功能失败、数据竞争、业务死循环或死锁。没有修改生产代码、断言、超时或默认 race 配置。

完整命令、逐项耗时、CPU profile 和测试超过十分钟的原因见 [再次回归记录](regression-2026-11-15.md)。测试范围等价及汇总见 [quality-summary.json](evidence/reverification-2026-11-15/quality-summary.json)；本轮为采样分开运行 app race 和其余包，总耗时不代表默认并行质量门的耗时。

## 首次实现运行记录

| 检查 | 结果与证据 |
| --- | --- |
| `make verify` | **通过，exit 0**；最终运行 26.224 秒，应用包正常/race 命中上一轮完整成功缓存，调整后的 TUI 快照重新检查。[最终命令](evidence/make-verify.json)、[完整日志](evidence/make-verify.log) |
| 完整非缓存质量门 | **通过，exit 0**；1,039.083 秒，应用包正常测试 148.188 秒、race 873.128 秒。包含分支规约测试、gofmt、vet、Staticcheck、架构守卫、全量与 race。[命令](evidence/make-verify-attempt-2.json)、[日志](evidence/make-verify-attempt-2.log) |
| 首轮 Staticcheck 修复 | 检出测试无效赋值及未消费的旧私有 helper，已清除；未增加抑制项、跳过或弱化业务断言。[失败命令](evidence/make-verify-attempt-1.json)、[日志](evidence/make-verify-attempt-1.log) |
| parser fuzz | **通过**；15 秒目标、4 worker、1,485,272 次执行；[命令/退出码](evidence/parser-fuzz.json)、[日志](evidence/parser-fuzz.log) |
| Darwin 双 Provider 真实 PTY | **通过**，两次完整运行；各 Provider 33 阶段、exit 0；[OpenAI](evidence/pty/openai-input-rewrite.json)、[Anthropic](evidence/pty/anthropic-input-rewrite.json) 与相邻 `.pty.log` |
| Linux/amd64、Windows/amd64 | **编译通过**；CGO_ENABLED=0，CLI 构建与所有 33 包逐包 `go test -c`，共 68 条命令；未原生执行。[命令/退出码](evidence/cross-compile.json)、[日志](evidence/cross-compile.log) |
| OpenSpec | change、六份主规格、全库 strict 全部通过（62/62：60 份主规格、2 个活动 change）；实际命令和 exit code 见 [openspec-validation.json](evidence/openspec-validation.json)、[日志](evidence/openspec-validation.log) |

可复现的 parser 与 PTY 命令（本地模拟服务，无真实 API）：

```sh
go test ./internal/extension/hook -run '^$' -fuzz '^FuzzPreInputReplacement$' -fuzztime=15s -parallel=4
go test -c -o /tmp/easycode-pre-rewrite-app.test ./internal/app
python3 scripts/input-rewrite-smoke.py /tmp/easycode-pre-rewrite-app.test --output-dir /tmp/easycode-input-rewrite-pty-recheck
```

PTY 覆盖 80×24、28×10、8×2、NO_COLOR、Unicode、控制字符、resize、默认拒绝、独立信任、实际 E 批准、Ctrl+O 中 O/C/E 审计、锚点及取消清理。测试同时核验 journal、原目标未写、E 目标确实写入和最终退出，不能仅凭终端画面宣称成功。显式 PTY helper 在普通测试中不启动真实终端，已由上述脚本实际执行。

## 证据索引

下列业务断言均在最终全量正常/race 质量门执行；U4 为单独运行的真实终端验收。所有网络使用本地模拟 HTTP/SSE/WebSocket，不使用真实 API key。新 Go 测试文件列出其完整测试入口；复用的既有骨架保留原有断言。

| 索引 | 测试来源 | 入口 |
| --- | --- | --- |
| D1 | [internal/domain/hook_input_rewrite_test.go](../../../../internal/domain/hook_input_rewrite_test.go) | `TestHookReplacementRequiresCompleteCurrentShape` / `TestHookReplacementCanonicalExpansionAndExactBoundary` / `TestHookRewriteGettersAndDigestDoNotAlias` |
| D2 | [internal/domain/hook_input_effect_test.go](../../../../internal/domain/hook_input_effect_test.go) | `TestHookInputEffectsVerifyChainAndOwnTheirInput` / `TestPreFinishedRequiresEffectAndRejectsEffectsOnOtherEvents` |
| P1 | [internal/extension/hook/input_rewrite_test.go](../../../../internal/extension/hook/input_rewrite_test.go) | `TestPreOutputHasOneCompleteStandaloneReplacement` / `TestPreOutputCanonicalExpansionIsNotRawOutputOverflow` / `FuzzPreInputReplacement` |
| P2 | [internal/tool/builtin/hook_input_rewrite_test.go](../../../../internal/tool/builtin/hook_input_rewrite_test.go) | `TestHookOwnerPassesTypedReplacementAndRejectsParserMismatch` |
| T1 | [internal/tool/input_rewrite_test.go](../../../../internal/tool/input_rewrite_test.go) | `TestInputSelectionKeepsAllNineDirectCalls` / `TestRewriteInvocationRetainsIdentityAndFacadeProfiles` / `TestInputSelectionSeparatesCheckingRejectionAndFinalSelection` |
| T2 | [internal/tool/input_rewrite_output_test.go](../../../../internal/tool/input_rewrite_output_test.go) | `TestInputRewriteResultUsesActualAdmissionAndOrigin` / `TestInputRewriteBudgetPreservesFullNoteAndRejectsBeforeAdmission` / `TestInputRewriteDoesNotTrustLabelsOrHideActualInputAsRecovery` / `TestSelectedInputCannotClaimSuccessOrUncertaintyWithoutAdmission` / `TestUnchangedAndRejectedInputsKeepExistingPreviewBytes` / `TestRewriteProvenanceFitsAllEightMaximumIDs` |
| R1 | [internal/runtime/hook_input_rewrite_test.go](../../../../internal/runtime/hook_input_rewrite_test.go) | `TestPreRewriteThreeCapabilitiesUseOnlySelectedInput` / `TestRewriteReentryBindsActualIndexesAndRetires` |
| R2 | [internal/runtime/hook_input_rewrite_boundaries_test.go](../../../../internal/runtime/hook_input_rewrite_boundaries_test.go) | `TestPreRewriteLateCancellationPreservesDefiniteEffectAndFinalGate` / `TestPreRewriteLowBudgetStopsBeforeCandidateAndUsesSingleTicket` / `TestPreRewriteDurableInstallAndSyncFailuresPoisonBeforeOrdinaryEffects` / `TestPreRewriteOrdinaryDenyAbortAndCandidateFailureUseShortUnadmittedNote` / `TestPreRewriteCancellationAfterIntermediateSyncKeepsCandidateUnselected` |
| R3 | [internal/runtime/hook_input_rewrite_recovery_test.go](../../../../internal/runtime/hook_input_rewrite_recovery_test.go) | `TestPreRewriteRecoveryFromFixedCrashCutsNeverRerunsAndPreservesFrozenBytes` |
| S1 | [internal/session/hook_input_rewrite_test.go](../../../../internal/session/hook_input_rewrite_test.go) | `TestInputRewriteFixedCurrentBytesAndOriginalEffectiveEvidence` / `TestInputRewriteSemanticCorruptionPrecedesRepairAndKeepsBytes` |
| S2 | [internal/session/hook_fixture_test.go](../../../../internal/session/hook_fixture_test.go) | `TestHookLeaseAcrossProcessesRecoveryAndMonotonicAppend` |
| S3 | [internal/session/lease_test.go](../../../../internal/session/lease_test.go) | `TestJournalLeaseProcessOwnershipAndCrashRelease` / `TestLoaderDoesNotRepairWhileHelperOwnsLease` |
| N1 | [internal/provider/openai/hook_input_rewrite_test.go](../../../../internal/provider/openai/hook_input_rewrite_test.go) | `TestPreInputRewriteNativeGoldenRestoreAndCache` |
| N2 | [internal/provider/anthropic/hook_input_rewrite_test.go](../../../../internal/provider/anthropic/hook_input_rewrite_test.go) | `TestPreInputRewriteNativeGoldenRestoreAndCache` |
| N3 | [internal/provider/openai/post_websocket_test.go](../../../../internal/provider/openai/post_websocket_test.go) | `TestPostFeedbackWebSocketDurableSuffixDiscardAndRestoreReset` / `TestFailureFeedbackWebSocketDurableSuffixDiscardAndRestoreReset` / `TestPreInputRewriteWebSocketDurableSuffixDiscardAndRestoreReset` |
| A1 | [internal/app/hook_input_rewrite_e2e_darwin_test.go](../../../../internal/app/hook_input_rewrite_e2e_darwin_test.go) | `TestInputRewriteBothProvidersRealEffectsAndOfflineRestore` |
| A2 | [internal/app/hook_resume_test.go](../../../../internal/app/hook_resume_test.go) | `TestPreInputRewriteResumeReconcilesEveryBatchBeforeLoadingNewSnapshot` |
| A3 | [internal/app/exec_headless_e2e_test.go](../../../../internal/app/exec_headless_e2e_test.go) | `TestExecHeadlessModesFailClosedForBothProviders` / `TestHookHeadlessModesFailClosedForBothProviders` / `TestPostHookHeadlessModesFailClosedForBothProviders` / `TestFailureHookHeadlessModesFailClosedForBothProviders` / `TestPreInputRewriteHeadlessModesRequireTrustAndKeepExternalJSONLV1` |
| A4 | [internal/app/pre_hook_history_test.go](../../../../internal/app/pre_hook_history_test.go) | `TestPreCommittedHistoryKeepsOriginalAndRefreshesBoundedDetails` |
| A5 | [internal/app/hook_host_owner_test.go](../../../../internal/app/hook_host_owner_test.go) | `TestHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup` / `TestPostHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup` / `TestFailureHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup` / `TestStopHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup` |
| U1 | [internal/protocol/pre_input_test.go](../../../../internal/protocol/pre_input_test.go) | `TestPreInputActivityStrictBranchesAndEncodedBounds` / `TestPreInputCannotHideInOtherEventsOrNullTranscriptMembers` |
| U2 | [internal/tui/hook_input_rewrite_test.go](../../../../internal/tui/hook_input_rewrite_test.go) | `TestPreInputOpenDetailsRefreshKeepsAnchorAndRejectsLateChecking` / `TestPreInputPhaseSnapshotsAndSafeNarrowReview` / `TestPreInputApprovalDisablesInsufficientViewportAndPreservesDefaultDeny` |
| U3 | [internal/tui/hooks_test.go](../../../../internal/tui/hooks_test.go) | `TestHookSnapshots` |
| U4 | [scripts/input-rewrite-smoke.py](../../../../scripts/input-rewrite-smoke.py) | Darwin 实际 PTY，双 Provider 各 33 阶段 |
| G1 | [internal/runtime/hooks_test.go](../../../../internal/runtime/hooks_test.go) | `TestHookGateKeepsTrustSeparateAndCachesDurableDenial` / `TestHookGateContinueStillRequiresOrdinaryApproval` / `TestHookDenialPrecedesExistingMutationGrantAndCandidate` |
| G2 | [internal/runtime/stop_trust_test.go](../../../../internal/runtime/stop_trust_test.go) | `TestStopTrustReviewsAllDeclarationsBeforeFirstRun` / `TestStopCancelledTrustCannotInstallLateGrant` |
| G3 | [internal/runtime/hook_shutdown_test.go](../../../../internal/runtime/hook_shutdown_test.go) | `TestHookTrustCancellationRejectsStaleAndCrossIdentityResponses` |
| G4 | [internal/tui/stop_hooks_test.go](../../../../internal/tui/stop_hooks_test.go) | `TestStopTrustDefaultDenyPasteAndPhaseOrdering` |
| G5 | [internal/domain/hook_event_test.go](../../../../internal/domain/hook_event_test.go) | `TestHookEventsIsolatePlansRunsAndTrust` / `TestHookExitTwoValidationDependsOnEvent` |
| G6 | [internal/tool/builtin/hook_classification_test.go](../../../../internal/tool/builtin/hook_classification_test.go) | `TestHookClassificationPriorityAndEventExitTwo` |
| G7 | [internal/runtime/post_hooks_test.go](../../../../internal/runtime/post_hooks_test.go) | `TestPostTrustAndApprovalBothPrecedeEffects` / `TestPostEverySyncFailurePreservesRealResultAndStops` / `TestPreAndPostShareExecutionBudgetAndKeepFrozenPrefix` |
| G8 | [internal/extension/hook/output_test.go](../../../../internal/extension/hook/output_test.go) | `TestOutputSubsetAndNoFreeTextFeedback` |
| G9 | [internal/runtime/current_contract_test.go](../../../../internal/runtime/current_contract_test.go) | `TestPatchGrantRequiresDurableApproval` / `TestFileGrantCannotOverridePolicyDeny` |
| G10 | [internal/tool/builtin/hook_process_darwin_test.go](../../../../internal/tool/builtin/hook_process_darwin_test.go) | `TestHookProcessBoundsOutputAndPreservesExitClassification` |
| G11 | [internal/extension/hook/post_output_test.go](../../../../internal/extension/hook/post_output_test.go) | `TestPostOutputClosedGrammar` |
| G12 | [internal/extension/hook/stop_output_test.go](../../../../internal/extension/hook/stop_output_test.go) | `TestStopOutputHasOneExplicitModelDisclosurePath` / `TestStopOutputRejectsAmbiguousUnicodeAndLimits` / `FuzzStopOutputNeverDisclosesUnauthorizedBytes` |
| G13 | [internal/extension/hook/failure_test.go](../../../../internal/extension/hook/failure_test.go) | `TestFailureOutputUsesItsOwnClosedEvent` |

## 六份规格逐场景映射

以下 **14 个 requirement、88 个 scenario** 全部映射至实际断言。修改的通用 Hook 条目同时保留原有信任、四事件隔离与故障测试；新增替换行为以同一索引的正反例核验。

### `extension/command-hooks`

| Requirement / Scenario | 证据索引 |
| --- | --- |
| **Every hook needs narrowly bound process-local trust** | P1 R2 A3 U3 U4 |
| Trust one declaration | G1 G2 R2 |
| Reject an old response | G2 G3 |
| Reconsider a denial | G1 U4 |
| Restore without trust | A2 |
| Isolate trust across events | G5 G7 R2 |
| Deny a completion review | G2 G4 |
| Reject a tool response for a stop target | G2 G5 |
| Review input replacement authority | U3 U4 |
| **Hook wire uses typed input and strict bounded control output** | P1 P2 D1 D2 |
| Continue with empty output | G8 P1 |
| Reject unsupported approval output | G8 P1 |
| Do not persist secret-looking output | G8 G10 P1 S1 |
| Fail closed on timeout or malformed output | G6 G10 P1 |
| Validate exit two according to the event | G5 G6 |
| Separate execution failure from an unstarted budget suffix | G7 |
| Validate all three events independently | G8 G11 G13 |
| Keep all four parsers independent | G8 G11 G12 G13 P1 |
| Accept a bounded standalone replacement | D1 P1 P2 |
| Reject control output in another event | P1 U1 |

### `extension/pre-tool-use-input-rewrite`

| Requirement / Scenario | 证据索引 |
| --- | --- |
| **Input replacement is complete typed and capability preserving** | D1 P1 T1 |
| Replace a complete command input | D1 P1 T1 |
| Replace a complete file input | D1 P1 T1 |
| Reject a partial or mixed replacement | D1 P1 T1 |
| Reject a changed identity or capability | T1 P2 R1 |
| Preserve output and failure limits | P2 R2 |
| Preserve each Patch facade profile | D1 P1 T1 |
| Reject canonical expansion beyond the replacement limit | D1 P1 T1 |
| Validate bounds before accepting an equivalent object | D1 P1 T1 |
| Distinguish malformed runner evidence from script output errors | P1 P2 R2 |
| **Ordered hooks compose durable checking candidates before execution selection** | R1 R2 D2 S1 |
| Validate the previous replacement in a later hook | R1 R2 D2 S1 |
| Keep an equivalent replacement unchanged | R1 R2 D2 S1 |
| Restore the original value through ordered changes | R1 R2 D2 S1 |
| Stop after a later denial | R1 R2 D2 S1 |
| Fail before installing unconfirmed input | R2 A4 |
| Cancel after a definite intermediate result | R2 |
| Cancel during the final successful batch | R2 S1 |
| **Execution evidence binds selection to the complete original call and turn** | T1 R1 T2 |
| Reject the same ID with a different original input | T1 R1 T2 |
| Reuse a completed selected call | T1 R1 T2 |
| Preserve proof when changes return to the original value | T1 R1 T2 |
| **Model results identify the actual effective input within existing budgets** | T2 R1 R2 A1 |
| Report the command that was actually selected | T2 R1 R2 A1 |
| Refuse insufficient disclosure space before side effects | T2 R1 R2 A1 |
| Preserve explanation when output is large | T2 A1 |
| Restore a frozen result without another explanation | R3 A2 N1 N2 |
| Keep a denied post trust result within the safety allowance | T2 R1 R2 A1 |
| Preserve ordinary denial and pre-admission cancellation | T2 R1 R2 A1 |
| Explain an admitted startup failure accurately | T2 A1 |
| Ignore a forged annotation inside stdout | T2 N1 N2 |
| Retain admission when no post hook exists | T2 R1 R2 A1 |

### `provider/native-history-persistence`

| Requirement / Scenario | 证据索引 |
| --- | --- |
| **Rewritten calls retain native requests and verify effective outputs** | N1 N2 S1 |
| Preserve an OpenAI custom patch request | N1 N2 S1 |
| Preserve an Anthropic tool request | N1 N2 S1 |
| Reject native output for another effective input | N1 N2 S1 |
| Validate a rejected candidate without creating execution input | N1 N2 S1 |
| Validate explanation mode using actual admission | N1 N2 S1 |
| **Rewritten continuations preserve restoration and cache equivalence** | N1 N2 N3 A2 |
| Compare both Provider restorations | N1 N2 N3 A2 |
| Preserve a no-change request | N1 N2 N3 A2 |
| Compare full and incremental continuation | N1 N2 N3 A2 |

### `session/jsonl-store`

| Requirement / Scenario | 证据索引 |
| --- | --- |
| **Pre hook input effects are strict required execution evidence** | D2 S1 |
| Record one accepted change with its result | D2 S1 |
| Reject a forged effect chain | D2 S1 |
| Reject replacement attached to failure or another event | D2 S1 |
| Replace current fixtures without another reader | D2 S1 |
| Reject a bounded-on-disk but expanded parameter | D2 S1 |
| Keep rejected candidate metadata separate | D2 S1 |
| **Effective input recovery never reruns hooks or tool effects** | R3 S1 A2 |
| Recover after an intermediate replacement | R3 S1 A2 |
| Recover an unfinished rewrite attempt | R3 S1 A2 |
| Recover after approval without execution | R3 A2 T2 |
| Recover a finished tool before native outputs | R3 S1 A2 |
| Retain writer ownership during recovery | S2 S3 A5 |
| Reject cancellation that rewrites a completed gate | R3 S1 A2 |
| Preserve the selected input before ordinary approval | R3 S1 A2 |
| Reject a recovery note that hides a normal result | S1 T2 N1 N2 |

### `tool/approval`

| Requirement / Scenario | 证据索引 |
| --- | --- |
| **Rewritten execution uses the final effective candidate throughout approval** | T1 R1 A1 |
| Approve the rewritten file and content | T1 R1 A1 |
| Reject an approval for the original candidate | T1 R1 A1 |
| Require ordinary approval after command replacement | T1 R1 A1 |
| Reuse only the applicable grant | G1 G9 R1 |
| Do not approve a partially checked candidate | T1 R1 A1 |
| Reject a stale input-derived permission assessment | R1 T1 |
| **Input replacement never creates file evidence or bypasses preparation** | R1 A1 A3 |
| Require evidence for a new overwrite target | R1 A1 A3 |
| Reject a substituted unsafe path | A1 |
| Pass effective input to post checks | R1 A1 |
| Keep headless denied | A3 |

### `tui/hooks`

| Requirement / Scenario | 证据索引 |
| --- | --- |
| **Hook replacement review shows actual effective execution** | A4 U2 U4 |
| Approve the actual changed command | A4 U2 U4 |
| Inspect original and effective inputs after restart | A1 A4 U2 |
| Keep replacement separate from execution | A4 U2 U4 |
| Ignore a stale replacement detail | A4 U2 U4 |
| Review in narrow and uncolored terminals | U2 U3 U4 |
| Keep a rejected candidate visibly unexecuted | A4 U2 U4 |
| **Pre hook snapshots are installed before bounded activity notifications** | A4 R2 U1 U2 |
| Refresh a detail opened during checks | A4 R2 U1 U2 |
| Preserve original parameter history on live updates | A4 R2 U1 U2 |
| Bound encoded activity with escaped text | U1 U2 |
| Fail after durable state but before host installation | R2 |

## 49 项任务映射

| 任务 | 证据索引或验收记录 |
| --- | --- |
| 1.1 | D1 |
| 1.2 | D2 |
| 1.3 | T1 R1 |
| 1.4 | D2 S1 |
| 1.5 | T1 D1 |
| 2.1 | P1 D1 |
| 2.2 | P2 R2 |
| 2.3 | P1 |
| 2.4 | D1 D2 P1 |
| 3.1 | R1 R2 |
| 3.2 | R1 A1 |
| 3.3 | A1 |
| 3.4 | R1 A1 |
| 3.5 | R1 R2 |
| 3.6 | R1 |
| 3.7 | R2 |
| 4.1 | T2 |
| 4.2 | T2 R2 |
| 4.3 | T2 R1 A1 |
| 4.4 | T2 R3 A2 |
| 4.5 | T2 R2 A1 |
| 5.1 | D2 S1 R2 |
| 5.2 | S1 T1 |
| 5.3 | R3 A2 |
| 5.4 | S1 |
| 5.5 | S2 S3 A5 |
| 5.6 | R2 G7 A5 |
| 5.7 | S1 |
| 6.1 | N1 N2 A1 |
| 6.2 | N1 N2 |
| 6.3 | N1 N2 A2 |
| 6.4 | N1 N2 N3 |
| 6.5 | T2 N1 N2 |
| 7.1 | U1 R2 |
| 7.2 | U3 U4 |
| 7.3 | U2 A4 U4 |
| 7.4 | A4 R2 U2 |
| 7.5 | U1 U2 A4 |
| 7.6 | R2 |
| 8.1 | A1 |
| 8.2 | A2 A3 |
| 8.3 | U4 |
| 8.4 | D1 T1 T2 R2 A1 A4 |
| 9.1 | README、能力清单、总体架构、Roadmap、pitfall、ADR 0010 |
| 9.2 | make-verify.json/log 与 attempt-1/2 证据 |
| 9.3 | openspec-validation.json/log 与人工核对 |
| 9.4 | cross-compile.json/log |
| 9.5 | 本记录的 14 requirements / 88 scenarios / 49 tasks 映射 |
| 9.6 | spec-sync.json 与随后 strict 检查；change 已于 2026-11-15 归档 |

## 边界与故障的业务断言

- 三能力 × rewrite-then-noop/twice/revert/reject × 有/无 Post 的 24 组矩阵证明只有 E 进入候选、普通审批、执行和后置；旧 O 指纹、不同 O 但相同 ID、旧 turn/sample、重复 owner、poison 与过期响应均不能授权。九项直接工具继续使用明确的 unhooked 证明，原行为保留。
- 原始 stdout 合格不代表规范对象合格：12,108-byte 原始 Write 探针规范编码为 72,039 bytes，被安全拒绝。raw/canonical/finished/stdin/UI/说明各按真实编码独立校验；no-op 也不能绕过上限。Patch 同能力仍按 Edit 子串与 apply_patch LF 行 profile 检查，并验证反向适配不发生隐式归一化。
- runner 已正常返回、途中 finished Sync、最后 finished/continue batch、普通 allow 和 started 接收的取消线性化通过确定性 barrier 检查；保留真实结果，晚到取消不能改写 gate、启动下一 Hook 或伪称已运行工具。Sync/宿主安装失败保持唯一 durable 事实并 poison，零后续普通副作用。
- 模型说明按真实选择、准入与恢复事实决定，不能从 status、字符串标签或是否存在 Post 猜测。低预算在普通候选/批准前拒绝，继续只用一个 ticket；Post 信任拒绝、候选失败、普通 deny/abort、started 前取消使用安全短说明；准入后的真实启动失败保留完整 E，不能声称进程一定已启动。
- 双 Provider 真实 builtin 验收包含 Read、Patch、Write、Exec 正常非零退出、进程启动失败、Write base 冲突、缺 Read 资格、批准时 symlink 交换及 600000-byte 输出 artifact。原目标/命令标记未发生副作用，改写目标实际变化；workspace 外 bytes 不变，artifact 原始 bytes 没有说明污染。
- 九份新增合法固定 JSONL 切点与十份 checksum 合法的语义反例涵盖摘要/profile、canonical 膨胀、partial C、late cancel、recovery disguise、metadata、准入、旧 shape 与版本字段。测试直接读取仓库静态 bytes，不用当前 encoder 生成 fixture；语义验证失败先于半行 repair，整个 journal bytes 不变。
- 每个 batch 崩溃、重复崩溃和缺 result/native-output 均本地补偿；删除/损坏旧配置后仍零旧脚本、候选、批准、工具或恢复网络重跑，result 已冻结则原字节复用。真实子进程 lease 竞争、占用期间禁止 repair、崩溃释放、Sync/Close 后释放和续写 seq 单调都有实际断言。
- 双 Provider 的三能力 × 八模式证明 O native 与 E metadata/output 同时有效；15 份新 request golden 对照不中断/恢复的 canonical bytes、native item 顺序、footprint/revision 和完整 cache fingerprint。Anthropic marker/TTL 与 OpenAI key、WebSocket full/incremental/discard/restart 分别验证，opaque thinking/signature/encrypted/phase 不重建或转换。
- 活动采用 none/candidate/selected/rejected 四相位：Snapshot 先安装后通知，来源为 writer 的真实 sequence，最终相位使用 gate sequence。长 O 与所有 C/E 经纯内存分页展示；过期 checking、代次/offset、旧页和四页缓存不能覆盖新事实，草稿、锚点及视觉 scroll 保留。普通批准本身展示完整 E，不依赖旧详情补足。

## AGENTS 与当前契约人工核对

- Domain 的输入/profile/effect 不依赖 tool、Provider、Session、终端或 I/O；tool 纯适配由 Runtime、Session、Provider 共用，不复制解码规则。强类型构造/strict decoder/validator、深拷贝及能力消费者齐全，没有引入无约束 `any`、动态通用 JSON 或可执行占位 schema。
- 沿用既有进程/stream/workspace owner，没有新增隐藏 goroutine、全局可变 registry 或在 New/getter/render 中执行 I/O。快照与选择在锁外推导，锁内只安装有界内存值；commit 后内存安装失败不回滚、不重跑。无新增依赖或跨层反向导入，架构守卫随质量门通过。
- 唯一当前 Pre finished 必需 `input_effect`；其他事件禁止。没有新增 record kind、版本字段、历史 reader、迁移、可选兼容或 resume 原地改写。O/native、C 链、E、准入及 result/output 在 repair 前整体核验，不恢复普通 grant 或 Hook 信任。
- 原始 Hook stdout/stderr、自由文本 reason、命令配置与信任不持久化或透传。被严格接纳的规范参数是已确认的必要执行事实和有界模型说明；不额外采集磁盘正文/diff。非参数合成秘密哨兵不得进入错误、模型结果、日志或 snapshot；证据只保存本地合成数据，没有真实密钥、敏感 Header 或用户机密。
- FD 安全、Read 资格、路径与 sandbox 范围保持实际 builtin 边界；未新增权限通道。完整参数 16 KiB、来源 metadata 1 KiB、短说明 256 bytes、Pre 投影 canonical 4 KiB/整体活动 16 KiB、摘要 160 rune 均有独立实际编码断言。
- 普通批准小于 32×18、Pre 信任小于 20×6 时禁止确认，保留 Esc/Ctrl+C；恢复足够尺寸后保持默认拒绝。候选/已选/拒绝不假称 OS 进程启动，真实准入来源只有工具 started Sync。
- 既有 `TestHookSnapshots` 改为直接比较真实 `m.View()` 原始 bytes，去除旧测试额外添加的换行；没有 trim、忽略 whitespace 或弱化显示断言。`git diff --check` 通过。

## 主规格与文档同步

已同步 `extension/command-hooks`、`extension/pre-tool-use-input-rewrite`、`provider/native-history-persistence`、`session/jsonl-store`、`tool/approval`、`tui/hooks` 六份主规格。采用 CLI 返回的 planningHome 与六个确切 artifact 路径、同一 specs 规则快照；保留现有 Purpose、未修改 requirement 和原有 scenario，新增能力复制已批准 Purpose，无 TBD 或新行为。实际同步范围见 [spec-sync.json](evidence/spec-sync.json)。

README、能力清单、总体架构、Roadmap、pitfall 与 ADR 0010 已同步，明确必要参数/说明是原始输出禁存规则的有界例外；测试路径和实际 record vocabulary 已核对。49 项任务全部完成，CLI 状态为 `all_done`，见 [apply-progress.json](evidence/apply-progress.json)。最终差异、strict 和人工核对见 [final-audit.json](evidence/final-audit.json)。change 已于 2026-11-15 归档；未创建 commit 或 PR。

## 平台与能力限制

Darwin/arm64 实际运行双 Provider 本地模拟服务、真实文件/进程、sandbox 和 PTY；没有连接商业 API。Linux/Windows 只构建和测试编译，未在原生系统执行 FD/进程/sandbox/终端验收，不能据交叉编译声称这些行为均正常。

仅支持三项 mutation 的完整规范替换；16 KiB 无法覆盖任意 1 MiB Write，不截断后放行。串行候选链明确区别于参考 Claude 共享不变 stdin 的并行规则和参考 Codex 最后完成项规则。不包含 allow/ask、async、结果改写、新 Hook 事件、MCP 或通用 Claude 脚本兼容，未将整个 P6 标记为完成。
