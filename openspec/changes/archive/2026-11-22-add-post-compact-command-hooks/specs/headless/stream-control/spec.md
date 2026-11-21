## ADDED Requirements

### Requirement: Headless compaction skips local observation without changing v1 output

非交互 PostCompact 在合法匹配且未超项数上限时 MUST 在锁定计划时以 `hook_trust_required` 跳过，不进入本次检查的输入、平台前检、审查或执行，保持已成功摘要的安装。stream-json v1 SHALL 沿用当前关联的 `compaction.started` 和唯一 `compaction.completed`，无 PostCompact 宿主事件、脚本数据、命令、信任或新增字段；一次性输出沿用已有普通任务投影。匹配 PreCompact 仍遵循现有前置拒绝契约。

#### Scenario: Complete a matched non-interactive manual compaction

- **WHEN** stream-json 客户端发起合法手动压缩，唯一匹配的 Hook 为 PostCompact，摘要成功
- **THEN** 只输出原 correlated accepted、started 和 completed，Provider 摘要一次，后置命令和信任输出均为零
