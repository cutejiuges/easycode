## Why

EasyCode 当前已经具备 Provider、Runtime、配置和 Bubble Tea 的分层骨架，但还不能发起真实模型请求或形成可连续对话的用户闭环。现在需要先完成一个范围受控的 OpenAI Responses 文本纵向切片，用真实流式交互验证 base URL、SSE、原生历史、RuntimeEvent 和 TUI 边界，再继续扩展 Anthropic、工具和持久化能力。

## What Changes

- 支持将无路径前缀和带路径前缀的自定义 `base_url` 作为 API 前缀，并在追加 `responses` endpoint 时保留既有路径。
- 建立基于 Resty raw response body 的有界 SSE 流读取契约，覆盖随机 chunk、LF/CRLF、跨 UTF-8、取消、idle timeout 和提前断线。
- 实现 OpenAI Responses 无工具文本请求、流式事件归并和会话级原生历史；仅在显式 `response.completed` 后提交本轮历史。
- **BREAKING（内部接口）**：调整 Provider stream 与 Runtime turn 契约，使用显式完成、失败和取消终态，channel 关闭不再自动代表成功。
- 为 assistant 文本增量和 turn 失败提供可由 TUI 安全消费的强类型 RuntimeEvent payload。
- 将环境配置、OpenAI Provider、Runtime 和 Bubble Tea 装配成最小内存多轮 TUI REPL，支持提交、流式显示、取消、失败恢复和继续下一轮。
- 将 Provider capability 声明收敛到本变更实际实现并验证的能力。
- 明确延期 Anthropic Messages、工具调用、reasoning 展示、session 持久化、`previous_response_id`、自动重试、多行编辑和 slash commands。

## Capabilities

### New Capabilities

- `provider/http-stream-transport`: 自定义 base URL 前缀解析、稳定请求发送和有界可取消 SSE frame 流读取契约。
- `provider/openai-responses-text`: OpenAI Responses 文本请求、事件归并、显式完成判断和原生多轮历史行为。
- `runtime/chat-turn`: 单 turn 生命周期、显式 provider 终态、取消和共享语义事件投影。
- `tui/basic-chat`: 基于 RuntimeEvent 的最小多轮 TUI Chat/REPL 交互。

### Modified Capabilities

无。当前工程尚无已发布 OpenSpec capability。

## Impact

- 主要影响 `internal/provider/transport`、`internal/provider/openai`、`internal/provider`、`internal/runtime`、`internal/protocol`、`internal/app`、`internal/tui` 和 `cmd/easycode`。
- Provider 与 Runtime 的内部接口会调整；`domain/protocol <- provider <- runtime <- app/tui/cmd` 的依赖方向保持不变。
- 不新增第三方依赖，HTTP 继续使用 Resty v3，JSON 继续使用 Sonic，TUI 继续使用 Bubble Tea。
- 新增 transport fixture、Responses request/reducer golden、双轮 httptest 集成测试和固定终端尺寸 TUI snapshot。
- 更新架构文档中的 Codex 参考提交，并记录 Resty SSESource 不作为最终 frame parser 的结论。
