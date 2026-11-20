# tui/session-picker Specification

## Purpose

提供启动和聊天内可用的交互式会话选择器，使用户能通过有界元数据搜索和显式只读文本预览辨认历史会话，并在确认恢复身份与工作目录后安全进入目标会话，保持终端输入、取消和资源清理的一致语义。

## Requirements

### Requirement: Interactive resume is an explicit startup command

系统 SHALL 提供 `easycode resume [--all] [--config PATH]`。默认范围为当前规范化启动目录；`--all` 仅移除目录筛选。命令 SHALL 要求 stdin/stdout 均为终端，并在打开存储前校验参数与终端条件；不得回退读取其他终端。帮助 SHALL 返回 0 且不打开配置或存储；未知参数、多余位置参数、缺失选项值及非终端输入输出 SHALL 返回 2 和安全英文诊断。

命令 SHALL 在启动界面前加载并验证当前聊天配置，配置错误返回 1；配置和启动 cwd 在本次启动中保持固定。只有首参数 `resume` SHALL 进入该命令。已有 `--resume <thread-id>`、`--continue`、headless 和独立 `sessions list` 契约 MUST 保持不变。选择前 MUST NOT 创建 Session journal、读取项目指令、执行工具或发起模型请求。

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

每次成功加载 SHALL 使用既有 Catalog 协调与查询语义，按 committed 更新时间和 canonical thread ID 双降序返回当前范围最多 200 条。当前目录 SHALL 精确匹配规范化 creation cwd，不包含子目录或兄弟 worktree。搜索 SHALL 只覆盖已加载条目的显式标题、thread ID、Provider family、wire、model 和 creation cwd，按 Unicode 小写转换后的字面子串匹配，不解释正则，不搜索预览正文。标题 SHALL 独立匹配，再与既有元数据匹配取并集，跨标题与其他字段的词组不能形成新匹配。

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

### Requirement: Keyboard routing preserves explicit selection and cancellation

界面 SHALL 支持上下键、PageUp/PageDown、输入搜索、Ctrl+A 切换范围、Ctrl+R 刷新、Ctrl+V 预览及 Enter 请求恢复。更新列表 SHALL 按 thread ID 保持仍存在的选择，无法保留时选择首项；不得通过旧索引确定恢复目标。没有候选时 Enter/Ctrl+V 不产生操作。

Esc SHALL 优先关闭预览或跨目录确认，再清空非空搜索，最后取消启动并在清理成功后返回 0。Ctrl+C/信号取消 SHALL 在清理完成后返回 130；清理失败返回 1。加载中取消 MUST 等待清理，不能创建替代 Session。进入正式恢复后重复 Enter MUST NOT 发起第二次恢复，普通输入 MUST NOT 成为聊天 prompt。

#### Scenario: Return from preview without losing the selection
- **WHEN** 用户打开预览后按 Esc
- **THEN** 返回原列表并保留查询和所选 thread，预览正文被丢弃

#### Scenario: Clear search and then exit
- **WHEN** 用户在列表非空搜索中按 Esc，再按 Esc
- **THEN** 第一次清空搜索，第二次取消启动；没有新 Session 或自动模型请求

#### Scenario: Ignore repeated confirmation while resuming
- **WHEN** 正式恢复已被接收，用户继续按 Enter 或输入字符
- **THEN** 只存在一个恢复请求，没有自动提交的聊天输入

#### Scenario: Refresh named sessions without losing the selected identity
- **WHEN** Ctrl+R 刷新成功，同一所选 thread 仍匹配当前搜索，但标题或列表位置发生变化
- **THEN** 展示本次加载的标题，保留当前查询与按完整 thread ID 绑定的选择

### Requirement: Display is bounded terminal-safe and identity-preserving

列表 SHALL 优先使用显式标题辨识已命名的条目，并显示更新时间、Provider/model。无标题的条目 SHALL 沿用现有元数据展示。界面 SHALL 在选择详情中提供完整 thread ID、wire、创建目录和实际恢复目录。时间 SHALL 使用当前机器本地时区的固定日期时间并显示时区。配置三元组不匹配 SHALL 显示具体差异，不能自动换模型或隐藏历史。

model/wire 的转义展示 SHALL 限制为 256 个 code point，目录为 512 个；元数据的换行、Tab、ESC、C0/C1、DEL、Unicode 行/段分隔符及双向控制符 MUST 可见转义。显式标题 SHALL 遵守 session/titles 的 256 UTF-8 bytes 上限，以通过校验的纯文本展示并保留合法字素，列表按列宽截断，详情可滚动查看完整标题。预览 SHALL 使用从所选 journal 校验出的标题，与其他预览内容一同滚动，标题独立于正文展示预算。预览正文 SHALL 保留可见换行和制表布局，其他终端控制符必须转义，并遵循预览展示预算。窄屏、resize 和无色模式 MUST 保持选择与查询不变、无负尺寸，并能辨认当前选择；完整身份可换行/滚动，不得使用缩写身份发起操作。render MUST 无 I/O。

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

### Requirement: Async loading cannot corrupt current state or leak owners

列表、刷新和预览 SHALL 具有独立请求身份，预览还绑定 thread ID。过期结果或错误 MUST NOT 覆盖当前范围、预览或错误状态。系统 SHALL 最多运行一个存储/恢复操作，快速切范围只保留最新待处理意图，在旧操作取消及清理后开始新操作。

清理成功的加载错误 SHALL 保留已显示状态并明确标记；若请求范围与已显示范围不同，旧列表 MUST 标注其实际范围且在成功加载当前范围前禁止恢复/预览，不能把旧数据冒充新范围。用户可显式刷新重试。首次协调未成功时 MUST NOT 将旧索引查询视作已完成刷新；初次加载或显式刷新 SHALL 从同一次 Catalog 协调取得两种范围的有界列表与报告，成功清理后统一更新。切范围复用该次协调结果，保留不完整提示。重新打开选择器时重新协调。任何清理失败 SHALL 结束启动。退出或终端输出失败 MUST 停止接收新操作，取消在途操作并等待唯一 owner 完成清理；超时不得遗弃资源。选择器到 Chat 的输入/终端所有权 MUST 连续且唯一，不得同时存在两个输入 reader。

同范围刷新失败且清理成功时，最近已协调的同范围快照 SHALL 在提示失败后仍可用于选择和预览。范围不匹配时仍按旧列表规则禁用操作。具体选择的 journal 继续独立重新验证。

#### Scenario: Ignore an old scope response
- **WHEN** 用户连续切换范围，旧请求在新意图建立后返回
- **THEN** 旧结果不更新当前范围，存储操作不并发，最终显示最新范围的结果

#### Scenario: Fail a refresh without faking an empty result
- **WHEN** 刷新失败但已有成功加载的列表
- **THEN** 原列表和选择保留，失败提示与成功空列表可区分，同范围既有快照仍可预览或选择，只有显式操作才重试

#### Scenario: Exit during preview or terminal failure
- **WHEN** 预览正在加载时收到退出请求或终端输出失败
- **THEN** 取消查询并等到 lease、目录与任务释放后退出，迟到结果不会重新打开界面

#### Scenario: Toggle scope against the last reconciled view

- **WHEN** 已协调列表仍打开，派生索引其后暂不可用，用户切换范围
- **THEN** 界面使用同一次协调的目标列表和提示，显式刷新才能用新的协调结果取代

### Requirement: In-chat resume opens the existing picker or selects an exact thread

交互聊天 SHALL 提供 `/resume` 与 `/resume <thread-id>`。无参数时打开同一终端内的选择器，默认按规范化的启动工作目录列出候选，复用有界元数据搜索、刷新、Ctrl+A 范围切换和显式只读预览。候选 MUST 排除当前线程。显式参数 SHALL 只接受一个完整、规范的 UUIDv7 thread ID，按该 ID 定位，适用于未出现在已加载列表中的线程。当前线程 ID、非法 ID 和多余参数 SHALL 在开始目标恢复前以安全英文诊断拒绝。命令本身不得成为对话输入、Skill 调用、文件引用或 Provider 请求。

当前目录没有其他候选时，选择器 SHALL 明示该范围没有其他会话，并继续支持 Ctrl+A 切换范围及 Ctrl+R 刷新。发现、刷新和预览可恢复失败时 SHALL 保持选择器可用，显示安全英文诊断并保留查询和原选中身份。已显示结果的可操作性仍遵守范围与刷新契约。

只有当前会话真正空闲，并且没有接纳前准备、待提交、排队、取回中或尚未处理的返还输入时，系统才 SHALL 接受该命令。遇到忙碌状态 SHALL 提示处理当前操作或输入后重试。系统 MUST NOT 自动排队执行恢复、自动中断当前工作或丢弃未处理的输入。

#### Scenario: Find another session in the current workspace
- **WHEN** 空闲聊天提交无参数的 `/resume`
- **THEN** 同一终端打开选择器，按启动工作目录列出其他线程，用户可以按标题或其他已加载元数据搜索，也可刷新和预览

#### Scenario: Explore another scope when the local list is empty
- **WHEN** 当前目录只包含活动线程，用户打开 `/resume`
- **THEN** 选择器说明当前范围没有其他会话，保留 Ctrl+A 与 Ctrl+R。扩大范围后仍不显示活动线程

#### Scenario: Keep picker state after a recoverable browsing error
- **WHEN** 已设置查询并选中线程的用户遇到可恢复的刷新或预览错误
- **THEN** 选择器保留查询和源选中身份，标明错误与已显示结果的状态，可返回列表、刷新或取消

#### Scenario: Resume by an ID outside the loaded list
- **WHEN** 用户提供未出现在已加载列表中的同目录兼容线程的完整 ID
- **THEN** 只尝试恢复该线程。提交标题、当前线程 ID、部分 ID 或多个参数时均显示本地错误，不启动目标恢复

#### Scenario: Preserve pending work on a busy resume attempt
- **WHEN** 用户在当前操作尚未收口或仍有排队、取回中或未处理的返还输入时尝试 `/resume`
- **THEN** 恢复不进入后台队列，当前操作与输入仍按原路径处理，界面提示待空闲后显式重试

### Requirement: In-chat resume isolates directory and provider selection

聊天内只允许接管 creation cwd 与启动工作目录一致、Provider family、wire 与 model 完全匹配的 root thread。全目录范围中的其他目录线程 SHALL 可浏览和只读预览。只有在选择器列表中按普通 Enter 才能发起所选线程的恢复。预览不接收恢复操作，Esc 关闭预览并返回原列表与选项。

列表拒绝跨目录选择时 SHALL 保留原查询与选中身份，并显示安全转义的创建目录和 `easycode --resume <thread-id>`，提示从其创建目录以兼容配置启动。提示中的命令与完整 UUID SHALL 可通过换行或滚动读取，窄屏也不得丢失完整 ID。列表中的三元组不匹配 SHALL 显示差异并阻止接管，目录与配置同时不匹配时 SHALL 允许查看两方面信息。直接指定 ID 遇到目录不匹配时可使用带完整恢复命令的通用目录提示，遇到配置不匹配可使用安全的配置不兼容诊断。提示不得展示配置文件路径、自动切换目录、生成可执行的目录切换命令或操作剪贴板。实际恢复仍重新核验目录与配置。

#### Scenario: Browse another workspace without entering it
- **WHEN** 用户在窄屏通过 Ctrl+A 找到其他目录的线程，在预览中按 Enter，按 Esc 返回列表后再按 Enter
- **THEN** 预览不启动恢复。列表拒绝接管并保留查询和选择，用户能从可滚动或换行的详情读取创建目录、完整 UUID 和独立恢复命令，当前聊天和工作目录保持原状

#### Scenario: Keep incompatible sessions discoverable
- **WHEN** 列表中某线程同时来自其他工作目录且 Provider family、wire 或 model 不匹配
- **THEN** 仍可查看其已支持的只读预览，选择器提供目录提示和配置差异并拒绝接管

### Requirement: In-chat resume owns focus through cancellation and handoff

选择器及直接恢复进度 SHALL 独占终端输入，显示加载、恢复或清理状态。列表中的 Esc 沿用关闭详情或预览、清空搜索的返回层级，最外层 Esc 取消并返回聊天。接管前，Ctrl+C 取消聊天内恢复，正式恢复中的 Esc 也 SHALL 请求取消。终端事件循环在选择器或交接等待期间收到的按键由当前焦点消费。重复 Enter、文字或粘贴不得产生第二次恢复或缓存为后续聊天输入。

接管前取消，或已受理的正式恢复发生可恢复失败时，系统 SHALL 在确认清理后返回原会话并显示安全通知，按当前终端尺寸还原原 composer 的准确正文和光标。发现、预览及列表拒绝仍留在选择器。目标的有界初始历史页及身份 SHALL 在接管前验证，已验证的空历史以明确空状态处理。初始页缺失、无效或与目标身份不符时，正式恢复 SHALL 在接管前失败。

接管获准后，界面 SHALL 等待旧会话清理完成，显示目标身份与已验证的初始历史或空状态，再启用目标输入。清理期间的 Esc SHALL 被消费，保持进度可见且继续交接。Ctrl+C 或终端退出信号 SHALL 由宿主接手退出和清理，不能回滚至源会话。旧草稿不得成为目标输入。两次进入同一线程也 SHALL 有独立的界面操作身份，迟到的列表、预览、控制流或详情结果不得替换当前状态。启动选择器的按键、退出和跨目录确认仍遵循启动入口的契约。

#### Scenario: Cancel a pending switch and continue the original session
- **WHEN** 用户在目标接管前取消正在加载或恢复的会话，期间调整终端尺寸
- **THEN** 等待取消清理完成后返回原聊天，保留原正文和光标且采用新尺寸，目标不会因迟到结果成为活动线程

#### Scenario: Return from a failed formal recovery
- **WHEN** 列表确认或直接 ID 恢复已受理，但目标忙碌，或其初始历史缺失、无效或身份不匹配
- **THEN** 在清理确认后返回原聊天，显示可安全展示的失败原因，原正文及光标准确复原

#### Scenario: Ignore escape while completing an accepted handoff
- **WHEN** 已接受接管但旧会话尚在关闭，用户按 Esc
- **THEN** 继续显示清理进度，不退出或回滚。清理成功后，显示目标身份及已验证历史，再接收第一条输入

#### Scenario: Ignore replies from an earlier activation of the same thread
- **WHEN** 用户从 A 切换至 B，再切回 A，第一次打开 A 时发出的异步结果迟到
- **THEN** 第二次打开 A 仅接受本次激活的结果，旧界面投影和草稿不能进入当前会话
