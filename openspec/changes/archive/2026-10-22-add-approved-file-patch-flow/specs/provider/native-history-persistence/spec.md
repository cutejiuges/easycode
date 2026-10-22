## MODIFIED Requirements

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

