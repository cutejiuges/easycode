## MODIFIED Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持Provider SHALL将已校验的native history增量编码为受控opaque envelope，至少标识Provider family、wire、payload，不包含 payload_version 或同义格式标记。只有对应Provider的唯一当前codec可以解释其payload；Session、Runtime、Tool executor、TUI和其他共享消费者 MUST NOT依赖具体Provider wire类型。

每个 Provider SHALL 只维护一种当前 sealed native history entry 与一套 encoder/decoder。强类型 kind 为 user_inputs、sample、tool_outputs。user_inputs 保存有序原生用户消息及输入身份关联，不携带 usage；sample 只保存按 wire 顺序排列的完整模型输出及 raw usage，不含 input 字段；tool_outputs 保存按原 call index 排列的原生结果且不携带 usage。Anthropic Patch 使用 tool_result，OpenAI Patch 使用匹配 custom call 的 custom_tool_call_output，不得转成 function call。初始与中途输入都必须独立提交，禁止依附 sample 成功保存。

Anthropic entry SHALL保留原生message/content block与metadata；OpenAI entry SHALL保留原生input/output items，包括custom tool call/output。两家可以共享提交生命周期接口，但 MUST NOT共享或互转wire DTO。业务类型、构造器与decoder名称 MUST NOT使用版本后缀；MUST NOT 添加 payload canary、固定版本门禁或历史 reader。codec MUST对payload大小、commit shape、call/output wire kind与pairing、capability结果数量和原生item顺序执行显式验证。

#### Scenario: Round-trip a heterogeneous output entry
- **WHEN** 一个sample按顺序包含Glob、Patch、Read calls，且Runtime提供匹配的冻结results
- **THEN** 对应Provider当前codec往返后保留相同call IDs、wire kinds、结果顺序、preview bytes和原生output shape

#### Scenario: Encode an OpenAI sample commit
- **WHEN** OpenAI Responses完成一个包含消息、reasoning、function call与custom apply_patch call的sample
- **THEN** OpenAI当前codec按原生item顺序编码并可逐字段恢复该sample

#### Scenario: Encode an Anthropic sample commit
- **WHEN** Anthropic Messages完成一个包含text、thinking与Edit tool_use的message
- **THEN** Anthropic当前codec按原生content block顺序编码并可逐字段恢复该sample

#### Scenario: Keep shared layers opaque
- **WHEN** Session、Runtime或Tool层接收一个已校验native envelope
- **THEN** 共享层只保存和传递envelope，不解析、转换或重建Provider payload

#### Scenario: Reserve an input-only native commit
- **WHEN** 当前 Provider history 需要保存首轮或中途真实用户输入
- **THEN** 当前模型使用 user_inputs entry；sample 不复制或伪造输入

#### Scenario: Encode tool outputs in the current history schema
- **WHEN** Runtime提交按call index冻结的function、custom与Anthropic tool results
- **THEN** 当前codec以`tool_outputs` entry编码结果，不引入另一个payload schema

#### Scenario: Keep one current payload codec
- **WHEN** 当前codec编解码user_inputs、sample 与 tool_outputs 三种 entry kind
- **THEN** 三者共享一套无格式版本字段的当前 codec，生产代码不存在历史reader或版本分派

#### Scenario: Preserve Provider separation
- **WHEN** Anthropic Edit与OpenAI apply_patch表达语义相同的patch
- **THEN** 两家分别使用自身native DTO与codec，不互转opaque payload

#### Scenario: Reject an unexpected payload version field
- **WHEN** native envelope 或封闭 payload 包含非法附加 payload_version 字段
- **THEN** restore在返回部分Conversation或发起网络请求前失败，且不尝试旧decoder

#### Scenario: Keep one codec for both entry kinds
- **WHEN** Conversation先提交含calls的sample再提交tool outputs
- **THEN** Provider使用同一当前encoder/decoder和不同entry kind表达两次提交，不新增payload revision或业务DTO

native/checkpoint envelope 和封闭 payload MUST 不携带格式版本字段；未知字段、缺失必需字段和旧 sample input 字段失败关闭。恢复 SHALL 接收经过验证的真实 turn identity、期望提交角色和有序 InputIDs，并与 opaque entry 的 kind、身份及顺序交叉校验；任一错配必须事务性拒绝全部恢复，不允许共享层解析 wire。

#### Scenario: Reject mismatched input associations
- **WHEN** 外部输入事实与 native entry 的 ID、顺序或角色不一致
- **THEN** 恢复整体失败且没有部分 Conversation、repair 或网络副作用

普通 user_inputs/sample/tool_outputs 继续使用三种原生增量。压缩 SHALL 使用独立 Provider-owned checkpoint envelope，由 compaction_completed 保存，不作为普通 sample 或 provider_native_commit。checkpoint 以及失败计量 provenance 同样采用唯一当前无格式版本字段的封闭 typed codec 和严格校验；共享层只验证 metadata，不解析 Provider wire。

#### Scenario: Keep checkpoint separate from ordinary commits
- **WHEN** 一个 thread 完成手动压缩
- **THEN** checkpoint 不占用普通 native commit ordinal，不伪造 user_inputs/sample/tool_outputs 或 InputID

### Requirement: Restore validates all native commits transactionally

恢复会话前，对应Provider SHALL只接收Session语义回放计划中已通过lifecycle校验的`provider_native_commit`和压缩 checkpoint，并按seq校验其family、wire、commit shape、结构、角色/边界和原生不变量。只有所有待恢复提交均有效时才能创建可用Conversation；任一损坏、不匹配或不受支持的提交 MUST使恢复整体失败，不得加载前缀后继续网络请求。

同一thread中的user-input、sample 与 tool-output entries SHALL按持久化顺序恢复。Provider MUST验证每组原生outputs与前一未闭合tool calls的wire kind、call ID、capability、数量和顺序匹配，并验证首个 sample 前存在 initial user_inputs、continuation sample 位于已闭合 tool outputs 及可选 steering user_inputs 之后；全部 sample 不含 input 字段；孤立output、function/custom错配、重复output、跨Provider output、遗漏call或非法sample边界 MUST使恢复整体失败。Provider不得从`SemanticHistoryView`、RuntimeEvent、TUI transcript、approval fact或错误摘要补全缺失原生数据，也不得仅根据原生call/output推断共享工具副作用；执行、批准与重放决策只来自独立Session ledger。

#### Scenario: Restore ordered commits
- **WHEN** Session包含function/custom sample call entry、对应tool-output entry和后续continuation sample entry
- **THEN** 恢复后的native history与原提交顺序和wire kinds一致，并可供下一次请求直接使用

#### Scenario: Reject a mixed Provider history
- **WHEN** Anthropic thread中出现OpenAI envelope，或family正确但wire 不受支持或字段结构不合法
- **THEN** 恢复在创建可调用Conversation前失败且不发起网络请求

#### Scenario: Reject one corrupt commit atomically
- **WHEN** 多个提交中的任意一个缺少必需边界、角色或opaque数据结构损坏
- **THEN** Provider不返回包含部分历史的Conversation，且错误不包含原生payload正文

#### Scenario: Do not infer future tool execution from native history
- **WHEN** Provider恢复一个原生patch tool call sample
- **THEN** Provider codec只恢复原生item，不据此判定工具已获批准、已执行或生成合成结果
- **THEN** Runtime依据Session required ledger决定补偿、复用或标记不确定

#### Scenario: Reject a mismatched tool output
- **WHEN** tool-output entry的call ID、wire kind、capability、数量或顺序与前一sample未闭合calls不匹配
- **THEN** Provider恢复整体失败且不调用executor或网络

#### Scenario: Reject an invalid continuation sample
- **WHEN** 首个 sample 缺少 initial user_inputs，或后续 sample 前工具组未闭合，或 steering 关联非法
- **THEN** Provider恢复整体失败且不构造下一请求

恢复 SHALL 按 Session seq 交错验证普通增量和 checkpoint；checkpoint 必须匹配此前 committed/context revision、前驱或 root、有效原生内容 digest、family/wire/model。完整旧原生历史仍验证工具配对和 opaque 结构；checkpoint 不得掩盖被覆盖区域损坏。最新 checkpoint 非法时 MUST 整体拒绝，不回退旧 checkpoint，不调用网络。

#### Scenario: Reject a stale checkpoint source
- **WHEN** checksum 合法但 checkpoint 引用了不匹配的前驱、窗口 digest 或提交位置
- **THEN** 整体恢复失败，无部分 Conversation、补偿写入或 Provider 请求

#### Scenario: Validate archived opaque records
- **WHEN** 最新 checkpoint 完整但较早已覆盖的原生 tool pair 或 opaque entry 非法
- **THEN** 恢复失败，不因该内容已不在模型窗口而跳过验证

### Requirement: Native usage round-trip preserves Provider facts

OpenAI native commit SHALL 在当前无格式版本字段的 payload 中保存 completed response 中受支持 raw usage 字段及其字段级已知/未知状态；Anthropic native commit SHALL 继续在无格式版本字段的 payload 中保存最终 message raw usage。两个 Provider 的 codec 都 MUST 在 encode/decode/restore round-trip 后保持 raw usage 数值、缺失状态和 Provider 字段含义等价，且不得因为存在 normalized usage 而删除或重写 raw usage。usage SHALL 沿用唯一当前 sample 契约，不引入双版本 decoder；user_inputs 不得伪造 usage。

#### Scenario: Round-trip OpenAI raw usage

- **WHEN** OpenAI native commit 包含部分已知、部分缺失的 Responses usage
- **THEN** 持久化并恢复后的 raw usage 数值和字段缺失状态与完成响应等价
- **THEN** commit 继续使用唯一的无格式版本字段 OpenAI native payload 契约

#### Scenario: Round-trip Anthropic raw usage

- **WHEN** Anthropic native commit 包含最终 message usage
- **THEN** 持久化并恢复后仍保留相同数值以及 known/unknown 状态

#### Scenario: Keep request compilation independent from normalized usage

- **WHEN** 相同 native history 分别与不同的宿主 usage 投影组合
- **THEN** 下一次 Provider request 的 native items、canonical bytes 和 fingerprint 不发生变化

### Requirement: Prepared checkpoints own immutable provenance and one resolution

Provider checkpoint SHALL 绑定独立 CompactionID、原始来源 revisions/前驱/digest、family/wire/model、有界原生摘要编码与生成响应 provenance，保存 raw usage；normalized usage 独立暴露给共享层。summary wrapper MUST 由 Provider 一次确定性生成并持久化，恢复不得按新版 prompt 重新包装。shared API MUST NOT 接受 any、任意 Message map 或可变全局 codec。准备、读取、编码、验证与 finalize/discard MUST 纯内存；finalize 仅在成功 Sync 后恰好一次执行，失败 discard 不改变当前窗口。checkpoint getter 和投影必须深拷贝。

#### Scenario: Freeze candidate bytes
- **WHEN** 调用方修改 prepared checkpoint getter 返回的 bytes 或 summary 投影
- **THEN** 原 checkpoint、请求 bytes、provenance 和后续恢复结果不变

#### Scenario: Resolve once after durability
- **WHEN** finalize/discard 或重复 resolution 竞争
- **THEN** 只执行一个合法 resolution，成功 Sync 后窗口只切换一次，cleanup 不早于 resolution 完成

#### Scenario: Keep native summary provenance out of the request
- **WHEN** 摘要响应包含合法 opaque reasoning 与 raw usage
- **THEN** provenance 保真保存，后续请求只发送摘要上下文和新增原生历史，不重放本次生成响应的 reasoning/usage
