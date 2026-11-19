## Context

参见 [proposal](proposal.md)。当前交互启动在第一轮前通过 `app/session_service.go` 的 `create` 和 `session.CreateRootJournal` 提交初始 metadata，恢复时则在同一 lease 下加载、回放、验证原生记录、处理可修复尾部并转移写入所有权。`AgentLoop` 持有会话控制状态，TUI 在 `submitComposer` 中分流本地命令。Catalog 由前台协调逐个取得 lease，使用 `ReplayPlan` 投影；启动 picker 在最多 200 条已加载记录里匹配元数据。

### 路线图位置

P2 已交付 root journal、恢复与可重建 Catalog，P5 已交付启动选择器、命令补全和多行 composer。`docs/roadmap/product-roadmap.md` 的 P2 当前进度明确列出标题缺口。`/rename` 将这两条已有路径连成从聊天命名到重启后查找的完整用法。刚交付的文件引用和任务清单已能承载多步骤工作，手动标题让用户能按任务在多个历史会话之间切换。动态模型切换涉及模型兼容性和请求设置，fork 涉及分支身份和原生历史，MCP/Subagent 涉及进程或线程生命周期，各自需要独立的前置契约。

### 参考工程

Claude Code 的参考版本为 `claude-code-sourcemap` 2.1.88，revision `a8a678cb6244e6770e1e421767ff0987a1d95549`。路径相对于 `restored-src/src/`。

| 观察位置 | 可复用的行为 |
| --- | --- |
| `commands/rename/index.ts:3`、`commands/rename/rename.ts:35`、`:85` | `/rename <name>` 是本地命令，规范化显式标题，保存以后给简短系统提示。裸命令通过模型生成建议。 |
| `utils/sessionStorage.ts:2617`、`:3658` | `custom-title` 作为独立日志事实追加，读取时以后写入的事实为当前值。 |
| `utils/slashCommandParsing.ts:25`、`utils/sessionStorage.ts:2572`、`:5037` | 通用 slash 解析先 trim。标题追加使用 `appendFileSync`，该调用处没有文件 `Sync`。发现流程可以使用无首条 prompt 的已命名会话。 |
| `utils/log.ts:25`、`components/LogSelector.tsx:421`、`:1537` | 历史选择器根据显示标题搜索，标题优先参与辨认。 |
| `screens/REPL.tsx:3158`、`:3207` | `immediate` 本地命令可在忙时绕过聊天输入队列。 |

Codex 的参考 revision 为 `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`。路径相对于 `codex-rs/`。

| 观察位置 | 可复用的行为 |
| --- | --- |
| `tui/src/chatwidget/slash_dispatch.rs:917`、`tui/src/chatwidget/interaction.rs:530` | 同时提供行内命名和独立单字段编辑框，编辑框使用当前标题预填。 |
| `core/src/util.rs:90`、`tui/src/bottom_pane/custom_prompt_view.rs:148`、`tui/src/chatwidget/interaction.rs:587` | 手动标题 trim 后必须非空。独立编辑框消费 Esc 和提交键，两条命名入口使用同一可用性检查。 |
| `tui/src/app/thread_routing.rs:875`、`app-server/src/request_processors/thread_processor.rs:1974`、`tui/src/chatwidget/session_flow.rs:415` | 手动提交走独立的线程元数据请求，成功应答后才更新聊天界面，并绑定正确的线程。 |
| `tui/src/resume_picker.rs:1292`、`:3843` | 已命名的条目优先显示名称，大小写不敏感搜索保留其他字段，展开详情显示真实线程 ID。 |
| `tui/src/resume_picker.rs:2103`、`:2552` | TUI 列表请求不传服务端搜索词，在本地对已加载条目逐字段匹配。 |
| `thread-store/src/local/update_thread_metadata.rs:99`、`:160`、`rollout/src/session_index.rs:54` | 支持在首轮前物化会话并保存名称，存储与索引各有结果边界。兼容名称索引的写入只调用 Flush。 |

本切片组合 Claude Code 的行内命令、简短确认和独立标题事实，以及 Codex 的预填编辑、带身份的命令应答和名称优先的选择器。裸命令直接打开本地编辑框。持久化采用 EasyCode 的 `Sync` 标准，Catalog 通过既有协调刷新。

## Goals / Non-Goals

一个会话从空白启动、命名、退出，到离线列出、按名找到、预览并继续对话，全程以同一 canonical thread ID 关联。改名只增加一条必要的宿主事实，显示与发现均使用它的投影。

本切片的控制面限定当前互动会话、空闲提交和手动非空标题。活跃会话改名、模型建议、离线写入、实时索引以及按名称作恢复身份属于后续范围。

## Decisions

### 1. 规范标题与本地编辑

在 `domain` 定义一个轻量标题值，复用现有强类型字符串模式。解析用户输入时先验证完整 UTF-8 和不允许的码点，再裁去两端 Unicode `Zs` 空格并检查非空及 256 UTF-8 bytes。严格存储校验要求输入已经规范化。C0、DEL、C1、行/段分隔符和 Unicode `Bidi_Control` 不接受，合法中文、组合字符、ZWJ emoji、内部空格、`@` 和引号保留原字节。使用同一值校验内部命令、journal payload 和 Catalog 读取；未命名用投影的零值表达，已提交记录的值必须有效。错误内容固定为安全英文。

在 `internal/tui/model.go` 的本地提交入口分流 `/rename`，使用 `internal/domain/skill_slash.go` 的共享保留名，补全候选量增加一项。首词匹配只决定路由，从词尾切出的原始后缀先检查禁用码点再裁 `Zs`，只有空或全 `Zs` 后缀打开编辑框。`/rename\t` 和 `/rename Title\n` 因此是本地输入错误。仿照 `/effort` 和 `/tasks`，在精确命令的普通 Enter 进入 `composer.input` 前分流，避免末尾反斜杠触发普通聊天的换行规则。候选 Tab 填入词，候选 Enter 走同一命令入口。属于同名 Skill 的 `$rename` 保留原附件规则。

裸命令在 TUI 已知有活动操作或待处理输入时提示忙，否则打开独立输入焦点。TUI 的判断只用于交互，保存仍由会话 owner 在真实状态下接纳。编辑框复用 composer 的字素编辑、光标与宽度布局，普通 Enter 直接提交，Alt+Enter 和 Ctrl+J 不走多行编辑。解码后的 paste 先于按键分流，编辑缓冲限 1 KiB，含禁用字符或超限的编辑原子拒绝，保存仍校验规范文本的 256 bytes。Bubble Tea 1.3.10 的 `key_sequences.go` 在产生 `KeyMsg.Runes` 前滤掉 `RuneError`，标题层只能验证实际收到的文本，领域构造和 journal codec 继续验证原始字符串或字节。打开时保留原聊天草稿、光标和浏览状态，从补全 Enter 打开时保留填入前的草稿。提交前 Esc 或 Ctrl+C 取消并恢复，旧候选保持关闭。若后来到达审批、提问或 Hook 信任请求，保留编辑草稿并让出焦点。参数无效或可修正的拒绝时保留输入，成功后消费命令草稿并关闭编辑框。

`tea.Cmd` 等待命令交付，界面继续消费窗口和控制消息。保存中保留一个待确认请求及对应草稿，暂停编辑和重复提交。匹配的成功 `ControlItem` 更新当前标题及短状态，未交付错误单独按该请求关联。Esc 取消未提交的编辑。保存时 Ctrl+C 记录退出意图并取消该次请求的接纳等待，继续消费匹配结果或控制流关闭后再走现有退出路径。`session_busy` 等确定拒绝允许修正后手动再提交。writer 失败关闭或控制流失联时显示安全错误，停止接收输入和标题提交，非空草稿也不得阻挡退出。终端或信号提前关闭界面时，宿主继续排空 Runtime 并等待 writer，界面未收到的回执不代表提交失败。标题和保存状态纳入 `chat_space.go` 的现有行预算，标题按终端列宽和完整字素裁剪。

### 2. 单一接纳与既有持久写入

沿用 `ChatSession` 和 `AgentLoop` 的小型命令接口，新增内部 typed rename command 与专属校验。命令携带 request ID 和标题，作用域来自目标会话实例。复用 `updated` disposition、既有 command result 与 `session_busy` / `session_failed`；通知正文来自 TUI 已关联的规范待确认值。headless v1 继续使用现有输入/输出映射，普通文本不经过 TUI 本地路由。

| 接纳状态 | 行为 |
| --- | --- |
| 活动 turn、压缩、排队、清理尚未完成 | `session_busy`，当前操作继续。 |
| 会话关闭或 poisoned | 走原有退出路径，拒绝新修改。 |
| 空闲且标题等于当前值 | 返回成功，跳过 append。 |
| 空闲且标题不同 | 追加标题事实并等待 writer 的确定结果。 |

空闲判定借用手动 compaction 的条件，由 owner 检查真实状态，终态显示与 worker 完成之间仍属于忙。owner 在接纳时检查请求取消、会话运行 context 和 `ChatSession` 已有的 `stop`。`Shutdown` 先关闭 `stop` 再取消运行 context，只检查后者会漏掉已通过宿主活性检查的请求。使用单条请求携带这次需要的信号。接纳决定是线性化点，此后由 owner 等待 writer 返回确定结果，调用方取消或 shutdown 不能截断已接纳的提交。

在该空闲 owner 分支同步调用现有 `Journal.AppendBatch`。I/O 由既有 journal writer 执行，owner 不持锁，提交期间没有其他网络 worker 或待处理工具审批。追加成功 Sync 后，owner 安装标题、再走有序成功应答。若 Write 成功但 Sync 失败，按既有 poisoned 路径返回安全失败并终止会话写入。重开以重新验证出的完整前缀为准，已完整留存的记录仍可能成为当前标题。没有自动重试。

`chatResources.close` 等 `ChatSession` 排空后关闭原 writer。当前 app 对 `writer.Close` 只有一次带截止时间的等待，而 writer 在追加成功后关闭时仍会执行最终 Sync 和文件 Close。如果首次等待因取消或截止而返回，宿主保留该结果，继续等待同一已进入关闭状态的 writer（复用幂等 `Close`），完成后再关闭其依赖并发布宿主完成信号。已有 provider 强制关闭路径用于解除网络阻塞，写入和最终 Sync 继续由同一个 writer 安全收口。

忙时立即改名会在当前状态机的活动记录间插入元数据。`internal/session/replay.go:381` 拒绝压缩与其他记录交错，`:478` 要求活动 turn 的最后一个语义序号是 journal 尾。空闲接纳继续满足这两个恢复不变量，并覆盖首轮前、回合之间和恢复完成后的正常命名。

### 3. 一条追加事实和独立恢复投影

以 `session_title_updated` 进入 `internal/session` 封闭 registry，使用单条 root batch，payload 为 `{"title":"复核解析器"}`。typed draft 和校验器复用既有 seq、timestamp、batch、checksum 和写入上限。`DecodeRecord` 读取完整行时就验证此 kind 的 payload 和单条 batch 约束，并通过已扫描的 envelope 成员检查 `turn_id`、`parent_thread_id` 缺省。解码后的 Go 零值无法区分缺省和显式 `""`，所以字段存在性必须在该处检查。ReplayPlanner 负责会话时序，只在初始 metadata 后的空闲边界接受该事实，并以最后一条有效记录填充 `ReplayPlan.Title`。初始 batch、完整性修复流程和各活动操作的状态机沿用现有实现。标题事实独立于 NativeCommit 和 HistoryProjector。

新会话的已确认标题为空。在线装配从 `sessionService.resume` 的已校验 ReplayPlan 取得标题，经 app 装配传入控制器和 TUI。启动交互和开放命名要晚于 service 内的尾部收口和 app 层 `ReconcileStopTurn` / `ReconcileToolTurn`，使用同一 journal writer。标题可从 metadata-only 的 root 恢复，且预览仍能报告空正文。严格回放先于 repair 和补偿，损坏完整标题事实阻止继续，合法半行使用既有前缀规则。

同一有效会话的命名追加会推进 journal 物理 seq。Provider 历史、输入和 sample 的语义顺序及稳定缓存源继续来自原有事实。复用 `internal/app/cache_e2e_test.go` 的同一 thread identity 与隔离存储根做对照，冻结配置、工作区来源和输入，比较两家 HTTP/SSE 的完整 canonical bytes、原生 item 顺序与 CachePlan fingerprint。在带工具或 checkpoint 的既有 fixture 中验证物理 seq 增量。OpenAI WebSocket 若有同连接可复用状态仍按原条件增量，恢复后新连接仍发完整请求。协议的 full/incremental 差异不属于命名变化。

### 4. 可重建索引、时间和选择身份

`catalog.Projector.Project` 从已校验的 `ReplayPlan` 获取标题，继续从最后 committed record 取 `UpdatedAt`、`LastSequence`、`LastChecksum`。当前 `threads.title` 为 `TEXT NOT NULL`，空字符串表示未命名。同步调整结构校验、建表、绑定读取与 upsert，缺列或同名错类型/可空列沿现有路径重建。`Catalog.Reconcile` 在逐 journal lease 下取得已验证投影，释放该 lease 后再用短事务发布。持同一个 Catalog 进程锁的协调串行，写标题只走 journal。另一进程可在投影验证后、SQLite 提交前更新 journal，该条目仍如实表示已验证的观察时点，即使报告无 busy。下次在变化提交后取得 lease 的协调即可取得新标题。已有 busy 行保留最后验证值，新会话 busy 且未索引时仍可能暂缺。

一次成功的标题变化是已提交活动，可以移动最近列表顺序，也可以改变 `--continue` 选中的会话。`--continue` 仍精确使用工作目录、Provider family、wire 和 model。保留这一规则可以让全部入口继续使用现有排序事实。Codex 的名称更新与业务 recency 通常分离；沿用那套做法需要为 EasyCode 引入独立活动时间投影并修订当前排序契约。相同规范标题不追加记录，因此不推动更新时间。

`sessions list` 为已命名条目增加安全标题行。picker 映射标题到 `SessionEntry`，对标题独立做 Unicode 小写子串匹配，再与原字段匹配结果取并集，避免标题和旁边字段拼出命中。搜索继续受 200 条查询窗口、作用域和确认规则限制。同名条目保持独立选择，刷新按 ID 保留匹配项。标题直接取自已经验证的值，以纯文本渲染，避免 picker 的通用 `Cf` 转义改写合法 ZWJ。列表在列宽内优先展示标题，详情可看完整标题和真实 ID。预览使用同次严格验证出的标题及正文，以独立标题字段和原有请求身份发布。标题随着预览滚动，正文预算独立。列表快照由协调更新，预览和正式恢复分别按目标 ID 重新取得 lease 并验证。

## Risks / Trade-offs

| 风险或取舍 | 验收证据 |
| --- | --- |
| 同时提交普通输入、取消或退出时出现提前确认、重复标题或遗弃 lease | 在 handoff、追加 Sync 和最终 Sync 分别设测试门，证明取消边界、poisoned 收口及最后 lease 释放。 |
| 标题 batch 穿插在活动记录中，或错误被可修复尾部掩盖 | 固定 current bytes 和具备有效外层 checksum 的非法位置样本。验证在线恢复、离线预览和 Catalog 拒绝，非法 journal 原 bytes 保留。 |
| 标题泄漏到模型、输入历史或可执行终端内容 | 双 Provider request/cache 对照、headless 路由回归、Unicode 和终端控制边界、窄屏无色验收。标题只来自显式交互，错误不附标题、底层 I/O 或配置。 |
| 目录同时被占用，旧名称仍能从索引查到 | 真实独立进程持 lease 验证 stale 行和提示，释放后刷新并从删除的 SQLite 重建。验证同名和刷新后按真实 ID 选择。 |

## Deployment

当前程序添加独立记录 kind；已有无标题的合法 journal 用同一个解码器读取，原文件不作格式替换。缺少标题列的 SQLite 在下次协调按当前结构重建。旧程序会依现行未知 kind 规则拒绝包含新事实的 journal，发布回退不能依赖旧程序打开这些会话。所有新验收数据使用当前契约。
