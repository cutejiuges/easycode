# extension/post-tool-use-failure Specification

## Purpose

定义执行尝试确定失败后的同步只读命令 Hook，使用户能够在保持原工具错误、权限与取消边界的前提下提供项目特定诊断，并通过事实驱动事件选择、独立启动门禁、明确披露及严格恢复保持双 Provider 行为一致。

## Requirements

### Requirement: Failure hooks only observe definite admitted mutation failures

系统 SHALL 仅对 fs.patch、fs.write、process.exec 的执行尝试准入与确定 error/conflict result 均已 Sync 的调用选择 PostToolUseFailure；Exec 不支持 conflict。tool_execution_started 只表示 executor 执行尝试已准入，不证明进程已启动或文件已改变，准入后的启动失败同样可形成确定失败。原工具结果与 Hook outcome MUST 独立，不生成第二个原工具 result、不自动重试、不递归触发失败 Hook。正常非零退出的 Exec SHALL 保持现有 success/ok 和真实 exit_code，只选择 PostToolUse，包括 go test 或 rg 正常返回 1；本能力不提供命令语义解释或测试失败自动修复。

准备失败、准备冲突、普通或 Hook 拒绝、取消、结果不确定、预算 admission 拒绝、结果保存失败及恢复补偿 MUST NOT 启动失败检查。当前 turn 已取消或 writer/runtime poisoned 时 MUST NOT 以不可取消 context 继续启动检查。无匹配事件不执行脚本、不伪造运行统计。

系统 SHALL 分离原工具准入/结果事实、后置选择 success/failure/none、检查终态 completed/skipped/cancelled/uncertain。选择只由同一调用已验证的 durable 准入和原结果决定：已准入 success 为 success，已准入 error/conflict 为 failure；无准入或原结果 denied/cancelled/outcome_uncertain 为 none。none 只表示不选择后置事件，不能表示原工具未执行。尚无确定 durable 原结果时 MUST NOT 进行选择。当前 context、配置、剩余预算或恢复模式不得改变事实选择。

存在对应分支时，当前取消 SHALL 在 stdin/剩余期限前检与 Hook started 准入之前处理；它只改变检查终态，不改原结果或已确定选择。writer/runtime poisoned 阻止后续脚本 admission，按原持久化/恢复边界处理；恢复永不运行脚本。选择、启动门禁和终态不得由错误文字、preview、PID 或时间推断。

#### Scenario: Diagnose a definite execution failure
- **WHEN** 已批准的文件 mutation 执行尝试已准入后返回确定 error/conflict，或 Exec 超时且 cleanup 已确认，原 result 已 Sync
- **THEN** 仅选择已信任的失败分支，原 result/status/code 与 Provider call/output pairing 不变

#### Scenario: Keep a nonzero completed command on its existing event
- **WHEN** Exec 正常退出且 result=success/ok、exit_code 非零
- **THEN** 不运行失败检查，沿用既有 PostToolUse 和真实 exit_code

#### Scenario: Do not diagnose an unexecuted or uncertain action
- **WHEN** 原调用没有执行尝试准入，或原结果为 denied/cancelled/outcome_uncertain
- **THEN** 选择为 none，失败脚本启动次数为零；不据此反推原工具是否已执行

#### Scenario: Diagnose a failed start after attempt admission
- **WHEN** Exec 执行尝试准入已 Sync，OS 进程尚未启动便返回确定 process_start_failed，原 result 已 Sync
- **THEN** 选择 failure，有匹配且启动前检通过时可运行失败诊断，不宣称原进程运行过

#### Scenario: Preserve selection when cancellation follows the error
- **WHEN** 原 error result 已 Sync，匹配失败分支存在，但检查 started 前 turn 被取消
- **THEN** 选择仍为 failure，检查以 cancelled/cancelled 收口，原错误保留且脚本启动次数为零

#### Scenario: Select no event for a cancelled original result
- **WHEN** 原工具执行尝试已准入，随后原 result 本身以 cancelled 保存
- **THEN** 选择为 none、无 Hook started/finished/反馈；不因 none 宣称原工具没执行

#### Scenario: Derive the same selection during recovery
- **WHEN** 相同准入与 error/conflict result 在实时路径、取消后的路径或离线恢复中被验证
- **THEN** 均选择 failure；当前状态只影响启动或终态，恢复零脚本，不把选择改成 none

### Requirement: Both possible post events are reviewed before mutation

系统 SHALL 在 Pre 门禁通过后、候选准备和普通批准之前冻结同调用两个可能后置分支，按 PostToolUse、PostToolUseFailure 及各自来源/配置顺序完成独立信任。仅保存有匹配的分支；每分支最多 8 项。同一命令的跨事件信任 MUST NOT 复用，任一匹配分支未信任、被拒绝或取消均不得进入原工具执行。

已验证的 durable 准入/原结果 SHALL 确定唯一 success/failure/none 选择，只允许与选择一致的事件运行；另一分支不得产生 finished、模型提示或执行统计。success/failure 对应事件未配置但存在另一分支时 SHALL 保留选择、固定 skipped/hook_event_unmatched，无 Hook started/finished/反馈，processed/unstarted_from=0，不要求存在未运行后缀；turn 取消仍由外层取消流程处理。none 不借用任何分支，同样无运行与反馈。无任何匹配保持既有无 Post 路径。headless 匹配任一后置分支 SHALL 返回 hook_trust_required，零脚本与目标副作用。

#### Scenario: Trust two mutually exclusive possibilities
- **WHEN** 同一次 Write 匹配成功和失败声明且两者已分别信任
- **THEN** 执行前冻结完整计划，结果确定后最多运行一个分支，不重复预留或关闭调用

#### Scenario: Refuse an untrusted failure handler before changing a file
- **WHEN** 成功分支已信任但失败分支未获授权，或 headless 无交互信任
- **THEN** 工具不进入候选或执行，没有文件修改和事后授权请求

#### Scenario: Skip an unmatched selected event
- **WHEN** 只配置失败分支而工具执行成功
- **THEN** 后置明确 skipped/hook_event_unmatched，零失败进程、零反馈，释放唯一预留

### Requirement: Failure stdin is typed bounded and contains only a safe error summary

失败 stdin SHALL 为最多 512 KiB 的严格 UTF-8 JSON，顶层必填字段恰好为 hook_event_name、session_id、thread_id、turn_id、tool_use_id、tool_name、cwd、tool_input、error、is_interrupt。事件固定 PostToolUseFailure，tool_use_id 为真实 Provider call ID，tool_name 保留原 facade，cwd 为安全 workspace 根，tool_input 使用既有三分支封闭输入。is_interrupt MUST 为 false。

error SHALL 为宿主生成英文 `Tool execution failed: <code>`，只从已验证原结果的 1..128 小写 ASCII 字母/数字/下划线 code 生成；不满足投影限制时使用固定 tool_execution_failed。摘要最多 256 bytes、无 NUL，不包含底层 error、preview、stdout/stderr、artifact 引用或磁盘读取内容。不得包含 tool_response、transcript、历史、opaque reasoning、PID、环境或 Provider 凭据。原用户参数继续受既有本地信任边界约束。

缺失/未知/重复键、null、错误类型、错事件、非法身份、is_interrupt=true 或尾随 JSON MUST 拒绝。输入超限 SHALL 保存 hook_input_limit 并零启动，不裁剪参数、不改变原结果；不通过其他文件或 artifact 读取路径补齐。

#### Scenario: Project a safe failure without copying stderr
- **WHEN** 原 Exec 结果为 error/timed_out，preview 或底层错误中有合成 secret 哨兵
- **THEN** stdin 的 error 仅为 Tool execution failed: timed_out，原输出哨兵和 artifact ID 不进入投影

#### Scenario: Refuse a success-shaped failure request
- **WHEN** 失败 stdin 含 tool_response、缺失 is_interrupt 或将其设为 true
- **THEN** 严格拒绝且不启动进程，不接受另一个事件的输入形状

#### Scenario: Refuse an oversized failure input
- **WHEN** 完整工具参数与失败包装超过 512 KiB
- **THEN** 原错误保留，后置记录输入超限，脚本执行次数为零

### Requirement: Failure feedback is explicit complete and independent of the original error

失败事件 SHALL 默认 feedback=none；仅 model 且该精确事件/来源/策略已获执行前信任时，允许把严格 additionalContext 作为原调用的冻结工具数据发送给 Provider 并保存 Session。exit 0 仅接受空白、严格空对象，或唯一 hookSpecificOutput，其中 hookEventName=PostToolUseFailure、additionalContext 为非空无 NUL UTF-8 且最多 4 KiB。正文必须完整通过实际编码、身份与共享预算验证，不截断。

none 的非空正文 SHALL hook_feedback_not_allowed；错事件、未知/重复字段、null、尾随值、decision/continue/permissionDecision/改写/async SHALL hook_output_invalid；普通非零退出包括 2 SHALL hook_exit_failed。原 stdout/stderr 与被拒正文 MUST NOT 进入 Session、模型、错误、snapshot、diagnostic 或 artifact。检查普通错误不改变原错误、不制造额外工具调用，不提升反馈为 system/developer 或用户输入。

执行 SHALL 沿用现有只读/禁网沙箱、最小环境和进程 owner，单项默认 5 秒/最大 30 秒，与 Pre 共用每调用 30 秒执行预算；输出各 16 KiB。取消/超时等待 owned 资源清理，shutdown 超时不释放 lease 或接收下一 turn；不支持安全 adapter 的平台失败关闭。

#### Scenario: Deliver project advice while retaining the tool failure
- **WHEN** 已信任 model 失败检查返回合法 additionalContext
- **THEN** 仅正文和安全来源按原调用输出配对提交，两家 Provider 仍看到相同原错误，不自动重试

#### Scenario: Keep failure checks private by default
- **WHEN** none 检查输出带合成 secret 的 additionalContext、普通 stdout 或 stderr
- **THEN** 只保存安全统计与分类，所有未授权正文不进入持久化或展示

#### Scenario: Do not turn a hook error into a new tool failure
- **WHEN** 失败检查自身 exit 2、超时或输出非法，且 cleanup 已确认
- **THEN** 保留原工具错误和独立检查错误，不递归运行失败 Hook，后续允许的调用仍保持原顺序
