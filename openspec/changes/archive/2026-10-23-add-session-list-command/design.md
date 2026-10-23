## Context

动机见 [proposal.md](proposal.md)。当前 `internal/session/catalog` 的 `Entry` 已保存所需元数据，`LatestCompatible` 只暴露单条精确选择；`Reconcile` 已具备 descriptor-relative 枚举、逐 journal lease、Loader/ReplayPlanner/Projector、短 SQLite 事务。`internal/app/app.go` 中 `selectContinueThread` 将索引选择与 `sessionService.resume` 分开，但普通 app.Run 会先进入聊天配置和资源装配，不能直接复用为离线列表入口。

本设计涉及 CLI/app/catalog 跨包接口及存储生命周期，所以创建 design artifact。当前 SQLite schema v1 足够，本变更不改变 JSONL/SQLite schema、Provider native payload、RuntimeEvent 或工具契约。

### 两参考工程的只读实现依据

调研基于本机 `/Users/cutejiuge/IdeaProjects/` 下的参考工程，不是线上最新版本声明；未修改、构建或引入依赖。Claude Code 为 sourcemap 恢复源码，包含编译展开及禁用分支，不能把所有代码名都当作已启用产品功能。

| 参考位置（相对各自仓库） | 观察到的行为 | EasyCode 取舍 |
|---|---|---|
| Claude `restored-src/src/commands/resume/index.ts`、`resume.tsx`，`ResumeCommand`、`filterResumableSessions` | `/resume` 无参数显示 picker；加载同仓库日志、切换全部项目；过滤 sidechain 和当前会话；选中后才加载完整日志 | 采用范围优先、发现后显式恢复、root 会话优先；当前没有可复用的 slash command/picker，本次继续已确认的独立 CLI，不伪装实现无参数恢复 |
| Claude `restored-src/src/components/LogSelector.tsx`，`buildLogLabel`、`buildLogMetadata` | 紧凑标题与元数据分层、宽度截断、跨项目附加路径、标题筛选；深度/agentic search 在所读快照中存在禁用分支 | 采用紧凑多行与显式路径；现有 Catalog 禁止正文索引且无标题，首行用完整 ID，暂不读取正文生成摘要或承诺智能搜索 |
| Claude `restored-src/src/utils/sessionStorage.ts`，`loadSameRepoMessageLogsProgressive`、`loadAllProjectsMessageLogsProgressive` | 先收集轻量日志并逐步 enrich，支持跨 worktree 范围；部分发现路径依赖 stat/mtime | 采用有界展示和元数据投影；继续按 durable record 时间排序，不能照搬 mtime 作为 recency，也不新增宽松旁路 reader |
| Claude `restored-src/src/utils/crossProjectResume.ts`，`checkCrossProjectResume` | 跨项目提供 shell-quoted 的 `cd … && claude --resume …`；同仓库直接恢复的分支还有用户类型 gate | 采用显式告知创建目录；EasyCode resume 契约固定使用启动 cwd，首版不生成 cd 命令、不切目录、不复制剪贴板，不把有 gate 的能力概括成普遍行为 |
| Codex `codex-rs/cli/src/main.rs`，`ResumeCommand` | `resume`、`--last`、`--all`，全部模式显示 cwd；另有非交互会话来源选择 | 采用 `--all` 与明确范围；当前 Session 未记录交互来源，不虚构来源过滤；既有 `--continue`、`--resume UUID` 保持 |
| Codex `codex-rs/thread-store/src/local/list_threads.rs`，`list_threads` | 列表请求独立于恢复，支持 cursor、排序、过滤、page；与 state DB/rollout 协作 | 采用独立强类型查询、先筛选后 limit 与 `has_more`；先固定一种稳定排序，不提前实现 cursor 协议或新的索引后台服务 |
| Codex `codex-rs/tui/src/resume_picker.rs`、`resume_picker/page_loading.rs` 及 pagination error snapshot | 明确 scope/provider filter、更新时间/创建时间排序、分页状态和加载失败展示；构造状态默认 UpdatedAt | 采用默认最近活动、范围提示和失败可见；不照搬 Provider 隐藏规则，列表不要求配置，因此显示所有 Provider 并在恢复时检查 |
| Codex `codex-rs/utils/cli/src/resume_command.rs`，`resume_command` | 恢复 hint 独立格式化，名称需 shell quoting，缺名称可用 ID | 采用可复制的完整 UUID 和统一恢复指引；不引入名称歧义、名称查找或任意字符串命令拼接 |

参考 revision：Claude sourcemap `a8a678cb6244e6770e1e421767ff0987a1d95549`；Codex `044da98b4a7ff2dd0079f706280e0a6934048222`。这些是调研快照标识，不要求开发/测试环境具备参考仓库。

推演结论：Claude Code 的完整优势是可搜索的交互恢复选择器，本次只能交付其“局部范围优先、条目紧凑、恢复指引清楚”的基础。Codex 的查询/恢复分离适合当前架构，稳定排序和有界结果可立即落地；两者的 worktree、标题、预览和分页均需现有模型未提供的事实或 UI 生命周期，不能为了“综合优点”一次性扩张范围。

## Goals / Non-Goals

**Goals:** 将发现操作做成不进入聊天资源链的真实入口；复用一个当前事实源与既有安全协调；用可回归文本与故障注入证明输出、取消和资源所有权。

**Non-Goals:** 不产生新 Session facts，不增加恢复兼容 reader，不接入 17ad2 的命令执行能力。产品范围排除项见 proposal；不预建未来 picker facade、cursor 类型或空扩展接口。

## Decisions

### 1. 独立 CLI 路由与应用入口

新增 `cmd/easycode/sessions.go` 解析 `sessions list`；仅在 `run` 的首参数为 `sessions` 时路由，子命令选项置于 `list` 后。新的 app `ListSessions(ctx, SessionListOptions) Outcome` 直接接受输出/错误 writer、范围、limit、可注入数据路径；使用小型强类型参数，不扩充聊天 `Options` 的分支，不调用 `app.Run`。

参数和 writer 校验、取消检查先于数据资源打开。首版默认 20、上限 200，帮助走纯文本早返回。没有用户数据时复用 OpenOrCreate 生命周期，允许创建私有目录及空索引，但绝不创建 journal。`--all` 不需要取得 cwd；当前目录模式复用 `session.NormalizeCreationCWD`。列表不读取聊天 config 或项目指令，损坏 config 不影响历史发现。

替代方案：把 ListMode 塞入 app.Run 会与配置验证和 Tool 装配绑定，并直接扩大 17ad2 的合并冲突；给 `--resume` 加可选参数会改变现有 flag 语法并引入 TUI 选择状态，均不选。

### 2. Catalog 增量接口与可解释限制

在 `internal/session/catalog/list.go` 增加 `ListQuery`、`ListResult` 与 `List(ctx, query)`。query 用一个明确的 scope enum 和规范化 cwd 表示当前目录/全部目录；不允许矛盾组合，limit 必须 1–200。result 只包含 `[]Entry` 与 `HasMore`；当前查询是真实消费者，不提前设计通用分页 API。

复用 ready/decodeEntry，SQL 使用固定的两个分支及绑定参数，`ORDER BY updated_at_ns DESC, thread_id DESC LIMIT limit+1`，额外一条只用于 HasMore。先目录过滤再限制，禁止取全量条目后在 CLI 过滤。保留 `LatestCompatible` 与 Selector 的现有语义，不迫使四字段兼容查询接受空参数。无需 schema migration，也不新增第三方依赖。

索引当前仍做全量 reconciliation，结果 limit 不是扫描预算。首次实现不承诺大型历史库恒定延迟；不使用 limit 提前中断 journal 枚举，否则会错误删除未扫到的行或错过更近记录。现有支持平台为 macOS/Linux，其他平台沿用失败关闭。

### 3. 显式资源 owner 与失败语义

应用执行 `validate -> resolve paths -> open repository -> open catalog -> reconcile -> list -> close catalog -> close repository -> render/write`。资源均由本次调用唯一持有，正常路径在输出成功列表前显式关闭，错误路径确保逆序释放并返回安全 Outcome；不启动 goroutine，不在持锁状态写用户终端。

Reconcile 本来就可能修复可恢复尾部，所以本命令是“本地发现”，并非“零磁盘写入”。帮助和文档明确复用该行为；禁止引入第二套弱校验只读 JSONL parser。完整 journal 保持 byte-identical，未完成 turn 不补偿，仅合法 EOF 半行/未完成 batch 可在既有 lease/Sync 下修复。

应用在每个有副作用阶段前检查取消，并在 Reconcile、查询后检查 ctx，取消不能转为正常列表。SQLite 使用现有 SetInterrupt，查询循环检查取消；不新增等待 helper goroutine。取消不回滚已按原契约完成的尾修复或索引提交，必须如实保留结果并释放资源，不声称该命令具备跨文件事务回滚。

ReconciliationReport 的 busy/invalid 是全存储扫描计数，现有接口没有逐行 busy identity。保留 busy 已索引行、跳过未索引行，并输出全局警告；不为显示逐行状态扩张 Session 契约。正常空结果与“因 busy/invalid 不完整”的空结果通过 stderr 区分。`--continue` 的 BusyUnindexed 拒绝规则不变。

替代方案：遇到任一 busy 就阻止整个列表会使常见多终端使用无法浏览历史；静默跳过会误导用户；本设计返回可用列表并警告。列表只代表最后验证元数据，真正 resume 仍重新校验，绝不把可见条目标为 ready。

### 4. 紧凑文本、身份可复制、安全字段

采用固定的多行条目而非宽表格，长 UUID 保持完整，窄终端可自然换行；不调用终端尺寸探测、不清屏、不输出 ANSI。示意如下，具体空行在 golden 固定：

```text
Sessions in current directory (most recently updated first)
Directory: /workspace/easycode

019abcde-1234-7123-8123-123456789abc
  Updated: 2026-10-23T08:30:00Z
  Provider: openai / responses | Model: example-model

Resume: easycode --resume <thread-id>
Use matching provider, wire and model settings. Resume uses your current directory.
```

全部模式标题为 `Sessions across all directories` 并在每项加 Directory。空状态使用 `No indexed sessions found in current directory.` 或 `No indexed sessions found across all directories.`，当前范围给出 `Try: easycode sessions list --all`。HasMore 提示提高 limit 或说明 200 上限，不虚构总数。提供通用 resume 用法，用户复制完整 ID；不把任意 model/path 插入 shell 命令。

列表仅用 Entry 元数据。格式化函数是纯内存函数，不触发 IO；展示前转义 C0/C1/DEL、ESC、Unicode 行分隔和 bidi 控制符，再按 code point 上限截断且含省略标记，截断不切断转义单元。wire/model 最多 256、目录最多 512；标题中的当前目录也适用。测试把 metadata 中的控制字符视为不可信输入。UTC RFC3339Nano 避免 locale、相对时间 clock 和舍弃排序精度的问题。

stderr 警告先于 stdout 列表写出；诊断失败直接返回 output failure。输出失败不重复完整列表；已写出的部分文本无法撤销，不宣称输出原子性。仅使用稳定安全英文错误，不透传数据库、路径 opener 或 journal decoder 的底层 cause。

### 5. 与 17ad2 的冲突边界

调研时两个工作树基线均为 `afb21d6`。17ad2 的 `add-command-execution` 修改 `main` 子进程早分支、app 聊天装配、Tool/Provider/Runtime/Session record 与 TUI；本变更的生产代码集中于新的 list/catalog/app/CLI 文件，main 只改 `run` 路由。不会修改其工作树，也不把其未提交代码复制过来。

共享文档采用独立小段更新。apply 前重新检查 17ad2 status/diff，若其新增同名列表或同一 Catalog 接口，应先更新规划；若仅共享 main/docs，保持两个功能分支并各自回归。合并后重跑全门，不把当前工作树测试通过等同于合并验证。

## Risks / Trade-offs

- [当前目录不等于项目/仓库] → 用户文案明确 directory；保留 worktree 聚合为后续有事实模型支持的能力，不猜测 `.git` 或做前缀匹配。
- [ID 比标题难辨认] → 展示完整 ID、时间、模型和目录；标题需另行定义事实源及隐私契约，此次不能取 prompt 首行充数。
- [全量协调可能较慢] → 披露复杂度，支持取消；用已有小规模真实 journal 测试，不宣称 limit 提升扫描速度。实时索引/分页另行批准。
- [busy 元数据可能过时，跨模型条目无法直接恢复] → 全局不完整警告、显式配置匹配提示；恢复继续严格验证，禁止自动改配置或回退到其他 thread。
- [列表可能触发尾修复] → 文档说明，故障测试验证 lease 前零 repair、有效记录不重写、未完成 turn 零补偿。
- [参考工程含不同信任/兼容策略] → 只借鉴产品行为和边界；不复制 Codex 旧 daemon 字符串匹配 fallback、Claude mtime 排序或任何未启用分支。

## Migration Plan

实现阶段只增加新命令与列表查询，不迁移 JSONL、不修改 SQLite schema、不重写 current fixtures。通过定向单测、真实子进程 lease 集成测试、CLI 文本 golden 与完整 `make verify` 后更新架构/Roadmap/README；归档前同步本 change 的两份 delta。回滚代码即可撤销入口；本命令留下的派生 SQLite 和允许的尾修复遵循原有机制，没有新增记录需要降级 reader。
