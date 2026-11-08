# PostToolUse 归档后的共享规格保真

2026-11-08 归档 `add-post-tool-use-command-hooks` 时，主规格已增加后置事件。全量校验发现本 change 的两条 MODIFIED 需求仍缺少主规格新增的 Post 场景；另外六条重叠需求的旧正文也可能在后续同步时覆盖已交付语义。

因此将 `extension/command-hooks`、`session/jsonl-store`、`session/resume`、`tool/invocation-loop`、`tui/hooks` 中共八个重叠需求区块保真更新到当前主规格，保留全部原有 Pre 场景。其余需求、proposal/design/tasks、43/43 完成状态和产品实现不变；本 change 继续保持活动状态，未一并归档。

这是共享规格基线协调，不把 PostToolUse 算作本 change 新增实现。后续归档本 change 时应保留当前主规格已有的 Post 语义，避免用旧的整块 MODIFIED/ADDED 内容回退当前契约。
