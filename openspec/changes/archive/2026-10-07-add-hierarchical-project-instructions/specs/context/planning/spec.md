## MODIFIED Requirements

### Requirement: Context planning uses typed ordered sources

上下文规划 SHALL 只接收强类型的 Provider profile、可选项目指令快照、已提交语义历史、Provider 原生历史 footprint、当前用户输入和可选预算。来源 MUST 按 `provider_profile`、可选 `project_instructions`、`committed_history`、`current_input` 的固定顺序出现；无项目指令文档时 MUST 省略 `project_instructions`。规划不得隐式读取时间戳、随机 ID、cwd、环境变量、Session 路径、git 状态、文件系统或网络。

`provider_profile` SHALL 只包含 Provider family、model 和显式 schema revision，不得包含 API key、base URL 凭据或其他 secret。`project_instructions` SHALL 只包含经过发现契约验证的不可变快照、内容派生 revision、截断状态和项目根相对来源，不得包含绝对项目根或启动 cwd。规划 MUST 是纯内存操作，且不得修改输入快照或 Provider 原生历史。

#### Scenario: Plan the same inputs twice

- **WHEN** 两次规划使用相同 profile、项目指令快照、语义历史、原生 footprint、当前输入和预算
- **THEN** 两次计划的来源顺序、revision、估算、预算状态、canonical cache segments 和指纹完全一致

#### Scenario: Exclude ambient process state

- **WHEN** 规划输入不变但当前时间、cwd、环境变量、Session 文件路径或随机源发生变化
- **THEN** 计划的 canonical bytes、分段指纹和预算判定保持不变

#### Scenario: Keep secrets out of the profile source

- **WHEN** 相同 family 和 model 使用不同 API key 或带凭据的配置来源
- **THEN** `provider_profile` 来源和稳定前缀指纹保持一致
- **THEN** 计划摘要、错误和诊断不包含 API key 或 URL 凭据

#### Scenario: Omit an empty project source

- **WHEN** 项目指令快照不含任何选中文档
- **THEN** 计划保持 `provider_profile`、`committed_history`、`current_input` 的既有顺序
- **THEN** cache segments、stable prefix fingerprint 和预算结果不因空占位而改变

### Requirement: Context plans are immutable validated snapshots

上下文计划 SHALL 使用固定 plan revision，并深拷贝所有调用方可变输入。计划 MUST 拒绝未知来源 kind、重复来源、非法顺序、空 source revision、非法项目指令快照、非法估算状态、Provider family 不匹配及无效缓存计划。所有返回 slice、bytes 或嵌套值的读取操作 MUST 返回独立副本。

计划 SHALL 把 `provider_profile` 映射为 stable cache segment，把可选 `project_instructions` 映射为 project-stable cache segment，把 `committed_history` 映射为 turn-stable segment，把 `current_input` 映射为 volatile segment，并复用 `context/cache-plan` 的 canonical JSON、排序和 stable prefix fingerprint 契约。项目指令 SHALL 使用其内容派生 revision 与规范化注入文本计算估算和 cache segment；来源生命周期和缓存稳定级别 MUST 保持为不同的强类型概念，不得用一个字符串字段同时表达两者。

#### Scenario: Mutate caller-owned planning inputs

- **WHEN** 调用方在计划创建后修改原始项目指令、语义 turn slice、当前输入 buffer 或 getter 返回的数据
- **THEN** 计划中的来源、估算、canonical segments 和指纹保持不变

#### Scenario: Reject a malformed source sequence

- **WHEN** 来源缺失必需项、包含重复 kind，或 `project_instructions` 不在 profile 与 committed history 之间
- **THEN** 规划失败且不返回可供 Runtime 或 Provider 使用的部分计划

#### Scenario: Change only the volatile input

- **WHEN** 两次计划的 profile、项目指令和 committed history 相同但当前用户输入不同
- **THEN** volatile segment 内容发生变化
- **THEN** stable prefix fingerprint 保持一致

#### Scenario: Change project instructions

- **WHEN** 仅项目指令的规范化内容或项目根相对来源发生变化
- **THEN** project-stable segment、计划总估算和 stable prefix fingerprint 发生变化
- **THEN** committed history 与 current input segments 保持不变
