## ADDED Requirements

### Requirement: Question activity preserves committed decisions in history

主线 SHALL 以独立提问活动打断 Read/Search 折叠，准确显示等待回答、提交中、已回答、跳过、取消、不可用与不确定；终态来自 durable 结果。Ctrl+O SHALL 能查看原问题、全部选项及完整冻结回答，恢复后仍可回看但不可重新提交旧问卷。回答显示为该工具活动的结果，不伪装成普通用户消息或审批。

问卷 modal SHALL 优先于 transcript 并保存原阅读锚点；已 durable 的历史详情通过既有 typed 查询分页，复用最多四页及 64 KiB 原始正文页预算，不能为问卷复制无界历史缓存。浏览、折叠、resize、无色和安全转义不改变模型 preview、原生历史或授权。

#### Scenario: Inspect a restored answer
- **WHEN** 恢复后打开 Ctrl+O 中较早的提问详情
- **THEN** 显示与提交时一致的问题/回答/跳过状态，零模型请求、零 ledger 写入、零重新提问

#### Scenario: Suspend and resume a history page
- **WHEN** 浏览旧工具详情时出现问卷，用户提交后返回浏览
- **THEN** 问卷期间浏览按键不能泄漏到聊天或授权，结束后恢复原锚点并遵守共享页预算
