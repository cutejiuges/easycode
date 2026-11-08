# 当前能力与实现边界

> 核对日期：2026-11-08。基线：当前仓库实现与已同步的 OpenSpec 主规格，包含结构化用户提问、PreToolUse/PostToolUse Hook、命令/Skill 首词补全与已验收归档的双 Provider 被动推理阅读。
> 本文记录当前代码可执行的能力；[产品 Roadmap](product-roadmap.md)保留阶段目标与剩余工作。已接受的 ADR 和归档 change 记录各自当时的决策，不能单独作为现有功能清单。

## 用户入口

| 能力 | 当前实现 | 边界与证据 |
| --- | --- | --- |
| 双 Provider | Anthropic Messages、OpenAI Responses；HTTP/SSE，OpenAI 可显式启用 WebSocket | 不隐式支持 Chat Completions，不实现登录/OAuth/订阅鉴权；`cmd/easycode/main.go`、`internal/provider/{anthropic,openai,transport}` |
| CLI | 默认 TUI、`--version`、`--config`、`--resume ID`、`--continue`、`--print`、`--json`、`--json --input-format stream-json` | `--resume` 与 `--continue` 互斥；没有 `doctor`、`provider probe`、fork 或模型切换命令；`cmd/easycode/*_test.go` |
| 会话发现 | `sessions list [--all] [--limit N]`；`resume [--all] [--config PATH]` 启动选择器 | 离线 list 无需模型配置；picker 需要配置及终端。搜索限最近至多 200 条元数据，非正文全文搜索；`internal/app/session_*_test.go` |
| 本地 slash commands | `/cache`、`/context`、`/compact [instructions]`、`/skills`、`/hooks`，以及可调用 Skill 的 `/<name>` / `$name` | TUI 命令与 Skill 附件入口，支持 `/` / `$` 首词补全及冻结来源筛选；stream-json 使用独立 typed 控制消息，普通文字不执行 slash。`/help`、`/fork`、`/model`、`/plugins`、`/mcp`、`/cost` 尚未实现 |
| 命令/Skill 补全 | 确定性筛选、上下键选择、Tab 填入、内置 Enter 执行及 Skill Enter 只填入；Esc 关闭、paste 抑制、窄屏停用 | 仅首词名称，不含文件/参数值补全、正文 mentions 或动态目录；`internal/tui/completion*_test.go`、`internal/app/completion_e2e_test.go`；候选不进入 Session 或缓存前缀 |
| 输入控制 | 多行、字素编辑、decoded paste、有界历史、同轮追加、FIFO strict-next、原子取回、停止返还 | 未提交队列/草稿只在内存；同轮追加只在完整工具 outputs 后的采样安全点提交；`internal/runtime/agent_loop_test.go`、`internal/tui/composer*_test.go` |

## 九项模型工具与执行前检查

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

九项工具共享原生 call/output 配对、结果预算和恢复。连续只读批次最多 8 路；Patch/Write/Exec/Question 为独占屏障，只有前三项需要普通副作用批准。验证入口包括 `internal/tool/builtin/specs_test.go`、`internal/app/question_workflow_darwin_test.go` 与 `internal/app/hook_e2e_darwin_test.go`。

PreToolUse Hook 是三项 mutation 的前置门禁，**不是第十项模型工具**。它读取 user/project 冻结严格 JSON、逐项默认拒绝的内存信任，并在候选和普通批准前执行；Darwin 只读工作区、禁网，每项默认 5 秒/最大 30 秒，每调用总执行预算 30 秒，stdin 最大 512 KiB、双输出各 16 KiB。headless 匹配项返回 `hook_trust_required`，无匹配沿用普通 `approval_required`。四类 durable facts 保证恢复只补偿、不重跑；不保存输出正文或 reason。PostToolUse 已支持成功 Patch/Write/Exec 的同步只读检查：执行前独立信任，真实结果先 Sync，默认 none、显式 model 结构化反馈，4 KiB/项与 16 KiB/调用、分段预算及零重跑恢复。后置错误保持工具真实成功，Ctrl+O 后置详情按 durable sequence 刷新。其余事件、async、allow/ask、参数/结果改写、plugin hooks 和热加载尚未实现。

## 历史、上下文、缓存与展示

| 领域 | 当前实现 | 仍未实现 |
| --- | --- | --- |
| 原生历史 | 两家独立 request/stream/native codecs，保留 thinking/signature/redacted thinking、encrypted reasoning、message phase 与未知 opaque 扩展；单向 HistoryProjector/TranscriptProjector | 跨 Provider/wire 转换、compacted fork、child-thread 执行与 Subagent |
| Session | append-only JSONL、批次/seq/checksum、typed 当前 decoder、跨进程 lease、验证后 repair、原生恢复与本地中断补偿；SQLite 为可重建 root-thread 索引 | title/tag、项目/worktree 聚合、全文搜索、实时索引、迁移器/历史 reader；内部 Session/native/checkpoint 无格式版本标记 |
| 上下文 | `provider_profile -> tool_catalog -> [project_instructions] -> [skill_catalog] -> committed_history -> current_input`，确定性 CachePlan、来源 fingerprint、本地 `byte_heuristic_v1`、显式窗口预算 | 远端精确 token count、自动文件 rehydrate、动态 git/env/world-state 注入 |
| 项目指令 | 从启动 cwd 显式冻结层级 AGENTS.md/CLAUDE.md，相对来源、预算截断、临时注入双 Provider 请求，不写原生历史 | 全局用户指令、includes、`.claude/rules`、热加载 |
| 缓存 | Anthropic disabled/5m/1h 显式断点；OpenAI omit/thread key；可选 WebSocket durable 前缀增量与受限回退；`/cache`/`cache.query` | 费用/价格、长期累计遥测、动态 plugin/MCP 目录策略；不能保证服务端命中 |
| Usage | raw usage 保留于各 Provider native facts；共享五项三态指标及 turn 聚合，区分未知/不适用；最近 sample/turn/summary 有界缓存观测 | 实时独立 usage 流、cost 面板、配额与完整 metrics/tracing |
| 压缩 | 手动 `/compact`、显式窗口下阈值自动摘要、独立 CompactionID/checkpoint、失败保留旧窗口、完整历史可读、取消/恢复 | 删除旧 JSONL、跨 Provider fork、PreCompact/PostCompact Hook、通用分块摘要 |
| TUI | 流式 Markdown、代码/表格/原文阅读、resize/窄屏/NO_COLOR、普通批准/问卷/Hook 审查、紧凑工具主线、Ctrl+O 分页详情、context/cache、resume picker、命令/Skill 补全、thinking/summary 活动提示及 Ctrl+O 阅读 | 主动推理配置、fork picker、后台任务 UI、通用命令管理面板 |
| 输出保存 | Exec stdout/stderr 与搜索连续前缀、授权 descriptor、ReadArtifact、独立宿主详情查询；恢复不重读 artifact | Read/Patch/Write/Skill/Question 的通用自动 artifact、自动过期、远程 artifact 服务 |

补全验证入口为 `internal/tui/completion*_test.go`、`internal/app/completion_e2e_test.go` 与 `scripts/completion-smoke.py`。候选状态只在 TUI 内存；正式提交仍执行 Skill 来源校验及既有输入生命周期，手输/补全/恢复保持双 Provider 请求和缓存等价。

相关证据包括两家 `exec_restore_test.go`、`internal/app/cache_e2e_test.go`、`resume_e2e_test.go`、`hook_cache_test.go`、`hook_resume_test.go`、`questions_e2e_test.go` 及[当前规格目录](../../openspec/specs/)。具体平台与完整测试命令以各 change 的验收记录为准。

## 配置、存储与平台

主配置为 `~/.config/easycode/config.json` 或显式 `--config PATH`，Provider 四字段可由非空 `EASYCODE_PROVIDER`、`EASYCODE_BASE_URL`、`EASYCODE_API_KEY`、`EASYCODE_MODEL` 覆盖。窗口、压缩和缓存选项只通过 JSON 配置；没有项目级主配置合并、TOML 主配置或额外凭据登录。Skills 与 Hooks 使用各自独立来源规则。

默认数据根为 `~/.easycode`：journal 为 `sessions/YYYY/MM/DD/<thread-id>.jsonl`，Catalog 为 `state.sqlite`，私有输出位于 `sessions/artifacts`。其权限、实际 descriptor 身份和 sandbox 读写根关系均在装配/执行边界检查；不向模型暴露存储路径。没有启用默认 diagnostic 日志文件、plugin 安装目录或文件 watcher。

| 平台/范围 | 实际边界 |
| --- | --- |
| Darwin | 当前真实命令/Hook 沙箱、文件发布、PTY/TUI 与完整 `make verify` 的主要验收平台 |
| Linux | 有 Unix 配置/Session/文件系统 adapter；命令与 Hook 真实执行失败关闭。交叉编译和各次记录的测试范围，不代表所有文件系统/终端场景都已原生验收 |
| Windows 等 | 缺少等价安全 opener 的运行路径明确失败关闭；全库 Windows 静态检查仍有与 main 相同的既有诊断，交叉编译单一 adapter 不表示完整质量门或功能支持 |
| 跨平台承诺 | 任意脱离子孙进程、通用沙箱、后台任务和全平台发布矩阵尚未完成；每项平台结果以对应 change 验收记录为准 |

## 后续能力与审阅入口

P6 当前交付本地 Skills 和同步 PreToolUse/PostToolUse；MCP、本地 Plugins、manifest importer 与热加载仍是 P6 后续目标。P7 Subagent 和 P8 telemetry/诊断/发布强化的批准占位尚未接入 Runtime/app。doctor、模型/Provider 动态切换、后台任务、通用迁移设计不得仅因出现在路线图或 ADR 中就视作已经实现。

- 功能使用和配置：[README](../../README.md)。
- 长期目标及剩余验收：[产品 Roadmap](product-roadmap.md)。
- 实现边界：[总体架构](../architecture/overall-architecture.md)、[Tool 系统](../architecture/tool-system.md)。
- 必须遵循的规约：[AGENTS.md](../../AGENTS.md)。
- PostToolUse 验收：[verification.md](../../openspec/changes/archive/2026-11-08-add-post-tool-use-command-hooks/verification.md)。
- PreToolUse 验收：[verification.md](../../openspec/changes/add-pre-tool-use-command-hooks/verification.md)。
- 命令/Skill 补全：[归档核对记录](../../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/archive.md)、[追加验证报告](../../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/revalidation.md)。

推理展示只读取 Anthropic thinking 与 OpenAI summary_text 白名单，opaque-only 为 unavailable；签名、密文、raw 和未知扩展不进入显示或 headless。主线不累计完成推理行，活动显示共用 64 KiB/50 项，查询正文与其他详情共用四页缓存。提交来源与 sample/block 映射仅在内存中存在，Session/native/checkpoint 当前格式不变。验证入口为 `internal/provider/*/reasoning_stream_test.go`、`internal/provider/*/reasoning_restore_test.go`、`internal/runtime/reasoning_test.go`、`internal/tui/reasoning_test.go`、`internal/headless/reasoning_test.go`、`internal/app/reasoning_e2e_test.go` 与 Darwin `scripts/reasoning-smoke.py`。
