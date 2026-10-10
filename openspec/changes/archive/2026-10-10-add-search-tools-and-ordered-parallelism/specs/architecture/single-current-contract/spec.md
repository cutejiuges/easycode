## Purpose

定义 EasyCode 在产品发布前对内部协议、持久化格式与外部机器协议的单一当前契约，防止开发期演进累积成 V1/V2 并存的业务模型、decoder 和迁移分支。

## ADDED Requirements

### Requirement: Every boundary has one current executable contract

每个 Session record、Provider native envelope、Tool input/result、Runtime command/event、Context plan、缓存计划和 SQLite 索引 SHALL 只有一套当前 typed model、constructor、validator、encoder 与 decoder。生产代码 MUST NOT 通过版本号、后缀类型、legacy DTO、fallback decoder、migration graph 或 feature flag 在同一二进制中选择多套历史业务语义。

纯内存值和同进程命令在没有跨版本交换需求时 MUST NOT 携带固定格式版本。核心业务类型、构造器和 decoder 名称 MUST NOT 使用 `V1`、`V2`、`V3` 或 `Legacy` 后缀表达当前实现。

#### Scenario: Reject a second runtime decoder
- **WHEN** 实现尝试为同一 Session kind、Tool capability 或内部 Runtime command 注册第二个按 revision 选择的 decoder
- **THEN** 架构检查失败，且该多版本路由不得进入生产构建

#### Scenario: Keep an in-memory value unversioned
- **WHEN** Context plan 或 Runtime command 只在当前进程内由强类型调用方传递
- **THEN** 该值通过 kind 与强类型 payload 校验，不携带只会等于常量的 format version

### Requirement: Boundary markers validate but never route history

确有跨进程、跨磁盘或外部消费者边界的格式 MAY 保留一个当前 version/revision canary，包括 headless JSONL、Session envelope、Provider native envelope 和 SQLite `user_version`。当前程序 SHALL 在解析 payload 前验证 marker 精确等于当前值；marker 不匹配 MUST fail closed，且 MUST NOT 选择旧 decoder、猜测兼容 shape 或原地改写输入。

内容派生的 source revision、fingerprint、checksum、Provider history committed revision、estimator method、UUIDv7 与应用版本不是格式兼容路由，SHALL 保留其原有语义。Provider opaque unknown item 由 Provider codec 无损保存，不得被误删或解释为共享层版本兼容。

#### Scenario: Reject a stale persisted marker
- **WHEN** 当前程序读取 schema marker 不等于当前常量的开发期 Session journal
- **THEN** 恢复在 repair、append、executor、Provider 网络请求或 SQLite 写入前失败
- **THEN** 系统不调用历史 decoder或改写原始 bytes

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

SQLite 作为可重建索引，在 schema marker 不匹配时 SHALL 从 JSONL 事实源重建，不得维护历史 migration 链。仓库 MUST 通过静态架构检查与测试约束：每个边界只有一个当前 codec；fixture 目录不按 v1/v2 并存；Session 不透传 optional 未知 record；工具不按 input/result revision 分派。首次稳定发布后的兼容窗口必须由新的已确认 OpenSpec 变更另行定义，不能由当前代码预埋。

#### Scenario: Replace a development fixture
- **WHEN** 当前变更改变 Tool ledger 的持久化 shape
- **THEN** 仓库提交一套由固定 bytes 构成的当前 fixture 和预期 replay 摘要
- **THEN** 旧开发 fixture、兼容 decoder 和 migration 测试被删除而不是并存

#### Scenario: Rebuild a derived index
- **WHEN** SQLite `user_version` 与当前索引 schema 不一致
- **THEN** 系统在验证 JSONL 事实源后重建索引
- **THEN** SQLite 不通过逐版本 migration 改写事实源或维持历史 schema
