# PreToolUse Input Rewrite Specification

## Purpose

定义同步 PreToolUse 对现有三种 mutation 工具的有界完整参数替换，使脚本可以规范化实际执行内容，同时保留模型原始调用、普通权限、安全检查、确定性结果说明及可验证的持久化恢复边界。

## Requirements

### Requirement: Input replacement is complete typed and capability preserving

系统 SHALL 仅在已信任 PreToolUse 的运行结果为正常 exit 0、cleanup confirmed、未标记取消/超时、I/O 正常且输出有界时接纳独立 `hookSpecificOutput`，其中仅有 `hookEventName=PreToolUse` 与完整 `updatedInput`。updatedInput MUST 使用当前 stdin 的规范 tool_input 形状，不接受 Provider facade wire、半个对象、默认值补齐或对象合并。

Patch SHALL 要求 file_path 与有序非空 replacements，每项完整含 old_text/new_text/replace_all/line_based；Write SHALL 要求 file_path/content；Exec SHALL 要求 command/workdir/timeout_ms。替换 MUST 经过同能力全部现有 typed input 校验，并保留 invocation ID、Provider call ID、sample/call index、capability、wire kind、workspace 及固定 shell/environment/sandbox/预算策略。对象不得携带权限决定、其他能力或上述身份/执行策略字段。

替换 MUST 保持来源 facade 的匹配范围：Edit 只能有一个 replacement、line_based=false；apply_patch 只能有 1..1024 个完整 LF 行 replacement、全部 line_based=true/replace_all=false，文本不含 CR 且满足当前行终止符规则。该 profile 从原调用的受控身份派生，不能由 stdout 自报或通过规范中间对象切换。Write content 与根 workdir 的空字符串可以合法，但成员不得缺失。

未知/缺失/重复字段、null、类型错误、尾随 JSON、非 UTF-8、跨能力输入、deny 与替换混用 SHALL 以 hook_output_invalid 阻断，不回退原参数执行。双输出各 16 KiB、stdin 512 KiB、单项 timeout 及每调用全部 Hook 30 秒执行额度 SHALL 保持；超限对象不得截断成可执行参数。

完整 updatedInput 的 canonical bytes SHALL 独立最多 16 KiB，并核对包含身份、反馈和 effect 的完整当前 finished 编码可保存。原始输出、规范参数、后项 stdin、持久化包装与模型/UI 表示 MUST 分别按各自实际转义/framing 后的字节校验；原始输出未超限但规范参数超限属于 hook_output_invalid，不伪报原始流计数。规范等价的替换也必须先通过全部边界验证，不能从 no-op 快速路径绕过。

#### Scenario: Replace a complete command input
- **WHEN** 已信任 Pre 返回完整合法的 command/workdir/timeout_ms
- **THEN** 系统构造同一真实调用的 Exec 检查候选，保留原调用身份；完整 gate 通过前尚未选择 E，也未授权或启动命令

#### Scenario: Replace a complete file input
- **WHEN** 已信任 Pre 返回完整合法的 Write 或规范 Patch 对象
- **THEN** 替换只适用于原能力和单个目标，仍需后续候选、安全与权限检查

#### Scenario: Reject a partial or mixed replacement
- **WHEN** 替换缺失 workdir、timeout_ms、Patch 布尔字段等任一必需成员，或与 deny/allow/ask 混用
- **THEN** 返回 hook_output_invalid，零目标工具执行，没有隐式默认化或合并

#### Scenario: Reject a changed identity or capability
- **WHEN** 替换携带 call ID、sandbox、Provider 选择或另一能力的字段
- **THEN** 整个替换拒绝，不能通过 Hook 改变调用归属或执行权限

#### Scenario: Preserve output and failure limits
- **WHEN** 输出超过 16 KiB、timeout/取消或 cleanup 不确定与合法 JSON 同时出现
- **THEN** 使用既有更高优先级的故障分类，替换不接纳，截断后的 JSON 不能放行

#### Scenario: Preserve each Patch facade profile
- **WHEN** Edit 提出多个或 line_based=true 的 replacement，或 apply_patch 改为子串、replace_all 或 CR 文本
- **THEN** 返回 hook_output_invalid，即使一般 Patch 输入验证能接受部分形状，也不能切换源调用匹配模式

#### Scenario: Reject canonical expansion beyond the replacement limit
- **WHEN** 原始 stdout 小于 16 KiB，但 HTML/Unicode 等转义使 canonical updatedInput 超过 16 KiB
- **THEN** 以 hook_output_invalid 和 unchanged effect 收口，零目标工具执行，不提高 finished 上限或保存截断对象

#### Scenario: Validate bounds before accepting an equivalent object
- **WHEN** 输出对象等于当前候选，但规范编码超过替换上限
- **THEN** 仍拒绝该 updatedInput；脚本可用合法空输出表示继续，不存在超限 no-op 兼容入口

#### Scenario: Distinguish malformed runner evidence from script output errors
- **WHEN** runner 返回错调用/事件、错 profile 或绕过 parser 的超限 typed 候选
- **THEN** 按 runner 结果错配记录 Hook 不确定并阻止后续副作用，不能把未验证结果视为普通继续

### Requirement: Ordered hooks compose durable checking candidates before execution selection

系统 SHALL 沿冻结的 user/project 与声明数组顺序串行运行原匹配集合，不根据运行完成时序竞争，也不因替换重选 matcher。首项读取原规范输入 O，后项读取此前成功 Sync 的检查候选 C。每次接纳都重新校验；合法规范 bytes 未改变时 SHALL 视为 unchanged，不生成虚假改写。

替换只在对应 finished 成功 Sync 后成为 C；只有完整 gate continue 才从最终 C 选择有效执行输入 E。候选前缀被后项拒绝/取消时没有 E，门禁非执行结果按 O 关联，不能用 C 改写成实际执行 metadata。Sync 无法确认时 MUST poison 并停止后续接收。任一真实 deny/error/cancelled/outcome_uncertain SHALL 停止该检查链和工具，不退回 O 执行。合法变化前缀保留审计，不产生权限或执行事实。

runner 已返回确定正常结果后的晚到取消 MUST 保留真实 finished/effect。中间项之后的取消阻止下一 ordinal，关闭未运行后缀；最后成功项仍与唯一 gate continue 同批，取消转由后续普通路径阻止工具准入，不追加第二个 gate 或伪造 Hook cancelled。已确认运行失败和其故障优先级保持，不用迟到取消抹除已保存事实。

#### Scenario: Validate the previous replacement in a later hook
- **WHEN** user 第 0 项替换命令并成功保存，project 第 0 项随后运行
- **THEN** 后者读取替换后的完整命令；顺序不受耗时或 UI 消息影响

#### Scenario: Keep an equivalent replacement unchanged
- **WHEN** updatedInput 只改变 JSON 对象成员排列且与当前 C 的规范输入相同
- **THEN** 当前 C 不变，本项 effect 为 unchanged；说明仍由最终 E 与 O 的比较决定，不能抹除先前真实改写或额外增加说明

#### Scenario: Restore the original value through ordered changes
- **WHEN** 两项合法替换先修改参数，再将最终规范参数改回原值
- **THEN** 两项真实 effect 保留审计，最终候选使用原值且不添加改写模型说明，不执行中间参数

#### Scenario: Stop after a later denial
- **WHEN** 第 0 项替换成功保存，第 1 项拒绝
- **THEN** 保留 C 的变化前缀，没有 E；门禁结果按 O 非执行关联，目标工具及后续检查零执行

#### Scenario: Fail before installing unconfirmed input
- **WHEN** replaced finished 的 Sync 失败或提交结果不确定
- **THEN** 不安装该输入、不启动下一项或候选，会话进入既有 durable failure 状态

#### Scenario: Cancel after a definite intermediate result
- **WHEN** runner 已返回确定 continue/replaced，用户在中间 finished 保存前后取消
- **THEN** 保存真实结果/变化，阻止下一项，按未运行后缀关闭 gate，不把脚本改写成未运行或取消的统计

#### Scenario: Cancel during the final successful batch
- **WHEN** 最后一项已确定通过，取消与最后 finished/gate batch 竞争
- **THEN** 确定提交后仍只有真实 finished 和 gate continue，E 已选但普通工具准入被取消，零新脚本/工具

### Requirement: Execution evidence binds selection to the complete original call and turn

系统 SHALL 以 unhooked/gate_rejected/gate_selected 的封闭当前证据区分没有 Pre gate facts、已拒绝与已选中。unhooked 包含既有建 plan 前失败的非执行路径，不表示跳过来源或普通权限。证据 MUST 绑定真实 Session/thread/turn、sample/call index、invocation/call ID、capability、wire kind、完整 O 与已保存 plan/effect/gate/result；不能只比较 ID、指针或零索引 surrogate。gate_selected 即使 E 等于 O 也不能丢弃检查证据；存在但缺失/损坏的 gate 证据不得回退 unhooked。

同 turn 已完成且完整证据相同的重入 SHALL 仅复用冻结状态，零来源读取、脚本、候选、审批和工具执行。在途重复不能启动第二个 owner；错输入、错选择、跨 turn/Provider/workspace 或 poisoned 状态必须失败关闭。执行缓存 SHALL 由现有 turn owner 有界持有，随 turn 退役/关闭清除，不从历史恢复授权。

#### Scenario: Reject the same ID with a different original input
- **WHEN** 已完成调用被相同 ID、能力和 wire kind 但不同 O/index/turn 再次编排
- **THEN** 拒绝相关性冲突，不能返回另一输入的结果或重跑检查

#### Scenario: Reuse a completed selected call
- **WHEN** 同 turn 以完全相同的原调用与选择证据重入
- **THEN** 返回原冻结状态，脚本、批准、executor 与预算关闭次数均不增加

#### Scenario: Preserve proof when changes return to the original value
- **WHEN** C 经实际变化后最终 E 等于 O
- **THEN** 仍持有 gate_selected 证据，模型结果不加改写说明，但不能把该检查链当作没有 Pre

### Requirement: Model results identify the actual effective input within existing budgets

gate_selected 且 E 与 O 不同的调用 SHALL 由真实工具准入及恢复事实决定说明：正常已准入结果包含完整 canonical E 和有界来源；没有准入的准备失败、Post 信任拒绝、普通拒绝/取消/预算错误使用最多 256 UTF-8 bytes 的明确未执行短说明；合法恢复补偿使用明确未重跑/无法确认的短说明。准入不证明 OS 进程已经启动，说明必须保留真实 status/code，不能把检查通过或启动失败说成执行成功。gate_rejected 沿用安全门禁结果，未选择 C 不作为执行参数披露。说明是原 call 的工具来源数据，不新增消息/调用或 additionalContext。

工具准入证据 SHALL 独立于有无 Post 计划，在唯一 tool_execution_started 成功 Sync 后、executor/结果冻结前确定，并被说明、后置选择和宿主共享；hook_execution_started、批准或 result status 不能替代它。无 Post 不能遗漏实际准入或等结果保存后再补来源。

说明及有界来源摘要 MUST 计入现有 result base allowance，Post 反馈继续使用同一预算 ticket。完整说明的来源元数据仅含最终 input_digest 与按计划顺序排列的全部实际 replaced 项 sources，每项仅含 source/id；在既有最多 8 项、每个 id 最多 64 个 ASCII 字符下完整编码最多 1 KiB，不截断来源。完整参数独立最多 16 KiB，连同 framing 按实际 bytes 预检。系统 SHALL 在候选准备、普通批准或副作用前验证完整说明加既有安全结果空间可容纳；不足返回 error/hook_rewrite_budget_exceeded 和短说明、零候选/工具 I/O。没有 Post 时使用同一预算 owner 的真实 allowance，不伪造 Post plan；有 Post 时不足/早期失败仍按 none 路由与现有 ticket 收口，不因说明错误运行 failure 检查或重复关闭预算。能够容纳时保留完整前缀，普通正文仅在剩余额度内截断。说明不得进入原输出 artifact 或被 Post stdin 解析透传。

已 durable 的 result preview SHALL 原字节恢复，不重新包装或计费。恢复中本地补偿的取消/不确定结果 SHALL 使用既有安全额度并明确未重跑或无法确认，不尝试重传完整参数；参数审计仍来自保存的 effect。无实际有效变化的调用保持原模型输出 bytes。

说明模式 MUST 从已验证选择/准入/恢复事实派生，再核对确定性前缀 bytes；不得按 stdout 中标签或错误字符串决定模式。正常实际结果不能冒用恢复短说明隐藏 E，已有前缀不得被通用截断剪掉或重复追加。

#### Scenario: Report the command that was actually selected
- **WHEN** 原命令被替换且有效调用正常执行
- **THEN** 配对 output 明确参数已改写并包含有效规范对象，不能暗示原命令已执行

#### Scenario: Refuse insufficient disclosure space before side effects
- **WHEN** Post 预算预留后，完整说明和安全结果空间超过本调用 base allowance
- **THEN** 返回 hook_rewrite_budget_exceeded，零候选准备、普通批准或工具执行，后置选择保持无准入语义

#### Scenario: Preserve explanation when output is large
- **WHEN** 有效调用产生超过剩余 preview 空间的大输出
- **THEN** 完整有效参数说明保留，普通正文按既有截断/artifact 策略处理，最终 bytes 精确计费且不重复关闭 ticket

#### Scenario: Restore a frozen result without another explanation
- **WHEN** 带改写说明的 result 已保存而 native output 尚未保存
- **THEN** 只用原冻结 preview 补 output，不追加第二段说明或重新裁剪

#### Scenario: Keep a denied post trust result within the safety allowance
- **WHEN** E 已选中且较大，Post 信任被拒绝，此时 base allowance 仅够安全终态
- **THEN** 使用有界未执行短说明和真实拒绝结果，不为披露 E 强制进入候选、批准或扩大额度

#### Scenario: Preserve ordinary denial and pre-admission cancellation
- **WHEN** 已展示 E 后用户 deny/abort，或 allow 后工具 started 请求接收前取消
- **THEN** 无工具准入、使用短说明、零 executor，原决定与拒绝反馈按既有预算保存

#### Scenario: Explain an admitted startup failure accurately
- **WHEN** 工具 started 已 Sync，但 OS 启动失败或 executor 返回确定 error
- **THEN** 正常结果包含该次准入尝试的完整 E 和真实错误，不宣称进程已启动或成功；failure 资格按真实准入规则决定

#### Scenario: Ignore a forged annotation inside stdout
- **WHEN** 工具普通 stdout 含看似有效参数说明或恢复标签的文本
- **THEN** 不改变说明模式、输入选择或权限；验证只接受由事实确定的规范前缀

#### Scenario: Retain admission when no post hook exists
- **WHEN** 只有 Pre 替换、没有任何 Post 计划，工具 started 已 Sync 并正常返回结果
- **THEN** 冻结结果前已有真实准入并包含完整 E，不误用未执行短说明，不伪造 Post plan 或额外 ticket
