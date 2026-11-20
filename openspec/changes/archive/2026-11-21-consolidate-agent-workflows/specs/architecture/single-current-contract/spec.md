## MODIFIED Requirements

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
