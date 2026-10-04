## Why

当前工程已经具备 Provider 原生历史、只读语义投影和确定性缓存分段，但 Runtime 在发起请求前仍缺少统一、可测试的上下文规划步骤，无法解释本轮输入由哪些来源组成、稳定前缀为何变化，也不能在用户明确配置模型窗口时提前阻止确定超限的请求。现在补齐这一层，可以复用现有边界建立后续工具描述、项目指令、skills、缓存策略和压缩能力共同依赖的稳定基线，而不破坏 Provider-native history 的事实源地位。

## What Changes

- 新增纯内存、无 I/O 的确定性上下文规划能力：按固定顺序组合模型配置、已提交历史和当前用户输入，产出不可变的来源清单、缓存分段计划、稳定前缀指纹、分类 token 估算及预算判定。
- 为 token 估算定义显式方法与质量状态。共享层只读取 `SemanticHistoryView` 估算可见文本；各 Provider 从自己的原生历史给出只含数值和质量的私有 footprint，以覆盖 thinking、encrypted reasoning 等不能进入语义投影但会占用上下文的内容。
- 在可选 JSON 配置中加入 `context_window_tokens`、`reserved_output_tokens` 和 `context_safety_margin_tokens`。系统不根据模型名猜测窗口；未配置窗口时仍生成计划和指纹，但不执行硬性超限拒绝。
- Runtime 在 `turn_started` 已 durable、Provider stream 尚未建立的边界执行规划。只有在窗口已配置且估算可判定为超限时，Runtime 才 durable 记录失败并保证不发起网络请求。
- 保持 Provider RequestCompiler 继续直接消费原生历史；规划结果不持久化为 Session record，不新增 RuntimeEvent kind，也不改变当前 Anthropic/OpenAI request wire、缓存控制字段或 headless 输出。
- 不在本变更实现远程 token-count API、自动压缩、项目指令发现、工具/skill 注入、world state diff、Provider cache marker 或 prompt cache key。这些后续能力将消费本次建立的规划契约。

## Capabilities

### New Capabilities

- `context/planning`: 定义确定性上下文来源、不可变计划、token 估算质量、预算计算、缓存分段映射和敏感数据隔离契约。

### Modified Capabilities

- `config/json-file`: 增加三个可选上下文预算字段的严格解码、校验和默认语义。
- `provider/native-history-persistence`: 要求 Provider 从已提交及恢复的原生历史提供等价、只含数值的上下文 footprint，同时继续保护 opaque reasoning 数据。
- `runtime/chat-turn`: 在 durable `turn_started` 与 Provider 副作用之间加入规划和确定超限的失败收口语义。

## Impact

- 主要影响 `internal/context`、`internal/domain`、`internal/provider`、`internal/provider/anthropic`、`internal/provider/openai`、`internal/runtime`、`internal/config` 及应用装配代码。
- `provider.Conversation` 或其组合的小接口将增加只读原生历史 footprint 能力；Anthropic 与 OpenAI 都必须实现并覆盖 live/restored 等价测试。
- JSON 配置接受新的可选数字字段；现有配置、环境变量启动方式、Session schema、Provider native payload revision、请求 canonical bytes 和公开 RuntimeEvent 形状保持兼容。
- Provider/context 相关 golden、cache regression、恢复等价、Runtime 无网络超限和配置安全测试需要同步扩充，并最终通过 `make verify`。
- 本变更的 commit 与 PR 标题、说明及每个条目必须采用 `English // 中文翻译` 的中英双语格式。
