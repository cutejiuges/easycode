# runtime/plan-mode Specification

## Purpose

定义当前交互会话的规划模式控制、操作捕获和恢复边界，使同一会话可以在工作区只读探索后由用户明确开始实施，并持续遵守已有工具、队列、压缩及持久化契约。

## Requirements

### Requirement: An interactive session owns its current planning mode

每个交互会话 SHALL 从 `default` 模式开始，并由该会话的单一控制 owner 管理 `default` 与 `plan`。只有 TUI 的显式本地模式控制或用户明确确认方案的实施交接 SHALL 设置目标模式。宿主 MUST 依据关联当前交互实例、会话和请求的接纳确认显示有效模式。每次新的用户操作使用新的请求身份，已有活动及有界近期请求 MUST 按现有去重规则拒绝，宿主不得自动重发。模型输出、用户正文、问卷回答、补全与读取历史 MUST NOT 触发模式切换或方案实施交接。用户已通过普通模式控制离开 Plan 时，可独立提交普通实施输入。

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

工作模式与 `/effort` 的选择 SHALL 独立更新。切换模式保留已选 effort，设置或重置 effort 保留已选模式。每个新操作 SHALL 一次冻结当前模式和实际推理设置，由同一选择派生规划指令、普通请求工具定义和准入。普通请求模式与指令或目录错配 MUST 在网络请求前拒绝。一个回合中的首次预检、普通采样、工具结果续采样、同轮追加、自动压缩后回到普通采样及 Stop 反馈续轮 MUST 使用同一捕获值。手动压缩 SHALL 使用被接纳时冻结的选择为后续普通采样窗口预检。等待队列 SHALL 沿用当前排队和取消规则。普通模式切换不得改写、合并、隐式提交或重新排序输入。

Plan 回合中的普通采样 SHALL 使用工作区只读工具准入。会话级 Patch/Write grant 与已有 Read 证据不得解锁计划回合。用户配置并独立信任的只读、禁网 Stop 和 PreCompact 检查 SHALL 仍按既有契约运行，并且反馈不得改变模式或工具准入。现有 Session、搜索保存输出和任务清单的私有状态写入仍按各自契约进行。

#### Scenario: Keep a long planning turn read-only

- **WHEN** Plan 回合经过工具续采样、同轮输入、自动压缩或 Stop 反馈
- **THEN** 后续普通采样继续使用同一 Plan 工具许可和指令，已配对的调用与结果保持原生顺序

#### Scenario: Change effort and mode independently

- **WHEN** 用户在 Plan 内选择或重置 effort，或先改变 effort 再进入、退出 Plan
- **THEN** 下一普通操作使用两个当前选值，`/context` 的所选估算与实际请求对应，模式切换不撤销 effort，effort 控制不撤销 Plan
- **THEN** 若 effort 在 Plan 回合活动时更新，该回合所有续采样维持已捕获的模式与 effort，之后被接纳的操作使用新的 effort

#### Scenario: Exit without authorizing or starting implementation

- **WHEN** 用户在空闲的 Plan 会话中用 `/plan off` 或 `Shift+Tab` 请求回到普通模式
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

### Requirement: An offer identifies one complete committed proposal

两家 Provider 的固定 Plan 指令 SHALL 要求将完整可实施的方案放在独立行的 `<proposed_plan>` 与 `</proposed_plan>` 标记内。修订 SHALL 给出完整替代稿。候选 SHALL 来自当前交互会话中采用 Plan 策略的实时回合的最终已提交助手采样。回合须成功、适用的 Stop 检查已确认通过、worker 已结束、会话可继续且队列为空。若已接纳中断，即使最终样本在竞态中成功提交，该回合也不得提供审阅。

Anthropic Messages SHALL 从同一助手消息按序读取可见正文。出现显式 `stop_reason` 时只接受 `end_turn`。缺省 stop reason 的正常完结仍须核对完整边界。OpenAI Responses SHALL 在正常完结中选取唯一有正文的 `final_answer` 助手消息。若可见助手消息都未提供 phase，只接受唯一有正文的助手消息。候选不能从多条消息拼接，也不能从已声明为非最终或未知 phase 的消息提取。Provider 投影仅用于计划审阅。

选定正文 MUST 恰含一个闭合块。标记须逐字独占一行，块外仅允许空白行。支持 LF、CRLF 和以 EOF 结束的闭标签行。块内须有非空白的 UTF-8 Markdown 且不超过 32 KiB，保留标记行之间的原始字节作为审阅和实施的共同依据。部分正文、重复标记、问卷或任务进度、含工具调用的采样、Stop 后仍续轮的采样、取消或失败的回合以及恢复转录 MUST NOT 产生候选。

同一交互会话 SHALL 至多持有一份当前候选，并将其身份绑定到 Session、Thread 和原始采样。接纳普通聊天输入、接纳独立压缩、实际离开 Plan 或接纳实施 SHALL 使旧身份失效。会话交接预留期间 MUST 拒绝确认，失败或取消交接保留源会话和原有候选有效性，接管目标使用自己的会话状态。读取和关闭审阅、只读查询及接纳前失败不消耗候选。

#### Scenario: Offer after a successful terminal and actual cleanup

- **WHEN** 双 Provider 之一在 Plan 最终采样中完成并提交一个合法方案，且 Stop（若有）通过
- **THEN** 等 worker 结束且会话确认空闲后才提供该确切版本，模式依然为 Plan
- **THEN** 若该采样经历 Stop 续轮、回合失败或取消，或已有待处理输入，便不从该采样提供审阅

#### Scenario: Keep an unreviewable reply available as ordinary transcript

- **WHEN** 规划回复仅有任务进度或正文缺少闭合行、在缩进代码中呈现标记、含多份方案、正文为空或超过上限
- **THEN** 当前回复依然可按其普通转录语义阅读，且没有实施候选

#### Scenario: Distinguish final Provider messages from truncated or preliminary output

- **WHEN** OpenAI 在同一次完成的采样中先发 commentary，再发一条完整标记的 `final_answer`
- **THEN** 只以最终答复的原文提供审阅，commentary 保持普通展示
- **THEN** 仅有非最终 phase、标记跨消息或 Anthropic 以 `max_tokens` 结束时，即使能从显示文本中读到闭合标记，也没有审阅候选

#### Scenario: Let newer input supersede an earlier plan

- **WHEN** 用户追加输入仍在等待，且较早的 Plan 样本完成
- **THEN** 不从这次完成弹出旧方案
- **THEN** 若追加内容在工具或 Stop 边界提交，后续实时最终成功样本可提供新方案

### Requirement: Approving a proposal atomically admits one Default turn

明确选择实施的类型化控制 SHALL 仅由活跃的当前交互宿主发起，以当前候选身份确认。输入正文由当前会话持有的完整原文构造。已退出 Plan、身份不匹配或已失效、会话不健康、准备输入中、队列非空、当前 worker 或独立压缩存续、持有交接许可、输入编码超限以及接纳前取消或关闭，MUST 在改变模式和接纳实施输入之前拒绝。确认和普通输入接纳之间 SHALL 有可确定的先后。

成功接纳 SHALL 在同一决定中消费该候选、切换到 Default、接纳恰好一条实施输入，并在新回合启动前关联确认模式和输入身份。获批输入 SHALL 是新回合唯一的初始输入，随后到达的普通输入只按已启动回合的规则处理。实施回合保留当时所选 effort，沿用普通模式的采样与准入。固定实施前缀后 SHALL 逐字节携带获批的 Markdown，正文中的 slash、Skill 或 `@` 字样只作为字面输入，且自动压缩只摘要旧窗口。方案确认只授权开启实施回合。工具候选、已有窄范围 grant、独立 Hook 信任、普通批准和沙箱 SHALL 依照现有契约决定是否执行工具。

接纳后 SHALL 返回确定的接纳结果，模式保持 Default，取消等待确认不取消已接纳的实施回合。输入只在普通提交 Sync 后被视为已提交。另行中断已启动回合、接纳后预检失败或不确定写入 MUST 遵循当前输入归属和会话故障规则，禁止自动重发。方案候选与确认凭据只属于进程中的交互实例。恢复 MUST 使用普通原生输入和已提交事实，且不得重建候选、重启实施或重放工具副作用。headless 保持原有输入分派和外部控制契约。

#### Scenario: Keep regular approval separate from plan approval

- **WHEN** 用户确认当前方案，Default 实施回合提出一个没有既有 grant 的文件修改
- **THEN** 原 Plan 回合保持只读，Default 实施输入只接纳一次；文件修改需要针对该次调用走普通审批
- **THEN** 在工具批准完成前工作区不产生该修改

#### Scenario: Reject stale and duplicate confirmation

- **WHEN** 确认引用上一版本的方案、另一交互会话或已经消费的候选，或者在交接许可、输入准备、队列或压缩争用期间送达
- **THEN** 它不改变当前模式、输入队列或任何工具授权，不启动额外实施回合

#### Scenario: Resolve cancellation on either side of admission

- **WHEN** 用户取消等待，且命令已投递到会话
- **THEN** 接纳前取消没有模式改变、实施输入或请求，接纳后取消等待仍返回已接纳的 Default 结果，正常启动实施
- **THEN** 独立中断实施回合或关闭会话时，实施正文按实际输入状态结算
- **THEN** 恢复按 journal 中实际提交的事实进行，且不自动重试该输入

#### Scenario: Deliver the exact approved plan across compaction and restore

- **WHEN** 任一 Provider 的首个 Default 实施请求前执行自动压缩，摘要省略方案中的区分性正文
- **THEN** Default 真实请求仍完整携带所确认的原文，压缩和实施不增加任何专用方案记录
- **THEN** 相同后续输入在持续运行和恢复后的完整逻辑请求 canonical bytes、原生 item 顺序以及缓存 fingerprint 按既有契约等价
