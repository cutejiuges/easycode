## MODIFIED Requirements

### Requirement: User can submit and observe a streaming text turn

空闲状态下，用户 SHALL 能编辑单行文本，并通过 Enter 提交非空输入。提交后 TUI SHALL 在 transcript 中显示用户消息，并按 RuntimeEvent 顺序增量显示 assistant 文本；活动 turn 结束前 MUST NOT 启动第二个并发 turn。

Runtime发布`tool_approval_requested`时，TUI SHALL暂停聊天composer的键盘路由并进入与该request identity绑定的modal approval overlay。overlay解决前的普通字符、Enter、方向键和Tab不得修改或提交聊天草稿；已有草稿必须原样保留，并在overlay关闭后恢复。TUI只通过强类型`resolve_tool_approval`或定向interrupt响应当前request，不得直接调用Policy、Session、Provider或Patch executor。

#### Scenario: Submit a non-empty prompt
- **WHEN** 用户在空闲状态输入文本并按Enter
- **THEN** transcript增加该用户消息并启动一个turn
- **THEN** 输入区进入活动turn状态

#### Scenario: Ignore an empty submission
- **WHEN** 输入仅为空白且用户按Enter
- **THEN** TUI不启动Provider请求且transcript不增加空消息

#### Scenario: Render ordered text deltas
- **WHEN** Runtime依次产生多个`assistant_text_delta`
- **THEN** TUI将增量按事件顺序追加到当前assistant消息

#### Scenario: Preserve the composer under an approval overlay
- **WHEN** 用户已有未提交草稿且当前turn打开approval overlay
- **THEN** overlay期间的按键只路由到approval状态机，聊天草稿逐字节保持不变
- **THEN** overlay解决后原草稿重新可见且不会被自动提交

#### Scenario: Prevent concurrent submission
- **WHEN** 当前turn正在streaming或等待approval且用户按Enter
- **THEN** TUI不启动第二个turn
- **THEN** 当前turn继续保持唯一活动turn

### Requirement: Cancel and quit behavior is unambiguous

活动 turn streaming期间，Esc或Ctrl+C SHALL取消当前turn而不直接退出进程。approval overlay活跃时，Esc或Ctrl+C SHALL发送与显示中turn identity匹配的定向interrupt，并立即禁用重复决定；TUI必须等待Runtime的resolved/terminal事件后关闭overlay，不得本地伪造批准、拒绝或文件结果。空闲且输入为空时，Ctrl+C SHALL退出应用。普通字符`q` MUST作为当前聚焦控件的输入处理，不能作为全局退出快捷键。

#### Scenario: Interrupt a streaming turn
- **WHEN** assistant正在streaming且用户按Esc或Ctrl+C
- **THEN** TUI请求取消当前turn并保留已经显示的transcript
- **THEN** 清理结束后TUI返回可输入状态

#### Scenario: Interrupt an approval
- **WHEN** approval overlay活跃且用户按Esc或Ctrl+C
- **THEN** TUI发送匹配turn的interrupt并阻止再次提交decision
- **THEN** Runtime收口后overlay关闭且Patch未被TUI直接执行

#### Scenario: Exit while idle
- **WHEN** TUI空闲、输入为空且用户按Ctrl+C
- **THEN** 应用正常退出并恢复终端状态

#### Scenario: Type the letter q
- **WHEN** 聊天输入框聚焦且用户输入`q`
- **THEN** `q`出现在草稿中且应用不退出

## ADDED Requirements

### Requirement: File approval uses a Claude-style deterministic overlay

TUI SHALL将typed Patch approval request投影为居中的modal overlay，按稳定顺序显示workspace相对路径、稳定英文风险摘要、完整有界unified diff和内核提供的三个选择：`Yes`、`Yes, allow all edits during this session`、`No`。初始焦点 SHALL为`No`；上下方向键在允许选择间循环，Enter提交当前选择，TUI不得根据终端尺寸或Provider来源改变决定语义。

只有`No`聚焦时Tab SHALL切换到拒绝反馈编辑器，提示用户说明“应该怎么做”；反馈为合法UTF-8，去除外围空白后至多16 KiB。反馈编辑器的Enter提交deny，Esc先退出反馈编辑器并保留overlay，再次Esc按中断语义处理。`Yes`与session allow不接受反馈，也不得提供候选内联编辑。Runtime声明的decision集合缺失、未知或与当前overlay identity不一致时，TUI必须失败关闭而不是猜测默认批准。

固定尺寸不足以完整显示diff时，overlay SHALL在自身viewport中滚动并持续显示路径、选择和当前位置；不得截断diff后仍允许批准。颜色不可用时仍须用文本、边框和焦点标记区分新增、删除和当前选择。resize只改变布局，不改变request、草稿、反馈或焦点语义。

#### Scenario: Render an approval at a normal terminal size
- **WHEN** TUI在固定尺寸收到合法Patch approval request
- **THEN** snapshot包含相对路径、完整diff、三个有序选择和默认聚焦的`No`
- **THEN** snapshot不包含workspace绝对路径、完整未变文件或Provider wire

#### Scenario: Allow one patch
- **WHEN** 用户聚焦`Yes`并按Enter
- **THEN** TUI提交匹配identity和fingerprint的`allow_once`response且禁用重复提交

#### Scenario: Allow edits for the current Runtime session
- **WHEN** 用户聚焦session选项并按Enter
- **THEN** TUI提交`allow_session`，其标签明确限定为当前session
- **THEN** TUI不写用户配置、项目配置或持久权限规则

#### Scenario: Deny with guidance
- **WHEN** 用户在`No`上按Tab、输入合法反馈并提交
- **THEN** TUI提交deny与该反馈，聊天composer内容不变
- **THEN** overlay关闭后模型可通过新的tool call提出修订方案

#### Scenario: Scroll a large bounded diff
- **WHEN** 完整合法diff超过当前overlay viewport但未超过内核大小边界
- **THEN** 用户可以滚动查看全部diff且选择始终可达
- **THEN** resize后显示内容仍来自同一candidate fingerprint

#### Scenario: Fail closed on an unsupported decision set
- **WHEN** approval event缺少`No`、含未知decision或identity与当前活动turn不匹配
- **THEN** TUI不发送allow response并以安全错误请求中断当前turn
