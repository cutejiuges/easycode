# provider/native-history-persistence Specification

## Purpose

定义 Provider-owned 的原生历史持久化与恢复边界，使 Anthropic Messages 和 OpenAI Responses 在进程重启后仍能无损续写，同时避免 Session、Runtime 或 UI 解析具体 wire 或从语义投影反向构造请求。

## Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持 Provider SHALL 将一个已校验的 native history 增量编码为受控 opaque envelope，至少标识 Provider family、wire、payload revision 和 payload。只有对应 Provider 的 codec 可以解释其 payload；Session、Runtime、TUI 和其他共享消费者 MUST NOT 依赖具体 Provider wire 类型。

当前文本切片 SHALL 把一次已完成 sample 的输入与输出作为一个原子 native history 增量，而不是把整个 turn 作为编码单元。公共接口 MUST NOT 固化“每个 commit 都同时包含输入和输出”：后续 ToolWireCodec 必须能在工具结果 durable 后、下一次 Provider 请求前提交 input-only 原生增量。codec MUST 使用确定性 JSON 规则，并 SHALL 对 payload 大小、commit shape 和 revision 执行显式校验。

#### Scenario: Encode an OpenAI sample commit

- **WHEN** OpenAI Responses sample 收到有效 `response.completed`
- **THEN** OpenAI codec 产生包含当前用户原生 item 和按 wire 顺序排列的完成 output items 的 OpenAI envelope

#### Scenario: Encode an Anthropic sample commit

- **WHEN** Anthropic Messages sample 收到有效 `message_stop`
- **THEN** Anthropic codec 产生包含当前用户 message、完整 assistant message 和最终 metadata 的 Anthropic envelope

#### Scenario: Keep shared layers opaque

- **WHEN** Session writer 持久化任一 Provider envelope
- **THEN** 它只校验公共 envelope 和有界 JSON，不解析、转换或投影 Provider payload

#### Scenario: Reserve an input-only native commit

- **WHEN** 后续工具循环需要在下一次模型请求前把已完成 tool result 加入 Provider native history
- **THEN** Provider codec 可以通过新的 payload revision/shape 编码该有序增量
- **THEN** Session 与 Runtime 的公共 envelope 不需要新增 Provider wire 字段

### Requirement: Native round-trip preserves Provider-private semantics

OpenAI codec MUST 无损保留 Responses item 顺序、message role/content、phase、reasoning summary、encrypted content、未知原生 item 和受控 raw JSON。Anthropic codec MUST 无损保留 message/content-block 顺序、thinking、signature、redacted thinking、stop metadata、usage 的 known/unknown 状态、未知 block 和受控 raw JSON。

编码后再解码 MUST 产生与原提交等价且不共享可变 buffer 的原生历史。codec 不得把缺失 usage 当作已知零，不得把 opaque reasoning 暴露到共享语义字段。

#### Scenario: Round-trip Anthropic opaque thinking

- **WHEN** Anthropic 原生提交包含 thinking/signature、redacted thinking、文本和未知 raw block
- **THEN** 持久化并恢复后的内容、顺序和 opaque bytes 与原提交等价

#### Scenario: Round-trip OpenAI encrypted reasoning

- **WHEN** OpenAI 原生提交包含 reasoning summary、encrypted content、phase、文本和未知 raw item
- **THEN** 持久化并恢复后的内容、顺序和 opaque bytes 与原提交等价

#### Scenario: Keep missing usage unknown

- **WHEN** Provider 原生提交没有返回某个 usage 字段
- **THEN** round-trip 后该字段仍为 unknown，而不是已知零或 not-applicable

### Requirement: Restore validates all native commits transactionally

恢复会话前，对应 Provider SHALL 只接收 Session 语义回放计划中已通过 lifecycle 校验的 `provider_native_commit`，并按 seq 校验其 family、wire、payload revision、commit shape、结构、角色/边界和原生不变量。只有所有待恢复提交均有效时才能创建可用 Conversation；任一损坏、不匹配或不受支持的提交 MUST 使恢复整体失败，不得加载前缀后继续网络请求。

同一 thread 中的多个 native commits SHALL 按持久化顺序恢复。Provider MUST NOT 从 `SemanticHistoryView`、RuntimeEvent、TUI transcript 或错误摘要补全缺失原生数据，也不得仅根据原生 tool call/result 推断共享工具副作用、静默插入合成工具结果或删除原生 item 来掩盖非法恢复状态。

#### Scenario: Restore ordered commits

- **WHEN** Session 包含同一 Provider 的多个合法 sample commits
- **THEN** 恢复后的 native history 与原提交顺序一致，并可供下一次请求直接使用

#### Scenario: Reject a mixed Provider history

- **WHEN** Anthropic thread 中出现 OpenAI envelope，或 family 正确但 wire/revision 不受支持
- **THEN** 恢复在创建可调用 Conversation 前失败且不发起网络请求

#### Scenario: Reject one corrupt commit atomically

- **WHEN** 多个提交中的任意一个缺少必需边界、角色或 opaque 数据结构损坏
- **THEN** Provider 不返回包含部分历史的 Conversation
- **THEN** 错误不包含原生 payload 正文

#### Scenario: Do not infer future tool execution from native history

- **WHEN** 后续工具版本恢复一个包含原生 tool call 的 commit
- **THEN** Provider codec 只恢复原生 item，不据此判定工具已执行或生成合成 tool result
- **THEN** 工具是否可重放由 Session 中独立的 required ledger records 决定

### Requirement: Restored requests match uninterrupted requests

对相同 Provider 配置、相同已提交历史和相同下一条用户输入，从 JSONL 恢复后的 RequestCompiler 输出 MUST 与未退出进程的 Conversation 输出保持相同的 Provider-native item 顺序、canonical request bytes 和 cache fingerprint。恢复不得生成新的 Provider item ID、签名、encrypted content 或其他 wire 字段来替代持久化值。

#### Scenario: Continue a restored OpenAI conversation

- **WHEN** OpenAI 会话持久化多个 commits、重启恢复并提交相同的下一条输入
- **THEN** 下一次 Responses request 与未重启路径的 input 顺序、canonical bytes 和 fingerprint 完全相同

#### Scenario: Continue a restored Anthropic conversation

- **WHEN** Anthropic 会话持久化多个 commits、重启恢复并提交相同的下一条输入
- **THEN** 下一次 Messages request 与未重启路径的 messages 顺序、canonical bytes 和 fingerprint 完全相同

### Requirement: Restored semantic projection remains derived and lossy

恢复成功后的 Conversation SHALL 通过既有 `HistoryProjector` 从已恢复 native history 重新生成语义视图。该视图 MUST 与进程退出前的可见文本语义一致，但仍不得包含 signature、encrypted reasoning、usage、未知扩展或其他 Provider-private 数据，也不得成为恢复或后续请求的输入。

#### Scenario: Project a restored conversation

- **WHEN** 任一 Provider 从 JSONL 原生 commits 恢复 Conversation 并读取历史投影
- **THEN** 投影的 turn 边界与可见用户/assistant 文本和退出前一致
- **THEN** opaque Provider 数据只保留在 native history 中

#### Scenario: Mutate a restored projection

- **WHEN** 调用方修改恢复后返回的语义视图再提交下一轮
- **THEN** Provider request 继续使用未被修改的 restored native history

### Requirement: Prepared native commits are non-zero and self-validating

prepared sample SHALL 只能由通过公共字段和有界 JSON 校验的 native commit envelope 创建，并持有不共享可变 payload buffer 的副本。复制或读取 envelope MUST 显式返回校验失败，绝不能把非法状态转换为“零值 envelope + nil error”。finalizer SHALL 仅在 durable success 后执行一次；无效或重复 finalize MUST 返回错误且不修改 committed native history。

#### Scenario: Clone a valid native envelope

- **WHEN** 调用方复制一个合法 native commit envelope 并修改任一返回的 payload bytes
- **THEN** 原 envelope、prepared sample 和其他副本的 payload 保持不变

#### Scenario: Read an invalid prepared sample

- **WHEN** 调用方读取 nil、零值或内部 envelope 非法的 prepared sample
- **THEN** 操作返回明确错误而不是成功返回零值 envelope

#### Scenario: Finalize exactly once after durability

- **WHEN** sample 对应 Session batch 已成功 Sync 且 Runtime 首次调用 finalizer
- **THEN** native history 恰好提交该增量一次
- **THEN** 任何后续 finalize 调用返回错误且不重复提交

### Requirement: Prepared samples couple native history with normalized usage

每个成功 prepared sample SHALL 同时包含一个可独立复制并重新校验的 Provider-native commit envelope 和一个不可变 normalized sample usage。两者 MUST 来自同一个已完成 Provider response；Runtime 不得接受缺少任一部分、已经 finalized、复制失败或 normalized usage 非法的 prepared sample。

Normalized usage 属于共享计量事实，不得嵌入公共 opaque envelope 后再由 Session 或 Runtime 解析。Provider-native raw usage SHALL 留在相应 Provider payload 中；normalized usage 不得替代 raw usage，也不得参与 native history 请求编译。

#### Scenario: Read a complete prepared sample

- **WHEN** Provider 完成一个合法 sample 并创建 prepared sample
- **THEN** Runtime 可取得互不共享可变 buffer 的 native envelope 和 normalized usage 副本
- **THEN** 两个事实对应同一个 Provider completion

#### Scenario: Reject a prepared sample without usage

- **WHEN** completed terminal 携带的 prepared sample 没有合法 normalized usage
- **THEN** Runtime 在 Session append 前以 stream protocol failure 收口
- **THEN** native finalizer 不执行且成功事实不进入 journal

### Requirement: Native usage round-trip preserves Provider facts

OpenAI native commit SHALL 在当前 payload v1 中保存 completed response 中受支持 raw usage 字段及其字段级已知/未知状态；Anthropic native commit SHALL 继续在 payload v1 中保存最终 message raw usage。两个 Provider 的 codec 都 MUST 在 encode/decode/restore round-trip 后保持 raw usage 数值、缺失状态和 Provider 字段含义等价，且不得因为存在 normalized usage 而删除或重写 raw usage。本变更不得为 usage 引入新的 native payload revision 或双版本 decoder。

#### Scenario: Round-trip OpenAI raw usage

- **WHEN** OpenAI native commit 包含部分已知、部分缺失的 Responses usage
- **THEN** 持久化并恢复后的 raw usage 数值和字段缺失状态与完成响应等价
- **THEN** commit 继续使用唯一的 OpenAI native payload v1

#### Scenario: Round-trip Anthropic raw usage

- **WHEN** Anthropic native commit 包含最终 message usage
- **THEN** 持久化并恢复后仍保留相同数值以及 known/unknown 状态

#### Scenario: Keep request compilation independent from normalized usage

- **WHEN** 相同 native history 分别与不同的宿主 usage 投影组合
- **THEN** 下一次 Provider request 的 native items、canonical bytes 和 fingerprint 不发生变化
