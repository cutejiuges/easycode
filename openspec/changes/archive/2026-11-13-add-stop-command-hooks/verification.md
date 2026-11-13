# Stop command Hooks 实施与验收

核对日期：2026-11-13。工程基线为 `539d54eb410b816fb4d04e1da8b7c4f5af22cb91`，开发分支 `feat/stop-command-hooks`。以下按时间保留实施与追加验收记录，最新归档结论见文末；未提交或推送。历史规划阶段的检查不作为新增功能的实现证据。

## 选择依据与参考设计复核

当前三类工具 Hook 已有冻结来源、逐项默认拒绝信任、只读沙箱和唯一进程 owner；缺少的是成功无调用答复的完成门禁。Stop 能复用这些边界并形成独立纵向闭环，因此优先于同时引入 Plugins/MCP、后台进程或 Subagent。它不增加第十个模型工具，也不改变修复工具的权限。

两份参考工程均只读检查，未修改、作为构建依赖或复制大段实现：

| 固定参考 | 再次核对的依据 | 本次取舍及实现证据 |
| --- | --- | --- |
| `claude-code-sourcemap@a8a678cb6244e6770e1e421767ff0987a1d95549` | `restored-src/src/query.ts` 的无 calls/API error 分支与 `handleStopHooks`；`restored-src/src/utils/hooks.ts` 的 `executeStopHooks`、active、last assistant text | 完成前检查、错误不触发、false/true/true、同回合续轮。EasyCode 用精确 turn/sample 投影，空答复不回退；TUI 审查、独立反馈及取消经真实 PTY 验证 |
| `codex@c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee` | `codex-rs/core/src/session/turn.rs` 的 `run_turn_stop_hooks`、阻断消息记录和 continuation；设计中的 hooks suite 对照 | 保留同 turn、先保存反馈再采样，以及双方原生历史优势；增加两次上限、来源绑定、独立意图/交付和离线补偿，避免摘要吞掉未消费 reason |

主动保留的差异：默认拒绝且信任仅在实例内；none 阻断结束；只有正常 exit0/model 实际阻断项可续轮；exit2 不转发 stderr；坏来源不能冒充无 Stop；headless 不执行；不增加异步、HTTP/prompt/agent Hook、热加载或跨平台沙箱。

## 实际结构与契约核对

- `domain`：封闭 tool_call/turn_stop target、Tool/Stop scope、checks/unavailable 计划、六种决定、精确 sample 文本、冻结反馈、FeedbackOnly/WithSteering。运行身份只有唯一当前结构。
- `extension/hook` 与 `tool/builtin`：四事件严格配置与专属输出解析，复用现有 HookRunner、launcher、stdin/双流/Wait/原组清理 owner。
- `provider`：两家精确正文投影、hook_feedback user-level 包装、独立来源、严格恢复交叉校验；一个 prepared owner 原子 finalize 一/两个 native entry，原有 opaque、配对和稳定工具前缀保持。
- `runtime`：`stop_hooks.go` 编排完成检查，`stop_delivery.go` 管理本地交付义务及真实输入，`stop_recovery.go` 只消费冻结事实；自动压缩复用原 guard 和失败抑制。
- `session`：只新增 `turn_stop_hook_planned/decided`，复用 started/finished；sample/usage/plan、最终结果/决定、反馈交付、terminal 各有明确批次与取消接收点。JSONL 不增加版本字段或兼容 reader。
- `app/tui/protocol`：target/plan/detail sequence/phase 绑定通知及范围查询；复合 finalize 后一次发布实际原生位置；独立 Hook 来源不成为用户 recall，复用全局四页缓存及异步 owner。

两份 delta 的输入控制/工具循环修订均已实现：continued 决定闭合后唯一 input owner 只领取一次连续 auto 前缀，strict-next 保持屏障，真实 InputID、原文和冻结 Skill 附件只在实际复合 Sync 后激活。

## 回归矩阵

| 范围 | 实际验证入口 | 核对结果 |
| --- | --- | --- |
| 强类型、关闭未知 shape、来源/实际最后项 | `internal/domain/stop_hooks_test.go`、`internal/protocol/stop_hooks_test.go`、架构边界测试 | 互斥 target、无假 invocation/InputID、正常 exit0/model 准入、旧字段/未知/额外版本字段拒绝，深拷贝 |
| 配置、输出、编码长度 | `internal/extension/hook/stop_config_test.go`、`stop_output_test.go`、fuzz seeds | 四事件共享限制/id/revision，Stop 无 matcher，reason 4 KiB、双流 16 KiB、实际转义长度、严格 JSON 和 none/exit2 丢弃 |
| 双 Provider 原生/请求/缓存 | 两家 `stop_projection_test.go`、`stop_hook_cache_test.go` 与固定 `testdata/stop_*.golden.json` | 精确空/重复/空白 sample、opaque 隔离；两种交付、revision +1/+2、native 顺序、Skill、live/restored bytes、footprint；Anthropic TTL/marker、OpenAI key/full/incremental |
| 后续完整窗口摘要 | 两家 `stop_hook_cache_test.go` | 已消费反馈进入来源 digest；改变反馈改变 digest；checkpoint 另计 revision，完整历史和 transcript 保留，恢复请求一致 |
| 当前 Session grammar | `internal/session/stop_replay_test.go`、`stop_fixture_test.go` | 计划/连续前缀/started/决定、单/三记录交付、提前采样/拆批/重复交付/首次消费前压缩拒绝 |
| 固定 bytes 与损坏 | `internal/session/testdata/current/stop_*.jsonl`、`stop_replay.expected.json` | 直接读取仓库不可变输入；13 个正常/恢复窗口、2 个 checksum 正确的非法窗口；叠加 torn tail 时语义失败先于 repair，bytes 不变 |
| 旧当前 Hook fixtures | `internal/session/testdata/current/hook_*.jsonl`、`post_hook_*.jsonl` | 直接替换当前 target/run identity 及 checksum；没有新增 reader、迁移器或历史版本 |
| 预检优先级及信任 | `internal/runtime/stop_trust_test.go` | 取消→坏来源/数量→headless→平台→stdin→额度→全部信任→运行；错误 target/plan/sample/来源拒绝；取消后迟到 grant 不安装；无新增 headless 专属投影/探测/执行 |
| 同 turn 及额度 | `internal/runtime/stop_hooks_test.go`、`stop_trust_test.go`、`internal/app/stop_hook_limits_test.go` | 两次续轮、false/true/true、30 秒累计不重置；第 16 sample 通过或终止拒绝；64 calls 后文本续轮；最后 none/exit2 不借用兄弟 model 或误报次数上限 |
| 取消与写入故障 | `internal/runtime/stop_fault_test.go` | sample/intent/单独和复合交付的接收前后取消、Sync 不确定；真实 denied 不改写，未接收输入返还，已 Sync 输入 committed，不确定输入 uncertain；零后续请求/重提/第二 terminal；实际不确定/I/O 优先于迟到取消 |
| 输入唯一所有权 | `internal/runtime/stop_input_test.go` | auto A/B、retract、strict-next、屏障后 auto 的真实 loop 验证，领取一次，真实身份和事件各一次 |
| 请求预算 | `internal/runtime/stop_compaction_test.go` | 用户超限只返还 claim，重建反馈单独候选；反馈自身超限仍本地交付后失败；没有第二次摘要或下一普通请求 |
| 自动摘要与 reason 丢失回归 | `internal/app/stop_hook_compaction_test.go` | 合法摘要刻意省略 reason；只摘要旧窗口，冻结反馈/Skill 纠正留给首次请求；摘要成功/错误/空/取消均验证，取消仍完成本地交付 |
| 离线恢复 | `internal/runtime/stop_recovery_test.go`、`internal/app/stop_hook_resume_test.go` | 未开始、成功前缀、started、unavailable；真实 Provider 每个 durable 边界、pending/已交付/复合输入、摘要未启/started/completed 和重复崩溃；先旧收口后当前 loader，零旧配置/脚本/工具/网络执行 |
| 跨进程所有权 | `internal/session/stop_fixture_test.go`、`internal/app/hook_host_owner_test.go` 的 Stop 用例 | 真子进程占用/崩溃释放；repair 到 writer 连续 lease，seq 单调；阻塞清理及 shutdown 超时期间禁止新 turn/释放 lease |
| 真实只读沙箱/隐私 | `internal/tool/builtin/hook_process_darwin_test.go` 的 Stop 用例、`internal/app/stop_hook_e2e_darwin_test.go` | stdin、工作区写入/联网/私有文件读取拒绝；通过/两阻断/第三阻断/none/exit2/拒绝/无效输出；原始输出和未授权 reason 不进入 journal |
| 三种 headless 入口 | `internal/app/stop_hook_headless_test.go` | 一次模型请求后安全拒绝，答复和 usage 已保存，零脚本/Hook 外部事件。stream-json 保持现有“控制循环成功、turn.failed 报业务失败”的退出口径 |
| 带 Stop 配置的失败样本 | `internal/app/stop_hook_trigger_test.go` | 两家 Provider 的 HTTP 错误、SSE 错误、缺少终态和受控取消共 8 个场景；零 Stop 计划/信任/执行/通知、零未完成样本及 usage 提交，保留实际失败类别和唯一 terminal |
| 只读详情与 UI | `internal/app/stop_hook_query_test.go`、`internal/tui/stop_hooks_test.go`、`stop_hook_history_test.go` | target/plan/seq、迟到回复/generation、四页上限、独立 cursor、深拷贝、无 fake cursor/用户 recall；80×24/100×30/40×16、NO_COLOR、默认拒绝/paste、阶段状态 |

这些入口共同覆盖 12 份 delta 的 42 项需求及 295 个场景；现有场景通过复用原回归与新增断言覆盖，不声称“一场景一个独立测试”。原有工具、Skill、推理、输入、Session、缓存和进程 owner 测试由完整质量门继续运行。

最终将 64 calls 场景补强为真实修复链：先由 Stop 阻断文本答复，模型再发出 63 次 Read 和一次 Write，Write 经普通批准写入 `repaired.txt`，随后纯文本完成。双 Provider 均断言三次请求、64 个真实工具事实、文件 bytes、Stop 目标 sample=2 与续轮计量仍为 1；通过 Stop 不会自动批准修复工具，也不重置预算。

## 复核发现及修正

1. 具有必填 target 的 HookActivity 不适合用零值表示“没有 Post 摘要”。当前可选摘要改为指针并严格校验；旧 E2E 改为比较非空摘要的实际值，保留原正文/位置断言。
2. Stop finished 不能送入工具 invocation 索引；独立 Stop 快照接管统计。复合交付一次 finalize 后必须一次安装两个 native sequence，防止查询看到半个位置表。
3. 压缩来源 digest 必须纳入已交付 hook_feedback；双 Provider 均补齐，并用改变反馈的来源摘要回归验证。
4. Stop 详情的来源判断不能改变原 UI 默认文本来源；修复判断并保留全部既有 Markdown/推理 golden 和交互断言。
5. 实际清理不确定/I/O 结果不能被迟到取消覆盖成确定取消；同时收紧决定 validator 和执行回归。
6. current fixture 的生成仅在开发时一次完成；临时生成测试已删除。普通测试只读取固定文件，拒绝以当前 encoder 动态生成输入 fixture。

## 命令、平台与状态

原生平台：Darwin arm64，Go 1.24.13。所有服务、凭据、SSE 与脚本均为本地合成 fixture，未调用真实 Provider。

已执行的定向入口包括：

```sh
go test ./internal/runtime -run TestStop -count=1
go test ./internal/session -run 'Test(CurrentStop|Stop)' -count=1
go test ./internal/provider/openai ./internal/provider/anthropic ./internal/tui -run Stop -count=1
go test ./internal/app -run TestStop -count=1 -timeout=90s
go test ./internal/tool/builtin -run 'Stop|HookProcess' -count=1
go test ./internal/architecture -count=1
go test ./internal/app -run TestPostBothProvidersRealSandboxThreeCapabilitiesAndDisclosure -count=1
go test -c ./internal/app -o /tmp/easycode-stop-app.test
python3 scripts/stop-hook-smoke.py /tmp/easycode-stop-app.test --output-dir /tmp/easycode-stop-pty
```

PTY 在两家 Provider 分别完成：默认拒绝审查、完成用途说明、/hooks 详情与 reset、再次审查、同回合继续、Ctrl+O 独立 Hook 行、冻结反馈与检查 metadata、80×24 resize、返回主线、取消并等待清理、退出。每家实际 `STOP_TERMINAL_EXIT=0 REQUESTS=4`；原始 PTY 与阶段结果保存在 `/tmp/easycode-stop-pty`。共享脚本另回归 PreToolUse/PostToolUse/PostToolUseFailure。

跨目标命令为 `GOOS=<目标> GOARCH=<架构> go build ./...` 与 `GOOS=<目标> GOARCH=<架构> go test -vet=off -exec /usr/bin/true ./... -run '^$'`，覆盖 Linux/Windows × amd64/arm64。第二条只编译测试，不执行目标二进制，不作为跨平台原生运行或质量门通过证明。

最终完整质量门、主规格同步与 strict validation 的退出状态见下方交付核对；48 项实施任务均已完成。

## 交付核对

- 已同步 12 份主规格，修改 25 项并新增 17 项需求；57 份主规格 strict validation 和本 change strict validation 均通过。再次计算同步结果无差异，既有场景保留。
- `openspec validate --all --strict --json` 为 58/59；唯一未通过项为既有 `add-pre-tool-use-command-hooks`。实施前它已有 resume 场景缺失和 ADDED 已存在冲突；同步 Stop 后该旧 delta 也缺少新增 Stop 场景。本次未修改或归档该旧变更，不将该检查声称为全绿。
- 第一次完整 race 在包级 10 分钟总时限触发时，仍在既有 `TestWriteSSEChunksCancellationEOFAndLimitsPrecedeSideEffects/anthropic/aggregate-limit` 中计算，未报告数据竞争或业务断言失败。实施前同一包 race 基线为 513.017 秒，本次新增回归后需扩大进程总预算；所有业务 deadline、样本规模与断言保持。
- 后续完整命令 `make verify GO=/tmp/easycode-stop-go` 退出 0，app 单测 115.228 秒、race 598.555 秒。临时 wrapper 仅在 `go test` 增加 `-timeout=20m`，其余命令原样执行；不在仓库引入豁免、跳过或改弱测试。

临时 wrapper 的完整内容：

```sh
#!/bin/sh
if [ "$1" = test ]; then
  shift
  exec go test -timeout=20m "$@"
fi
exec go "$@"
```

包级预算随后固化为 Makefile 的 `TEST_TIMEOUT ?= 20m`，test/test-race 共用该有限总时限；后续最终验收使用普通 `make verify`。这是测试进程的总运行预算，所有操作级 timeout、取消屏障、样本量、业务断言及 race 检测均保持。临时 wrapper 不属于交付依赖。

最终普通 `make verify` 退出 0；包含最后补强的双 Provider 工具修复链，app 单测 113.395 秒、race 587.094 秒。分支规约、gofmt、go vet、Staticcheck、全量架构及业务测试、全量 race 均通过，完整输出位于 `/tmp/easycode-stop-verify-final.log`。最终再次执行 `git diff --check`、57 份主规格 strict validation 和本 change strict validation，均退出 0；未修改 go.mod/go.sum，也未保留临时 fixture 生成测试。

十二份 delta specs、状态表、两份固定参考及实际测试已完成逐项复核；主规格同步完成，实施任务 48/48。本节验收完成时停在已验证、待独立归档阶段；之后的归档结果见文末。

## 2026-11-13 追加能力、回归及端到端复核

用户要求再次充分验证后，重新审查当前实现及测试覆盖；本轮补充失败触发边界测试，未修改业务代码、规格契约或依赖。原有 Provider 失败测试未安装 Stop 配置，新增 `TestStopNeverChecksFailedOrCancelledProviderSamples` 将合法 Stop 声明与四种失败同时放入真实 App/HTTP/SSE/Session 链路，两家 Provider 共 8 个场景均通过。取消使用已发送不完整样本的 channel 握手，不依赖 sleep；进一步验证私密错误正文、合成 key 及未完成样本正文均未进入 journal。

本轮新增运行记录：

| 验证 | 命令及原始结果 | 结果 |
| --- | --- | --- |
| 失败触发边界 | `go test ./internal/app -run TestStopNeverChecksFailedOrCancelledProviderSamples -count=1 -v -timeout=90s`；`/tmp/easycode-stop-trigger-revalidation.log` | 8/8 通过，包运行 1.821 秒 |
| Stop 输出 fuzz | `go test ./internal/extension/hook -run '^$' -fuzz '^FuzzStopOutputNeverDisclosesUnauthorizedBytes$' -fuzztime=45s -parallel=4`；`/tmp/easycode-stop-fuzz-revalidation.log` | 4,615,658 次执行通过，无崩溃或未经授权的反馈 |
| 核心随机顺序 race 回归 | `go test -race -shuffle=20261113 -count=5 -timeout=5m -json ./internal/domain ./internal/protocol ./internal/extension/hook ./internal/provider ./internal/provider/openai ./internal/provider/anthropic ./internal/runtime ./internal/session ./internal/tui -run 'Test(CurrentStop\|Stop)'`；`/tmp/easycode-stop-shuffle-revalidation.jsonl` | 9 包通过；106 个叶场景各执行 5 次，共 530 次，无失败/跳过；不计空运行的子进程 helper |
| 完整 App 随机顺序 race 回归 | `go test -race -shuffle=20261113 -count=3 -timeout=10m -json ./internal/app -run '^TestStop'`；`/tmp/easycode-stop-app-revalidation.jsonl` | 退出 0，293.048 秒；80 个叶场景各执行 3 次，共 240 次，无失败/跳过；不计空运行的 PTY helper |
| 双 Provider 真实 PTY | 重新 `go test -c ./internal/app -o /tmp/easycode-stop-revalidation-app.test`，分别运行 `scripts/stop-hook-smoke.py` 与 `scripts/smoke-hooks.py --event PreToolUse/PostToolUse/PostToolUseFailure`；`/tmp/easycode-stop-revalidation-pty` | 8/8 流程退出 0；每家 Stop 22 阶段、Pre 14、Post 21、Failure 22 |
| 实际构建与编译 | `go build -trimpath -o /tmp/easycode-stop-revalidation ./cmd/easycode` 及 `--version`；四个 Linux/Windows × amd64/arm64 测试编译；`/tmp/easycode-stop-revalidation-*-compile.log` | 全部退出 0；交叉测试只编译，不声称原生运行 |
| 规格与格式 | 主 specs、本 change strict validation 及 `git diff --check`；`/tmp/easycode-stop-main-spec-revalidation.json`、`/tmp/easycode-stop-change-revalidation.json` | 57/57 主规格及本 change 通过；全仓库仍为 58/59，唯一失败是旧 PreToolUse delta 遗漏现有场景，见 `/tmp/easycode-stop-all-revalidation.json` |
| 完整质量门 | 普通 `make verify`；`/tmp/easycode-stop-revalidation-verify.log` | 退出 0；gofmt、go vet、Staticcheck、分支规约、架构测试、全量业务测试及 race 全部通过；app 普通测试 118.111 秒、race 667.065 秒 |

本轮实际端到端仍使用本地 Provider fixture 和 Darwin 只读沙箱，未调用真实外部服务；跨平台编译不能替代目标平台运行。仓库级旧 PreToolUse delta 的已知规格漂移仍按上节单独报告。

App 三轮 race 重复运行最初设置了 4 分钟包级总预算，在第三轮触发该总预算；当时正在运行的摘要场景仅启动 1 秒，前两轮全部场景均通过，未报告业务断言失败或数据竞争。原输出保留在 `/tmp/easycode-stop-app-revalidation-4m.jsonl`。保持 seed、三次重复、所有场景和操作级 deadline 不变，将本轮命令的包级总预算改为 10 分钟完整重跑；该调整不修改 Makefile 或产品运行预算。

对 App JSON 测试输出另做逐项终态核对：两家 Provider 的 third-block、none、exit-two、deny、invalid 各重复三次，30 个实际错误码分别为 `stop_hook_limit_exceeded`、`hook_denied`、`hook_denied`、`hook_trust_denied`、`hook_output_invalid`，全部符合预期。叶场景统计检查每项恰好重复三次，原始 summary 为 `/tmp/easycode-stop-app-revalidation-summary.json`。

追加复核已完成，核心层与 App 的 186 个叶场景共执行 770 次，均通过；加上输出 fuzz、真实沙箱/工具修复链、双 Provider PTY、不可变 fixture/恢复与完整质量门，本轮未发现新的业务缺陷或回归。补充测试和验收记录均保留在当前工作区，未提交、推送或归档。

## 规格漂移修复与归档前确认（2026-11-13）

用户后续明确要求先修复旧 PreToolUse delta，再归档本变更。旧 delta 的 11 项落后需求已完整对齐主规格并保留后续 30 个场景；原有需求和场景均保留，旧变更继续活动且不在此次归档范围。共享架构规则及共同配置 Purpose 同步当前四事件交付事实，业务代码、协议和依赖不变。

归档前 `openspec validate --all --strict --json` 已由 58/59 恢复为 59/59，两个活动变更均零 issue，原始结果 `/tmp/easycode-pre-drift-after.json`。Stop 48 项任务无未完成项，CLI 返回的 12 份 delta 共 42 项需求、295 个场景逐块与主规格一致，无待同步差异。实际测试入口、Session/native record vocabulary、架构、Roadmap、pitfall 及文档引用已核对；规划期过时测试文件名已替换为真实入口。执行完成后的归档结果见下一节。

## 归档结果（2026-11-13）

`openspec archive add-stop-command-hooks --yes --json` 已退出 0，归档目录为 `openspec/changes/archive/2026-11-13-add-stop-command-hooks`；`.openspec.yaml`、全部 artifacts、48 项完成任务与验收记录随目录保留。未使用跳过校验选项；主规格此前已同步，CLI 报告零新增/修改/删除/重命名操作，归档前后 57 份主规格 SHA-256 全部相同。原始 CLI 结果为 `/tmp/easycode-stop-archive-result.json`。

归档后 `openspec validate --all --strict --json` 退出 0，58/58 通过：57 份主规格与仍活动的 PreToolUse 变更均有效，原始结果 `/tmp/easycode-stop-postarchive-validation.json`。数量减少一项是已完成 Stop 退出活动变更集合；PreToolUse 没有被移动或归档。README、总体架构、能力清单、Roadmap、pitfall 的 Stop 引用均已改为归档路径；45 份 Markdown 中 158 个本地文件链接全部有效，24 个实际测试路径、PTY 入口及 15 份固定 Stop journal 已核对。

归档后普通 `make verify` 已完整退出 0，日志 `/tmp/easycode-stop-archive-make-verify.log`；包含分支规约回归、gofmt、go vet、Staticcheck、架构边界、全量测试与 race。app 普通测试为 114.673 秒；本阶段没有改动 Go 代码，app race 复用同代码上轮已通过的 Go 缓存，其余逐包状态保留在日志中。旧 Pre delta 的原 158 个场景全部保留，当前共 188 个，恢复 30 个且删除 0 个；需求名称与顺序均保留。本阶段仅修改规格、文档及归档位置，业务代码、依赖和工具权限不变。未提交或推送。

## 合并远端 main 与发布前组合验证（2026-11-13）

用户后续要求合并远端 main、解决冲突并推送创建 PR。Stop 首先保存为本地提交 `4debb76`，pre-commit 完整通过；随后合入 `origin/main@35a4ede`（本地 Plugin Skills，PR #38）。README 与四份架构/路线图文档的冲突按两项能力并存解决，不覆盖已交付的 Plugin 技能包；核心源码自动合并后，另核对了旧 Stop/Tool reconciliation 始终先于当前 Plugin/Skill/Hook 来源加载。

复用既有压缩矩阵新增 `TestStopCompactionKeepsFrozenPluginCorrectionAfterSourceRemoval`：两家 Provider 分别验证正常/失败/空/取消摘要，排队的限定名插件附件准备后删除整个插件目录；实际交付保留 InputID、原文、plugin scope/namespace 与冻结正文，私有来源路径不进入请求，取消时未接纳输入不激活，重复恢复不重读旧来源。与原本地 Skill 的八组场景合计十六组均通过（7.144 秒）；原有恢复窗口断言保留。Runtime、双 Provider、TUI 与架构的 Stop/Plugin 定向回归通过，全仓库 strict 为 60/60。

暂存完整检查发现新增 40×16 信任快照的一处尾空格；将同义提示缩短为 `Earlier tool effects are not undone.`，按真实 View 一次更新三种尺寸的固定 trust 快照，随后删除临时生成入口。普通测试仍逐字节读取固定快照，未增加运行时生成分支；暂存检查无尾空格，九份 Stop snapshot 与默认拒绝行为保持验证。

本节记录冲突解决和定向实测；发布前完整质量门由正常 pre-commit/pre-push 钩子继续执行，其最终结果与实际平台端到端回归随 PR 报告。此次合并不增加 Hook 事件、插件执行贡献、格式版本或历史 reader。
