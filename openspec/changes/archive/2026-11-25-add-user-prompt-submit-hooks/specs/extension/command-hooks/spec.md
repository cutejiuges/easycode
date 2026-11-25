## MODIFIED Requirements

### Requirement: Command hook configuration is bounded strict and frozen

系统 SHALL 显式加载用户 `~/.config/easycode/hooks.json` 与启动 workspace 根 `.easycode/hooks.json`，只接受顶层 hooks 对象中的 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact、PostCompact、SessionStart、UserPromptSubmit 数组；三个工具事件、两个压缩事件和 SessionStart 的 group 含 matcher/hooks，Stop 和 UserPromptSubmit group 只含 hooks 且 MUST 拒绝任何 matcher；每条含 id/type/command/timeout，两个工具后置事件、Stop、PreCompact、PostCompact、SessionStart 和 UserPromptSubmit 支持 feedback；type MUST 为 command。缺失文件表示空来源；重复/未知字段、非 UTF-8、大小写别名、null、尾随 JSON、错误类型与未支持事件或行为 MUST 使整个来源无效。timeout 省略为 5 秒，只允许整数 1..30；id 为 1..64 字节小写 ASCII 字母/数字/单连字符、首尾字母数字且来源内唯一，command 非空且最多 8 KiB。

每来源 SHALL 最多 64 KiB 和 32 条 Hook，合并最多 64 条；单调用每事件最多匹配 8 条。工具事件的 matcher MUST 是 `*` 或以 `|` 分隔的非重复字面名称：Edit/apply_patch 对应 fs.patch，Write 对应 fs.write，Bash/exec_command 对应 process.exec。空分支、未知名称或正则语法 MUST 拒绝；别名命中相同能力不得重复执行一条声明。工具事件的 `*` SHALL 仅指上述三项能力。两个压缩事件的 matcher SHALL 仅接受单一字面 `manual`、`auto` 或 `*`，其中 `*` 匹配两种压缩触发。SessionStart 的 matcher SHALL 仅接受单一字面 `startup`、`resume`、`compact` 或 `*`，其中 `*` 匹配三个已支持来源。

同一事件 SHALL 按用户来源、项目来源、配置数组位置稳定选择，按相同顺序执行实际获准项。source revision SHALL 由完整规范内容派生；绝对路径、时间、inode、执行或信任身份不得参与内容 revision。相同内容和数组顺序 MUST 产生相同 bytes/revision，getter 返回值修改不得改变快照。快照只在启动时建立，后续磁盘编辑不改变当前运行语义。


PostToolUse/PostToolUseFailure/Stop/PreCompact/SessionStart 的 feedback SHALL 仅接受 none/model，省略规范化为 none。PostCompact 和 UserPromptSubmit 仅支持省略或 none；PreToolUse 出现 feedback MUST 拒绝。每来源 32 条和合并 64 条上限按八个事件合计，id 在来源全部事件中唯一；规范配置显式按 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact、PostCompact、SessionStart、UserPromptSubmit 排序，不以对象键插入顺序决定 revision。未知事件继续失败关闭。source revision 覆盖八事件全部声明，command fingerprint SHALL 绑定事件与披露策略。

#### Scenario: Load two ordered sources
- **WHEN** 用户和项目各有合法的匹配 Hook，文件对象键顺序不同
- **THEN** 用户项先于项目项执行，数组顺序保留，规范 bytes 与 revision 可重复

#### Scenario: Refuse a partial snapshot
- **WHEN** 一个已存在来源损坏、超限或包含未支持事件，另一个来源合法
- **THEN** 三项 mutation 调用以 hook_config_invalid 阻断，不从合法来源组成部分放行；已保存答复、只读工具与配置浏览仍可用，但最终完成边界 MUST 以 hook_config_invalid 失败且零 Stop 执行。接纳的压缩同样以 hook_config_invalid 收口且零 PreCompact 执行，不能通过损坏来源推断没有 Stop 或 PreCompact。待发送用户提交以 hook_config_invalid 拒绝且零 UserPromptSubmit 执行

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
- **WHEN** PreToolUse 声明包含 feedback 或八个事件合计超出来源数量上限
- **THEN** 整个来源无效且工具与 Hook 均不执行

#### Scenario: Freeze failure declarations in the same source
- **WHEN** 同一来源同时配置三事件，失败声明使用 feedback=model，且对象键顺序不同
- **THEN** 规范顺序固定、revision 相同；三事件合计数量/id 约束和每事件匹配上限均生效

#### Scenario: Reject duplicate ids across success and failure
- **WHEN** 同来源成功和失败声明使用相同 id
- **THEN** 整个来源无效，不以另一事件或合法来源部分放行 mutation

Stop SHALL 仅按回合完成事件选择，不与任一工具能力匹配。每候选最多 8 项，保持 user/project 和数组顺序；八事件共享来源计数/id 唯一规则。合法无诊断且 Stop 为空时保留无 Stop 快速路径，既有工具 matcher 的 * 仍只指三种 mutation。

#### Scenario: Load a completion hook without a matcher
- **WHEN** Stop group 只含合法 hooks，feedback 省略
- **THEN** 规范化为 feedback=none 并保持顺序；任何 Stop matcher、八事件重复 id 或合计超限使来源无效

#### Scenario: Preserve a valid tool-only configuration
- **WHEN** 两来源合法且未配置 Stop
- **THEN** 聊天完成沿既有无 Stop 路径，不新增 Stop facts 或请求内容

#### Scenario: Freeze compaction declarations with other events

- **WHEN** 一个合法来源同时配置工具、Stop 与 PreCompact，PreCompact 使用 `manual` 并省略 feedback
- **THEN** 规范顺序不受 JSON 键顺序影响，PreCompact 规范化为 feedback=none，其 id 和数量纳入该来源全部事件的共享约束
- **THEN** 将 PreCompact matcher 改为 `manual|auto` 使整个来源无效

#### Scenario: Freeze an observation hook with the shared source

- **WHEN** 同一来源同时含两个压缩事件，PostCompact 缺省 feedback 且 trigger matcher 是 `*`
- **THEN** PostCompact 以 none 规范化，参与同一来源的顺序、revision、条数和 ID 约束，独立于 PreCompact 匹配
- **THEN** 将 PostCompact feedback 改为 model 时拒绝整个来源

#### Scenario: Freeze SessionStart declarations with existing events

- **WHEN** 同一来源混合三种 SessionStart matcher、PostCompact 及工具检查，且仅交换对象键顺序
- **THEN** 得到相同规范 bytes 和 revision，配置位置仍决定同一事件的执行顺序
- **THEN** matcher 为 clear、fork、正则或包含多个分支时，整个来源无效

#### Scenario: Freeze prompt checks in the common event inventory

- **WHEN** 同一来源合法配置 UserPromptSubmit 和已有事件，并且只交换 JSON 对象键顺序
- **THEN** 规范 bytes 与 revision 保持一致，UserPromptSubmit 参与共享来源数量、ID 唯一性和命令指纹约束
- **THEN** 给 UserPromptSubmit 添加 matcher 或 feedback=model 时整个来源无效

### Requirement: Every hook needs narrowly bound process-local trust

用户及项目来源 SHALL 均默认未信任。每条信任 MUST 绑定当前 Session/workspace 实例、来源身份与 revision、Hook id、事件、feedback 策略、命令 fingerprint 以及 shell/environment/sandbox 身份；不得从项目打开、Skill 使用、普通工具批准、其他 Hook 或历史 Session 推导授权。指纹 SHALL 包含规范 matcher、命令和 timeout，不含宿主环境实际值。

首次运行前 MUST 提供完整命令、来源、事件、作用能力、回合完成范围、压缩触发范围、SessionStart 来源或提示词明文披露范围、超时、只读/禁网限制和脚本依赖可变化的审查；仅接受本进程会话信任或拒绝，默认拒绝。响应 MUST 关联活动 request/session/thread、封闭 tool_call、turn_stop、compaction、session_start 或 user_prompt_submit 目标及快照身份。工具与 Stop 使用真实 turn 关联，PreCompact 使用真实 compaction ID 及其作用域，手动无 TurnID、自动绑定真实 turn 与采样下标；过期、重复、错配或取消后的响应在授权和执行前拒绝。拒绝状态保持至该项被显式 reset 或进程结束；reset 仅在无活动 turn、无独立手动压缩且目标身份匹配时清除状态，不建立信任、不重试工具。

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

PreCompact SHALL 在基础压缩预检接纳且检查计划已 Sync 后，按 extension/pre-compact-hooks 的平台与输入前检以及逐项信任顺序工作。审查明确运行在本次摘要请求之前、可阻止压缩。feedback=model 的合法摘要重点将发给本次摘要 Provider，生成的摘要可能保留这些内容。全部匹配声明通过信任前零脚本。无交互或拒绝时停止本次压缩，信任和拒绝不从历史恢复。

#### Scenario: Require all compaction trust before running the first declaration

- **WHEN** 第一条 PreCompact 声明已受信任而第二条被拒绝
- **THEN** 当前未通过审查的声明不获授权，本次拒绝后零 PreCompact 进程和摘要请求

SessionStart 的信任 SHALL 按 extension/session-start-context 精确绑定新建或恢复的实际首轮，或本次成功压缩的 CompactionID、scope、checkpoint 序号与原生来源。审查同时显示命令、读取范围及显式模型披露将保存到 Session 的事实。同命令的 PostCompact 或之前的目标响应不能授权本次。

#### Scenario: Reject a PostCompact response for SessionStart

- **WHEN** 已安装同一 checkpoint 且两个阶段使用相同命令，旧 PostCompact 审查响应在 SessionStart 审查阶段到达
- **THEN** 当前信任请求与计划保持原身份，旧响应零授权和零执行

UserPromptSubmit 的信任请求 SHALL 绑定实际已领取的 turn、采样位置、InputID 与匹配计划。审查说明脚本读取提交明文、可阻止提交并且使用只读禁网 sandbox。当前实例中该声明的后续匹配沿用已作出的信任或拒绝，直到按已有规则 reset 或实例退出。按声明获准后才可启动当前检查，信任不跨事件、快照或会话实例复用。

#### Scenario: Reject a response from an older prompt

- **WHEN** 当前正在审查一条提示词，前一个输入的响应迟到，且两次使用同一命令声明
- **THEN** 当前信任请求保持原身份，该响应无授权效果，未获授权的命令启动次数为零
