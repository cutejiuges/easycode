## ADDED Requirements

### Requirement: Prompt checks precede candidate history and preserve continuation duties

Runtime SHALL 在 `turn_started` 和本次输入领取后，在初始、工具后追加或 Stop 纠正的安全点完成适用的 `UserPromptSubmit`。通过项才可成为该次输入的自动压缩候选和最终原生提交候选。待执行 startup/resume 的 `SessionStart` 保持在首条合格输入的自动压缩和输入预检之后。没有通过项的初始 turn SHALL 在采样和压缩请求之前收口。

Stop continued 的耐久决定 SHALL 先于真实纠正检查。遇到拒绝或取消时按既有路径单独履行未交付反馈。已提交的工具输出与既有采样、usage、原生项提交边界保持。当前检查的进程清理仍由唯一 turn owner 完成。

确定拒绝全部工具后追加时，当前 turn SHALL 继续使用已提交窗口和配对完整的工具输出。可按该窗口自身预算触发现有自动压缩。拒绝正文和附件不得成为候选。取消或运行结果未知时 SHALL 禁止下一次远端请求。信任审批、进程执行和必要清理均计入活动 worker，活跃期间沿用现有会话交接的忙状态。

#### Scenario: Close a poisoned correction check on recovery

- **WHEN** Stop 的反馈已接纳，随后用户纠正门禁的结果写入不确定
- **THEN** 当前 owner 禁止继续采样。恢复在本地验证和收口门禁后才按既有契约补偿反馈

#### Scenario: Continue after rejecting all ordinary steering

- **WHEN** 原生工具结果已提交，当前安全点的追加全部确定被拒，旧回合及 writer 健康
- **THEN** 追加逐项返还，下一次请求仍只使用已提交的合法窗口与工具结果
