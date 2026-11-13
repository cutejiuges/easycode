## Purpose

定义用户显式选择的本地插件如何以受限、可诊断且确定性的方式贡献带命名空间的技能，使一组工作流能够在交互与非交互入口复用，并在来源隔离、权限、缓存和原生恢复方面保持现有保证。

## ADDED Requirements

### Requirement: Plugin directories are explicit bounded session inputs

系统 SHALL 支持可重复的 `--plugin-dir PATH`，覆盖普通 TUI、print、json、stream-json、`--resume`、`--continue` 与 `easycode resume`。每次进程最多接受 32 项，每项最多 4096 UTF-8 bytes；空参数、非法 UTF-8 或超限 SHALL 在资源创建前以安全英文 usage error 和 exit 2 拒绝。相对路径 SHALL 以启动时捕获的 cwd 解析，不因 picker 选中的历史工作目录改变，不展开环境变量、波浪号、glob 或远端 URL。

只有本次明确提供的目录 SHALL 成为插件来源；系统 MUST NOT 自动扫描项目或用户插件缓存、安装/复制插件、持久化启用路径或从历史中重新启用插件。`--version`、`--help`、离线 sessions list、picker 预览与取消 MUST NOT 加载插件。文件不存在或来源无效 SHALL 作为隔离诊断，保留普通聊天和历史恢复能力。usage 错误、帮助/默认值展示及诊断 MUST NOT 回显插件参数原值、宿主绝对路径或非法字节；直接调用应用入口的参数也必须接受相同边界校验。

#### Scenario: Reuse an explicit plugin across entry points
- **WHEN** 用户在 TUI、print、stream-json 或恢复入口传入相同合法目录
- **THEN** 使用相同来源规则和技能快照，没有入口专用的隐式安装或扫描

#### Scenario: Keep the launch directory binding during picker recovery
- **WHEN** 用户以相对 plugin-dir 启动 picker 并选择另一工作目录的会话
- **THEN** 插件路径仍相对于启动 cwd 解析，历史会话目录不重新解释该参数

#### Scenario: Reject invalid argument limits before opening a session
- **WHEN** 参数为空、超过 32 项或单项超过限制
- **THEN** 返回 usage error，未创建或修复 Session，未读取插件，未访问 Provider

#### Scenario: Reject private invalid values without echoing them
- **WHEN** plugin-dir 值含超长绝对路径、非法 UTF-8 或其他无效内容
- **THEN** 在资源创建前返回安全 usage code，stdout/stderr/帮助内容不含原参数值或其转义副本

#### Scenario: Leave offline discovery independent
- **WHEN** 用户运行 version/help/sessions list，或取消 picker
- **THEN** 插件读取次数为零，不改变 journal 或插件文件

### Requirement: Manifest selection fails closed

每个明确插件目录 SHALL 按 `.easycode-plugin/plugin.json`、根 `plugin.json`、`.codex-plugin/plugin.json`、`.claude-plugin/plugin.json` 的固定优先级选择首个存在项。存在项不是安全常规文件、不可读、损坏或不受支持时 MUST 阻断该插件；不得回退、合并另一 manifest 或从目录名生成默认 manifest。全部缺失 SHALL 报告 `plugin_manifest_missing`。

根 plugin.json SHALL 仅接受 `$schema` 精确为 `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json` 的 Agent Plugins 格式；其他值或缺失 schema MUST 报告 `plugin_manifest_unsupported`，不再尝试较低优先级文件。schema URI 仅用于外部格式识别，MUST NOT 触发网络访问或内部 Session/native 版本门禁。

系统 SHALL 在完整有界 JSON 无歧义校验后保留合法声明 ID，即使其他字段使该插件不受支持，该 ID 仍参与重复插件冲突判定。不能从损坏或重复 name 的 JSON 猜测 ID；无法无歧义取得名称的来源仅有参数序号诊断。

#### Scenario: Select one manifest deterministically
- **WHEN** 同一插件同时包含合法 EasyCode 与 Claude manifest
- **THEN** 只采用 EasyCode manifest，枚举顺序不改变贡献

#### Scenario: Block a corrupt or unsafe preferred manifest
- **WHEN** 高优先级 manifest 损坏、被替换为 symlink 或不可读，较低项合法
- **THEN** 插件禁用，未读取低优先级贡献，也不合成替代名称

#### Scenario: Recognize the current reference root format locally
- **WHEN** 根 plugin.json 携带受支持 schema
- **THEN** 按 Agent Plugins 技能子集导入，网络请求次数为零

#### Scenario: Refuse an unrelated root manifest
- **WHEN** 根 plugin.json 不属于受支持 schema，同时存在 Codex 或 Claude manifest
- **THEN** 报告不支持并阻断插件，不静默选用另一格式

### Requirement: Importers accept an explicit skills only subset

四种格式 SHALL 只有一套当前内部技能贡献语义。manifest MUST 是不超过 64 KiB、嵌套不超过 16 层的合法无歧义 UTF-8 JSON 对象；所有层级重复成员、null、错误类型、尾随内容、未知字段和有损 Unicode MUST 拒绝。

必需 name SHALL 为 1–64 bytes 的小写 ASCII 字母/数字/单连字符 slug，首尾为字母或数字，不自动重命名。共有可选字段 SHALL 仅包括 version、description、author、homepage、repository、license、keywords。version 非空且最多 128 bytes；description 最多 1024 bytes；author 仅允许 name/email/url 字符串；其他字符串最多 2048 bytes；keywords 最多 32 项、每项最多 64 bytes。metadata 中的地址 MUST NOT 被访问，version MUST NOT 用作内部兼容选择。

EasyCode、Codex JSON 和 Claude JSON SHALL 支持可选 skills 字符串或 1–8 项非空字符串数组：缺省使用 `./skills`，显式值替代默认根。Agent Plugins SHALL 只使用约定 `./skills`，不接受顶层 skills；extensions 仅允许缺省或空对象。系统 MUST NOT 对整个外部生态宣称完整兼容。

显式 hooks、mcpServers/mcp_servers、agents、commands、outputStyles、lspServers、apps 或非空 extensions SHALL 整体禁用插件并报告 `plugin_unsupported_contribution`，不能只丢弃这些声明而发布成功贡献。其他未知字段 SHALL 报告 `plugin_unsupported_field`。未声明的其他目录不得被扫描或执行，插件展示 SHALL 明示 `Skills only`。

#### Scenario: Import equivalent minimal skill packages
- **WHEN** 四种格式分别以合法 metadata 描述同一类技能包
- **THEN** 都能形成具名的技能贡献，并保留可解释的 importer 来源身份

#### Scenario: Use explicit paths without implicit defaults
- **WHEN** 非 Agent Plugins manifest 显式声明 custom roots，同时默认 skills 目录也存在
- **THEN** 仅发现显式 roots，默认目录不会被额外并入

#### Scenario: Reject unsupported behavior even when empty
- **WHEN** manifest 声明 hooks、commands、MCP 或其他未支持行为，值为空或非空
- **THEN** 整个插件不可用，相关目标文件未被读取，其他插件和本地技能仍可用

#### Scenario: Reject ambiguous or excessive JSON
- **WHEN** manifest 包含嵌套重复键、null、非法代理字符、尾随 JSON 或超深/超长内容
- **THEN** 在发布贡献前拒绝，错误只含稳定英文 code，不回显文件内容

### Requirement: Skill roots and namespaces resolve deterministically

manifest root SHALL 以 `./` 开头，其余部分为最多 4096 bytes、最多 128 组件的规范相对路径；MUST 拒绝空值、`.`/`..`、绝对路径、反斜杠、盘符、NUL、重复分隔符和非规范 clean 结果。roots SHALL 按规范相对路径排序、去重；不得执行 glob 或递归扫描。

EasyCode/Claude/Codex JSON 技能根存在 SKILL.md 时 SHALL 作为单技能根，非法直接主文档 MUST NOT 回退扫描；否则只发现直接子目录的 SKILL.md，本轮不递归更深层。EasyCode/Claude 的 leaf SHALL 使用实际 Skill 目录 basename，显式 frontmatter.name 必须相同；Codex 的 leaf SHALL 使用合法 frontmatter.name，缺省取该 basename。Agent Plugins SHALL 只接受约定 skills 根的直接子目录，leaf 使用 Codex 规则；若 skills/SKILL.md 本身存在则整体报告 plugin_unsupported_layout，不视为 empty 或单技能根。其他主文档/调用策略沿用 extension/local-skills 的严格子集。

默认 skills 目录缺失 SHALL 产生 empty 插件；显式 roots 任一缺失或不可读 SHALL 使整个插件 unavailable，不发布遗漏根的部分快照。

插件技能 SHALL 只以 `plugin:leaf` 提供，恰好一层冒号，每段最多 64 bytes，总长最多 129 bytes。不得为插件技能创建裸名别名，不得改变已有 local 裸名的来源优先级。

#### Scenario: Discover a collection and a direct skill root
- **WHEN** 合法插件分别提供 skills/review/SKILL.md 或 custom-root/SKILL.md
- **THEN** 按各自确定 leaf 规则发现限定名，只把 metadata 加入目录，正文按需加载

#### Scenario: Preserve importer specific reference names
- **WHEN** Skill 目录名与 frontmatter.name 不同，分别通过 Claude 和 Codex importer 加载
- **THEN** Claude 按本轮严格目录名子集拒绝不匹配项而不改调用名，Codex 保留合法 frontmatter 引用名；集合根和单技能根的命名结果一致

#### Scenario: Reject a direct root in Agent Plugins format
- **WHEN** Agent Plugins 的 skills/SKILL.md 存在，同时或另有 skills/review/SKILL.md
- **THEN** 插件报告不支持的布局，不选择任意一个、不递归回退、不展示为合法空插件

#### Scenario: Do not fall back from a broken direct document
- **WHEN** root/SKILL.md 存在但非法，root 下另有合法子目录技能
- **THEN** 该 root 不通过集合扫描绕过错误

#### Scenario: Keep bare and qualified names independent
- **WHEN** 本地存在 review，qa 和 docs 插件也各有 review
- **THEN** 三项分别为 review、qa:review、docs:review，不产生隐式遮蔽

#### Scenario: Reject one missing declared root atomically
- **WHEN** 一个插件的多个显式 roots 中有一个不可读或不存在
- **THEN** 整插件 unavailable，其他 roots 不单独发布为完整成功插件

### Requirement: Identity conflicts never select an implicit winner

同一实际目录重复传入 SHALL 合并一次，文件身份只用于本机去重，不得进入公开来源或 fingerprint。不同实际目录声明同一插件 ID SHALL 全部禁用该 ID，并报告 `plugin_name_conflict`，不依据 CLI 顺序、文件时间或绝对路径选胜者。

只有实际 Skill 目录身份与主文档文件身份都相同的重复发现 SHALL 合并一次；相同主文档的硬链接位于不同目录时 MUST 保留不同资源根绑定，不得仅按文件身份合并。不同绑定产生同一限定名 SHALL 全部禁用该名，并报告 `plugin_skill_conflict`。可确定 leaf 的非法候选也必须阻止同名回退。其他合法限定名仍可用，并展示 partial 状态。

#### Scenario: Ignore duplicate spelling of the same directory
- **WHEN** 同一实际目录以重复或规范化等价参数提供
- **THEN** 只产生一份插件/技能贡献和一组模型目录条目

#### Scenario: Reject duplicate plugin identities in either order
- **WHEN** 两个不同目录声明 qa，参数顺序分别交换
- **THEN** 两种顺序均禁用 qa 全部贡献，不选择任意目录作为来源

#### Scenario: Reserve an unambiguous ID from an unsupported manifest
- **WHEN** 两个不同目录均无歧义声明 qa，其中一个又声明了未支持行为
- **THEN** qa 的全部贡献因插件 ID 冲突禁用，不因先排除非法来源而选用另一个 qa

#### Scenario: Do not merge distinct resource roots through a hardlink
- **WHEN** 两个 Skill 目录的主文档为同一硬链接文件，但各有不同的 references 内容
- **THEN** 两个绑定不去重，同一限定名冲突时全部禁用，不任意选择一份资源

#### Scenario: Isolate a duplicate leaf
- **WHEN** 一个插件中不同 roots 各提供 review，另有唯一 lint
- **THEN** qa:review 不可调用，qa:lint 保持可用，列表解释冲突

### Requirement: Source checks and reads stay bound to actual descriptors

发现、manifest、主文档及资源读取 SHALL 逐段拒绝 symlink、特殊文件和越界访问，检查类型、目录身份、权限策略和读取必须绑定实际打开的 FD。检查一个路径后重新使用未绑定路径读取 MUST NOT 视为安全保证。文件在打开边界被替换为 symlink/特殊文件、脱离已验证范围或使冻结绑定不匹配时 SHALL 确定失败，不能读取违规替代目标；已打开的合法 FD 不得因之后路径改名而切换到另一文件。合法资源正文在新请求时可按既有规则变化，不要求冻结全部未读取资源。

插件/技能读取根与私有 Session/artifact 存储 MUST 不相同且互不包含；此关系 SHALL 由实际已打开目录身份与祖先关系判断，不以字符串前缀替代。来源检查后 MUST 从同一已验证的实际 Skill 目录 FD 完成资源相对读取，不得把整个插件目录当作该 Skill 的资源根，不能访问兄弟技能或私有存储。缺少等价安全 opener 的平台 SHALL 报告不支持，不回退普通文件读取。

#### Scenario: Reject a replacement at each open boundary
- **WHEN** 确定性注入将 plugin root、manifest、SKILL.md 或资源替换为 symlink/特殊文件
- **THEN** 不读取目标正文，返回安全错误，所有 owned FD 最终关闭

#### Scenario: Refuse private storage overlap
- **WHEN** 插件或技能读取根与 Session/artifact 目录相同、位于其内或包含它
- **THEN** 该来源被拒绝，模型不能通过技能资源读取私有 journal 或输出

#### Scenario: Fail safely on unsupported platforms
- **WHEN** 平台没有可实现相同安全语义的 opener
- **THEN** 插件标记 unavailable，不跟随链接或放宽文件类型检查

### Requirement: Plugin snapshots are frozen bounded and failure isolated

系统 SHALL 在旧事实恢复与 reconciliation 完成后，为未来调用一次性冻结插件与 Skill 目录，并在任何用户工作启动或未来 Provider 请求前安装完成。取消 SHALL 不发布部分候选，来源加载不启动隐藏 goroutine。列表、补全、模型 metadata 与正文解析 SHALL 使用同一来源快照，返回值不得共享调用方可变状态。

每插件 SHALL 最多 8 个 roots、256 个技能候选，全部插件最多 1024 个技能候选。每 root 原始目录枚举最多 1024 项，每插件累计最多 2048 项、全部插件最多 8192 项；普通文件、非法名称、无主文档目录和重复项均计入原始枚举额度。读取 SHALL 在上限加一时检测超限，禁止先读取无限列表再截断。启动主文档与适用调用策略副文件合计最多 16 MiB；无效候选和副文件也计费。诊断最多 256 项，溢出保留计数与安全 code。单技能沿用 64 KiB 主文档/资源、16 KiB 参数、128 KiB 包装正文；本地与插件合计最多 256 个可用技能，模型 metadata 最多 64 KiB。

系统 SHALL 先保留已有本地合法获胜项，再按插件 ID ASCII 顺序原子纳入每插件合法集合；使合并数量/目录 bytes 超限的插件 SHALL 整体 `plugin_catalog_limit`，不得挤掉本地项或截取任意前 N 项。根级/插件级超限回滚当前插件，受全局额度影响的剩余插件明确 unavailable；普通聊天、其他已合法来源和旧历史保持可用。无热加载或自动旧快照回退。

系统 SHALL 先完成全部 manifest ID/冲突判定，再按插件 ID 和规范相对路径消费技能扫描额度；最终插件状态、可用计数与诊断 SHALL 在策略、冲突和全量纳入预算确定后同时发布，不能用 manifest 初步成功状态替代最终目录状态。

#### Scenario: Share one frozen view
- **WHEN** 插件发现完成后磁盘新增一个技能，用户浏览插件、技能与补全
- **THEN** 三处都只显示已冻结条目，不各自重新扫描或扩展模型目录

#### Scenario: Preserve local skills when a plugin exceeds catalog limits
- **WHEN** 某插件的完整合法集合会使合并上限超出
- **THEN** 整插件拒绝，本地与此前已合法插件保留，结果不依赖文件枚举顺序

#### Scenario: Bound unrelated directory entries
- **WHEN** 来源包含超过原始枚举上限的大量普通文件、非法名称或没有 SKILL.md 的目录
- **THEN** 有界检测并拒绝该来源，不无限扫描或错误发布 empty；其他已合法来源不被重新选择

#### Scenario: Publish counts after final catalog admission
- **WHEN** manifest 合法但有效策略、名称冲突或合并目录额度导致技能未被纳入
- **THEN** 插件面板、技能列表、补全与模型目录同时反映最终结果，没有过期 available 状态或错误计数

#### Scenario: Cancel loading without a partial publication
- **WHEN** 加载在来源读取中被取消
- **THEN** 不启动 Provider 工作、不发布半份快照，打开资源完成清理

#### Scenario: Recover before inspecting future plugin sources
- **WHEN** 已有中断 turn 需要离线补偿，同时新 plugin-dir 无效
- **THEN** 旧事实先按既有规则收口，新来源错误只限制未来插件调用

### Requirement: New reads validate the frozen binding without refreshing it

每次新的 Skill 主文档或资源请求 SHALL 验证原实际来源身份、选定 manifest 原始 bytes、主文档及适用副文件的存在性/内容；变化 SHALL 返回 `skill_source_changed`，不重新选择 importer/root 或回退其他技能。插件附件 revision SHALL 由规范化 manifest 贡献、实际 Skill 目录规范相对路径、leaf、完整主文档内容、副文件存在性/digest 与有效调用策略派生，不含宿主路径、设备身份或时间。主文档原始 digest 与附件复合 revision MUST 保持不同语义，不得用复合 revision 校验原始文件内容。

已 Prepare/queued 的附件和已 durable 的结果 SHALL 保留其冻结正文；排队、提交重试和恢复不得重新读文件或重复展开参数。资源读取仍受同来源、同 revision 和当前 turn 资格限制。

#### Scenario: Detect a changed manifest before a new read
- **WHEN** 启动后修改选定 manifest，即使 SKILL.md 未改
- **THEN** 新主文档或资源请求失败为 skill_source_changed，不重新发布目录

#### Scenario: Detect sidecar presence changes without hot reload
- **WHEN** 冻结时缺失的 Codex 副文件出现，或原有副文件删除/改写/变为 symlink
- **THEN** 新读取拒绝为 skill_source_changed，不更新本会话目录、重新授权或读取其他来源

#### Scenario: Keep queued instructions frozen
- **WHEN** 显式插件调用已 Prepare 后进入队列，随后文件变化
- **THEN** 已准备附件按原文提交，队列不会再次读取或展开来源

#### Scenario: Reject stale manual selection for new resources
- **WHEN** 资源请求所依赖的显式附件与当前插件 source/revision 不匹配
- **THEN** 不能使用 manual-only 的同 turn 例外，不切换其他来源

### Requirement: Plugin skills preserve authority and cache separation

插件贡献 SHALL 复用现有 Skill 输入、结果、ledger、并发与预算，而不是新增模型工具。显式目录仅允许读取指令与同来源资源，不授予写文件、进程、网络或 Hook 信任。加载与参数展开 MUST NOT 执行 shell、插件变量、环境引用、include 或插件脚本。user-invocable/disable-model-invocation SHALL 保持现有独立规则。

稳定模型目录 SHALL 只包含实际发送的 Skill metadata、限定名称和安全来源；插件正文、version、UI 状态/诊断、绝对路径、CLI 参数顺序和文件时间 MUST NOT 污染稳定前缀。metadata 变化仅使相应 Skill 目录内容指纹改变；未调用正文变化不改变 metadata 指纹，新调用实际发送的正文变化才改变对应历史尾部。仅 version、副文件 interface 或其他未发送注解变化即使改变私有来源/附件 revision，相同 Content 与有效 metadata 下也 MUST 保持请求 bytes 和历史内容指纹相同，不把 digest 或私有绑定加入 prompt。浏览与选择不得改变 Session 或请求。

#### Scenario: Keep plugin instructions inert
- **WHEN** 正文或参数含 shell 元字符、反引号、环境变量或 CLAUDE_PLUGIN_ROOT 表达式
- **THEN** 只作为普通文本提供，不执行进程、不展开宿主信息、不提高工具权限

#### Scenario: Preserve stable bytes after moving a plugin
- **WHEN** 同内容插件在不同宿主绝对目录加载且参数/枚举顺序不同
- **THEN** 模型目录 canonical bytes、来源身份和内容 fingerprint 一致

#### Scenario: Separate metadata and body invalidation
- **WHEN** 分别只修改未调用技能正文或可见 metadata，并重新启动
- **THEN** 前者保持 metadata 指纹，后者准确改变 skill_catalog 指纹，其他稳定来源指纹不受影响

#### Scenario: Keep private binding changes outside visible history fingerprints
- **WHEN** 重新加载只改变非模型注解的插件，两次生成的 Skill.Content 与有效 metadata 相同但私有 revision 不同
- **THEN** 两家各自发送的附件正文、request canonical bytes、footprint 和历史内容 fingerprint 相同，只有私有来源关联事实可不同

### Requirement: Closing a source retains ownership until every read finishes

插件来源 SHALL 具有唯一关闭 owner、明确的关闭接收线性化点和最终完成信号，覆盖模型读取及尚未进入输入 admission 的显式准备。关闭接收后 MUST 拒绝新读取、取消在途读取并保留仍被使用的原根；等待超时 SHALL 升级为取消并关闭在途读取自己的文件 handle，但 MUST NOT 据此提前归还借用、宣告清理完成或释放仍被借用的原根。最后一个读取释放或关闭发起者只能由一个完成实际清理，重复关闭复用最终结果。

持有的插件根目录最多 32 个，同时在途来源读取最多 16 个；超出并发额度 SHALL 以 skill_source_busy 拒绝且不读取文件或提交输入。所有读取成功、错误、取消和加载回滚路径 SHALL 最终释放 handle；来源完成后才能释放其依赖的私有存储检查资源。不通过每次等待创建 helper goroutine，也不得在持有状态锁时执行 I/O、回调、等待或 Close。

#### Scenario: Close while an explicit preparation has not reached admission
- **WHEN** 一个显式 Prepare 被确定性阻塞，此时运行关闭并触发等待超时
- **THEN** 根/handle 所有权保持且完成信号未触发，新读取被拒绝；解除阻塞后读取取消、清理恰好一次并发出最终完成信号，没有该输入事实

#### Scenario: Close after concurrent model and host reads
- **WHEN** 模型与宿主读取同时在途，随后重复关闭来源
- **THEN** 每项 handle 都完成释放，原根只关闭一次，不因 Runtime 已结束而提前释放存储边界

#### Scenario: Reject additional reads at the source concurrency limit
- **WHEN** 16 个来源读取已获得接收，另一个模型/宿主读取到达
- **THEN** 额外读取返回 skill_source_busy，文件读取与 journal bytes 不增加，已有读取不被无条件丢弃
