## MODIFIED Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

内置命令 SHALL 优先占用同名 slash；同名 Skill SHALL 仍可在 `$` 列表发现。候选 SHALL 包含名称、简短说明，以及 Skill 的参数提示和不含绝对路径的来源；参数提示只供展示，不写入调用参数。不提供未实现命令。

插件候选 SHALL 使用完整 plugin:leaf 限定名，与 /plugins 和 /skills 来自所有策略、冲突及预算纳入后同一最终冻结目录，不提供裸名别名或冲突来源；Codex 有效策略禁止模型调用的合法手动项仍可补全。原有裸名 plugins Skill 仅在 $ 列表占用该名称，/plugins 为内置命令，/skills 选择这类保留名技能也必须填入 $name。总 Skill 候选仍最多 256 项，内置候选为八项；增加 tasks 内置项不得挤占原 Skill 上限。

#### Scenario: Discover commands and manual skills
- **WHEN** 用户分别输入 `/` 与 `$`
- **THEN** 前者显示当前八项内置命令及合法显式 Skill，后者只显示合法显式 Skill，包括禁止模型自行调用的条目

#### Scenario: Respect a broken winning source
- **WHEN** 同名低优先级 Skill 被非法高优先级来源遮蔽
- **THEN** 两种候选列表均不提供该低优先级 Skill，不重新选择来源

#### Scenario: Keep a colliding skill reachable
- **WHEN** 存在名为 cache 的可显式调用 Skill
- **THEN** `/cache` 只代表内置命令，`$cache` 代表该 Skill，两个列表均无同名歧义

#### Scenario: Discover qualified skills and plugin browser
- **WHEN** 用户分别查询 /plug、/qa: 与 $qa:
- **THEN** 可发现 plugins 内置命令与合法 qa 限定技能，manual-only 保持可见，禁用项不进入可接受候选

#### Scenario: Complete effort without hiding a skill
- **WHEN** 合法本地 Skill 名为 effort，用户浏览 /eff、$eff 或从 /skills 选择该 Skill
- **THEN** /eff 提供内置 effort，$eff 提供技能，/skills 选择填入 $effort；Tab 只填入，内置 Enter 只经正常命令路径查询一次

#### Scenario: Preserve arguments when accepting effort
- **WHEN** 草稿为 /eff status 或 /eff high extra，用户在名称区接受 effort 内置候选
- **THEN** 仅补全首词，保留全部参数；status 走同一查询，非法多参数仍安全拒绝且草稿保持，不生成参数候选或重复通知

#### Scenario: Discover tasks without hiding the colliding skill
- **WHEN** 合法可显式调用 Skill 名为 tasks，用户浏览 `/ta`、`$ta` 或从 `/skills` 选择该 Skill
- **THEN** slash 提供内置 tasks，dollar 提供该 Skill，`/skills` 选择填入 `$tasks`；内置候选共八项且原 256 项 Skill 上限不变

#### Scenario: Complete a tasks query through the existing command path
- **WHEN** 用户对 `/ta` 的 tasks 内置候选分别使用 Tab 和普通 Enter，或在 `/ta unexpected` 名称区接受该候选
- **THEN** Tab 只填入 `/tasks `，普通 Enter 只查询一次当前任务；既有参数完整保留，非法参数经本地命令校验拒绝，不向模型提交或重复查询

### Requirement: Completion respects modal focus paste and cancellation

审批、问卷、Hook 信任及浏览、Skills 列表、Plugins 列表及详情、上下文/缓存面板、任务完整清单面板、transcript 浏览、提交/取回等待、取消清理期间 SHALL 保留既有独占焦点；补全不可见且不得处理它们的按键。普通聊天中候选可见时 Esc SHALL 仅关闭候选，保持草稿、光标、历史、输入队列与活动 turn 不变；相同名称未编辑时，流式事件或 resize MUST NOT 立即重开候选。关闭后独立 Esc SHALL 沿用现有取回/停止语义。普通 Ctrl+C SHALL 沿用活动 turn 的停止及空闲退出规则，不被补全吞掉。

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

#### Scenario: Preserve exclusive task panel focus and the actual draft
- **WHEN** 用户以 Enter 接受 `/ta` 的 tasks 内置候选，在任务面板导航、接收更新和 resize 后取消返回
- **THEN** 补全不处理面板按键，composer 恢复原 `/ta` 正文与光标并采用当前布局，不保留接受时附加的空格、不提交输入也不自动重开旧候选

### Requirement: Suggestions are bounded safe and visible

候选 SHALL 在 composer 邻近区域显示，至多显示 5 个候选行及一行操作提示，超出部分通过选择滚动；选中项必须可见。布局 SHALL 将候选实际占用计入主线可用高度，优先保留 composer 光标和编辑内容。终端高度不足以容纳一个候选行时 SHALL 隐藏并停用候选按键，保留普通编辑。resize 不改变草稿、光标或候选身份。NO_COLOR 下 SHALL 以文本标记显示选择，普通和窄屏下均可区分命令、Skill 与来源。

候选、当前任务预览、主线、输入队列及固定状态 SHALL 使用同一终端总高度预算；composer 光标/编辑内容、队列、固定状态和高优先审批/问卷/Hook 审查优先，其次为用户正在操作的补全。可见补全期间任务预览 SHALL 降为最多一行统计，空间不足时隐藏，不能占用让当前选中候选可见所需的行。Ctrl+T、新任务快照、队列或状态变化及 resize SHALL 根据任务与候选实际可见行数共同刷新布局及缓存身份，不因各自使用旧剩余高度产生溢出、隐藏选择或更换候选身份。任务区域因补全暂时压缩不改变已提交任务事实或用户此前的展开选择。

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

#### Scenario: Preserve a selected completion with a full task list
- **WHEN** 有 20 项当前任务、5 项可匹配候选、多行草稿、排队输入和固定状态，用户在 40x12 或更小终端选择候选、resize 并按 Ctrl+T
- **THEN** 总布局受终端高度约束，composer/队列/状态先保留，补全选中项在可容纳候选时始终可见，任务最多一行或隐藏；补全身份、草稿、光标和任务事实不变，不能靠重复占用剩余高度挤掉选择

#### Scenario: Recalculate combined height after an asynchronous task update
- **WHEN** 用户正在补全名称时新任务更新、队列或固定状态改变实际可用高度
- **THEN** 共享布局和缓存身份重新计算，任务不会重新展开挤走候选；空间不足一个候选行时沿用安全停用规则，之后恢复空间仍保持可验证的候选身份与原草稿
