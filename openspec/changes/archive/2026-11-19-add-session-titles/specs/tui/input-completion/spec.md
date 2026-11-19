## MODIFIED Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks`、`/rename` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

内置命令 SHALL 优先占用同名 slash；同名 Skill SHALL 仍可在 `$` 列表发现。候选 SHALL 包含名称、简短说明，以及 Skill 的参数提示和不含绝对路径的来源；参数提示只供展示，不写入调用参数。不提供未实现命令。

插件候选 SHALL 使用完整 plugin:leaf 限定名，与 /plugins 和 /skills 来自所有策略、冲突及预算纳入后同一最终冻结目录，不提供裸名别名或冲突来源；Codex 有效策略禁止模型调用的合法手动项仍可补全。原有裸名 plugins Skill 仅在 $ 列表占用该名称，/plugins 为内置命令，/skills 选择这类保留名技能也必须填入 $name。总 Skill 候选仍最多 256 项，内置候选为九项；内置候选不得挤占原 Skill 上限。

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

#### Scenario: Discover rename without hiding the colliding skill
- **WHEN** 合法可显式调用 Skill 名为 rename，用户浏览 `/ren`、`$ren` 或从 `/skills` 选择该 Skill
- **THEN** slash 提供内置 rename，dollar 提供该 Skill，`/skills` 选择填入 `$rename`；原 256 项 Skill 上限不变

#### Scenario: Complete rename through the normal command path
- **WHEN** 会话空闲且无等待输入，用户对 `/ren` 的 rename 内置候选使用 Tab 或普通 Enter，或在 `/ren Next iteration` 的名称区接受候选
- **THEN** Tab 仅填入命令词和原有分隔符规则要求的空格；无参数时普通 Enter 打开标题编辑器，不保存标题；带参数的普通 Enter 保留完整标题参数并沿用同一验证和接纳规则，不重复保存或提交给模型

### Requirement: Completion respects modal focus paste and cancellation

审批、问卷、Hook 信任及浏览、Skills 列表、Plugins 列表及详情、上下文/缓存面板、任务完整清单面板、会话标题编辑及保存等待、transcript 浏览、提交/取回等待、取消清理期间 SHALL 保留各自的独占输入焦点；补全不可见且不得处理它们的按键。普通聊天中候选可见时 Esc SHALL 仅关闭候选，保持草稿、光标、历史、输入队列与活动 turn 不变；相同名称未编辑时，流式事件或 resize MUST NOT 立即重开候选。关闭后独立 Esc SHALL 沿用现有取回/停止语义。普通 Ctrl+C SHALL 沿用活动 turn 的停止及空闲退出规则，不被补全吞掉。

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

#### Scenario: Preserve exclusive rename editor focus and the original draft
- **WHEN** 空闲且无等待输入的会话中，用户以 Enter 接受 `/ren` 的 rename 内置候选，在标题编辑器内输入文字并取消
- **THEN** 补全不处理编辑器按键，composer 恢复原 `/ren` 正文与光标，保持候选关闭，取消没有保存标题或提交输入

### Requirement: File completion resolves the editable mention at the current cursor

文件 @ 补全 SHALL 在正文任意位置按当前光标的路径编辑区确定目标，共用显式文件引用语法；不得把命令/Skill 的首词限制扩大到文件，也不得改变 /、$ 的原有激活范围。目标 MUST 在字符和字素合法边界内，允许未闭合引号作为编辑中状态；email、转义、@@、行内/围栏代码不得激活。光标进入 #L 范围、引用之后或其他正文 MUST 不激活路径补全；已有范围与 token 外正文保留，不 fuzzy 修正范围。

九项精确内置命令的参数 SHALL 按原命令语义保持文字，文件目标识别前复用 domain 的同一保留名纯判断，不开启文件搜索或规则读取，不额外保存一套命令名。256 项 Skill 容量和同名 Skill 的 $ 入口 MUST 保持；`$effort`、`$rename` 等合法 Skill 输入的正文参数仍可明确附带文件，不能因为名称保留而禁用合法文件引用。

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
- **WHEN** 用户编辑或提交 `/rename Review @src/main.go`，或在会话标题编辑器中输入相同标题
- **THEN** 不开启文件候选、不读规则或文件、不冻结附件；`@` 和余下标题按标题契约处理，`$rename @src/main.go` 仍使用普通 Skill 输入的文件规则
