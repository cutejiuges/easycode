# ADR-0004：首版 OpenAI Wire 仅支持 Responses

- 状态：accepted
- 日期：2026-09-18
- Roadmap 阶段：P0-P1

## 背景

EasyCode 允许用户提供任意 `base_url`、`api_key` 和模型名称。许多国产模型、本地推理服务和第三方 OpenAI 兼容服务只实现 Chat Completions，或者只部分实现 Responses。如果“兼容 OpenAI 服务商”不区分 wire，仅支持 Responses 会让这些服务在首版完全不可用。

另一方面，OpenAI Responses 才能完整表达 reasoning item、encrypted reasoning、message phase、增量 response、部分工具形态和缓存信息。以 Chat Completions 作为首版共同底座会在架构建立之初永久丢失这些能力。

## 约束

- 产品需要保留 OpenAI/Codex 的原生能力，不把 Responses 降格为 Chat Completions。
- Anthropic Messages 与 OpenAI Responses 都必须支持自定义 base URL。
- 不能因为 endpoint 看起来兼容就推断 reasoning、tools、cache 或 streaming 能力。
- 不得把只支持 Chat Completions 的服务宣传为首版已兼容。

## 候选方案

1. 首版同时完整实现 Responses 和 Chat Completions。
2. 首版只实现 Chat Completions，以最大化兼容服务数量。
3. 首版完整实现 Responses，后续增加明确降级的 Chat Completions wire。

## 最终决策

采用方案 3，并在 P1 中优先实现 OpenAI Responses，再实现 Anthropic Messages。首版 wire 范围为：

- `family = anthropic, wire = messages`
- `family = openai, wire = responses`

`base_url + api_key` 只表示连接方式，不表示任意 OpenAI-compatible endpoint 都可用。Provider probe/doctor 必须报告 endpoint/wire 不兼容，而不是静默切换或返回模糊网络错误。

未来增加：

- `family = openai, wire = chat_completions`

该模式是显式选择或探测后确认的降级能力，具有独立 RequestCompiler、StreamReducer、NativeHistory、ToolWireCodec、UsageParser、HistoryProjector 和 fixture。不得由 Responses adapter 隐式改写请求。

## 代价与风险

- v1 无法使用仅支持 `/v1/chat/completions` 的国产生态、本地模型和兼容网关。
- 用户可能把“OpenAI compatible”误解为可直接运行，需要在 README、配置诊断和错误信息中明示 wire 范围。
- 后续 Chat Completions 实现需要单独的测试和维护成本。

接受该生态代价，是为了先建立无损的 OpenAI 原生能力和正确的 Provider Kernel 边界，而不是认为 Chat Completions 不重要。

## Provider 与缓存影响

- Responses 和 Chat Completions 必须拥有独立 canonical request、cache key 和 usage 归一化 fixture。
- Chat Completions 不得伪造 encrypted reasoning、message phase 或 Responses incremental state。
- 不同 wire 之间恢复会话按 compacted fork 处理，除非将来有明确且可证明无损的迁移协议。

## Session/协议迁移影响

- Session metadata 必须记录 provider family、wire、model 和 capability revision。
- 增加 Chat Completions 后，旧 Responses Session 继续按 Responses 恢复，不因 endpoint 探测结果自动换 wire。
- wire 变更属于契约变更，必须通过新的 OpenSpec change，并引用本 ADR。

## 验证与回归测试

- P1 fixture 必须覆盖 Responses 成功、Chat-only endpoint 的明确拒绝和 capability downgrade。
- 错误码与错误消息使用英文，并能区分 endpoint missing、wire unsupported 和 capability unsupported。
- README/doctor 显示当前 wire，不能只显示 `provider=openai`。
- 未来 Chat Completions 开工时，以本文的降级边界作为验收基线。

## 回滚或替代方案

如果真实用户覆盖率证明 Chat-only 服务是首版发布的必要条件，应通过新的 OpenSpec change 提前 Chat Completions 阶段；不得直接把 Responses 实现替换成 Chat Completions。
