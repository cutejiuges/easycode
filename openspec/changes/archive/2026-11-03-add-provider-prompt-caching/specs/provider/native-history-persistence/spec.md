## ADDED Requirements

### Requirement: Cache and transport state never replace native restoration

Session/native/checkpoint SHALL 继续只保存唯一当前原生内容、计量与已有事实；不得新增缓存格式版本、历史 reader、可选兼容 record、连接 token、cache_control、派生 key 或可执行续写游标。服务端缓存与 WebSocket 内存状态 MUST NOT 成为恢复依据；native response ID 的原有保留不代表恢复连接续写资格。恢复 SHALL 保持既有 lease、完整验证后 repair、Sync 和不重跑副作用规则。

相同 ThreadID、配置、项目指令、catalog 和有效原生窗口下，uninterrupted 与 restored 的完整逻辑请求 canonical bytes、完整请求 fingerprint、内容分段 fingerprint 以及 native item 顺序 MUST 全部相同。HTTP/SSE 和 WebSocket full create 的各自 wire 派生也 SHALL 分别逐字节相同。连接有效的 incremental wire 与恢复后的 full wire MUST 显式区分：只有在完整请求字节级等价已独立验证后，才能进一步证明前次请求 input 加 durable 原生 outputs 加本次 suffix 精确组成当前完整 input；不得用有损语义文本等价替代完整请求字节比较，也不得宣称不同传输帧相同。

#### Scenario: Restore an enabled Anthropic cache
- **WHEN** 同一当前固定 fixture 在相同 ephemeral_5m 或 ephemeral_1h 配置下恢复
- **THEN** 断点位置、完整 request canonical bytes、原生顺序和 fingerprint 与不中断路径一致
- **THEN** fixture 原有 bytes 不被缓存逻辑或恢复过程重写

#### Scenario: Compare full and incremental OpenAI paths
- **WHEN** 同一历史的连续连接选择 incremental，而恢复连接选择 full
- **THEN** 两条路径独立编译的完整请求 bytes 和 fingerprint 完全相同
- **THEN** 实际 wire 分别匹配 full/incremental golden，原生前缀与 suffix 可精确重组，不伪造 wire bytes 相等

#### Scenario: Keep recovery independent of remote cache eviction
- **WHEN** 服务端缓存已过期或进程重启使 WebSocket 状态丢失
- **THEN** 本地恢复仍成功，第一条实际请求完整携带有效原生上下文，恢复阶段不联网
