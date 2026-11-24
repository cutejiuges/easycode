## Purpose

定义本地根会话启动、恢复和成功压缩时用同步 `SessionStart` 命令读取当前工作区并补充模型上下文的契约，涵盖授权时机、实际披露范围，以及拒绝、失败与恢复时的原生交付行为。

## ADDED Requirements

### Requirement: SessionStart dispatch follows a real activation or installed checkpoint

`SessionStart` SHALL 使用冻结的用户和项目命令来源，按 `startup`、`resume`、`compact` 或匹配三者的 `*` 字面 matcher 选择。实际执行按用户来源、项目来源及数组顺序确定。至多选择八条。同源 ID、配置额度及命令快照继续使用共享 Hook 规则。

新建根会话 SHALL 使用 `startup`。CLI `--resume`、`--continue`、启动选择器和聊天内 `/resume` 成功接管的根会话 SHALL 使用 `resume`。该来源由目标实例持有。运行条件是已接纳的真实对话回合完成既有初始自动压缩步骤，且真实首轮输入自身通过普通请求与批次预检。计划 Sync 后不得在该实例中重试。预检拒绝或计划接纳前取消保留来源。激活上下文 SHALL 在首个普通 sample 前结算，已有自动摘要请求可以先发生。只有离线读取、资源准备、拒绝或取消的切换候选、同线程无操作切换以及仅执行本地命令 SHALL 零派发此来源。准备候选可以持有冻结身份；必须等到目标实际获得对话提交权才允许它消费。

`compact` SHALL 绑定手动或自动成功安装的单个 checkpoint，在 PostCompact 收口或确认无需运行后，在同一压缩 owner 内审查和执行。未成功安装的候选、失败和传输重试 MUST 零派发。越过取消屏障、清理未完成或 Session 已失去可靠写入能力时 MUST 零新派发。

匹配超限或冻结来源无效时 SHALL 跳过本次上下文尝试，并可用稳定原因向 TUI 报告。不得选择部分来源或截取匹配项执行。合法无匹配和非交互 SHALL 零 SessionStart 输入准备、额外的派发平台前检、信任请求、命令与新增持久化事实。共享 Hook 启动 availability 检查遵循现有规则。非交互既有输出词汇沿用原契约。其他 Hook 仍按各自共享配置诊断和运行契约处理。

#### Scenario: Distinguish startup from an adopted resume

- **WHEN** 同一声明组同时定义 `startup` 和 `resume`，用户新开会话，随后通过聊天内选择器完成目标会话接管
- **THEN** 新会话的首个真实对话回合只触发 `startup`，目标的首个真实对话回合只触发 `resume`，候选预览和接管前均零命令

#### Scenario: Wait for the exact installed checkpoint

- **WHEN** 自动摘要经过 Provider 回退后成功 Sync 并安装，已配置的 PostCompact 检查随后以已确认的非零退出收口
- **THEN** 对该 checkpoint 只派发一次 `compact`，在 PostCompact 收口之后，下一采样之前

#### Scenario: Retain resume across manual compaction before the first prompt

- **WHEN** 已接管的恢复会话先执行手动 `/compact`，随后一条对话输入在普通预检时被拒绝，再提交一条可接纳的输入
- **THEN** 手动压缩只运行 `compact`。被拒绝的对话输入不启动或消耗 `resume`。后续首个可采样回合只运行一次 `resume`，在普通 sample 前交付

#### Scenario: Keep preview and headless paths inert

- **WHEN** 离线预览、取消 `/resume` 候选或非交互的新建、恢复或压缩具有合法匹配项
- **THEN** 本次没有 SessionStart 命令、审查、模型披露或记录，已经验证的历史上下文仍按原生历史恢复

### Requirement: SessionStart accepts only authorized bounded context

stdin SHALL 是最多 512 KiB 的规范 UTF-8 JSON，含 `hook_event_name` 值 `SessionStart`、`source`、`session_id`、`thread_id` 和经边界验证的工作区 `cwd`。`startup` 与 `resume` 增加真实 `turn_id`。`compact` 增加 `compaction_id` 与 `trigger`。自动压缩另携真实 `turn_id` 和 `sample_index`，手动压缩使用无 turn/sample 的分支。输入 MUST 排除用户正文、语义/原生摘要、transcript 路径、完整历史、环境值与凭据。实际编码超限时零启动。

所有匹配声明 SHALL 先通过平台和输入预检，再逐项完成独立的进程内信任，全部审核同意之前启动次数为零。信任绑定 `SessionStart`、实际目标、来源、计划和已展示的披露策略。同一冻结声明的信任或拒绝覆盖其 matcher 在当前会话实例中的所有后续匹配来源。审查 SHALL 明示配置 matcher、实际来源及未来授权范围。reset 只影响将来的派发。拒绝任一项 SHALL 跳过整个计划。runner SHALL 复用受限环境、真实 workspace FD、进程组清理及双流各 16 KiB 上限，按计划顺序串行运行。每个来源实例或 checkpoint 尝试共享 30 秒命令执行额度，各命令使用配置和剩余额度中的较小值。确定的单项错误可以继续同一计划的后续项。执行额度耗尽允许在清理确认后交付已完成的合格前缀，未启动后缀只留下原因。applied 批次接纳前发生当前取消或执行不确定时，MUST 丢弃全部未提交正文。

成功退出且 stdout 为空白或严格空对象表示无上下文。仅在显式 `feedback: model`、完整成功退出且输出为严格 `hookSpecificOutput` 对象时 SHALL 将合法文本纳入候选。唯一字段为 `hookEventName` 值 `SessionStart` 与非空 `additionalContext`。单项正文最多 4 KiB，所有送模包装的最终 UTF-8 bytes 合计最多 16 KiB。在成功且已授权的候选中，按声明顺序接纳可容纳的最长完整前缀。总量为各项最终模型包装文本的字节数之和。首个超额候选及其后缀按安全状态省略。外层 wire 与持久化编码另作完整预检。不得截断已接纳正文。`feedback: none` 下的披露返回安全失败，任意普通非零含 exit 2 返回 `hook_exit_failed`。纯文本、重复/未知字段、控制或自动用户消息、无效 UTF-8、截断结果和未授权文本 MUST 排除。原始双流与自由文本错误 SHALL 仅由有界进程 owner 消费。持久化正文仅限经过授权、预算和原生交付验证的最终模型包装。

#### Scenario: Authorize a distinct disclosure

- **WHEN** 相同命令已获 PostCompact 信任，SessionStart 声明启用 `feedback: model`
- **THEN** 系统仍单独展示 SessionStart 披露审查，脚本只收到本事件允许的元数据，实际接受的上下文可以作为用户层模型数据保存

#### Scenario: Preserve real work on a rejected context

- **WHEN** 用户拒绝审查，或全部输出因非零退出、非法结构、额度或缺少授权而不可用
- **THEN** 首轮使用真实输入继续原流程，或保留成功压缩并用原接续候选继续，脚本结果不改变工具权限或压缩计量

#### Scenario: Reuse a declaration decision only within its approved matcher and instance

- **WHEN** 用户在 startup 的 `*` 声明审查中通过或拒绝，然后同一实例发生 compact，另有同命令的 PostCompact 声明
- **THEN** 本声明采用先前已显示范围内的决定，PostCompact 使用自己的授权。reset 后的新匹配可以重新审查，先前保存的上下文保持其真实历史位置

#### Scenario: Preserve ordered whole bodies

- **WHEN** 多条已审核声明返回合法上下文但完整包装合计超过 16 KiB
- **THEN** 仅使用按声明顺序可容纳的完整前缀，保真保留已接受正文中的 Unicode、换行和空白，超额正文不保存或发送

### Requirement: SessionStart local outcomes preserve the real operation

已确认拒绝、失败、无输出、平台不可用或上下文预算不足 SHALL 使用安全结果说明并走原对话或压缩路径。取消 SHALL 由原回合或压缩 owner 等待清理并停止下一请求。已确认的 checkpoint 或输入事实必须保留，尚未确认接纳的用户输入按原流程返回。写入结果或必要清理无法确认时 SHALL 关闭后续接纳，保存已确认事实，保留原 owner 至资源清理完成。

#### Scenario: Preserve a completed summary when context is too large

- **WHEN** `compact` 的合法输出会使安装后的窗口不再满足缩减或下一普通候选预算
- **THEN** 仅跳过上下文，既有 checkpoint、计量、待交付输入及 Stop 反馈保持其真实状态，无第二次摘要

#### Scenario: Stop after committing a checkpoint but before context delivery

- **WHEN** 用户在 `compact` 命令中 Ctrl+C，checkpoint 已经 Sync
- **THEN** 先确认资源清理，再报告成功窗口和独立取消结果，当前操作不会再发普通请求
