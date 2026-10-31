## Purpose

定义贴近 Claude Code 的上下文查询和手动压缩交互，使用户在同一会话中理解模型有效窗口、指定摘要重点、取消压缩并查看完整历史，同时保留现有 composer、输入队列和工具详情体验。

## ADDED Requirements

### Requirement: Local context commands are distinct from user messages

TUI SHALL 在 composer 提交边界识别首行精确命令 `/context` 与 `/compact`，后者可带剩余正文作为保留重点；`/context` 带参数 MUST 本地拒绝。命令和其显示结果 MUST NOT 形成 Provider user_inputs 或普通用户 transcript。其他非精确匹配文本 SHALL 保持普通文本行为，不按子串猜测命令。拒绝时 MUST 保留草稿、光标和历史；接受后可加入本地 recall，不伪造持久化用户输入。

#### Scenario: Submit summary guidance
- **WHEN** 用户提交 `/compact 保留权限决策和未完成测试`
- **THEN** 文本作为摘要重点传递，TUI 不生成普通用户输入提交或普通 turn

#### Scenario: Preserve ordinary text
- **WHEN** 用户提交 `/compactness`、文件路径或正文中间包含 `/compact`
- **THEN** 按普通输入处理，不执行本地压缩

#### Scenario: Reject a busy operation without losing the draft
- **WHEN** 活动 turn、审批或已排队输入存在时提交 `/compact`
- **THEN** 显示安全英文 busy 提示，草稿/光标保持，且无压缩网络或 started fact

### Requirement: Compaction uses a concise main transcript and expandable history

进行中的压缩 SHALL 显示 `Compacting conversation` 状态，不输出普通 assistant delta。成功 SHALL 显示简短 `Conversation compacted` 边界与 Ctrl+O 历史提示；完整摘要默认折叠，只在历史/详情中展开。手动压缩本身 MUST NOT 自动发起普通回答；用户已排队的显式新输入可在清理和成功终态后继续。原始用户/assistant/tool 历史和工具详情 MUST 保持可访问，恢复后边界顺序一致，不重放 spinner 或伪造实时耗时。

#### Scenario: Complete without follow-up input
- **WHEN** 手动压缩成功且没有排队输入
- **THEN** 显示完成边界后等待输入，普通 Provider sample 调用次数不增加

#### Scenario: Inspect old details after resume
- **WHEN** 成功压缩并重启后用户通过 Ctrl+O 查看压缩前工具详情和摘要
- **THEN** 原条目关联/内容不变，摘要作为独立边界详情显示，不作为用户或 assistant 原消息

#### Scenario: Do not show a false completion
- **WHEN** 摘要失败、取消或完成提交尚未确定
- **THEN** 不显示成功边界，不收起为已完成摘要，旧历史仍可查看

### Requirement: Composer and cancellation remain responsive during compaction

压缩期间 SHALL 允许编辑草稿并有界排队新输入，排队输入不参与当前摘要。Ctrl+C SHALL 定向取消活动 CompactionID；Esc 在详情层先关闭详情，在普通输入层有 queued auto 时先取回，否则取消压缩。过期操作完成/取消事件 MUST NOT 清除新操作状态。普通失败或取消后 auto 输入 SHALL 返还，strict-next 保持现有顺序规则；正文和草稿不得静默丢弃。

#### Scenario: Queue and continue after success
- **WHEN** 压缩期间用户提交新输入，随后压缩成功
- **THEN** 输入在新窗口上按 FIFO 继续，旧摘要请求中没有该输入

#### Scenario: Recover queued input after cancellation
- **WHEN** 用户取消压缩且有 queued auto
- **THEN** auto 进入 returned tray，可取回编辑；取消清理前不自动启动新 turn

### Requirement: Context display preserves uncertainty and terminal constraints

`/context` SHALL 展示模型、有效窗口来源估算、配置容量、预留、安全余量及估算方法/质量；分类总量 MUST 与同一查询快照一致。unknown 或未配置窗口时不得显示伪造的可用百分比。窄屏、极小尺寸和无色终端 SHALL 提供可读降级，View MUST 无 I/O。Provider usage 和费用不得混同为当前窗口占用。

#### Scenario: Display an unconfigured window
- **WHEN** 用户未配置 context window
- **THEN** 展示可用估算并标注窗口未配置，不猜测模型容量或绘制确定占用比例

#### Scenario: Render a narrow terminal
- **WHEN** 终端尺寸缩小或颜色被禁用
- **THEN** 数值、estimated/unknown 状态和关键操作仍可辨认，不因重绘改变规划或执行外部操作
