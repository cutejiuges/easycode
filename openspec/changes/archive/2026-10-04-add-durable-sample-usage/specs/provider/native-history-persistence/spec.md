## ADDED Requirements

### Requirement: Prepared samples couple native history with normalized usage

每个成功 prepared sample SHALL 同时包含一个可独立复制并重新校验的 Provider-native commit envelope 和一个不可变 normalized sample usage。两者 MUST 来自同一个已完成 Provider response；Runtime 不得接受缺少任一部分、已经 finalized、复制失败或 normalized usage 非法的 prepared sample。

Normalized usage 属于共享计量事实，不得嵌入公共 opaque envelope 后再由 Session 或 Runtime 解析。Provider-native raw usage SHALL 留在相应 Provider payload 中；normalized usage 不得替代 raw usage，也不得参与 native history 请求编译。

#### Scenario: Read a complete prepared sample

- **WHEN** Provider 完成一个合法 sample 并创建 prepared sample
- **THEN** Runtime 可取得互不共享可变 buffer 的 native envelope 和 normalized usage 副本
- **THEN** 两个事实对应同一个 Provider completion

#### Scenario: Reject a prepared sample without usage

- **WHEN** completed terminal 携带的 prepared sample 没有合法 normalized usage
- **THEN** Runtime 在 Session append 前以 stream protocol failure 收口
- **THEN** native finalizer 不执行且成功事实不进入 journal

### Requirement: Native usage round-trip preserves Provider facts

OpenAI native commit SHALL 在当前 payload v1 中保存 completed response 中受支持 raw usage 字段及其字段级已知/未知状态；Anthropic native commit SHALL 继续在 payload v1 中保存最终 message raw usage。两个 Provider 的 codec 都 MUST 在 encode/decode/restore round-trip 后保持 raw usage 数值、缺失状态和 Provider 字段含义等价，且不得因为存在 normalized usage 而删除或重写 raw usage。本变更不得为 usage 引入新的 native payload revision 或双版本 decoder。

#### Scenario: Round-trip OpenAI raw usage

- **WHEN** OpenAI native commit 包含部分已知、部分缺失的 Responses usage
- **THEN** 持久化并恢复后的 raw usage 数值和字段缺失状态与完成响应等价
- **THEN** commit 继续使用唯一的 OpenAI native payload v1

#### Scenario: Round-trip Anthropic raw usage

- **WHEN** Anthropic native commit 包含最终 message usage
- **THEN** 持久化并恢复后仍保留相同数值以及 known/unknown 状态

#### Scenario: Keep request compilation independent from normalized usage

- **WHEN** 相同 native history 分别与不同的宿主 usage 投影组合
- **THEN** 下一次 Provider request 的 native items、canonical bytes 和 fingerprint 不发生变化
