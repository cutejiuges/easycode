# extension/local-skills Specification

## Purpose

定义本地 Skills 从用户及项目目录发现、按需加载、显式调用、来源隔离和交互展示的完整行为，使复用工作流具备可解释的选择规则、资源上限与失败语义，并在双 Provider 与会话恢复中保持一致。

## Requirements

### Requirement: Local discovery is bounded deterministic and source aware

系统 SHALL 在启动时发现用户根 `~/.config/easycode/skills`、`~/.agents/skills`、`~/.claude/skills`，以及最近 `.git` 根到启动 cwd 连续目录链中的 `.easycode/skills`、`.agents/skills`、`.claude/skills`。无项目边界时只检查 cwd。每根只检查直接子目录的 `SKILL.md`；不得扫描插件缓存或任意 home 树。项目优先用户、近目录优先远目录、同层 easycode > agents > claude。最高优先项非法时 MUST 阻断该名称，不回退低优先级项。

祖先链 SHALL 最多 128 层、候选项最多 1024、可用条目最多 256、metadata prompt 最多 64 KiB。超限根不得发布部分目录；其他合法根保持可用；合并目录超限时不发布可执行目录但普通聊天可用。候选遍历与目录输出 MUST 稳定排序，诊断有界且不含文件正文。目录生命周期固定到本会话，新增/删除在重新启动后生效。

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

系统 SHALL 接受 UTF-8 Markdown 和有界 YAML frontmatter，支持 name、description、argument-hint、user-invocable、disable-model-invocation、license、字符串映射 metadata。name 缺省为目录名，显式 name MUST 与目录名一致；名称 1–64 ASCII 小写字母/数字/单连字符，首尾为字母或数字。description 必填且最多 1024 UTF-8 bytes，frontmatter 最多 16 KiB，完整文档最多 64 KiB。布尔字段只接受布尔值，缺省 user-invocable=true、disable-model-invocation=false。

重复键、alias/merge/tag、错误类型、非 UTF-8 和未知字段 MUST 禁用该条目。allowed-tools/model/hooks/context/agent/paths/shell/effort 等未支持行为字段 MUST 报告 skill_unsupported_field，不静默忽略。args 最多 16 KiB，展开正文与来源包装最多 128 KiB，超限必须拒绝。参数只进行字面量 $ARGUMENTS 替换；无占位且有参数时追加固定参数段；不得执行 shell 插值、环境展开或文件 include。

#### Scenario: Reject a fork skill explicitly
- **WHEN** frontmatter 包含 context: fork 或 allowed-tools
- **THEN** 条目显示不支持且不能被显式或模型调用，不创建子代理或授权

#### Scenario: Keep argument expansion inert
- **WHEN** 参数包含 shell 元字符、反引号或环境引用
- **THEN** 参数作为普通文本加入正文，不启动进程，不展开环境变量

### Requirement: Reads stay bound to the discovered source

系统 SHALL 为每项绑定唯一来源身份和发现时完整主文档 digest；metadata revision 不依赖绝对路径、时间、inode 或调用身份。读取时 MUST 使用同一来源，以实际打开 FD 验证类型与范围，拒绝 symlink、特殊文件、目录穿越、绝对资源路径和跨根访问。主文档改变 SHALL 返回 skill_source_changed，不能回退其他来源或普通文件工具。

Skill 工具 SHALL 接受互斥的主文档加载 `{skill,args?}` 或资源读取 `{skill,resource}`，不接受任意 Skill 路径。资源只允许相对该 Skill 目录的 UTF-8 常规文件、最多 64 KiB；读取资源前同样验证主文档 digest。读取只产生文本，不授予写文件、执行或网络权限。

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

系统 SHALL 识别首词 `/<known-name> args` 和 `$name args`，只展开一个显式 Skill。内置命令优先，未匹配 slash 与绝对路径保持普通正文；未知 `$name` 返回 skill_not_found。正文中间、代码块中的名称不作自动展开。user-invocable=false MUST 阻止显式调用；disable-model-invocation=true MUST 排除模型目录并拒绝模型猜名调用。读取成功只说明指令可供当前任务使用，不等于用户批准后续副作用。

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

#### Scenario: Inspect and select locally
- **WHEN** 用户打开 /skills、选择一个可调用项并按 Enter
- **THEN** composer 出现调用命令，Provider 请求和 Session 写入次数仍为零

#### Scenario: Preserve a draft on cancellation
- **WHEN** 用户退出列表或取消尚未 admission 的加载
- **THEN** 原草稿可继续编辑，过期异步结果不会提交输入

### Requirement: Explicit durable selection permits same-turn supporting resources

对于 disable-model-invocation=true 的 Skill，系统 SHALL 只在当前 turn 已成功 durable 提交用户显式调用后允许模型读取该 Skill 同来源、同 revision 的辅助资源。该资格不得允许模型重新加载主文档，不得由 queued/prepared 输入、其他 turn 或模型猜名建立，不得提升写入/执行/网络权限。资格只从当前 turn 已提交附件派生，turn 结束失效，不保存独立授权 record；恢复历史不自动执行读取。

#### Scenario: Read references after an explicit selection
- **WHEN** 用户显式调用的附件在当前 turn 已 Sync/finalize，模型请求相同来源和 revision 的 references/example.md
- **THEN** 允许来源内有界读取，即使该 Skill 禁止模型自行调用

#### Scenario: Reject stale or uncommitted selection
- **WHEN** 显式输入仅 queued、来自旧 turn、来源/revision 不符，或模型请求主文档
- **THEN** 不能使用此例外，读取被拒绝且不回退其他通道
