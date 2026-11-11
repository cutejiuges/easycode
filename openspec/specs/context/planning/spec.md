# context/planning Specification

## Purpose

定义 Provider 请求前的确定性上下文来源、不可变计划、缓存分段、token 估算和预算判定，使共享 Runtime 能在不解析或重建 Provider 原生历史的前提下诊断上下文并阻止明确超限的请求。

## Requirements

### Requirement: Context planning uses typed ordered sources

上下文规划 SHALL 只接收强类型的 Provider profile、Tool Catalog snapshot、可选项目指令快照、可选 Skill metadata 快照、已提交有效窗口语义快照、Provider 原生历史 footprint、不携带额外正文的当前采样状态和可选预算。来源 MUST 按 `provider_profile`、`tool_catalog`、可选 `project_instructions`、可选 `skill_catalog`、`committed_history`、`current_input` 的固定顺序出现；无项目指令文档时 MUST 省略 `project_instructions`，无可向模型披露的 Skill 时 MUST 省略 `skill_catalog`。规划不得隐式读取时间戳、随机 ID、cwd、环境变量、Session 路径、git 状态、文件系统或网络。

`provider_profile` SHALL 只包含 Provider family、model、既有显式 schema revision 以及实际非默认普通请求策略的确定性身份，不得包含 API key、base URL 凭据、配置路径或其他 secret。新设置省略时 profile canonical bytes 和 source revision MUST 与既有默认基线一致；不得递增固定 revision、增加格式版本路由或从 profile 反向重建原生历史。`tool_catalog` SHALL 只包含 catalog revision、稳定排序的 facade schema canonical bytes 与内容派生 fingerprint，不得包含 executor、invocation/call identity、权限临时状态或 workspace 绝对路径。`project_instructions` SHALL 只包含经过发现契约验证的不可变快照、内容派生 revision、截断状态和项目根相对来源，不得包含绝对项目根或启动 cwd。规划 MUST 是纯内存操作，且不得修改输入快照或 Provider 原生历史。

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

### Requirement: Context plans are immutable validated snapshots

上下文计划 SHALL 使用唯一当前的强类型 shape，并深拷贝所有调用方可变输入；纯内存 plan MUST NOT 携带固定 format revision 或按 revision 选择 validator。计划 MUST 拒绝未知来源 kind、重复来源、非法顺序、空 source revision、非法 Tool Catalog、非法项目指令快照、非法 Skill 目录、非法估算状态、Provider family 不匹配及无效缓存计划。所有返回 slice、bytes 或嵌套值的读取操作 MUST 返回独立副本。

计划 SHALL 把 `provider_profile` 与包含 Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill、Question 的完整 `tool_catalog` 映射为 stable cache segments，把可选 `project_instructions` 映射为 project-stable cache segment，把可选 `skill_catalog` 映射为 project-stable segment，把 `committed_history` 映射为 turn-stable segment，把 `current_input` 映射为 volatile segment，并复用 `context/cache-plan` 的 canonical JSON、排序和 stable prefix fingerprint 契约。Tool Catalog 估算 SHALL 覆盖实际发送给对应 Provider 的全部 facade schema/description；Exec动态command、workdir、timeout、approval、sandbox临时状态与环境实际值不得进入catalog source。来源生命周期、source revision 与缓存稳定级别 MUST 保持为不同的强类型概念。

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

committed_history 的 cache canonical 内容 SHALL 仅含封闭 summary/text/skill_attachment kind、有序角色和文本，不包含 InputID、turn ID 或展示状态；同内容不同关联身份必须得到相同内容 fingerprint。

#### Scenario: Keep identity out of cache content
- **WHEN** 仅改变输入和 turn 身份但原生可见内容及其顺序相同
- **THEN** 请求与语义规划内容 fingerprint 相同，stable prefix 不变

committed_history source revision SHALL 使用独立单调 context revision；普通 native commit 和成功 checkpoint 各推进一次，失败/查询不推进。摘要正文按有来源标记的文本参与该尾部分段 canonical 内容，CompactionID、原始记录位置和时间不得进入内容 fingerprint。既有“角色/文本”规范在摘要出现时扩展为封闭 summary/text/skill_attachment kind，仍不得纳入展示身份。

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
- **THEN** 两次得到同一九能力定义 bytes 与新 fingerprint，失效来源明确为 tool_catalog

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
