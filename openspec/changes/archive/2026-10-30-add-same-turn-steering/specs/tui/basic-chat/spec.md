## MODIFIED Requirements

### Requirement: User can submit and observe a streaming text turn

空闲或普通运行状态下，用户 SHALL 能编辑多行文本，并通过普通 Enter 提交合法非空白输入；换行快捷键与反斜杠换行规则 SHALL 优先于提交。正文 SHALL 保留输入规范化后的缩进、内部换行、尾部空格和尾换行，不得以 trim 改写；空白判断仅用于拒绝空输入。等待本次 admission 时 MUST 保留并冻结提交草稿，不能重复提交；接收成功后运行中允许继续编辑并提交自动输入。

提交接收成功后 TUI SHALL 显示未保存的队列预览，收到 committed 事件才按 InputID 在正式 transcript 显示恰好一条用户消息，并按 RuntimeEvent 顺序增量显示 assistant 文本。接收仅表示进程内 admission，不得声称 Session 已 durable。接收失败 SHALL 恢复原草稿、光标和历史浏览状态，且不增加已接收用户消息或输入历史。接收成功后的 turn 失败 MUST NOT 自动恢复 pending 草稿、重新提交或重试工具。

Runtime发布`tool_approval_requested`时，TUI SHALL暂停聊天composer的键盘路由并进入与该request identity绑定的modal approval overlay。overlay解决前的普通字符、Enter、方向键、Tab和粘贴不得修改或提交聊天草稿；已有草稿、光标和历史浏览状态必须保留，并在overlay关闭后恢复。TUI只通过强类型`resolve_tool_approval`或定向interrupt响应当前request，不得直接调用Policy、Session、Provider或Patch executor。

#### Scenario: Submit a non-empty prompt
- **WHEN** 用户在空闲状态输入文本并按普通 Enter，且不匹配换行规则
- **THEN** 输入进入等待接收状态，最多发起一次提交
- **THEN** 接收成功后显示队列预览，持续消费控制流；输入 committed 后 transcript 增加该用户消息

#### Scenario: Ignore an empty submission
- **WHEN** 输入仅为空白且用户按普通 Enter
- **THEN** TUI 不启动 Provider 请求且 transcript 不增加空消息，草稿保持原样

#### Scenario: Render ordered text deltas
- **WHEN** Runtime依次产生多个`assistant_text_delta`
- **THEN** TUI将增量按事件顺序追加到当前assistant消息

#### Scenario: Preserve the composer under an approval overlay
- **WHEN** 用户已有未提交草稿且当前turn打开approval overlay
- **THEN** overlay期间的按键和粘贴只路由到approval状态机，聊天草稿逐字节保持不变
- **THEN** overlay解决后原草稿、光标和浏览状态恢复且不会自动提交，resize只允许调整显示布局

#### Scenario: Prevent concurrent submission
- **WHEN** 提交或取回正在等待接收，或 approval 占据焦点且用户按 Enter
- **THEN** TUI不发起第二次提交、不排队，当前输入保持唯一执行路径

#### Scenario: Preserve multiline content exactly after normalization
- **WHEN** 草稿包含首行缩进、多行代码、尾部空格和尾换行并被接收
- **THEN** 提交的正文与草稿内容一致，不进行外围空白删除或 Unicode 规范化
- **THEN** 两种 Provider 都接收同一正文，终端尺寸与光标位置不进入模型请求

#### Scenario: Recover a rejected submission
- **WHEN** 提交接收返回错误
- **THEN** TUI 恢复提交前草稿、光标和历史浏览状态并显示安全英文错误
- **THEN** transcript 和输入历史中不增加该次输入，也不自动重试

#### Scenario: Keep an accepted failed turn distinct from rejection
- **WHEN** 提交已被接收，随后 Provider 失败、事件流异常或用户取消
- **THEN** 已显示的用户消息和输入历史保留，不自动恢复为待提交草稿或启动第二次执行

#### Scenario: Interrupt while admission is pending
- **WHEN** 提交接收尚未返回时用户按 Esc 或 Ctrl+C
- **THEN** TUI 保留取消意图，若接收成功则沿用当前会话中断路径并等待清理
- **THEN** 若接收失败则恢复草稿并清除取消意图，不误取消后续 turn

committed 用户消息 SHALL 结束前一个 assistant 文本 cell，后续 delta 创建新 cell；全部控制反馈与 RuntimeEvent 由同一长期有序消费者处理。

#### Scenario: Queue while a tool turn runs
- **WHEN** 普通工具运行期间用户编辑并 Enter 提交
- **THEN** 草稿接收成功才清除，队列显示 queued，安全点提交后准确移入 transcript，不建立并发 stream

### Requirement: Cancel and quit behavior is unambiguous

普通活动 turn 期间，Esc SHALL 优先取回 queued auto；没有可取回输入时定向 stop。Ctrl+C SHALL 定向 stop 当前 turn 而不退出进程。局部浮层先按自身 Esc 规则处理。approval overlay活跃时，Esc或Ctrl+C SHALL发送与显示中turn identity匹配的定向 stop，并立即禁用重复决定；TUI必须等待Runtime的resolved/terminal事件后关闭overlay，不得本地伪造批准、拒绝或文件结果。空闲且输入为空时，Ctrl+C SHALL退出应用。普通字符`q` MUST作为当前聚焦控件的输入处理，不能作为全局退出快捷键。

#### Scenario: Interrupt a streaming turn
- **WHEN** assistant 正在 streaming，用户按 Ctrl+C，或没有可取回队列时按 Esc
- **THEN** TUI请求取消当前turn并保留已经显示的transcript
- **THEN** 清理结束后TUI返回可输入状态

#### Scenario: Interrupt an approval
- **WHEN** approval overlay活跃且用户按Esc或Ctrl+C
- **THEN** TUI发送匹配 turn 的 stop并阻止再次提交decision
- **THEN** Runtime收口后overlay关闭且Patch未被TUI直接执行

#### Scenario: Exit while idle
- **WHEN** TUI空闲、输入为空且用户按Ctrl+C
- **THEN** 应用正常退出并恢复终端状态

#### Scenario: Type the letter q
- **WHEN** 聊天输入框聚焦且用户输入`q`
- **THEN** `q`出现在草稿中且应用不退出

stop SHALL 返还未消费 auto，保留严格下一 turn；claimed 输入等待确定 resolution。返还内容进入有界不可执行托盘，不覆盖现有草稿、不截断、不自动重提；托盘与 queued/claimed 共用展示预算，取回/丢弃前不能无限接收新 auto。

#### Scenario: Return more than one draft can hold
- **WHEN** 停止返还多条合计超过 4 MiB 的输入
- **THEN** 托盘保留每条完整文本，用户逐条取回；当前草稿不变，不自动执行

### Requirement: Composer editing preserves grapheme boundaries

聊天 composer SHALL 支持在光标处插入文本、按扩展字素移动左右光标、用 Backspace 删除前一字素和 Delete 删除后一字素。光标 MUST 始终处于合法字素边界。Home/End 与 Ctrl+A/E SHALL 移到逻辑行首/尾，Ctrl+B/F SHALL 等价于左/右。上下键 SHALL 优先在软折行后的视觉行之间移动并保留目标显示列；较短行不得使光标越界或落在字素中间。水平移动、修改文本、resize 和历史切换 SHALL 重置目标列。

Alt+Enter 与 Ctrl+J SHALL 插入 LF。普通 Enter 且光标前一字素为反斜杠时 SHALL 以 LF 替换该反斜杠，不提交。输入中的 CRLF 和独立 CR SHALL 规范化为 LF，不做其他 Unicode 规范化。手动 Tab SHALL 保持无操作；粘贴中的 Tab 属于正文。空闲 Esc 和空闲非空草稿 Ctrl+C SHALL 保持草稿，空闲空草稿 Ctrl+C 的退出语义不变。

#### Scenario: Edit inside a Unicode line
- **WHEN** 用户在中文、组合重音或 ZWJ emoji 之间移动、插入和前后删除
- **THEN** 操作以完整字素为单位，结果为合法 UTF-8，其他正文保持不变

#### Scenario: Preserve a preferred column
- **WHEN** 用户从长视觉行向下经过短行后继续进入长行
- **THEN** 短行光标停在合法可达边界，下一长行恢复原目标显示列

#### Scenario: Insert newlines without submitting
- **WHEN** 用户按 Alt+Enter、Ctrl+J，或在反斜杠后按普通 Enter
- **THEN** 只插入规定的换行，不产生提交、历史条目或 Provider 请求

#### Scenario: Keep multiline editing separate from exit
- **WHEN** 用户空闲且无 queued 输入时在非空草稿按 Esc 或 Ctrl+C
- **THEN** 草稿保持原样，应用不退出且不提交

### Requirement: Explicit text paste is atomic and bounded

对终端已解码并显式标识为 paste 的文本，TUI SHALL 在光标处原子插入规范化后的整个文本，内部换行、Tab、Esc、Ctrl+C 等内容 MUST NOT 被解释成操作键。随后到达的独立普通 Enter SHALL 按普通提交规则处理。空 paste SHALL 无操作。

草稿 MUST 不超过 4 MiB UTF-8 字节；插入或粘贴使规范化后的正文超限时 SHALL 整次拒绝、保持此前草稿及光标并显示不含正文的安全英文错误，不得截断后假装成功。

支持说明 MUST 明确：安全粘贴保证基于显式 bracketed paste/已解码事件；无此标记的输入按普通按键处理。不得声称 Shift+Enter 在所有终端可用、原始终端字节无损或草稿限额覆盖解码前缓冲。输入区帮助 SHALL 提供 Alt+Enter/Ctrl+J 等明确支持的换行方式。

#### Scenario: Paste code containing newlines and control characters
- **WHEN** 空闲 composer 收到含 CRLF、独立 CR、LF、Tab、Esc 和 Ctrl+C 的显式 paste
- **THEN** 换行规范化后一次插入，控制字符作为正文保留且不触发提交、取消或审批
- **THEN** 只有后续独立的操作键才能产生相应行为

#### Scenario: Reject an oversized paste atomically
- **WHEN** 新 paste 会使草稿超过 4 MiB
- **THEN** 原草稿、光标和历史浏览状态不变，显示安全错误，不生成部分新正文

#### Scenario: Accept the exact input limit
- **WHEN** 规范化后的输入恰好为 4 MiB 且不是全空白
- **THEN** 草稿可被编辑与提交，不因字符数或显示转义长度被错误拒绝

#### Scenario: Paste into a modal or an active turn
- **WHEN** 审批、命令详情或提交/取回等待占据输入焦点时收到 paste
- **THEN** 聊天草稿不变，不提交、不排队，也不把 paste 内部控制字符解释为批准或中断

### Requirement: Session input recall is bounded and preserves the working draft

TUI SHALL 提供当前 thread 的内存输入历史：以已有恢复语义视图中的用户文本初始化，并加入本进程接收成功的输入。不得访问全局输入历史、另写历史文件或从 UI 历史反向构建 Provider 请求。种子输入 SHALL 使用相同换行规范化；单条超限或全空白种子不进入 recall，不影响既有 transcript。

历史 SHALL 同时限制为最多 100 条及正文合计 8 MiB；相邻完全相同的输入折叠，超过任一预算按最旧优先淘汰。历史淘汰不影响 Session 事实。

空草稿首视觉行按 Up SHALL 优先取回 queued auto；无 queued auto 时，以及其他符合 recall 条件的首视觉行 Up，才进入较旧历史，在末视觉行按 Down SHALL 进入较新历史；普通上下移动优先。首次浏览 SHALL 保存当前草稿与光标，包括纯空白草稿。越过最新条目 SHALL 恢复保存的草稿与光标。上翻召回后光标放在首视觉行最后一个可停靠位置，下翻召回后放在正文末尾。编辑召回文本 SHALL 结束浏览并将其作为当前工作草稿，不改写历史条目；这时之前保存的浏览前草稿被放弃。无条目、最旧边界以及非浏览状态底部 Down SHALL 无操作。

#### Scenario: Return from history to the original draft
- **WHEN** 用户浏览历史且没有修改召回文本，然后下翻越过最新条目
- **THEN** 原草稿包括空白、光标位置均恢复，不自动提交

#### Scenario: Adopt an edited recalled prompt
- **WHEN** 用户修改一个召回条目
- **THEN** 修改后的内容成为当前工作草稿并结束历史浏览，已存历史不变
- **THEN** 该修改只有在新的提交接收成功后才进入输入历史

#### Scenario: Evict old entries without changing the journal
- **WHEN** 新条目使条数或总字节预算超限
- **THEN** 最旧条目被淘汰直到同时满足预算，Session records 不变

#### Scenario: Seed recall from a resumed thread
- **WHEN** 应用已提供恢复后的语义历史视图
- **THEN** TUI 从其中用户文本生成有界 recall，不读取 journal 或 Provider native payload

recall SHALL 从有序已提交用户消息初始化；accepted live 输入加入后，committed 不得重复加入。取回指定 IDs 前必须校验按 FIFO 以 LF 连接并接原草稿的总量不超过 4 MiB，等待时冻结草稿，失败不得修改；新增队列项不得被连带取回。

#### Scenario: Lose a retract race without losing the draft
- **WHEN** 取回前输入已经被 claim
- **THEN** 显示 input_not_queued，原草稿/光标/队列事实不变，不复制已被消费的正文
