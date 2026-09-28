## Context

参见 `proposal.md` 的动机。当前 OpenAI Responses 文本切片已经提供可复用的 Resty raw-body SSE transport、显式 Provider terminal、会话级 Runtime、typed assistant text event 和基础 TUI；`internal/provider/anthropic` 只有一个简单原生 block 类型和始终返回 `not_implemented` 的 Conversation。配置层已经接受 `anthropic`，但 `internal/app` 将资源所有权固定为 `openai.Provider` 并主动拒绝 Anthropic。

设计以两个指定本地参考工程为只读行为依据：Claude Code 2.1.88 的 `restored-src/src/services/api/claude.ts` 与 `utils/messages.ts` 用于核对 Messages 请求、block 状态和 thinking 回放；Codex 提交 `7498521d288b9b3b96ffba4eedf089d8d6e06a84` 的 `codex-api/src/sse/responses.rs` 与 `protocol/src/models.rs` 用于复核原生 item、显式完成和未知事件隔离边界。参考工程不被修改、不参与 EasyCode 构建，也不大段复制其实现。

本设计还受以下现有约束影响：Provider-native history 是续写事实源；TUI 只能消费 RuntimeEvent；transport 只负责 HTTP/SSE frame；鉴权 header 不进入 request fingerprint；所有 stream goroutine 必须有 owner 和清理路径；注释使用中文而对外错误使用英文。

## Goals / Non-Goals

**Goals:**

- 在不改变共享 RuntimeEvent wire 的前提下，让 Anthropic Messages 复用现有文本 Chat 生命周期。
- 用独立的强类型 RequestCompiler、StreamReducer 和 NativeHistory 保持 Anthropic message/content-block 语义。
- 固定 endpoint、headers、正文默认值、状态转换、终态和失败提交规则，使实现可由 golden 与 fixture 确定性验证。
- 无损保存并在同一 Anthropic conversation 中回放 thinking、signature 和 redacted thinking。
- 保持 OpenAI 现有行为不变，并让应用资源所有权不再绑定某个具体 Provider。

**Non-Goals:**

- 不实现工具声明、tool_use/input_json、tool_result 或模型继续采样。
- 不主动启用 thinking，不展示 thinking，也不增加 reasoning RuntimeEvent。
- 不实现 CachePlanner、cache marker、UsageParser、HistoryProjector 或 capability 探测；本切片只稳定 request fingerprint 并保留原始 usage。
- 不实现自动重试、provider 热切换、跨 Provider 历史转换、JSONL/resume 或 headless。
- 不扩展外部 JSON 配置字段，也不维护会随模型发布持续变化的模型能力表。

## Decisions

### 1. 直接实现 Messages wire，并继续复用现有 transport

Anthropic Conversation 使用 `transport.Client.StreamSSE` 向相对路径 `messages` 发起 POST。headers 固定包含：

```text
x-api-key: <secret>
anthropic-version: 2023-06-01
accept: text/event-stream
content-type: application/json
```

`base_url` 仍由公共 transport 当作 API prefix 处理，因此 host-only 与带代理路径的地址拥有相同追加规则。Provider 不引入 Anthropic SDK：当前 transport 已满足任意兼容 base URL、稳定 JSON、受控 SSE frame、取消和 timeout，SDK 会重复网络层并弱化请求字节控制。

Claude Code 参考实现通过 SDK 得到相同的 API version 与 `x-api-key` 行为；EasyCode 只采纳 wire 结果，不复制其 SDK、鉴权、登录、beta 或 retry 体系。备选方案是新增 SDK，但它会增加依赖、绕过现有 transport fixture，且无法改善本切片的协议正确性，因此不采用。

### 2. `max_tokens` 使用 Provider 内部显式默认值

`anthropic.Config` 增加仅供内部装配和测试使用的 `MaxOutputTokens`；零值归一化为具名常量 `4096`，负值拒绝，显式覆盖必须为正数。请求始终序列化最终值，避免遗漏 Messages 必填字段或由兼容网关猜默认值。

Claude Code 2.1.88 按已知模型维护 4096 至更高值的能力表，但 EasyCode 允许任意模型名和兼容服务，照搬该表会快速过期并把服务商策略泄漏到首个切片。选择 4096 作为保守兼容默认值，后续若要让用户配置输出预算，必须单独修改 config capability 并验证不同 Provider 的语义，而不是在本变更中加入 Anthropic 专属 JSON 字段。

### 3. 原生历史以 Anthropic message 和 content block 为单位

保留 `NativeItem` 作为实现 `provider.NativeItem` 的完成 content block，并扩展为强类型字段：

```text
NativeItem
  Type
  Text
  Thinking
  Signature
  RedactedData
  Raw (仅受控 opaque block，不直接暴露给共享层)

nativeMessage
  Role: user | assistant
  Content: []NativeItem

messageMetadata
  ID / Model / StopReason
  RawUsage（缺失字段保持 unknown）
```

Conversation history 保存 `nativeMessage` 与对应完成 metadata，并在 snapshot/commit 时深拷贝 slice 和 `json.RawMessage`。user message 也使用 Anthropic text block，不借用 OpenAI `NativeItem` 或共享扁平 Message。

已知 text/thinking block 由强类型字段稳定序列化；redacted thinking 以强类型 `data` 加受控 raw 副本保存，回放时不得解析或改写。`Raw` 不使用普通 `json:"raw"` 写入 wire，而由自定义 marshal/unmarshal 控制，避免把内部 envelope 字段错误发送给 API。未知 event 可以忽略，未知数据只能留在 Anthropic 包内的 raw envelope，不能变成跨层 `map[string]any`。

备选方案是只保存最终可见文本，无法回放 signature；另一个备选方案是保存所有 event JSON 并在下一轮重放，会把 stream event 与 request content block 混为一体。两者都违反原生历史边界，因此不采用。

### 4. RequestCompiler 是无副作用的确定性纯函数

请求结构固定为：

```json
{
  "model": "<configured-model>",
  "messages": ["<anthropic-native-message-params>"],
  "max_tokens": 4096,
  "stream": true
}
```

首轮 messages 只有当前 user text block；后续轮为已提交 history 的深拷贝加当前 user message。response metadata（message id、model、stop reason、usage）不属于下一次 request message，不会混入 content blocks。tools、system、thinking、temperature、beta 和 `cache_control` 全部省略，而不是输出空值或占位字段。

编译器只接收 model、归一化输出上限、history 和 user message，不读取环境、时间、TUI 或随机状态。API key 只进入 HTTP header，不进入 request struct、golden 或 cache fingerprint。相同输入通过 `codec.MarshalStable` 和 `context.NewSegment` 必须得到相同 bytes/hash。

### 5. 每个 turn 使用独立的索引状态机

Anthropic reducer 是 Conversation 每轮新建的状态对象，至少持有 message 是否开始、按 index 分配的 block slots、各 slot 的 started/stopped 状态、完成 block 顺序和最终 metadata。处理规则如下：

- `message_start`：只能出现一次；保存 message id/model 与初始 usage，不把 response message 直接作为 request history。
- `content_block_start`：index 必须未占用；text/thinking 初始化为空并只从后续 delta 累积，避免参考实现记录的 start 内容与首个 delta 重复；redacted thinking 在 start 时复制 opaque data。
- `content_block_delta`：index 必须处于 started；`text_delta` 只匹配 text 并发出既有 `assistant_text_delta`，`thinking_delta` 与 `signature_delta` 只匹配 thinking。signature 按协议作为最终 opaque 值保存而不是可见文本。
- `content_block_stop`：index 必须 started 且未 stopped；冻结深拷贝后的 `NativeItem`，按 index/wire 顺序进入 staging 并在 terminal 前发布 native event。
- `message_delta`：保存 stop reason，按 Anthropic 累计 usage 语义合并；message_start 的输入/cache 字段不会被 message_delta 中代表“未更新”的零值覆盖，输出字段采用最新服务端值。
- `message_stop`：要求已收到 message_start、至少一个完成 block、没有活动 block；随后标记 reducer 成功，任何后续 Provider event 都是协议错误。
- `error`：转为安全的 provider request failure；`ping` 和未知但格式正确的 event 不改变状态。

损坏 JSON、重复 index、未 start 的 delta/stop、block/delta 类型错配和未闭合 block 都立即失败。SSE frame 的 event name 不作为核心控制流依据，Reducer 只读取 JSON envelope 的强类型 `type`；这避免兼容服务省略 SSE `event:` 行时行为漂移。

### 6. `message_stop` 驱动事务提交和唯一 terminal

`Conversation.Stream` 延续 OpenAI Conversation 的所有权模型：Provider 持有不可变配置与 transport；每个 Conversation 独占 history 和 active-turn guard；每个请求由一个 consume goroutine 拥有 reducer staging、transport stream 和输出 channel。

收到合法 `message_stop` 后，consumer 取消并排空 transport stream，原子提交“当前 user message + 完整 assistant message + metadata”，发送一次 completed terminal，然后关闭输出 channel。error、cancel、idle timeout、oversized event、解析错误或 EOF 全部丢弃 staging，并发送一次 failed/cancelled terminal。transport 或 Provider 均不自动重连/重放；已有文本可能已经显示在 TUI，但不会进入下一次 request history。

Anthropic 与 OpenAI reducer 不抽成通用基类。两者可以复用 transport 与共享 `provider.StreamEvent`，但 message/block 和 response/item 生命周期不同；复制少量终态映射比创建带 Provider 分支的“通用 reducer”更清晰。若后续出现完全相同且稳定的 transport error 映射，可用小型无状态 helper 收敛，但本变更不预建抽象。

### 7. Capabilities 只声明已经闭环的能力

Anthropic Provider 在本切片完成后声明 `Streaming=true` 和 `ThinkingSignature=true`。即使默认 request 不主动开启 thinking，reducer、native history 和下一轮编译已经能够无损承载服务端提供的 thinking/signature/redacted blocks，因此 signature preservation 是实际能力。

FunctionTools、CustomTools、ParallelToolCalls、PromptCacheControl、PromptCacheKey、PreviousResponse、ReasoningSummary、RawReasoning 和 EncryptedReasoning 保持 false。原始 usage 的保存不等于统一 UsageParser 已完成，也不新增虚假的 usage capability。

### 8. App 使用小型资源关闭边界选择 Provider

`newChatResources` 在现有统一配置校验后按 `ProviderFamily` 分支：

- OpenAI：保持当前 `openai.New -> NewConversation`；
- Anthropic：使用 `anthropic.New -> NewConversation`；
- 其他值：沿用配置或 provider unavailable 错误，不做 wire 猜测。

`chatResources` 不再持有具体 `*openai.Provider`，改为 app 包内最小 `Close() error` 接口或等价关闭函数，同时继续持有同一个 `ChatSession`。该接口只表达 app 已经需要的资源生命周期，不加入共享 Provider Kernel，也不形成新的跨包公共 API。

TUI Model、Runtime 和 ChatSession 不需要 Provider 分支：两套 Conversation 都产生相同 typed text/terminal 事件。现有“Anthropic unsupported” app 测试改为“Anthropic 可装配且不建连”，再增加两种 Provider 分别进入 idle 的配置来源测试；既有 OpenAI 测试必须继续通过。

### 9. 缓存与安全回归集中在最终 wire 边界

虽然本切片不发送 `cache_control`，Provider 变更仍必须建立 request golden 和 fingerprint regression。golden 只保存正文，不包含 `x-api-key`；header 由 httptest 断言并使用固定假 key；HTTP 非成功、stream error 和配置错误不得回显 header 或 body。

Claude Code 的 cache marker 会避开 thinking/redacted blocks，但 EasyCode 在 ContextPlanner 和稳定 system/tool segment 尚未接入前不提前复制该策略。`PromptCacheControl` 保持 false，避免 capability 与 wire 不一致。后续 CachePlanner 变更必须独立增加 marker 位置 golden，并继续保证 block 原始顺序不变。

### 10. 测试按 request、reducer、conversation 和 app 四层组织

- Request 单测：首轮 request golden、历史顺序、延期字段缺失、4096 默认值、显式输出上限和稳定 fingerprint。
- Native/reducer 单测：text、thinking/signature、redacted thinking、usage 合并、stop reason、unknown event、error event、重复/越界 index、类型错配、未闭合 block、JSON 损坏和 native round-trip。
- Conversation 集成测试：httptest 验证 headers、host-only/带路径 prefix、两轮 request、message_stop terminal、cancel、idle timeout、EOF、HTTP error、失败不提交以及两个 conversation 历史隔离。
- App/TUI 回归：两种 family 都可无网络装配并进入 idle；TUI 继续只消费 RuntimeEvent；snapshot 和错误中不出现 API key、header 或 body。

Provider fixture 依据指定参考工程的事件序列自行编写最小 JSON，不复制大段源代码或依赖真实服务。公共 SSE 任意 chunk、UTF-8、LF/CRLF 和 frame 上限已有 transport 测试；Anthropic 集成测试补充跨多个 frame 的 Provider 归并，不重复实现 parser 测试。

## Risks / Trade-offs

- [4096 默认输出上限对新模型偏保守] → 保证未知兼容服务的首个切片更少因上限拒绝请求；Provider Config 保留显式覆盖入口，外部可配置化另走 config change。
- [部分兼容网关省略 `message_delta` 或 usage] → `message_stop` 仍可完成，缺失 metadata 保持 unknown；不以零伪装，也不阻塞文本对话。
- [非标准服务只在 `content_block_start` 返回非空 text 且不发送 delta] → 按 Anthropic 流式契约和参考实现仅从 delta 累积；这种服务会得到空 block 并由 fixture 暴露，不加入模糊去重启发式。
- [未知 event 被忽略可能掩盖新能力] → 只忽略格式正确且不影响当前状态机的 event，已知生命周期违规立即失败，capability 保持 false，后续用独立 change 增加支持。
- [opaque redacted block 增加内存占用] → 继续受 transport 单 event 4 MiB 上限约束，并在 snapshot/commit 深拷贝，避免共享可变 buffer。
- [App 的资源抽象可能被扩张成 Provider 大接口] → 接口只保留 `Close() error`，创建和能力仍由具体 Provider/现有 Factory 完成。
- [完整 P1 仍缺 CachePlanner、UsageParser 和 HistoryProjector] → 在 proposal、capabilities 和 Roadmap 中保持明确未完成，不用本切片的原始 usage 保存冒充阶段退出条件。

## Migration Plan

1. 先定义 Anthropic 原生 message/content-block、稳定 request compiler 和 golden/fingerprint 测试，不接入网络副作用。
2. 实现独立 reducer 及事件 fixture，固定合法/非法状态转换、thinking round-trip、usage unknown 和显式 `message_stop` 终态。
3. 将 reducer、turn staging 和 native history 接入现有 SSE transport，完成取消、timeout、EOF、双轮与 prefix 集成测试。
4. 将 app 资源所有权收敛为最小关闭边界，按 family 装配 Anthropic 或 OpenAI，并更新 TUI/app 回归测试。
5. 更新 README、Roadmap 与必要的踩坑记录，运行 `make verify` 并人工核对依赖方向、secret、cache fingerprint 和无 dead code。

当前没有 Anthropic 持久化 session 或外部稳定 API，因此不需要数据迁移。若实施必须回滚，可整体撤销 Anthropic Provider 与 app 分支，恢复 Anthropic unsupported 行为；OpenAI 请求、历史、RuntimeEvent 和 TUI 状态模型不需要回滚。
