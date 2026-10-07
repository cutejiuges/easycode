## Why

当前 `provider.StreamEvent` 的导出字段允许任意调用方构造零值、冲突 payload、nil failure 或已失效 prepared sample，Runtime 又会在发现非法事件或 terminal 后事件时立即返回而不继续消费 channel。这会让 Provider 生产 goroutine 阻塞在发送路径、Conversation 长期保持 active，并使下一 turn、shutdown 和资源关闭受到残留流影响。

该问题位于 Provider 公共内部契约与 Runtime 流 owner 的交界处，现有测试虽然覆盖正常成功、失败和取消，但尚未固定“消费端提前失败后仍取消并排空到生产者关闭”的完整生命周期，因此需要在进入 Tool Loop 前先收紧。

## What Changes

- **BREAKING（仓库内部 API）**：将 `provider.StreamEvent` 字段改为私有，只允许通过 semantic、native item、completed、failed、cancelled 五类 typed constructor 创建，并提供只读 getter 与完整 `Validate`。
- 为 `protocol.Event` 增加纯内存验证入口，使 semantic stream event 只能携带当前已知且字段组合合法的语义事件；不新增 RuntimeEvent kind 或 payload revision。
- Runtime 成为每次 Provider stream 的消费 owner：使用派生 context，逐项验证事件，记录唯一 terminal，并始终消费到 channel 关闭。
- 遇到非法事件、重复 terminal、terminal 后事件或消费端提前失败时，Runtime 立即取消派生 context，随后同步排空 channel，等待 Provider 生产者完成清理；只有清理完成后才 durable 收口失败并允许下一 turn。
- OpenAI 与 Anthropic Provider 只发布由 typed constructor 创建的合法事件，并继续由生产者关闭 channel、取消后排空 transport、最终释放 Conversation active 状态。
- 重构 `Runtime.RunTurn`、`AgentLoop.Run` 与 `StreamRunner.Run` 的私有运行状态，使 turn、队列、停止请求、stdout writer 和 cleanup owner 的转换可直接测试，保持现有事件顺序、错误码、JSONL、Session schema 与用户可见输出不变。
- 简化 Provider 装配：`newProviderResource` 只负责创建 Provider，wire 只由 `providerWire` 映射，删除被丢弃的重复返回值。

## Capabilities

### New Capabilities

- `provider/stream-lifecycle`：定义共享 StreamEvent 的封闭构造、字段组合验证、唯一终态、生产者关闭 channel，以及取消后完成清理的生命周期契约。

### Modified Capabilities

- `runtime/chat-turn`：补充 Runtime 在消费端协议错误或 terminal 冲突时取消并排空 Provider stream、等待生产者退出后再 durable 收口的要求。
- `runtime/input-control`：明确长期输入 owner 只有在前一 turn 的 Provider stream 已关闭且清理完成后，才能启动下一排队输入或确认最终关闭。

## Impact

- 主要影响 `internal/provider` 公共契约、OpenAI/Anthropic stream producer、`internal/runtime`、`internal/headless/stream_runner.go` 和 `internal/app` Provider 装配。
- 全部直接构造或读取 `StreamEvent` 导出字段的生产代码与测试都必须一次性迁移，不保留双轨兼容字段。
- 不改变 Provider request bytes、cache fingerprint、Provider-native history、Session JSONL/SQLite schema、RuntimeCommand/RuntimeEvent vocabulary、headless JSONL revision 或用户可见错误语义。
- 不新增外部依赖、后台 watchdog 或无 owner 的超时 goroutine；已有应用强制关闭 transport 路径继续负责解除不遵守取消契约的阻塞 Provider。
