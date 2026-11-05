## Context

选型动机见 [proposal.md](proposal.md)。调研基线为 EasyCode `7a38413`；无活动 change。Roadmap 总状态仍标记 P2，但实际已经完成多项 P3/P4/P5 纵向能力，不能仅凭阶段标签判断扩展接入条件。

现有 `internal/extension/{hook,skill,plugin}` 均为不可接入 Runtime/app 的批准占位。`internal/tool/catalog.go` 及 invocation spec 明确封闭为七项能力；`domain.UserInput` 只有原始正文，`runtime/input_commit.go` 已提供 prepared/preflight/Sync/finalize；`context/planning.go` 固定来源顺序。新增 Skills 必须贯穿这些契约，不能仅删除 TODO 或增加 prompt 字符串。

### 参考证据与取舍

参考目录实际位于 `/Users/cutejiuge/IdeaProjects/`，不是当前 worktree 的相邻目录。全部只读，无复制实现或构建依赖。

| 参考快照 | 已检查路径（相对参考仓库） | 吸收的行为与舍弃的实现 |
|---|---|---|
| Claude Code sourcemap `a8a678cb6244e6770e1e421767ff0987a1d95549`，仓库文档标注产品 2.1.88 | `restored-src/src/skills/loadSkillsDir.ts` | frontmatter、显式调用与模型调用分别控制、参数替换；不照搬全局 memoize、启动状态、宽松字段转换或 shell 插值 |
| 同上 | `restored-src/src/tools/SkillTool/{SkillTool.ts,prompt.ts}` | 目录先于正文、Skill 工具、完整调用结果、描述预算；不实现 fork、临时 allowed-tools 授权或远程发现 |
| 同上 | `restored-src/src/components/skills/SkillsMenu.tsx` | 按来源分组和稳定排序的 `/skills` 展示；用 Bubble Tea 现有宿主边界实现 |
| 同上 | `restored-src/src/utils/hooks/hooksConfigSnapshot.ts`、`utils/plugins/pluginLoader.ts` | 确认 hooks 信任层与插件贡献装配比纯本地 Skills 有更多前置依赖 |
| Codex `6f8bf683cee83ed4d580a2e106b587d8451db936` | `codex-rs/skills/src/parser.rs` | metadata 专用解析和名称上限；不采用错误 YAML 自动修复 |
| 同上 | `codex-rs/ext/skills/src/{catalog_prompt,sources,invocation}.rs` | 渐进披露、来源 authority、资源沿相同来源读取、显式 mention；不引入 cloud/executor provider、多来源 fallback 或遥测 |
| 同上 | `codex-rs/hooks/src/registry.rs`、`codex-rs/utils/plugins/src/lib.rs` | hooks 的独立运行期、插件命名空间与来源身份；不照搬构造期副作用、legacy 或迁移分支 |

本设计描述这些本地快照的取舍，不声称兼容两家最新产品全部行为。

## Goals / Non-Goals

**Goals:** 交付可在 TUI、一次性 headless 和 stream-json 调用的完整本地 Skills 切片；双 Provider 使用相同扩展语义但独立编译原生请求；加载、预算和恢复可通过无外网测试证明。

**Non-Goals:** 见 proposal；首批目录生命周期固定到本进程会话，修改后重启生效。读取到内容变化时显式报错，不悄悄刷新目录、回退来源或使用旧内容执行新调用。

## Decisions

### 1. 优先 Skills

| 选项 | 当前可复用基础 | 尚需补齐 | 决定 |
|---|---|---|---|
| Skills | 输入队列、Tool Loop、FD 安全读取经验、上下文预算、原生恢复 | 来源目录、正文读取、输入附件、UI | 首个切片，直接改善重复开发任务 |
| Hooks | Exec/审批、Runtime 生命周期 | 信任审查、事件顺序、阻断/改写、超时、外部副作用 ledger | 后续独立 change |
| Plugins | 仅贡献占位 | 实际贡献消费者、manifest、安装及依赖、信任和刷新 | 先拥有真实 Skill 消费者再推进 |

### 2. 有界本地发现与确定性身份

用户根按优先级为 `~/.config/easycode/skills`、`~/.agents/skills`、`~/.claude/skills`；不猜测 `CODEX_HOME`、不扫描插件缓存。项目从最近 `.git` 边界到启动 cwd 的目录链发现 `.easycode/skills`、`.agents/skills`、`.claude/skills`，无边界时只检查 cwd。每个根只枚举直接子目录 `<name>/SKILL.md`，不递归扫描任意树。

项目优先用户，较近目录优先较远目录，同层 EasyCode > agents > Claude。被遮蔽项仍在 `/skills` 显示来源和原因；最高优先项损坏时名称被阻断，不能静默选低优先项。名称使用 1–64 位小写 ASCII 字母/数字/单连字符，首尾为字母或数字，与目录名一致；不做 Unicode 模糊归一化。同名显示优胜来源；调用只接受目录中已解析的名称，不接受任意绝对文件路径。

固定上限：祖先链 128 层、枚举候选 1024 项、可用目录 256 项、frontmatter 16 KiB、description 1024 UTF-8 bytes、单份 SKILL.md 64 KiB、最终 metadata prompt 64 KiB。超限根不发布部分快照；保留其他合法根及安全诊断。最终 catalog 超限则不发布可执行 catalog，并保持普通聊天可用。排序后构造完整候选快照再发布，不截断指令正文。

稳定身份使用 scope、项目根相对目录、来源种类和 skill 名称；绝对 home/cwd、mtime、inode 不进入模型目录或 fingerprint。metadata canonical bytes 与 document digest 分离：目录 revision 只覆盖实际可见 metadata/策略；发现时以有界读取计算整份文档 digest，验证后丢弃正文，调用时再读并比较。这样目录可检测正文换包，同时正文不会预载到 prompt。新增或删除文件在本进程不热更新。

### 3. Frontmatter 支持矩阵

首批支持 `name`（缺省取目录名）、必填 `description`、`argument-hint`、严格布尔 `user-invocable`（默认 true）、`disable-model-invocation`（默认 false），以及只作展示信息的 `license` 和字符串映射 `metadata`。重复 key、非 UTF-8、YAML alias/merge、自定义 tag、非预期字段类型和未知字段禁用该项，错误不回显文件正文。

`allowed-tools`、`model`、`hooks`、`context`、`agent`、`paths`、`shell`、`effort` 等行为字段均报告 `skill_unsupported_field`，不忽略。正文只替换字面量 `$ARGUMENTS`，不解释 shell、环境变量、位置参数或反引号；动态 shell 预处理不会执行。无占位符而存在 args 时，追加带固定标记的参数段。替换后的全文重新验证 UTF-8/字节预算。

### 4. 小组件组合与安全 I/O

`extension/skill` 分为 parser、catalog、local source、invocation preparation，实例持有状态，无万能 ExtensionManager。domain 仅持有已验证 metadata、引用及附件值，不导入磁盘代码。tool 声明 Skill executor 的窄接口和强类型 input/result；app 以 adapter 组合本地 source，不建立 tool -> extension 的跨层依赖。runtime 负责排队/提交和工具调度；tui 只读宿主投影。

所有 `New*` 为纯内存；`Load/Read/Prepare/Close` 为显式生命周期。local source 用 descriptor-relative no-follow 打开与实际句柄类型/大小校验，拒绝 symlink、特殊文件、`..`、绝对资源路径和越界；macOS 已知系统别名采用已有工程同等策略，不能普遍 realpath 后信任。可复用现有安全 walker 的有界纯逻辑；只有实际共用时才抽取有明确文件职责的底层包，禁止让 extension 导入 session 或 tool/builtin。

Skill 工具参数 `{skill,args?}` 加载主文档，`{skill,resource}` 读取该 Skill 内相对文本资源；args 与 resource 互斥，strict decoder 转为封闭两分支。资源上限同为 64 KiB，不提供目录扫描或脚本执行。args 最多 16 KiB，展开正文与包装最多 128 KiB，超限在读取后的纯内存准备阶段明确拒绝。读取资源前验证当前主文档 digest；所有读取由 catalog 绑定的唯一 source 处理，来源失败不得绕到普通 Read 或 Exec。资源读结果明确标为资源，不伪称已经激活 Skill。

本次采用 dependency inversion 的窄执行接口和不可变快照，复用既有 Command/Repository 生命周期；不引入模板继承、全局 registry 或第二套调度器。

只读 Skill 加载本身不新增持久审批；Skill 是任务指令而不是权限许可。修改文件、运行脚本等仍走原工具权限与 sandbox。项目 metadata 是不可信描述，在 prompt 中编码/隔离，不拼接成高优先级指令。未实现的 allowed-tools 不产生授权。

### 5. 一套准备服务，两个调用入口

`/skills` 是本地控制操作，显示可用、隐藏、被遮蔽、损坏和不支持条目；按来源分组，显示名称、描述、参数提示与诊断，不调用模型。Enter 将 `/<name> ` 放入 composer，用户再提交；Esc 返回原草稿。内置 `/context`、`/cache`、`/compact` 优先。未匹配的 slash 和绝对路径保持原文，首词 `$name` 是显式 Skill 请求，未知名称报告错误。正文、代码块、粘贴中间的 `$` 不做全局正则替换。

显式输入在进入 AgentLoop admission 之前，由宿主唯一 owner 的 cancellable preparation 操作安全读取，生成原始用户文本加独立 `SkillAttachment`；每条最多一个显式附件。TUI 用 tea.Cmd 和请求身份丢弃过期结果，串行提交保持用户顺序；headless/stream-json 复用准备服务。准备失败保留原草稿，不写 journal、不请求 Provider。构造器、getter、View 与 AgentLoop admission 不做 I/O。

附件包含 stable source identity、name、document revision、参数及一次生成的完整展开正文；UserInput.Text 始终是用户输入。附件和原文共同占每条 4 MiB、队列 16 MiB 及 native/batch 编码预算；无需新增独立 Skill 队列。queued 后文件修改不改变已准备内容；取回/returned tray 只恢复用户原文，重新提交才重新读取。调用 `user-invocable:false` 的显式请求失败；`disable-model-invocation:true` 的项不进入模型目录，Skill 工具猜名字也不能加载主文档。唯一例外是当前 turn 已 durable 提交用户显式调用后，可读取同来源、同 revision 的辅助资源；queued/prepared 输入不构成授权，其他 turn、主文档加载、来源变化均拒绝。资格由当前 turn 已提交输入附件投影派生，随 turn 结束失效，不新增持久权限记录或永久 active flag。

模型入口是第八项能力 `skill.read`，两家 facade 均名为 `Skill`、structured JSON。复用 ready -> started -> result -> native outputs，不造用户输入、不凭空构造模型 tool call。只在 ToolCallReady 和 durable started 后读取，按 call index 冻结结果。成功 UI 展示 Skill 名称/来源与加载状态，全文仅在详情查看；只有结果 durable 后才报告加载成功。

### 6. 原生历史、预算和缓存

显式调用由各 Provider 把原始文本和有来源标记的附件编码为独立有序原生内容块，仍属于同一真实 InputID。附件不能伪装成用户亲手输入的正文，也不写 system 权限指令。冻结的原生包装连同来源一次持久化；恢复不再执行参数替换。SemanticHistoryView 用户 recall 保留原始文本，附件需要独立只读投影用于详情/估算，不以其反向构造请求；对应 history-projection delta 与 native delta 同步定义。

模型调用冻结完整 Tool result，恢复补齐 outputs 只使用 result bytes；新增工具不得绕过既有不自动重跑规则。Skill 读取成功但结果未 durable 的崩溃，沿用 started 未完成的 uncertain 补偿。Session envelope 仍无版本，native kind 保持 user_inputs/sample/tool_outputs，不增加旁路 activation 日志。

目录作为 `skill_catalog` project-stable source，位置在可选 project_instructions 之后、committed_history 之前；空目录省略，schema 本身作为稳定 tool catalog 一部分。metadata 与正文分开计量，加载正文只进入历史尾部；不把正文、动态路径或调用 ID 放入稳定 catalog。`/context` 和 `/cache` 显示来源估算及 revision 变化，不显示正文。

Skill 完整成功 preview（正文、来源、参数包装）最多 128 KiB，同时服从 sample 512 KiB/turn 2 MiB 及既有每剩余调用 1 KiB 预留。额度不足返回小型 `result_budget_exceeded`，不返回部分成功正文，不通过 artifact 隐藏缺失指令；剩余结果仍严格配对。显式附件则经候选历史预算检查及自动压缩既有流程，失败不提交正文。

启动/恢复重新发现当前目录只影响未来调用；历史附件始终使用冻结内容。恢复等价断言要求相同当前目录/配置；当前目录确实变化时，稳定分段发生可解释变化，不能谎称整条请求不变。Skill 已加载正文在 compaction 后随既有摘要规则处理，不从磁盘自动复活旧指令，也不新增永久 active flag。

### 7. 验证矩阵

| 层级 | 必须证明的行为 |
|---|---|
| parser/catalog | 缺省/类型/重复 key/未知行为字段；固定优先级、损坏遮蔽、排序、数量/字节上限、getter 深拷贝 |
| local source | 同 FD 校验、symlink/TOCTOU 确定性替换、特殊文件、资源越界、主文件更改、取消、无跨来源 fallback |
| input/host | 三入口一致、原文保留、冻结队列、取回重读、取消与 admission 竞态、未知首词、slash 保留、全额编码预算 |
| Tool/Provider | 两家 request/stream golden、partial args 零读取、重复 call、资源分支、错误配对、有序额度不足、完整成功正文 |
| native/session | 两家固定 current fixture；Sync 失败、result/output 缝隙崩溃、文件删除后恢复、无重读无重跑、opaque bytes 与 item 顺序保真 |
| cache/context | 相同相对内容迁移绝对目录指纹不变；metadata 变更可定位；正文只变历史尾部；restored/full canonical bytes 等价，OpenAI incremental 单独 golden |
| TUI | 固定大小正常/窄屏/空列表/错误/遮蔽 snapshot；Enter/Esc/草稿/工具详情与过期响应 |
| 工程 | `make verify`；占位退出与依赖 AST 守卫；全部 current fixture 固定 bytes，测试不读真实 home 或访问外网 |

## Risks / Trade-offs

- [常用第三方 Skill 含 hooks/fork/allowed-tools] → 明确禁用并展示原因，首批不宣称全兼容；避免静默丢失约束。
- [文件在发现后变化] → 主文档 digest 检查失败，不热更新；已入队/已提交内容冻结。资源正文按调用时读取并冻结。
- [调用接口改动影响范围较大] → 保持当前八能力唯一契约，完整更新构造器/decoder/validator/fixture，不引入旧 reader。
- [只读加载影响模型后续操作] → 目录和附件有来源标记、既有权限不提升，危险操作仍由工具策略约束。
- [指令完整性与小预算冲突] → 全文成功或明确失败，以可解释拒绝替代半份指令。
- [目录发现增加启动 I/O] → 有界根/项/bytes、显式生命周期、无 watcher；未请求 Skill 时不加载正文到模型上下文。

## Migration Plan

仅调整开发期唯一当前契约与 `testdata/current`，无生产数据迁移、版本门禁或 append-only 原地改写。实现和验证完成后同步主 specs、架构/Roadmap/pitfall/README，再由归档流程处理 change。回滚采用整项代码与当前 fixture 一起回退；不承诺新 Skill 历史由旧二进制读取。
