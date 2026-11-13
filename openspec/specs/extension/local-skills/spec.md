# extension/local-skills Specification

## Purpose

定义本地 Skills 从用户及项目目录发现、按需加载、显式调用、来源隔离和交互展示的完整行为，使复用工作流具备可解释的选择规则、资源上限与失败语义，并在双 Provider 与会话恢复中保持一致。

## Requirements

### Requirement: Local discovery is bounded deterministic and source aware

系统 SHALL 在启动时发现用户根 `~/.config/easycode/skills`、`~/.agents/skills`、`~/.claude/skills`，以及最近 `.git` 根到启动 cwd 连续目录链中的 `.easycode/skills`、`.agents/skills`、`.claude/skills`。无项目边界时只检查 cwd。每根只检查直接子目录的 `SKILL.md`；不得扫描插件缓存或任意 home 树。项目优先用户、近目录优先远目录、同层 easycode > agents > claude。最高优先项非法时 MUST 阻断该名称，不回退低优先级项。

祖先链 SHALL 最多 128 层、候选项最多 1024、可用条目最多 256、metadata prompt 最多 64 KiB。超限根不得发布部分目录；其他合法根保持可用；合并目录超限时不发布可执行目录但普通聊天可用。候选遍历与目录输出 MUST 稳定排序，诊断有界且不含文件正文。目录生命周期固定到本会话，新增/删除在重新启动后生效。

显式本地插件的技能贡献 SHALL 按 extension/local-plugins 的规则合入同一冻结目录，只提供 plugin:leaf 限定名，不加入 user/project 裸名优先级链。插件来源失败不得禁用原有合法本地技能；合并数量和 metadata 仍受共享上限约束，插件被纳入或整体拒绝的结果可在插件列表解释。

#### Scenario: Resolve duplicate names reproducibly
- **WHEN** 用户与多个项目层同时声明同名 Skill，文件枚举顺序不同
- **THEN** 相同优先级规则选择唯一来源，目录 bytes/fingerprint 相同，遮蔽项可在列表解释

#### Scenario: Do not fall back past a broken winner
- **WHEN** 最高优先级 Skill 存在但格式损坏
- **THEN** 同名调用失败且不加载低优先级正文，其他名称仍可用

#### Scenario: Bound startup discovery
- **WHEN** 一个来源超过层数或候选上限
- **THEN** 该来源报告安全诊断且不发布部分条目，不继续无限遍历

### Requirement: Skill formats have a strict supported subset

系统 SHALL 接受 UTF-8 Markdown 和有界 YAML frontmatter，支持 name、description、argument-hint、user-invocable、disable-model-invocation、license、字符串映射 metadata。本地 Skill 与 EasyCode/Claude 插件的 leaf SHALL 为实际 Skill 目录名，显式 name MUST 与该目录名一致；Codex/Agent Plugins 的 leaf SHALL 为合法 frontmatter.name，缺省使用实际 Skill 目录名。leaf 为 1–64 ASCII 小写字母/数字/单连字符，首尾为字母或数字。description 必填且最多 1024 UTF-8 bytes，frontmatter 最多 16 KiB，完整文档最多 64 KiB。布尔字段只接受布尔值，缺省 user-invocable=true、disable-model-invocation=false。

重复键、alias/merge/tag、错误类型、非 UTF-8 和未知字段 MUST 禁用该条目。allowed-tools/model/hooks/context/agent/paths/shell/effort 等未支持行为字段 MUST 报告 skill_unsupported_field，不静默忽略。args 最多 16 KiB，展开正文与来源包装最多 128 KiB，超限必须拒绝。参数只进行字面量 $ARGUMENTS 替换；无占位且有参数时追加固定参数段；不得执行 shell 插值、环境展开或文件 include。

裸名本地来源 SHALL 保持原有名称规则。插件 SHALL 按其 importer 的命名与布局规则确定 leaf，随后限定为 plugin:leaf；不能把 Claude 的展示 name 改为调用名，也不能把 Codex 的合法引用名改为目录名。Agent Plugins 只接受直接子目录布局，不自动把根 SKILL.md 当作单技能。限定名恰好一层冒号、每段最多 64 bytes、总长最多 129 bytes。本文所列未支持行为字段对插件 Skill 同样失败关闭，不因 manifest importer 而放宽。

#### Scenario: Reject a fork skill explicitly
- **WHEN** frontmatter 包含 context: fork 或 allowed-tools
- **THEN** 条目显示不支持且不能被显式或模型调用，不创建子代理或授权

#### Scenario: Keep argument expansion inert
- **WHEN** 参数包含 shell 元字符、反引号或环境引用
- **THEN** 参数作为普通文本加入正文，不启动进程，不展开环境变量

### Requirement: Reads stay bound to the discovered source

系统 SHALL 为每项绑定唯一来源身份和发现时完整主文档 digest；metadata revision 不依赖绝对路径、时间、inode 或调用身份。读取时 MUST 使用同一来源，以实际打开 FD 验证类型与范围，拒绝 symlink、特殊文件、目录穿越、绝对资源路径和跨根访问。主文档改变 SHALL 返回 skill_source_changed，不能回退其他来源或普通文件工具。

Skill 工具 SHALL 接受互斥的主文档加载 `{skill,args?}` 或资源读取 `{skill,resource}`，不接受任意 Skill 路径。资源只允许相对该 Skill 目录的 UTF-8 常规文件、最多 64 KiB；读取资源前同样验证主文档 digest。读取只产生文本，不授予写文件、执行或网络权限。

插件读取 SHALL 额外校验冻结的实际来源身份、选定 manifest 原始 bytes 及适用的调用策略副文件存在性/内容，并从同一已校验的实际 Skill 目录 FD 完成主文档/资源读取；manifest、主文档或副文件绑定改变均返回 skill_source_changed。插件附件 revision SHALL 绑定规范化贡献、实际 Skill 目录规范相对路径、leaf、主文档完整内容、副文件存在性/digest 与有效策略；主文档内容校验仍使用独立原始 digest。资源不能通过兄弟技能、其他插件、裸名或普通读取通道回退；插件根不能扩大单技能资源根。私有 Session/artifact 存储隔离、来源借用/关闭完成及并发读取额度遵循 extension/local-plugins。

#### Scenario: Reject a replaced path deterministically
- **WHEN** 在打开检查点将候选目录或文件替换为 symlink
- **THEN** 不读取 symlink 目标，返回安全错误，不回显目标内容

#### Scenario: Refuse cross-source resource reads
- **WHEN** 模型请求 ../、绝对路径或其他 Skill 的资源
- **THEN** 请求失败且不尝试其他读取通道

#### Scenario: Read a supporting document
- **WHEN** 已发现 Skill 的主文档未变且请求合法 references/example.md
- **THEN** 返回同来源有界全文并标识为资源，不声称主 Skill 被激活

### Requirement: Explicit and model invocation have distinct permissions

系统 SHALL 识别首词 `/<known-name> args` 和 `$name args`，只展开一个显式 Skill。内置命令优先，未匹配的裸名 slash 与绝对路径保持普通正文；未知 `$name` 返回 skill_not_found。语法合法的 `/<plugin>:<leaf>` 和 `$<plugin>:<leaf>` SHALL 明确作为 Skill 调用，未知限定名返回 skill_not_found，已知但禁用的插件返回 skill_unavailable，不把失败的限定调用转为普通 prompt。内置 /plugins 优先，裸名 plugins Skill 仍可通过 $plugins 调用。正文中间、代码块中的名称不作自动展开。user-invocable=false MUST 阻止显式调用；disable-model-invocation=true MUST 排除模型目录并拒绝模型猜名调用。读取成功只说明指令可供当前任务使用，不等于用户批准后续副作用。

显式准备 SHALL 在输入 admission 前完成，TUI、一次性 headless、stream-json 使用同一行为。准备失败、取消或源变化时不得发起 Provider 请求或写入该输入。模型调用 SHALL 使用完整 ToolCallReady 和既有 durable ledger；部分参数不得触发读取。

#### Scenario: Invoke with either spelling
- **WHEN** 用户分别提交 /review diff 和 $review diff
- **THEN** 两者生成同一 Skill 展开内容，分别保留各自原始用户文本

#### Scenario: Keep ordinary text unchanged
- **WHEN** 输入是未注册的 /tmp/file 或普通正文中包含 $review
- **THEN** 按既有普通输入处理，不读取 Skill

#### Scenario: Reject a model-only or manual-only violation
- **WHEN** 用户调用 user-invocable=false 的 Skill，或模型请求加载 disable-model-invocation=true 的 Skill 主文档
- **THEN** 对应调用失败且没有该正文进入历史

### Requirement: Skills UI explains availability without model calls

TUI SHALL 提供 /skills，本地按来源分组、名称排序，显示描述、参数提示、可调用性、遮蔽和安全诊断。列表不访问 Provider；Enter 只把所选可显式调用的命令放入 composer，Esc 保留原草稿。View MUST 无 I/O；异步完成必须校验请求身份，旧响应不得覆盖新草稿。成功加载只在 durable 后显示简洁名称/来源，详情提供正文。

插件条目 SHALL 显示完整限定名和安全插件来源，与 /plugins 详情和补全来自同一快照；可调用性、禁用原因和选择结果必须一致。展示不得引入插件绝对路径或在浏览时重新加载 manifest。选择与内置命令冲突的裸名技能 SHALL 填入 $name，其他技能按既有 slash 规则填入，不能因选择 plugins/cache 等技能而执行相应内置命令。

#### Scenario: Inspect and select locally
- **WHEN** 用户打开 /skills、选择一个可调用项并按 Enter
- **THEN** composer 出现调用命令，Provider 请求和 Session 写入次数仍为零

#### Scenario: Select a skill whose bare name is reserved
- **WHEN** 用户在 /skills 中选择合法裸名 plugins、cache、compact、context、hooks 或 skills
- **THEN** 填入对应 $name，后续提交走 Skill 准备，不误开浏览器或执行其他本地命令

#### Scenario: Preserve a draft on cancellation
- **WHEN** 用户退出列表或取消尚未 admission 的加载
- **THEN** 原草稿可继续编辑，过期异步结果不会提交输入

### Requirement: Explicit durable selection permits same-turn supporting resources

对于 disable-model-invocation=true 的 Skill，系统 SHALL 只在当前 turn 已成功 durable 提交用户显式调用后允许模型读取该 Skill 同来源、同 revision 的辅助资源。该资格不得允许模型重新加载主文档，不得由 queued/prepared 输入、其他 turn 或模型猜名建立，不得提升写入/执行/网络权限。资格只从当前 turn 已提交附件派生，turn 结束失效，不保存独立授权 record；恢复历史不自动执行读取。

插件来源 SHALL 将 namespace、source.scope/kind/directory 和复合 revision 一起匹配；同名其他插件、本地裸名、重启后已变化的贡献和未重新明确配置的来源均不得满足资格。插件加载或列表选择本身不能产生该例外。

#### Scenario: Read references after an explicit selection
- **WHEN** 用户显式调用的附件在当前 turn 已 Sync/finalize，模型请求相同来源和 revision 的 references/example.md
- **THEN** 允许来源内有界读取，即使该 Skill 禁止模型自行调用

#### Scenario: Reject stale or uncommitted selection
- **WHEN** 显式输入仅 queued、来自旧 turn、来源/revision 不符，或模型请求主文档
- **THEN** 不能使用此例外，读取被拒绝且不回退其他通道

### Requirement: Qualified skill identity is bound to its current source

当前 Skill metadata、附件和工具结果 SHALL 严格验证来源与名称的组合。plugin 来源 SHALL 使用 scope=plugin、kind=easycode/claude/codex/agentplugins、directory=插件 ID；限定名的 namespace 必须与该 ID 相同。非 plugin 的 user/project 来源 SHALL 继续只允许 easycode/agents/claude 与裸名。插件来源不得使用裸名，其他来源不得伪装限定名；未知 kind、额外字段、非法组合或不合法 revision 必须失败关闭。

来源只保存逻辑身份，不保存绝对目录。既有附件字段和当前 decoder SHALL 直接承载该唯一结构，不新增版本、历史 reader、可选 activation record 或通用无类型扩展字段。

#### Scenario: Reject a forged namespace association
- **WHEN** 附件 name 为 qa:review，但来源 directory 为 docs 或 scope 为 project
- **THEN** 当前校验失败，不提交或恢复该附件，不查询目录修复其来源

#### Scenario: Keep existing local identity valid
- **WHEN** 原有合法 user/project Skill 仍使用裸名和当前来源组合
- **THEN** 不需新增字段或转换即可继续调用与恢复

#### Scenario: Reject a qualified call without provider work
- **WHEN** 用户显式调用不存在或已禁用插件的 /qa:review
- **THEN** 返回明确 Skill 错误，不新增该输入 journal bytes，不向 Provider 发送替代普通文本

### Requirement: Codex sidecars restrict invocation without enabling unsupported behavior

codex/agentplugins 插件技能 SHALL 有界读取实际 Skill 目录内可选 agents/openai.yaml，并严格支持 policy.allow_implicit_invocation 的布尔限制。有效 disable-model-invocation SHALL 为主文档该标志与副文件显式 false 的逻辑或；副文件 true MUST NOT 重新启用主文档已禁止的模型调用。user-invocable 仍取主文档值。禁止模型调用时 SHALL 排除模型目录并拒绝自行加载主文档；只有已 durable 当前 turn 的同来源、同复合 revision 显式选择才能建立既有辅助资源例外。

副文件 SHALL 最多 64 KiB、YAML 最大嵌套 16 层；重复键、alias/merge/tag、null、错误类型或未知字段 MUST 拒绝。除 policy.allow_implicit_invocation 外，仅接受 interface 的 display_name、short_description、icon_small、icon_large、brand_color、default_prompt 有界字符串注解，各最多 2048 bytes、short_description 最多 1024；注解不触发资源/URL 读取、默认 prompt 注入或工具授权。dependencies 与 policy.products 即使为空也必须报告 skill_unsupported_field，不隐式安装工具或忽略产品限制。

副文件不存在可采用主文档策略；存在但格式错误 SHALL 禁用该 Skill 并报告 skill_metadata_invalid，不安全/不可读 SHALL 报告 skill_metadata_unavailable，不能当作 absent 或放行。发现时冻结 absent/present 与原始 digest，后续新读取均校验；出现、删除、改写或路径变为 symlink SHALL 返回 skill_source_changed，不热加载策略。副文件与主文档共享启动读取额度，存在性/digest 与有效策略进入附件复合 revision；导入过程不自动将副文件原文写入模型稳定目录或 Session。后续明确的合法 resource 请求读取该文件时，仍按既有 Skill result 冻结，不新增旁路记录。此规则不改变 user/project、easycode/claude 来源的既有 SKILL.md 子集。

#### Scenario: Honor a manual only Codex policy
- **WHEN** Codex 技能主文档允许用户调用，而副文件 allow_implicit_invocation=false
- **THEN** 技能不进入模型目录且模型自行加载失败，用户显式调用仍可用，Sync 后的同来源辅助资源例外保持

#### Scenario: Keep the stricter main document policy
- **WHEN** 主文档 disable-model-invocation=true，而副文件 allow_implicit_invocation=true
- **THEN** 有效策略仍禁止模型调用，不能被副文件重新启用

#### Scenario: Reject unsupported sidecar behavior
- **WHEN** 副文件声明 dependencies 或 policy.products，或者含未知行为字段
- **THEN** 该 Skill 禁用并显示明确错误，其他合法技能保持可用，不安装或执行外部能力

#### Scenario: Reject invalid or unreadable metadata safely
- **WHEN** 副文件重复键、错误布尔值、非法 YAML、超过限制、不可读或经不安全路径访问
- **THEN** 禁用该项并返回安全 metadata 分类，不回退到主文档允许模型调用的默认值

#### Scenario: Detect both missing to present and present to missing changes
- **WHEN** 同一进程内增加、删除或改写被冻结的副文件
- **THEN** 新主文档/资源读取失败为 skill_source_changed，已 Prepare 或 durable 的正文继续使用原冻结内容

#### Scenario: Keep display annotations outside the model prefix
- **WHEN** 重启时只修改有效副文件的 interface 注解而未改变名称、描述或有效调用策略
- **THEN** 模型目录 canonical bytes/fingerprint 不变，新读取使用新复合 revision，图标与默认 prompt 均不被访问或执行
