## MODIFIED Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks`、`/resume` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

内置命令 SHALL 优先占用同名 slash；同名 Skill SHALL 仍可在 `$` 列表发现。候选 SHALL 包含名称、简短说明，以及 Skill 的参数提示和不含绝对路径的来源；参数提示只供展示，不写入调用参数。不提供未实现命令。

插件候选 SHALL 使用完整 plugin:leaf 限定名，与 /plugins 和 /skills 来自所有策略、冲突及预算纳入后同一最终冻结目录，不提供裸名别名或冲突来源；Codex 有效策略禁止模型调用的合法手动项仍可补全。原有裸名 plugins Skill 仅在 $ 列表占用该名称，/plugins 为内置命令，/skills 选择这类保留名技能也必须填入 $name。总 Skill 候选仍最多 256 项，内置候选为九项，内置命令不占 Skill 上限。

#### Scenario: Discover commands and manual skills
- **WHEN** 用户分别输入 `/` 与 `$`
- **THEN** 前者显示当前九项内置命令及合法显式 Skill，后者只显示合法显式 Skill，包括禁止模型自行调用的条目

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
- **THEN** slash 提供内置 tasks，dollar 提供该 Skill，`/skills` 选择填入 `$tasks`；内置候选共九项且原 256 项 Skill 上限不变

#### Scenario: Complete a tasks query through the existing command path
- **WHEN** 用户对 `/ta` 的 tasks 内置候选分别使用 Tab 和普通 Enter，或在 `/ta unexpected` 名称区接受该候选
- **THEN** Tab 只填入 `/tasks `，普通 Enter 只查询一次当前任务；既有参数完整保留，非法参数经本地命令校验拒绝，不向模型提交或重复查询

### Requirement: File completion resolves the editable mention at the current cursor

文件 @ 补全 SHALL 在正文任意位置按当前光标的路径编辑区确定目标，共用显式文件引用语法；不得把命令/Skill 的首词限制扩大到文件，也不得改变 /、$ 的原有激活范围。目标 MUST 在字符和字素合法边界内，允许未闭合引号作为编辑中状态；email、转义、@@、行内/围栏代码不得激活。光标进入 #L 范围、引用之后或其他正文 MUST 不激活路径补全；已有范围与 token 外正文保留，不 fuzzy 修正范围。

精确内置命令的参数 SHALL 按原命令语义保持文字，文件目标识别前沿用共同内置命令的保留名判断。交互专属 `/resume` 的参数同样不得开启文件搜索或规则读取，补全与提交使用同一个精确识别规则。既有内置命令、256 项 Skill 容量、同名 Skill 的 $ 入口 MUST 保持。通过 `$name` 或插件限定名调用的合法 Skill 仍可在交互正文参数中明确附带文件。

#### Scenario: Complete a file in the middle of a multiline draft
- **WHEN** 用户在多行正文中间的 @src/ma 光标处选择 src/main.go，后面已有行范围及文字
- **THEN** 只替换目标路径，行范围、其他正文和合法字素光标保持，尾部不被覆盖

#### Scenario: Keep command and Skill targeting unchanged
- **WHEN** 用户编辑 text $review、/tmp/file 或 /review args 的参数区
- **THEN** 原 /、$ 补全仍不激活，只有满足独立文件语法的 @ 目标可以开启文件候选

#### Scenario: Keep local effort and compaction arguments literal
- **WHEN** 用户编辑或提交 /effort @src/main.go、/compact 保留 @src/main.go 或其他精确内置命令的参数
- **THEN** 不开启文件候选、不读规则或文件；effort 参数按原契约拒绝，compact 仍处理原 instructions，不新增附件

#### Scenario: Keep an effort-named Skill available with files
- **WHEN** 合法同名 Skill 通过 $effort @src/main.go 调用，且展示目录已包含完整 256 项 Skill
- **THEN** @ 目标仍可查询及在正式提交后准备，/effort 内置候选和全部 Skill 容量保持，技能选择不误执行本地设置

#### Scenario: Keep file references available for a resume-named Skill
- **WHEN** 用户分别编辑精确内置 `/resume @src/ma` 和合法 Skill 调用 `$resume @src/ma`
- **THEN** 前者没有文件候选，普通提交时报告参数非法。后者按既有文件语义开启候选，正式提交时正常准备附件

## ADDED Requirements

### Requirement: Resume completion uses local execution and preserves the draft

`/resume` SHALL 只在交互聊天占用同名的裸 slash 入口。可手动调用的同名本地 Skill SHALL 通过 `$resume` 保持可用，`/skills` 选择时填入 `$resume`。同名插件 Skill 仍以完整限定名调用和补全。一次性 headless 与 stream-json 的输入均 SHALL 沿用既有显式 Skill 分派，合法的 `/resume` 或 `$resume` Skill 调用按原规则准备。无同名 Skill 时，普通输入和现有控制契约均保持原样。

Tab SHALL 仅补全首词，保留参数与分隔空白。用户显式普通提交才 SHALL 产生恢复意图，包括通过普通 Enter 接受内置候选后的同一次提交。交互聊天 SHALL 在 Skill 或文件引用准备之前识别完整首词 `/resume`。粘贴 SHALL 只编辑文本，粘贴事件中的控制键不得触发恢复。Alt+Enter、Ctrl+J 和反斜杠续行 MUST 先按原规则插入换行。无效参数或忙碌拒绝 SHALL 显示本地英文反馈，保留可编辑草稿，命令不得进入模型或输入队列。

从补全打开选择器或直接恢复进度时，取消或可恢复失败返回 SHALL 还原接受候选前的准确正文和光标，并使用当前终端布局。恢复期间候选不可见且不接收按键。返回原聊天不会自动重开旧候选。

#### Scenario: Discover resume while keeping manual skills reachable
- **WHEN** 冻结目录包含名为 resume 的可显式调用 Skill 和名为 qa:resume 的插件 Skill，用户查询 `/res`、`$res` 或 `/qa:res`
- **THEN** 分别可选内置命令、裸名 Skill 和插件的完整限定名。经 `/skills` 选择裸名 Skill 填入 `$resume`

#### Scenario: Complete once and restore the pre-completion draft on cancel
- **WHEN** 用户在 `/res` 的内置候选上按 Tab，或在同样的原草稿上按 Enter 后取消选择器
- **THEN** Tab 仅填入 `/resume `。Enter 打开一次选择器，取消后恢复 `/res` 和原光标，未提交对话输入或打开旧候选

#### Scenario: Keep malformed resume arguments out of model input
- **WHEN** 用户在名称区补全并提交含多余参数或文件引用样式参数的 `/res`
- **THEN** 保留参数并报告本地命令错误，未启动恢复、准备 Skill、读取文件或向模型提交

#### Scenario: Do not switch on paste or multiline editing
- **WHEN** 用户粘贴 `/resume` 及换行，或使用 Alt+Enter、Ctrl+J、反斜杠续行编辑命令草稿
- **THEN** 按现有规则更新草稿，直到用户之后显式普通提交才执行参数校验和本地恢复入口

#### Scenario: Preserve explicit skills in both headless paths
- **WHEN** 用户在一次性 headless 和 stream-json 分别提交合法的 `/resume` 或 `$resume` Skill 调用
- **THEN** 两个入口按原指令准备同一 Skill 并继续正常输入，从未运行终端恢复动作
