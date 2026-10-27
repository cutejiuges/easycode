# provider/native-history-persistence Specification

## Purpose

定义 Provider-owned 的原生历史持久化与恢复边界，使 Anthropic Messages 和 OpenAI Responses 在进程重启后仍能无损续写，同时避免 Session、Runtime 或 UI 解析具体 wire 或从语义投影反向构造请求。

## Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持Provider SHALL将已校验的native history增量编码为受控opaque envelope，至少标识Provider family、wire、单一当前payload canary和payload。只有对应Provider的唯一当前codec可以解释其payload；Session、Runtime、Tool executor、TUI和其他共享消费者 MUST NOT依赖具体Provider wire类型。

每个Provider SHALL只维护一种当前sealed native history entry模型和一套当前encoder/decoder。entry使用强类型kind区分`sample`与`tool_outputs`：sample保存可选当前输入、按wire顺序排列的完整输出及raw usage；tool outputs保存按原call index排列的Read、Glob、Grep、Patch原生结果输入且不携带usage。Anthropic Patch结果使用`tool_result`，OpenAI Patch结果使用与原custom call匹配的`custom_tool_call_output`；不得把custom call转换为function call。首个sample必须具有真实用户输入，紧跟合法tool outputs的continuation sample必须不含伪造用户输入；含tool calls的assistant输出仍属于sample entry。

Anthropic entry SHALL保留原生message/content block与metadata；OpenAI entry SHALL保留原生input/output items，包括custom tool call/output。两家可以共享提交生命周期接口，但 MUST NOT共享或互转wire DTO。业务类型、构造器与decoder名称 MUST NOT使用版本后缀；payload canary只做精确当前边界校验，MUST NOT路由旧reader。codec MUST对payload大小、commit shape、call/output wire kind与pairing、capability结果数量和原生item顺序执行显式验证。

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
- **WHEN** 当前Provider history需要表达只含真实用户输入的首个sample
- **THEN** 当前entry模型表达该合法shape，且continuation sample不伪造用户输入

#### Scenario: Encode tool outputs in the current history schema
- **WHEN** Runtime提交按call index冻结的function、custom与Anthropic tool results
- **THEN** 当前codec以`tool_outputs` entry编码结果，不引入另一个payload schema

#### Scenario: Keep one current payload codec
- **WHEN** 当前codec编解码sample与tool_outputs两种entry kind
- **THEN** 两者共享一个精确当前canary，生产代码不存在历史reader或版本分派

#### Scenario: Preserve Provider separation
- **WHEN** Anthropic Edit与OpenAI apply_patch表达语义相同的patch
- **THEN** 两家分别使用自身native DTO与codec，不互转opaque payload

#### Scenario: Reject a stale payload canary
- **WHEN** native envelope的payload canary不等于当前Provider codec常量
- **THEN** restore在返回部分Conversation或发起网络请求前失败，且不尝试旧decoder

#### Scenario: Keep one codec for both entry kinds
- **WHEN** Conversation先提交含calls的sample再提交tool outputs
- **THEN** Provider使用同一当前encoder/decoder和不同entry kind表达两次提交，不新增payload revision或业务DTO

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

恢复会话前，对应Provider SHALL只接收Session语义回放计划中已通过lifecycle校验的`provider_native_commit`，并按seq校验其family、wire、payload revision、commit shape、结构、角色/边界和原生不变量。只有所有待恢复提交均有效时才能创建可用Conversation；任一损坏、不匹配或不受支持的提交 MUST使恢复整体失败，不得加载前缀后继续网络请求。

同一thread中的sample与tool-output entries SHALL按持久化顺序恢复。Provider MUST验证每组原生outputs与前一未闭合tool calls的wire kind、call ID、capability、数量和顺序匹配，并验证首个sample有输入、continuation sample只紧跟已闭合tool outputs且没有伪造输入；孤立output、function/custom错配、重复output、跨Provider output、遗漏call或非法sample边界 MUST使恢复整体失败。Provider不得从`SemanticHistoryView`、RuntimeEvent、TUI transcript、approval fact或错误摘要补全缺失原生数据，也不得仅根据原生call/output推断共享工具副作用；执行、批准与重放决策只来自独立Session ledger。

#### Scenario: Restore ordered commits
- **WHEN** Session包含function/custom sample call entry、对应tool-output entry和后续continuation sample entry
- **THEN** 恢复后的native history与原提交顺序和wire kinds一致，并可供下一次请求直接使用

#### Scenario: Reject a mixed Provider history
- **WHEN** Anthropic thread中出现OpenAI envelope，或family正确但wire/revision不受支持
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
- **WHEN** 首个sample缺少输入，或无输入sample没有紧跟已闭合tool outputs
- **THEN** Provider恢复整体失败且不构造下一请求

### Requirement: Restored requests match uninterrupted requests

对相同Provider配置、相同Tool Catalog snapshot、相同已提交历史、相同下一条用户输入和相同项目指令快照，从JSONL恢复后的RequestCompiler输出 MUST与未退出进程的Conversation输出保持相同Provider-native item顺序、tool definition顺序与wire kind、canonical request bytes和cache fingerprint。恢复不得生成新的Provider item/call ID、签名、encrypted content或其他wire字段来替代持久化值。

项目指令上下文与Tool Catalog SHALL在请求时从当前不可变快照重新生成，MUST NOT编码进native commit。快照变化时，已恢复native history及其revision、item顺序和opaque bytes MUST保持不变；只有重新生成的外部上下文、请求bytes和对应fingerprint可发生内容派生变化。进程内approval grant不得从native history恢复，也不得影响catalog fingerprint。

#### Scenario: Continue a restored OpenAI conversation
- **WHEN** OpenAI会话持久化function/custom call/output commits并以相同catalog和项目指令快照恢复
- **THEN** 下一次Responses request与未重启路径的input、tools、canonical bytes和fingerprint完全相同

#### Scenario: Continue a restored Anthropic conversation
- **WHEN** Anthropic会话持久化tool call/output commits并以相同catalog和项目指令快照恢复
- **THEN** 下一次Messages request与未重启路径的messages、tools、canonical bytes和fingerprint完全相同

#### Scenario: Resume after project instructions change
- **WHEN** 相同native commits在新进程中恢复，但项目指令或Tool Catalog snapshot发生变化
- **THEN** restored native history的revision、原生item顺序和opaque bytes完全不变
- **THEN** 下一请求变化只能归因于当前外部snapshot，并产生对应fingerprint变化

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

每个Provider SHALL从按call index排列、capability、Provider call ID与原call wire kind匹配的冻结typed results纯内存构造一个prepared tool-output entry。构造 MUST重新验证result status、preview bytes、数量、capability及function/custom/tool_use pairing，不得读取文件、网络、Session、approval registry或UI状态，也不得按result codec revision选择实现。

prepared tool-output entry SHALL提供可复制、自验证的当前envelope和恰好一次finalizer，但 MUST NOT携带sample usage。Runtime只有在对应`provider_native_commit`成功`Sync`后才能finalize；append/Sync失败时原生Conversation不得把output当作已提交history。

#### Scenario: Prepare heterogeneous outputs without I/O
- **WHEN** Runtime提供一组已durable的Glob、Patch与Read results
- **THEN** Provider在纯内存中产生按原call index和wire kind匹配的tool-output envelope，且不重新执行或访问文件系统

#### Scenario: Prepare outputs without I/O
- **WHEN** Runtime提供一组已durable的当前typed results
- **THEN** Provider只执行纯内存验证与编码，不读取文件、网络、Session或UI状态

#### Scenario: Reject reordered results
- **WHEN** Runtime提供的result顺序、capability、Provider call ID或wire kind与前一sample不匹配
- **THEN** preparation失败且不产生可持久化envelope或finalizer

#### Scenario: Finalize outputs after Sync
- **WHEN** tool-output native commit成功durable
- **THEN** finalizer恰好一次把outputs加入Conversation history
- **THEN** durable失败或重复finalize不会修改history

### Requirement: Prepared commits require an explicit complete lifecycle

sample 与 tool-output prepared commit SHALL 各有唯一当前构造契约，调用方必须显式提供成功提交与未提交丢弃行为。生产接口 MUST NOT 通过旧构造入口自动补入空清理行为；测试也必须使用相同当前契约。构造保持纯内存，无效 envelope、usage 或缺失生命周期行为在 durable 写入前失败。

每个 prepared commit SHALL 恰好选择 finalize 或 discard 之一，二者互斥且各自最多执行一次。成功 Sync 后才 finalize；未 durable 的失败路径 discard 并释放 staging。重复或竞争 resolution 不得再次执行回调；回调完成前 owner 不得报告清理完成。sample 保留 normalized usage，tool outputs 不携带 sample usage。

#### Scenario: Reject an incomplete lifecycle
- **WHEN** 构造 prepared commit 缺少 finalize 或 discard 行为
- **THEN** 构造失败，生产代码没有自动补空行为的旧入口

#### Scenario: Discard an uncommitted sample
- **WHEN** sample 无法完成 durable 提交
- **THEN** staging 丢弃恰好一次，committed history、revision 和下一请求不包含该 sample

#### Scenario: Resolve competing lifecycle operations
- **WHEN** finalize、discard 或重复 resolution 竞争同一个 prepared commit
- **THEN** 只有一个合法状态转换执行一次对应回调，其他操作返回明确错误
- **THEN** 不重复提交 history，也不在回调未完成时释放其 owner 的资源

#### Scenario: Keep restored requests equivalent
- **WHEN** 两家 Provider 分别完成合法 sample/tool-output 提交，并从相同 journal 恢复
- **THEN** restored 与 uninterrupted 路径的原生 item 顺序、opaque bytes、下一请求 canonical bytes 和 fingerprint 一致
