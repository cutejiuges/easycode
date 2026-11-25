## ADDED Requirements

### Requirement: Prompt check facts constrain the next real input association

当前 journal SHALL 支持封闭的 `UserPromptSubmit` 计划、运行启动和结束、逐输入决策及检查收口。每项事实使用单记录 batch。首轮计划位于 `turn_started` 后，也可位于完整工具 outputs 后，或位于 Stop continued 已保存且反馈尚未交付的目标采样点。计划必须早于该安全点的压缩、激活及原生输入。所有事实关联同一 turn、采样边界、计划与 input，且使用同一现有 envelope。开放计划仅接受自己的有序检查前缀。

通过决定 MUST 有全部声明的有序成功完成证据。拒绝或失败须对应本条输入的首个终止运行结果、对应的未启动声明，或可由当前记录形状表达的事件级前检原因。计划正常收口须覆盖完整 claim。取消、未知或恢复收口可封闭尚未决定的有序后缀，保持已保存的决定，收口时不得产生新通过授权。取消、未知或恢复关闭均无原生用户输入交付资格。启动和结束的外层 TurnID 必须等于 typed target，结束使用同一运行身份，且反馈必须为空。

同一 InputID SHALL 只出现在一项检查计划中。已提交的 InputID 不可重新进入检查。正常收口后，目标位置出现的输入批次只能是完整且有序的通过子序列，按普通、SessionStart 初始或 Stop 纠正的现有原子形状写入。收口不保证输入已提交。曾在检查中出现的身份不得从后续位置或无计划路径写入。回放 SHALL 拒绝错位和重复计划、非法运行前缀、未获准输入及原生交付不匹配。

如果同一安全点走到下一 sample、Stop `FeedbackOnly` 或 turn 终态而没有用户输入交付，未使用的检查资格即失效，身份仍保留。该约束适用于完整回放、分支前缀 metadata 验证和继承历史。

#### Scenario: Reject reuse of a denied ID at another safe point

- **WHEN** 较晚输入关联尝试用曾被拦截且未进入原生历史的 InputID 提交另一位置的输入
- **THEN** Session 在 repair 与恢复副作用前报告错误，历史保持原状

#### Scenario: Reject a forged prompt check batch before tail repair

- **WHEN** 末尾已有完整且 checksum 合法的检查记录，却将该单记录事实声明为一个未写完的多记录 batch
- **THEN** loader 在 repair 前拒绝非法批次，原文件保留。真正的末尾半行继续按现有规则修复
