## MODIFIED Requirements

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
