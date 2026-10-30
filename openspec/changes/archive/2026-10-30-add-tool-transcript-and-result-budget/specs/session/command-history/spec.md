## MODIFIED Requirements

### Requirement: Command history derives only from validated facts

系统 SHALL 从经过 replay 校验的完整七工具 ready、approval、started、result facts 建立有序投影，包含稳定 invocation identity、seq、工具名称、typed 输入摘要、适用时的完整命令与相对 workdir、执行状态、冻结 preview 和保存 descriptor。拒绝、取消和恢复补偿结果不得丢失。输出文件存在性不能推断工具状态，瞬态 progress 不得作为恢复事实。该投影 MUST 不改变 Provider native history 或 SemanticHistoryView。

#### Scenario: Restore older commands
- **WHEN** thread 含多个已完成 turn、拒绝命令及中断尾部
- **THEN** 历史查询按 seq 提供相应命令及已验证结果，不局限于最新命令或 ToolRecovery 尾部

#### Scenario: Keep provider history independent
- **WHEN** 用户查询、选择或展开历史命令
- **THEN** native history、stable catalog fingerprint 和 journal bytes 均不变化

投影 SHALL 覆盖 Read、Glob、Grep、Patch、Write、Exec 与 ReadArtifact，并由 app 与 Provider 纯 transcript 投影的调用锚点关联，保留 native 文本/调用交错和用户输入关联。seq 与 entry 内位置决定顺序，完成时刻不移动条目。历史修改仅显示已保存摘要，不重读文件生成 diff。

#### Scenario: Join mixed tool history
- **WHEN** 同一 sample 含文本、Read、文本、Exec 且完成顺序不同
- **THEN** 查询保持原生交错与稳定调用身份，恢复后顺序相同，拒绝和补偿结果仍可见

### Requirement: Host queries are typed bounded and session scoped

宿主 SHALL 提供当前 thread 的 typed transcript 列表、工具详情和输出范围查询，列表单页最多 50 项、游标绑定稳定 seq/entry 内位置/invocation，输出复用相同授权与完整性规则。构造/getter/render MUST 不执行 I/O；异步查询必须可取消，具有唯一 owner 和完成信号。错误只影响查询，不修改既有事实。

#### Scenario: Read history without invoking tools
- **WHEN** 用户在恢复后翻页查看已授权输出
- **THEN** 系统仅执行只读宿主查询，不产生 Provider 请求、审批或 tool ledger record

#### Scenario: Cancel a stale selection
- **WHEN** 选择命令、流或 Session 改变时旧查询仍运行
- **THEN** 旧查询收到取消并由 owner 收尾，迟到回复不能覆盖当前选择

#### Scenario: Show unavailable output
- **WHEN** 历史命令的 artifact 缺失或损坏
- **THEN** 列表与冻结 preview 仍然可用，范围查询返回安全 unavailable/corrupt，继续会话不受影响

正文页 SHALL 不超过 64 KiB UTF-8，保存输出页不超过 64 KiB raw/256 KiB escaped，客户端最多缓存四页。查询与布局 MUST 不预先复制或渲染全部历史；已有 Session 持有原生历史不意味着查询可另建无界正文副本。现有命令查询 owner SHALL 演进为唯一 transcript/detail owner，而不是并存两套服务。

#### Scenario: Page long text and search output
- **WHEN** 旧会话含长 assistant 正文与 search_result 引用
- **THEN** 查询按稳定正文偏移或 artifact 范围分页，取消与迟到回复规则和命令详情相同
