# ADR-0003：使用 Session 线程树组织会话

- 状态：accepted
- 日期：2026-09-18
- Roadmap 阶段：P0-P2、P7

## 背景

EasyCode 需要支持普通多轮对话、resume、fork、compaction 和 Subagent。把任意消息组织成可分叉消息树，会让 Provider 原生 tool call/result 配对、事件顺序和缓存前缀难以维护；只使用单一线性会话又无法表达子代理和跨 Provider fork。

## 约束

- Provider-native history 在单个执行上下文中必须保持有序。
- Tool call/result pairing 和 turn boundary 不能跨分支隐式合并。
- Subagent 需要独立预算、取消、Provider 和 Session 恢复位置。
- UI、Session Store 和 RuntimeEvent 必须能无歧义定位同一条执行链。

## 候选方案

1. 所有消息构成可任意分叉和合并的消息树。
2. 每个 Session 只允许一条线性历史。
3. Session 持有线程树，每个 Thread 内部保持线性 turn/native history。

## 最终决策

采用方案 3：

- `session_id` 标识一个逻辑会话、持久化命名空间和线程树所有者。
- `thread_id` 标识 Session 内一条线性执行历史。
- 每个 Session 恰有一个 root thread。
- fork、compaction fork 和 Subagent 创建 child thread，并记录 `parent_thread_id` 与创建原因。
- 每个 turn 只属于一个 thread；每个 Provider-native item 只属于一个 turn/thread。
- Child thread 的结果通过显式 completion envelope 返回父线程，禁止把 child native history 直接拼入父线程。

```text
Session
  root thread
    turn 1 -> turn 2 -> turn 3
                 |
                 +-- child thread A -> completion envelope
                 |
                 +-- provider-switch compacted fork
```

## 代价与风险

- Session 索引和 UI 需要同时理解 session 与 thread。
- fork 继承规则必须显式定义，不能复制临时权限、usage hint 或不可兼容 native history。
- Completion envelope 需要稳定的 pairing、状态和失败语义。

## Provider 与缓存影响

- 同 Provider child thread 可以按继承策略复用稳定上下文，但拥有独立 CachePlan revision。
- 跨 Provider child/fork 必须先 compact，不复制 opaque reasoning。
- Thread ID、时间戳和树位置不得进入稳定 prompt 前缀。

## Session/协议迁移影响

- EventEnvelope 和 Session Record 同时携带 `session_id` 与 `thread_id`；child 事件额外携带 `parent_thread_id` 或可查询的 thread metadata。
- JSONL 是 Session 级事实源，记录按单调 seq 追加；SQLite 投影维护 thread tree 索引。
- 在线性历史阶段创建的旧 Session 可迁移为仅含 root thread 的树。

## 验证与回归测试

- 覆盖 root thread、fork、Subagent completion、取消和 resume。
- 验证 child native history 不进入父 RequestCompiler。
- 删除 SQLite 后能从 JSONL 重建相同线程树。
- EventEnvelope 中 session/thread/turn 关系违反约束时必须产生英文协议错误。

## 回滚或替代方案

如果未来需要协作式合并，只在 thread 之间增加显式 merge artifact；不把单个 Thread 改造成可任意合并的消息 DAG。
