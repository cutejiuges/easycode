# context/cache-diagnostics Specification

## Purpose

提供不含请求正文的缓存观测和失效解释，使用户与宿主分别理解稳定内容、策略、实际服务端用量和传输续写模式，同时保留未知值、恢复边界和操作归属，避免诊断反向改变模型输入或持久化事实。

## Requirements

### Requirement: Cache diagnostics separate content policy and transport

诊断 SHALL 表达 provider/wire/model、当前有效策略、transport、full/incremental 模式及原因、有序来源的 kind/revision/content fingerprint、stable-prefix fingerprint，以及与上一成功普通 sample 相比的首个来源变化。Provider SHALL 另提供有效原生上下文 digest 和 first/append/replaced/boundary 关系；相同语义规划指纹不得被当作 opaque 原生内容相同的证明。正常 append SHALL 标识为 history_appended，不能误报旧前缀失效。策略 fingerprint MUST 与内容 fingerprint 分开；缓存 key 的值、实际请求、原生 reasoning、路径、连接 header、token 凭据和完整 prompt diff MUST NOT 出现在查询、日志或 snapshot 中。getter 和格式化 SHALL 无 I/O，所有切片/字节值均不可变。

#### Scenario: Explain only a policy change
- **WHEN** 内容相同但 TTL 或 key 模式不同
- **THEN** 内容 fingerprint 一致，策略比较标识变化，不能谎报工具或指令内容变化

#### Scenario: Ignore a UI-only change
- **WHEN** 仅终端尺寸、主题、详情开关、cwd 展示或 spinner 改变
- **THEN** 内容/策略 fingerprint 和模型请求 bytes 保持不变

#### Scenario: Distinguish appended history from a replaced prefix
- **WHEN** 新请求只在已确认的原生前缀后追加 sample outputs、tool outputs 或用户输入
- **THEN** 历史来源变化显示 history_appended，稳定前缀不失效；即使通过 HTTP full 发送也保持该关系

#### Scenario: Detect an opaque-only difference
- **WHEN** 两个窗口语义投影相同，但模型可见 opaque 原生字段不同
- **THEN** 原生上下文 digest 和关系明确显示变化，不以相同语义 fingerprint 宣称可复用

### Requirement: Observations follow durable operation boundaries

缓存观测 SHALL 绑定实际普通 sample 或独立 CompactionID，并使用同一次请求冻结的输入元数据、最终 transport 结果和 normalized usage；纯预检、查询、输入候选与未发送请求不得推进观测。普通成功基线只在 sample 成功 Sync/finalize 后推进一次；摘要成功基线只在 checkpoint 成功 Sync/finalize 后推进，且不覆盖普通基线。失败和取消可提供有界英文状态，但不得计为成功观测或把部分 usage 合并进成功 turn。

#### Scenario: Query repeatedly without changing counters
- **WHEN** 宿主在普通请求或压缩活动中重复查询
- **THEN** 获得最近 committed 快照及独立 activity，不发新请求、不推进成功统计或改变续写资格

#### Scenario: Fail after receiving a completion
- **WHEN** Provider 生成完毕但 journal Sync 失败
- **THEN** 不发布成功缓存观测、不更新普通基线，Runtime 按现有规则 poisoned

### Requirement: Cache metrics preserve uncertainty and scope

观测 SHALL 复用 input_uncached、cache_read、cache_write、output、reasoning_output 五项 normalized usage，不重新解释 Provider 原始计数字段。比例只在所有适用分量 known 且无溢出、分母大于零时计算，not_applicable 不贡献数值，unknown 不得当零。未观测、已观测零、unknown 和 disabled 必须可区分。快照 SHALL 分别标识最近 durable 普通 sample、最近成功 turn 聚合与最近成功摘要；摘要不得计入普通 turn。进程内统计及比较 SHALL 明确标记 scope=process；恢复后标记 restored_baseline，不伪造重启前诊断或网络连接连续性。

#### Scenario: Keep an incomplete denominator unknown
- **WHEN** 已知 cache_read，但任一适用输入计量 unknown 或分母为零
- **THEN** 保留原指标状态，比例显示 unknown，不以 cache_read 或本地估算补齐分母

#### Scenario: Explain compaction separately
- **WHEN** 压缩成功后第一条普通请求缓存读取减少
- **THEN** 显示 context_compacted 边界，普通 turn 聚合不包含摘要 usage

### Requirement: Invalidation explanations are bounded evidence rather than server claims

内容差异 SHALL 使用固定来源顺序及封闭原因 first_request、history_appended、source_content_changed、source_revision_changed、source_added、source_removed、native_context_changed、context_compacted、restored_baseline；策略和 transport 原因独立呈现。相同 fingerprint MUST NOT 被解释为实际 cache hit，服务端 cache_read 明确大于零才可表示观察到读取；明确零与 usage 缺失必须区分。无客户端变化的读取降低只能标识 unexplained_cache_read_drop；TTL 或路由只能作为可能原因，不能宣称确定驱逐。状态 SHALL 按 Conversation 实例持有，快照编码不超过 64 KiB，来源不超过当前支持的五种，不保存正文闭包或全局历史；内存占用不得随 turn 数无限增长。

#### Scenario: Preserve an unexplained miss
- **WHEN** 内容及策略相同且服务端明确报告 cache_read 从正值降为零
- **THEN** 记录观察结果与未知原因，不编造 TTL 到期或客户端内容变化

#### Scenario: Bound a long conversation
- **WHEN** 同一会话完成大量 samples 或另一会话并行运行
- **THEN** 诊断只保存固定数量的最近值，状态相互隔离，无无限列表或全局 map
