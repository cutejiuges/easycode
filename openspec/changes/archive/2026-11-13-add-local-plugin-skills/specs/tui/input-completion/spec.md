## MODIFIED Requirements

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
