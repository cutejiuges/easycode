# session/resume Specification

## Purpose

定义 root Session 的创建和显式 thread resume 行为，使用户能在进程重启后安全恢复同一 Provider 的可见对话与原生续写状态，同时明确拒绝不兼容配置、损坏历史和隐式 Provider 转换。

## Requirements

### Requirement: Interactive chat creates a stable root Session identity

未指定 resume 的交互式 Chat SHALL 为逻辑 Session、root thread 和每个 turn 生成全局唯一且格式可验证的标识。Session 元数据 SHALL 记录 root thread、创建时间、Provider family、wire、model、schema revision 和创建时规范化的绝对 `creation_cwd`，但 MUST NOT 记录 API key、base URL、Authorization、Cookie、敏感 Header 或配置文件路径。

默认 thread journal SHALL 位于 Session 数据根的日期目录中，并以 thread ID 定位；Session 数据根必须可在测试和宿主装配时显式注入，不得依赖可变全局状态。

`creation_cwd` 只作为 Session 与未来可重建索引的非敏感业务元数据。本切片 MUST NOT 根据该字段自动切换进程工作目录、把 cwd 不同解释为 Provider 不兼容，或用它替代后续工具阶段定义的 canonical workspace root 与路径权限边界。

#### Scenario: Start a new interactive Session

- **WHEN** 用户使用有效配置启动普通交互模式且未指定 resume
- **THEN** 应用创建彼此关联的 session ID 与 root thread ID，并让后续 turn 使用该身份
- **THEN** 持久化元数据只包含恢复所需的非敏感配置摘要和创建 cwd

#### Scenario: Keep creation cwd as metadata only

- **WHEN** 用户从不同 cwd 显式恢复一个当前文本 Session
- **THEN** 应用不因 cwd 不同而自动 `chdir` 或拒绝 Provider 历史恢复
- **THEN** Session 中的 cwd 不进入 Provider request、cache fingerprint 或连接配置

#### Scenario: Keep request cache inputs independent of Session location

- **WHEN** 相同对话使用不同 Session 路径、时间戳或日志设置运行
- **THEN** 这些动态字段不进入 Provider request 的稳定前缀或 cache fingerprint

### Requirement: User can explicitly resume a root thread

CLI SHALL 提供 `--resume <thread-id>` 选择已有 root thread。应用 MUST 在进入 TUI 或发起网络请求前解析标识、定位并完整校验 journal；缺失参数、无效标识、不存在的 thread、非 root thread、损坏或不受支持的 Session MUST 返回安全英文错误，不得静默创建新 Session 或回退为空会话。

本切片 MUST NOT 把 `--resume` 解释为 `--continue`，也不得自动选择“最近一次”Session。

#### Scenario: Resume an existing root thread

- **WHEN** 用户传入一个存在且完整的 root thread ID
- **THEN** 应用加载该 thread 的全部 committed records 并恢复原 Session/thread identity

#### Scenario: Reject an unknown thread

- **WHEN** `--resume` 指向不存在或格式无效的 thread ID
- **THEN** 应用在网络请求前失败且不创建替代 Session 文件

#### Scenario: Do not implicitly continue

- **WHEN** 用户正常启动 EasyCode 而没有传入 `--resume`
- **THEN** 应用创建新的 root Session，而不是自动加载最近历史

### Requirement: Resume requires compatible Provider configuration

恢复 SHALL 要求当前配置的 Provider family、wire 和 model 与 Session 元数据完全一致。API key 与 base URL SHALL 仅从当前配置加载并允许更新，不得从 Session 恢复；family/wire/model 不匹配 MUST 在 Provider 请求前以明确错误拒绝。本切片 MUST NOT 转换 native history、伪造 opaque reasoning 或自动创建跨 Provider/model fork。

#### Scenario: Resume with refreshed credentials

- **WHEN** 当前配置使用与 Session 相同的 family/wire/model，但提供新的 base URL 或 API key
- **THEN** 应用使用当前连接配置和已恢复 native history继续会话
- **THEN** 新连接 secret 不会写入 Session

#### Scenario: Reject a Provider mismatch

- **WHEN** 当前配置的 family、wire 或 model 与 Session 元数据不同
- **THEN** resume 在网络请求前失败且原 journal 保持不变
- **THEN** 系统不尝试把原生历史转换为另一 Provider 或模型

### Requirement: Resume replays semantic history and continues native history

恢复成功后，应用 SHALL 使用经过语义回放校验的 `provider_native_commit` 调用 Provider codec 重建 native history，并单独使用 `HistoryProjector` 生成 TUI 初始 transcript。TUI MUST NOT 解析 JSONL 中的 Provider payload。后续用户输入 SHALL 继续使用恢复后的 native history 编译请求，并向原 thread 追加更大的 seq；不得重写、复制或重新编号既有 records。

#### Scenario: Replay visible transcript

- **WHEN** 一个包含多个成功 turn 的 thread 被恢复
- **THEN** TUI 在接受新输入前按原顺序显示各 turn 的用户和 assistant 可见文本
- **THEN** transcript 不显示 thinking、signature、encrypted content、usage 或未知原生扩展

#### Scenario: Continue after resume

- **WHEN** 用户在恢复后的 TUI 提交下一条输入并成功完成
- **THEN** Provider 使用恢复后的 native history 续写
- **THEN** 新 Session records 追加到同一 thread 且 seq 延续既有最大值

### Requirement: Interrupted and failed turns cannot become fabricated history

resume SHALL 只把 durable 且通过语义回放校验的 `provider_native_commit` 恢复为 Provider 历史。仅含 `turn_started`、`turn_failed`、未完成 batch 或部分流式文本的 turn MUST NOT 被补全为成功提交；当前文本切片不要求重放失败 turn 的完整 transcript。尾部修复结果 SHALL 可诊断地报告，但不得把修复内容或原生 payload 输出到 TUI 错误中。

当当前文本 revision 的 journal 以完整 committed `turn_started` 结束时，应用 SHALL 在完成全量 load/repair、配置兼容检查和事务式 Provider 恢复之后、向 TUI 暴露可用 Session 或接受新 turn 之前，使用原 turn identity durable 追加唯一的 required `turn_failed`，其机器可读 code 为 `session_interrupted`。该补偿记录只收口 lifecycle，不得加入 native history、重放旧输入或调用 Provider；append/Sync 失败 MUST 使 resume 失败并阻止后续副作用。配置不匹配或其他恢复校验失败时不得写入该补偿记录。

#### Scenario: Resume after a cancelled turn

- **WHEN** 最后一个 turn 在 Provider 原生 commit 前被取消或失败
- **THEN** 恢复后的 native history 与该 turn 开始前相同
- **THEN** 下一次请求不包含失败 turn 的用户输入或部分 assistant 文本

#### Scenario: Resume after repairing a tail

- **WHEN** loader 修复了最后一个未完成 batch
- **THEN** 应用只恢复此前 committed records，并以安全摘要报告 Session 已修复

#### Scenario: Close a committed interrupted turn before reuse

- **WHEN** 合法 journal 的最后状态是只有 `turn_started` 的 interrupted tail
- **THEN** 应用恢复此前 committed native history并在网络请求前 durable 追加 `turn_failed(code=session_interrupted)`
- **THEN** 新 turn 只能在该失败边界 Sync 成功后开始，旧用户输入不会被自动重发

#### Scenario: Fail while closing an interrupted turn

- **WHEN** interrupted-tail 补偿记录无法 append 或 Sync
- **THEN** resume 返回安全英文 session 错误且不进入可交互状态、不调用 Provider
