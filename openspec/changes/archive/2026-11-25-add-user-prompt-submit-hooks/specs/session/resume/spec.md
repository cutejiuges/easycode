## ADDED Requirements

### Requirement: Resume settles prompt checks before ongoing turn compensation

恢复 SHALL 在连续 exclusive lease 下，先完成完整前缀、合法尾批和 Provider 原生语义验证，再 repair 和本地补偿。离线预览及分支前缀验证共用检查事实的语义验证。尚未关闭的 `UserPromptSubmit` 必须在该 turn 的 Stop 反馈、Tool Loop 或中断终态前关闭并 Sync。已关闭检查、实际执行决定和完整原生交付保持原记录。恢复只结算缺失事实，不请求信任、不启动脚本、不发送网络请求，也不推断尚未写入的用户正文或授权。

分支切点 SHALL 只继承已关闭的检查事实、占用过的 InputID 和真实已提交的原生输入。旧记录不给新实例授予信任。分支或恢复实例随后领取的新输入按自身冻结配置和信任正常检查。

#### Scenario: Recover an interrupted prompt check before Stop feedback

- **WHEN** durable Stop 决定尚欠反馈交付，且真实纠正的 `UserPromptSubmit` 前缀在启动事实后中断
- **THEN** 本地结算检查后仅交付原冻结反馈并结束旧 turn。未保存纠正不进入原生历史，再次恢复不重复任何已 Sync 事实

#### Scenario: Verify inherited checks and trust the next input independently

- **WHEN** 从含已拒及已提交输入的已关闭前缀创建并进入分支
- **THEN** 验证并继承原有标识与历史，创建和切换期间没有检查运行。下一条新输入须按分支实例的当前信任规则审查
