## Why

EasyCode 已经完成 OpenAI Responses 文本对话，但配置中允许选择的 Anthropic family 仍停留在占位实现，导致 P1 双 Provider Kernel 无法形成用户可用闭环。现在需要基于本地 Claude Code 2.1.88 和 Codex 指定快照完成一条范围受控的 Anthropic Messages 文本纵向切片，在扩展 Tool、Session 或完整 TUI 前验证第二套原生 wire 能正确复用现有 transport、RuntimeEvent 和 TUI 边界。

## What Changes

- 实现 Anthropic Messages 文本请求编译，使用 `x-api-key`、固定 `anthropic-version`、显式 `max_tokens` 和 `stream=true`，并将 `messages` endpoint 追加到现有 `base_url` API 前缀。
- 实现独立的 Anthropic content-block reducer，按 `message_start/delta/stop` 与 `content_block_start/delta/stop` 状态归并 text、thinking、signature 和 redacted thinking。
- 建立会话级 Anthropic 原生消息历史和 turn staging；仅在合法 `message_stop` 后提交用户消息与 assistant blocks，失败、取消、timeout 或提前 EOF 不污染后续请求。
- 将 assistant 文本 delta 投影为既有 typed RuntimeEvent，同时在 Anthropic 包内保留 stop reason、原始 usage 与未知扩展，不让 wire 类型泄漏到 Runtime 或 TUI。
- 扩展应用装配和基础 TUI 契约，使 JSON/环境配置选择 `anthropic` 或 `openai` 时都能进入同一文本 Chat，并继续复用各自隔离的原生历史。
- 增加 Anthropic request golden、稳定 fingerprint、事件状态机、thinking/signature/redacted round-trip、双轮 API prefix、取消/timeout/EOF、capability 和 secret 防泄漏回归测试。
- 明确延期 Anthropic tools、主动 thinking 配置、完整 CachePlanner/UsageParser/HistoryProjector、自动重试、Session/headless 和高级 TUI；本变更不实现 Chat Completions 或 provider wire 自动降级。

## Capabilities

### New Capabilities

- `provider/anthropic-messages-text`: Anthropic Messages 文本请求、content-block 流式归并、原生多轮历史、opaque thinking 数据保留和显式完成语义。

### Modified Capabilities

- `tui/basic-chat`: 将仅支持 OpenAI Responses 的启动与内存多轮契约扩展为支持已配置的 Anthropic Messages 或 OpenAI Responses conversation。

## Impact

- 主要影响 `internal/provider/anthropic`、`internal/app` 及其测试，并复用 `internal/provider/transport`、`internal/provider`、`internal/runtime`、`internal/protocol` 和 `internal/tui` 的现有契约。
- Anthropic 原生消息与 content block 保持在 Provider 子包内；依赖方向继续保持 `domain/protocol <- provider <- runtime <- app/tui/cmd`，不会引入统一扁平 Message 或让 TUI 解析 provider wire。
- 不新增第三方依赖，不扩展 JSON 配置 schema，也不修改 RuntimeEvent wire；`max_tokens` 使用 Provider 内部具名默认值，为后续能力配置保留入口。
- 实现和测试以指定的 Claude Code 2.1.88 sourcemap 工程与 Codex 提交 `7498521d288b9b3b96ffba4eedf089d8d6e06a84` 为只读行为参考，不将其源码或依赖引入构建。
