# 逐批验收

## 第 1 批 普通 sample 计量对账

### 端到端回归

在 macOS arm64 编译基线 `0f700a0` 与修复后的 `easycode`。用隔离的 HOME、工作区和本地 SSE 服务，通过真实 stream-json 生成两家会话。恢复入口为 `easycode --json --resume`，离线入口在真实 PTY 启动 `easycode resume` 后按 Ctrl+V。有效预览渲染已保存正文，退出后还原终端属性。每个异常场景从同一合法前缀派生，重算受影响行的 checksum 并附加半行。断言 CLI 退出码、公共错误、预览结果、请求计数与 journal 字节。

| 场景 | 结果 |
| --- | --- |
| 三种计量矛盾，分别变更已知数值、`known:0` 到 `unknown`、`unknown` 到 `not_applicable` | 基线 OpenAI 仍可预览，恢复会修尾并发出一次请求。修复后两家都拒绝预览，恢复返回 `session_corruption` 和 `sample usage is invalid`，请求为零，journal 全字节不变。 |
| native sample 与 `sample_usage` 次序不合法 | 两家均在 picker 的 Session 结构扫描阶段排除该日志。显式恢复仍返回 `validate session replay failed`，请求为零，journal 全字节不变。 |
| OpenAI usage 一致但原生 message role 错误 | 修复前后均拒绝预览，恢复都在原生回放阶段以 `restore provider native history failed` 拒绝。请求为零，journal 全字节不变。 |
| 合法会话在同一 thread 继续 | 两家均通过不停机与恢复对照。两版二进制读取同一 durable 前缀时，后续请求 canonical bytes、`user → assistant → user` 顺序、request/native/stable fingerprint 与终态 usage 完全一致。 |

回归覆盖真实 HTTP/SSE、CLI 和 macOS PTY 入口，服务端使用确定性的本地协议应答。远程模型服务及其他平台的原生 PTY 本批未运行。

### Review 与质量门

- 独立 review 确认在线与离线装配都强制绑定计量验证器。OpenAI 对账只读 `kind` 和 `usage`，避免提前报告其他原生条目错误。恢复在进入 repair、writer 启动或中断补偿之前返回计量错误。独立摘要计量沿用既有入口。
- 保留一条最小固定 fixture 回归 `TestOpenAISampleUsageRejectsKnownZeroForUnknownBeforeRepair`。它在基线上复现错误，在修复后通过。原跨进程 lease 测试换用合法的普通样本，争用与续写断言保持一致。复用现有双 Provider 缓存/恢复 golden 和 Anthropic 对账回归。
- `make verify` 通过，普通阶段 582 项，定向 race 61 项。OpenSpec 严格校验 68/68，`git diff --check` 通过。用户已确认第 1 批，任务 1.1–1.4 已关闭。

## 第 2 批 Provider 操作准入与流清理

### 端到端回归

在 macOS arm64 冻结第 1 批生产源码和当前生产源码，分别编译 CLI。两版使用隔离工作区、私有 HOME 和确定性的本地 HTTP SSE 或 WebSocket 服务。最终二进制通过 13 个真实 CLI 场景，其中两条通过 PTY 操作 TUI。

| 场景 | 结果 |
| --- | --- |
| OpenAI 同批读取到重复 `response.completed` 或终态后的完整数据帧 | 第 1 批基线会提交成功样本。修复版只返回一个 `stream_protocol_error`，不落盘 sample、usage 或 tool ready。客户端关闭尚未结束的 HTTP body，下一轮可正常完成。 |
| 合法终态后服务端保持连接，或缓冲区仅有未分隔的半帧 | 合法样本正常提交，客户端关闭 body，无需远端 EOF。部分工具参数与两家异常断流另行验证，均无成功样本，下一轮可继续。 |
| 双 Provider TUI 在已经观察到流输出后按 Ctrl+C | 连接关闭并记录取消，失败样本不入历史。下一轮正常提交，退出后 PTY 属性恢复。 |
| 双 Provider 同一 thread 的不中断和恢复续写 | 与基线编译产物逐字节一致。OpenAI 请求为 8449 字节，Anthropic 为 8191 字节，原生顺序和 request/native/stable fingerprint 一致。 |
| OpenAI WebSocket 增量、取消、恢复与回退 | 已提交响应后的增量帧只带一个新条目和正确 previous response ID。已发送请求的失败未重放 HTTP。取消后用新连接全量发送，重启恢复也是全量。426 握手拒绝精确发起一个 GET 和一个 HTTP POST。 |

现有真实 CLI 手动摘要回归及双 Provider 自动摘要回归通过。覆盖任务证据、摘要只生成一次、独立摘要请求和恢复续写等价。公开远程模型和其他平台的原生 PTY 本批未运行。

### 并发验证与 Review

- 使用临时 `go test -overlay -race` 场景，在原生准备、discard 及 checkpoint 变更处设置同步屏障。验证准入失败无请求，重复 resolution 只执行一次，真实 WebSocket close 完成前下一次采样和 reset 均被拒绝。摘要预检全过程使用冻结窗口，过期预检在打开 transport 前返回。
- 复用并加强现有推理背压回归。先停读至普通事件容量耗尽，读出一项后验证生产恢复，再停读并取消，确认原 producer 清理后以保留槽位发布唯一终态。复用已接纳 Sync 竞态、强制关闭和任务缓存 golden。新增一条确定性 SSE cutoff 回归，锁定完整尾帧和 body 关闭边界。
- 独立 review 核对会话准入和 release 次序、原生窗口一致性、锁外 I/O、无额外 reaper、WebSocket durable 续写资格和 HTTP 尾帧范围，未发现阻塞问题。
- `make verify` 通过，普通阶段 583 项，定向 race 61 项。OpenSpec 严格校验 68/68，`git diff --check` 通过。用户已确认第 2 批，任务 2.1–2.6 已关闭。

## 第 3 批 工具审批与文件发布

### 端到端回归

在 macOS arm64 编译第 2 批基线与第 3 批候选，使用两个本地 Provider SSE 模拟端点、隔离 HOME 和实际工作区。最终 CLI 通过 26 个场景，其中 24 个操作真实 PTY，2 个使用 headless stream-json。

| 场景 | 结果 |
| --- | --- |
| 双 Provider 的 Patch 和 Write 批准、拒绝及 grant 复用 | 核对实际文件字节、审批指纹和 Session 事件。批准、started 和工具结果按顺序提交。拒绝和对应结果同批提交。Write 嵌套创建保留 CRLF，Patch 正常替换目标。 |
| 文件冲突、Ctrl+C 和显式 Exec Abort | 文件冲突保留外部写入。两家基线将 Patch Ctrl+C 写成 `interactive`，修复版写成 `interruption`。Write 内部取消维持 `interruption`，显式 Exec Abort 保持 `interactive`。取消没有启动执行，后续输入可以继续。合法 Exec 只写一次标记。 |
| Hook 改写与现有 grant | 展示的候选和实际写入都只指向改写后的目标。Hook 拒绝先于已有 Write grant 生效。检查原调用 ID、原生配对输出、冻结反馈和 headless 无交互审批事件。 |

现有 `scripts/input-rewrite-smoke.py` 和 `scripts/smoke-hooks.py` 在双 Provider 的实际 PTY 上通过。覆盖审批材料审查、窄屏限制、信任、Hook 拒绝、审计详情和取消。远程模型服务本批未运行。Linux amd64 交叉编译了文件工具测试二进制，实际文件权限与 PTY 场景在 macOS arm64 执行。

### 确定性场景与独立 Review

- 12 个临时 Runtime race 场景核对 Stop、interrupt、shutdown 与响应的先后顺序，以及未提供的选项、重复响应、来源和已发布请求的关联 resolved。通过 Sync 屏障确认 grant 到成功持久化才可见。授权复用不会误发 resolved，策略或 Hook 拒绝仍优先。复用既有 Exec/Write 的 started 接收前后取消验证。
- 文件故障场景跨基线复现 Patch 暂存项被替换后仍可返回 `ok`。修复版保持正式目标和陌生暂存项原样，返回 `patch_durability_uncertain`。临时父目录替换和 Write 清理失败场景通过。独立 review 核对了 lock 外 I/O、实际文件描述符、确定失败与不确定结果，以及 Write 的 no-replace 和目录链发布边界。
- 保留两条最小回归。`TestAcceptedExecApprovalSurvivesCancellationBeforeStart` 固定已接纳决定与独立执行取消，`TestPatchRejectsAndRetainsReplacedStagingEntry` 固定发布前陌生暂存项保留。两条均能区分基线缺口和修复版行为。
- `make verify` 通过，普通阶段 585 项，定向 race 61 项。OpenSpec 严格校验 68/68，`git diff --check` 通过。用户已确认第 3 批，任务 3.1–3.5 已关闭。

## 第 4 批 Session 回放、writer 与 Catalog

### 已实现范围的端到端回归

在 macOS arm64 分别编译第 3 批基线和最终候选，使用隔离工作区、两家本地 SSE 模拟端点、真实 CLI 进程和 PTY。验证使用当前持久化格式。远程模型及其他平台的原生 CLI 未运行。

| 场景 | 结果 |
| --- | --- |
| 两家 Provider 分别执行两次 Task 更新和摘要并恢复 | 摘要条目和新输入顺序正确。合法半行 repair 后的请求与不中断路径及基线逐字节一致，OpenAI 8887 字节，Anthropic 8712 字节。stable、request、native 指纹相同。 |
| 在两条原生记录之间向编译后二进制发送 SIGTERM | 用临时编译 overlay 确定触发位置。两家都返回 `user_cancelled`，半行原样保留，未发出模型请求。退出后新进程获取同一 lease 并正常续写。 |
| 校验顺序、CLI 竞争和进程崩溃 | 组合错误分别验证 Session grammar、sample 计量和 checkpoint 阶段，与基线首错一致。异常日志未修改。真实跨进程 busy 及崩溃释放后序号单调。忙会话仍可在索引中查询，`--continue` 选择原 thread。 |
| 离线预览和派生索引行为 | 双 Provider 的真实 PTY 预览不写日志、不访问模型，现有会话选择器 smoke 7 项通过。丢失或损坏的索引可重建，合法库的 `user_version=73` 得以保留。 |

### Review 与确定性场景

- 使用临时 race 对照核验 163 份可加载的当前 fixture。149 份接受、14 份拒绝，182 条工具投影及 17 个摘要边界与原算法一致。组合损坏保留 grammar 先于 artifact 的错误顺序。元数据路径与完整回放共用这两层校验。
- 长历史含 128 个任务结果和 128 个摘要，任务 reducer 恰好访问 128 次，读取全部摘要来源与最终状态的额外访问为零。两家各自验证在线回放、usage 对账与摘要投影共用一次 checkpoint 解码。离线 usage 仍优先于原生回放。双重损坏的首错与基线一致。
- 保留一条 writer 回归 `TestJournalWriterRetainsSequenceAfterEncodingRejection`。基线在编码失败且文件还未写入时误 poison。修复版保持零 Write、零 Sync、原序号并接纳后续合法写入。复用短写、Sync 失败和取消接纳测试的定向 race 验证。
- `make verify` 通过，普通阶段 586 项，定向 race 61 项。OpenSpec 严格校验 68/68，`git diff --check` 通过。任务 4.1–4.4 已完成。

### Catalog 路线和验收

- 旧路径先检查并关闭句柄，再按路径调用 SQLite。确定性替换观测到主库及两类回滚日志首次 I/O 时的实际句柄不同。官方驱动 v0.32.0 与 v0.37.0 的公开接口没有逐连接句柄注入能力。自行路由原型通过两连接隔离的基本场景，SQLite 的无连接身份回调和平台锁、同步仍需额外处理。结合 Codex 的可重建索引与 JSONL 回源设计，用户确认 SQLite 非硬要求，切换至唯一当前的私有 `catalog.json`。
- macOS arm64 上运行编译后的 CLI 与真实 PTY，使用两家 loopback Provider。验证缺失/损坏快照重建、模式错误/symlink/FIFO 拒绝、旧派生库和 sidecar 字节不变、真实进程跨进程 busy/崩溃释放、锁等待取消、忙会话投影、`--continue`、离线预览与恢复。现有选择器 7 项、双 Provider 会话标题回归通过。双 Provider 恢复请求和指纹回归通过。
- 独立 review 发现无变化的协调会跳过目标身份校验，以及一次打开取消窗口可能误报成功，均已修复。保留少量 Catalog 集成用例，另以临时确定性场景验证暂存对象与目标替换、首次发布 no-replace、文件 Sync 或发布后目录 Sync 失败，以及子进程在暂存 Sync 后、发布前被终止。拒绝保留旧快照，目录 Sync 失败使实例停止提供快照查询，重开时重新核验所见文件。被强杀的暂存文件可能留在私有目录，后续打开忽略该文件。
- 同一台 macOS 机器的 512 会话样本从相同 journal 构建。快照约 275 KiB，旧索引约 408 KiB。冷列表约 232 ms 对 586 ms，三次热列表中位数约 226 ms 对 523 ms。热运行未改快照的 inode、mtime 或 bytes。结果只描述该样本。
- `make verify` 通过，普通阶段 587 项，定向 race 61 项。OpenSpec 严格校验 68/68，`git diff --check` 通过。Linux amd64 CLI、Catalog 和 App 测试已交叉编译。在隔离的 Alpine Linux ARM64 访客中以普通用户、私有 tmpfs 原生运行 Catalog 全包和三项 App 集成回归，覆盖双 Provider 恢复与续接、离线标题列表、跨进程恢复，均通过。Linux 的真实终端交互与物理断电未纳入此次验证。用户确认第 4 批修复后开启第 5 批，任务 4.1–4.8 已关闭。

## 第 5 批 Transcript、TUI 与 headless

### 独立 review 与修复

- 旧的实时 native commit 重投影整个前缀并重建工具索引。现在启动时关联完整历史，实时只读取已 finalize 的后缀及 ready facts，按 journal seq 和原生位置一次更新。隐藏 outputs 推进 ordinal，复合 Stop 同批追加其可见条目，摘要按 terminal seq 进入索引。工具详情更新定位已关联调用。查找最新位置之后的空页安全返回。
- 原只读面板会覆盖审批并截获 Ctrl+C，slash 草稿续行可能误提交。共用焦点判定后，独占交互优先，底层面板位置与草稿保留。活动暂停按显示的 turn ID 或独立 compaction ID 定向交付一次。换行优先于补全和命令提交，Runtime 控制项沿同一路径归约，避免重复后处理。
- 原 headless 在最后 error 写入前停止 transport owner。真实继承管道填满后，旧版收到 SIGTERM 仍堵在写入。现在最后一帧完成前保留 owner，macOS/Linux CLI 可关闭可取消的描述符，再等待 Runtime 和 Session lease 释放。终态输出失败后停止重试。v1 命令名和接纳 disposition 共用协议校验，拒绝 `context.query + updated`。
- 独立复核了后缀序号与工具关联、查询快照和分页、交互焦点、末帧 cleanup。保留少量关键回归测试，其余由仓库外临时场景和已有 smoke 覆盖。

### 端到端与质量门

- macOS arm64 用编译后的 CLI、真实 PTY 和两家 loopback Provider 完成 16 个端到端场景。验证只读 Skills 面板上的审批、位置和草稿恢复、定向 Ctrl+C、`/rename` 续行、相同正文不同 cursor、28 轮历史旧页及继续追加、锚点损坏在 TUI/headless 一致拒绝且不访问模型、不改 journal。复用现有双 Provider Stop、PreCompact、补全及 Markdown PTY smoke。
- 两家 Provider 的末帧阻塞场景都在观测到真实管道写入等待后发送 SIGTERM。进程退出码为 1，保留全部 65,536 字节管道填充内容，无额外帧，journal 字节不变，可重获同一 lease。健康管道和普通文件重定向覆盖有序 control.response、EOF drain、显式 shutdown、单一错误、一次性 JSON 顺序和继承 FD 标志。
- 编译后的双 Provider checkpoint 场景中，正常恢复及修复合法半行后的请求与不中断路径、第四批基线逐字节一致。OpenAI 8,887 字节、Anthropic 8,712 字节。原生顺序和 fingerprint 一致，真实 PTY 的离线预览保持日志字节。
- 4,096 与 8,192 项历史各连续追加 96 条相同形状的带调用提交，两个规模均为 1 次新原生位置访问、4,626 B/op 和 16 allocations/op。测量单独覆盖宿主投影，未计 Provider 请求编译。
- Alpine Linux ARM64 隔离访客通过 headless 全包、定向 TUI/Runtime/App 回归，覆盖双 Provider 恢复、末帧取消、过期 turn 拒绝、断管后 lease 重获和锚点失败。Linux/Windows amd64 CLI 交叉编译通过。Windows 未原生执行，真实 PTY 场景在 macOS 完成。
- `make verify` 通过，普通阶段 590 项，定向 race 61 项。OpenSpec 严格校验 68/68，`git diff --check` 通过。用户确认第 5 批修复后开启第 6 批，任务 5.1–5.5 已关闭。

## 第 6 批 Provider、工具装配与局部去重

### 独立 review 和兼容性

- 双 Provider 的语义视图、原生体量和 revision 由同一窗口派生，编译请求、估算和摘要来源复用各自的原生遍历。确定性组合故障证实预算拒绝仍优先于窗口投影错误。Anthropic marker 位置和数量取自编译计划，既有 5m/1h、scope、opaque native 及缓存 golden 保持，另做 80 组 marker 坐标差分。OpenAI 传输选择与请求收据共用一次 canonical 比较。
- 双 Provider 共享已冻结的正文、Skill 和有序文件块证据，以及 Task/Question 对原调用与结果的关联。OpenAI 两类流式调用共用 item ID、call ID、output index 和输入完成状态。既有关键用例验证混合 function/custom 顺序，临时负例验证部分输入、重复 ID/index、wire 不匹配与超限均不产生 ready/native。恢复保持 question 先于 task 的首错顺序。
- 同一份强类型工具绑定派生在线目录及执行组件，离线事实核验只用声明。普通 turn 与手动摘要共用 worker 启停、发布和取消。Exec/Hook 共用进程边界类型并分别执行各自策略。独立对照确认工具定义逐字节一致，cache.query 的 tool_catalog revision 和 fingerprint 相同。
- Protocol、Session 与 headless 各自持有 DTO，复用三态纯转换。两家原生 usage 复用 optional uint 形状与转换，known 零值、缺失值形状和独立指针均经验证。Runtime payload 和 headless v1 整帧仍分别受 96 KiB 限制。codec 保留 Hook null 拒绝。临时探针覆盖文件候选恰好达到字节上限与越界、同次准备零引用重扫、独立接纳一次复验、重复路径只读取一次、引用顺序和静态错误时零文件及 Skill I/O。
- 用 Go 类型信息审计 Darwin arm64、Linux amd64、Windows amd64 的生产、测试与条件编译引用，确认八个导出 getter 无消费者后删除。去掉已无用途的恢复 getter 与值类型 clone 转发。架构守卫校验批准的占位和现行严格构造边界。

### 端到端和质量门

- 使用冻结的第五批 CLI 与最终编译 CLI 对照本地真实 WebSocket。首请求全量、同连接增量、独立全量摘要及摘要后全量重建均逐字节一致。按各次操作真实 ID 单独核对摘要收据，再比较稳定缓存快照。收到 426 握手拒绝时只发送一次 HTTP，create 发出后报错时不重试。
- 编译版 CLI 的双 Provider checkpoint 场景覆盖 uninterrupted、恢复和合法半行修复。canonical 请求 OpenAI 8,887 B、Anthropic 8,712 B 均与第五批基线逐字节一致，原生顺序、fingerprint、三类错误首错及离线 PTY 预览通过。
- macOS 真实 PTY 覆盖双 Provider 的 8 个 Exec 允许/取消及 Hook 改写/拒绝对照场景，以及文件引用、任务进度、提问共 12 个场景和 PreCompact 手动/自动 4 个场景。关键业务之外复用了既有 fixture 和临时场景。
- 固定 4,096 项请求证据样本中，编译每次分配约从 24.5 MB 降到 4.95 MB。WebSocket 前缀比较访问从 8,192 降至 4,096 项，每次比较从约 8.26 MB 到零额外分配。临时 UTF-8、opaque、空输入及合法偏移的差分验证保持 canonical 指纹和实际发送字节一致。
- Alpine Linux ARM64 隔离访客运行 35 项顶层回归及子用例，覆盖两家 golden、WebSocket 续写及取消、应用恢复、文件准备和进程清理。实际沙箱及交互式 PTY 在 macOS 验证。Linux/Windows amd64 CLI 交叉编译通过，Windows 未原生执行。
- 当次 `make verify` 通过，590 项普通测试、61 项定向 race。OpenSpec 严格校验 68/68，`git diff --check` 通过。以下复审结果补充最终验收。

### 复审修正与最终回归

- 批准后取消、只读准入超时和确定写入失败，均由真实 JournalWriter、Loader、ReplayPlanner 和恢复流程验证。未获接纳的 executor 零调用。批准事实保留，已配对结果严格可回放。工具组不完整时无越序 `turn_failed`，重开后由现有补偿闭合，追加新 turn 时原有日志前缀保持。双 Provider 各两条编译版 headless 故障场景经普通 CLI `--resume` 继续，打开阶段零额外请求。临时 overlay 只控制确定性故障点。
- 同一组前序结果耗尽预算的真实 journal 可回放。故障留出的活动尾部可以计划、复制、验证并补齐。预算尚未耗尽时追加同码拒绝，严格回放依然失败。Patch 与 Write 发布故障覆盖取消、实际目录 Sync 结果和暂存对象替换。已有两处关键测试补充断言，其他复现留在临时目录。
- 两家共四项 macOS PTY 专项覆盖删索引、切范围、离线预览、刷新和聊天内筛选。201 个真实 journal 中，排除源 thread 后两种位置均返回 200 个合格目标，溢出标记为 false。两家 headless v1 缓存帧在固定或归一化会话标识后与修复前逐字节相等。
- 两家实际 reducer 使用 2,048 个 256 B 片段归并合法 Write 输入，并保留 native 参数和 ready 关联。相同新实现仅替换回字符串拼接作为对照，Anthropic 单次累计分配约 633 MiB 降至约 14 MiB，OpenAI 约 629 MiB 降至约 19 MiB。OpenAI 乱序推理场景另核对 16 KiB 分页、准确字节偏移、双换行和 opaque 原文。
- 最终 macOS 编译产物的 21 组 CLI/PTY 回归通过，覆盖审批、Hook、工具续写、headless 控制、摘要、会话切换、Markdown、文件引用、任务和推理。双 Provider checkpoint 请求与修复前、不中断、恢复及合法半行修复逐字节一致，OpenAI 8,887 B、Anthropic 8,712 B，原生顺序和 fingerprint 一致。OpenAI HTTP/WS golden 捕获字段嵌入后的顺序变化，调整字段组后全部通过。真实 WebSocket 顺序、完整及增量帧、独立摘要、握手回退和已发送后不重试均与基线一致。
- 双 Provider 的普通文件和继承管道重定向保持 v1 帧顺序和描述符标志。额外用仓库外的 Go 阻塞栈探针确认末帧已在实际写入中等待，再发送 SIGTERM。两家均保持完整 65,536 B 管道内容，无多余帧，未修改 journal，退出后重新获得原 lease。最初只匹配内核 write 帧的观察未识别 Go netpoll 等待，复核使用实际编码器和 FD 写入栈。
- 当次 `make verify` 通过，590 项普通测试、61 项定向 race。OpenSpec strict 68/68，`git diff --check` 通过。Alpine Linux arm64 访客定向验证两家 Provider、Runtime、App、文件工具、Catalog。Linux/Windows amd64 CLI 交叉编译通过。PTY 在 macOS 执行，Provider 使用本地模拟端点，Windows 无原生运行。复审修正 6.9–6.14 完成。

### 再次复审与回归

- 双 Provider 的真实 stream-json 验证未知 Skill 返回 `rejected/invalid_input`，重复请求走现有去重，`cache.query` 和后续普通输入正常。另在本地 Provider 正在等待时提交未知 Skill 和查询，原活动 turn 仍成功结束。畸形帧保持 `stream_protocol_error`，注入读取故障返回 `input_read_failed`，均未输出输入正文或底层错误原文。
- 编译版 OpenAI CLI 拒绝重复、冲突和未闭合的普通消息。HTTP data-only、具名 SSE 和 WS 的类型化错误都不落入成功样本。HTTP 和 WS 独立摘要故障不安装 checkpoint，既有窗口可继续使用。合法匿名完成项可持久化并恢复。旧二进制生成的冲突样本由修正版在恢复时拒绝，零新增请求或 journal 写入。双 Provider 的恢复、不中断和合法半行修复请求仍逐字节等价，checkpoint 请求分别为 8,887 B 和 8,712 B。OpenAI WebSocket 全量、增量、摘要、回退和禁止已发送后重试保持基线语义。
- 真 Writer 的时钟回拨场景保留最新标题、seq、实际 UTC 时间和原 journal 字节，关闭再打开 Catalog 后仍可查询。两家编译版 CLI 的重复 `sessions list` 和 `--continue` 均识别同一 thread。两家真实 PTY 验证同范围刷新失败仍可预览，切换范围可沿用最近协调的另一份有界结果。
- 七个真实 journal 场景验证同一只读段的乱序在活动、完整及元数据回放中一致拒绝，正常并行和跨独占段正常通过。Patch 结果不确定且 Post 未收口时保留原始 `session_write` 和可补偿尾部，重开仅补齐配对事实，旧 checksum 不变。自定义 Task Ask 的正当未执行拒绝可回放，其他伪造拒绝仍失败。
- 活跃窗口保持同一摘要，只改变约 64 KiB 的归档项从 128 条到 256 条，OpenAI 投影分配从约 9.56 MB 和 19.07 MB 降到相同的 9,378 B/op。4 MiB 文本的 TUI 提交分派从约 160 MiB 降到约 4.8 KiB，Skill 首词识别从 32 MiB 降到零分配。测量使用仓库外的临时基准，数据仅描述所测路径。保留一条 Provider 关键回归，扩充现有回放断言，TUI 原断言改用生产命令分派。
- 定版后重新编译并执行 21 组 macOS CLI/PTY 回归，另验证真实继承管道阻塞末帧后取消、日志字节保持和 lease 重获。`make verify` 通过 591 项普通测试和 61 项定向 race。OpenSpec strict 68/68，`git diff --check` 通过。Alpine Linux arm64 的 Provider、Runtime、App、文件工具和 Catalog 定向回归通过，Linux/Windows amd64 CLI 交叉编译通过。PTY 在 macOS 执行，模型端点为本地模拟服务，Windows 未原生运行。任务 6.15–6.21 完成。

### 归档核验

- 2026-11-21 用户确认第 6 批验收并选择同步主规格。重新拉取后执行 `make verify`、`make test-race-full`、21 组 macOS CLI/PTY 和双 Provider 专项端到端回归，均通过。模型端点使用本地模拟服务。
- 同步 14 份主规格，新增 4 项、修改 23 项并移除 2 项旧 SQLite 要求。逐条核对合并结果，并校准架构、路线图、踩坑记录和测试路径。变更与 67 份主规格均通过 OpenSpec 严格校验，49 项任务全部完成。
