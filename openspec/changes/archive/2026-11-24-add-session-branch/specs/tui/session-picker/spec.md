## MODIFIED Requirements

### Requirement: Interactive resume is an explicit startup command

系统 SHALL 提供 `easycode resume [--all] [--config PATH]`。默认范围为当前规范化启动目录；`--all` 仅移除目录筛选。命令 SHALL 要求 stdin/stdout 均为终端，并在打开存储前校验参数与终端条件；不得回退读取其他终端。帮助 SHALL 返回 0 且不打开配置或存储；未知参数、多余位置参数、缺失选项值及非终端输入输出 SHALL 返回 2 和安全英文诊断。

命令 SHALL 在启动界面前加载并验证当前聊天配置，配置错误返回 1；配置和启动 cwd 在本次启动中保持固定。只有首参数 `resume` SHALL 进入该命令。已有 `--resume <thread-id>`、`--continue`、headless 和独立 `sessions list` 使用现有 CLI 参数及输出约束。可选线程范围 SHALL 按 session/resume 和 session/catalog 包含可恢复的根线程及分支。选择前 MUST NOT 创建 Session journal、读取项目指令、执行工具或发起模型请求。

#### Scenario: Open the picker with valid configuration
- **WHEN** 用户在交互终端使用有效配置执行 `easycode resume`
- **THEN** 界面加载当前目录候选，尚未创建新 thread 或发起 Provider 请求

#### Scenario: Reject usage before resource acquisition
- **WHEN** 用户请求帮助或传入未知选项、位置参数，或 stdin/stdout 任一不是终端
- **THEN** 帮助返回 0，非法调用返回 2，配置和存储均未打开

#### Scenario: Preserve a headless prompt
- **WHEN** 用户执行 `easycode --print "resume"`
- **THEN** 文本作为原有 headless prompt，不进入选择器

### Requirement: Discovery and metadata search have explicit bounds

每次成功加载 SHALL 使用既有 Catalog 协调与查询语义，按 committed 更新时间和 canonical thread ID 双降序返回当前范围最多 200 条。合格根线程及分支 SHALL 共用同一查询范围、排序及上限。当前目录 SHALL 精确匹配规范化 creation cwd，不包含子目录或兄弟 worktree。搜索 SHALL 只覆盖已加载条目的显式标题、thread ID、Provider family、wire、model 和 creation cwd，按 Unicode 小写转换后的字面子串匹配，不解释正则，也不搜索预览正文或从血缘投影出的父线程 ID 与分支类型。标题 SHALL 独立匹配，再与既有元数据匹配取并集，跨标题与其他字段的词组不能形成新匹配。

搜索输入 SHALL 限制为 256 个 Unicode code point；decoded paste 的 CR/LF/Tab SHALL 转为空格，超过限制的编辑整体拒绝且不丢失原查询。退格 SHALL 按字素边界删除。UI SHALL 区分空目录、没有匹配项和加载失败；有额外条目时 MUST 明确搜索仅覆盖最近 200 条。协调的 busy/invalid 提示 MUST 标明全存储范围、可能陈旧或遗漏，不能声称每个显示条目当前可恢复。键入搜索 MUST NOT 触发 journal 扫描。

#### Scenario: Search a bounded mixed-provider list
- **WHEN** 当前目录有超过 200 条、包含两家 Provider 的会话，用户输入模型名
- **THEN** 仅在最近 200 条中筛选，不自动隐藏其他 Provider，也不搜索正文，界面说明搜索边界

#### Scenario: Distinguish no matches from incomplete discovery
- **WHEN** 搜索没有结果，且协调报告 busy/invalid 或存在更多未加载条目
- **THEN** 界面说明已加载范围无匹配，保留不完整提示，不宣称存储中不存在该会话

#### Scenario: Reject excessive pasted search text
- **WHEN** 一次粘贴经空白规范化后使查询超过 256 个 code point
- **THEN** 原查询、选择和列表不变，界面显示安全的输入上限提示

#### Scenario: Search by title without changing scope or identity
- **WHEN** 当前范围的已加载条目包含标题大小写不同的同名会话，范围外或最近 200 条之外也有同名会话
- **THEN** 标题查询只显示范围内且已加载的匹配条目，选择仍按各自的完整 thread ID；原有元数据也仍可搜索

#### Scenario: Avoid matching across the title boundary

- **WHEN** 查询只匹配标题末尾与另一个元数据字段开头拼成的字符串
- **THEN** 该条目不因拼接命中，查询完整标题或原元数据仍按各自规则返回

#### Scenario: Keep branch lineage out of the search keys
- **WHEN** 已加载分支的父线程 ID 未出现在该分支既有可搜索元数据中，用户搜索该父 ID
- **THEN** 不因分支血缘命中该行。使用分支自身的标题或完整 thread ID 仍可辨认和选定该分支

### Requirement: Display is bounded terminal-safe and identity-preserving

列表 SHALL 优先使用显式标题辨识已命名的条目，并显示更新时间、Provider/model。分支条目 SHALL 标明分支类型。无标题的条目 SHALL 沿用现有元数据展示。界面 SHALL 在选择详情中提供完整 thread ID、wire、创建目录和实际恢复目录。时间 SHALL 使用当前机器本地时区的固定日期时间并显示时区。配置三元组不匹配 SHALL 显示具体差异，不能自动换模型或隐藏历史。

model/wire 的转义展示 SHALL 限制为 256 个 code point，目录为 512 个；元数据的换行、Tab、ESC、C0/C1、DEL、Unicode 行/段分隔符及双向控制符 MUST 可见转义。显式标题 SHALL 遵守 session/titles 的 256 UTF-8 bytes 上限，以通过校验的纯文本展示并保留合法字素，列表按列宽截断，详情可滚动查看完整标题。分支预览 SHALL 展示已从所选 journal 校验出的完整父线程 ID，可经换行或滚动读完。预览 SHALL 使用从所选 journal 校验出的标题，与其他预览内容一同滚动，标题独立于正文展示预算。预览正文 SHALL 保留可见换行和制表布局，其他终端控制符必须转义，并遵循预览展示预算。窄屏、resize 和无色模式 MUST 保持选择与查询不变、无负尺寸，并能辨认当前选择；完整身份可换行/滚动，不得使用缩写身份发起操作。render MUST 无 I/O。

#### Scenario: Render hostile metadata and narrow screens
- **WHEN** model/cwd 含终端控制字符、中文、emoji 或超长文本，且终端被缩窄
- **THEN** 输出无可执行控制序列，展示有界，所选身份不变且详情可查看完整 ID

#### Scenario: Show compatibility without claiming availability
- **WHEN** 候选的 family、wire 或 model 与当前配置不匹配
- **THEN** 界面显示不匹配字段，仍允许符合 session/preview 契约的只读预览，不自动转换历史

#### Scenario: Preview the title from the selected journal
- **WHEN** 同一 thread 的缓存列表标题与已验证 journal 标题不同，或者预览正文达到展示上限
- **THEN** 预览显示其 journal 标题（无显式标题时无标题字段），标题独立于正文限额；列表查询和当前选择仍按原有规则保留

#### Scenario: Keep a long Unicode title and its identity accessible

- **WHEN** 已命名条目标题含 ZWJ emoji 且超出当前列宽，用户缩小终端并打开详情
- **THEN** 列表裁剪不拆开字素，详情显示完整合法标题与 thread ID，所选会话仍按该 ID 关联

#### Scenario: Verify the parent identity for a branch preview
- **WHEN** 用户在窄屏中打开同名根线程和分支的预览或切换选择
- **THEN** 列表可辨认分支，分支预览显示经本次目标 journal 校验的完整父线程 ID。预览、选择与恢复按各自完整 thread ID 关联

### Requirement: In-chat resume isolates directory and provider selection

聊天内只允许接管 creation cwd 与启动工作目录一致、Provider family、wire 与 model 完全匹配的根线程或分支。全目录范围中的其他目录线程 SHALL 可浏览和只读预览。只有在选择器列表中按普通 Enter 才能发起所选线程的恢复。预览不接收恢复操作，Esc 关闭预览并返回原列表与选项。启动选择器的跨目录明确确认对根线程和分支同样适用。

列表拒绝跨目录选择时 SHALL 保留原查询与选中身份，并显示安全转义的创建目录和 `easycode --resume <thread-id>`，提示从其创建目录以兼容配置启动。提示中的命令与完整 UUID SHALL 可通过换行或滚动读取，窄屏也不得丢失完整 ID。列表中的三元组不匹配 SHALL 显示差异并阻止接管，目录与配置同时不匹配时 SHALL 允许查看两方面信息。直接指定 ID 遇到目录不匹配时可使用带完整恢复命令的通用目录提示，遇到配置不匹配可使用安全的配置不兼容诊断。提示不得展示配置文件路径、自动切换目录、生成可执行的目录切换命令或操作剪贴板。实际恢复仍重新核验目录与配置。

#### Scenario: Browse another workspace without entering it
- **WHEN** 用户在窄屏通过 Ctrl+A 找到其他目录的线程，在预览中按 Enter，按 Esc 返回列表后再按 Enter
- **THEN** 预览不启动恢复。列表拒绝接管并保留查询和选择，用户能从可滚动或换行的详情读取创建目录、完整 UUID 和独立恢复命令，当前聊天和工作目录保持原状

#### Scenario: Keep incompatible sessions discoverable
- **WHEN** 列表中某线程同时来自其他工作目录且 Provider family、wire 或 model 不匹配
- **THEN** 仍可查看其已支持的只读预览，选择器提供目录提示和配置差异并拒绝接管

#### Scenario: Resume either side of a branch in the same directory
- **WHEN** 原线程和分支都使用当前工作目录及兼容配置，用户从任一侧在选择器选取另一侧，或提供对方完整 ID
- **THEN** 恢复只尝试用户选定的线程，当前线程仍从列表排除。分支使用与根线程相同的兼容和目录校验

## ADDED Requirements

### Requirement: Branch creation uses the active chat handoff and reports durable outcomes

`/branch [标题]` 和 `/fork [标题]` SHALL 在当前交互聊天请求创建本地分支。只有当前启动工作目录与源 journal 的 creation cwd 相同，且源真正空闲、无未确认输入、取回中输入或待处理返还文本时才能开始。界面忙时 SHALL 保留草稿并提示显式重试。创建无需另开选择器或二次确认。目标准备期间独占终端，显示创建或清理进度，重复 Enter、普通字符与粘贴均由当前焦点消费。

界面接管前 Esc 或 Ctrl+C SHALL 请求取消，等待匹配的创建结果与清理确认。若源仍健康，界面恢复源草稿和光标。反馈 SHALL 区分分支未发布、分支已持久化但未切换、发布耐久性不确定。已持久化分支必须提供带完整 thread ID 的 `/resume <thread-id>` 恢复提示。耐久性不确定时必须说明状态，以完整目标 ID 帮助定位，不能承诺目标存在或可恢复。取消后到达的候选不得切换界面。

只有同一次尝试的已验证目标身份及有界初始 transcript 足以接管时才确认切换。接管获准后，等待源资源关闭，Esc 不撤销接管，Ctrl+C 按宿主退出清理。完成后 SHALL 显示新线程身份、已确认标题和返回源线程的 `/resume <thread-id>` 提示，再启用目标输入。窄屏下完整命令必须可经换行或滚动读完。源草稿不得带入目标，源异步结果不得覆盖目标界面。终端没有机会交付本次已发布或不确定结果时，由宿主完成清理并通过安全 CLI 诊断报告完整目标 ID。

#### Scenario: Cancel creation and retain the actual publication outcome
- **WHEN** 用户在目标接管前按 Esc 或 Ctrl+C，发布可能尚未发生、已完成或无法确认
- **THEN** 只有清理完成且源健康才返回源草稿，分别显示取消、已保存分支及完整恢复 ID，或明确的耐久性不确定诊断。重复输入和迟到候选不产生额外分支或切换

#### Scenario: Show the source recovery command in the activated branch
- **WHEN** 分支目标通过校验并被接管，用户在源关闭期间调整为窄屏并按 Esc
- **THEN** 完成源关闭后显示新分支，保留可读的完整源恢复命令及目标历史。Esc 不回滚已接受交接，等待期间按键不会成为目标输入
