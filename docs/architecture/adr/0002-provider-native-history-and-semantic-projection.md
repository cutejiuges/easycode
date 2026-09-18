# ADR-0002：保留 Provider 原生历史并提供单向语义投影

- 状态：accepted
- 日期：2026-09-18
- Roadmap 阶段：P0-P2

## 背景

Anthropic Messages 与 OpenAI Responses 在请求结构、流式事件、推理数据、工具输入和缓存语义上存在不可逆差异。把两边压缩成一个通用扁平 Message，再从 Message 重建下一次请求，会丢失 thinking signature、redacted thinking、encrypted reasoning、message phase 和原生工具配对信息。

但共享层仍需要读取历史中的稳定语义：ContextPlanner 要估算 token，TUI 要在 resume 时渲染历史，Stop hook 要读取最后一段 assistant 文本，Subagent 要提取 completion 正文。如果没有统一接缝，这些消费者会分别实现 Provider 分支，形成重复且容易漂移的投影逻辑。

Provider 差异按泄漏方向分为三类：

1. **序列化差异**：请求、响应、SSE event 和 native item 结构，只能留在 Provider 包内。
2. **能力差异**：reasoning、工具、缓存、图像等能力，通过 Capability Profile 显式暴露，不能根据服务商名称猜测。
3. **策略差异**：请求编译、流式归并、缓存、工具 wire、压缩和历史投影，通过 Provider Kernel 的小接口组合实现。

这个分类既支持未来增加第三种 Provider，也防止差异扩散到 Runtime、TUI、Hooks 和 Subagent。

## 约束

- 下一次请求、原生 resume 和 Session 事实记录必须使用 Provider-native history。
- opaque reasoning 数据不得伪造、跨 Provider 转换或通过通用结构重建。
- 共享消费者不能直接依赖 Anthropic/OpenAI wire 类型。
- 相同 Provider 的语义投影逻辑只能实现一次。
- 投影视图不承诺可逆，也不能成为新的 canonical message model。

## 候选方案

1. 所有 Provider 映射为统一 Message，并以 Message 作为事实源。
2. 各共享消费者分别解析 Provider-native history。
3. 保留 Provider-native history，并由每个 Provider 实现单向 `HistoryProjector`。

## 最终决策

采用方案 3。Provider Kernel 增加第九个策略 `HistoryProjector`：

```text
Provider-native history
          |
   HistoryProjector
          |
  SemanticHistoryView
   |      |      |      |
 token  resume  hooks  subagent
 estimate render text   result
```

`SemanticHistoryView` 是只读、允许有损、带来源标识的语义视图，可以表达用户/assistant 文本、可展示的 reasoning summary、工具调用与结果摘要、phase 和完成状态。它不得保存或暴露可用于伪造续写的 signature/encrypted content。

以下消费者统一使用该视图：

- token 预算与估算器；
- resume/history UI 回放；
- Hooks 所需的受限文本上下文；
- Subagent completion envelope 的正文抽取。

RequestCompiler、NativeHistory、Session 原生记录和 Provider resume 禁止消费该视图反向构造 wire。

## 代价与风险

- 每个 Provider 需要额外维护一套投影器和 golden fixture。
- 投影本身有损，新增共享消费者时必须确认所需语义已被显式表达。
- 如果视图字段不断追逐所有 Provider 细节，它会退化为统一消息模型；因此字段只服务共享只读场景。

## Provider 与缓存影响

- 投影不参与 Provider 请求 canonical bytes，也不替代 cache plan。
- token 估算可以读取投影视图，但最终 usage 和缓存指标仍以 Provider `UsageParser` 的归一化结果为准。
- 投影结构变更不得无意改变稳定 prompt 前缀。

## Session/协议迁移影响

- Session 继续持久化 Provider-native item；语义视图默认按需重建，不作为事实源。
- 如果未来持久化投影缓存，必须带独立 schema/revision，并允许从 native history 全量重建。
- RuntimeEvent 可以由 live reducer 或 history replay 产生，但两条路径必须通过 fixture 保持用户可见语义一致。

## 验证与回归测试

- 每个 Provider 建立 native history 到 semantic view 的 golden test。
- 同一份历史在 live stream 与 resume replay 下产生等价的可见文本、工具状态和 phase。
- token、Hook、Subagent 和 TUI 测试不得导入 Provider wire 类型。
- 增加架构检查，禁止从 `SemanticHistoryView` 调用 RequestCompiler。

## 回滚或替代方案

如果单一语义视图无法满足差异较大的消费者，可以拆分为更小的只读投影接口，例如 `TextProjector` 和 `ToolHistoryProjector`；不得回退为统一 Message 事实源或让消费者直接解析 native item。
