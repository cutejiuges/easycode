## ADDED Requirements

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
