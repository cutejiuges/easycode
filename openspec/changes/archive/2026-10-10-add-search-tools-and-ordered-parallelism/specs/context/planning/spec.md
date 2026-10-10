## MODIFIED Requirements

### Requirement: Context plans are immutable validated snapshots

上下文计划 SHALL 使用唯一当前的强类型 shape，并深拷贝所有调用方可变输入；纯内存 plan MUST NOT 携带固定 format revision 或按 revision 选择 validator。计划 MUST 拒绝未知来源 kind、重复来源、非法顺序、空 source revision、非法 Tool Catalog、非法项目指令快照、非法估算状态、Provider family 不匹配及无效缓存计划。所有返回 slice、bytes 或嵌套值的读取操作 MUST 返回独立副本。

计划 SHALL 把 `provider_profile` 与包含 Read、Glob、Grep 的完整 `tool_catalog` 映射为 stable cache segments，把可选 `project_instructions` 映射为 project-stable cache segment，把 `committed_history` 映射为 turn-stable segment，把 `current_input` 映射为 volatile segment，并复用 `context/cache-plan` 的 canonical JSON、排序和 stable prefix fingerprint 契约。Tool Catalog 估算 SHALL 覆盖实际发送给对应 Provider 的全部 facade schema/description；来源生命周期、source revision 与缓存稳定级别 MUST 保持为不同的强类型概念。

#### Scenario: Mutate caller-owned planning inputs
- **WHEN** 调用方在计划创建后修改原 Tool Catalog bytes、项目指令、语义 turn slice、当前输入 buffer 或 getter 返回的数据
- **THEN** 计划中的来源、估算、canonical segments 和指纹保持不变

#### Scenario: Reject a malformed source sequence
- **WHEN** 来源缺失必需项、包含重复 kind，或 `tool_catalog` 不在 profile 与可选项目指令之间
- **THEN** 规划失败且不返回可供 Runtime 或 Provider 使用的部分计划

#### Scenario: Change only the volatile input
- **WHEN** 两次计划的 profile、Tool Catalog、项目指令和 committed history 相同但当前用户输入不同
- **THEN** volatile segment 内容发生变化
- **THEN** stable prefix fingerprint 保持一致

#### Scenario: Change project instructions
- **WHEN** 仅项目指令的规范化内容或项目根相对来源发生变化
- **THEN** project-stable segment、计划总估算和 stable prefix fingerprint 发生变化
- **THEN** tool catalog、committed history 与 current input segments 保持不变

#### Scenario: Change the Tool Catalog
- **WHEN** Read、Glob 或 Grep 任一 facade schema、description 或 catalog source revision 发生变化
- **THEN** tool catalog segment、计划总估算与 stable prefix fingerprint 发生变化并可定位到该 source revision
- **THEN** committed history 与 current input segments 保持不变

#### Scenario: Avoid a context-plan version router
- **WHEN** Runtime 规划当前 Provider 请求
- **THEN** ContextPlan API 不要求永远等于常量的 plan revision，且生产代码不存在旧计划 decoder 分支
