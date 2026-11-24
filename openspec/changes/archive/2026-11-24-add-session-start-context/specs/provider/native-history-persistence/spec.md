## MODIFIED Requirements

### Requirement: Each Provider owns its native commit codec

每个受支持Provider SHALL将已校验的native history增量编码为受控opaque envelope，至少标识Provider family、wire、payload，不包含 payload_version 或同义格式标记。只有对应Provider的唯一当前codec可以解释其payload；Session、Runtime、Tool executor、TUI和其他共享消费者 MUST NOT依赖具体Provider wire类型。

每个 Provider SHALL 只维护一种当前 sealed native history entry 与一套 encoder/decoder。强类型 kind 为 user_inputs、sample、tool_outputs、hook_feedback、session_start_context。user_inputs 保存有序原生用户消息及输入身份关联，不携带 usage；sample 只保存按 wire 顺序排列的完整模型输出及 raw usage，不含 input 字段；tool_outputs 保存按原 call index 排列的原生结果且不携带 usage。Anthropic Patch 使用 tool_result，OpenAI Patch 使用匹配 custom call 的 custom_tool_call_output，不得转成 function call。初始与中途输入都必须在 sample 前提交，真实用户输入保持独立的 user_inputs 原生条目。

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
- **WHEN** 当前 codec 编解码 user_inputs、sample、tool_outputs、hook_feedback 和 session_start_context 五种 entry kind
- **THEN** 这些种类共享一套无格式版本字段的当前 codec，生产代码不存在历史reader或版本分派

#### Scenario: Preserve Provider separation
- **WHEN** Anthropic Edit与OpenAI apply_patch表达语义相同的patch
- **THEN** 两家分别使用自身native DTO与codec，不互转opaque payload

#### Scenario: Reject an unexpected payload version field
- **WHEN** native envelope 或封闭 payload 包含非法附加 payload_version 字段
- **THEN** restore在返回部分Conversation或发起网络请求前失败，且不尝试旧decoder

#### Scenario: Keep one codec for both entry kinds
- **WHEN** Conversation先提交含calls的sample再提交tool outputs
- **THEN** Provider使用同一当前encoder/decoder和不同entry kind表达两次提交，不新增payload revision或业务DTO

native/checkpoint envelope 和封闭 payload MUST 不携带格式版本字段；未知字段、缺失必需字段和旧 sample input 字段失败关闭。恢复 SHALL 按增量种类接收已经验证的真实作用域、期望提交角色与关联证据。真实用户输入使用 turn identity 和有序 InputIDs，SessionStart 使用闭合事件的来源证据。对应证据必须与 opaque entry 的 kind、身份及顺序交叉校验；任一错配必须事务性拒绝全部恢复，不允许共享层解析 wire。

#### Scenario: Reject mismatched input associations
- **WHEN** 外部输入事实与 native entry 的 ID、顺序或角色不一致
- **THEN** 恢复整体失败且没有部分 Conversation、repair 或网络副作用

user_inputs、sample、tool_outputs、hook_feedback 和 session_start_context 使用同一当前原生增量 codec。压缩 SHALL 使用独立 Provider-owned checkpoint envelope，由 compaction_completed 保存，不作为普通 sample 或 provider_native_commit。checkpoint 以及失败计量 provenance 同样采用唯一当前无格式版本字段的封闭 typed codec 和严格校验；共享层只验证 metadata，不解析 Provider wire。

#### Scenario: Keep checkpoint separate from ordinary commits
- **WHEN** 一个 thread 完成手动压缩
- **THEN** checkpoint 不占用普通 native commit ordinal，不伪造 user_inputs/sample/tool_outputs 或 InputID

hook_feedback SHALL 只保存已授权 Stop 反馈的独立 native user-level 文本及强类型来源，禁止 sample usage、真实用户 InputID、tool call/output 或 system/developer 权限。当前 restore role 另含 hook_feedback，并与此前唯一 continued 冻结决定的来源/最终字节交叉验证；不得依赖正文标签猜测角色。

#### Scenario: Encode a separate hook feedback entry
- **WHEN** 一个合法 Stop 阻断产生已授权冻结模型文本
- **THEN** 两家各自编码 hook_feedback，保持单一当前 codec，无假调用/InputID/usage 或共享 wire DTO

session_start_context SHALL 编码按事件声明授权的有界规范正文、封闭来源和可校验关联。Anthropic Messages 使用角色为 user 的原生文本消息，OpenAI Responses 使用角色为 user 的原生 input_text item。两家都 SHALL 保留实际送给模型的规范数据包装、正文及顺序。关联所需的私有 metadata 随 native payload 持久化。该 kind MUST NOT 携带 InputID、助手 sample、usage、Stop 反馈、工具调用或输出，也不得成为 system/developer 指令、项目指令或工具授权。

session_start_context 的作用域 SHALL 封闭区分初始输入关联与已安装 checkpoint 关联。startup/resume 绑定当次真实 TurnID，紧邻其 initial user_inputs 之前。compact 绑定成功 CompactionID、scope、checkpoint sequence 和已验证的 checkpoint 来源，手动压缩没有 TurnID，自动压缩只使用 scope 中的真实所属 turn 和目标 sample。上下文的 native envelope 由已应用的 session_start_hook_closed 保存，该 closure 的 seq 是该增量的真实持久化位置。初始真实输入仍由独立 native user_inputs 保存。压缩摘要的 checkpoint envelope 与 usage 事实保持独立。

#### Scenario: Encode authorized initial context for both Providers
- **WHEN** 同一 turn 的首个真实输入与通过信任和披露校验的 startup 或 resume 上下文准备提交
- **THEN** 各 Provider 用自己的当前 codec 编码上下文和真实输入两个原生条目，原生请求保留上下文在真实输入之前的顺序。真实 InputID 只关联原输入

#### Scenario: Encode checkpoint context without inventing a turn
- **WHEN** 手动压缩安装合法新窗口且已应用 compact 上下文
- **THEN** 两家各自编码独立 native 增量，使用实际 CompactionID 和 checkpoint 关联。新窗口按原生摘要、上下文、之后增量的顺序编译

### Requirement: Restore validates all native commits transactionally

恢复会话前，对应Provider SHALL只接收 Session 语义回放计划中已通过 lifecycle 校验的原生增量和压缩 checkpoint，并按seq校验其family、wire、commit shape、结构、角色/边界和原生不变量。只有所有待恢复提交均有效时才能创建可用Conversation；任一损坏、不匹配或不受支持的提交 MUST使恢复整体失败，不得加载前缀后继续网络请求。

同一 thread 中的 user-input、sample、tool-output、hook-feedback 和 session-start-context entries SHALL按持久化顺序恢复。Provider MUST验证每组原生outputs与前一未闭合tool calls的wire kind、call ID、capability、数量和顺序匹配，并验证首个 sample 前存在 initial user_inputs、工具 continuation sample 位于已闭合 tool outputs 及可选 steering user_inputs 之后，Stop continuation sample 则位于合法无 calls sample 及已闭合 continued/hook_feedback 之后；全部 sample 不含 input 字段；孤立output、function/custom错配、重复output、跨Provider output、遗漏call或非法sample边界 MUST使恢复整体失败。Provider不得从`SemanticHistoryView`、RuntimeEvent、TUI transcript、approval fact或错误摘要补全缺失原生数据，也不得仅根据原生call/output推断共享工具副作用；执行、批准与重放决策只来自独立Session ledger。

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

线上恢复和离线投影 SHALL 以 Session 验证的已应用关闭事实为 session_start_context 提供强类型来源证据。解码 SHALL 对账真实作用域、声明引用及 ordinal、输出披露资格、总量、完整规范包装字节及有序内容 digest，并拒绝角色与证据缺失、错配或重复。恢复 MUST 逐字节保留已接纳的模型正文，不根据展示文字重新编码，也不读取脚本或新配置。新的显式会话激活在独立的操作边界决定当前 resume Hook。checkpoint 覆盖范围内的旧上下文仍须通过完整校验。

startup/resume 的上下文与同批真实 initial user_inputs 构成完整边界，原生位置严格相邻且同属真实 turn。上下文 SHALL 由 [session_start_hook_closed(applied), turn_inputs_committed(initial), provider_native_commit(user_inputs)] 的第一个事实提供。compact 上下文 SHALL 由成功 checkpoint 和适用 PostCompact 收口之后的 singleton session_start_hook_closed(applied) 提供，且为该 checkpoint 后首个原生增量。Provider MUST 验证它对应刚安装的已验证 checkpoint、保留真实作用域且不掩盖未闭合工具调用。compact 增量仅追加原生文本，保持采样资格、工具配对、steering 资格与 Stop 反馈的真实前驱不变。两家普通采样 MUST 使用同一已验证逻辑前驱确定可采样状态、真实 TurnID 及样本下标，并将它们用于实际流与样本持久化。拥有原生正文的物理尾项不得覆盖这些值。手动压缩后的上下文自身不产生下一次采样资格。

#### Scenario: Reject forged SessionStart evidence before repair
- **WHEN** 已应用关闭事实带有合法外层 checksum，而原生角色、来源、作用域、顺序、内容或 digest 与授权证据不一致
- **THEN** 恢复和离线预览拒绝整个 journal，零 repair、网络或 Hook 重跑

#### Scenario: Validate applied context placement
- **WHEN** startup 或 resume 上下文缺少同批 initial 输入，或者 compact 上下文引用旧 checkpoint、错误作用域、重复交付或出现在另一个原生增量之后
- **THEN** 拒绝整个 history，既不打开部分 Conversation，也不生成缺失条目

#### Scenario: Keep the real predecessor after automatic compaction
- **WHEN** 已闭合工具结果或合法待交付 Stop 反馈的 turn 安装新窗口并保存 compact 上下文
- **THEN** 已验证的 compact 原生条目位于窗口摘要之后。工具续写即使没有 steering 仍按原工具结果进入下一真实样本，且保持真实 TurnID 和样本下标。Stop 续写仍依据真实 sample 及其独立验证的反馈交付

### Requirement: Restored requests match uninterrupted requests

对相同Provider配置、相同Tool Catalog snapshot、相同已提交历史、相同下一条用户输入、相同本次实际送入候选的 SessionStart 上下文和相同项目指令快照，从JSONL恢复后的RequestCompiler输出 MUST与未退出进程的Conversation输出保持相同Provider-native item顺序、tool definition顺序与wire kind、canonical request bytes和cache fingerprint。恢复不得生成新的Provider item/call ID、签名、encrypted content或其他wire字段来替代持久化值。

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

已经保存的 SessionStart 上下文 SHALL 随同一有效窗口原样恢复。显式激活后经信任审查产生的合法 resume 上下文，只有在实际提交后才改变请求尾部及对应 fingerprint。查看历史、拒绝或跳过新检查不产生新的原生正文。OpenAI WebSocket 瞬态资格仍沿既有 durable prefix、恢复和压缩重置规则判定。恢复和压缩后的首个普通 WS 请求 SHALL 全量发出且无 previous_response_id，之后只对已证明可复用的连接与前缀发送准确 suffix。完整逻辑请求用于 canonical 等价比较。HTTP 回退使用同一已提交内容，不重跑 Hook。

#### Scenario: Rebuild a continuation with saved SessionStart context
- **WHEN** 两家 Provider 分别保存已应用的 startup/resume 上下文与真实输入，以及成功 checkpoint 后的 compact 上下文，随后以等价的实际新输入和操作属性继续
- **THEN** 同一逻辑请求在不中断和恢复路径的原生顺序、canonical bytes、有效窗口 revision 和 cache fingerprint 相同。离线恢复期间脚本和网络次数为零

#### Scenario: Attribute new resume disclosure to a new native suffix
- **WHEN** 同一已保存会话重新激活，并有一份新批准且成功提交的 resume 上下文
- **THEN** 旧原生 bytes 保持相同，新上下文只在确切的首个真实输入之前出现一次，完整请求随实际新增内容变化

### Requirement: Prepared commits require an explicit complete lifecycle

user-input、sample、tool-output、hook-feedback 与 session-start-context prepared commit SHALL 各有唯一当前构造契约，调用方必须显式提供成功提交与未提交丢弃行为。生产接口 MUST NOT 通过旧构造入口自动补入空清理行为；测试也必须使用相同当前契约。构造保持纯内存，无效 envelope、usage 或缺失生命周期行为在 durable 写入前失败。

每个 prepared commit SHALL 恰好选择 finalize 或 discard 之一，二者互斥且各自最多执行一次。成功 Sync 后才 finalize；未 durable 的失败路径 discard 并释放 staging。重复或竞争 resolution 不得再次执行回调；回调完成前 owner 不得报告清理完成。sample 保留 normalized usage，user inputs、tool outputs、hook feedback 和 SessionStart 上下文不携带 sample usage。

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

startup/resume SHALL 在同一 Conversation admission 下准备上下文及真实初始输入，基于同一候选快照预检实际模型请求，由唯一 owner 负责收口。完整批次成功 Sync 后以有界内存操作原子 finalize 两个原生条目，原生和上下文 revision 随实际增量推进。compact SHALL 在 checkpoint 已 Sync 并安装且适用 PostCompact 收口之后，使用同一 Conversation admission 骨架准备一个新窗口增量。关闭事实成功 Sync 后才发布该增量。该增量不改写已安装摘要、任务状态或使用量。后续候选和宿主投影使用实际有效窗口的新 revision。

#### Scenario: Discard refused context before a writer accepts it
- **WHEN** 规范上下文候选预检触发确定的预算或编码上限，或在候选提交接收前取消
- **THEN** 唯一准备 owner 恰好一次 discard，已提交 native history 保持原字节，已经安装的 checkpoint 保留

#### Scenario: Await the confirmed combined Sync result
- **WHEN** 带上下文的首轮提交已被 writer 接收，提交期间收到取消
- **THEN** owner 等待提交结果，确认 Sync 成功时原子 finalize 全部原生条目一次。不可确定时不发新请求、不重试或拆分该批次
