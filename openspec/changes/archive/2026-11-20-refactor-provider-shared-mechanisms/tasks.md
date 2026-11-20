## 1. 锁定等价基线

- [x] 1.1 记录当前生产源码基线及现有验证入口，核对双 Provider 固定 Session、请求 golden、TUI snapshot、headless 帧、race 清单和实际平台。将准备复核的字节、事件顺序、错误分类和副作用计数列入验收基线。
- [x] 1.2 为长会话多组工具结果回放和重复工具目录查找记录耗时、分配基线。保留代表性的正常及损坏输入，原始测量输出存放于忽略的本地目录或 CI artifact。

## 2. 统一装配与迁移上层调用

- [x] 2.1 在 app 的一个装配边界按经验证的 family/wire 绑定在线工厂、离线原生读取器、配置值和当前工具视图。验证启动、继续、恢复、聊天内切换、异 Provider 无凭据预览以及非法身份的现有错误。
- [x] 2.2 将 Runtime 和宿主所需的强类型采样、输入、Stop、工具、压缩、投影和恢复操作在装配时绑定到各自消费者。覆盖两家实际实现、可选能力缺省时的触发时机与错误，以及 OpenAI fallback 后动态续写能力的当前状态。
- [x] 2.3 迁移 Context、Runtime 缓存、config、protocol 和 TUI 中的身份与查询逻辑。验证使用实际 wire、启用窗口时的输出上限、允许的 effort 档位、显式设定和缺省值、能力查询与文案均符合现有契约。用非法 family/wire 与非法 effort 值确认封闭校验。
- [x] 2.4 统一工具目录对 facade 身份、能力、wire kind 和结构化声明的查询，供活跃请求和无执行器的离线恢复共用。用两家的工具名称、schema canonical bytes、调用错误和目录 fingerprint golden 核对，包括 Patch、Question、Task 的专用形状。

## 3. 收敛原生窗口与准备流程

- [x] 3.1 共享强类型原生历史的存储、append、checkpoint 安装与窗口读取。保持各自的 native entry 和 codec，用双 Provider 的 `TestCheckpointWindowAndRestoredRequest` 校验 offset、revision、摘要、完整历史与有效窗口。
- [x] 3.2 共享准备和提交的公共代码，并复用现有的一次性 resolution。用 finalize/discard 与传输清理测试核对既有准入时点、回调顺序、失败后状态和资源所有权。
- [x] 3.3 收敛普通用户输入、steering 和 Stop 反馈的候选准备与预检，保留纯编码检查、占用 pending 的准备、反馈单独交付及双条目提交的边界。用双 Provider steering、Stop 续写、输入额度、取消和 journal 批次回归核对。
- [x] 3.4 共享工具输出预检、待闭合调用定位和批次时序。用已知合法及非法配对验证 call ID、wire kind、capability、数量、顺序、冻结结果和现有错误，覆盖 Question 与 Task 的独立证据。

## 4. 共享请求、采样与摘要执行骨架

- [x] 4.1 让预检和实际发送复用共享的请求编译与证据流程；以当前操作冻结的推理设置、工具视图、项目指令、Skill、窗口和请求用途确定输入。用现有请求 golden 核对重新预检、canonical body、属性和各类 fingerprint。
- [x] 4.2 用小策略组合共享普通采样的流启动、帧消费、事件发布、终态和资源清理。对比两家完整事件序列和失败分类，覆盖 reasoning、工具调用、idle timeout、EOF、取消、拒绝和异常帧，并验证部分归并结果不进入 durable 历史。
- [x] 4.3 让普通采样与独立摘要复用流生命周期机制，各自保留用途、原生终态与提交入口。用手动/自动压缩、PreCompact 修订后重新预检、无工具调用、无缩减、Sync 失败和无效摘要回归核对 checkpoint 与用量归属。
- [x] 4.4 在实际 transport 边界核对 Anthropic marker/TTL 和 OpenAI cache key、HTTP/WebSocket full/incremental、生成前 fallback、既有不重发规则及摘要独立连接。在 durable finalize、discard 和既有取消路径比对基线与下一请求。

## 5. 共享原生回放、投影与热点计算

- [x] 5.1 收敛有序回放及 checkpoint 遍历，并复用存档历史的只读投影流程。保持 native 解码、投影白名单和证据校验各自的语义。验证合法 history/transcript/footprint、旧窗口仍可读、checkpoint 覆盖范围内的损坏仍被拒绝。
- [x] 5.2 将现有样本和摘要用量校验绑定到选定的恢复策略。核对 Anthropic 样本对账、OpenAI 当前可读日志、两家摘要计量、各入口的错误优先顺序和取消语义。用半行、损坏及忙锁用例核对离线只读、在线 repair/lease 和无外部副作用。
- [x] 5.3 将长会话回放中反复扫描历史前缀的工具时序检查提取为逐条推进的回放状态。核对合法记录的投影、原生顺序和后续请求，核对多处损坏时的拒绝与首个错误，再用同一基线用例比较耗时和分配。
- [x] 5.4 在构建工具目录快照时预计算并冻结所需的按 Provider 视图与查找索引。用真实消费者验证只读与深拷贝边界、零值/非法快照拒绝、canonical bytes 和 fingerprint，并用同一重复查找基线比较分配与耗时。

## 6. 处理审查确认的等价技术债

- [x] 6.1 统一 Session draft 与读盘记录相同的 27 种 typed decoder 分派。用固定 current JSONL 及损坏或错位记录核对 draft 特有的父线程约束、严格校验、首个错误与实际编码字节。
- [x] 6.2 合并 JournalWriter 正常消费与关闭排空的请求处理，保留现有接纳、seq、Sync、poison 和 lease 行为。复用 `TestJournalWriterCancelledCloseStillDrainsAndCloses`、持有 lease 的最终 Sync 与既有故障注入用例。
- [x] 6.3 抽取 Patch、Write、Exec 共用的审批事实、拒绝结果落盘、执行后通知和预算处理。用各工具的代表性取消、拒绝、用户授权和 Sync 失败测试保留各自的候选、来源、结果分类、错误文案和顺序。
- [x] 6.4 复用 Runtime Cache/Context 查询共同的活动状态与结果发布，保留各自的读模型和计算。对比查询返回值、命令回包次序、活动标记和实际 cache observation。
- [x] 6.5 收敛 Runtime Hook 运行器调用中相同的时限、用时结算和身份校验。复用既有授权、关闭与 Sync 故障注入用例，核对 PreToolUse、PostToolUse 及失败分支、Stop、PreCompact 各自的信任、事件、持久化和反馈边界。
- [x] 6.6 共享 Hook 输出解析的限额与同义退出分类和空结果处理。用现有 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact 用例核对退出码、UTF-8、错误优先级及反馈授权。
- [x] 6.7 复用 UUIDv7 生成、解析和格式化算法。用确定性输入和当前固定 Session 身份核对类型、文本、时间位、非法输入与既有错误。
- [x] 6.8 收敛 Cache、Context、Effort、Compaction 协议事件的共同装配，并保留各自的 typed constructor、strict decoder 与 validator。以同一合法或非法输入比对 payload、scope、校验顺序与错误。
- [x] 6.9 共用 TUI 的 Pre/Post transcript 缓存失效和列表刷新，将相关输入、焦点和显示状态交给现有子状态负责。用焦点、滚动、迟到回包、paste、审批和 snapshot 回归核对原场景。
- [x] 6.10 共享 headless 两套 JSONL encoder 的 canonical framing 和完整写入机制。用固定 v1 fixture、短写、已有取消和错误路径核对完整帧、错误文案及报告边界。
- [x] 6.11 按已有职责收敛 app 的资源组装、Runtime 的工具执行状态和 TUI 的投影子状态。复用 handoff、shutdown 和租约测试核对既有 owner、状态归属、释放顺序和 goroutine 生命周期。
- [x] 6.12 核对并清理确无消费者的执行结果字段、进度临时赋值和 getter，将已有测试专用辅助代码放回测试作用域。通过引用扫描、构建及 Staticcheck 复核，保留当前契约需要的构造与解码入口。

## 7. 等价验收与文档

- [x] 7.1 用架构检查校验共享流程不按具体 Provider 家族选策略，宿主对已绑定能力走强类型入口。覆盖允许的身份一致性校验、装配选择和违规的选择分支，以两家实际实现及代表性兼容配置验证共同流程。
- [x] 7.2 使用双 Provider 的固定原生样本与 live/restored 端到端用例比对原生 item 顺序、opaque 内容、canonical 请求字节、缓存指纹、摘要、工具配对、日志字节、失败分类和副作用计数。
- [x] 7.3 用现有 fault injection、关键 race、跨进程 lease 和代表性的 PTY/CLI 用例验证取消、清理、审批、Hook、焦点、流式输出和 headless 帧边界。按仓库规则审查 race 入口。
- [x] 7.4 执行 `make verify`、本 change 的 OpenSpec strict 和 `git diff --check`。记录实际运行的平台、性能对比和生产 Go 净行数，说明未验证项及差异。行为差异须在当前范围内恢复等价，行为修复进入独立评审。
- [x] 7.5 对照验收后的实现更新总体架构、ADR 0002 的当期说明及适用的 Roadmap 和踩坑索引。核对主规格与实际代码一致后进入同步和归档评审。
