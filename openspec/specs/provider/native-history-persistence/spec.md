# provider/native-history-persistence Specification

## Purpose

定义 Provider-owned 的原生历史持久化与恢复边界，使 Anthropic Messages 和 OpenAI Responses 在进程重启后仍能无损续写，同时避免 Session、Runtime 或 UI 解析具体 wire 或从语义投影反向构造请求。

## Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持Provider SHALL将已校验的native history增量编码为受控opaque envelope，至少标识Provider family、wire、payload，不包含 payload_version 或同义格式标记。只有对应Provider的唯一当前codec可以解释其payload；Session、Runtime、Tool executor、TUI和其他共享消费者 MUST NOT依赖具体Provider wire类型。

每个 Provider SHALL 只维护一种当前 sealed native history entry 与一套 encoder/decoder。强类型 kind 为 user_inputs、sample、tool_outputs、hook_feedback。user_inputs 保存有序原生用户消息及输入身份关联，不携带 usage；sample 只保存按 wire 顺序排列的完整模型输出及 raw usage，不含 input 字段；tool_outputs 保存按原 call index 排列的原生结果且不携带 usage。Anthropic Patch 使用 tool_result，OpenAI Patch 使用匹配 custom call 的 custom_tool_call_output，不得转成 function call。初始与中途输入都必须独立提交，禁止依附 sample 成功保存。

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
- **WHEN** 当前 codec 编解码 user_inputs、sample、tool_outputs 与 hook_feedback 四种 entry kind
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

普通 user_inputs/sample/tool_outputs/hook_feedback 使用四种当前原生增量。压缩 SHALL 使用独立 Provider-owned checkpoint envelope，由 compaction_completed 保存，不作为普通 sample 或 provider_native_commit。checkpoint 以及失败计量 provenance 同样采用唯一当前无格式版本字段的封闭 typed codec 和严格校验；共享层只验证 metadata，不解析 Provider wire。

#### Scenario: Keep checkpoint separate from ordinary commits
- **WHEN** 一个 thread 完成手动压缩
- **THEN** checkpoint 不占用普通 native commit ordinal，不伪造 user_inputs/sample/tool_outputs 或 InputID

hook_feedback SHALL 只保存已授权 Stop 反馈的独立 native user-level 文本及强类型来源，禁止 sample usage、真实用户 InputID、tool call/output 或 system/developer 权限。当前 restore role 另含 hook_feedback，并与此前唯一 continued 冻结决定的来源/最终字节交叉验证；不得依赖正文标签猜测角色。

#### Scenario: Encode a separate hook feedback entry
- **WHEN** 一个合法 Stop 阻断产生已授权冻结模型文本
- **THEN** 两家各自编码 hook_feedback，保持单一当前 codec，无假调用/InputID/usage 或共享 wire DTO

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

恢复会话前，对应Provider SHALL只接收Session语义回放计划中已通过lifecycle校验的`provider_native_commit`和压缩 checkpoint，并按seq校验其family、wire、commit shape、结构、角色/边界和原生不变量。只有所有待恢复提交均有效时才能创建可用Conversation；任一损坏、不匹配或不受支持的提交 MUST使恢复整体失败，不得加载前缀后继续网络请求。

同一thread中的user-input、sample、tool-output 与 hook-feedback entries SHALL按持久化顺序恢复。Provider MUST验证每组原生outputs与前一未闭合tool calls的wire kind、call ID、capability、数量和顺序匹配，并验证首个 sample 前存在 initial user_inputs、工具 continuation sample 位于已闭合 tool outputs 及可选 steering user_inputs 之后，Stop continuation sample 则位于合法无 calls sample 及已闭合 continued/hook_feedback 之后；全部 sample 不含 input 字段；孤立output、function/custom错配、重复output、跨Provider output、遗漏call或非法sample边界 MUST使恢复整体失败。Provider不得从`SemanticHistoryView`、RuntimeEvent、TUI transcript、approval fact或错误摘要补全缺失原生数据，也不得仅根据原生call/output推断共享工具副作用；执行、批准与重放决策只来自独立Session ledger。

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

hook_feedback 的 native 角色、真实 turn/source sample、plan/run/声明关联及模型正文 SHALL 与 Session 已验证 Stop 决定逐字节一致。错来源、未经 model 授权、超过两次、存在未闭合 calls 或无匹配决定必须事务性拒绝；checkpoint 仍需验证被覆盖的完整历史，包括该反馈。

#### Scenario: Reject a forged hook input
- **WHEN** native hook_feedback 的来源、角色或正文与 Stop 决定不一致，即使 checksum 正确
- **THEN** 完整恢复失败，零 repair/部分 Conversation/网络或重新编码猜测

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

恢复等价性 SHALL 覆盖 checkpoint 后零个/多个增量及连续多次压缩。模型请求使用最新合法原生摘要窗口和后续增量，完整历史仍可独立查询；恢复不得重新生成摘要或 wrapper。checkpoint 来源 digest 不包含当前项目指令/工具目录，当前快照只在请求时注入。

#### Scenario: Restore a compacted continuation
- **WHEN** 两家 Provider 分别从同一成功 checkpoint 和后续工具闭环恢复
- **THEN** 与 uninterrupted 路径的下一请求 canonical bytes、原生顺序、context revision 和 fingerprint 相同
- **THEN** 生成摘要网络次数为零，工具执行次数为零

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

### Requirement: Native history exposes a private context footprint

每个受支持 Provider Conversation SHALL 从已成功提交的原生历史生成只读、强类型的上下文 footprint。footprint MUST 表示下一次请求会重放的已提交原生内容总估算，包括可见文本以及 Anthropic thinking/signature/redacted thinking、OpenAI reasoning/encrypted content 和受控未知原生项；不得计入 usage、response ID、transport metadata 或尚未提交的 streaming staging。

footprint SHALL 只暴露 Provider family、可恢复等价的单调 context revision、版本化 estimator method、`estimated` 或 `unknown` 状态及 token 数，不得暴露原生 item、正文、signature、encrypted bytes、raw JSON 或可逆摘要。空历史且无 checkpoint 时 context revision SHALL 为零；每个成功 finalized native commit 或 checkpoint SHALL 恰好推进一次 context revision，失败或 staging sample 不得推进。读取 footprint MUST 是无网络、文件、数据库和进程副作用的内存操作，不得修改原生历史或后续请求编译结果。

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
- **THEN** 已独立提交的当前用户输入仍计入 footprint，未提交的部分 assistant 输出不计入

#### Scenario: Advance revision only after native commit

- **WHEN** 一个 prepared sample 已完成但尚未 durable finalize，随后成功 finalize
- **THEN** finalize 前 footprint revision 不变，finalize 后恰好增加一次
- **THEN** 重复或失败 finalize 不会再次推进 revision

#### Scenario: Preserve request compilation after measuring

- **WHEN** 对同一 Conversation 读取一次或多次 footprint 后编译下一轮请求
- **THEN** 请求的原生 item 顺序、canonical bytes 和 cache fingerprint 与未读取 footprint 时完全相同

footprint SHALL 仅覆盖当前有效原生窗口；完整 native committed revision 与 context revision 是不同概念。checkpoint 推进 context revision 而不增加普通 native commit 数；不得将归档全文估算与窗口 footprint 合并。

#### Scenario: Advance only the context revision on compaction
- **WHEN** 一个有效 checkpoint 成功 Sync 并 finalize
- **THEN** context revision 恰好推进一次，普通 native committed revision 和原 item 顺序不变，footprint 只计算新窗口

### Requirement: Restored native footprint matches uninterrupted history

对于相同 Provider 配置和相同已提交 native commits 和 checkpoints，从 Session 恢复的 Conversation SHALL 产生与未退出进程的 Conversation 相同 family、context revision、estimator method、估算状态和 token 数。恢复路径 MUST 从已验证的 Provider-native history 重新计算 footprint，不得从 `SemanticHistoryView`、RuntimeEvent、normalized usage 或 Session 错误摘要反推 opaque 内容。

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

每个Provider SHALL从按call index排列、capability、Provider call ID与原call wire kind匹配的强类型模型输出值（冻结原结果及独立冻结反馈）纯内存构造一个prepared tool-output entry。构造 MUST重新验证result status、preview bytes、数量、capability及function/custom/tool_use pairing，不得读取文件、网络、Session、approval registry或UI状态，也不得按result codec revision选择实现。

prepared tool-output entry SHALL提供可复制、自验证的当前envelope和恰好一次finalizer，但 MUST NOT携带sample usage。Runtime只有在对应`provider_native_commit`成功`Sync`后才能finalize；append/Sync失败时原生Conversation不得把output当作已提交history。


PostToolUse 反馈 SHALL 只在同调用已完整收口且通过披露/预算/身份校验后组合到对应 output 正文，保留原 status/code、is_error、call ID、function/custom/tool_result kind 和顺序。无匹配或全部 feedback=none 时 MUST 保持既有 output bytes 完全相同；model 模式空反馈只省略反馈段，原 preview 可因预留而缩短，编译只复用已经冻结的预览，不重新扩展。非空反馈为工具来源数据，不创建额外 call、InputID 或 system/developer 消息。tool-output restore 证据 SHALL 包含同组已验证结果/后置收口/反馈，使用同一编码规则核验存储正文，禁止根据正文标签猜测或从展示投影反建请求。

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

#### Scenario: Encode feedback for native tool facades
- **WHEN** Anthropic Edit/Write/Bash 或 OpenAI custom apply_patch/function Write/exec_command 具有合法冻结反馈
- **THEN** 只有对应 output 正文增加规范反馈段，原成功、wire kind、配对和 item 顺序不变

#### Scenario: Keep empty feedback byte identical
- **WHEN** 无 Post 计划或 feedback=none 且没有模型反馈
- **THEN** 原生 output 与旧路径逐字节相同，不附加空数组或标签

#### Scenario: Reject forged stored feedback
- **WHEN** native 正文含有与 result/post terminal/冻结反馈证据冲突的内容、顺序或身份
- **THEN** 整体恢复在 repair 或任何外部副作用前失败

#### Scenario: Preserve request and cache behavior
- **WHEN** 相同结果及反馈分别通过不中断、恢复与可选 WebSocket 路径继续
- **THEN** 完整逻辑 canonical request、原生顺序、opaque bytes 和内容 fingerprint 等价；未调用时 Hook 配置变化不污染稳定前缀

#### Scenario: Preserve reserved output when an enabled hook returns nothing
- **WHEN** model 计划使真实结果预览缩短，而 Hook 返回空对象
- **THEN** output 保持该次已冻结预览且没有空反馈包装，恢复 bytes 等于该次相同事实的不中断路径，不与无 Hook 的较大预览错误比较

### Requirement: Prepared commits require an explicit complete lifecycle

user-input、sample、tool-output 与 hook-feedback prepared commit SHALL 各有唯一当前构造契约，调用方必须显式提供成功提交与未提交丢弃行为。生产接口 MUST NOT 通过旧构造入口自动补入空清理行为；测试也必须使用相同当前契约。构造保持纯内存，无效 envelope、usage 或缺失生命周期行为在 durable 写入前失败。

每个 prepared commit SHALL 恰好选择 finalize 或 discard 之一，二者互斥且各自最多执行一次。成功 Sync 后才 finalize；未 durable 的失败路径 discard 并释放 staging。重复或竞争 resolution 不得再次执行回调；回调完成前 owner 不得报告清理完成。sample 保留 normalized usage，user inputs、tool outputs 和 hook feedback 不携带 sample usage。

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
- **WHEN** 两家 Provider 分别完成合法 user-input/sample/tool-output 提交，并从相同 journal 恢复
- **THEN** restored 与 uninterrupted 路径的原生 item 顺序、opaque bytes、下一请求 canonical bytes 和 fingerprint 一致

用户输入准备 SHALL 是纯内存操作，返回独立只读的输入关联及 envelope；只有语义与生命周期相同的私有逻辑可以复用，不得把用户输入编码为工具结果。

Stop 续轮 SHALL 具有独立 typed 准备契约，封闭区分反馈单独交付和反馈+真实 steering；两种来源/角色不混合，不能把 Hook 变成 UserInput 或工具结果。只复用语义/生命周期相同的纯内存 resolution 与候选预检骨架。continued 决定先冻结最终模型字节，预备对象只有在后续一/三记录交付 batch Sync 后 finalize 一次；未提交失败 discard 一次，不能自动补空清理行为。复合 finalize 是有界原子内存提交；native revision 按实际一/两个 entries 推进，context revision 另叠加既有 checkpoint 计数，不新增并行 pending owner。

#### Scenario: Reject feedback before durability
- **WHEN** 冻结反馈的 append/Sync 失败或其 prepared 对象非法
- **THEN** 不推进 committed revision、不发后续请求；未提交 staging 被明确丢弃，不自动重跑脚本

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

### Requirement: Cache and transport state never replace native restoration

Session/native/checkpoint SHALL 继续只保存唯一当前原生内容、计量与已有事实；不得新增缓存格式版本、历史 reader、可选兼容 record、连接 token、cache_control、派生 key 或可执行续写游标。服务端缓存与 WebSocket 内存状态 MUST NOT 成为恢复依据；native response ID 的原有保留不代表恢复连接续写资格。恢复 SHALL 保持既有 lease、完整验证后 repair、Sync 和不重跑副作用规则。

相同 ThreadID、配置、项目指令、catalog 和有效原生窗口下，uninterrupted 与 restored 的完整逻辑请求 canonical bytes、完整请求 fingerprint、内容分段 fingerprint 以及 native item 顺序 MUST 全部相同。HTTP/SSE 和 WebSocket full create 的各自 wire 派生也 SHALL 分别逐字节相同。连接有效的 incremental wire 与恢复后的 full wire MUST 显式区分：只有在完整请求字节级等价已独立验证后，才能进一步证明前次请求 input 加 durable 原生 outputs 加本次 suffix 精确组成当前完整 input；不得用有损语义文本等价替代完整请求字节比较，也不得宣称不同传输帧相同。

#### Scenario: Restore an enabled Anthropic cache
- **WHEN** 同一当前固定 fixture 在相同 ephemeral_5m 或 ephemeral_1h 配置下恢复
- **THEN** 断点位置、完整 request canonical bytes、原生顺序和 fingerprint 与不中断路径一致
- **THEN** fixture 原有 bytes 不被缓存逻辑或恢复过程重写

#### Scenario: Compare full and incremental OpenAI paths
- **WHEN** 同一历史的连续连接选择 incremental，而恢复连接选择 full
- **THEN** 两条路径独立编译的完整请求 bytes 和 fingerprint 完全相同
- **THEN** 实际 wire 分别匹配 full/incremental golden，原生前缀与 suffix 可精确重组，不伪造 wire bytes 相等

#### Scenario: Keep recovery independent of remote cache eviction
- **WHEN** 服务端缓存已过期或进程重启使 WebSocket 状态丢失
- **THEN** 本地恢复仍成功，第一条实际请求完整携带有效原生上下文，恢复阶段不联网

### Requirement: Native Skill content is frozen and restored without source I/O

两家 Provider SHALL 以唯一当前 user_inputs 契约保存原始用户正文和独立 Skill 附件的关联，原生请求按原文、带来源标记的附件顺序编码，保持同一真实 InputID。附件包装和展开正文在首次提交时冻结，不能以 system 权限消息或虚构工具调用持久化。tool_outputs SHALL 保存 Skill 工具已冻结的完整结果。strict decoder 必须校验附件字段、数量、关联、上限及原生内容一致性；不得引入旧 reader、格式版本或旁路 activation record。

同 turn 辅助资源资格 SHALL 由已验证且 durable 的显式附件派生，不能新增权限记录或凭恢复启动工具。

恢复 MUST 只消费已提交原生/结果事实，不读取 SKILL.md 或资源、不重新替换参数、不执行工具。result 已 durable 而 output 未提交时按 call index 补齐；started 无结果时沿用 outcome_uncertain 补偿。相同当前目录与配置下，uninterrupted/restored 的完整 canonical request、native item 顺序、opaque bytes、context footprint 和 fingerprints MUST 相同；当前目录变更只影响未来目录上下文，不能重写历史附件。

插件 Skill SHALL 使用相同当前附件/结果结构，严格校验 plugin scope、importer kind、插件 ID、限定名称 namespace 与复合 revision 的关联；来源 revision 绑定主文档与适用副文件状态，但恢复不重新计算来源文件摘要。导入不额外保存宿主路径、manifest/sidecar 原文、启用状态或安装记录，也不新增副文件格式 reader；后来明确的合法 resource 读取仍按现有冻结工具结果保存。离线恢复和 output 缺口补偿 MUST 不访问旧插件根、manifest、SKILL.md 或资源，也不从附件重新启用插件。当前来源快照只用于未来调用，在旧事实 reconciliation 后加载。

相同当前 SkillCatalog 下，插件 Skill 的 uninterrupted/restored 完整请求及 fingerprint 必须等价；若本次不再配置插件或 metadata 改变，只允许当前目录上下文与对应请求前缀发生可解释变化，历史原生附件/output、opaque bytes 和顺序仍须相同，不宣称变化后的完整请求与旧目录相等。

#### Scenario: Restore after the skill file disappears
- **WHEN** 含附件的输入或 Skill 工具结果已 durable，文件随后删除
- **THEN** 历史正文仍逐字节相同；恢复没有 Skill 文件读取，工具不会重跑

#### Scenario: Repair the output gap
- **WHEN** Skill result 已 durable 但 native output 未提交
- **THEN** 仅用冻结 result 补齐匹配输出，原生顺序和请求 bytes 与未中断路径一致

#### Scenario: Reject a forged attachment
- **WHEN** native 附件关联、字段或内容与当前编码约束不一致
- **THEN** 整体恢复失败，不返回部分会话且不 repair 合法前缀之外的语义错误

#### Scenario: Compact without reactivating from disk
- **WHEN** 已加载 Skill 正文进入已压缩历史区间
- **THEN** 续写按既有 checkpoint 使用摘要，不从文件重新注入正文或恢复永久授权

#### Scenario: Restore frozen plugin instructions after removing the plugin
- **WHEN** 插件显式附件或 Skill result 已 Sync，插件目录随后删除且本次没有 plugin-dir
- **THEN** 历史正文与原生 output 逐字节保留，恢复不读取或重新启用旧来源，未来目录不含该插件

#### Scenario: Reject a forged plugin source before repair
- **WHEN** checksum 合法但插件 source/name/revision 关联非法或与原生附件正文不一致
- **THEN** 整体恢复失败，原 journal bytes 不变，不返回部分 Conversation 或读取来源修补

#### Scenario: Compare plugin requests with an identical current catalog
- **WHEN** 两家分别从含插件附件、工具结果和 opaque reasoning 的固定 current bytes 恢复，并使用相同当前目录快照
- **THEN** 下一完整 canonical request、native item 顺序、opaque bytes、footprint 和内容/请求 fingerprint 均与不中断路径一致

#### Scenario: Restore after a sidecar policy or presence changes
- **WHEN** 含 Codex 调用策略的输入/工具结果已 Sync，此后副文件改变、消失或只在本次未来来源出现
- **THEN** 恢复及输出缺口补偿不读取副文件、不重新推导策略或复合摘要，原附件/原生 output bytes 保持，新目录仅由本次明确来源决定

#### Scenario: Preserve an explicitly read policy resource as ordinary frozen text
- **WHEN** 已准入的合法资源读取把 agents/openai.yaml 正文作为普通 Skill result 提交后恢复
- **THEN** 直接使用已冻结结果，不再加载该文件、不从结果正文恢复授权，也不创建新 record kind 或旁路 metadata 记录

### Requirement: Committed question answers restore byte for byte

两家 Provider SHALL 在现有 sample/tool_outputs 当前契约内保存提问调用和冻结回答，不添加 entry kind、格式版本、游标、特殊 user_inputs 或额外可选 record。恢复必须验证 call ID、capability、wire kind、题目和回答关联、顺序及完整 preview，任何错配整体失败且不 repair 或返回部分 Conversation。

相同 catalog、项目指令、配置与已提交事实下，提问完成或 result/output 缺口补偿后的 uninterrupted/restored 下一完整请求 canonical bytes、原生 item 顺序、context footprint/revision 与内容/请求 fingerprint MUST 相同。Anthropic signature/redacted thinking、OpenAI reasoning/encrypted content/phase 原样保留。恢复不能重新生成回答、读取终端或依赖请求曾被 UI 展示。

#### Scenario: Restore a mixed native call group
- **WHEN** Read、Question、Patch results 已 durable，而 native outputs 尚未提交
- **THEN** 恢复按原 call index 用冻结 bytes 补齐同一组 outputs，无交互、工具和网络执行

#### Scenario: Compare uninterrupted and restored requests
- **WHEN** 两家分别恢复含提问及 opaque reasoning 的固定 current fixture
- **THEN** 下一完整请求 bytes、原生顺序、footprint 和 fingerprints 与不中断路径相同

#### Scenario: Reject a forged answer association
- **WHEN** checksum 合法但 question result 与 ready/native 的题目或 call identity 不匹配
- **THEN** 完整恢复失败，journal bytes 保持不变且无交互副作用

### Requirement: Stop feedback preserves native continuation and cache equivalence

Stop 反馈 SHALL 以 user-level 检查数据及确定性来源包装进入相应 Provider 原生历史，不覆盖真实用户约束、不授予工具权限。请求只能由自身 native history 编译；共享语义视图或 RuntimeEvent 不得反向构造原生反馈。只有实际交付 finalize 才推进 native/context revision：反馈 entry 增加一，适用时真实 user_inputs entry 再增加一，checkpoint 计数仍独立；决定/claim 不推进 native revision。footprint 覆盖实际反馈及真实输入，规范计量不伪造 sample usage。

同一配置、catalog、项目/Skill 快照及同一已补偿历史下，uninterrupted/restored 的完整 canonical request、native 次序、opaque bytes、footprint/revision 和内容/请求 fingerprint SHALL 等价；Anthropic marker/TTL 与 OpenAI key、full/incremental wire 派生分别验证。只改配置/信任而无反馈事实不得污染稳定前缀；恢复不读配置、脚本、artifact 或重新生成 wrapper。

#### Scenario: Compare both native continuations
- **WHEN** 两家分别从含 Stop 反馈、工具配对与 opaque reasoning 的相同事实直接续写和恢复续写
- **THEN** 完整请求 bytes、原生次序、footprint 和 fingerprints 一致，opaque 数据未转换或丢失

#### Scenario: Keep a full request distinct from incremental wire
- **WHEN** 连续连接选择增量而恢复选择 full
- **THEN** 独立完整逻辑请求相等，各自 wire 匹配对应 golden，不宣称不同帧字节相同

### Requirement: Stop native delivery preserves pending feedback and genuine steering

Provider SHALL 将已保存 continued 决定的最终冻结文本作为交付来源，独立编码 hook_feedback；有实际 claim 时才另编码完整真实 user_inputs 和 InputAssociation，原生顺序为反馈后真实用户。候选预检必须同时计入两种来源、全部原文字节和已有 Skill 附件；重建候选不重新展开附件或读取文件。只有真实附件在交付 Sync/finalize 后按原契约产生本 turn Skill 资源资格，Hook 正文不能激活 Skill 或 Read/artifact/工具权限。

候选摘要只读取旧 committed 窗口，prepared 候选必须先 discard，最终反馈和真实输入暂不加入该摘要。重建后输出字节、InputIDs、附件、编码次序与窗口/请求 footprint 必须一致；真实输入被拒绝可重建反馈单独交付，不重做摘要。原生交付在 Session feedback_pending 状态下通过角色/来源/正文交叉验证，不按 batch 长度或文本标签猜测。

恢复允许纯内存编码尚未交付的冻结反馈，不能重新渲染、添加 recovery 文本或改变 wrapper；没有已保存的真实 user_inputs 就不能恢复 claim。相同已补偿事实下 native bytes/order、下一完整请求、opaque、footprint 与 fingerprints 必须等价，绝不重新执行或联网。

#### Scenario: Deliver both sources atomically
- **WHEN** 冻结反馈后实际领取一个合法用户纠正
- **THEN** 两个不同角色的 native entries 同批 Sync/finalize，用户原 ID/附件保持，反馈在前且不伪造 InputID；revision 按两个 entries 推进

#### Scenario: Recover only the frozen obligation
- **WHEN** continued 已保存，但没有一/三记录 native 交付
- **THEN** 只从冻结最终文本构造一条反馈，零输入恢复/渲染/脚本/网络，编码与不中断的同一事实相同

#### Scenario: Preserve an explicit Skill correction
- **WHEN** Stop 续轮领取的真实用户输入含已经准备的 Skill 附件
- **THEN** 预算/编码包含完整附件，原文和附件分别保留；资源资格只在 Sync/finalize 后生效，Hook 内容不能代替附件
