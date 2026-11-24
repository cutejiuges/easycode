## MODIFIED Requirements

### Requirement: Hooks browser is local bounded and draft preserving

TUI SHALL 提供 /hooks，按事件、适用工具 matcher、无 matcher 的回合完成范围、压缩事件触发 matcher 或 SessionStart 来源 matcher 及声明来源展示冻结声明与 available/untrusted/trusted/denied/invalid/unsupported 状态，并可进入完整命令、timeout 和安全限制详情。浏览 MUST 无 Provider、磁盘、Session 写入或进程副作用；View 只读取内存投影。窄屏与长命令 SHALL 可滚动完整审查，不将截断行冒充完整声明；Esc 返回原草稿。

空闲时详情 SHALL 允许对精确匹配快照的单项 reset 内存信任或拒绝；reset 不运行命令、不建立授权、不重试旧调用。活动 turn 或独立手动压缩中 reset MUST 拒绝。无效或不支持项不得提供可执行信任动作。

#### Scenario: Browse without side effects
- **WHEN** 用户打开 /hooks、浏览详情并返回
- **THEN** 进程/Provider/Session 写入与来源读取次数为零，原草稿保持不变

#### Scenario: Inspect a long declaration in a narrow terminal
- **WHEN** 终端宽度不足以单行显示命令
- **THEN** 可滚动查看完整命令和限制，没有隐藏的授权字段或按键误提交

Stop SHALL 明确显示回合完成检查、feedback 策略和现有信任状态，不显示伪造工具能力或 matcher。浏览、窄屏完整命令、空闲 reset 继续无 I/O/授权/重试副作用。

#### Scenario: Browse completion declarations
- **WHEN** 用户打开带 Stop 的 /hooks
- **THEN** 显示回合完成范围、完整命令与披露策略，无 matcher、进程或 Provider 操作，Esc 保留草稿

#### Scenario: Browse compaction declarations

- **WHEN** 用户打开带 PreCompact 的 /hooks 并进入详情
- **THEN** 显示 `manual`、`auto` 或 `*`、来源、披露策略及完整命令。浏览无脚本、Provider 或 Session 写入，Esc 返回时保留草稿

#### Scenario: Browse session start declarations

- **WHEN** 用户浏览带 SessionStart 声明的 /hooks 并打开详情
- **THEN** 显示 `startup`、`resume`、`compact` 或 `*` 的设置值、声明来源、完整命令和明确的运行时机。浏览与退出保留草稿，不启动脚本或 Provider 请求

### Requirement: Hook trust review is distinct and correlated

首次匹配的未信任 Hook SHALL 显示独立 overlay，完整展示命令、来源、工具能力匹配、回合完成范围、压缩触发范围或 SessionStart 实际来源、相对执行范围、timeout、只读/禁网限制与脚本可变性；默认拒绝，仅提供本会话信任和拒绝。普通工具批准与 Hook 信任 overlay MUST 互斥，任何按键不得同时消费到 composer 或其他请求。

确认 SHALL 只发送当前有效身份的 typed response，最终授权由 Runtime 确认；重复、旧请求或取消后的完成消息不得改变投影、草稿或后续 turn。Esc SHALL 拒绝当前信任，Ctrl+C 定向取消活动 turn，独立手动压缩审查则定向取消本次压缩。浏览列表不能通过本地状态直接建立授权。

#### Scenario: Preserve the default denial
- **WHEN** 信任 overlay 刚打开且用户直接按 Enter 或 Esc
- **THEN** 对应 Hook 被拒绝；工具目标不执行，Stop 目标零检查/续轮并失败关闭，普通工具 approval 不出现

#### Scenario: Isolate stale confirmations
- **WHEN** 一个信任请求取消后，旧完成消息到达且新 turn 已开始
- **THEN** 新 turn 的授权、草稿和活动 overlay 不改变

Stop 审查 SHALL 在候选答复已保存后展示，说明读取本次答复、只读/禁网及 model 文本发送并持久化；拒绝不回滚既有工具或正文。确认只绑定当前真实 turn/sample target、请求、plan 和声明；tool/stop、旧 sample 或过期响应不得互换。最大命令转义预算保持完整审查，paste 不授权。

#### Scenario: Review a completion check after a saved answer
- **WHEN** 未信任 Stop 在候选提交后请求审查
- **THEN** 展示回合级身份和披露用途，Enter/Esc 默认拒绝；候选保持已保存，不声称已通过或已回滚工具

PreCompact 审查 SHALL 在本次压缩接纳后显示。它 MUST 说明触发范围和检查可阻止本次压缩。手动审查须说明两种反馈模式下脚本均通过 stdin 接收本次 `/compact` 的规范化用户重点。启用 `feedback:model` 时，还须说明合规脚本重点将发送给本次摘要 Provider，生成的摘要可能保留这些内容。确认只绑定当前真实 compaction ID、作用域、计划、请求和声明。自动范围的响应还须关联真实 TurnID 和采样下标。paste 不批准，初始选项为拒绝，用户直接按 Enter 或 Esc 均拒绝当前检查。窄屏可滚动审查完整命令和披露说明。无法呈现必要审查时 MUST 停用信任，保留拒绝与取消。按键不得传给 composer、工具审批、Stop 审查或其他请求。

#### Scenario: Review manual and automatic compaction independently

- **WHEN** 用户在有草稿的会话中遇到手动 PreCompact 审查，之后另一回合触发自动 PreCompact 审查
- **THEN** 各审查明确显示自身触发范围和披露用途，旧确认无法作用到新目标。Ctrl+C 分别定向取消当前手动压缩或自动所属 turn，等待收口且草稿保持

SessionStart 审查 SHALL 显示本次实际 `startup`、`resume` 或 `compact` 来源，即使声明 matcher 为 `*`。审查 SHALL 展示配置 matcher 和本次实际来源，并说明同一冻结声明的信任或拒绝对当前实例未来匹配的适用范围。审查 SHALL 明确命令在本地以只读、禁网限制运行，`feedback: model` 下通过校验及预算并接纳的 `additionalContext` 会保存到 Session，保留在有效历史时随后续请求提供给配置的 Provider，且可能被之后的摘要保留。reset 只影响未来匹配与审查。审查仍使用本会话信任或拒绝。窄屏 SHALL 可滚动审查完整命令和披露说明。无法呈现完整必要内容时 MUST 停用信任，保留拒绝和取消。paste、旧响应和 modal 按键不得泄漏到 composer、工具审批、问卷或其他请求。

`startup` 与 `resume` 审查 SHALL 只在实际采用的根会话首个符合条件的真实对话回合内出现。已有初始自动压缩先完成检查，SessionStart 审查位于真实输入提交之前。确认 SHALL 绑定当前会话和 thread、真实 TurnID、实际来源、计划、声明和请求身份。`compact` 审查 SHALL 在 checkpoint 成功保存并安装且本次 PostCompact 已收口或无需运行后展示。确认 SHALL 绑定同一 session/thread、CompactionID、完整作用域、checkpoint 序列和修订、实际来源、计划、声明和请求。自动作用域使用真实 TurnID 和采样下标。手动作用域不得伪造 TurnID。

#### Scenario: Keep the user's first turn after denying session context

- **WHEN** 已采用会话的首个真实回合正在审查 SessionStart，用户 Esc 或保留默认拒绝后按 Enter
- **THEN** 拒绝对应声明。原始输入按正常流程继续，拒绝项的脚本和上下文不进入该回合。草稿与其他授权保持独立

#### Scenario: Reject confirmations for a different active session start target

- **WHEN** 为旧聊天、不再活动的 turn、旧 checkpoint 或 PostCompact 事件发出的旧确认到达，当前活动目标是新的 SessionStart
- **THEN** 不确认当前请求、不覆盖审查或草稿。合法确认只作用于当前作用域及精确声明

### Requirement: Hooks review exposes event timing and model disclosure

/hooks SHALL 同时展示 PreToolUse、PostToolUse、PostToolUseFailure、Stop、PreCompact、PostCompact、SessionStart 七事件、来源、工具 matcher、回合完成范围、压缩触发范围或会话来源 matcher、feedback=none/model 与原有信任状态，完整详情支持窄屏滚动且不执行 I/O。两个后置分支的信任 overlay MUST 出现在原工具候选/普通批准/执行之前，分别说明成功后检查或执行尝试确定失败后检查、只读/禁网及 model 模式内容会发送给 Provider 并保存到 Session；失败说明须明确准入后的启动失败可诊断、正常非零退出仍走成功事件，不宣传测试失败自动修复。默认拒绝、Esc 拒绝、Ctrl+C 取消。reset 不隐式开启披露、不重试旧工具，历史信任不建立当前授权。paste、过期响应和 modal 输入不得泄漏到 composer 或其他批准。

失败检查的审查 MUST 发生于原工具候选与普通批准前，不因可能不执行而隐式信任。实际运行期间主线与详情 SHALL 保留原 Error/Conflict，检查成功不能显示为工具修复成功。只有当前选择分支可发布 checking/finished；不同事件、plan、旧 turn 或过期 detail_sequence 不得覆盖当前状态。Ctrl+O 只展示安全统计及已授权 durable 反馈；未披露 stdout/stderr 不展示。原 error 保存后当前取消不得把已选失败事件改成 none 或把原工具显示为未执行；原 result 自身 cancelled 而选择 none 时也不得据此显示工具从未运行。取消后等待唯一 owner cleanup，保留草稿/锚点，完成前不能 idle。

后置持久化提交通知 SHALL 仅包含调用、计划摘要、持久化详情序列及阶段，不保存固定成功事件或可空事件字段。计划阶段原结果未知时，宿主只显示“后置计划已保存”，不能预选成功/失败或显示检查已开始。实际运行事件 SHALL 从同一调用、同一计划和最新已验证事实快照读取；通知不能替代结果或事实源。字段缺失/未知、错调用/计划与过期序列必须拒绝或按既有过期消息规则忽略，构造/getter/render 不执行 I/O。

完整命令审查 SHALL 支持 command 契约允许的最大原始字节，包括 JSON 转义后大于原始长度的命令。携带声明的信任请求 SHALL 采用与合法声明最坏编码相容的有界预算；MUST NOT 因合法转义扩张而截断审查、误报无效控制事件或取消回合。原始命令长度、严格字段校验、默认拒绝和精确信任归属 MUST 保持。

#### Scenario: Review a model-feedback hook in a narrow terminal
- **WHEN** Post 命令较长且 feedback=model
- **THEN** 用户可滚动审查完整命令、事件及披露用途，直接 Enter 仍默认拒绝且零修改

#### Scenario: Keep the successful result visible during post failure
- **WHEN** Write 已成功但后置检查失败或被取消
- **THEN** 显示成功 Write 与独立检查失败/取消，原草稿与阅读锚点保持，cleanup 完成前不允许下一 turn

#### Scenario: Accept post ordinal zero after pre ordinal zero
- **WHEN** 同 invocation 的 Pre 第 0 项已完成，工具成功后 Post 第 0 项开始并完成
- **THEN** 历史关联不冲突，Post checking/终态均可见，原工具成功保持

#### Scenario: Ignore an old phase update
- **WHEN** 工具之后收到 Pre 重开、错误 plan 或旧 detail_sequence 的消息
- **THEN** 不改变 Post 状态、信任或草稿，不让旧消息覆盖新快照

#### Scenario: Review failure timing before an action
- **WHEN** 同调用存在成功和失败声明，用户在窄屏打开信任审查
- **THEN** 可完整审查事件、命令和披露用途；默认 Enter/Esc 仍拒绝，不进入普通批准或修改文件

#### Scenario: Preserve the failed tool during a successful diagnostic
- **WHEN** 工具 error/conflict 已保存，而失败诊断脚本通过并提供已授权反馈
- **THEN** 主线保留原工具失败并独立展示检查完成，Ctrl+O 可读反馈，不显示工具修复或自动重试

#### Scenario: Ignore a stale alternative branch update
- **WHEN** 当前选择失败分支，而成功分支消息或旧序列/旧 turn 异步响应到达
- **THEN** 当前检查、信任、草稿和阅读锚点不改变；paste 不授权或提交其他 modal

#### Scenario: Present a plan before any result exists
- **WHEN** 双分支计划已保存而原工具尚无 result，宿主收到合法调用/计划/序列/阶段通知
- **THEN** 仅显示中性计划状态，不预填 PostToolUse 或 PostToolUseFailure，不显示原工具或脚本已执行

#### Scenario: Reject the superseded fixed-event notification
- **WHEN** 后置提交通知仍含 event 字段，或通知身份/序列与最新事实快照不一致
- **THEN** 当前 strict decoder 或投影校验拒绝旧 shape/错配，不用固定事件刷新详情或改变信任

#### Scenario: Show cancellation without erasing the durable failure
- **WHEN** 原 error 已保存且匹配失败分支尚未 started，用户取消
- **THEN** 保留原工具错误、failure 选择及独立检查取消，零脚本；草稿和阅读锚点保持，不显示工具未执行或已修复

#### Scenario: Review a maximum command after JSON expansion
- **WHEN** 七事件之一的合法最大命令包含 JSON 需要转义的字符，审查消息编码后超过 16 KiB
- **THEN** 构造、严格解码、AgentLoop 与本地审查保留完整命令和身份；未知字段或原始命令超限仍被拒绝，不默认授权或取消合法回合

Stop 历史详情及提交通知 SHALL 精确绑定回合/sample target、plan digest、durable detail sequence 及 plan/decision/delivery 阶段；不借用 Post 的原工具 success/failure route。旧 target/plan、过期序列或跨事件更新不得覆盖当前状态。Ctrl+O 复用既有有界页缓存、异步查询与代次校验，render 不读取 journal。恢复后的候选/反馈/续答、检查结果及真实 cursor 与 live committed 一致，Hook 正文不进入用户 recall。

#### Scenario: Read completion details after restart
- **WHEN** 用户恢复含候选/Stop 反馈/续答的会话并打开 Ctrl+O
- **THEN** 展示同一顺序/来源/cursor 及安全统计，只有授权正文可读，无脚本重读或副作用

#### Scenario: Ignore a previous sample check response
- **WHEN** 当前正在检查较新 sample 而旧 sample/plan/detail sequence 消息迟到
- **THEN** 当前状态、信任、草稿和阅读锚点不变，不将旧通过应用到新候选

continued 决定的正文虽然已 durable，也只能在 Stop 详情显示待交付来源；native cursor 必须来自实际一/三记录交付，不能在决定阶段伪造。真实用户追加使用原 InputID/committed 通知与 recall，Hook 与真实用户条目分别显示。取消返还不能覆盖草稿；复合 Sync 成功后的真实输入不得错误放回托盘或自动重发。

#### Scenario: Distinguish pending and delivered feedback
- **WHEN** 决定已保存、摘要正在运行，而 native 反馈还不存在
- **THEN** 可读已授权冻结详情，但不生成假 native cursor 或声称模型已接收；交付后刷新同一目标的真实位置

#### Scenario: Keep a committed correction out of the returned tray
- **WHEN** 复合交付 Sync 成功后用户取消
- **THEN** 原反馈和真实输入保持已保存，取消只结束后续工作，不返还或重复提交该输入

### Requirement: PostCompact review and status preserve the successful window

TUI SHALL 在真实的已安装 checkpoint 阶段显示已压缩及后置审查/运行信息。`/hooks` 与完整审查 MUST 显示事件、触发 matcher、来源、timeout、完整命令和向本地脚本披露当前压缩摘要。默认选择为拒绝，Enter 确认当前选择，Esc 拒绝，Ctrl+C 定向取消。长命令和窄屏可完整安全审查或明确禁止授权；草稿、历史锚点和清理所有权保持。状态 MUST 绑定真实 scope、checkpoint 序号、实际 PostCompact plan、事件与相位，忽略旧 PreCompact 或其他操作的活动和信任响应。交互路径的未计划执行跳过只展示匹配 checkpoint 和稳定原因，禁止伪造计划、审查或运行。合法无匹配沿用原显示。

当前操作的终态 SHALL 同时显示成功压缩和独立的后置结果。取消期间仍接纳真实范围匹配的安装与关闭通知、拒绝迟到审查或 checking，已安装后继续显示压缩已保存，清理未完成前维持活动。检查关闭未确认时只显示安全的不确定状态。检查展示限当前操作、有界分类及计数，历史摘要沿原 Ctrl+O 权限和阅读路径。若同一成功压缩另有 SessionStart，PostCompact 的结果 SHALL 独立保持。后续 SessionStart 运行、拒绝或失败不改变已保存的窗口或 PostCompact 结果。

#### Scenario: Cancel during post-install review

- **WHEN** checkpoint 已安装且当前 PostCompact 审查正等待，用户 Ctrl+C 并随后收到旧审查响应
- **THEN** 取消等待期间保持已压缩，接纳匹配的关闭并将结果并入唯一压缩成功终态；解除审查后草稿保留，旧响应不授权新命令

## ADDED Requirements

### Requirement: SessionStart status and details follow committed model context

TUI SHALL 用有界状态区分 SessionStart 等待审查、运行、继续且无上下文、已保存上下文、取消收口或结果不确定，并显示实际来源。检查不会生成新的用户或助手消息、普通工具 Running 行或用户输入 recall。浏览、候选预览、未采用的恢复结果及单纯加载历史均不得开启审查。已匹配目标取消后，只接受相应的真实关闭事实。旧信任、运行进度或查询回复不得改变当前状态、草稿或阅读锚点。

Ctrl+C SHALL 取消现有 owner 并等待清理。对于 `startup` 和 `resume`，使用所属的真实 turn 取消。对于手动 `compact`，只取消此次压缩剩余工作。对于自动 `compact`，取消所属 turn。已安装的 checkpoint SHALL 保持成功和可用。SessionStart 收口前不得退回 idle。相应的 turn 或压缩操作关闭时，SHALL 清除其审查弹窗与待确认请求，即使取消路径没有单独的信任结算通知。

对于 `compact`，开始审查或运行之前 SHALL 先显示来自同一次已安装 checkpoint 的压缩成功状态。该状态在 PostCompact 无匹配声明时仍适用。压缩视图 SHALL 分别关联 checkpoint、PostCompact 终态及 SessionStart 计划和终态，不因阶段顺序相邻而共享状态或信任。对外压缩完成后同时显示已保存压缩及各实际运行阶段的有界结果。压缩完成的修订与估算仍指 checkpoint，成功保存的新上下文在此之后由更新的当前窗口快照展示。取消期间保留已安装 checkpoint，并等到相应检查的真实关闭；迟到的审查或运行通知不得恢复弹窗或阶段。control stream 在确认安装之后、活动阶段终态之前关闭时，保留已确定的 checkpoint 成功及已知后置结果，当前活动阶段显示安全的不确定状态，且不得冒充关闭已保存或接纳下一输入。

经信任、接纳、Sync 并发布的模型上下文 SHALL 作为独立只读条目在 Ctrl+O 详情中按真实 journal 游标和来源显示。详情列表只提供来源及有界安全状态，正文通过既有有界文本范围查询显示精确冻结模型正文，并按终端安全规则转义展示。未纳入模型上下文的文本、未成功提交的候选和原始 stdout/stderr 不得出现在详情或主线。成功 Sync 只可报告上下文已保存。详情和状态不得据此声称 Provider 已收到内容。实时与恢复后的展示 SHALL 使用同一已验证事实、游标、来源和正文。详情页沿用有界查询、缓存和代次校验。已打开的列表 SHALL 以最新真实关闭 seq 扩展已提交查询上界并提供新条目，保留已有合法阅读锚点，旧页响应不得覆盖新一代结果。View 不读取 journal 或原脚本。headless v1 不增加 SessionStart 活动、信任事件、来源、结果或本地上下文字段。

#### Scenario: Show saved session context separately from the original input

- **WHEN** 用户信任 SessionStart，结构化上下文与本次真实输入成功 Sync，随后打开 Ctrl+O
- **THEN** 真实输入保持原正文及身份。独立详情可分页读已保存的模型上下文，标记实际来源和真实游标。没有重复的主线消息，也没有仅因 Sync 就声称 Provider 已接收

#### Scenario: Keep the installed checkpoint during compact session context cancellation

- **WHEN** 该次 checkpoint 已保存并安装，PostCompact 已关闭或无需运行，且 SessionStart 等待审查或正在运行时用户 Ctrl+C
- **THEN** 保持压缩成功和已知的独立 PostCompact 结果，等待当前 owner 清理后显示 SessionStart 的真实终态。旧审核响应不能授权新命令。若 SessionStart 上下文未提交，Ctrl+O 不出现模型正文

#### Scenario: Retain known success if control closes before context completion

- **WHEN** TUI 已收到已安装 checkpoint 并进入 compact SessionStart，然后在未收到真实关闭时失去控制流
- **THEN** 保留已保存的压缩结果、已知 PostCompact 终态和上下文结果不确定状态，清除过期审查，不接受继续提交或制造模型正文

#### Scenario: Refresh saved compact session context without reopening old checks

- **WHEN** checkpoint 后的 SessionStart 合法上下文已保存，随即发生详情刷新或会话恢复
- **THEN** 当前窗口可显示保存后修订。Ctrl+O 保持 checkpoint、SessionStart 和真实对话条目的提交顺序，旧结果不替代新页，也不重跑读取历史所见的 Hook
