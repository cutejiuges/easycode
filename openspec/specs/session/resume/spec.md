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

### Requirement: Resume keeps a descriptor-bound path ownership chain

resume SHALL 从已安全打开并校验的数据根开始，相对于父目录句柄逐级定位目标 journal；目录类型、权限、journal 类型与权限以及 exclusive lease MUST 绑定到实际参与后续 load/repair/append 的句柄。路径组件在检查后被替换 MUST NOT 改变本次恢复所读取、修复或续写的对象。

#### Scenario: Replace a directory component during resume

- **WHEN** 测试在 resume 路径解析期间确定性地把日期目录替换为指向数据根外的 symlink
- **THEN** resume 在 load、repair、Provider 恢复和网络请求前失败，或继续使用替换前已取得的安全目录句柄
- **THEN** 数据根外的目标不会被读取、截断或追加

#### Scenario: Replace the journal before lease acquisition

- **WHEN** 测试在 journal 路径检查与打开边界把目标替换为 symlink 或非普通文件
- **THEN** resume 不会在替换目标上取得 lease、执行 repair 或启动 writer
- **THEN** 应用返回安全英文 Session 错误

#### Scenario: Continue on the leased file handle

- **WHEN** resume 已安全打开 journal 并取得 exclusive lease
- **THEN** load、可允许的尾部 repair、ReplayPlan、writer transfer 和后续 append 使用同一连续句柄所有权链
- **THEN** 路径随后被重命名或替换不会把续写切换到另一个文件

### Requirement: Resume uses current startup project instructions as external context

新进程通过 `--resume` 或 `--continue` 恢复 thread 时，应用 SHALL 使用当前进程启动工作目录发现的单一项目指令快照编译后续请求。该快照 MUST 独立于 Session `creation_cwd`、Catalog row、JSONL records、恢复出的 Provider native history和 TUI transcript；应用不得自动切换到创建目录读取旧项目指令，也不得把当前绝对 cwd 或项目根直接注入请求或 cache fingerprint。

当前快照与原进程快照相同时，恢复后请求 MUST 满足 native history 恢复等价契约。当前快照不同或不存在时，应用 SHALL 保持既有 records 和 restored native history 不变，只更新请求时的临时项目上下文及其内容派生 cache 输入；本次变化不得成为 Provider 配置不兼容、Session migration 或 journal 重写的理由。

#### Scenario: Resume from a different cwd with the same instruction snapshot

- **WHEN** 用户从不同绝对 cwd 显式恢复 thread，但当前发现得到相同的规范化项目指令快照
- **THEN** 应用不 `chdir`、不修改 Session metadata，并继续使用恢复出的 native history
- **THEN** 绝对 cwd 差异不改变请求 canonical bytes 或 cache fingerprint

#### Scenario: Resume with changed project instructions

- **WHEN** 用户恢复兼容 thread，而当前启动上下文发现的项目指令与原进程不同
- **THEN** 应用不改写既有 JSONL records、不伪造 native history，也不把变化判为 Provider 不兼容
- **THEN** 下一请求只使用当前快照生成一个临时项目指令上下文

#### Scenario: Fail discovery before resumed network activity

- **WHEN** 当前启动上下文的项目指令发现因不安全文件、读取失败或非法 UTF-8 失败
- **THEN** resume 或 continue 在进入可提交状态和发起 Provider 请求前失败
- **THEN** 目标 journal bytes 与恢复出的 native history 保持不变

### Requirement: Resume locally reconciles an unfinished Tool Loop from durable ledger facts

在取得并持续持有目标 thread exclusive lease、完成当前 Loader/ReplayPlanner、Provider 配置兼容检查和事务式 native history 恢复后，resume SHALL 检查活动 tool turn 的完整有序 call group，并在向宿主暴露可用 Session 或接受新输入前完成确定性本地 reconciliation。

- ready 但未 started 的 invocation SHALL 按 call index 使用原 invocation ID 追加 `cancelled/session_interrupted_before_execution` result。
- result 已 durable 但对应 tool-output native entry 尚未 durable 时，系统 SHALL 从保存的 preview bytes 重建完整有序 output commit，不得重读或重新搜索文件。
- execution started 但 result 缺失时，系统 MUST 按 call index 追加 `outcome_uncertain` result，禁止自动重试。
- tool-output entry 已 durable 时，系统 MUST NOT 重复追加 output。
- 所有缺失 output 补齐后，系统 MUST 以且仅以一个 `turn_failed` 关闭旧 turn。
- 最终 sample 已 durable 完成时，resume 不得重新执行、追加 output 或重发 sample。

reconciliation MUST 保持原 call index、capability 与 Provider call identity，只允许纯内存 Provider output 编码和 Session append/Sync；MUST NOT 调用 Read、Glob、Grep executor，MUST NOT 发起 Provider 请求，也 MUST NOT 自动继续下一 sample。每次 append/Sync 失败 MUST 使 resume 失败并阻止新 turn 副作用。旧开发 schema、未知 record/canary 或混合 revision journal MUST 在 reconciliation、repair 和 Provider 恢复前 fail closed，不得选择兼容 reader。

#### Scenario: Locally cancel an ordered ready group
- **WHEN** journal 在含 Glob、Grep、Read 的 ready batch 后结束且没有 started
- **THEN** resume 以原 invocation 和 Provider call identity 按 call index 持久化三个 cancelled results 及 matching outputs
- **THEN** 所有 executor 与 Provider stream 调用次数均为零

#### Scenario: Locally cancel a ready Read
- **WHEN** journal 在单个 Read ready 后结束且没有 execution-start
- **THEN** resume 使用原 identity 持久化 cancelled result 与 matching output，且 Read executor 调用次数为零

#### Scenario: Resume durable parallel results
- **WHEN** 一个并行执行组的全部 result facts 已按 call index durable 但 tool-output native commit 缺失
- **THEN** resume 复用逐字节相同的有序模型 previews 提交 output且所有 executor 调用次数不增加
- **THEN** resume 追加一个 `turn_failed` 且不发起 Provider 请求

#### Scenario: Resume a durable Read result
- **WHEN** Read result 已 durable 但 tool-output native commit 缺失
- **THEN** resume 复用逐字节相同的 preview 提交 output，且 Read executor 调用次数不增加

#### Scenario: Fail closed on uncertain searches
- **WHEN** Glob 与 Grep 的 execution-start 已 durable 但 results 缺失
- **THEN** resume 不调用搜索，按 call index durable 记录 `outcome_uncertain` 补偿并停止自动推进该 turn

#### Scenario: Fail closed on an uncertain Read
- **WHEN** Read execution-start 已 durable 但 result 缺失
- **THEN** resume 不调用 Read，durable 记录 `outcome_uncertain` 并停止自动推进该 turn

#### Scenario: Do not duplicate a durable output commit
- **WHEN** calls、results 和 tool-output entry 均完整 durable 但尚无 terminal
- **THEN** resume 只追加一个 `turn_failed`，不重复 output且不启动下一 sample

#### Scenario: Reject a superseded development journal
- **WHEN** resume 目标使用本变更前的 Tool input/result revision shape 或 optional record envelope
- **THEN** 系统在 repair、append、executor、Provider 网络或 SQLite 写入前失败
- **THEN** journal bytes 保持不变且不加载旧 decoder

#### Scenario: Continue on the next user input
- **GIVEN** unfinished tool turn 已完成本地补偿
- **WHEN** 用户提交下一次输入
- **THEN** Provider 请求保持 tool call/output pairing 和原生 item 顺序
- **THEN** canonical bytes 与 fingerprint 等于从同一 reconciled history 不经重启构造的请求

#### Scenario: Preserve ownership during reconciliation
- **WHEN** 另一个进程竞争同一 unfinished tool thread
- **THEN** 只有持有连续 exclusive lease 的进程可以 reconcile，竞争者在读取后 repair、Session append 或任何外部副作用前以 `session_busy` 失败
