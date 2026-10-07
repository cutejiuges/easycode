## MODIFIED Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持 Provider SHALL 将一个已校验的 native history 增量编码为受控 opaque envelope，至少标识 Provider family、wire、payload revision 和 payload。只有对应 Provider 的 codec 可以解释其 payload；Session、Runtime、Tool executor、TUI 和其他共享消费者 MUST NOT 依赖具体 Provider wire 类型。

每个Provider SHALL只维护一种当前sealed native history entry模型和一套当前payload codec。entry使用强类型kind区分 `sample` 与 `tool_outputs`：sample保存可选当前输入、按wire顺序排列的完整输出及raw usage；tool outputs保存有序原生结果输入且不携带usage。首个sample必须具有真实用户输入，紧跟合法tool outputs的continuation sample必须不含伪造用户输入。含tool calls的assistant输出仍属于sample entry。

Anthropic entry SHALL保留原生message/content block与metadata；OpenAI entry SHALL保留原生input/output items。两家可以共享提交生命周期接口，但MUST NOT共享或互转wire DTO。Go业务类型、构造器与decoder名称MUST NOT使用 `V1`、`V2`、`V3` 等版本后缀；公共envelope中的单一当前payload revision只用于严格边界校验。

codec MUST 使用确定性 JSON 规则，并 SHALL 对 payload 大小、commit shape、revision、call/output pairing 和原生 item 顺序执行显式校验。

#### Scenario: Encode an OpenAI sample commit
- **WHEN** OpenAI Responses sample 收到有效 `response.completed`，其中包含文本或 function calls
- **THEN** OpenAI codec 产生包含当前用户原生 item和按 wire 顺序排列的完成 output items 的 sample envelope

#### Scenario: Encode an Anthropic sample commit
- **WHEN** Anthropic Messages sample 收到有效 `message_stop`，其中包含文本或 `tool_use` blocks
- **THEN** Anthropic codec 产生包含当前用户 message、完整 assistant message 和最终 metadata 的 sample envelope

#### Scenario: Keep shared layers opaque
- **WHEN** Session writer 持久化任一 Provider envelope
- **THEN** 它只校验公共 envelope 和有界 JSON，不解析、转换或投影 Provider payload

#### Scenario: Encode tool outputs in the current history schema
- **WHEN** 一个 sample 的全部 Read results 已 durable 且需要继续采样
- **THEN** 对应 Provider codec以当前schema的 `tool_outputs` kind编码有序原生outputs且不生成 `sample_usage`
- **THEN** 下一continuation sample使用同一schema的 `sample` kind且不伪造用户输入

#### Scenario: Keep one current payload codec
- **WHEN** Tool Loop 需要在下一 sample 前提交已完成 results
- **THEN** Provider使用同一当前encoder/decoder和不同entry kind表达结果，公共envelope无需新增Provider wire字段或payload revision

#### Scenario: Reserve an input-only native commit
- **WHEN** Tool Loop需要在下一sample前提交已完成results
- **THEN** 该既有场景名称映射到当前 `tool_outputs` entry kind，不新增payload revision、旧reader或版本后缀业务类型

### Requirement: Restore validates all native commits transactionally

恢复会话前，对应 Provider SHALL 只接收 Session 语义回放计划中已通过 lifecycle 校验的 `provider_native_commit`，并按 seq 校验其 family、wire、payload revision、commit shape、结构、角色/边界和原生不变量。只有所有待恢复提交均有效时才能创建可用 Conversation；任一损坏、不匹配或不受支持的提交 MUST 使恢复整体失败，不得加载前缀后继续网络请求。

同一 thread 中的sample与tool-output entries SHALL按持久化顺序恢复。Provider MUST验证每组原生outputs与前一未闭合tool calls的类型、call ID、数量和顺序匹配，并验证首个sample有输入、continuation sample只紧跟已闭合tool outputs且没有伪造输入；孤立output、重复output、跨Provider output、遗漏call或非法sample边界 MUST使恢复整体失败。Provider不得从 `SemanticHistoryView`、RuntimeEvent、TUI transcript或错误摘要补全缺失原生数据，也不得仅根据原生call/output推断共享工具副作用；执行与重放决策只来自独立Session ledger。

#### Scenario: Restore ordered commits
- **WHEN** Session 包含sample call entry、对应tool-output entry和后续continuation sample entry
- **THEN** 恢复后的 native history 与原提交顺序一致，并可供下一次请求直接使用

#### Scenario: Reject a mixed Provider history
- **WHEN** Anthropic thread 中出现 OpenAI envelope，或 family 正确但 wire/revision 不受支持
- **THEN** 恢复在创建可调用 Conversation 前失败且不发起网络请求

#### Scenario: Reject one corrupt commit atomically
- **WHEN** 多个提交中的任意一个缺少必需边界、角色或 opaque 数据结构损坏
- **THEN** Provider 不返回包含部分历史的 Conversation，且错误不包含原生 payload 正文

#### Scenario: Reject a mismatched tool output
- **WHEN** tool-output entry的call ID、类型、数量或顺序与前一sample的未闭合calls不匹配
- **THEN** Provider 恢复整体失败且不调用 executor或网络

#### Scenario: Reject an invalid continuation sample
- **WHEN** 首个sample缺少输入，或无输入sample没有紧跟已闭合tool outputs
- **THEN** Provider恢复整体失败且不构造下一请求

#### Scenario: Do not infer future tool execution from native history
- **WHEN** Provider 恢复一个原生 tool call sample
- **THEN** Provider codec 只恢复原生 item，不据此判定工具已执行或生成合成结果
- **THEN** Runtime 依据 Session required ledger 决定继续、复用或标记不确定

### Requirement: Restored requests match uninterrupted requests

对相同 Provider 配置、相同 Tool Catalog snapshot、相同已提交历史、相同下一条用户输入和相同项目指令快照，从 JSONL 恢复后的 RequestCompiler 输出 MUST 与未退出进程的 Conversation 输出保持相同的 Provider-native item 顺序、tool schema 顺序、canonical request bytes 和 cache fingerprint。恢复不得生成新的 Provider item/call ID、签名、encrypted content 或其他 wire 字段来替代持久化值。

项目指令上下文与 Tool Catalog SHALL 在请求时从当前不可变快照重新生成，MUST NOT 编码进 native commit。快照变化时，已恢复 native history 及其 revision、item 顺序和 opaque bytes MUST 保持不变；只有重新生成的外部上下文、请求 bytes 和对应 fingerprint 可发生内容派生的变化。

#### Scenario: Continue a restored OpenAI conversation
- **WHEN** OpenAI 会话持久化 call/output commits并以相同 catalog和项目指令快照恢复
- **THEN** 下一次 Responses request 与未重启路径的 input、tools、canonical bytes 和 fingerprint 完全相同

#### Scenario: Continue a restored Anthropic conversation
- **WHEN** Anthropic 会话持久化 call/output commits并以相同 catalog和项目指令快照恢复
- **THEN** 下一次 Messages request 与未重启路径的 messages、tools、canonical bytes 和 fingerprint 完全相同

#### Scenario: Resume after project instructions change
- **WHEN** 相同 native commits 在新进程中恢复，但项目指令或 Tool Catalog snapshot 发生变化
- **THEN** restored native history 的 revision、原生 item顺序和 opaque bytes 完全不变
- **THEN** 下一请求变化只能归因于当前外部 snapshot，并产生对应 fingerprint 变化

## ADDED Requirements

### Requirement: Tool result preparation is pure and finalized after durability

每个 Provider SHALL 从按 call index 排列的冻结 typed results 纯内存构造一个prepared tool-output entry。构造 MUST 重新验证 Provider call ID、结果状态、preview bytes、codec revision、数量与前一 calls 的配对，不得读取文件、网络、Session 或 UI 状态。

prepared tool-output entry SHALL 提供可复制、自验证的 envelope 和恰好一次 finalizer，但 MUST NOT 携带 sample usage。Runtime 只有在对应 `provider_native_commit` 成功 `Sync` 后才能 finalize；append/Sync 失败时原生 Conversation 不得把 output 当作已提交历史。

#### Scenario: Prepare outputs without I/O
- **WHEN** Runtime 提供一组已 durable 的 Read results
- **THEN** Provider 在纯内存中产生匹配的tool-output envelope，且不重新执行或读取文件

#### Scenario: Finalize outputs after Sync
- **WHEN** tool-output native commit 成功 durable
- **THEN** finalizer 恰好一次把 outputs 加入 Conversation history
- **THEN** durable 失败或重复 finalize 不会修改历史
