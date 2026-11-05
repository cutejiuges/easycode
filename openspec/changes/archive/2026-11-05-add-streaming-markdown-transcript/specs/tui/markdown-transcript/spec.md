## Purpose

为 EasyCode 双 Provider 的助手回复提供一致、安全、有界的终端 Markdown 阅读体验，覆盖流式输出、已提交主线与历史正文详情，并明确完整正文、摘要和分页片段的区别，保持原始历史、用户输入、工具审批和后续模型请求不受展示影响。

## ADDED Requirements

### Requirement: Complete assistant text has structured Markdown presentation

TUI SHALL 对完整助手正文展示标题、段落、强调、删除线、引用、有序和无序列表、任务列表、行内代码、围栏和缩进代码块、链接及表格。代码 SHALL 保留可见内容、缩进和空行，超宽内容按显示列换行。有色模式 SHALL 为 Go、JSON、Python、JavaScript/TypeScript、Shell 提供基础语法高亮；未知语言保持纯文本，已支持语言高亮失败或超预算时保留正文并显示原因。无色模式不得丢失内容。用户正文、工具输入/结果和审批 diff MUST 保持各自原文展示，不因包含 Markdown 标记改变语义。HTML、图片、数学和 Mermaid SHALL 保留可读文字或代码表示，不执行或加载外部内容。

#### Scenario: Read a coding answer
- **WHEN** 助手回复同时包含标题、任务列表和带语言标签的代码块
- **THEN** 用户能辨认标题、任务状态和代码结构，代码内容不被删除或执行
- **THEN** 同样文本出现在工具结果或用户输入中时保持原文展示

#### Scenario: Preserve unsupported rich content visibly
- **WHEN** 助手正文包含 HTML、图片引用或 Mermaid 围栏
- **THEN** 显示可读原文、替代文本和目标或代码块，不下载图片、不执行 HTML、不启动外部程序

### Requirement: Code highlighting uses isolated and bounded lexical scanning

高亮 SHALL 使用实例持有的有限语言词法扫描器，覆盖 Go、JSON、Python、JavaScript、TypeScript、Shell 的基础词法分类，包括关键字、标识符、数字、字符串、注释及运算符/标点；JSON 额外区分属性名，Shell 额外区分变量。支持合法字符串前缀、跨行字符串/注释、模板与 Shell 展开及 here-document 的有界状态处理；基础词法着色不承诺类型推断或完整语法分析。未知语言 SHALL 保留安全纯文本，未闭合 Markdown 围栏 SHALL 在闭合或消息收口后才高亮。支持语言的典型合法样例 MUST 实际产生正确类别和样式，不能以全部纯文本回退代替语言实现。

扫描器 MUST 无 I/O、goroutine、定时器、外部进程及全局可变配置，不依赖回溯正则或全局超时时钟。每次扫描 SHALL 隔离词法状态，token MUST 使用连续、无重叠、完整覆盖输入且处于合法 UTF-8 边界的源区间，拼接后逐字节等于扫描输入。净化展示副本的偏移 MUST 通过映射关联原文，不改变持久化字节或查询偏移。

扫描 MUST 对输入字节读取/比较、lookahead、状态转换及 token 发射计费，步骤不超过 `128 × (n + 1)`，n 为展示输入字节数且最多 256 KiB。嵌套栈和待处理 here-document 队列各最多 64 项，scratch 最多 16 KiB；token 与解析节点共享 32768 节点预算，token 数组、scratch 与源映射容量计入既有 2 MiB 派生预算，临时与已缓存数据同时存在时也 MUST 计入。分配前检查，禁止先生成再截断预算。

预算耗尽或扫描失败 SHALL 丢弃未发布的部分高亮、返回明确类型原因并保留安全纯文本；正文仍受原有布局及分页规则保护。成功与失败结果 SHALL 按来源、内容和语言复用，重复 View、resize、r 和同一来源完成通知 MUST NOT 重扫未变代码；预算失败不得逐帧重试。页面淘汰 SHALL 释放全部 token 与源引用。

#### Scenario: Distinguish code from strings and comments
- **WHEN** 六种受支持语言的代码含关键字、数字、注释标记、转义引号和多行字符串，模板或 Shell 展开还含嵌套括号
- **THEN** 固定词法样例得到预期类别和区间，字符串中的注释标记不结束字符串，块间不泄漏状态
- **THEN** 有色及无色显示保留相同代码字符、缩进和空行，控制序列仍被安全展示

#### Scenario: Stop at a deterministic budget
- **WHEN** 输入触及步骤、token、派生容量、嵌套深度或 here-document 队列限额
- **THEN** 在超额读取或分配前停止并显示高亮不可用原因，不发布部分 token、不丢失原文或改变会话成功状态
- **THEN** 后续重复绘制不会重复执行同一失败扫描，输入与取消仍满足原有响应目标

#### Scenario: Isolate instances and blocks
- **WHEN** 两个实例交错处理不同语言，或同一实例连续处理未闭合字符串与正常代码块
- **THEN** 每块词法状态独立，结果与单独扫描相同，无全局资源、后台工作或跨实例可变状态

#### Scenario: Reuse highlighting after streaming completion
- **WHEN** 同一代码块以不同合法 UTF-8 delta 划分到达，随后闭合或消息收口，再发生 resize、r 和重复 View
- **THEN** 未闭合阶段不重复高亮，收口结果与一次性扫描相同，未变代码不再扫描
- **THEN** 新代码块到达或文档引用定义改变不导致已有未变代码块重新高亮

### Requirement: Streaming presentation is independent of chunk boundaries

对于相同有界原文前缀，解析结果 SHALL 与完整基准解析具有相同语义，不受 delta 划分影响。屏幕展示 SHALL 区分稳定正文、可变尾块与未完成行预览：普通文字及时显示，表格未完成行和链接未闭合目标允许暂留，MUST NOT 为每个 delta 强制重排全部历史。完成、失败或取消 SHALL 收口全部已接收尾文且无遗漏、无重复；失败与取消不得伪装已保存。

#### Scenario: Reference definition arrives after prose
- **WHEN** 引用定义在后续 delta 到达并影响前文
- **THEN** 相关解析和布局失效后得到与完整基准一致的语义，不保留错误的稳定块

#### Scenario: Hold an unfinished table row
- **WHEN** 表格行或链接目标跨多个 delta 到达
- **THEN** 未完成结构按确定规则暂留，普通文字仍有进度，结构完成后展示正确内容
- **THEN** 最终完成或取消时所有已接收文字均被显示或明确标示为有界片段，不因没有换行而消失

#### Scenario: Keep ordinary small deltas responsive
- **WHEN** 长代码或普通长行以小 delta 持续输出，用户编辑或中断
- **THEN** 输入、审批与中断继续被处理，不因反复解析全部历史而积压到输出结束

### Requirement: Incomplete text is disclosed and never parsed as a complete document

TUI MUST 区分完整正文、从头增长的正文、截断尾片段、列表摘要和正文分页片段。存在缺失前后文的持久化摘要或分页片段 SHALL 明确显示为原文片段，不猜测代码围栏、表格和引用上下文。正文超过单页时 SHALL 保留完整的原文分页访问能力，不后台拼接任意长度文档。

主线 SHALL 按当前可见范围和阅读位置加载助手正文，优先保留预算内仍可见的已加载项，不仅补齐最新回复。正文查询单页上限为 64 KiB；只有收到整篇正文才展示完整 Markdown，失败、超长或预算不足时保留安全摘要/原文片段及可阅读入口。第五个超额候选不得导致加载与淘汰循环。旧回复在详情中使用同一完整性规则，提示 MUST 与正文分离。

#### Scenario: Keep visible earlier replies readable
- **WHEN** 可见助手正文 A 和 B 之间存在工具活动，正文各超过摘要长度且不超过单页
- **THEN** 在预算允许且查询成功后 A 和 B 均显示完整 Markdown，工具保持真实顺序
- **THEN** 新回复到达不使仍可见的 A 无故退回摘要，离屏历史不被批量无界加载

#### Scenario: Browse a multi-page fenced block
- **WHEN** 正文大于 64 KiB 且当前页从代码块中间开始
- **THEN** 显示原文片段及实际字节位置，不将代码中的 Markdown 当作正文结构
- **THEN** 用户仍可前后翻页读取全部已保存正文

#### Scenario: Reject late hydration
- **WHEN** 主线补齐返回前用户切换查询状态或新的 committed 快照到达
- **THEN** 旧回复不覆盖当前正文，取消后的查询资源按原所有权路径清理

#### Scenario: Lose the beginning of a streaming reply
- **WHEN** 流式显示超过既有 64 KiB 上限并仅保留尾片段
- **THEN** 展示明确的流式原文片段提示，不沿用已丢弃前缀的解析状态

### Requirement: Terminal layout remains readable and safe

布局 SHALL 按终端显示宽度和字素处理中文、组合字符及 emoji，正常字素不得被拆坏。网格表格无法在可用宽度内合理展示或单行记录换行超过四行时 SHALL 转为逐记录字段展示，保留列顺序和全部单元格。无色模式 SHALL 仍保留列表、代码、引用和表格的结构。极窄屏无法容纳单个宽字素时 SHALL 使用可辨认替代标记而不输出破损字节。

来源正文中的终端控制序列、实体解码后的控制字符和双向文本控制字符 MUST 被安全展示，不得控制终端、写剪贴板或改变光标。链接 SHALL 展示标签及目标，不隐式访问目标；渲染产生的样式仅限受控 SGR。净化 MUST 只作用于展示副本，不修改查询偏移或持久化正文。

#### Scenario: Read a wide table on a narrow screen
- **WHEN** 含中文和长路径的多列表格在 34 列窗口中无法合理排布
- **THEN** 切为按列名展示的记录，内容仍可读且无横向溢出；扩大窗口后可恢复网格

#### Scenario: Reject encoded terminal injection
- **WHEN** Markdown 链接、普通文字或 HTML entity 包含 ESC、OSC 或 bidi 控制
- **THEN** 显示安全文字，不产生源内容指定的终端指令，也不修改原文

#### Scenario: Render without color
- **WHEN** 宿主选择无色模式并展示同一正文
- **THEN** 不输出样式控制码，代码、列表和引用仍有可辨认结构，模型请求保持不变

### Requirement: Raw view respects existing focus and query boundaries

完整助手正文详情 SHALL 提供 `r` 切换 Markdown 与安全原文，显示当前模式并按消息与源文本位置尽量保持阅读锚点；精确位置不存在时定位最近保留范围并给出提示。片段详情 SHALL 固定原文并说明原因。离开详情或选择新条目 SHALL 恢复默认模式。原文模式 SHALL 保留 Markdown 标记、空格和换行，仅净化终端控制。审批优先级、Esc/Ctrl+O 导航、Ctrl+C 中断和草稿隔离 MUST 沿用原有规则。

#### Scenario: Inspect literal source and return
- **WHEN** 用户在完整助手详情按 r，滚动并返回主线
- **THEN** 能读取原始 Markdown 标记，草稿、光标和发送状态不变，不启动模型请求

#### Scenario: Approval takes focus
- **WHEN** 历史详情打开期间审批到达，用户按 r 或 Enter
- **THEN** 按键仅由审批当前规则处理，不切换背后正文、不向 composer 泄漏输入

### Requirement: Rendering is bounded and has no authority over conversation state

Markdown 展示 SHALL 维持最多四个查询缓存页和每正文页 64 KiB 的现有约束，主线补齐占用的页必须计入总数。重复绘制未变化正文 MUST 复用准备好的解析和布局；活动消息变化 MUST NOT 触发全部已完成历史重新解析。resize SHALL 只改变布局，不直接请求正文或修改语义事实；新增可见项在后续导航或事实更新时加载。所有展示状态 SHALL 由实例持有，构造和 View 不得执行 I/O 或启动后台任务。解析节点、布局及交接视图 MUST 具有独立预算，不能复制查询正文绕过页数限制；超预算 SHALL 明示并提供安全原文读取。

显示、切换原文、resize 和历史浏览 MUST 不改变 Provider canonical request、native item 顺序、cache fingerprint、Session bytes、工具审批或执行结果，不暴露 opaque reasoning。

#### Scenario: Repaint a long history
- **WHEN** 50 项历史中只有最新消息继续增长，期间重复 View 并调整窗口
- **THEN** 旧消息不被重复解析，查询缓存仍不超过四页，不额外发起模型或正文请求

#### Scenario: Continue after reading Markdown
- **WHEN** 任一 Provider 会话经过富文本查看、原文切换和 resize 后继续对话或恢复
- **THEN** 后续请求 canonical bytes、native 顺序和 fingerprint 与未进行这些展示操作的同一历史一致

### Requirement: Durable projection replaces the live display without a blank transition

收到已提交边界时 TUI SHALL 有界暂留当前可见画面，直到该边界的查询投影可以原子接替，MUST NOT 先清空正文再等待查询。边界后到达的活动内容 SHALL 独立保留；连续提交、取消、失败和迟到查询不得造成旧正文覆盖新内容或重复展示。暂留视图 MUST 不用于恢复、模型请求、工具授权或结果事实，不按文本相等猜测消息身份。

#### Scenario: Delay committed projection
- **WHEN** 提交已确认但正文查询被确定性阻塞，随后有新 delta 到达
- **THEN** 已显示内容不闪空，新内容继续接收；查询完成后对应范围恰好一次被替换

#### Scenario: Coalesce successive commits
- **WHEN** 前次查询尚未返回时又发生两次提交，旧结果随后到达
- **THEN** 以最新目标边界交接，旧结果不能倒退画面，暂留状态不随提交次数无限增长

#### Scenario: Fail projection refresh
- **WHEN** 已提交投影查询失败
- **THEN** 保留有界画面并显示刷新失败，不伪造查询成功，不重发模型或重复执行工具

### Requirement: Source anchors survive presentation changes

主线回看、详情 Markdown/原文切换和 resize SHALL 尽量保持同一消息的源位置；查看旧内容时新输出不得强制跳到底部。默认跟随模式 SHALL 持续显示最新内容；用户回到主线底部恢复跟随。锚点离开有界历史窗口时 SHALL 显示提示并选择最近保留位置，不能静默冒充原位置。

#### Scenario: Read while new output arrives
- **WHEN** 用户在主线回看旧回复，模型继续输出
- **THEN** 阅读位置保留并显示新内容提示，返回底部后恢复跟随

#### Scenario: Toggle and resize a code block
- **WHEN** 用户在代码中部切换原文并改变宽度
- **THEN** 仍定位该消息对应源范围，草稿和正文不变，不固定跳回首行
