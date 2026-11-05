## ADDED Requirements

### Requirement: Structured answer admission is correlated and exactly once

控制面 SHALL 为提问回答提供独立强类型 resolve 命令、constructor、strict decoder 和 validator，包含 RequestID、expected TurnID、question request ID、invocation ID、question fingerprint 与完整有序回答。该命令不进入输入 FIFO、不创建 InputID/新 turn、不解释为 approval 或 steering。不存在活动提问、旧 turn、旧请求、指纹错配和重复回答分别以稳定英文 `no_active_question`、`turn_mismatch`、`question_mismatch` 或 `duplicate_request` 拒绝；拒绝无 Session、队列、授权或工具副作用。

同一 owner SHALL 对 response、stop、shutdown 排序。接收前观察到取消则零接收；回答一旦接收，由 turn owner 持有直到确定 result 提交或失败，即使调用者取消或随后 stop，也不得丢弃已接收回答、自动重试或因关闭提前释放 lease。最多一个活动提问，无独立无界队列、轮询或每次等待创建的 helper goroutine。

#### Scenario: Receive an answer for an old request
- **WHEN** 问卷 A 已结束，问卷 B 活动时收到 A 的回答
- **THEN** 拒绝旧响应，B、聊天 FIFO 和 Session 不变

#### Scenario: Serialize answer and stop
- **WHEN** 回答和 stop 竞争
- **THEN** stop 先接收则拒绝后到回答；回答先接收则先取得其确定持久化结果，再按 stop 关闭当前 turn

#### Scenario: Retain ownership after a submitter timeout
- **WHEN** 回答接收后调用者 context 到期或宿主断开
- **THEN** 唯一 owner 继续完成 result/outputs/terminal 和清理，不能释放仍在使用的 writer 或 lease

### Requirement: Question events separate waiting acceptance and commitment

系统 SHALL 提供强类型 user_question_requested 与 user_question_resolved 瞬态事件，携带匹配 session/thread/turn、request、invocation 和 fingerprint；每个当前 kind 有专属构造、strict decoder 和 validator。requested 仅在 started Sync 后发布；resolved 仅在实际已发布 requested 且 result Sync 后发布，并准确区分 answered、skipped 或 cancelled；无交互拒绝不发布问卷事件。命令的 accepted/resolving 与 result durable 是不同事实；UI 事件不进入 JSONL，也不代替恢复依据。

#### Scenario: Resolve after durability
- **WHEN** 回答 command accepted 后 result 尚未 Sync
- **THEN** 只能观察提交中；成功 Sync 才发布 resolved，Sync 不确定不得发布成功
