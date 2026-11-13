## Why

EasyCode 已具备九项模型工具、原生历史恢复和三类同步工具 Hook，但模型返回无工具调用的成功 sample 后会立即宣布回合完成，仓库还不能检查最终答复并要求同回合补做工作。当前 HistoryProjector、命令信任、只读沙箱和共享进程 owner 已具备基础，适合补齐有界 Stop 完成检查，比同时引入后台进程、Plugins/MCP 或 Subagent 更容易形成独立可验收闭环。

## What Changes

- 增加 root 用户回合的同步 command `Stop`：成功、无 calls 的 sample 持久化后检查，通过才完成；结构化阻断可要求模型在同一 turn 继续，最多两次，不重置既有 16 sample / 64 tool call 上限。
- 复用 user/project 冻结来源、逐项默认拒绝的进程内信任、`/hooks` 审查、Darwin 工作区只读/禁网沙箱和唯一进程 cleanup owner。Stop 不使用 tool matcher，不伪造模型工具或 invocation。
- 默认 `feedback=none`；仅显式 `model` 且已信任的严格 JSON block reason 可以形成模型反馈。原始 stdout/stderr、exit 2 原文与未授权 reason 不保存、不回显、不自动发送；错误、取消、不确定或上限耗尽均不伪造检查通过。
- Stop 阻断结果先独立冻结；反馈与可接收的队首 auto 输入作为待交付候选，必要时只压缩旧 committed 窗口，再原子保存反馈及真实用户输入。恢复补齐已接收的本地反馈交付，不联网续写；不靠摘要提示词保证最新反馈不丢失。
- **BREAKING**：用封闭的 tool-call/turn-stop 目标表达当前 Hook 身份，直接替换受影响 current fixtures；扩展唯一当前 Session grammar 与 Provider-native `hook_feedback` 提交，不添加内部格式版本或兼容 reader。最终 sample 与回合成功分离，恢复仅按 durable facts 本地收口，不重新运行检查、不自动请求模型。
- 明确坏 Hook 来源的完成边界：严格来源诊断阻止宣告 Stop 检查成功，即便无法可靠判断损坏来源中是否配置 Stop；流式答复、只读工具和 `/hooks` 诊断仍可用，最终回合以安全配置错误失败。合法空来源仍保持现有无 Hook 路径。
- 提供 Claude 风格的检查中、阻断后继续与失败状态，以及 Ctrl+O 的安全检查详情；headless 保持无交互信任失败关闭和现有外部 JSONL v1 输出契约。

### Non-Goals

不实现 StopFailure/SubagentStop、异步/HTTP/prompt/agent Hook、`continue:false`、任意 stdout/stderr 反馈、Hook 自动批准、自动重跑旧进程、持久信任、热加载、Plugins/MCP、后台任务或跨平台沙箱扩张。只读检查不能被宣传为可直接运行所有需要写缓存或产物的测试命令；需要修改的修复/验证仍由模型发起现有工具并经过普通权限检查。

## Capabilities

### New Capabilities

- `extension/stop-hooks`：Stop 触发、严格输入/输出、有限续轮、执行与反馈预算、终态和隐私。

### Modified Capabilities

- `extension/command-hooks`：四事件配置、无 matcher 的 Stop 声明、封闭目标身份、共用信任与执行边界。
- `runtime/chat-turn`：最终 sample、Stop 门禁、单一 turn terminal、取消与聚合 usage 的独立线性化。
- `runtime/input-control`：Stop 阻断决定闭合后的自动输入安全点、FIFO 屏障及 claim 的唯一持久化结果。
- `tool/invocation-loop`：无 calls 完成门禁、Stop 追加安全点及 sample/call 原上限，不伪造工具 outputs。
- `context/auto-compaction`：已冻结但尚未交付的 Stop 反馈/用户输入候选、只摘要旧窗口、一次尝试及原字节交付。
- `provider/native-history-persistence`：独立 typed Hook 反馈准备、原生提交/恢复与缓存等价。
- `provider/history-projection`：精确 sample 的 assistant 正文与独立 Hook 来源投影，不回退到前一条非空答复。
- `provider/transcript-projection`：独立 Hook 反馈条目、真实 cursor 和 live/restored 展示等价。
- `session/jsonl-store`：Stop 计划/运行/决定、sample 与反馈批次规则、强类型当前 fixtures。
- `session/resume`：Stop 各中断窗口的离线补偿、零重跑、连续 lease 与重复恢复幂等。
- `tui/hooks`：回合级审查、披露解释、检查与续轮状态、只读历史详情和取消隔离。

## Impact

涉及 `internal/domain`、`internal/protocol`、`internal/extension/hook`、`internal/runtime`、`internal/session`、双 Provider、`internal/app` 和 `internal/tui`；共享 `internal/executor/process` 与安全来源打开骨架继续复用，不建设通用事件总线或万能 Hook 框架。Context/compaction/cache 的既有预算和原生事实约束必须覆盖新历史条目，不新增工具 schema、Provider capability 开关、第三方依赖或 headless 外部事件。

参考基线为 EasyCode `539d54e`、Claude Code sourcemap `a8a678c` 与 Codex `c0cc9fc`（与首轮 `6f8bf683c` 对照）的本地源码。设计已给出函数级证据、差异取舍、恢复矩阵和可执行验收映射；两参考工程均只读，不构建依赖或复制成段实现。后续明确 apply 已完成，48 项实施任务与主规格同步均已完成，充分验证记录见 [verification.md](verification.md)。
