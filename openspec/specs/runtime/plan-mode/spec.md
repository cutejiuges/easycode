# runtime/plan-mode Specification

## Purpose

定义当前交互会话的规划模式控制、操作捕获和恢复边界，使同一会话可以在工作区只读探索后由用户明确开始实施，并持续遵守已有工具、队列、压缩及持久化契约。

## Requirements

### Requirement: An interactive session owns its current planning mode

每个交互会话 SHALL 从 `default` 模式开始，并由该会话的单一控制 owner 管理 `default` 与 `plan`。只有 TUI 的显式本地控制 SHALL 设置目标模式。宿主 MUST 依据关联当前交互实例、会话和请求的接纳确认显示有效模式。每次新的用户操作使用新的请求身份，已有活动及有界近期请求 MUST 按现有去重规则拒绝，宿主不得自动重发模式控制。模型输出、用户正文、问卷回答、补全与读取历史 MUST NOT 切换模式或启动实施回合。

模式控制 SHALL 仅在真实空闲状态接纳。用户输入仍在准备、已接纳但尚未启动、等待排队、当前回合的任何阶段（含起始、问卷、检查和清理）、独立压缩或会话交接许可持有期间，控制 MUST 保留原模式并以现有忙碌或关闭错误拒绝。模式接纳与输入准备、排队、操作启动和交接 MUST 有可确定的先后，失败后不得残留阻碍新输入的占用。取消、宿主或控制循环关闭、会话不可继续若先于接纳，MUST 保留原状态。接纳先于取消时 MUST 返回确定结果。

#### Scenario: Switch only after the worker releases ownership

- **WHEN** 当前回合已发布结束事件但仍在清理，用户请求切换模式
- **THEN** 控制返回忙碌且继续使用原模式
- **THEN** 清理完成且队列为空后可以显式再次切换

#### Scenario: Serialize input preparation with entering plan mode

- **WHEN** 一个交互草稿正在准备 Skill 或文件附件，同时宿主尝试进入 Plan Mode
- **THEN** 已取得准备接纳的输入保持当前模式，切换被拒绝
- **THEN** 切换先接纳时，后续接纳的输入使用新的模式

#### Scenario: Reject a transition during a resume handoff

- **WHEN** 当前源会话已为 `/resume` 保留交接许可
- **THEN** 模式切换不改变源或候选会话；交接取消后源会话保留原模式并可继续接纳

#### Scenario: Confirm an accepted transition after caller cancellation

- **WHEN** 调用方在模式接纳边界前或后取消
- **THEN** 接纳前返回取消且模式不变；接纳后返回确定的有效模式，同一请求仍在去重账本时再次送达被拒绝

#### Scenario: Reject mode control during shutdown

- **WHEN** 模式请求已交给当前 owner，宿主或控制上下文在接纳前关闭
- **THEN** 不交换模式，入口占用随确定结果或流关闭收口，正常关闭路径继续

### Requirement: An operation keeps one immutable planning policy

工作模式与 `/effort` 的选择 SHALL 独立更新。切换模式保留已选 effort，设置或重置 effort 保留已选模式。每个新操作 SHALL 一次冻结当前模式和实际推理设置，由同一选择派生规划指令、普通请求工具定义和准入。普通请求模式与指令或目录错配 MUST 在网络请求前拒绝。一个回合中的首次预检、普通采样、工具结果续采样、同轮追加、自动压缩后回到普通采样及 Stop 反馈续轮 MUST 使用同一捕获值。手动压缩 SHALL 使用被接纳时冻结的选择为后续普通采样窗口预检。等待队列 SHALL 沿用当前排队和取消规则；模式控制不得改写、合并、隐式提交或重新排序输入。

Plan 回合中的普通采样 SHALL 使用工作区只读工具准入。会话级 Patch/Write grant 与已有 Read 证据不得解锁计划回合。用户配置并独立信任的只读、禁网 Stop 和 PreCompact 检查 SHALL 仍按既有契约运行，并且反馈不得改变模式或工具准入。现有 Session、搜索保存输出和任务清单的私有状态写入仍按各自契约进行。

#### Scenario: Keep a long planning turn read-only

- **WHEN** Plan 回合经过工具续采样、同轮输入、自动压缩或 Stop 反馈
- **THEN** 后续普通采样继续使用同一 Plan 工具许可和指令，已配对的调用与结果保持原生顺序

#### Scenario: Change effort and mode independently

- **WHEN** 用户在 Plan 内选择或重置 effort，或先改变 effort 再进入、退出 Plan
- **THEN** 下一普通操作使用两个当前选值，`/context` 的所选估算与实际请求对应，模式切换不撤销 effort，effort 控制不撤销 Plan
- **THEN** 若 effort 在 Plan 回合活动时更新，该回合所有续采样维持已捕获的模式与 effort，之后被接纳的操作使用新的 effort

#### Scenario: Exit without authorizing or starting implementation

- **WHEN** 用户在空闲的 Plan 会话中显式请求回到普通模式
- **THEN** 仅更新有效模式，零新用户输入、Provider 请求、工作区修改和工具授权
- **THEN** 之后独立发送的用户输入才接纳为普通回合，依照既有候选、grant、审批和 sandbox 规则处理工具

### Requirement: Planning state ends with its interactive control owner

模式、接纳身份与状态提示 SHALL 仅属于活动交互会话，不产生新的 Session record、native item、配置字段、历史输入或外部 headless 消息。真实用户输入、助手计划正文和已完成工具事实 SHALL 按正常原生历史持久化及恢复，使用既有 checkpoint 和离线阅读语义。新进程、`--resume`、`--continue` 和成功切换进入的目标会话 MUST 从普通模式开始。恢复 MUST NOT 重放模式控制、发起新请求、执行工具或将历史计划变为当前授权。失败或取消的聊天内切换 SHALL 保留源交互会话的模式。

#### Scenario: Resume a session that contained a plan

- **WHEN** 一个包含已提交 Markdown 计划的交互会话退出，或用户后来通过 `/resume` 切回该会话
- **THEN** 普通转录可阅读该计划，恢复没有工具执行或 Provider 请求，活动模式为普通模式
- **THEN** 用户可在空闲时重新进入 Plan，继续修订同一会话的方案

#### Scenario: Keep headless mode controls unavailable and preserve skills

- **WHEN** 一次性或 stream-json headless 宿主提交 `/plan`、`/plan off` 或 `$plan`
- **THEN** 依照既有显式 Skill 分派，匹配的合法 `plan` 技能正常加载；未匹配裸 slash 首词时按普通正文处理。显式美元分派保留既有错误，模式始终为 `default`
- **THEN** 单独发送外部类型化模式命令时按未支持控制拒绝
