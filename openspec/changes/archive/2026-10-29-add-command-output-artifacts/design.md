## Context

动机见 proposal.md。当前 `internal/tool/exec_output.go` 为 stdout/stderr 各保留 512 KiB 头尾，`exec_result.go`/`exec_renderer.go` 冻结最多 256 KiB 的模型预览；`tool/builtin/process_execution.go` 与 `process_progress.go` 管理管道和进程 owner。丢失的中间内容没有磁盘副本。`runtime/exclusive.go` 已提供审批、started、结果提交边界，`tool_scheduler.go` 已有只读并行与 exclusive 屏障。

`session/replay.go` 的 ToolRecovery 仅描述未完成尾部，不能当作全部历史命令。`app/session_service.go` 当前恢复 Provider 文本投影，`domain/history.go` 的 SemanticHistoryView 只有用户与助手文本。`tui/command_detail.go` 当前只打开最新命令的内存详情。因此仅给 Exec 加文件路径，不能解决重启查看、授权、分页与冻结恢复。

本次只读检查的参考源为 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap`（2.1.88）和 `/Users/cutejiuge/IdeaProjects/codex`（`6f8bf683c`），并非 worktree 的直接兄弟目录；实现不得依赖这些目录。

| 参考实现 | 观察到的实现 | 本方案取舍 |
| --- | --- | --- |
| Claude `utils/Shell.ts`、`utils/task/TaskOutput.ts`、`tools/BashTool/BashTool.tsx` | 普通 Bash 可把两个流写到同一文件；按需读尾部，大结果迁入 tool-results | 保留紧凑展示、按需详情；两个流分存，不伪造跨流总顺序 |
| Claude `utils/toolResultStorage.ts`、`query.ts` | 按工具/消息预算替换大结果，冻结替换文本；部分记录异步提交 | 保留预览与完整输出分离；EasyCode 必须 Sync 后确认，不能照搬异步持久化保证 |
| Claude `utils/task/diskOutput.ts`、`utils/cleanup.ts` | 异步磁盘写入、后台输出限制与按年龄清理 | 使用前台严格配额和 thread lease 清理；不自动删除仍被引用的文件 |
| Codex `codex-rs/core/src/unified_exec/process.rs`、`codex-rs/core/src/unified_exec/process_manager.rs` | pending/transcript 使用有界 HeadTailBuffer，轮询排空 pending | 保留内存有界与生命周期分层；所检查路径不保证完整日志落盘，不能把 raw_output 当成完整 OS 输出 |
| Codex `codex-rs/core/src/tools/context.rs`、`codex-rs/tui/src/exec_cell` | 模型输出与展示分离，稳定 call identity，最终状态替换实时状态 | 采用独立 presenter、稳定 invocation 与终态覆盖；不把轮询细节暴露到聊天主线 |

该能力直接修复“看到省略但无法继续查错”的断点，复用已具备的命令执行、双 Provider 和 durable 工具循环，并为后续工具预算提供真实存储消费者。后台进程需额外进程控制、stdin、跨 turn 生命周期；全工具预算还牵涉上下文替换与压缩，现阶段优先此闭环更容易独立验收。此切片不代表完成 Roadmap 全部工具结果预算或 P3。

## Goals / Non-Goals

**Goals:** 用户通过现有详情入口选择历史命令并分页查看保存内容；模型通过强类型 ReadArtifact 获得同一授权数据。执行事实、存储完整性、内存省略和显示折叠分别表达。恢复后的原生请求不依赖输出文件仍然存在。

**Non-Goals:** 不改变 Provider 原生历史作为续写事实的地位，不把 SemanticHistoryView 变成工具数据库，不扩展 workspace Read，不实现后台控制、PTY、通用文件浏览器、自动过期或全工具输出预算。具体范围见 proposal.md。

## Decisions

长期决策记录见 [ADR-0005：私有命令输出与独立宿主查询](../../../../docs/architecture/adr/0005-private-command-output-and-host-queries.md)。

### 1. 按数据职责组合，接口由消费者定义

- `internal/tool`：新增 artifact ID、descriptor、保存状态、ReadArtifact input/result 的不可变业务值；构造、strict decode、validator 封闭，不接受 any。descriptor 只含不透明 ID、所属 thread/invocation/stream、保存长度、观察计数、digest 和 complete/partial；绝对路径不出存储边界。
- `internal/tool/builtin`：Exec 采集端依赖小的输出保存接口，ReadArtifact executor 依赖范围读取接口；不得 import session。内存头尾、磁盘连续前缀、终端展示分别组合，禁止用会在首个磁盘错误后停止排空的 io.MultiWriter。
- `internal/session/artifact`：实现私有文件存储、配额、seal、验证范围读与 lease 下的清理；依赖 tool 值类型，不反向依赖 runtime/TUI。保存、范围读和清理分别实现，不做万能 Manager。
- `internal/session`：strict result records 与纯命令历史投影。投影从已验证 ready/approval/result 建立，历史结果不是 ToolRecovery 的副作用。
- `internal/runtime`：负责调用授权和 durable 顺序；typed terminal 携带保存状态和 descriptor，progress 不携带无限内容。
- `internal/app`：显式 Open 存储并装配接口，持有 thread 授权集合和宿主命令查询服务。`internal/protocol` 只定义 typed query/result 值，不引用 tool、文件系统或 session 实现；app 将 tool descriptor 映射为 protocol 的只读展示值，禁止 protocol 为复用结构反向 import tool。
- `internal/tui`：Update 发出异步查询命令，View 仅渲染快照。

采用组合、依赖倒置和单向读模型；不引入抽象基类、全局 registry、空 facade、跨层 reader。所有 New* 纯内存；Open/Start/Run 获取外部资源。相对于直接在 TUI 打开文件，该结构能使授权、完整性和同一范围读取行为被模型与 UI 共用，同时保留不同的呈现策略。

### 2. 从首字节暂存，保存连续前缀

Exec 准入后从两个管道首字节开始分别暂存原始 bytes，内存仍各 512 KiB 头尾。最终按现有 256 KiB preview 总预算判断：若两个流均确认 EOF 且全部能无省略表示则删除暂存，状态为 not_needed；否则发布非空流的暂存。保存状态为 complete、partial 或 unavailable，与 exit/timeout/cancelled 正交。确认 EOF 的无输出流为 not_needed；未确认 EOF 且没有可发布前缀时为 unavailable。小输出可完整内联时不因先前预留失败而显示丢失，仍为 not_needed。完整性还要求该流确认 EOF；被强制关闭的流不得声称完整。

每流最多保存连续前缀 16 MiB，单次最多 32 MiB。到上限或写入失败后停止该流保存，仍排空并计数；冻结头尾仍可能包含磁盘前缀之外的尾部，界面必须说明。保存失败但可安全 seal 的非空前缀为 partial；无法确认 Sync/发布则 unavailable，无可读引用。存储失败本身不把确定的非零 exit 改写成进程基础设施错误。

选择同步有界写入，接受磁盘背压，不增设无界异步队列。磁盘调用不能由 context 保证打断：终止进程与关闭管道由 owner 独立协调，但 reader 正阻塞写入时必须保留其资源、lease 和最终 done。shutdown 超时返回未清理状态，不释放 owner，不开始新 turn；阻塞解除后唯一 owner 完成清理。锁内只进行有界内存转换，不能持内存互斥锁做 I/O。

### 3. 明确配额与跨进程所有权

默认 thread 256 MiB、存储根 1 GiB，限制保存的原始数据 bytes；不宣称覆盖目录/日志等文件系统开销。数值是本方案可评审默认值，不是实测最优值；本次无配置字段，测试可注入更小的不可变预算。

在 root 专用跨进程 accounting lease 下，开始一次保存前全额预留 32 MiB；thread 或 root 不足则不预留且报告 quota_exceeded，不部分竞争预算。即使小命令也短期占用预留，这是确定性换利用率的取舍。reservation 有 invocation identity 和 owner lease，持有至最终资源清理。每 invocation 按 max(reservation, 实际 staging+published bytes) 收费，发布使用同文件 rename，不复制双计。既有已完成文件按实际 bytes 计费；进程崩溃释放 owner lease，下一次 accounting 校验将陈旧 reservation 调整为实存 bytes，保守保留无法验证的占用并拒绝新预留。

accounting lease 保护跨进程读改写事务，不是内存 mutex；期间仅访问安全打开的 artifact/reservation 文件，不读取 journal、不等待其他 thread lease。固定顺序为 thread lease → root accounting lease，禁止反向等待。reservation 创建/调整必须明确 Sync 边界和失败返回；无法确认时保守占用预算，不能超卖。按 thread 存储，拒绝 symlink、非普通文件、过宽权限和外来硬链接；对象发布后不可变。

已被 durable result 引用的对象无自动 TTL/LRU。取得该 thread lease、完成 journal 全量验证及尾部修复后，才删除该 thread 未被有效结果引用的 staging/孤儿对象。不得根据 age 删除其他 thread 文件，不因 root 满自动删除历史。启动失败或 journal 损坏时不清理对象。清理不推断工具成功，不修改已经提交的 result。

### 4. 发布不等于授权

存储位于 Session 根下的 artifacts 目录。app 在启动与每次 Exec 启动前，根据实际打开的目录身份和同一 SandboxPlan 的读写范围复核隔离。存储根与 workspace 相同或互为祖先、存储根位于其他 sandbox 可读或可写根内、身份无法确认或改变时，必须返回 `unsafe_storage_layout`，不静默禁用保存。路径逐级安全打开；存储路径拒绝 symlink，系统路径别名显式规范化并验证实际身份。目录祖先关系通过句柄验证，不能用字符串前缀替代；校验后继续使用已验证句柄。此边界不宣称抵御产品之外拥有同等 OS 权限的任意进程。

顺序固定为：started durable → 执行/采集 → 文件 Sync → no-replace 发布 → 目录 Sync → 冻结 descriptor 与模型 preview → result batch Sync → Provider output native commit Sync → 后续 sample。暂存和已发布但无 durable 引用的文件均不能被用户查询或 ReadArtifact 获取。descriptor 与结果一次冻结，磁盘状态不能在恢复时改写 preview。

文件成功发布而 result 写入失败形成孤儿；不能重执行命令。已有 result 缺少 native output 时恢复使用保存的 preview/code/typed metadata 补偿，不读 artifact。result/native 提交失败沿现有 poisoned 规则保留清理所有权。当前 thread 的授权集合仅由已验证 durable result descriptors 建立，活动提交成功后增量更新；调用方传来的 thread/path 不作为授权依据。

### 5. ReadArtifact 是独立只读能力

两家 Provider 都使用 structured `ReadArtifact`，capability 为 `artifact.read`。输入仅 `artifact_id`、`offset`（默认 0）和 `limit`（默认/最大 65,536 raw bytes，最小 1）；严格拒绝 null、未知/重复字段、非整数、负数、溢出、尾随 JSON。不存在 filepath 参数，不暴露跨 thread 读取，不授予 workspace 路径能力，不生成 Write ReadObservation。合法授权调用走 parallel_read，完整 ready durable 后才允许 I/O，依旧遵守 ledger 和 ordered output。

读取时从安全根逐级 open，依据同一个 FD 验证类型、权限、长度与 digest。首版每页顺序校验整个对象（最多 16 MiB），只缓存所需范围，digest 不符不返回正文；不引入可过期的全局验证缓存。offset 超过保存长度返回 invalid_range，等于长度返回空页 EOF。offset 位于有效 UTF-8 字符内部返回 invalid_range；结束处回退到完整字符；如果 limit 无法容纳下一个完整字符，返回 invalid_range 和最小所需 limit，禁止零进度循环。原始无效 bytes 用 `\xHH` 展示；next_offset 按实际消费原始 bytes 计算。稳定 metadata+转义正文最多 256 KiB，必要时提前结束页并返回真实 next_offset。stored EOF 与 source_complete 独立，partial 的 EOF 不表示原命令没有更多输出。

安全英文错误区分 unavailable、corrupt、invalid_range 和 unauthorized；不存在与跨 thread ID 对外统一 unauthorized，不泄漏存在性。读取结果本身不再 offload，失败正文也不得进入诊断。用户允许的命令输出可能含用户数据，按私有 Session 数据处理；系统不得注入 Provider/config secrets，不能声称可识别任意命令自行输出的所有秘密。

### 6. 用户操作沿用紧凑主线与按需详情

主线仍为命令摘要、Waiting/Running、近期输出、exit/耗时/规模；保存状态用“已保存”“仅保存前段”“未保存”表达，不显示账本、内部路径或 invocation ID。省略标记分别说明预览省略和保存缺失。

Ctrl+O 打开命令列表并默认选择最新项；↑/↓ 选择，Enter 进入详情。详情保留完整命令、冻结预览、stdout/stderr 保存状态，Tab 切换流，PgUp/PgDn 按页查看保存内容；首屏可立即显示内存预览并异步加载首个保存页。Esc 从详情返回列表，再次 Esc 关闭；Ctrl+O 在覆盖层中直接关闭。Ctrl+C 仍定向取消活动命令，其他输入不泄漏到 composer。运行中的命令只有有界近期输出，未提交文件不开放读取。审批覆盖层优先级保持原契约。

宿主提供 typed ListCommands 与 ReadCommandOutput 查询，绑定当前 thread 的实例授权；列表每页最多 50 行，以稳定 seq/invocation 游标查询，两个流各有原始 offset。最多缓存 4 个输出页（每页不超过 256 KiB 展示文本），列表只保留当前页。查询带 selection generation，切换命令、流、关闭或换 session 后取消旧请求并丢弃迟到回复；宿主 owner 等待请求结束，TUI 不创建无归属 goroutine。resize 和 View 不发起磁盘 I/O，翻页由 Update 的 Cmd 查询。查询不调用 Provider、不写工具 ledger、不改变 native history。

历史命令投影只保留 metadata，不加载输出正文。恢复全部已完成/拒绝/中断命令，按 seq 排序；已有文本历史保持原行为，通过 Ctrl+O 访问独立命令列表，避免伪造工具文本注入 Provider 历史。文件缺失或损坏只影响查看，展示可读错误与原冻结预览，仍能继续会话。

### 7. 保持当前契约与可回归缓存

当前封闭 input/result 联合一次加入 ReadArtifact 和 Exec 保存字段，将 Session 当前 envelope canary 从 3 更新为 4，并同步受影响的当前 payload canary 与所有固定 current fixtures；不加 V2、optional legacy reader 或历史迁移代码。ReadArtifact definition 只包含稳定描述和 schema；artifact ID、quota 使用量、目录、时间不能进入 catalog fingerprint。同一 result 的即时与恢复 preview、native item 顺序、canonical request 和 fingerprint 必须等价。Provider-native history 中已有 reasoning/encrypted/signature/phase 完整保留，不通过 command history 重建请求。headless JSONL 外部 v1 不增加 progress 或内部 descriptor 事件。

## Risks / Trade-offs

- [前台落盘增加延迟，磁盘写入可阻塞] → 有界同步写入、独立取消协调、确定性阻塞注入与 done 检查；不承诺立即释放资源。
- [全额预留降低剩余空间利用率] → 明确 quota_exceeded，实际完成后释放余量；本次不引入复杂增量竞争。
- [逐页完整校验消耗 I/O] → 单流 16 MiB 上限、只保留页内容；后续优化必须保持同等完整性，不先做缓存。
- [保留策略使根目录最终写满] → 明确状态并继续有界预览；不静默删历史，不承诺无限保存。
- [原始字节可能含无效 UTF-8 或控制序列] → 保存原始 bytes，模型有界转义，TUI 再做安全显示；测试页边界、超长行与终端控制字符。
- [本机同用户恶意进程可破坏文件] → descriptor-bound 校验与 digest 检测，不声称抵御拥有同等 OS 权限的任意攻击者。
- [双流原始交错无法重建] → 按流查看和固定 stdout→stderr 模型预览，不展示虚构跨流顺序。

## Migration Plan

先实现并验证强类型与存储边界，再接 Exec/ReadArtifact、durable loop、宿主与 TUI，最后统一更新当前 fixtures、golden、架构与 Roadmap。所有能力同一 change 内接入真实消费者，通过质量门后才声明完成。现有开发期 journal 不兼容新 canary，启动在 repair/append 前失败；不原地改写历史。回滚源码时必须使用匹配旧 canary 的独立开发数据，不允许旧程序打开新 journal。不新增 SQLite schema 或构建依赖。
