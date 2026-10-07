## Context

选择动机见 [proposal.md](./proposal.md)。当前实现有四个直接影响方案的事实：

- `internal/app` 在创建或恢复聊天资源前调用一次 `os.Getwd`，但随后只把该值作为 Session `creation_cwd` 与 continue 选择条件；当前没有项目文件发现阶段。
- `internal/context` 的 Planner 是纯内存组件，当前固定产生 `provider_profile -> committed_history -> current_input` 三个来源。CachePlan 已支持 `project_stable`，因此无需扩展缓存稳定性枚举或改变 canonical codec。
- `provider.Conversation.Stream` 当前只接收文本 `TurnInput`。OpenAI 与 Anthropic RequestCompiler 都直接使用各自 native history，并只把真实当前用户项交给成功 sample 的 finalizer；这恰好提供了插入“请求可见但不可提交”上下文的边界。
- Session 恢复只持久化 Provider-native commits，显式允许从不同 cwd 恢复且不自动 `chdir`。项目指令若进入 native commit 或 JSONL，就会把外部工作区状态错误地变成会话事实并破坏恢复模型。

本设计对两个只读参考工程进行了针对性对照：

- Claude Code 的 `restored-src/src/utils/claudemd.ts` 从项目根到工作目录加载项目记忆，并在 `context.ts` 中按会话缓存；`utils/api.ts` 的 `prependUserContext` 把它作为独立 contextual user message 放在真实消息前，而不是改写真实用户输入。其完整实现还包含 managed/user memory、`.claude/rules`、`@include`、外部文件授权和热更新，这些不属于当前切片。
- Codex 的 `codex-rs/core/src/agents_md.rs` 以项目根限制向上搜索，按 root-to-cwd 合并 `AGENTS.md`，使用 fallback 名称和 32 KiB 总预算；`context/user_instructions.rs` 将其编码为有标记的 user fragment，`context/world_state/agents_md.rs` 用 replacement/removal 状态避免新旧指令叠加。EasyCode 的进程级快照固定不热更新，因此不需要引入 world-state diff，但必须保留“独立上下文、不可混入真实历史”的语义。

## Goals / Non-Goals

**Goals:**

- 在不改变 Session schema、RuntimeEvent vocabulary 和 Provider-native commit codec 的前提下，打通一次性发现、强类型快照、ContextPlan、双 Provider request 和 resume 回归链路。
- 复用无内部依赖的 `internal/codec` 生成 canonical JSON，避免领域包直接持有 Sonic 配置或复制稳定编码规则。
- 让文件系统副作用停留在显式 `Load` 生命周期中；构造器、Planner、Provider getter 和序列化继续保持纯内存。
- 让无项目指令的调用保留现有请求 bytes 与 cache fingerprint；让有指令时的变化只由规范化内容、相对来源和版本产生。
- 用 descriptor-bound 读取满足 symlink/TOCTOU 约束，并提供确定性故障注入测试。

**Non-Goals:**

- 不建立通用 memory/rules/include 引擎，也不抽象未来 Skills、Hooks、MCP 或 Subagent 的目录发现。
- 不新增用户配置字段；初版使用版本化的 32 KiB 应用默认值，Loader 允许测试显式注入不超过 4 MiB 硬上限的其他正数上限。
- 不在进程内监控文件变化，不设计 replacement/removal diff，也不让 TUI 展示或编辑项目指令。
- 不把项目指令升级为 system/developer role，不启用 Anthropic `system`、`cache_control` 或 OpenAI `instructions` 字段。

## Decisions

### 1. 使用独立 Loader 和领域快照，启动时只加载一次

新增明确归属的项目指令加载组件，建议位于 `internal/context/projectinstructions`。`NewLoader(maxBytes)` 只校验参数并保存实例级策略；`Load(startupDirectory)` 才执行目录与文件 I/O。应用在取得启动 cwd 后、打开 Session repository/Catalog、构造 Provider 或进入 TUI 前调用一次 `Load`，再把快照沿现有装配链传给 Runtime。

不可变值建议放在 `internal/domain`，由 `ProjectInstructionDocument` 与 `ProjectInstructionsSnapshot` 表达。字段保持私有，通过 typed constructor 校验 schema revision、项目相对来源、UTF-8 内容、顺序、总预算与截断状态；所有 getter 深拷贝 slice/bytes。零值不作为有效非空快照使用，另提供显式空快照值，避免 Provider 把未初始化状态误当成可注入内容。

选择启动快照而不是每轮读取，原因是：同一 Runtime 的规划与请求必须使用同一事实；文件 I/O 不应进入锁内或 Provider 请求生命周期；进程级稳定性也与 Claude Code 的会话缓存一致。新进程 resume 会重新加载，因此用户仍可通过重启显式采用新规则。

备选方案是每轮热读文件，或把文件正文写入 Session。前者让相邻 turn 的 stable prefix 受并发文件变化影响并扩大 TOCTOU 面；后者会泄露工作区内容、引入 schema migration，并把外部上下文错误地当成历史事实，均不采用。

### 2. 通过目录描述符向上发现，并拒绝指令 symlink

Darwin/Linux Loader 使用 `openat`/`fstatat`、`O_CLOEXEC`、`O_DIRECTORY` 和 `O_NOFOLLOW` 从实际启动目录描述符向父目录遍历。每层只检查固定名称 `.git`、`AGENTS.md`、`CLAUDE.md`；`.git` 仅接受实际打开对象为常规目录或常规文件。候选读取通过当前目录描述符 `openat`，打开后以 `fstat` 再次确认常规文件，读取和类型校验始终针对同一 fd。非支持平台使用明确的 unsupported 实现并 fail closed。

候选优先级只把 `ENOENT` 解释为“不存在”；`AGENTS.md` 已存在但为 symlink、特殊文件、不可读或非法 UTF-8 时立即失败，不能通过回退 `CLAUDE.md` 绕过。`.git` 为 symlink 或特殊对象也按 unsafe 失败，避免越过一个伪边界继续读取父目录。错误经 fault 层映射为规范中的稳定英文 code，消息只使用清理后的项目相对来源。

选择拒绝 symlink 是有意缩小首版攻击面。Claude Code 会解析 symlink，Codex 在其 sandbox/executor 模型中允许部分 symlink；EasyCode 当前尚无 workspace sandbox capability，跨平台安全地允许 symlink 需要额外定义根内解析和 descriptor ownership，因此延期到独立变更。单独 `Lstat` 后 `Open`、`EvalSymlinks` 后按字符串前缀判断，以及复用 Session 私有路径 helper 都不采用：前两者不满足竞态约束，后者会把不同资源生命周期耦合到 Session 包。

### 3. 用版本化、内容寻址的渲染形成单一快照身份

Loader 先以 leaf-to-root 方式收集候选，遇到最近 `.git` 边界后反转为 root-to-startup 顺序；无边界时只保留启动目录。每层先决定 `AGENTS.md`/`CLAUDE.md`，再统一应用预算。应用默认值为 `32 << 10` bytes，与 Codex 的默认项目文档预算一致。

预算覆盖最终模型可见的规范化文本，包括固定头、JSON quoted 相对来源边界和文件内容。按 root-to-startup 消费预算；最后一个可容纳文件在 UTF-8 rune 边界截断，之后的文件不再加入。默认值为 32 KiB，可注入值的硬上限为 4 MiB；领域快照与 Loader 构造器都拒绝零值、负值和超过硬上限的值，避免极大整数绕过边界后触发预分配 panic 或进程级内存压力。快照记录 `truncated=true`，但该状态只进入 typed snapshot、ContextPlan 与测试诊断，不进入 RuntimeEvent 或 Session。

快照使用 `project-instructions-v1` schema 和 `project-instructions-prompt-v1` renderer。Renderer 使用固定 ASCII 头、JSON quoted 的 `/` 分隔相对来源及明确文档边界，绝不包含绝对项目根。revision 是 canonical 结构 `{schema_revision, renderer_revision, documents, truncated, byte_limit}` 的 SHA-256；mtime、inode、uid、cwd、发现耗时和打开顺序不参与。领域快照通过无内部依赖的 `internal/codec.MarshalCanonical` 生成稳定 bytes；`internal/codec` 是允许 domain/protocol 使用的基础设施叶子包，生产代码只有该包可以直接导入 Sonic。Provider request 使用同一已渲染文本，Planner cache segment 使用同一结构化快照，避免两条路径各自规范化后产生不同身份。

Loader 对指令文件进行流式 UTF-8 校验并只保留模型预算所需前缀。读取缓冲与保留区的预分配必须由固定小缓冲和已校验硬上限共同约束；`MaxInt` 等极大参数必须在构造阶段返回错误，不能传入 `strings.Builder.Grow` 或触发近似参数大小的分配。

相对来源必须参与 revision，因为同样正文位于根规则与子目录规则时语义范围不同。备选方案只 hash 拼接正文会发生来源碰撞；直接 hash 绝对路径会降低可移植性并泄露本机路径，均不采用。

### 4. ContextPlan 增加可选的 project-stable 来源

`PlanningInput` 增加项目指令快照并深拷贝。无文档时不产生 segment，保持当前三个来源、现有 canonical bytes 和 stable prefix fingerprint；有文档时固定顺序变为：

```text
provider_profile -> project_instructions -> committed_history -> current_input
stable             project_stable         turn_stable          volatile
```

`project_instructions` 使用 replace lifecycle，revision 取快照内容 revision，token 估算对实际渲染文本使用现有 `byte_heuristic_v1`。总预算将其与现有来源饱和求和。`ContextPlan` revision 升级，因为合法来源集合和顺序发生变化；通用 CachePlan 已能校验 `project_stable` 且没有持久化格式，因此不需要升级其 schema。

Planner 仍只消费传入值，不发现文件，也不向 Provider 生成 wire。备选方案让 Planner 读取 cwd 会破坏纯函数与可测试性；把项目内容并入 `provider_profile` 会让来源级诊断、失效原因和 token 归因丢失，均不采用。

### 5. Runtime 持有快照，并在一次 turn 中同时投影到 Planner 与 Provider

`runtime.Config` 接收不可变快照，`New` 校验并深拷贝。`RunTurn` durable 写入 `turn_started` 后，以该快照构造 PlanningInput；计划允许继续时，再把同一快照附加到传给 Conversation 的 Provider TurnInput。调用 Runtime 的 ChatSession、AgentLoop、headless 与 TUI 仍只提交真实用户文本，不能逐轮替换项目指令。

共享 `provider.TurnInput` 增加私有的项目上下文字段及 typed attach/getter，底层值引用领域快照并在边界复制。保留公开 `Text` 可减少现有宿主改动，但 Runtime 会覆盖任何非内部来源的项目上下文，保证应用启动快照是唯一 owner。Provider 只读该值；它不依赖 `internal/context`，依赖方向仍为 `domain <- provider` 与 `domain <- context`。

选择扩展 TurnInput 而不是扩展 `Conversation` 接口的第二个位置参数，可让“本次请求所需的完整输入”保持一个值并减少全部 fake Conversation 的签名迁移。选择由 Runtime 注入而不是宿主提交，可防止 follow-up queue 保存文件上下文或把它误认成用户输入。

### 6. 两个 Provider 各自创建临时 user context，不共享 wire

两个 RequestCompiler 都接收各自 native history、真实 user item/message，以及可选快照：

- OpenAI 在 `input` 最前插入一个由 `NewUserItem(renderedInstructions)` 生成的临时 context item。
- Anthropic 在 `messages` 最前插入一个由 `newUserMessage(renderedInstructions)` 生成的临时 context message；仍不发送顶层 `system` 字段。

随后依次拼接已提交 native history 和当前真实 user item/message。流归并与 prepared sample finalizer 继续只捕获真实 user item/message 和服务端 output；临时上下文不传入 consumer/finalizer，因此不会出现在 native history、commit codec 或 HistoryProjector。每轮都从固定快照重新创建一次，request 中始终只有一个副本。

这一选择同时对齐 Claude Code 的 contextual user message 和 Codex 的 user instruction fragment，也保留“产品行为优先对齐 Claude Code”的要求。使用 Anthropic `system`/OpenAI `instructions` 的备选方案角色语义不同，会扩大当前明确延期的 request surface；把文本与当前用户内容拼成一个 item 则会污染 durable user turn、TUI transcript 与恢复历史，均不采用。共享层只传 typed snapshot，不创建统一 Message，Provider-native 编译边界不变。

### 7. 恢复等价以当前外部快照相同为前提

项目指令不持久化。resume/continue 在打开或修复 journal 前已经获得当前进程快照；Session codec、ReplayPlan、native restore 和 `creation_cwd` 逻辑均不修改。相同 native commits、下一输入、Provider 配置和项目快照必须得到相同请求 bytes、native item 顺序与 cache fingerprint。

快照变化时，恢复的 Conversation revision 与 native items 必须逐 byte 保持原样；变化只反映在请求开头的一个临时 context 和 project-stable segment。`creation_cwd` 继续仅作 metadata/continue selector，既不用于重新定位旧项目，也不直接进入 fingerprint。这样可以把“会话事实是否恢复正确”与“当前工作区规则是否变化”分别验证。

备选方案持久化 snapshot revision 并拒绝变化，会让普通仓库规则更新导致 Session 不可恢复；自动回到 `creation_cwd` 加载则会隐式 `chdir` 并扩大路径权限范围，因此均不采用。

### 8. 历史 headless E2E 显式隔离启动工作目录

已有 headless 请求/恢复 E2E 的目的，是验证 Provider 原生历史、输出边界与 AgentLoop 一致性，而不是验证仓库项目指令。它们必须为直接装配路径和完整 `Run` 路径使用同一个独立临时启动目录，不能读取测试进程当前仓库的真实 `AGENTS.md`。专门的项目指令 E2E 继续使用显式仓库 fixture，从而让两类测试分别固定自己的输入来源，并避免开发者修改仓库规则时无意改变历史 golden 或 fingerprint。

## Risks / Trade-offs

- [根到叶预算可能截掉更具体的深层规则] -> 与 Codex 的确定性总预算和既定合并顺序保持一致，使用 32 KiB 默认值并显式记录截断；若真实项目证明需要优先保留深层文件，必须通过后续变更同时修改 spec、renderer revision 和 golden。
- [拒绝 symlink 会使少数仓库无法直接复用集中式规则文件] -> 返回明确 unsafe 错误，不静默忽略或跟随；等 workspace sandbox 与跨平台根内解析契约存在后再单独支持。
- [Anthropic 请求出现相邻 user messages] -> Messages wire 允许连续同角色消息，且参考 Claude Code 同样前置 contextual user message；golden 与集成测试固定实际序列，prepared history 仍只提交真实 user message。
- [指令文本增加 token 消耗并可能触发原本不会发生的 over-limit] -> Planner 对实际 renderer 输出计量，Provider 网络调用前给出既有 `context_limit_exceeded`，不会绕过预算或隐式丢弃历史。
- [项目文件内容出现在请求测试 fixture] -> 仅使用专门构造的非敏感固定文本 golden；普通日志、错误、snapshot 和 Session fixture 禁止记录真实项目正文。
- [启动发现发生在 Session 打开前，错误不会形成 journal 事实] -> 这是预期的 admission 失败；错误由 app 直接报告，确保不留下空 Session、Catalog row 或半恢复 writer。

## Migration Plan

1. 先引入纯内存领域快照、Loader 与确定性单元测试，不连接 Runtime。
2. 扩展 ContextPlan 并更新 plan/cache 回归测试；无文档 fixture 必须证明 bytes 与 fingerprint 不变。
3. 扩展 Provider TurnInput 和两个 RequestCompiler，更新有/无指令 golden，并证明 prepared native commits 不含临时上下文。
4. 在 app 启动装配中加载一次快照，再接入 Runtime；补齐交互、headless、resume/continue 与故障路径集成测试。
5. 同步总体架构、Roadmap 和 pitfall 记录，执行完整 `make verify` 后才可归档。
6. 收口 canonical codec 归属、4 MiB 硬上限和历史 headless E2E cwd 隔离，增加架构门禁并重新执行全部质量门。

该变更没有 Session/SQLite migration，也没有外部数据回填。回滚时可移除启动 Loader、Runtime 快照和 Provider 临时 context 分支；由于 journal 与 native commit 格式未变，已有 Session 仍可由回滚版本恢复。已由含项目指令版本生成的模型回复属于正常 Provider output，不需要也不得从历史中删除。
