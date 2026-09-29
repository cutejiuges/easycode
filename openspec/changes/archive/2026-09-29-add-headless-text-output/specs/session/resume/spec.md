## ADDED Requirements

### Requirement: Headless startup creates a stable root Session identity

未指定 `--resume` 的 `--print` 或 `--json` 调用 SHALL 在 prompt 校验成功后创建新的逻辑 Session 和 root thread，并为本次 turn 生成稳定且格式可验证的身份。headless SHALL 使用与交互式 Chat 相同的 Session metadata、私有 JSONL、exclusive lease、single writer 和 durable turn 语义；输入无效时不得留下空 Session。

#### Scenario: Start a new headless Session

- **WHEN** 用户以有效配置和 prompt 启动 headless 且未指定 `--resume`
- **THEN** 应用创建新的 session/root thread identity，并让本次 turn 的全部事件使用该身份
- **THEN** Session metadata 和 journal 不包含连接 secret、prompt 副本之外的诊断副本或 headless 输出事件

#### Scenario: Reject invalid input before Session creation

- **WHEN** headless prompt 为空、非法或超过输入边界
- **THEN** 应用不创建 Session 目录、thread journal 或替代 identity

### Requirement: Headless resume continues native history without replaying prior output

显式 `--resume <thread-id>` 与 headless 模式组合时，应用 SHALL 复用现有连续 exclusive lease、ReplayPlanner、配置兼容检查、Provider-owned codec 和 interrupted-tail 补偿路径恢复原 Session/thread identity。当前 prompt SHALL 使用恢复后的 Provider-native history 编译请求，并把新 records 追加到原 thread 的下一 seq；headless MUST NOT 从 `SemanticHistoryView`、JSONL payload 或 RuntimeEvent 反向构造续写请求。

恢复出的既有 transcript MUST NOT 作为本次 `--print` 文本或 `--json` 事件重新输出。JSON 模式 SHALL 只输出一次标记 `resumed=true` 的 `thread.started` 和本次新 turn 的事件；文本模式 SHALL 只输出本次新 turn 的最终 assistant 文本。未指定 `--resume` 时不得隐式选择最近 Session，headless 本切片也不得把该行为解释为 `--continue`。

#### Scenario: Resume a thread in text mode

- **WHEN** 用户使用 `--print --resume <thread-id>` 提交下一条 prompt
- **THEN** Provider 使用已恢复的原生历史续写，records 追加到同一 thread
- **THEN** stdout 只包含本次 assistant 最终文本，不包含既有用户或 assistant transcript

#### Scenario: Resume a thread in JSON mode

- **WHEN** 用户使用 `--json --resume <thread-id>` 提交下一条 prompt
- **THEN** 第一条事件是携带原 session/thread ID 且 `resumed=true` 的 `thread.started`
- **THEN** 后续只包含本次新 turn 的事件，不重放既有 turn 或 Provider-native item

#### Scenario: Reject an incompatible headless resume

- **WHEN** headless resume 的目标缺失、被占用、损坏，或当前 family/wire/model 不兼容
- **THEN** 应用在新 Provider 请求和新 turn append 前以既有稳定 Session 错误失败
- **THEN** 原 journal bytes 保持符合现有 repair/compensation 契约，不创建替代 Session
