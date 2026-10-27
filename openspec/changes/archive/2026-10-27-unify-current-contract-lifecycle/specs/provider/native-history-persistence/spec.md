## ADDED Requirements

### Requirement: Prepared commits require an explicit complete lifecycle

sample 与 tool-output prepared commit SHALL 各有唯一当前构造契约，调用方必须显式提供成功提交与未提交丢弃行为。生产接口 MUST NOT 通过旧构造入口自动补入空清理行为；测试也必须使用相同当前契约。构造保持纯内存，无效 envelope、usage 或缺失生命周期行为在 durable 写入前失败。

每个 prepared commit SHALL 恰好选择 finalize 或 discard 之一，二者互斥且各自最多执行一次。成功 Sync 后才 finalize；未 durable 的失败路径 discard 并释放 staging。重复或竞争 resolution 不得再次执行回调；回调完成前 owner 不得报告清理完成。sample 保留 normalized usage，tool outputs 不携带 sample usage。

#### Scenario: Reject an incomplete lifecycle
- **WHEN** 构造 prepared commit 缺少 finalize 或 discard 行为
- **THEN** 构造失败，生产代码没有自动补空行为的旧入口

#### Scenario: Discard an uncommitted sample
- **WHEN** sample 无法完成 durable 提交
- **THEN** staging 丢弃恰好一次，committed history、revision 和下一请求不包含该 sample

#### Scenario: Resolve competing lifecycle operations
- **WHEN** finalize、discard 或重复 resolution 竞争同一个 prepared commit
- **THEN** 只有一个合法状态转换执行一次对应回调，其他操作返回明确错误
- **THEN** 不重复提交 history，也不在回调未完成时释放其 owner 的资源

#### Scenario: Keep restored requests equivalent
- **WHEN** 两家 Provider 分别完成合法 sample/tool-output 提交，并从相同 journal 恢复
- **THEN** restored 与 uninterrupted 路径的原生 item 顺序、opaque bytes、下一请求 canonical bytes 和 fingerprint 一致
