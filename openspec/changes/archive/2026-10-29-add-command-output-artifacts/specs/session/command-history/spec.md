## Purpose

定义用户查看当前会话历史命令的独立只读投影与分页查询，使恢复后的界面能够定位执行结果和保存输出，同时保持 Provider 原生续写、工具账本及语义文本历史的职责分离。

## ADDED Requirements

### Requirement: Command history derives only from validated facts

系统 SHALL 从经过 replay 校验的完整命令 ready、approval、started、result facts 建立有序投影，包含稳定 invocation identity、seq、完整命令、相对 workdir、执行状态、冻结 preview 和保存 descriptor。拒绝、取消和恢复补偿结果不得丢失。输出文件存在性不能推断命令状态，瞬态 progress 不得作为恢复事实。该投影 MUST 不改变 Provider native history 或 SemanticHistoryView。

#### Scenario: Restore older commands
- **WHEN** thread 含多个已完成 turn、拒绝命令及中断尾部
- **THEN** 历史查询按 seq 提供相应命令及已验证结果，不局限于最新命令或 ToolRecovery 尾部

#### Scenario: Keep provider history independent
- **WHEN** 用户查询、选择或展开历史命令
- **THEN** native history、stable catalog fingerprint 和 journal bytes 均不变化

### Requirement: Host queries are typed bounded and session scoped

宿主 SHALL 提供当前 thread 的 typed 命令列表和输出范围查询，列表单页最多 50 项、游标绑定稳定 seq/invocation，输出复用相同授权与完整性规则。构造/getter/render MUST 不执行 I/O；异步查询必须可取消，具有唯一 owner 和完成信号。错误只影响查询，不修改既有事实。

#### Scenario: Read history without invoking tools
- **WHEN** 用户在恢复后翻页查看已授权输出
- **THEN** 系统仅执行只读宿主查询，不产生 Provider 请求、审批或 tool ledger record

#### Scenario: Cancel a stale selection
- **WHEN** 选择命令、流或 Session 改变时旧查询仍运行
- **THEN** 旧查询收到取消并由 owner 收尾，迟到回复不能覆盖当前选择

#### Scenario: Show unavailable output
- **WHEN** 历史命令的 artifact 缺失或损坏
- **THEN** 列表与冻结 preview 仍然可用，范围查询返回安全 unavailable/corrupt，继续会话不受影响
