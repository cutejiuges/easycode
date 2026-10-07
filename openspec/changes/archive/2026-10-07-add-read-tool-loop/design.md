## Context

参见 [proposal.md](proposal.md) 的动机。本变更建立在以下已实现边界上：

- `provider.Conversation.Stream` 当前一次只完成一个文本 sample，`PreparedSample` 原子携带 native envelope、normalized usage 与纯内存 finalizer。
- `runtime.Runtime.RunTurn` 当前在 `turn_started` 后只消费一个 Provider stream，并以 `[provider_native_commit, sample_usage, turn_completed]` 单 batch 成功收口。
- Session JSONL 已具有 exclusive lease、single writer、append/Sync batch、strict typed codec、ReplayPlanner和尾部修复；SQLite只投影最小Session目录信息。当前尚无线上用户，开发期fixture可以随本次根源重构一次性重写。
- Anthropic history 当前以 `nativeTurn` 保存user/assistant messages，OpenAI history以 `nativeTurn` 保存user item/output items；该结构把一次sample错误约束为必须同时具有新用户输入和assistant输出，无法表达tool outputs后的continuation sample。
- `internal/tool` 与 `internal/tool/builtin` 是allowlist中的P3占位，仍导出 `json.RawMessage` 输入和提前列出的未实现工具，不能直接接入Runtime。
- ContextPlan当前固定四类来源；Runtime在turn开始后、Provider stream前进行一次规划。
- 应用启动时已能冻结项目指令快照和规范化cwd，但还没有可供文件工具使用的workspace handle capability。

本设计受 [Tool系统架构](../../../docs/architecture/tool-system.md) 和本change的delta specs约束。它优先复用现有Provider-native、durable和cleanup边界，不建立第二套agent runtime。

## Goals / Non-Goals

**Goals:**

- 用一个真实Read能力证明Catalog、双Provider tool wire、多sample循环、ledger、恢复和安全文件读取的完整纵向边界。
- 让所有不可逆状态转换都具有明确的durable线性化点，并使恢复只追加本地确定事实；下一次用户输入从已正确配对的Provider原生历史继续。
- 让新增公共契约保持sealed、typed、可验证，删除或替换当前无真实消费者的通用占位。
- 保持现有单writer、Provider stream owner、ContextPlan和host command/event模型不分叉。

**Non-Goals:**

- 不为了未来工具预建动态插件registry、通用artifact store、并行DAG scheduler或完整approval/sandbox配置系统。
- 不在本change公开Tool RuntimeEvent、headless JSON事件或TUI专用展示；宿主仍只观察现有turn/text/terminal事件。
- 不优化Claude Code式“tool block结束即提前执行”的流内延迟；首版必须等待完整sample durable。
- 不对started-without-result的Read做“因为只读所以安全重试”的特殊推断，统一执行保守的at-most-once恢复规则。

## Decisions

### 1. 以纯值 Tool Kernel 保持依赖单向

`internal/tool` 只保存无I/O值契约和小接口，Provider可以依赖这些值，Tool不得导入Provider、Runtime、Session或TUI：

```text
domain/protocol
      ^
      |
   tool values <----- provider adapters
      ^                    ^
      |                    |
      +------ runtime -----+
                 |
          session/app owners
```

核心值包括：

- `CapabilityID`、`FacadeID`、`CatalogSnapshot`和Provider-specific facade view。
- `ReadInput`、`ReadyCall`、`InvocationID`、`ProviderCallID`。
- `InvocationResult`、`ResultStatus`和冻结的 `ModelPreview`。
- `Executor`和固定的 `ReadOnlyPolicy` 小接口。

`ReadyCall`使用sealed capability payload：当前schema只能由typed `ReadInput` constructor/decoder创建，不对导出构造器暴露 `any`、`map[string]any` 或调用方可写 `json.RawMessage`。Provider-specific decoder负责wire framing，最终复用同一个Read input decoder与validator。

当前 `tool.Spec`、raw `Invocation`、raw `Result`和提前返回六个工具的 `builtin.Specs()` 将被替换，不保留兼容分支。原因是这些占位尚无生产消费者或稳定外部契约，直接收紧比为错误抽象维护双轨更安全。

备选方案是把ToolCall放入 `domain` 或让Runtime自己解析Provider JSON；前者会把Provider facade细节污染最低层，后者会让Runtime解释wire，因此不采用。

### 2. CatalogSnapshot 同时驱动请求schema、路由和缓存来源

应用组合根通过显式构造创建一个仅含Read注册项的Catalog，再冻结为深拷贝snapshot。snapshot按 `(capability_id, facade_id)` 排序，保存：

- catalog/schema revision。
- capability到typed decoder、executor route和result renderer的封闭映射。
- Anthropic与OpenAI各自的名称、description和schema canonical bytes。
- 对稳定内容计算的SHA-256 fingerprint。

snapshot本身不保存executor实例、workspace handle或权限临时状态。Runtime持有snapshot和单独注入的executor/policy依赖；Provider只取得与自己family匹配的不可变facade view。

ContextPlan升级revision，在 `provider_profile` 后加入stable `tool_catalog` segment。该segment编码catalog revision、family-specific facade canonical bytes和fingerprint；不会加入workspace路径或invocation identity。首次sample使用user-text current input；后续sample使用typed `tool_continuation` current-input variant，其canonical值只表示采样原因且估算为零，真实tool outputs已包含在Provider native footprint中。

备选方案是仅依赖最终request fingerprint。它能发现请求变化，却无法在ContextPlan中定位tool schema失效来源，也与已确认的缓存分层不一致，因此不采用。

### 3. Provider完成完整sample后才交付typed calls

`PreparedSample`扩展为同时拥有：

- native sample envelope。
- normalized sample usage。
- 按原生item顺序深拷贝的 `[]tool.ReadyCall`。
- 恰好一次finalizer。

Anthropic reducer为 `tool_use` block保存id/name和有界partial JSON buffer；只有 `content_block_stop` 后才strict decode，`message_stop`时再次验证所有blocks与call ID唯一性。OpenAI reducer关联 `response.output_item.added/done`、`response.function_call_arguments.delta/done` 与最终function-call item，只有 `response.completed`时全部arguments完整才产生ready calls。

Provider仍拥有原生call item；ReadyCall只携带共享执行需要的typed值。Runtime在Session append前重新clone/validate envelope、usage和calls，任何一项非法都丢弃整个prepared sample。

备选方案是在block/item完成时立即调用Read。虽然更接近Claude Code低延迟体验，但native sample和usage尚未durable，stream随后失败会产生无法恢复的孤立I/O，因此延期到独立change。

### 4. Conversation历史从“turn slice”重构为单一封闭entry序列

两家Provider各自拥有一种当前 `nativeHistoryEntry` 封闭值，使用语义discriminator表达两种variant，而不是按场景增加版本类型：

```text
sample       = optional current input + completed native outputs + raw usage
tool_outputs = ordered native tool result inputs, no usage
```

首个sample必须携带真实用户输入；紧跟合法tool outputs的continuation sample不得伪造新用户输入。Anthropic entry保存原生message/content block与metadata，OpenAI entry保存原生input/output item；共享层只持有opaque envelope，不解释或互转两家wire。

每家Provider只有一个当前payload schema、encoder、strict decoder和validator。schema中的kind负责区分variant，字段组合由validator封闭校验；Go类型、构造器和文件名不使用 `V1/V2/V3` 后缀。公共envelope仍携带单一当前payload revision用于拒绝未知数据，但本变更直接替换旧开发期codec与fixture，不保留旧reader或混合revision路径。

`Conversation.PrepareToolOutputs` 接收冻结的ordered results，验证它们与最后一组未闭合calls的类型、call ID、数量和顺序，然后纯内存生成不带usage的prepared entry。Runtime在native commit成功Sync后才finalize。下一次Stream使用显式continuation输入，在同一entry模型中提交只有assistant输出和raw usage的sample。

恢复时Provider按seq用同一decoder恢复entry并维护原生call pairing状态；任何孤立、重复、遗漏、错序output、非法首个sample或在非tool-output边界出现无输入sample都会使整个Restore失败。Provider不解释Session ledger，Session也不解析Provider payload。

备选方案是保留旧text sample并为tool outputs、continuation依次增加payload revision。它会把同一对话状态机拆成多套结构和decoder，且当前没有线上数据需要兼容，因此不采用。

### 5. Runtime 使用一个显式多sample状态机

Runtime仍是turn唯一owner，内部状态机如下：

```text
turn_started durable
        |
        v
plan sample -> stream/drain -> validate prepared sample
        |
        v
sample native + usage + ready[] durable -> sample finalize
        |
        +-- no calls --> aggregate usage -> turn_completed durable
        |
        +-- calls ----> process invocations in call order
                              |
                              v
                       prepare outputs
                              |
                              v
                       output native durable/finalize
                              |
                              +------> next sample
```

Runtime为每个sample创建新的派生stream context并复用现有“验证、取消、排空到channel close”owner规则。固定上限为每turn 16 samples和64 calls；计数在发起下一stream或分配新invocation前检查。

首版使用简单for-loop顺序处理calls，不引入Scheduler接口或goroutine。并发change出现真实第二种调度策略时再提取Scheduler；现在预建接口只会形成无意义抽象。Provider `ParallelToolCalls`保持false，但对服务端仍返回多个calls的情况按顺序安全处理。

### 6. Session ledger 使用三个新required kind

新增payload均使用各自唯一的当前typed codec；record envelope的数值revision只用于严格解码，不进入类型名称：

| Kind | 关键字段 | Durable含义 |
| --- | --- | --- |
| `tool_call_ready` | invocation/provider call IDs、sample/call index、capability/input revision、ReadInput | sample与完整参数已成为执行前事实 |
| `tool_execution_started` | invocation ID | executor已经被Runtime接收 |
| `tool_call_result` | invocation ID、status/code、codec revision、preview bytes、bounded metadata | 不需要重新执行即可生成Provider output |

Invocation ID由注入的UUIDv7 generator在构造ready batch前一次性分配；Provider call ID只用于原生配对，二者不能互换。ReplayPlanner检查同turn内invocation/call ID唯一、index连续和单向状态转换。

写入边界：

1. Call sample：`[provider_native_commit, sample_usage, tool_call_ready...]` 同batch。
2. 每个调用：正常路径单独Sync `tool_execution_started`，随后执行Read，再单独Sync `tool_call_result`；若取消在started被接受前线性化，则直接Sync cancelled result且不执行Read。
3. 全组结果：单独Sync tool-output `provider_native_commit`，随后finalize到Conversation。
4. Final sample：`[provider_native_commit, sample_usage, turn_completed]` 同batch。

将started与result拆开会保留不可消除的崩溃窗口，但能准确识别 `outcome_uncertain`。把二者与外部I/O放入一个Session batch无法使文件和外部世界原子化，因此不伪装exactly-once。

### 7. 恢复由显式 Reconcile 生命周期完成

`sessionService.resume`继续在同一exclusive lease下load、repair、plan和Provider Restore，但不再把所有活动turn一律补成 `session_interrupted`。ReplayPlan新增封闭的tool recovery projection，包含每组calls、ledger状态、是否已有tool-output commit及sample usage顺序。

应用在构造宿主前显式调用Runtime reconciliation：

- ready/no-started：写入 `cancelled/session_interrupted_before_execution` result，不执行工具。
- result/no-output-commit：从持久化preview准备outputs。
- started/no-result：写入 `outcome_uncertain` result，不重试工具。
- output-committed/no-terminal：不重复output。
- 上述各分支补齐Provider pairing后，写入且只写入一个 `turn_failed`。

reconciliation只允许纯内存Provider output编码与Session append/Sync，不调用Executor、不启动Provider stream，也不继续旧turn的下一sample。它不向headless stdout重放旧turn事件。TUI在reconciliation后重新取得HistoryProjector快照作为初始transcript；headless随后只处理用户本次新prompt。任何append/Sync失败都阻止宿主进入可提交状态。

这保持现有 `--resume`/`--continue` 的“旧输出不作为本次headless输出重放”契约，同时允许未完成Tool Loop安全收口。

### 8. Read 使用显式打开的 workspace capability

应用从当前启动cwd规范化workspace root，并通过 `OpenWorkspace` 显式取得目录handle；`NewReadExecutor`仅组合已打开capability和配置，不执行I/O。Session `creation_cwd`只保留metadata，resume也使用当前启动workspace，不自动chdir。

Darwin/Linux实现从root fd开始使用handle-relative open逐级拒绝symlink，最终 `fstat` 验证regular file、权限和不超过16MiB。测试通过注入的确定性交换点覆盖TOCTOU。不支持等价实现的平台返回稳定unsupported错误。

Read先strict校验 `file_path`、offset和limit，再安全打开文件。接受workspace内绝对输入是为了接近Claude Code使用习惯，但内部立即归一化为root-relative组件；模型preview、Session metadata和错误只保存相对路径。

读取后拒绝NUL/非法UTF-8。renderer按原始1-based行号输出，单行最多2000 Unicode code points、总preview最多256KiB，并在UTF-8和完整编码边界截断。`tool_call_result`保存最终preview和renderer revision，Provider codec只复制该preview。

本change没有artifact store；因为Read一次只返回最多2000行且preview已有硬上限，省略内容通过typed truncation metadata说明。完整artifact由后续统一结果预算change提供。

### 9. 固定Read自动允许策略，不提前公开approval协议

本change的Policy只有真实消费者：仅允许Catalog中 `fs.read`，其他capability拒绝；路径与文件能力由workspace handle强制。没有 `ask`、持久权限规则或用户输入修改，因此不新增approval command/event。

Policy allow不替代workspace enforcement。即便调用已allow，executor仍必须逐handle验证路径。未来approval change可替换Policy组合，但不得弱化Read executor的文件安全。

### 10. Host协议保持不变

Runtime不会发布tool ready/progress/result事件，`protocol.Event`中的保留ItemID/CallID仍不启用。现有TUI、一次性headless和stream-control只观察assistant text与turn terminal；模型拿到Read output后产生的最终文本按既有路径展示。

这是有意的纵向切片边界：本change先证明执行与恢复事实，避免同时冻结未经产品验证的Tool UI wire。后续Tool Presenter change必须基于这里的typed result投影，不能读取Provider wire或Session opaque payload。

## Risks / Trade-offs

- **[风险] 首个change同时触及Provider、Runtime、Session和文件安全，实现量仍然较大。** -> 按tasks先落纯值契约与fixture，再分别接两家Provider，最后启用Runtime/app；每个阶段保持测试可运行。
- **[风险] Anthropic/OpenAI兼容服务的tool streaming事件可能不完整。** -> 严格实现官方原生序列并以fixture覆盖；未知事件可忽略，但缺少完成边界或arguments时失败关闭，不做字符串猜测兼容。
- **[风险] 根源替换native history schema会同时影响两家Provider和开发期Session fixture。** -> 两家分别实现自己的entry codec与请求golden，Restore在返回Conversation前事务性验证完整序列，并以不中断/恢复后的下一请求canonical bytes等价作为质量门。
- **[风险] `outcome_uncertain` 对只读Read显得保守。** -> 统一语义避免未来写工具复制错误重试逻辑；若要为可证明幂等capability增加重试，另行定义显式能力和测试。
- **[风险] result preview同时存在ledger与Provider-native history会增加JSONL体积。** -> preview上限256KiB且单turn调用有上限；重复是恢复无需重读的必要代价，后续artifact change可在不改写旧records的前提下演进。
- **[风险] 当前workspace策略拒绝symlink，可能与部分真实仓库布局不兼容。** -> 首版安全失败关闭并给出稳定错误；允许受控symlink需要新的handle-bound信任设计，不能静默放宽。
- **[风险] 本地补偿会终止旧turn，而不会自动完成用户原请求。** -> 恢复阶段以唯一 `turn_failed` 明确旧turn边界，避免无人确认时产生文件或网络副作用；下一次用户输入可从已正确配对的原生历史继续。
- **[风险] 没有Tool UI事件会降低首版可观察性。** -> Session ledger与测试提供审计，用户仍看到最终模型回复；不以临时不稳定事件污染外部协议。

## Migration Plan

1. 先替换未启用Tool占位，建立pure typed catalog/input/result与Read renderer测试；此时不对Provider声明tools。
2. ContextPlan升级到新revision并加入catalog segment，更新确定性与cache regression；无历史Session schema变化。
3. 分别以单一当前entry模型重构Anthropic/OpenAI request、reducer、sample/tool-output codec与恢复状态机，删除旧 `nativeTurn` codec并重写两家的native fixtures。
4. 新增三个Session required kind和ReplayPlan tool状态，一次性重写当前开发期JSONL/ReplayPlan fixture，不保留旧Provider payload reader或混合revision测试。
5. 接入Runtime多sample与ledger，再接入app workspace/reconciliation；只有双Provider端到端与恢复等价全部通过后才把FunctionTools capability置为true。
6. 同步架构、Roadmap和pitfall状态，执行 `make verify` 后才允许archive。

当前没有线上用户或需要保留的历史journal，本变更不提供旧开发期数据迁移和旧二进制回滚。实现合入前由fixture整体替换证明当前schema自洽；合入后如产生真实用户数据，后续变更必须恢复不可变fixture与显式迁移纪律。
