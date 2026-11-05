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
