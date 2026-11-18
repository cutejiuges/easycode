# context/file-mentions Specification

## Purpose

定义用户如何在交互式正文中明确引用工作区文件，以及系统如何安全、有界地读取并冻结这些文件。文件附件与用户原文、工具授权和项目指令分别保留，使排队、恢复、上下文计量与历史阅读使用一致事实。

## Requirements

### Requirement: Interactive file mentions use explicit deterministic syntax

交互式文件引用 SHALL 支持行首或 Unicode 空白后的 `@path`、`@path#L10`、`@path#L10-20` 和 `@"path with spaces"#L2-5`。引号路径 MUST 使用严格 JSON 字符串语法，范围位于结束引号之后；未引号路径以空白结束，含空白或 # 的路径必须加引号。引用 SHALL 只表达当前工具工作区内规范相对文件路径，允许规范化前导 ./，拒绝绝对/drive/UNC 路径、~ 展开、反斜杠路径、任意 .. 或空组件、控制字符及超过 4096 bytes 的路径。行范围 MUST 为无溢出的正整数闭区间，起点不大于终点；非法 fragment 或未闭合引号 MUST 显式失败，不能猜测其他文件。

系统 SHALL 保留原文，并按第一次出现的顺序对相同路径和选择去重；同路径不同范围保持独立，最多 8 个不同引用。email 内的 @、@@、转义 sigil、行内代码及反引号/波浪线代码围栏中的引用 SHALL 保持文字；引号路径中的反引号/# 必须作为路径数据处理。Unicode 转义 MUST 拒绝未配对 surrogate，不得替换为另一条路径。本地命令先按原有命令语义处理；普通 headless/stream-json 文本 MUST 不因 @ 隐式读取文件。

任何附件来源 I/O 前 MUST 验证整条原文、完整引用集合、数量及静态额度；交互草稿携带的引用必须与原文的完整有序解析结果相等，不能夹带未提及路径。

引用解析 SHALL 只消费本次真实用户原文，不递归扫描 Skill/文件正文、Hook 反馈、模型输出或历史详情；不能把展开内容拼接为新的用户原文后发现引用。本地 slash 参数，包括 /compact instructions 和 /effort 的合法/非法参数，MUST 保持原有控制语义且不触发文件候选发现、规则或附件读取；精确内置首词复用现有 domain 保留名判断。合法 $effort Skill 的正文仍可明确附带文件，不被内置 slash 误拦截。

#### Scenario: Attach quoted paths and line ranges in order
- **WHEN** 用户提交含普通路径、引号路径和重复同范围引用的交互式正文
- **THEN** 原文逐字保留，唯一引用按首次出现顺序准备，行范围准确且重复项不重复加载

#### Scenario: Preserve literal and code text
- **WHEN** 正文包含 email、@@、转义 sigil、行内或围栏代码，或相同 @ 文本通过普通 headless 输入
- **THEN** 对这些文字的文件读取次数为零，文字不会被猜测为文件授权

#### Scenario: Reject invalid explicit references atomically
- **WHEN** 正式提交含未闭合引号、非法/逆序/溢出范围、越界路径或第九个不同引用
- **THEN** 整条输入以安全英文错误拒绝，无 admission、Session 写入或新 Provider 请求，原草稿和光标保持

#### Scenario: Validate all references before reading the first source
- **WHEN** 前面的引用合法但正文末尾存在非法引用或多余的第九项
- **THEN** 全部附件来源读取次数为零，不先读取合法前缀

#### Scenario: Reject malformed Unicode without changing the target
- **WHEN** 引号路径包含未配对 surrogate 或非法 JSON 转义
- **THEN** 准备失败且不访问替换字符路径；合法引号路径内的反引号与 # 保持原值

#### Scenario: Do not recursively activate references from attached content
- **WHEN** 明确附带的文件、Skill 展开或 Hook 反馈正文包含另一个 @ 路径
- **THEN** 后者保持数据，不产生额外引用或来源读取；/compact 参数中的同样文字也不读取文件

### Requirement: File preparation remains bound to the existing workspace capability

文件准备 SHALL 只读取当前工具工作区 capability 内的普通 UTF-8 文件，拒绝 NUL 内容、symlink、目录、设备、FIFO、socket 和不可读对象。每级路径、实际文件类型/权限/大小与读取对象 MUST 绑定同一安全打开的描述符；不能以单独的 stat/Lstat 后普通 Open 代替。读前后任一文件检查失败、身份/属性变化或实际读取量不等于最终大小 MUST 拒绝整条准备，不能降为缺少证据后仍交付正文。无法提供等价安全语义的平台 MUST 明确失败关闭。

候选路径或历史 creation_cwd MUST NOT 扩大读取根；搜索结果不构成读取授权。准备错误 MUST 使用稳定英文类别，不回显底层错误、绝对路径或正文。任一附件失败时 MUST 不接收其余部分附件。

#### Scenario: Replace a path with a symlink during preparation
- **WHEN** 路径组件在打开边界被确定性替换为项目内外 symlink
- **THEN** 准备失败或继续使用此前已验证的同一描述符，替换目标不进入附件

#### Scenario: Detect a changing regular file
- **WHEN** 文件在读取期间变化且读前后校验不一致
- **THEN** 整条输入拒绝且不提交此前已准备的其他附件，不把半份内容表示为完整快照

#### Scenario: Fail an unavailable descriptor check
- **WHEN** 任一读前/读后描述符检查失败或最终大小与实际读取不一致
- **THEN** 不返回可用附件，即使已读取的正文是合法 UTF-8，也不把检查失败等同于文件未改变

#### Scenario: Keep a resumed workspace independent of historical paths
- **WHEN** 已存会话在另一个启动工作区恢复后提交新的文件引用
- **THEN** 仅当前工作区可被读取，历史根和旧附件不扩大权限

### Requirement: File attachments obey source and rendered-content limits

每条输入的文件准备 SHALL 至多读取 64 MiB 源 bytes，单源上限 16 MiB；相同路径的多个范围 SHALL 在同次准备中复用同一安全打开对象且只读一次。失败或取消前实际读取也 MUST 计入本次源额度。每个附件的完整包装文本 MUST 不超过 64 KiB，全部附件不超过 512 KiB，元数据与包装开销在限额内预留。

whole 选择 SHALL 默认保留前 200 行，显式范围最多保留 2000 行，并同时受字节上限约束。截断 MUST 位于有效 UTF-8 边界且不切开 CRLF，保存源总行数、实际起止行、末行是否部分保留和 truncated；部分末行必须同时标记 ends_mid_line 与 truncated，不能静默表示为完整行/文件。空文件 SHALL 合法，源与实际范围均为零；范围起点超过 EOF MUST 失败，终点超过 EOF 取实际结尾。不足以容纳可用正文时 SHALL 返回大小错误。

行 SHALL 以原始 LF 分界，结尾 LF 不额外产生空行；选中内容保留这些行原有的 CRLF/LF、裸 CR、BOM 和末尾是否有换行，不得 split/join 后规范化。源缓冲及选中片段保留 MUST 有界，片段不得长期持有整份大源缓冲。64 KiB/512 KiB MUST 计入最终包装的 JSON 转义膨胀，选择前缀的计算不能随正文长度平方增长。

#### Scenario: Freeze a truncated long file
- **WHEN** whole 文件超过默认行数或包装字节上限
- **THEN** 只冻结合法 UTF-8 前缀，实际范围及 truncated 与模型实际获得的内容一致

#### Scenario: Read an empty file and an unavailable range
- **WHEN** 分别引用空文件整体及某文件 EOF 之后的起点
- **THEN** 前者得到合法空附件，后者原子拒绝，二者不能混为读取失败或完整非空内容

#### Scenario: Account for repeated-path ranges and failed reads
- **WHEN** 同一路径出现不同范围，或先读取的来源消耗预算后后项失败
- **THEN** 同路径只读一次，已发生读取不能重置额度以绕过 64 MiB 上限

#### Scenario: Preserve original line endings and EOF semantics
- **WHEN** 文件分别包含 CRLF、BOM、裸 CR、空行，或以 LF 结束/没有最后换行
- **THEN** 附件为请求行跨度的原始字节片段，结尾 LF 不伪造新行，未截断时保留全部所选字节

#### Scenario: Budget escaped content and identify a partial last line
- **WHEN** 长行包含大量双引号/反斜杠/控制字符，JSON 包装达到限额
- **THEN** 最终包装不超限且有限复杂度计算前缀，部分末行与源总行数如实保存，截断不切开 CRLF/UTF-8

### Requirement: Frozen file content remains user data without granting tool authority

文件附件 SHALL 用包含相对路径、请求选择、源总行数、实际范围、部分末行/截断标记及内容的确定性数据包装保存；内容派生 digest MUST 校验全部冻结字段，范围与内容还须独立满足语义不变量，digest 不是来源签名。包装 MUST 不受正文中伪造分隔符影响，不含绝对根、mtime、inode、随机 ID 或输入身份。文件内容 SHALL 保持用户级数据，不成为项目指令、system/developer 消息、Skill 激活或工具调用。

准备与提交附件 MUST NOT 生成 Read observation、mutation grant、Hook 信任、artifact 授权或额外工具 ledger，也不得从正文中的 /effort 或其他控制文字改变进程设置。相同规范引用和冻结内容 MUST 得到相同包装与 digest；effort、选择来源和进程序号不得进入包装、附件元数据或 digest。

#### Scenario: Include hostile delimiters as file data
- **WHEN** 文件含看似关闭附件或追加系统指令的文本
- **THEN** 全部文本仍在同一数据包装中，不创建更高权限消息或新的语义来源

#### Scenario: Preserve Write eligibility checks
- **WHEN** 模型仅通过文件附件看到一个现有文件，随后尝试整文件覆盖
- **THEN** 覆盖仍必须满足既有完整 Read 证据与普通审批，附件不自动满足这些条件

#### Scenario: Keep control-looking file text as data
- **WHEN** 已明确附带文件的正文包含 /effort high、effort.set 或另一个 @ 路径
- **THEN** 不改变当前选择、捕获策略或序号，不递归读取来源；相同冻结内容在不同 effort 下仍有相同包装和 digest

### Requirement: Search and pre-admission reads have an explicit cleanup owner

宿主 SHALL 明确启动并持有文件来源，限制为一个活动内容准备、一个活动文件候选 session 和一个最新待处理查询。启动失败 MUST 同步回滚，重复启动不得创建第二个 worker；未启动/重复关闭幂等。过量准备必须明确 busy 拒绝而非隐藏排队。关门与请求借用 MUST 原子决定接收或拒绝；关闭先停止接收并取消在途操作，再等待读取栈、索引/搜索和句柄清理的最终完成信号，之后才释放工作区和相关 Session 资源。pre-admission 读取 MUST 同样受此所有权约束。超时或强制 Close 不能自动证明 I/O 已返回，不能发出虚假完成或放弃资源。

构造、getter、序列化和 render MUST 无外部 I/O、无隐藏 goroutine；锁内只能进行有界内存转换，不能执行读取/Close/回调或阻塞等待。不得为每次查询或取消创建不受控的长期等待 goroutine。

#### Scenario: Close while an attachment is still being prepared
- **WHEN** 准备尚未 admission，宿主请求关闭且测试屏障暂时阻止读取退出
- **THEN** 工作区与 lease 所有权保留，只有读取和清理最终完成后才报告关闭完成

#### Scenario: Replace a pending query during shutdown
- **WHEN** 新查询与来源关闭竞争
- **THEN** 关闭门之后不接收新查询，活动和待处理查询均有确定取消结果且不遗留 worker

#### Scenario: Roll back startup and reject excess preparations
- **WHEN** 来源启动失败，或已有内容准备期间收到第二份内容准备
- **THEN** 启动不泄漏后台任务/句柄，过量准备立即确定拒绝；之后关闭仍可等待真实完成且不重复清理
