## Purpose

定义 Claude 风格的本地 Hooks 浏览、来源解释、命令信任审查及工具检查状态展示，使用户能清楚知道哪些检查会运行、授权范围是什么，并在取消、窄屏和异步响应条件下保持草稿、身份及权限隔离。

## ADDED Requirements

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
