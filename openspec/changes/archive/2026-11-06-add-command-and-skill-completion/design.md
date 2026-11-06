## Context

动机见 [proposal.md](proposal.md)。本设计记录用户已确认的 P5 命令与显式 Skill 补全方案；实际交付、验证证据及平台限制见 [验收记录](verification.md) 和 [追加验证](revalidation.md)。

### 已核对的当前接入点

| 现有实现 | 事实与可复用边界 |
| --- | --- |
| `internal/tui/composer.go`、`composer_input.go`、`composer_layout.go` | 字节光标、字素移动、原子有界 replace、已解码 paste、纯内存布局已存在。手动 Tab 当前无操作。补全必须调用同一编辑入口，避免按 rune 拼接破坏字素和历史状态。 |
| `internal/tui/model.go:handleKey` | 已有问卷、Hook、Skills、审批、详情、取消及 admission 等焦点分支；普通聊天最终统一处理 composerSubmit。补全要插在普通聊天按键与队列 Esc/Up 之前，保留更高层 modal 的所有权。 |
| `internal/tui/context.go`、`hooks.go`、`skills.go` | 五项内置命令有真实消费者；`/skills` 的 Enter 只填 composer。现有本地命令校验及控制路径可以共用。 |
| `internal/app/app.go:assembleChatModel` | 普通启动和启动选择器恢复共用装配，调用 WithSkills 注入冻结展示条目；无需新增扫描服务或异步目录查询。 |
| `internal/domain/skills.go`、`internal/extension/skill/local.go` | 展示列表包含失效/遮蔽/手动项；模型 SkillCatalog 会过滤 disable-model-invocation，不能拿它当手动补全来源。正式 Prepare 识别首词 `/name` / `$name`，重新检查来源 digest 并生成附件。 |
| `internal/tui/transcript_viewport.go` | 主线高度已扣除 composer 与队列。需要再扣除候选的实际行数，保持阅读锚点与光标可见。 |
| `internal/tui/composer_terminal_test.go`、`internal/app/composer_e2e_test.go` | 已有 Bubble Tea 真实解码测试和双 Provider 原生正文/恢复对照，可扩展为补全的终端与请求回归。 |

已有 `add-pre-tool-use-command-hooks` 为 43/43 完成状态，本次不更改其 artifacts 或代为归档。当前 Roadmap 与 capability-status 都将输入补全列为未实现。

### 两个参考工程的证据与取舍

所有参考只读，路径根分别是 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap` 与 `/Users/cutejiuge/IdeaProjects/codex`。本地 worktree 的 `../` 没有这两个目录，实际通过 Git 主 checkout 的相邻目录找到。

| 固定快照及相对文件 | 已核对行为 | 本次吸收与差异 |
| --- | --- | --- |
| Claude Code `2.1.88`，`package/package.json` | 包版本来自快照自身；源码由 sourcemap 还原 | 只将其作为当前本地参考，不将还原目录组织或实验功能当作应复制的架构。 |
| Claude `restored-src/src/utils/suggestions/commandSuggestions.ts`：generateCommandSuggestions、applyCommandSuggestion | 名称/说明/参数/来源候选；精确与前缀优先；可按历史使用率和 Fuse 排序；Tab 与 Enter 的 shouldExecute 区分 | 采用候选信息结构、精确优先及 Tab 只填入；以确定性简单匹配代替全局 Fuse cache 和使用率排序，无新依赖或历史文件。 |
| Claude `restored-src/src/hooks/useTypeahead.tsx`：handleTab、handleEnter、handleAutocompleteDismiss | Tab 调用 shouldExecute=false；Enter 调用 true；记住关闭时的输入，避免立即重开；注册 autocomplete overlay 使 Esc 优先关闭；modal 活跃时停用补全按键 | 保留关闭抑制和 modal 优先级；内部使用实例值与编辑目标身份，不复制 React hooks 或全局可变状态。 |
| Claude `restored-src/src/components/PromptInput/PromptInputFooterSuggestions.tsx` | OVERLAY_MAX_ITEMS=5；命令名与说明按列宽显示，元数据承担展示 | 采用最多五项、相邻输入区列表及来源提示；转义和列宽由 EasyCode 现有规则统一实现。 |
| Codex `6f8bf683cee83ed4d580a2e106b587d8451db936`，`codex-rs/tui/src/bottom_pane/chat_composer/slash_input.rs` | 独立 SlashInput；名称编辑范围；Tab 完成保留既有尾部；Esc 保留草稿；Enter 将选择转换成既有命令结果；部分命令 Tab 可直接分派 | 吸收范围替换与统一分派；交互冲突处按 Claude，EasyCode 的 Tab 一律不执行。 |
| 同一 Codex，`chat_composer/completion_target.rs`、`popup_state.rs` | UTF-8 范围与目标身份、单个活动 popup、关闭 token 身份；通用 token 解析还能处理正文 mentions 和原子元素 | 使用首词的有界范围和实例状态；EasyCode 当前只有首词 Skill 激活，因此不加入正文 mentions、原子附件或跨 sigil 通用框架。 |
| 同一 Codex，`bottom_pane/skill_popup.rs` | MentionItem 分离 display_name/insert_text/description/category，精确/前缀/fuzzy 分级，选择滚动 | 采用展示与插入分离、分级匹配和滚动；Skill 来源仍由现有 Local 唯一解析，不引入路径绑定或另一套扩展 registry。 |

### 为什么当前优先这块

| 候选 | 当前前置条件与价值 | 本次结论 |
| --- | --- | --- |
| 命令/Skill 补全 | composer、冻结来源、调用语义、双 Provider、持久化及恢复已存在；每次输入都能受益 | 选取完整纵向切片，主要新增用户可见交互和可测纯逻辑。 |
| reasoning 折叠 UI | 原生保真已有基础，还需独立确定哪些 thinking/summary 可展示、流式与历史如何对应 | 价值明确，边界不同，适合下一项独立设计。 |
| 后台命令、MCP 或 Subagent | 需要新的 process/server/thread 生命周期、安全、恢复与协议消费者 | Roadmap 保留；本次先改善已交付能力的日常入口。 |
| 更多 Hook 事件 | 可复用已有前置检查，但仍涉及事件专用输入/结果、失败和恢复语义 | 与命令发现问题分离，避免扩大当前切片。 |

## Goals / Non-Goals

**Goals:**

- 将补全实现为 TUI 实例拥有的编辑状态，不形成 Runtime/Provider 的第二个事实源。
- 复用冻结列表、编辑入口、本地命令校验和 Skill 准备链，保证选择与执行的职责清晰。
- 使焦点、参数保留、关闭抑制和预算在写代码前可直接转化为回归场景。
- 以手输、补全和恢复三条路径的原生请求等价作为集成验收。

**Non-Goals:**

- 不增加跨包公共接口、通用命令总线、任意回调 registry、索引数据库或异步 worker。
- 不改变 Skill 首词激活/来源优先级，不从模型目录重建手动目录，不从 UI 状态生成 SkillAttachment。
- 不扩展文件搜索、参数值补全、正文 mentions、动态目录或执行能力；产品范围见 proposal。

## Decisions

### 1. 状态与纯算法分开，全部由 Model 实例持有

拟新增 `internal/tui/completion.go`，保存私有 completionKind、completionCandidate、completionTarget、completionState，以及目录构建、目标解析、匹配与编辑计划的纯函数；`completion_view.go` 负责有界安全布局。必要时按职责拆分，但不新建通用包或单实现策略接口。

candidate 使用封闭 kind（内置命令或 Skill）、规范名称、说明、参数提示和 domain.SkillSource；identity 由 kind/name/source 组成，不以当前显示行或数组下标作为身份。内置命令描述由返回新值的函数或 switch 构造，不使用包级 map/slice/registry。

Model 持有不可变候选快照、当前匹配索引、选择身份、滚动位置、目标范围和隐藏原因。编辑后的匹配只发布新值；不要在复制的 Model 中原地修改共享 backing array。与已有 composer 的不可变布局/历史值复制约定一致。

设计模式只采用现有 Model/Update/View 状态机与值组合；不新增抽象工厂、Manager 或模板继承。New/With 方法保持纯内存。无新增 goroutine、owner、channel 或清理协议。

替代方案：每次 View 重算/扫描会混淆布局与状态；把补全放进 Runtime 会扩大协议并使 headless 承担终端语义；通用 registry 会过早承载尚未实现命令。因此仅在 TUI Update 准备状态，View 读取它。

### 2. 展示目录来自 WithSkills，执行仍来自 Prepare

WithSkills 已复制冻结列表。在其同一内存配置路径生成 completion candidates，先验证 available、UserInvocable 与 metadata/source 一致，再按名称/来源稳定排序。Model-only 和无效/遮蔽项过滤；manual-only 保留。可用 Skill 最多 domain.MaxSkills=256，内置命令五项，独立身份最多 261；`/` 与 `$` 是同一 Skill 候选的两种插入拼写，不复制两份正文或注册两个执行器。

`/` 排除与当前五项内置命令同名的 Skill；`$` 保留它。candidate 来源只用于身份与显示，真正提交时仍由 `skillInputPreparer -> skill.Local.Prepare` 定位冻结获胜来源、检查 digest、读取全文和准备附件。无权在补全时读取正文、检查磁盘、执行脚本或缓存展开结果。

替代方案：使用 SkillCatalog 会漏掉 manual-only；重新扫描会使显示和提交使用不同目录；直接附加正文会绕过现有预算/取消。这三种均不采用。

### 3. 名称范围、查询和替换分别建模

目标解析只检查首个非空白 token；保留前导空白，sigil 为 `/` 或 `$`，光标必须在 sigil 后到名称末尾之间。名称为至多 64 字节的 ASCII 字母/数字/连字符，裸 sigil 允许空查询。额外 `/`、`.`、代码反引号或其他标点不参与补全；因此 `/tmp/file` 不会成为命令候选。参数区域与后续 token 不激活。

target 保存整个 token 的 start/end、光标、sigil 和光标前查询。查询是光标左侧片段，替换使用完整名称范围，避免 `/re|view` 补全成重复后缀。名称区合法性按完整 token 校验；所有偏移以 UTF-8 byte 表示，并验证边界。文本开头的空白沿用现有 Unicode 空白定义。

接受先计算字节增量和替换计划，再调用既有 composer.replace，不额外预建一份完整大草稿；失败不修改正文、光标、历史或选择。只有没有既有分隔符时才补一个空格；已有空格、Tab、LF 和后续正文逐字节保留。光标放在完成名称后的第一个分隔位置，交由既有字素归位逻辑处理。参数提示仅渲染，绝不拼进正文。

不持有整个草稿的历史版本来识别关闭目标：保存短 token 的克隆、范围和编辑身份，避免一个短 substring 保留旧 4 MiB backing storage。

### 4. 匹配可解释，选择身份不随序号漂移

将最多 64 字节 ASCII 查询转小写；候选名称预先保存小写值。比较层级固定：exact=0、prefix=1、名称子序列=2、说明大小写无关子串=3；未命中排除。子序列只判断存在性，不引入不透明打分。描述的字符串比较不影响原文或 Unicode 规范化。

排序键为 `(matchTier, kindRank, canonicalName, sourceID)`，内置 kindRank 在 Skill 之前；空查询不打分，按其余键排序。同一 identity 不重复。同一首词起点及 sigil 内查询改变后，若当前 identity 仍命中则保留，否则选首项并归位滚动；首词起点、sigil 或目录身份改变时清除旧选择。

不引入 Fuse/远端索引、MRU、磁盘使用率或时间排序。代价是没有参考工程全部搜索启发式，但 261 项目录足以用简单、可验证算法实现稳定体验。

### 5. 按键只产生编辑或既有提交意图

| 条件/按键 | 行为 |
| --- | --- |
| 更高层 modal/面板或 pending/retracting/cancelling | 保留现有处理者；补全隐藏且不接收按键。 |
| 显式 Paste | 优先走既有整体插入分支；不按事件 Type 解释内部操作键，关闭旧候选，并抑制当前粘贴目标自动弹出。 |
| Alt+Enter / Ctrl+J / 既有反斜杠换行 | 走 composer 原换行逻辑，随后按新位置刷新目标。 |
| 可见列表 ↑/↓ | 循环改变选择并保证选择可见，不动 composer 光标/历史/队列。 |
| Tab，有可见列表 | 原子替换并关闭，仅编辑。 |
| Tab，无列表但当前目标被 paste/Esc/modal 返回抑制 | 若当前目标合法且有可显示匹配，显式重新打开列表；本次只打开，下一次 Tab 才接受。 |
| Tab，其他无候选状态 | 无操作。 |
| 普通 Enter，内置命令候选 | 完成后产生同一个 composerSubmit 意图，复用原本的命令校验/提交分支。 |
| 普通 Enter，Skill 候选 | 完成并关闭，仅编辑；后续独立 Enter 才准备和提交。 |
| Esc，可见候选 | 只关闭，记录当前目标抑制；不取回/停止，不向下继续分派。 |
| Esc，无候选 | 原有队列取回、compaction 取消、turn 停止和空闲规则。 |
| Ctrl+C | 原有活动任务停止或空闲规则，清除可见候选后交给原处理者，不能只关闭列表。 |
| 其他编辑/光标操作 | 先走 composer，再刷新目标；不截获普通字母、Home/End 等。 |

将 model.go 中 composerSubmit 后的现有逻辑提取为私有共享方法，供手输与补全的同一意图调用。不要补全分支直接调用 contextSession、Append 或 Skill executor。内置命令候选包含完整保留参数时，仍由已有 `/cache` 等校验拒绝非法参数，不擅自丢弃。

Enter 的 Skill 行为刻意沿用 EasyCode 已交付 `/skills` 的“选择只填入”。Claude 对不带 argNames 的 prompt 可以直接提交，但 EasyCode 只有自由文本 argument-hint，不能据此推断“无需参数”或“必填参数”。本次统一先填入，不增加 frontmatter 字段或推断执行。

### 6. 关闭、粘贴与 modal 转换采用明确状态

completionState 的本地状态为 inactive、visible、suppressed；suppressed 携带 Escape、Paste、Modal、Accepted 等封闭原因与短 token 身份。原因只在内存，用于决定重新开放条件，不能写入 RuntimeEvent 或 Session。

```text
inactive -- eligible name edit --> visible
visible  -- filter edit -------> visible / inactive
visible  -- Esc / Paste -------> suppressed
visible  -- accept ------------> suppressed
visible  -- modal opens -------> suppressed
suppressed -- name edit ------> visible / inactive
suppressed -- explicit Tab ---> visible / inactive
any -- task cancellation ------> inactive
```

每次 Update 的派生状态准备验证当前焦点、目标及尺寸，但流式文字、异步回复、resize 和纯 View 不清除抑制。modal 关闭后保留原 composer，候选保持隐藏直到名称编辑或显式 Tab，避免用户刚按完审批 Enter 又误触旧选择。已接受 token 的分隔符将光标移出名称编辑区；光标重新进入并编辑名称才可重新匹配。

普通 Ctrl+C、PgUp/PgDn、Ctrl+O 仍由现有任务/历史导航处理；进入浏览后候选不能留在屏幕上截获按键。显式粘贴后的独立 Enter 必须提交实际粘贴文本，不接受近似候选。此次不处理未标记输入的 paste 猜测，沿用当前终端保证范围。

### 7. 布局与计算预算可回归

布局放在 composer 上方，最多五个候选行和一个提示行。候选行显示选择标记、插入名称、Skill 参数提示、类型/相对来源和说明；按当前终端列宽安全裁剪，不依靠颜色表达选择。窄屏先保留名称和选择，逐步省略说明/提示；候选滚动提供当前位置。

Update 只在输入/光标/候选身份/尺寸/焦点变化时重建匹配或布局；assistant delta 不重排不改变抑制。外层 Update 应先准备当前补全占用，再准备 Markdown 主线/详情及 hydration，使 transcriptHeight 和本次查询使用同一可用高度。transcriptHeight 扣除候选实际行数。composer 仍优先保证光标，候选空间可收缩至零；列宽不足 13 列、无法同时显示选择标记、名称前缀和类型时也收缩至零。零可见行时停用候选按键。resize 只能改变布局与视口，不能改变正文或选择身份。

预算：最多 261 个身份和匹配索引、一个当前结果集、六行展示；completion 新增长期保留内存上限为 2 MiB（不重复计已有冻结元数据与 composer 的已有内存）。匹配不扫描完整参数或全文历史；首词定位最多对当前 4 MiB 草稿作一次有界扫描，流式增量复用目标。每行安全展示另设有界字节容量，最多 16 KiB/行，六行合计最多 96 KiB；超过显示预算要有省略提示，不能以扩大终端列宽导致无限分配。预算不足时整组停用补全，普通编辑/手动提交仍可用，并显示安全英文提示；不能悄悄删掉部分可调用目录。

基准记录 0/256 Skill、64 字节查询、长参数、4 MiB 草稿与大量连续 assistant delta；报告时间/分配，不以特定机器纳秒阈值作为跨平台门。确定性测试断言条目/容量和不保留历史结果，而不是依赖 GC 运行时机。

### 8. 执行、缓存和恢复边界完全走现有链路

补全本身无 I/O。正式提交的原始文本仍由现有 Append/PrepareInput/admission 链准备一次，当前 source digest 校验、全文预算、附件冻结、Sync/finalize 和失败返还均保留。Skill 文件改变应在正式准备时返回 skill_source_changed，不能为了让补全成功而回退另一来源或读取通道。

UI 的 query、选择、隐藏状态、搜索顺序、列宽、NO_COLOR 和说明裁剪不进入请求、工具目录、稳定 prompt、附件 metadata 或任何持久化事实。本次不新增 schema、格式版本、兼容 reader 或迁移。Prompt fingerprint 不因为“看过或选过候选”改变。

同一最终输入的直接键入和补全必须在两家 Provider 分别产生相同 canonical 请求。集成对照固定同一 thread/turn/call 身份与来源，或在同一冻结输入上取请求快照，避免把两个不同随机 thread 的 OpenAI cache key 当作 UI 差异；不得通过删除真实请求字段掩盖不等价。已有原生恢复 fixture 继续验证 item 顺序和完整原生 reasoning 保真。

### 9. 文件职责及实施顺序

| 文件/区域 | 预期改动 |
| --- | --- |
| `internal/tui/completion.go` | 私有值、目录构建、目标、匹配、编辑计划和状态转换；按需再拆同包文件。 |
| `internal/tui/completion_view.go` | 纯内存安全行布局与可见预算。 |
| `internal/tui/model.go` | 实例字段、Update 派生准备、按键焦点接入、共用提交方法和 View 拼接。 |
| `internal/tui/skills.go` | 冻结来源候选装配；共享可显式调用谓词及填入逻辑，维持既有 picker 契约。 |
| `internal/tui/composer.go`、`composer_input.go` | 复用编辑入口与输入意图，仅在必要处提取小辅助函数；不复制布局算法。 |
| `internal/tui/transcript_viewport.go` | 扣除候选实际行数，保持历史阅读锚点。 |
| `internal/tui/*completion*_test.go`、`testdata/` | 下表的纯逻辑、状态、snapshot、终端与预算测试。 |
| `internal/app/*completion*_test.go` | 正常/恢复装配、零补全 I/O、双 Provider 原生请求和缓存对照。 |
| `README.md`、`docs/roadmap/{capability-status,product-roadmap,pitfall-log}.md`、架构说明 | 实现并验收后更新真实入口、按键、边界、验证路径和新增踩坑；不在提案时写成已交付。 |

### 10. 验证矩阵

| 层级 | 核心场景与断言 |
| --- | --- |
| 纯逻辑 | 五个内置命令；手动/模型权限、来源遮蔽和同名冲突；随机枚举顺序；精确/前缀/子序列/说明排名；保留选择；目标、参数、多行、UTF-8、长度边界；2 MiB 保留容量。 |
| 编辑回归 | 中间名称替换；有/无分隔空白；leading/trailing whitespace；4 MiB 恰好/超限原子拒绝；历史浏览接受后状态；不改参数或 hint 注入。 |
| 焦点回归 | visible 的上下/Tab/Enter/Esc；连续 Esc；Ctrl+C；queued/claimed/pending/returned；问卷、Hook、Patch/Write/Exec 审批、各面板和 transcript；过期提交回复不覆盖新草稿。 |
| 粘贴/终端 | Bubble Tea 解码真实 Tab、Enter、独立 Esc、Alt+Enter、Ctrl+J；完整及分段 bracketed paste 含 Tab/Esc/Ctrl+C；paste 后独立 Enter 按真实文本提交；关闭后流式事件不重开。 |
| 固定 snapshot | 80x24、28x10、20x5、1x1；空目录、多于五项滚动、长说明与 hint、中文/emoji 参数、终端及 bidi 注入、NO_COLOR；选中项/光标可见与主线高度。 |
| app/Provider | 手输 vs 补全相同 Skill/参数；Tab/Skill Enter/导航时来源读取、Provider 请求与 journal 增量均为零；正式提交恰好一次附件准备与输入；source_changed/取消/超限失败保持草稿。 |
| 缓存/恢复 | 两 Provider 请求 canonical bytes、native item 顺序与 source/segment fingerprint 等价；resume 后候选从启动快照装配，原生历史不重写；headless/stream-json 既有行为保持。 |
| 性能 | 有界目录与显示容量确定性断言；长参数和大草稿不重复复制；assistant delta 不重做候选匹配/布局；基准记录而非概率或 sleep 证明。 |
| 工程门 | OpenSpec strict 校验；`make verify` 包括分支策略、gofmt、vet、Staticcheck、全量测试、架构守卫及 race；真实 Darwin PTY smoke 并记录尺寸、终端恢复和命令。 |

测试数据使用固定假凭据与本地 httptest，不读取真实用户配置、不调用远端模型；并发边界以 channels/检查点注入同步，不用 sleep。若后续变更触及公共契约或持久化边界，先回到 artifacts 评审，不能静默扩张。

## Risks / Trade-offs

- [Tab/上下/Esc 与现有契约冲突] → `tui/basic-chat` delta 完整保留既有 requirement 与 scenarios，只增加补全激活前提，并覆盖无候选回退。
- [manual-only 被模型目录过滤] → 明确从 WithSkills 展示列表构建，不从 SkillCatalog 构建。
- [关闭或 modal 返回后重开导致误选] → 保存目标抑制原因，所有 assistant/control 事件只刷新派生状态，不自动清除抑制。
- [换名覆盖参数或粘贴改写正文] → 只替换首 token，并在 paste 事件上原子插入后抑制自动候选。
- [名称相同但来源不同] → 接受获胜来源的稳定 identity，同名 slash 内置优先，仍由正式 Prepare 再校验来源。
- [小屏候选不可见却吞 Enter/Tab] → 可见行数为零即停用候选按键，snapshot 覆盖极小尺寸。
- [Model 值复制共享可变切片] → 发布后不可变，编辑结果分配当前集合，不对旧切片原地排序或覆盖。
- [复制参考代码带入全局 cache/异步复杂度] → 只吸收行为，自行实现同包纯值和有界算法，不增加进程生命周期。

## Migration Plan

本次只改变当前 TUI 交互，不改变存储或线协议，无迁移器、历史 reader、格式版本或兼容窗口。提案经审阅且后续明确 apply 后，按 tasks 实现、执行对应回归与完整质量门，再记录验收。验收完成后同步 delta specs、能力清单及必要架构/踩坑说明，最后另行归档。回退时撤销该切片的 TUI 代码与相应交互规格，既有 Session 原生事实不需改写。
