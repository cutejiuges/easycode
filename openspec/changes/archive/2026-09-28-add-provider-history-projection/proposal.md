## Why

OpenAI Responses 与 Anthropic Messages 已经能够事务化保存各自的原生多轮历史，但共享层仍没有安全读取历史文本的统一接缝。P2 的 token 估算与 resume、后续 Hooks 和 Subagent 如果直接解析两套 Provider wire，会复制分支并诱导共享语义反向替代原生历史，因此需要在进入 Session 工作前落实 ADR-0002 已确定的单向 HistoryProjector。

## What Changes

- 新增最小 `SemanticHistoryView`，按已提交 turn 提供来源 Provider、用户文本和 assistant 可见文本；每次投影返回独立快照，不暴露可变的原生历史。
- 为 Provider Conversation 增加统一 HistoryProjector 契约，由 OpenAI 与 Anthropic 各自从自身 native history 单向生成语义视图。**BREAKING（内部接口）**：现有 `provider.Conversation` 实现和测试替身需要补充投影方法。
- Anthropic 按既有 `nativeTurn` 边界拼接 text blocks；OpenAI 将当前扁平历史收敛为显式 turn 边界，再按原顺序展平给 RequestCompiler，并聚合当前文本切片可见的 `input_text`/`output_text`。
- 投影仅包含成功提交的文本语义；thinking、signature、redacted thinking、reasoning summary、encrypted content、phase、usage、未知扩展和请求字段不得进入视图，也不得从视图反向构造 Provider 请求。
- 增加两家 Provider 的 projection golden、opaque 数据隔离、快照独立性、失败/活动 turn 不可见、live/replay 文本一致性、并发 race 及 OpenAI request/fingerprint 不变回归测试。
- 更新 P1 进展与相关文档；本变更不接入 Session 持久化、resume UI、token estimator、Hooks、Subagent、UsageParser、CachePlanner、reasoning 展示或 Tool 历史。

## Capabilities

### New Capabilities

- `provider/history-projection`: 定义两家 Provider 将已提交原生历史投影为只读、允许有损且不可用于续写的共享文本语义视图，并保证 live/replay 可见文本一致和 opaque 数据隔离。

### Modified Capabilities

无。

## Impact

- 主要影响 `internal/domain`、`internal/provider`、`internal/provider/openai`、`internal/provider/anthropic` 及相邻测试和 golden fixture。
- `internal/runtime` 的 Conversation 测试替身需要满足新增小接口，但 Runtime 继续只处理 stream 和 RuntimeEvent，不负责历史投影。
- OpenAI native history 的内部存储形态会改变；Responses request 的 item 顺序、canonical bytes、fingerprint 和多轮行为必须保持不变。Anthropic native history 与请求编译不改变。
- 不修改外部配置、CLI、RuntimeEvent、Session/JSONL/SQLite schema 或 Provider wire，不新增第三方依赖，也不改变现有 capability 声明和基础 TUI 行为。
