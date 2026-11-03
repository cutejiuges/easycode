## ADDED Requirements

### Requirement: Local cache inspection is concise and isolated from model input

TUI SHALL 在 composer 提交边界精确识别独立 /cache，不接受参数；/caches、正文中间的 /cache 或非命令文本 MUST 保持普通正文。合法命令 SHALL 只查询宿主缓存快照，不产生 user_inputs、turn 或网络。接受后可加入本地 recall，拒绝时保留草稿与光标。/context SHALL 增加当前缓存策略及 transport 的简短摘要；/cache SHALL 展示最近 durable 普通 sample、最近成功 turn 聚合、独立摘要、内容/策略变化和实际 transport 原因，不默认将长诊断插入聊天主线。

面板 SHALL 明确 process scope、restored_baseline、未观测、disabled、known zero 与 unknown，不显示原始 key、prompt、header、opaque reasoning 或绝对路径。Esc 关闭面板并恢复草稿，Ctrl+C 与审批/历史详情沿用既有优先级，查询中的迟到响应不得重新打开已关闭面板或覆盖新请求。固定尺寸、窄屏、小屏、无色与 hostile metadata SHALL 有有界可读展示；View 无 I/O 且不改变请求 fingerprint。

#### Scenario: Inspect cache during a running turn
- **WHEN** 用户提交 /cache 且普通任务活动中
- **THEN** 显示最近 committed 观测与活动状态，不取消任务、不启动新 turn

#### Scenario: Reject cache arguments without losing work
- **WHEN** 用户提交 /cache extra
- **THEN** 显示安全英文参数错误，草稿、光标和 Provider 调用次数保持

#### Scenario: Close before the response arrives
- **WHEN** 用户在查询返回前关闭面板，随后旧快照到达
- **THEN** 旧结果不重开面板，不覆盖新的 /context 或 /cache 查询状态

#### Scenario: Render unknown usage in a narrow terminal
- **WHEN** cache_write unknown 且窗口很窄
- **THEN** 仍可辨识 unknown、有效策略和关闭操作，不显示虚假的百分比
