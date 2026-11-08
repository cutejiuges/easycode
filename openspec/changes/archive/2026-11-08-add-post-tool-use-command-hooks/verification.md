# PostToolUse command Hooks 验收

日期：2026-11-08。平台：Darwin 27.0.0 / arm64，Go 1.24.13。变更：`add-post-tool-use-command-hooks`；基线 `d7af3c9`，开发分支 `feat/add-post-tool-use-hooks`。本报告记录实现阶段工作区的验收，不代表其他隔离工程的合并结果；推送前完整复验见 [revalidation.md](revalidation.md)。

## 交付行为与选择理由

当前已实现成功 Patch/Write/Exec 后的同步只读、禁网检查，默认 `feedback=none`。只有显式 model、当前实例信任、严格结构化且预算接纳的 additionalContext 才进入模型历史和 Session。真实 result 先 Sync，后置失败不改写工具成功；取消与不确定保留唯一 owner，恢复不重跑旧工具/脚本，不为未启动后缀补发模型提示。

该切片复用已经存在的安全配置 FD、信任审查、exclusive 骨架、进程 owner 和三项工具消费者，补齐修改后的检查反馈闭环。相比继续扩张 Skills 的 allowed-tools、fork、frontmatter hooks 或热加载，它的前置生命周期已经具备。体验沿用 Claude Code 的工具结果与独立后置检查；内核采用可验证的强类型元数据投影、双 Provider 原生 output 及独立恢复证据。

两参考工程保持只读：Claude Code 2.1.88 sourcemap `a8a678c`、Codex `6f8bf683c`。参考位置、差异与取舍见 design.md §2 的证据表；没有修改、依赖、构建或大段复制参考工程。没有实现另一隔离进程负责的 reasoning 展示。

## 命令与结果

| 命令 | 退出码 | 结论 |
| --- | --- | --- |
| `make verify` | 0 | 最终代码的分支策略、格式、vet、Staticcheck、架构边界、全量测试及 race 全部通过 |
| `openspec validate add-post-tool-use-command-hooks --strict` | 0 | 最终 change 合法 |
| `openspec validate --all --strict` | 0 | 实施验收时 51 个主规格与 2 个活动 change 共 53 项通过；归档后的数量与复核见 `archive.md`，过长 requirement 为信息提示 |
| `go test -c ./internal/app -o /tmp/easycode-post-hook-complete-app.test` | 0 | 真实 PTY 使用的当前 app 测试二进制 |
| `python3 scripts/smoke-hooks.py /tmp/easycode-post-hook-complete-app.test --event PostToolUse` | 0 | OpenAI、Anthropic 两家真实 PTY 全流程通过 |
| 同上，`--event PreToolUse` | 0 | 两家既有 Pre 交互与终端恢复回归通过 |
| `GOOS=linux GOARCH=amd64 go build ./...` | 0 | 生产代码交叉构建通过；不表示 Linux 执行沙箱验收 |
| Linux 的 builtin/app `go test -c` | 0 | 最终代码的非 Darwin 消费者可编译，不在本机执行 |
| README JSON 片段包装解码、shell 与测试 fixture 逐字比较 | 0 | 示例语法及被实际测试的多行命令一致 |
| `python3 -m py_compile scripts/smoke-hooks.py`、`git diff --check` | 0 | 脚本语法及差异空白检查通过 |

关键新增测试均实际执行，没有仅依赖测试名称推断通过。全量命令中的普通测试还覆盖既有 Pre、Skills、Provider、Session、进程、安全文件和架构规则。

最终质量门日志：`/tmp/easycode-post-hook-complete-verify.log`；双事件双 Provider PTY 日志：`/tmp/easycode-post-hook-complete-pty.log`；OpenSpec 日志：`/tmp/easycode-post-hook-complete-spec.log`。日志是本机验收辅助，业务断言与固定输入均已落入仓库测试，不依赖临时日志才能复验。

## 任务与业务证据

| 任务 | 实际测试文件与断言 |
| --- | --- |
| 1.1–1.4 | `internal/domain/hook_event_test.go` 验证 event/policy 不共用 trust/run ID、Pre/Post exit 2 分开；`post_hook_codec_test.go` 逐成员缺失、重复键、null、未知字段、旧形状、内容与预算 identity 拒绝；`hook_response_test.go` 验证封闭分支与授权候选 |
| 2.1–2.4 | `internal/extension/hook/post_config_test.go`：缺省 none、Pre 禁止 feedback、来源内跨事件 ID 唯一、32/64 合计上限、单事件匹配 8 项、别名去重、键序不影响 canonical revision、policy 修改影响 revision；`post_output_test.go`：严格输出语法、非零 exit 2、none/非法正文丢弃 |
| 2.3、2.5–2.7 | 既有 `internal/extension/localfile`、Skill/Hook loader 与 process 故障测试覆盖同 FD、路径替换、双流/stdin/取消/Wait/Done；`hook_classification_test.go` 通过纯注入矩阵验证取消/timeout/超限/I/O 唯一分类；`hook_process_darwin_test.go` 实际执行两事件，验证只读、禁网、私有 Session 不可读及最小环境 |
| 3.1–3.4、3.6–3.7 | `internal/tool/post_feedback_test.go`：canonical 包装与转义膨胀、4 KiB/16 KiB、每后项 512-byte 提示预留、budget_unavailable/零 R、前缀不驱逐、ticket 单次 base/按 sequence feedback/close；250 KiB Exec 预览在 A=256 KiB/R=16 KiB 时只保留 240 KiB 额度，空反馈不会扩展原预览 |
| 3.4–3.5 | `internal/domain/post_hook_input_test.go` 的固定完整三分支 JSON 断言不含 preview/artifact/PID/私有路径；`internal/app/post_hook_e2e_darwin_test.go` 的 raw stdout/stderr secret 哨兵在 model/none/非法结构中不进入 journal 或后续请求；固定 input_limit fixture 证明真实超限输入零 started |
| 4.1–4.6 | `internal/session/post_hook_replay_test.go` 验证独立 Pre/Post 语法、唯一成功 result、普通 error 前缀可继续、最终 finished+terminal 原子、屏障与错序、跨事件引用、预算及 feedback_digest 伪造拒绝；`post_hook_fixture_test.go` 读取不可变 bytes，不在运行时生成 fixture |
| 5.1–5.2 | `internal/runtime/post_hooks_test.go`、`hooks_test.go`、`internal/domain/hook_event_test.go`：信任早于候选/普通批准、既有工具 grant 不等于 Hook 信任、默认拒绝/headless/旧响应失败关闭、reset 与实例隔离；app resume 删除/损坏配置后仍先完成旧补偿，不恢复信任 |
| 5.3–5.4 | `internal/runtime/post_tool_outcome_test.go` 实际走三项 exclusive 路径，覆盖准备失败、拒绝、取消、错误、两项文件冲突与执行不确定；后置零运行、唯一 result，不确定进入恢复屏障。`internal/app/hook_e2e_darwin_test.go` 覆盖 Read/Write/Read/Exec 混合顺序、Post exit 2 普通错误、Exec exit 7 成功与 native pairing |
| 5.5–5.7 | `internal/runtime/post_hooks_test.go` 注入时钟、信任等待与 durable 接收屏障，验证 Pre/Post 共用 30 秒、未启动预算后缀无假 timeout、每个 plan/result/started/finished/completed Sync 前后故障、runner 身份错误、缓存输入/结果伪造及取消；`internal/app/hook_host_owner_test.go` 的 Post 真实子进程验证 shutdown 超时仍持有 workspace/lease，最终 Done 后才释放 |
| 6.1–6.4 | 两家 `post_output_test.go` 与 6 份固定 output golden 覆盖 Patch/custom、Write、非零 Exec 原 call ID/status/is_error/wire kind；RestoreRecord 要求独立 typed ModelOutput，拒绝缺失/重复证据、错配及原生正文伪造，离线预览和正式恢复共用验证步骤 |
| 6.5–6.6 | 两家 `post_output_test.go` 验证真实摘要请求包含反馈，并复用连续两次压缩/恢复断言检查工具位置、opaque、canonical request 与 cache fingerprint；Provider 与 app 的既有压缩/恢复、`hook_cache_test.go` 验证 config/trust/policy 不污染稳定前缀及 catalog；同一反馈事实的不中断/恢复 canonical request、native 顺序、opaque 与 fingerprint 等价。OpenAI `post_websocket_test.go` 通过真实本地 WS 握手验证 durable 后增量、discard/full reset 与恢复后 full request |
| 7.1–7.6 | `internal/app/hook_resume_test.go` 在每个实际 committed batch 截断并在每个补偿后再崩溃，两家零重跑/零自动采样/二次 bytes 不变；next-not-started 只有 cancelled terminal，next-started 才有 uncertain finished；`internal/session/post_hook_fixture_test.go` 的真实子进程验证 lease 竞争、崩溃释放、占用禁止 repair、seq 单调，非法完整前缀+半行保持 bytes 不变 |
| 8.1–8.3 | `internal/tui/hooks_test.go` 固定 12 份普通/窄屏 snapshot、无色纯 render、Pre/Post 默认 Enter/Esc 拒绝、paste/旧响应/modal 隔离、Ctrl+C 保留 cleanup；`post_hook_commit_test.go` 与 `post_hook_hydration_test.go` 验证 native commit 刷新/恢复仍保留受控摘要，工具关闭后 Post checking 可见、迟到的 Pre checking/terminal 均不能覆盖 Post 或重开工具、聚合失败不改变工具成功 |
| 8.4、8.7 | `internal/app/post_hook_query_test.go` 验证 expected detail_sequence、transcript_changed、授权正文控制符转义、原预览不变且零 artifact I/O；`internal/tui/post_hook_commit_test.go` 保持打开详情时注入新 durable 修订、旧回复/混页拒绝、generation 推进、首屏重载、四页缓存及工具锚点/草稿保持；既有 browser 测试验证 64 KiB 页界限 |
| 8.5–8.6 | `internal/protocol/post_hook_commit_test.go` 与 `internal/headless/post_hook_commit_test.go` 严格校验内部刷新事件且 headless 不产生新外部事件；app 三入口 headless Post fixture 验证失败关闭；Session 的同 invocation Pre 0/Post 0 独立历史及 app actual transport 验证 Sync→快照→事件顺序 |
| 9.1–9.2、9.6 | `internal/app/post_hook_e2e_darwin_test.go`：两家×三能力×none/model 真进程检查与模型继续，额外 stdout/stderr 哨兵；文档同一 wrapper 实际验证 TODO 返回 exit 0 additionalContext、clean 返回 `{}`、文件缺失非零且无反馈；PTY 覆盖批准、详情、取消、resize/无色和 clean exit |

## 固定事实与架构核对

- 新增唯一当前 kind：`tool_post_hook_planned`、`tool_post_hook_completed`；`hook_execution_started/finished` 使用带 event/plan digest 的当前身份及 typed feedback。没有 schema_version、旧 reader、migration 或 resume 原地重写。
- 13 份 `post_hook_*.jsonl` 覆盖 plan、成功 result、started、部分 finished/下项 started、completed、none、拒绝、预算后缀、stdin 超限、两种恢复终态与非法前缀。受身份影响的 Pre 固定 bytes 同步替换。
- 独立恢复证据校验发现三份手工 Anthropic fixture 显式含 encoder 不产生的 `is_error:false`；已替换为唯一当前 canonical 形状并重算固定校验值，没有放宽 reader。
- Session 从前序事实重建 A/R 和 ticket、校验反馈增量/最终 `feedback_digest`；工具输出仍编码在原生 tool output 角色，不伪造 user/system/developer/InputID，不反向使用 SemanticHistoryView 构造请求。
- 生产新增代码无可变包级全局、无 any/动态 map 公共协议、无跨层反向依赖；New/getter/render 仍纯内存。进程与 durable worker 使用原 owner、done 与清理链，没有新增后台 helper waiter。
- README、总体/Tool 架构、capability-status、Roadmap、pitfall 与 proposal/design/delta specs 已同步；任务按实际实现与测试路径核对。本次归档已逐一同步并比对 10 份主规格；归档阶段核对、文档引用修复和质量门复跑见 `archive.md`。

## 运行边界

命令 Hook 的实际沙箱执行仅在本机 Darwin 验收；Linux 只有交叉编译，其他 adapter 继续在执行前失败关闭。所有 Provider、WS、headless 和 PTY 验收使用本地 mock 与合成凭据，未访问真实模型或使用真实 key。没有验收 Windows 原生运行，也没有声明新增平台支持。

不包含其他 Hook 事件、PostToolUseFailure、参数/结果改写、自动格式化、async、Plugins/MCP、Skills fork/权限声明或热加载。显式授权反馈可以包含用户愿意披露的工作区文字，默认关闭并非任意内容脱敏保证。P6 整体仍未退出。
