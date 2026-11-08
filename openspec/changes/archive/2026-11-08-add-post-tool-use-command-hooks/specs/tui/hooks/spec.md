## MODIFIED Requirements

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

## ADDED Requirements

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
