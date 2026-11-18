## Context

动机见 [proposal.md](proposal.md)。以下基于 2026-11-16 合并 main 至 b0387c4 后的当前工作区，并沿用已重新核对的两个本地参考快照；不能据此宣称本能力已经实现。

| 当前接缝 | 已观察行为 | 本次利用方式 |
| --- | --- | --- |
| `internal/tui/completion{,_state,_view}.go`、`domain/skill_slash.go` | 七个内置命令和 Skill 的首词补全，保留名来自同一纯 switch；独立接受/提交、modal/paste 抑制 | 复用保留名判断，增加独立的正文文件目标与异步候选，不能遗漏 /effort 或压缩参数边界 |
| `internal/runtime/session.go`、`skills.go` | `ChatSession.Append` 在 admission 前准备 Skill；TUI 已有 pending context、取消和 generation | 组合实际需要的输入准备策略，继续使用同一个 AgentLoop |
| `internal/domain/user_input.go`、`skills.go` | 原文与一个冻结 Skill 附件分别保存，ByteSize 覆盖队列额度 | 增加有界文件附件集合及深拷贝，保持原 InputID |
| `internal/tool/builtin/{read,walker,workspace_unix}.go` | 已打开的 Workspace capability、逐段 no-follow、实际 FD 校验和可取消 UTF-8 读取 | 复用安全打开和有限读取机制；附件读取不伪造 Read invocation |
| 同上 `readEntryNames()` | 当前实现调用 `Readdirnames(-1)`，入口数量限制不能限制这一步分配 | 把目录读取机制改为显式有限批次；新扫描策略与既有 Glob/Grep 策略分别保留 |
| 两家 `user_input.go`、`commit.go`、`native.go` | 第一个块为原文，存在 Skill 时第二块与 metadata 严格相等；clone 复制 metadata | 扩展当前块布局及全部 clone/decoder/恢复验证，不能只追加请求正文 |
| `provider/input_metadata.go` | 共享关联元数据不属于模型 wire；没有文件附件 | 新增必需 files 数组，以输入身份和有序附件关联 |
| `domain/history.go`、两家 `history.go`/`transcript.go`、`app/transcript_queries.go` | recall 与 Skill 详情分开，详情按需分页 | 原文继续单独回放，附件详情只读取已提交事实 |
| `context/planning.go` | 固定来源；有效窗口与 native footprint 取覆盖值；封闭 text/skill_attachment 内容 | 增加实际使用的 file_attachment 内容，避免重复计量或污染目录 |
| `runtime/effort_loop.go`、`agent_loop.go`、`input_commit.go` | effort 是进程内选择，普通 turn 启动前捕获 profile；PreparedUserInputs.CheckRequest 接收实际操作的 TurnInput | 文件准备仅冻结内容；在实际交付操作中使用同一捕获策略完成动态预检和请求，不从启动配置重建 |
| `tui/{effort,chat_space,completion_view}.go` | effort 使用短通知而非面板，输入/队列/状态共用真实行数；反馈有请求及操作代次 | 文件候选共享布局预算与编辑失效路径，不建立第二套全屏预算或复用 effort 的权威序号 |

### 参考工程证据与取舍

参考根分别为 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap`（`a8a678cb6244e6770e1e421767ff0987a1d95549`，包版本 2.1.88）和 `/Users/cutejiuge/IdeaProjects/codex`（`c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`）。它们不在当前 worktree 的直接父目录，核对时通过主 checkout 的同级目录定位；保持只读。

| 工程与实际读取的位置 | 观察 | 采用或调整 |
| --- | --- | --- |
| Claude `restored-src/src/hooks/fileSuggestions.ts:715` 的 generateFileSuggestions、`:789` 的 applyFileSuggestion | 空查询、文件名搜索、候选上限、仅替换对应路径片段；支持后台索引 | 采用路径选择与正文编辑分离；本次使用实例 owner，不复制模块级可变缓存、外部建议命令或广域路径展开 |
| Claude `restored-src/src/utils/attachments.ts:1894` 的 processAtMentionedFiles | 提交输入时提取引用，文件内容通过读取工具能力准备为附件 | 采用正式提交时读取；复用本工程 FD 能力，不复制 stat 后再读的校验方式，也不制造工具调用事实 |
| 同文件 `:2757` 的 extractAtMentionedFiles、`:2836` 的 parseAtMentionedFileLines | `@path`、引号路径、行范围及去重 | 保留语法体验；按原文出现次序去重，非法范围显式失败；代码片段、转义和普通 email 保持文字 |
| 同文件 `:3020` 的 generateFileAttachment | 文件权限、大小限制、截断、文件已读状态与附件分开 | 采用有界截断和实际范围标记；不用 mtime 决定跳过内容，也不自动给予 Write 的完整 Read 资格 |
| Codex `codex-rs/tui/src/bottom_pane/chat_composer/completion_target.rs:1` | 按光标附近 token 和字符边界决定补全目标 | 采用正文目标和字节范围校验，复用已有 composer 字素边界；不增加另一套编辑器 |
| Codex `codex-rs/tui/src/file_search.rs:1` | 搜索由宿主 owner 管理，使用会话 token，根目录变化使旧搜索失效 | 采用单 owner、显式取消和代次；锁内只做内存转换，不复制锁内启动搜索 |
| Codex `codex-rs/tui/src/bottom_pane/file_search_popup.rs:70`、mentions_v2/popup.rs | pending/display query 分开，丢弃旧查询结果；完整插入路径与窄屏显示不同 | 对查询、目标、草稿 revision 和 generation 全部核对，覆盖同查询先后出现的 ABA 情况；截短显示不回写输入 |
| Claude `restored-src/src/hooks/useTypeahead.tsx:148`、`:261`、`:424` | 完成路径补空格；识别光标两侧完整 token；取消 debounce、保留选择并过滤旧结果 | 补齐结束引号/范围后的光标与旧候选停用；不复制只比较 query 的接收条件或模块级可变状态 |
| Claude `restored-src/src/hooks/fileSuggestions.ts:248`、`:459`、`:629` | 默认处理 ignore、tracked 与 untracked，查询复用索引；刷新有 generation | 确定工作区本地过滤与候选 session 索引，不把每个按键变为一次全盘扫描；明确不模拟 Git index 例外 |
| Codex `codex-rs/file-search/src/lib.rs:161`、`:420`、`:546`；lib_tests.rs:502 | 查询更新复用 walker 与匹配器；显示 walk_complete；测试仓库外父级 ignore 不应隐藏当前文件 | 保留一个受限路径索引、渐进 revision 与不完整状态；不读工作区外规则，不照搬 unbounded channel、后台线程或 follow_links |
| Codex `codex-rs/tui/src/bottom_pane/chat_composer.rs:2517`、`:2874` | 选中后跨过分隔并抑制旧目标，路径替换与其他正文分开 | 接受后退出完整引用范围，明确相邻文本/字素与第二次 Enter 行为 |

这些是指定快照的实现依据，不是对两个产品最新版本的承诺。参考工程的目录附件、MCP/Agent 联合 mentions、自动再读取和持久偏好不进入本切片。

## Goals / Non-Goals

**Goals:** 让用户在现有聊天与同轮追加中明确附带文件；以同一原文和冻结内容获得可重现请求；让每个文件读取、搜索、准备、接收、提交与清理都有明确边界。

**Non-Goals:** 文件附件不成为项目指令或 system/developer 消息，不激活嵌套 AGENTS/CLAUDE/rules，不创建模型工具、权限 grant 或完整 Read 证据。当前工作区就是既有 Tools 的启动 cwd capability，不能借项目根、历史 creation_cwd 或补全扩大根。外部 headless 协议及普通文本不增加隐式读盘语义。

## Decisions

### 1. 强类型草稿表示明确意图，正式提交前冻结

新增纯内存的交互输入草稿，区分普通文字与带文件引用的交互意图，保存原始正文和已解析的 FileReference 集合。TUI 的正常提交路径调用共享纯解析器；本地 slash command 仍先按原规则分派，不能因为参数内有 @ 而触发读盘。headless 继续构造普通文字草稿，已有已存文件附件照常恢复。

解析只针对本次真实用户原文；不递归解析 Skill 展开、文件正文、Hook 反馈、模型输出或历史详情中的 @。附件组合前后原文始终是同一字段，不能先拼接展开正文再解析引用。本地命令参数仍不触发文件准备，包括 /compact 的自定义 instructions 和 /effort 的合法或非法参数。TUI 在文件目标识别前复用 domain.IsReservedSkillSlashName 的精确首词判断，对这七项内置命令的参数也不开文件候选；不另存保留名列表或把命令执行塞进通用引用解析器。保留名 Skill 通过 $effort 等合法显式入口调用时，正文参数仍可明确附带文件。

FileReference 使用封闭 whole/lines 选择，typed 构造器验证相对路径与正整数范围，零值不可用。FileAttachment 包含引用、源总行数、实际起止行、末行是否部分保留、UTF-8 内容、truncated 和内容派生 digest，提供严格 decoder、validator、纯包装和副本读取。所有集合私有持有并深拷贝；上层不得透传可变 slice、map 或 any。文件集合与最多一个现有 Skill 附件组合，而不把 Skill 扩展成无边界附件容器。

在 app 装配可取消的输入准备组合：既有 Skill 准备、文件准备都完成后，才调用现有 AgentLoop admission。以实际消费者所需的小接口替代当前仅 Skill 的准备入口；保留 Skill catalog 发布和开始工作后的安装关闭规则，不建立任意注册 pipeline。输入身份仍由现有 owner 分配。

内容准备在任何附件来源 I/O 前先完成整条原文、全部引用语法、路径、数量和静态额度验证；不能读完第一份文件才发现第九个引用或非法尾部。草稿中的 typed 引用必须与原文共享解析器得到的完整有序集合一致，宿主不能借伪造引用列表读取正文未明确选择的路径。文件全部准备成功后，静态 admission 只检查附件、队列及编码可表示性；实际 input batch 在 writer 接收前还须完成 native/record/batch、上下文请求以及强制提交事件的编码与摘要预检。动态预算与 PreparedUserInputs.CheckRequest 必须在实际交付操作中使用捕获的 TurnInput，不能在准备时冻结启动 profile 后把旧结论当作后续采样许可。确定性展示编码失败不能在成功 Sync 后才使输入进入 uncertain。

#### 1.1 内容冻结与推理策略捕获分别遵守既有 owner

FileAttachment 和排队草稿只冻结内容、引用和输入身份，不保存 effort、ProviderProfile 或 selection_revision。既有 thread 输入 owner 在普通 turn 的唯一 worker 启动前捕获选定 profile；尚未启动的 strict-next 输入，即使文件早已准备，仍在真实启动时取最新选择。进入当前 turn 的自动追加、工具安全点与 Stop steering 则继续使用该 turn 的旧捕获值。准备失败、取回、重提或文件扫描都不是应用新 effort 的边界。

实际输入预检、body、properties、receipt、自动摘要及 Stop discard/rebuild 均传递同一操作策略。独立手动摘要在接纳时捕获自己的策略，/compact 参数中的 @ 保持文字。不能从 runtime.profile 的启动设置、UI 快照或文件来源自行创建另一个 sampling input；复用现有 operationProfile 与 samplingInput 路径。effort.set 接纳顺序仍由原 owner 决定，不新增输入队列或持久化策略事实。

terminal 之后的 finishing 必须等旧 worker/stream 真正清理完成，文件来源已经完成准备或取消也不能提前刷新 idle profile、释放 Session lease 或启动下一 turn。用 owner 接收、文件读取和 worker 完成屏障验证这些独立线性化点，不以计时猜测请求使用哪个策略。

选择这一方式，是为了让普通 headless 文字里的 @ 保持原文，并让 TUI、Runtime 和 Provider 不自行读取文件。仅在请求编译时读盘会绕过 admission 预算、队列冻结与恢复事实，故不采用。

### 2. 语法与编辑目标共用同一纯解析规则

- 引用在行首或 Unicode 空白后以 @ 开始，支持 `@src/main.go`、`@src/main.go#L10`、`@src/main.go#L10-20` 和 `@"docs/design notes.md"#L2-5`。引号路径按严格 JSON 字符串解码；# 范围只能在结束引号之后。未引号路径以空白结束，含空白或 # 的文件名用引号。
- `./` 可在纯内存规范化时去除；拒绝绝对路径、drive/UNC、`~` 展开、反斜杠路径、任意 `..` 组件、空组件、NUL/控制字符和超过 4096 bytes 的路径。只接受正的闭区间，逆序、溢出和非行范围 fragment 返回安全错误，不猜测另一个文件。
- `foo@example.com`、`@@name`、转义的 `\@name`、行内代码与反引号/波浪线代码围栏中的 @ 保持原文。引号未闭合时允许编辑搜索，正式提交则报错；引号内反引号和 # 是路径数据，不开启代码或范围。JSON Unicode 转义只接受合法 surrogate 配对，不把非法转义替换为另一条路径。路径后的行范围单独解析，补全只替换路径片段；用户文字和范围保留。标点紧邻未引号路径时按文件名处理，用户可用引号消除歧义。
- 按首次出现次序对同路径及同选择去重；同路径不同范围分别保留。至多 8 个不同引用，任何准备失败整条输入不接收，原草稿/光标/历史不变。原文始终保真保存，不剥掉 @ 或把附件拼入原文。

### 3. 有界搜索与现有编辑焦点组合

文件名查询通过独立的 typed 宿主查询接口和结果流完成，不添加搜索 RuntimeCommand/Event。目录项与发现规则是宿主读取的元数据；候选不读取待附带文件正文。查询保存 workspace/session generation、composer revision、路径编辑范围、请求 generation 和用于生成结果的 index revision；这些身份只存在内存。结果只有目标、请求和候选快照全部仍有效时可接受。

#### 3.1 工作区本地过滤策略

复审核对 Claude fileSuggestions.ts:248 的 tracked/untracked 分路、:289 的 .ignore/.rgignore 过滤，以及 Codex file-search/src/lib.rs:420 的 ignore walker 和 lib_tests.rs:502 的父目录规则回归。候选需要明确排除生成目录，不能只排除 .git 后扫描整个 node_modules。

本次选择确定性的工作区本地发现规则：对每个可遍历目录，按 .gitignore、.ignore、.rgignore 顺序加载规则，越深目录规则覆盖祖先、同文件最后匹配优先。只读取 Workspace 内的这些固定名称；不读取上级/用户规则、.git/info/exclude 或 Git index，不执行 git/rg。隐藏文件参与发现。无 Git 的目录同样使用这些本地规则；这是候选过滤策略，不模拟 Claude 的 tracked 例外或 Codex 的仓库探测策略。被过滤文件仍允许通过完整手输路径明确准备，过滤不代表读权限或授权。

模式语义参照 [Git 官方 pattern format](https://git-scm.com/docs/gitignore#_pattern_format)：注释与转义、尾空格、否定、相对锚定、目录规则及通配符；排除目录不遍历，其内部规则不能自行救回子项。分别测试前导/中间/尾部 **、*、?、字符类、!/转义 !/#、父目录重新纳入与嵌套覆盖。名称按实际 UTF-8 保留，匹配大小写敏感、不隐式规范化。不能把当前 Glob 工具参数 decoder 当成 ignore parser；只共享语义相同的 FD 与有限读取机制。

规则文件必须以同一 FD 安全读取并验证 UTF-8/NUL/类型与一致性；每文件至多 64 KiB，本次发现累计至多 512 KiB、4096 个有效规则，单条模式至多 4096 bytes。缺失为无规则；不安全、不可读、非法或超限的规则使所属子树整体省略并显示 ignore_unavailable，不得当作空规则扩大候选范围。规则在该次发现读取后冻结，关闭再打开候选才重新发现。每次发现的 ignore 匹配最多消耗 32000000 个匹配状态转换，超过则结束为不完整；循环内可取消，不能依赖 deadline 约束一次无限回溯。

#### 3.2 一次候选交互复用一个文件名索引

宿主显式 Start 一个搜索 worker，它分批推进发现与匹配，共用一个最新待处理查询槽；一个逻辑文件候选 session 最多有一个目录发现。首次打开 @ 目标建立 session，以 sigil 编辑锚点识别同一目标；路径输入使 token 末尾和查询改变，或在同一路径内移动光标，都只替换匹配任务，不重启目录发现。离开锚点/目标后重新进入则是新的 session。否则快速打字可能让遍历永远停在前缀子树。单 worker 在有界步骤之间优先处理取消和最新查询，不在每按键时创建 goroutine、等待者或另一棵索引。

索引只保存规范相对文件名和匹配所需元数据，最多 50000 项/16 MiB（包含名称与元数据计费），不保存文件正文；目录在完整枚举并排序后才向索引发布条目。每个有界追加批次推进 index revision，结果携带对应 revision，旧索引快照不能覆盖较新结果；TUI 仍只保留最多 50 项/128 KiB 结果。只保留一个索引，不逐次复制全量路径。全部 New/getter/View 无 I/O，不预热进程级目录或保存全局缓存。

目录读取每批最多 256 个名称，深度 64、原始条目总量 200000，单目录 4096 名称/1 MiB，在途名称栈合计 8 MiB。当前目录读完前任一枚举额度耗尽，则丢弃该目录未使用的全部名称并明确 incomplete；不能先 Readdirnames(-1) 后截断。既有 Glob/Grep 复用批量读取机制，保持其原有策略，不套用本次过滤、单目录或索引上限。

一次目录发现有 2 秒 deadline，查询变化不重置 deadline。到限、I/O 失败或超时可保留此前已完整发布目录的路径，并标记具体不完整原因，不能显示为空且声称已搜完。匹配每次至多 16000000 个状态转换，分批检查取消；到限结果同样标记不完整。状态计费覆盖消耗字符/模式的推进和重复尝试，不能只数候选数来隐藏昂贵内层循环。额度需允许代表性的多千路径/多规则场景正常工作，以任务 7.6 记录实际覆盖与性能；相同已发布快照的排名确定，不承诺不同 deadline 时刻产生相同覆盖子集。

Esc、paste、modal、接受、提交或离开目标使本次 session 关闭：立即取消发现/匹配并使所有旧结果不可接受，worker 确认退出本次工作后释放索引；重新打开建立新 session，因而可发现新建/重命名文件和新的忽略规则。不新增 watcher 或静态跨候选缓存。关闭期间只保留一个最新请求，不能同时启动第二次发现；根关闭仍须等待最终 Done。

#### 3.3 结果与按键的接收边界

排名按完整路径精确、完整路径前缀、文件名前缀、路径子串、路径子序列，再按规范相对路径排序；ASCII 大小写仅用于候选匹配，不改变真实路径，不做 Unicode 规范化。候选仅提供本次引用语法可表达的普通文件路径：非法 UTF-8、控制字符、反斜杠、超长等不受支持名称跳过并记入省略统计，不把替换字符或显示转义当成原路径。只看文件名不承诺正文是 UTF-8，也不按扩展名伪造内容可用性；正式提交再验证正文。裸 @ 显示有界结果；查询有效 UTF-8，解码后至多 4096 bytes，引号原始编辑片段至多 24578 bytes，超限不搜索且不裁成另一条路径。

新查询开始后旧结果可保留为明确的 loading 展示，但立即失去可接受资格；不能在旧行上 Enter/Tab 把 A 的路径写进 B。最新结果到达时按完整路径身份保留仍命中的选择，否则选首项。loading/empty/error/incomplete 与可接受候选分开；无本代可接受项时不套用旧选项，普通 Enter 走提交并可给出准备错误，Esc 仍可取消本次候选交互。流式更新不能触发新搜索。

#### 3.4 接受、范围与光标

路径补全只在路径编辑区激活，光标进入 #L 范围、结束引用之后或其他正文时关闭；范围本身不被 fuzzy search 或自动修正。Tab/Enter 仅替换完整路径片段，保留原有合法或尚待修正的范围及 token 外文字。插入文本由与解析器配对的纯 FileReference serializer 生成，必须满足 parse(render(reference)) 等于原引用；不能只凭空格决定引号。空格/#/双引号/反引号、前导 @ 等会与语法冲突的名字使用规范 JSON 引号，例如 @special.ts 插入为 @"@special.ts"，避免变成表示原文的 @@special.ts。替换前核对草稿与目标代次，超 4 MiB 原子拒绝。

接受后光标越过结束引号与整个范围，停在引用后的合法分隔边界；没有分隔时补一个空格，已有分隔不重复添加，不跨入组合字素。候选保持抑制，第二次独立 Enter 才提交。与参考工程不同，本次不实现 Tab 公共前缀展开、目录候选、图片选择或 atomic mention chip。

Esc 两步、Ctrl+C、Alt+Enter/Ctrl+J、paste、审批/问卷/Hook/面板、resize 和极窄屏沿用现有焦点优先级。文件候选与命令候选互斥，接入现有 chat_space 的 composer、可见 queue、状态和 effort 短通知预算；最多五行是可用空间内的上限，不能另扣一套固定终端高度。内容区不足时不接受看不见的候选，保留输入区及 FIFO 队首；resize 不裁掉队列事实或丢失待执行输入。effort 是短通知而非新增 modal，读取其快照不得重开文件候选或取消当前有效查询。

文件候选接受、取回、返回托盘恢复及历史召回都走统一 composer 编辑失效路径，使旧 effort 局部反馈和旧文件结果同时失去改写草稿的资格；文件查询身份独立于 effort.selection_revision。迟到 effort 快照仍可沿既有权威选择水位更新选择信息，不能借此清空新文件草稿、抢焦点或恢复旧活动状态。显示转义和路径截短只处理副本；完整路径和范围始终用于实际输入。

### 4. 文件内容与资源预算

读取使用当前 Workspace 的逐段 no-follow opener，并以同一 FD 的类型/权限/大小和读前后 dev/inode/mode/size/mtime/ctime 检查约束对象。任一检查失败、身份变化或实际读取量与最终大小不一致均拒绝，不能像现有 Read 降为 unavailable observation 后继续给附件正文。不使用 stat/Lstat 后普通 Open，不跟随 symlink；路径在安全打开后重命名或替换时可以继续冻结同一已验证 FD 的内容，不重开路径。复用 Read 的 UTF-8/NUL 检查和 16 MiB 源文件上限，通过小型读取 helper 共享机制，不调用 Read executor 或制造 ledger。

每条输入最多读取 64 MiB 源 bytes，同路径多范围借用同一安全打开对象并只读一次；真实读取和失败/取消消耗计入本次预算。最多保留每附件 64 KiB 包装文本、合计 512 KiB。whole 默认前 200 行；显式范围最多保留 2000 行，仍受字节上限限制。只在合法 UTF-8 边界截断，保存实际范围与 truncated；无可容纳的正文时返回大小错误。空文件合法，实际范围为 0..0；范围起点超过 EOF 失败，终点超过 EOF 取实际结尾。包装和元数据开销在上限内预留，不能先拼接无界文本再检查。

源只保留一个至多 16 MiB 的文件缓冲，按同路径分组选择全部范围，再按原引用次序发布；选中片段必须独立保留，不能用小 substring 长期持有整份源缓冲。64 KiB 指最终固定标题与 canonical JSON 包装的 UTF-8 长度，不是原始正文长度：双引号、反斜杠、控制字符的 JSON 转义膨胀也计费。使用有界 JSON 长度计算/编码选择可容纳前缀，不能对每个字符前缀重序列化整份内容形成平方复杂度。

行号以原始 LF 边界计算，非空文件末尾 LF 不产生额外空行，空文件为零行；选中范围含原本属于这些行的终止符。CRLF、裸 CR、BOM 和最后是否有换行保持原始 bytes，不使用当前 Read 的 splitTextLines（它会去掉行尾 CR）或重新 Join。截断不切开 UTF-8 或 CRLF 对；实际 end_line 指最后保留了字节的源行，部分末行由 ends_mid_line=true 与 truncated=true 明示，不能让用户把该行当作完整。total_lines 保存本次完整源读取观察到的总行数，使纯内存 validator 能判断请求与 EOF 的交集；truncated=false 必须覆盖这个交集且不含部分末行，不能用真 digest 掩盖不一致的范围。校验只证明冻结事实内部一致，不把 digest 当作来源真实性签名。

附件包装为固定标题加 canonical JSON 数据对象，包含相对 path、请求选择、实际范围、truncated 与 content；文件内容不能通过伪造结束分隔符逃出数据对象。digest 来自这些冻结字段，不含 mtime、inode、绝对根或 InputID。Provider 在首次提交生成实际文本块；恢复只校验存储块与当前强类型字段一致并直接复用存储块，不重读、重新截断或重建另一份请求文本。

准备失败使用封闭 typed failure，app 映射现有 invalid_input 与固定英文 reason：file_reference_invalid、file_reference_outside_workspace、file_reference_unavailable、file_reference_unsupported、file_reference_encoding、file_reference_changed、file_reference_limit 或 file_reference_busy；平台能力缺失保持 unsupported_platform，context 取消保留既有取消语义。控制流读取 typed kind，不匹配错误字符串；不回显底层错误、绝对路径或正文。若文件在完成准备后改变，已 queued 内容保持原冻结值；取回/returned tray 只还原用户文字，重新提交读取新的内容。

### 5. 唯一当前持久化与原生恢复

当前 InputMetadata 增加必需非 null 的 `files` 数组。每项只关联一个本批 InputID 和该输入的有序 FileAttachment 列表；同 InputID 最多一个集合，集合非空且至多 8 项，无附件时顶层 `files: []`。保留现有 skills 语义，所有 metadata 深拷贝。缺失 files、未知字段、重复 JSON 成员或非法关联直接拒绝；这是语义字段，不是版本标记。

当前存储字段明确为 files 中每项的 input_id 与 attachments；附件的必需字段按顺序为 path、selection、total_lines、actual_start_line、actual_end_line、ends_mid_line、truncated、content、digest。selection 仅允许 {kind: whole} 或 {kind: lines, start_line, end_line}，whole 不能含行字段，lines 不能缺失字段；digest 是除 digest 外附件字段的 canonical bytes 的小写 SHA-256。模型包装为固定 `# Attached workspace file\n` 加这份不含 digest 的 canonical JSON 再加 `\n`，不加入格式 revision。包装作为实际 native text 保存并在恢复时逐字比对，getter 不能访问磁盘。

每条原生用户消息的块布局固定为：原始文字、可选 Skill、按首次出现顺序排列的文件包装。Anthropic 使用自身 text block，OpenAI 使用自身 input_text part；只扩展这两个已真实消费的编码分支，不统一 wire。decoder 验证块数量、类型、角色、InputID 归属、范围、上限、digest 与包装 bytes。非空文件集合必须等于原文解析后的完整有序唯一引用集合，不接受只保存其中一部分、遗漏路径或额外文件；literal 输入允许原文包含 @ 而 files 为空，不能在恢复时通过重新解析原文自行读取。字段合法、digest 合法及原生文本一致三者都必须成立；重算 checksum 或 digest 不能替代语义校验。

仍然提交现有 `turn_inputs_committed` + `provider_native_commit` batch，沿用 Sync/finalize、取消线性化与 poisoned 处理；没有新 Session 外层 record、activation record、SQLite 字段、格式版本或兼容 reader。旧 turn 本地补偿、Stop 的反馈加 steering、自动摘要 discard/rebuild 都传递完整冻结附件，不重读来源。恢复及离线读取在 repair 前完成全部校验；合法输入已 Sync 而进程退出时，只按现有中断契约收口，不自动发送模型请求。

生产不迁移、不原地重写旧 journal。实现时替换仓库内不可变 current bytes，并增加缺少 files 的旧开发 shape 拒绝 fixture；测试运行不通过当前 encoder 生成自身预期 bytes。下一请求等价的前提是相同 ThreadID、配置、显式上下文来源、已提交事实、候选输入以及实际捕获的完整推理设置。运行时 effort override 与选择序号不持久化：恢复先使用本次启动值，等价实验必须在两侧显式重选相同 wire 设置再提交；序号和来源可以不同，但请求/native/profile fingerprints 仍相同。若恢复后没有重选而策略不同，只保证历史附件及 opaque bytes 不变，不承诺下一请求相同，也不从附件或摘要猜测旧设置。OpenAI WebSocket full/incremental 各自验证，不能把两种帧宣称为相同 bytes。

### 6. 只读投影、缓存和 UI 内存

SemanticMessage/TranscriptItem 将文件附件作为独立强类型集合关联原 InputID，Text 仍只含用户原文。完整历史保持附件；有效窗口只包括 checkpoint 后附件，摘要不触发文件 rehydrate。所有 clone/getter 和并发查询复制集合，不允许修改投影影响请求。

context/planning 在已有 committed_history source 内增加封闭 file_attachment 内容 kind，按原文、Skill、文件顺序对实际包装计量和 canonicalize；只计模型可见字段，不把元数据 digest、身份或搜索状态作为额外 prompt 内容。与 native footprint 仍取覆盖值。在相同实际推理设置下，原 profile/tools/project/skill_catalog 来源及稳定前缀保持；只有实际提交附件改变历史尾部和完整请求，搜索/焦点/候选不进入任何缓存分段。

独立新操作改变实际 effort 时，provider_profile、请求 properties 与完整请求身份应按既有推理契约改变；这不属于附件污染稳定前缀。固定同一 native 窗口与候选输入分别编译两种策略，隔离文件内容变化与策略变化，不能拿“搜索不影响 prefix”掩盖旧 profile。选择来源或序号变化但实际设置相同不改变任何这些身份。OpenAI WS 只在实际属性和原生前缀均相容时增量；无中间操作的 high→low→high 与真实失败/取消/摘要后选回 high 分别测试，后者不能借相同文件 digest 或 effort 复活已失效的 previous_response_id。

实时 UserInputCommitted 增加独立文件摘要值（total、items、omitted），items 仅含路径、选择、源/实际行数、ends_mid_line/truncated 和实际包装字节数，不复制文件内容。4 KiB 上限针对整个摘要的已编码 JSON bytes，必须计入路径转义。按附件次序保留可完整放入的最长前缀；不截断实际路径字段，单项太长时也可 items=[]、omitted=total，主线显示附件数量与省略状态。详情仍有全部来源，展示截短不能伪装成另一条合法路径。

摘要使用单独 typed constructor/strict decoder；从完整冻结输入派生并在 writer admission 前验证最终强制事件，不通过只有摘要的事件重新构造或验证完整附件。app 的已提交 transcript owner 持有正文，经现有 Ctrl+O 有界分页提供；详情复用四页缓存，主线/recall 不自动展开，待提交/返回托盘只存原文。live 与 restored 使用同一个纯摘要投影，先发生的 queued/prepared 不构成已保存事实。外部 headless JSONL 不透传新的内部字段。

### 7. 资源所有权与完成顺序

app 持有文件输入来源及其 Workspace 借用，显式 Start/Close。来源同时限制一个内容准备和一个搜索；关闭先关接收门、取消读取/搜索、关闭在途可中断句柄、等待准备栈/worker 和最终 Done，随后才可关闭 Workspace。pre-admission Prepare 也计入清理，不得只等待 AgentLoop。锁内只选择 owner/更新有界状态，读取、回调、channel 等待和 Close 全在锁外；不为每次 Wait 创建 helper goroutine。

Start 只能成功一次，失败同步回滚且不发布半个 owner；未 Start 的 Close 仍幂等。过量内容准备立即 typed busy 拒绝，不建立隐藏队列；各请求只持有取消函数/身份，长期结构不保存 context。关闭门与内容借用/搜索接收在同一有界状态转换中线性化：先关门则拒绝，先借用则 Close 负责取消并等待最终结果。候选 session 结束只释放该索引，不等于销毁可继续准备输入的根来源。

宿主退出先停止 TUI 输入、关文件来源接收门并取消 pending/搜索，继续排空现有 Runtime 输出和完成已接收的 durable resolution，再等待文件来源及既有其他来源清理，最后按原顺序关闭 writer/lease、Workspace 和存储。不能先等一个永不退出的 pre-admission Prepare，再发出取消。deadline 只结束调用方等待，不放弃资源所有权或宣称完成；唯一 cleanup owner 保留最终结果与 Done。强制关闭在途句柄仍不能证明底层 I/O 已退出时，不发送虚假的 Done，保留资源和关闭责任直到真实返回；以屏障与失败注入测试此顺序。

## Risks / Trade-offs

- [原生 metadata 更新影响全部 current fixture] → 唯一当前字段一次替换；固定 bytes、缺失字段拒绝、跨进程 lease 与拒绝时原 journal bytes 不变均纳入任务。
- [快速输入使旧结果覆盖新目标] → 不只比较 query，还比较 workspace/草稿/目标/generation；用可控结果顺序验证 ABA，不用 sleep。
- [大目录枚举、忽略模式或文件读取放大内存/耗时] → 原始枚举、规则 bytes/匹配状态、名称栈、索引、结果、源 bytes、包装和输入队列分别设限；复用索引而不重新遍历，扫描不完整可见，完整手输仍可尝试安全读取。
- [源在读取时变化或关闭期间仍准备附件] → FD-bound 校验、读前后检测、pre-admission 借用与显式 cleanup owner；不能静默保留部分附件。
- [附件被当作完整 Read 或指令权限] → 不产生 ReadObservation/ledger/grant，文本保持用户级数据；现有 Write/Hook/Skill 权限回归保持。
- [长路径或转义使摘要超额，Sync 后才暴露事件错误] → 摘要按编码后 4 KiB 保留完整项前缀并报告 omitted；在持久化前验证事件，正文只经有界详情查询。
- [本地发现与参考工程结果不同] → 明确本地规则对 tracked/非 Git 文件同样生效、没有父级/全局规则；精确记录排序与不完整状态，不承诺 deadline 截断后覆盖集相同或大型仓库全量索引。
- [main 新增动态 effort 后沿用启动 profile 或误判恢复等价] → 内容准备不捕获策略，动态预检和所有同轮路径使用实际操作策略；恢复重选、策略变化的 profile 失效与 WS reset 分别验证。
- [候选复用旧高度/反馈路径挤掉输入或清空新草稿] → 共用当前 chat_space 和 composer 编辑失效机制，覆盖 effort 通知、64 条队列、极窄屏及迟到反馈。

## Migration Plan

不存在用户数据迁移。后续 apply 在本分支实现本次当前契约、更新固定 fixture 和文档，再运行本次测试矩阵及完整质量门。方案、源码、fixture 与规格作为同一变更交付；不支持一半升级、双 reader 或 resume 重写。若实现证明这些边界不足，先修订 artifacts 并确认扩大范围，再继续实现。

## Verification

| 层级 | 必须验证的事实 |
| --- | --- |
| 纯逻辑 | 语法/目标一致、surrogate、引号/范围后的光标；ignore 嵌套覆盖/否定/剪枝；CRLF/BOM/末尾换行/空行计数；JSON 转义后预算、digest、完整引用集合与 deep clone |
| 文件边界 | symlink/目录/特殊文件、目录替换、fstat 失败/读取中变更、UTF-8/NUL、长行、源/输出/枚举/ignore 预算；用注入点和屏障证明，无概率竞态 |
| 生命周期 | 一个索引跨查询复用、渐进 revision/ABA/旧候选停用、关闭重开可见新文件；pending 返还、FIFO strict 屏障、工具安全点及 Stop steering；准备/入队/操作启动/finishing 各屏障下的 effort 捕获；Start 回滚/Close gate/Done 与 Sync 线性化 |
| Provider | 两家 text+Skill+多文件固定 golden；prepared/finalize、opaque 保真、缺 metadata/错块/错范围拒绝；两侧显式重选相同设置后的 uninterrupted/restored 请求/native/footprint/fingerprint 等价，不重选时只有历史保真承诺 |
| 缓存/压缩 | 固定策略下搜索零请求/零 prefix 变化、附件只影响历史尾部；真实策略变化可定位 provider_profile，选择序号不入身份；Anthropic 缓存与 OpenAI thread key/WS full、增量、失败/摘要重建；全部摘要/恢复均不重读来源 |
| Session | 固定 current JSONL、input-only 崩溃与二次恢复、删除来源后的离线 preview/resume、真实子进程 lease 竞争/崩溃释放/单调 seq/失败 bytes 不变 |
| 交互/回归 | 固定尺寸和真实 Darwin PTY：正文中间/引号/范围/分隔插入，旧 loading 行不可接受，七项内置命令参数抑制与 $effort Skill+文件；effort 通知/64 条队列共同布局、迟到反馈；忽略文件与手输、关闭重开、paste、取回、停止、modal、resize、NO_COLOR、Ctrl+O；headless 文字及九工具/Skill 保持 |
| 全量 | `make verify`、本 change strict 校验和全部 OpenSpec strict 校验；记录平台、命令、结果及无法运行的项目，不以交叉编译替代原生验收 |

验证记录在实现阶段写入本 change 的 verification.md；本轮基线结果由最终交付说明记录，不勾选任何实现任务。

## 2026-11-15 方案复审闭环

本轮重新读取了两个已固定 revision 的索引、typeahead、composer、ignore 回归和附件代码，并与当前 EasyCode 的安全 opener、Read 分行、提交事件和 app cleanup 逐项核对。以下均为已修订的方案缺口，不是实现阶段已通过的测试。

| 编号 | 原方案缺口及证据 | 本次决定与验收落点 |
| --- | --- | --- |
| R1 | 仅排除 VCS，漏掉两家已有的 ignore 处理 | 本地分层规则、剪枝与独立 I/O/计算预算；specs/tui/input-completion，任务 2.6/2.7 |
| R2 | 每查询重走目录，快速输入可能反复取消而没有进展 | 单次候选索引复用、渐进 revision、明确覆盖不完整与重开刷新；任务 2.4/2.5/6.2 |
| R3 | 只拒绝迟到结果，未停用已显示旧列表；光标可能留在 #L 前，前导 @ 等名字不能按简单引号规则往返 | 新请求先停用旧行、按路径保选择、共享 serializer 往返与移出范围；任务 6.1/6.3/6.6 |
| R4 | 当前 Read splitTextLines 会去掉 CR，且结尾 LF 的空行口径未定义 | 原始 bytes 的 LF 行跨度、CRLF/BOM 保留与有效截断；任务 1.3/2.3 |
| R5 | 原文长度不能代表 JSON 包装大小；多范围小片段可能保留整个大缓冲 | 最终包装长度、有限复杂度选前缀、同路径分组与独立片段；任务 1.3/2.3 |
| R6 | native 字段和完整引用集合只作概括，缺少 EOF/部分行事实，可能遗漏附件或伪造完整性 | 确定唯一字段及 total_lines/ends_mid_line、选择 union、digest/范围/块/完整集合验证；任务 4.1/4.2/4.3 |
| R7 | 八个 4096-byte 路径不能都放入 4 KiB 摘要；当前提交事件在 Sync 后创建 | 独立摘要 total/items/omitted、完整项前缀与编码前检；任务 3.2/5.5/5.6 |
| R8 | 关闭顺序未覆盖 Start 失败、busy、关门与 pre-admission 借用竞争 | 显式回滚、关门时先取消再等待、真实 Done 前保留资源；任务 2.5/3.4/3.5 |

保留的明确边界：工作区内普通 UTF-8 文件；直接手输可以绕过候选过滤但不能绕过安全读取；不新增目录附件、图片、watcher、祖先规则读取、Git index 解析或参考工程运行依赖。未发现需要另换能力方向的阻塞项，实际行为已由后续 apply 的测试矩阵和完整质量门验证，证据见 verification.md。

## 2026-11-16 main 合并后的方案复审

本轮已 fetch origin/main 并将当前分支从 5c02ae6 快进至 b0387c4，无文本冲突；合并前后的十份既有 change 文件逐字节一致。随后将 main 的已归档 effort 契约、实际 owner/Provider 预检/TUI 布局与本方案复核，并重新只读核对上表固定快照的引用准备、替换光标、搜索 session 与过期结果逻辑。以下记录该次方案复审的语义整合；其后实现和最终验收见下节及 verification.md。

| 编号 | 合并后发现的缺口 | 本次决定与验收落点 |
| --- | --- | --- |
| R9 | 原方案未区分提交时的内容冻结与操作启动时的动态 effort 捕获，预检可能误用启动 profile | 使用既有 operationProfile/TurnInput；strict-next 取启动时选择，同轮工具/Stop/自动摘要保留旧策略，finishing 不提前启动；任务 3.7/7.7 |
| R10 | “相同配置恢复等价”漏掉只在进程内存在的 effort override | 恢复重置后显式重选相同设置再比较请求，序号不同不影响身份；不重选只保证历史附件/opaque 保真，禁止策略持久化；任务 4.4/7.7 |
| R11 | 仍写六项内置命令，未防止 /effort 或 /compact 参数出现不生效的文件候选 | 复用 domain 保留名纯 switch，保留七项及 256 Skill 容量；$effort Skill 可带文件，本地参数无发现/正文 I/O；任务 6.7/7.7 |
| R12 | main 新增 effort 短通知及真实 queue 行预算，旧布局和反馈若另起路径会隐藏候选或覆盖新草稿 | 文件/命令候选互斥并共用 chat_space 与编辑失效，迟到反馈不能清草稿；任务 6.4/6.7 |
| R13 | 稳定前缀不变的断言未限定实际推理策略，WS 也可能误以同文件/同 effort 恢复旧 baseline | 分开验证附件与策略的失效来源、同 wire 不同序号，以及失败/取消/摘要后的 full 重建；任务 5.7/7.7 |

四条新增组合任务只验证本能力与现有 effort 的衔接，不扩展 effort 的档位、作用域或持久化格式。该次复审将任务扩展为 47 项，当时尚未进入实现；后续已完成 47/47，实际验证结果记录于 verification.md。

## 实现落点与验收核对（2026-11-17）

- 交互入口使用 ChatSession.Append(ctx, domain.InputDraft) 与 NewSubmitConversationalInputDraftCommand；普通 headless string 构造器显式使用 literal draft。组合端口为 Runtime.InputPreparer/InstallInputSources，workspaceInputSource 是同时消费 Skill 与文件准备的实际 app 来源，没有旧 SkillPreparer/InstallSkills 兼容分支。
- domain 的 FileReference/FileSelection、FileAttachment/FileAttachments、InputDraft、FileSearchQuery/FileSearchSnapshot 与 FileSummary 均有实际消费者。InputMetadata.files 必需；UserInputCommittedPayload.files 与 TranscriptListItem.files 是独立摘要，主线和 Ctrl+O 使用同一冻结值派生；离线 PreviewMessage 只保留 FileSummary，不把文件 body 带入 picker。
- 搜索 worker、准备 gate 与生命周期位于 internal/app/file_source.go、file_search_worker.go；排序/有限状态匹配在 file_matcher.go。文件 no-follow 打开、稳定 FD 读取、有限目录批次与 ignore 规则位于 internal/tool/builtin，而解析、截取、序列化和布局不执行 I/O。
- 目录恰好 4096 项时增加有界 EOF 探针，探针计入原始条目预算；4097 项整目录省略。TUI 的命令/Skill 与文件互斥行数先失效再布局，避免旧补全行影响 effort/队列视口；解析缓存绑定 composer revision/cursor，恢复草稿显式失效，流式与 resize 不重复查询。
- 当前记录仍为 turn_inputs_committed + provider_native_commit；native kind 是 user_inputs/sample/tool_outputs/hook_feedback，checkpoint 为原有独立 envelope。没有新增 Session kind、格式版本、迁移器、Provider capability、工具或外部 JSONL 字段。固定 current bytes 已替换，坏语义在 repair 前拒绝；无自动恢复联网或来源重读。
- 文件原生/request/cache/Stop/checkpoint/WS goldens、TUI snapshots、实际额度、取消/Sync/关闭、真实 input-only 子进程崩溃、双 Provider 与 NO_COLOR 的 PTY 均已落地；完整命令和最终质量门结果见 verification.md。apply 阶段保留待归档，不自动提交或推送；后续归档状态见 [archive.md](archive.md)。

### 实现后代码复审（2026-11-17）

按原有 FD-bound、真实请求缓存位置与输入交互契约修复三处遗漏：目录 `openat` 和读取 `fstat` 在 `SyscallConn.Control` 内持有描述符借用，避免取消 Close 使数字失效或复用；Anthropic 缓存桥接累计真实 FeedbackInput 消息，与请求构造的位置一致；读后快照内发生取消时先保留 context 取消结果，再判断文件变化。新增关闭门和读后取消屏障、Stop+Skill+多文件长窗口的 marker golden、采样/摘要与恢复等价回归。

真实 PTY 复验另发现脚本会丢弃先到回执，以及首次 Tab 依赖搜索先后顺序；增加回执缓冲、动作水位和显式分段粘贴边界，生产 TUI 接受规则保持。问题、修复前失败证据与本轮最终验证记录于 [review.md](review.md)。原 47 项任务、唯一当前格式和能力范围保持。
