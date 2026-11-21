## MODIFIED Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks`、`/rename`、`/resume`、`/plan` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。`/plan` 的同名保留只作用于交互式 TUI。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

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
