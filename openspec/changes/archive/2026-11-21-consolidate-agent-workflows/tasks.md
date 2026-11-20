## 1. 普通 sample 计量对账

- [x] 1.1 固定第 1 批对照源码、两家固定 fixture 和在线恢复、离线预览入口。用临时日志复现 OpenAI 计量矛盾仍被接受，并记录 Anthropic 当前错误分类和合法路径的续写请求。
- [x] 1.2 接入 OpenAI 原生普通 sample 计量校验，并把在线、离线装配中的 validator 都设为受支持 wire 的必需能力。验证五项状态和值与原生归一结果一致，保持 Anthropic 阶段顺序和独立摘要计量。
- [x] 1.3 用临时 checksum 合法的数值不符、状态不符和相邻关系错误场景验证拒绝。带半行的拒绝日志字节不变，writer、补偿、模型请求为零。复用 `TestThinkingUsageFixedCurrentRestoreAndRejectBeforeRepair` 及双 Provider 固定缓存/恢复对照，合法场景请求字节、原生顺序与 fingerprint 一致。
- [x] 1.4 交付第 1 批独立 review、场景证据、`make verify`、OpenSpec strict 和 diff 核对结果。收到用户确认后关闭本批。

## 2. Provider 操作准入与流清理

- [x] 2.1 建立一个 Conversation 操作接纳和完成状态，接入采样、压缩、prepare、finalize/discard 及瞬态续写清理。用显式同步屏障验证竞争接纳至多一次，失败操作无 I/O，重复 resolution 只运行一次回调。
- [x] 2.2 从同一已提交窗口派生普通请求、摘要预检与来源证据。构造提交或 checkpoint 竞争场景，核对 entries、checkpoint、offset 和 revision 不混用，过期摘要预检在发送前被拒绝。
- [x] 2.3 为非终态输出提供可取消的发布和保证可用的终态容量，保持唯一生产者关闭及 Runtime 排空。以停读和故意阻塞 transport 的场景验证可等待的关闭、唯一终态、已观察推理生命周期及已接纳 Sync 的语义。
- [x] 2.4 在 Responses HTTP 流收口验证已接纳尾帧。用有序短 SSE 场景验证重复 terminal 或其后有效帧只产生协议失败，合法 completed 后连接保持打开时仍可完成。核对部分工具输入零 ready。
- [x] 2.5 验证 OpenAI discard、强制关闭、WebSocket 精确增量、完整重置和回退。用可控清理屏障验证下一采样在旧连接释放前不进入，握手拒绝后的允许回退和已发送后零重放各执行一次。复用双 Provider 的 terminal 竞态及 task-cache 场景。
- [x] 2.6 交付第 2 批独立 review、场景证据、`make verify`、OpenSpec strict 和 diff 核对结果。收到用户确认后关闭本批。

## 3. 工具审批与文件发布

- [x] 3.1 让活动请求的选项、身份与取消共用审批准入，冻结实际 outcome/source/feedback 和是否展示。用相反次序的 Stop 与 allow_session、显式 Exec Abort、Patch/Write 未提供 Abort 和重复响应，验证唯一决定及真实来源。
- [x] 3.2 让 Patch、Write 和 Exec 共用 approval fact、拒绝结果与关联 resolved 收口。验证 grant 仅在对应 Sync 后生效，deny/Hook 优先，Exec 仍每次 ask，复用 grant 不发空 resolved。保留批准和 execution started 的独立接纳，在已有 fault hook 上分别验证 Sync 前后取消及零重复副作用。
- [x] 3.3 复用 Patch 与 Write 的暂存所有权校验，保持 Patch 替换、Write 禁止覆盖以及目录链发布策略。使用文件身份、文件/目录 Sync 和清理的确定性场景，验证陌生对象在发现错配时不发布、不由本次清理删除。
- [x] 3.4 复用 `TestPatchExecutorOwnsCleanupBoundary`、`TestWriteFailureAndCancellationPublicationBoundaries` 和 Hook/改写 smoke。核对目标及 journal 字节、调用次序、冻结批准材料、配对原生输出和失败后的可继续状态。
- [x] 3.5 交付第 3 批独立 review、场景证据、`make verify`、OpenSpec strict 和 diff 核对结果。收到用户确认后关闭本批。

## 4. Session 回放、writer 与 Catalog

- [x] 4.1 在严格回放时保存有序的强类型调用、结果、Hook 和 artifact 事实，让校验及投影复用它们。验证 grammar、artifact 与工具投影的首错顺序，Catalog 的元数据路径不构建它不消费的展示数据。Live 选参用相同事实派生。
- [x] 4.2 按真实成功 result seq 一次归约 Task 状态，并在 compaction started 的位置捕获来源。让同一份已解码的 Provider checkpoint 证据供检查、安装、计量与展示消费。以长历史计数验证无按 checkpoint 重扫工具矩阵、无跨消费者重复完整解码，保持每个入口的阶段错误优先级。
- [x] 4.3 贯通恢复 context 与原生回放，按现有错误/取消路径释放 lease。在检查两条记录之间用确定性屏障取消，验证不 repair、不写入、不补偿、零 Provider 请求，且同一线程 lease 可以再次获取。
- [x] 4.4 区分 writer 首次 Write 前的确定失败与 Write、短写、Sync 后的未知状态。临时场景验证两类错误的序号、字节、poison 和后续接纳差异。保留 Runtime 对已经开始的副作用及重放边界的保守处理。
- [x] 4.5 验证旧 Catalog 首次 I/O 缺口与官方 API 可接入边界，按确认后的取舍在同一 change 采用私有原子快照。保留 JSONL 事实源、逐 journal lease 和 busy 查询语义。
- [x] 4.6 实现实际描述符校验、单一快照结构及原子发布，移除 SQLite 和旧派生 reader。验证缺失或损坏后重建、非法快照拒绝、首次读取实际对象和发布前身份替换；验证同步、取消、busy 与过期行时的快照及 journal bytes。
- [x] 4.7 比较 uninterrupted/restored 的请求、原生顺序和 fingerprint。用组合错误、半行、缺失/损坏快照及真实跨进程 lease 核对错误优先级和所有权。复用 `TestCatalogLockAcrossProcessesAndCrashRelease`，运行已编译 CLI 与交互式 TUI 回归。
- [x] 4.8 交付第 4 批独立 review、场景证据、`make verify`、OpenSpec strict 和 diff 核对结果。收到用户确认后关闭本批。

## 5. Transcript、TUI 与 headless 宿主

- [x] 5.1 启动时全量严格关联已有历史，实时获取此次 finalize 的原生后缀和相关 ready facts，原子更新 cursor、工具和主线索引。以相同文本、不可见 outputs、Stop 复合交付、compaction 和分页场景逐项对照 live/restored 顺序、正文和错配拒绝。
- [x] 5.2 验证长历史追加只访问本批新增原生位置和相关锚点。对 1× 和 2× 长度的历史前缀追加相同批次，比较访问次数和内存分配。演练锚点损坏仍导致 TUI 与 headless 沿用失败边界，保持已 durable 数据。
- [x] 5.3 统一视图、按键与补全焦点，先处理 composer 续行再走提交分派，去掉 RuntimeEvent 的重复后处理。用 PTY 场景验证只读面板之上的审批焦点、定向 Ctrl+C、面板位置和草稿恢复，以及 slash 续行不改标题。复用现有 transcript、completion 和 Hook smoke。
- [x] 5.4 保持 headless transport owner 到最终帧写入完成，统一 v1 命令映射及响应校验。用可控 pipe 验证最后 error 阻塞时取消可退出、lease 可重获且无重复写。验证 `context.query + updated` 拒绝，合法接受帧、EOF drain、shutdown 及一次性 JSON 顺序保持。
- [x] 5.5 交付第 5 批独立 review、场景证据、`make verify`、OpenSpec strict 和 diff 核对结果。收到用户确认后关闭本批。

## 6. 已核实的重复与无用代码

- [x] 6.1 用一次现有原生窗口捕获派生双 Provider 的请求、体量、压缩来源和 Context revision。核对各家的字段、scope/TTL、opaque 内容及窗口证据字节。
- [x] 6.2 去除 canonical 前缀比较的副本和重复计算，由 OpenAI 已冻结的逻辑计划派生 WebSocket 帧。复用双 Provider cache golden，核对 full/incremental、连接重置和 receipt。记录固定长窗口下请求证据的访问次数和分配。
- [x] 6.3 共用已验证的正文、Skill、文件块顺序和 Task/Question 结果关联，收敛 OpenAI function/custom 调用状态。用结构化与自由文本边界、部分输入、重复身份和恢复场景核对原 native kind、限额、错误和 call/output 配对。
- [x] 6.4 用一份强类型能力绑定派生目录与 Runtime 执行依赖。复用同语义进程接口和 turn/压缩 worker 生命周期。验证目录 canonical bytes 与 fingerprint、只读离线能力、Hook 与 Exec 生命周期、取消和清理等价。
- [x] 6.5 收敛三态 usage/optional uint 纯转换、文件搜索字节预算和同次准备内的草稿重验，移除 codec 已拒绝的 Hook null 二次扫描。明确命名 Runtime payload 与 headless v1 帧各自的任务快照上限。对照 known 零值、两个信封的预算边界、引用顺序与副作用前静态校验。
- [x] 6.6 移除经生产、测试和平台引用审计确认无消费者的状态、getter 和纯转发，简化值复制。复核现行构造/严格解码能力和批准的占位，比较所有正常入口行为。
- [x] 6.7 交付第 6 批复审及修正后的场景证据、`make verify`、OpenSpec strict 和 diff 核对结果。收到用户确认后关闭本批。
- [x] 6.8 六批均经确认后汇总逐批证据，核对实际规则、术语和相关架构、Roadmap、pitfall 文档。按既有流程同步主规格及严格校验，留待独立归档。
- [x] 6.9 统一工具 started 的准入、可确定的取消和超时分类，以及 Patch、Write、Exec 已允许未开始的回放规则。按已提交额度回放未启动的预算拒绝。真实 journal 验证零未准入副作用、可回放、可续写和恢复补偿，避免未收口工具组落下越序 terminal。
- [x] 6.10 共用 Patch 与 Write 的适用暂存步骤，验证已提交后取消仍成功、目录 Sync 失败仍不可确认、发布前取消及陌生对象保留。
- [x] 6.11 会话选择器以同一次协调的两种范围和报告切换，再在分页前排除当前会话。复现索引缺失和 200 条边界，验证刷新、取消和真实 PTY。
- [x] 6.12 双 Provider 工具及同类文本分片线性追加；收窄普通流原生 item 传输，共享 Responses 请求字段和压缩 usage 验证。验证双 Provider canonical 恢复、WS、原生 opaque、背压和分片预算。
- [x] 6.13 清理历史、transcript 与 headless 的无消费者入口、只写状态和重复校验。收敛用量字段转换及工具目录和执行组件装配，核对各格式字节、缓存 fingerprint 及正式接纳边界。
- [x] 6.14 完成复审修复后的全量质量门及编译产物回归，记录实际 CLI、PTY、真实 journal、临时故障和平台覆盖范围。
- [x] 6.15 区分 headless 已解码命令准备拒绝与全局流错误，复用单一关联及输出 owner。双 Provider 验证未知 Skill 有序拒绝、后续查询及 turn、重复身份与真实坏帧收口。
- [x] 6.16 校验 OpenAI 消息的已知身份与闭合，收敛 HTTP SSE、WebSocket 和独立摘要的 `error` 帧。用编译版验证冲突、部分消息、错误帧、合法 item-only 和恢复续写。
- [x] 6.17 双 Provider 活跃窗口只复制需要的后缀及摘要，收敛解码 clone 与 Anthropic 原生计量校验。核对归档增长时的分配、opaque 内容、原生顺序、请求字节及指纹。
- [x] 6.18 调整 Catalog 的墙钟校验和选择器同范围刷新失败状态。真 Writer 时钟回拨应继续可发现。真实 PTY 核对失败提示、同范围继续预览、跨范围和已有取消语义。
- [x] 6.19 收敛 Runtime/Session 的只读组顺序、不确定副作用错误及 Task 策略拒绝。真实 JSONL 验证活动与闭合回放一致、保留原错误及可补偿前缀、自定义策略拒绝零执行。
- [x] 6.20 本地命令只解析必要词，删去无调用的估算、结果/应答包装及测试专用的运行时入口。核对普通长文本分配和合法命令行为。
- [x] 6.21 对再次复审执行全量质量门和编译后 CLI、PTY 与故障回归，记录范围、等价性和证据。
