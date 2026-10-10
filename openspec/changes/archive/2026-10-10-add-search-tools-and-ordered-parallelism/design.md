## Context

动机与范围见 [proposal.md](./proposal.md)。当前实现已经具备可恢复的单一 `Read` Tool Loop，但关键结构仍以 `Read` 为中心：`tool.ReadyCall` 只保存 `ReadInput`，`InvocationResult` 只保存 `ReadResultMetadata`，Runtime 直接持有 `ReadExecutor`，Session ready/result payload 带有 Read 专属 revision，Provider result preparation 也只接受 Read results。此时先增加另一个顺序工具会把这些特化继续复制；先做通用“任意工具”框架又会违反未实现能力不得占位的约束。

当前 `Workspace` 已通过 `OpenWorkspace` 显式持有 root directory handle，并在 Darwin/Linux 使用 `openat`、`O_NOFOLLOW` 与 `fstat` 安全读取普通文件。搜索应扩展这一安全边界，而不是另起一个基于绝对路径、`filepath.Walk` 或外部进程的旁路。

版本审计没有发现实际同时服务历史用户的两套 reader；发现的是会诱发未来双版本的脚手架：Session 的 `ReplayOptional`/`OptionalRecords`、`LookupDescriptor(kind, version)`、Tool input/result revision 路由、纯内存 CachePlan/ContextPlan/RuntimeCommand 的固定 version，以及进程内 RuntimeEvent 的 version。相反，Provider native payload、Session envelope、headless JSONL 与 SQLite 都是真实跨边界格式，其单一当前 canary 需要保留以 fail closed。

参考工程只读核对如下：

| 参考 | 可借鉴 | 不照搬 |
|---|---|---|
| Claude Code sourcemap `a8a678cb...`：`GlobTool`、`GrepTool`、`toolOrchestration.ts`、`StreamingToolExecutor.ts`、`sdk-tools.d.ts` | 独立 Glob/Grep schema，Grep 的 content/files/count 形状，默认有界输出，只读 capability 可成批并行 | 依赖 `rg` 的路径式搜索、mtime/执行完成时序影响展示、工具在完整 sample durable 前启动，以及首个错误取消 sibling |
| Codex `c248f6d4...`：`core/src/tools/parallel.rs`、`core/src/session/turn.rs` | parallel-safe 取得读门、exclusive 取得写门；`FuturesOrdered` 按原调用顺序 drain | 让 shell `rg` 代替模型原生 Glob/Grep、跟随 symlink 的 fuzzy file search，以及缺少 EasyCode Session ledger 线性化点的直接执行 |

选择本模块作为当前阶段推进项，原因不是“搜索工具常见”，而是它具有最高的架构验证密度：

1. 它补齐 coding agent 最短探索链路 `Glob -> Grep -> Read`，立即提升真实仓库任务的完成能力。
2. 三个能力都只读、可取消、结果可有界，适合作为并行调度的最低风险首个消费者；不需要提前引入权限询问、写冲突或 shell sandbox。
3. 同一 sample 的异构工具组会迫使 Tool、Provider、Session、Runtime 与恢复链路真正泛化，能消除当前 Read 特化，而不是增加旁路。
4. 当前尚无历史用户，正是替换开发期 Session shape、删除版本路由成本最低的窗口；等 Edit/Bash/MCP 接入后再处理会把兼容面与状态空间成倍扩大。
5. 搜索结果会进入 Tool Catalog fingerprint、Provider golden、Session fixture 与恢复输出，能同时验证缓存稳定性、native history 等价和 ordered durability 三项架构硬约束。

## Goals / Non-Goals

**Goals:**

- 只对真实完成的 `Read`、`Glob`、`Grep` 建立封闭强类型能力集合，不引入通用 JSON 工具或未来工具占位。
- 在 workspace handle 安全边界内实现确定、可测试、有资源上限的文件发现与行搜索。
- 允许同一 sample 的只读调用有界并行执行，但让 invocation identity、Session seq、result/output 顺序和 Provider pairing 始终由原 call index 决定。
- 将取消、started admission、执行完成、durable result 和恢复补偿的线性化点写成一个可故障注入的状态机。
- 一次性删除开发期多版本路由面，并用架构测试阻止 V1/V2 DTO、decoder registry 和 optional Session record 回流。

**Non-Goals:**

- 不实现 Edit、Write、Bash、权限询问、exclusive scheduler consumer、MCP、Skill、Subagent 或 same-turn steer。
- 不读取 `.gitignore`，不支持 PCRE、multiline regexp、ripgrep type registry、任意 rg 参数、workspace 外目录或 symlink traversal。
- 不为搜索大结果引入通用 artifact store；本切片只保存有界模型 preview 与恢复必需 metadata。
- 不迁移旧开发 journal，不提供隐藏兼容 flag、旧 reader 或一次性命令行 migrator。
- 不改变外部 headless JSONL v1、Provider wire protocol、API URL 中的 `/v1`、UUIDv7、应用版本或内容 fingerprint 语义。

## Decisions

### 1. 以三个真实 capability 的封闭联合泛化 Tool 核心

`internal/tool` 增加 `CapabilityGlob` 与 `CapabilityGrep`，并为三种输入、invocation 和结果 metadata 分别提供 typed constructor、validator 与 getter。`ReadyCall` 和 `InvocationResult` 使用私有字段表达封闭 tagged union；对外只暴露 capability、共同 identity/status/preview，以及 capability 专属的安全 accessor。构造器保证任意时刻恰好有一种 payload，调用方不能构造 capability 与 payload 不匹配的值。

执行接口仍保持小而具体：`ReadExecutor`、`GlobExecutor`、`GrepExecutor` 各自只接受对应 typed invocation。一个实例持有的 `ExecutorSet`/dispatcher 通过 capability switch 组合它们；不存在包级 registry、`map[string]any` 或 revision switch。Catalog 从同一实例化 capability 集合生成 facade，只有 executor、schema、strict decoder、renderer 与 Runtime consumer 全部存在时才纳入 snapshot。

`CatalogRevision`、`ReadInputRevision`、`ReadResultCodecRevision`、`ReadRendererRevision` 及其 ready/result 字段删除。Catalog source revision 由稳定排序后的实际 facade name、description 与 canonical schema bytes 派生，Context 继续使用该内容 fingerprint 做缓存失效。

备选方案：

- 为每个工具复制 `ReadyCall`/Session/Provider 流程：改动小但立即产生三套状态机，拒绝。
- 暴露 `ToolInput interface{}` 或 `json.RawMessage` 到 Runtime：扩展方便但破坏核心强类型和构造期校验，拒绝。
- 先实现可注册任意工具的动态框架：没有真实消费者且会提前暴露能力，拒绝。

### 2. 搜索复用 Workspace root handle，并在平台边界实现 secure walker

`Workspace` 继续是应用唯一打开并负责关闭的资源 owner。`workspaceHandle` 在 Darwin/Linux 扩展为目录遍历原语：复制 root/child directory handle、读取目录项名称、相对当前 directory FD 以 `O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK` 打开 child，并立即 `fstat` 验证实际对象。generic search engine 只消费这些安全原语和规范相对路径；不持有绝对路径，不在 `Lstat` 与 `Open` 之间信任名称。

遍历先对每个目录的 entry name 做字节序排序，但最终仍对收集到的候选/匹配 workspace-relative path 全局排序；这避免 DFS 在 `a-x` 与 `a/y` 等路径上产生非全局字典序。Glob 与 Grep 都跳过 VCS metadata 目录，普通 dotfile 正常参与。条目在枚举后消失可记录为 skipped race；被替换为 symlink、非普通文件或非目录时不跟随。

unsupported 平台实现只返回 `unsupported_platform`，不会回退到路径式遍历。Windows 的等价 handle/reparse-point 实现必须由后续 change 连同确定性安全测试加入。

备选方案：

- 调用系统 `rg`：体验接近 Claude Code，但引入二进制可用性、参数注入、路径解析、取消/进程清理和 symlink 语义差异，且绕开现有 root FD，拒绝。
- `filepath.WalkDir` 后 `Open`：无法把类型/符号链接检查绑定到实际打开 handle，拒绝。
- 直接使用 Codex fuzzy search crate 的思路：它服务宿主补全而非模型工具，且其 symlink/排序契约不同，拒绝。

### 3. Glob 使用分段 matcher，Grep 使用行级 Go regexp

Glob pattern 与候选 path 均规范为 `/` 分隔。matcher 把 pattern 分段：普通 segment 复用 `path.Match` 的 `*`、`?`、字符类语义，`**` 通过带 memo 的动态规划消费零个或多个完整 path segments。输入限制为 4 KiB、最多 256 个 segments；非法字符类或 NUL 在执行前失败。可选 `path` 决定起始目录，但 matcher 始终面对完整 workspace-relative path，避免同一 pattern 因遍历根不同而改变含义。

Grep 在构造 invocation 时编译 Go regexp；`case_insensitive` 通过受控 flag 进入同一 RE2 语义，不拼接用户可控 shell。候选 path 先按可选 glob 过滤并全局排序，再逐文件顺序扫描，避免在一次 Grep 内叠加第二层无界并行。只接受不含 NUL 且完整 UTF-8 的普通文本；count 按匹配行计数，content 以路径/行号为主键，context 行只展示不增加 match count。

搜索预算由 executor 常量和值对象持有：深度 64、目录项 200,000、Grep 文件 50,000、单文件 16 MiB、累计 256 MiB、Glob 默认/最大 100/1000、Grep 默认/最大 250/1000、搜索 preview 64 KiB、展示行 500 code points。typed result 区分：已发现但因 limit/preview 省略的精确数量，以及因扫描预算耗尽导致搜索空间不完整的 reason；后者不伪造未知总数。

模型 preview 对有序 typed matches 做单独渲染。若 64 KiB 不足，预留 omission marker 后保留确定的前缀与后缀；Session 保存的就是 Provider 实际接收的冻结 bytes，恢复不重新渲染。Read 保持现有 256 KiB 上限，搜索使用更小的 capability 专属上限。

备选方案：

- 完整复制 ripgrep 语义：首个切片成本过高且会暴露 type/ignore/PCRE 等不稳定表面，拒绝。
- 找到 limit 后立即停止：快，但无法区分完整结果与预算提前终止，也会使 omission metadata 不可靠，拒绝；实现仅在全局硬预算触发时停止。
- mtime 排序：接近 Claude Code 的部分行为，但破坏 cache/golden 确定性，拒绝。

### 4. Runtime 采用“先 durable 准入，再并行执行，最后有序提交”

每个 completed sample 的处理顺序固定为：

```text
prepared sample
  -> [native commit, usage, ready(0..n)] 一次 Sync + sample finalize
  -> started(0), started(1), ... 逐个 Sync / 或分类为 pre-start cancelled
  -> 最多 8 个 parallel_read workers 写入固定 result slot
  -> result(0), result(1), ... 由协调 owner 逐个 Sync
  -> 一个按 call index 编码的 native tool_outputs commit Sync + finalize
  -> 下一 Provider sample
```

`readBatchScheduler` 是 Runtime 实例持有的策略，默认并发度 8，不是包级可变配置。当前三个 capability 都映射为 `parallel_read`；本变更不引入没有消费者的 exclusive 执行分支。未来写工具需要新的 OpenSpec 明确 batch partition 与冲突语义后才能加入。

调度器先分配与原 calls 等长的 slots。协调 owner 按 index 检查取消并调用 Session append；只有 `tool_execution_started` admission 被接受的 slot 才进入 jobs channel。jobs channel 由协调 owner 创建并关闭，固定数量 workers 从中取任务，每个 slot 只由一个 worker 写。协调 owner 用一次 `WaitGroup.Wait` 等待自己启动的 workers，workers 不写 Session、不 finalize Provider、不关闭共享 event channel。

取消的线性化点是 started append admission：之前观察到取消，slot 生成 cancelled result且没有 executor I/O；之后即使 context 已取消，也必须调用 executor 一次，让 executor 返回 cancelled/typed result。若 call k 的 started append 失败，k 及之后不执行；此前已 accepted 的 calls 仍执行并由 owner 等待。durability 无法确认时 Session poisoned，当前进程不尝试补写或下一 Provider sample，恢复依据已验证 journal 把 started/no-result 关闭为 `outcome_uncertain`。

工具的普通错误只占据自己的 slot，不取消 sibling。结果到达 channel 的顺序、goroutine 调度和实际持续时间都不进入 Session 或 Provider output。这样既采用 Codex 的 ordered collection 思路，也满足 EasyCode “事实先 durable、恢复不重放副作用”的更强约束。

备选方案：

- worker 完成即写 Session：吞吐略高，但 seq 和 Provider bytes 受调度影响，拒绝。
- 一个 started batch 原子提交全部调用：简化 I/O，但无法给每次 executor 接收建立独立取消线性化点，拒绝。
- 任一搜索失败就取消 sibling：类似部分 Claude Code 路径，但会丢失模型已请求的独立结果并复杂化 pairing，拒绝。

### 5. Session schema 直接替换为唯一当前 typed baseline

`Record` 保留 schema/payload canary、seq、identity、batch 与 checksum，删除 `ReplayRequirement` 字段；当前 vocabulary 中所有 records 都是 required。loader 先精确比较当前 canary，再按 `EventKind` 进入唯一 typed decoder。`LookupDescriptor(kind, version)` 改为只按 kind 的封闭 switch/lookup；unknown kind、任意 canary 不匹配或旧 shape 都在 repair/append 前失败，且没有 opaque optional list。

`SessionMetaPayload.SchemaRevision` 删除，因为 envelope 已提供当前 schema canary。Tool ready payload 保留 capability 并用 capability 专属 input union；result payload新增 capability，保存冻结 preview 与专属 metadata，删除 input/result revision。这样每个 record 可独立 strict decode，ReplayPlanner 再验证 result capability 与 ready invocation 一致。

parallel sample 的 seq 规则为：ready batch 按 calls 排列，started records 按 call index，result records 按 call index，最后一个 tool-output native commit 覆盖完整 call group。ReplayPlanner 拒绝完成顺序写盘、缺项、重复或 capability/call ID 错配。恢复仍不执行工具，只从冻结 previews 重建完整有序 output commit。

仓库 `internal/session/testdata/current` 直接替换；不创建 `v1/`、`v2/` 或 `legacy/` 目录。SQLite `user_version` 不匹配时删除并由 JSONL 重建索引，绝不修改 JSONL 事实源。

### 6. Provider 保持原生 history，只泛化有序 result group

Anthropic 与 OpenAI reducer 继续用同一 Catalog snapshot strict decode calls，并生成共享层的封闭 `ReadyCall`。Provider native sample commit 原样保存 Anthropic content blocks 或 OpenAI items；共享层不把 native call 转成统一消息再恢复。

两家 `PrepareToolOutputs` 从前一个 staged sample 的原生 calls 和有序 `InvocationResult` group 纯内存构造 output。它重新验证数量、call ID、capability、status 与 preview，但不读取 workspace，不依赖 Session codec，也不接受 result revision。Provider native envelope 的 payload canary 保留为单一当前值；它只做 exact check，不新增旧 reader。

每家增加三工具 catalog request golden、异构 parallel call/output golden，以及 uninterrupted/restored canonical request bytes、native item 顺序和 fingerprint 等价测试。opaque thinking/signature/redacted thinking/reasoning/encrypted content 仍只由本 Provider codec 保存。

### 7. 删除内部版本轴，保留真正边界与内容 revision

具体审计与动作如下：

| 位置 | 当前形态 | 动作 |
|---|---|---|
| `internal/protocol.Command` | 私有 `version`、`CurrentVersion`、内部 JSON decoder | 删除 version 与内部 wire decoder；headless 严格解码外部 stream-json v1 后调用 typed constructor |
| `internal/protocol.Event` | 进程内 event 携带 `Version`，headless 再投影 | 删除内部 Version；headless encoder 自己写外部 `version: 1` |
| `context.Plan` / `ContextPlan` | 固定 version 字段、getter、validator 分支 | 删除固定字段与旧版本错误分支；保留 source revision、segment fingerprint、estimator method |
| Tool catalog/call/result | `tool-catalog.v1`、`read-input.v1`、`read-result.v1`、renderer revision | 删除路由字段与常量；catalog revision 改为实际 schema/description 的内容 fingerprint |
| Session | required/optional、kind+payload version registry、metadata schema revision | 所有当前 facts required；canary exact check 后只按 kind 解码；删除 optional 与冗余 metadata revision |
| Provider native envelope | 单一 payload revision | 保留 exact canary；架构测试确保每家只有一个 codec且无版本后缀 DTO |
| Headless JSONL/stream-json | 已冻结外部 `version: 1` | 保留一个 strict decoder/encoder；版本不进入内部 command/event |
| SQLite catalog | `user_version` | 保留检测；不匹配时从 JSONL 重建，不增加 migration graph |
| project instructions/cache sources | schema/source revision 与 fingerprint | 保留内容语义和失效定位；禁止用它选择历史 runtime DTO |

`internal/architecture` 增加静态守卫，至少检查生产代码不存在 Session optional API、version 参数 decoder registry、Tool input/result revision API、内部 Plan/Command/Event 固定 version、V1/V2/Legacy 当前业务类型，以及按版本划分的 Session fixture 目录。守卫使用明确 allowlist 识别 Provider/Session/headless/SQLite 边界 canary与 `UUIDv7` 等非格式词汇，避免粗暴字符串禁令误伤合法概念。

同时更新根 `AGENTS.md`：发布前规则改为“直接替换唯一当前 fixture，旧开发数据 fail closed”；首次稳定发布后的兼容 fixture/migration 要求只有在另一个 OpenSpec 明确兼容窗口后才生效。总体架构、Tool 系统、Roadmap 与 pitfall log 同步相同词汇，消除现有文档一边要求 v1 migration、一边要求不保留旧 reader 的冲突。

### 8. 验证按安全、顺序和恢复三条轴组织

搜索单测覆盖 glob matcher、regexp modes、Unicode/长行、binary/非法 UTF-8、所有预算与稳定 preview。Unix 集成测试通过 hook 确定性替换目录项，验证 symlink/TOCTOU、非普通文件、权限拒绝和外部 bytes 不变；unsupported 平台验证 fail closed。测试不依赖真实 `rg`、sleep 或概率竞态。

调度器测试使用可控 executor gates 和故障注入 writer，证明最大并发 8、实际乱序完成但有序 durable、取消在线性化点两侧的差异、started append 在第 k 项失败、worker error 不取消 sibling、owner 等待全部 goroutine。race test 验证 slot 每项单写和无泄漏。

Session 固定 fixture 覆盖异构 calls、parallel completion 后有序 records、result/output 间崩溃、started/no-result、ready/no-started、旧 canary/optional shape 拒绝且 journal bytes 不变。app E2E 对两家 Provider 验证恢复前后 canonical request、native item 顺序、call/output pairing 和 catalog fingerprint 一致。

## Risks / Trade-offs

- [自研搜索语义与用户熟悉的 ripgrep 有差异] → schema 和 prompt 明确 RE2、单行、无 ignore/type/PCRE；golden 固定行为，后续扩展必须单独变更契约。
- [大仓库遍历带来 CPU、FD 与内存压力] → 全局条目/文件/字节/深度预算，目录 handle 及时关闭，单个 Grep 内顺序扫描，跨调用并发最多 8。
- [workspace 在搜索期间变化，无法提供快照隔离] → 每次访问绑定实际 handle 并防逃逸；消失/替换按稳定跳过或错误处理，结果 metadata 标识搜索不完整，不宣称文件系统快照。
- [“全部 started 后再执行”增加 Session Sync 次数与首结果延迟] → 它换取逐调用线性化和可恢复性；首个切片优先正确性，后续只有在不改变 ledger 语义时才能批量优化。
- [后续 admission 失败时，早期 accepted 调用仍必须执行，行为反直觉] → 状态机和故障注入测试明确 ownership；等待早期 workers 后 poison，恢复把缺 result 的 started 调用判为不确定，绝不重跑。
- [一次性 schema 替换使本地开发 Session 不可恢复] → 产品尚无历史用户；错误在任何修改前 fail closed，交付说明明确删除开发数据即可恢复，不用兼容代码永久承担成本。
- [大范围同时触及 Tool/Session/Provider/Runtime，review 面较大] → 实现按 tasks 的可编译阶段推进，但 Catalog 只在三工具全链路、恢复和 golden 都完成后一次切换；中间不得用 feature flag 暴露半成品。

## Migration Plan

1. 先更新工程约束与架构守卫，建立唯一当前契约的静态质量门；随后删除纯内存 version 和 Tool revision API，修复当前调用方。
2. 定义 Glob/Grep typed values、closed union 与三工具 Catalog；在尚未接入 app 前完成 matcher、renderer 和 cache fingerprint 单测。
3. 扩展 Workspace handle 和 secure walker，完成 Darwin/Linux 安全、预算、取消与 unsupported 平台测试。
4. 泛化两家 Provider 的 call decode/output preparation 与 native golden，但仍不允许未完成 executor 出现在应用 Catalog。
5. 一次性替换 Session envelope/tool payload codec、ReplayPlanner 和 `testdata/current`；删除 optional/legacy 路径，验证旧开发 fixture只读拒绝。
6. 接入 Runtime ordered admission/worker pool/ordered commit 和 resume reconciliation，完成故障注入与 race tests。
7. 最后切换 app wiring，使同一个已打开 Workspace 组合三个 executors并暴露完整 Catalog；更新 headless/TUI 投影、文档、Roadmap 与 pitfall。
8. 运行 `make verify`。如必须回滚，整体回滚该 change 的实现与当前 fixture；不通过新增兼容 reader 回滚。旧开发 Session 不做迁移，SQLite 索引由保留的 JSONL 重新生成。
