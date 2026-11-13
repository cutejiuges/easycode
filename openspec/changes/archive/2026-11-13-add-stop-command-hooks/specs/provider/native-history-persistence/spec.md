## MODIFIED Requirements

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

## ADDED Requirements

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
