## 1. Anthropic 原生模型与确定性请求

- [x] 1.1 完善 `anthropic.Config` 和 Provider 构造校验，加入具名的 4096 默认输出上限、显式正数覆盖、model/API key/base URL 安全校验，并用表驱动单测验证默认值、非法值和错误不泄漏 secret。
- [x] 1.2 将 `NativeItem` 扩展为可深拷贝的 text、thinking/signature、redacted thinking 强类型 block，增加 provider-native message、message metadata 和可区分 unknown 的原始 usage 类型；用 marshal/unmarshal 与 clone 单测验证 opaque data、签名、顺序和缺失 usage 不丢失。
- [x] 1.3 实现无副作用的 Messages request compiler，按 `user/assistant` message 边界编译完整已提交历史和当前用户 text block，并验证 tools、system、thinking、temperature、beta、`cache_control` 等延期字段不会出现在正文中。
- [x] 1.4 添加首轮 Messages request golden 和 request fingerprint regression，验证 `model/messages/max_tokens/stream` 的 canonical bytes 稳定，且 API key、时间戳、随机 ID 和宿主状态不参与正文或 fingerprint。

## 2. Content-block StreamReducer

- [x] 2.1 实现每 turn 独立的强类型 event envelope 与索引状态机，处理 `message_start`、text block start/delta/stop 和 `message_stop`；用 reducer 单测验证 typed text delta、完成 native item、block 顺序和 start/delta 文本不重复。
- [x] 2.2 增加 thinking、`signature_delta` 与 `redacted_thinking` 归并，确保 signature 不投影为可见文本并在完成 block 中原样保存；用最小本地 fixture 验证 thinking/signature/redacted round-trip。
- [x] 2.3 实现 `message_delta` 的 stop reason 与累计 usage 合并，保持缺失字段为 unknown，并用测试验证 message_start 的 input/cache 值不会被无信息零值覆盖且 output 值采用最终服务端结果。
- [x] 2.4 实现 error、ping 和未知 event 策略，以及重复 index、未 start 的 delta/stop、block/delta 类型错配、未闭合 block、重复/提前 `message_stop` 和损坏 JSON 的失败路径；用表驱动测试验证稳定英文错误码、无 panic 且失败不产生伪造完成事件。

## 3. Conversation、原生历史与 Transport 接入

- [x] 3.1 将 Anthropic Provider 重构为不可变配置/transport owner，并让每个 Conversation 独占 active-turn guard 和并发安全 native history；用两个 conversation 和并发提交测试验证历史不共享且同一会话最多一个活动 turn。
- [x] 3.2 将 Messages compiler 接入 `StreamSSE` 的相对 `messages` endpoint，发送 `x-api-key`、`anthropic-version: 2023-06-01`、SSE accept 与 JSON content type；用 httptest 验证 host-only/路径 prefix、method、headers、稳定正文和 HTTP 错误脱敏。
- [x] 3.3 实现 stream consumer 的有序 semantic/native/terminal 输出与 `message_stop` 后事务提交，确保 user message、assistant blocks 和 metadata 一次性进入历史；用成功、terminal 恰好一次和 terminal 后无事件测试验证生命周期。
- [x] 3.4 增加无外网双轮集成测试，分别覆盖 host-only 与带路径的 `base_url`，断言第二轮 request 按顺序回放首轮 user、thinking/signature/redacted/text assistant blocks 和新 user message。
- [x] 3.5 覆盖 server error、取消、idle timeout、oversized event、非法 reducer 状态和 `message_stop` 前 EOF，验证 response body/goroutine/channel 被清理、请求不自动重放且全部 turn staging 被丢弃。
- [x] 3.6 收敛 Anthropic Capabilities 为 `Streaming` 与 `ThinkingSignature`，保持 tools、cache、incremental 和未实现 reasoning 能力关闭；用 capability matrix 测试防止声明漂移。

## 4. App 与基础 TUI 双 Provider 装配

- [x] 4.1 将 `chatResources` 的具体 OpenAI 所有权替换为 app 包内最小 `Close() error` 资源边界，并按配置 family 装配 OpenAI Responses 或 Anthropic Messages Conversation；用构造与 shutdown 测试验证两者都能无网络创建、关闭且未知 family 不触发请求。
- [x] 4.2 将现有 Anthropic unsupported 回归改为 Anthropic 可用测试，并覆盖显式 JSON、默认 JSON 与纯环境变量配置启动到 idle；验证 API key、配置路径和 header 不进入错误或 TUI 输出。
- [x] 4.3 运行现有 TUI model/snapshot 与 Runtime/ChatSession 测试，确认无需 Provider 分支即可显示 Anthropic text delta、完成、取消和失败，并通过依赖检查确认 `internal/tui` 未导入任何 Anthropic/OpenAI wire 类型。
- [x] 4.4 回归 OpenAI 首轮/双轮、取消、失败恢复和 app 装配测试，确认资源抽象与 family switch 未改变 Responses request、原生历史或现有 TUI 行为。

## 5. 文档、回归与质量门

- [x] 5.1 更新 README 的当前能力、Provider 配置与限制，说明交互模式支持 Anthropic Messages 和 OpenAI Responses，同时继续明确 tools、Session、headless、cache/reasoning UI 尚未实现；人工核对示例不包含真实 secret。
- [x] 5.2 更新 Roadmap 的 P1 纵向切片进展和未完成项；若实施发现新的可复用踩坑则记录到 `pitfall-log.md` 并关联本 change，不为没有发生的问题编造记录。
- [x] 5.3 运行 Anthropic request golden/fingerprint、reducer、conversation 集成、app/TUI 和全部 OpenAI 回归测试，确认 fixture 不访问外网且不包含 API key、Authorization、Cookie、完整敏感 header 或请求错误正文。
- [x] 5.4 执行 `make verify`，确认 gofmt、go vet、固定版本 Staticcheck、全量测试和 race test 全部通过，并人工检查依赖方向、goroutine 清理、无重复副作用、无 dead code、无无用依赖及 OpenSpec/文档一致性。
