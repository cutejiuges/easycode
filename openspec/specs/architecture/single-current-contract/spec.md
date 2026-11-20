# Single Current Contract Specification

## Purpose

定义 EasyCode 在产品发布前对内部协议、持久化格式与外部机器协议的单一当前契约，防止开发期演进累积成 V1/V2 并存的业务模型、decoder 和迁移分支。

## Requirements

### Requirement: Every boundary has one current executable contract

每个 Session record、Provider native envelope、Tool input/result、Runtime command/event、Context plan、缓存计划和 Catalog 快照 SHALL 只有一套当前 typed model、constructor、validator、encoder 与 decoder。生产代码 MUST NOT 通过版本号、后缀类型、legacy DTO、fallback decoder、migration graph 或 feature flag 在同一二进制中选择多套历史业务语义。

未经用户明确要求 MUST NOT 主动设计多版本兼容、历史 reader、迁移器或为未来兼容增加内部版本门禁；发布状态本身不构成授权。纯内存值和同进程命令在没有跨版本交换需求时 MUST NOT 携带固定格式版本。核心业务类型、构造器和 decoder 名称 MUST NOT 使用 `V1`、`V2`、`V3` 或 `Legacy` 后缀表达当前实现。

#### Scenario: Reject a second runtime decoder
- **WHEN** 实现尝试为同一 Session kind、Tool capability 或内部 Runtime command 注册第二个按 revision 选择的 decoder
- **THEN** 架构检查失败，且该多版本路由不得进入生产构建

#### Scenario: Keep an in-memory value unversioned
- **WHEN** Context plan 或 Runtime command 只在当前进程内由强类型调用方传递
- **THEN** 该值通过 kind 与强类型 payload 校验，不携带只会等于常量的 format version

### Requirement: Boundary markers validate but never route history

内部持久化边界 SHALL 使用严格当前结构，不因跨进程或跨磁盘而默认引入版本标记。Session/native/checkpoint envelope 和压缩来源 MUST 不含 schema_version、payload_version、固定 prompt_revision、canary 或同义格式标记；附加版本字段按未知字段拒绝。Catalog 快照 MUST 不含格式版本门禁，只按完整性和当前强类型结构决定是否重建。已明确声明的 headless JSONL 外部协议 SHALL 保留当前 version，并精确校验，不选择旧 decoder、猜测兼容 shape 或原地改写输入。

内容派生的 source revision、fingerprint、checksum、Provider history committed revision、estimator method、UUIDv7 与应用版本不是格式兼容路由，SHALL 保留其原有语义。Provider opaque unknown item 由 Provider codec 无损保存，不得被误删或解释为共享层版本兼容。

#### Scenario: Reject a stale persisted marker
- **WHEN** 当前程序读取包含非法附加 schema_version 或 payload_version 的 Session journal
- **THEN** 恢复在 repair、append、executor、Provider 网络请求或 Catalog 发布前失败
- **THEN** 系统按未知字段拒绝，不调用历史 decoder或改写原始 bytes

#### Scenario: Preserve a content revision
- **WHEN** Tool Catalog、项目指令或缓存分段的规范化内容发生变化
- **THEN** 对应 source revision 或 fingerprint 按内容变化
- **THEN** 系统不把该变化解释为需要并存业务 DTO 的格式升级

#### Scenario: Keep one external protocol decoder
- **WHEN** headless 客户端发送当前 `version: 1` 的 stream-json 命令
- **THEN** 外部协议仍按其已声明的严格 v1 shape 解码
- **THEN** 内部 RuntimeCommand 不复制该外部 version，也不存在另一个 headless 版本 decoder

### Requirement: Pre-release breaking changes replace the current baseline

在产品首次稳定发布前，契约破坏性变化 SHALL 直接替换仓库中的当前 schema、当前 encoder/decoder、golden 与不可变 fixture。被替换的开发期 journal 或 fixture MUST 明确拒绝，不得以兼容历史用户为理由保留旧 reader、旧 writer、双写、在线 migration 或混合 revision replay。

Catalog 快照作为可重建索引，在完整性或实际结构不符合当前要求时 SHALL 从 JSONL 事实源重建，不得维护历史 migration 链。仓库 MUST 通过静态架构检查与测试约束：每个边界只有一个当前 codec，禁止 Session/native/checkpoint 格式版本、固定压缩 prompt revision 和 Catalog 格式版本门禁；fixture 目录不按 v1/v2 并存；Session 不透传 optional 未知 record；工具不按 input/result revision 分派。首次稳定发布后的兼容窗口必须由新的已确认 OpenSpec 变更另行定义，不能由当前代码预埋。

#### Scenario: Replace a development fixture
- **WHEN** 当前变更改变 Tool ledger 的持久化 shape
- **THEN** 仓库提交一套由固定 bytes 构成的当前 fixture 和预期 replay 摘要
- **THEN** 旧开发 fixture、兼容 decoder 和 migration 测试被删除而不是并存

#### Scenario: Rebuild a derived index
- **WHEN** Catalog 快照结构不符合当前要求
- **THEN** 系统在验证 JSONL 事实源后重建索引
- **THEN** Catalog 不通过逐版本 migration 改写事实源或维持历史 schema

### Requirement: Canonical encoding is independent of replaceable global configuration

缓存 fingerprint、Session checksum、Provider request 和当前持久化对象的 canonical 编码 SHALL 使用固定且不可由包级可变配置替换的规则。相同有效输入 MUST 在不同实例和恢复前后产生相同 bytes；第三方默认 codec 的修改不得改变该结果。纯内存构造不得为实现 codec 共享而启动后台任务或打开外部资源。

#### Scenario: Replace an unrelated global codec
- **WHEN** 隔离测试进程替换第三方默认 codec 配置，再编码同一有效对象
- **THEN** 当前 canonical bytes、checksum 与 fingerprint 和替换前逐字节一致
- **THEN** 测试不得通过共享进程全局修改干扰其他并行测试

#### Scenario: Preserve current valid fixtures
- **WHEN** 当前合法 Session、Provider 和 Tool fixture 经统一 codec 编解码
- **THEN** 既有 canonical bytes 与语义保持一致，不生成另一套版本化 fixture

### Requirement: Closed JSON contracts reject ambiguous or lossy input

自有封闭 JSON 契约 SHALL 在业务副作用前拒绝重复字段、未知字段、大小写别名、尾随值、非法 UTF-8、孤立 Unicode 代理项、缺失必需字段和不允许的 null。可选字段的默认值 MUST 只应用于字段省略，不能把显式 null 当作省略；嵌套封闭对象遵循同样规则。基础解析规则 SHALL 通过统一入口实现，边界仍持有自己的字段与业务验证，不允许宽松旧 decoder 回退。

Provider 外部 wire 中已允许的未知原生 item 与 opaque 扩展 SHALL 继续无损保留；封闭自有 envelope 的严格校验不得变成对所有未知原生字段的一概拒绝。

#### Scenario: Reject null instead of defaulting an integer
- **WHEN** Read 输入为 `{"file_path":"a","offset":null}` 或 `{"file_path":"a","limit":null}`
- **THEN** 输入被拒绝且 executor 不执行；省略 offset/limit 的合法输入仍采用既定默认值

#### Scenario: Reject nested ambiguity
- **WHEN** 当前持久化 payload 的嵌套封闭对象包含重复键、大小写别名或非 nullable 字段上的 null
- **THEN** 解码失败，不加载部分状态、不 repair 或 append 输入 journal

#### Scenario: Preserve opaque provider data
- **WHEN** 合法 native commit 包含未知原生 item、thinking signature 或 encrypted content
- **THEN** 往返与恢复保持原生顺序和 opaque bytes，严格校验不将其转换为共享 DTO

### Requirement: Architecture verification covers contract escape paths

质量门 SHALL 检查所有核心边界（包括 Tool）的无约束动态类型泄漏、业务包绕过统一 JSON 入口、canonical 路径读取可替换全局 codec，以及本次移除的旧构造入口回流。构造器副作用检查 MUST 覆盖生产包而不只覆盖协议包，并以确定性生命周期测试补足静态分析不能证明的边界。

#### Scenario: Reject a contract violation fixture
- **WHEN** 负例暴露 Tool any 参数、在业务包直接解码 JSON、读取可变全局 codec 或重新引入已删除的简化构造入口
- **THEN** 架构检查失败，并定位到对应违规边界

#### Scenario: Keep legitimate boundary markers
- **WHEN** 代码使用外部 headless version、内容 revision 或 Provider opaque envelope
- **THEN** 架构检查不会把这些合法边界误判为多版本兼容实现

### Requirement: Hook execution uses one typed contract and shared owned lifecycle

启用的 Hook 能力 SHALL 仅包含具备完整消费者、validator、strict decoder 和测试的同步 command Hook；当前已交付 PreToolUse、PostToolUse、PostToolUseFailure 与 Stop。未实现事件或行为不得保留可执行 kind、空 facade 或占位策略。Hook 输入/结果、信任命令/事件、执行计划、Session facts 和 recovery 子状态 MUST 使用封闭强类型，核心导出边界不得接受或返回 any 或动态 map，未知字段或 kind 必须失败关闭。

Exec 与 Hook SHALL 共用语义一致的进程接受、并行 I/O、取消、退出观察和最终清理保证，同时分别保留审批、输出、sandbox 和结果策略；不得通过伪造模型调用或借用普通工具 grant 启动 Hook。共享进程边界不得依赖 Runtime、宿主、Provider wire、Tool result 或 Session。Skills 与 Hooks 共用文件打开骨架时 MUST 保留各自来源 authority 与既有安全语义，不得引入跨来源 fallback。

构造、getter、编码和 View MUST 保持纯内存；资源打开、运行和关闭必须使用显式生命周期。每个后台任务必须有唯一 owner 和最终 done，关闭超时不能丢弃所有权。新契约 MUST 直接更新唯一当前 codecs/fixtures，不添加内部格式版本、legacy reader 或迁移器。

#### Scenario: Reject an executable future hook
- **WHEN** 尝试暴露尚未实现的事件、async 或 updatedInput 的可执行入口但没有真实完整消费者
- **THEN** 架构或契约验证失败，不能按占位 capability 宣称支持

#### Scenario: Preserve existing consumers during reuse
- **WHEN** Exec/Hook 共用生命周期、Skills/Hooks 共用安全打开骨架
- **THEN** Exec 仍需原审批且预算/artifact/nonzero-exit 行为不变，Skills 仍保持 no-follow、来源隔离与相同 catalog/request fingerprints

#### Scenario: Construct without external resources
- **WHEN** 只构造 Hook、共享 owner 或信任请求以及读取内存投影
- **THEN** 没有文件打开、进程、网络、数据库或隐藏 goroutine

### Requirement: Provider strategy selection stays at its owning boundary

App SHALL 通过同一装配边界将经验证的 `family/wire` 绑定到在线工厂和离线读取器。在线会话使用有效配置，离线预览使用日志身份。离线装配 MUST 保持无凭据、无网络的读取路径。上层 SHALL 在装配时绑定所需的强类型操作。已绑定的必需操作由调用方直接使用。可选能力缺失时的现有错误和回退行为 MUST 保持，动态能力按使用时的实际状态查询。

Runtime、Context 与 Provider 共享流程 SHALL 使用真实身份、冻结设置、能力查询和已绑定的策略。生产路径 MUST NOT 通过具体家族或具体实现选择共享流程，也不得在操作过程中临时探测已绑定的必需接口。身份相等校验、封闭协议值校验、配置字段解析和 UI 文案 SHALL 保留其所属边界的规则。

#### Scenario: Resolve online and offline identity through one boundary
- **WHEN** 用户启动、恢复或切换会话，或预览另一种已支持协议的日志
- **THEN** 在线工厂、离线读取器和缓存诊断使用实际 `family/wire`，离线预览无需在线凭据
- **THEN** 按各入口现有规则拒绝不受支持的协议身份或证据之间的身份不匹配，在验证完成前没有 repair、写入或 Provider 请求

#### Scenario: Verify shared consumers with a substituted strategy
- **WHEN** 装配边界为现有操作绑定另一个满足当前契约的策略
- **THEN** Runtime 和 Context 通过同一入口消费其身份、配置和能力，历史窗口、预算与续写无需增加家族分支

#### Scenario: Detect provider-specific behavior in a shared consumer
- **WHEN** 生产 Runtime、Context 或 Provider 共享流程按具体家族选择策略，或临时探测已绑定的必需接口
- **THEN** 架构验证定位该越界，合法的身份比较和封闭协议值校验仍通过

#### Scenario: Observe changing continuation capability
- **WHEN** OpenAI 会话从 WebSocket 回退到 HTTP，或某项可选操作在当前配置中不可用
- **THEN** 调用方按现有语义读取当前能力或返回既有拒绝，装配时的绑定不掩盖运行中的状态变化

### Requirement: Provider operations share orchestration and preserve native contracts

Anthropic Messages 与 OpenAI Responses SHALL 共用采样、独立摘要、历史窗口、原生提交准备和有序回放的流程骨架。Provider 适配器 MUST 保留各自的强类型原生条目、wire 编解码、工具配对、缓存策略、传输选择、终态判定和计量。原生历史 SHALL 继续作为请求续写与恢复的事实源，语义投影只用于只读消费者。普通采样和独立摘要 SHALL 通过各自的强类型操作保留现有输出和阶段边界。

预检 SHALL 保持现有副作用边界和准入时机。提交和回放 MUST 保留既有事实顺序、验证阶段及错误优先级。Provider 普通 sample 对账、排他准入、取消与终态后事件按相应规格失败关闭。Runtime 继续持有预算、队列、Hook、审批和持久化提交决策，原生 finalize 仍以当前 durable 事实为前提。既有 owner SHALL 继续负责流、瞬态续写资源、Session lease 和关闭结果。

同一冻结窗口的请求、来源 digest、体量和上下文 revision SHALL 使用一致的已提交原生证据。恢复和只读投影 SHALL 复用已验证的强类型事实，保留各入口原有的验证职责及拒绝顺序。模型可见工具定义与执行依赖 MUST 来自同一组经验证的强类型绑定，离线原生证据验证继续使用无执行器的只读声明。

#### Scenario: Preserve wire semantics across shared sampling and compaction
- **WHEN** 双 Provider 在符合传输协议且未取消的情况下对等价的冻结输入执行采样、工具续写或独立摘要
- **THEN** 各家与重构前的 canonical 请求、原生条目顺序、缓存指纹、终态分类、usage 和公开事件序列一致
- **THEN** Anthropic 的缓存断点与思考预算，以及 OpenAI 的 WebSocket 复用、回退和独立摘要连接继续遵循原有条件

#### Scenario: Preserve prepare and finalize boundaries
- **WHEN** 预检、准备、成功提交或未提交丢弃通过共享流程执行
- **THEN** 符合排他准入规则的操作保留缓存与原生历史可见性、Sync 与 finalize 顺序和回调次数，未完成清理的资源不得交接

#### Scenario: Preserve online restoration and offline preview semantics
- **WHEN** 在线恢复或离线预览包含工具结果、Stop 反馈、摘要 checkpoint 或损坏证据的当前格式日志
- **THEN** 共享回放遵守当前入口的验证顺序，合法日志保留摘要、工具结果与原生 opaque 数据，两家普通 sample 按已保存的计量证据对账
- **THEN** 合法会话下一请求与既有恢复路径逐字节等价，未触发新增对账拒绝的非法日志保持原错误，所有路径维持原有写入、repair 和 lease 边界

#### Scenario: Keep projections and request evidence on one revision
- **WHEN** 一个窗口被用于规划请求、预算或压缩来源
- **THEN** 原生条目、checkpoint 和修订号来自同一捕获点，保持各自 Provider 的原生 wire 和缓存策略

#### Scenario: Keep archived history out of active window copies
- **WHEN** 压缩后归档段增长而活动后缀与摘要不变
- **THEN** 活动规划的条目复制只由所消费的后缀及摘要决定，完整历史与恢复仍保留全部原生证据

#### Scenario: Bind advertised capabilities to executable dependencies
- **WHEN** 宿主装配当前工具目录及执行策略，或者离线验证已保存的调用
- **THEN** 在线模型可见能力与 Runtime 执行依赖一致，离线校验仅使用纯声明，不取得工具权限或网络资源
