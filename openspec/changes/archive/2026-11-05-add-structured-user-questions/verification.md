# 结构化用户提问实现验收

日期：2026-11-05。变更：`add-structured-user-questions`，schema：`spec-driven`。

## 选择与实现范围

该切片利用已有双 Provider Tool Loop、Session、AgentLoop 与 composer，补齐“模型遇到不确定性 → 用户决定 → 继续同一任务”。产品采用 Claude Code 的逐题选择、说明、其他回答和复核；内核采用稳定题目 ID、请求/响应分离、原生 call/output 配对和严格恢复。参考快照、实际路径与取舍见 `design.md`，两个参考仓库均只读，没有构建依赖或代码拷贝。

已实现九项工具中的 `user.question`：Anthropic `AskUserQuestion`、OpenAI `request_user_input`；1–4 题、每题 2–4 选项，单选/多选、说明、其他文字、明确跳过和复核提交。提问独占工具位置，不产生副作用批准或 grant。无交互模式配对返回 `denied/user_input_unavailable`。

共享问题值位于 `internal/protocol/question_values.go`，tool 使用别名组合调用/结果，保持依赖方向。`QuestionExecutor` 不依赖 Provider/TUI；Runtime 复用独占执行骨架和原有 worker/owner。Session 不新增问卷 event、版本或兼容 reader，回答仅保存在完整冻结 preview。原生历史仍是请求事实源。

## 契约场景与测试映射

所有路径相对仓库根；以下测试均包含业务结果断言，Session current 输入 fixture 均为仓库固定 bytes，不在测试运行时生成。

| 契约 / tasks | 主要测试与固定材料 | 验证内容 |
|---|---|---|
| typed input/answer/result；1.1–1.3 | `internal/protocol/question_values_test.go`、`user_question_test.go`；`internal/tool/question_test.go` | 严格字段、重复键/ID/标签、null/别名/Unicode、精确数量/大小边界、单选/多选/跳过、原顺序与正文空白、深拷贝、metadata 与题目关联、拒绝伪造标签 |
| executor/catalog；2.1–2.3 | `internal/tool/builtin/question_test.go`；`internal/tool/tool_test.go`；`internal/tool/testdata/exec_catalog.golden.json` | 真实执行器、无交互/取消/失败、无虚构默认答案、九能力定义与稳定排序、构造缺件拒绝；Question exclusive 且不需要 approval，既有三类副作用批准保持 |
| 双 Provider wire/stream；3.1–3.2 | `internal/provider/openai/question_test.go`、`internal/provider/anthropic/question_test.go`；两家 `testdata/*request*.golden.json`；`internal/app/questions_e2e_test.go` | strict facade、逐字符参数增量、中文/UTF-8 分片、模型预填答案拒绝；实际 HTTP/SSE 按变化字节块切断边界；缺少 terminal 的断线/取消零问卷与工具事实 |
| immutable preflight/native restore；3.3–3.4 | 两家 `question_test.go`；`internal/provider/restore_question_test.go`；`internal/app/question_recovery_test.go` | preflight 不修改 history/revision，不借用活动 history；错误 invocation/call 拒绝；原生 call/output、完整 preview、题目、共享 ready/result 与原生输出相等；未知/伪造关联整体失败 |
| 回答控制与 durable；4.1–4.5 | `internal/protocol/user_question_test.go`；`internal/runtime/user_question_test.go`；`internal/app/question_workflow_darwin_test.go` | 过期/重复回答、取消先后、接收后义务、完整结果预算及转义膨胀、失败不扣账；started/result 的 Sync 屏障、Sync 失败 poison、不误报 resolved；阻塞 Sync 时 shutdown 不放弃 owner/done；后序 Write/Patch/Exec 仍独立审批 |
| 当前 Session 与恢复；5.1–5.4 | `internal/session/question_fixture_test.go`、`question_replay_test.go`；`internal/app/question_recovery_test.go`；`internal/session/testdata/current/question_*.jsonl` | 两家 Read/Question/Write 固定当前数据；ready/started/result/output 边界、skip/unavailable、checksum 合法的伪造标签、原生题目/答案错配；零交互/工具/网络恢复，已有 preview 逐字节保留，拒绝路径 journal 不变 |
| 压缩与 canonical 等价；5.4、7.2 | 两家 `TestQuestionPreflightNativeRestoreAndForgery` 复用 `assertCompactionAfterToolSteering`；既有 `restore_test.go`、`cache_test.go`、`websocket_test.go` | 比较 uninterrupted/restored 完整请求 canonical bytes、native envelope 顺序/opaque bytes、footprint/revision；连续两次 checkpoint、后续工具/steering、CachePlan fingerprints；HTTP/full/incremental 各自 golden，不混淆传输帧 |
| 真实进程 lease；5.5 | `internal/session/question_replay_test.go`（`TestQuestionInterruptedLeaseCrashAndMonotonicAppend`）；`internal/app/question_workflow_darwin_test.go`；既有 `host_lease_test.go` | 等待问题期间真实子进程竞争、禁止 repair、拒绝时 bytes 不变；杀进程释放、尾部修复、续写 seq 单调；结合阻塞 result Sync 的 owner 测试与原有阻塞 cleanup lease 测试验证清理顺序，无 sleep 竞态证明 |
| app/headless；6.1 | `internal/app/question_headless_test.go`；`questions_e2e_test.go` | --print/--json/stream-json 双 Provider 闭环、两次采样与同 call ID output；没有问卷事件或额外 stdin 读取，现有格式/退出行为保持 |
| TUI 状态/布局；6.2–6.4 | `internal/tui/user_questions_test.go`；`internal/tui/testdata/question_{80x24,34x12,12x6}_{false,true}.golden` | 四题混合回答、说明/其他/明确跳过、复核、拒绝留草稿、重复/迟到响应、独立聊天 composer、paste/resize/停止、中文/emoji/控制字符、长内容动作可达和 View 无副作用 |
| 历史；6.5 | `internal/tui/user_questions_test.go`（`TestQuestionHistoryIsReadOnlyAndBreaksReadGroups`）；`internal/app/question_recovery_test.go`；既有 `transcript_browser_test.go` | 打断读取分组、只读旧问卷、左右切换完整题目/结果、UTF-8 小页拼接保留精确答案、共享最多四页及 64 KiB 页预算 |
| 完整任务链；7.1 | `internal/app/question_workflow_darwin_test.go` | 双 Provider Read → Question → 下一 sample Write → Question → Patch → Exec → 总结，两个问题串行；旧响应不能回答新问题，三个副作用各批准一次，原生输出顺序和真实文件/命令结果均核验 |
| 缓存来源；7.2 | `internal/context/question_planning_test.go`、`exec_planning_test.go`；更新后的 catalog/request/continuation/cache/compaction/WebSocket goldens | 第九项静态 schema 产生可解释的 tool_catalog revision/prefix 变化；动态题目/答案/身份/窗口/预算不污染稳定目录；当前固定 bytes 回归 |
| 真实 PTY；7.3 | `scripts/user-question-smoke.py`、`internal/app/question_terminal_test.go`、`pty-smoke.json` | 本地假 SSE，选择继续、无色 12×6 输入其他文字后恢复 80×24、停止等待、含问卷历史的启动选择器取消；退出与 termios 恢复；独立事件/窗口握手，无 sleep 作为正确性依据 |

## 质量门结果

实现前基线 `make verify` 已通过。实现后多次完整质量门已通过；最后一次补强原生/共享关联及长问卷光标检查后的 `make verify` 全部通过；全量 race 中 app 包约 292.2 秒，TUI 包约 5.4 秒。

已运行并通过：

- `go test ./internal/provider/... ./internal/app -run 'TestQuestion|TestSessionPreviewCurrentFixtures' -count=1`
- `go test ./internal/session ./internal/tui -run TestQuestion -count=1`
- `python3 scripts/user-question-smoke.py`，实际输出保存在 `pty-smoke.json`。
- `openspec validate add-structured-user-questions --strict`
- `openspec validate --specs`：48 项通过，0 失败；只有需求正文长度的 INFO 提示。
- `git diff --check`。

`make verify` 包含分支规则回归、gofmt、go vet、Staticcheck、架构守卫、全量测试和全量 race。pre-commit 沿用同一质量门，没有删除、跳过或弱化测试。

## PTY 结果及平台范围

实际宿主为 macOS 27.0.1 arm64，Go 1.24.13 darwin/arm64。四个案例均恢复终端模式：选择与其他文字各 2 个本地请求、停止等待 1 个请求，正常退出 0；启动选择器取消 0 个请求、退出 130。停止当前问题沿用聊天语义：先取消 turn，再在空闲时退出；不是把提问取消错误映射成整个聊天进程退出 130。

没有访问真实 Provider 或使用真实 API key。未执行 Linux/Windows 原生 PTY、sandbox 和文件描述符安全路径：当前宿主无法提供这些平台的原生环境。它们的实际运行行为不以此次 macOS 验收或交叉编译推定；Exec 当前仍仅支持 Darwin，其他平台保持既有失败关闭策略。

## 交付核对

- JSONL 当前 vocabulary 仍为既有 ready/approval/started/result/native/usage/turn/compaction/input 事实；Question 不产生 approval，不引入 UI admission record、版本、migration 或双 reader。
- 进程内新增 `resolve_user_question`、`user_question_requested`、`user_question_resolved`；外部 headless JSONL 未新增回答命令。
- 新的 RestoreRecord 绑定只在内存传递已有 frozen facts，Provider 安装前核对题目和答案一致；不从 SemanticHistoryView 或 transcript 反构造请求。
- 无新第三方依赖、全局可变 registry、隐藏 goroutine、无用 facade 或修改参考工程。新增生产文件遵循纯构造、中文注释、英文安全错误和单向依赖。
- README、总体/Tool 架构、Roadmap、pitfall 与当前九能力目录、平台限制及实际函数/测试路径已同步。
- 主规格 11 个能力已合并，保留旧场景；归档收口时核对路径、任务和规格一致性。

## 规格同步与归档结果

11 个 delta specs 的 20 个完整 requirement 已逐一核对与 main specs 相同，原有 scenarios 保留；`openspec validate --specs` 共 48 项通过。随后执行 `openspec archive add-structured-user-questions --yes --skip-specs --json` 成功，归档至 `openspec/changes/archive/2026-11-05-add-structured-user-questions`。`--skip-specs` 仅避免重复写入已经同步并验证的主规格，未省略规格同步。归档动作完成后任务为 32/32，所有检查结果均来自实际工具输出。
