# tool/task-progress Specification

## Purpose

定义双 Provider 共享的有界任务进度清单，使模型能够完整报告复杂任务的当前步骤和完成情况，并让用户在跨回合、压缩及恢复后读取同一份已提交事实；该能力不改变执行权限，也不把模型报告的完成状态当作验证证据。

## Requirements

### Requirement: Task progress has two strict facade profiles and one current semantic contract

系统 SHALL 暴露唯一能力 `task.progress`。Anthropic structured facade SHALL 为 `TodoWrite`，唯一输入为 `todos` 数组，条目必需且只允许 `content`、`activeForm`、`status`；OpenAI function facade SHALL 为 `update_plan`，唯一输入为必需 `plan` 数组和可选 `explanation`，条目必需且只允许 `step`、`status`。两个 facade SHALL 分别使用当前严格 decoder，在工具层构造带必需语义来源 profile 的封闭 TaskInput，并持有同一纯规范领域 TaskUpdate；不得使用版本字段、通用字典或同时接受两种 facade shape 的兼容 decoder。领域 TaskUpdate 只保存规范任务标签、active_form、status 和 explanation，active_form 允许为空，不认识 Provider、facade 名称或 wire DTO。TaskProgressState、checkpoint 的 task_progress 与 tasks.snapshot 只携带纯规范更新和成功来源，不重复 profile；profile 与 Provider 输入约束留在工具输入、Session 工具事实及对应 Provider facade 的校验边界。

每个输入对象及条目 MUST 拒绝 null、重复键、未知字段、非法 UTF-8、尾随 JSON value、缺失必需字段及另一 facade 的字段。Anthropic 的 `content` 和 OpenAI 的 `step` SHALL 成为共享任务标签，原始合法文字逐字保留；Anthropic 的 `activeForm` 必需且逐字保留，OpenAI 对应语义字段必须归一化为空且不得在 wire 输入携带它。`explanation` 只属于 OpenAI，缺省表达无说明，不通过空值或 Anthropic 附加字段绕过来源约束。唯一当前工具输入 decoder SHALL 显式接收 Provider family 与 facade profile，校验两者、允许字段及领域规范更新的一致性，不根据 JSON 字段猜测 facade、不先试一种 decoder 再退回另一种。原生恢复和离线只读正文查询 SHALL 复用相同已验证工具输入/result evidence；领域值不替工具层解释 activeForm 必需性或 explanation 归属，不将 profile 作为历史格式或版本分派。

#### Scenario: Normalize each native facade
- **WHEN** 两家分别返回合法 TodoWrite.todos 与 update_plan.plan 完整输入
- **THEN** 两者产生 `task.progress` ready call，各自保留对应来源、完整条目及原生 facade/call identity，OpenAI 的 activeForm 为空而 Anthropic 的原文字不丢失

#### Scenario: Reject fields from the other facade
- **WHEN** TodoWrite 输入包含 plan/explanation，或 update_plan 条目包含 content/activeForm，或任一输入含 null、重复键、未知字段及尾随 value
- **THEN** 匹配 facade 的 strict decoder 拒绝，Provider sample 不产生可执行任务 ready call，也不更新已提交清单

#### Scenario: Reject an inconsistent semantic source
- **WHEN** 工具层 TaskInput 的显式 OpenAI profile 携带非空 active_form，或显式 Anthropic profile 携带 explanation 或缺少 activeForm
- **THEN** 工具 typed 构造、严格解码和 validator 拒绝该值，不让领域值识别 Provider，也不尝试另一种历史 reader


OpenAI update_plan SHALL 在实际请求中显式发送服务端 schema `strict:false`，以保持 explanation 可省略且 null 被本地拒绝的当前输入契约；false 不得因零值省略而交给服务端默认推断。本地严格 decoder/validator 的所有拒绝规则保持不变，不把服务端非严格生成模式解释为接受未知或非法输入。其他既有 OpenAI function facade 的 strict 值及既有定义 wire bytes 保持，custom apply_patch 仍省略不适用的 strict 字段，Anthropic 不携带 OpenAI 的服务端 strict 字段。每 facade 的服务端 schema 模式 SHALL 为工具/Provider 边界的强类型固定策略，参与目录 canonical definition、source revision、fingerprint 与实际请求属性证据；不得存入领域任务值、按动态任务内容切换或用遗漏字段代替显式 false。

模型实际可见的稳定工具 description SHALL 披露：最多 20 项；每个模型提供的标签/activeForm 最多 512 UTF-8 bytes、非空、无外围空白和控制字符；OpenAI 可选 explanation 最多 2 KiB，控制字符只允许 LF/Tab；总文字最多 16 KiB、raw input 与 canonical snapshot 分别最多 64 KiB；标签精确唯一、至多一个 in_progress 且允许零个；每次完整替换、空数组明确清除、状态跨用户回合保留，以及 completed 只是模型报告而非验证证据。它 SHALL 说明适用于多步骤任务，成功后无需复述完整清单，只有下一次显式完整更新才替换当前清单。JSON Schema 的 maxLength 表示字符数，MUST NOT 被宣称或使用为 UTF-8 bytes 的唯一执行检查；实际字节约束仍由本地 validator 强制。静态说明不得混入当前任务、预算余额、来源 identity 或 UI 状态。

#### Scenario: Compile the optional explanation without server strict rejection
- **WHEN** OpenAI 首个实际请求声明 update_plan，schema 的 explanation 可省略且未声明 nullable
- **THEN** 实际 JSON 明确包含该 facade 的 strict:false；合法省略 explanation 的工具输入可解码，explanation:null 仍被本地拒绝，其他 function strict 与 custom 字段保持原 wire bytes

#### Scenario: Bind the server schema mode to stable request evidence
- **WHEN** 仅在固定测试目录中改变一个 facade 的服务端 schema 模式，或以不同注册顺序创建相同最终目录
- **THEN** 前者使目录 canonical/source revision/fingerprint 和实际请求属性证据可解释变化，后者保持完全相同；动态任务更新不改变这些静态值

#### Scenario: Describe the real byte boundaries in the first request
- **WHEN** 两家真实 CLI 将含任务 facade 的第一个请求发送给本地验证服务
- **THEN** 服务在返回 SSE 前核验实际稳定 description、schema、服务端模式和必需字段，并明确拒绝不符合协议的定义；golden 覆盖完整限制及两家字段差异，不以无条件返回固定 tool call 代替请求验收

#### Scenario: Decode only the explicitly selected facade
- **WHEN** 相同或混合 JSON 字段被交给显式选择的 family/profile，或来源 profile 与 native facade 错配
- **THEN** 工具边界按该单一 profile 拒绝非法组合，不猜测字段、不修改领域类型、不通过另一个 decoder 接受输入

### Requirement: Task updates are bounded complete replacements

单次更新 SHALL 最多包含 20 项。每个模型提供的 content/step/activeForm SHALL 为非空、无外围空白且不含 Unicode 控制字符的合法 UTF-8 字符串，各最多 512 UTF-8 bytes；OpenAI 未提供的 activeForm 按来源契约归一化为空，不受必需文字非空限制。共享标签 SHALL 按未经 Unicode 规范化的完整文字精确比较且不得重复；status 只允许 `pending`、`in_progress`、`completed`，至多一项为 `in_progress`。OpenAI explanation 如提供 SHALL 为合法 UTF-8、最多 2 KiB，只允许 LF（U+000A）与 Tab（U+0009）这两类控制字符；所有用户提供的任务文字与 explanation 合计最多 16 KiB，完整 raw facade input 和共享 canonical snapshot 分别最多 64 KiB，不能只计解码后文字或省略 JSON 编码膨胀。

每次成功更新 SHALL 原子替换当前线程的完整清单和说明，不是合并或逐项 patch。空数组 SHALL 明确清除当前清单；非空更新允许全 pending、全 completed、调整顺序和 completed 回退为 pending/in_progress，不凭旧状态拒绝合理重规划。当前状态 SHALL 区分从未提交任务的 none、已成功显式清除的 clear、非空 snapshot；none 分支不得携带 source/update；clear 和 snapshot 分支必须携带 source 与完整规范 TaskUpdate，source 由 result_sequence、turn_id、invocation_id 组成并指向真实成功结果，clear 的 items 为空，snapshot 的 items 非空。所有分支严格拒绝缺失、null、未知字段和矛盾组合，不作为 optional 旧格式 reader。不得截断条目、文字或 explanation 后报告成功，不自动转存 artifact 代替缺失清单。

#### Scenario: Replace instead of merge
- **WHEN** 已有 A/B/C 清单后成功提交仅含 B/D 的完整更新
- **THEN** 最新清单恰好为 B/D 及本次说明，A/C 和旧说明不作为残留项保留

#### Scenario: Clear a previous plan explicitly
- **WHEN** 成功任务更新的数组为空
- **THEN** 当前状态为带本次成功来源的 clear，条目为空且与从未创建任务的 none 可区分

#### Scenario: Accept an intentional replanning
- **WHEN** 某 completed 项被完整更新为 pending，或全部条目先标记为 pending
- **THEN** 合法更新被接受，不将任务清单当作具有不可逆迁移限制的执行状态机

#### Scenario: Reject an invalid or oversized list intact
- **WHEN** 更新包含 21 项、重复标签、两个 in_progress、空白标签、控制字符、超长文字，或 raw/canonical 编码超过上限
- **THEN** 拒绝整个更新，保留旧清单，不裁剪成部分成功结果，不回显非法输入正文

#### Scenario: Preserve valid Unicode literally
- **WHEN** 两项标签使用不同合法 Unicode 字节序列且各自满足全部限制
- **THEN** 保存原始字节并按精确文字判断唯一性，不为显示方便规范化、改写或丢失来源字段


#### Scenario: Measure multibyte labels by UTF-8 bytes
- **WHEN** 原样中文与 ASCII 混合标签或 activeForm 分别恰好编码为 512 和 513 UTF-8 bytes，二者字符数均不超过 schema maxLength
- **THEN** 512-byte 值在其他约束合法时接受，513-byte 值由本地 validator 整体拒绝，不能仅凭字符数允许超限

#### Scenario: Keep facade syntax outside pure task snapshots
- **WHEN** 已验证工具输入形成领域更新、当前状态、checkpoint 和外部任务快照
- **THEN** 这些纯快照只保存规范字段及适用成功来源；facade profile 由对应原调用证据验证，不重复存入领域值或允许 checkpoint 自报 profile 绕过来源校验

### Requirement: Task updates use ordered exclusive execution without permissions or tool hooks

任务更新 SHALL 复用既有唯一调用 ID、幂等 ledger、sample/turn call 上限及按原 call index 提交规则，并作为独占屏障执行。其前序只读批次必须完整提交，后序调用必须等待该更新确定收口；同一 sample 的多次任务更新按模型调用顺序生效。执行器 SHALL 仅以纯内存方式构造强类型结果，不读写工作区、任务文件、数据库或网络，不启动进程或 goroutine，不自行持有当前线程状态。

任务更新 SHALL 不请求普通副作用 approval、不产生 session grant，也不触发针对文件/命令 mutation 的 PreToolUse、PostToolUse 或 PostToolUseFailure；它不绕过同回合正常 Stop 完成检查。构造器、getter、序列化和 UI 渲染均保持无 I/O。未通过 Provider strict decoder 的非法输入使整个 sample 失败，不制造 ready 或配对结果；已 durable ready 之后的执行拒绝、预算不足或未准入调用必须保留原调用的有界配对错误，不提前更新任务状态。

#### Scenario: Order two updates among mixed tools
- **WHEN** 同一 sample 按顺序包含 Read、任务更新 A、Grep、任务更新 B
- **THEN** 各结果按原 call index 提交；A 在其成功结果 durable 后生效，B 在自己的成功结果 durable 后完整替换 A，不受只读 worker 的完成顺序影响

#### Scenario: Execute without an approval prompt
- **WHEN** 合法任务更新进入执行 slot
- **THEN** 不询问批准、不运行 Tool Hooks、不建立权限 grant、不产生外部 I/O，仍计入既有调用数量和输出预算

### Requirement: Only durable successful results change current task progress

完整强类型任务结果 SHALL 保存 capability、原调用身份、可通过已验证 TaskInput 核验的来源映射、完整纯规范任务更新、冻结安全模型确认及预算事实，并作为当前任务真相。执行候选、ready call、preview、UI event 或尚未成功 Sync 的结果 MUST NOT 改变当前任务状态。只有成功结果确定 durable 后，当前线程的最新成功任务投影才原子前移；后续 native tool-output 暂未提交不回滚已确定的任务事实，恢复应补齐原配对 output。

append/Sync 确定失败 SHALL 保留旧清单；durability 无法确认时按既有 poisoned/cleanup 契约收口，不发布虚假成功，也不释放 writer 所有权后继续任务。接收前取消不得生效；接收后取消须先取得确定 durable resolution，不能用取消丢弃已经成功提交的更新。普通错误、拒绝、预算错误、中断补偿、未提交 sample 与调用输入中的 completed 均不能制造成功状态。

当前任务投影 SHALL 跨当前线程的普通用户回合保留，直到后续成功替换或清除；不得在 turn completed/failed 时自动清空或自动把剩余项标记 completed。恢复 SHALL 按完整 Session 事实顺序重建最新成功结果，不重跑执行器、Tool Hook 或模型，不依据 TUI 快照或摘要文字猜测状态。

#### Scenario: Publish after Sync only
- **WHEN** 成功候选已经构造但结果的 Sync 尚未成功
- **THEN** 当前查询和任务事件仍显示旧清单；Sync 成功后才显示本次完整更新及真实来源

#### Scenario: Recover a result before its native output
- **WHEN** 任务成功结果 durable 后进程退出，匹配 native tool-output 尚未保存
- **THEN** 恢复从结果重建该清单，只补齐冻结原生 output，不重新执行任务工具，不回退到前一个任务结果

#### Scenario: Keep the plan across a failed later turn
- **WHEN** 后续普通回合失败或被取消但没有新的成功任务结果
- **THEN** 原任务清单及来源保留，不伪造完成或清除

#### Scenario: Keep uncertain durability closed
- **WHEN** 任务结果 Sync 无法确认成功与否
- **THEN** 不发布成功任务更新或发起后续模型请求，沿既有 poisoned owner 路径等待确定清理及离线恢复

### Requirement: Task completion is reported progress and not execution authority

系统 SHALL 将 completed 展示为模型报告的任务状态，不能把它作为测试通过、文件已修改、命令成功、权限已批准、Stop 已通过或用户已接受结果的证据。清单不自动执行步骤、不调度依赖、不生成新工具调用、不进入或退出 Plan Mode，也不创建 Subagent、后台任务或独立协作存储。简单任务不被强制要求先创建清单；未使用任务工具时原有对话与执行行为保持。

#### Scenario: A model marks verification complete without running tests
- **WHEN** 模型将“运行测试”项标记 completed 但 Session 没有真实执行证据
- **THEN** UI 只展示任务报告，执行与完成检查仍依据各自真实事实，系统不生成测试成功或 approval 事实

#### Scenario: Finish with incomplete tasks
- **WHEN** 模型完成答复但仍有 pending/in_progress 项
- **THEN** 保留真实任务状态，不自动补齐 completed、不隐式继续模型，也不改变现有 Stop 决策规则

### Requirement: Current task snapshots remain exact through compaction

每个唯一当前 checkpoint SHALL 必需包含 `task_progress`，用封闭三态表达 none、clear 或 snapshot；后两者带真实成功结果来源和完整更新。当前任务快照 SHALL 与该 checkpoint 对应 `compaction_started` 之前完整 ledger 中的最近成功任务结果逐字交叉核验，不能使用当前最新清单替代更早压缩的来源、接受任意摘要复述或信任单独 native 正文。

摘要候选 SHALL 在自身 Provider 原生摘要包装中确定性携带准确任务状态；无任务、明确清除和非空清单保持可区分，canonical 字段顺序及字节有界。该动态状态不得进入稳定 tool catalog 或 provider profile，不在每次普通 sample 额外重复注入。完整候选编码、footprint 和窗口预算必须计入任务包装；超限或压缩失败保留旧窗口与任务事实，不截断快照。恢复只验证并使用已保存 checkpoint/ledger，不重读工作区、任务文件或外部服务。

#### Scenario: Compact a nonempty task list exactly
- **WHEN** 最近成功任务结果包含进行中条目、说明及对应来源，随后压缩成功
- **THEN** checkpoint 的 task_progress 与该成功结果完全一致；后续请求包含原生摘要包装中准确清单，不依赖摘要模型是否复述任务

#### Scenario: Reject a checkpoint from the wrong ledger prefix
- **WHEN** 某 checkpoint 使用在自身 compaction_started 之后才成功的任务更新，或丢失明确 clear 来源
- **THEN** 恢复在 repair 和任何外部副作用前拒绝全部状态，不选择看似更新的清单修补历史

#### Scenario: Preserve exact tasks when the candidate exceeds its window
- **WHEN** 加入精确任务包装使压缩候选完整编码或窗口预算超限
- **THEN** 按既有预算失败规则收口并保留旧窗口，不裁任务 metadata，不提交声称成功但缺失任务的 checkpoint
