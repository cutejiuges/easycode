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

应用 SHALL 从经过完整 replay 验证的命令 facts 单独恢复命令历史投影和当前 thread 授权 descriptor 集合，不改变 HistoryProjector 或 native request。恢复不得为重建请求读取 artifact；文件缺失仅在按需查看时报告。

#### Scenario: Resume command details without stored files
- **WHEN** 包含历史 Exec 的 thread 恢复时对应输出文件缺失
- **THEN** 历史命令、原 preview 和状态仍可展示，续写请求保持相同；按需读取显示 unavailable

### Requirement: Interrupted and failed turns cannot become fabricated history

resume SHALL 只把 durable 且通过语义回放校验的 `provider_native_commit` 恢复为 Provider 历史。仅含 `turn_started`、`turn_failed`、未完成 batch 或部分流式文本的 turn MUST NOT 被补全为成功提交；当前文本切片不要求重放失败 turn 的完整 transcript。尾部修复结果 SHALL 可诊断地报告，但不得把修复内容或原生 payload 输出到 TUI 错误中。

当当前 journal 以完整 committed turn_started、initial 用户输入 batch 或已闭合工具组后的 steering 输入 batch 结束且没有 terminal 时，应用 SHALL 在完成全量 load/repair、配置兼容检查和事务式 Provider 恢复之后、向 TUI 暴露可用 Session 或接受新 turn 之前，使用原 turn identity durable 追加唯一的 required `turn_failed`，其机器可读 code 为 `session_interrupted`。该补偿记录只收口 lifecycle，不得加入 native history、重放旧输入或调用 Provider；append/Sync 失败 MUST 使 resume 失败并阻止后续副作用。配置不匹配或其他恢复校验失败时不得写入该补偿记录。

#### Scenario: Resume after a cancelled turn

- **WHEN** 最后一个 turn 在任何用户输入或 sample native commit 前被取消或失败
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

恢复 SHALL 保留失败 turn 中已 durable 的用户输入和成功 sample，并以真实 turn 归属投影。内存 queued/claimed、草稿和返还托盘不属于恢复事实，不自动重放；input-only tail 在新输入前按原 turn identity 追加唯一 session_interrupted 失败终态。

#### Scenario: Resume committed steering without issuing a request
- **WHEN** steering 输入 Sync 后进程退出且没有下一 sample
- **THEN** 恢复保留用户文本，本地收口旧 turn，零 Provider/工具调用；用户之后提交的新任务携带完整原生历史

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

- Patch ready但没有approval fact的invocation SHALL按call index使用原identity追加`cancelled/session_interrupted_before_approval` result。
- approval allow已durable但execution未started的Patch SHALL追加`cancelled/session_interrupted_before_execution` result；不得恢复grant或复用批准。
- ready但未started的自动允许Read/Glob/Grep/ReadArtifact SHALL追加`cancelled/session_interrupted_before_execution` result。
- result已durable但对应tool-output native entry尚未durable时，系统 SHALL从保存的preview bytes和wire kind重建完整有序output commit，不得重读、重新搜索或修改文件。
- execution started但result缺失时，系统 MUST按call index追加`outcome_uncertain` result，禁止自动重试；Patch不得根据当前文件内容猜测执行结果。
- tool-output entry已durable时，系统 MUST NOT重复追加output。
- 所有缺失output补齐后，系统 MUST以且仅以一个`turn_failed`关闭旧turn。
- 最终sample已durable完成时，resume不得重新执行、重新询问、追加output或重发sample。

reconciliation MUST保持原call index、capability、Provider call identity、wire kind和已durable candidate fingerprint，只允许纯内存Provider output编码和Session append/Sync；MUST NOT调用任一tool executor、候选准备器或approval presenter，MUST NOT访问Patch目标、恢复进程内session grant、发起Provider请求或自动继续下一sample。每次append/Sync失败 MUST使resume失败并阻止新turn副作用。旧开发schema、未知record/canary或混合revision journal MUST在reconciliation、repair和Provider恢复前fail closed，不得选择兼容reader。

上述常规 ready/approval/started 矩阵 SHALL 在没有未收口 Hook 子状态时适用。有 Hook plan 时 MUST 先消费已验证的冻结计划，且不读取当前 hooks.json、不重新选择 matcher、不请求信任、不调用 Hook runner：plan 尚未开始或仅完成成功前缀时，追加 gate cancelled 与原工具 cancelled/session_interrupted_before_hook；Hook started 无 finished 时，以同 batch 追加 recovery finished(outcome_uncertain, exit unknown, cleanup_confirmed=false)、gate outcome_uncertain 与原工具 cancelled/hook_outcome_uncertain。系统 MUST NOT 将 Hook 不确定冒充目标工具已执行或宣称旧进程已清理。

gate continue 后的未审批/allow 未 tool-started 状态 SHALL 沿用本条常规取消；gate 阻断和 result 已 durable 时直接复用 result；tool started 无 result 则仍按目标工具 outcome_uncertain 补偿。所有缺失 outputs 只由 frozen results 与已收口 Post 的冻结反馈编码，结束旧 turn 后不自动采样。reconciliation 再次中断不得重复已 durable 的 Hook completion、gate、result、outputs 或 terminal。

旧 turn reconciliation SHALL 在当前 Hook 来源 Load/install 之前完成，并始终持有原连续 lease；当前配置只服务未来调用。已删除或损坏配置不得改变历史补偿或恢复 bytes；旧事实不建立当前 trust。完成补偿后新 turn 重新接受当前配置和信任检查。与同一 reconciled history 的不中断路径比较时，双 Provider canonical request、native item 顺序、opaque bytes、context footprint 与 cache fingerprints MUST 相同；旧门禁恢复的配置 loader、信任、进程、工具与 Provider 网络调用次数均为零。


已规划 Post 的恢复 SHALL 区分真实结果与检查状态：原工具未 started/result 时按既有状态补偿并收口 skipped；tool started 无 result 时仍为原工具 outcome_uncertain，零 Post；成功 result 且有对应分支、无 Post started 时保留成功并收口 Post interrupted；Post started 无 finished 时追加 recovery-origin uncertain finished 和对应 Post terminal，不声称旧进程 cleanup 已确认。已 durable 的前缀反馈保持不变，未开始项不执行。部分 finished 后下一项尚未 started 时只追加 cancelled/session_interrupted_before_post_hook terminal，processed/unstarted_from 等于已有 finished 数量，不能生成 uncertain finished；下一项确实 started 未 finished 时，才为该项追加 recovery-origin uncertain finished 并与 uncertain terminal 同批，剩余后缀不伪造执行。已收口 skipped 后缀完全复用其原因和已冻结 suffix_feedback，不重新判断输入或预算。恢复新建的 terminal 明确 origin=recovery、suffix_feedback 为空；只组合已经 durable 的前缀/后缀模型 bytes，不为未开始项补发提示。完整 Post terminal 而 native outputs 缺失时只用真实结果与冻结反馈编码；outputs 已存在时只关闭旧 turn。所有状态均不读当前配置/脚本/artifact，不重建信任，不查询旧 PID，不重跑工具/Hook、不自动采样。第二次恢复不得重复任何已保存事实。

上述 Post 本地恢复 SHALL 同样适用于执行尝试准入后原 error/conflict 已 durable、选择 PostToolUseFailure 的调用，保留原错误与 failure 选择且不执行任何脚本。恢复必须先按同调用的准入/原结果验证 success/failure/none；当前 context、配置、预算和恢复模式不得改变选择。tool_execution_started 只代表执行尝试准入，准入后确定的启动失败仍选 failure；准入前准备错误、原结果 denied/cancelled/outcome_uncertain 为 none。none 不表示原工具没执行。确定失败且存在失败分支、无 Hook started 时只补 recovery cancelled/session_interrupted_before_post_hook terminal；已有 finished 前缀保持原 bytes；只有 Hook started 无 finished 的项补 recovery uncertain，和 terminal 同批。未选中分支没有运行补偿或反馈。

恢复生成 terminal 的 suffix_feedback MUST 为空；既有 terminal 的冻结前缀/后缀完全复用。所有 current 结构验证、双 Provider 事务式恢复和本地 reconciliation 始终持有连续 lease，早于当前来源 Load 与新输入。再次恢复不得重复 result、finished、terminal、outputs 或计费；canonical request/native order/opaque bytes/footprint/cache fingerprint 须等价于同一已补偿历史的不中断路径。

恢复时 route=none SHALL 保持零 Hook started/finished/反馈，并按已验证原事实补适用终态；工具尝试准入但 result 缺失时先沿既有规则补原 outcome_uncertain，再按 none 收口。success/failure 的对应事件未配置时 SHALL 保留该选择并补 recovery skipped/hook_event_unmatched，processed/unstarted_from=0、反馈为空，不要求不存在的分支有后缀。已经 durable 的实时取消 terminal 只复用，不用恢复原因替换：原 error 后当前取消形成的 failure/cancelled 不得重分类成 none。

#### Scenario: Close a patch before approval
- **WHEN** journal在Patch ready durable后、approval fact前结束
- **THEN** resume持久化`session_interrupted_before_approval`结果与matching output并关闭旧turn
- **THEN** approval presenter、候选准备器、Patch executor和Provider stream调用次数均为零

#### Scenario: Locally cancel an ordered ready group
- **WHEN** journal在含Glob、Grep、Read与Patch的ready batch后结束且没有任何started或approval
- **THEN** resume按原call index持久化只读调用的`session_interrupted_before_execution`与Patch的`session_interrupted_before_approval`结果及matching outputs
- **THEN** 所有executor、approval presenter与Provider stream调用次数均为零

#### Scenario: Locally cancel a ready Read
- **WHEN** journal在单个Read ready后结束且没有execution-start
- **THEN** resume使用原identity持久化`session_interrupted_before_execution` result与matching output，且Read executor调用次数为零

#### Scenario: Close an approved patch before execution
- **WHEN** journal包含matching allow fact但没有execution-started
- **THEN** resume持久化`session_interrupted_before_execution`结果且不恢复session grant或修改目标文件

#### Scenario: Fail closed on an uncertain patch
- **WHEN** Patch execution-started已durable但result缺失
- **THEN** resume追加`outcome_uncertain`结果与matching output，不读取目标或自动重试
- **THEN** 旧turn以一个失败终态关闭

#### Scenario: Resume a durable denied result
- **WHEN** deny decision与denied result均已durable但tool-output commit缺失
- **THEN** resume复用冻结preview和原wire kind生成逐字节相同的output
- **THEN** 不重新显示approval也不访问workspace

#### Scenario: Resume durable ordered results
- **WHEN** 异构调用组的全部result facts已按call index durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的有序模型previews提交output且所有executor调用次数不增加
- **THEN** resume追加一个`turn_failed`且不发起Provider请求

#### Scenario: Resume durable parallel results
- **WHEN** 一个并行只读组的全部result facts已按call index durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的有序模型previews提交output且所有executor调用次数不增加
- **THEN** resume追加一个`turn_failed`且不发起Provider请求

#### Scenario: Resume a durable Read result
- **WHEN** Read result已durable但tool-output native commit缺失
- **THEN** resume复用逐字节相同的preview提交output，且Read executor调用次数不增加

#### Scenario: Fail closed on uncertain searches
- **WHEN** Glob与Grep的execution-start已durable但results缺失
- **THEN** resume不调用搜索，按call index durable记录`outcome_uncertain`补偿并停止自动推进该turn

#### Scenario: Fail closed on an uncertain Read
- **WHEN** Read execution-start已durable但result缺失
- **THEN** resume不调用Read，durable记录`outcome_uncertain`并停止自动推进该turn

#### Scenario: Do not duplicate a durable output commit
- **WHEN** calls、approval、results和tool-output entry均完整durable但尚无terminal
- **THEN** resume只追加一个`turn_failed`，不重复output且不启动下一sample

#### Scenario: Reject a superseded development journal
- **WHEN** resume目标使用本变更前的Tool/approval/native payload shape或optional record envelope
- **THEN** 系统在repair、append、executor、Provider网络或SQLite写入前失败
- **THEN** journal bytes保持不变且不加载旧decoder

#### Scenario: Continue on the next user input
- **GIVEN** unfinished tool turn已完成本地补偿
- **WHEN** 用户提交下一次输入
- **THEN** Provider请求保持function/custom/tool-use call/output pairing和原生item顺序
- **THEN** canonical bytes与fingerprint等于从同一reconciled history不经重启构造的请求

#### Scenario: Preserve ownership during reconciliation
- **WHEN** 另一个进程竞争同一unfinished tool thread
- **THEN** 只有持有连续exclusive lease的进程可以reconcile，竞争者在读取后repair、Session append或任何外部副作用前以`session_busy`失败

#### Scenario: Do not repeat interrupted artifact reading
- **WHEN** ReadArtifact ready 或 started 后缺少 result
- **THEN** 恢复沿既有取消/不确定规则补偿，不访问文件或自动重新执行

#### Scenario: Reconcile a partly completed plan offline
- **WHEN** 计划有两项，第一项已 continue，第二项未 started
- **THEN** 不续跑第二项，追加 gate/tool cancelled 和原生配对，关闭旧 turn；所有外部执行调用次数为零

#### Scenario: Record uncertain hook recovery
- **WHEN** Hook started 已 durable 但 finished 缺失，且没有目标 tool started
- **THEN** 同批补偿 Hook 不确定、gate 不确定和目标工具 cancelled/hook_outcome_uncertain；不查询 PID、不读取 workspace 或配置

#### Scenario: Recover after configuration disappears
- **WHEN** Hook result 已 durable 而 native output 缺失，配置随后删除或损坏
- **THEN** 仅用冻结结果补齐原 call slot 的输出，canonical bytes/原生顺序/fingerprint 等于同一已补偿历史的不中断路径

#### Scenario: Repeat recovery after a second crash
- **WHEN** 第一次恢复已提交 Hook 补偿 batch 后再次退出
- **THEN** 第二次恢复复用已提交事实，不重复 finished/gate/result/output，旧 turn 最多一个失败 terminal

#### Scenario: Retain lease while a competing process attempts repair
- **WHEN** 持有者正在补偿 Hook，真实竞争进程请求同一 journal
- **THEN** 竞争者 session_busy，不能读取后 repair 或追加；持有者续写 seq 单调，失败路径 bytes 不变

#### Scenario: Recover after success but before the post check
- **WHEN** result=success 已 durable，Post plan 存在且包含成功分支，而无 Post started
- **THEN** 保留唯一 success，Post 记 interrupted 并补齐输出；原工具及 Hook 执行次数为零

#### Scenario: Recover an uncertain post run
- **WHEN** 成功 result 之后只有 Post started 而无 finished
- **THEN** 只补偿 Post uncertain，保留工具 success 及已完成反馈前缀，关闭旧 turn

#### Scenario: Recover frozen feedback without its source
- **WHEN** 结果、Post finished/completed 和反馈已 durable 而 output 缺失，配置/脚本随后消失
- **THEN** 离线生成相同配对 output、canonical bytes/原生顺序/fingerprint，不重算预算

#### Scenario: Preserve lease and idempotency during repeated recovery
- **WHEN** 真实竞争进程在 Post 补偿期间尝试打开/repair，且持有者补偿后再次崩溃
- **THEN** 竞争者 session_busy、失败 bytes 不变；二次恢复不重复 result/finished/completed/output/terminal，seq 单调

#### Scenario: Recover between post items without uncertainty
- **WHEN** 第 0 项 finished 已保存，第 1 项从未 started 而进程退出
- **THEN** 保留第 0 项及反馈，仅追加 cancelled/session_interrupted_before_post_hook terminal；第 1 项没有补偿 finished 或 cleanup 声明

#### Scenario: Recover a started next item separately from its suffix
- **WHEN** 第 0 项已 finished，第 1 项 started 未 finished，第 2 项尚未 started
- **THEN** 只为第 1 项补 recovery-origin uncertain finished，和 uncertain terminal 同批；第 2 项仍是未启动后缀

#### Scenario: Reuse a durable skipped suffix and closed budget
- **WHEN** skipped terminal 及最终预算已保存而 native outputs 缺失
- **THEN** 原因/前缀/反馈/释放余额不变，离线补输出不重复关闭调用或扣账

#### Scenario: Recover a definite failure before its diagnostic starts
- **WHEN** 原 error/conflict result 与双分支计划均已 durable，失败项未 started
- **THEN** 保留原错误，仅补失败分支 interrupted terminal 和配对 output；工具、Hook、信任和 Provider 网络调用次数为零

#### Scenario: Recover a started failure diagnostic
- **WHEN** 失败分支第 0 项 finished、第 1 项 started 未 finished、第 2 项未开始
- **THEN** 保留第 0 项反馈，只为第 1 项补 uncertain finished 并与 terminal 同批，第 2 项与成功分支无伪运行或新提示

#### Scenario: Recover without failure configuration or artifacts
- **WHEN** 失败反馈已 durable、native output 缺失，原配置/脚本/artifact 已删除
- **THEN** 只用冻结 bytes 补 output，恢复后 canonical request、原生顺序和 fingerprint 等价，不访问消失来源

#### Scenario: Keep a competing process outside failure reconciliation
- **WHEN** 恢复正在追加失败检查补偿，真实子进程竞争同一 journal 并尝试 repair
- **THEN** 竞争者 session_busy、失败路径 bytes 不变；持有者 seq 单调，崩溃释放后再次恢复不重复事实

#### Scenario: Preserve a live cancellation after a durable error
- **WHEN** 原工具准入与 error 已保存，当前取消又保存了 failure/cancelled terminal，之后恢复
- **THEN** 保留原错误、failure 选择和原 terminal/code，只补缺失 outputs；二次恢复不重复 terminal、output 或计费

#### Scenario: Recover an unmatched selected event
- **WHEN** 已准入 success 只有失败分支，或已准入 error/conflict 只有成功分支，而 terminal 缺失
- **THEN** 保留 success/failure 并补 recovery skipped/hook_event_unmatched，零计数/反馈且不要求伪后缀，不借用另一分支

#### Scenario: Do not infer a failed process from attempt admission
- **WHEN** tool_execution_started 已保存但原 result 缺失，进程是否启动或文件是否修改未知
- **THEN** 补原 outcome_uncertain，选择 none 并零诊断脚本，不推测原操作失败或未发生

#### Scenario: Select failure for a saved launch error offline
- **WHEN** 准入后进程尚未启动便形成确定 process_start_failed，原 error result 已保存且存在失败分支
- **THEN** 恢复仍选 failure，仅补检查中断终态和配对输出，不尝试启动原命令或诊断脚本

### Requirement: Resume restores checkpoints and locally closes interrupted compaction

resume/continue SHALL 在连续 exclusive lease、完整 Loader/replay 和 Provider 兼容/原生验证后恢复最新合法 checkpoint 及后续增量，同时保留原始 transcript 和工具详情。只存在 started 的压缩尾部 SHALL 在接收新输入前本地 Sync 唯一 compaction_failed(code=compaction_interrupted)，保持旧窗口；不得自动联网生成摘要、重读文件或执行工具。completion 已完整持久化但原进程未 finalize 时 SHALL 恢复新窗口，不再追加失败或重复 checkpoint。最新 checkpoint 损坏、来源错配或 Provider 不兼容 MUST 拒绝整个恢复，不回退旧 checkpoint，也不写补偿。

当前启动项目指令/工具目录 SHALL 继续作为外部上下文；checkpoint 不恢复旧 cwd、secret、配置、队列或 permission grant。Catalog 继续可从 JSONL 重建，以最新合法 seq/timestamp 表示最近活动，不保存摘要正文。

#### Scenario: Close an interrupted summary locally
- **WHEN** 合法 journal 以 compaction_started 结束
- **THEN** 本地收口失败后暴露旧窗口，Provider/工具/文件重读次数均为零

#### Scenario: Resume after Sync before finalize
- **WHEN** completion 已 durable 而进程在内存切换前退出
- **THEN** 重启直接恢复新窗口，下一请求等于 uninterrupted，操作不重复计量或执行

#### Scenario: Fail compensation safely
- **WHEN** interrupted compaction 的补偿 append/Sync 失败
- **THEN** resume 失败并阻止新输入副作用，不放弃 cleanup ownership

#### Scenario: Validate before compensation
- **WHEN** journal 有未完成 started 但更早 checkpoint 来源错配或当前 Provider 配置不兼容
- **THEN** 拒绝恢复，不写 compaction_failed、不改变已有 journal bytes

#### Scenario: Keep history readable after compaction
- **WHEN** 用户恢复压缩过的会话且旧 artifact 文件缺失
- **THEN** 原历史及冻结工具 preview 仍可显示，续写只需原生窗口；按需读 artifact 才报告 unavailable

### Requirement: Resume validates before tail repair

resume/continue SHALL 使用连续 exclusive lease 只读加载，先完成 replay、Provider 兼容及原生/checkpoint 校验，再显式修复尾部并 Sync，然后启动 writer 和本地 reconciliation。校验失败 MUST 保持包括未完成尾部在内的原始 bytes 不变。Catalog 扫描 MUST NOT 执行尾部修复。修复失败或 durability 不确定 MUST 阻止 writer 和新副作用。

#### Scenario: Reject an incompatible session with a partial tail
- **WHEN** 当前模型与 Session 不兼容且文件末尾存在半行
- **THEN** 拒绝恢复，journal 全部 bytes 不变

#### Scenario: Repair before local reconciliation
- **WHEN** committed 前缀及 Provider 完整验证通过，尾部存在未完成 batch
- **THEN** 同一 lease 下先修复并 Sync，再从正确 next seq 本地收口，不释放后重新打开文件

### Requirement: Picker selection enters the existing explicit recovery boundary

用户从启动选择器确认目标后，系统 SHALL 固定该 canonical thread ID，并重新获得连续 exclusive lease，执行与显式 `--resume` 一致的读取、回放、配置/native 校验、合法修复、补偿和 writer transfer。Catalog 元数据及预览结果 MUST NOT 替代任何检查。目标 busy、删除、损坏或不兼容 MUST 明确失败，禁止选择其他 thread、创建新 Session 或转换 Provider/model。

当前配置 family/wire/model 不匹配时选择器 SHALL 显示差异并阻止提交；实际恢复仍须重新验证。恢复成功 SHALL 不自动执行 Provider sample 或旧工具，等待下一次真实用户输入。旧 `--resume`、`--continue` 和 headless 的选择及输出语义 MUST 保持不变。

#### Scenario: Resume an explicitly selected root thread
- **WHEN** 用户选中存在、兼容且空闲的 root thread
- **THEN** 恢复同一 Session/thread identity、原生历史与可见历史，保持 lease 到活动 writer 清理完成，不自动调用模型

#### Scenario: Race selection with another owner
- **WHEN** 列表/预览后另一进程取得目标 lease
- **THEN** 恢复返回 session_busy，原 journal bytes 不变，不尝试更旧会话

### Requirement: Cross-directory picker recovery requires explicit workspace acknowledgment

选择器中目标 creation cwd 与冻结的启动 cwd 不同时，系统 SHALL 在提交恢复前展示二者，并说明当前项目指令和工具 workspace 使用启动 cwd。确认默认 SHALL 为返回列表，只有明确选择继续才恢复。系统 MUST NOT chdir、自动查找其他 worktree、生成可执行 cd 命令或写剪贴板。该确认不改变既有显式 `--resume` 的跨目录契约。

#### Scenario: Decline a different-directory target
- **WHEN** 用户选中另一目录会话后按 Esc 或接受默认返回选项
- **THEN** 回到列表，未执行恢复、读取项目指令或修改 journal

#### Scenario: Confirm recovery in the current workspace
- **WHEN** 用户明确确认在启动 cwd 继续另一创建目录的会话
- **THEN** cwd 不变，项目指令和工具权限来自当前 workspace，原 metadata/native history 不被重写

### Requirement: Startup recovery exposes cancellation and failure stages honestly

恢复请求 SHALL 具有唯一接收边界。取消早于接收时 MUST 不启动恢复；接收后取消 SHALL 请求终止并等待确定结果和资源清理，不能宣称已撤销持久化变化。恢复成功与取消竞争时，未交付的活动资源 MUST 由原 owner 关闭，禁止泄漏 writer 或启动 Chat。恢复已被接收后 Esc/Ctrl+C SHALL 取消整个启动，确定结果且清理成功返回 130；结果不确定或清理失败返回 1。

发生在可能修改 journal 之前且清理成功的拒绝 SHALL 可返回原列表，保留选择和搜索，等待用户显式重试。已开始合法 repair/compensation 后的失败、Sync 不确定、poisoned 或清理失败 SHALL 结束本次启动，不发布可交互 Chat、不自动重试、不承诺 bytes 未变化。已成功提交的修复/补偿 MUST 保留，不为失败或取消回滚 append-only facts。超时 MUST NOT 放弃资源所有权，必须保持最终完成信号及清理责任。

#### Scenario: Cancel before recovery admission
- **WHEN** 请求尚未接收便被取消
- **THEN** 不读取目标、不获取 writer、不产生恢复副作用

#### Scenario: Cancel after a successful recovery before handoff
- **WHEN** 恢复已产生有效活动资源，但取消先于 Chat 接收完成
- **THEN** 原 owner 关闭 writer/Provider/工具资源并等待完成，UI 不进入 Chat

#### Scenario: Fail after a legitimate durable repair
- **WHEN** 合法修复或中断补偿已发生，后续 artifact/工具/Runtime 装配失败
- **THEN** 保留合法事实并释放资源，以失败结束启动，不将结果描述为只读拒绝或自动重复恢复

#### Scenario: Reject invalid history before repair
- **WHEN** 配置或 native 校验发现不兼容、损坏或旧开发 shape
- **THEN** 修复前拒绝，journal bytes 不变，清理成功后允许用户在列表中另行选择

### Requirement: Resume closes completion checks using only frozen facts

resume SHALL 在连续 exclusive lease、全部当前记录/replay/Provider/checkpoint 校验及合法显式 repair 后，先本地关闭中断摘要，再收口未完成 Stop，最后才安装当前 Hook 来源和接受新输入。不得读取旧配置/脚本/artifact、请求信任、查询 PID、重新执行 Hook/工具或自动请求 Provider。允许纯内存 Provider codec 从已验证事实编码本地交付，不等于允许网络请求。

checks plan 未 started 或仅有成功前缀时 SHALL 同批追加 plan-scope、recovery-origin decided(cancelled, session_interrupted_before_stop_hook) 与 turn_failed，processed 保持前缀；不得给未启动项制造 finished。unavailable plan SHALL 使用冻结的配置/匹配错误收口，不重新推测当前配置。已 started 未 finished 的唯一项 SHALL 与 hook-scope、recovery-origin outcome_uncertain finished、decided(outcome_uncertain) 及 turn_failed 同批，来源指向该真实 ordinal，不宣称旧进程 cleanup 已确认。此前 sample、usage 和事实 bytes 保留。

#### Scenario: Close a check before it starts
- **WHEN** journal 以 sample/usage/checks plan 或合法成功前缀结束
- **THEN** 本地取消并关闭 turn，零 loader/trust/runner/executor/Provider 网络，未运行后缀没有假 finished

#### Scenario: Close an uncertain completion command
- **WHEN** Hook started 已保存而 finished 缺失
- **THEN** 仅该项补 uncertain，决定及 turn_failed 同批，不重跑、不查询进程或伪造 cleanup

#### Scenario: Preserve a frozen source failure
- **WHEN** unavailable plan 已保存而配置后来修好或删除
- **THEN** 恢复仍按原计划安全错误关闭，不重新加载配置或转为通过

### Requirement: Restored stop feedback is never resent or reauthorized

continued 决定 durable 但 native 交付缺失时，resume SHALL 以 prior 决定中最终冻结模型字节和来源纯内存准备 FeedbackOnly，并成功 Sync/finalize 恰好一条 native(hook_feedback)，再补唯一 session_interrupted turn_failed。不得重渲染 reason/wrapper、重新请求授权、增加决定/usage/计费或恢复未保存的用户队列；不得自动把本地交付发送给模型。已保存一记录或三记录交付时 SHALL 恢复实际 native entries 与真实用户关联，不再次交付、返还或重发已保存用户输入。已保存 passed/failed terminal batch 完全复用。

continued 后若有未完成 auto compaction，SHALL 先按原契约本地收口摘要，再完成 FeedbackOnly 交付并关闭 turn；成功 checkpoint 恢复被摘要的旧窗口，再交付冻结原文，不能把尚未保存的反馈或用户输入推定已经包含于摘要。feedback 交付后至其目标 sample 首次请求前的新摘要属于非法事实，不作为可重试恢复窗口；后续普通 sample/工具安全点仍沿既有规则。后续真实工具尾部仍按现有 Tool recovery 配对，不能因为 Stop 扩大执行授权。

下一次真实用户输入 SHALL 从已补偿 native history 续写；相同快照/config 下 canonical request、native 次序、opaque bytes、footprint 和 fingerprints 与同一已补偿历史不中断路径等价。历史信任不安装到新进程，当前来源只服务未来真正调用。纯内存交付编码失败必须失败关闭，durability 不确定必须 poison，不能放弃已接收义务而发布可用 Session。

#### Scenario: Resume after feedback without sampling
- **WHEN** continued 与一记录或三记录 native 交付完整保存后进程退出
- **THEN** 恢复全部真实条目且只关闭旧 turn；Provider 网络/Hook 次数为零，下一真实输入请求与同一事实不中断路径相等

#### Scenario: Recover the post-feedback checkpoint
- **WHEN** continued 后、反馈交付前的旧窗口 auto compaction completion 已保存而进程尚未继续
- **THEN** 恢复新窗口、交付原冻结反馈并关闭，不重新摘要、自动发请求或宣布旧回合成功；反馈后的再摘要不是此合法窗口

#### Scenario: Resume after the hook script disappears
- **WHEN** 有已保存检查/反馈的脚本、配置或输出对象消失
- **THEN** 恢复 bytes 前缀不变，历史只读详情和下一请求仍只由 native/frozen facts 生成

#### Scenario: Complete a pending local delivery
- **WHEN** continued 冻结决定已保存但没有 native feedback
- **THEN** 本地交付恰好一次原最终字节并关闭旧 turn，无新决定/计费、重渲染、配置读取或网络

#### Scenario: Close compaction before delivering frozen feedback
- **WHEN** pending delivery 期间 compaction_started 已保存而 completion 缺失
- **THEN** 先本地保存 compaction_interrupted，再交付反馈并关闭，不恢复未保存用户 claim 或重试摘要

#### Scenario: Recover an admitted user correction
- **WHEN** 三记录交付已 Sync，而内存 finalize/宿主通知前崩溃
- **THEN** 反馈和真实用户 InputIDs/Skill 附件均恢复一次，用户输入保持 committed；无反馈重复、队列返还或虚构用户事件

### Requirement: Completion recovery retains lease and repeated-crash idempotency

Stop reconciliation SHALL 与 load/repair/writer 共用同一连续文件所有权窗口，直到最终 Sync/Close；真实竞争进程在读后 repair、append 或 Provider 恢复前 session_busy，失败路径 journal bytes 不变。二次崩溃后的恢复不得重复 finished/decision/native feedback/user inputs/usage/terminal，seq 单调且原前缀不改写；只补实际缺失的本地交付或终态。任何更早语义/Provider 损坏即使另有半行，也必须在 repair/补偿前整体拒绝；Sync 不确定不得发布可用 Session。

#### Scenario: Compete with recovery from another process
- **WHEN** 持有者正在补偿 Stop 而真实子进程请求同 journal
- **THEN** 子进程 session_busy、零 repair/append，持有者释放或崩溃后才可重新获取 lease

#### Scenario: Crash after a recovery batch
- **WHEN** 首次补偿交付已 Sync、terminal 尚未保存时再次退出
- **THEN** 第二次恢复复用交付，只补真正缺失终态，不重复任何模型反馈、用户输入或预算扣账

#### Scenario: Reject corruption before repairing a suffix
- **WHEN** 合法外壳下的 Stop 身份/计量/反馈语义非法且文件另有半行
- **THEN** 拒绝且全部原 bytes 不变，零 repair/补偿/外部调用
