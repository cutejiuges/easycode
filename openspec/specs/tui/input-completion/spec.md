# tui/input-completion Specification

## Purpose

定义交互式输入中内置命令与显式 Skill 的可发现性、确定性候选选择和安全编辑行为，使用户能够在保留草稿与参数的前提下完成调用，并在两种 Provider、恢复、窄屏及取消场景下维持现有执行和持久化边界。

## Requirements

### Requirement: Completion offers only available explicit actions

TUI SHALL 为 `/` 提供已实现的 `/cache`、`/context`、`/compact`、`/effort`、`/skills`、`/hooks`、`/plugins`、`/tasks`、`/rename`、`/resume` 和可显式调用 Skill，为 `$` 仅提供可显式调用 Skill。Skill SHALL 来自本会话冻结的展示目录，仅包含状态 available、user-invocable=true 的获胜条目；disable-model-invocation=true 不得排除合法手动调用。invalid、unsupported、shadowed、unavailable 和 model-only 条目 MUST NOT 成为可接受候选。

内置命令 SHALL 优先占用同名 slash；同名 Skill SHALL 仍可在 `$` 列表发现。候选 SHALL 包含名称、简短说明，以及 Skill 的参数提示和不含绝对路径的来源；参数提示只供展示，不写入调用参数。不提供未实现命令。

插件候选 SHALL 使用完整 plugin:leaf 限定名，与 /plugins 和 /skills 来自所有策略、冲突及预算纳入后同一最终冻结目录，不提供裸名别名或冲突来源；Codex 有效策略禁止模型调用的合法手动项仍可补全。原有裸名 plugins Skill 仅在 $ 列表占用该名称，/plugins 为内置命令，/skills 选择这类保留名技能也必须填入 $name。总 Skill 候选仍最多 256 项，十项内置候选不得挤占 Skill 上限。

#### Scenario: Discover commands and manual skills
- **WHEN** 用户分别输入 `/` 与 `$`
- **THEN** 前者显示当前十项内置命令及合法显式 Skill，后者只显示合法显式 Skill，包括禁止模型自行调用的条目

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

### Requirement: Completion applies to the editable first token only

命令与 Skill 的 /、$ 补全 SHALL 只在首个非空白 token 以 `/` 或 `$` 开始且光标位于 sigil 之后、名称结尾之内时激活，包括名称末尾。前导空白允许保留；裸名编辑区仅允许最多 64 个 ASCII 字母、数字或连字符；插件限定名编辑区允许恰好一个冒号、两段各最多 64 个同类字符，总长最多 129 bytes。裸 sigil 允许空查询，编辑前缀可暂时以 qa: 这样的冒号结尾，但不能有第二个冒号或空 namespace。光标前名称片段 SHALL 作为查询，接受时 SHALL 替换整个名称 token，保持原 sigil。光标已进入参数、其他正文 token、代码围栏，或名称含额外路径分隔符等不合法字符时 SHALL 不激活。

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

命令与 Skill 的名称匹配 SHALL 使用 ASCII 大小写无关查询，按完全匹配、前缀匹配、名称子序列、说明子串依次排序；只保留命中项。同一等级 SHALL 按内置命令优先、规范名称 ASCII 升序、来源身份升序稳定排序。空查询 SHALL 依相同类别及名称/来源次序提供全部候选。不使用时间、随机值、文件枚举顺序、历史调用次数或远端结果排序。

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

### Requirement: Completion does not become execution or persistence state

命令/Skill 的发现、筛选、导航、关闭、Tab 接受和 Skill 的 Enter 接受 SHALL 只操作本地内存，不读取磁盘正文、发起网络、启动进程、写 Session 或改变 Runtime 权限。正式提交后 SHALL 继续使用现有 Skill 来源/digest 校验、冻结附件、预算、admission、Sync 和失败恢复路径。源变化或取消 SHALL 保留原输入供编辑，不通过补全建立执行或资源读取资格。

在相同已提交事实、上下文来源、实际冻结附件及操作捕获的完整推理设置下，相同最终正文，无论由手动输入、不同尺寸的补全、关闭后重开或历史召回产生，SHALL 得到相同双 Provider 请求 canonical bytes、native item 顺序与稳定 fingerprint；恢复后显式重选相同设置再继续请求 SHALL 与未中断路径等价，进程选择序号和来源可以不同。不同磁盘内容或实际设置不在该请求等价承诺内，不能为了等价重读历史附件或恢复旧 effort。候选、查询、焦点、选择、隐藏原因及来源展示 MUST NOT 写入用户消息、Session/native/checkpoint 或缓存前缀。headless 与 stream-json SHALL 保持现有文本/控制契约，不识别终端补全按键。

#### Scenario: Detect a changed source at normal submission
- **WHEN** Skill 已被补全，但文件在正式提交前发生变化
- **THEN** 正常准备返回 skill_source_changed，不发送 Provider 请求，不持久化该输入，不重新发现或切换来源

#### Scenario: Match manual completed and restored requests
- **WHEN** 相同最终 Skill 输入分别手动键入及经补全后提交，并从已提交历史恢复
- **THEN** 两家 Provider 各自的 canonical 请求、原生项顺序和 fingerprint 一致，补全前未增加 journal records 或 Skill 读取

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

#### Scenario: Keep a title containing an at sign literal
- **WHEN** 用户编辑或提交 `/rename Review @src/main.go`，或在会话标题编辑器中输入相同标题
- **THEN** 不开启文件候选、不读规则或文件、不冻结附件；`@` 和余下标题按标题契约处理，`$rename @src/main.go` 仍使用普通 Skill 输入的文件规则

#### Scenario: Keep file references available for a resume-named Skill
- **WHEN** 用户分别编辑精确内置 `/resume @src/ma` 和合法 Skill 调用 `$resume @src/ma`
- **THEN** 前者没有文件候选，普通提交时报告参数非法。后者按既有文件语义开启候选，正式提交时正常准备附件

### Requirement: File candidates are bounded asynchronous metadata with generation validation

文件候选 SHALL 通过显式启动的单个宿主搜索 owner 提供，不读取待附带文件正文，不在构造/getter/render 中扫描、启动进程或联网；规则文件只作为工作区发现配置读取。一次候选 session SHALL 至多持有一个目录发现和一个最新待处理查询，TUI 只持有一个结果等待路径，不为每按键创建长期等待任务。查询 MUST 为合法 UTF-8，解码后至多 4096 bytes，引号原始编辑片段至多 24578 bytes；超限不得裁成另一条路径。

结果 MUST 同时匹配工作区/session generation、草稿 revision、路径范围、请求代次及有效 index revision；文件查询代次独立于 effort 的进程序号或观测水位。新查询一经发出，旧显示结果 MUST 立即失去接受资格；仅过滤迟到结果不足以保证安全。旧行可作为明确 loading 展示，不能在当前查询尚未收到结果时通过 Tab/Enter 接受。有效新结果到达时按完整路径身份保留仍命中的选择，否则选首项；旧 revision 和 ABA 结果不能覆盖较新快照。

搜索 SHALL 跳过 VCS 元数据、symlink 和特殊文件。深度至多 64、原始总条目至多 200000，名称读取每批至多 256，单目录至多 4096 名称/1 MiB，在途名称合计至多 8 MiB。任一枚举额度在当前目录读完前耗尽 SHALL 整目录省略，不能使用其随机部分名称或先无界枚举再截断；完整目录名称排序后才处理子项。

匹配 SHALL 按完整路径精确、完整路径前缀、文件名前缀、路径子串、路径子序列分级，再按规范相对路径排序。ASCII 大小写只影响候选匹配，不改变真实路径或规范化 Unicode。候选 MUST 仅包含当前引用语法可表达的普通文件路径；非法 UTF-8、控制字符、反斜杠和超长名称跳过并记入省略统计，不用替换字符生成另一条路径。文件名发现不承诺正文可读取或是 UTF-8，最终仍在提交时验证。最多保留排名前 50 项/128 KiB；空查询提供有界列表，完整手输提交不依赖候选命中。候选与查询身份不进入 Session、prompt 或 cache sources。

#### Scenario: Discard an old result for a repeated query
- **WHEN** 查询 A、B、A 先后产生，第一次 A 的结果最后到达
- **THEN** 只有当前代次结果可显示，旧 A 不能因为 query 相同而被接受

#### Scenario: Bound a very large directory before allocation
- **WHEN** 某目录超过单目录名称或字节限额
- **THEN** 有限批次停止并整目录省略，结果标记不完整，其他确定性可遍历目录继续；无无界名称数组

#### Scenario: Match file names deterministically
- **WHEN** 相同可接受路径集合以不同枚举顺序返回
- **THEN** 相同查询的分级、规范路径次序及可接受候选相同，显示截短不改变完整插入值

#### Scenario: Disable the displayed previous query before the new result arrives
- **WHEN** A 的列表仍显示，用户已输入查询 B 并立即按 Tab 或 Enter
- **THEN** A 的任何路径都不能写入 B 的目标，无本代可接受项时普通 Enter 走既有提交规则且失败保持草稿

### Requirement: File acceptance preserves existing focus and requires a separate submission

本代可接受文件候选的 Tab/Enter SHALL 只填入合法引用并关闭列表，至多发生一次原子编辑。路径插入 MUST 与共享解析规则往返相等；空格/#/双引号/反引号、前导 @ 等冲突名称使用规范 JSON 引号，不能只检查空格而插入 @@ 这种原文 token，已有范围不改写。接受后光标 MUST 越过结束引号与整个范围，到引用后的合法分隔边界；无分隔时加一个空格，有分隔不重复添加，不进入组合字素。候选保持接受抑制，替换超 4 MiB 必须整次拒绝。后续独立 Enter 才通过正常输入准备和 admission 读取文件。候选显示、移动、接受和关闭不能产生附件正文读取、Tool 调用、Session 写入或权限提升。

文件候选 SHALL 复用现有最多五行及操作提示、NO_COLOR、可见转义、滚动与窄屏布局，与命令候选互斥并共用 chat_space 中 composer、可见队列、状态及 effort 短通知的真实行预算。terminal 无法容纳候选时停用候选按键，保留输入区与 FIFO 队首，不能因展示裁剪删除排队事实。effort 通知不是 modal，其快照本身不重开文件候选或取消有效查询。审批、问卷、Hook/Skills/Plugins/其他面板、详情、pending/取消保持焦点优先；paste 整体插入且抑制候选，Alt+Enter/Ctrl+J 保留换行，Ctrl+C 保留停止/取消；Esc 只关闭当前列表，再次 Esc 才走既有取回/停止。流式增量和 resize 不能重开被关闭目标。

文件接受、取回/returned 恢复和历史召回 MUST 经过与手工编辑一致的 composer 失效路径；旧文件结果及旧 effort 局部反馈不能改写新草稿、焦点或活动状态。effort 快照仍可按既有选择水位更新权威选择信息，不能因其序号相同就清空或恢复新的文件输入。

#### Scenario: Fill then separately submit a file reference
- **WHEN** 用户在文件候选上按 Enter，然后再独立 Enter
- **THEN** 第一次只填入路径且附件正文读取为零，光标已退出完整引用；第二次才准备一次冻结附件并正常提交

#### Scenario: Preserve a range and leave the completed mention
- **WHEN** 在 @"docs/a b.md"#L2-5 的路径内选择新文件，引用之后有分隔与尾部文字
- **THEN** 引号路径被规范替换，#L2-5 和尾部保持，光标位于范围后的合法分隔边界，立即再 Enter 不重开或再次接受候选

#### Scenario: Edit a line range without accepting a file
- **WHEN** 光标在 @src/main.go#L10-20 的数字或范围区
- **THEN** 路径候选不激活且 Enter 不自动替换路径，非法范围仅在正常提交时按引用规则拒绝

#### Scenario: Share a narrow viewport with effort and queued inputs
- **WHEN** effort 状态或帮助尚可见，另有多行草稿、64 条待处理输入和文件候选，终端缩至 28x10、20x5 或 1x1
- **THEN** 输入和可容纳的 FIFO 队首优先、总行列不越界、隐藏候选不可接受；恢复尺寸后排队事实和完整草稿保持

#### Scenario: Ignore local feedback after a new file edit
- **WHEN** 旧 effort 查询或设置的结果/快照迟到，而用户已接受文件路径、取回输入或召回历史并形成新草稿
- **THEN** 不清空草稿、抢焦点或重开旧候选；合法权威选择观测可更新，但文件结果仍须匹配其独立编辑代次

#### Scenario: Round-trip a name that conflicts with mention syntax
- **WHEN** 用户选择前导 @、含反引号、双引号、# 或空格的合法路径
- **THEN** 插入文本解析为同一完整路径和原范围，不变成原文 escape、代码或第二条引用；不受支持名称根本不成为可接受候选

#### Scenario: Keep a pasted path unchanged
- **WHEN** 候选打开时粘贴包含 @、Tab 或换行的文字
- **THEN** 只完成有界原子插入，粘贴内部按键无行为；后续独立提交不接受旧候选替换文本

#### Scenario: Preserve modal and tiny-terminal control keys
- **WHEN** 文件搜索尚未返回而进入审批，或窗口缩小到无法显示候选
- **THEN** 候选不得截获焦点按键，迟到结果不重开面板，草稿/实际路径/光标保持

#### Scenario: Match manual completed and restored file inputs
- **WHEN** 同一最终引用和源内容分别手输、经补全提交，并从已提交事实恢复
- **THEN** 每家 Provider 的 canonical 请求、native 顺序和 fingerprint 一致，补全状态未进入持久化

### Requirement: File discovery uses workspace-local ignore rules without granting authority

候选发现 SHALL 只使用工作区内分层 .gitignore、.ignore、.rgignore，按目录从浅到深、同目录按上述文件顺序、同文件最后匹配优先。规则 SHALL 支持 Gitignore 样式的注释/转义、空格、否定、相对锚定、目录模式、*、?、字符类与 **；已排除目录被剪枝，不能靠该目录内规则重新纳入。工作区外/用户全局规则、Git index 和 .git/info/exclude MUST 不读取，生产发现不启动 git/rg。隐藏文件仍按相同规则参与；本地规则对 tracked/非 Git 文件同样生效，不宣称完整 Git 状态等价。

规则文件 MUST 经 descriptor-bound 安全读取，单文件至多 64 KiB，发现累计至多 512 KiB/4096 个有效规则，单模式至多 4096 bytes。缺失视为无规则；不安全、不可读、非法或超限规则 MUST 使所属子树省略并标记 ignore_unavailable，不能当作空规则继续扩大候选。一次发现 ignore 匹配至多 32000000 个状态转换，循环内必须可取消；规则正文不进入历史、错误或 prompt。

候选过滤 MUST 不作为读取权限。用户完整手输被过滤文件仍可明确准备，继续满足相同路径/FD/内容限制；不能以扩大根或跟随 symlink 绕过安全边界。

#### Scenario: Apply nested overrides and prune excluded directories
- **WHEN** 根与子目录规则包含覆盖、否定和目录排除，且排除目录内存在试图重新纳入自身文件的规则
- **THEN** 按固定来源/匹配顺序决定候选，已被剪枝目录不遍历，其内部规则不生效

#### Scenario: Keep parent global and Git state outside discovery
- **WHEN** 工作区外规则为 *、用户全局配置变化，或文件是否 tracked 改变而本地规则和文件集合不变
- **THEN** 候选不受这些外部状态影响，普通非 Git 工作区仍按其自身本地规则发现

#### Scenario: Type an ignored file explicitly
- **WHEN** 文件被有效规则从候选隐藏，但用户完整手输其合法引用
- **THEN** 正常文件准备可以读取它，过滤没有建立或撤销权限

#### Scenario: Fail closed on an unsafe or excessive rule source
- **WHEN** 某目录的规则是 symlink、非法内容或超过规则额度
- **THEN** 所属子树从候选省略且不完整原因明确，不悄悄忽略规则；独立手输仍只走安全读取

### Requirement: One candidate session reuses a bounded progressive filename index

一次文件候选打开期间 SHALL 保留一个最多 50000 项/16 MiB 的文件名索引，额度包含名称和匹配元数据，不保留附件正文。同一 sigil 锚点的路径文字或光标改变 MUST 只更新匹配，不因 token 末尾变化重新启动发现或重置 deadline；索引按有界批次发布并单调推进 revision。每次匹配最多消耗 16000000 个状态转换并分批检查取消，消耗字符/模式和重复匹配均计费，不能以只计候选数掩盖昂贵内层操作、无限回溯或每查询全量复制索引。

一次发现 deadline SHALL 为 2 秒。原始条目、名称、规则、索引、匹配工作到限或 I/O/时间失败时，SHALL 区分完全、进行中与不完整结果；可以保留此前已完整发布的目录，但不能伪称搜索完整或无匹配。相同索引快照排名确定；不同 deadline 覆盖子集不保证相同。手输路径仍独立于索引覆盖。

Esc、paste、modal、接受、提交或离开目标 SHALL 结束候选 session，取消发现/匹配并使旧结果不可接受，worker 完成本次清理后释放索引。重新打开 SHALL 创建新 session 并重新发现文件与规则；不得使用永久缓存、watcher 或同时存在的第二份无界索引。

#### Scenario: Continue discovering while the user types rapidly
- **WHEN** 一个目录发现尚未完成，用户连续更新同一 @ 目标查询
- **THEN** 发现启动次数仍为一，已发现路径可服务最新查询，deadline 不按每个字符重置

#### Scenario: Preserve and label partial results at the deadline
- **WHEN** 发现 deadline 到达但已有完整目录发布到索引
- **THEN** 对已发布路径仍可匹配和接受，并明确显示覆盖不完整；空结果不能被显示为全量无匹配

#### Scenario: Reopen after files and rules change
- **WHEN** 用户关闭候选，创建/重命名文件或修改本地规则，再重新打开
- **THEN** 旧 session 已取消，新的发现反映当前文件/规则，旧结果不能复活或替换当前选择

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
