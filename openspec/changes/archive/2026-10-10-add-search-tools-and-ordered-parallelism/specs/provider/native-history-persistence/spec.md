## MODIFIED Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持 Provider SHALL 将已校验的 native history 增量编码为受控 opaque envelope，至少标识 Provider family、wire、单一当前 payload canary 和 payload。只有对应 Provider 的唯一当前 codec 可以解释其 payload；Session、Runtime、Tool executor、TUI 和其他共享消费者 MUST NOT 依赖具体 Provider wire 类型。

每个 Provider SHALL 只维护一种当前 sealed native history entry 模型和一套当前 encoder/decoder。entry 使用强类型 kind 区分 `sample` 与 `tool_outputs`：sample 保存可选当前输入、按 wire 顺序排列的完整输出及 raw usage；tool outputs 保存按原 call index 排列的 Read、Glob、Grep 原生结果输入且不携带 usage。首个 sample 必须具有真实用户输入，紧跟合法 tool outputs 的 continuation sample 必须不含伪造用户输入；含 tool calls 的 assistant 输出仍属于 sample entry。

Anthropic entry SHALL 保留原生 message/content block 与 metadata；OpenAI entry SHALL 保留原生 input/output items。两家可以共享提交生命周期接口，但 MUST NOT 共享或互转 wire DTO。业务类型、构造器与 decoder 名称 MUST NOT 使用版本后缀；payload canary 只做精确当前边界校验，MUST NOT 路由旧 reader。codec MUST 对 payload 大小、commit shape、call/output pairing、capability 结果数量和原生 item 顺序执行显式验证。

#### Scenario: Round-trip a heterogeneous output entry
- **WHEN** 一个 sample 按顺序包含 Glob、Grep、Read calls，且 Runtime 提供匹配的冻结 results
- **THEN** 对应 Provider 当前 codec 往返后保留相同 call IDs、结果顺序、preview bytes 和原生 output shape

#### Scenario: Encode an OpenAI sample commit
- **WHEN** OpenAI Responses 完成一个包含消息、reasoning 与工具调用的 sample
- **THEN** OpenAI 当前 codec 按原生 item 顺序编码并可逐字段恢复该 sample

#### Scenario: Encode an Anthropic sample commit
- **WHEN** Anthropic Messages 完成一个包含 text、thinking 与 tool_use 的 message
- **THEN** Anthropic 当前 codec 按原生 content block 顺序编码并可逐字段恢复该 sample

#### Scenario: Keep shared layers opaque
- **WHEN** Session、Runtime 或 Tool 层接收一个已校验 native envelope
- **THEN** 共享层只保存和传递 envelope，不解析、转换或重建 Provider payload

#### Scenario: Reserve an input-only native commit
- **WHEN** 当前 Provider history 需要表达只含真实用户输入的首个 sample
- **THEN** 当前 entry 模型表达该合法 shape，且 continuation sample 不伪造用户输入

#### Scenario: Encode tool outputs in the current history schema
- **WHEN** Runtime 提交按 call index 冻结的异构 tool results
- **THEN** 当前 codec 以 `tool_outputs` entry 编码结果，不引入另一个 payload schema

#### Scenario: Keep one current payload codec
- **WHEN** 当前 codec 编解码 sample 与 tool_outputs 两种 entry kind
- **THEN** 两者共享一个精确当前 canary，生产代码不存在历史 reader 或版本分派

#### Scenario: Preserve Provider separation
- **WHEN** Anthropic 与 OpenAI 表达语义相同的工具组
- **THEN** 两家分别使用自身 native DTO 与 codec，不互转 opaque payload

#### Scenario: Reject a stale payload canary
- **WHEN** native envelope 的 payload canary 不等于当前 Provider codec 常量
- **THEN** restore 在返回部分 Conversation 或发起网络请求前失败，且不尝试旧 decoder

#### Scenario: Keep one codec for both entry kinds
- **WHEN** Conversation 先提交含 calls 的 sample 再提交 tool outputs
- **THEN** Provider 使用同一当前 encoder/decoder 和不同 entry kind 表达两次提交，不新增 payload revision 或业务 DTO

### Requirement: Tool result preparation is pure and finalized after durability

每个 Provider SHALL 从按 call index 排列、capability 与原 call 匹配的冻结 typed results 纯内存构造一个 prepared tool-output entry。构造 MUST 重新验证 Provider call ID、result status、preview bytes、数量、capability 与前一 calls 的配对，不得读取文件、网络、Session 或 UI 状态，也不得按 result codec revision 选择实现。

prepared tool-output entry SHALL 提供可复制、自验证的当前 envelope 和恰好一次 finalizer，但 MUST NOT 携带 sample usage。Runtime 只有在对应 `provider_native_commit` 成功 `Sync` 后才能 finalize；append/Sync 失败时原生 Conversation 不得把 output 当作已提交历史。

#### Scenario: Prepare heterogeneous outputs without I/O
- **WHEN** Runtime 提供一组已 durable 的 Glob、Grep 与 Read results
- **THEN** Provider 在纯内存中产生按原 call index 匹配的 tool-output envelope，且不重新执行或访问文件系统

#### Scenario: Prepare outputs without I/O
- **WHEN** Runtime 提供一组已 durable 的当前 typed results
- **THEN** Provider 只执行纯内存验证与编码，不读取文件、网络、Session 或 UI 状态

#### Scenario: Reject reordered results
- **WHEN** Runtime 提供的 result 顺序、capability 或 Provider call ID 与前一 sample 不匹配
- **THEN** preparation 失败且不产生可持久化 envelope 或 finalizer

#### Scenario: Finalize outputs after Sync
- **WHEN** tool-output native commit 成功 durable
- **THEN** finalizer 恰好一次把 outputs 加入 Conversation history
- **THEN** durable 失败或重复 finalize 不会修改历史
