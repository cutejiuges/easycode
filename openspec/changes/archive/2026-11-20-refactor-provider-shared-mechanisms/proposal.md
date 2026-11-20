## Why

Anthropic Messages 与 OpenAI Responses 各自维护相同的操作流程。App 分别选择工厂、wire 和离线 reader，Runtime 又在执行路径中反复探测接口。相同规则散落在装配、运行和两家适配器中，修改一条链路要重复实现和验证。

## What Changes

- 统一 App 的 Provider 装配。用同一个经验证的协议身份选择在线工厂与离线 reader，冻结设置和工具视图，显式绑定 Runtime 与 App 所需的强类型操作。启动、继续、恢复、聊天内切换和预览走对应的装配入口。
- 让两家共用原生历史窗口、输入与 Stop 准备、工具时序、请求编译与证据、普通采样、独立摘要和原生回放主流程。各家策略解释原生条目、协议终态、用量、工具结果、缓存和传输行为。
- 收敛恢复与工具视图的重复工作。增量验证已遍历的历史前缀并复用不可变工具目录的分家族视图，按现有接受范围、首个错误、请求字节和指纹验收。
- 完成全仓审查中的等价优化，覆盖 Session 分派与 writer、Runtime 审批持久化和 Hook 调用、TUI 状态与缓存、Headless JSONL、UUIDv7、工具 facade 与协议事件装配，以及无消费者代码。按现有所有权收敛 App、Runtime、TUI 的局部职责。
- 以当前实现为等价基线。保留 CLI、交互、协议和存储格式、既有错误分类与顺序、请求及缓存指纹。并发门禁修复、取消响应增强、校验收紧及其他行为修复另行评审，具体边界见[设计](design.md)。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `architecture/single-current-contract` — 约束 Provider 装配、能力绑定及两家共享操作流程的职责边界，并由架构检查及行为等价验证守住。现有产品和 wire 规格继续作为验收依据。

## Impact

主要影响 Provider、App、Runtime 和 Context 的内部接口。审查收敛另涉及 Session、Tool、Domain、Protocol、Hook、TUI、Headless 和架构检查。实现按[任务](tasks.md)逐段验证，全链路通过后再同步规格和架构文档。
