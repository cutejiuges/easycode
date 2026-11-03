## ADDED Requirements

### Requirement: Runtime publishes cache observations only after durable finalization

Runtime SHALL 为每次实际 sample 关联同一冻结请求来源、实际 full/incremental transport 与 normalized usage，并在 native commit/sample_usage 及 ready 或最终完成 batch 成功 Sync、prepared sample finalize 成功后，恰好一次接收成功缓存观测。该观测不新增 Session record，不修改现有 ready、approval、execution_started、result、tool-output 或 turn terminal 次序。输入候选预检和查询 MUST NOT 更新缓存基线；Sync/finalize 失败 MUST 遵循现有 poisoned 规则且不得发布成功观测或启动下一请求。

独立摘要使用 CompactionID 与原有 checkpoint durable 边界，普通 sample、成功 turn 聚合及摘要观测 MUST 分离。活动 worker SHALL 通过既有有序 publication 向唯一 AgentLoop owner 发布不可变诊断快照；查询不得并发读取正在修改的 Provider staging、执行 I/O 或等待活动请求结束。停机排空必须包含已接收查询响应并等待原有 transport owner 完成。

#### Scenario: Observe an intermediate tool sample once
- **WHEN** 带工具调用的 sample native/usage/ready 已 Sync/finalize
- **THEN** 该 sample 的缓存观测推进一次，工具仍必须在既有批准及 started durable 后才能执行

#### Scenario: Keep a failed commit out of diagnostics
- **WHEN** Provider completed 后 native batch Sync 失败
- **THEN** 成功统计和续写基线不推进，Session poisoned，后续请求与工具副作用被禁止

#### Scenario: Preserve a committed sample before a later tool failure
- **WHEN** 中间 sample 已 durable，而后续工具失败导致 turn_failed
- **THEN** 最近成功 sample 观测保留，最近成功 turn 聚合不被部分用量覆盖，原有失败终态 shape 不变

#### Scenario: Query while a worker is blocked
- **WHEN** 普通 stream、工具审批或摘要正在等待外部输入
- **THEN** owner 返回最近 committed 快照及 activity，查询不阻塞在网络、Provider 锁或 journal I/O 上
