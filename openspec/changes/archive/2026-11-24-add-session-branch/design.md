## Context

动机见 [proposal.md](proposal.md)。当前 `/resume` 已由 Runtime 的空闲许可和 app 的接管 owner 协作。Repository 通过路径中的 canonical ThreadID 获取跨进程 lease，Loader 验证单身份 JSONL，ReplayPlanner 和 Catalog 目前只承认 root。在线 resume、离线预览各自通过已绑定 Provider 策略校验原生项、工具证据与压缩 checkpoint。 [空闲许可](../../../../internal/runtime/resume_permit.go) · [接管 owner](../../../../internal/app/picker_owner.go) · [恢复编排](../../../../internal/app/session_service.go) · [线程树 ADR](../../../../docs/architecture/adr/0003-session-thread-tree.md)

### 参考实现复盘

- Claude Code 2.1.88 的 `restored-src/src/commands/branch/branch.ts:77–164,222–281` 顺序复制主线消息，保留消息 UUID，重建父链和分支来源。它还把预算替换记录重绑到新会话。实时切换沿用内存替换映射，冷恢复从日志按工具调用 ID 取回同一替换正文。EasyCode 直接保留已交付的原生 tool output 和预算事实，避免在重启时重新渲染预览或重算预算。命名后立即切换、展示返回路径、停留在当前工作目录均沿用 Claude 的交互。默认标题使用已提交标题或固定文案，避免把 prompt 作为索引元数据。参考源码 `restored-src/src/utils/sessionStorage.ts:3682–3693`、`restored-src/src/utils/toolResultStorage.ts:960–988`、`restored-src/src/screens/REPL.tsx:1867–1925`。
- Claude 的交接还会清理未闭合工具消息、执行 SessionEnd/SessionStart、复制计划文件并重建 Read 状态。EasyCode 通过空闲许可取得完整 cut，以原生 ledger 验证工具配对，审批、Read 资格和待审方案由新实例初始化。历史方案原文只参与回放。这样可复用当前无副作用的恢复路径。参考源码 `restored-src/src/screens/REPL.tsx:1735–1828`、`restored-src/src/utils/sessionRestore.ts:435–503`。
- Codex `9f2b87495bf979f6c1953cf00c98143981664914` 的普通 TUI slash 路径会拒绝活动或待启动的回合并保留草稿。app-server 另提供最新切点和显式 turn 切点，其活动快照可以只给子线程合成中断标记。存储层区分可脱离源 rollout 的 copied fork 与依赖有界祖先段的 paginated reference-backed fork。EasyCode 采用空闲时的最新完整切点和 copied fork 的独立恢复特性。目标持久化与挂载分开，即使首屏 hydration 失败也应保留已保存分支。参考源码 `codex-rs/tui/src/chatwidget/slash_dispatch.rs:147–205`、`codex-rs/tui/src/app_server_session.rs:1637–1655`、`codex-rs/app-server/src/request_processors/thread_processor.rs:5633–5655,5900–5926`、`codex-rs/core/src/thread_manager.rs:1896–1947,3477–3559`、`codex-rs/core/tests/suite/fork_thread.rs:314`。

## Goals / Non-Goals

**Goals**

- 一个用户意图对应一个分支创建尝试。句柄安全、源边界冻结、分支发布和终端接管分别可以验证，失败时用户能分辨分支是否已创建。
- 在同一逻辑 Session 内提供可独立恢复的线性线程，支持从分支继续派生。历史事实的归属、原生 bytes、工具配对与压缩证据都可追溯。

**Non-Goals**

- 历史任意切点、活动回合快照、工作区回滚或 worktree 创建、分支合并、跨 Provider 或模型迁移，以及 Subagent 和 headless 创建属于后续能力。

## Decisions

### 一个本地交互意图

`/branch` 和 `/fork` 通过同一分派与补全规则形成一个宿主意图。复用标题值校验，先检查原始后缀，纯 Zs 空白按省略处理。默认标题从切点上的已验证名称派生为 `<源标题> (Branch)`。源未命名或追加后超出标题上限时使用 `Conversation (Branch)`。显式标题照常允许重名。标题中的 `@`、引号和普通 Enter 提交时的反斜杠按文字解释。Alt+Enter、Ctrl+J 和已解码粘贴沿用编辑行为。

只在源含已提交用户输入、Runtime 准入健康且操作已真实结束时派生。创建时要求启动工作目录与源记录的规范化创建目录一致。若显式 CLI 恢复曾在另一目录启动，当前会话仍遵循已有恢复契约，创建分支提示用户从源创建目录启动。现有根线程与分支使用相同的入口规则，包括启动选择器的跨目录明确确认、显式 CLI 恢复使用启动目录，以及聊天内选择和 `--continue` 的目录匹配。

Runtime 复用交接许可的唯一准入点。许可绑定源实例和请求意图，只执行内存接纳与释放。候选复用当前终端已验证的实际 workspace、私有存储及冻结扩展来源，沿用现有交接的句柄身份检查。app 在同一个交接 worker 中协调 Session I/O 与装配。原始请求保持不可变，新生成的目标 ID 随类型化发布结果传递，并与尝试、源实例和候选接管逐一核对。TUI 在准备阶段独占焦点，接管前取消后恢复原草稿、光标和运行实例。完成后展示分支标题及完整源恢复命令，窄屏可换行查看。将分支命令直接交给普通输入或另起交接 owner 会导致输入排队与资源所有权出现新的竞态。 [本地分派](../../../../internal/tui/model.go) · [焦点与接管](../../../../internal/tui/chat_shell.go)

### 分支 journal 的记录归属和边界

保持 ADR-0003 的一个根线程与同 Session 血缘。分支文件包含切点之前源 journal 的完整原始 bytes，随后是唯一标记本次派生的单记录 batch `thread_forked`。标记 envelope 的 SessionID 与前缀一致，ThreadID 是新 UUIDv7，ParentThreadID 是切点处的源 ThreadID，UTC timestamp 是分支创建时间，TurnID 缺省。payload 只含 `source_seq`、`source_sha256` 和规范 `title` 三个必填值。digest 对标记之前的完整 bytes 计算，包括 LF。新记录从前缀末尾的 seq 顺序递增，沿用现有行及 batch 上限。

Loader 在读取每条完整行时递推 SHA-256。验证标记时取得此前累计值，随后才把标记原文送入同一哈希，以便继续验证更深的派生链。逐条 checksum 继续校验本条 envelope 与 payload。前缀哈希按实际消费的行和 LF 更新，独立于修复凭据对整个文件的哈希。缓冲读取可能预取后续行，不能用底层 reader 的读取位置决定分支边界。完整性证据用于验证私有仓库内的自包含记录链，信任边界与现有 journal checksum 一致。当前记录拥有者只在合法标记处变更，父必须是上一个段的拥有者，子必须在本条链中首次出现，SessionID 固定。最终完整 batch 的拥有者必须等于文件定位的 ThreadID。源段和已完整提交的标记都属于事实。缺失、截断或无效的目标分支标记判作损坏，普通的分支后缀尾部损坏仍按既有条件修复。

ReplayPlanner 用同一状态机消费已验证链。初始 metadata 建立唯一 root，每个标记之前必须是完整的操作边界，且来源已有用户输入。按原始归属建立历史 ToolCallOrigin 和 artifact 对账，按原序推进全历史的 native revision、checkpoint 前驱、任务来源与投影。跨标记时重建当前线程的 artifact 引用映射，同时保留祖先段已发现的首个引用错误及现有报告顺序。根 Session 元数据继续指向根，最后一段提供活动线程、当前标题、当前可恢复 tail 和本线程的写入顺序。复用 loader 与 replay 构造只读预览和活动恢复，同一原生验证应早于 repair 或接管。Catalog 用经过验证的当前线程 metadata，保持现有与 Provider 独立的有界发现路径。

选用同文件前缀使 fork-of-fork 只需线性遍历和复制，消除了持有多个祖先 lease 或递归读取活动源的依赖。仅保存父指针会让恢复依赖源路径和独占 lease。把记录改写成新 ThreadID 会抹去授权和 checkpoint 证据，双文件快照会引入另一套原子发布与孤儿协调。 [现有装载](../../../../internal/session/loader.go) · [现有回放](../../../../internal/session/replay.go) · [压缩前驱验证](../../../../internal/session/compaction.go)

### 由 lease owner 读取、验证和发布

源 writer 已接收的请求必须先完成。writer owner 持有最后一次确认的 seq 与文件末尾偏移，从已验证且必要时已修复的打开结果初始化，每次成功 Sync 后推进。通过同一 leased descriptor 按偏移复制准确范围，并用目标校验结果对照源身份和最后提交序号。文件 stat 或物理 EOF 只用于发现与确认范围不符。遇到实际源日志失配或不能取得已确认范围时收口源写入，保留原始 bytes 供恢复检查。目标故障或取消后，只有源仍健康才可返回继续。复制采用有界缓冲，app 无需按路径重开源，不安装第二个源 lease。

在 UUIDv7 定位的私有日期目录独占创建暂存文件，实际句柄校验普通文件、0600、owner 及单链接身份，并取得目标 lease。暂存名使用 `.tmp` 后缀，枚举器会把任何畸形 `.jsonl` 当作错误。使用现有 record codec 写入标记，仅对暂存候选执行完整 Session 与 Provider 校验。文件 Sync 后，通过根目录的安全遍历复核实际日期目录与暂存 inode，再执行排他 rename。最终名字创建后，目录 Sync 前后验证规范路由仍指向同一日期目录和同一 leased inode。日期目录被改名或替换时，持有旧目录句柄的 rename/Sync 仍可能成功，因此目录项校验须覆盖从根到目标的定位。发布前失配属于确定未发布，rename 后定位失配属于不确定。清理由实际句柄确认本次拥有的暂存，替换项或可能已发布的名字留给既有安全边界。用同一 lease 续写，避免发布成功与目标 writer 启动之间的重开窗口。源与候选的存储 I/O 都在 owner 路径锁外执行。 [普通创建](../../../../internal/session/repository.go) · [现有排他发布](../../../../internal/session/catalog/secure_store_unix.go)

| 阶段 | 结果与所有者 |
| --- | --- |
| 接纳前 | 已取消的请求无创建副作用。忙、无已提交输入、目录不符或输入无效时原聊天可继续。 |
| 已接纳，尚未接纳原子发布 | 取消或失败时等当前 I/O 返回，清理本尝试持有的暂存和目标资源，确认释放源许可。 |
| 原子发布已接纳 | 等待原子操作、目录 Sync 和规范路径确认。三个条件均确认时返回 durable 目标 ID。发布、路径绑定或 durability 无法确认时返回带目标 ID 的不确定结果并停止目标写入。 |
| 已确认发布，接管前取消或准备失败 | 保留分支。确认目标清理以及源健康后返回原聊天，并显示可恢复的目标 ID。接管前源已失效时按终端清理路径结束，诊断仍带已知分支结果。 |
| 接管已接受 | 按现有关闭路径等待源 worker、writer 和 lease 完成，随后启用目标输入。Esc 由过渡状态消费。退出或终端故障由同一 owner 清理，并保留已知发布结果。 |

在原子发布接纳点，owner 在同一次有界内存转换中检查取消、请求世代、当前源实例健康及匹配许可。在候选校验时获得的健康结果不能替代本次接纳。I/O 在锁外执行。接纳后即使源退出，也要把原子操作收口为确定未发布、已确认发布或不确定。rename 后至目标 lease 释放前，竞争者按现有 busy 规则收口，Catalog 不能把占用中的新条目当作已验证候选。

接管是独立的接纳点。宿主在尝试 owner 上保留发布分类和完整目标 ID，避免 UI generation 变化、终端取消或结果 channel 停止消费吞掉已提交事实。交互可用时从匹配的终态展示。仅投递到可取消的结果 channel 不算交付。终端已退出或不可写时，宿主等待唯一 owner 完成并通过现有安全诊断通道报告尚未交付的结果，保留原退出类别。结果只包含安全状态和 canonical ID。终态结果沿 app Outcome 返回 CLI，独立于主故障及 ReportState；共享环境清理失败时仍保留该结果。这样即使退出分类为已报告的取消，CLI 仍能交付未显示的分支诊断。接管失败后的诊断沿用同一 ID。已确认发布后才启动目标 writer，以及需要写目录的线程资源装配或 artifact 维护。若此时装配失败，目标仍可恢复。 [当前退出调用链](../../../../internal/app/session_picker.go#L108) · [CLI 结果报告](../../../../cmd/easycode/main.go#L148) · [候选与取消](../../../../internal/app/picker_owner.go#L303) · [artifact 装配](../../../../internal/app/app.go#L383)

后来经既有入口恢复仍可定位的有效分支时，Repository 在目标 lease 下重新校验实际日期目录和目标 inode，同步目标文件及目录，确认规范定位未变，再允许 repair、补偿或 writer 接管。只读预览和 Catalog 继续执行只读验证。

### 原生恢复与执行状态

app 复用已绑定的 `RestoreConversation`、`orderedRestoreRecords`、压缩用量核对和 transcript 投影。完整继承已提交用户输入、双 Provider 原生顺序、工具输出、冻结 Hook 反馈和 checkpoint。保留各事实的真实历史来源。任务状态随最后已提交结果继承，后续更新只追加到分支。继承样本的计量可以查看，分支新操作的进程观测独立起算。压缩后的下一次派生仍从全部已提交 journal 建立，生效窗口由已有 checkpoint 恢复逻辑决定。

活动工具 ledger、临时审批和 Hook 信任、workspace Read 资格、待审计划和交互 effort 选项由新控制实例初始化。分支当前 artifact 授权、清理和模型读取以当前 owner 的已提交引用为限。宿主转录投影从历史 ToolCallOrigin 给出保存时的完整度和实际来源 ThreadID。来源不同的保存输出显示历史预览和返回实际来源的提示，不显示分页入口或发起首屏原文查询。宿主查询本身也按该条目的归属拒绝原文，避免仅靠 UI 判断或用当前映射解析继承行。应用复用冻结项目来源，仅服务新操作。 [恢复证据装配](../../../../internal/app/compaction_restore.go) · [artifact 装配](../../../../internal/app/app.go#L383) · [授权约束](../../../../internal/tool/artifact_references.go)

分支持有独立的 Provider resource、Conversation 和传输，源关闭只关闭其自身资源。两家首次分支采样发送完整的有效原生窗口及本次输入。切点已有压缩时，窗口由最新有效 checkpoint 和其后原生增量构成，压缩前的原始事实用于本地验证与转录。OpenAI 首个分支 WebSocket 请求不携带源的 `previous_response_id`，按当前 policy 使用分支 ThreadID 派生 key。Anthropic 保留有效窗口中的原生项及适用的 cache marker/TTL 策略。Codex 同样区分 transcript 与从 compaction 起始的模型上下文，并在没有可用续写游标时发送完整请求。参考源码 `codex-rs/thread-store/src/local/paginated_fork.rs:100–111`、`codex-rs/core/src/client.rs:2487–2529`。血缘、标题、创建时间、物理源路径仅用于持久化或宿主，模型输入的稳定部分继续由原生内容、配置和冻结目录决定。比较两个时点下**同一分支**的 canonical bytes、item sequence、footprint 和 fingerprint。比较源切点与分支时单独核对继承内容、稳定段及预期路由差异。 [原生回放](../../../../internal/provider/native_replay.go) · [OpenAI key](../../../../internal/provider/openai/user_input.go)

### 发现与标题

Catalog 以 ThreadID 为唯一项，同一 SessionID 可以出现多个条目。分支的创建时间与初始标题取本线程标记，当前更新时间来自其最后完整提交。根与分支共用兼容筛选、busy/invalid 规则、排序和 limit。列表提供 `Branch of: <完整父 ThreadID>`，选择器可显示紧凑分支标记及验证后的父 ID，标题和父线索均经过终端安全展示。既有搜索字段继续负责过滤。Catalog 对 `--continue` 只提供候选 ThreadID，取得目标 lease 后须重新校验所选记录与同一规范化 cwd 及 Provider 三元组匹配。分支活跃时的 `/rename` 写入分支后缀，父线程的后续改名和追加只影响父自己的目录行。

## Risks / Trade-offs

- 长对话分支需要复制一次已提交前缀，磁盘及目录重建扫描量随各分支的物理日志大小增长。复制缓冲有界，完整回放保留当前 Loader 与 Provider 校验所需状态。只对候选物化一次完整计划，避免额外物化源计划或按祖先深度重复扫描。磁盘满时按发布前失败收口。
- 进程在发布前崩溃可能留下私有暂存。其文件名不进入现有发现流程，内容受私有目录和文件权限保护。正常取消和失败由创建 owner 回收自己仍持有的暂存。崩溃暂存的跨进程回收留给独立的所有权设计。
- 同工作区的两条对话可各自恢复，但都操作实际工作区。新线程按现有规则重新读取和审批，降低把分支历史当作当前文件状态的风险。
- 各线程可能处于不同的 POSIX 与文件系统环境。具备现有安全 opener 和排他发布的适配器才可创建，按实际平台验收结果声明能力。

## Migration Plan

启用时同步唯一当前规格与固定 fixture，并更新 README、能力清单、架构、ADR-0003 和路线图。普通根日志符合新 grammar 的单线程形态，分支使用真实的类型化事实。Catalog 的旧派生快照若不符合当前结构，通过既有重建路径从 journal 生成。已创建分支应使用识别其记录词汇的构建恢复。降级前关闭活动 writer，并选择旧构建能够读取的数据集或保留独立备份。
