## Why

EasyCode 已交付双 Provider 只读 Plan Mode，但精确实施某一版方案仍需退出规划、从转录找回原文，再手动提交实施指令。P5 下一步可以复用原生正文投影和普通输入生命周期，让用户直接审阅并交接该版本，即使首个实施请求前发生自动压缩。

## What Changes

- 成功完成的实时 Plan 回合可从最终原生答复中提供一份明确标记的完整 Markdown 方案。进度说明、未完成或被截断的答复沿用现有转录展示。用户可审阅原文、继续规划，或明确选择在当前会话中开始实施。
- 采用 Claude Code 的先审阅后决策交互和 Codex 的显式方案标记及独立实施回合。界面默认继续规划，普通退出 Plan 保持原有语义。
- 会话 owner 在同一次接纳中切换到 Default 并接纳一条携带确切方案原文的新输入。该输入沿普通采样、压缩、提交和恢复路径处理。确认方案只授权开启实施回合，工具、Hook 和沙箱授权各自沿用现有规则。
- 交接限定在当前交互会话中。新线程实施、清空上下文、计划文件与外部编辑器、切换审批策略以及 headless 交接属于独立能力。

## Capabilities

### New Capabilities

### Modified Capabilities

- `runtime/plan-mode` 增加从已提交最终样本识别方案、按当前交互实例确认、原子接纳 Default 实施及恢复边界。
- `tui/plan-mode` 增加审阅方案、继续规划和明确启动同会话实施的本地交互。

## Impact

- 影响 `internal/domain` 的 Plan 指令、`internal/protocol` 的内部交互控制、`internal/runtime` 的完成与接纳边界，以及 `internal/tui` 的转录阅读和焦点路由。
- Anthropic Messages 和 OpenAI Responses 各自核对原生完结证据，共用方案识别与实施接纳。Plan 的固定指令会进入既有 Provider profile 内容身份，实施正文按现有真实用户输入编码，Session、原生历史与外部 headless 使用当前契约。
- 验收复用双 Provider 本地夹具、现有 Plan Mode 终端脚本与 `make verify`，核对压缩前后方案字节、正常工具审批、恢复等价及缓存身份。Apply 后更新 README、能力清单和相关架构文档。
