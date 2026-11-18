## MODIFIED Requirements

### Requirement: Manual compaction summarizes the complete active window

系统 SHALL 在显式请求时总结整个当前有效窗口；首次窗口包含全部已提交原生历史，后续窗口包含最近成功摘要及其后的原生提交。系统 MUST 保留完整原始历史，MUST NOT 自动裁掉旧内容、保留任意局部片段、分块重试、切换模型或调用服务端专用 compaction API。摘要 MUST 有明确的压缩来源，不得伪造用户输入、普通 assistant 回答或用户授权。没有原生历史或最近成功压缩后没有新增原生提交时 SHALL 以 `nothing_to_compact` 在网络和 Session 写入前拒绝。

#### Scenario: Compact once and continue
- **WHEN** 用户对包含已闭合工具调用的历史执行手动压缩并成功提交新摘要
- **THEN** 下一普通请求包含摘要及之后新增内容，不重发被覆盖的原文；原始工具配对与 transcript 仍可查询
- **THEN** 压缩本身不创建普通 turn 或发起普通续写

#### Scenario: Compact a second window
- **WHEN** 第一次成功压缩后新增原生提交，再执行手动压缩
- **THEN** 摘要请求包含前一摘要和新增提交，不重新加载全部归档前文
- **THEN** 原 journal 前缀保持不变，两个压缩边界均可回放

#### Scenario: Avoid repeated empty work
- **WHEN** 空会话或成功压缩后未新增原生提交时再次请求压缩
- **THEN** 返回 nothing_to_compact，零网络、零 started record，窗口不变

压缩 SHALL 另外冻结该 compaction_started 之前完整 committed 前缀的任务进度状态，并把精确、有界快照作为宿主生成的来源明确任务段放进新 checkpoint 的 Provider 原生摘要包装。当前状态只取该前缀最新成功 task.progress result：从无成功更新为 none，最新成功空列表为 clear，最新成功非空列表为 snapshot。失败、取消、预算拒绝及未完成调用不推进状态。不得让模型生成的摘要复述、推断或覆盖该快照，也不得每次普通 sample 额外注入同一清单。

Task 包装 SHALL 保留完整任务文本、顺序、状态、activeForm 及存在时的说明，只表达模型报告的清单；它不是新用户输入、权限、验证证明或执行计划批准。source seq/turn/invocation 仅用于持久化校验，不进入模型包装；工具 facade profile 只保留在原始 TaskInput 事实，domain 更新、状态与 checkpoint 不携带它。none 不添加任务正文，clear 必须明确当前清单已清空，不能从自然语言摘要复活旧列表。

#### Scenario: Preserve a complete task snapshot during compaction
- **WHEN** source 前缀最新成功更新含多个任务，模型 generation 遗漏任务或声称不同进度
- **THEN** 新原生窗口确定性保留源快照完整内容，UI 当前状态仍只来自已验证 ledger；模型 generation 单独真实保存且不被伪改成完整任务列表

#### Scenario: Preserve an explicit clearing across repeated compaction
- **WHEN** 旧窗口曾有任务，最近成功更新是空数组，随后压缩两次
- **THEN** 两次 checkpoint 的状态均为有真实来源的 clear，原生包装明确没有当前列表，旧摘要任务不能复活

### Requirement: Summary generation is bounded and cannot execute tools

摘要输入 SHALL 只由对应 Provider 的原生有效窗口编译，保留当前模型、稳定工具目录和项目指令；共享语义投影 MUST NOT 反向生成请求。可选保留重点 SHALL 是合法 UTF-8、去除外围空白后不超过 16 KiB 的文本，可为空，不作为用户任务提交。摘要请求 MUST 在 wire 上禁止工具选择并在响应消费端拒绝工具调用，且不得启动工具执行器、审批或子代理。网络操作 SHALL 有 120 秒期限并支持更早的用户取消和有 owner 的清理。

摘要 MUST 为完整成功响应中的非空 UTF-8 文本，正文最多 256 KiB；refusal、截断/incomplete、协议失败、tool call、空白或超限文本 MUST 失败，不能用部分文本或错误正文形成成功 checkpoint。系统 MUST 按 typed 终态处理错误，不得按文本包含某个词判断成功。摘要生成 SHALL 关注最新任务、用户纠正、约束、已完成及验证、关键决策/文件、未解决问题和下一步，不要求披露推理过程。

#### Scenario: A server returns a tool call despite disabled tools
- **WHEN** 任一 Provider 的摘要响应包含工具调用
- **THEN** 压缩失败且所有工具执行、审批和副作用次数为零，旧窗口不变

#### Scenario: Reject incomplete output
- **WHEN** 摘要响应先产生文本再断流、取消或以输出长度限制结束
- **THEN** 部分文本不作为摘要提交，不发布 completed

#### Scenario: Preserve protocol meaning
- **WHEN** 成功摘要正文包含正在总结的错误日志词语
- **THEN** 系统不因正文含 Error 等词语拒绝它，而按合法终态和候选验证判断

#### Scenario: Reject incompatible endpoints without fallback
- **WHEN** 用户配置的 base_url 拒绝禁用工具或摘要输出限制字段
- **THEN** 报告安全压缩失败，不自动删除保护字段、换 API 或重试

任务保真 SHALL 由已验证 typed 状态和确定性宿主包装实现，不能要求模型摘要回显全部列表后再把其文字当成事实。摘要请求仍只由对应 Provider 原生有效窗口编译，冻结的任务状态仅供候选 checkpoint 构造和核验，不从共享语义投影反向生成原生调用。真实 generation、其推理/usage 和宿主任务段 MUST 保持可区分；ProjectCheckpoint 的单向摘要继续返回真实模型摘要，不把宿主 task JSON 伪成模型生成文本。

#### Scenario: Separate generated summary from the exact task wrapper
- **WHEN** Provider 成功生成合法摘要，宿主随后将冻结任务快照包装进候选原生摘要
- **THEN** generation 的原始文本与 provenance 保持不变，任务段可独立从 typed 来源验证；任务段不是 assistant sample、user InputID 或新的 tool output

#### Scenario: Keep task handling inert while summarizing
- **WHEN** 摘要 generation 或已提交任务文本包含待办、工具名或操作指示
- **THEN** 摘要过程零任务更新执行器、审批、工具、Hook 或子代理调用，原清单来源不变化

### Requirement: Compaction validates input and replacement budgets

摘要请求 SHALL 执行独立输入估算和编码上限预检，并将实际摘要输出上限纳入预算。窗口已配置时，摘要输入估算加摘要输出预留及安全余量 MUST 不大于窗口；估算 unknown 时 MUST 返回 `compaction_budget_unknown`，明确超限返回 `compaction_budget_exceeded`。普通采样的 unknown 非阻断行为不变。

候选摘要 SHALL 按同一当前规划算法重新估算；只有估算完整、总量严格小于原有效窗口，且配置窗口时满足普通有效输入上限，才能安装。无缩减 SHALL 返回 `compaction_no_reduction`。无窗口配置时不得推断容量，仍要求可编码、估算可比较且确有缩减。所有算术 MUST 检查溢出或饱和，现有 native/record/batch bytes 上限不得放宽。

#### Scenario: Reject a request before any durable start
- **WHEN** 已配置窗口且摘要请求加预留明确超限
- **THEN** 请求被拒绝，零 Session 写入、零 Provider 请求，旧窗口不变

#### Scenario: Keep the old window after an ineffective summary
- **WHEN** 生成摘要合法但新窗口估算大于等于原窗口
- **THEN** 持久化失败终态并保留可信 usage，旧窗口不变，不自动再次压缩

#### Scenario: Compact without a configured model window
- **WHEN** 无窗口配置且摘要请求可编码、候选估算完整并有缩减
- **THEN** 允许成功压缩，但查询不显示容量百分比或保证未超出服务端窗口

候选检查 MUST 包含完整原生摘要包装及 Task 段的真实编码和 footprint，不能只检查模型 generation 或计数确认。任务最多二十项及既有文字/规范快照上限仍适用；完整 wrapper、checkpoint、record/batch 的现有边界不得放宽。合成候选过大、unknown、没有缩减或超窗口时，压缩失败并保留旧窗口；不裁剪任务、不丢 activeForm/说明、不把部分清单声明为成功，也不自动重试另一摘要。

#### Scenario: Reject an oversized combined summary and task snapshot
- **WHEN** generation 与任务快照各自合法，但完整包装使候选 checkpoint/record 编码超限或无法缩减原窗口
- **THEN** 沿对应预算/候选失败路径保留旧窗口，不安装部分列表、不放宽大小限制，真实摘要计量仍按原来源保存

#### Scenario: Use native footprint to cover the complete wrapper
- **WHEN** 语义摘要仍只含模型生成文本，而原生包装另含完整任务快照
- **THEN** 候选普通预算及缩减比较使用覆盖完整原生 footprint 的计划，任务段计量一次且不能漏算

摘要候选规划 SHALL 在同一 committed_history 元数据中使用该候选冻结任务状态的内容 stamp；依照 context/planning，none 省略 task_content_digest，clear/snapshot 使用 SHA-256(CanonicalJSON({state, update})) 的 64 位小写十六进制摘要，排除所有来源身份与 facade profile。它不改变 generation、不增加 source/网络正文或 token；成功压缩前后的估算仍由语义/完整原生 footprint 的覆盖值决定。相同 revision/token/summary 下不同任务不能因估算相同而共享历史内容 fingerprint。

#### Scenario: Plan equal-footprint candidates with different tasks
- **WHEN** 两个压缩候选的真实模型摘要、context revision 和完整原生估算相同，规范任务内容不同
- **THEN** 候选历史 fingerprint 不同，估算各只覆盖实际完整原生包装一次，任务来源身份不进入 stamp

### Requirement: Window installation follows durable success

每次压缩 SHALL 具有独立 CompactionID，开始前绑定当前来源窗口；准备和候选验证不得修改已提交窗口。只有对应完成记录成功 Sync 后才能 finalize 新窗口并发布 completed。writer 接收提交前取消 MUST 不安装候选；接收后 MUST 等待确定结果，成功 Sync 与取消竞争时只报告 completed，无法确认 durability 时 MUST poison 并停止后续副作用。旧原生记录、tool ledger、permission grants、artifact 授权 MUST 不因压缩而删除、恢复或扩大。

#### Scenario: Cancel before completion admission
- **WHEN** 候选已生成但 writer 尚未接收完成提交时取消
- **THEN** 丢弃候选并收口取消，旧窗口可恢复，无 completed 记录

#### Scenario: Cancel after durable completion
- **WHEN** 完成提交已被接受并成功 Sync，同时取消到达
- **THEN** 确认新窗口并只报告一次 completed，不追加 cancelled/failed

#### Scenario: Poison an uncertain commit
- **WHEN** checkpoint append 或 Sync 结果不确定
- **THEN** 不继续采样或自动重试，lease 和 cleanup owner 保留到最终关闭

#### Scenario: Keep summary text outside the authority model
- **WHEN** 摘要声称某命令已批准或包含一个 artifact ID
- **THEN** 后续权限和读取仍按现有真实 grant/descriptor 校验，不从摘要授予权限

每个 Provider checkpoint payload SHALL 必需保存 task_progress 的 none/clear/snapshot 当前封闭联合。none 不含伪造更新来源；clear/snapshot 必须包含来自该 compaction_started 前缀最新成功结果的完整规范更新及 source sequence、turn、invocation 关联。构造、strict decoder、replay 和原生恢复 MUST 交叉核验字段、状态、来源与实际确定性包装，不能仅信任 hash 或 checksum。

checkpoint 只携带该 source 的冻结状态，不成为新的任务更新事实。压缩成功不推进任务更新来源，失败或取消不改变当前列表；checkpoint completion Sync 后才安装新有效窗口。恢复验证旧 checkpoint 必须对照其各自 source 前缀，不得与未来更新或恢复结束时全局列表比较。缺失 task_progress、错配来源、虚构列表或非当前 shape 必须在 repair、补偿和网络之前整体拒绝，不回退旧 checkpoint，不增加内部格式版本、兼容 reader 或迁移。

#### Scenario: Reject a checksum-valid forged task checkpoint
- **WHEN** checkpoint 外壳完整，但清单或来源与 started 前缀最新成功 result 不符，或 task_progress 字段缺失
- **THEN** 整体恢复失败且原 bytes 不变，即使尾部另有半行也不先 repair，不重建部分可用窗口

#### Scenario: Validate each historical task source independently
- **WHEN** checkpoint A 保存清单甲，之后任务更新为乙并产生 checkpoint B
- **THEN** A 对照其旧 source 接受甲，B 对照其新 source 接受乙；恢复后当前列表为最新成功结果，不将 A 判为必须等于乙

#### Scenario: Keep task state separate from window installation
- **WHEN** 源任务已成功保存，而压缩完成提交取消、Sync 失败或成功后内存 finalize 前退出
- **THEN** 取消/失败不回滚任务状态；成功 completion 恢复其精确包装且不制造新任务更新或重复来源

checkpoint 中的更新 SHALL 是不携带 facade profile 的规范 domain.TaskUpdate；profile 仅在原始工具输入事实中验证。snapshot/clear 的 source result_sequence 必须是该前缀真实成功 tool_call_result 的 record sequence，不能使用 ready、execution_started、detail、native commit、另一个结果或将来结果的位置。来源身份不进入任务包装或任务内容 stamp，但其精确匹配仍是安装与恢复的必要条件。

#### Scenario: Reject a task source that names a nonresult sequence
- **WHEN** checkpoint 清单与文字合法且 checksum 已重算，但 result_sequence 指向 ready/detail/native seq、另一合法结果或 started 后才成功的未来结果
- **THEN** 在安装或恢复前整体拒绝，不能用相同计数、相同内容 digest 或自洽原生包装替代真实结果来源
