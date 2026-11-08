# tui/hooks Specification

## Purpose

定义 Claude 风格的本地 Hooks 浏览、来源解释、命令信任审查及工具检查状态展示，使用户能清楚知道哪些检查会运行、授权范围是什么，并在取消、窄屏和异步响应条件下保持草稿、身份及权限隔离。

## Requirements

### Requirement: Hooks browser is local bounded and draft preserving

TUI SHALL 提供 /hooks，按事件、matcher 和来源展示冻结声明与 available/untrusted/trusted/denied/invalid/unsupported 状态，并可进入完整命令、timeout 和安全限制详情。浏览 MUST 无 Provider、磁盘、Session 写入或进程副作用；View 只读取内存投影。窄屏与长命令 SHALL 可滚动完整审查，不将截断行冒充完整声明；Esc 返回原草稿。

空闲时详情 SHALL 允许对精确匹配快照的单项 reset 内存信任或拒绝；reset 不运行命令、不建立授权、不重试旧调用。活动 turn 中 reset MUST 拒绝。无效或不支持项不得提供可执行信任动作。

#### Scenario: Browse without side effects
- **WHEN** 用户打开 /hooks、浏览详情并返回
- **THEN** 进程/Provider/Session 写入与来源读取次数为零，原草稿保持不变

#### Scenario: Inspect a long declaration in a narrow terminal
- **WHEN** 终端宽度不足以单行显示命令
- **THEN** 可滚动查看完整命令和限制，没有隐藏的授权字段或按键误提交

### Requirement: Hook trust review is distinct and correlated

首次匹配的未信任 Hook SHALL 显示独立 overlay，完整展示命令、来源、能力匹配、相对执行范围、timeout、只读/禁网限制与脚本可变性；默认拒绝，仅提供本会话信任和拒绝。普通工具批准与 Hook 信任 overlay MUST 互斥，任何按键不得同时消费到 composer 或其他请求。

确认 SHALL 只发送当前有效身份的 typed response，最终授权由 Runtime 确认；重复、旧请求或取消后的完成消息不得改变投影、草稿或后续 turn。Esc SHALL 拒绝当前信任，Ctrl+C 定向取消活动 turn。浏览列表不能通过本地状态直接建立授权。

#### Scenario: Preserve the default denial
- **WHEN** 信任 overlay 刚打开且用户直接按 Enter 或 Esc
- **THEN** 对应 Hook 被拒绝，目标工具不执行，普通工具 approval 不出现

#### Scenario: Isolate stale confirmations
- **WHEN** 一个信任请求取消后，旧完成消息到达且新 turn 已开始
- **THEN** 新 turn 的授权、草稿和活动 overlay 不改变

### Requirement: Hook activity never masquerades as tool execution

TUI SHALL 将检查中、被检查阻断、检查失败与真正工具 Running 区分。只有 tool_execution_started durable 后才能显示目标工具已运行；未披露输出的详情只展示检查身份、来源种类、稳定 code、timeout 与输出 byte count，并说明详细输出未收录。原始 stdout/stderr/reason MUST NOT 自动进入普通主线与 snapshot；宿主生成的 metadata 不含 PID、绝对路径或 ledger 身份。用户明确授权的反馈正文只在详情安全转义展示，不宣称能清除任意正文内的路径。


Post 活动 SHALL 明确置于成功工具结果之后，显示检查中/通过/失败/取消而不把原工具改成 Error。工具终态与后置活动正交，MUST NOT 因 toolClosed 丢弃合法 Post checking；Pre 不能在工具结束后重开。仅已授权并 durable 的模型反馈通过独立有界详情展示，不把它当作原始 stdout/stderr。等待 Post cleanup 时保持 turn 活动与定向中断，不能提前返回 idle。

前后置历史 SHALL 按 invocation/event/plan digest/ordinal 关联并分别验证前缀，不能使用单一数组长度校验两个事件的 ordinal。未启动后缀和 lifecycle completed 中的普通错误必须由真实 terminal/逐项事实展示，不能根据不存在 finished 推断通过。受控 completed/skipped 聚合活动 SHALL 明确显示后置完成或跳过；取消和不确定保持独立，聚合活动不伪造单项运行统计。

#### Scenario: Present a blocked Write
- **WHEN** Write 在 Hook 阶段被阻断且从未 tool started
- **THEN** 主线显示检查阻断而非 Write 执行失败，详情显示安全检查摘要，文件 bytes 不变

#### Scenario: Cancel while checking
- **WHEN** 用户取消 Hook，清理尚未完成
- **THEN** 界面反映正在收口，不接受下一 turn；Runtime 确认清理完成后才恢复可用状态

### Requirement: Hooks review exposes event timing and model disclosure

/hooks SHALL 同时展示两事件、来源、matcher、feedback=none/model 与原有信任状态，完整详情支持窄屏滚动且不执行 I/O。Post 信任 overlay MUST 出现在原工具批准/执行之前，说明成功后检查、只读/禁网及 model 模式内容会发送给 Provider 并保存到 Session；默认拒绝、Esc 拒绝、Ctrl+C 取消。reset 不隐式开启披露、不重试旧工具，历史信任不建立当前授权。paste、过期响应和 modal 输入不得泄漏到 composer 或其他批准。

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
