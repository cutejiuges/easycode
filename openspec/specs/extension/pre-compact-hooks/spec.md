# extension/pre-compact-hooks Specification

## Purpose

在手动与自动会话压缩向模型请求摘要前，为用户提供受限的本地检查。用户可以授权检查阻止本次压缩，或为交接摘要提供工作区重点，并在取消、失败和恢复时保留可验证的压缩边界。

## Requirements

### Requirement: PreCompact matches a frozen compaction trigger

系统 SHALL 从既有启动冻结的用户与项目 Hook 来源读取 `PreCompact` 同步 command 声明。group 的 `matcher` MUST 是单一字面值 `manual`、`auto` 或 `*`。`*` 匹配两种压缩触发，空值、组合表达式、大小写别名和正则 MUST 拒绝。`feedback` SHALL 为 `none` 或 `model`，省略等同 `none`。声明按用户来源、项目来源及各自数组顺序稳定匹配，适用共享每次 8 项、每来源 32 项、合计 64 项及来源内唯一 ID 上限。

只有基础纯预检接纳并冻结本次压缩身份、来源和作用域后 SHALL 选择匹配项。合法来源无匹配时 MUST 沿用原有摘要路径，零检查、零审查、零摘要请求字节差异。存在损坏来源或匹配超限 MUST 持久化无尝试的阻止结论，以 `hook_config_invalid` 停止本次压缩，不以另一合法来源部分放行或推断无匹配。该门禁独立于工具 Hook、Stop 及普通采样。

#### Scenario: Select only the current trigger

- **WHEN** 用户配置 `manual`，项目按顺序配置 `auto` 和 `*`，自动阈值接纳本次压缩
- **THEN** 只选择项目 `auto` 和 `*`，在审查通过后按声明顺序各运行一次

#### Scenario: Keep nonmatching compaction identical

- **WHEN** 配置只匹配 `manual`，且自动压缩合法接纳
- **THEN** 不生成检查尝试或信任请求，发送与无 Hook 配置相同的 canonical 摘要请求

#### Scenario: Reject invalid source or more than eight matches

- **WHEN** 一个来源损坏，或同一次压缩的合法匹配超过八项
- **THEN** 零脚本、零摘要网络调用、不安装候选窗口，不用剩余合法声明部分放行

### Requirement: All matching declarations are trusted before execution

每项信任 SHALL 复用当前进程的逐声明授权，与事件、来源 revision、命令、匹配范围、披露策略和 workspace 绑定。请求及响应 MUST 另外绑定当前 session/thread、真实 compaction ID 与作用域、计划摘要及声明。手动目标无 TurnID，自动目标使用当前真实 TurnID 和采样下标。任何已有工具/Stop 信任、已完成或错误作用域响应 MUST 无法授权本次检查。所有匹配项先通过有效信任再启动任何进程。非交互匹配 SHALL 以 `hook_trust_required` 停止本次压缩，在本次 Hook 执行前检、审查和启动之前收口。先前已观察的平台能力不得覆盖该分类。交互路径在平台和安全执行能力不可用时 MUST 在审查前失败。

进程 SHALL 复用共享只读、禁网、最小环境的命令执行和清理契约，串行执行稳定顺序的连续前缀。本次压缩检查的进程阶段总执行额度至多 30 秒，单项期限不超过声明超时及剩余额度。信任等待和之后的 Provider 请求独立于该额度。到达执行期限后仍须完成进程组、Wait 和流清理。取消 SHALL 停止未准入后缀并等待当前已接收命令及双流实际清理，未准入项不得伪造 started 或 timeout。

#### Scenario: Later denial prevents all matching scripts

- **WHEN** 第一项获得会话信任，但用户拒绝第二项
- **THEN** 本次压缩失败关闭，两个脚本都不启动，不请求摘要，活动窗口保持旧提交状态

#### Scenario: Isolate response targets

- **WHEN** 旧 compaction ID、另一作用域、另一声明或工具/Stop 目标的响应到达
- **THEN** 授权和执行次数不变，手动检查不接受伪造的 TurnID

#### Scenario: Stop the remainder when process budget is exhausted

- **WHEN** 第一个已信任命令用尽本次检查的进程额度，第二项还未启动
- **THEN** 零新增进程，保留已确认前缀，按已知额度耗尽收口且不发摘要请求

### Requirement: PreCompact wire exposes only scoped input and bounded control

stdin MUST 是共享输入上限内的严格 UTF-8 JSON。它 SHALL 包含 `hook_event_name: PreCompact`、`session_id`、`thread_id`、真实 `compaction_id`、当前规范 `cwd`、`trigger` 及 `custom_instructions`。`custom_instructions` 只包含本次基础预检冻结的规范用户指令，空指令为 JSON `null`。`manual` MUST 不含 turn/sample 字段。`auto` MUST 含实际 `turn_id` 和 `sample_index`。系统 SHALL 为所有声明提供相同的原始用户指令，不把前一条 Hook 的输出放进下一条 stdin。MUST NOT 发送 transcript 路径、摘要历史、Provider 原生或推理内容、待提交输入、工具参数或凭据。

stdout/stderr SHALL 分别按共享 16 KiB 限额捕获。限额未超且进程清理确定时，exit 0 的空白、严格空对象 `{}` 或 `{ "continue": true }` 允许继续且不添加内容。只有单字段 `{ "continue": false }` 明确阻止压缩。继续分支可包含 `hookSpecificOutput` 对象，`continue` 可显式为 `true` 或省略。该对象完整字段必须为 `hookEventName: PreCompact` 与非空 UTF-8 `summaryInstructions`，只在声明为 `feedback: model` 时可进入接纳。普通非零退出（包括 2）、格式错误、未知字段（包括 `stopReason` 和 `decision`）、重复字段、纯文本、错误事件、`null`、尾随值、阻止与反馈混合及未经允许的披露 MUST 使本次压缩失败。清理不确定优先于取消，取消优先于超时，超时优先于流失败/超限，流失败/超限优先于退出码与 exit 0 解析。

#### Scenario: Pass the same scoped manual input to each declaration

- **WHEN** 用户以非空 `/compact` 重点触发两条匹配声明，第一个提供合法摘要重点
- **THEN** 双方收到同一规范用户重点和真实压缩身份，第二项不收到第一项的结果，手动输入无 turn/sample 字段

#### Scenario: Keep pending input out of automatic hook input

- **WHEN** 轮到一条带文件附件的待提交用户输入，阈值决定先自动压缩
- **THEN** Hook 只接收真实自动作用域和 `custom_instructions: null`，没有待提交输入、附件或上次会话文本

#### Scenario: Distinguish explicit block from exit failures

- **WHEN** exit 0 返回 `continue:false`，或另一次命令在 exit 2 下返回相同 stdout
- **THEN** 前者记录明确阻止，后者记录退出失败，二者均无摘要请求或新窗口

#### Scenario: Stop the checked sequence on explicit block

- **WHEN** 三项声明均已获得信任，第一项继续，第二项以 exit 0 返回 `continue:false`
- **THEN** 第三项未准入，当前操作记录 `hook_denied`，无摘要请求。若第二项同时携带 `stopReason`，则按 `hook_failed` 关闭且隐藏自由文本

#### Scenario: Reject unsupported feedback

- **WHEN** 声明为 `none` 仍返回摘要重点，或任一声明返回纯文本、重复字段或混合阻止和摘要重点
- **THEN** 拒绝该输出、不发布正文、不产生摘要请求、不启用任何其他 Hook 的输出语义

### Requirement: Accepted summary instructions are bounded and ephemeral

每条已授权且成功接纳的 `summaryInstructions` SHALL 经首尾空白规范化并受 4 KiB UTF-8 上限。组合后的规范用户指令在前，Hook 输出在后，内容按执行顺序以固定空行分隔。完整组合含分隔符 MUST 最多 16 KiB。空用户指令不生成空前缀。组合超限 MUST 以 `compaction_budget_exceeded` 停止本次压缩，不静默截断、部分合并或发送原请求作为替代。

只有所有匹配项结果允许继续，且以同一冻结来源、历史、推理与输出配置通过完整摘要预算重新预检后，组合指令 SHALL 成为本次摘要请求的额外关注点。已接纳文本只发给该摘要请求，不形成普通用户消息、新普通采样输入、Tool 调用、授权、稳定缓存前缀或目录来源。生成的摘要仍可能包含获授权提交的资料，并在 checkpoint 持久化和成为以后续写的历史参考。

Mode 为 `none` MUST 不向模型披露脚本输出。原始双流、自由文本、已解析或组合的指令正文 MUST NOT 直接写入 Session、诊断、错误、artifact、界面或 headless。检查事实仅保留验证所需的身份、状态、字节长度、摘要和有界计数。摘要模型生成的 checkpoint 不用于重建 Hook 原输出。

#### Scenario: Reveal accepted context only in the summary request

- **WHEN** 受信任的 model 声明返回有界合法重点，且完整请求通过预算
- **THEN** 两家 Provider 的摘要请求各按同一顺序附带重点，普通请求和稳定缓存前缀不直接附带此输入，检查事件和 Session 检查记录不含其正文

#### Scenario: Recheck the actual expanded request

- **WHEN** 基础请求预算通过，但新增重点的实际编码超出摘要请求预算
- **THEN** 以确定预算错误关闭压缩，零摘要网络调用，旧窗口仍有效

### Requirement: PreCompact facts close once without replaying effects

匹配的压缩检查 SHALL 由当前 compaction ID、作用域、已验证来源和冻结声明计划统一关联。计划及每次执行准入 MUST 在进程启动前 Sync。检查结果和唯一决定 MUST 按执行前缀顺序保存，摘要调用只在完整允许决定已 Sync 且最终请求预检通过后开始。网络准入前 MUST 再次观察取消。若取消先于网络准入，已持久化的允许决定仍保留，并以 `compaction_cancelled` 结束，零摘要请求。Hook 阻止、失败、取消或预算拒绝 MUST 通过压缩失败终态关闭本次操作，不能安装新窗口或撤销已提交用户、工具或 Stop 事实。写入结果不确定时沿用 poisoned 和资源所有权收口。

压缩终态 SHALL 按对应的安全失败分类发布 `hook_config_invalid`、`hook_trust_required`、`hook_trust_denied`、`hook_denied` 或 `hook_failed`。取消与摘要预算沿用既有压缩失败分类。更细的检查原因只以有界内部 Hook code 保存或展示。

恢复 SHALL 只从验证通过的已提交压缩与检查事实构成安全结果，不重启脚本、不恢复授权、不重新读取 Hook 文件重建旧输出、不补发旧摘要请求。成功操作使用既有原生 checkpoint。未完成操作按恢复契约结束并保持原窗口；未确认进程结果不得推断通过。

#### Scenario: Recover a crash after hook admission

- **WHEN** 进程在检查准入 Sync 后、确定结果尚未提交时退出
- **THEN** 恢复验证已有前缀后以必要的本地失败事实收口，零旧 Hook 或摘要网络调用，重复恢复不重复补偿
