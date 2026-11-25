## MODIFIED Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks`、`/rename`、`/resume`、`/plan`、`/branch`、`/fork`、`/rewind` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。`/plan`、`/branch`、`/fork` 和 `/rewind` 的同名保留只作用于交互式 TUI。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

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

精确内置命令的参数 SHALL 按原命令语义保持文字，文件目标识别前沿用共同内置命令的保留名判断。交互专属 `/resume`、`/branch`、`/fork`、`/rewind` 的参数同样不得开启文件搜索或规则读取，补全与提交使用同一个精确识别规则。保留原首词的历史草稿按 `tui/rewind` 感知正文或显式 Skill 意图，仍遵循文件语法中的跨宿主保留名限制。既有内置命令、256 项 Skill 容量、同名 Skill 的 $ 入口 MUST 保持。通过 `$name` 或插件限定名调用的合法 Skill 仍可在交互正文参数中明确附带文件。

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

### Requirement: Rewind completion opens the local picker and preserves the draft

`/rewind` SHALL 提供无参数的内置候选，只在交互聊天保留裸 slash 首词，并在 Skill 与文件引用准备前识别。可显式调用的同名 Skill SHALL 通过 `$rewind` 发现和调用，`/skills` 选择该 Skill 时填入同一入口，插件 Skill 使用完整限定名。一次性 headless 和 stream-json SHALL 沿用现有输入分派，合法显式 Skill 正常准备，普通输入继续提交给模型。

Tab SHALL 只补全首词并保持原空白和参数。仅显式普通提交才 SHALL 走本地命令校验，其中普通 Enter 接受内置候选时允许同一次操作提交。完整后缀仅含空白时按无参数处理，非空白参数 SHALL 本地拒绝并输出安全英文反馈。粘贴、Alt+Enter、Ctrl+J 及反斜杠续行继续按现有规则编辑草稿。内置命令及非法参数不得进入输入回顾、队列、附件准备或模型。

取消或可恢复失败时 SHALL 还原普通命令提交前的草稿和字素光标。命令由补全提交时，SHALL 还原到接受候选前的草稿状态。选择器、确认和创建期间不得激活补全，返回源会话时不得自动打开旧候选。激活目标并回填草稿时 SHALL 像粘贴一样抑制命令和文件候选，回填正文尚保留原首词意图时，命令候选不得替换该首词或抢占发送。

#### Scenario: Discover rewind without hiding the colliding skill
- **WHEN** 冻结目录包含合法的 rewind 和 qa:rewind Skill，用户查询 `/rew`、`$rew` 或 `/qa:rew`
- **THEN** 分别给出内置命令、裸名 Skill 和插件限定名，`/skills` 选择裸名 Skill 时填入 `$rewind`

#### Scenario: Open rewind once and recover the pre-completion draft
- **WHEN** 用户在 `/rew` 候选上按 Tab，或在同样的原草稿上按普通 Enter 后取消选择器
- **THEN** Tab 只填入 `/rewind `，普通 Enter 打开一次选择器，取消后恢复 `/rew` 和原光标，输入回顾和提交次数不变

#### Scenario: Keep rewind arguments local and explicit Skill references available
- **WHEN** 用户编辑并提交 `/rewind @src/ma`，或通过 `$rewind @src/ma` 调用同名合法 Skill
- **THEN** 前者不触发文件候选和附件准备，普通提交时本地拒绝。后者按既有 Skill 和文件引用规则提供候选及准备附件

#### Scenario: Preserve paste and headless input semantics
- **WHEN** 用户粘贴含 `/rewind` 和换行的草稿，或一次性 headless 或 stream-json 收到同名显式 Skill 调用或普通输入
- **THEN** 粘贴仅更新草稿，两个 headless 入口沿用现有分派，均不打开选择器或创建分支

#### Scenario: Do not complete a recalled command-like word on activation
- **WHEN** 历史分支激活并填入以交互命令名起始的旧正文，或在已填入的文件引用处开始编辑
- **THEN** 回填本身没有命令或文件候选，也不读取工作区。编辑后可在当前草稿意图及文件语法允许时查询文件，首次发送仍使用经确认的正文或 Skill 意图
