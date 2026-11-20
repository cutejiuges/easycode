## ADDED Requirements

### Requirement: Ordinary sample usage agrees with stored native evidence

Anthropic Messages 与 OpenAI Responses 的在线恢复、继续会话和离线预览 SHALL 对每个已持久化普通 sample 核对其真实 turn 内的相邻 `sample_usage`。适配器 MUST 按当前原生 codec 解码 raw usage，并用自身既有归一规则确定五个指标的状态和值。已知零、unknown 和 not_applicable 必须分别比较。缺失、错配或不相等 MUST 拒绝会话，不用任一证据覆盖另一份。

对账 SHALL 在已完成 journal 结构、关联和身份校验之后，并在 repair、writer 接管、补偿或模型请求之前完成。预览沿用无凭据、无 Provider 请求的只读路径。独立摘要 usage 与普通 sample 分开。计量证据不得参与请求编译或缓存 fingerprint。

#### Scenario: Reject contradictory OpenAI sample accounting before repair
- **WHEN** OpenAI 日志的两份普通 sample 计量各自合法且 checksum 有效，但状态或数值不一致，文件末尾还存在半行
- **THEN** 离线预览和在线恢复均拒绝该日志，在线报告 session_corruption，journal bytes 保持原样且没有 writer、补偿或 Provider 请求

#### Scenario: Keep valid metric states and continuation stable
- **WHEN** 两家 Provider 的合法普通 sample 包含已知零和缺失的指标
- **THEN** 已知零和原生缺失分别通过 `known:0` 与 `unknown` 对账，把缺失标记为 `not_applicable` 时拒绝
- **THEN** 合法恢复与不中断路径的下一请求 canonical bytes、原生顺序和缓存 fingerprint 一致
