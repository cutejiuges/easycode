## Why

EasyCode 已能读取项目、向用户澄清需求并展示任务进度，但用户要求先规划时，模型仍会收到修改文件和执行命令的工具。P5 的交互式 Plan Mode 可以复用这些已交付能力，让用户在同一会话里先调查和修订方案，再主动开始实施。它提供日常交互收益，也避开了会话分支、MCP 和 Subagent 所需的新生命周期。

## What Changes

- TUI 提供 `/plan`、`/plan off`、`Shift+Tab` 和持续可见的只读模式提示。模式由 AgentLoop 接纳，直到当前回合和等待输入收口才允许切换。
- 双 Provider 使用相同的工作区只读工具白名单。模型可读取、搜索、查看已授权输出和技能、结构化提问及记录任务进度。修改和执行命令的调用在本次采样准入时失败关闭。
- 双 Provider 按实际模式编译短规划指令，复用原生会话和 Markdown 展示。计划可在后续规划回合修订。用户显式回到普通模式，再单独发送实施指令，工具继续适用现有权限政策。
- 当前切片限定为 TUI 用户发起的进出模式。参考工程中的模型发起 Enter/Exit、专用计划文件、一键实施、清上下文和新会话实施属于独立能力。

## Capabilities

### New Capabilities

- `runtime/plan-mode` 定义交互模式 owner、互斥接纳、跨采样捕获及关闭与恢复。
- `tui/plan-mode` 定义本地命令、键位、持续状态、Markdown 计划及独立实施提交。

### Modified Capabilities

- `tui/input-completion` 纳入 `/plan` 并保留 `$plan` 的同名技能入口。
- `tool/invocation-loop` 将完整注册目录与按模式冻结的已声明目录区分，定义 Plan Mode 工具准入。
- `context/planning` 估算当前规划指令及工具定义，并按实际来源报告缓存变化。
- `provider/anthropic-messages-text` 在 Plan Mode 普通采样编译系统规划指令，缓存副本保留同一指令。
- `provider/openai-responses-text` 在 Plan Mode 普通采样编译顶层规划指令，HTTP/WS 使用同一冻结输入。

## Impact

涉及 domain/protocol 内部类型、Runtime/ChatSession 接纳、工具目录、上下文估算及缓存证据、Anthropic/OpenAI 请求编译、TUI 与交付文档。沿用现有工具输入和结果、权限审批、原生历史及 Session record 结构。计划正文作为普通助手答复随原生历史恢复，进入新进程或切换会话后从普通模式开始。一次性和 stream-json headless 入口继续使用普通模式。
