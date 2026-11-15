## RENAMED Requirements

- FROM: `### Requirement: Hook wire uses a typed input and a strict denial subset`
- TO: `### Requirement: Hook wire uses typed input and strict bounded control output`

## MODIFIED Requirements

### Requirement: Every hook needs narrowly bound process-local trust

用户及项目来源 SHALL 均默认未信任。每条信任 MUST 绑定当前 Session/workspace 实例、来源身份与 revision、Hook id、事件、feedback 策略、命令 fingerprint 以及 shell/environment/sandbox 身份；不得从项目打开、Skill 使用、普通工具批准、其他 Hook 或历史 Session 推导授权。指纹 SHALL 包含规范 matcher、命令和 timeout，不含宿主环境实际值。

首次运行前 MUST 提供完整命令、来源、事件、作用能力或回合完成范围、超时、只读/禁网限制和脚本依赖可变化的审查；仅接受本进程会话信任或拒绝，默认拒绝。响应 MUST 关联活动 request/session/thread/turn、封闭 tool_call 或 turn_stop 目标及快照身份；过期、重复、错配或取消后的响应在授权和执行前拒绝。拒绝状态保持至该项被显式 reset 或进程结束；reset 仅在无活动 turn 且目标身份匹配时清除状态，不建立信任、不重试工具。

信任和拒绝 MUST 由实例持有，不写配置或 Session，不跨重启、resume、Provider/workspace/Session 实例复用。信任不冻结所引用脚本的完整依赖内容，不得声称命令摘要证明了所有未来脚本 bytes。


PostToolUse 的全部匹配声明 SHALL 在原工具候选准备及副作用前独立审查；审查明确成功后执行以及 feedback=model 的内容将发送给 Provider 并保存到 Session。前置信任不能授权后置事件，即使 shell 命令相同。拒绝或无交互信任 MUST 在原工具执行前失败关闭，不能先修改后再询问。

PreToolUse 审查 SHALL 明确脚本可以在原 facade 匹配范围内提出三能力规范参数的完整替换，已接纳候选会作为必要事实保存，只有 gate 通过才选择工具有效参数；原调用结果按真实准入/恢复状态提供完整或有界短说明。该能力不授予普通工具权限、不恢复 grant、不允许审批中编辑。拒绝、headless 或未完成信任时不能接纳替换或启动对应脚本。

#### Scenario: Trust one declaration
- **WHEN** 用户信任一条合法请求，随后相同快照的另一 Hook 被匹配
- **THEN** 另一 Hook 仍未信任，且普通 Patch/Write/Exec 审批均未因此授权

#### Scenario: Reject an old response
- **WHEN** 请求已取消或 turn/指纹变化后收到旧确认
- **THEN** 内存 trust、journal 与进程启动次数均不改变

#### Scenario: Reconsider a denial
- **WHEN** 空闲时用户对匹配快照的单项执行 reset，随后模型发出新的真实工具调用
- **THEN** 下一调用重新请求信任，不复用旧响应，不自动重试原调用

#### Scenario: Restore without trust
- **WHEN** 历史含已运行 Hook 的会话在新进程恢复
- **THEN** 历史事实不建立当前授权；未来调用仍须重新信任

#### Scenario: Isolate trust across events
- **WHEN** PreToolUse 同一命令已获信任而 PostToolUse 或其 feedback 策略尚未信任
- **THEN** 仍须独立审查；拒绝时原工具执行次数为零

Stop 的 mode/platform/input/执行额度前检 SHALL 遵循 extension/stop-hooks 的固定顺序；headless 在门禁不新增 Stop 专用正文投影或平台检查；既有启动 availability 探测不计入该门禁，已保存平台不可用快照不能覆盖 headless 的 hook_trust_required。其信任始终是已配置声明的进程内授权，不因 pending 用户输入、摘要或反馈交付产生任何普通工具权限。

Stop SHALL 在本次成功无 calls sample 与计划已 Sync/finalize、输入及平台预检通过后审查全部声明，全部信任前零 Stop 命令。Stop response MUST 精确关联 sample target 和 plan，不能复用工具信任响应。审查明确读取最终答复、model 内容将发送给 Provider 并存入 Session，以及先前已执行工具不会回滚。headless 无交互直接以 hook_trust_required 失败；信任不从历史恢复。

#### Scenario: Deny a completion review
- **WHEN** 用户在 Stop 审查接受默认拒绝
- **THEN** 已提交答复保留，零 Stop/后续采样，以唯一失败终态结束；此前工具事实不改写

#### Scenario: Reject a tool response for a stop target
- **WHEN** 用同一声明的旧 tool_call 响应回答 turn_stop 请求
- **THEN** 授权和执行次数不变，不能把 TurnID 作为 invocation 接受

#### Scenario: Review input replacement authority
- **WHEN** 用户首次审查一个 PreToolUse 声明
- **THEN** 可查看参数替换及保存/模型说明用途，Enter/Esc 仍默认拒绝，普通工具权限独立

### Requirement: Hook wire uses typed input and strict bounded control output

PreToolUse stdin SHALL 是最多 512 KiB 的 UTF-8 JSON，含 PreToolUse 事件、session/thread/turn、tool_use_id、真实 tool_name、安全 cwd 和三分支 typed tool_input。Patch SHALL 包含 file_path 与有序 replacements 的 old_text/new_text/replace_all/line_based，Write 为 file_path/content，Exec 为 command/workdir/timeout_ms。不得发送 transcript 路径、完整历史、opaque reasoning 或凭据；超限以 hook_input_limit 阻断，不截断输入或启动进程。

各事件 stdout/stderr SHALL 各最多捕获 16 KiB。PreToolUse exit 0 且 stdout 为空白或严格空对象表示继续；非空输出 SHALL 仅接受两个互斥分支：hookSpecificOutput 中 hookEventName=PreToolUse、permissionDecision=deny 与非空且最多 4 KiB 的 UTF-8 permissionDecisionReason；或者同一 hookEventName 加唯一 updatedInput 完整规范参数对象，语义遵循 extension/pre-tool-use-input-rewrite。在清理确认且 I/O 正常有界时，PreToolUse exit 2 SHALL 以 hook_denied 阻断且不解析 stdout JSON；PreToolUse 其他非零退出 SHALL 以 hook_exit_failed 阻断。PreToolUse exit 0 的 allow/ask、additionalContext、旧 decision shape、async、未知/重复字段、尾随值和非法 UTF-8 MUST 以 hook_output_invalid 阻断。

多原因同时出现时 MUST 按 cleanup 不确定、turn 取消、timeout、输出超限/I/O 失败、exit 2、其他非零、exit 0 输出解析的顺序选择唯一分类，不能因退出码 2 掩盖清理不确定或因截断后的 JSON 放行。

原始 stdout/stderr/自由文本 reason MUST NOT 自动进入 Session、diagnostic、错误、模型 preview 或 artifact。对未被严格接纳为必要执行参数的输出，仅发布宿主生成、最多 1 KiB 的检查身份与稳定 code 摘要；已接纳参数的说明另遵循 extension/pre-tool-use-input-rewrite 的封闭选择与预算规则。超过任一输出上限 MUST 清理进程并报告 hook_output_limit，不得将截断后恰好合法的 JSON 当作成功。该输入和输出 SHALL 声明为 EasyCode 支持子集，不承诺完整 Claude/Codex 脚本兼容。


以上 permissionDecision/exit 2 阻断语义及后续阻断场景仅适用于 PreToolUse。PostToolUse exit 2 SHALL 为 error/hook_exit_failed，其 validator 必须接受已知 exit_code=2；Pre 的相同组合仍必须拒绝，不能为共享 parser 全局放宽。三个事件均按 cleanup 不确定、取消、实际超时、输出超限/I/O、普通退出与解析的优先级校验。未启动后缀使用独立 disposition，不伪造执行统计。PostToolUse SHALL 遵循 extension/post-tool-use 的独立成功后事件和结构化披露规则；只有显式 feedback=model、已信任且完整通过预算/身份验证的 additionalContext 可以成为冻结模型反馈。原始 stdout/stderr、未授权正文与执行错误仍 MUST NOT 被当成反馈保存或回显；PreToolUse 仍拒绝任何 additionalContext。

PostToolUseFailure SHALL 使用 extension/post-tool-use-failure 定义的独立严格 stdin，而不是成功 tool_response。该事件与成功 Post 共享输出与披露约束，但 hookEventName 必须精确等于实际执行事件。PreToolUse 仅支持本条拒绝与完整参数替换子集；其他事件 MUST NOT 接受 updatedInput，不得因共享解析器扩大其输出权限。失败检查 exit 2 只表示 hook_exit_failed，不自动重试、拦截或改写已保存原结果。

原始双流禁存规则不禁止已严格接纳的规范参数作为必要执行事实及冻结结果说明保存；该例外 SHALL 仅限实际三能力 typed input，不能保存整个 stdin、未知输出字段、reason 或 additionalContext。完整替换与 deny 混合、对象字段不完整或类型错误 MUST 使用 hook_output_invalid，目标工具零执行。

updatedInput 的来源 profile、规范 16 KiB 大小及当前 finished 完整编码 SHALL 在 effect 接纳前核验；原始流计数不替代规范大小，HTML/Unicode 转义不能扩大持久化额度。本文的替换子集 SHALL 明确区别于 Claude 并行共享输入及 Codex allow+updatedInput 的 wire 规则，不按同名字段声称脚本可直接通用，也不自动接受 allow/ask。

#### Scenario: Continue with empty output
- **WHEN** exit 0 且 stdout 只有空白或为一个严格空对象
- **THEN** 当前 Hook 继续，但不授予普通工具执行权限

#### Scenario: Reject unsupported approval output
- **WHEN** PreToolUse stdout 含 permissionDecision=allow/ask 或 additionalContext
- **THEN** 当前调用以 hook_output_invalid 阻断，没有参数改写或工具自动批准

#### Scenario: Do not persist secret-looking output
- **WHEN** exit 2 的 stderr 或 structured deny reason 含合成 secret 哨兵
- **THEN** Session、模型结果、错误、日志、artifact 与界面 snapshot 均不含哨兵，只包含安全摘要

#### Scenario: Fail closed on timeout or malformed output
- **WHEN** PreToolUse Hook 超时、普通非零退出、输出超限或 JSON 解析失败
- **THEN** 对应调用阻断，目标工具不执行；结果使用稳定 typed code，不默默放行

#### Scenario: Validate exit two according to the event
- **WHEN** 同一 exit_known=true/exit_code=2 分别作为 PreToolUse 与 PostToolUse 结果
- **THEN** Pre 仅接受 denied/hook_denied，Post 接受 error/hook_exit_failed；事件错配的 status/code 组合拒绝

#### Scenario: Separate execution failure from an unstarted budget suffix
- **WHEN** Post 尚未 started 时同调用检查预算已经耗尽
- **THEN** 不构造 timed_out 运行结果，不启动进程，只以 hook_execution_budget_exhausted 收口未启动后缀

#### Scenario: Validate all three events independently
- **WHEN** 相同退出码 2 或带 additionalContext 的 stdout 分别用于三个事件
- **THEN** Pre 保持 deny/拒绝自由文本反馈规则，两个 Post 只按各自事件接收显式 model 内容，非零退出均为检查失败

本条三个工具事件 wire 约束 SHALL 保持事件隔离；Stop 输入/输出仅按 extension/stop-hooks 校验。Stop 的结构化 block 不能放宽 Pre 的 additionalContext 拒绝，也不能把两个 Post 的 exit 2 重分类为阻断。只有已信任 Stop 的显式 model reason 可成为独立冻结反馈；原始双流和未授权 reason 仍不得持久化或展示。

#### Scenario: Keep all four parsers independent
- **WHEN** 同一 stdout JSON/退出码用于四事件
- **THEN** 各按实际事件严格验证，不以新增 Stop block 规则改变现有三事件结果

#### Scenario: Accept a bounded standalone replacement
- **WHEN** 已信任 Pre 正常 exit 0，仅返回正确事件和同能力完整 updatedInput，全部输入/输出边界合法
- **THEN** 参数进入严格验证与 durable 接纳路径，尚不表示原工具已获批准或执行

#### Scenario: Reject control output in another event
- **WHEN** PostToolUse、PostToolUseFailure 或 Stop 输出 updatedInput，或 Pre 将 deny 与替换混用
- **THEN** 按真实事件拒绝非法输出，不改写参数或扩大其他事件权限
