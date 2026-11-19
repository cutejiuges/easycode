## MODIFIED Requirements

### Requirement: Discovery and metadata search have explicit bounds

每次成功加载 SHALL 使用既有 Catalog 协调与查询语义，按 committed 更新时间和 canonical thread ID 双降序返回当前范围最多 200 条。当前目录 SHALL 精确匹配规范化 creation cwd，不包含子目录或兄弟 worktree。搜索 SHALL 只覆盖已加载条目的显式标题、thread ID、Provider family、wire、model 和 creation cwd，按 Unicode 小写转换后的字面子串匹配，不解释正则，不搜索预览正文。标题 SHALL 独立匹配，再与既有元数据匹配取并集，跨标题与其他字段的词组不能形成新匹配。

搜索输入 SHALL 限制为 256 个 Unicode code point；decoded paste 的 CR/LF/Tab SHALL 转为空格，超过限制的编辑整体拒绝且不丢失原查询。退格 SHALL 按字素边界删除。UI SHALL 区分空目录、没有匹配项和加载失败；有额外条目时 MUST 明确搜索仅覆盖最近 200 条。协调的 busy/invalid 提示 MUST 标明全存储范围、可能陈旧或遗漏，不能声称每个显示条目当前可恢复。键入搜索 MUST NOT 触发 journal 扫描。

#### Scenario: Search a bounded mixed-provider list
- **WHEN** 当前目录有超过 200 条、包含两家 Provider 的会话，用户输入模型名
- **THEN** 仅在最近 200 条中筛选，不自动隐藏其他 Provider，也不搜索正文，界面说明搜索边界

#### Scenario: Distinguish no matches from incomplete discovery
- **WHEN** 搜索没有结果，且协调报告 busy/invalid 或存在更多未加载条目
- **THEN** 界面说明已加载范围无匹配，保留不完整提示，不宣称存储中不存在该会话

#### Scenario: Reject excessive pasted search text
- **WHEN** 一次粘贴经空白规范化后使查询超过 256 个 code point
- **THEN** 原查询、选择和列表不变，界面显示安全的输入上限提示

#### Scenario: Search by title without changing scope or identity
- **WHEN** 当前范围的已加载条目包含标题大小写不同的同名会话，范围外或最近 200 条之外也有同名会话
- **THEN** 标题查询只显示范围内且已加载的匹配条目，选择仍按各自的完整 thread ID；原有元数据也仍可搜索

#### Scenario: Avoid matching across the title boundary

- **WHEN** 查询只匹配标题末尾与另一个元数据字段开头拼成的字符串
- **THEN** 该条目不因拼接命中，查询完整标题或原元数据仍按各自规则返回

### Requirement: Keyboard routing preserves explicit selection and cancellation

界面 SHALL 支持上下键、PageUp/PageDown、输入搜索、Ctrl+A 切换范围、Ctrl+R 刷新、Ctrl+V 预览及 Enter 请求恢复。更新列表 SHALL 按 thread ID 保持仍存在的选择，无法保留时选择首项；不得通过旧索引确定恢复目标。没有候选时 Enter/Ctrl+V 不产生操作。

Esc SHALL 优先关闭预览或跨目录确认，再清空非空搜索，最后取消启动并在清理成功后返回 0。Ctrl+C/信号取消 SHALL 在清理完成后返回 130；清理失败返回 1。加载中取消 MUST 等待清理，不能创建替代 Session。进入正式恢复后重复 Enter MUST NOT 发起第二次恢复，普通输入 MUST NOT 成为聊天 prompt。

#### Scenario: Return from preview without losing the selection
- **WHEN** 用户打开预览后按 Esc
- **THEN** 返回原列表并保留查询和所选 thread，预览正文被丢弃

#### Scenario: Clear search and then exit
- **WHEN** 用户在列表非空搜索中按 Esc，再按 Esc
- **THEN** 第一次清空搜索，第二次取消启动；没有新 Session 或自动模型请求

#### Scenario: Ignore repeated confirmation while resuming
- **WHEN** 正式恢复已被接收，用户继续按 Enter 或输入字符
- **THEN** 只存在一个恢复请求，没有自动提交的聊天输入

#### Scenario: Refresh named sessions without losing the selected identity
- **WHEN** Ctrl+R 刷新成功，同一所选 thread 仍匹配当前搜索，但标题或列表位置发生变化
- **THEN** 展示本次加载的标题，保留当前查询与按完整 thread ID 绑定的选择

### Requirement: Display is bounded terminal-safe and identity-preserving

列表 SHALL 优先使用显式标题辨识已命名的条目，并显示更新时间、Provider/model。无标题的条目 SHALL 沿用现有元数据展示。界面 SHALL 在选择详情中提供完整 thread ID、wire、创建目录和实际恢复目录。时间 SHALL 使用当前机器本地时区的固定日期时间并显示时区。配置三元组不匹配 SHALL 显示具体差异，不能自动换模型或隐藏历史。

model/wire 的转义展示 SHALL 限制为 256 个 code point，目录为 512 个；元数据的换行、Tab、ESC、C0/C1、DEL、Unicode 行/段分隔符及双向控制符 MUST 可见转义。显式标题 SHALL 遵守 session/titles 的 256 UTF-8 bytes 上限，以通过校验的纯文本展示并保留合法字素，列表按列宽截断，详情可滚动查看完整标题。预览 SHALL 使用从所选 journal 校验出的标题，与其他预览内容一同滚动，标题独立于正文展示预算。预览正文 SHALL 保留可见换行和制表布局，其他终端控制符必须转义，并遵循预览展示预算。窄屏、resize 和无色模式 MUST 保持选择与查询不变、无负尺寸，并能辨认当前选择；完整身份可换行/滚动，不得使用缩写身份发起操作。render MUST 无 I/O。

#### Scenario: Render hostile metadata and narrow screens
- **WHEN** model/cwd 含终端控制字符、中文、emoji 或超长文本，且终端被缩窄
- **THEN** 输出无可执行控制序列，展示有界，所选身份不变且详情可查看完整 ID

#### Scenario: Show compatibility without claiming availability
- **WHEN** 候选的 family、wire 或 model 与当前配置不匹配
- **THEN** 界面显示不匹配字段，仍允许符合 session/preview 契约的只读预览，不自动转换历史

#### Scenario: Preview the title from the selected journal
- **WHEN** 同一 thread 的缓存列表标题与已验证 journal 标题不同，或者预览正文达到展示上限
- **THEN** 预览显示其 journal 标题（无显式标题时无标题字段），标题独立于正文限额；列表查询和当前选择仍按原有规则保留

#### Scenario: Keep a long Unicode title and its identity accessible

- **WHEN** 已命名条目标题含 ZWJ emoji 且超出当前列宽，用户缩小终端并打开详情
- **THEN** 列表裁剪不拆开字素，详情显示完整合法标题与 thread ID，所选会话仍按该 ID 关联
