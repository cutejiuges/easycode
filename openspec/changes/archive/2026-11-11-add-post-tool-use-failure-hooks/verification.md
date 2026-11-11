# PostToolUseFailure 实现与验收

> 后续 Windows helper 平台标签修复与质量门重跑见 [追加验收](windows-helper-regression-2026-11-11.md)。下文保留当次结果，包括修复前 Windows 测试编译失败。

> 2026-11-11 后续独立回归发现并修复了审查消息编码预算问题，新增 36 个端到端用例；见 [独立回归报告](regression-2026-11-11.md)。下文保留首次验收快照，不替代后续结果。

## 范围与基线

- 变更：`add-post-tool-use-failure-hooks`；分支：`feat/post-tool-use-failure-hooks`；设计基线：`27be635`。
- 验收跨度：2026-11-08 至 2026-11-09（Asia/Shanghai）；环境：Darwin 27.0.0 arm64，Go 1.24.13。参考工程仅只读核对，没有修改、构建依赖或复制大段实现；快照与取舍见 [design.md](design.md)。
- 本次交付：执行尝试确定失败后的同步诊断，复用既有来源、信任、进程 owner、调用预算、原生配对与恢复；没有命令语义解释、自动修复、重试、异步脚本或兼容 reader。
- 设计阶段的基线验证不计入本次新增功能证据。下列测试均针对当前实现；最终全量质量门状态另列。

## 实现契约与测试映射

| 契约 | 代码与业务断言 |
| --- | --- |
| 三事件、精简双分支与唯一当前格式 | `internal/domain/post_hooks.go`；`failure_hook_test.go`、`post_branch_test.go` 验证严格输入、事件/归属、顺序、revision、clone、digest、16 个备选声明和独立提示槽。旧根 shape 和终态旧 event 被拒绝。 |
| 三事件配置与精确信任 | `internal/extension/hook/config.go`；`failure_test.go` 验证规范顺序、来源总数、匹配上限、别名、披露与错事件输出。Runtime 的 16 项审查测试证明两分支全部信任后才出现普通批准。 |
| 原工具事实决定 success/failure/none | `internal/tool/post_selection.go`、`internal/runtime/exclusive.go`；`tool/post_selection_test.go`、`runtime/failure_hooks_test.go` 覆盖三能力、准备/批准/取消/不确定、跨调用准入、正常 exit=1、启动失败、互斥运行和缓存重入。准入由成功 Sync 的 started sequence 显式携带。 |
| 选择与当前取消独立 | 原 error Sync 后取消仍为 failure/cancelled 且零新脚本；未配置失败分支仍为 failure/skipped/hook_event_unmatched。none/unmatched 无伪运行或反馈。见 `runtime/failure_hooks_test.go`。 |
| 单次预算与工具数据反馈 | `tool/post_feedback.go`、`model_output.go`；`post_feedback_test.go`、`post_selection_test.go` 验证 A−R、一次预留/关闭、未选择 model 分支不回涨预览、4/16 KiB、整块拒绝、跨分支 ordinal=0、逐段实际编码与空反馈。 |
| Pre 与失败 Post 共用期限 | `TestPreAndFailureShareExecutionBudgetAndKeepFrozenPrefix` 用假时钟证明 Pre 消耗 20 秒后失败分支仅得到 10 秒；信任等待不扣费，预算后缀不伪造 timeout。 |
| 当前固定 Session bytes 与 lease | `session/post_hook_fixture_test.go` 通过生产 Loader/ReplayPlanner 验证 14 个有效失败 fixture 和一个语义损坏 fixture；含启动失败、双分支、披露关闭、反馈预算不足、取消、unmatched、崩溃前缀和恢复终态。小额度 fixture 从同 sample 前序已保存预算事实重建 A=1524、R=500、base=1024、delivery=budget_unavailable。真实子进程竞争验证占用不 repair、崩溃释放、续写 seq 单调及失败 bytes 不变。 |
| 恢复不执行外部能力 | `runtime/failure_recovery_test.go` 覆盖固定切点和每次恢复 append/Sync 前后故障；外部工具/准备/Hook/Provider 采样计数为零，原 error bytes 和已保存 failure/cancelled terminal 不变。`app/hook_resume_test.go` 覆盖两家每批次崩溃、二次崩溃、缺失/损坏配置和原生输出补偿。 |
| 双 Provider 原生身份与缓存 | 两家 `post_output_test.go`、`failure_output_test.go` 的六份失败 golden 保留 OpenAI function/custom apply_patch、Anthropic tool_result 的 call ID、status/is_error、配对和顺序。完成、项间中断、启动失败、原 error 后取消共 20 个失败恢复用例比较 canonical request、opaque、footprint 和缓存证据。OpenAI `post_websocket_test.go` 增加失败反馈的 durable suffix、discard reset、恢复后完整请求。 |
| 真实本地服务与受限脚本 | `app/failure_hook_e2e_darwin_test.go` 的 2 Provider × 3 能力 × 6 模式共 36 个用例：model、none、非法 stdout、exit 2、unmatched、信任拒绝。文件冲突在普通批准前确定性注入；Exec 启动错误在执行边界注入，Hook 使用真实 Darwin 沙箱。保存结果后模型继续采样一次，无重复原工具执行。删除配置后复制同一冻结历史，对照后续请求、opaque、footprint/cache fingerprint。 |
| 中性通知、原错误与详情隔离 | `PostHookCommittedPayload` 只有四字段；`protocol/post_hook_commit_test.go` 拒绝旧 event/未知/重复字段。`app/post_hook_query_test.go` 验证计划中性、错分支不替换快照、详情序列和原正文隔离。`tui/post_hook_commit_test.go` 验证 Error/Conflict 与检查状态并列、旧消息/旧页隔离和草稿锚点。新增四份失败列表/详情/信任/窄屏 golden。 |
| 进程、安全与隐私 | `builtin/hook_process_darwin_test.go` 在失败事件下验证真实只读工作区、禁网、私有路径不可读与敏感环境不继承。`failure_hook_owner_test.go` 用假时钟/通道覆盖阻塞 stdin、超时、输出超限、I/O、cleanup 不确定及最终 Wait；`app/hook_host_owner_test.go` 验证失败后的 shutdown timeout 仍持有跨进程 lease。合成 raw stdout/stderr、拒绝正文、底层 cause 与凭据哨兵不进入未授权投影。 |

固定输入位于 `internal/session/testdata/current/post_hook_failure_*.jsonl`；测试运行时不生成这些输入。Provider golden 位于两家 `testdata/failure_*_output.golden.json`。没有增加生产依赖或包级可变状态。

## 本次发现并修复的边界

1. 原结果后置提交 Sync 失败后，缓存调用也必须先检查 poisoned 状态，不能只拦新 turn。`TestFailureEverySyncFaultPreventsCachedReadmission` 覆盖计划、原 result、started、finished、terminal 的前后故障，原结果/副作用均唯一。
2. 输出超限可能先于进程退出观察。当原组探测已确认停止时，多余 TERM 在 Darwin 返回 EPERM，会误记 cleanup uncertain。共享 owner 只对仍活跃或无法探测的组执行 TERM/KILL；信号调用与自然退出并发时，通过最终原组探测、唯一 Wait 与流收口确认清理。信号失败且原组仍活跃、探测失败、Wait 或排空不确定时仍失败关闭。通道屏障回归覆盖 I/O 错误前已停止与首次探测后退出两个顺序；停止组不收信号，自然退出竞态只有在最终确认清理后才确定。阻塞 I/O 的假进程按活跃/交付退出转换，永久信号失败的假进程保持活跃组，旧 uncertainty 断言保留。真实 18 KiB 输出原断言保留，相关 owner 与 Hook 用例 race 重复 50 轮通过。

## 真实 PTY

执行命令：

```sh
go test -c -o /tmp/easycode-failure-app.test ./internal/app
python3 scripts/smoke-hooks.py /tmp/easycode-failure-app.test --event PostToolUseFailure --output-dir /tmp/easycode-failure-pty
```

两家均完成 22 阶段并以 0 退出：默认拒绝、浏览/reset、重新精确信任、普通批准、真实 Write conflict、Ctrl+O 原错误/授权诊断、返回锚点、第二次冲突后检查中 Ctrl+C、清理后退出。helper 在退出后使用生产 Loader/ReplayPlanner 核验最终 `conflict/failure/cancelled` 事实，没有用渲染替代 durable 断言。结果未知的中性计划与过期消息另外通过宿主/协议/TUI 单测验证，PTY 不通过关闭 modal 来绕过审查。

| Provider | 实际原始输出 | 状态与阶段 | bytes | SHA-256 |
| --- | --- | --- | --- | --- |
| OpenAI | [openai-failure.pty.log](evidence/openai-failure.pty.log) | [exit=0，22 阶段](evidence/openai-failure.json) | 13422 | `9ac03eab5cae301d7e957598e296d1e3e38361b508cb6e75d620a59cdcdc971e` |
| Anthropic | [anthropic-failure.pty.log](evidence/anthropic-failure.pty.log) | [exit=0，22 阶段](evidence/anthropic-failure.json) | 13424 | `68cc04128527c1ddf8ef3be51665b8f20ab135153e675f6853bbdbd983b6dd3c` |

补充回归：相同新版二进制分别执行 `--event PreToolUse` 和 `--event PostToolUse`，两家均以 0 退出；成功检查、默认拒绝、浏览/reset、普通批准及取消流程保持。

专项重复命令：

```sh
go test -race ./internal/executor/process ./internal/tool/builtin -run 'TestOwner|TestHookProcessBoundsOutputAndPreservesExitClassification|TestFailureHookRunner' -count=50
```

## 质量门与平台状态

| 检查 | 状态 |
| --- | --- |
| 新增 domain/tool/runtime/session/protocol/TUI 与双 Provider 定向测试 | 通过 |
| 真实 Darwin 沙箱、36 个失败端到端及双 Provider PTY | 通过 |
| owner/Hook 输出专项 `go test -race ... -count=50` | 通过 |
| `make verify`：分支规约、gofmt、vet、Staticcheck、架构、全量、race | 通过，exit=0；[实际日志](evidence/quality-gates.log) / [退出状态](evidence/quality-gates.json) |
| `openspec validate add-post-tool-use-failure-hooks --strict` | 通过 |
| `openspec validate --specs --strict` | 55 个主规格通过，0 失败；长 requirement 信息提示保留 |
| `GOOS=linux GOARCH=amd64 go test -exec=true ./...` | 编译通过，没有运行 Linux 二进制 |
| `GOOS=windows GOARCH=amd64 go build ./...` | 生产代码编译通过，没有运行 Windows 二进制 |
| `GOOS=windows GOARCH=amd64 go test -exec=true ./...` | 全量测试编译未通过：既有 `session_picker_test.go`/`session_picker_terminal_test.go` 引用的 list helper 位于仅 darwin/linux 构建的 `session_list_test.go`；这些文件未修改。本次相关核心包测试编译通过；不宣称 Windows 全量测试或运行支持。 |

非 Darwin 仍使用既有失败关闭 adapter，无不受限 fallback。本机没有 Linux/Windows 原生 runner，跨平台结果仅为上述编译证据。首次全量 race 发现并记录输出超限的 owner 边界，修复后的最终质量门单独列示，不把失败运行计为通过。

## 文档与收口

README、总体架构、Tool 架构、capability-status、Roadmap、pitfall 已同步，并明确正常非零退出、准入不代表进程启动、取消不改选择、一次预算预留及中性通知。六个主规格路径已合并，保留原要求及场景；TUI 原来泛称 Post 的文字明确区分两个事件，和已批准设计保持一致。

首次验收时只完成 apply/verify 和主规格同步，未归档任何 change。后续用户授权的最终契约核对与归档结果见 [archive.md](archive.md)；其他 change 不在本次范围。
