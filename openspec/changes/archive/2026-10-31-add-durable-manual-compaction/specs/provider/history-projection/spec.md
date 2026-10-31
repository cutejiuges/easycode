## ADDED Requirements

### Requirement: Active window projection is distinct from complete history

完整历史和 transcript 投影 SHALL 保持原始用户/assistant 消息、真实 turn/InputID、原生提交 ordinal 和工具锚点，不因压缩删除或重新编号。系统 SHALL 另提供有效窗口的只读语义快照供规划，包括带独立来源的摘要文本与 checkpoint 后可见消息。summary MUST NOT 伪造用户/assistant turn。所有投影只从 Provider-native facts 派生且不得反向构造请求。压缩摘要详情 SHALL 从 Provider checkpoint 只读投影产生，app 以 Session sequence 插入边界，不把压缩输出追加到普通 transcript native ordinal。

#### Scenario: Preserve complete history and expose a shorter window
- **WHEN** 一个含工具的会话完成手动压缩
- **THEN** 原始 ProjectHistory/Transcript 消息和锚点保持不变，有效窗口快照只包含摘要与之后增量
- **THEN** 两种投影均无 I/O、不暴露 opaque reasoning，修改返回副本不影响窗口

#### Scenario: Render a checkpoint summary
- **WHEN** 宿主显示压缩摘要
- **THEN** 使用独立压缩边界和摘要详情，不生成假的用户 InputID、turn 或 assistant completion
