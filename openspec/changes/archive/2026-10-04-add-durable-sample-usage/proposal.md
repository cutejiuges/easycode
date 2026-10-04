## Why

当前双 Provider 文本链路只在 Anthropic 原生提交中部分保存 usage，OpenAI usage 尚未解析，共享层也没有能够区分已知零、未知和不适用的统一事实，因此 Runtime、Session 和 headless 无法可靠报告一次已计费 sample 的 token 使用。项目仍处于 P2 开发阶段且尚未发布稳定协议，现在应直接补全并冻结单一 v1 基线，避免为尚未完成的能力引入并行版本和长期兼容分支。

## What Changes

- 新增 Provider 无关的 sample usage 模型，统一表达 `input_uncached`、`cache_read`、`cache_write`、`output` 和 `reasoning_output`，每个指标显式区分 `known`、`unknown` 与 `not_applicable`。
- 为 Anthropic Messages 和 OpenAI Responses 分别实现严格的原始 usage 解析、Provider 语义校验和统一投影；保留 Provider 原始字段，不以零替代缺失值。
- 让每个成功 `PreparedSample` 同时携带 opaque native commit 与不可变 normalized usage；失败、取消或非法 usage 不产生成功 sample。
- 新增 required `sample_usage(v1)` Session record，并与对应 `provider_native_commit(v1)`、`turn_completed(v1)` 在当前文本 turn 的同一 durable batch 中提交。
- 在 durable append/Sync 和 Provider finalizer 成功后，通过 `turn_completed` RuntimeEvent 发布本 turn 聚合 usage；当前单 sample turn 的聚合值等于该 sample usage。
- 将 OpenAI native payload v1、进程内 RuntimeEvent v1、Session v1 和 headless JSONL v1 直接补全为包含 usage 的唯一当前契约，不新增任何 v2 或双 decoder。
- **BREAKING**：重写当前开发期 Session v1 fixture 和 headless v1 golden；本变更前生成的两记录完成 journal 不再支持 resume，必须在任何 repair、append 或 Provider 请求前 fail closed，且不得原地迁移或静默补造 usage。
- 补充 Provider fixture/golden、重写后的 Session v1 不可变 fixture、恢复等价、Runtime durable 顺序及 headless JSONL v1 golden 测试。
- 本变更不实现价格或成本计算、context-window 占用、compaction 决策、TUI usage 展示、thread/session 累计输出、SQLite usage 索引、工具循环或 Provider cache 请求策略。

## Capabilities

### New Capabilities

- `provider/usage-accounting`: 定义跨 Provider 的 sample usage 指标、三态语义、Provider 映射、校验与聚合规则。

### Modified Capabilities

- `provider/anthropic-messages-text`: 将现有 Anthropic 原始 usage 归并规则扩展为严格校验并产生 normalized sample usage。
- `provider/openai-responses-text`: 解析并保留 `response.completed` 原始 usage，产生 normalized sample usage。
- `provider/native-history-persistence`: 成功 sample 同时提供原生提交与 normalized usage，并保证原始 usage 的持久化/恢复等价。
- `runtime/chat-turn`: 在 durable sample facts 提交后通过完成事件发布 turn usage，且不得在失败路径伪造 usage。
- `session/jsonl-store`: 新增 required `sample_usage(v1)` record，并将成功完成 batch 重写为唯一的三记录 v1 基线。
- `headless/text-output`: 保持 JSONL v1，为 `turn.completed` 增加 required usage，并以新 golden 冻结完整 v1 shape。

## Impact

- 受影响代码主要位于 `internal/provider`、`internal/provider/anthropic`、`internal/provider/openai`、`internal/protocol`、`internal/runtime`、`internal/session` 和 `internal/headless`。
- `PreparedSample`、RuntimeEvent payload、Session record registry/replay plan 和 headless `turn.completed` 是需要同步修改的跨包契约；具体 Provider wire 类型仍留在各 Provider 包内。
- Session envelope、所有当前 Session payload、两家 Provider native payload、进程内 RuntimeEvent 和 Headless JSONL 均保持唯一版本 `1`；版本字段继续作为未来真正迁移时的兼容护栏，但本变更不维护并行 revision。
- 当前开发期 Session v1 fixture 将原位重建为完整 usage 基线，并在本变更验收后重新冻结；旧两记录 journal 明确不兼容。Headless JSONL 仍为 `version: 1`，事件类型、顺序、终态和退出码不变，但新的 `turn.completed.usage` 成为 v1 必需字段。
- 不新增第三方依赖，也不改变 Provider 请求缓存 fingerprint；新增的 usage 只来自响应完成态，不参与下一次请求编译。
