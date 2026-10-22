## Context

参见 [proposal.md](./proposal.md) 的动机。当前实现已经具备四个可复用基础：Provider-owned native history、Read/Glob/Grep 的强类型 Tool Loop、以 JSONL 为事实源的 durable ledger，以及由 `AgentLoop` 单 owner 串行化的输入控制。缺口集中在三个边界：Catalog 只能表达 structured schema，Policy 只有静态 allow/deny，Runtime/TUI 之间没有可暂停并关联恢复的 approval 协议。

本设计同时受以下约束塑形：

- Anthropic `Edit` 是 structured JSON，而 OpenAI `apply_patch` 是 custom freeform；共享层必须得到同一个 typed `fs.patch`，但不能抹平或互转原生 wire。
- approval 只回答“用户是否同意这个确定候选”，不能替代 workspace containment、symlink/文件类型检查、基线冲突检测或 durable execution admission。
- Session 是 append-only 事实源；崩溃恢复只能补偿，不能重新询问、自动重试 Patch 或从当前文件反推既有副作用。
- 交互体验采用 Claude Code 的 diff-first modal、默认安全选项与拒绝反馈；内核采用 Codex 的 approval/sandbox 分离、typed decision、拒绝与中断分离，以及 Provider-native custom tool 表达。
- 当前工程已依赖 `golang.org/x/sys` 并具有 descriptor-relative Session/Workspace 安全实现，本变更复用这一平台边界，不增加运行时依赖。

## Goals / Non-Goals

**Goals:**

- 形成从 Provider 原生调用、typed Patch、候选准备、交互批准、durable ledger、安全替换到原生 output 的单一闭环。
- 使每个可见 diff 精确绑定即将写入的 bytes，并使每个副作用都能由 candidate fingerprint、approval fact、execution-started 与 result 审计。
- 在并行只读工具之间插入真实 exclusive Patch 屏障，同时维持 call/output 顺序和崩溃后的 at-most-once 自动执行。
- 让 TUI 只投影 Runtime 给出的决定，不拥有 Policy、文件系统或 Session 写权限。

**Non-Goals:**

- 不建立通用 permission rule engine、sandbox broker 或跨工具风险分类框架；当前 session grant 仅覆盖同一 Runtime/workspace 的 `fs.patch`。
- 不抽象多文件事务、任意文件 mutation AST 或通用 artifact；首版只支持一个既有 UTF-8 常规文件的 replacement plan。
- 不在恢复时继续旧 Tool Loop，也不实现 same-turn steering。拒绝反馈作为当前 call 的 result 交给下一次 Provider sample。
- 不承诺保留 ACL、扩展属性、硬链接拓扑或所有权；首版只保留规范要求的受支持权限位，原子替换会产生新 inode。

## Decisions

### 1. 扩展现有封闭联合，而不是引入动态 Tool 注册表

`internal/tool` 增加 `CapabilityPatch`、`PatchInput`、`PatchInvocation`、`PatchResultMetadata`，并把 `ReadyCall`、`Invocation`、`Result` 扩为四能力封闭联合。所有新增值保持私有字段、typed constructor、clone/getter 和 exhaustive `Validate`；核心边界不引入 `any`、动态 map、revision router 或包级 registry。

Catalog 的 `Facade` 增加封闭 `WireKind`（`structured`、`custom_freeform`）以及与 wire 匹配的 definition 联合：structured facade 持有 canonical JSON schema，custom facade持有稳定 freeform format description。排序键固定为 capability ID、wire kind、facade ID；canonical bytes显式编码wire kind。Catalog构造器接收Read/Glob/Grep/Patch真实组件并一次性生成八个facade，继续执行“缺少消费者即不暴露”。

选择封闭联合是因为当前能力数量小且工程明确要求单一当前契约；动态 registry 会把 capability/input/wire kind 的不匹配推迟到运行时，并诱导包级可变状态。替代方案“把 OpenAI apply_patch 伪装成 function schema”会偏离原生 Responses wire并破坏 custom call/output pairing，因此不采用。

### 2. Provider 只负责 wire 归并，Patch parser 负责无副作用规范化

Anthropic reducer 延续 indexed content-block 状态机：完整 `Edit` JSON 经 strict decoder 生成一个 replacement。OpenAI reducer增加 custom-call draft，以 call/item identity 归并 input delta/done；完成后把完整文本交给 `internal/tool` 的 strict patch parser。parser只接受一个 `Update File`、完整消费全部输入并生成有序 non-overlapping replacements，不读取 workspace。

Provider native entry显式保存原call wire kind。OpenAI sample/output使用 `custom_tool_call`/`custom_tool_call_output`，Anthropic继续使用 `tool_use`/`tool_result`。共享的 output preparation根据 durable ready fact保存的wire kind分派到Provider codec；approval事实永不进入native history。

解析与文件匹配分两阶段，可确保stream reducer无I/O、partial delta无副作用，并使同一typed plan在候选准备时才根据真实base验证exact match。替代方案“边收delta边解析/读文件”会让失败sample产生不可审计I/O和不完整候选；“先统一成JSON”会丢失OpenAI custom wire，因此均不采用。

### 3. Patch candidate 是执行前的不可变能力票据

在 `internal/tool/builtin` 增加由 workspace handle 构造的 `PatchPreparer` 和 `PatchExecutor`。`PatchPreparer.Prepare` 仅在ready batch durable后调用，并完成：

1. 规范化workspace相对路径，使用父目录handle逐级`openat(..., O_NOFOLLOW)`，由`fstat`确认最终对象为可读写常规文件。
2. 在16 MiB边界内读取完整UTF-8 base，对typed replacements执行exact match与重叠校验。
3. 计算final bytes、base SHA-256、稳定文件identity/metadata evidence、完整unified diff和changed-line summary。
4. 以capability、typed input、规范路径、base evidence、final SHA-256/final bytes摘要派生candidate fingerprint；时间、随机ID、绝对路径与UI状态不参与。

内存中的 `PatchCandidate` 持有执行所需base evidence与final bytes；RuntimeEvent只取得相对路径、fingerprint、完整有界diff和安全摘要。Session approval fact保存fingerprint而不复制文件正文；若进程退出，candidate自然丢失，恢复路径也明确禁止执行。

候选diff使用仓库内实现的确定性line-based unified diff，固定context、换行与header格式，不调用外部`git diff`。完整diff超过256 KiB即在approval前失败；不允许截断后批准。这样展示内容、fingerprint与执行内容一一对应。替代方案“批准原始patch文本”无法证明实际匹配结果；“批准后再算diff”使用户看不到真实影响，均不采用。

### 4. 批准后重新验证，再通过同目录原子替换提交

`PatchExecutor.Execute`只接受`PatchCandidate`，且调用点必须已经durable写入matching allow与execution-started。执行器重新从workspace root handle定位父目录与目标，比较路径、类型、可写性、base hash及平台可用identity；不匹配返回`patch_conflict`。

Unix实现使用现有`x/sys/unix`模式：在父目录handle下以不可预测名称和`O_CREAT|O_EXCL|O_NOFOLLOW`创建私有临时常规文件，写满final bytes，应用保留的权限位，`fsync`临时文件，`renameat`覆盖目标，再`fsync`父目录。临时文件由执行器唯一owner关闭并在rename前失败时`unlinkat`。不具备等价descriptor-relative保证的平台通过build-tag实现返回稳定unsupported错误，而不是退化到`Lstat`+`os.WriteFile`。

rename是副作用线性化点：之前的确定失败保证原目标不变；之后的取消或目录Sync失败返回committed/durability-uncertain结果并poison当前Session，禁止自动重试。选择原子替换而非原地truncate/write，是为了避免部分文件；代价是新inode及不保留ACL/xattr/硬链接语义，已作为首版边界明确。

### 5. approval 由 Runtime turn owner管理，Policy不持有UI回调

Tool Policy返回`allow`、`ask`、`deny`的强类型值。Patch `ask`包含内核允许的decision集合，但不包含channel或函数。Runtime为candidate分配`ApprovalRequestID`，记录一个`activeApproval`，发布`tool_approval_requested`事件，并在同一turn owner控制下等待response、interrupt或shutdown。

`AgentLoop`增加`resolve_tool_approval` command。owner校验expected turn、request、invocation与fingerprint后把response交给当前turn专属有界通道；相同command request ID通过既有recent-request ledger去重。channel由turn owner创建和关闭，Runtime不为每次等待创建helper goroutine。command一旦被owner接收，调用方context不再撤销它，最终由turn给出durable outcome或失败。

Policy拥有一个Runtime实例级`PatchGrantSet`值；`allow_session`只写入该实例的workspace-wide Patch grant。grant命中仍生成candidate并写`allow_from_session`事实。grant不进入Session、配置、Catalog或fingerprint。相比让TUI直接调用executor，这一设计保持headless、TUI和未来宿主共用相同安全/持久化路径；相比把approval responder放进Tool层，它避免副作用层反向依赖协议/TUI。

### 6. 以调度屏障顺序准备和批准，而不是预先批准整批调用

Runtime把call group切为连续`parallel_read`批次和单个`exclusive` Patch。按call index推进：只读批次先durable写入全部started，再以最多8 workers执行并按index写results；到达Patch时才准备候选、评估Policy、等待approval、写approval fact，并根据结果拒绝或独占执行；Patch result durable后才进入后续批次。

核心状态如下：

```text
Read/Glob/Grep: ready -> started -> result
Patch allow:    ready -> approval(allow*) -> started -> result
Patch deny:     ready -> [approval(deny) + result(denied)]
Patch invalid:  ready -> result(error)
Patch abort:    ready -> approval(abort) -> cancelled group -> turn_failed
```

选择在屏障处准备候选很重要：如果同一sample连续修改同一文件，第二个diff必须建立在第一个Patch已提交后的base上。预先准备/批准整批Patch会展示陈旧diff，并导致可避免的conflict。所有results仍按call index durable；worker完成顺序不改变ledger或Provider output。

### 7. approval response、durable decision和执行准入是三个不同线性化点

用户按Enter只完成Runtime内存中的response admission；它不等于持久批准。到达该call slot后：

- `allow_once`、`allow_session`、`allow_from_session`先单独append/Sync `tool_approval_decided`，再append/Sync `tool_execution_started`，之后才调用executor。
- `deny`把`tool_approval_decided`与`tool_call_result(denied)`放在同一batch，零started、零executor。
- `abort`记录approval outcome，随后由现有取消owner为未完成slots生成有序补偿output，并以唯一`turn_failed`收口。

这三个点分离后，崩溃语义可精确判断：没有approval fact表示用户response未成为事实；有allow无started表示明确批准但执行未被接收；有started无result表示结果未知。替代方案“用户点击即执行、事后写日志”存在不可审计副作用窗口；把approval event写入Session则会持久化UI瞬态且无法表达最终决定。

### 8. Session直接替换为包含approval与wire kind的唯一当前schema

Session新增`tool_approval_decided` kind，并扩展ready/result payload保存Patch typed input、Provider wire kind与Patch metadata。schema/payload canary及`testdata/current`整体替换，不保留旧reader。ReplayPlanner按capability验证上述状态机和exclusive屏障；Provider output还必须匹配call ID与wire kind。

resume reconciliation只消费durable facts：无approval的Patch补`session_interrupted_before_approval`；allow后未started补`session_interrupted_before_execution`；started无result补`outcome_uncertain`；已有result则从冻结preview与wire kind重建output。恢复不构造candidate、不访问workspace、不显示overlay、不恢复grant。这样 uninterrupted与restored request在相同外部snapshot下仍具有相同native bytes、item顺序和fingerprint。

### 9. TUI使用显式modal子状态，交互向Claude Code靠拢

`internal/tui.Model`在idle/streaming之上增加独立`approvalOverlay`值，而不是把approval字段散落进composer。overlay保存request identity、candidate fingerprint、只读diff viewport、三项固定选择、反馈编辑状态与submitted标志。默认焦点为`No`；方向键选择，Enter提交，只有`No`上Tab进入反馈编辑；Esc先退出反馈编辑，再次Esc或overlay外Esc/Ctrl+C发送定向interrupt。

overlay活跃时key routing首先命中modal，聊天草稿既不变更也不提交。提交response后立即置submitted，直到收到`tool_approval_resolved`或turn terminal才关闭，避免双击/按键重复。小终端通过overlay内部viewport滚动完整diff，路径与选择固定；无颜色时仍用符号和焦点标记表达语义。固定尺寸snapshot覆盖普通、小尺寸、滚动、resize、反馈、取消及重复event。

这保留Claude Code最有价值的“在动作附近展示精确diff、提供一次/会话/拒绝”的交互，但决定集合来自内核，避免UI自行创造权限范围。首版不实现IDE式内联编辑；修改意见通过deny feedback让模型发起全新调用。

### 10. Headless宿主使用同一路径自动拒绝，不扩展外部输入协议

`--print`、`--json`与stream-json当前没有交互responder，因此Runtime在宿主能力标记为non-interactive时把`ask`解析为typed deny，code固定为`approval_required`，并走相同denied result/native output路径。不会等待stdin，也不向外部version 1 command wire增加approval response。

交互式app组合根负责把TUI command送入AgentLoop；Runtime、Tool与Provider不知道Bubble Tea。未来若要让headless接受远程approval，必须单独OpenSpec定义鉴权、超时和外部协议revision，不能复用本变更的进程内command偷渡。

### 11. 测试围绕边界等价与故障注入组织

实现按层添加测试：typed parser/union与diff纯单测；Provider request/stream/native golden；Patch descriptor-relative安全与确定性write/Sync/rename故障注入；Runtime approval/interrupt竞态及exclusive scheduler；Session固定bytes fixtures和每个崩溃点；TUI固定尺寸snapshot；app双Provider交互/headless E2E。

安全测试复用现有workspace与Session的hook/fake方式，在open、candidate完成、临时文件Sync、rename和目录Sync边界确定性替换symlink或注入错误，不使用sleep证明TOCTOU。最终执行`make verify`，同时校验Catalog/cache fingerprint以及uninterrupted/restored canonical request等价。

## Risks / Trade-offs

- [原子替换改变inode、硬链接及未保留metadata语义] → 首版仅声明普通UTF-8文件和受支持权限位；检测不支持属性时失败关闭，后续若扩展必须另立规格。
- [candidate在内存中包含完整base/final bytes，16 MiB文件会增加峰值内存] → 单Patch独占、候选与diff均有硬上限，call完成后立即释放；不并行准备多个Patch。
- [用户等待approval时Session持有活动turn和thread lease] → 这是保证单writer与exactly-once所必需；interrupt/shutdown由同一owner解除等待并有确定完成信号。
- [OpenAI兼容服务可能未实现custom tool流事件] → 只有完整golden/集成契约通过才声明custom capability；非法或缺失事件按stream protocol failure处理，不退回function伪装。
- [目录Sync在不同平台行为不同] → 平台实现显式声明支持；无法确认rename后的durability时poison Session并报告uncertain，不自动重试。
- [Session canary直接替换导致旧开发journal不可恢复] → 项目尚未稳定发布，按工程约束fail closed；迁移前保留测试证据而不保留生产兼容reader。
- [拒绝反馈可能携带敏感内容] → 限制UTF-8与16 KiB，只进入权限受控Session result/native history，不进入diagnostic log、错误或approval metadata。

## Migration Plan

1. 先落地共享typed values、Catalog wire kind与Patch parsers，并更新Provider request/stream/native golden；此时只有完整Catalog构造成功才可暴露Patch。
2. 实现candidate/diff与平台安全executor，使用纯单测和故障注入验证零副作用准备、冲突与原子替换。
3. 扩展Session codec、canary、ReplayPlanner和固定`testdata/current`，再接入Runtime scheduler、approval owner与恢复补偿，避免运行时产生旧shape记录。
4. 接入TUI modal与app交互能力标记，headless保持fail closed；完成双Provider E2E、snapshot、race和恢复等价测试。
5. 同步Tool架构、Roadmap与pitfall，运行`make verify`后才允许归档。

回滚必须把代码、Provider golden、Session canary与全部current fixtures作为一个原子版本整体回退。已经由新版本写出的journal不会由旧实现读取；回滚版本应安全拒绝，不进行原地降级或重写append-only历史。
