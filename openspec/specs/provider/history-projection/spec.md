## Purpose

定义 Provider 原生历史到共享文本语义视图的单向、只读投影契约，使 token 估算、历史回放、Hooks 和 Subagent 等后续消费者能够读取一致语义，而不解析 Provider wire 或替代原生续写事实源。

## Requirements

### Requirement: Project committed native history into ordered semantic turns

每个受支持的 Provider SHALL 将已成功提交的原生历史投影为按提交顺序排列的语义 turn。每个语义 turn MUST 使用真实 turn identity，并以强类型有序消息保留用户与 assistant 可见文本；用户消息包含 InputID，同 turn 中不同用户输入不得合并，视图 MUST 标识原生历史所属的 Provider family。

投影 SHALL 只读取已提交历史；未提交 staging 和部分 assistant 数据 MUST NOT 出现在视图中；失败、取消、timeout 或提前 EOF 不得删除此前独立提交的用户输入及成功 sample。空会话 SHALL 产生来源明确且 turn 列表为空的有效视图。

#### Scenario: Project an empty conversation

- **WHEN** 任一受支持 Provider 的会话尚未提交任何 native history
- **THEN** 投影结果标识该 Provider family，且语义 turn 列表为空

#### Scenario: Preserve committed turn order

- **WHEN** 会话已按顺序成功提交多个 turn
- **THEN** 投影结果按相同顺序包含每个 turn 的用户文本和 assistant 可见文本

#### Scenario: Exclude an uncommitted turn

- **WHEN** 一个新 turn 仍在 streaming，或最终以失败、取消、timeout 或提前 EOF 结束
- **THEN** 投影保留该 turn 已提交的用户输入及成功 sample，不包含未提交 assistant 输出

#### Scenario: Preserve interleaved messages in one turn
- **WHEN** 同一真实 turn 依次提交用户、assistant sample、中途用户和最终 assistant
- **THEN** 投影保持四者消息顺序与同一 turn 归属，恢复不能以每条用户消息推断新 turn

已提交 hook_feedback SHALL 具有独立的 Hook 来源语义角色及真实 turn/source sample 关联，不能制造 InputID、用户召回文本或助手完成。其正文只来自已验证 native 事实，不重新读取脚本。既有用户/assistant 消息的原始顺序与身份保持。仅有 continued 冻结决定而 native 尚未交付时，语义历史不得提前出现反馈；真实纠正随三记录交付保存后，保留原 InputID/Skill 附件归属，不与 Hook 来源合并。

#### Scenario: Keep a hook message distinct from real user input
- **WHEN** 同一 turn 的候选答复后保存 Stop 反馈与真实纠正，再保存模型续答
- **THEN** 投影保持 assistant/Hook/user/assistant 顺序及真实用户身份；反馈不成为真实 user 或 recall 条目

已提交的 SessionStart 上下文 SHALL 独立标识来源并保留原生提交顺序。startup/resume 上下文位于实际所属语义 turn 的第一条真实 initial 输入之前。compact 上下文 SHALL 以有序的 checkpoint 关联项投影，自动来源可关联实际作用域，手动来源以真实 CompactionID 和 checkpoint 位置锚定。语义 turn 始终使用真实身份，来源项保存获准且已提交的规范模型正文及其来源。上下文 MUST NOT 变成真实用户消息、用户召回条目、Stop 反馈或助手 sample。只有已应用的 native 内容产生该语义项。

#### Scenario: Keep the initial input association intact
- **WHEN** 已保存的 startup 或 resume 上下文和带有 Skill 或文件附件的首个真实输入同批提交
- **THEN** 语义历史保持上下文、用户及后续助手项的顺序，既有 InputID、Skill 和文件附件只关联真实输入

#### Scenario: Project manual compact context outside the turn list
- **WHEN** 手动压缩后已保存 compact 上下文，而用户尚未提交下一条真实输入
- **THEN** 完整语义视图保留实际 checkpoint 来源与顺序，不产生新的 turn、用户消息或助手 sample

### Requirement: Provider projectors preserve visible text semantics

Anthropic 投影 SHALL 按原生 message 与 content block 顺序投影独立消息，仅在同一消息内部连接属于原始正文的 text blocks。OpenAI 投影 SHALL 按真实 turn、原生 item 与 content part 顺序投影独立用户和 assistant 消息，仅在同一消息内部连接属于原始正文的文本。连接 MUST 精确保留各文本片段的内容和接收顺序，不得自行插入、删除或规范化空格与换行。

对于同一成功 turn，live RuntimeEvent 文本增量按顺序连接后的 assistant 文本 MUST 与该 turn 完成后的历史投影文本一致。

#### Scenario: Project Anthropic text blocks

- **WHEN** 一个已提交 Anthropic turn 的 user 和 assistant message 各包含一个或多个 text block
- **THEN** 投影保持每条 user/assistant 消息边界并按各自 content block 顺序连接文本，并保留原始空格和换行

#### Scenario: Project OpenAI text parts and items

- **WHEN** 一个已提交 OpenAI turn 包含 user `input_text` 以及一个或多个 assistant message item 的 `output_text`
- **THEN** 投影按 item 和 content part 的原始顺序连接对应文本，并保持该 turn 的单一语义边界

#### Scenario: Match live and replayed assistant text

- **WHEN** 一个 turn 的 live stream 产生多个 assistant 文本增量并成功提交
- **THEN** 按事件顺序连接的 live 文本与随后投影得到的 assistant 文本完全相同

显式 Skill 附件 SHALL 使用独立强类型只读投影关联到原 InputID，不拼入用户原文；用户 recall 只读取原文，详情可读取附件来源及正文，有效窗口估算覆盖实际附件内容。离线预览与正常恢复使用相同投影，附件不能成为 Provider 请求来源。

#### Scenario: Recall a Skill command without its expansion
- **WHEN** 原始输入 /review diff 携带已提交 Skill 附件
- **THEN** recall 仍为 /review diff，附件详情单独可见，估算不遗漏或重复计算附件

#### Scenario: Preview Skill history offline
- **WHEN** 不提供凭据且 Skill 文件不存在时投影合法存储历史
- **THEN** 与正常恢复具有相同原文和附件投影，不访问磁盘、网络或 opaque reasoning

### Requirement: Semantic history excludes Provider-private and opaque data

语义视图 MUST NOT 包含或暴露 Anthropic thinking、signature、redacted thinking、OpenAI reasoning summary、encrypted content、Provider phase、usage、response/message ID、未知扩展、原始 JSON 或请求字段。没有可见文本的受支持原生项 SHALL 被忽略且不得伪造占位文本。

投影不得改变、删除或重写原生历史；同一原生历史在投影前后编译出的后续 Provider 请求 MUST 保持相同的原生顺序、canonical bytes 和 fingerprint。

#### Scenario: Omit Anthropic opaque thinking data

- **WHEN** Anthropic 原生历史包含 thinking/signature、redacted thinking 和 text blocks
- **THEN** 语义视图只包含 text block 的可见文本，且不包含 signature、redacted data 或 thinking 正文

#### Scenario: Omit OpenAI reasoning data

- **WHEN** OpenAI 原生历史包含 reasoning summary、encrypted content、phase、未知 item 和 assistant text
- **THEN** 语义视图只包含当前切片支持的用户与 assistant 可见文本，不包含其他原生字段或 opaque bytes

#### Scenario: Preserve request compilation after projection

- **WHEN** 对一份已提交原生历史执行一次或多次语义投影后再编译下一轮请求
- **THEN** 请求的原生 item 顺序、canonical bytes 和 fingerprint 与未执行投影时完全相同

上述隐私排除 SHALL 同样适用于按 sample 的 Stop 正文读取；已授权 hook_feedback 的公开正文及受控语义来源可以投影，但不得带入其原始 stdout/stderr、原生 wire 或 Provider-private 字段。

#### Scenario: Keep hook text separate from opaque data
- **WHEN** 同一历史含明确 model 反馈、thinking/summary、签名、密文与未知扩展
- **THEN** Hook 语义只提供已授权正文与受控来源，opaque 数据仍只在 native 中

### Requirement: Projection returns an independent read-only snapshot

每次投影 SHALL 返回与会话内部 native history 以及其他投影结果不共享可变集合的独立快照。调用方修改或丢弃已返回视图 MUST NOT 改变后续投影、后续请求或其他会话的历史。

投影 SHALL 是无网络、文件、数据库和进程副作用的内存操作，并 MUST 与已提交历史的并发读取和 turn 提交保持数据竞争安全。

#### Scenario: Mutate a returned view

- **WHEN** 调用方修改一个已返回视图中的 turn 文本或 turn 列表
- **THEN** 再次投影仍返回未被修改的已提交语义，后续 Provider 请求也不受影响

#### Scenario: Project isolated conversations

- **WHEN** 同一个 Provider 创建两个具有不同已提交历史的 conversation
- **THEN** 两个投影只包含各自 conversation 的语义 turn，不共享或混合数据

#### Scenario: Project while another turn completes

- **WHEN** 历史投影与同一 conversation 的 turn 成功提交并发发生
- **THEN** 每次返回的视图都是提交前或提交后的完整快照，不出现部分 turn 或数据竞争

### Requirement: Semantic history cannot become a Provider request source

Provider RequestCompiler、原生 resume 和未来 Session 恢复 MUST 继续消费对应 Provider 的 native history，MUST NOT 从语义视图构造、补全或修复 Provider 请求。共享消费者 SHALL 只依赖语义历史契约，不得导入 Anthropic 或 OpenAI wire 类型来重新解释历史。

#### Scenario: Continue after reading semantic history

- **WHEN** 共享消费者读取语义视图后，会话提交下一条用户输入
- **THEN** Provider 仅使用自身已提交 native history 和新输入编译请求，不读取语义视图

#### Scenario: Keep shared consumers independent of Provider wire

- **WHEN** token、resume 渲染、Hook 或 Subagent 等共享消费者接入历史读取能力
- **THEN** 消费者只接收语义历史类型，不需要 Anthropic Messages 或 OpenAI Responses 原生类型

### Requirement: Active window projection is distinct from complete history

完整历史和 transcript 投影 SHALL 保持原始用户/assistant 消息、真实 turn/InputID、原生提交 ordinal 和工具锚点，不因压缩删除或重新编号。系统 SHALL 另提供有效窗口的只读语义快照供规划，包括带独立来源的摘要文本与 checkpoint 后可见消息。summary MUST NOT 伪造用户/assistant turn。所有投影只从 Provider-native facts 派生且不得反向构造请求。压缩摘要详情 SHALL 从 Provider checkpoint 只读投影产生，app 以 Session sequence 插入边界，不把压缩输出追加到普通 transcript native ordinal。

#### Scenario: Preserve complete history and expose a shorter window
- **WHEN** 一个含工具的会话完成手动压缩
- **THEN** 原始 ProjectHistory/Transcript 消息和锚点保持不变，有效窗口快照只包含摘要与之后增量
- **THEN** 两种投影均无 I/O、不暴露 opaque reasoning，修改返回副本不影响窗口

#### Scenario: Render a checkpoint summary
- **WHEN** 宿主显示压缩摘要
- **THEN** 使用独立压缩边界和摘要详情，不生成假的用户 InputID、turn 或 assistant completion

有效窗口 SHALL 在已安装原生摘要之后及其后的真实历史之前，保留本 checkpoint 已提交的 compact 上下文。真实 turn 内的 startup/resume 上下文按原生位置参与有效窗口。后续 checkpoint 覆盖的旧上下文退出有效窗口，完整历史仍保留真实来源与位置供读取。既有 /resume 预览继续只选择真实用户及 assistant 文本，SessionStart 上下文不占正文或条数预算。

#### Scenario: Project a compacted window after both sources
- **WHEN** 首轮普通自动压缩后依次保存本 checkpoint 的 compact 上下文、当次 resume 上下文和真实输入
- **THEN** 有效窗口保留摘要、compact 上下文、resume 上下文和真实输入的内容顺序。完整历史仍可投影旧事实

#### Scenario: Preview a history with saved hooks
- **WHEN** 离线预览含两种 SessionStart 作用域及真实用户和助手文本
- **THEN** 验证覆盖全部原生事实，预览只返回真实用户和助手文本，且正文预算不会被 Hook 内容占用

### Requirement: Stored native history supports offline semantic projection

每个受支持 Provider SHALL 提供不依赖连接凭据、base URL、transport 或活动 Runtime 的离线语义投影能力，接收强类型 thread/Provider/wire/model 来源与有序恢复记录，并支持对宿主提供的关联强类型压缩用量证据进行离线校验。离线投影与证据校验 SHALL 与正式恢复共享当前 native strict decoder、role/InputID 关联、历史顺序/tool pairing、checkpoint 去重/source/fingerprint 和用量证据验证规则；不得绕过未知 shape 检查或使用兼容 reader。

投影 SHALL 只进行可取消的内存计算，无文件、数据库、网络、进程副作用或隐藏后台任务；未完成完整验证不得返回部分成功视图。模型来源 SHALL 使用已验证的存储 metadata，不依赖当前连接模型。返回 SHALL 是独立的完整语义历史快照，保留现有可见文本/真实身份规则，不暴露可执行 Conversation、原生内部对象或 opaque 数据。

#### Scenario: Project both provider histories without connection configuration
- **WHEN** 调用方仅提供合法来源和当前恢复记录，没有任何 API key/base URL
- **THEN** 两家 Provider 均可返回与正常恢复后 ProjectHistory 相同的可见语义，网络/文件/进程操作为零

#### Scenario: Reject inconsistent native or compaction records
- **WHEN** 任一记录包含错误 role、InputID 关联、配对顺序、重复 checkpoint、错误 source 或 usage/provenance
- **THEN** 离线校验拒绝整份输入，其合法性判定与正式恢复的只读校验一致，不返回部分视图

#### Scenario: Cancel an offline projection
- **WHEN** 调用在接收前或逐条验证期间取消
- **THEN** 不返回成功视图，无外部副作用，已创建临时内存状态不成为共享历史

### Requirement: Offline projection preserves native continuation and cache semantics

引入离线投影后，正式恢复 SHALL 继续消费原生记录；语义视图不得成为 RequestCompiler、checkpoint 或工具补偿的反向输入。对同一有效历史执行零次或多次离线投影 SHALL 不改变后续 canonical request bytes、原生 item 顺序、内容/策略 fingerprint、opaque reasoning 或项目上下文来源。正常恢复的瞬态连接重置语义 SHALL 保持不变。

#### Scenario: Compare uninterrupted restored and previewed requests
- **WHEN** 两家 Provider 对含同轮输入、工具和压缩的相同历史分别直接续写、恢复续写、预览后恢复续写
- **THEN** 按既有恢复等价口径得到相同 canonical bytes、native 顺序与 fingerprint，预览不新增请求项或缓存失效原因

#### Scenario: Mutate a preview projection
- **WHEN** 消费者修改或丢弃离线返回视图后再次投影或正常恢复
- **THEN** 原始记录、其他视图、原生续写与 opaque 数据不受影响

### Requirement: Completion hooks project the exact committed assistant sample

每个 Provider SHALL 提供按真实 turn/sample identity 读取已提交 assistant 正文的单向、纯内存、只读投影，验证目标存在且返回独立副本。只能读取目标 sample 内的 assistant 可见 text，保持原字节语义和顺序，不依赖 phase、正文相等、工具 ready 或可见列表长度推测位置。目标不存在必须明确失败；目标无 text 返回合法空字符串，不回退前一次非空答复。投影不读取 staged 数据、不更改后续请求/指纹、不访问磁盘或网络。

#### Scenario: Read an empty committed sample
- **WHEN** 前一 sample 含文本而指定 sample 只有不可展示原生内容
- **THEN** 返回同一目标的空正文，不泄漏 thinking/summary 或复制前一 sample

#### Scenario: Preserve whitespace and duplicate text
- **WHEN** 两个不同 sample 正文相同且含前后空白
- **THEN** 每次读取仍按各自身份准确返回原正文，不按内容去重或 trim

#### Scenario: Reject a staged or wrong target
- **WHEN** 指定 sample 尚未提交、属于另一 turn 或不存在
- **THEN** 返回明确错误，无部分投影或原生历史变化

### Requirement: File projections keep original user text and independent attachments

两家 Provider SHALL 只从已验证且已提交的 native facts 投影有序强类型文件附件，关联原 InputID，保持原文、可选 Skill 与文件的来源区别。用户 Text 和 recall MUST 只包含原始正文，不能混入文件展开文本、推理或伪造工具/用户消息。完整 transcript SHALL 保留文件附件，有效窗口只保留 checkpoint 之后的实际内容，不能从文件重新填充被压缩的历史。

文件投影 SHALL 无 I/O，并与普通恢复和离线预览一致；返回的集合和嵌套值 MUST 与原生历史、其他视图独立，允许修改副本而不改变请求。投影、阅读和召回不得从旧正文/附件中的 effort 文字恢复进程选择或执行控制；只有再次明确提交后的正常命令/输入路径可以产生相应行为。

#### Scenario: Recall a file question without copying its body
- **WHEN** 输入 `请检查 @src/main.go#L1-20` 已提交并带文件附件
- **THEN** recall 仍为该原文，文件来源/实际范围/截断和内容单独可读，原 InputID 不变

#### Scenario: Preview after removing source files
- **WHEN** 无凭据的离线读取面对合法含文件历史，且来源不存在
- **THEN** 投影等于正常恢复后的投影，文件和网络读取次数为零

#### Scenario: Read a complete transcript after compaction
- **WHEN** 文件附件已经被成功 checkpoint 覆盖
- **THEN** 完整历史仍可阅读原附件，有效窗口不再次计入或重新读取这些文件

#### Scenario: Read control-looking content without restoring settings
- **WHEN** 附件或历史正文包含 /effort high，用户离线预览、恢复或在 Ctrl+O 中阅读
- **THEN** 不发送 effort 控制或恢复旧选择，当前操作策略不变；历史仍只提供原文及独立冻结附件

### Requirement: File transcript views use bounded summaries and frozen detail pages

实时已提交输入展示 SHALL 只携带独立强类型摘要，包含 total、items 和 omitted；items 仅为完整路径、选择、源/实际范围、部分末行/截断及实际包装字节数，不能反向构造完整附件。4 KiB SHALL 限制整个摘要已编码 JSON，包括路径转义和元数据；按原附件顺序保留可完整容纳的最长项前缀，遗漏数必须准确。单项过长允许 items 为空但 total/omitted 保持真实，不得把截短标签伪装成规范文件路径。

文件正文 MUST NOT 复制到每次状态事件、列表或用户 returned tray。主线 SHALL 紧凑显示附件存在、范围/部分行/截断及省略状态，Ctrl+O 详情 SHALL 通过已有有界分页和四页缓存读取全部已提交附件，不能按需重新打开源文件。控制字符转义和展示截短 MUST 只作用于副本。

实时与恢复后的摘要、来源和详情 SHALL 来自相同已提交事实，不能以 queued/prepared 或事件到达推定成功持久化。外部 headless JSONL MUST 保持其当前独立字段契约，不直接透传新的内部摘要字段。

#### Scenario: Inspect an attachment on a narrow terminal
- **WHEN** 已提交文件的路径很长且正文含终端控制字符，用户在 NO_COLOR 和窄屏下阅读详情
- **THEN** 显示有界转义和可辨识相对来源，分页正文与冻结内容一致，实际输入和请求不受显示影响

#### Scenario: Cancel before an attachment commit
- **WHEN** 附件仅 prepared/queued 而尚未 Sync 成功，用户查看已保存历史
- **THEN** 该附件不能被展示为已提交详情，也不能进入恢复历史或已提交文件摘要

#### Scenario: Omit an overlong summary item without altering its path
- **WHEN** 多个长路径或 JSON 转义使完整摘要超出 4 KiB，或首项本身无法容纳
- **THEN** 编码后摘要保持限额及真实 total/omitted，只有完整项前缀可见；Ctrl+O 仍能逐字读取全部相对路径及附件

#### Scenario: Share one summary across live and restored history
- **WHEN** 相同冻结附件分别通过实时提交事件和恢复后列表展示
- **THEN** 两者使用相同项选择、计数和范围/部分行语义，不因恢复或显示宽度重写实际附件
