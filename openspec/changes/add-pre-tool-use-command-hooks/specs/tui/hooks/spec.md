## Purpose

定义 Claude 风格的本地 Hooks 浏览、来源解释、命令信任审查及工具检查状态展示，使用户能清楚知道哪些检查会运行、授权范围是什么，并在取消、窄屏和异步响应条件下保持草稿、身份及权限隔离。

## ADDED Requirements

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

TUI SHALL 将检查中、被检查阻断、检查失败与真正工具 Running 区分。只有 tool_execution_started durable 后才能显示目标工具已运行；详情只展示检查身份、来源种类、稳定 code、timeout 与输出 byte count，并说明详细输出未收录。原始 stdout/stderr/reason、PID、绝对路径和 ledger 身份 MUST NOT 进入普通主线与 snapshot。

#### Scenario: Present a blocked Write
- **WHEN** Write 在 Hook 阶段被阻断且从未 tool started
- **THEN** 主线显示检查阻断而非 Write 执行失败，详情显示安全检查摘要，文件 bytes 不变

#### Scenario: Cancel while checking
- **WHEN** 用户取消 Hook，清理尚未完成
- **THEN** 界面反映正在收口，不接受下一 turn；Runtime 确认清理完成后才恢复可用状态
