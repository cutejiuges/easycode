## Context

动机与已确认范围见 [proposal.md](proposal.md)。本文件为待审阅设计，以下新增类型、文件和测试都是实施目标，不代表已经存在。

当前可复用的实际接缝：

| 接缝 | 当前实现 | 本 change 的使用方式 |
| --- | --- | --- |
| Skill 来源与解析 | `internal/extension/skill/local.go`、`parser.go` | 保留 Local 作为唯一 Prepare/Read 入口，扩充显式插件贡献，不另建插件 Skill 执行器 |
| 文件安全 | `internal/extension/localfile/{read,open_unix}.go` | 复用逐段 no-follow、常规文件和有界读取；补齐相对于已打开来源目录的读取以绑定检查与使用 |
| 输入生命周期 | `internal/runtime/skills.go`、`input_commit.go` | 在 admission 前展开一次，排队只保存冻结附件，Sync 后才可见 |
| 模型工具与预算 | `internal/tool/skill.go`、`internal/app/skills.go` | 仍使用一项 Skill 能力、原 ledger、只读并行与结果预算 |
| 目录和缓存 | `internal/domain/skills.go`、`internal/context/skills_test.go` | 合并成一份 SkillCatalog，不新增 plugin prompt segment |
| 恢复 | 两家 `skill_test.go`、`internal/session/skill.go`、`internal/app/skills_test.go` | 附件、结果与原生 output 继续冻结，恢复不访问历史来源 |
| 交互 | `internal/tui/skills.go`、`completion*.go` | 延续列表、先填入后提交、焦点隔离和固定尺寸测试 |
| 启动后安装来源 | `internal/app/hooks.go`、`internal/runtime/hooks.go` | 参考已有恢复后安装未来 Hook 快照的生命周期，明确 Skill 的同类装配时点 |

`internal/extension/extension.go` 和 `internal/extension/plugin/plugin.go` 目前只有批准占位，没有消费者；`internal/subagent/control.go` 仍是 P7 占位。现存 `add-pre-tool-use-command-hooks` 的 tasks 已全部完成，本 change 不改动或归档它。

2026-11-12 在本工作区执行的基线 `make verify` 已通过，覆盖分支策略测试、gofmt、go vet、Staticcheck、架构/全量测试及 race test；`internal/app` 的 race 测试实际完成用时约 512 秒。这个结果只证明实施前基线，不证明本次计划能力已经实现或验收。

### 两个参考工程的核对范围

以下结论来自本机源码，固定到此次读取的提交，不推定线上版本与其完全一致。参考目录只读，没有修改、执行构建或引入其依赖。

| 工程与固定提交 | 实际阅读的主要位置 | 已观察到的行为及设计取舍 |
| --- | --- | --- |
| Claude Code sourcemap `a8a678cb6244e6770e1e421767ff0987a1d95549` | `restored-src/src/utils/plugins/pluginLoader.ts:1330-1405,1555-1636,2917-2990` | `--plugin-dir` 是本次会话的直接目录来源，不经 marketplace；采用这个入口，保留可诊断的单插件失败隔离；不采用缺少 manifest 时合成名称或配置损坏后猜测来源 |
| 同上 | `restored-src/src/utils/plugins/loadPluginCommands.ts:59-96,684-843` | 插件技能使用 `plugin:skill`；支持集合根和含 SKILL.md 的单技能根。采用两种形态，复用 EasyCode 更严格的 frontmatter 和 FD 边界；不采用 symlink 跟随或递归 commands 导入 |
| 同上 | `restored-src/src/utils/plugins/schemas.ts:275-320,486-504` 与 `restored-src/src/utils/plugins/pluginLoader.ts:1376-1383,1569-1603` | schema 把 skills 写作额外路径，但实际 loader 在 manifest.skills 存在时跳过默认目录探测。因此本轮明确规定“显式 roots 替代默认 roots”，不凭注释推断隐式并集；以独立 fixture 固定这项选择 |
| 同上 | `restored-src/src/skills/loadSkillsDir.ts:181-267,319-393`、`restored-src/src/components/skills/SkillsMenu.tsx` | metadata 渐进披露、参数提示、手动/模型调用区别值得保留；shell 插值、插件变量展开和扩展权限不进入本次子集 |
| Codex `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee` | `codex-rs/core-plugins/src/{manifest.rs,agent_plugin_manifest.rs}`、`codex-rs/utils/plugins/src/plugin_namespace.rs:15-92` | 同时存在 `.codex-plugin/plugin.json`、Claude manifest 和带精确 `$schema` 的根 `plugin.json`；采用受限 adapter，根 schema 只用于识别外部格式，永不联网取 schema，内部不添加版本门禁 |
| 同上 | `codex-rs/core-plugins/src/loader.rs::load_plugin_skill_inventory/plugin_skill_roots`、`codex-rs/utils/plugins/src/lib.rs` | 贡献携带插件身份和技能根，roots 排序去重；Agent Plugins 使用 DirectChildren，Codex JSON 使用递归发现。采用各格式明确的布局子集和来源边界，不把插件文件路径放进 Provider wire 或交由 TUI 解析 |
| 同上 | `codex-rs/ext/skills/src/loader/namespace.rs:135-181` | 使用限定名称而不是抹掉所有来源；采用插件前缀，不引入同名的短别名或基于最近使用时间的选择 |
| 同上 | `codex-rs/core/src/plugins/skill_snapshot_tests.rs:20-103` | 测试要求插件目录与宿主 Skill 服务复用同一冻结快照；采用单次发布，避免 `/plugins`、`/skills`、模型目录各扫描一次得到不同结果 |
| 同上 | `codex-rs/skills/src/{parser.rs,metadata.rs,model.rs}`、`codex-rs/ext/skills/src/loader/{host.rs,metadata.rs,host_tests.rs}`、`codex-rs/ext/skills/src/provider/host.rs` | Codex 以 frontmatter.name 决定引用名，sidecar 的 allow_implicit_invocation=false 会隐藏模型目录；保留这些有效限制，依赖与产品筛选未实现时明确拒绝，不照搬可选 metadata 解析失败后放行的规则 |
| 同上 | `codex-rs/ext/skills/src/host_outcome.rs::read_skill_text`、`codex-rs/core-plugins/src/{loader.rs,loader_tests.rs}` | 冻结 metadata 不授予读取权限，读取仍匹配当前 filesystem authority；最新 loader 可并发读取但按配置名顺序消费。采用来源再验证与确定性提交，本轮有界本地加载保持串行，不因引入并发改变冲突/预算结果 |
| Claude Code 同一提交 | `restored-src/src/utils/plugins/loadPluginCommands.ts::loadSkillsFromDirectory/createPluginCommand`、`restored-src/src/commands/plugin/ManagePlugins.tsx::handleBack`、`restored-src/src/commands/plugin/pluginDetailsHelpers.tsx` | Claude 的实际调用名使用目录 basename，frontmatter.name 是展示名；Enter 进入详情、Esc 按视图层级返回。采用调用名及导航骨架，EasyCode 的严格 name 子集、只读面板与安全转义单独列明 |

参考源根目录分别为 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap`、`/Users/cutejiuge/IdeaProjects/codex`。表中路径在各自根目录下解析。实现前若源码位置变化，应核对相同业务行为，不把行号作为代码契约。

### 2026-11-12 二次自检与修订结论

初稿使用的 Codex 提交为 `6f8bf683cee83ed4d580a2e106b587d8451db936`；本次只读核对发现参考工程已更新到上表提交，已检查相关差异及新增测试。用户已确认本轮导入 allow_implicit_invocation 限制，并同步修订 proposal、design、五份 specs 与 tasks；仍不引入插件可执行贡献。

| 自检项 | 初稿缺漏或反例 | 本次设计处理与验收落点 |
| --- | --- | --- |
| R1 引用名与布局 | Claude 的目录名与 Codex 的 frontmatter 名被统一；Agent Plugins 的 DirectChildren 与单技能根混为一谈 | 分 importer 列明命名/布局表；同一目录名、不同 frontmatter.name 的对照 fixture，见任务 4.1 |
| R2 调用策略 | 忽略 agents/openai.yaml 可能把手动技能加入模型目录 | 导入单项布尔限制、严格合并，拒绝未支持依赖/产品策略，见任务 3.5/4.5 |
| R3 来源指纹 | 只校验主文档/manifest，无法发现副文件从缺失变为 present 或策略变化 | 将副文件存在性、内容 digest 和有效策略加入冻结绑定；恢复仍零来源读取，见任务 4.3/8.6 |
| R4 资源上限 | 限制技能候选不等于限制巨量无关目录条目，也未计副文件读取 | 有界原始枚举、分层计数与共享 16 MiB 主文档/副文件额度，见任务 4.4 |
| R5 冲突与发布 | 格式非法但名称明确的插件可能绕过 ID 冲突；loader 计数可能早于最终 Skill 纳入 | 先取得无歧义 ID 再检测冲突，最终纳入后一次生成列表/目录；硬链接主文档不能合并不同资源根，见任务 4.2/5.4 |
| R6 读取关闭 | 仅返回裸 FD 未定义在途读取完成；ChatSession.Shutdown 不等待发生在 AgentLoop 外的 PrepareInput | 来源 owner、受控 handle、关闭接收线性化点及最终 Done；app 等待后再释放存储边界，见任务 2.4/5.2 |
| R7 本地交互 | 任意多行草稿没有打开 /plugins 的入口；/skills 把裸名 plugins 填为 /plugins 会再次打开面板 | 精确定义命令/补全打开时的草稿，保留当前窗口布局；保留名技能填入 $name，见任务 7.2/7.5 |
| R8 CLI 隐私 | Go flag.Value.Set 返回错误时，标准库会用 %q 回显非法参数原值 | 有界收集并在 Parse 后报告安全分类；对绝对路径、超长值及非法 UTF-8 验证 stderr 无原值，见任务 6.4 |
| R9 当前读取边界 | 冻结 metadata 若被误当授权，恢复或来源关闭后可能仍读取 | 所有新读取重新取得同来源 handle，关闭后拒绝；恢复仅消费冻结事实，见任务 2.4/8.3/9.5 |

上述为方案审阅结论，表内测试是必须实施的验收要求，不能将规格校验或前次基线 make verify 当作这些新功能已经通过测试的证据。

## Goals / Non-Goals

**Goals:**

- 首次接入真实插件贡献，沿用现有工具和双 Provider 主链，新增服务只有 manifest/来源装配与本地浏览。
- 让用户能明确回答“本次加载了哪个插件、哪些技能可用、为何被禁用、重新启动后怎样生效”。
- 使插件所在绝对目录、CLI 参数顺序、枚举顺序和文件时间不影响模型目录；恢复只依赖已提交事实。

**Non-Goals:**

- 不实现通用扩展容器或未来贡献空接口；只删除/替换 `extension` 和 `plugin` 中被本轮真实技能贡献接替的占位。
- 不复用参考工程的登录、全局 registry、历史 manifest 迁移器或 marketplace store。多个外部 importer 不等于内部多版本 reader。
- 不把静态指令读取视为插件命令信任。现有工具批准、Hook 信任、sandbox 和当前 turn 资源资格各自保持原语义。

## Decisions

### 1. 选择本地 Plugin 技能包作为当前切片

| 候选 | 现有基础与缺口 | 当前取舍 |
| --- | --- | --- |
| Skills 增量 | 已有发现、字面参数、资源、手动/模型调用、补全和恢复；path activation/allowed-tools/fork 会牵动新的上下文或权限生命周期 | 本轮通过插件复用这条成熟链路；不为扩充字段另造调用路径 |
| 新 Hook 事件 | 已交付 PreToolUse/PostToolUse/PostToolUseFailure；Stop 等事件需要新的 turn 终态、阻止结束与循环限制契约 | 有价值，但无法直接解决一组工作流的分发和命名空间问题 |
| 本地 Plugins | 缺失，但技能贡献可以直接消费现有完整实现，不引入进程或子线程 | 选择，交付 P6 可用闭环，同时为以后经批准的其他贡献提供真实边界 |
| Subagent | 线程树、writer、并发/深度/预算、fork、取消和 completion 均未实现 | 跨越到 P7 会同时改动太多尚未验证的基础契约，当前不优先 |

没有选择仅保存 manifest 或只增加 `/plugins` 空面板：本次必须能完成“加载 -> 发现 -> 选择/工具读取 -> 持久化 -> 恢复”的真实流程。

### 2. 显式 CLI 来源，只有本次进程有效

新增 `--plugin-dir PATH`，可重复最多 32 次。`main.go` 与 `resume.go` 共用一个有界参数收集器；`app.Options.PluginDirs` 拷贝输入。普通 TUI、print、json、stream-json、`--resume`、`--continue` 与 `easycode resume` 都传递同一份参数。

- 相对路径在 app 捕获的启动 cwd 下解析，picker 选择其他历史工作目录不重解释参数；不展开 `~`、环境变量、URL、glob 或远端引用。
- 空参数、超 32 项、单项超 4096 bytes 或非法 UTF-8 属于 usage error，在资源创建前返回英文错误和 exit 2；文件不存在/格式损坏属于隔离的来源诊断，不使普通聊天或历史恢复失效。
- Go 1.24 的 `flag.FlagSet.parseOne` 在 Value.Set 返回错误时会回显 value；收集器只保留有界值与安全错误分类，不让原值进入 Set 错误，在 Parse 后统一拒绝。String/默认值展示也不能返回已配置绝对路径；直接调用 app.Options 的入口复核同一边界。
- 相同实际目录重复传入合并一次；使用实际目录 FD 身份判断别名，身份不出来源边界。不同目录声明相同插件 ID 时全部禁用该 ID，不采用第一个/最后一个获胜。
- 不写主配置或 Session，不创建安装目录；`--version`、`--help`、离线 `sessions list` 不扫描插件，picker 预览与取消也不加载插件。

采用显式 flag 而不是自动项目扫描，可以立即交付 Claude 风格体验，并避免尚未设计的项目插件启用权限和安装状态。动态启停以后需要独立 change，本轮 `/plugins` 明确为只读。

### 3. 有限 manifest adapter 与唯一当前贡献

按下表优先级选择首个存在的 manifest；存在但不可安全打开、损坏或不支持时立即阻断该插件，不尝试后面的格式。不合并多个 manifest，不从目录名合成缺失的 manifest。

| 优先级 | 位置 | 来源 kind | 支持内容 |
| --- | --- | --- | --- |
| 1 | `.easycode-plugin/plugin.json` | `easycode` | 本轮原生技能 manifest |
| 2 | 根 `plugin.json` | `agentplugins` | `$schema` 必须精确为 `https://agent-plugins.org/schemas/1.0.0/plugin.schema.json`；使用约定 `./skills`，不接受顶层 skills 覆盖 |
| 3 | `.codex-plugin/plugin.json` | `codex` | Codex JSON 的 metadata 与 skills 子集 |
| 4 | `.claude-plugin/plugin.json` | `claude` | Claude JSON 的 metadata 与 skills 子集 |

根 `plugin.json` 存在却与上述 schema 无关也报 `plugin_manifest_unsupported`，不回退。这个严格选择是本地显式目录的可解释行为，不模仿参考代码的所有 fallback。

所有格式共有必需 `name`：1–64 bytes，沿用 EasyCode 小写字母/数字/单连字符 slug 规则，不做大小写归一化或替换非法字符。可选 metadata 为 version（非空，最多 128 bytes）、description（最多 1024 bytes）、author（name/email/url 字符串对象）、homepage/repository/license 字符串和 keywords 字符串数组。URL/仓库值只作有界 metadata，永不访问；每个字符串最多 2048 bytes，keywords 最多 32 项且单项最多 64 bytes。metadata 不构成版本兼容判断。

三种非 Agent Plugins 格式的 `skills` 可缺省，或为一个 `./...` 字符串/最多 8 项非空字符串数组：缺省使用 `./skills`；显式值完全替换默认根。空数组、空字符串、错误类型、未知字段、任何层级重复键、null、尾随 JSON、非法 Unicode 全部拒绝。Agent Plugins 允许空 `extensions: {}`，非空 extensions 报不支持；其他格式不接受 extensions。author、homepage、repository、license 和 keywords 是经过验证的注解，本轮不把它们放进模型目录、附件身份或未被消费的核心字段。

`hooks`、`mcpServers`/`mcp_servers`、`agents`、`commands`、`outputStyles`、`lspServers`、`apps` 等行为字段即使为空也使整插件 `plugin_unsupported_contribution`；未知字段为 `plugin_unsupported_field`。不读取这些声明指向的文件，不把其配置降级成技能指令。对未声明的其他目录不扫描；面板对全部成功插件持续标识 `Skills only`，因此也不声称加载了约定 hooks/MCP 文件。

JSON 复用 codec 的重复成员/Unicode/封闭对象检查与 Sonic；导入器各有 typed wire DTO，标准化为只有实际技能根的当前 `extension.Contribution`。用明确的格式 switch 与小适配函数，无可变全局 registry；不保留原来无消费者的 `Skills/Agents/Hooks/... []string` 大杂烩和旧 Importer 签名。所有 New 保持纯内存，只有 Load/Open/Read 执行 I/O。

manifest 发现分两个步骤：先在 bytes/深度/JSON 无歧义校验后提取合法声明 ID，再验证完整支持子集。即使后一步因未知字段或未支持贡献而失败，该安全 ID 仍参与不同目录间的冲突检测；无可无歧义提取的名称则只产生参数序号诊断，不能用正则或损坏 JSON 猜名。扫描技能前先完成全部目录的 manifest 分类和 ID 冲突判定，避免 CLI 顺序或错误来源消耗技能预算后改变胜者。

### 4. 两种技能根，稳定发现和冲突策略

manifest 路径必须以 `./` 开头，去掉该前缀后为最多 4096 bytes 的规范相对 POSIX 路径；拒绝空路径、`.`/`..`、绝对路径、反斜杠、盘符、NUL、重复分隔符、非规范 clean 结果及超过 128 个组件。路径排序、去重后再读取，禁止通配或递归目录树扫描。

布局与引用名分别决策，不把两家不相同的语义压平：

| 来源 kind | 技能根布局 | leaf 的确定方式 |
| --- | --- | --- |
| easycode / claude | 根含 SKILL.md 则为单技能根，否则仅扫描直接子目录；直接主文档非法不回退 | 使用 SKILL.md 所在目录 basename；显式 frontmatter.name 必须等于该值。Claude 参考实现会将不同 name 作为展示名，本轮延续 EasyCode 更严格的子集，不把它改为调用名 |
| codex | 支持单技能根或直接子目录集合；本轮不递归更深层 | 使用合法 frontmatter.name，缺省取 SKILL.md 所在目录 basename；不能把已有 frontmatter 名强制改成目录名 |
| agentplugins | 仅约定 ./skills 的直接子目录集合，与参考 DirectChildren 一致 | 同 codex；./skills/SKILL.md 本身不属于受支持布局，若存在则以 plugin_unsupported_layout 拒绝该插件，不伪装 empty 或递归回退 |

所有 leaf 仍执行既定 1–64 bytes slug 规则，不采用外部实现的字符串修复、大小写归一化或非法字符替换。缺省 `./skills` 不存在时插件为 empty；显式声明的根缺失/不可读使该插件整体 unavailable，不发布遗漏根的部分快照。

只有实际 Skill 目录身份与 SKILL 文件身份都相同的重复发现才合并一次，绑定使用该 Skill 目录相对于插件根的规范路径；重叠 roots 命中同一目录不会产生不同资源根。相同主文档被硬链接到两个不同目录时，两个目录的资源可能不同，不能仅按文件 inode 去重。不同绑定产生同一 plugin:leaf 时全部禁用该名；其他合法技能仍可用，插件为 partial。非法候选已能确定 leaf 时占住该名称，不能通过另一个根绕过错误。单个 Skill 格式错误隔离到该绑定；根级失败、manifest 错误和插件 ID 冲突隔离到整插件。

本地裸名 Skill 的原有 project/user 优先级完全保留；插件只提供限定名，不抢占裸名，也不创建短别名。多个插件都提供 review 时分别为 `qa:review`、`docs:review`。

### 5. 统一 Skill 引用与来源身份

保留 `ValidateSkillName` 的文件 leaf 规则，增加纯内存的 `SkillReference` 解析/校验：裸名为原有 local 引用；限定名仅允许 `plugin:leaf` 一层冒号，每段最多 64 bytes，总长最多 129 bytes。资源相对路径验证仍禁止冒号，不能混用名称和文件路径校验。

不增加附件字段：

| 当前字段 | 插件取值与验证 |
| --- | --- |
| `source.scope` | 固定 `plugin` |
| `source.kind` | 只接受上表四个 importer kind |
| `source.directory` | 插件 ID，不是宿主目录 |
| metadata/attachment `name` | 必须为 `source.directory:leaf`，namespace 必须与来源一致 |
| attachment `revision` | 64 位小写 SHA-256；绑定规范化 manifest 贡献、实际 Skill 目录规范相对路径、leaf、完整主文档 bytes、副文件存在性/digest 与有效调用策略 |
| attachment `arguments/content` | 沿用原有上限、字面 `$ARGUMENTS` 和固定来源包装 |

原 local 的 user/project + easycode/agents/claude 组合、裸名与主文档 digest 行为保持；非 plugin 来源不得携带限定名，codex/agentplugins kind 不得用于 user/project。严格 decoder、metadata/result validator 都复核这些组合，不能仅放宽字符串长度。

集合根与单技能根共享 frontmatter 解析、展开和内容预算；由已验证 importer kind 选择上表的封闭命名/布局规则，只在确定 leaf 的纯逻辑上有显式差别，不复制 parser，也不使用 struct embed/shadow 模拟模板方法。SkillInput、SkillAttachment、SkillMetadata 和补全分别使用适合各自完整值/编辑前缀的校验器。

`/<plugin>:<leaf>` 与 `$<plugin>:<leaf>` 均是明确技能调用。语法合法的未知限定 slash 也返回 `skill_not_found`，已知但禁用的插件返回 `skill_unavailable`，避免加载失败后把明确调用当普通 prompt 发送。未知普通 slash 和 `/tmp/file` 仍按原行为作为正文；内置 `/plugins` 优先，裸名 plugins Skill 通过 `$plugins` 保持可达。

Local 的冻结解析状态必须同时包含最终技能项与已声明插件 namespace 的可用性，不能只从成功 Contribution 或 SkillMetadata 反推配置过哪些插件。manifest 合法 ID 但禁用/冲突的来源即使没有技能项也保留安全 namespace 状态，因此能返回 skill_unavailable；未知 namespace 返回 skill_not_found，合法 empty 插件的不存在技能同样为 not_found。该有界实例状态供 Prepare/Read 共用，不进入模型目录、Session 或授权记录。

### 5.1 Codex 调用策略副文件

仅 codex/agentplugins 技能读取其实际 Skill 目录内的 `agents/openai.yaml`，路径相对于同一已验证目录打开，不用 manifest 顶层 agents 字段，也不创建 Agent 能力。缺省 absent；存在时最多 64 KiB、YAML 深度 16，严格拒绝重复键、alias/merge/tag、null、错误类型和未知字段；父目录或副文件不安全/不可读时禁用该 Skill，不能当作 absent 或解析失败后放行。

支持 `policy.allow_implicit_invocation` 的严格 true/false，可缺省。有效 `DisableModelInvocation = frontmatter.disable-model-invocation OR (allow_implicit_invocation 显式为 false)`；sidecar 的 true 不能重新启用 frontmatter 已禁止的模型调用。UserInvocable 保持 frontmatter 原值。false 从模型目录移除，并拒绝模型自行加载主文档；当前 turn 已 durable 显式选择后的同来源资源例外仍沿用既有规则。这比 Codex 的仅隐藏 prompt 更严格，属于本轮明确的调用子集。

新读取通过来源校验后必须继续使用冻结的有效 metadata；不能让重新解析 SKILL.md 得到的原始布尔值覆盖 sidecar 的限制。原始 document digest、sidecar 绑定和 attachment revision 各自持有，不能把现有 localEntry.digest 全部替换成复合 revision 后再拿它校验原始文件。

sidecar 仅额外接受 interface 中 display_name、short_description、icon_small、icon_large、brand_color、default_prompt 的有界字符串注解（每项最多 2048 bytes，short_description 最多 1024）；不访问图标/URL、不插入默认 prompt、不新建未使用的核心字段。dependencies、policy.products 即使为空也报 skill_unsupported_field；不隐式安装 MCP/工具或推定 EasyCode 属于某个外部产品。格式错误为 skill_metadata_invalid，安全读取失败为 skill_metadata_unavailable。

发现时冻结副文件 absent/present 与原始内容 digest，形成 effective metadata 后再构建目录。每次新主文档/资源读取校验同一状态：增加、删除、改写副文件或把其路径改为 symlink 均使本会话返回 skill_source_changed；不热加载新策略。副文件 bytes 与主文档共用启动 16 MiB 额度。正文和资源输出使用同一复合 revision；导入不自动保存副文件原文，恢复不重新读取副文件。若后来通过合法的普通 resource 请求读取该文件，其正文按既有 Skill result 冻结，不新建旁路副文件记录。

因此副文件单独改变调用策略时，重启后的模型目录按有效布尔值精确变化；interface 注解仅改变来源绑定，不进入模型目录。没有修改的现有 local、easycode/claude 插件技能保持 SKILL.md 子集，不自动读取 Codex sidecar。

### 6. 来源读取绑定同一目录 FD，显式限制权限

插件来源 owner 在 Load 中取得并持有每个合法插件根的实际目录 FD，最多 32 个；失败/冲突来源及时关闭，New 不打开资源。绝对路径、实际根身份、选定 manifest 原始 digest 与读取绑定不进入 domain 展示值、Provider、Session、错误或缓存。新读取重新核对当前路径身份与所持根，再从该根 no-follow 打开 Skill 目录、主文档及副文件；资源必须从本次同一个实际 Skill 目录 FD 继续 openat，不能把插件根整体当作任意资源根，也不能检查一个 FD 后再按绝对路径重新打开。

每次新读取先验证原根身份及选定 manifest bytes 未变，再验证主文档与适用的副文件绑定；任何绑定变化统一返回 skill_source_changed，本会话不重新解析、重新选择或重新授权。已经 Prepare 的附件与已经 durable 的结果仍按冻结内容提交/恢复，排队不重新读盘。资源正文允许按既有规则在合法请求时读取当前文件，但必须绑定同主文档和插件贡献 revision。

`localfile` 只提供 descriptor 安全与有界读取；plugin 持有 manifest/source 根 owner；skill 持有主文档/副文件和资源语义。来源 Open 返回受控 SourceHandle，包含本次实际目录和恰好一次释放行为；不能只返回裸 os.File 而丢失借用计数。调用方对主文档、资源及错误路径 defer Close，Close 返回明确完成结果；不把 manifest 解析塞进 ReadArtifact 或执行器。

来源关闭采用一个 owner 级接收门：Open/Prepare/模型读取先在有界锁内取得借用，整个 owner 同时最多 16 个；超限立即返回 skill_source_busy，不读文件、不提交输入。全部 I/O/回调在锁外。在途名单只持 cancel/受控强制关闭函数与计数，不保存 context。

首次显式 Close 在锁内标记 closing、拒绝新借用，并恰好一次启动本实例唯一的 cleanup worker；New/Load 不为其启动隐藏 goroutine。worker 在锁外取消全部在途操作、执行受控文件 handle 关闭，等待固定 drained 信号后关闭 owned roots，保存最终结果并关闭固定 Done。最后一个读取释放只推进计数及 drained，不抢走 cleanup owner；文件关闭、读取栈和已发起的强制关闭均完成后才归还该借用。正常 Close 与强制关闭共享恰好一次的文件释放结果，不能把取消已发出或 Close 已开始当作完成。

每个 Close 调用只等待同一 Done 或自己的等待 context；超时返回不放弃 worker/原根所有权，worker 继续完成取消与强制关闭路径，只有真实 drained 后才清理原根。重复关闭不启动第二个 worker，不为每次等待创建 helper goroutine。这样即使某个文件关闭被确定性阻塞，等待方仍能按期限返回，并最终观察同一完成结果；所有状态锁内仍只允许有界内存转换。

ChatSession.Submit/Append 在进入 AgentLoop 前调用 PrepareInput，因此 ChatSession.Done 不是所有来源读取完成的证据。app 先停止宿主/Runtime，再显式关闭来源 owner 并等待其 Done，之后才能释放 artifact.Layout；超时沿用唯一 owner 和最终等待路径。测试必须阻塞一个尚未 admission 的 Prepare，同时执行 close，证明超时不丢 FD/所有权、恢复信号后完成清理且不提交输入。

静态技能来源沿用现有 Local Skill 的可读普通文件策略，不把文件 owner 或可写位解释为授权/可信标记；本轮不因此创建持久信任。私有存储自身的 0700/owner 约束继续由既有存储边界检查。

app 另外将私有 Session/artifact 存储作为禁止读取根注入来源边界：复用 artifact.Layout 现有 FD 祖先关系检查，校验本次实际打开的插件/技能目录与私有存储不相同、互不包含；检查后继续使用该 FD 完成读取。artifact 层只接收目录 FD，plugin 不依赖 Session；禁止只用字符串前缀判断。每次新读取重新执行边界检查，目录替换在确定性故障注入下失败关闭。不能借插件资源入口取得 ReadArtifact 之外的私有存储内容。

显式目录加载只允许披露技能 metadata/指令和同来源资源；不执行 `!`/反引号、`${CLAUDE_PLUGIN_ROOT}`、`${CLAUDE_SKILL_DIR}` 或环境表达式。正文里此类文字保持普通数据，不授予工具权限。手动/模型调用策略和已 durable 当前 turn 的辅助资源例外全部沿用现有 Skill 规则。

### 7. 快照、资源预算与故障隔离

Load 在临时实例中构建完整候选，再单次发布；`/plugins`、`/skills`、completion 和 SkillCatalog 来自同一结果，不分别扫描。Getter 返回独立副本，禁止共享调用方可变 slices。无热加载、无旧快照兼容分支；启动取消不发布部分结果。

| 上限 | 决策 |
| --- | --- |
| 插件参数/manifest | 最多 32 根；每 manifest 64 KiB；JSON 最大嵌套 16 层 |
| roots/原始枚举/候选 | 每插件最多 8 个去重 roots；每 root 最多枚举 1024 项，每插件累计最多 2048 项，全部插件最多 8192 项；每插件最多 256 个技能候选，全部插件最多 1024 个技能候选 |
| 启动读取 | 全部插件 SKILL 主文档与适用 sidecar 合计最多 16 MiB；逐个有界读取，正文解析后不在目录内保留 |
| 单技能 | 复用 64 KiB 主文档/资源、16 KiB args、128 KiB 包装后正文 |
| 合并可用技能 | 本地与插件合计最多 256 项，包含 manual-only；模型 metadata 仍最多 64 KiB |
| 诊断 | 插件诊断最多 256 项，溢出保留计数与安全 code；不保留正文/原始错误 |

先冻结既有本地获胜项，再按插件 ID ASCII 顺序原子加入每插件的合法技能集合。某插件使 256 项或 64 KiB 上限超出时，该插件整体 `plugin_catalog_limit`，保留本地和已接收插件，继续尝试其后的插件；不发布任意前 N 项。根/候选/累计读取超限时回滚当前插件，剩余受总预算影响的插件显式 unavailable，排序使结果可重现。全局资源额度计数也计入无效候选，防止错误文件绕过限制。

目录只读取上限加一项来检测超限，不能先 Readdir 全部再截断。原始枚举计费包含普通文件、不合法名称、无 SKILL.md 目录与重复项；技能候选计费包括格式错误项。所有 manifest 的无歧义 ID 与冲突先确定，再按 ID/规范相对路径顺序消费技能预算；对全局额度耗尽明确停止来源，不依赖 OS 返回顺序挑选部分条目。

这些 plugin 额度不会重定义本地 Skill 现有根级失败策略。若本地本身超限，其原有 unavailable/普通聊天可用语义仍成立。

### 8. app 装配与唯一消费者链

```text
CLI explicit roots
  --> app captured launch cwd
  --> restore/validate/reconcile old facts
  --> plugin.Load --> typed skill contributions
  --> skill.Local.Load(local roots + plugin contributions)
  --> one frozen metadata/list/read snapshot
        --> Runtime future sampling + existing Skill executor
        --> TUI /plugins + /skills + completion
```

Runtime、Provider、Session 不导入 `extension/plugin`，不解析 manifest。`extension` 的 host contribution 只含本轮使用的技能来源绑定；绝对 root 只存在于边界内部，没有 JSON 序列化入口。

真实边界按下表拆分，名称可在实施中遵循同义命名，但职责和生命周期固定：

| 边界值/接口 | 只承载的内容 | 实际消费者 |
| --- | --- | --- |
| domain 插件展示值 | ID、format、version/description、状态、数量、安全 code；纯内存校验和 clone | TUI 与 app，不含 FD/宿主路径 |
| extension.Contribution | 一个插件的安全身份/规范化贡献 revision 及已排序技能 roots；移除未来贡献字段 | app -> skill.Local.Load |
| extension 技能 root | 规范相对路径、安全 SkillSource、绑定 revision、窄 opener；没有序列化入口 | 共用 Skill 发现/读取 |
| 来源 opener / handle | Open(context.Context) 返回带实际目录与一次释放行为的受控 SourceHandle，显式 I/O；handle.Close 归还借用 | Skill 读取调用方关闭 handle；来源 owner 的 Close/Done 管理原根与全部在途读取 |
| app 存储边界 adapter | 对本次已打开目录 FD 执行已有存储身份/祖先检查，只返回安全分类 | plugin 的来源打开过程，不向下暴露 Session 类型 |

plugin 实现 opener 并持有私有路径/digest/根 FD owner；skill 只消费 extension 的窄边界，不反向依赖 plugin，extension 不依赖 app/runtime。localfile 增加相对实际目录 FD 的有界打开/读取，主文档/副文件与资源验证使用同一 handle；不把 context 保存到来源结构体。不引入带空实现的通用 registry、I/O manager 或其他未使用策略接口。

plugin.Load 输出无歧义声明 ID、manifest 来源分类/安全诊断与可扫描贡献，不能提前发布其最终技能计数；失败来源的安全 ID 仍传给 Skill 解析状态。skill.Local.Load 在有效策略、冲突、读取/目录预算和原子纳入全部确定后返回最终每插件汇总与 namespace 可用性；app 用 manifest 描述加该汇总纯内存生成 PluginListEntry，再同时发布 SkillCatalog、/skills、/plugins 和 completion。这样 catalog_limit 拒绝或零合法项不会在 /plugins 仍显示 available，getter 不反向扫描或修改来源。

目前 app 在 runtime reconciliation 前加载 local Skills。实施时将 local/plugin 快照装配统一放到 reconciliation 之后，并提供纯内存、启动前恰好一次的 `Runtime.InstallSkills`：安装 catalog/preparer，之后不能在活动、已启动或 poisoned Runtime 上替换。对应旧 Config catalog/preparer 初始化入口在同一 change 中替换并更新调用者，不保留双配置路径。既有注入的 Skill executor 仍指向同一个来源 owner，reconciliation 永不调用它；工具 schema 与执行能力不增加。

安装发生在 StartInteractive/AgentLoop.Run/RunTurn 和任何未来请求之前。来源检查、解析、fingerprint 均在锁外完成；安装只执行有界内存赋值。离线恢复成功后即使新插件损坏或没有再传 flag，历史仍完整可读；新目录只服务未来调用。

### 9. Claude 风格浏览，沿用现有编辑语义

`/plugins` 无参数，本地打开冻结列表：名称、可选 version、format、`Skills only`、available/partial/empty/unavailable/invalid/conflict 和安全诊断。未知 ID 的错误项以本地 CLI 参数序号定位，绝对路径不显示；有效项按 ID 排序，错误项稳定排列。

状态按 source 结果精确映射：读取/平台/资源额度阻断为 unavailable，manifest 非法或叶级错误导致零可用项为 invalid，插件 ID 冲突为 conflict；有合法技能且存在叶级错误为 partial，无候选且无错误为 empty，其他合法非空项为 available。重复叶名导致零可用项也属于 invalid，不误标为成功的空插件。

Enter 进入插件详情，列出可用/禁用技能及原因；↑/↓滚动，Esc 逐层返回并恢复打开前草稿。详情内选择可显式调用的 Skill 再按 Enter，仅把 `/plugin:skill ` 放入 composer。禁止模型自行调用的 Skill 仍允许手动选择；model-only、冲突或不可用项不可接受。不提供安装、启停、信任、重载按钮。

打开前草稿的含义严格对应真实入口：手动提交 /plugins 时保存完整命令草稿（含前后空白与光标）；从补全 Enter 打开时保存完成替换前的 /plug 等 composer 值，沿用现有 originalDraft/restoreComposer 接缝。本轮没有从任意其他多行任务草稿直接打开面板的新入口，不凭空承诺恢复那份不在内存里的文字。取消逐层返回并保持当前终端尺寸，不能恢复旧 layout；选择技能则原子替换当前命令草稿，超限时留在详情且保存原状态。

`/skills` 同时列出已有裸名和插件限定名，来源为安全 ID。补全增加第六个 builtin `/plugins`，保持最多 256 个 Skill 候选。完整限定名语法一层冒号；编辑前缀允许 `qa:` 和不超过完整长度的合法部分段，保持 ASCII 大小写无关搜索和已有排序。Tab/Skill Enter 只编辑；内置 Enter 走原本地命令分发。

两处技能选择共用纯内存调用文字 helper：插件限定名通常填 /plugin:leaf，裸名与六个 builtin 保留名冲突时填 $name。尤其 /skills 选择名为 plugins 的 local Skill 必须填 $plugins，不能再次打开 /plugins；同一规则涵盖 cache/compact/context/hooks/skills，补全与实际准备的保留名行为由回归固定。

Plugins panel 与 Skills/Hooks/问卷/批准/详情一样独占焦点，不能让补全处理其 Enter/Esc。保留 decoded paste、Ctrl+C、活动输入队列、超 4 MiB 原子拒绝和窄屏规则。View/导航/选择不读盘、不调用 Provider、不写 Session；所有 metadata 用既有可见转义与列宽约束。空列表显示当前没有本地插件及使用 `--plugin-dir` 后重启的说明，不伪装成安装向导。

用户路径示例：目录 qa-pack 包含 `.claude-plugin/plugin.json` 和 `skills/review/SKILL.md`。manifest 为：

```json
{"name":"qa","version":"0.1.0","description":"Project review workflows"}
```

SKILL.md 为静态文档：

```markdown
---
name: review
description: Review the requested local changes
argument-hint: "[scope]"
---
请按 references/checklist.md 中的清单审查：$ARGUMENTS。
```

启动 `easycode --plugin-dir ./qa-pack`，`/plugins` 查看 qa，进入详情选择 qa:review 后 composer 只有 `/qa:review `；用户补充 `current diff` 再提交才读取并冻结正文。也可直接输入 `$qa:review current diff`。模型按需读取清单时仍使用同一个 Skill 工具输入 `{"skill":"qa:review","resource":"references/checklist.md"}`，不能传插件绝对路径。

EasyCode/Codex JSON 示例可分别把同一 metadata 放在表中的 `.easycode-plugin`/`.codex-plugin` 位置；Agent Plugins 示例则在根 plugin.json 增加上述精确 `$schema`，其技能目录布局相同。这四份最小示例必须在实施时落成独立固定 fixture 并被真实入口测试消费，不能只留为说明文字。

### 10. 原生恢复与缓存契约

内部 Session/native/checkpoint 字段与 record kinds 保持当前结构；只扩展来源/名称合法值及交叉字段校验。新增固定的 plugin current fixtures，现有合法 fixture bytes 不因无关改动重写；测试禁止在运行时由 encoder 生成恢复事实。无 schema_version、migration、旧 reader 或 activation/install records。

两家 user_inputs 继续保存原文及独立附件，tool_outputs 使用既有 Skill result；恢复、缺口补偿和 transcript 不寻找插件，不联网、不重新展开或执行。冻结来源归属不代表恢复时允许新资源访问，只有当前已安装且匹配 source/revision 的来源才能满足新的工具请求。

区分三类内容摘要：

1. 来源检查的原始 manifest/main/sidecar bytes digest 及 sidecar 存在性：只服务变更检测，包含字节变化，不进入稳定 metadata。
2. 插件贡献 revision / 附件 revision：规范化 manifest 贡献、规范实际 Skill 目录、leaf、主文档与 sidecar 绑定及有效策略内容派生，无绝对路径、设备身份、时间或参数序号；manifest version 是不执行兼容判断的 metadata。
3. SkillCatalog revision/fingerprint：继续只覆盖实际发送的稳定 metadata，包括限定名称和安全来源，不包含正文、插件版本/诊断、原始 digest 或绝对路径。

只有实际发送的 Skill.Content 或可见 metadata 变化才使对应请求/内容分段变化。正文变化在新调用后改变历史尾部；仅 version、sidecar interface 或其他未发送的注解变化可以改变私有来源绑定/附件 revision，但相同 Content 与有效 metadata 下，模型 request bytes、history 内容指纹和稳定前缀必须保持相同，不能把检查 digest 注入 prompt 来强制失效。当前 `provider/*/user_input.go` 只发送附件 Content，`context/planning.go` 的 semanticSkillAttachment canonical 项也只保存 Role/Text，应保留此边界。metadata/name/有效调用策略改变，准确使 skill_catalog 分段失效；在相同 ToolCatalog/配置的比较中，tool_catalog/provider_profile/project_instructions 不变。UI 浏览/选择不改变任何 fingerprint。

同一当前目录快照下，分别比较不中断和恢复后的两家 canonical request bytes、native item 顺序、opaque reasoning、footprint、各分段与完整 fingerprint。删除插件后恢复，只要求历史附件/output bytes 不变；当前 SkillCatalog 缺失引起未来请求 prefix 改变是明确的来源变化，不错误宣称整个请求仍逐字节相同。OpenAI WebSocket 比较完整逻辑请求，再分别验证 full/incremental wire，不把不同传输帧说成相等。

### 11. 错误分类与可观测边界

来源 loader 只返回英文稳定 code：`plugin_root_unavailable`、`plugin_path_unsafe`、`plugin_storage_overlap`、`plugin_manifest_missing`、`plugin_manifest_invalid`、`plugin_manifest_unsupported`、`plugin_unsupported_field`、`plugin_unsupported_contribution`、`plugin_unsupported_layout`、`plugin_name_conflict`、`plugin_skill_conflict`、`plugin_root_limit`、`plugin_catalog_limit`、`plugin_platform_unsupported`。取消返回 context 取消，不转换为格式错误。

Skill 新读取继续使用 skill_source_changed、skill_unavailable、skill_not_found 和既有错误分类，sidecar 另有 skill_metadata_invalid/skill_metadata_unavailable；closing 来源的新调用返回 skill_unavailable，来源并发额度耗尽返回 skill_source_busy。调用失败由现有 Tool/输入错误路径处理；没有新模型工具、RuntimeEvent、headless 外部 JSONL 消息或 SQLite 表。TUI 可见诊断通过冻结列表提供，headless 不额外向 stdout 插入文本。错误、测试 snapshot 与日志均不回显自动加载的 manifest/sidecar 原文、宿主路径、底层 os error 或凭据。

### 12. 验证矩阵与退出条件

| 层次 | 必须证明的业务行为 | 主要落点 |
| --- | --- | --- |
| 引用/当前类型 | leaf/限定名边界、scope-kind-name 交叉验证、深拷贝、旧非法 shape fail closed | `internal/domain/plugin*_test.go`、`skills_test.go`、`internal/tool/skill_test.go` |
| importer | 四种固定 manifest，优先级/损坏阻断、默认/自定义根、所有错误形态、16 层及 bytes/项数边界 | `internal/extension/plugin/*_test.go`、静态 testdata |
| 来源安全 | 根/manifest/SKILL/资源 symlink 与替换、私有存储重叠、权限/特殊文件、取消后无 FD 泄漏 | `extension/localfile`、`extension/plugin`、`extension/skill` 的确定性故障注入 |
| 一份快照 | 插件与技能列表/补全/模型目录一致；重复 ID 全禁用；坏候选不回退；预算不破坏已有本地技能 | `extension/skill/plugin*_test.go`、`internal/app/plugin*_test.go` |
| 真实调用 | 两种显式语法、模型 Skill、同来源资源、manual-only 同 turn 例外；失败时 Provider/journal 无新增输入 | app + runtime 集成，复用现有 skills 测试服务器 |
| 双 Provider/缓存 | 固定 request/native goldens，源目录搬迁和顺序不变性，正文与 metadata 精确失效，原生 reasoning 保真 | `provider/{openai,anthropic}`、`context`、`app` cache/restore tests |
| durable/恢复 | ready/started/result/output 各前缀、删除来源、corrupt source/name 关联、Sync 失败/poisoned、真实进程 lease 和失败 journal bytes 不变 | 新固定 `session/testdata/current/plugin_*`、Session/app 回归；沿用现有 lease 子进程质量门 |
| CLI/headless | 各入口参数传递、picker 取消/预览不 Load、启动 cwd 绑定、未知限定调用拒绝、JSONL 无多余帧 | `cmd/easycode/*plugin*_test.go`、`internal/app/plugin*_test.go` |
| TUI/PTY | /plugins 列表/详情/选择/草稿、补全冒号、80x24/28x10/极小尺寸、NO_COLOR、paste、活动 turn、Ctrl+C/终端恢复 | `internal/tui/plugins_test.go`、固定 golden、`scripts/plugins-smoke.py` |
| 架构/全量 | 没有反向依赖、隐藏 I/O/启动、全局 registry、空贡献和新增 schema 占位；原有九工具/Hook/问卷回归 | `internal/architecture/*plugin*_test.go`、`make verify` |

所有模型交互用本地 fixture server，不使用真实 key 或默认外网。TOCTOU、取消和 Sync 故障通过现有检查点/阻塞握手注入，PTY 使用明确事件/窗口握手；不得用 sleep 或概率 race 宣称正确。

实现验收还须运行本地 CLI/PTY smoke、目标故障回归、严格 OpenSpec 校验与完整 `make verify`，记录命令、结果、平台和未执行范围。当前方案阶段的基线验证与未来功能验收分开报告，不能以现有测试通过声称新增功能正常。

## Risks / Trade-offs

- [受限 importer 不能直接加载所有外部插件] -> 面板常驻 Skills only，列举支持字段；显式未支持贡献整体报错；提供四种最小可运行 fixture，不对完整生态兼容作承诺。
- [命名空间影响原有 64 bytes 假设] -> leaf 与 reference 校验分离；覆盖核心 decoder、工具输入、completion、队列预算和窄屏截断，不只修改 UI。
- [manifest/root 检查与正文读取之间发生替换] -> 同一 FD 进行身份/存储边界检查及相对读取；确定性覆盖每个打开边界，不靠 Lstat 后 Open。
- [多个来源各自加载导致缓存漂移] -> 只有 app 发布一次快照，插件目录与 Skill 服务共享结果；冻结后新读取只校验原绑定，不刷新目录。
- [先加载未来插件阻塞旧 turn 恢复] -> reconciliation 在插件来源 I/O 前完成；未来 Skill 安装在启动前完成且不能替换活动 Runtime。
- [大型插件挤掉本地技能] -> 先保留本地合法项，再按 ID 原子接收插件；所有限制有失败诊断，不能发布不确定子集。
- [把外部 schema URI 当内部版本兼容] -> URI 只在 importer 识别外部格式；当前附件/JSONL 保持单一 shape，不增加内部版本字段或迁移逻辑。
- [平台差异] -> Darwin 为完整 PTY 验收平台；Linux 使用现有安全文件 adapter 做原生来源测试；没有等价 opener 的平台明确 unavailable，不跟随链接或退回普通 ReadFile。

## Migration Plan

没有用户数据迁移、安装状态或远端发布步骤。实施中直接替换当前占位、当前校验和相关调用入口；保留本来仍合法的固定 current bytes，新增插件当前 fixture。不能为原占位或旧开发结构保留兼容 reader。

验收通过后同步五项主 specs 与 README/能力清单/架构/Roadmap/pitfall，再按独立归档流程核对真实测试路径和词汇。本轮不会宣称 P6 全部退出。使用者移除 `--plugin-dir` 并重启即停止为未来调用提供插件技能，已提交历史仍离线可读；代码回退不等于旧程序能够读取新增 source 值，不添加未获授权的向后 reader。
