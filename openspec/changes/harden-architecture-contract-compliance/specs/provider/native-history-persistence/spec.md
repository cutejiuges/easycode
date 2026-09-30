## ADDED Requirements

### Requirement: Prepared native commits are non-zero and self-validating

prepared sample SHALL 只能由通过公共字段和有界 JSON 校验的 native commit envelope 创建，并持有不共享可变 payload buffer 的副本。复制或读取 envelope MUST 显式返回校验失败，绝不能把非法状态转换为“零值 envelope + nil error”。finalizer SHALL 仅在 durable success 后执行一次；无效或重复 finalize MUST 返回错误且不修改 committed native history。

#### Scenario: Clone a valid native envelope

- **WHEN** 调用方复制一个合法 native commit envelope 并修改任一返回的 payload bytes
- **THEN** 原 envelope、prepared sample 和其他副本的 payload 保持不变

#### Scenario: Read an invalid prepared sample

- **WHEN** 调用方读取 nil、零值或内部 envelope 非法的 prepared sample
- **THEN** 操作返回明确错误而不是成功返回零值 envelope

#### Scenario: Finalize exactly once after durability

- **WHEN** sample 对应 Session batch 已成功 Sync 且 Runtime 首次调用 finalizer
- **THEN** native history 恰好提交该增量一次
- **THEN** 任何后续 finalize 调用返回错误且不重复提交
