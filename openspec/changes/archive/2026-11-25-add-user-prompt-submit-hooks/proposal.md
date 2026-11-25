## Why

EasyCode 已覆盖工具调用前后、回合完成、压缩前后及会话激活，真实用户输入在交给模型前仍缺少可配置的本地检查。`UserPromptSubmit` 能让团队在首条提示或同轮追加中拦截不符合工作区约定的提交，用户可以取回并编辑原文，现有同步 Hook 和输入 owner 足以承载这一切片。

## What Changes

- 增加同步 `UserPromptSubmit` 命令声明。每条已领取的对话输入在进入模型上下文前独立检查，覆盖首轮、工具后的追加、Stop 续轮中的真实用户纠正，以及用户确认的 Plan 实施正文。
- 脚本通过有界 JSON stdin 读取本次文本，经现有逐声明信任和只读禁网沙箱运行。支持退出码 `2` 和结构化 `decision: block`，并按英文固定原因报告拒绝或运行失败。
- 在同一个 FIFO claim 内保留通过项的顺序，拒绝项按原身份返回，可取回编辑。初始输入全被拒绝时结束该回合。进行中的回合可继续处理已保存的工具输出或 Stop 反馈。
- 保存最小检查、执行及结算事实，支持中断后本地收口。通过的输入仍由现有双 Provider 原生提交路径处理。
- `/hooks` 增加事件浏览与明文披露说明。交互端显示声明身份和固定原因。headless 的严格提交和自动追加均报告需要信任，复用各自的 v1 归属与终态。模型上下文注入留给后续独立能力。

## Capabilities

### New Capabilities

- `extension/user-prompt-submit-hooks` 定义提交事件、stdin、退出控制、执行时机和恢复。

### Modified Capabilities

- `extension/command-hooks` 扩充冻结配置允许的事件与信任目标。
- `runtime/chat-turn` 补充门禁在初轮和续轮中的持久化顺序。
- `runtime/conversational-input` 补充一次 claim 中部分输入被拦截时的归属、状态和 FIFO 结算。
- `session/jsonl-store` 和 `session/resume` 补充检查事实、有效输入关联和本地恢复顺序。
- `tui/hooks` 补充提示词披露审查、检查状态和取回交互。
- `headless/stream-control` 补充独立对话提交遇到门禁时的错误和输出顺序。

## Impact

影响 `internal/domain`、`internal/extension/hook`、`internal/runtime`、`internal/session`、`internal/protocol`、`internal/app` 和 `internal/tui`。复用现有 Hook 进程 owner、信任、Session lease、Writer batch 和输入预检。Provider 编译与原生 wire 继续使用当前输入契约。验收覆盖双 Provider 请求与恢复等价、真实 Darwin 终端及无交互入口。
