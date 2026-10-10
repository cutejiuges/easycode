## MODIFIED Requirements

### Requirement: Runtime commands are versioned and strongly typed

内部输入控制面 SHALL 为 `submit_input`、`interrupt` 和 `shutdown` 提供唯一当前的强类型 command、专属 constructor 和 validator；内部 command 不携带 format revision，也不存在按 revision 选择的 decoder。每条 command MUST 携带非空 request identity；`submit_input` MUST 携带合法 UTF-8、去除外围空白后非空且不超过 4 MiB 的文本，`interrupt` MUST 携带预期活动 `turn_id`，`shutdown` MUST NOT 接受无关 payload。

command kind 与 typed payload 不匹配、非法 ID、非法 UTF-8 和不适用于该 kind 的字段 MUST 在产生排队、turn ID、Session 写入或 Provider 副作用前失败。command、提交结果与拒绝原因 MUST NOT 使用或暴露无约束 `any`。外部 headless stream-json 的 `version: 1` 由 headless adapter 严格解码后投影为内部 command，不得把外部 wire version 复制进 Runtime 核心类型。

#### Scenario: Accept a valid submit command
- **WHEN** 宿主提交具有唯一 request identity 和合法文本的当前 `submit_input`
- **THEN** 控制面返回强类型提交结果，并且后续生命周期可以通过同一 request identity 关联

#### Scenario: Reject an invalid command without side effects
- **WHEN** command 包含未知 kind、非法 target turn、空白文本、超限文本或 kind/payload 不匹配
- **THEN** 控制面返回稳定英文 `invalid_command` 或 `invalid_input` 错误
- **THEN** command 不进入队列、不分配 turn ID、不写 Session 且不调用 Provider

#### Scenario: Adapt the current external protocol once
- **WHEN** headless adapter 成功解码一个 stream-json `version: 1` command
- **THEN** adapter 构造不含 version 的内部强类型 command
- **THEN** Runtime 不暴露外部 JSON decoder 或第二套版本化 command DTO
