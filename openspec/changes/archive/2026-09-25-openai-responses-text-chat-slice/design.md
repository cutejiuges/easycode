## Context

当前代码已经建立 `config -> provider -> runtime -> app/tui` 的依赖方向，但纵向链路仍是占位实现：OpenAI Provider 返回 `not_implemented`，transport 只验证了 Resty `SSESource` 的单事件 happy path，Runtime 将 stream channel 关闭直接视为成功，TUI 只投影事件数量，app 也未装配真实配置和 Provider。

本设计受以下约束影响：

- Provider-native history 是续写事实源，不能由 RuntimeEvent 或 TUI transcript 反向构造。
- Resty v3 继续作为 HTTP client，但 RC 版本的 `SSESource` 默认 32 KiB event buffer、只对初始建连重试，且其 frame 边界行为不满足当前要求，因此不能直接作为最终 stream contract。
- 所有网络和流生命周期必须支持 context 取消、idle timeout、确定的 goroutine owner 和清理等待路径。
- 本变更是跨 P1/P5 的薄纵向切片，不代表完整 Provider Kernel、Session 或 Claude 风格 TUI 已完成。
- 最新 Codex 参考仍在 HTTP Responses 请求中发送完整原生 `input`，只有 WebSocket continuation 使用 `previous_response_id`，并把 completed 前 EOF 视为错误。

## Goals / Non-Goals

**Goals:**

- 固定可复用的 API prefix 与 SSE transport 契约，再在其上实现 OpenAI 文本流。
- 让一份会话级 OpenAI 原生历史驱动连续两轮请求，并保证失败 turn 不污染已提交历史。
- 使用显式 provider terminal 修正 Runtime 的成功判断和取消语义。
- 让 Bubble Tea 只通过共享语义事件和小型会话接口完成真实流式 Chat。
- 将测试重点放在协议边界、随机 chunk、终态、取消和第二轮 request input，而不是视觉功能数量。

**Non-Goals:**

- 不实现 Anthropic Messages、Chat Completions 或 wire 自动降级。
- 不实现 tools、reasoning UI、usage/cache UI、prompt cache key 或 `previous_response_id`。
- 不实现自动重试、断线续传、session JSONL、resume、SQLite 或跨进程历史。
- 不实现多行编辑、paste 保护、命令补全、slash commands、Markdown 流式排版或 transcript 滚动优化。
- 不把 TUI transcript、RuntimeEvent 或 SemanticHistoryView 作为 Provider 请求事实源。

## Decisions

### 1. 将 Provider 配置与会话级 Conversation 状态分开

`openai.Provider` 负责不可变配置、capabilities、共享 Resty transport 和资源关闭；它创建 session-scoped `openai.Conversation`。`Conversation` 持有 `NativeHistory` 和当前 turn staging，并实现 Runtime 所需的小型 stream 接口。

共享包新增或调整 `provider.Conversation` 契约，Runtime 持有 conversation 而不是直接持有可跨会话复用的 Provider。首版 app 只创建一个 conversation，但该边界避免未来多个 Session 意外共享 history。

备选方案是继续让 `openai.Provider` 同时持有配置、transport 和可变历史。该方案实现更少，但生命周期语义含混，未来 app-server 或多 Session 必须再次拆接口，因此不采用。

### 2. 单 channel 保持事件顺序，显式 terminal 决定结果

Provider stream 使用一个有序 channel 发送带 kind 的 `provider.StreamEvent`：

- semantic：携带 RuntimeEvent 投影；
- native-item：携带完成的 provider-native item；
- completed：成功 terminal；
- failed：失败 terminal 和 error；
- cancelled：取消 terminal，并保留 `context.Canceled` 可识别性。

每个 stream 必须恰好发送一个 terminal，然后由 stream owner 关闭 channel。Runtime 只有读到 completed 才产生 `turn_completed`；terminal 前 channel close 统一转为 `stream_protocol_error`。单 channel 保证所有文本和 native item 都位于 terminal 之前，不引入多 channel 的跨 channel 排序问题。

取消仍由 context 发起。Provider 收到取消后关闭 response body、等待 transport reader 退出、清空 staging，再发送 cancelled terminal。Runtime 在取消后继续完成这条清理握手，不把 `ctx.Done()` 直接当成无需等待的返回路径。

备选方案是以 channel close 表示完成，但它无法区分正常完成、网络 EOF 和 goroutine 异常退出；当前实现已经暴露该问题，因此不采用。

### 3. Base URL 是经过校验的 API prefix

transport 在构造时解析并保存不可变的 base URL：只允许 `http`/`https`、有效 host，拒绝 userinfo、query 和 fragment。endpoint 必须是受控相对路径；resolver 去除边界重复斜杠后追加 endpoint，但保留 base URL 原有路径。

例如：

```text
https://gateway.example            + responses -> https://gateway.example/responses
https://gateway.example/openai/v1/ + responses -> https://gateway.example/openai/v1/responses
```

系统不自动补 `/v1`。官方 OpenAI 用户需要配置 `https://api.openai.com/v1`，而自定义网关可以选择自己的 prefix。query 参数未来通过独立强类型配置加入，避免把签名参数或敏感信息混入 base URL。

不继续使用 `url.ResolveReference` 处理 endpoint，因为以 `/` 开头的引用会丢弃已有 `/v1` 或代理路径前缀。

### 4. Resty 负责 HTTP，独立 parser 负责 SSE frame

transport 使用 Resty 构造并发送 POST，请求 body 由 `codec.MarshalStable` 预先编码，响应设置为 raw body 消费。streaming 请求禁用自动 retry；一旦请求可能被服务端接受，transport 不自行重放。

独立 SSE parser 只负责 byte stream 到 `SSEEvent`：

- 同时识别 LF、CRLF 和 EOF；
- 支持任意 read chunk 和跨 UTF-8 chunk；
- 合并多行 `data`，保留 `event`/`id`，忽略 comment payload；
- comment 和有效 frame 都刷新 idle watchdog；
- 默认单 event 上限为 4 MiB，超限返回 protocol error；
- 默认 idle timeout 为 5 分钟，测试和 Provider 配置可以覆盖。

transport supervisor 拥有 response body、reader goroutine 和输出 channel。取消、timeout 或上层停止消费时，supervisor 先关闭 body 解除阻塞，再等待 reader 退出，最后关闭输出 channel。channel 只由创建它的 supervisor 关闭。

备选方案是继续使用 Resty `SSESource` 并围绕其回调补丁，但其 event 上限、CRLF/frame 行为和连接结束语义会泄漏到 Provider reducer，难以形成项目要求的稳定 contract，因此选择 raw body parser。

### 5. Responses request 使用完整 native input，不启用增量 continuation

第一轮 request body 至少包含：

```json
{
  "model": "<configured-model>",
  "input": ["<typed native items>"],
  "stream": true,
  "store": false,
  "include": ["reasoning.encrypted_content"]
}
```

用户输入编码为 OpenAI user message/input-text item。后续 request 的 `input` 是已提交 `NativeHistory` 加当前用户 item。当前切片不发送 tools、prompt cache key 或 `previous_response_id`。

Responses item 使用 OpenAI 包内的 discriminated envelope：已知 message/reasoning 字段强类型化，未知扩展只能保留在受控 `json.RawMessage` 中，不能向 Runtime 或 TUI 传播 `map[string]any`。`response.output_text.delta` 只用于实时投影；`response.output_item.done` 才进入 turn staging。

`response.completed` 到达时，Conversation 原子提交“当前用户 item + staged output items”。failed、incomplete、cancel、timeout、解析失败或 completed 前 EOF 都丢弃 staging。已经显示的 partial text 可以留在 TUI transcript，但不能成为下次请求历史。

### 6. Reducer 和 HTTP/SSE parser 保持两层状态机

SSE parser 不解析 OpenAI JSON。OpenAI reducer 读取 `type` 并处理本切片事件：

- `response.created`：保存 response id 作为本轮 metadata；
- `response.output_text.delta`：构造 typed assistant delta；
- `response.output_item.done`：解析并暂存 native item；
- `response.completed`：校验完成数据并提交 history；
- `response.failed` / `response.incomplete`：构造安全 provider error；
- 未知且格式正确的事件：诊断后忽略；
- 已知事件缺少必需字段或 JSON 损坏：终止为 stream protocol error。

诊断日志只记录 event type、安全错误分类、request id 和长度等摘要，默认不记录 data/body。Provider capability 根据真实完成的 reducer、history 和 UI 行为返回，未实现字段保持 false。

### 7. RuntimeEvent 增加 typed payload helper

`protocol.Event` 继续保持版本化 envelope 和 `json.RawMessage` payload，但每个已支持语义使用强类型 payload struct 和构造/解析 helper。首个切片至少增加：

- `AssistantTextDeltaPayload{Text string}`；
- `TurnFailedPayload{Code string, Message string, Cancelled bool}`。

Provider reducer 只能通过 helper 生成语义事件，TUI 只能通过对应 decoder 消费。这样既不让 protocol 依赖 OpenAI wire，也不让各宿主重复猜测 payload shape。

Runtime 使用短临界区状态保护确保一个 conversation 同时只有一个 active turn；锁只保护 active flag/cancel handle，网络读取和 emitter 回调都在锁外执行。terminal 发布集中在单一 finalizer 中，避免取消与 completed 竞争时产生两个最终事件。

### 8. TUI 依赖会话 facade，不依赖 Provider

在 Runtime/App 边界提供小型 `ChatSession` facade：提交输入时创建 turn context，异步运行 `RunTurn`，将 RuntimeEvent 顺序送入会话 event stream；interrupt 只调用当前 turn 的 cancel function。长期结构体不保存 `context.Context`。

Bubble Tea Model 只持有 `ChatSession` 接口、纯 UI 状态和当前 turn 状态。耗时操作由 `tea.Cmd` 完成：submit command 启动 turn，wait command 每次取一个 RuntimeEvent 并重新订阅。Model 不调用 transport、Provider 或 native history。

首版 composer 使用内部 rune buffer 实现单行输入，不新增 Bubbles textarea 依赖。状态包括 idle/streaming、draft、transcript、当前 assistant buffer 和安全错误摘要。Enter 仅在 idle 且 trim 后非空时提交；streaming 时 Enter 不排队。Esc/Ctrl+C 在 streaming 时 interrupt，idle 且空 draft 时 Ctrl+C 退出，`q` 始终作为普通字符。

app 在启动 TUI 前加载和校验环境配置，只装配 OpenAI Responses；Anthropic 配置返回明确 unsupported 错误。退出时 app 负责取消活动 turn、等待会话 shutdown 并关闭 Provider transport。

### 9. 测试按边界分层

- Transport 单测：base URL table、stable JSON、LF/CRLF、多行 data、comment、随机 chunk、跨 UTF-8、4 MiB limit、cancel、idle timeout、EOF 和 goroutine 清理。
- OpenAI golden：首轮/次轮 request body、已知和未知 event fixture、failed/incomplete、encrypted reasoning round-trip、completed 前 EOF。
- Runtime 单测：显式 completed、failed、channel early close、cancel/completed race、并发 submit 拒绝、事件顺序。
- 集成测试：httptest server 完成两轮 Responses 流，并断言第二轮 input 包含第一轮原生 output item；整个测试不访问外网且 fixture 不包含真实 key。
- TUI snapshot：固定终端尺寸下覆盖 idle、draft、streaming、completed、cancelled 和 failed。
- 交付前执行 `make verify`，不得弱化现有 gofmt、vet、Staticcheck、test 和 race gate。

## Risks / Trade-offs

- [自研 SSE parser 引入协议边界风险] → 先完成独立 parser fixture 和随机 chunk 回归，再接入 OpenAI reducer；parser 保持无 Provider JSON 语义。
- [部分第三方 Responses 网关不发送标准 completed] → 按规范将 completed 前 EOF 视为错误，并在错误中明确报告 stream protocol 问题，不为兼容而把 EOF 静默降级为成功。
- [4 MiB event 上限可能不足或过宽] → 上限集中为可配置 transport option，并通过 oversized fixture 固定默认行为；后续依据真实 fixture 单独调整。
- [失败 turn 的用户输入不会进入 native history] → TUI 保留可见 transcript，但模型历史只提交成功 turn；retry/编辑重发在后续变更中设计，避免本次隐式重复请求。
- [本次 TUI 跨越 Roadmap P5 的一部分] → 只实现验证 RuntimeEvent 边界所需的单行交互，不引入 Markdown、overlay、diff 或 session picker。
- [Conversation/Provider 生命周期增加装配代码] → 由 app 统一创建和关闭，换取未来多 Session 不共享可变 history 的清晰边界。

## Migration Plan

1. 先替换 transport URL/SSE contract 并让所有 transport fixture 通过。
2. 引入 Provider/Conversation 与显式 stream terminal，更新 fake provider 和 Runtime 测试。
3. 实现 OpenAI request、reducer、native staging/history 和双轮集成测试。
4. 增加 typed RuntimeEvent payload 与 ChatSession facade，再接入 TUI 和 app 配置装配。
5. 更新架构参考提交、Roadmap/踩坑记录，并执行 `make verify`。

当前没有持久化 Session 或外部稳定 API，因此不需要数据迁移。若实施中必须回滚，可整体撤销该 change；旧 scaffold 不会读取新格式数据。不得只回滚 terminal 判断而保留新的 Provider stream contract。
