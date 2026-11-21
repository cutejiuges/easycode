# context/planning Specification

## Purpose

定义 Provider 请求前的确定性上下文来源、不可变计划、缓存分段、token 估算和预算判定，使共享 Runtime 能在不解析或重建 Provider 原生历史的前提下诊断上下文并阻止明确超限的请求。

## Requirements

### Requirement: Context planning uses typed ordered sources

上下文规划 SHALL 只接收强类型的 Provider profile、当前操作冻结的已声明 Tool Catalog snapshot、可选项目指令快照、可选 Skill metadata 快照、已提交有效窗口语义快照、Provider 原生历史 footprint、不携带额外正文的当前采样状态、可选任务内容 stamp 和可选预算。来源 MUST 按 `provider_profile`、`tool_catalog`、可选 `project_instructions`、可选 `skill_catalog`、`committed_history`、`current_input` 的固定顺序出现；无项目指令文档时 MUST 省略 `project_instructions`，无可向模型披露的 Skill 时 MUST 省略 `skill_catalog`。规划不得隐式读取时间戳、随机 ID、cwd、环境变量、Session 路径、git 状态、文件系统或网络。

`provider_profile` SHALL 只包含 Provider family、model、既有显式 schema revision 以及实际非默认普通请求策略的确定性身份，不得包含 API key、base URL 凭据、配置路径或其他 secret。新设置省略时 profile canonical bytes 和 source revision MUST 与既有默认基线一致；不得递增固定 revision、增加格式版本路由或从 profile 反向重建原生历史。`tool_catalog` SHALL 只包含 catalog revision、稳定排序的 facade schema canonical bytes 与内容派生 fingerprint，不得包含 executor、invocation/call identity、权限临时状态或 workspace 绝对路径。`project_instructions` SHALL 只包含经过发现契约验证的不可变快照、内容派生 revision、截断状态和项目根相对来源，不得包含绝对项目根或启动 cwd。规划 MUST 是纯内存操作，且不得修改输入快照或 Provider 原生历史。

Plan Mode 的普通采样 SHALL 在 `provider_profile` 的 canonical 内容使用由实际发送的本机固定规划指令正文派生的身份。该来源的估算 SHALL 覆盖这份正文一次。当前操作冻结的已声明目录 SHALL 同时供规划与 Provider 编译使用。普通输入预检、工具或问卷后的同回合续写以及压缩前后用于普通采样的窗口比较，均 SHALL 使用相应操作冻结的规划指令与目录。摘要请求本身沿用独立的实际 wire 估算与既有摘要契约。规划指令的正文和身份不得作为项目指令、当前输入或已提交历史另计。

#### Scenario: Plan the same inputs twice
- **WHEN** 两次规划使用相同 profile、Tool Catalog、项目指令快照、Skill metadata、有效窗口语义快照、原生 footprint、当前输入和预算
- **THEN** 两次计划的来源顺序、revision、估算、预算状态、canonical cache segments 和指纹完全一致

#### Scenario: Exclude ambient process state
- **WHEN** 规划输入不变但当前时间、cwd、环境变量、Session 文件路径或随机调用 identity 发生变化
- **THEN** 计划的 canonical bytes、分段指纹和预算判定保持不变

#### Scenario: Keep secrets out of the profile source
- **WHEN** 相同 family、model 和 Tool Catalog 使用不同 API key、workspace 绝对位置或带凭据的配置来源
- **THEN** `provider_profile`、`tool_catalog` 与稳定前缀指纹保持一致
- **THEN** 计划摘要、错误和诊断不包含 API key、URL 凭据或 workspace 绝对路径

#### Scenario: Omit an empty project source
- **WHEN** 项目指令快照不含任何选中文档且 Skill 目录为空
- **THEN** 计划保持 `provider_profile`、`tool_catalog`、`committed_history`、`current_input` 的固定顺序
- **THEN** cache segments、stable prefix fingerprint 和预算结果不因空占位而改变

输入提交前 SHALL 以已提交历史加 prepared inputs 的独立候选历史进行纯内存规划；候选语义视图仅用于估算，候选请求只能由 Provider 原生历史构造。实际网络采样前全部文本已经提交。current_input 保留为无额外正文的采样标记，不保留旧 Text/continuation 双 shape。

规划历史 SHALL 为 Provider 从有效窗口产生的只读语义快照，摘要具有独立来源类型，不伪造 user InputID/turn；完整历史只供 recall/transcript。prepared inputs 的候选预检 SHALL 同样基于有效窗口。current_input 仍无额外正文，source 顺序与稳定级别不变。

#### Scenario: Plan after a successful compaction
- **WHEN** 完整原生历史很大但成功 checkpoint 已替换有效窗口
- **THEN** committed_history 只计摘要与其后增量，完整历史不再进入覆盖估算；请求仍由原生窗口编译

Skill metadata SHALL 只包含实际发送的稳定名称、描述、调用策略与相对来源；正文、绝对路径、digest 检查的文件身份和队列状态不得进入稳定目录。

#### Scenario: Plan a skill catalog
- **WHEN** 模型可用 Skill metadata 非空
- **THEN** skill_catalog 在项目指令之后、已提交历史之前出现，其估算覆盖实际发送的目录文本

#### Scenario: Attribute a reasoning policy change
- **WHEN** 只改变实际发送的普通推理/输出属性，其他上下文来源不变
- **THEN** provider_profile 的内容派生身份、分段及稳定前缀 fingerprint 发生可解释变化，tools/project/skills/history 各自指纹不变

#### Scenario: Preserve default profile identity
- **WHEN** 所有新增字段省略，或显式 Anthropic 输出值与既有 4096 完全相同且没有其他变化
- **THEN** 相同普通请求属性得到既有 profile bytes；显示的 explicit/default 来源不参与指纹

#### Scenario: Change only the requested thinking display
- **WHEN** 仅将实际发送的 Anthropic display 从省略改为 summarized 或 omitted
- **THEN** provider_profile 和请求属性记录该明确变化，tools/project/skills/native 内容指纹不变；不能从模型名推定省略等于某个显式值

推理 raw/normalized usage SHALL 只作为完成样本或摘要的计量事实，不进入 profile、工具目录、prompt 或 stable prefix；读取计量不改变输入估算方法，也不能用累计 thinking 消耗替代当前窗口 footprint。

任务清单 SHALL 只通过已提交的对应 Provider 原生调用/结果及 checkpoint 的原生摘要包装进入有效历史；普通 sample 不再从当前列表投影重复注入清单。任务进度不新增上下文 source，固定来源顺序保持不变，current_input 仍无额外正文。压缩来源的完整 typed 任务快照只用于验证和构造有来源的 checkpoint 包装，不允许共享语义视图反向重建调用 wire。

#### Scenario: Plan a task update without an extra source
- **WHEN** TodoWrite 或 update_plan 的原生调用与结果已提交
- **THEN** 规划仍使用原固定 source 顺序，任务参数/结果由 committed_history 的原生 footprint 覆盖，不再增加 task source 或 current_input 正文

#### Scenario: Plan a task-preserving checkpoint
- **WHEN** checkpoint 原生摘要包装包含精确任务快照，而语义摘要仍为真实模型 generation
- **THEN** 规划读取同一有效窗口的语义摘要和完整原生 footprint，不把宿主清单包装伪装成模型 generation 或追加新的用户任务

任务内容 stamp SHALL 仅存在于同一 committed_history 的 canonical 元数据，不是新增 source、prompt 段、token 贡献或普通 TurnInput 附件。none 时必须省略 task_content_digest；clear/snapshot 时该字段 SHALL 是 SHA-256(CanonicalJSON({state, update})) 的 64 位小写十六进制字符串，其中 update 为纯 domain.TaskUpdate。规范内容不包含工具 facade profile、result_sequence、turn_id、invocation_id、CompactionID 或其他关联身份；不能对包含这些字段的整个 TaskInput/TaskProgressState/checkpoint payload 求 hash 代替。

普通采样、prepared 输入候选预检、摘要预检/替换候选与恢复后的规划 SHALL 分别绑定对应已验证窗口的任务状态并从它派生 stamp，不能使用尚未成功提交的候选、未来更新或错误窗口的清单。stamp 不改通用 NativeHistoryFootprint 契约，不产生网络正文，也不使已由原生历史/摘要包装覆盖的任务文字再估算一次。语义摘要仍为真实模型 generation。

#### Scenario: Bind task content metadata to each planning window
- **WHEN** 当前、输入候选、摘要候选和恢复规划分别使用某个已验证窗口
- **THEN** 每份规划的任务 stamp 来自该窗口对应状态，来源顺序和 token 估算规则不变，普通 TurnInput 与实际请求不新增任务正文

#### Scenario: Attribute a Plan Mode switch to the actual request sources
- **WHEN** 固定同一 Provider、推理设置和原生窗口，分别规划普通模式和 Plan Mode 的普通请求
- **THEN** Plan 的规划指令由 `provider_profile` 计入一次，`tool_catalog` 准确反映已声明定义，两项分别得到与实际内容对应的不同 revision 和 fingerprint，稳定前缀指纹随之变化
- **THEN** 来源顺序及项目指令、Skill 和历史内容保持一致。切换次数、界面状态和请求关联身份不进入规划内容

#### Scenario: Compare candidate windows under the captured Plan Mode sources
- **WHEN** Plan Mode 操作触发手动或自动压缩并评估继续普通采样所用的候选窗口
- **THEN** 压缩前后均用同一份冻结的规划指令和已声明目录比较普通请求窗口
- **THEN** 摘要请求的独立估算与预检对应实际摘要请求，摘要生成与原生 checkpoint 沿用既有语义

#### Scenario: Return to the same default profile identity
- **WHEN** 两次规划都处于普通模式且实际模型内容与设置相同，中间曾选中 Plan Mode
- **THEN** 普通模式不产生规划指令字段、额外正文估算或模式序号，其 canonical bytes、source revision 及稳定前缀指纹与既有普通基线相同

### Requirement: Context plans are immutable validated snapshots

上下文计划 SHALL 使用唯一当前的强类型 shape，并深拷贝所有调用方可变输入；纯内存 plan MUST NOT 携带固定 format revision 或按 revision 选择 validator。计划 MUST 拒绝未知来源 kind、重复来源、非法顺序、空 source revision、非法 Tool Catalog、非法项目指令快照、非法 Skill 目录、非法估算状态、Provider family 不匹配及无效缓存计划。所有返回 slice、bytes 或嵌套值的读取操作 MUST 返回独立副本。

计划 SHALL 把 `provider_profile` 与当前操作已声明的 `tool_catalog` 映射为 stable cache segments，把可选 `project_instructions` 映射为 project-stable cache segment，把可选 `skill_catalog` 映射为 project-stable segment，把 `committed_history` 映射为 turn-stable segment，把 `current_input` 映射为 volatile segment，并复用 `context/cache-plan` 的 canonical JSON、排序和 stable prefix fingerprint 契约。Tool Catalog 的 canonical bytes、source revision、fingerprint 与估算 SHALL 覆盖实际发送给对应 Provider 的全部 facade schema/description。普通模式使用包含 Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill、Question、Task 的完整目录，Plan Mode 依照 tool/invocation-loop 使用七项定义。Exec 动态 command、workdir、timeout、approval、sandbox 临时状态与环境实际值不得进入 catalog source。来源生命周期、source revision 与缓存稳定级别 MUST 保持为不同的强类型概念。

#### Scenario: Mutate caller-owned planning inputs
- **WHEN** 调用方在计划创建后修改原 Tool Catalog bytes、项目指令、语义 turn slice、有序消息 buffer 或 getter 返回的数据
- **THEN** 计划中的来源、估算、canonical segments 和指纹保持不变

#### Scenario: Reject a malformed source sequence
- **WHEN** 来源缺失必需项、包含重复 kind，或 `tool_catalog` 不在 profile 与可选项目指令之间
- **THEN** 规划失败且不返回可供 Runtime 或 Provider 使用的部分计划

#### Scenario: Change only the volatile input
- **WHEN** 调用方尝试仅替换 current_input 为携带新正文的旧开发 shape
- **THEN** 当前 validator 拒绝旧 shape；合法追加必须先提交历史，不能通过 volatile input 绕过持久化
- **THEN** 拒绝操作不改变已有计划的 stable prefix fingerprint

#### Scenario: Change project instructions
- **WHEN** 仅项目指令的规范化内容或项目根相对来源发生变化
- **THEN** project-stable segment、计划总估算和 stable prefix fingerprint 发生变化
- **THEN** tool catalog、committed history 与 current input segments 保持不变

#### Scenario: Change the Tool Catalog
- **WHEN** 任一当前facade的schema、description或catalog source revision发生变化
- **THEN** tool catalog segment、计划总估算与stable prefix fingerprint发生变化并可定位到该source revision
- **THEN** committed history 与 current input segments 保持不变

#### Scenario: Keep command world state out of the stable prefix
- **WHEN** catalog定义不变但实际command、workdir、环境值、approval identity或sandbox实例状态变化
- **THEN** tool catalog canonical bytes、source revision与stable prefix fingerprint保持不变

#### Scenario: Avoid a context-plan version router
- **WHEN** Runtime 规划当前 Provider 请求
- **THEN** ContextPlan API 不要求永远等于常量的 plan revision，且生产代码不存在旧计划 decoder 分支

artifact ID、保存状态、存储目录、实时配额与读取游标 MUST NOT 进入 stable catalog source。

#### Scenario: Keep artifact activity outside the stable prefix
- **WHEN** 同一 catalog 下创建、读取、删除输出对象或改变配额使用量
- **THEN** catalog canonical bytes、source revision 和 stable prefix fingerprint 不变；估算包含 ReadArtifact 实际 facade schema

committed_history 的 cache canonical 正文 SHALL 仅含封闭 summary/text/skill_attachment/file_attachment kind、有序角色和文本；同一来源的 canonical 元数据另允许下述有界任务内容 stamp，不包含 InputID、turn ID、facade profile 或展示状态。同内容不同关联身份必须得到相同内容 fingerprint。

#### Scenario: Keep identity out of cache content
- **WHEN** 仅改变输入和 turn 身份但原生可见内容及其顺序相同
- **THEN** 请求与语义规划内容 fingerprint 相同，stable prefix 不变

committed_history source revision SHALL 使用独立单调 context revision；普通 native commit 和成功 checkpoint 各推进一次，失败/查询不推进。摘要正文按有来源标记的文本参与该尾部分段 canonical 内容，CompactionID、原始记录位置和时间不得进入内容 fingerprint。既有“角色/文本”规范在摘要出现时扩展为封闭 summary/text/skill_attachment/file_attachment kind，仍不得纳入展示身份。

#### Scenario: Change only compaction identity
- **WHEN** 两个窗口的摘要与增量内容、source revisions 相同但 CompactionID 或 Session 路径不同
- **THEN** 请求 canonical bytes、历史内容 fingerprint 和 stable prefix fingerprint 一致

#### Scenario: Retain a stable prefix across compaction
- **WHEN** 只成功替换有效历史，profile/tools/project instructions/skill catalog 不变
- **THEN** 稳定前缀 fingerprint 不变，committed_history 的 revision/内容 fingerprint 发生可解释变化

#### Scenario: Change skill metadata without dynamic paths
- **WHEN** 只改变 Skill 描述或调用策略
- **THEN** skill_catalog 的 revision、fingerprint 和估算发生可解释变化；仅移动绝对根目录而相对内容不变时指纹保持不变

#### Scenario: Load a body into the history tail
- **WHEN** 同一目录下成功调用 Skill
- **THEN** 正文只计入有效历史一次，不改变 metadata 或稳定工具 schema，不再通过 current_input 重复计量

Question 的静态 schema/description SHALL 纳入同一 tool_catalog source；新增定义改变该来源 revision/fingerprint 和其下游稳定前缀，不能伪称新增工具后 prefix 不变。动态问题、选择、说明、提问 request identity、结果 allowance 和窗口焦点不得进入稳定 catalog；已提交原生调用/结果由原生 footprint 覆盖，不以额外 current_input 或 skill_catalog 重复计量。

#### Scenario: Separate question definitions from answers
- **WHEN** catalog 定义不变，仅问题、回答、UI、身份或剩余额度变化
- **THEN** 稳定 catalog bytes/revision/fingerprint 不变，实际已提交历史按内容改变尾部和完整请求 fingerprint

#### Scenario: Add a question definition deterministically
- **WHEN** 将提问定义加入当前完整 catalog 并以不同注册顺序构造
- **THEN** 两次得到同一十能力定义 bytes 与新 fingerprint，失效来源明确为 tool_catalog

任务进度的静态 schema/description SHALL 纳入同一 tool_catalog source，并遵循两家 facade 的确定性定义；添加该能力必须可解释地改变目录 revision/fingerprint。动态任务标签、activeForm、状态、说明、当前列表、折叠/焦点、更新来源 sequence/turn/invocation 不得进入稳定目录或 profile。任务内容在原生历史或 checkpoint 包装中的变化 SHALL 只改变适用历史尾部及完整请求，不改变已冻结工具定义。仅来源身份不同而实际列表和其他模型内容相同，内容 fingerprint MUST 相同。

#### Scenario: Add task progress to the complete catalog
- **WHEN** 两次以不同注册顺序构造含 TodoWrite/update_plan 的完整当前目录
- **THEN** 两次得到相同十能力 canonical 定义及新 fingerprint，失效来源为 tool_catalog

#### Scenario: Change task state without changing the stable prefix
- **WHEN** 静态目录不变，模型把同一任务从 pending 更新到 completed 或用户折叠列表
- **THEN** 状态更新只改变已提交历史尾部，折叠不改变任何请求；tool_catalog canonical bytes/revision/fingerprint 保持不变

#### Scenario: Keep task provenance out of model content
- **WHEN** 两份合法 checkpoint 的模型摘要及规范任务内容相同，仅来源 seq、turn、invocation 或 CompactionID 不同
- **THEN** 实际摘要包装 bytes 和内容 fingerprint 相同，来源仍分别在 typed metadata 中验证

任务内容 stamp SHALL 使用固定 canonical 规则，none 省略与 clear/snapshot 有 digest 是同一当前规划契约中的语义分支，不是历史格式、内部版本或兼容 reader。对任意相同 context revision、估算状态/token 数和模型摘要，规范任务内容不同 MUST 使 committed_history 内容 fingerprint 不同；只改变结果来源或 facade profile 而规范 domain 更新相同 MUST 得到相同 stamp 和内容 fingerprint。none 与成功 clear 即使均无条目也必须可区分。stamp 无法验证、kind/digest 不一致或使用错误窗口状态时 MUST 拒绝计划，不能用 token 数相同掩盖内容差异。

#### Scenario: Distinguish equal-sized task snapshots in history fingerprints
- **WHEN** 两份已验证窗口的 context revision、估算方法/状态/token 数、语义消息及真实模型摘要相同，仅规范任务内容由同长度 A 改为 B
- **THEN** 两份 committed_history 内容 stamp 与 fingerprint 不同，provider_profile/tool_catalog/project/skill 稳定段不变

#### Scenario: Exclude source identity and facade profile from the stamp
- **WHEN** 两份合法状态具有相同 kind 及规范 domain.TaskUpdate，仅来源 seq/turn/invocation/CompactionID 或原调用 facade profile 不同
- **THEN** stamp、任务参考正文与 committed_history 内容 fingerprint 相同，不把工具输入 profile 或持久化关联字段带入规划内容；不据此宣称两家 Provider 的完整原生包装结构相同

#### Scenario: Distinguish no task from an explicit empty update
- **WHEN** 一个窗口从无成功任务更新，另一个窗口最新成功更新是无说明的空列表
- **THEN** none 窗口省略 stamp，clear 窗口带规范空更新的 digest，二者 committed_history canonical 内容不同

#### Scenario: Match the declared tools in Plan Mode
- **WHEN** 普通采样冻结了 Plan Mode 的已声明目录
- **THEN** `tool_catalog` 的顺序、canonical 定义、内容派生身份和估算覆盖 Read、Glob、Grep、ReadArtifact、Skill、Question 与任务进度，恢复完整声明时得到普通模式对应的目录身份
- **THEN** 变动可定位到 `tool_catalog` 来源，原生历史中的既有调用与结果保留原内容身份

### Requirement: Token estimates preserve method and uncertainty

每个可计量来源和计划总量 SHALL 携带非空 estimator method、显式 `estimated` 或 `unknown` 状态以及无符号 token 数；unknown 状态不得伪装为已知零。当前切片 MUST 使用版本化、本地、确定性的启发式算法，不得调用远程 token-count API，也不得将 Provider 返回的 normalized usage 直接解释为当前上下文占用。

共享层 SHALL 从 `ActiveContextView` 估算已提交可见文本，按 turn 内有序消息计量每条文本一次；current_input 采样标记没有新增正文估算。Provider 原生 footprint 表示已提交原生历史的总估算，已经包含其中可见和 opaque 内容；规划合并已提交历史时 MUST 使用能够覆盖共享可见估算与原生总估算的单一值，不得将两者直接相加造成重复计数。若任一覆盖输入 unknown，结果 MUST 保留不确定性而不是静默降低为较小的已知值。

所有加减法 MUST 检测溢出或使用饱和语义，不得因整数 wraparound 把超大上下文判为可用。

#### Scenario: Include Provider-private history without exposing it

- **WHEN** 原生历史包含可见文本和不进入语义投影的 thinking、redacted thinking 或 encrypted reasoning
- **THEN** committed history 总估算覆盖语义文本估算和 Provider 原生总估算
- **THEN** 计划不包含 opaque 正文、signature、encrypted bytes 或原生 item

#### Scenario: Avoid double-counting visible history

- **WHEN** 有效窗口语义快照估算为 100 tokens，Provider 原生历史总 footprint 为 140 tokens
- **THEN** committed history 贡献为覆盖两者的 140 tokens，而不是 240 tokens

#### Scenario: Preserve an unknown estimate

- **WHEN** Provider 无法为一个会在下一请求中重放的原生 item 产生受支持估算
- **THEN** committed history 和计划总量标识为 unknown
- **THEN** unknown 不会被展示或计算为零 token

#### Scenario: Count steering once
- **WHEN** 两条追加输入已经进入 native history 并参与下一 sample 规划
- **THEN** 可见估算与 native footprint 使用覆盖值而不相加，追加文本不再次作为 current input 计量

语义估算与原生 footprint MUST 指向同一有效窗口和 context revision；压缩摘要只计一次，已归档正文不计入。摘要请求消耗的 usage 不得用作新窗口占用。

#### Scenario: Avoid counting archived history
- **WHEN** 已归档历史估算 100000，当前摘要语义估算 1000，当前原生窗口估算 1200
- **THEN** 历史贡献为 1200，而非 100000、101200 或 2200

Task 原生参数/结果 SHALL 沿既有覆盖规则计量一次。checkpoint 的 Provider 原生 footprint MUST 覆盖真实模型摘要包装及完整确定性任务段，而单向语义摘要继续只表示真实模型 generation；共享规划使用两者的单一覆盖值，不把宿主 task JSON 拼入语义摘要后再次求和，不把恢复内存列表另计一份。归档任务调用不再重放到有效窗口；其业务状态仍由完整 ledger 验证，不因此使归档正文重复进入估算。

#### Scenario: Count an exact checkpoint task snapshot once
- **WHEN** 当前语义摘要估算 100 tokens，含完整任务包装的原生窗口 footprint 为 150 tokens，旧调用已归档
- **THEN** committed_history 贡献为 150，不是 250、重复加入当前列表的数值或全部归档历史估算
- **THEN** 查询、列表折叠及 /tasks 阅读不改变估算、context revision 或请求 fingerprint

任务内容 stamp SHALL 仅参与 committed_history 的内容身份，不增加 profile、工具目录、current_input 或历史 token 贡献；不能对 digest 字符串重新估算正文或把 TaskProgressState 另加到原生 footprint。原生 footprint 仍完整覆盖实际发送的任务调用或 checkpoint 包装，不为实现 stamp 扩大为另一套通用 native 内容协议。

#### Scenario: Change task content identity without double-counting it
- **WHEN** 两份窗口任务内容不同但实际原生 footprint 同为 150 tokens，真实语义摘要同为 100 tokens
- **THEN** committed_history fingerprints 不同而各自历史估算均为 150，不因 stamp 再加 digest 或任务文字的 token

### Requirement: Budget decisions require explicit configuration and complete evidence

配置窗口存在时，有效输入上限 SHALL 等于 `context_window_tokens - reserved_output_tokens - context_safety_margin_tokens`。计划总量小于或等于有效输入上限 SHALL 判定为 `within_limit`；完整已知且大于上限 SHALL 判定为 `over_limit`。没有配置窗口或任一必需估算 unknown 时，判定 SHALL 为 `not_enforced` 或 `indeterminate`，不得产生硬性超限结论。

系统 MUST NOT 根据 model 名称、Provider family、远程错误或静态内置表猜测 context window。预算状态及错误摘要 MUST 只暴露数值、估算方法、质量和来源 kind，不得包含 prompt、历史正文、opaque data 或 secret。

#### Scenario: Accept a plan at the exact limit

- **WHEN** 窗口为 10,000，输出预留为 1,000，安全余量为 500，完整已知的计划总量为 8,500
- **THEN** 有效输入上限为 8,500 且预算状态为 `within_limit`

#### Scenario: Reject only a confirmed overage

- **WHEN** 窗口和预留已配置，所有必需估算已知且计划总量大于有效输入上限
- **THEN** 预算状态为 `over_limit` 并包含不泄露内容的数值摘要

#### Scenario: Plan without a configured window

- **WHEN** 用户没有配置 `context_window_tokens`
- **THEN** 系统仍生成来源、cache plan、指纹和 token 估算
- **THEN** 预算状态为 `not_enforced` 且系统不根据 model 名称推断窗口

#### Scenario: Keep an uncertain plan non-blocking

- **WHEN** 已配置窗口但计划总量因为原生 footprint unknown 而不可完整判定
- **THEN** 预算状态为 `indeterminate`
- **THEN** 系统不把该状态误报为明确超限

### Requirement: File attachments affect only the actual effective history tail

有效历史规划 SHALL 在同一真实用户消息内按原文、可选 Skill、有序文件包装分别计入实际模型可见内容，文件使用封闭 file_attachment 内容 kind，不包含 InputID、turn ID、摘要展示状态、文件 digest、绝对根、mtime、inode 或候选搜索状态。候选预检 MUST 包括全部文件包装，并使用实际交付操作捕获的完整推理设置；user-input 编码和动态预算超限必须先于该输入 batch 的 Sync 确定拒绝。共享估算与 Provider native footprint MUST 指向同一有效窗口/revision 并继续使用覆盖值，不能相加或重复计算元数据。

在相同实际推理设置和其他来源下，文件查询、候选、选择、文件读取和 UI 状态 SHALL 不改变 provider_profile/tool_catalog/project_instructions/skill_catalog 的 bytes、revision 和稳定前缀 fingerprint。忽略规则、文件名索引、索引 revision、发现状态和候选 session MUST 不进入任何上下文来源、摘要 source digest 或模型文本。仅真实附件提交改变 committed_history 内容和完整请求；它的源行数/实际范围和部分行标记只作为实际包装的一部分计入一次。checkpoint 覆盖的附件不再重复计入有效窗口，也不得从磁盘自动再加载。

新独立操作改变实际 effort 时，provider_profile、请求 properties 和完整请求身份 SHALL 按既有推理契约改变，不能为维持文件补全的稳定前缀断言而复用旧 profile。固定同一 native 窗口和候选输入时，真实策略变化不得改变目录或原生历史内容身份；wire 设置相同而来源/选择序号不同不得改变请求/profile/native 身份。文件包装和 digest MUST 不包含 effort、选择来源或序号。OpenAI WS 继续核对真实属性和原生前缀；实际失败、取消、摘要或重连后的 reset/full 不能因同文件或选回旧值而被绕过。

#### Scenario: Search without changing the prompt
- **WHEN** 用户多次输入 @ 查询、移动候选、关闭或接受候选但未提交
- **THEN** 已提交历史、所有上下文来源和请求 fingerprint 保持不变，没有 Provider 请求

#### Scenario: Change discovery rules without committing an attachment
- **WHEN** 忽略规则、索引覆盖、候选 session 或显示的索引 revision 改变，但没有新的输入提交
- **THEN** 请求、上下文各分段和 compaction source digest 均不受影响，发现配置不变成项目指令或 Skill metadata

#### Scenario: Change only the attached file body
- **WHEN** 两份输入使用相同原文、目录和关联快照，但实际冻结文件内容不同
- **THEN** 历史尾部与完整请求 fingerprint 可解释地变化，原稳定来源/前缀保持不变

#### Scenario: Count attachments once before and after restore
- **WHEN** 同一包含多文件的有效窗口分别直接规划与从合法 native 历史恢复后规划
- **THEN** 内容 canonical、估算方法/状态/值、footprint/revision 及分段 fingerprints 相同，正文不因 metadata 重复计量

#### Scenario: Compact a file-bearing history
- **WHEN** checkpoint 已覆盖文件附件且新窗口只含摘要与后续增量
- **THEN** 只计当前窗口一次，完整旧文件仍可阅读但不重新注入、读盘或污染稳定目录

#### Scenario: Isolate a changed operation policy from a file change
- **WHEN** 固定同一文件 native 窗口、候选输入和目录，仅将新操作的实际 effort 从 high 改为 low
- **THEN** provider_profile 与请求属性按策略失效，目录/原生历史内容与附件 digest 不变；预检、body 和 receipt 不混用两种策略

#### Scenario: Keep equal wire settings independent of selection revisions
- **WHEN** 同一文件输入的实际设置相同，但选择来源和进程序号不同
- **THEN** profile、完整逻辑请求、native 内容和所有相应指纹相同，序号不进入摘要来源或包装

#### Scenario: Distinguish unused selections from a reset connection
- **WHEN** 分别在没有中间操作时 high→low→high，及在真实失败/取消/摘要后选回 high，并使用相同文件窗口
- **THEN** 前者按实际属性和前缀判断增量，后者沿原 reset/full 规则；附件 digest 相同不允许旧 previous_response_id 复活
