# tui/task-progress Specification

## Purpose

定义模型报告的线程任务进度如何在普通聊天、完整清单面板与恢复后的界面中准确展示，使用户能辨认当前步骤、未完成事项和已完成数量，并保持持久化事实、输入焦点、阅读位置与 Provider 原生历史之间的清晰边界。

## Requirements

### Requirement: Current progress reflects only committed thread facts

TUI SHALL 将任务进度作为当前线程已成功持久化结果的只读投影，在用户回合结束、继续输入、停止及恢复后保留最近一次成功更新。只有成功任务结果完成 Sync 后发布的新快照 SHALL 改变当前清单；仅收到原生调用、参数增量、ready、started、失败、取消或未确认 durable 的结果 MUST NOT 提前发布候选内容或覆盖旧清单。清单项的 completed SHALL 明确表示模型报告的进度，不代表测试验证、权限批准或线程任务实际完成，不自动结束当前回合。

投影 SHALL 区分从未更新、显式空清单清除、未全部完成和全部完成；显式清除移除输入区活动列表，但只读完整面板仍能表明最近清单已被清除。进入新用户回合不得隐式清除旧状态。TUI MUST NOT 从自由文本、工具名称、assistant final、计时器或当前文件状态推断任务状态。

#### Scenario: Keep the previous list before result Sync
- **WHEN** 当前已有清单，模型提交新清单而结果 Sync 尚未成功，或者更新失败、取消或 durable 状态不确定
- **THEN** 当前清单保持不变，界面不将候选项显示为已保存的任务进度

#### Scenario: Preserve progress across user turns and resume
- **WHEN** 一个回合结束或被停止，用户提交后续输入或恢复同一线程
- **THEN** 最近成功结果对应的清单及完成状态仍可查看，不隐式重置或补造进行中任务

#### Scenario: Distinguish explicit clear from no update
- **WHEN** 线程从未成功更新任务，或者最后一次成功更新传入空清单
- **THEN** 两者均无输入区活动列表，而完整面板分别显示尚无任务更新和清单已被显式清除

#### Scenario: Do not infer verified completion
- **WHEN** 模型将全部任务标为 completed，而后续测试或 Stop 检查仍未结束
- **THEN** 任务区域只汇总模型报告的完成数量，不显示验证通过、不改变审批或停止边界，也不结束回合

### Requirement: Inline progress remains bounded near the composer

普通聊天 SHALL 在输入框上方提供有界当前清单，显示完成数量、总数、状态标记和当前进行中项；进行中项的规范 activeForm 非空时展示原 activeForm，否则展示规范 content，其余项展示 content。界面 SHALL 只消费规范任务快照，不读取 facade profile 或原生 wire，也不伪造 OpenAI activeForm。清单 SHALL 最多包含既有任务契约允许的 20 项，每项模型提供的 content 与 activeForm 分别最多 512 UTF-8 bytes；界面不能通过显示截断把超限输入变成合法任务事实。每项预览 SHALL 只占一物理行，安全转义后按显示宽度在字素边界截断，不换行。普通可用高度下 SHALL 根据终端高度将任务项区域限制在 3 至 6 行；数量标题最多一行，隐藏数量最多额外一行，展开区域总计最多 8 行；可用高度不足时 SHALL 继续收缩或只显示一行数量摘要。

任务区域 SHALL 与 composer、输入队列、固定状态/错误、补全和主线共用终端总高度预算；composer 光标与编辑内容、输入队列、固定状态及高优先审批/问卷/Hook 审查优先，其次为用户正在操作的补全。补全可见时任务区域 SHALL 降为最多一行统计，剩余空间不足时隐藏，不得挤掉当前选中候选或改变候选身份；Ctrl+T 和新任务更新也不得突破该优先级。布局及其派生缓存身份 SHALL 包含任务实际可见行数、补全实际可见行数与共同可用高度；任务折叠、更新、resize 或队列/状态高度变化后 SHALL 依据同一预算刷新，不能各自占用同一份剩余空间。

截断列表 SHALL 保持原清单相对顺序，以进行中项及邻近项作为可见窗口，并显示被隐藏的数量；没有进行中项时从原顺序前端显示。界面 MUST NOT 依据时间、文本匹配或随机排序重排任务。窄屏 SHALL 退化为清晰数量摘要，不产生横向溢出；NO_COLOR SHALL 使用文本和状态标记区分状态。显示副本 SHALL 按终端宽度布局并可见转义 C0/C1、DEL、双向控制和终端转义，原始 content、activeForm、说明及历史 bytes MUST 保持不变。

#### Scenario: Anchor a truncated list at the current task
- **WHEN** 20 项清单的进行中项位于中后段，而当前终端只允许显示少量任务行
- **THEN** 当前进行中项及邻近项可见，仍按输入顺序排列，并准确提示隐藏数量

#### Scenario: Preserve editing on a small terminal
- **WHEN** 终端高度或宽度不足以容纳通常的任务列表，或者审批需要展示空间
- **THEN** 任务区域缩为摘要或让出空间，composer 与审批保持可用，不改变清单、草稿或光标

#### Scenario: Render hostile task text without changing facts
- **WHEN** 合法 content 或 activeForm 含允许的双向格式字符，或合法 explanation 包含 LF/Tab，用户在普通、窄屏和 NO_COLOR 间切换
- **THEN** 界面安全呈现且所有行受列宽限制，任务原文、模型结果和 fingerprint 不变；非法标签控制字符仍在输入边界拒绝

#### Scenario: Share space with active completion under combined pressure
- **WHEN** 当前有 20 项任务、5 项可匹配补全、多行草稿、排队输入及固定状态，用户在窄屏中导航候选、resize、按 Ctrl+T 或接收新任务更新
- **THEN** 总显示不超过终端高度，先保留 composer、队列和状态，再保证补全当前选择可见；任务最多一行或隐藏，实际行数变化使共享布局重新计算，草稿、候选身份及任务原文不变

### Requirement: Progress expansion follows explicit local interaction

新的非空且未全部完成的成功更新 SHALL 自动展开输入区清单；新的全部完成更新 SHALL 自动折叠为一行完成数量统计，并保持完整清单可读。界面 MUST NOT 使用定时器删除任务、清空 durable 状态或将折叠行为写入 Session。普通聊天的独立 Ctrl+T SHALL 在当前非空清单的展开与折叠间切换；无清单时 SHALL 无操作，不发送输入或查询。仅重复查询同一成功事实、resize 或流式正文事件 MUST NOT 撤销用户的折叠选择。更新自动展开不得夺取已打开 modal 的焦点。

首次接受经验证的当前快照 SHALL 对非空未全部完成清单默认展开，对全部完成清单默认折叠，对 none/clear 不建立活动列表；该初始展示规则同时适用于启动、恢复以及初始查询返回前先到达的合法更新。后续重复取得相同事实 MUST NOT 重置用户展示选择。补全或高优先交互暂时压缩任务区域只影响实际可见行数，不能把布局退化当作用户折叠或修改已保存任务状态。

#### Scenario: Show a new list and allow manual folding
- **WHEN** 新的未全部完成清单成功持久化，随后用户在普通聊天按 Ctrl+T
- **THEN** 新清单先展开，Ctrl+T 仅折叠任务区域，草稿、光标、输入队列、活动回合及浏览锚点保持不变

#### Scenario: Retain the completed list without a deletion timer
- **WHEN** 新更新将非空清单全部标为 completed
- **THEN** 输入区自动显示一行完成统计，等待任意时长不清空任务，用户仍可展开或在完整面板阅读全部项

#### Scenario: Preserve a manual fold during refresh
- **WHEN** 用户折叠清单后发生正文增量、resize 或同一结果来源的只读快照回复
- **THEN** 清单保持折叠，不重复自动展开或提交本地命令

#### Scenario: Ignore an empty toggle
- **WHEN** 线程尚无清单或最近已显式清除清单，用户在普通聊天按 Ctrl+T
- **THEN** 草稿、浏览状态和回合不变，零模型请求、零 Session 写入且不创建空任务

#### Scenario: Initialize expanded and completed progress consistently
- **WHEN** 启动或恢复后首次经验证快照分别为未全部完成清单、全部完成清单、none 或 clear
- **THEN** 未完成默认展开，全部完成默认折叠为统计，none/clear 无活动列表；随后重复查询不重置用户 Ctrl+T 选择

### Requirement: Tasks command opens a read only complete list

TUI SHALL 提供无参数本地 `/tasks`，在空闲或活动回合通过强类型宿主查询打开当前线程完整清单面板，按原顺序显示全部项、各项状态和完整 content/activeForm，以及最近更新说明与完成统计。面板 SHALL 可滚动并受终端尺寸约束，对从未更新和显式清除提供不同说明；查询失败 SHALL 显示安全英文错误并允许返回，不能伪装成空清单。`/tasks` 附带参数 SHALL 经正常本地命令校验拒绝并保持完整草稿，不进入模型或作为 Skill 调用。该命令 SHALL 只读，不切换任务状态、调用模型、运行工具或写 Session。

面板 SHALL 保留打开前的草稿、光标和主线阅读锚点；退出后恢复原状态与当前布局。面板导航、Enter、普通字符及 paste MUST NOT 泄漏到 composer 或成为审批回答；Esc SHALL 只关闭面板，Ctrl+C SHALL 沿用当前回合定向停止及空闲退出规则。审批、问卷及 Hook 信任/审查 SHALL 优先于面板接收按键，并在返回时保持有效任务阅读位置。面板只保存当前有界清单，不建立额外无界历史副本。

面板打开期间收到较新成功结果 SHALL 原子更新内容；原锚点的完整任务标签仍存在时保留该项阅读位置，否则将滚动位置夹到新内容的合法范围，不新增任务 ID。明确 clear SHALL 在同一面板显示清除状态，不自动关闭或恢复旧清单。

#### Scenario: Inspect all task fields during a turn
- **WHEN** 活动回合中用户打开 `/tasks` 并滚动一份 20 项清单
- **THEN** 可读取各项完整文本、状态和最近更新说明，现有回合继续，零模型请求及零 Session 写入

#### Scenario: Reject arguments without losing the draft
- **WHEN** 用户提交 `/tasks unexpected`
- **THEN** 本地命令安全拒绝，保留完整草稿和光标，不向 Provider 发送该文字或执行同名 Skill

#### Scenario: Restore draft and reading position after closing
- **WHEN** 用户打开面板、滚动和 resize，然后按 Esc
- **THEN** 返回打开前草稿和主线锚点并采用当前窗口布局，面板导航未产生用户输入或任务修改

#### Scenario: Higher priority interaction interrupts the panel
- **WHEN** 用户正在查看任务面板时新审批、问卷或 Hook 审查到达
- **THEN** 独占交互接管按键，任务面板按键不能触发批准或回答，结束后可返回有效任务阅读位置

#### Scenario: Refresh a replaced current list without inventing task identity
- **WHEN** 面板停留在某项时新成功更新重排或删除该项，随后可能显式清除
- **THEN** 标签仍存在时保持对应项，否则滚动夹到新范围；清除在面板内明确显示，不关闭面板或覆盖草稿

### Requirement: Progress queries recover the committed projection without stale replacement

初始连接或恢复 SHALL 通过强类型当前任务快照查询 hydrate 当前进度；当前进度更新事件 SHALL 携带线程身份和来源成功结果的 durable result_sequence。result_sequence SHALL 只表示 journal 事实顺序，不是格式版本。非空更新与显式清除都 SHALL 绑定其成功结果；从未更新状态 SHALL 与已有成功来源明确区分。事件丢失或界面重建后 SHALL 可重新查询获得相同当前投影，不依赖 TUI 缓存成为事实源。

界面 SHALL 独立维护 pending、failed、ready 的本地 hydration 状态，不能以业务 none 代替尚未取得快照。首次合法快照或更新到达前 SHALL 显示 loading，查询失败后 SHALL 显示 unavailable 和安全英文错误并允许重试，不宣称尚无任务或清单已清除；只有经过验证的业务 none/clear 才能产生相应说明。已有经验证快照时刷新失败 SHALL 保留原内容并标明查询不可用，不回退为 none。hydration 状态只在 UI 内存，不扩展业务三态、Session 或 checkpoint。

查询回复及更新 SHALL 校验线程和 durable 结果顺序，查询回复另须匹配本地 UI generation；其他线程、较旧成功结果或已关闭/被替换查询的迟到回复 MUST NOT 覆盖新清单、重开面板或改变折叠状态。关闭面板或变更查询 generation MUST NOT 阻断同线程较新 durable 更新推进当前投影；更新不得因此重新打开面板或夺取焦点。无成功来源的初始 none 回复到达前若已接受成功结果更新，SHALL 将该 none 作为过期回复丢弃。

相同 result_sequence 只有完整 source、完整规范任务更新和所有派生计数均一致时 SHALL 幂等；相同序号而 turn_id、invocation_id、任务标签/顺序/activeForm/status、说明或计数任一不同，MUST 作为协议冲突拒绝，保留已验证快照和展示选择并报告不含任务正文的安全英文协议错误。不得覆盖、拼接矛盾快照或仅按序号相等将冲突当作正常重复。初始 hydration、手动查询、浏览、折叠与显示 SHALL 不读取 Provider wire、不发模型请求、不执行工具、不改变权限或写 Session，也不为查询安装 watcher 或新的持久化来源。

#### Scenario: Discard an older snapshot after an update
- **WHEN** 旧快照查询在途时收到较新 durable 结果的更新，随后旧回复返回
- **THEN** 界面保留较新清单与当前折叠状态，旧回复不能覆盖或重新打开已关闭面板

#### Scenario: Recover from a missing event
- **WHEN** 一个成功任务更新事件未送达 TUI，随后用户查询或界面重新建立
- **THEN** 通过当前线程快照取得最新成功清单及来源结果顺序，不重新执行更新或补造 journal 事实

#### Scenario: Isolate threads and obsolete UI requests
- **WHEN** 已关闭的面板请求或其他线程的回复在新 UI generation 到达
- **THEN** 回复被丢弃，当前任务、草稿、浏览锚点及模型历史不变

#### Scenario: Keep unknown hydration distinct from no tasks
- **WHEN** 恢复线程的首次任务查询被阻塞，随后失败并由用户重试
- **THEN** 未知期间显示 loading，失败后显示 unavailable 和安全错误；不显示业务 none/clear，重试取得合法快照后才进入 ready 并按其真实内容展示

#### Scenario: Ignore the initial empty reply after a committed update
- **WHEN** 初始查询的 none 回复在途时先收到并接受同线程成功任务更新，随后旧 none 回复返回
- **THEN** 界面保留成功更新、ready 状态和相应展示选择，不回退为空清单或重置面板锚点

#### Scenario: Reject contradictory snapshots with the same result sequence
- **WHEN** 已接受某结果序号后，又收到相同序号但完整 source、任一任务字段、顺序、说明或派生计数被逐字段改变的快照
- **THEN** 每个矛盾快照均报告安全协议错误并保留已验证内容、折叠选择和浏览锚点，不向模型提交文字或写 Session

#### Scenario: Accept an exact duplicate without presentation changes
- **WHEN** 相同线程、结果序号、完整 source、规范任务更新及派生计数完全一致的快照重复到达
- **THEN** 幂等保留当前内容，用户折叠、面板是否打开、滚动位置和草稿不变

#### Scenario: Continue observing updates after closing the task panel
- **WHEN** 用户关闭任务面板使查询 generation 前进，随后收到同线程较新 durable 任务更新以及旧面板回复
- **THEN** 新更新推进当前进度但不重开面板，旧回复被丢弃，草稿与焦点保持不变
