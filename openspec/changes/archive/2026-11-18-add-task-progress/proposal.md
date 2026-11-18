## Why

EasyCode 已能完成双 Provider 的读取、修改、执行与验证，但复杂任务仍只有工具活动和文字答复，用户无法稳定查看模型当前在做哪一步。Roadmap P5 的任务展示及 P4 的压缩后计划保留尚未闭环；现在补充有界任务进度，比直接引入 MCP 的动态工具、外部进程与信任体系更适合复用现有基础并完整验收。

## What Changes

- 增加一个 `task.progress` 能力：Anthropic 使用 structured `TodoWrite`，OpenAI 使用 structured `update_plan`；分别在本地严格解码为同一有界、线程内完整清单替换契约。OpenAI 此工具显式采用服务端 `strict:false` 以保留可省略的说明，本地验证不放宽；该策略属于静态工具定义。
- 支持 `pending/in_progress/completed`、至多一个进行中、显式空清单清除和更新说明；状态跨用户回合保留。完成状态只表示模型报告，不自动证明验证通过、授予权限或结束回合。
- 复用独占调度、结果预算、原生 call/output 配对与 Session ledger。执行器只构造结果，成功结果 Sync 后才更新当前投影；失败、取消及未提交调用不改变清单。
- 提供 Claude 风格的输入区邻近清单、当前步骤与完成数量、Ctrl+T 折叠、只读 `/tasks` 和 Ctrl+O 历史详情；任务与补全共享布局预算，首次查询未完成不冒充空清单，同序号矛盾快照拒绝。headless 正常执行，stream-json 支持只读 `tasks.query`。
- 压缩时在 checkpoint 的 Provider 原生摘要包装中确定性保留精确清单；以完整 ledger 的来源核验恢复，不依赖有损摘要复述任务，不逐 sample 重复注入动态清单。
- **BREAKING**：更新唯一当前工具封闭联合和 checkpoint shape，直接替换固定 current fixtures；缺少必需字段的旧开发格式失败关闭，不增加内部版本、兼容 reader 或迁移器。
- 不包含 Plan Mode、任务执行调度器、TaskCreate/TaskUpdate 四工具体系、任务依赖/owner、Subagent、后台任务、MCP 或协作存储。

## Capabilities

### New Capabilities

- `tool/task-progress`：有界任务快照、两家 facade、原子完整替换及成功结果的生效边界。
- `tui/task-progress`：当前清单、折叠与完整阅读、只读查询和输入焦点隔离。

### Modified Capabilities

- `tool/invocation-loop`：当前目录改为十项能力，增加 typed 调用与不需批准的独占屏障。
- `tool/result-budget`：任务确认结果受既有累计预算约束，完整任务 metadata 不作为可截断 preview。
- `provider/anthropic-messages-text`：当前 structured facade 集合包含 TodoWrite。
- `provider/native-history-persistence`：任务输入、结果与 checkpoint 快照保真及双 Provider 恢复等价。
- `context/planning`：完整目录计量包含第十能力，任务内容 stamp 纳入既有历史指纹，checkpoint 清单只通过原生 footprint 计量。
- `context/compaction`：精确任务快照的确定性包装、来源核验及候选完整预算。
- `session/jsonl-store`：当前 typed ready/result 支持任务事实及 checkpoint 任务来源校验。
- `session/resume`：只从成功结果重建当前清单，补偿不重跑，联网恢复与会话选择器离线预览在 repair 前共用完整验证。
- `headless/stream-control`：严格 `tasks.query` 与关联快照输出，一次性输出保持既有行为。
- `tui/input-completion`：增加 `/tasks` 内置补全并保留同名 Skill 的 `$tasks` 入口。
- `tui/tool-transcript`：统一任务更新活动、分组边界与冻结清单详情。

## Impact

实现涉及 `internal/domain`、`protocol`、`tool`/`builtin`、两家 `provider`、`runtime`、`session`、`app`、`tui`、`headless`，并更新目录 golden、固定 Session/checkpoint fixture、PTY 验收脚本、README、能力清单、架构与 Roadmap。不引入外部依赖、独立任务文件、SQLite 真相或隐藏 goroutine。

方案参考本地 Claude Code 快照 `a8a678c` 的 Tasks/TodoWrite 和 Codex 快照 `c0cc9fc` 的 update_plan；吸收前者清单交互与后者批量更新，明确补充严格验证、durable 生效、恢复和压缩保真。参考差异、准确源码位置、实现边界和故障验证矩阵见 `design.md`。本阶段只生成待评审 artifacts，后续明确请求 apply 才实现。
