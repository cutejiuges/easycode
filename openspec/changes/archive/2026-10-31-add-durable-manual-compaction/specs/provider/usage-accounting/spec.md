## ADDED Requirements

### Requirement: Compaction usage is a separate durable operation metric

压缩 usage SHALL 复用五项三态归一化口径，绑定 CompactionID 而非普通 turn/sample；成功完成时与 checkpoint 原子持久化，Provider raw usage 保留在其自有 provenance 中。获得完整合法响应但候选验证失败时，failed record SHALL 保存 observed normalized usage 和有界 Provider 计量 provenance；未获得可信终态时 SHALL 标记 unavailable，不伪造零或完整账单。usage MUST NOT 进入替换请求、fingerprint 或作为压缩后窗口估算。普通 turn 聚合语义保持不变。

#### Scenario: Account for a rejected candidate
- **WHEN** 模型返回合法完整摘要，但候选因无缩减被拒绝
- **THEN** 失败终态保留本次实际可观察 usage，不混入上一/下一 turn，旧窗口不变

#### Scenario: Preserve uncertainty on interruption
- **WHEN** 摘要 stream 在可信终态前中断
- **THEN** 操作计量为 unavailable，不估计缺失 usage，不发布虚假的普通 sample usage

#### Scenario: Restore usage independently of request content
- **WHEN** 从同一 checkpoint 恢复并读取计量
- **THEN** normalized/raw usage 状态等价且读取不改变下一请求 bytes
