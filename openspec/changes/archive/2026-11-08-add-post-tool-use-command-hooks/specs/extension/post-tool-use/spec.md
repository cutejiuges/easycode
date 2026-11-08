## Purpose

定义成功工具操作后的同步只读命令检查及显式模型反馈，使检查与真实工具结果保持独立、内容披露具有明确授权，且在取消、恢复、输出预算和双 Provider 续写中保持确定行为。

## ADDED Requirements

### Requirement: PostToolUse only observes durably successful executed mutations

系统 SHALL 仅对 fs.patch、fs.write、process.exec 的真实 execution_started 与成功 result 均确定 durable 后运行 PostToolUse。Exec 正常退出但 exit_code 非零仍沿用既有 ResultSuccess 语义。无匹配不产生 Post facts 或进程。前置拒绝、普通拒绝、准备错误、conflict、cancelled、outcome_uncertain 与未 accepted 预算拒绝 MUST NOT 运行后置命令，已计划项必须明确 skipped。

检查 outcome 与原结果 SHALL 独立。普通退出失败、超时、输入/输出超限及解析失败只形成检查失败，不撤销原副作用、不更改原 status/code、不生成第二个 tool result 或额外 Hook tool call。取消与 cleanup 不确定保持唯一 owner、资源等待和 turn 收口规则。

#### Scenario: Check a successful file change
- **WHEN** 获批 Edit/apply_patch 或 Write 已保存成功 result
- **THEN** 才可保存并接收匹配 Post started，检查失败仍保留原成功与唯一原生配对

#### Scenario: Check a nonzero command exit
- **WHEN** Bash/exec_command 正常结束、结果 success 且 exit_code 为非零
- **THEN** Post 收到真实 exit_code 并运行，不把该结果误作执行异常

#### Scenario: Do not check a denied or uncertain operation
- **WHEN** 原调用未成功执行或仅有 started 而无确定 result
- **THEN** Post 启动次数为零，原调用结果不被检查语义覆盖

### Requirement: PostToolUse receives a bounded stable tool response

stdin SHALL 为最多 512 KiB 的有效 UTF-8 JSON，顶层必填字段恰好为 hook_event_name、session_id、thread_id、turn_id、tool_use_id、tool_name、cwd、tool_input、tool_response。事件固定 PostToolUse；tool_use_id 为真实 Provider call ID，tool_name 保留真实 facade；cwd 为安全工作区根，tool_input 沿用三分支当前 typed 输入。tool_response 必填且恰好为 capability、status、code、metadata；status/code 固定 success/ok，能力与输入/facade 一致。各 metadata 的完整字段为：

- fs.patch：file_path、replacement_count、added_lines、removed_lines。
- fs.write：file_path、operation、bytes、lines、created_directories；operation 仅 create/overwrite。
- process.exec：workdir、exit_known、exit_code、stdout_bytes、stderr_bytes、output_truncated、timed_out、cancelled；exit 未知时用 false/0，已知时保留真实整数。

字段 SHALL 从已 durable 的真实结果元数据投影；计数为非负整数并满足对应现有 validator，路径为规范 workspace 相对路径。缺失/未知/重复字段、null、错误类型、跨能力字段与尾随 JSON MUST 拒绝。所有分支 SHALL 不提供 preview 或 stdout/stderr 正文，不读取 workspace/artifact 补齐，不透传或字符串清洗模型预览；MUST NOT 传递 native wire、artifact ID/存储路径、PID、环境、transcript 或 opaque reasoning。宿主字段不包含 Provider 凭据；用户工具参数仍遵守既有显式本地授权边界。

输入超限 SHALL 记录 hook_input_limit 且零脚本启动，保留原成功，不截断参数。只读/禁网沙箱、最小环境、FD 工作区、单项期限、输出上限与 cleanup SHALL 沿用 command-hooks 契约；不得隐式获得格式化或缓存写入权限。

#### Scenario: Reject an oversized combined input
- **WHEN** tool_input 加冻结结果及包装超过 512 KiB
- **THEN** 不执行该 Hook，保存有界失败分类且原成功结果不变

#### Scenario: Refuse a write from a post hook
- **WHEN** 已信任 Post 命令尝试修改工作区或联网
- **THEN** sandbox 拒绝，不因 Post 位于修改之后而获得写或网络权限

### Requirement: Model feedback requires explicit structured disclosure

反馈模式 SHALL 默认 none，只有 Post 声明显式 feedback=model 且相同快照/事件/策略已获独立信任，才能接收模型正文。信任必须在原工具副作用前完成，并明确该正文会发送给 Provider、随 Session 持久化。此授权不扩大普通工具权限。

Post exit 0 SHALL 只接受空白、严格空对象，或唯一 hookSpecificOutput 对象，包含 hookEventName=PostToolUse 和非空 UTF-8 additionalContext；原文最多 4 KiB、禁止 NUL。重复/未知字段、null、尾随值、错误事件、decision/continue/改写/async MUST 作为 hook_output_invalid。feedback=none 的非空内容 MUST 拒绝且不持久化，使用 hook_feedback_not_allowed；原始 stderr/普通 stdout 不作为模型正文。所有普通非零退出包括 2 均为 hook_exit_failed，不阻断已成功工具。

#### Scenario: Deliver explicitly selected context
- **WHEN** 已明确开启并信任的 Post 命令返回合法结构化 additionalContext
- **THEN** 仅该完整字段可成为有来源的冻结反馈，其余捕获字节不进入正文

#### Scenario: Keep default output private
- **WHEN** feedback 省略或为 none，脚本返回含合成 secret 哨兵的正文或 stderr
- **THEN** 模型、Session、错误、snapshot、diagnostic 和 artifact 均不包含哨兵，仅保留稳定分类

#### Scenario: Reject a post-execution block decision
- **WHEN** Post 返回 decision=block、permissionDecision 或 exit 2
- **THEN** 记录输出无效或执行失败，不撤销原成功，不自动重试工具

### Requirement: Frozen feedback retains provenance and complete content

每条被接收反馈 SHALL 具有精确 invocation/plan/event/ordinal/来源关联与独立预算事实，只保存确定通过披露、完整编码和预算验证的字段。模型中的来源标签只含事件、来源种类、合法 Hook id/ordinal 与内容或宿主 code，不含命令、路径、运行身份或时间。反馈 SHALL 为原工具数据，不提升为 system/developer 指令或伪造用户输入。

每块 MUST 完整接收或全部拒绝，拒绝 code 为 hook_feedback_budget_exceeded；禁止截断指令。已 durable 反馈恢复只复用冻结 bytes，UI 转义不回写正文，未授权输出与诊断正文不保存 artifact。身份错配、预算伪造或内容与证据冲突 MUST 失败关闭。

#### Scenario: Preserve an entire diagnostic
- **WHEN** 完整包装超过当前反馈预留但原文没有超过单项上限
- **THEN** 丢弃整块并保存 typed 预算分类，不裁剪成看似完整的成功反馈

#### Scenario: Reject feedback for another event or call
- **WHEN** 合法正文被关联到另一个 invocation、Pre 事件或 ordinal
- **THEN** 不接收该正文且不按字符串内容猜测归属

#### Scenario: Do not forward artifact references in an execution result
- **WHEN** 成功 Exec 的模型 preview 包含已保存 stdout/stderr 的 artifact_id 提示
- **THEN** Hook stdin 只包含上述结果元数据，无 preview/输出正文/artifact ID，生成过程不读 artifact 或按字符串过滤正文

#### Scenario: Decode each exact response branch
- **WHEN** 三能力的 Post stdin 出现缺失必填字段、错误数值类型、错误 exit known/zero 组合或跨能力 metadata
- **THEN** 当前 strict decoder 拒绝，真实工具结果不变且该 Hook 不执行
