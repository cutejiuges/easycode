## MODIFIED Requirements

### Requirement: Hooks browser is local bounded and draft preserving

TUI SHALL 提供 /hooks，按事件、适用工具 matcher 或无 matcher 的回合完成范围及来源展示冻结声明与 available/untrusted/trusted/denied/invalid/unsupported 状态，并可进入完整命令、timeout 和安全限制详情。浏览 MUST 无 Provider、磁盘、Session 写入或进程副作用；View 只读取内存投影。窄屏与长命令 SHALL 可滚动完整审查，不将截断行冒充完整声明；Esc 返回原草稿。

空闲时详情 SHALL 允许对精确匹配快照的单项 reset 内存信任或拒绝；reset 不运行命令、不建立授权、不重试旧调用。活动 turn 中 reset MUST 拒绝。无效或不支持项不得提供可执行信任动作。

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

### Requirement: Hook trust review is distinct and correlated

首次匹配的未信任 Hook SHALL 显示独立 overlay，完整展示命令、来源、工具能力匹配或回合完成范围、相对执行范围、timeout、只读/禁网限制与脚本可变性；默认拒绝，仅提供本会话信任和拒绝。普通工具批准与 Hook 信任 overlay MUST 互斥，任何按键不得同时消费到 composer 或其他请求。

确认 SHALL 只发送当前有效身份的 typed response，最终授权由 Runtime 确认；重复、旧请求或取消后的完成消息不得改变投影、草稿或后续 turn。Esc SHALL 拒绝当前信任，Ctrl+C 定向取消活动 turn。浏览列表不能通过本地状态直接建立授权。

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

### Requirement: Hook activity never masquerades as tool execution

TUI SHALL 将检查中、被检查阻断、检查失败与真正工具 Running 区分。只有 tool_execution_started durable 后才能显示目标工具执行尝试 Running；该事实不证明 OS 进程已启动或文件已改变；未披露输出的详情只展示检查身份、来源种类、稳定 code、timeout 与输出 byte count，并说明详细输出未收录。原始 stdout/stderr/reason MUST NOT 自动进入普通主线与 snapshot；宿主生成的 metadata 不含 PID、绝对路径或 ledger 身份。用户明确授权的反馈正文只在详情安全转义展示，不宣称能清除任意正文内的路径。


两个后置事件 SHALL 明确置于已保存的原工具确定终态之后，独立显示检查中/通过/失败/取消。PostToolUse 保留原 success，PostToolUseFailure 保留原 error/conflict；检查通过 MUST NOT 表示原错误已修复或改成 success。原 error 保存后的检查取消仍保留 failure 选择；原工具本身 cancelled 选择 none，不能借此声称工具没执行。工具终态与后置活动正交，MUST NOT 因 toolClosed 丢弃合法 Post checking；Pre 不能在工具结束后重开。仅已授权并 durable 的模型反馈通过独立有界详情展示，不把它当作原始 stdout/stderr。等待 Post cleanup 时保持 turn 活动与定向中断，不能提前返回 idle。

前后置历史 SHALL 按 invocation/event/plan digest/ordinal 关联并分别验证前缀，不能使用单一数组长度校验两个事件的 ordinal。未启动后缀和 lifecycle completed 中的普通错误必须由真实 terminal/逐项事实展示，不能根据不存在 finished 推断通过。受控 completed/skipped 聚合活动 SHALL 明确显示后置完成或跳过；取消和不确定保持独立，聚合活动不伪造单项运行统计。

#### Scenario: Present a blocked Write
- **WHEN** Write 在 Hook 阶段被阻断且从未 tool started
- **THEN** 主线显示检查阻断而非 Write 执行失败，详情显示安全检查摘要，文件 bytes 不变

#### Scenario: Cancel while checking
- **WHEN** 用户取消 Hook，清理尚未完成
- **THEN** 界面反映正在收口，不接受下一 turn；Runtime 确认清理完成后才恢复可用状态

#### Scenario: Reject a post activity for the other selected branch
- **WHEN** 同调用已保存原 success，但到达失败分支身份，或原 error/conflict 到达成功分支身份
- **THEN** 不覆盖原工具、检查快照或详情序列，不从迟到消息重新选择事件

Stop SHALL 独立显示检查中、阻断后继续、通过、错误/取消/不确定，不能冒充工具 Running。sample 已 committed 时正文及推理保持已保存，Stop 未收口时不能显示最终完成或 idle。Ctrl+C 等待实际 cleanup/terminal，不接收并发回合。只有已授权且 durable 的规范反馈在详情中转义展示；未授权双流不展示。

#### Scenario: Continue after a completion block
- **WHEN** 合法 continued 决定已保存但 native 交付尚未完成
- **THEN** 显示反馈已冻结、准备续轮或候选摘要；只有 native 交付完成才显示反馈已交付，保留原答复，不发布成功或添加假用户消息

#### Scenario: Cancel while completion cleanup is pending
- **WHEN** Stop 已收到取消但进程或输出清理未结束
- **THEN** 界面保持收口状态和已保存正文，直到真实 terminal 才恢复可用

### Requirement: Hooks review exposes event timing and model disclosure

/hooks SHALL 同时展示 PreToolUse、PostToolUse、PostToolUseFailure、Stop 四事件、来源、工具 matcher 或回合完成范围、feedback=none/model 与原有信任状态，完整详情支持窄屏滚动且不执行 I/O。两个后置分支的信任 overlay MUST 出现在原工具候选/普通批准/执行之前，分别说明成功后检查或执行尝试确定失败后检查、只读/禁网及 model 模式内容会发送给 Provider 并保存到 Session；失败说明须明确准入后的启动失败可诊断、正常非零退出仍走成功事件，不宣传测试失败自动修复。默认拒绝、Esc 拒绝、Ctrl+C 取消。reset 不隐式开启披露、不重试旧工具，历史信任不建立当前授权。paste、过期响应和 modal 输入不得泄漏到 composer 或其他批准。

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
- **WHEN** 四事件之一的合法最大命令包含 JSON 需要转义的字符，审查消息编码后超过 16 KiB
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
