# 当前能力与实现边界

> 核对日期：2026-11-18。基线：当前仓库实现及已同步主规格，包含本地 Plugin 技能包、启动推理控制、会话内 effort 选择及本轮回归修复、结构化用户提问、PreToolUse 参数改写、PreToolUse/PostToolUse/PostToolUseFailure/Stop Hook、命令/Skill 首词补全、工作区文件引用、任务清单与已验收归档的双 Provider 被动推理阅读。
> 本文记录当前代码可执行的能力；[产品 Roadmap](product-roadmap.md)保留阶段目标与剩余工作。已接受的 ADR 和归档 change 记录各自当时的决策，不能单独作为现有功能清单。

## 用户入口

| 能力 | 当前实现 | 边界与证据 |
| --- | --- | --- |
| 双 Provider | Anthropic Messages、OpenAI Responses；HTTP/SSE，OpenAI 可显式启用 WebSocket | 不隐式支持 Chat Completions，不实现登录/OAuth/订阅鉴权；`cmd/easycode/main.go`、`internal/provider/{anthropic,openai,transport}` |
| CLI | 默认 TUI、`--version`、`--config`、`--plugin-dir PATH`、`--resume ID`、`--continue`、`--print`、`--json`、`--json --input-format stream-json` | `--resume` 与 `--continue` 互斥；没有 `doctor`、`provider probe`、fork 或模型切换命令；`cmd/easycode/*_test.go` |
| 会话发现 | `sessions list [--all] [--limit N]`；`resume [--all] [--config PATH]` 启动选择器 | 离线 list 无需模型配置；picker 需要配置及终端。搜索限最近至多 200 条元数据，非正文全文搜索；`internal/app/session_*_test.go` |
| 本地 slash commands | `/cache`、`/context`、`/compact [instructions]`、`/effort [level|auto|reset]`、`/skills`、`/hooks`、`/plugins`、`/tasks`，以及可调用 Skill 的 `/<name>` / `$name` | TUI 命令与 Skill 附件入口，支持 `/` / `$` 首词补全及冻结来源筛选；stream-json 使用独立 typed 控制消息，普通文字不执行 slash。`/help`、`/fork`、`/model`、`/mcp`、`/cost` 尚未实现 |
| 命令/Skill 补全 | 确定性筛选、上下键选择、Tab 填入、内置 Enter 执行及 Skill Enter 只填入；Esc 关闭、paste 抑制、窄屏停用 | 命令/Skill 仅首词名称；正文文件引用使用独立来源，不含普通参数值补全；`internal/tui/completion*_test.go`、`internal/app/completion_e2e_test.go`；候选不进入 Session 或缓存前缀 |
| 工作区文件引用 | 正文光标处 @ 路径补全、引号及行范围、提交时冻结、独立摘要/分页详情、双 Provider 原生恢复 | 8 引用、16/64 MiB 源读取、64/512 KiB 包装；候选会话索引 50000 项/16 MiB、渐进代次与分层 ignore、单 worker/准备 gate；不授予工具权限；`internal/app/file*_test.go`、`internal/tui/file*_test.go`、`scripts/file-mentions-smoke.py` |
| 本地插件 | `--plugin-dir`，只读 `/plugins`，限定 Skill 调用与补全；Codex 副文件调用限制 | 复用同一 Skill 工具、权限和原生恢复；无 marketplace/安装、脚本/MCP/Agent 贡献或热加载；`internal/app/plugins_test.go`、`cmd/easycode/plugin_integration_test.go`、`scripts/plugins-smoke.py` |
| 启动推理控制 | 五个严格 JSON 字段；Anthropic thinking/display/独立 effort、OpenAI effort/summary、普通输出上限；/context 只读来源 | thinking/display/summary/输出额度启动冻结，effort 按完整操作捕获；窗口预留和摘要 manual budget 预检；不探测模型、不降级或重试；两家 `reasoning_request_test.go`、`internal/app/effort_e2e_test.go`、`scripts/reasoning-controls-smoke.py` |
| 会话内 effort | `/effort` 短通知、auto/unset/reset、八项内置补全；stream-json effort.query/set/snapshot | 下一操作生效，工具/Question/审批/Stop/自动摘要冻结，独立摘要接纳时捕获；纯进程选择和序号不写 Session/配置，活动 starting/finishing 可查；`internal/headless/effort_test.go`、`internal/app/effort_e2e_test.go`、`scripts/effort-smoke.py` |
| 输入控制 | 多行、字素编辑、decoded paste、有界历史、同轮追加、FIFO strict-next、原子取回、停止返还 | 未提交队列/草稿只在内存；同轮追加在完整工具 outputs 后，或 Stop continued 决定闭合后的交付安全点提交；`internal/runtime/agent_loop_test.go`、`internal/tui/composer*_test.go` |

## 十项模型工具与执行前检查

| 能力 | Anthropic / OpenAI facade | 当前限制 |
| --- | --- | --- |
| 读取 | `Read` / `Read` | 只读，UTF-8、路径/文件大小/行与预览限制；大文件不保证形成可覆盖 Write 的完整 Read 证据 |
| 文件查找 | `Glob` / `Glob` | 工作区内有界确定性 walker；不是无限递归扫描 |
| 文本搜索 | `Grep` / `Grep` | 有界逐行 RE2、上下文和输出模式，搜索原文可按策略保存 |
| 局部修改 | `Edit` / custom `apply_patch` | 一个已有 UTF-8 文件，完整候选/diff、普通批准、原子替换；不支持任意多文件 patch |
| 整文件写入 | `Write` / `Write` | 新建或整文件覆盖，输入最多 1 MiB；覆盖需先前已交付完整 Read；同 sample 读取不能预授权覆盖 |
| 前台命令 | `Bash` / `exec_command` | Darwin，非 login `/bin/sh`、最小环境、工作区写入/禁网沙箱、单次批准、取消/超时/有界输出；不提供 PTY、write_stdin 或后台任务 |
| 保存输出读取 | `ReadArtifact` / `ReadArtifact` | 当前线程已授权 artifact 的有界 raw 范围；不产生 Write 的读取资格 |
| 本地技能正文 | `Skill` / `Skill` | 只读加载主文档/同来源资源；主文档/资源各 64 KiB，最终包装正文 128 KiB，受共享 sample/turn 预算约束；不执行脚本或提升权限 |
| 结构化提问 | `AskUserQuestion` / `request_user_input` | 1–4 题，单选/多选/其他文字/说明/明确跳过及复核；exclusive 但不需权限批准；headless 返回 `user_input_unavailable`，恢复不重问 |
| 任务进度 | `TodoWrite` / `update_plan` | 完整替换、空数组清除、最多 20 项，成功 result Sync 后应用；无普通批准/Tool Hook，`completed` 仅模型报告 |

十项工具共享原生 call/output 配对、结果预算和恢复。连续只读批次最多 8 路；Patch/Write/Exec/Question/Task 为独占屏障，只有前三项需要普通副作用批准。验证入口包括 `internal/app/tool_loop_e2e_test.go`、`internal/app/questions_e2e_test.go` 与 `internal/app/hook_e2e_darwin_test.go`。

PreToolUse Hook 是三项 mutation 的前置门禁，**不占用模型工具项**。它读取 user/project 冻结严格 JSON、逐项默认拒绝的内存信任，并在候选和普通批准前执行；Darwin 只读工作区、禁网，每项默认 5 秒/最大 30 秒，每调用总执行预算 30 秒，stdin 最大 512 KiB、双输出各 16 KiB。headless 匹配项返回 `hook_trust_required`，无匹配沿用普通 `approval_required`。四类 durable facts 保证恢复只补偿、不重跑；不保存原始 stdout/stderr 或自由文本 reason。PostToolUse 已支持成功 Patch/Write/Exec 的同步只读检查：执行前独立信任，真实结果先 Sync，默认 none、显式 model 结构化反馈，4 KiB/项与 16 KiB/调用、分段预算及零重跑恢复。后置错误保持工具真实成功，Ctrl+O 后置详情按 durable sequence 刷新。PostToolUseFailure 已覆盖已准入 error/conflict、启动失败和确认清理超时；正常非零 exit 仍走成功分支。success/failure/none 与检查 disposition 独立，双分支先信任、只 reserve 一次，失败输入只含安全摘要。取消不改写原错误，恢复零外部副作用，计划通知中性。PreToolUse 已增加三项 mutation 的完整参数改写：唯一当前 finished 保存 unchanged/replaced input_effect，串行 O/C/E 选择，重新验证目标与普通审批，执行准入独立于 Post，模型说明按真实准入和恢复事实计费；原生调用保持 O，恢复验证后零重跑。活动摘要与 Ctrl+O 详情区分候选、选择和拒绝，极窄屏停用无法完整审查的批准。其余事件、async、allow/ask、结果改写、plugin hooks 和热加载尚未实现。

Stop 已支持精确答复的完成前检查、同回合最多两次续轮、累计 30 秒及原 16/64 限额。默认 none 不转发阻断文本；显式 model 的正常 exit0 阻断才可续轮。决定和本地交付独立，必要时先摘要旧窗口，随后交付冻结反馈及真实纠正；恢复零重跑、零网络，先补反馈再结束旧回合。Hook 来源独立于用户 recall，headless 匹配要求信任，坏来源拒绝完成。证据为 `internal/app/stop_hook*_test.go`、双 Provider `stop_hook_cache_test.go`、`scripts/stop-hook-smoke.py` 和[验收记录](../../openspec/changes/archive/2026-11-13-add-stop-command-hooks/verification.md)。

## 历史、上下文、缓存与展示

| 领域 | 当前实现 | 仍未实现 |
| --- | --- | --- |
| 原生历史 | 两家独立 request/stream/native codecs，保留 thinking/signature/redacted thinking、encrypted reasoning、message phase 与未知 opaque 扩展；单向 HistoryProjector/TranscriptProjector | 跨 Provider/wire 转换、compacted fork、child-thread 执行与 Subagent |
| Session | append-only JSONL、批次/seq/checksum、typed 当前 decoder、跨进程 lease、验证后 repair、原生恢复与本地中断补偿；SQLite 为可重建 root-thread 索引 | title/tag、项目/worktree 聚合、全文搜索、实时索引、迁移器/历史 reader；内部 Session/native/checkpoint 无格式版本标记 |
| 上下文 | `provider_profile -> tool_catalog -> [project_instructions] -> [skill_catalog] -> committed_history -> current_input`，确定性 CachePlan、来源 fingerprint、本地 `byte_heuristic_v1`、显式窗口预算 | 远端精确 token count、自动文件 rehydrate、动态 git/env/world-state 注入 |
| 项目指令 | 从启动 cwd 显式冻结层级 AGENTS.md/CLAUDE.md，相对来源、预算截断、临时注入双 Provider 请求，不写原生历史 | 全局用户指令、includes、`.claude/rules`、热加载 |
| 缓存 | Anthropic disabled/5m/1h 显式断点；OpenAI omit/thread key；可选 WebSocket durable 前缀增量与受限回退；`/cache`/`cache.query` | 费用/价格、长期累计遥测、动态 plugin/MCP 目录策略；不能保证服务端命中 |
| Usage | raw usage 保留于各 Provider native facts；共享五项三态指标及 turn 聚合，区分未知/不适用；最近 sample/turn/summary 有界缓存观测；Anthropic thinking_tokens 为 known/unknown，output 已包含 thinking | 实时独立 usage 流、cost 面板、配额与完整 metrics/tracing |
| 压缩 | 手动 `/compact`、显式窗口下阈值自动摘要、独立 CompactionID/checkpoint、失败保留旧窗口、完整历史可读、取消/恢复 | 删除旧 JSONL、跨 Provider fork、PreCompact/PostCompact Hook、通用分块摘要 |
| TUI | 流式 Markdown、代码/表格/原文阅读、resize/窄屏/NO_COLOR、普通批准/问卷/Hook 审查、紧凑工具主线、Ctrl+O 分页详情、context/cache、resume picker、命令/Skill 与正文文件补全、thinking/summary 活动提示及 Ctrl+O 阅读、冻结设置查询与有界滚动 | effort 之外的运行中推理切换、fork picker、后台任务 UI、通用命令管理面板 |
| 输出保存 | Exec stdout/stderr 与搜索连续前缀、授权 descriptor、ReadArtifact、独立宿主详情查询；恢复不重读 artifact | Read/Patch/Write/Skill/Question 的通用自动 artifact、自动过期、远程 artifact 服务 |

补全验证入口为 `internal/tui/completion*_test.go`、`internal/app/completion_e2e_test.go` 与 `scripts/completion-smoke.py`。候选状态只在 TUI 内存；正式提交仍执行 Skill 来源校验及既有输入生命周期，手输/补全/恢复保持双 Provider 请求和缓存等价。

相关证据包括两家 `exec_restore_test.go`、`internal/app/cache_e2e_test.go`、`resume_e2e_test.go`、`hook_cache_test.go`、`hook_resume_test.go`、`questions_e2e_test.go` 及[当前规格目录](../../openspec/specs/)。具体平台与完整测试命令以各 change 的验收记录为准。

## 配置、存储与平台

主配置为 `~/.config/easycode/config.json` 或显式 `--config PATH`，Provider 四字段可由非空 `EASYCODE_PROVIDER`、`EASYCODE_BASE_URL`、`EASYCODE_API_KEY`、`EASYCODE_MODEL` 覆盖。窗口、压缩和缓存选项只通过 JSON 配置；没有项目级主配置合并、TOML 主配置或额外凭据登录。Skills、Hooks 与显式 Plugin 目录使用各自来源规则；插件只贡献受限技能，不借用 Hook 信任。

默认数据根为 `~/.easycode`：journal 为 `sessions/YYYY/MM/DD/<thread-id>.jsonl`，Catalog 为 `state.sqlite`，私有输出位于 `sessions/artifacts`。其权限、实际 descriptor 身份和 sandbox 读写根关系均在装配/执行边界检查；不向模型暴露存储路径。没有启用默认 diagnostic 日志文件、plugin 安装目录或文件 watcher。

| 平台/范围 | 实际边界 |
| --- | --- |
| Darwin | 当前真实命令/Hook 沙箱、文件发布、PTY/TUI 与完整 `make verify` 的主要验收平台 |
| Linux | 有 Unix 配置/Session/文件系统 adapter；命令与 Hook 真实执行失败关闭。交叉编译和各次记录的测试范围，不代表所有文件系统/终端场景都已原生验收 |
| Windows 等 | 缺少等价安全 opener 的运行路径明确失败关闭；全库 Windows 静态检查仍有与 main 相同的既有诊断，交叉编译单一 adapter 不表示完整质量门或功能支持 |
| 跨平台承诺 | 任意脱离子孙进程、通用沙箱、后台任务和全平台发布矩阵尚未完成；每项平台结果以对应 change 验收记录为准 |

## 后续能力与审阅入口

P6 当前交付本地 Skills、同步 PreToolUse/PostToolUse/PostToolUseFailure/Stop 和显式本地 Plugin 技能包（EasyCode、Claude、Codex JSON、Agent Plugins 四种受限 manifest）。MCP、插件可执行贡献、marketplace/安装与热加载仍是后续目标。P7 Subagent 和 P8 telemetry/诊断/发布强化的批准占位尚未接入 Runtime/app。doctor、模型/Provider 动态切换、后台任务、通用迁移设计不得仅因出现在路线图或 ADR 中就视作已经实现。

- 功能使用和配置：[README](../../README.md)。
- 长期目标及剩余验收：[产品 Roadmap](product-roadmap.md)。
- 实现边界：[总体架构](../architecture/overall-architecture.md)、[Tool 系统](../architecture/tool-system.md)。
- 必须遵循的规约：[AGENTS.md](../../AGENTS.md)。
- PostToolUse 验收：[verification.md](../../openspec/changes/archive/2026-11-08-add-post-tool-use-command-hooks/verification.md)。
- PreToolUse 验收：[verification.md](../../openspec/changes/add-pre-tool-use-command-hooks/verification.md)。
- PreToolUse 参数改写验收：[verification.md](../../openspec/changes/archive/2026-11-15-add-pre-tool-use-input-rewrite/verification.md)。
- 命令/Skill 补全：[归档核对记录](../../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/archive.md)、[追加验证报告](../../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/revalidation.md)。

推理展示只读取 Anthropic thinking 与 OpenAI summary_text 白名单，opaque-only 为 unavailable；签名、密文、raw 和未知扩展不进入显示或 headless。主线不累计完成推理行，活动显示共用 64 KiB/50 项，查询正文与其他详情共用四页缓存。提交来源与 sample/block 映射仅在内存中存在，这部分展示不改变存储；启动推理控制另将 Anthropic raw usage 当前 shape 扩展为必需 thinking_tokens 状态，并替换固定 current bytes，旧开发 shape 拒绝。验证入口为 `internal/provider/*/reasoning_stream_test.go`、`internal/provider/*/reasoning_restore_test.go`、`internal/runtime/reasoning_test.go`、`internal/tui/reasoning_test.go`、`internal/app/reasoning_e2e_test.go` 与 Darwin `scripts/reasoning-smoke.py`。

启动控制与观测分开：display omitted/空 thinking 仍可有正值 usage，summarized 不保证正文。摘要保留同一冻结策略及独立额度，manual 冲突在 started 前拒绝；成功/候选失败计量分别归属 CompactionID。`internal/app/reasoning_usage_restore_test.go` 验证固定正值/零/unknown、raw/normalized 不一致及旧 shape 在 repair 前拒绝；`internal/runtime/reasoning_test.go` 验证取消后仍等待已接收提交的确定结果，`scripts/reasoning-controls-smoke.py` 覆盖 Darwin 双 Provider、NO_COLOR、/context、/cache、工具后摘要/预算拒绝、阅读和取消。外部 JSONL v1 字段不增加，reasoning_output 的值/状态按新计量契约更新。

2026-11-11 全面回归补齐双 Provider 完整事件帧校验，重复成员不能覆盖 usage 或响应 ID，异常样本不提交成功事实或执行工具，合法未知扩展和连续零值观测保持原义。当前代表入口为两家 `usage_validation_regression_test.go` 的严格 usage 回归与 `internal/app/reasoning_e2e_test.go` 的工具续写和输出过滤回归；当时 24 组真实 CLI、32 个 PTY 场景、5 轮乱序 race、421,051 次 fuzz 与完整质量门证据见[归档回归记录](../../openspec/changes/archive/2026-11-11-add-provider-reasoning-controls/regression-2026-11-11.md)。启动控制的主规格已同步；运行中设置切换、真实模型服务和全平台终端验收仍不在该交付范围。

- PostToolUseFailure 验收：[verification.md](../../openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/verification.md)。

- PostToolUseFailure 独立回归与审查编码修复：[regression-2026-11-11.md](../../openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/regression-2026-11-11.md)。

- Windows 共用测试 helper 标签修复与跨平台编译追加验收：[windows-helper-regression-2026-11-11.md](../../openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/windows-helper-regression-2026-11-11.md)。

- PostToolUseFailure 最终契约核对与归档：[archive.md](../../openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/archive.md)。

- 本地 Plugin 技能包：[使用与受限字段](../../README.md#本地-plugin-技能包)、[设计与参考源码](../../openspec/changes/archive/2026-11-13-add-local-plugin-skills/design.md)、[验证记录](../../openspec/changes/archive/2026-11-13-add-local-plugin-skills/verification.md)。

## 工作区文件引用的当前边界

`domain.InputDraft` 区分交互引用与普通字面输入；`ChatSession.Append` 接收 typed 草稿，经 `Runtime.InstallInputSources` 装配的实际来源组合 Skill/文件准备后进入 admission。只有完整冻结附件进入队列与 native facts；原文仍用于 recall/取回，文件内容不递归解析。准备不捕获 operationProfile，不产生 Read/Write/Hook/Skill 权限事实。内容准备和搜索的退出先于 Workspace、writer 与 lease 释放。

两家 `InputMetadata.files` 为必需非 null 数组，项为 input_id/attachments；缺失字段的旧开发 shape 在 repair 前拒绝。文件选择、源行数、实际范围、ends_mid_line、truncated、content、digest 同时校验；真实 digest 不能掩盖非法范围。冻结 JSON 数据包装只作为用户文本块，身份、digest、绝对根与搜索状态不进入 prompt 稳定前缀。压缩窗口不自动重新附带旧文件，完整历史仍可读取；恢复等价需两侧实际推理设置相同，override/选择序号不持久化。

4 KiB 摘要只保存完整来源项前缀和 total/items/omitted，预先构造并验证提交事件；正文由 transcript owner 分页提供，离线 picker 只保留摘要。外部 headless JSONL 沿用现有字段，普通 @ 不读文件。Darwin PTY 验证两家 Provider、NO_COLOR、光标/范围、过期候选、忽略规则、准备中 effort、排队取回、strict-next、停止和终端恢复；真实 input-only 子进程崩溃验证 lease/repair/单调 seq 与重复恢复零重读。具体命令、数值和平台限制见[验收记录](../../openspec/changes/archive/2026-11-17-add-workspace-file-mentions/verification.md)。

任务清单通过 Ctrl+T、只读 `/tasks` 和 stream-json `tasks.query` 观察，Ctrl+O 保留原调用详情。精确成功来源进入 checkpoint，内容 stamp 仅参与既有 committed_history 缓存身份；不新增估算来源或普通请求附件。验证见 `internal/runtime/task_progress_test.go`、`internal/app/task_transcript_test.go` 及[任务进度验收](../../openspec/changes/archive/2026-11-18-add-task-progress/verification.md)。Plan Mode、Subagent、后台任务与协作依赖仍未实现。
