## ADDED Requirements

### Requirement: User can continue the most recent compatible root Session

CLI SHALL 提供显式布尔参数 `--continue`，在进入 TUI、创建新 Session 或发起 Provider 请求前协调 Session Catalog，并从当前规范化 `creation_cwd` 中选择 Provider family、wire 和 model 与当前配置完全一致的最近 root Session。候选 MUST 按最后 committed record 的 `updated_at` 降序、canonical thread ID 降序确定；`--continue` 不得使用文件 mtime 排序，不得解析 symlink 获取另一个 cwd，也不得在本切片中运行 Git 或扩展到其他 worktree。

`--continue` 与 `--resume <thread-id>` MUST 互斥；同时指定 SHALL 作为 CLI 用法错误在读取或修改 Session、打开 Catalog、加载 Provider 配置或调用 Provider 前以退出码 `2` 失败。未找到兼容候选时 SHALL 返回安全英文 `session_not_found`，不得创建替代 Session；存在其他 cwd 或不兼容 family/wire/model 的 Session 不构成候选。

Catalog 只负责选择 thread ID。选择后应用 MUST 调用与显式 `--resume` 相同的路径，重新取得目标 journal 的连续 exclusive lease，重新执行 Loader、ReplayPlanner、配置兼容检查、Provider 恢复、interrupted-tail 补偿和 writer transfer；Catalog row 不得替代任何恢复校验。若已选择目标当前 busy，应用 SHALL 返回 `session_busy` 而不得静默选择更旧 thread；若 reconciliation 遇到无法分类的未索引 busy journal，自动选择 SHALL 以 `session_busy` 失败而不是假定它与当前选择无关。

#### Scenario: Continue the latest compatible Session

- **WHEN** 当前 cwd 中存在多个可恢复 root Session，且至少两个与当前 family、wire、model 兼容
- **THEN** `--continue` 选择 durable `updated_at` 最新的兼容 thread，并通过现有 resume 路径恢复它

#### Scenario: Ignore another cwd and incompatible provider configuration

- **WHEN** 全局最近 Session 位于不同 `creation_cwd`，或其 family、wire、model 任一项与当前配置不同
- **THEN** `--continue` 不选择该 Session，并继续在精确匹配候选中确定最近 thread

#### Scenario: Resolve an equal-time tie

- **WHEN** 两个兼容候选具有相同的最后 committed timestamp
- **THEN** `--continue` 确定性选择 canonical thread ID 较大的候选

#### Scenario: Reject continue when no compatible Session exists

- **WHEN** 当前 cwd 中没有可完整验证且与当前配置兼容的 root Session
- **THEN** 应用在创建 Session、进入 TUI 或调用 Provider 前返回 `session_not_found`

#### Scenario: Reject a selected busy Session

- **WHEN** Catalog 选择的最近兼容 thread 已被另一个进程持有
- **THEN** 现有 resume 路径返回 `session_busy`，且应用不回退到更旧 thread、不调用 Provider、不修改目标 journal

#### Scenario: Revalidate after catalog selection

- **WHEN** Catalog 选择完成后目标 journal 在实际 resume 前被替换、损坏或变为不兼容
- **THEN** descriptor-bound resume 重新校验并安全失败，而不是信任陈旧 Catalog row

## MODIFIED Requirements

### Requirement: User can explicitly resume a root thread

CLI SHALL 提供 `--resume <thread-id>` 选择已有 root thread。应用 MUST 在进入 TUI 或发起网络请求前解析标识、定位 journal，并在读取首个 record 或执行任何尾部 repair 前取得该 thread 的跨进程 exclusive lease。load、repair、ReplayPlanner、配置兼容检查、Provider 事务式恢复、interrupted-tail 补偿和后续 writer MUST 使用同一连续所有权窗口；成功恢复后 lease MUST 保持到活动 writer 完成关闭，不得在 load 与续写之间关闭后重新打开 journal。

缺失参数、无效标识、不存在的 thread、非 root thread、损坏或不受支持的 Session MUST 返回安全英文错误，不得静默创建新 Session 或回退为空会话。当目标 thread 已被另一个进程持有时，resume MUST 在读取后 repair、Provider 恢复、TUI 启动或网络请求前以稳定英文 `session_busy` 失败；失败路径 MUST 释放自身临时资源且不得修改 journal bytes、创建替代 Session 或影响现有 owner。

`--resume` MUST 始终使用调用方提供的 thread ID，不得把缺失或无效值解释为 `--continue`。用户未指定 `--resume` 或 `--continue` 时，应用仍 SHALL 创建新的 root Session，而不是隐式选择最近历史。

#### Scenario: Resume an existing root thread

- **WHEN** 用户传入一个存在、完整且未被其他进程占用的 root thread ID
- **THEN** 应用在一个连续 exclusive lease 下加载全部 committed records、恢复原 Session/thread identity 并启动同一 handle 上的 writer

#### Scenario: Reject an unknown thread

- **WHEN** `--resume` 指向不存在或格式无效的 thread ID
- **THEN** 应用在网络请求前失败且不创建替代 Session 文件

#### Scenario: Reject a concurrently active thread

- **WHEN** 另一个进程已经持有目标 root thread 的活动 writer lease
- **THEN** 当前 resume 返回稳定英文 `session_busy` 错误且不进入 TUI、不调用 Provider
- **THEN** 目标 journal 的长度、checksum、尾部状态和全部 bytes 保持不变

#### Scenario: Resume after the previous owner exits

- **WHEN** 先前 owner 正常关闭 writer 或进程终止后，用户再次 resume 同一 thread
- **THEN** 新进程可以取得 lease，完整校验 journal，并从最后一个 committed record 的下一 seq 继续

#### Scenario: Do not implicitly continue

- **WHEN** 用户正常启动 EasyCode 而没有传入 `--resume` 或 `--continue`
- **THEN** 应用创建新的 root Session，而不是自动加载最近历史

### Requirement: Headless startup creates a stable root Session identity

未指定 `--resume` 或 `--continue` 的 `--print` 或 `--json` 调用 SHALL 在 prompt 校验成功后创建新的逻辑 Session 和 root thread，并为本次 turn 生成稳定且格式可验证的身份。headless SHALL 使用与交互式 Chat 相同的 Session metadata、私有 JSONL、exclusive lease、single writer 和 durable turn 语义；输入无效时不得留下空 Session。

#### Scenario: Start a new headless Session

- **WHEN** 用户以有效配置和 prompt 启动 headless 且未指定 `--resume` 或 `--continue`
- **THEN** 应用创建新的 session/root thread identity，并让本次 turn 的全部事件使用该身份
- **THEN** Session metadata 和 journal 不包含连接 secret、prompt 副本之外的诊断副本或 headless 输出事件

#### Scenario: Reject invalid input before Session creation

- **WHEN** headless prompt 为空、非法或超过输入边界
- **THEN** 应用不创建 Session 目录、Catalog、thread journal 或替代 identity

### Requirement: Headless resume continues native history without replaying prior output

显式 `--resume <thread-id>` 或 `--continue` 与 headless 模式组合时，应用 SHALL 在自动选择完成后复用相同的连续 exclusive lease、ReplayPlanner、配置兼容检查、Provider-owned codec 和 interrupted-tail 补偿路径恢复原 Session/thread identity。当前 prompt SHALL 使用恢复后的 Provider-native history 编译请求，并把新 records 追加到原 thread 的下一 seq；headless MUST NOT 从 Catalog、`SemanticHistoryView`、JSONL payload 或 RuntimeEvent 反向构造续写请求。

恢复出的既有 transcript MUST NOT 作为本次 `--print` 文本或 `--json` 事件重新输出。JSON 模式 SHALL 只输出一次标记 `resumed=true` 的 `thread.started` 和本次新 turn 的事件；文本模式 SHALL 只输出本次新 turn 的最终 assistant 文本。未指定 `--resume` 或 `--continue` 时不得隐式选择最近 Session。

#### Scenario: Resume a thread in text mode

- **WHEN** 用户使用 `--print --resume <thread-id>` 提交下一条 prompt
- **THEN** Provider 使用已恢复的原生历史续写，records 追加到同一 thread
- **THEN** stdout 只包含本次 assistant 最终文本，不包含既有用户或 assistant transcript

#### Scenario: Continue a thread in text mode

- **WHEN** 用户使用 `--print --continue` 提交下一条 prompt
- **THEN** 应用自动选择最近兼容 thread，并使 stdout 只包含本次 assistant 最终文本

#### Scenario: Resume a thread in JSON mode

- **WHEN** 用户使用 `--json --resume <thread-id>` 提交下一条 prompt
- **THEN** 第一条事件是携带原 session/thread ID 且 `resumed=true` 的 `thread.started`
- **THEN** 后续只包含本次新 turn 的事件，不重放既有 turn 或 Provider-native item

#### Scenario: Continue a thread in JSON mode

- **WHEN** 用户使用 `--json --continue` 提交下一条 prompt
- **THEN** 第一条事件携带自动选择出的原 session/thread ID 且 `resumed=true`
- **THEN** 后续只包含本次新 turn 的事件，不输出 Catalog metadata 或既有 transcript

#### Scenario: Reject an incompatible headless resume

- **WHEN** headless 显式或自动恢复的目标缺失、被占用、损坏，或当前 family/wire/model 不兼容
- **THEN** 应用在新 Provider 请求和新 turn append 前以既有稳定 Session 错误失败
- **THEN** 原 journal bytes 保持符合现有 repair/compensation 契约，不创建替代 Session

