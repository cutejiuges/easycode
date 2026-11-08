## ADDED Requirements

### Requirement: Planned PostToolUse trust precedes mutation approval and effects

命中 PostToolUse 的调用 SHALL 在原工具候选准备、普通 approval、grant 使用或 execution_started 前完成冻结计划与全部独立信任检查。事件和 feedback 策略 MUST 属于授权身份；审查明确后置运行、只读沙箱及 model 模式内容发送/保存。Hook 信任不得批准原工具，普通 allow/session grant 不得授权 Hook 或模型披露。

headless 的合法匹配 Post 调用 SHALL 在平台执行前检与候选准备前返回 hook_trust_required，零脚本、零工具、零 stdin 等待；无匹配保持 approval_required。取消、错配或旧信任响应不产生授权、工具执行或反馈事实。

#### Scenario: Deny a post-only declaration before modification
- **WHEN** Write 只有 PostToolUse 且用户拒绝该声明的信任
- **THEN** 原 Write 不准备候选、不执行，调用得到配对信任拒绝；Post 计划明确 skipped

#### Scenario: Keep ordinary command approval
- **WHEN** Post 声明全部已信任且 Exec 准备合法
- **THEN** Exec 仍需普通单次批准，不因 Post trust 得到命令 grant

#### Scenario: Reject post hooks in every headless entry
- **WHEN** print、一次性 JSON 或 stream-json 的 mutation 仅匹配 PostToolUse
- **THEN** 返回 hook_trust_required、零候选/工具/Hook 启动，外部输出无交互信息
