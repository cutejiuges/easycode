## ADDED Requirements

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
