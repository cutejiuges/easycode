## MODIFIED Requirements

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

### Requirement: Completion does not become execution or persistence state

命令/Skill 的发现、筛选、导航、关闭、Tab 接受和 Skill 的 Enter 接受 SHALL 只操作本地内存，不读取磁盘正文、发起网络、启动进程、写 Session 或改变 Runtime 权限。正式提交后 SHALL 继续使用现有 Skill 来源/digest 校验、冻结附件、预算、admission、Sync 和失败恢复路径。源变化或取消 SHALL 保留原输入供编辑，不通过补全建立执行或资源读取资格。

在相同已提交事实、上下文来源、实际冻结附件及操作捕获的完整推理设置下，相同最终正文，无论由手动输入、不同尺寸的补全、关闭后重开或历史召回产生，SHALL 得到相同双 Provider 请求 canonical bytes、native item 顺序与稳定 fingerprint；恢复后显式重选相同设置再继续请求 SHALL 与未中断路径等价，进程选择序号和来源可以不同。不同磁盘内容或实际设置不在该请求等价承诺内，不能为了等价重读历史附件或恢复旧 effort。候选、查询、焦点、选择、隐藏原因及来源展示 MUST NOT 写入用户消息、Session/native/checkpoint 或缓存前缀。headless 与 stream-json SHALL 保持现有文本/控制契约，不识别终端补全按键。

#### Scenario: Detect a changed source at normal submission
- **WHEN** Skill 已被补全，但文件在正式提交前发生变化
- **THEN** 正常准备返回 skill_source_changed，不发送 Provider 请求，不持久化该输入，不重新发现或切换来源

#### Scenario: Match manual completed and restored requests
- **WHEN** 相同最终 Skill 输入分别手动键入及经补全后提交，并从已提交历史恢复
- **THEN** 两家 Provider 各自的 canonical 请求、原生项顺序和 fingerprint 一致，补全前未增加 journal records 或 Skill 读取


## ADDED Requirements

### Requirement: File completion resolves the editable mention at the current cursor

文件 @ 补全 SHALL 在正文任意位置按当前光标的路径编辑区确定目标，共用显式文件引用语法；不得把命令/Skill 的首词限制扩大到文件，也不得改变 /、$ 的原有激活范围。目标 MUST 在字符和字素合法边界内，允许未闭合引号作为编辑中状态；email、转义、@@、行内/围栏代码不得激活。光标进入 #L 范围、引用之后或其他正文 MUST 不激活路径补全；已有范围与 token 外正文保留，不 fuzzy 修正范围。

七项精确内置命令的参数 SHALL 按原命令语义保持文字，文件目标识别前复用 domain 的同一保留名纯判断，不开启文件搜索或规则读取，不额外保存一套命令名。既有 /effort 及其他六项、256 项 Skill 容量、同名 Skill 的 $ 入口 MUST 保持；$effort 等合法 Skill 输入的正文参数仍可明确附带文件，不能因为名称保留而禁用合法文件引用。

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
