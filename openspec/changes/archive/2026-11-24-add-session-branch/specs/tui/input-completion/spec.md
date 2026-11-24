## MODIFIED Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks`、`/rename`、`/resume`、`/plan`、`/branch`、`/fork` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。`/plan`、`/branch` 和 `/fork` 的同名保留只作用于交互式 TUI。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

内置命令 SHALL 优先占用同名 slash；同名 Skill SHALL 仍可在 `$` 列表发现。候选 SHALL 包含名称、简短说明，以及 Skill 的参数提示和不含绝对路径的来源；参数提示只供展示，不写入调用参数。不提供未实现命令。

插件候选 SHALL 使用完整 plugin:leaf 限定名，与 /plugins 和 /skills 来自所有策略、冲突及预算纳入后同一最终冻结目录，不提供裸名别名或冲突来源；Codex 有效策略禁止模型调用的合法手动项仍可补全。原有裸名 plugins Skill 仅在 $ 列表占用该名称，/plugins 为内置命令，/skills 选择这类保留名技能也必须填入 $name。总 Skill 候选仍最多 256 项，内置候选不得挤占 Skill 上限。

#### Scenario: Discover commands and manual skills
- **WHEN** 用户分别输入 `/` 与 `$`
- **THEN** 前者显示当前可用的内置命令及合法显式 Skill，后者只显示合法显式 Skill，包括禁止模型自行调用的条目

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
- **THEN** slash 提供内置 tasks，dollar 提供该 Skill，`/skills` 选择填入 `$tasks`，原 256 项 Skill 上限保持

#### Scenario: Complete a tasks query through the existing command path
- **WHEN** 用户对 `/ta` 的 tasks 内置候选分别使用 Tab 和普通 Enter，或在 `/ta unexpected` 名称区接受该候选
- **THEN** Tab 只填入 `/tasks `，普通 Enter 只查询一次当前任务；既有参数完整保留，非法参数经本地命令校验拒绝，不向模型提交或重复查询

#### Scenario: Discover rename without hiding the colliding skill
- **WHEN** 合法可显式调用 Skill 名为 rename，用户浏览 `/ren`、`$ren` 或从 `/skills` 选择该 Skill
- **THEN** slash 提供内置 rename，dollar 提供该 Skill，`/skills` 选择填入 `$rename`；原 256 项 Skill 上限不变

#### Scenario: Complete rename through the normal command path
- **WHEN** 会话空闲且无等待输入，用户对 `/ren` 的 rename 内置候选使用 Tab 或普通 Enter，或在 `/ren Next iteration` 的名称区接受候选
- **THEN** Tab 仅填入命令词和原有分隔符规则要求的空格；无参数时普通 Enter 打开标题编辑器，不保存标题；带参数的普通 Enter 保留完整标题参数并沿用同一验证和接纳规则，不重复保存或提交给模型

#### Scenario: Discover plan without hiding the colliding skill
- **WHEN** 合法可显式调用的 Skill 名为 plan，用户浏览 `/pla`、`$pla` 或从 `/skills` 选择该 Skill
- **THEN** slash 提供内置 plan，dollar 提供该 Skill，`/skills` 选择填入 `$plan`，原 256 项 Skill 上限保持

#### Scenario: Complete plan through the existing command path
- **WHEN** 用户通过 Tab 或普通 Enter 接受 `/pla` 的内置 Plan 候选，或在 `/pla off` 的命令词上接受候选
- **THEN** Tab 只补全名称并保留参数，普通 Enter 经本地 Plan 命令规则请求一次状态或切换，错误不转换为模型输入

#### Scenario: Keep at signs in local plan arguments literal
- **WHEN** 在 TUI 编辑 `/plan @src` 或 `$plan @src`，且存在可显式调用的同名 Skill
- **THEN** 内置命令参数中的 `@` 不产生文件发现或读取，非法参数在本地拒绝；显式 Skill 的 `@` 按既有文件引用流程处理

### Requirement: File completion resolves the editable mention at the current cursor

文件 @ 补全 SHALL 在正文任意位置按当前光标的路径编辑区确定目标，共用显式文件引用语法；不得把命令/Skill 的首词限制扩大到文件，也不得改变 /、$ 的原有激活范围。目标 MUST 在字符和字素合法边界内，允许未闭合引号作为编辑中状态；email、转义、@@、行内/围栏代码不得激活。光标进入 #L 范围、引用之后或其他正文 MUST 不激活路径补全；已有范围与 token 外正文保留，不 fuzzy 修正范围。

精确内置命令的参数 SHALL 按原命令语义保持文字，文件目标识别前沿用共同内置命令的保留名判断。交互专属 `/resume`、`/branch`、`/fork` 的参数同样不得开启文件搜索或规则读取，补全与提交使用同一个精确识别规则。既有内置命令、256 项 Skill 容量、同名 Skill 的 $ 入口 MUST 保持。通过 `$name` 或插件限定名调用的合法 Skill 仍可在交互正文参数中明确附带文件。

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

#### Scenario: Keep a title containing an at sign literal
- **WHEN** 用户编辑或提交 `/rename Review @src/main.go`、`/branch Review @src/main.go` 或 `/fork Review @src/main.go`，或在会话标题编辑器中输入相同标题
- **THEN** 不开启文件候选、不读规则或文件、不冻结附件；`@` 和余下标题按标题契约处理，合法的 `$rename`、`$branch`、`$fork` Skill 调用仍使用普通 Skill 输入的文件规则

#### Scenario: Keep file references available for a resume-named Skill
- **WHEN** 用户分别编辑精确内置 `/resume @src/ma` 和合法 Skill 调用 `$resume @src/ma`
- **THEN** 前者没有文件候选，普通提交时报告参数非法。后者按既有文件语义开启候选，正式提交时正常准备附件

## ADDED Requirements

### Requirement: Branch completion uses local execution and preserves the draft

`/branch` 与 `/fork` SHALL 分别提供内置候选，使用同一个本地分支语义。分支命令及其标题不进入聊天输入回顾。两个精确首词只在交互聊天占用同名裸 slash 入口，并 SHALL 在 Skill 或文件引用准备前识别。可手动调用的同名本地 Skill SHALL 通过 `$branch`、`$fork` 保持可用，`/skills` 选择时填入相应美元入口。插件 Skill 仍以完整限定名调用和补全。一次性 headless 和 stream-json 中相同文本 SHALL 沿原分派规则走显式 Skill 或普通输入。非交互协议不提供分支创建操作。

Tab SHALL 只补全首词，保留原参数与分隔空白。普通 Enter 接受内置候选 SHALL 与手工提交走相同本地验证和准入，并且只提交一次。标题校验 SHALL 首先检查首词后的完整原始文本，包括外围字符，再按 session/titles 的 Unicode `Zs` 规则规范化。空参数或仅含 `Zs` 视为省略标题。引号、`@` 和明确标题末尾的反斜杠按字面处理。含禁用字符的参数 SHALL 以安全英文诊断拒绝。粘贴、Alt+Enter 与 Ctrl+J SHALL 只编辑输入，分支命令不进入模型、队列、附件或输入回顾。

提交后取消或可恢复失败，包括无效参数和忙碌拒绝，SHALL 按当前终端尺寸保留实际提交前的草稿与字素光标，补全提交时 SHALL 还原接受候选之前的内容。分支准备期间候选不得接收输入，返回时不得自动重开旧候选。

#### Scenario: Discover both branch spellings without hiding manual Skills
- **WHEN** 冻结目录包含可显式调用的 branch、fork 和 qa:fork Skill，用户查询 `/bra`、`/for`、`$bra`、`$for` 或 `/qa:for`
- **THEN** slash 裸名命中所对应的分支内置候选，dollar 命中对应本地 Skill，插件限定名仍可选。`/skills` 选择两个同名本地 Skill 时分别填入 `$branch` 与 `$fork`

#### Scenario: Complete once and preserve title and pre-completion draft
- **WHEN** 用户在 `/bra Review @src "v2"` 的命令词上按 Tab 或普通 Enter 接受候选，普通 Enter 提交因忙碌、取消或可恢复失败而未能切换
- **THEN** Tab 只补全命令词，普通 Enter 至多产生一次本地请求，标题按原参数解析。未切换成功时草稿与光标恢复到接受候选前，不读取附件或发送模型请求

#### Scenario: Validate the complete suffix before applying a default title
- **WHEN** 用户已解码粘贴含末尾 Tab 或换行的 `/branch` 草稿，之后按普通 Enter
- **THEN** 粘贴本身不创建分支，提交时拒绝整个非法参数，不能因裁掉控制字符创建默认标题分支，草稿仍可编辑

#### Scenario: Preserve headless input routing
- **WHEN** 一次性 headless 或 stream-json 提交 `/branch`、`/fork` 或相应美元入口
- **THEN** 合法显式 Skill 按既有规则准备。其他普通输入继续经过原输入语义，所有情况均不创建分支
