## Context

参见 [proposal.md](./proposal.md) 的动机说明。当前共享 `StreamEvent` 允许调用方直接写入 kind、semantic、native、prepared sample 与 error，Runtime 只在读取循环中按 kind 分支，发现非法顺序时会立即返回。OpenAI 与 Anthropic 的生产 goroutine 使用有界输出 channel；当 Runtime 提前停止读取时，生产者可能阻塞在发送路径，Conversation 的 active 状态和 transport 清理也无法完成。

现有生命周期已经约定生产者关闭输出 channel、Provider 取消后排空 transport、应用在 shutdown 超时时强制关闭 transport。本设计必须加强这些边界而不改变 Provider wire、原生历史、缓存 fingerprint、Session records、RuntimeEvent 或 headless JSONL。

## Goals / Non-Goals

**Goals:**

- 使共享 stream event 的合法状态由类型化构造与纯内存验证共同保证。
- 让 Runtime 成为单次 Provider stream 的唯一消费 owner，并把 channel 关闭作为生产者清理完成信号。
- 在任何消费端提前失败后立即取消生产者，同时继续排空到 channel 关闭。
- 将 Runtime、AgentLoop 与 StreamRunner 的可变运行状态集中到单次运行对象，使转换和清理 ownership 可直接测试。
- 保持 durable 顺序、错误码、用户输出以及两家 Provider 的 request/native/cache 行为不变。

**Non-Goals:**

- 不新增 RuntimeEvent kind、Provider wire 字段、Session record、schema revision 或迁移逻辑。
- 不改变 Conversation 接口为通用 stream handle，也不增加跨包 manager、utils 或共享状态机框架。
- 不为不遵守取消契约的 Provider 增加 watchdog、超时关闭 channel 或无 owner waiter。
- 不在本变更中引入 Tool Loop、并行 sample 或跨 Provider history 转换。

## Decisions

### 1. StreamEvent 使用封闭字段、类型化构造和只读访问

`StreamEvent` 的 kind 与所有 payload 改为私有字段。共享包提供 semantic、native item、completed、failed、cancelled 五个构造器；构造器返回 `(StreamEvent, error)`，以便在边界处拒绝 nil、冲突或无效 payload。事件对外只暴露 `Kind`、semantic/native/prepared/error 的只读访问器以及 `Validate`，不提供可变引用或兼容字段。

`Validate` 按 kind 检查恰好一个适用 payload：semantic 调用 `protocol.Event` 的纯内存验证并限制为 Provider 可发布的语义 kind；native item 检查非 nil、有效 family 与非空 item kind；completed 通过 prepared sample 的只读复制入口确认尚未 finalized 且 envelope/usage 有效；failed 与 cancelled 要求非 nil error。验证不会执行 finalizer，也不会改变 sample 状态。

`protocol.Event` 增加统一的纯内存验证入口，按已知 kind 分派现有 strict decoder/validator。它允许 Provider 刚产生、尚未由 Runtime 装饰 identity 的合法 assistant delta，也允许 Runtime 装饰后的完整事件；identity 一致性仍由 Runtime 的既有 decorate 边界负责。StreamEvent semantic 构造器再限制 Provider 可发布的 kind，避免将 `turn_started` 或 turn terminal 伪装为普通 stream semantic。

备选方案是保留导出字段并只在 Runtime 检查。该方案仍允许每个调用点构造非法组合，并把错误延迟到 goroutine 已启动之后，因此不采用。另一方案是为每个 kind 建立不同 Go 类型，但这会让 channel 和 Provider 接口需要新的 sum-type facade，增加迁移面而没有额外行为收益。

### 2. Runtime 派生 context，并由单一 helper 消费到 channel 关闭

`RunTurn` 在调用 `Conversation.Stream` 前创建单次 stream 派生 context。成功取得 channel 后，消费 helper 按顺序执行以下状态转换：

1. 对每个事件先调用 `Validate`。
2. 在首个 terminal 前转发合法 semantic；native item 保持既有处理语义。
3. 记录唯一 terminal，但不在收到 terminal 时返回，继续读取直到 channel 关闭。
4. 发现非法事件、重复 terminal、terminal 后事件或其他消费端失败时，保留首个错误，立即取消派生 context，停止向宿主转发，并继续排空 channel。
5. channel 关闭后，根据首个消费错误、terminal 与调用 context 计算唯一结果；成功 completed sample 只有此时才进入 durable commit/finalize。

helper 的返回值包含已验证 terminal 或首个消费错误，不拥有 durable 写入。这样 `RunTurn` 可以拆成同层级的 turn 开始、上下文规划、stream 消费、成功提交和失败收口 helper，且 durable ordering 保持集中可审计。所有退出路径都调用 cancel；若 `Conversation.Stream` 建立失败，则无需排空尚未创建的 channel。

选择同步排空而不是启动后台 drainer，是因为 RunTurn 本身就是消费 owner，只有它等待 channel 关闭才能证明生产 goroutine 已退出并安全启动下一 turn。选择 channel 关闭作为完成信号，而不是扩展 Conversation 返回额外 `Done`，是因为现有生产者已拥有 channel 且 defer 顺序可保证关闭发生在 transport 与 active 状态清理之后。

Provider 若取消后不关闭 channel，Runtime 会继续等待；shutdown 的既有应用资源 owner 仍可强制关闭 transport，使生产路径退出。这里不增加 timeout goroutine，因为超时返回却遗留 waiter 会破坏唯一 cleanup owner。

### 3. Provider 生产者只发布已构造事件，并保留关闭 ownership

OpenAI 与 Anthropic reducer 继续产生各自 typed semantic/native 结果，conversation producer 在发送前用共享构造器封装。每个生产路径只允许一次 terminal：wire 成功产生 completed，调用取消产生 cancelled，其他 transport、idle timeout 或 protocol error 产生 failed。

生产 goroutine 仍是输出 channel 的创建者和唯一关闭者。其退出顺序固定为停止或排空 transport、取消内部 context、释放 Conversation active 状态、关闭输出 channel。集成测试从消费者侧断言 channel 关闭时这些清理事实已经成立。

不抽取跨 Provider 的通用发送 manager。两家 Provider 的 reducer、transport error 映射和 native sample 准备生命周期不同，只共享 `StreamEvent` 契约；强行共享发送骨架会隐藏 wire 特有的终态判定。

### 4. 三个编排入口使用各自的单次运行状态

`Runtime.RunTurn` 只保留编排骨架，私有 helper 分别负责开始 durable turn、构造上下文计划、消费 stream、提交成功 sample 与 durable 失败收口。helper 使用显式参数和返回值，不把 context 保存到 Runtime 长期结构。

`AgentLoop.Run` 创建仅属于该次 Run 的私有状态对象，集中持有 mode、follow-up queue、queued bytes、active turn、outstanding/recent request ledger 与 run error。admission、interrupt、turn event、turn completion 和关闭分别成为该对象的方法；`AgentLoop` 仍拥有固定 requests/outputs/done channel，活动 turn 仍只有一个 worker，owner goroutine 仍是所有状态转换的唯一写者。

`StreamRunner.Run` 创建私有运行状态，集中持有 transport request、reader/loop/stop completion、首个 failure、stdout 可用性和 projector/encoder。状态方法分别处理 transport 请求、幂等 stop、first-error-wins 和输出事件；Run goroutine 仍是唯一 stdout writer，transport owner 仍是唯一 input/output closer。

这些状态对象不会跨包共享。三处虽然都有“first error”或“closing”概念，但它们的 channel ownership、durability 与退出条件不同，共享通用状态机会模糊职责。

### 5. Provider 创建与 wire 映射保持单一来源

`newProviderResource` 只返回已创建的 Provider resource 与错误，不再同时返回随后被丢弃的 wire 字符串。`providerWire` 保持 family 到 `responses`/`messages` 的唯一映射来源，所有装配与测试都通过该入口获取 wire。

备选方案是让 resource 携带 wire，但当前 Provider resource 的职责是生命周期和 conversation factory；wire 属于配置/Session 元数据映射，把它放入 resource 会重复 family 已表达的信息。

## Risks / Trade-offs

- [风险] 同步排空会让不遵守取消契约的 Provider 延长 RunTurn 返回时间 → 保留应用资源 owner 的 transport 强制关闭路径，并用确定性阻塞 transport 测试证明能够解除等待。
- [风险] completed 已到达后出现尾随事件时 sample 已被准备但不会 finalize → 将 prepared sample 保持在 staging，协议失败只 durable 记录失败边界，生产者退出后由局部对象释放。
- [风险] `protocol.Event` 统一验证可能错误要求 Provider 阶段已有 Session identity → 验证只处理版本、kind、typed payload 与字段组合，identity 完整性继续由 Runtime 装饰及宿主边界验证。
- [风险] 大规模私有字段迁移会遗漏测试夹具或 fake conversation → 通过全仓搜索直接字段访问、架构测试和全量编译一次性清除，不保留双轨接口。
- [风险] 状态对象重构可能改变队列、interrupt、EOF 或 broken output 的竞态结果 → 保留现有回归测试，并为提取后的每个状态转换增加无需 sleep 的直接测试。

## Migration Plan

1. 先加入 `protocol.Event` 与 `StreamEvent` 验证、构造器、访问器及表驱动测试。
2. 将 OpenAI、Anthropic 生产路径和全部测试 fake 迁移到 typed constructor，验证每条路径唯一 terminal 和最终 channel close。
3. 引入 Runtime 消费 helper 与派生 context，补齐非法事件、尾随 terminal、提前关闭和取消竞态故障注入，再拆分 RunTurn 阶段 helper。
4. 在行为测试保护下分别提取 AgentLoop 和 StreamRunner 的单次运行状态。
5. 简化 Provider resource 返回值与 wire 映射，运行 request golden、cache fingerprint、Session/runtime/headless 回归和完整质量门。

本变更只涉及仓库内部 API，不需要数据迁移。若实现期间发现 contract 无法保持，可在归档前回退该 change 的代码提交；既有 Session、Provider history 与配置文件不需要回滚或重写。
