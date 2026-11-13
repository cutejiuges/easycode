# tui/input-completion Specification

## Purpose

定义交互式输入中内置命令与显式 Skill 的可发现性、确定性候选选择和安全编辑行为，使用户能够在保留草稿与参数的前提下完成调用，并在两种 Provider、恢复、窄屏及取消场景下维持现有执行和持久化边界。

## Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/skills`、`/hooks`、`/plugins` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

内置命令 SHALL 优先占用同名 slash；同名 Skill SHALL 仍可在 `$` 列表发现。候选 SHALL 包含名称、简短说明，以及 Skill 的参数提示和不含绝对路径的来源；参数提示只供展示，不写入调用参数。不提供未实现命令。

插件候选 SHALL 使用完整 plugin:leaf 限定名，与 /plugins 和 /skills 来自所有策略、冲突及预算纳入后同一最终冻结目录，不提供裸名别名或冲突来源；Codex 有效策略禁止模型调用的合法手动项仍可补全。原有裸名 plugins Skill 仅在 $ 列表占用该名称，/plugins 为内置命令，/skills 选择这类保留名技能也必须填入 $name。总 Skill 候选仍最多 256 项，内置候选为六项。

#### Scenario: Discover commands and manual skills
- **WHEN** 用户分别输入 `/` 与 `$`
- **THEN** 前者显示当前内置命令及合法显式 Skill，后者只显示合法显式 Skill，包括禁止模型自行调用的条目

#### Scenario: Respect a broken winning source
- **WHEN** 同名低优先级 Skill 被非法高优先级来源遮蔽
- **THEN** 两种候选列表均不提供该低优先级 Skill，不重新选择来源

#### Scenario: Keep a colliding skill reachable
- **WHEN** 存在名为 cache 的可显式调用 Skill
- **THEN** `/cache` 只代表内置命令，`$cache` 代表该 Skill，两个列表均无同名歧义

#### Scenario: Discover qualified skills and plugin browser
- **WHEN** 用户分别查询 /plug、/qa: 与 $qa:
- **THEN** 可发现 plugins 内置命令与合法 qa 限定技能，manual-only 保持可见，禁用项不进入可接受候选

### Requirement: Completion applies to the editable first token only

补全 SHALL 只在首个非空白 token 以 `/` 或 `$` 开始且光标位于 sigil 之后、名称结尾之内时激活，包括名称末尾。前导空白允许保留；裸名编辑区仅允许最多 64 个 ASCII 字母、数字或连字符；插件限定名编辑区允许恰好一个冒号、两段各最多 64 个同类字符，总长最多 129 bytes。裸 sigil 允许空查询，编辑前缀可暂时以 qa: 这样的冒号结尾，但不能有第二个冒号或空 namespace。光标前名称片段 SHALL 作为查询，接受时 SHALL 替换整个名称 token，保持原 sigil。光标已进入参数、其他正文 token、代码围栏，或名称含额外路径分隔符等不合法字符时 SHALL 不激活。

接受候选 MUST 保留 token 之外的前导空白、既有分隔空白、参数、换行及尾部正文；没有分隔符时在调用后追加一个空格，已有分隔符不重复添加。光标 SHALL 落在已完成 token 后的首个合法分隔位置，不能落入字素内部。若替换使正文超过 4 MiB，MUST 原子拒绝并保留原草稿、光标、历史和选择，显示不含正文的安全英文错误。

#### Scenario: Complete a name while preserving arguments
- **WHEN** 草稿为 `  /rev old\n尾部  `，光标处于 rev 内，用户选择 review Skill
- **THEN** 结果为 `  /review old\n尾部  `，仅名称改变，光标处于名称后的分隔位置

#### Scenario: Leave ordinary paths and text alone
- **WHEN** 用户编辑 `/tmp/file`、`text $review`、代码围栏内文字或已进入 `/review args` 的参数区
- **THEN** 不出现可接受候选，不改变原有输入及提交语义

#### Scenario: Reject an oversized replacement
- **WHEN** 原草稿接近 4 MiB，接受较长名称及分隔符将超限
- **THEN** 替换整次失败，原文本、字节光标及历史状态不变，不提交

#### Scenario: Complete an unfinished plugin namespace
- **WHEN** 草稿为 /qa:re old，光标在名称内并选择 qa:review
- **THEN** 只替换完整名称 token，得到 /qa:review old，参数、分隔空白和换行保持不变，不加载技能

#### Scenario: Reject malformed qualified edit targets
- **WHEN** token 包含多层冒号、空 namespace、路径分隔符或超过 129 bytes
- **THEN** 不激活可接受候选，不把它截断后提交或解析为另一来源

### Requirement: Candidate search and selection are deterministic

名称匹配 SHALL 使用 ASCII 大小写无关查询，按完全匹配、前缀匹配、名称子序列、说明子串依次排序；只保留命中项。同一等级 SHALL 按内置命令优先、规范名称 ASCII 升序、来源身份升序稳定排序。空查询 SHALL 依相同类别及名称/来源次序提供全部候选。不使用时间、随机值、文件枚举顺序、历史调用次数或远端结果排序。

↑/↓ SHALL 在当前匹配列表循环选择；查询改变后仍命中的选择 SHALL 按稳定候选身份保留，否则选首项。目录或编辑目标改变时 SHALL 重新验证选择。无匹配项 SHALL 关闭候选，不吞掉普通 Enter、上下键或 Esc。

#### Scenario: Prefer a precise command name
- **WHEN** 查询与一个名称完全匹配，同时与其他名称前缀或说明命中
- **THEN** 完全匹配排第一，重复同样输入获得相同列表顺序

#### Scenario: Preserve a selected source under filtering
- **WHEN** 用户选择一个候选后继续输入，所选身份仍在结果中
- **THEN** 选择保持该候选；不再命中时选择新的首项

#### Scenario: Ignore discovery enumeration order
- **WHEN** 同一合法冻结目录以不同条目枚举顺序注入两个 TUI
- **THEN** 相同查询的候选、选择顺序和展示完全一致

### Requirement: Accepting and executing have explicit separate behavior

可见候选激活时，手动 Tab SHALL 只完成当前选项并关闭候选；不加载 Skill、不执行命令、不提交输入。普通 Enter SHALL 对内置命令先完成名称再交由现有完整输入提交规则处理，包括保留参数后的校验和现有错误。普通 Enter 对 Skill SHALL 只完成调用并关闭候选，沿用 `/skills` 的选择体验；后续独立 Enter 才走正常 Skill 准备与 admission。接受后的末尾空格 SHALL 使候选保持关闭直到用户重新编辑名称。

Alt+Enter、Ctrl+J 和反斜杠换行 SHALL 优先执行原有换行语义，不接受候选。当前合法名称因 Esc、粘贴或 modal 返回而被抑制，且有可显示匹配时，独立手动 Tab SHALL 只重新打开列表，本次不接受；其他无可见候选时 Tab SHALL 无操作。无可见候选时 Enter SHALL 沿用原有提交规则。任何接受动作最多产生一次既有提交，不建立独立执行通道。

#### Scenario: Fill without starting work
- **WHEN** 用户在 `/con` 的候选中用 Tab 选择 context
- **THEN** composer 成为 `/context `，Provider、Session 写入、context 查询及 Skill 读取次数均为零

#### Scenario: Dispatch a selected built-in once
- **WHEN** 用户在 `/con` 候选中普通 Enter 选择 context
- **THEN** 通过既有 context 查询路径执行一次，不向模型提交 `/con` 或 `/context`

#### Scenario: Edit a Skill before submitting
- **WHEN** 用户在 `$rev` 候选中普通 Enter 选择 review
- **THEN** composer 成为 `$review `，不加载正文；输入参数后独立 Enter 才准备 Skill 并提交

#### Scenario: Keep existing argument errors
- **WHEN** 光标回到 `/con unexpected` 名称区，并普通 Enter 选择 context
- **THEN** 现有本地命令校验报告不接受参数，完整草稿保持可编辑，不丢弃参数或提交给模型

#### Scenario: Reopen a dismissed target explicitly
- **WHEN** 用户用 Esc 关闭 `/con` 候选，在未编辑同一名称时按独立 Tab
- **THEN** 列表重新显示但正文和光标不变，随后独立 Tab 才接受当前选择

### Requirement: Completion respects modal focus paste and cancellation

审批、问卷、Hook 信任及浏览、Skills 列表、Plugins 列表及详情、上下文/缓存面板、transcript 浏览、提交/取回等待、取消清理期间 SHALL 保留既有独占焦点；补全不可见且不得处理它们的按键。普通聊天中候选可见时 Esc SHALL 仅关闭候选，保持草稿、光标、历史、输入队列与活动 turn 不变；相同名称未编辑时，流式事件或 resize MUST NOT 立即重开候选。关闭后独立 Esc SHALL 沿用现有取回/停止语义。普通 Ctrl+C SHALL 沿用活动 turn 的停止及空闲退出规则，不被补全吞掉。

显式标识为 paste 的事件 SHALL 先作为整体文本插入，内含 Tab、Enter、Esc、Ctrl+C 均不得接受候选、提交或取消。因该粘贴形成的当前目标 SHALL 暂不自动弹出候选，直到后续独立名称编辑或显式 Tab 请求；后续独立 Enter SHALL 直接执行现有提交规则，不将已粘贴文字改为相似候选。

#### Scenario: Dismiss while a turn and queue are active
- **WHEN** 有活动 turn、queued 输入及可见候选，用户按 Esc
- **THEN** 仅候选关闭；后续流式增量不重开它，再次 Esc 才按现有规则取回队列

#### Scenario: Isolate approval input
- **WHEN** 候选显示期间出现审批或问卷
- **THEN** 候选失去焦点且不可见，Tab/Enter/方向键/粘贴只由该独占界面处理

#### Scenario: Paste a command as literal text
- **WHEN** 候选打开时粘贴含换行、Tab 和控制字符的命令文本
- **THEN** 仅进行既有有界原子插入，内部按键无行为，后续独立 Enter 不接受旧候选

#### Scenario: Stop directly with control C
- **WHEN** 可见候选期间有活动 turn，用户按 Ctrl+C
- **THEN** 定向停止当前 turn，等待唯一 owner 完成清理，不先要求关闭候选

#### Scenario: Keep plugin navigation exclusive
- **WHEN** 从可见补全进入 /plugins 的列表或详情
- **THEN** 补全关闭且不处理面板按键，Esc 返回保持打开前草稿，流式事件不重开旧候选

#### Scenario: Preserve the actual pre completion command draft
- **WHEN** 用户以 Enter 接受 /plug 的 plugins 内置候选，再从插件面板取消，期间发生 resize
- **THEN** composer 恢复原 /plug 正文与光标，保持当前窗口布局，不保留补全附加空格、提交输入或自动重开候选

### Requirement: Suggestions are bounded safe and visible

候选 SHALL 在 composer 邻近区域显示，至多显示 5 个候选行及一行操作提示，超出部分通过选择滚动；选中项必须可见。布局 SHALL 将候选实际占用计入主线可用高度，优先保留 composer 光标和编辑内容。终端高度不足以容纳一个候选行时 SHALL 隐藏并停用候选按键，保留普通编辑。resize 不改变草稿、光标或候选身份。NO_COLOR 下 SHALL 以文本标记显示选择，普通和窄屏下均可区分命令、Skill 与来源。

候选描述、参数提示和来源 SHALL 使用既有可见转义规则处理 C0/C1、DEL、双向控制与终端转义；禁止输出可执行控制序列或将展示截断回写正文。只保留当前有界候选及布局，不因每次查询或流式增量累积旧结果。

#### Scenario: Browse on a narrow terminal
- **WHEN** 终端为 28x10，候选超过 5 项且存在长描述
- **THEN** 可滚动选择，当前项与光标可见，所有行受终端列宽限制，参数提示不进入正文

#### Scenario: Preserve editing on a tiny terminal
- **WHEN** 终端缩小到不足以显示一个候选行
- **THEN** 候选不显示也不截获按键，正文和字节光标不变

#### Scenario: Render hostile metadata without color
- **WHEN** Skill 说明含终端控制或双向字符且启用 NO_COLOR
- **THEN** 以可见转义与文本选择标记显示，不产生终端命令，不改变调用文本

### Requirement: Completion does not become execution or persistence state

发现、筛选、导航、关闭、Tab 接受和 Skill 的 Enter 接受 SHALL 只操作本地内存，不读取磁盘正文、发起网络、启动进程、写 Session 或改变 Runtime 权限。正式提交后 SHALL 继续使用现有 Skill 来源/digest 校验、冻结附件、预算、admission、Sync 和失败恢复路径。源变化或取消 SHALL 保留原输入供编辑，不通过补全建立执行或资源读取资格。

相同最终正文，无论由手动输入、不同尺寸的补全、关闭后重开或历史召回产生，SHALL 得到相同双 Provider 请求 canonical bytes、native item 顺序与稳定 fingerprint；恢复后继续请求 SHALL 与未中断路径等价。候选、查询、焦点、选择、隐藏原因及来源展示 MUST NOT 写入用户消息、Session/native/checkpoint 或缓存前缀。headless 与 stream-json SHALL 保持现有文本/控制契约，不识别终端补全按键。

#### Scenario: Detect a changed source at normal submission
- **WHEN** Skill 已被补全，但文件在正式提交前发生变化
- **THEN** 正常准备返回 skill_source_changed，不发送 Provider 请求，不持久化该输入，不重新发现或切换来源

#### Scenario: Match manual completed and restored requests
- **WHEN** 相同最终 Skill 输入分别手动键入及经补全后提交，并从已提交历史恢复
- **THEN** 两家 Provider 各自的 canonical 请求、原生项顺序和 fingerprint 一致，补全前未增加 journal records 或 Skill 读取
