# provider/native-history-persistence Specification

## Purpose

定义 Provider-owned 的原生历史持久化与恢复边界，使 Anthropic Messages 和 OpenAI Responses 在进程重启后仍能无损续写，同时避免 Session、Runtime 或 UI 解析具体 wire 或从语义投影反向构造请求。

## Requirements

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

#### Scenario: Do not infer future tool execution from native history
- **WHEN** Provider 恢复一个原生 tool call sample
- **THEN** Provider codec 只恢复原生 item，不据此判定工具已执行或生成合成结果
- **THEN** Runtime 依据 Session required ledger 决定继续、复用或标记不确定

#### Scenario: Reject a mismatched tool output
- **WHEN** tool-output entry的call ID、类型、数量或顺序与前一sample的未闭合calls不匹配
- **THEN** Provider 恢复整体失败且不调用 executor或网络

#### Scenario: Reject an invalid continuation sample
- **WHEN** 首个sample缺少输入，或无输入sample没有紧跟已闭合tool outputs
- **THEN** Provider恢复整体失败且不构造下一请求

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

### Requirement: Native history exposes a private context footprint

每个受支持 Provider Conversation SHALL 从已成功提交的原生历史生成只读、强类型的上下文 footprint。footprint MUST 表示下一次请求会重放的已提交原生内容总估算，包括可见文本以及 Anthropic thinking/signature/redacted thinking、OpenAI reasoning/encrypted content 和受控未知原生项；不得计入 usage、response ID、transport metadata 或尚未提交的 streaming staging。

footprint SHALL 只暴露 Provider family、可恢复等价的单调 committed revision、版本化 estimator method、`estimated` 或 `unknown` 状态及 token 数，不得暴露原生 item、正文、signature、encrypted bytes、raw JSON 或可逆摘要。空历史 revision SHALL 为零；每个成功 finalized native commit SHALL 恰好推进一次 revision，失败或 staging sample 不得推进。读取 footprint MUST 是无网络、文件、数据库和进程副作用的内存操作，不得修改原生历史或后续请求编译结果。

#### Scenario: Estimate committed opaque Anthropic history

- **WHEN** Anthropic 已提交历史包含文本、thinking/signature 和 redacted thinking blocks
- **THEN** footprint 估算下一次 Messages 请求会重放的全部相关原生内容
- **THEN** 返回值不包含 block 正文、signature 或 redacted data

#### Scenario: Estimate committed encrypted OpenAI history

- **WHEN** OpenAI 已提交历史包含 message、reasoning summary、encrypted content 和受支持未知 item
- **THEN** footprint 估算下一次 Responses 请求会重放的全部相关原生内容
- **THEN** 返回值不包含 item、summary 正文、encrypted bytes 或 raw JSON

#### Scenario: Exclude an active staging turn

- **WHEN** Conversation 正在归并一个尚未成功 durable finalize 的 sample
- **THEN** footprint 与该 sample 开始前一致
- **THEN** 当前用户输入和部分 assistant 输出不进入已提交历史 footprint

#### Scenario: Advance revision only after native commit

- **WHEN** 一个 prepared sample 已完成但尚未 durable finalize，随后成功 finalize
- **THEN** finalize 前 footprint revision 不变，finalize 后恰好增加一次
- **THEN** 重复或失败 finalize 不会再次推进 revision

#### Scenario: Preserve request compilation after measuring

- **WHEN** 对同一 Conversation 读取一次或多次 footprint 后编译下一轮请求
- **THEN** 请求的原生 item 顺序、canonical bytes 和 cache fingerprint 与未读取 footprint 时完全相同

### Requirement: Restored native footprint matches uninterrupted history

对于相同 Provider 配置和相同已提交 native commits，从 Session 恢复的 Conversation SHALL 产生与未退出进程的 Conversation 相同 family、committed revision、estimator method、估算状态和 token 数。恢复路径 MUST 从已验证的 Provider-native history 重新计算 footprint，不得从 `SemanticHistoryView`、RuntimeEvent、normalized usage 或 Session 错误摘要反推 opaque 内容。

#### Scenario: Compare uninterrupted and restored Anthropic footprints

- **WHEN** Anthropic 会话持久化包含 opaque thinking 的 commits 并从相同 commits 恢复
- **THEN** restored 与 uninterrupted footprint 完全相同

#### Scenario: Compare uninterrupted and restored OpenAI footprints

- **WHEN** OpenAI 会话持久化包含 encrypted reasoning 的 commits 并从相同 commits 恢复
- **THEN** restored 与 uninterrupted footprint 完全相同

#### Scenario: Reject corrupt history before footprint use

- **WHEN** 任一 native commit 无法通过既有事务性恢复校验
- **THEN** Provider 不返回可供 footprint 读取的部分 Conversation
- **THEN** Runtime 不会使用部分历史继续规划或发起网络请求

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
