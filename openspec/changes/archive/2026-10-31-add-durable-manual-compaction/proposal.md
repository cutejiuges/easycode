## Why

EasyCode 已具备上下文估算、输入准入与工具输出预算，但长会话接近窗口上限后只能拒绝输入，无法在保留工作方向的前提下继续。现在需要以 Claude Code 的手动压缩体验补齐这一闭环，同时建立双 Provider 可验证、可恢复的上下文窗口切换。

## What Changes

- 提供 TUI 本地 `/context` 与 `/compact [保留重点]`；压缩过程可取消，完成后显示简短边界，Ctrl+O 查看摘要与原历史，手动压缩本身不触发普通续写。
- 将完整原生记录与有效请求窗口分离；完整 transcript、工具详情和 opaque reasoning 保留，首次压缩总结整个有效窗口，后续压缩总结上次摘要及新增内容。
- 双 Provider 各自编译有输出预算、禁止工具执行的独立摘要请求；验证候选后成功 Sync checkpoint，再切换内存窗口。摘要不能从 SemanticHistoryView 反向构造。
- 引入独立 CompactionID、操作状态与 Session facts，统一取消、队列、shutdown、usage 与 interrupted-tail 恢复；恢复不自动联网或重执行工具。
- stream-json v1 增加显式 context/compact 控制命令与输出，保持原命令及一次性 headless 输出语义；不扫描普通文本执行 slash 命令。
- 增加显式摘要输出预算配置；上下文展示保留 estimated/unknown，不将 usage 当作窗口占用。
- **BREAKING**：Session 当前 canary 从 6 替换为 7，Provider native canary 从 2 替换为 3；当前 fixture 同步替换，旧开发 journal 失败关闭，不实现兼容 reader 或原地迁移。

范围不含自动/后台/分块/局部压缩、远程专用 compaction API、跨 Provider fork、精确 tokenizer、价格估算、自动重读文件、Hook 执行、Skills/MCP/Subagent、完整 slash 平台和 Provider 缓存策略。

恢复前置修订：将 Loader 读取与尾部修复分开。完整 replay、Provider 兼容和原生/checkpoint 校验通过后，才在同一 lease 下执行截断与 Sync；Catalog 扫描只读。组合损坏或校验失败时 journal bytes 不变。

## Capabilities

### New Capabilities
- `context/compaction`：手动全窗口压缩的预算、摘要质量、原生窗口、操作与 checkpoint 契约。
- `tui/context-commands`：Claude 风格上下文展示、本地命令、压缩状态、取消、历史和输入交互。

### Modified Capabilities
- `context/planning`：以有效窗口只读快照计量，提供不修改状态的查询摘要。
- `provider/native-history-persistence`：Provider-owned checkpoint、有效窗口 revision 与事务恢复，当前 canary 3。
- `provider/history-projection`：完整历史与有效窗口投影职责分离，摘要不伪造用户 turn。
- `provider/usage-accounting`：独立计量压缩操作，保留字段状态且不混入普通 turn。
- `runtime/input-control`：独立压缩 admission、定向取消、队列与唯一 owner 清理。
- `session/jsonl-store`：当前 canary 7、压缩 records、与普通 turn 互斥的 replay grammar。
- `session/catalog`：协调只读扫描完整 committed 前缀，尾部修复留给完整验证后的 resume。
- `session/resume`：checkpoint 恢复和未完成压缩本地收口。
- `headless/stream-control`：显式上下文查询、压缩和取消 wire，单 writer 排序。
- `config/json-file`：可选 `compaction_output_tokens` 及与窗口配置的校验。

## Impact

影响 `internal/domain`、`protocol`、`context`、双 Provider、`session`、`runtime`、`app`、`config`、`headless`、`tui` 及其 tests/current fixtures；复用 Resty/Sonic、prepared lifecycle、single writer/lease、输入队列与 transcript query，不新增第三方依赖，不修改参考工程。

实施阶段同步 README、总体架构第 9/10/13 节、Roadmap、pitfall，并新增解释窗口/checkpoint 边界的 ADR。修正第 9 节仍把 tool schemas 写作未来来源、current_input 写作正文的过时说明。产品交互优先参考 Claude，恢复与身份设计吸收 Codex 优点，严格遵守本仓库 durable-before-memory 约束。
