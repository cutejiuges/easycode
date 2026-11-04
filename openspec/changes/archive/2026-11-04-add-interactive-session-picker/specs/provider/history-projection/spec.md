## ADDED Requirements

### Requirement: Stored native history supports offline semantic projection

每个受支持 Provider SHALL 提供不依赖连接凭据、base URL、transport 或活动 Runtime 的离线语义投影能力，接收强类型 thread/Provider/wire/model 来源与有序恢复记录，并支持对宿主提供的关联强类型压缩用量证据进行离线校验。离线投影与证据校验 SHALL 与正式恢复共享当前 native strict decoder、role/InputID 关联、历史顺序/tool pairing、checkpoint 去重/source/fingerprint 和用量证据验证规则；不得绕过未知 shape 检查或使用兼容 reader。

投影 SHALL 只进行可取消的内存计算，无文件、数据库、网络、进程副作用或隐藏后台任务；未完成完整验证不得返回部分成功视图。模型来源 SHALL 使用已验证的存储 metadata，不依赖当前连接模型。返回 SHALL 是独立的完整语义历史快照，保留现有可见文本/真实身份规则，不暴露可执行 Conversation、原生内部对象或 opaque 数据。

#### Scenario: Project both provider histories without connection configuration
- **WHEN** 调用方仅提供合法来源和当前恢复记录，没有任何 API key/base URL
- **THEN** 两家 Provider 均可返回与正常恢复后 ProjectHistory 相同的可见语义，网络/文件/进程操作为零

#### Scenario: Reject inconsistent native or compaction records
- **WHEN** 任一记录包含错误 role、InputID 关联、配对顺序、重复 checkpoint、错误 source 或 usage/provenance
- **THEN** 离线校验拒绝整份输入，其合法性判定与正式恢复的只读校验一致，不返回部分视图

#### Scenario: Cancel an offline projection
- **WHEN** 调用在接收前或逐条验证期间取消
- **THEN** 不返回成功视图，无外部副作用，已创建临时内存状态不成为共享历史

### Requirement: Offline projection preserves native continuation and cache semantics

引入离线投影后，正式恢复 SHALL 继续消费原生记录；语义视图不得成为 RequestCompiler、checkpoint 或工具补偿的反向输入。对同一有效历史执行零次或多次离线投影 SHALL 不改变后续 canonical request bytes、原生 item 顺序、内容/策略 fingerprint、opaque reasoning 或项目上下文来源。正常恢复的瞬态连接重置语义 SHALL 保持不变。

#### Scenario: Compare uninterrupted restored and previewed requests
- **WHEN** 两家 Provider 对含同轮输入、工具和压缩的相同历史分别直接续写、恢复续写、预览后恢复续写
- **THEN** 按既有恢复等价口径得到相同 canonical bytes、native 顺序与 fingerprint，预览不新增请求项或缓存失效原因

#### Scenario: Mutate a preview projection
- **WHEN** 消费者修改或丢弃离线返回视图后再次投影或正常恢复
- **THEN** 原始记录、其他视图、原生续写与 opaque 数据不受影响
