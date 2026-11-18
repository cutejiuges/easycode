# 会话内 effort 完整代码复审

> 历史日志、运行清单和诊断转储已清理，仅保留文字结论；当前验收以现存测试及质量门为准。

当前变更已于 2026-11-15 归档，最终收尾见 [archive.md](archive.md)。下文保留复审当时的事实和状态。

本文保留 2026-11-14 完整代码复审的事实和结论。2026-11-15 再次回归、真实 CLI/PTY 与测试耗时排查见 [regression-2026-11-15.md](regression-2026-11-15.md)。

复审日期：2026-11-14；分支：`feat/runtime-effort-control`。本轮按用户要求复查已实现的 change，保留此前实现和证据，重新打开 tasks 第 9 节。范围仍是进程内 effort、完整操作冻结、双 Provider 既有请求路径、缓存恢复和 TUI；没有增加配置持久化、模型探测、原生 configuration_update 或兼容格式。

本文记录源码检查与修复，最终执行结果见 [verification.md](verification.md) 的复审验收段及 evidence/review。初始工作区清单见 initial-workspace.json；此前质量门不替代本轮修复后的验证。

## 发现与修复

| 编号 | 严重度、可复现问题及影响 | 修复与业务回归 |
| --- | --- | --- |
| R1 | P2：独立手动摘要已接纳后，若首次 `compaction_started` 写入在提交前取消，尚未进入 Kernel 的 summary 路径，原有 WebSocket continuation 未清除。切回 high 后可能沿用摘要之前的 response ID。原普通 turn 有失败收口，独立摘要缺失。 | 普通 turn 和摘要复用 `releaseFailedContinuation`，仍由原 worker 清理；取消/失败先清除 continuation，再恢复临时 operationProfile、结束 active 和完成 join。`TestEffortManualCancellationBeforeNetworkResetsContinuation` 在修复前确定失败。新增真实 owner + WebSocket 的 `TestEffortOwnerWebSocketNoopAndPreNetworkSummaryCancellation`：同值来源变化及未执行 low 保持一条连接和增量；取消已接纳摘要时零摘要网络/零新事实，旧连接先关闭，下一 high 使用五条完整原生输入且无旧 response ID，receipt 同为 full。 |
| R2 | P2：TUI 保留名已有 effort，Skill 输入解析的独立 switch 漏项，headless 普通 `/effort` 正文可能意外挂载同名 Skill。 | `domain.IsReservedSkillSlashName` 为两个消费者提供同一纯 switch。`TestEffortSlashReservationPreservesExplicitSkill` 修复前失败；新增双 Provider 实际 headless 验收，裸 slash 保持正文和 high 设置，`$effort` 才加载附件，稳定 tools/cache key 不变；TUI `/skills` 选中同名项仍填入 `$effort `，限定插件名称不受保留名影响。 |
| R3 | P2：28×10 中正文、reasoning 状态、effort 通知和 queued 输入同时出现时，通知预算未扣除其他状态与队列，固定 transcript 最小一行以及未裁剪提示产生 11 行和超宽内容，挤出 composer；满队列会进一步放大。 | `chat_space.go` 共享真实行预算，正文允许零可用行，主视图最终裁剪宽高并优先保留输入区；队列只裁剪展示，保留 FIFO 头和隐藏数量。新增原场景固定快照、64 条队列/状态叠加、四种尺寸和输入保真回归，保留原测试断言。17 份 effort 快照包括原 16 份及新增 busy queue；既有 12 列 Markdown 标题和 28 列快捷提示快照仅修正超宽显示。Darwin PTY 新增真实 queued 输入与通知叠加、缩放后的物理行列断言。 |
| R4 | P3：TUI 的选择水位缓存保存整份 snapshot，迟到的新选择仅更新三个选择字段，旧活动身份/状态可能残留为不自洽内部对象。 | 缓存改为仅含 selected/source/revision 的私有纯值。相同序号却不同选择/来源的矛盾观测拒绝；新序号仍能更新权威选择而不恢复草稿/旧通知。新增重复 response、重复 snapshot、新草稿保留与同序号冲突业务断言。 |
| R5 | P3：非法 `/effort` 后执行本地 help，帮助已成功显示但旧错误仍留在主线。 | 成功本地 help 清除旧 errorSummary，仍零控制提交；`TestEffortHelpClearsRejectedCommandError` 验证错误恢复、帮助和草稿消费。 |
| R6 | P2：封闭 active 联合允许 turn_starting 配 activity=compacting，违反“自动摘要必须有 durable turn”的方案；还允许序号 0 的非启动来源，以及同选择序号却捕获不同 wire 值。 | snapshot validator 和 strict decoder 拒绝不可能组合。新增 `TestEffortStartingCannotClaimCompactionActivity` 与 `TestEffortRevisionAndSelectionStayConsistent`。不新增字段、版本或兼容 reader，固定合法 golden 不变。 |
| R7 | P3：架构文档仍说设置不进入内部 ContextSnapshotPayload，与本 change 的必需 effort 相矛盾；设计验收表还保存不存在的计划测试文件名。 | 架构明确内部/外部投影区别、失败收口、选择缓存和队列布局；设计表改为实际测试路径；踩坑记录补充首次网络前取消、共享保留名和状态叠加。 |

另补充原验收覆盖不足：将 Stop 冻结回归扩展为无摘要/有自动摘要两种实际 HTTP 场景。在首个 Stop continued 后确定性开启自动摘要预算，证明摘要属于同一 root turn、独立输出上限为 4096、三次普通 sample 和一次摘要均 high，下一回合才 low。只补业务验证，没有改动 Stop 或自动摘要调度语义。

## 两个参考工程复核

两处参考均只读，未构建、未加入依赖。参考清洁状态、HEAD 和选定源码校验值见 `evidence/review/reference-readonly.json`。

| 参考、位置 | 复核结论与本轮对应 |
| --- | --- |
| Claude Code，`a8a678cb6244e6770e1e421767ff0987a1d95549`，`restored-src/src/commands/effort/effort.tsx` 的 showCurrentEffort/executeEffort/call | 本地命令、简短 onDone 反馈、current/status、auto/unset 与 help 仍是产品参考。EasyCode 保留明确 reset、原生档位和进程作用域，不采用其用户配置写入及模型默认推断。R5 修复帮助后的错误恢复；R3 保持短通知、输入优先和原焦点路由。 |
| Claude `restored-src/src/query.ts:694`、`services/compact/compact.ts:1305`、`:1324` | query 可读取最新 appState，compact 关闭 thinking 但继续传 effort。EasyCode 按已确认方案冻结完整 turn，摘要保留 thinking/display/effort，只使用独立输出额度；新增 Stop+摘要组合回归验证该差异，不将其误写成完全等价。 |
| Codex，`c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`，`codex-rs/core/src/session/step_settings.rs:36`、`session/mod.rs:2162` | 不可变捕获设置、选定值与有效值分开、候选校验后发布。EasyCode 单 owner 的状态交换和活动 profile 捕获已满足；R1 补齐 worker 完成前资源清理和临时值复位，R4 让 UI 选择缓存只持有其自身语义。 |
| Codex `core/src/session/reasoning_effort.rs:23`、`:96`；`core/tests/suite/reasoning_effort_override.rs:739`、`:1018`、`:1239`、`:1596`、`:1656` | sampling/compaction 的 request pin、可信 update、重连及恢复有独立边界。EasyCode 只使用已实现的普通请求属性比较，不新增 pin/update；R1 的真实连接回归补上原 Kernel 测试无法覆盖的“操作已接纳但尚未调用 Kernel”窗口。 |
| Codex `tui/src/app/thread_settings.rs:240`、`:274` | 请求接收与权威设置应用分离。EasyCode tea.Cmd 成功不投递第二份结果，ControlItem 是唯一成功反馈源；重复、迟到、同序号旧操作及新 overlay 均保留测试，R4 补同序号矛盾选择。 |

## 原 38 项任务逐项核对

表中“保留”表示检查实现满足已确认方案；“补强”指本轮修复或新增业务断言。全部执行结果由本轮最终验证确认，不以源码检查替代测试。

| 任务 | 审查结论与实际证据 |
| --- | --- |
| 1.1 | 保留：`domain/effort_test.go` 保留两个 family 的原字段和显式输出来源，auto/reset/none 分离。 |
| 1.2 | 保留：`context/inference.go` 为 config 和 owner 共用，`context/inference_test.go` 与 config reasoning 矩阵验证预算；无反向依赖。 |
| 1.3 | 保留：`TestEffortBudgetAndProfileIdentity` 固定同一输入比较 profile；真实 WS 另验证未执行选择不改已提交缓存证据。 |
| 2.1 | 保留：`protocol/effort_test.go` 的专属命令、结果和安全错误；headless 只在文本边界解析别名。 |
| 2.2 | 补强 R6：strict 联合及活动类型/状态组合；固定 active golden、16 KiB 上限。 |
| 2.3 | 保留：内部 context 必需 effort，外部只投影 ContextMetricsPayload，固定 `context_metrics.golden.jsonl` 不变。 |
| 2.4 | 补强 R6：必需整数、溢出/未来捕获、初始来源和同序号捕获值一致；无内部格式标记。 |
| 3.1 | 保留：`provider.NewSamplingInput` 必需设置；两个 `RejectsMissingAndForeignPolicy` 在编译前拒绝零值/跨 family。 |
| 3.2 | 保留：Anthropic `effort_test.go` 的 3 thinking×3 cache×4 choice×2 purpose 固定指纹；手动预算继续同源。 |
| 3.3 | 保留：OpenAI `effort_test.go`、`effort_websocket_test.go` 与既有 HTTP/WS 测试；没有 config fallback。 |
| 3.4 | 保留：两家 plan 同时产生 body/properties/native evidence/receipt；原默认及恢复 fixture 固定，配置序号不入请求。 |
| 4.1 | 保留：`TestEffortOwnerAtomicChoicesOverflowObservationAndNoFacts`、handoff 回归和有界去重/关闭测试。 |
| 4.2 | 保留并补强：`TestEffortTurnCaptureQueueAndCancelledHandoff`、Stop HTTP 组合、Question/Approval app 回归；worker 退出后清除临时 profile。 |
| 4.3 | 补强：manual/automatic compaction capture、真实 Stop 后自动摘要、独立额度和来源抑制/熔断测试。 |
| 4.4 | 保留：owner 活动期只读发布副本；idle 对 selectedProfile 纯规划；缓存仍是实际最后提交请求的证据。 |
| 4.5 | 保留：构造错误拒绝前无交换，context 观测错误不回滚，effort 查询独立可用。 |
| 4.6 | 补强 R1：started Sync、失败、finishing/cancel 屏障保留；独立摘要在网络前取消也加入原 worker 清理边界。 |
| 5.1 | 保留：ChatSession 两个小接口只提交既有 owner，无额外直接 RunTurn 通路。 |
| 5.2 | 补强 R5：精确命令、参数矩阵、原草稿/光标/历史保真和 help 清错，帮助零提交。 |
| 5.3 | 保留：`tui/context_test.go` 的纯渲染/迟到查询；不变字段来自启动装配，effort 来自 owner。 |
| 5.4 | 修复 R2：共享保留名；256 Skill+7 内置容量、Tab、$、/skills、headless 和限定名回归。 |
| 5.5 | 修复 R3：17 份 effort 固定快照，四种尺寸、64 输入/状态叠加及真实 PTY 物理边界。 |
| 5.6 | 补强 R4：独立选择水位、重复回执/快照、同序号冲突、新草稿/overlay/取消中迟到回复；不恢复活动状态。 |
| 6.1 | 保留：4 KiB 严格 branch、null/未知/重复/UTF-8/EOF，headless `input.submit` 不解释 slash。 |
| 6.2 | 保留：显式 DTO 和既有唯一 writer；response 在 snapshot 前，拒绝无 snapshot，外部 v1 和旧数值 bytes 保持。 |
| 6.3 | 补强 R2：实际 headless 同名 Skill 附件与原正文控制隔离，EOF drain 与流中控制仍由既有 runner 验证。 |
| 6.4 | 保留：`effort_stream_test.go` 的短写、满 buffer detach、接收前/后取消；app 子进程 lease/cleanup 回归。 |
| 7.1 | 补强：双 Provider tools/question/approval 加真实 Stop+摘要组合；核对真实 body 和次数。 |
| 7.2 | 补强 R1：400/断流/取消/摘要失败不降档或重试；首次网络前取消零摘要请求，既有权限和 lease 行为不变。 |
| 7.3 | 保留：`TestRuntimeEffortRestoredRequestIgnoresProcessRevision` 与重启测试；opaque/pairing/checkpoint、request/native/profile/source 指纹和 journal 前缀等价。 |
| 7.4 | 补强：原 Anthropic/cache/golden 矩阵保留，真实 owner 证明 source-only 和未执行 low 不重连或推进观测。 |
| 7.5 | 补强 R3：Darwin PTY 为双 Provider×颜色×正常/取消 8 组；增加 queued+notice 叠加和 resize 后行列断言。 |
| 7.6 | 修复 R1：Kernel 的实际失败/取消/summary/reconnect 回归外，新增 owner 的接纳后网络前取消；无旧 response ID 复活。 |
| 7.7 | 保留：capture 未发送、本地预算拒绝零请求、starting/finishing/overlay 与新流序号 0；实际 PTY 非模态键路由。 |
| 8.1 | 修复 R7：架构和设计过时措辞修正，README/Roadmap/capability 与六项主规格范围保持。 |
| 8.2 | 重跑：修复最终源码上完整 `make verify`，不得以此前门或局部测试代替。 |
| 8.3 | 重跑：本 change、全部主规格、全部 changes/specs strict；链接和真实文件名、当前 record vocabulary 再查。 |
| 8.4 | 更新：本 review、verification 和独立 review 证据，不覆盖此前执行记录；当前 change 保留供评审。 |

## AGENTS.md 七组约束

| 组 | 审查结果与边界 |
| --- | --- |
| 1 项目基线 | Go、Resty/Sonic/Bubble Tea 沿用；双原生协议、base_url/api_key 模式不变；参考只读，未添加库或跨工程依赖。 |
| 2 OpenSpec | 在用户明确的 apply/复审范围内修补契约与验收；六份 delta 和主规格既有需求覆盖修复，未扩张模型/持久化范围。第 9 节跟踪本轮，最终验证后才完成。 |
| 3 架构 | 新共享 helper 只有纯命令名称判断，domain 无 IO/TUI 依赖；选择/活动策略/Provider/显示各有边界。无全局可变 registry、any 协议、隐藏 goroutine、双 reader、版本门或空能力；构造函数仍纯内存。 |
| 4 Provider/缓存 | 编译仅读取强类型操作输入；native history 是事实源，未反向使用 SemanticHistoryView。相同 plan 产生请求与证据；选择序号/来源不进入缓存。普通/摘要失败共用原 worker 清理，恢复 canonical bytes 与原生顺序由双家 app 回归及固定 fixture 保证。 |
| 5 Tool/Session/安全 | 未修改 Tool schema、Session/current/native/checkpoint/SQLite 格式或 record kind。没有新的文件/进程执行或 secret 输出；设置不写 journal/config。原 append-only/Sync、真实子进程 lease、坏尾修复、幂等/取消和 shutdown cleanup 矩阵继续由全量/race 执行。 |
| 6 Go/整洁 | 新注释为中文、对外错误为英文；核心流程用强类型和 kind switch。无锁内 IO/等待，无新增后台 owner；复用失败收口、保留名和布局预算，操作值退出复位；无 commit/PR、无绕过 hooks。 |
| 7 验证/完成 | 可复现缺口先有失败业务断言，再修复；新增真实 WS、双 Provider headless、Stop+摘要、固定快照与 PTY。最终 `make verify` 含 vet/Staticcheck/架构/全量/race；真实终端仅声明 Darwin。结果、限度与任务完成以最终验收段为准。 |

## 明确保留的行为

当前操作的工具循环、自动追加、Stop 和自动摘要保持捕获的设置；queued strict-next 在真正启动时读取最新选择。设置命令不重置连接或安装缓存基线，只有真实请求及其清理拥有该权力。auto 只省略 effort，reset 恢复启动值，服务器默认未知；错误不降档、不重试。历史、usage、工具 pairing、opaque reasoning 和恢复的新进程序号规则不变。

未新增需延期的能力或未解决的实现例外。真实远端模型与 Linux/Windows 原生终端未在本机执行；使用本地服务、假凭据和 Darwin 真实 PTY，平台与网络范围不能外推。

## 最终状态

44/44 项任务完成。本轮 `make verify`、功能 race、18 组 Darwin PTY、本 change 与全部 60 项主规格/62 项 changes+specs 严格校验全部通过；完整质量门耗时 897.92 秒，1,368 个输入文件执行前后校验一致。六份 delta 的 14 条 requirement 与当前主规格正文完全一致。详见 [verification.md](verification.md) 及 任务覆盖核对。修改保留在当前工作区，未提交、推送或归档。
