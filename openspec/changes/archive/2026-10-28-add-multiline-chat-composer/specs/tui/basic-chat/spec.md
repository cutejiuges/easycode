## MODIFIED Requirements

### Requirement: User can submit and observe a streaming text turn

空闲状态下，用户 SHALL 能编辑多行文本，并通过普通 Enter 提交合法非空白输入；换行快捷键与反斜杠换行规则 SHALL 优先于提交。正文 SHALL 保留输入规范化后的缩进、内部换行、尾部空格和尾换行，不得以 trim 改写；空白判断仅用于拒绝空输入。等待提交接收及活动 turn 结束前 MUST NOT 发起第二次提交或把输入排入后续 turn。

提交接收成功后 TUI SHALL 在 transcript 中显示恰好一条用户消息，并按 RuntimeEvent 顺序增量显示 assistant 文本。接收仅表示进程内 admission，不得声称 Session 已 durable。接收失败 SHALL 恢复原草稿、光标和历史浏览状态，且不增加已接收用户消息或输入历史。接收成功后的 turn 失败 MUST NOT 自动恢复 pending 草稿、重新提交或重试工具。

Runtime发布`tool_approval_requested`时，TUI SHALL暂停聊天composer的键盘路由并进入与该request identity绑定的modal approval overlay。overlay解决前的普通字符、Enter、方向键、Tab和粘贴不得修改或提交聊天草稿；已有草稿、光标和历史浏览状态必须保留，并在overlay关闭后恢复。TUI只通过强类型`resolve_tool_approval`或定向interrupt响应当前request，不得直接调用Policy、Session、Provider或Patch executor。

#### Scenario: Submit a non-empty prompt
- **WHEN** 用户在空闲状态输入文本并按普通 Enter，且不匹配换行规则
- **THEN** 输入进入等待接收状态，最多发起一次提交
- **THEN** 接收成功后 transcript 增加该用户消息并开始消费 turn 事件

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
- **WHEN** 提交正在等待接收、当前turn正在streaming或等待approval且用户按Enter
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

## ADDED Requirements

### Requirement: Composer editing preserves grapheme boundaries

聊天 composer SHALL 支持在光标处插入文本、按扩展字素移动左右光标、用 Backspace 删除前一字素和 Delete 删除后一字素。光标 MUST 始终处于合法字素边界。Home/End 与 Ctrl+A/E SHALL 移到逻辑行首/尾，Ctrl+B/F SHALL 等价于左/右。上下键 SHALL 优先在软折行后的视觉行之间移动并保留目标显示列；较短行不得使光标越界或落在字素中间。水平移动、修改文本、resize 和历史切换 SHALL 重置目标列。

Alt+Enter 与 Ctrl+J SHALL 插入 LF。普通 Enter 且光标前一字素为反斜杠时 SHALL 以 LF 替换该反斜杠，不提交。输入中的 CRLF 和独立 CR SHALL 规范化为 LF，不做其他 Unicode 规范化。手动 Tab SHALL 保持无操作；粘贴中的 Tab 属于正文。空闲 Esc 和非空草稿 Ctrl+C SHALL 保持草稿，空草稿 Ctrl+C 的退出语义不变。

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
- **WHEN** 用户在非空草稿按 Esc 或 Ctrl+C
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
- **WHEN** 审批、命令详情、提交等待或活动 turn 占据输入焦点时收到 paste
- **THEN** 聊天草稿不变，不提交、不排队，也不把 paste 内部控制字符解释为批准或中断

### Requirement: Session input recall is bounded and preserves the working draft

TUI SHALL 提供当前 thread 的内存输入历史：以已有恢复语义视图中的用户文本初始化，并加入本进程接收成功的输入。不得访问全局输入历史、另写历史文件或从 UI 历史反向构建 Provider 请求。种子输入 SHALL 使用相同换行规范化；单条超限或全空白种子不进入 recall，不影响既有 transcript。

历史 SHALL 同时限制为最多 100 条及正文合计 8 MiB；相邻完全相同的输入折叠，超过任一预算按最旧优先淘汰。历史淘汰不影响 Session 事实。

在首视觉行按 Up SHALL 进入较旧历史，在末视觉行按 Down SHALL 进入较新历史；普通上下移动优先。首次浏览 SHALL 保存当前草稿与光标，包括纯空白草稿。越过最新条目 SHALL 恢复保存的草稿与光标。上翻召回后光标放在首视觉行最后一个可停靠位置，下翻召回后放在正文末尾。编辑召回文本 SHALL 结束浏览并将其作为当前工作草稿，不改写历史条目；这时之前保存的浏览前草稿被放弃。无条目、最旧边界以及非浏览状态底部 Down SHALL 无操作。

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

### Requirement: Composer layout is safe deterministic and cursor aware

输入区 SHALL 根据终端宽高软折行，最多显示 8 行正文并在尺寸受限时进一步收缩，光标始终在可见区域。resize MUST 不改变正文、字节光标或历史条目。窄屏 SHALL 优先显示光标和正文，再按可用空间显示提示；尚未取得尺寸时按 80x24 渲染。光标移动与显示 SHALL 使用一致的字素/显示列映射，纯 render 不执行 I/O 或修改业务状态。

LF SHALL 显示为换行，Tab SHALL 按 4 列制表位展示；C0（LF/Tab 除外）、DEL、C1、双向控制字符 SHALL 显示为可见转义，不得执行原始终端控制。相同安全显示规则 SHALL 用于已提交用户消息。展示副本 MUST NOT 回写正文、Provider 请求、Session 或 cache fingerprint。无色模式 SHALL 仍提供可见的插入位置，转义显示和光标标记不得造成正文光标落在字素中间。

#### Scenario: Resize a long draft
- **WHEN** 多行或长单行草稿在正常、窄屏和极小尺寸之间切换
- **THEN** 正文和字节光标不变，视口调整使插入位置可见且不出现负尺寸或越界

#### Scenario: Render a terminal escape safely before and after submission
- **WHEN** 草稿包含终端控制字符并随后被接收
- **THEN** 输入区与用户 transcript 均显示可见转义，不输出可执行的原始控制字符
- **THEN** 提交正文保留输入规范化后的原值

#### Scenario: Keep UI state out of requests
- **WHEN** 同一正文经过不同尺寸、光标移动或历史浏览后被提交
- **THEN** 双 Provider 的对应请求 canonical bytes 与稳定前缀不受这些 UI 操作影响
- **THEN** 同一已提交历史在 uninterrupted 与 restored 路径生成等价的后续请求、原生 item 顺序与 fingerprint

#### Scenario: Render without color
- **WHEN** 输入区以无色方式显示含中文、emoji 和软折行的草稿
- **THEN** 仍能识别插入位置，显示列计算与光标移动一致
