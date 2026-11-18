# 工作区文件引用验收记录

日期：2026-11-17。分支：`feat/workspace-file-mentions`；起始 main 合并基线：`b0387c4`。宿主：Darwin/arm64，macOS 27.2.1，Go 1.24.13，Apple M1 Pro。

全部 47/47 项任务已完成，完整 `make verify` 退出码 0，全量普通与 race 均通过。六份 delta 已同步主规格，62/62 主规格、64/64 全部 changes/specs 严格校验通过；change 已于 2026-11-17 归档。

实现后又完成一轮独立代码复审，修复三个生产缺陷及 PTY 验收同步问题。最终代码重新执行完整 `make verify`，退出码 0，全量普通与 race 通过；14/14 PTY、4/4 回执单测及 64/64 OpenSpec 严格校验通过。修复前失败证据、实际命令与最新结果见 [review.md](review.md)，下方保留首次完整验收结果。

## 选择和参考依据

选择文件上下文引用，是因为已有 composer、工作区 FD、冻结 Skill 输入、双 Provider native history 和 transcript owner 可以组合出可用的完整链路，直接减少选取代码上下文的成本。MCP、Subagent 的服务/线程/预算前置条件仍需后续变更。

参考工程全程只读，没有修改或构建依赖。结束前重新核对快照：

| 参考 | 实际快照 | 核对与取舍 |
| --- | --- | --- |
| Claude Code sourcemap | `a8a678cb6244e6770e1e421767ff0987a1d95549`，2.1.88 | fileSuggestions、useTypeahead、attachments：正文路径/引号/范围和正式提交时读取；采用“先填入、再独立提交”，保留本工程严格 FD 和冻结语义 |
| Codex | `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee` | completion_target、file_search、file_search_popup、file-search：光标目标、session token、pending/display query、渐进索引与完整路径插入；加强完整代次、预算和锁外生命周期 |

具体原文件/行号和三轮复审取舍保留在 [design.md](design.md)。没有引入目录附件、图片/PDF、外部路径、watcher、自动 rehydrate、MCP 或 Subagent。

## 已实现边界

- `InputDraft` 区分 literal 与交互文件意图，正文原文、Skill、FileAttachments 独立保存。交互 `ChatSession.Append` 只接受 typed draft，普通 headless string 构造器不解释 `@`。
- 工作区来源用纯构造与显式 Start/BeginClose/Close，单搜索 worker、单准备 gate、最新查询槽、唯一结果等待路径。目录批次最多 256；同锚点不重置遍历/deadline，旧结果立即失效。
- 同路径多个范围只读一次；先验证全量引用，任一读取/元数据/编码失败整条拒绝。实际 FD 的类型、权限、dev/inode/mode/size/mtime/ctime 与读前后稳定性共同检查，不创建 Read/Write/Skill/Hook 权限事实。
- 双 Provider 分别生成原文、可选 Skill、有序文件文本块。必需 `input_metadata.files` 为 `[]` 或有序 input_id/attachments 组；引用集合、块、digest 与语义共同验证。旧开发 shape 直接失败，不存在版本 reader、迁移或原地重写。
- 强制 UserInputCommitted 与完整 4 KiB 文件摘要在 writer 接纳前构造/比对，Sync 后只发布冻结事件。主线/recall 不展开正文，Ctrl+O 提供全部来源/实际范围/截断/部分末行及冻结正文分页，共用四页缓存；离线 picker 只保留文件摘要。
- `committed_history` 的 file_attachment 按实际包装计量，与 native footprint 取覆盖值；搜索、绝对根、身份和 digest 不进入稳定前缀。内容冻结不捕获 effort；strict-next 开始时取最新选择，同轮工具/Stop/自动摘要保持原操作策略。

## 任务与业务断言映射

| 任务 | 测试与实际断言 |
| --- | --- |
| 1.1–1.4 | `internal/domain/file_mentions_test.go`、`file_attachment_test.go`：路径/选择 strict codec、引用去重、8/9 项、literal/代码/email/转义、JSON 引号/surrogate、CRLF/BOM/空文件/最终换行、UTF-8/CRLF/长行截断、固定 bytes/golden、真 digest 下非法语义、副本隔离及 ByteSize |
| 2.1–2.2 | `internal/tool/builtin/directory_names_test.go`、`workspace_file_content_unix_test.go`、`workspace_file_platform_test.go`：有限名称批次与旧 walker 字典序、实际 FD/父目录替换、symlink/FIFO/权限/属性/大小/编码故障、取消后等待读取栈、unsupported_platform |
| 2.3 | `internal/app/file_preparation_test.go`：全量验证早于来源 I/O，同路径多范围单次读取，失败计费、16/64 MiB、全或无、来源内容不递归解释 |
| 2.4–2.7 | `internal/app/file_source_test.go`、`file_bounds_test.go`；builtin `file_ignore_test.go`、`file_discovery_unix_test.go`、`file_discovery_bounds_test.go`：单 worker/busy/关门/Done、同 session 复用与 deadline、代次/索引 revision、50000/16 MiB、50/128 KiB、16000000 状态、规则优先级/剪枝/失效子树、32000000 状态、4096/4097 EOF、200000 条目、64 层、1 MiB 目录与 8 MiB 栈、关闭全部 FD |
| 3.1–3.2 | `internal/protocol/file_input_test.go`、`internal/runtime/file_input_test.go`、`input_event_preflight_test.go`、`skill_install_test.go`：typed 安装、无关 payload 拒绝、未准备 draft 不接纳、metadata/转义计费、队列额度、坏提交事件在 writer 前拒绝且不 poison |
| 3.3–3.4 | `file_effort_test.go`、`file_mentions_e2e_test.go`；runtime `input_commit_test.go` 与 TUI `file_completion_test.go`：FIFO、取回重提才重读、queued 冻结、pending+active 同时取消、Sync 后取消仍 committed、确定拒绝/Sync 不确定的不同 resolution |
| 3.5–3.6 | `internal/app/file_shutdown_test.go`、`file_mentions_e2e_test.go`：阻塞读取/取消/重复 Close、真实子进程 lease 在 Done 前仍 busy、Workspace/writer 所有权保持；只见附件后 Write 返回 `write_requires_full_read`，零重复副作用 |
| 3.7 | `internal/app/file_effort_test.go`、`effort_e2e_test.go`、`stop_hook_compaction_test.go`、`reasoning_auto_compaction_test.go` 与 runtime `effort_test.go`：准备中/queued 后选择、strict-next、工具/Stop、摘要、starting/finishing 屏障，实际请求/预检/收据共用策略 |
| 4.1–4.3 | `internal/provider/input_metadata_test.go`、两家 `file_input_test.go`：必需 files/非 null/关联顺序/完整引用集合、无附件 literal、native 与 request 固定 golden、prepare/discard/单 finalize、无损 reasoning/opaque/phase、克隆隔离 |
| 4.4 | 两家 `file_input_test.go`、`stop_hook_cache_test.go` 与 app `effort_e2e_test.go`：直接、离线预览后恢复、Stop/checkpoint 的完整 request bytes、native 顺序、footprint/revision、fingerprints；同实际设置重选但进程序号不同仍等价，未重选恢复启动值，旧 bytes 不变 |
| 5.1–5.4 | `internal/context/file_planning_test.go`、两家 `file_input_test.go`/`stop_hook_cache_test.go` 与 app 文件集成：独立文件投影、实际内容只计一次、native 覆盖、固定 cache source golden、稳定前缀隔离、压缩后不 rehydrate；Anthropic disabled/5m/1h、OpenAI omit/thread、WS full/incremental 分别比较 |
| 5.5–5.6 | `internal/domain/file_summary_test.go`、`internal/protocol/file_input_test.go`、runtime `input_event_preflight_test.go`、app `file_transcript_test.go`、TUI `file_completion_focus_test.go`：整个编码 4 KiB 的完整项前缀/省略、全部超长来源仍有完整分页、四页缓存、live/restored 摘要一致、事件不带 body/digest |
| 5.7 | `internal/context/file_planning_test.go`、两家 `file_input_test.go`、OpenAI `effort_websocket_test.go`：同文件窗口 high/low/high 只按实际策略改变 profile/properties，native 不变；无中间操作保留增量，真实失败/取消/摘要后不能复活旧 response ID |
| 6.1–6.3 | `internal/tui/file_completion_test.go`、`file_completion_focus_test.go`、`file_completion_fuzz_test.go`：引号/范围/Unicode/字素/长路径/原子拒绝、首次填入不执行、第二次独立提交、paste/换行/Esc/modal/停止、单结果等待路径与重绑、ABA/旧回包/旧索引拒绝 |
| 6.4–6.7 | 上述 TUI 测试与 20 份固定 snapshot：80×24、28×10、20×5、1×1，loading/complete/incomplete/error、64 FIFO 输入/多行/effort 通知共存、隐藏行不可接受；精确七内置参数不搜索、显式 `$effort` 可带文件，迟到 effort 保留权威选择但不清草稿或抢焦点 |
| 7.1–7.4 | app `file_mentions_e2e_test.go`、`file_effort_test.go`、`file_restore_test.go`、`file_crash_test.go`、`file_headless_test.go`：双传输/Skill+多范围+多文件/工具后追加、失败后继续、当前固定合法/非法 bytes、删除来源、真实进程 input-only Sync 后崩溃、lease 竞争禁止 repair/崩溃释放/单调 seq、二次恢复不重复、headless literal + 旧交互附件 resume/外部字段不变 |
| 7.5/7.7 | `scripts/file-mentions-smoke.py`：四组真实 Darwin PTY；另外重跑 effort 八组与 completion 两组；见下表及原始 JSON |
| 7.6 | domain/builtin/app 的有限复杂度单测、确定性大目录/规则/快速查询屏障与三个 benchmark；不以机器时长作为正确性断言 |
| 8.1–8.4 | README、capability-status、overall-architecture、product-roadmap、pitfall、主规格及本报告；`internal/architecture/file_input_boundary_test.go` 含负例；完整质量门和 strict CLI 结果见下文 |

上表缩写的 Go 测试均位于对应 `internal/` 目录。参考工程没有被作为测试/构建依赖。

## 命令及证据

| 检查 | 结果 | 原始证据 |
| --- | --- | --- |
| `make verify` | 通过，退出码 0；branch policy、gofmt、vet、Staticcheck、架构、全量普通与 race；app 普通 185.399 秒、race 976.885 秒 | [make-verify.txt](evidence/make-verify.txt) |
| 新文件功能定向 suite | 通过；app 8.214 秒，TUI 0.644 秒 | [file-targeted-tests.txt](evidence/file-targeted-tests.txt) |
| `go test ./internal/architecture -count=1` | 通过，3.276 秒；新增来源/交互边界与负例 | [architecture-tests.txt](evidence/architecture-tests.txt) |
| TUI serializer fuzz，5 秒 | 通过，171960 次；原文外部、范围、完整路径往返和字素断言 | [composer-fuzz.txt](evidence/composer-fuzz.txt) |
| 三项 benchmark，`-benchtime=3x -benchmem` | 通过；性能仅为本机观测 | [benchmarks.txt](evidence/benchmarks.txt) |
| `python3 scripts/file-mentions-smoke.py` | 4/4 通过，双 Provider × 普通/NO_COLOR；各 3 次模型请求、4 次正文读取、5 次发现；终端属性恢复 | [file-mentions-smoke.json](evidence/file-mentions-smoke.json) |
| `python3 scripts/effort-smoke.py` | 8/8 通过，全部 passed | [effort-smoke.json](evidence/effort-smoke.json) |
| `python3 scripts/completion-smoke.py` | 2/2 通过，普通/NO_COLOR、Write 拒绝、终端恢复 | [completion-smoke.json](evidence/completion-smoke.json) |
| Linux/Windows amd64 app 测试二进制交叉编译 | 两项退出码 0，只编译未运行 | [platform-compilation.txt](evidence/platform-compilation.txt) |
| 本 change `openspec validate ... --strict` | 通过 | [openspec-change.txt](evidence/openspec-change.txt) |
| 同步前 `openspec validate --all --strict --json` | 63/63：2 changes、61 specs，通过 | [openspec-all-before-sync.json](evidence/openspec-all-before-sync.json) |
| 主规格同步及同步后 strict | 新增 1 份、更新 5 份；19 项 ADDED、4 项 MODIFIED；62/62 主规格、64/64 全部项通过 | [spec-sync.json](evidence/spec-sync.json)、[主规格](evidence/openspec-specs-after-sync.json)、[全部项](evidence/openspec-all-after-sync.json)、[change](evidence/openspec-change-final.txt) |

PTY 每个阶段用键盘、结果、窗口及 Provider/读取屏障握手，不以 sleep 或概率竞态证明正确。第一份读取完成后阻塞返回再改磁盘文件，证明准备与捕获不同；queued 后取回原文再提交读取新内容，再改文件也不改 frozen request。首次候选/接受对正文、Session、Provider 均零写入/调用，忽略规则关闭重开真实刷新，停止产生唯一终态。

## 额度与性能观测

| 实际额度 | 验证方式 |
| --- | --- |
| 8 引用、4096-byte 路径、4 MiB 输入、64/16 MiB 队列 | strict 构造/codec、越界表格、全量验证先于 I/O、metadata/JSON 转义和 FIFO quota |
| 16/64 MiB 源、whole 200/lines 2000、64/512 KiB 包装 | 实际读取计数、失败消耗、同路径分组、空/长/恶意内容、UTF-8/CRLF/完整性固定 bytes |
| 50000 项/16 MiB 索引、50 项/128 KiB 结果 | 50001 项和 4096-byte 长路径，完整路径不截短，真实匹配中耗尽 16000000 状态后报告不完整 |
| 200000 条目、64 层、目录 4096/1 MiB、名称栈 8 MiB | 惰性目录 fixture 的真实计数和所有打开/关闭一致；4096/4097 EOF、整目录省略 |
| 规则 64/512 KiB、4096 规则/模式 4096 bytes、32000000 状态 | 安全/损坏/超限规则子树省略，实际大规则 NFA 工作耗尽与取消 |
| 摘要 4 KiB、详情每页 64 KiB/四页缓存、native/record 16 MiB、batch 64 MiB | 整个编码、最长完整项前缀、全部来源省略后的完整分页，明确额外页重新查询，原生/record/展示预检与不确定 Sync 区分 |

本机三次采样：长转义行冻结约 2.70 ms/op、705877 B/op、40 allocations；规则匹配约 3.64 µs/op、949 B/op、5 allocations；50000 项文件名匹配约 38.41 ms/op、2781605 B/op、21719 allocations，触及 16000000 状态预算。最后一项报告不完整覆盖是预期预算行为；这些数字不构成服务时延或吞吐保证。

## 平台与未运行范围

真实文件/取消/lease/恢复、HTTP/SSE/WebSocket 假服务及 14 组 PTY 在 Darwin/arm64 执行。Linux/amd64 与 Windows/amd64 仅编译 app 测试二进制，未在本机原生执行；其他平台缺少等价安全 opener 的路径继续明确失败关闭。没有使用真实 API key 或调用远端模型服务，测试默认不访问外网。

以上为 apply 阶段的验收结果，阶段内未归档、commit、push 或创建 PR。随后按用户确认于 2026-11-17 完成归档，记录见 [archive.md](archive.md)。
