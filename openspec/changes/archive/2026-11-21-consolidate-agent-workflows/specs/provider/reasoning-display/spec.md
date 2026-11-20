## MODIFIED Requirements

### Requirement: Reasoning display has a strictly scoped stream lifecycle

内部展示 SHALL 使用稳定 sample/block scope、来源与 Started/Delta/Completed 三阶段的强类型当前契约，严格拒绝重复/未知字段、null、跨种类字段和非法状态。块只允许开始一次，Delta 为连续原文范围，Completed 是有界替换快照而不是再次追加；块完成不代表保存。正常 sample 成功 MUST 已关闭全部块；sample 失败或取消 SHALL 根据 runtime/chat-turn 的真实提交解析一次收口全部当前块，允许缺少块 Completed，且不影响先前已提交 sample。

Provider 完整 item 首次提供允许正文时 SHALL 发布有界的 Started/Completed 序列，仍只形成一个展示块；不允许内部消费者隐式接受未开始的 Completed。内部过期 sample、已关闭块的新 delta、重复关闭或身份错配 MUST 失败并沿原 owner 取消/排空。Provider 边界明确允许幂等或丢弃的外部完成通知不发布内部事件，不得混同内部非法事件。

#### Scenario: Consume item-only output once
- **WHEN** OpenAI 仅以合法完整 reasoning item 提供正文
- **THEN** 发布有序的 Started/Completed。Provider 私有归并保留完整原生 item，展示只有一个块和一份正文，无需额外外部事件

#### Scenario: Fail a sample with open blocks
- **WHEN** stream 在多个块尚未 Completed 时取消、失败或提前 EOF
- **THEN** 真实 sample resolution 收口所有当前块，不额外制造第二次 turn failure，不把先前 durable 块标为未保存

#### Scenario: Reject an invalid internal update
- **WHEN** 内部 delta 引用已关闭块或其他 sample
- **THEN** 返回安全协议错误并完成唯一 stream cleanup，不能以默认忽略事件绕过
