# extension/command-hooks Specification

## Purpose

定义本地同步 PreToolUse/PostToolUse 命令 Hook 的共同来源、配置、信任、受限执行与失败行为，使修改前门禁与成功后检查保持双 Provider 行为、权限隔离、隐私和取消清理的一致性。

## Requirements

### Requirement: Command hook configuration is bounded strict and frozen

系统 SHALL 显式加载用户 `~/.config/easycode/hooks.json` 与启动 workspace 根 `.easycode/hooks.json`，只接受顶层 hooks 对象中的 PreToolUse 与 PostToolUse 数组、每项 matcher/hooks 以及每条 id/type/command/timeout，以及仅 PostToolUse 支持的 feedback；type MUST 为 command。缺失文件表示空来源；重复/未知字段、非 UTF-8、大小写别名、null、尾随 JSON、错误类型与未支持事件或行为 MUST 使整个来源无效。timeout 省略为 5 秒，只允许整数 1..30；id 为 1..64 字节小写 ASCII 字母/数字/单连字符、首尾字母数字且来源内唯一，command 非空且最多 8 KiB。

每来源 SHALL 最多 64 KiB 和 32 条 Hook，合并最多 64 条；单调用每事件最多匹配 8 条。matcher MUST 是 `*` 或以 `|` 分隔的非重复字面名称：Edit/apply_patch 对应 fs.patch，Write 对应 fs.write，Bash/exec_command 对应 process.exec。空分支、未知名称或正则语法 MUST 拒绝；别名命中相同能力不得重复执行一条声明。`*` SHALL 仅指上述三项能力。

同一事件的全部匹配项 MUST 按用户来源、项目来源、配置数组位置稳定执行。source revision SHALL 由完整规范内容派生；绝对路径、时间、inode、执行或信任身份不得参与内容 revision。相同内容和数组顺序 MUST 产生相同 bytes/revision，getter 返回值修改不得改变快照。快照只在启动时建立，后续磁盘编辑不改变当前运行语义。


PostToolUse 的 feedback SHALL 仅接受 none/model，省略规范化为 none；PreToolUse 出现 feedback MUST 拒绝。每来源 32 条和合并 64 条上限按两个事件合计，id 在来源全部事件中唯一；规范配置显式按 PreToolUse、PostToolUse 排序，不以对象键插入顺序决定 revision。未知事件继续失败关闭。source revision 覆盖两事件全部声明，command fingerprint SHALL 绑定事件与披露策略。

#### Scenario: Load two ordered sources
- **WHEN** 用户和项目各有合法的匹配 Hook，文件对象键顺序不同
- **THEN** 用户项先于项目项执行，数组顺序保留，规范 bytes 与 revision 可重复

#### Scenario: Refuse a partial snapshot
- **WHEN** 一个已存在来源损坏、超限或包含未支持事件，另一个来源合法
- **THEN** 三项 mutation 调用以 hook_config_invalid 阻断，不从合法来源组成部分放行；普通聊天、只读工具与配置浏览仍可用

#### Scenario: Match aliases only once
- **WHEN** matcher 为 Bash|exec_command，当前 ready 调用属于 process.exec
- **THEN** 该声明只执行一次，输入中的真实 facade 名不被替换

#### Scenario: Freeze startup content
- **WHEN** 快照建立后磁盘配置发生编辑
- **THEN** 当前进程继续使用已捕获声明，getter、浏览和调用不重读配置；重启才建立新快照

#### Scenario: Freeze both events and disclosure mode
- **WHEN** 同一配置声明两个事件且 PostToolUse 省略 feedback，随后仅更改为 model
- **THEN** 首次规范值为 none，第二次 source revision/command fingerprint 改变，旧信任不适用；对象键顺序不影响 bytes

#### Scenario: Reject frontloaded disclosure on PreToolUse
- **WHEN** PreToolUse 声明包含 feedback 或两个事件合计超出来源数量上限
- **THEN** 整个来源无效且工具与 Hook 均不执行

### Requirement: Hook source checks bind to opened descriptors

配置读取 MUST 逐段拒绝 symlink，并由同一实际 FD 校验常规文件、大小及权限；源文件与受信任来源目录不得允许 group/other 写入。特殊文件、目录穿越、读取失败或捕获过程中检测到变化 MUST 使来源无效，不回显路径目标或内容，不退回普通路径读取。支持平台的既有系统路径别名可按固定规则处理，其他平台无法提供安全打开时 MUST 失败关闭。

#### Scenario: Replace a configuration path at open
- **WHEN** 确定性注入将配置或来源目录替换为 symlink 或特殊文件
- **THEN** 系统拒绝或仅读取已完成句柄校验的原对象，不读取未经校验的替换目标

#### Scenario: Reject writable source authority
- **WHEN** 实际打开的 Hook 文件或受信任来源目录允许 group/other 写入
- **THEN** 来源无效，任何 Hook 启动次数为零

### Requirement: Every hook needs narrowly bound process-local trust

用户及项目来源 SHALL 均默认未信任。每条信任 MUST 绑定当前 Session/workspace 实例、来源身份与 revision、Hook id、事件、feedback 策略、命令 fingerprint 以及 shell/environment/sandbox 身份；不得从项目打开、Skill 使用、普通工具批准、其他 Hook 或历史 Session 推导授权。指纹 SHALL 包含规范 matcher、命令和 timeout，不含宿主环境实际值。

首次运行前 MUST 提供完整命令、来源、事件、作用能力、超时、只读/禁网限制和脚本依赖可变化的审查；仅接受本进程会话信任或拒绝，默认拒绝。响应 MUST 关联活动 request/session/thread/turn/invocation 和快照身份；过期、重复、错配或取消后的响应在授权和执行前拒绝。拒绝状态保持至该项被显式 reset 或进程结束；reset 仅在无活动 turn 且目标身份匹配时清除状态，不建立信任、不重试工具。

信任和拒绝 MUST 由实例持有，不写配置或 Session，不跨重启、resume、Provider/workspace/Session 实例复用。信任不冻结所引用脚本的完整依赖内容，不得声称命令摘要证明了所有未来脚本 bytes。


PostToolUse 的全部匹配声明 SHALL 在原工具候选准备及副作用前独立审查；审查明确成功后执行以及 feedback=model 的内容将发送给 Provider 并保存到 Session。前置信任不能授权后置事件，即使 shell 命令相同。拒绝或无交互信任 MUST 在原工具执行前失败关闭，不能先修改后再询问。

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

### Requirement: Hook commands execute with explicit restricted lifecycle

Hook SHALL 只在三项 mutation 调用完整 ready 且对应事件计划和 Hook 执行 started 已成功 Sync 后启动。执行 MUST 使用非 login `/bin/sh`、最小环境、实际目录 FD 绑定的 workspace 根、工作区只读且禁网 sandbox；仅 `/dev/null` 必需行为允许文件写入。不得继承 Provider 凭据、敏感 Header/Cookie、Session 路径、内部控制变量或真实 home 写权限。命令参数不得由模型输入或环境插值形成；工具参数只通过 stdin 传递。

首批真实执行平台 SHALL 为 Darwin；交互运行路径中其他平台或 sandbox 不可兑现时 MUST 在信任请求与命令启动前返回 unsupported_platform 或 sandbox_unavailable，禁止无 sandbox fallback。headless 匹配项 SHALL 先按无交互信任规则返回 hook_trust_required，不进入平台执行前检。每条工具调用全部 Hook 的执行时间预算 SHALL 为 30 秒；单项期限不超过配置值及剩余预算，信任等待不计入执行预算但必须可取消。

唯一 owner MUST 管理 stdin writer、stdout/stderr readers、退出观察、直接进程 Wait 与受控进程组清理。取消/超时先 TERM，250 ms 后需要时升级 KILL，保留有界原组检查和 1 秒输出排空；清理完成前不得报告最终 Done、释放 thread lease 或接收下一 turn。不得把受控进程组描述为任意脱离子孙进程容器。


PreToolUse 保持候选/普通审批前门禁；PostToolUse started SHALL 另要求原 tool_execution_started 与成功 tool_call_result 已确定 Sync。每调用全部 Hook 的 30 秒执行预算由 Pre/Post 共用，不因事件切换重新计时。正常非零 Exec exit 仍按其真实 ResultSuccess 处理。Hook input 过大或执行错误在 Post 阶段只记录检查失败，MUST NOT 重写真实工具结果。

#### Scenario: Run a read-only local check
- **WHEN** 已信任 Hook 在具备安全 adapter 的平台运行
- **THEN** 能从 stdin 读到请求并读取工作区，写工作区、联网及访问 Session/真实 home 均被 sandbox 拒绝

#### Scenario: Cancel a blocked stdin writer
- **WHEN** 子进程不读 stdin 且 turn 被取消
- **THEN** owner 终止受控进程、解除 writer 阻塞并等待全部 owned 资源，不遗留后台 goroutine

#### Scenario: Retain ownership after shutdown timeout
- **WHEN** shutdown 超时而进程 Wait 或 reader 清理尚未完成
- **THEN** owner 与 lease 仍被持有、下一 turn 被拒绝；最终清理确认后才关闭 Done

#### Scenario: Share the execution budget across phases
- **WHEN** 前置检查已消耗同调用执行预算而成功工具之后到达 PostToolUse
- **THEN** 后置执行期限只使用剩余预算，耗尽项不启动且保留工具成功

### Requirement: Hook wire uses a typed input and a strict denial subset

PreToolUse stdin SHALL 是最多 512 KiB 的 UTF-8 JSON，含 PreToolUse 事件、session/thread/turn、tool_use_id、真实 tool_name、安全 cwd 和三分支 typed tool_input。Patch SHALL 包含 file_path 与有序 replacements 的 old_text/new_text/replace_all/line_based，Write 为 file_path/content，Exec 为 command/workdir/timeout_ms。不得发送 transcript 路径、完整历史、opaque reasoning 或凭据；超限以 hook_input_limit 阻断，不截断输入或启动进程。

各事件 stdout/stderr SHALL 各最多捕获 16 KiB。PreToolUse exit 0 且 stdout 为空白或严格空对象表示继续；唯一非空支持输出为 hookSpecificOutput 中 hookEventName=PreToolUse、permissionDecision=deny 与非空且最多 4 KiB 的 UTF-8 permissionDecisionReason。在清理确认且 I/O 正常有界时，PreToolUse exit 2 SHALL 以 hook_denied 阻断且不解析 stdout JSON；PreToolUse 其他非零退出 SHALL 以 hook_exit_failed 阻断。PreToolUse exit 0 的 allow/ask、updatedInput、additionalContext、旧 decision shape、async、未知/重复字段、尾随值和非法 UTF-8 MUST 以 hook_output_invalid 阻断。

多原因同时出现时 MUST 按 cleanup 不确定、turn 取消、timeout、输出超限/I/O 失败、exit 2、其他非零、exit 0 输出解析的顺序选择唯一分类，不能因退出码 2 掩盖清理不确定或因截断后的 JSON 放行。

原始 stdout/stderr/自由文本 reason MUST NOT 自动进入 Session、diagnostic、错误、模型 preview 或 artifact；只发布由宿主生成、最多 1 KiB 的检查身份与稳定 code 摘要。超过任一输出上限 MUST 清理进程并报告 hook_output_limit，不得将截断后恰好合法的 JSON 当作成功。该输入和输出 SHALL 声明为 EasyCode 支持子集，不承诺完整 Claude/Codex 脚本兼容。


以上 permissionDecision/exit 2 阻断语义及后续阻断场景仅适用于 PreToolUse。PostToolUse exit 2 SHALL 为 error/hook_exit_failed，其 validator 必须接受已知 exit_code=2；Pre 的相同组合仍必须拒绝，不能为共享 parser 全局放宽。两事件均按 cleanup 不确定、取消、实际超时、输出超限/I/O、普通退出与解析的优先级校验。未启动后缀使用独立 disposition，不伪造执行统计。PostToolUse SHALL 遵循 extension/post-tool-use 的独立成功后事件和结构化披露规则；只有显式 feedback=model、已信任且完整通过预算/身份验证的 additionalContext 可以成为冻结模型反馈。原始 stdout/stderr、未授权正文与执行错误仍 MUST NOT 被当成反馈保存或回显；PreToolUse 仍拒绝任何 additionalContext。

#### Scenario: Continue with empty output
- **WHEN** exit 0 且 stdout 只有空白或为一个严格空对象
- **THEN** 当前 Hook 继续，但不授予普通工具执行权限

#### Scenario: Reject unsupported approval output
- **WHEN** PreToolUse stdout 含 permissionDecision=allow、updatedInput 或 additionalContext
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
