## MODIFIED Requirements

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

## ADDED Requirements

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
