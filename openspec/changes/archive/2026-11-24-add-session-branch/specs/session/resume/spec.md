## MODIFIED Requirements

### Requirement: Interactive chat creates a stable root Session identity

未指定 resume 的交互式 Chat SHALL 为逻辑 Session、root thread 和每个 turn 生成全局唯一且格式可验证的标识。Session 元数据 SHALL 记录 root thread、创建时间、Provider family、wire、model 和创建时规范化的绝对 `creation_cwd`，但 MUST NOT 记录 API key、base URL、Authorization、Cookie、敏感 Header 或配置文件路径。

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

### Requirement: User can continue the most recent compatible thread

CLI SHALL 提供显式布尔参数 `--continue`，在进入 TUI、创建新 Session 或发起 Provider 请求前协调 Session Catalog，并从当前规范化 `creation_cwd` 中选择 Provider family、wire 和 model 与当前配置完全一致的最近根线程或可恢复分支。分支以自身最新提交记录参与排序，尚无后续记录时以已提交的 `thread_forked` 为准。候选 MUST 按最后 committed record 的 `updated_at` 降序、canonical thread ID 降序确定；`--continue` 不得使用文件 mtime 排序，不得解析 symlink 获取另一个 cwd，也不得在本切片中运行 Git 或扩展到其他 worktree。

`--continue` 与 `--resume <thread-id>` MUST 互斥；同时指定 SHALL 作为 CLI 用法错误在读取或修改 Session、打开 Catalog、加载 Provider 配置或调用 Provider 前以退出码 `2` 失败。未找到兼容候选时 SHALL 返回安全英文 `session_not_found`，不得创建替代 Session；存在其他 cwd 或不兼容 family/wire/model 的 Session 不构成候选。

Catalog 只负责选择 thread ID。选择后应用 MUST 调用与显式 `--resume` 相同的路径，重新取得目标 journal 的连续 exclusive lease，重新执行 Loader、ReplayPlanner、配置兼容检查、Provider 恢复、interrupted-tail 补偿和 writer transfer；Catalog row 不得替代任何恢复校验。取得目标 lease 后 MUST 重新按实际记录核对规范化创建目录与本次启动目录的精确匹配，以及 Provider 三元组。若已选择目标当前 busy，应用 SHALL 返回 `session_busy` 而不得静默选择更旧 thread；若 reconciliation 遇到无法分类的未索引 busy journal，自动选择 SHALL 以 `session_busy` 失败而不是假定它与当前选择无关。

#### Scenario: Continue the latest compatible Session

- **WHEN** 当前 cwd 中存在多个可恢复根线程或分支，且至少两个与当前 family、wire、model 兼容
- **THEN** `--continue` 选择 durable `updated_at` 最新的兼容 thread，并通过现有 resume 路径恢复它

#### Scenario: Ignore another cwd and incompatible provider configuration

- **WHEN** 全局最近 Session 位于不同 `creation_cwd`，或其 family、wire、model 任一项与当前配置不同
- **THEN** `--continue` 不选择该 Session，并继续在精确匹配候选中确定最近 thread

#### Scenario: Resolve an equal-time tie

- **WHEN** 两个兼容候选具有相同的最后 committed timestamp
- **THEN** `--continue` 确定性选择 canonical thread ID 较大的候选

#### Scenario: Reject continue when no compatible Session exists

- **WHEN** 当前 cwd 中没有可完整验证且与当前配置兼容的根线程或分支
- **THEN** 应用在创建 Session、进入 TUI 或调用 Provider 前返回 `session_not_found`

#### Scenario: Reject a selected busy Session

- **WHEN** Catalog 选择的最近兼容 thread 已被另一个进程持有
- **THEN** 现有 resume 路径返回 `session_busy`，且应用不回退到更旧 thread、不调用 Provider、不修改目标 journal

#### Scenario: Revalidate after catalog selection

- **WHEN** Catalog 选择完成后实际打开的目标 journal 损坏、身份不符、Provider 配置不兼容，或同 ID 文件已换成创建目录不同的有效 journal
- **THEN** descriptor-bound resume 重新校验并安全失败，不修复、不切换目录、不请求模型

#### Scenario: Continue a recently created branch
- **WHEN** 同工作区的兼容根线程与分支共用逻辑 Session，且分支的 `thread_forked` 是两者最新的本线程提交事实
- **THEN** `--continue` 选择分支的 ThreadID。分支被占用时返回 `session_busy`，不退回根线程

### Requirement: User can explicitly resume a recoverable thread

CLI SHALL 提供 `--resume <thread-id>` 选择已有根线程或可恢复分支。应用 MUST 在进入 TUI 或发起网络请求前解析标识、定位 journal，并在读取首个 record 或执行任何尾部 repair 前取得该 thread 的跨进程 exclusive lease。load、repair、ReplayPlanner、配置兼容检查、Provider 事务式恢复、interrupted-tail 补偿和后续 writer MUST 使用同一连续所有权窗口；成功恢复后 lease MUST 保持到活动 writer 完成关闭，不得在 load 与续写之间关闭后重新打开 journal。

缺失参数、无效标识、不存在的 thread、不可恢复的 thread、损坏或不受支持的 Session MUST 返回安全英文错误，不得静默创建新 Session 或回退为空会话。当目标 thread 已被另一个进程持有时，resume MUST 在读取后 repair、Provider 恢复、TUI 启动或网络请求前以稳定英文 `session_busy` 失败；失败路径 MUST 释放自身临时资源且不得修改 journal bytes、创建替代 Session 或影响现有 owner。

`--resume` MUST 始终使用调用方提供的 thread ID，不得把缺失或无效值解释为 `--continue`。用户未指定 `--resume` 或 `--continue` 时，应用仍 SHALL 创建新的 root Session，而不是隐式选择最近历史。根线程与分支均使用启动工作目录及其现有工具权限；显式 CLI 恢复不切换目录。分支恢复仅 SHALL 打开该分支 journal，验证其中的前缀、血缘及当前线程身份，沿用同一描述符接管续写，不读取或获取父线程的 journal lease。

#### Scenario: Resume an existing root thread

- **WHEN** 用户传入一个存在、完整且未被其他进程占用的 root thread ID
- **THEN** 应用在一个连续 exclusive lease 下加载全部 committed records、恢复原 Session/thread identity 并启动同一 handle 上的 writer

#### Scenario: Reject an unknown thread

- **WHEN** `--resume` 指向不存在或格式无效的 thread ID
- **THEN** 应用在网络请求前失败且不创建替代 Session 文件

#### Scenario: Reject a concurrently active thread

- **WHEN** 另一个进程已经持有目标根线程或分支的活动 writer lease
- **THEN** 当前 resume 返回稳定英文 `session_busy` 错误且不进入 TUI、不调用 Provider
- **THEN** 目标 journal 的长度、checksum、尾部状态和全部 bytes 保持不变

#### Scenario: Resume after the previous owner exits

- **WHEN** 先前 owner 正常关闭 writer 或进程终止后，用户再次 resume 同一 thread
- **THEN** 新进程可以取得 lease，完整校验 journal，并从最后一个 committed record 的下一 seq 继续

#### Scenario: Do not implicitly continue

- **WHEN** 用户正常启动 EasyCode 而没有传入 `--resume` 或 `--continue`
- **THEN** 应用创建新的 root Session，而不是自动加载最近历史

#### Scenario: Resume an explicit branch independently of its parent
- **WHEN** 用户显式恢复分支的完整 canonical ThreadID，父 journal 已移出存储根，或父线程由另一进程持有活动 writer lease
- **THEN** 应用仅在分支的连续独占 lease 下验证继承前缀与最新分支边界，恢复共享的 SessionID 和分支自己的 ThreadID，向分支续写时从其最新已提交记录的下一 seq 开始
- **THEN** 分支验证和接管期间不读取、修复或清理父 journal 及其输出存储

### Requirement: Headless resume continues native history without replaying prior output

显式 `--resume <thread-id>` 或 `--continue` 与 headless 模式组合时，应用 SHALL 在自动选择完成后复用相同的连续 exclusive lease、ReplayPlanner、配置兼容检查、Provider-owned codec 和 interrupted-tail 补偿路径恢复所选根线程或分支的 SessionID 与 ThreadID。外部 JSONL v1 SHALL 使用原有字段，分支创建仍限于交互聊天。当前 prompt SHALL 使用恢复后的 Provider-native history 编译请求，并把新 records 追加到原 thread 的下一 seq；headless MUST NOT 从 Catalog、`SemanticHistoryView`、JSONL payload 或 RuntimeEvent 反向构造续写请求。

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

#### Scenario: Resume an existing branch without echoing inherited history
- **WHEN** 一次性或 stream-json 宿主通过既有恢复选项接入合法分支并接纳新输入
- **THEN** 首次新请求使用已验证的继承原生前缀与分支后续事实，仅追加分支的新记录。JSON 使用原有 resumed `thread.started` 和新操作事件，文本模式仅输出本次最终答复
- **THEN** 输出不重放继承或分支的既有 transcript，外部 v1 不扩展血缘字段或创建操作

### Requirement: Resume validates before tail repair

resume/continue SHALL 使用连续 exclusive lease 只读加载，先完成 replay、Provider 兼容及原生/checkpoint 校验，再显式修复尾部并 Sync，然后启动 writer 和本地 reconciliation。校验失败 MUST 保持包括未完成尾部在内的原始 bytes 不变。分支 SHALL 在同一 lease 下验证完整的继承前缀、每条已提交分支边界及定位目标的 ThreadID，再由相应 Provider 验证其原生顺序、证据与 checkpoint。尾部修复资格只适用于当前分支已验证的创建事实之后。正式恢复分支时，完整验证后 MUST 在目标 lease 下确认已验证存储根经规范日期目录定位到同一持有的文件，并确认文件和目录持久化，再进行 repair、补偿或 writer 接管。目录项确认失败时 MUST 保留原始 bytes，并给出安全英文错误。只读预览和 Catalog 扫描不执行该恢复确认或尾部修复。修复失败或 durability 不确定 MUST 阻止 writer 和新副作用。

原生记录、计量与 checkpoint 的校验 SHALL 使用恢复请求的取消，至少在相邻记录的有界验证之间观察取消。校验阶段接受取消时，恢复 MUST 作为取消收口并等待原 lease 和其他资源释放。已有格式、关联、计量、回放与 checkpoint 投影的阶段错误优先级 SHALL 按现有入口保留，另行定义的计量对账按其契约处理。

执行接纳前的确定取消或超时 SHALL 以匹配调用的结果收口。尚有未收口的工具组且无法完整写入结果与原生输出时，Runtime MUST 阻止新的副作用并保留可回放的 committed 前缀，将该尾部交给既有恢复补偿，不能提前持久化 `turn_failed`。

同一 turn 在尚有未 started 的只读调用时耗尽正文预算，回放 SHALL 按已提交调用次序重建额度，只有额度已耗尽才能接受未启动调用的有界 `result_budget_exceeded` 结果。其他未启动的普通失败和成功结果仍拒绝。

同一连续只读段的已接纳 started MUST 先于该段任何工具结果。Runtime 主回放、metadata 和活动尾部的恢复检查 SHALL 就此顺序作相同判定。前段完整闭合后，跨独占屏障的下一段可继续执行。若独占调用已确定无法继续且其 Post 或预算收口未完成，Runtime MUST 保存原失败及已提交前缀，不追加跨越未收口调用的取消结果。

无审批的 Task 调用在配置策略要求确认而无法执行时 SHALL 允许匹配当前调用的执行前拒绝 `permission_denied`。回放接受该终态和其原生配对，且不能将其解释为 started、批准或需要恢复执行。保持其他未批准调用的既有严格检查。

#### Scenario: Apply the same read-batch order to every replay surface
- **WHEN** 同一只读段前项已有结果，之后才记录后项 started
- **THEN** 活动尾部、已经配对 outputs 的完整日志和元数据回放均拒绝该次序

#### Scenario: Retain uncertainty with an unfinished exclusive barrier
- **WHEN** 独占调用输出不确定结果，Post 或预算还未收口，工具组仍有后续调用
- **THEN** 返回原失败，后续未准入调用不执行，恢复可继续收口原有前缀

#### Scenario: Replay a Task rejected by an ask policy
- **WHEN** 自定义策略对无交互审批能力的 Task 要求确认
- **THEN** 保存未启动的关联拒绝及其输出，在线结束和后续回放均接受

#### Scenario: Replay budget-rejected calls after an execution barrier
- **WHEN** 同一工具组的前序结果耗尽 turn 正文预算，后续只读调用在执行前收到有界预算错误
- **THEN** 重新加载接受已提交的结果与原生输出，未获接纳的工具不执行且不会在恢复时重试

#### Scenario: Reopen a journal after read-only tool admission fails
- **WHEN** 已有 durable ready，而只读工具的 started 接纳前出现确定超时，或写入故障阻止工具组收口
- **THEN** 未获接纳的工具不执行，保存的 journal 可回放并按规则收口，恢复时不会重执行该工具

#### Scenario: Reject an incompatible session with a partial tail
- **WHEN** 当前模型与 Session 不兼容且文件末尾存在半行
- **THEN** 拒绝恢复，journal 全部 bytes 不变

#### Scenario: Repair before local reconciliation
- **WHEN** committed 前缀及 Provider 完整验证通过，尾部存在未完成 batch
- **THEN** 同一 lease 下先修复并 Sync，再从正确 next seq 本地收口，不释放后重新打开文件

#### Scenario: Cancel while validating restored native records
- **WHEN** HTTP 或 WebSocket 尚未开启，恢复请求在原生回放两个记录的验证之间被取消
- **THEN** 恢复停止后续校验并等待当前 lease 关闭，不执行 repair、writer 接管、补偿或 Provider 请求

#### Scenario: Reject invalid inherited facts before repairing a branch tail
- **WHEN** 分支的继承前缀、分支边界或原生 checkpoint 验证失败，文件末尾另有可修复半行
- **THEN** 恢复返回安全错误，分支全部 bytes 保持不变，不安装候选、不执行补偿、不启动 writer 或 Provider 请求

### Requirement: Picker selection enters the existing explicit recovery boundary

用户从启动选择器确认目标后，系统 SHALL 固定该 canonical thread ID，并重新获得连续 exclusive lease，执行与显式 `--resume` 一致的读取、回放、配置/native 校验、合法修复、补偿和 writer transfer。Catalog 元数据及预览结果 MUST NOT 替代任何检查。目标 busy、删除、损坏或不兼容 MUST 明确失败，禁止选择其他 thread、创建新 Session 或转换 Provider/model。

当前配置 family/wire/model 不匹配时选择器 SHALL 显示差异并阻止提交；实际恢复仍须重新验证。恢复成功 SHALL 不自动执行 Provider sample 或旧工具，等待下一次真实用户输入。根线程和可恢复分支均按适用的工作目录规则进入同一恢复路径。`--resume`、`--continue` 和 headless SHALL 保持既有 CLI 参数、并发接管和输出规则。

#### Scenario: Resume an explicitly selected root thread
- **WHEN** 用户选中存在、兼容且空闲的 root thread
- **THEN** 恢复同一 Session/thread identity、原生历史与可见历史，保持 lease 到活动 writer 清理完成，不自动调用模型

#### Scenario: Race selection with another owner
- **WHEN** 列表/预览后另一进程取得目标 lease
- **THEN** 恢复返回 session_busy，原 journal bytes 不变，不尝试更旧会话

#### Scenario: Resume the selected branch under the same ownership rules
- **WHEN** 用户在启动选择器显式选择兼容且空闲的分支，并满足既有跨目录确认条件
- **THEN** 应用以分支 ThreadID 定位并验证同一 journal，恢复独立分支状态，保持 lease 到活动 writer 清理完成，等待真实输入才请求 Provider

### Requirement: In-chat resume validates the selected thread under its exclusive lease

聊天内选择器 SHALL 以用户选中的完整 canonical thread ID 定位目标。直接输入完整 canonical UUIDv7 SHALL 跳过 Catalog 查询、列表和预览，使用同一个显式恢复入口。指向当前源 thread 的请求 MUST 在尝试取得目标 lease 前识别并返回，保留现有聊天实例。Catalog 候选与预览只提供选择依据，缺失或陈旧的索引行不改变 journal 的恢复资格。

系统 MUST 在读取目标 journal 前取得其跨进程 exclusive lease。在同一描述符与连续 lease 下，先校验 Session/thread 身份、根线程或分支归属、分支继承前缀与最新分支边界、目标 `creation_cwd` 与规范化启动 cwd 的精确匹配、冻结 Provider family/wire/model 的精确匹配以及现有回放、原生历史和 checkpoint 契约。只有通过上述校验后才允许执行现有尾部修复与中断补偿，并将 lease 转交给目标 writer。目录检查采用既有启动路径规范化语义，保持实际工具工作区和进程目录。启动选择器、`--resume`、`--continue` 和 headless 继续使用各自的工作目录与恢复规则。

聊天内恢复 SHALL 使用首次激活时冻结的实际工作区和私有存储根权限。目标操作 MUST 使用原目录描述符或在实际取得后验证身份等价的描述符，再进入 journal 读取、修复、补偿、artifact 维护或其他写入。若采用重新打开路径的方式，验证结果必须绑定到实际用于操作的描述符。发现替换或身份无法确认时，系统 SHALL 在相关操作前拒绝，不得改用同名目录、空存储或其他权限根。

#### Scenario: Locate a target outside the picker results
- **WHEN** 用户输入当前列表范围外的完整 canonical thread ID，且目标与当前工作区及配置匹配
- **THEN** 应用直接取得该目标的 lease、验证 journal 并恢复原 Session/thread，不打开列表或选择替代会话

#### Scenario: Keep the active thread when its ID is supplied
- **WHEN** 用户输入当前聊天的完整 thread ID
- **THEN** 应用提示当前已在该会话，且不重新取得源 lease、装配或挂载聊天

#### Scenario: Reject stale metadata before repair
- **WHEN** Catalog 显示目标可恢复，但 lease 下读到的 journal 目录或 Provider 配置不匹配，且尾部存在未提交数据
- **THEN** 应用在 repair 前拒绝目标，目标的 committed records 和未提交尾部逐字节保持原样
- **THEN** 应用保留源会话，跨目录时指引从目标创建目录使用独立 CLI 恢复

#### Scenario: Reject a target acquired by a competing process
- **WHEN** 用户确认目标前后，另一进程已取得目标的活动 writer lease
- **THEN** 应用返回 `session_busy`，目标不发生读取后修复或补偿，源继续持有原 lease

#### Scenario: Reject a substituted workspace or storage root
- **WHEN** 确定性屏障在冻结目录身份后、取得目标操作实际使用的描述符前暂停恢复，期间原工作区或私有存储根被替换为同路径的另一目录
- **THEN** 系统拒绝不匹配的描述符，且替代目录未被用于读取 journal、修复、写入或 artifact 维护。源会话及原数据保持原状

#### Scenario: Switch between a source and its branch in the same workspace
- **WHEN** 用户在空闲聊天内按完整 ThreadID 恢复兼容且与启动 cwd 相同的另一个分支或源线程
- **THEN** 应用分别在源与目标的所有权窗口内完成既有交接，目标在自己的 ThreadID、lease 和最新提交位置继续

## RENAMED Requirements

- FROM: `### Requirement: User can continue the most recent compatible root Session`
- TO: `### Requirement: User can continue the most recent compatible thread`
- FROM: `### Requirement: User can explicitly resume a root thread`
- TO: `### Requirement: User can explicitly resume a recoverable thread`
- FROM: `### Requirement: In-chat resume validates the selected root under its exclusive lease`
- TO: `### Requirement: In-chat resume validates the selected thread under its exclusive lease`
