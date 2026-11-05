## Context

动机见 [proposal.md](proposal.md)。本次读取了 Roadmap、总体架构、踩坑记录及 `tui/basic-chat`、`tui/tool-transcript`、`provider/transcript-projection` 全部要求。当前无其他活动 change，开始探索时工作树干净。

实际入口：

| 文件 | 当前行为 | 设计影响 |
|---|---|---|
| `internal/tui/tool_presenter.go` | `renderTranscript` 将正文净化后硬换行，工具行独立分组 | 在文本分支组合助手渲染器，保持工具分组边界 |
| `internal/tui/model.go` | `View` 每次重新布局；窗口消息由 Update 接收 | 在 Update 准备布局，View 只组合已准备结果 |
| `internal/tui/transcript_state.go` | 主线最多 50 项；流式正文超过 64 KiB 保留尾片段；commit 清除 provisional 后异步查询 | 用类型字段记录完整性，不能靠提示文案判断；提交后失效旧解析状态 |
| `internal/app/transcript_queries.go` | 文本列表只给 512 字节展示摘要；正文查询保留原文与 UTF-8 偏移 | 不把摘要当 Markdown 原文；通过现有正文端口按需补齐 |
| `internal/tui/transcript_browser.go` | 四页缓存、64 KiB 正文页、generation 拒绝迟到结果 | 复用查询和所有权，新增缓存须计入同一预算 |
| `internal/tui/model.go:commandDisplayText` | 保留换行/Tab，替换 control/Cf | 继续只处理展示副本；字素中的 ZWJ 需专门验证，不能借改 Markdown 全局改工具展示 |

### 两份参考工程的证据与取舍

实际参考位置为主 checkout 的同级目录 `/Users/cutejiuge/IdeaProjects/`，不是当前 worktree 的 `../`。只读使用，未构建或修改。

| 参考快照 | 阅读位置 | 吸收 | 不照搬 |
|---|---|---|---|
| Claude Code sourcemap `a8a678c` | `restored-src/src/components/Markdown.tsx`：Markdown、StreamingMarkdown | 紧凑块间距、稳定前缀与活动尾块分离、正文缓存 | 模块级 Map、只检查前 500 字符的语法快路径、剥除 prompt XML 标签 |
| 同上 | `components/MarkdownTable.tsx`、`utils/markdown.ts` | 终端宽度布局、列内换行、过窄/过高时纵向字段展示；代码与列表结构 | 运行时加载依赖、OSC 链接、全局解析器配置 |
| Codex `6f8bf683c` | `codex-rs/tui/src/streaming/render.rs`、`markdown_render/streaming.rs` | 以源字节与块边界追踪布局；引用定义到达后失效前文；resize 重算 | Rust/ratatui 类型、渲染时锁内回调、全局主题读取 |
| 同上 | `history_cell/markdown_render_cache.rs`、`streaming/render_tests.rs` | 单宽度缓存；每个流式前缀与完整渲染比较的测试方法 | 多主题、数学、Mermaid、文件链接解析等额外范围 |

仓库架构文档记载的 Codex 参考提交 `c248f6d48b` 与实际 checkout 不同；本方案以上述实际提交为证据，不声称复现其他版本的产品行为。

## Goals / Non-Goals

**Goals:** 按 Claude Code 的紧凑消息阅读体验组织布局，吸收两方的块缓存、稳定尾部、源位置和回归方法；完成、回看、恢复保持正文连续，显示不能参与模型事实构建。

**Non-Goals:** 不扩大 Provider/Session 协议；不实现全历史虚拟滚动、跨 64 KiB 页的 Markdown 重建、主题系统、OSC 超链接、剪贴板、HTML/图片执行、reasoning 展示。基础代码高亮属于本次交付。

## Decisions

### 1. 以连续阅读选择本切片

现有 tools、恢复、压缩和缓存已可复用，本切片无需引入扩展信任或新的持久化事务。助手正文采用 Claude 风格的消息标记、统一正文缩进、紧凑块间距；流式与完成保持同一布局语言。工具活动仍由原 presenter 负责，用户正文不解释 Markdown。

新增参考证据：Claude `components/messages/AssistantTextMessage.tsx` 普通正文直接进入 Markdown，`components/Messages.tsx` 的 StreamingMarkdown 使用相同圆点和缩进；`VirtualMessageList.tsx` 具有位置锚点，但受模式开关影响。Codex `streaming/controller.rs`、`prose_preview.rs` 把完整行、表格暂留和有界未完成行预览分开，`render_tests.rs` 检验解析等价。这些是本地快照行为，不宣称完整复制产品。

### 2. 组件组合与依赖边界

建议按职责新增 `markdown_parser.go`、`markdown_layout.go`、`markdown_stream.go`、`markdown_highlight.go`、`transcript_viewport.go`；现有 `transcript_state.go` 负责投影交接，`transcript_browser.go` 复用布局与共享页面仓。文件名可微调，禁止将 parser、调度、缓存、键盘和 I/O 聚合为超级 Model。

结构为有界原文/身份 -> 解析文档 -> 块布局与源位置 -> 主线/详情投影。parser/高亮适配只在 TUI 内产生私有强类型结构；View 只组合 Update 已准备的行，无 I/O 或缓存写入。New 纯内存，样式由 app 启动时显式注入，NO_COLOR 非空、TERM=dumb、非终端输出选无色。没有自动终端背景探测。

Markdown 继续选固定 Goldmark v1 发布版本，实例 parser 和扩展，不用全局默认 renderer；apply 首项仍核对 Go 1.24、许可证、实际调用路径的构造、全局状态和资源预算。高亮改用项目内实例持有、预算受控的有限语言词法扫描器，不引入 Chroma/regexp2。此前依赖检查的历史证据见 [dependency-check.md](dependency-check.md)：已检查的 Chroma 版本在匹配时隐式启动全局超时时钟，单规则超时也不等同代码块预算。该记录中的“待确认”建议已获用户批准，由本节落实，不再是当前待决事项。

选择显式状态机，是为了在主线程内给出可计数的前进、分配和退出边界。保留全部基础高亮语言和交互能力；不新增高亮子进程、IPC 或后台 worker，不通过修改第三方全局超时配置规避生命周期要求。基础高亮是词法着色，不承担编译器、类型推断、符号解析或语言服务器职责。

#### 2.1 组合、输入与结果

`markdown_highlight.go` 负责语言选择、预算和结果，按语言拆分私有扫描规则文件；共享游标、UTF-8 解码、转义处理及 token 输出等语义一致的逻辑。语言规则只返回词法动作，不访问 Model、Provider、Session 或 I/O；不建立通用插件 registry，不通过 struct embed/shadow 模拟分派。语言、词法状态、token 类别与失败原因使用私有枚举和明确结构，错误文案只在展示边界映射为英文，不靠字符串驱动控制流。

实例持有配置及有限状态；每次代码块扫描独立重置游标、栈和工作计数，文档持有成功 token。无包级可变关键字表、scratch pool 或缓存；别名、关键字使用固定 switch，实例间无共享可变内存。New 仅纯内存构造；扫描同步、无 I/O、无锁、无 goroutine、无定时器、无用户回调。并行测试使用不同实例，不承诺同一可变实例并发复用。

输入为代码块的展示文本及其到原文的映射。token 仅保存类别和该输入的 UTF-8 字节区间，不复制 token 字符串；区间必须连续、无重叠、无遗漏并覆盖整个输入。拼接 token 切片逐字节等于输入（含 CR/LF、Tab、空格、空行），安全净化及终端换行在统一布局边界处理，不通过高亮补换行、改缩进或实体解码。对于解析前已有净化的展示副本，继续通过已有映射回溯原文，禁止把展示偏移当作查询偏移。SGR 只由样式映射产生，源内容中的 ESC/OSC/bidi 不得因 token 分类绕过净化；样式边界不得切开字素，跨 token 的单个字素采用确定的首个非空 token 样式。

#### 2.2 有限语言覆盖

共享类别为普通文本、关键字、标识符、数字、字符串、注释、运算符/标点；额外区分 JSON 属性名、Shell 变量。无需语义分析即可识别的类别必须真实着色，不能把受支持语言全部当纯文本冒充实现。

| 语言及固定别名 | 必须覆盖的词法结构 | 易错边界与验证样例 |
|---|---|---|
| Go：go/golang | 关键字、Unicode 标识符、十进制/二八十六进制及浮点/指数/虚数、转义字符串/字符、反引号原始字符串、两类注释、运算符 | 注释标记出现在字符串中；原始字符串跨行；数字分隔符；未闭合字符串不能吞掉后续独立块 |
| JSON：json | 属性字符串、值字符串、转义/Unicode 转义、数字、true/false/null、结构标点 | 冒号前空白；字符串内转义引号/冒号；非标准注释或非法数字只按可识别词法显示，不修复或执行 |
| Python：py/python/python3 | 关键字、Unicode 标识符、数字、单/双/三引号字符串、r/b/u/f 及合法组合前缀、注释、运算符 | 三引号跨行、转义、raw 尾反斜杠；f-string 的整个字面量保持字符串样式，跟踪替换字段括号及其引号以准确找到外层结束，不要求内部表达式语义着色 |
| JavaScript/TypeScript：js/javascript/mjs/cjs/ts/typescript | 两者关键字、标识符、数字/BigInt、单/双引号、模板字符串及插值、两类注释、运算符；明确表达式起始处的正则字面量 | 模板插值使用有界状态栈，字符串/注释中的括号不改变栈；正则转义和字符组；以前一个有效 token 判断常见 regex/division 上下文，无法词法判定时保留斜杠为运算符，不伪称完整语法分析；JSX/TSX 内容保持可读，不新增嵌入语言解析器 |
| Shell：sh/bash/shell/zsh | 控制关键字、单/双/ANSI-C 引号、转义、单词边界注释、变量及参数展开、命令替换、运算符、here-document | # 在普通单词内不作为注释；引号内变量；嵌套展开用有界栈；here-document 按声明顺序识别带引号及 <<- 终止符，正文作为字符串样式，不执行替换；不承诺各 shell 方言完整语法分析 |

别名只取围栏信息串首个语言字段，ASCII 大小写归一化，不按文件名读取资源或自动猜语言。未知语言正常使用安全纯文本，不显示失败告警。未闭合 Markdown 围栏先保持纯文本，围栏闭合或消息完成/失败/取消收口后再扫描；代码本身不完整时能确定的词法区间仍保留，未闭合字符串/注释延续至该代码块末尾，不跨块泄漏状态。无法维持区间不变量则整块回退，不返回部分高亮结果。

#### 2.3 确定性执行和内存预算

不使用回溯正则、从当前位置反复搜索整个剩余输入或无限回退。扫描循环必须前进至少一个输入字节，或完成一次有计数的状态出入栈；UTF-8 只在合法边界产生 token。字符串、注释、数字、模板、here-document 终止符比较、关键字比较及所有 lookahead 都计入工作量；不得把无界字符串搜索或标准库扫描隐藏在一个计数步骤中。here-document 终止符保存为输入区间，逐行比较的字节全部收费；没有复制任意长度终止符或未计费的反复预扫描。

每块展示输入上限 256 KiB（为最多 64 KiB 原文预留净化扩展空间，但不假定所有净化都小于四倍），超出立即返回预算原因；净化本身须在分配前受既有布局预算约束。扫描步骤上限为 `128 × (n + 1)`，其中 n 为展示输入字节数；每次字节读取/比较、状态转换、token 发射收费，耗尽前停止。该计数是算法工作上界，不声称等同墙钟毫秒。嵌套栈及待处理 here-document 队列各最多 64 项，scratch 总容量最多 16 KiB；不构建复制源文的 rune 数组。所有限制由实例预算持有，测试可注入更小值，生产不得提高这些上限。

高亮 token 与 Markdown 节点共用既有 32768 节点预算，token backing array 与源映射按容量计入既有 2 MiB 派生内存预算；临时构建和原缓存同时存在时也计入，必须预留后分配，不能先全量生成再检查。扫描栈/scratch 也计入该共享内存预算。无色模式可跳过词法着色，不改变字符和结构。预算不足或内部扫描失败时释放未发布 token，返回带类型的原因，显示 `Highlighting unavailable: budget exceeded` 或 `Highlighting unavailable: scan failed`；完整代码继续沿安全纯文本路径布局，若布局也达预算，沿现有明确片段/原文入口处理，不截断 durable 正文。

成功结果及失败状态均按文档来源/内容身份和语言缓存；resize、r、View、同一来源的重复完成通知不得重新扫描。同一 source 的预算失败不得在每帧重试，只有 source 改变或页面重新进入可分配的缓存窗口才重新评估。页淘汰释放 token 和源引用。未闭合围栏的小 delta 不高亮；已闭合且 source 不变的块只扫描一次，新增另一块不重扫它，引用定义变化也不使未变代码内容重新高亮。该规则与原有最多四份文档派生状态及 50 项有界主线一致。

### 3. 可见正文加载和阅读位置

主线保留最多 50 个逻辑项，默认跟随底部。普通主线 PgUp/PgDn 在已加载逻辑窗口内翻屏；PgUp 进入回看，PgDn 到底部恢复跟随。更早历史沿用 Ctrl+O，不在本次做无限历史列表。面板和审批的同名快捷键优先；composer 的方向键和字素编辑不变。回看时新输出只显示新内容提示，不移动阅读锚点；窗口范围前移导致锚点被淘汰时移到最早保留项并显示明确提示。

以 cursor + 原文 UTF-8 字节位置 + 行内偏移记录已提交锚点；临时正文使用局部生成身份，不能假冒持久化 cursor。布局生成源位置到显示行的映射。净化、实体解码、表格布局、折行后仍回溯原文范围，合成边框映射到所属块；源段消失时选择最近保留边界并提示。resize、r 模式切换、审批关闭均保留该语义锚点，不固定回顶部；默认跟随状态仍跟随底部。

正文查询复用 ListTranscript/ReadTranscriptBody。列表摘要只作摘要，不将提示拼入解析 source。可见助手项存在 More 时进入加载候选；优先当前阅读锚点、已经可见且已加载项、活动末尾，再按距离和 cursor 排序。查询命令串行取一页，返回后经 Update 调度下一页；去重 pending 和已加载 cursor，不从 View 或 resize 直接发查询。resize 仅重排已加载内容；新出现的未加载项待下一次用户导航或事实更新再纳入查询，保持既有 resize 无 I/O 契约。

同一 cursor 的已提交正文不可变，缓存键含 thread/cursor/source/offset。列表与主线接受结果仍校验 generation/Through；失效的查询结果丢弃，不修改新页面。加载成功只有整篇落在单页时进入完整 Markdown。页面不足、超长、失败显示安全摘要或原文片段及入口。失败在该查询 generation 内不自动重试；用户重新导航/显式重新打开后可重试，不能形成渲染驱动查询循环。

主线和详情共用四个查询缓存槽，不再按 1+3 固定分区。当前列表页也占槽；活动详情、当前锚点正文优先保留，其余可见正文次之，非可见页 LRU 淘汰。出现第五个可见候选时显示显式折叠入口，不因缓存淘汰又立即重查导致循环；进入新的阅读窗口再重新分配。仍可见且预算允许的旧正文不得因为新回复到达退回 512 字节摘要。

### 4. durable 交接与连续 commit

持久化事实来自查询；流式副本没有足够 native item 身份，不能按字符串相等、名称或时间猜测配对。使用现有 commit sequence 作为整段边界，保持以下独立状态：当前已接受投影、至目标 watermark 的暂留展示、watermark 后的活动 delta。

收到 commit 时冻结当前可见区域为一份有界交接视图，启动 Through=sequence 的投影查询；不先清空屏幕。新 delta 进入新的活动区域。等待期间再次 commit，只提升目标 watermark、取消旧查询并更新同一暂留视图，不积累无限快照。通过现有 generation 拒绝旧结果；查询完成并将当前展示预算允许的正文准备好后，在一次 Update 内替换至 watermark 的整段投影，并保留 watermark 后的活动区域。查询缓慢不会阻塞控制流消费、审批或 stop。

交接视图只含可见行、来源标签和锚点，不含可执行状态，不供恢复、请求或工具结果使用。临时 source 按已有 64 KiB 窗口约束；交接视图按下述布局预算裁剪且注明片段。查询失败保留画面并显示 `History refresh failed`，不会标记查询成功或重新执行工具；下一次事实更新或用户重新打开可重试。关闭后全部暂留状态释放。

commit 表示持久化边界，Markdown 稳定块只表示布局可复用，两者不得混同。完成、失败、取消均刷新尚未展示的尾部；取消/失败继续保留未完成标记，重启后仅恢复已有 durable 事实。同 sample 的 text/call/text 以查询结果为准，不用流式 cell 猜 native block。

### 5. 解析正确性与展示节奏分层

完整解析是正确性基准，增量解析使用稳定前缀、活动顶层尾块及文档级引用上下文。不能按空行任意截源串：列表松紧、Setext、围栏、表格和引用定义必须参与边界判断。引用定义变化等跨块影响触发完整有界重算并重建映射；无法证明安全的边界留在活动区。双向源范围只索引对应原文或明确的展示副本，不能混用。

每个 delta 都按序保存原始文本，但不要求每次全量解析文档。普通未完成行使用至多 8 KiB 的展示预览；完整换行或结构边界到达才更新块解析。代码围栏内部优先追加安全代码行，闭合后统一解析/高亮。表格保持活动尾块，完整行到达后才更新列宽；未闭合链接目标暂留，普通标签文字仍可读。过长未完成行只显示带片段标识的有界尾预览。完成/取消/失败将剩余尾部作为最终有界原文收口，不能丢弃一直没有换行的文本。

测试分两层：解析器在指定前缀上的结果与完整解析基准一致；屏幕允许对尚未完成的结构暂留，要求普通文字有进度、同一源状态下规则确定、最终收口一致且无重复。不能再要求每个屏幕帧等同完整解析，否则将否定表格和链接暂留。

不新增定时动画队列或隐藏 worker。输入控制与事件消费仍沿用现有 owner。单次工作有界，普通小 delta 不重扫所有历史；如果基准测试证明尾块计算仍阻塞输入，先修订设计，不临时加无清理 owner 的 goroutine。

### 6. 样式、安全和明确限制

标题、列表、引用、代码和表格使用同一语法规则。表格每列最小三个显示列，宽度包含消息缩进和边框；放不下或任一记录超过四行时整表改为逐记录字段布局，空/重复表头补列序号。链接显示标签及目标，图片显示 alt/目标，HTML 安全显示原文，数学/Mermaid 保留原文或代码。源内容不产生 OSC、光标移动、剪贴板或外部 I/O；仅 renderer 输出受控 SGR。

控制字符在展示副本进入解析前和实体解码后处理，保留正常字素中的 ZWJ/variation selector；bidi 控制可见化。代码高亮 token 再经同一安全输出路径。无色保持结构和全部可见内容。极窄窗口容不下单个宽字素时用可辨认替代标记，原始字节仍可从查询取得。

完整助手详情使用 r 切换 Markdown/安全原文，保持源锚点；新条目默认 Markdown。分页片段固定安全原文，显示位置和原因；查询偏移始终为原文 UTF-8 字节。大于 64 KiB 的完整 Markdown、无限回看和完整 Claude 虚拟列表明确不在本次，不将这一边界宣传为已实现。

### 7. 资源预算与验证

下表未带目录的 Go 测试和 testdata 均位于 internal/tui。

四页限制包含主线、详情、额外 command 页面及其共享引用，不能复制到另一列表绕过计数。每正文页 64 KiB，artifact 沿用 64 KiB raw/256 KiB escaped；未提交 source 最多 64 KiB，尾预览最多 8 KiB 且引用该 source。新增 Markdown 缓存合计最多 32768 个解析/高亮节点、2 MiB 布局字符串与映射数组（按容量计算），包括交接视图；最多四份文档派生状态，页淘汰即释放对应状态。达到预算停止生成富文本，显示原因并提供有界原文浏览，不截断持久化正文。巨大表格、深嵌套和字素扩展必须测试，不只按原文大小估算内存。

| 验证 | 直接断言 | 计划文件 |
|---|---|---|
| Markdown/高亮 | 常见语言、未知语言、原文相等、纯文本失败路径、Unicode、安全序列、窄表 | `internal/tui/markdown_test.go`、`markdown_highlight_test.go` |
| 流式分块 | 所有合法 UTF-8 切点/固定种子；基准解析等价；尾部暂留与最终 flush 分别断言 | `markdown_stream_test.go` |
| 提交交接 | 延迟查询期间画面保留，连续 commits、新 delta、错代结果、失败、取消；无重复工具 | `transcript_state_test.go` |
| 加载与位置 | A/tool/B、旧回复继续富文本、四页预算、第五候选不循环、回看时新输出不抢位置、r/resize 锚点 | `transcript_browser_test.go`、`transcript_viewport_test.go` |
| 快照 | 80x24、34x12、12x6，有色/无色、代码、表格、未完成结构、handoff、回看、恢复 | `markdown_snapshot_test.go`、固定 `testdata/markdown_*.golden` |
| 双 Provider | mock stream -> durable -> 查询 -> TUI -> resume；canonical request/native 顺序/fingerprint/journal bytes 不变 | `internal/app/markdown_transcript_test.go` |
| 性能 | 50 项、64 KiB 单行/代码/表格、1-32 字符小 delta，100 次 View 零解析、resize 零 source 重解析 | `markdown_bench_test.go` |
| PTY | 输入/审批/取消响应、resize、PgUp/PgDn、Ctrl+O/r、关闭资源与终端恢复 | `scripts/markdown-transcript-smoke.py` |

确定性性能回归用解析次数、被扫描字节数、预算计数和控制消息处理顺序证明普通小 delta 不重复处理整个稳定前缀。PTY 在记录硬件与负载的本机上测输入到回显、stop 到受理延迟，目标 p95 <=100ms、最大 <=250ms（不把 executor 清理耗时计为 UI 受理）；记录原始测量，不以 sleep 或概率竞态证明正确性。若失败，不能宣称流式体验验收通过。所有测试用本地 mock，无真实 key。

#### 高亮专项验收矩阵

下列验证与上表累加，不替代原有交互及双 Provider 验收。计划测试文件属于实现阶段，本次修订不声称它们已存在。

| 层次 | 输入与确定性断言 | 计划位置 |
|---|---|---|
| 词法正确性 | 每种语言和别名的手写固定期望类别/区间 fixture，覆盖 2.2 每行边界；关键字在注释/字符串中不误着色；数字与紧邻运算符不互吞；空输入、无尾换行、CRLF、中文和 emoji | `internal/tui/markdown_highlight_test.go`（表驱动词法样例） |
| 保真与安全 | 拼接 token slice 与扫描输入逐字节相等；token 有序连续且 UTF-8 边界正确；无样式布局与有色布局去受控 SGR 后字符相同；ESC/OSC、实体、bidi、组合字素无法注入控制序列，查询源偏移不变 | `markdown_highlight_test.go`、`markdown_test.go` |
| 故障与预算 | 注入精确剩余步骤、token/内存预算，覆盖限额前/等于/超过；第 65 层嵌套/第 65 个 here-document、256 KiB 展示输入边界、扩容时旧新容量同时记账；断言明确原因、无部分 token、正文保留、无每帧重试 | `markdown_highlight_test.go` |
| 实例与生命周期 | 两实例交错扫描不同语言，多代码块复用无状态泄漏；独立实例并行 race；依赖/源码检查高亮路径没有 go 语句、timer、I/O、全局可变表或第三方高亮依赖，不能用有噪声的进程 goroutine 总数作为唯一证明 | `markdown_highlight_test.go`、`internal/architecture` 中相应约束测试 |
| 模糊测试 | 固定种子覆盖每种语言的字符串、模板、注释、展开及终止符；任意有效 UTF-8 输入必须终止，成功时区间保真，失败时为受控原因且预算未越界，无 panic；无效 UTF-8 在展示输入边界拒绝或净化，不生成破损 token | `markdown_highlight_test.go` |
| 流式复用 | 相同代码源以每个合法 UTF-8 切点及固定种子分块，未闭合时扫描次数为零，闭合/收口后结果一致；100 次 View、resize/r 和重复完成消息不增加词法扫描次数；源变更才失效；淘汰后计数释放 | `markdown_stream_test.go`、`markdown_highlight_test.go` |
| 端到端 | 有色/无色三种固定窗口快照纳入六种语言、预算回退提示；两 Provider 的高亮正文经浏览/恢复后原生请求及 journal 不变；取消收口后的不完整代码仍可见 | `markdown_snapshot_test.go`、`internal/app/markdown_transcript_test.go` |

性能增加纯扫描冷路径和缓存热路径子基准：每种语言 1/8/32/64 KiB、单行及多行、密集单字符 token、转义/未闭合字符串、模板深嵌套、here-document 长公共前缀及最大净化扩展；记录每样本 source/display bytes、扫描步骤、token 数、scratch/派生容量峰值、ns/op、B/op、allocs/op 与回退原因。固定输入按倍数放大时验证步骤不超过上述线性上界，异常输入不得隐藏在“正常纯文本”结果中。典型合法样例在默认预算下必须成功高亮，不能靠普遍触发预算满足性能目标。代码闭合/收口冷路径纳入原有 50 项和 1–32 字符 delta 基准，不能只测缓存命中。

实施时执行 `go test ./internal/tui -run 'Markdown|Highlight|Transcript'`、各高亮 fuzz target 独立 `-fuzztime=30s`、`go test ./internal/tui -run '^$' -bench 'Markdown|Highlight' -benchmem -count=5`（以实际测试名核对），保存原始输出、Go/OS/CPU、负载、样本大小和预算峰值。fuzz 是补充探索，不替代确定性回归。PTY 在代码块闭合/收口触发冷扫描及预算回退时同步测输入/审批/stop，沿用 p95 <=100ms、最大 <=250ms；用事件屏障和受理回执定位测量，不用 sleep 证明顺序。不达标先修订方案，不私加 worker 或放宽既有响应目标。

完成后 `make verify` 覆盖格式、vet、Staticcheck、架构、全量与 race；专项 PTY/性能独立记录。真实 Linux 无 runner 时列未验证，不能以交叉编译替代。

## Risks / Trade-offs

- [边界复用可能误冻结语义] -> 保留 canonical oracle、引用上下文和完整重算；不安全块留在尾部。
- [四页预算不能同时展开任意多回复] -> 保留锚点与可见项优先；超额显式折叠，导航重分配，不查询振荡。
- [原文分页缺少跨页语法上下文] -> 安全原文浏览，不伪造跨页富文本。
- [异步交接与新流重叠] -> watermark 整段替换、单份交接视图、后续活动区分离；无文本身份猜测。
- [自有词法规则漏判或扫描退化] -> 固定语言样例、独立期望 token、区间保真、全路径计费与线性扫描量测试；保留明确基础词法边界，不扩大为语义分析器。
- [局部范围无法完全复刻 Claude] -> 明确无无限虚拟列表/OSC/主题系统，交付按本规格验收。

## Migration Plan

不新增数据版本或迁移。按解析与安全布局、流式状态、共享页面预算、阅读锚点、提交交接、双 Provider 集成、专项验证顺序实施。回退仅撤回 TUI/依赖和说明，不重写 journal。全部 artifacts 审阅后明确 apply 才实现；验收后同步 specs 并归档。实际任务状态见 tasks.md，测量与场景索引见 [验收记录](verification/README.md)。
