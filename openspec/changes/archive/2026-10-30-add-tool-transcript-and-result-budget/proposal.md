## Why

现有七项工具已经具备执行、审批、原生续写与恢复链路，但主线主要展示文本和命令，Ctrl+O 仍是命令列表，用户无法一致地追踪读取、搜索、修改与模型实际收到的结果。单次预览上限也无法约束多工具、多 sample 的累计输出；需要在同一个变更中交付 Claude Code 风格的工具 transcript 和工具特定的结果预算，让紧凑展示、模型预览、保存内容与恢复事实保持一致。

## What Changes

- 为全部七项现有工具提供有序活动、明确状态和恢复后查询；连续成功 Read/Search 可折叠，修改、执行、错误、拒绝与不确定结果保持可辨认。
- **BREAKING**：Ctrl+O 从命令历史列表改为当前会话详细 transcript；命令输出分页成为对应活动的二级详情。保留 composer、审批焦点与定向中断语义。
- 增加 Provider 原生历史到可见文本/调用锚点的纯内存投影，统一 live 与 restored 顺序；不改变原生请求来源，不向 SemanticHistoryView 或缓存输入加入展示数据。
- 为七项工具实施单调用、sample 与 turn 的确定性结果预算：Read 保持定点读取且不自动转存，ReadArtifact 按实际页进度返回，Glob/Grep 可保存本次采集结果，Exec 复用双流保存，Patch/Write 保留紧凑摘要。
- 泛化私有 artifact 的真实文本结果来源、配额、授权与查询；区分界面折叠、模型省略、采集范围不完整和保存不完整。
- **BREAKING**：替换唯一当前进程内展示协议及必要 Session payload，更新当前 canary/固定 fixtures，旧开发 shape 失败关闭，不引入兼容 reader。
- 同一 change 必须完成 UI、预算、存储、双 Provider、恢复、故障注入、性能/预算场景验证与文档同步，不能只完成其中一半后归档。

## Capabilities

### New Capabilities

- `tui/tool-transcript`：七工具紧凑主线、分组、详细 transcript、二级输出详情和输入焦点规则。
- `provider/transcript-projection`：保持原生顺序且排除 opaque 数据的文本/调用锚点投影。
- `tool/result-budget`：工具特定模型输出预算、确定分配、冻结、耗尽及恢复规则。

### Modified Capabilities

- `tui/basic-chat`：替换命令专用 Ctrl+O 入口与主线文本布局，保留已有聊天和审批行为。
- `session/command-history`：将已有命令历史能力纳入七工具有序宿主查询，保留命令详情能力，移除重复命令专用 owner。
- `session/output-artifacts`：增加 Glob/Grep 文本结果保存、明确 coverage 和按来源预留的配额。
- `session/jsonl-store`：冻结预算与搜索保存元数据，替换当前 canary，保持唯一 writer 和记录词汇。
- `tool/invocation-loop`：预算接入有序执行和结果提交、展示提交边界及耗尽后的完整配对。
- `tool/read-file`：增加预算拒绝和观察失效，保留原有范围、长行和单次硬上限。
- `tool/read-artifact`：在分配预算内生成有进度的页，支持搜索结果引用。
- `tool/search-files`：在安全扫描边界内保存本次结果、按预算生成预览与精确省略说明。
- `tool/command-execution`：将命令预览预算与保存决策联动，保留执行、取消、双流及审批契约。

## Impact

涉及 `internal/domain`、`protocol`、双 Provider、`tool`/`builtin`、`runtime`、`session`/`artifact`、`app`、`tui` 及相关测试。复用现有 scheduler、prepared commit、writer/lease、artifact store、异步查询 owner、composer 与审批，不增加 SDK、服务或参考仓库依赖。参考 Claude Code 2.1.88 的紧凑工具消息与 Ctrl+O 语义，吸收 Codex `6f8bf683c` 的稳定活动身份、有界输出和多层展示设计。

不包含后台进程/PTY、自动 compaction、Markdown 全面改造、Skills/MCP/Subagent、权限扩大、自动过期或历史完整 diff 快照。Read/ReadArtifact 不自动生成新 artifact 是工具特定策略，并非延期交付；历史修改仅展示当前事实能够证明的摘要，不重读文件伪造历史 diff。
