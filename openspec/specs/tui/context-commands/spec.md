# tui/context-commands Specification

## Purpose

定义贴近 Claude Code 的上下文查询、手动压缩与会话内 effort 交互，使用户理解当前窗口、启动约束、活动捕获和后续选择，指定摘要重点、取消操作并查看完整历史，同时保留现有 composer、队列、工具详情与 overlay 焦点。

## Requirements

### Requirement: Local context commands are distinct from user messages

TUI SHALL 在 composer 提交边界识别首行精确命令 `/context` 与 `/compact`，后者可带剩余正文作为保留重点；`/context` 带参数 MUST 本地拒绝。命令和其显示结果 MUST NOT 形成 Provider user_inputs 或普通用户 transcript。其他非精确匹配文本 SHALL 保持普通文本行为，不按子串猜测命令。拒绝时 MUST 保留草稿、光标和历史；接受后可加入本地 recall，不伪造持久化用户输入。

#### Scenario: Submit summary guidance
- **WHEN** 用户提交 `/compact 保留权限决策和未完成测试`
- **THEN** 文本作为摘要重点传递，TUI 不生成普通用户输入提交或普通 turn

#### Scenario: Preserve ordinary text
- **WHEN** 用户提交 `/compactness`、文件路径或正文中间包含 `/compact`
- **THEN** 按普通输入处理，不执行本地压缩

#### Scenario: Reject a busy operation without losing the draft
- **WHEN** 活动 turn、审批或已排队输入存在时提交 `/compact`
- **THEN** 显示安全英文 busy 提示，草稿/光标保持，且无压缩网络或 started fact

### Requirement: Compaction uses a concise main transcript and expandable history

进行中的压缩 SHALL 显示 `Compacting conversation` 状态，不输出普通 assistant delta。成功 SHALL 显示简短 `Conversation compacted` 边界与 Ctrl+O 历史提示；完整摘要默认折叠，只在历史/详情中展开。手动压缩本身 MUST NOT 自动发起普通回答；用户已排队的显式新输入可在清理和成功终态后继续。原始用户/assistant/tool 历史和工具详情 MUST 保持可访问，恢复后边界顺序一致，不重放 spinner 或伪造实时耗时。

#### Scenario: Complete without follow-up input
- **WHEN** 手动压缩成功且没有排队输入
- **THEN** 显示完成边界后等待输入，普通 Provider sample 调用次数不增加

#### Scenario: Inspect old details after resume
- **WHEN** 成功压缩并重启后用户通过 Ctrl+O 查看压缩前工具详情和摘要
- **THEN** 原条目关联/内容不变，摘要作为独立边界详情显示，不作为用户或 assistant 原消息

#### Scenario: Do not show a false completion
- **WHEN** 摘要失败、取消或完成提交尚未确定
- **THEN** 不显示成功边界，不收起为已完成摘要，旧历史仍可查看

### Requirement: Composer and cancellation remain responsive during compaction

压缩期间 SHALL 允许编辑草稿并有界排队新输入，排队输入不参与当前摘要。Ctrl+C SHALL 定向取消活动 CompactionID；Esc 在详情层先关闭详情，在普通输入层有 queued auto 时先取回，否则取消压缩。过期操作完成/取消事件 MUST NOT 清除新操作状态。普通失败或取消后 auto 输入 SHALL 返还，strict-next 保持现有顺序规则；正文和草稿不得静默丢弃。

#### Scenario: Queue and continue after success
- **WHEN** 压缩期间用户提交新输入，随后压缩成功
- **THEN** 输入在新窗口上按 FIFO 继续，旧摘要请求中没有该输入

#### Scenario: Recover queued input after cancellation
- **WHEN** 用户取消压缩且有 queued auto
- **THEN** auto 进入 returned tray，可取回编辑；取消清理前不自动启动新 turn

### Requirement: Context display preserves uncertainty and terminal constraints

`/context` SHALL 展示模型、有效窗口来源估算、配置容量、预留、安全余量及估算方法/质量；分类总量 MUST 与同一查询快照一致。unknown 或未配置窗口时不得显示伪造的可用百分比。窄屏、极小尺寸和无色终端 SHALL 提供可读降级，View MUST 无 I/O。Provider usage 和费用不得混同为当前窗口占用。

#### Scenario: Display an unconfigured window
- **WHEN** 用户未配置 context window
- **THEN** 展示可用估算并标注窗口未配置，不猜测模型容量或绘制确定占用比例

#### Scenario: Render a narrow terminal
- **WHEN** 终端尺寸缩小或颜色被禁用
- **THEN** 数值、estimated/unknown 状态和关键操作仍可辨认，不因重绘改变规划或执行外部操作

### Requirement: Local cache inspection is concise and isolated from model input

TUI SHALL 在 composer 提交边界精确识别独立 /cache，不接受参数；/caches、正文中间的 /cache 或非命令文本 MUST 保持普通正文。合法命令 SHALL 只查询宿主缓存快照，不产生 user_inputs、turn 或网络。接受后可加入本地 recall，拒绝时保留草稿与光标。/context SHALL 增加当前缓存策略及 transport 的简短摘要；/cache SHALL 展示最近 durable 普通 sample、最近成功 turn 聚合、独立摘要、内容/策略变化和实际 transport 原因，不默认将长诊断插入聊天主线。

面板 SHALL 明确 process scope、restored_baseline、未观测、disabled、known zero 与 unknown，不显示原始 key、prompt、header、opaque reasoning 或绝对路径。Esc 关闭面板并恢复草稿，Ctrl+C 与审批/历史详情沿用既有优先级，查询中的迟到响应不得重新打开已关闭面板或覆盖新请求。固定尺寸、窄屏、小屏、无色与 hostile metadata SHALL 有有界可读展示；View 无 I/O 且不改变请求 fingerprint。

#### Scenario: Inspect cache during a running turn
- **WHEN** 用户提交 /cache 且普通任务活动中
- **THEN** 显示最近 committed 观测与活动状态，不取消任务、不启动新 turn

#### Scenario: Reject cache arguments without losing work
- **WHEN** 用户提交 /cache extra
- **THEN** 显示安全英文参数错误，草稿、光标和 Provider 调用次数保持

#### Scenario: Close before the response arrives
- **WHEN** 用户在查询返回前关闭面板，随后旧快照到达
- **THEN** 旧结果不重开面板，不覆盖新的 /context 或 /cache 查询状态

#### Scenario: Render unknown usage in a narrow terminal
- **WHEN** cache_write unknown 且窗口很窄
- **THEN** 仍可辨识 unknown、有效策略和关闭操作，不显示虚假的百分比

### Requirement: Context inspection explains frozen ordinary inference settings

`/context` SHALL 展示非敏感推理设置：Provider、启动冻结的 thinking 模式/manual budget、Anthropic display、summary 策略、普通输出上限及其 explicit/default/未设置来源；effort SHALL 展示当前操作捕获值、后续选定值、选择来源、阶段和生效边界。starting 不表示已有 durable TurnID 或真实发送，finishing 不表示清理已结束。活动窗口数值及捕获策略属于同一 committed 查询视图，idle 时展示下一操作将捕获的选择。用户选择、操作捕获、真实请求和返回观测 MUST 分开；省略项表示 provider default 或未发送，不伪造具体默认强度、不等于 disabled；OpenAI 无显式输出上限标记 server default/unknown。summarized 不表示必然有正文，omitted/空 thinking 不表示 reasoning usage 为零。

不可变启动字段 SHALL 由应用纯内存装配，effort 状态和动态窗口 SHALL 来自同一控制 owner 的内部查询快照，不从 UI 的乐观更新推断生效。内部事件 SHALL 使用唯一当前 typed payload 和严格 validator；headless 外部既有 context.snapshot 数值 shape MUST 通过显式安全投影保持不变。UI 不直接访问 Provider 或配置文件；查询/重绘无 I/O、不推进 revision、不改变请求、输入队列或设置。所有字段共用有界布局，普通/窄屏/极小尺寸/NO_COLOR 下仍可辨认有效状态和关闭操作，长设置按既有安全终端规则处理。

窗口数值只描述 ordinary committed context，不能当作正在进行的摘要输入、staging 或 usage。数值观测失败时 MUST 按既有规则报错，不显示与捕获/选定 profile 不符的旧数值；effort 快捷查询及合法选择保持独立可用。选择序号只用于隔离迟到观测，不显示成 Session/格式版本。

#### Scenario: Inspect adaptive settings
- **WHEN** 启动配置为 Anthropic adaptive/high、普通上限 16384 后打开 /context
- **THEN** 显示真实启动字段、选定 effort 和来源，活动提示与 Ctrl+O 阅读仍只反映实际返回内容

#### Scenario: Explain omitted settings honestly
- **WHEN** OpenAI 所有新增字段省略，或 effort 选择为 auto
- **THEN** 显示对应参数未发送、普通输出上限未知，不将其标成 medium、零或已关闭

#### Scenario: Query while a tool or summary is active
- **WHEN** high 任务或摘要活动中已接受 low，随后打开/关闭 context 面板
- **THEN** 当前设置仍显示 high，后续选择 low 明确待生效；窗口仍为同一 committed snapshot，原任务、队列、取消和焦点优先级保持

#### Scenario: Preserve external output contracts
- **WHEN** 配置主动推理并通过 stream-json 查询 context，或使用 text/json 输出
- **THEN** 既有外部 context.snapshot 和一次性输出的字段/版本不因内部设置展示扩张，推理正文和 opaque 哨兵不可见；effort 状态仅通过显式 effort 控制返回

已有 headless reasoning_output 字段 SHALL 按 provider/usage-accounting 返回 known/unknown 数值状态；字段结构/版本保持不等于计量语义仍为 not_applicable。TUI 设置展示不得承诺估算预算是已计费 usage 或配置值已被 endpoint 验证。

#### Scenario: Render a constrained terminal
- **WHEN** 窄屏、极小高度、NO_COLOR 或含 hostile metadata 的模型名下打开面板
- **THEN** 文本经过安全处理、有界裁剪，Esc 关闭仍可辨认，重绘不触发网络或配置读取

#### Scenario: Separate display choice from observed thinking
- **WHEN** display omitted 且 thinking_tokens 为 30，或 summarized 但响应没有可阅读正文
- **THEN** 配置、真实计量和 unavailable 各自准确展示，不推断零消耗、不制造正文，Ctrl+O 不改变请求

### Requirement: Local effort commands change only the next operation selection

TUI SHALL 在 composer 提交边界精确识别小写 `/effort`：无参数及 current/status 执行同一查询；原生 level 执行设置；auto/unset 执行同一字段省略选择；reset 恢复启动值；help/-h/--help 只显示静态用法。参数按 ASCII 大小写无关匹配归一化，除此以外的参数、多个参数或额外正文 MUST 本地拒绝，不能发送给模型。帮助不提交控制命令或改变设置，合法档位以权威查询为准。`/efforts`、正文中间的 `/effort` 和文件路径保持普通输入语义。

查询 SHALL 使用简短英文非模态反馈，不新增独占面板；显示当前操作捕获值、后续选择和 process scope，详细来源/额度/阶段留在既有 `/context`。合法设置经同一控制 owner 接收；关联 response 只表示 admission，匹配的权威 snapshot 到达后才安装数值和成功反馈，不能由调用返回值再更新一次。idle 说明供下一操作使用，busy 明确当前捕获值保持及下一回合/独立摘要生效；starting/finishing/cancel_requested 不得误报为实际发送或清理完成。auto/reset 必须可区分。

参数或设置拒绝 MUST 保留提交前完整草稿、字节光标、历史与队列；接受后可加入进程本地 recall，不能进入 Session、Provider history 或持久化用户回忆。只保留最近通知，状态最多两行、帮助最多六行；空间不足优先 composer。通知不拥有焦点，Esc/Ctrl+C 保持原有输入/取消规则；下一次显式编辑、提交或其他本地命令清除通知，流式/resize 不重复弹出。审批、问卷、Hook、详情和取消清理保持原优先级，不新增绕过 overlay 的通道。帮助不得把低 effort 解释成少测试或弱化质量门。

权威缓存 SHALL 按 selection_revision 排除较旧选择，局部通知/草稿 SHALL 另按 request/generation 与操作身份处理。过期回复不得清除新草稿、抢焦点、关闭新 overlay 或重现旧通知；已经接收的选择不因 UI 取消反馈而回滚。

#### Scenario: Set effort during ordinary streaming
- **WHEN** high 普通回合活动，用户提交 /effort low
- **THEN** 显示下一操作使用 low、当前仍为 high；不插入用户消息、不取消 stream、不重跑工具

#### Scenario: Preserve an invalid draft
- **WHEN** 用户提交 /effort high extra 或被当前冻结配置拒绝的 /effort max
- **THEN** 显示安全英文错误，原始草稿和光标可继续编辑，设置与 journal 不变

#### Scenario: Ignore a stale effort reply
- **WHEN** /effort 查询后用户已编辑新草稿、发起新 /context 或观察到较新选择，旧回复才到达
- **THEN** 不覆盖较新选择、草稿或通知，不抢占新面板；已接受选择仍由 owner 保留

#### Scenario: Keep literal text separate
- **WHEN** 用户输入 /efforts 或正文中间包含 /effort
- **THEN** 按既有普通文本路径处理，不改变 effort

#### Scenario: Keep query aliases and help local
- **WHEN** 用户分别提交 /effort current、/effort status、/effort unset 和 /effort --help
- **THEN** 前两者复用查询，unset 复用 auto；帮助只显示本地用法，零控制提交、网络和设置变化

#### Scenario: Preserve nonmodal keyboard priority
- **WHEN** effort 通知显示期间存在 queued 输入或活动 turn，用户按 Esc/Ctrl+C
- **THEN** 按原取回/取消优先级处理，不要求先关闭新面板；流式与 resize 不重开通知

#### Scenario: Do not undo an accepted choice after dismissing feedback
- **WHEN** low 已被 owner 接收，UI 反馈被取消，之后旧回执或新 Question overlay 到达
- **THEN** owner 仍保留 low，旧回执不清除新草稿或关闭 Question，下次查询能看见合法选择

#### Scenario: Separate observation errors from settings
- **WHEN** context 数值观测不可用，而 effort.query 仍返回合法选择
- **THEN** 快捷通知准确显示设置，/context 安全失败；不回滚选择、不展示旧 profile 数值
