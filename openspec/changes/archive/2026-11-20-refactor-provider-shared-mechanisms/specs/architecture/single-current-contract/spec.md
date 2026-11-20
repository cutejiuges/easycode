## ADDED Requirements

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

预检 SHALL 保持现有副作用边界和准入时机。提交和回放 MUST 保留当前事实顺序、验证时机、接受范围及失败结果。Runtime 继续持有预算、队列、Hook、审批和持久化提交决策，原生 finalize 仍以当前 durable 事实为前提。既有 owner SHALL 继续负责流、瞬态续写资源、Session lease 和关闭结果。

#### Scenario: Preserve wire semantics across shared sampling and compaction
- **WHEN** 双 Provider 对等价的冻结输入执行采样、工具续写或独立摘要
- **THEN** 各家与重构前的 canonical 请求、原生条目顺序、缓存指纹、终态分类、usage 和公开事件序列一致
- **THEN** Anthropic 的缓存断点与思考预算，以及 OpenAI 的 WebSocket 复用、回退和独立摘要连接继续遵循原有条件

#### Scenario: Preserve prepare and finalize boundaries
- **WHEN** 预检、准备、成功提交或未提交丢弃通过共享流程执行
- **THEN** 既有准入次序、缓存与原生历史可见性、Sync 与 finalize 顺序、回调次数和清理边界均保持一致

#### Scenario: Preserve online restoration and offline preview semantics
- **WHEN** 在线恢复或离线预览包含工具结果、Stop 反馈、摘要 checkpoint 或损坏证据的当前格式日志
- **THEN** 共享回放遵守当前入口的验证顺序和接受范围，合法日志保留摘要、工具结果与原生 opaque 数据
- **THEN** 合法会话下一请求与既有恢复路径逐字节等价，当前会被拒绝的日志保持原错误及写入、repair 和 lease 边界
