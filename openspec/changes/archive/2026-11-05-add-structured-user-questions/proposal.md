## Why

EasyCode 已能完成双 Provider 的读、搜、改、执行、压缩和本地 Skill 闭环，但模型需要用户决定时只能输出普通文本，无法在当前工具调用位置收集结构化回答并继续任务。P5 已明确规划 AskUser；在现有有序调度、Session ledger 和 TUI 基础上补齐该能力，比此时引入 Hook/MCP 的外部进程与信任体系，或后台进程的 PTY/资源生命周期，更适合形成可验证的日常使用增量。

## What Changes

- 增加 `user.question` 能力：Anthropic `AskUserQuestion` 与 OpenAI `request_user_input` structured facade，使用共享的强类型问题和回答契约。
- 支持 1–4 题、每题 2–4 个选项、单选/多选、自动提供的其他文字回答、补充说明、逐题切换与提交前复核；明确跳过和中断语义。
- 增加与活动 turn、invocation、提问请求及问题内容 fingerprint 绑定的回答控制命令和瞬态事件，由既有 AgentLoop/turn owner 完成唯一 resolution。
- 分开“调度独占”和“需要副作用审批”的判断；提问形成独占屏障，回答不产生文件/命令 grant，不借用 approval decided 事实。
- 复用 ready/started/result/native-output 事实，保存完整冻结回答；回答 Sync 后才显示已提交。覆盖崩溃补偿、重复/过期响应、取消、关闭和结果预算。
- 接入紧凑主线和 Ctrl+O 历史详情；无交互宿主返回 `user_input_unavailable` 配对结果，不等待 stdin、不自动代答。
- **BREAKING**：当前 catalog 从八能力变为九能力，并扩展唯一当前 Tool input/result、Runtime command/event 和 Session typed union；直接更新当前 fixture，不引入格式版本、旧 reader 或迁移器。

## Capabilities

### New Capabilities

- `tool/user-question`：问题/回答值、Provider facade、执行与恢复边界、无交互行为。
- `tui/user-questions`：Claude 风格问题弹窗、选项/文字编辑、复核、键盘隔离、历史回看。

### Modified Capabilities

- `tool/invocation-loop`：九能力目录、完整参数 ready、独占提问屏障和有序配对。
- `runtime/input-control`：关联回答 admission、一次接收、取消排序及唯一清理 owner。
- `session/jsonl-store`：提问 typed input/result、无需 approval 的独占 grammar 与恢复校验。
- `provider/openai-responses-text`：`request_user_input` function call 编译、归并与 output。
- `provider/anthropic-messages-text`：`AskUserQuestion` tool_use 编译、归并与 tool_result。
- `provider/native-history-persistence`：回答冻结、未中断/恢复请求等价及 opaque 语义保真。
- `tool/result-budget`：回答完整性、提交前编码预算预检和超限草稿保留。
- `tui/tool-transcript`：提问活动、已提交回答与历史详情、modal 焦点优先级。
- `context/planning`：新增工具定义的确定性来源，动态问题/回答不污染稳定前缀。

## Impact

- 涉及 `internal/tool`、`internal/protocol`、`internal/runtime`、`internal/session`、两家 `internal/provider`、`internal/app` 与 `internal/tui` 的既有边界；新增实现按职责落入小文件，保持既有依赖方向。
- 不增加第三方依赖；复用固定 Sonic codec、catalog、prepared commits、writer/lease、结果账本、composer 编辑与安全显示、宿主查询及生命周期。
- 参考 Claude Code 2.1.88 的提问与复核交互，以及本地 Codex `6f8bf683c` 的稳定问题 ID、控制事件与响应分离；参考源码只读，具体取舍和路径见 design。
- 本切片不包含 Plan Mode、非阻塞问题、自动倒计时代答、敏感输入收集、富媒体预览、MCP elicitation 或 Subagent 转发。
- 实现验收后同步 README、总体/Tool 架构、Roadmap 与 pitfall；本次提案不把计划能力写成已经实现。2026-11-05 当前未改代码基线 `make verify` 全通过，不能替代本功能实现验收。
