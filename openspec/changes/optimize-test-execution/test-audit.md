# 重测试与覆盖审核

基线来自 `baseline.json`：最新 main `b0387c4` 的独立源码快照，完整 `make verify` 退出 0、1114.245 秒；普通与 race 各执行 1597 个顶层入口、4707 条含子测试结果。普通构建缓存可复用，测试结果通过 `-count=1` 禁用。机器、轻量竞争说明见基线文件。

| 重路径 | 不能删除或缩小的业务边界 | 本轮处理 |
|---|---|---|
| app `TestSessionPreviewCurrentFixturesAreReadOnly` | 所有当前固定 fixture；半行预览不修复、journal bytes 不变、lease 释放、离线投影与原生 restore 等价、返回值无可变别名 | 既有测试与 fixture 全部保留；优化其共享严格解码调用 |
| app `TestFailureBothProvidersRealHookSandboxAndFrozenRestore`、`TestHookBothProvidersRealSandboxAndThreeCapabilities` | 两个 Provider、三类 mutation、授权/拒绝/坏输出/预算、真实 sandbox 与冻结恢复 | 保留矩阵与真实进程；独立测试进程分片，不对 Setenv 测试添加 Parallel |
| app `TestStopCompactionKeepsFrozenFeedbackAndGenuineSkillCorrection` 及插件对应测试 | 两个 Provider；成功/失败/空/取消摘要；冻结反馈与用户修正不丢失，附件来源删除后仍只用既有事实 | 已有 helper 复用相同装配；继续保留各独立来源及断言，不新增共享可变环境 |
| app `TestSevenToolTranscriptBudgetAndRestoredRequestForBothProviders` | 七工具原生 pairing、真实批准/拒绝、大 UTF-8 输出、artifact 与恢复请求 | 保留真实超限数据、进程与双 Provider；仅减少公共解码重复工作 |
| session `TestInputDraftBudgetIncludesCanonicalEnvelopeAndNewlines` | record 边界精确合法/超一字节、64 MiB batch 含 envelope 与换行 | 保留 16/64 MiB 边界，禁止用小数据代替真实阈值 |
| provider `TestWriteAggregateNativeAndRequestBudgetsRemainSixteenMiB` | 单调用合法、17 个调用聚合超限、native 与 wire 均拒绝，真实 16 MiB 常量 | 完整保留；共享 codec 减少重复扫描，无 race 专用宽松实现 |
| runtime 与 session 的恢复/取消/失败矩阵 | durable 次序、失败 bytes 不变、无重复副作用、原生恢复与 fingerprint 等价 | 全包普通与 race 继续执行；自动清单核对确保新入口不会漏掉 |

本轮没有删除、合并或移动原有业务测试，没有修改已有测试的业务输入、断言或 skip。Linux CI 暴露 `TestChatResourcesCloseOrdersSessionJournalAndProvider` 把 TurnStarted 当成 Stream 已启动；该测试改用已有 started 信号等待真实前置条件，完整 timeline 断言不变。`TestStreamEscalationClosesProviderBeforeLoopAndJournal` 的 fixture 在取消后返回 `ctx.Err()`，使取消失败结果与原有清理顺序保持确定。新增 Go 测试入口位于 codec 与 Session 编码等价性回归；因此原测试到保留测试的映射为同一文件、同一入口、同一断言的一一映射。全量入口及子测试结果另由最终验收与基线清单比较确认。

代码行数不作为删测依据。主要收益来自消除公共解码路径的重复扫描/临时分配和重包分片，不需要继续抽象已隔离的装配、跨用例缓存实例或缩减安全矩阵。

## 独立矩阵子用例的调度审核

`hook_resume_test.go` 的 `testHookResumeReconcilesEveryBatch`（Pre、Post、Failure、输入重写四个入口）、`stop_hook_compaction_test.go` 的 `testStopCompactionKeepsFrozenCorrection`（本地/插件两个入口）、`stop_hook_limits_test.go` 的 `TestStopSampleAndCallLimitsKeepTextContinuationAvailable` 共七个顶层入口，仅将已有独立子用例交给 `-parallel` 上限调度。原测试名称、所有模式、输入、崩溃点和断言位置完全不变，原断言到保留断言为一一映射。

- 每个子用例在其函数内创建 workspace、Session root、HTTP server、runner、请求计数/互斥锁和收口 owner，没有跨子用例可变引用。`appPluginFixture`、`cacheFixtureConfig`、`writeHookConfig`、`writeStopHookConfig`、`loadToolLoopPlan` 均使用传入或独立临时路径。
- 这些用例及所调用的 fixture 不执行 Setenv/Chdir；只读取 TestMain 一次创建的私有 HOME。其他修改环境的顶层测试仍顺序执行，不与其子用例交错。
- Hook 的同一家 Provider 下全部恢复检查点及二次崩溃仍保持原有顺序；没有把共享 baseline、captured requests 或 compensated request 的检查点拆成并发任务。
- 清理仍在子用例返回前等待原 control、resources、server；没有把父测试提前返回后的 defer 当成子用例资源 owner。实际效果须经乱序 race 和全量质量门验证。

## 超限 SSE 装配的内存审核

`write_stream_test.go` 的两个 Provider、八种场景及全部文件副作用/批准/完成/失败断言保持原位置。`oversize-input` 与 `aggregate-limit` 使用原 `serveWriteScenarioSample` 直接写入 HTTP 响应，不再预先把完整 SSE 放入 `bytes.Buffer`；17 个接近 1 MiB 的调用、独立路径、事件顺序与参数字节不变。相同的大文本模板在一次 handler 调用内复用；每个调用仍有独立参数字符串。随机 chunk、单字节、半包、取消与缺失终止事件场景继续使用原装配。没有降低真实边界或共享跨用例状态。
