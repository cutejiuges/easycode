## Context

[提案](proposal.md)界定六批范围。基线生产源码为 `0f700a0`。#48 已有泛型原生窗口、准备、回放和投影骨架。本变更在这些边界上继续收敛。原生历史是请求和恢复事实源，语义视图、transcript 和 Catalog 都从已验证事实派生。

## Goals / Non-Goals

**Goals**

- 让准入、提交、取消和清理各有可检查的线性化点。重复的校验、投影走一致的强类型路径。
- 每批结束时仓库可单独构建、验证与回退。评审能区分预期行为差异和非预期回归。

**Non-Goals**

- 无 Session 格式版本、历史 reader、迁移器或兼容双路径。非法日志按现行格式直接拒绝。
- 不强行合并 Provider 专属 wire、closed enum switch、不同补全排序及 headless 版本 DTO。不以行数目标或通用框架驱动拆分。

## Decisions

### 分批依赖和验证边界

| 批次 | 依赖与阶段交付 |
| --- | --- |
| 1 普通 sample 计量 | 从基线独立交付。目前保留 App 的验证阶段，第 4 批再消除重复原生解码。 |
| 2 Provider 所有权 | 提供一致的已提交窗口、明确的操作接纳和清理边界。第 4、5、6 批复用这些边界。 |
| 3 工具审批与暂存 | 在第 2 批之后实施。工具候选与 durable 批次维持既有协议，给第 4 批提供稳定审批事实。 |
| 4 Session 回放与 Catalog | 消费第 1 批的计量证据和第 3 批的审批事实，产出第 5 批可复用的有序回放投影。 |
| 5 宿主投影 | 使用第 2 批的原生快照和第 4 批的已验证恢复前缀，叠加第 3 批发布的关联审批事件。 |
| 6 剩余去重 | 使用已稳定的窗口、工具事实和发布接口，保持前五批确认的行为和错误分类。 |

每批先固定对照提交、给定输入和可观察结果，再以小步变更通过该批场景。允许的差异由所属 delta 列出，其他请求、持久化、顺序及错误按该批开始前的状态对照。每批自审并独立 review，执行本批场景、现有相关回归、`make verify`、OpenSpec strict 与 diff 核对。提交简短验收结论并等待用户确认，通过确认才勾选本批的验收项并接纳下一批。发现设计约束无法满足时在同一 change 更新设计并重新确认。

差异证据用固定 fixture、隔离目录、真实 CLI/PTY 或本地 SSE/WebSocket stub。需要 package 私有同步点时，可用仓库外的 Go `-overlay` 或已有 fault hook 运行一次性场景。锁、通道、可控 reader 或进程握手提供接纳和失败信号，测试不靠 sleep 或随机竞态。常规质量门使用真实工作树。修改 Provider、Context、目录或缓存时补做临时 golden/cache 对照，复用现行固定 fixture，记录 uninterrupted 与 restored canonical 请求字节、原生顺序和 fingerprint。仅当随机会话身份及线程派生 key 无法固定时，显式列出隔离的字段并独立验证其派生结果。关键链路可保留最小回归测试。临时场景、生成日志、原始 JSONL 和 profile 留在临时目录或忽略的本地输出，规格只记录可复现要点和验收结论。

### 早间 Review 条目与当前基线

以下编号对应以 `f6998f6` 为基线的 22 条发现。当前基线已包含 #48。

- #2 的 UUIDv7 算法、#5 的原生窗口和准备机制、#6 的离线原生回放、#8 的 facade 映射、#11 的宿主 wire 派生已共享。#22 的准备对象也已委托一次性 resolution。第 2、4 批沿用这些骨架处理剩余的所有权和重复消费问题。
- #1 仍影响 OpenAI 普通 sample 恢复，进入第 1 批。#7 的 checkpoint Task 证据校验随第 4 批共用已解码证据。#3 的 `UsageMetric.Value()` 返回标量副本，当前 `&value` 指向本地副本，计量转换的实际重复与 #9、#10 一起进入第 6 批。
- #4、#18 的搜索预算和草稿重验进入第 6 批。#15 的装配按真实能力绑定拆分，#16 只拆本计划触及且已有明确职责的流程。#17 以当前消费者重新确认删除范围。#19 的两种 96 KiB 上限分别约束 Runtime payload 与带换行的 headless v1 帧，保留各自上限并明确命名。
- #12 保留独立 Provider 缓存策略，第 6 批核对实际请求和缓存证据的派生。#13 的文件搜索与补全排序、#14 的闭合 kind 分派、#20 的共享严格解码遍历、#21 的独立 wire 映射保留现有实现边界。#14 中已经验证的重复事件处理和响应校验进入第 5 批。

### 1. 使用已绑定的双 Provider 计量验证

`internal/app/provider_binding.go` 的基线只为 Anthropic 填充 sample validator。在线恢复和离线预览统一绑定必需的计量验证能力。OpenAI reader 复用现行 `rawUsageCommit`、strict decoder 和原生归一化规则，只读该样本的 `kind` 与 `usage`。其他原生条目内容和时序仍在原有回放阶段核对，以保留非计量错误的报告顺序。共享 App 路径核对 turn、相邻 seq，再委托适配器比较五项领域值。Adapter 独占 raw wire，Session 保持通用 shape 和批次 grammar。保留 lease、兼容性、通用结构校验、对账、原生回放、repair 的现有阶段。OpenAI 新增的计量拒绝在对账阶段以既有错误分类报告。

这一批使用现有校验入口，避免同时替换回放流程。临时日志从合法副本生成一对 checksum 均有效但计量不同的证据，分别验证数值和状态不一致，并附半行以核对首次可见写入。正常例复用双 Provider 的已知零、未知和工具续写。当前两家普通样本的归一化都将缺失值映射为 `unknown`，额外验证这种证据不接受 `not_applicable`。固定 golden 与 journal vocabulary 不变。

### 2. 统一 Conversation 准入、窗口和 terminal 发布

将 `native_prepare.go`、`sample_stream.go`、`compaction_workflow.go` 的分离 active/pending 检查收敛为会话独占状态。接纳只做有界内存转换，活动流、待提交和解析清理的交接由同一所有权边界管理。分开追踪实际流清理和 prepared resolution 两项完成条件，任一仍悬挂便拒绝新操作。统一管理 OpenAI socket、response ID 和 reset 的使用时机，资源释放在锁外由接纳方执行，Provider 级强制关闭路径继续可解除阻塞。普通采样与压缩保持各自终态和流策略。

在开请求、摘要预检和确认来源时使用一个 `NativeWindow` 值，含 entries、revision、checkpoint、offset 和 count。候选只从同一已提交版本派生，真正的资源 I/O 和深复制在锁外完成。纯预检不占 pending。准备对象继续通过既有一次性 resolver 完成 durable Sync 后 finalize 或未提交 discard。下一操作必须等待 OpenAI discard 完成连接清理和瞬态字段释放。

普通发布使用有限队列，为 terminal 保留确定可用的容量。内部 `StreamEvents` 使用有序迭代，消费方取出一条非终态时由迭代器归还一个容量配额。非终态发送可由 context 解除，停读时 owner 仍能排空并退出。确定合法终态选定和 durable batch 接纳是两个独立时点，Runtime 持续迭代到内部队列由生产者关闭，并保留已观察推理范围的合法边界。普通 Responses HTTP 在候选完成帧处确定 transport 已读入的范围，按序把其中完整分隔的尾帧交给 reducer 核对，无需等待远端 EOF。WebSocket 保留原 response identity、成功才可复用和请求已发出后不回退重放的语义。

### 3. 复用审批终态与暂存文件所有权

`runtime/approval.go` 用单一审批终态记录绑定请求、冻结 decision/source/feedback 和是否发布请求。AgentLoop 收到 Stop、interrupt 或 shutdown 后关闭该 turn 对审批响应的接纳。审批等待的取消与显式响应争夺同一终态。选项以活动请求的有序集合为准，Patch/Write 继续只提供当前选项，Exec 的显式 Abort 可被正确归属。沿用已有 approval fact 和结果持久化入口，共享生成及发布决策逻辑。只有真实发布过的请求才发关联 resolved。保持各能力候选创建、英文文案、Read 证据、Hook 前置和 grant 范围。已接纳响应产生的批准事实仍独立于执行接收，执行只有在 started 成功 Sync 后才可调用一次。

从 `tool/builtin/write_staging_unix.go` 提取同语义的 descriptor-relative 暂存所有权部分供 Patch 使用。创建、文件描述符读写/权限核验、发布前重验、父目录 Sync 和清理按原提交点排序。检查暂存目录项的实际文件身份和父目录所有权，发布和清理使用受控目录句柄。保留 Patch 已有文件替换、Write 的 no-replace 和目录链发布、两者已确认发布后的既有结果分类。不能证明清理结果时保留不确定状态。路径检查加 rename 本身不承诺在任意外部并发修改下提供原子身份比较，验收按受控边界的实际保证进行。遇到不具备等价原子或所有权语义的平台继续失败关闭。

### 4. 复用 Session 已验证事实，硬化 writer 和 Catalog

在 `session/replay.go` 已有严格 grammar 归约时产出按 seq 有序的工具、Hook、结果与 artifact 证据。后续校验和投影消费强类型事实，保持原先 grammar、artifact 完整性和工具投影错误的报告顺序。Catalog 使用同一校验结果投影元数据，省去它不消费的历史展示物化。Live Runtime 的选参从 origin、invocation 和经核验 Hook facts 派生，不用合成 ReplayedToolCall 取得业务输入。

恢复记录携带实际 result seq 对应的任务状态，按有序事实一次归约，并在压缩 started seq 冻结来源。适配器的当前 checkpoint decoder 产出原生替换摘要、任务快照和计量 provenance，供校验、安装和展示复用。若某项证据提前解析，按原业务阶段报告解析错误。区分在线与离线原本的计量、回放和投影错误顺序。贯通 `app/session_service.go` 到两个 Factory 的取消，在记录间检查并保持恢复阶段的取消分类。验证完毕才允许 repair 和 writer 接管，lease 连续持有到最终清理。

`JournalWriter` 区分首个写调用前的确定失败与 Write、短写、Sync 后无法确认的结果。编码和大小拒绝返回未写入，保持 seq 和可写状态。后者沿用现有 mutation-uncertain 分类并 poison。Runtime 还要独立依据先前已提交的 started 或其他已接纳事实决定是否结束当前操作，不把 writer 健康等同可安全重试副作用。

Catalog 继续采用 Codex 的派生索引与 JSONL 恢复分离。当前每次协调已经严格回放全部可取得 lease 的 journal，持久化主要用于保留 busy journal 的上次已验证元数据和支持快照查询。官方 SQLite 驱动的命名 VFS 仍需进程级分派，且不能通过公开 API 复用已验证文件句柄的原生锁、同步和事务恢复。用户允许移除 SQLite。Catalog 使用单一私有 `catalog.json`（JSON 数组）保存已验证的强类型元数据，保留稳定排序及现有 CLI/TUI 的筛选与恢复契约。用既有 codec 严格校验单行元数据，允许 Catalog 依赖 codec。缺失或无效快照从 JSONL 重建，历史 SQLite 内容不参与读取、更新或迁移。

持有私有数据目录和 Catalog 跨进程锁的实际句柄，读取快照前核验同一描述符的类型、归属及权限。只在同一目录句柄中创建独占暂存文件，写完并 Sync，复核暂存及目标身份后原子替换。首次发布使用原子 no-replace，目录 Sync 成功后才更新实例可查询快照。已有目标的复核与替换受私有目录和协作锁约束，系统调用不提供通用的原子身份比较。拒绝不安全对象、身份错配或持久化失败，取消在发布前保留旧快照，进入原子替换后返回确定的同步结果。无效元数据整份丢弃，已有已验证 busy 行在新快照中保留，未索引 busy 继续抑制自动选择。按行流式解码、只在快照变化时写入，避免重写等价数据。原生 macOS/Linux 场景验证实际句柄、替换前后字节、进程竞争、崩溃后恢复、CLI 及 PTY；暂时不可执行的平台原生场景须明确记录。

### 5. 按提交后缀更新 transcript，统一宿主入口

`app/transcript_projection.go` 初始化时全量关联一次。运行中从已 finalize 的 native ordinal 读取只含此次提交的只读投影，使用 journal seq 与 item 序号关联现有 ready 和工具索引。不可见 outputs 仍推进 ordinal，Stop 反馈可推进对应 sample 水位，单批反馈和真实输入一起发布。checkpoint 以原 terminal seq 加入。预先验证新增 facts，再在短锁内增量更新条目和索引。只复制查询需要的有界页。Live Hook/result 更新定位已有工具项，不重扫或重排既有原生历史。headless 继续执行同样的事实和锚定检查，避免展示路径去重造成接受范围漂移。

TUI 用共享的焦点判定供 View、按键及补全使用。独占交互保留自己的确认、拒绝和取消键，底层只读面板保留草稿、列表位置和详情状态。普通只读面板的 Ctrl+C 交给活动操作取消入口。将续行和显式换行先解析为 composer 编辑意图，随后只对提交意图执行补全或 slash 分派。内部控制事件进入同一个 reducer，外层只运行一次补全、Markdown 和 hydration 后处理。

headless 保留唯一 transport owner 直到最后一帧写入完成，取消时关闭真实 stdout 解除阻塞，等待 Session 与 Runtime 的原清理 owner 结束。沿用输出不可用错误和健康 stdout 的单帧顺序。集中 v1 命令映射与 disposition 校验，保持外部 DTO 独立版本和各命令的严格解码。用确定性的 Write 接收信号、实际 pipe 和请求关联检查验证，也验证 EOF drain、显式 shutdown 和一次性 JSON 的原词汇。

### 6. 在已稳定边界内收敛剩余重复

复用各适配器已有的强类型原生遍历组织请求展开、footprint 和压缩来源，不把 SemanticHistoryView 作为编译输入。输入按原文、Skill 和文件顺序派生共享块及 metadata，适配器负责 `text` 或 `input_text` 等原生编码。Task/Question 共用已验证调用与结果比对，仍要求原 call ID、facade、wire kind 和有序配对。OpenAI function/custom 共享 item、call ID 和索引的归并状态，各自保留参数解码、限额、错误映射和原生字段。

让不可变 CanonicalJSON 在内部比较字节，复用一次请求基线比较。OpenAI 用已冻结的 Responses 计划构造 HTTP/full 和 WebSocket suffix，完整逻辑请求和实际传输仍分别用于正确收据。Anthropic 直接从编译计划获取 marker 位置和数量，保持消息边界、TTL 与 fingerprint。用同一窗口生成 Context view、footprint 和 revision。

由一份强类型能力绑定同时派生在线 facade 与执行依赖，保持离线身份声明纯内存。进程 Handle/Clock/Timer 复用已有 process 契约，Exec 与 Hook 仍有各自输出、信任及 sandbox。Turn 与手动压缩共用有唯一退出责任的 worker 启停及发布逻辑。三态 usage 与 optional uint 收敛纯转换，保留 Protocol、Session 和外部 v1 各自 DTO。文件搜索复用字节开销公式，文件准备在同一次受控操作传递已验证引用，保留独立接纳边界的校验。Hook 配置沿用 codec 对 null 的拒绝，删除后置重复解析。最后移除已证实无生产或测试消费者的字段、转发器或 getter，值字段直接赋值，并复核 build tags、现行强类型构造和批准的占位。

### 第 6 批复审发现与修正

工具 started 使用同一接纳判定。确定在接纳前取消或超时的调用不执行，按原 slot 回写取消结果。有 durable allow、无 started 的 Patch、Write、Exec 共用同一回放判定。有序调度在当前 turn 正文预算已用尽时向未准入调用返回有界预算错误，回放只在从前序结果重建的同一预算已耗尽时接受未启动只读调用的该结果。若准入因其他写入错误终止且尚有未关闭的工具组，Runtime 保留可恢复尾部，结束当前运行会话，避免提前写 `turn_failed`。恢复继续使用现有本地补偿与原生配对，不增加 record 或 reader。Patch 与 Write 共用适用的暂存文件写入、长度校验和同步步骤，保留替换、创建和所有权策略。rename 后取得成功的目录 Sync 即确定已提交，后到取消不改变该结果。

选择器初次加载和显式刷新在同一次已验证 Catalog 协调中取得本目录、全部目录两份有界列表及同一报告。所有资源清理成功后发布这一组结果。切换范围只读该组数据，重新打开选择器时失效。查询按范围和当前源 thread 筛选，再计算 200 条限制和 `HasMore`。

普通 Provider 流向 Runtime 提供 semantic 与终态，原生 item 保留于各自 reducer，随 prepared sample 在 durable 点提交。压缩内部仍能辨识原生项。参数和已发现的同类文本分片用可增长缓冲，维持字节上限、UTF-8／推理偏移、done 核对和原生顺序。OpenAI HTTP 与 WebSocket 共用 typed 请求字段及冻结计划，保留各自 wire envelope。在线恢复和离线预览从同一个 `StoredHistoryReader` 验证普通及压缩 usage。通过现有原生窗口投影代替重复 wrapper 和仅测试使用的入口。

保留进程内查询的实际校验和外部版本化 DTO，移除无调用的构造、decoder、字段及端口方法。输入正文与总字节检查在一次校验中各做一次，附件合并时保留静态引用边界与正式接纳复核。Protocol、Session、headless 共用 usage 领域字段转换，现有 wire shape 仍独立。各能力的目录声明和执行组件在同一强类型绑定中装配，执行路径仍可使用已有 typed 分派。合并字段映射时保持公开 v1 字段、验证顺序及 canonical 样本。

### 回归验收

两个 journal 缺口用真实 writer、Loader 与 replay 场景覆盖，验证副作用次数、已提交事实、恢复及续写。保留关键回归，其他边界使用临时 fault hook 或真实 CLI 和 PTY。双 Provider 请求、原生顺序与 fingerprint 对照第 6 批复审前的编译产物。复用原有双 Provider smoke、WebSocket、Hook、任务、选择器和 headless 流控回归。

### 第 6 批再次复审

headless 将已解码命令的可预期准备拒绝提交给同一命令 owner，复用请求关联、去重和有序 stdout。拒绝不分配 input ID，不启动 turn，不泄漏输入正文。解码错误、transport 取消和失去 owner 继续走各自终态。选择器将已显示快照与在途操作分开，同范围刷新失败后仍能预览和选择该快照，预览与实际恢复保持独立 lease 校验。当前范围未有快照时继续禁止使用其他范围结果。

Responses 的共享归并校验已知输出项的 ID、提供时的 index 和完成状态。已观察到的普通消息开始项必须在成功终态前唯一闭合，只有完成项的现行输入仍可按合法身份归并。已提交的当前原生样本同样拒绝冲突已知身份。统一按严格解码的 `error` 类型结束采样或摘要，允许它在 created 前报告 Provider 失败，保持脱敏与 WebSocket 已发送后不重试。按同一原生窗口捕获活跃段、摘要和计数，再复制需要的条目。完整历史查询与回放使用原有事实。删除新解码且尚未共享的冗余克隆，保留对外值隔离。Anthropic 的两条原生计量路径共享字段解码，OpenAI 各项折算规则独立。

Catalog 用 seq 定位最后提交事实，并原样使用该记录的 UTC 时间作跨线程排序键。墙钟回拨后，`updated_at` 可以早于 `created_at`，仍可投影、列出与恢复；不改 journal 或格式。连续只读组的 `started` 和结果次序在读取时按同一组规则校验，活动尾部、闭合组与元数据回放一致。独占失败仅在前序调用已收口时追加后续取消结果，未收口时保留原错误及可回放尾部。无审批能力的 Task 在自定义策略要求询问时可产生严格关联的执行前拒绝，不落审批或 started，恢复接受这一合法终态。

TUI 和 Skill 的本地命令识别只扫描首词，命令认出后再读取有界参数。原文继续交给正常正文与引用流程。删除无用 estimate、结果包装、独立答案 DTO decoder。生产保留 AgentLoop 回合入口，直接驱动 Runtime 的辅助移至测试。专项用编译版 CLI 和真实 PTY 复现旧问题及确认修复。复用双 Provider 完整回归、最小关键断言与仓库外故障和分配验证。

## Risks / Trade-offs

- 原生、持久化和缓存语义容易因合并边界变动而漂移。各批逐字节核对当前固定数据和合法实时/恢复请求，额外记录本批显式允许的差异。
- 改变校验阶段会改变多个坏证据并存时的首错。用合法 checksum 的组合错误、尾部半行及取消前缀验证错误和写入顺序。
- Provider、审批与 stdout 的背压容易保留无人清理的操作。验证控制信号、唯一终态、owner Done 及实际 lease 重获，不用延时循环作为成功证据。
- 原生 IO 方案需在实际受支持平台验证。跨编译仅作为编译证据，无法执行的原生检查记录缺口和影响，未经确认不得作为已通过交接。
- 全 CLI 长历史耗时也包含正常增长的请求。分别记录回放解码次数、每次 transcript 发布考察的条目数及同配置前后分配，避免从总墙钟推导缺失的复杂度证据。

## Migration Plan

无需数据迁移。每批在既有公开入口下原子切换同批接口与消费者，可以按该批 diff 复原源码。拒绝历史计量矛盾记录不改写记录。Catalog 仍可由 JSONL 重建。全部批次经确认后，核对实际词汇和文档，按现有流程同步主规格并另行归档。
