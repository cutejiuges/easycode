## ADDED Requirements

### Requirement: Preserve validated user text across hosts

Provider SHALL 将 TUI、一次性 headless 和 stream-json 提供的合法非空白用户正文原样放入自身原生用户消息，不得 trim 外围空白、规范化 Unicode 或重新解释文本。空白验证、输入上限及已有 wire shape SHALL 保持不变。TUI 在编辑阶段的换行规范化不扩展到其他宿主。提交与恢复 SHALL 保留相同正文及原生 item 顺序；正文变化可以改变完整请求 fingerprint，UI 状态不得污染稳定前缀。

#### Scenario: Preserve indentation and trailing whitespace
- **WHEN** 任一宿主提供含首行缩进、Tab、尾部空格和尾换行的合法用户文本
- **THEN** 原生用户消息及模型请求包含完全相同的正文
- **THEN** 不增加宿主专属 wire、兼容 reader 或协议版本

#### Scenario: Preserve restored request equivalence
- **WHEN** 已提交上述文本后从 Session 恢复并提交下一输入
- **THEN** 后续请求 canonical bytes、原生顺序和 fingerprint 与 uninterrupted 路径一致

#### Scenario: Reject blank input before network effects
- **WHEN** 输入只有空白字符
- **THEN** 原有验证仍拒绝请求，不发起网络或生成原生提交
