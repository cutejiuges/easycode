## 1. Tool 纯值契约与 Catalog

- [x] 1.1 用sealed、typed的Capability/Facade/CatalogSnapshot/ReadyCall/InvocationResult契约替换当前raw JSON占位，删除未实现工具清单与无消费者接口，并通过 `internal/tool` 构造、零值、深拷贝和架构依赖测试
- [x] 1.2 实现只注册Read的确定性Catalog snapshot、双Provider facade view、canonical schema/source revision/fingerprint，并通过注册顺序扰动、调用方修改和未绑定executor拒绝测试
- [x] 1.3 将当前实现收敛为无版本后缀的 `ReadInput` strict decoder与validator，固定file_path/offset/limit默认值和边界，并通过重复字段、未知字段、尾随JSON、非法UTF-8及数值边界表驱动测试
- [x] 1.4 实现typed Read result、status/error code、renderer revision与256 KiB模型preview，覆盖稳定行号、2000 code points长行截断、总预算、UTF-8边界和相同输入逐字节等价测试
- [x] 1.5 实现固定ReadOnlyPolicy，只允许真实 `fs.read` registration且不暴露ask/其他capability，并通过allow/deny与“未实现schema不出现”测试

## 2. ContextPlan 与缓存稳定性

- [x] 2.1 为PlanningInput和ContextPlan增加不可变Tool Catalog source及user-text/tool-continuation current-input variant，删除版本后缀类型与旧plan分支，并通过来源顺序、clone、非法组合和continuation零新增估算测试
- [x] 2.2 将family-specific catalog canonical bytes映射为stable cache segment，覆盖schema/revision变化失效、cwd/Session/invocation变化不失效和secret/绝对路径不泄漏测试
- [x] 2.3 更新Runtime规划输入与现有ContextPlan fixture，验证首个sample和tool continuation都在Provider副作用前规划，明确超限时零网络/零executor调用

## 3. Workspace 与 Read Executor

- [x] 3.1 增加显式 `OpenWorkspace`/Close生命周期和纯内存Read executor构造，使用当前启动cwd而非Session creation_cwd，并通过“New无文件系统副作用”、幂等关闭和资源owner测试
- [x] 3.2 在Darwin/Linux实现从workspace目录handle逐级相对打开、no-follow与最终handle类型/权限/16 MiB大小验证，并通过真实symlink、traversal、特殊文件和绝对内外路径测试
- [x] 3.3 增加确定性TOCTOU故障注入，验证目录/文件组件交换时不读取workspace外目标，并为无等价实现的平台增加明确失败关闭测试
- [x] 3.4 实现有界UTF-8文本读取、NUL/非法编码拒绝、offset/limit选择与context取消清理，并通过空文件、末尾无换行、大文件、binary、取消和无goroutine/handle泄漏测试
- [x] 3.5 将绝对输入规范化为workspace相对结果并投影安全英文错误，验证错误、preview、日志和测试snapshot均不包含绝对workspace、底层cause或secret

## 4. Provider 共享 Prepared 契约

- [x] 4.1 扩展PreparedSample以原子携带深拷贝ready calls，保留envelope/usage/finalizer既有不变量，并通过零值、非法call、重复call ID、调用方修改和finalize恰好一次测试
- [x] 4.2 将prepared提交收敛为语义命名的sample/tool-output值与Conversation tool-result preparation小接口，验证构造纯内存、配对失败不修改history、durable后finalize一次及重复finalize拒绝
- [x] 4.3 保持StreamEvent封闭构造和producer cleanup契约，增加completed sample含calls的验证、取消/非法terminal排空测试，并确保Tool类型未放宽为 `any` 或共享动态map

## 5. Anthropic Messages Tool Wire

- [x] 5.1 在Messages request中从CatalogSnapshot编译稳定Read schema，更新request golden并验证项目指令/messages/tools顺序、path prefix、secret隔离和fingerprint稳定性
- [x] 5.2 扩展Anthropic reducer归并 `tool_use` 与 `input_json_delta`，实现有界partial JSON、strict Read decode和call顺序，覆盖随机SSE chunk、半包、UTF-8、重复ID、未知tool、不完整参数、取消和EOF测试
- [x] 5.3 将Anthropic `nativeTurn`根源重构为单一当前sealed entry模型与codec，以kind表达首个sample、tool outputs和无新user message的continuation sample，删除旧reader并重写thinking/text/tool encode/decode golden
- [x] 5.4 实现Anthropic `tool_result` codec和call/output pairing恢复状态机，覆盖成功/error/cancelled/outcome_uncertain、错序/遗漏/重复ID、非法无输入sample及当前fixture restore测试
- [x] 5.5 仅在Anthropic request/reducer/current persistence/result codec/restore测试全部通过后启用function-tools capability，并验证parallel/custom/cache延期capability仍为false

## 6. OpenAI Responses Tool Wire

- [x] 6.1 在Responses request中从CatalogSnapshot编译stable strict Read function，更新request golden并验证input/tools/include顺序、store=false、secret隔离和fingerprint稳定性
- [x] 6.2 扩展Responses reducer关联function-call item与arguments delta/done，产生有序typed calls，覆盖随机SSE chunk、半包、UTF-8、identity冲突、重复call ID、未知tool、不完整参数、取消和断线测试
- [x] 6.3 将OpenAI `nativeTurn`根源重构为单一当前sealed entry模型与codec，以kind表达首个sample、tool outputs和无新user item的continuation sample，删除旧reader并重写message/reasoning/function round-trip golden
- [x] 6.4 实现OpenAI function call output codec和pairing恢复状态机，覆盖全部result状态、错序/遗漏/重复ID、非法无输入sample及当前fixture restore测试
- [x] 6.5 仅在OpenAI request/reducer/current persistence/result codec/restore测试全部通过后启用function-tools capability，并验证custom/parallel/previous-response延期capability仍为false

## 7. Session Tool Ledger 与当前 Schema

- [x] 7.1 新增InvocationID生成/校验及ready/started/result三个当前payload、descriptor、typed draft、strict decoder和validator，类型/构造器不带版本后缀，并覆盖零值、未知/重复字段、非法index/status、超限preview和深拷贝测试
- [x] 7.2 扩展ReplayPlan和ReplayPlanner以验证多sample、ready顺序、正常 `ready -> started -> result` 与受限 `ready -> cancelled result`、tool-output commit placement和唯一terminal，覆盖所有checksum合法但语义非法的排列
- [x] 7.3 扩展Runtime所需Journal batch测试，证明call sample `[native, usage, ready...]`原子Sync、started/result各自线性化、tool-output commit不带usage且失败后writer poisoned
- [x] 7.4 一次性重写人工冻结的当前文本与Tool Loop JSONL/ReplayPlan fixture，使用生产Loader/registry/codec验证双Provider当前fixture恢复、续写和未知required revision只读失败，确认不存在旧Provider payload reader或混合revision测试
- [x] 7.5 更新Session Catalog projector，确认新增tool records只影响validated committed recency且不把input、preview、call ID或ledger细节复制进SQLite，并通过删除索引重建等价测试

## 8. Runtime 多 Sample Tool Loop

- [x] 8.1 将RunTurn重构为单owner多sample状态机，复用每个stream的派生context/验证/排空路径，并通过text-only单sample回归和Read双sample成功测试
- [x] 8.2 实现call sample的native/usage/ready原子提交、sample finalize和invocation ID分配，验证append/Sync/finalizer任一点失败时零executor调用且Runtime/Journal正确poisoned
- [x] 8.3 实现顺序Read执行：以started append是否被接受作为Executor admission线性化点，接受前本地取消，接受后恰好执行一次，再durable result；覆盖拒绝、typed执行错误、两侧取消竞争和同invocation不重复执行
- [x] 8.4 实现ordered results到prepared tool-output entry的提交/finalize，再以无伪造用户输入的continuation开始下一sample，验证下一stream绝不早于output Sync且Provider outputs保持call index顺序
- [x] 8.5 实现16 samples/64 calls限制与稳定 `tool_loop_limit_exceeded`失败，覆盖边界值、超限零新增网络/工具调用和既有facts不改写
- [x] 8.6 按durable sample顺序聚合最终turn usage，覆盖known/unknown/not-applicable/溢出、多sample成功及后续失败保留中间usage但不发布成功terminal
- [x] 8.7 完成所有已durable calls在成功、error、取消和不确定状态下的Provider output配对，验证turn始终只有一个terminal且下一用户turn可从合法native history继续

## 9. Resume Reconciliation 与应用装配

- [x] 9.1 扩展sessionService恢复结果以携带sealed Tool recovery plan，同时保持从首字节load到writer关闭的连续exclusive lease，并通过真实子进程busy/崩溃释放测试
- [x] 9.2 增加无外部副作用的Runtime reconciliation生命周期，覆盖ready未started本地取消、result未output复用preview、started未result生成outcome_uncertain、output已提交不重复，最终追加唯一 `turn_failed`，全程零Executor和Provider调用
- [x] 9.3 在app组合根按启动cwd打开workspace、构造Read executor/policy/catalog并保证唯一cleanup owner，覆盖create/resume/continue及任一装配失败的逆序资源关闭测试
- [x] 9.4 保持TUI与headless外部事件词汇不变：reconciliation不重放旧turn到stdout，TUI从完成后的HistoryProjector加载transcript，并通过一次性text/json、stream-control和TUI snapshot回归测试
- [x] 9.5 为Anthropic与OpenAI分别增加ready崩溃、started崩溃、result崩溃、output后崩溃的app端到端测试，验证本地补偿与唯一失败终态，并逐字节比较下一次用户请求同等reconciled history的canonical bytes、native item顺序、tool schema和fingerprint

## 10. 集成验证与文档收口

- [x] 10.1 增加双Provider“用户请求读取 -> Read -> 模型总结”golden/e2e，验证无工具文本turn兼容、仅Read可见、调用/结果配对和最终宿主输出
- [x] 10.2 运行secret扫描与架构边界测试，确认API key、Authorization、workspace绝对路径、文件正文诊断副本和Provider wire未跨越约定层，且生产代码没有可变包级registry/cache
- [x] 10.3 同步 `docs/architecture/tool-system.md`、总体架构、Roadmap和pitfall实际状态，核对文档不把并行、approval、artifact、Tool UI或其他工具写成已实现
- [x] 10.4 运行 `openspec validate add-read-tool-loop --strict` 和 `make verify`，修复全部gofmt、vet、Staticcheck、架构、全量及race失败，并记录无法执行检查的明确原因与风险
