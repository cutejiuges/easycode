## MODIFIED Requirements

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

committed_history 的 cache canonical 内容 SHALL 仅含封闭 summary/text/skill_attachment/file_attachment kind、有序角色和文本，不包含 InputID、turn ID 或展示状态；同内容不同关联身份必须得到相同内容 fingerprint。

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
- **THEN** 两次得到同一九能力定义 bytes 与新 fingerprint，失效来源明确为 tool_catalog


## ADDED Requirements

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
