## MODIFIED Requirements

### Requirement: Presentation cannot enable reasoning or affect native truth

展示 SHALL 只消费实际返回值，不修改 thinking/display/effort/budget/summary 请求、工具目录、Provider 选择或缓存策略，不按模型名称推断远端能力。主动请求 summary 的能力与被动展示分开，ReasoningSummary SHALL 只按 provider/reasoning-control 的显式设置和完整已实现路径决定，不由展示改变；RawReasoning 保持 false，已实现原生保存能力保持原有含义。

相同历史展示前后及恢复前后的 canonical request、native 顺序和 fingerprint MUST 等价。OpenAI WebSocket 恢复的 full/incremental 按既有连接重置口径比较，Anthropic marker/TTL 独立核验；不改变原生 history 在续写/压缩中的使用方式。

#### Scenario: Continue after reading or restoring reasoning
- **WHEN** 用户阅读、原文切换、resize、恢复后继续同一历史
- **THEN** 对应请求 bytes、native 顺序及 fingerprint 与未进行展示操作时相同，签名和密文完整，无额外模型请求

#### Scenario: Receive no readable reasoning
- **WHEN** 服务端没有推理项或只有 opaque 推理
- **THEN** 分别无提示或只有 unavailable，不主动开启能力、不生成替代摘要

#### Scenario: Read with an explicitly configured policy
- **WHEN** 普通请求已显式配置推理策略，用户展开、关闭或重绘推理详情
- **THEN** 请求参数、能力位、预算和 fingerprint 不变，不披露原生签名/密文，不额外采样

#### Scenario: Keep empty thinking distinct from usage
- **WHEN** 显式 display omitted，或 summarized 请求实际只返回空 thinking/signature
- **THEN** 展示沿既有 unavailable 规则，实际 reasoning_output 独立取自 usage，不能推断为零、制造替代摘要或通过展开详情再次请求正文
