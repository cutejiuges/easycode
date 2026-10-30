# EasyCode 踩坑与经验记录

### [P3][2026-10-30] 预览省略与记录位置混用

- 状态：已修复，`add-tool-transcript-and-result-budget` 的 macOS 全量与 race 验收通过；Linux 原生矩阵因无 runner 未验收。
- 根因：按事件到达顺序拼接正文不能证明其已提交；以搜索 omitted 总数推断采集覆盖会把模型预算裁剪误报为搜索未完成；只有四页游标栈不能支持任意反向浏览。
- 修复：双 Provider 提供原生有序锚点，宿主连接 durable seq/ready 身份，界面以提交上界与 generation 替换 provisional；搜索分别记录模型省略数量和采集省略；正文与保存输出使用真实前后范围查询。
- 回归：`internal/app/transcript_projection_test.go`、`internal/protocol/transcript_query_test.go`、`internal/runtime/result_budget_test.go`、`internal/tui/transcript_state_test.go`。测试不再依赖已移除的 Status 行判断 turn 完成，而消费 typed terminal。


### [P3][2026-10-29] 私有权限不能隔离已获 workspace 权限的命令

- 状态：已修复，随 `add-command-output-artifacts` 验证。
- 根因：命令与宿主使用相同 OS 用户，0700/0600 不能阻止 sandbox 已授权路径内的读取或覆盖；将存储放入 workspace 或把 home 当 workspace 都可能暴露保存内容。
- 修复：启动及每次执行前复用实际 SandboxPlan 的根范围，以目录 FD 身份检查祖先关系；私有 symlink 拒绝，仅显式规范化 macOS 系统别名。校验失败返回 `unsafe_storage_layout`，不静默停用保存。
- 生命周期：同步保存可能阻塞，取消不代表资源释放；进程 owner 在写入/seal 解除阻塞并完成清理后才关闭 done。发布完成也不代表授权，必须等 result Sync。
- 回归：`internal/app/artifact_boundary_test.go`、`internal/tool/builtin/artifact_isolation_darwin_test.go`、`internal/tool/builtin/artifact_owner_test.go`、`internal/runtime/artifact_commit_test.go`。两个 Provider 写死六工具的编译校验也已随七工具 catalog、request golden 和恢复回归一起更新。

### [P3][2026-10-27] 当前契约与宿主清理边界不一致

- 状态：已解决。
- 影响范围：上线前 codec、prepared commit、TUI/一次性 headless 与 Patch 执行链。
- 根因：默认 Sonic codec 可被全局替换；分散 decoder 接受部分 null/重复字段；TUI 满 buffer 阻塞分发，资源关闭等待另一个空闲 facade；Patch 在批准 Sync 前安装 grant，started 绕过取消且非法结果被降级为普通 error。
- 修复：固定 codec 配置并集中严格检查，删除 prepared 简化入口；宿主统一 AgentLoop，detach 后排空并 join，transport 取消活动流；独占工具共用 durable admission 和 uncertain 收口。
- 架构与缓存影响：未新增兼容 reader 或持久化 shape，合法 canonical bytes、checksum 与 fingerprint 不变，真实 canary 和 native revision 分别保留。
- 回归：`internal/codec/global_isolation_test.go`、`internal/codec/validation_test.go`、`internal/provider/prepared_lifecycle_test.go`、`internal/runtime/session_owner_test.go`、`internal/runtime/current_contract_test.go`、`internal/provider/transport/close_test.go`、`internal/app/host_lease_test.go`、`internal/architecture/current_contract_test.go`。

> 本文记录实际开发中已经发生的问题、根因和防回归措施。  
> Roadmap 中的“已知踩坑与规避”是预防清单；本文是事实日志。

## 1. 记录规则

遇到以下情况必须记录：

- Provider 兼容行为与官方协议或预期不同。
- thinking/reasoning/tool item 出现数据丢失、乱序或无法恢复。
- prompt cache 命中异常下降或发生无法解释的失效。
- stream 重试造成重复输出、重复工具调用或状态不一致。
- session resume/fork/compact 出现历史破坏。
- 权限、sandbox、hook/plugin trust 出现安全问题。
- TUI 在终端恢复、输入路由或流式渲染上出现系统性问题。
- 架构边界导致循环依赖、重复实现或上帝模块倾向。

每一项必须关联自动化回归测试；如果暂时无法自动化，需要说明原因和替代验证方法。

## 2. 记录模板

```text
### [P?][YYYY-MM-DD] 简短标题

- 状态：发现 / 修复中 / 已解决 / 接受风险
- 影响版本或提交：
- 现象：
- 触发条件：
- 根因：
- 架构影响：
- 缓存影响：
- 修复方案：
- 未采用方案及原因：
- 回归测试：
- 关联 ADR/Issue/PR：
- 后续行动：
```

## 3. P0 工程与协议基线

### [P0][2026-09-18] Sonic 默认配置不能直接作为缓存 canonical JSON

- 状态：已解决
- 影响版本或提交：初始脚手架
- 现象：Sonic 的默认高性能配置没有启用 map key 排序，相同语义的 map 可能产生不同请求字节。
- 触发条件：Tool Schema、扩展 catalog 或请求结构包含 map，且插入顺序不同。
- 根因：性能优先配置与缓存所需的确定性目标不同。
- 架构影响：所有缓存敏感 JSON 必须通过统一 codec，业务包不得直接调用 `sonic.Marshal`。
- 缓存影响：可能造成 Anthropic/OpenAI 稳定前缀无意义失效。
- 修复方案：统一使用 `sonic.ConfigStd`，并通过 `codec.MarshalStable` 暴露。
- 未采用方案及原因：没有使用标准库 `encoding/json`，因为项目已经选定 Sonic，且 Sonic 提供稳定排序配置。
- 回归测试：`internal/codec/json_test.go`、`internal/context/cache_plan_test.go`。
- 关联 ADR/Issue/PR：ADR-0001。
- 后续行动：所有 Provider request golden test 必须从统一 codec 生成。

### [P0][2026-09-18] Provider usage 原始字段不能直接计算缓存比例

- 状态：已解决并落地 completed-sample 基线
- 影响版本或提交：P0 架构基线；OpenSpec `add-durable-sample-usage`
- 现象：如果使用 `cached_input_tokens / input_tokens` 作为统一公式，Anthropic 可能出现大于 1 的比例，而 OpenAI 的比例通常不大于 1。
- 触发条件：Anthropic 的 `input_tokens` 是未命中缓存的输入，cache read/write 是独立字段；OpenAI 的 `prompt_tokens` 已包含 `cached_tokens` 子集。
- 根因：两家 Provider usage 字段的分母语义不对称。
- 架构影响：UsageParser 必须先归一化为 `input_uncached`、`cache_read`、`cache_write` 三元组，指标层不得读取 Provider 原始字段计算 ratio。
- 缓存影响：错误口径会产生不可比、甚至大于 1 的命中率，误导缓存优化和成本诊断。
- 修复方案：归一化总输入为 `input_uncached + cache_read + cache_write`；Anthropic 按三者之和作为分母，OpenAI 从 prompt_tokens 与 cached_tokens 子集推导未缓存输入。字段区分 known、unknown 和 not-applicable，只有明确不适用才使用已知 0，缺失或零分母输出 unknown。
- 未采用方案及原因：不将两家原始字段强行命名为同一含义，也不把缺失值当 0。
- 回归测试：`internal/domain/usage_test.go`、Anthropic/OpenAI reducer/commit/restore tests；cache metrics regression 随后续观测能力补充。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0004。
- 后续行动：成本、比例和 telemetry 只消费已落地的 normalized usage；不得重新读取 raw Provider 字段建立第二套口径。

### [P0][2026-09-18] 双轨历史需要显式 HistoryProjector

- 状态：已解决设计口径
- 影响版本或提交：P0 架构基线
- 现象：token 估算、resume 渲染、Stop hook 文本和 Subagent completion 都需要读懂 native history；若没有统一接缝，四处会各写一份 Provider 分支。
- 触发条件：拒绝统一扁平 Message 后，共享消费者仍需要有限的语义视图。
- 根因：NativeHistory 负责存储和恢复，但不负责面向共享层的只读语义投影。
- 架构影响：Provider Kernel 增加 HistoryProjector，输出不可逆的 SemanticHistoryView；共享消费者只使用该视图。
- 缓存影响：投影不参与请求 canonical bytes；token 估算不得改变稳定 prompt 前缀。
- 修复方案：每个 Provider 实现一次 native history -> semantic view，并以 golden fixture 验证 live/replay 一致。
- 未采用方案及原因：不把投影视图作为新事实源，也不允许它反向构造 Provider request。
- 回归测试：Provider projector golden、resume replay、Hook、Subagent 和 TUI semantic fixture。
- 关联 ADR/Issue/PR：ADR-0002。
- 后续行动：P1 先定义接口和最小视图，P2/P5/P6/P7 接入消费者。

### [P0][2026-09-29] 分支语义不能只依赖评审者记忆

- 状态：已解决仓库内门禁
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：分支可以使用工具名、用户名或无语义前缀，新增功能与缺陷修复无法从名称识别，也没有一致的本地和 CI 判定入口。
- 触发条件：开发者直接创建任意分支，或 CI 在 detached merge ref 上误判当前 ref。
- 根因：分支约定只存在于文字说明，hook 与 workflow 没有共享 validator。
- 架构影响：单一 POSIX 脚本按显式参数、可信 CI source branch、本地 symbolic ref 的顺序解析；pre-commit、pre-push 和 GitHub Actions 复用同一规则。
- 缓存影响：无。
- 修复方案：普通分支强制匹配语义前缀与 kebab-case，并通过表驱动 shell regression 覆盖 detached、merge ref 和非法名称。
- 未采用方案及原因：未复制 regex 到多个 hook/workflow，也未通过 commit message 猜测意图，因为两者都会产生漂移或歧义。
- 回归测试：`scripts/check-branch-name_test.sh`、`make branch-policy-test`、`.github/workflows/verify.yml`。
- 关联 ADR/Issue/PR：OpenSpec `harden-architecture-contract-compliance`；属于工程治理落实，不新增 ADR。
- 后续行动：管理员仍须把 `branch-governance` 与 `verify` 配为 `main` required checks 并禁止 direct push；仓库文件本身不会自动启用 ruleset。

## 4. P1 双 Provider Kernel

### [P1][2026-09-18] Resty v3 当前仍是 RC 版本

- 状态：接受风险
- 影响版本或提交：`resty.dev/v3 v3.0.0-rc.4`
- 现象：Go 模块代理当前没有 Resty v3 稳定版，最新为 RC。
- 触发条件：初始化 Go module 并解析 `resty.dev/v3`。
- 根因：Resty v3 尚未发布稳定标签。
- 架构影响：Resty 被限制在 `internal/provider/transport`，避免 RC API 泄漏到 Provider Kernel 和 Runtime。
- 缓存影响：无直接影响；升级可能改变 request/SSE 行为，因此需要重新检查最终 wire 字节。
- 修复方案：固定 `v3.0.0-rc.4`，通过 transport 契约测试保护；稳定版升级单独处理。
- 未采用方案及原因：未回退 Resty v2，因为项目明确要求使用 Resty v3。
- 回归测试：`internal/provider/transport/client_test.go`。
- 关联 ADR/Issue/PR：ADR-0001。
- 后续行动：每次升级 Resty 都运行双 Provider fixture、SSE chunk、重试和缓存 golden suite。

重点关注：SSE chunk 边界、未知 event、opaque 字段、usage 缺失和兼容服务能力降级。

### [P1][2026-09-18] Resty SSE 能力必须在 Provider Kernel 前置验证

- 状态：接受流程约束
- 影响版本或提交：P1 规划
- 现象：如果直到 RequestCompiler/StreamReducer 已实现后才发现 RC 的 SSE 逐帧消费、背压或取消语义不满足要求，返工范围会扩散到整个 Provider Kernel。
- 触发条件：直接把 Resty SSESource 当作已验证的底层契约。
- 根因：transport 风险被错误地安排在 P1 中段，而不是第一道 gate。
- 架构影响：P1 第一项必须是 transport spike；Resty 只负责 frame，Provider reducer 独立消费事件。
- 缓存影响：SSE frame 丢失或重复会导致 native item、usage 和 cache metrics 不一致。
- 修复方案：用 httptest/mock server 做随机 chunk、半包、UTF-8、取消、idle timeout 和断线 fixture；失败时在 transport 包内切换 raw body + 自研 parser。
- 未采用方案及原因：不带着未知 stream 风险继续实现上层 provider 策略。
- 回归测试：transport chunk-boundary、cancel、timeout 和 reconnect fixture。
- 关联 ADR/Issue/PR：ADR-0001、P1 Roadmap。
- 后续行动：SSE gate 通过前不开始完整 RequestCompiler/Reducer。

### [P1][2026-09-19] Streaming 使用 raw body 与内部有界 SSE parser

- 状态：已解决
- 影响版本或提交：OpenAI Responses text chat 纵向切片
- 现象：Resty v3 RC 的 `SSESource` 默认 event buffer 和 frame/lifecycle 行为不足以固定 4 MiB event 上限、CRLF/EOF、任意 chunk、取消与 idle timeout 契约。
- 触发条件：直接依赖 `SSESource` 回调和默认 buffer 作为 Provider stream 的事实边界。
- 根因：HTTP client 的便利 API 与项目所需的 SSE 协议、资源 owner 和终态语义并不等价。
- 架构影响：Resty 只负责 HTTP 请求和 raw response body；`internal/provider/transport` 独立解析 frame，OpenAI reducer 只解析 Responses JSON event。
- 缓存影响：稳定 JSON bytes 在请求前生成；parser 不接触 request fingerprint，避免 transport 行为污染缓存输入。
- 修复方案：使用有界 parser 和 supervisor，覆盖 LF/CRLF、comment、多行 data、随机 chunk、UTF-8、EOF、取消、idle timeout 和 body/reader 清理。
- 未采用方案及原因：未围绕 `SSESource` 增加兼容补丁，因为其隐含上限和关闭语义仍会泄漏到 Provider。
- 回归测试：`internal/provider/transport/client_test.go`、`internal/provider/openai/integration_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `openai-responses-text-chat-slice`。
- 后续行动：升级 Resty 或调整 event 上限时重跑 transport golden、race 和双轮集成测试。

### [P1][2026-09-18] Go struct embed 不提供 Template Method 动态分派

- 状态：已解决设计口径
- 影响版本或提交：P0 架构设计
- 现象：基类结构体内调用 `self.hook()` 时，外层 embed 类型的同名方法不会被动态分派；代码可以编译，运行时却静默执行基类实现。
- 触发条件：用 struct embed + 方法 shadow 模拟面向对象 Template Method。
- 根因：Go 方法调用是静态绑定，embed 只提升方法集，不改变接收者。
- 架构影响：共享 TurnRuntime 必须持有显式策略接口/函数依赖，禁止依赖 embed 覆写生命周期钩子。
- 缓存影响：错误的 hook dispatch 可能遗漏上下文、工具或 cache invalidation，产生难以定位的 prefix 漂移。
- 修复方案：采用“骨架持有策略接口”的组合形态，小接口通过构造器注入。
- 未采用方案及原因：不引入伪继承层次，也不依赖运行时反射分派。
- 回归测试：Template lifecycle、hook invocation 和 architecture boundary test。
- 关联 ADR/Issue/PR：总体架构 §17。
- 后续行动：代码评审遇到 embed override 一律要求改为组合。

### [P1][2026-09-18] Provider 差异按序列化、能力、策略三类隔离

- 状态：已解决设计口径
- 影响版本或提交：P0 架构设计
- 现象：仅记录“薄 adapter 承载不了差异”无法说明为何双轨成本值得承担，也无法指导未来第三家 Provider 的边界。
- 触发条件：Provider 新增字段或能力时尝试把差异塞进共享 Message/Capability 大对象。
- 根因：没有区分差异泄漏的方向。
- 架构影响：序列化差异留在 wire/native 包；能力差异通过 capability profile；策略差异通过 Kernel 小接口组合。
- 缓存影响：canonical bytes 只由 wire/strategy 控制，能力变化必须显式进入 cache plan revision。
- 修复方案：沿三类框架新增 Provider 边界评审和 fixture。
- 未采用方案及原因：不以“JSON 形状相似”作为共享抽象的依据。
- 回归测试：provider boundary、capability matrix 和 cache golden。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0004。
- 后续行动：新增 Provider 时必须在 OpenSpec proposal 中说明三类差异归属。

### [P1][2026-09-29] Provider compiler 必须唯一拥有 canonical request bytes

- 状态：已解决当前双 Provider 文本请求
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：golden/fingerprint 可以基于一份序列化结果，而 transport 又对 `Body any` 二次 marshal，真实 HTTP bytes 存在漂移入口。
- 触发条件：JSON codec 选项、字段默认值或调用方对象在编译后发生变化。
- 根因：request serialization 有 Provider 与 transport 两个 owner，缓存比较的对象不一定等于实际发送正文。
- 架构影响：OpenAI/Anthropic 各自使用 typed builder 和 compiler 生成不可变 canonical JSON；transport 只校验并发送深拷贝快照。
- 缓存影响：request golden、stable fingerprint、uninterrupted/restored 比较与真实 HTTP body 现在使用同一份 compiler 产物。
- 修复方案：移除 transport marshal，将 canonical 值对象的合法性、大小和不可变性固定在构造边界。
- 未采用方案及原因：未继续向 transport 传 typed request，因为那会保留第二个序列化点；也未暴露共享可变 `[]byte`。
- 回归测试：`internal/codec/json_test.go`、`internal/provider/transport/client_test.go`、双 Provider `request_test.go`、`integration_test.go`、`restore_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `harden-architecture-contract-compliance`；落实现有缓存与 Provider 边界，不新增 ADR。
- 后续行动：任何新 Provider/compiler 都必须证明网络正文与 golden bytes 逐字节一致。

### [P1][2026-09-29] OpenAI Responses 终态必须绑定 created identity

- 状态：已解决
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：无状态 reducer 只看 event type，会接受缺少 `response.created`、冲突 response ID 或不合法顺序的终态。
- 触发条件：兼容网关串流、乱序、重复 created/terminal，或不同 response 的 terminal 被混入同一连接。
- 根因：response identity 与事件状态分散在 Provider loop，reducer 没有 sample 级 owner。
- 架构影响：每条 stream 创建独立 reducer，固定 created ID、active 状态、有序 item 与唯一 terminal；failed/incomplete 也必须匹配 identity。
- 缓存影响：拒绝把错误 response 的输出提交进 native history，防止后续请求与恢复 fingerprint 分叉。
- 修复方案：非法顺序或 ID 冲突返回稳定 stream protocol fault；仅 active、non-terminal 状态下忽略未知 well-formed event。
- 未采用方案及原因：未只在 Provider loop 保存一个字符串，因为这会把状态转换和验证拆给两个 owner；也未兼容缺失 created 的非标准网关。
- 回归测试：`internal/provider/openai/reducer_test.go`、`provider_test.go`、`integration_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `harden-architecture-contract-compliance`；未改变 wire 能力范围，不新增 ADR。
- 后续行动：新增 Responses event 时先扩展同一状态机和 fixture，不绕过 identity 校验。

### [P1][2026-10-07] 消费端协议失败不能提前放弃 Provider channel

- 状态：已解决双 Provider 文本流生命周期
- 影响版本或提交：OpenSpec `harden-provider-stream-lifecycle`
- 现象：Runtime 收到零值事件、重复 terminal 或 terminal 后事件时立即返回，会停止读取有界输出 channel，使 Provider producer 阻塞在后续发送，Conversation 长期保持 active，并污染下一 turn 与 shutdown。
- 触发条件：Provider 或测试 fake 违反唯一 terminal 契约，或调用取消与 terminal/transport 清理并发发生。
- 根因：terminal 既被当作业务结果，又被误当作 producer 已退出的完成信号；消费 owner 没有在提前失败后继续承担排空职责。
- 架构影响：`StreamEvent` 改为五类封闭 typed constructor 与只读 accessor；Runtime 为每次 stream 派生 context，验证并保留首错，取消后同步排空到 producer 关闭。Provider 创建者仍唯一关闭 channel，channel close 才表示 transport、active 状态和 goroutine 清理完成。
- 缓存影响：不改变 request bytes、native history、Session records、usage 或 fingerprint；失败 sample 不 finalize，也不进入下一请求。
- 修复方案：首次消费协议错误立即取消派生 context，停止向宿主转发但继续读取；terminal 后任何事件都转为 `stream_protocol_error`，等待 channel close 后再 durable 写入唯一失败边界。AgentLoop 和 shutdown 复用同一完成信号，不增加 watchdog 或无 owner waiter。
- 未采用方案及原因：不启动后台 drainer，因为这会让 RunTurn 在清理未完成时返回；不让消费方关闭 Provider channel，因为 channel 仍由 producer 创建并拥有；不增加超时 goroutine，因为超时返回不能转移资源 ownership。
- 回归测试：`internal/provider/provider_test.go`、双 Provider `provider_test.go`/`integration_test.go`、`internal/runtime/runtime_test.go`、`internal/runtime/agent_loop_test.go`、`internal/headless/stream_runner_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `harden-provider-stream-lifecycle`；沿用现有 Provider/Runtime ownership，不新增 ADR。
- 后续行动：P3 Tool Loop 的多 sample stream 必须复用唯一 terminal、派生 context与同步排空契约，不得为工具边界引入第二个 channel owner。

## 5. P2 Session 与 Headless Agent Loop

### [P2][2026-09-28] JSONL 不能只复制 Claude Code 或 Codex 的表面报文

- 状态：已解决当前文本切片
- 影响版本或提交：OpenSpec `add-jsonl-session-resume`
- 现象：Claude Code 的 transcript 会混合用户/assistant/tool/progress 等消息，Codex rollout 则记录 response item、event 和上下文；二者都包含工具相关事实，但记录边界、恢复来源和宿主事件并不相同。直接照搬任一 JSONL 形状会把 Provider wire、运行时展示和副作用事实耦合。
- 触发条件：在工具尚未实现时先设计 Session schema，并要求未来无损承接 thinking、tool use 和 MCP call。
- 根因：把“JSONL 是容器”误当成“所有事件共享同一语义”。Provider native history、工具幂等 ledger 和 RuntimeEvent 实际具有不同提交时机与恢复责任。
- 架构影响：公共 envelope 只负责当前 canary、顺序、批次、归属与完整性；`provider_native_commit` payload 由对应 Provider 私有解码。当前 kinds 除会话与文本回合事实外，已包含 `tool_call_ready`、`tool_execution_started` 和 `tool_call_result`；未来 permission、hook、subagent、cache 使用独立强类型 kind，展示事实不得通过 optional record 绕过完整恢复校验。
- 缓存影响：Session envelope 和动态路径不参与 Provider 请求 canonical bytes；恢复后的请求字节必须与未退出进程的下一轮一致。
- 修复方案：OpenAI 保存 user input item 与有序 output items，Anthropic 保存 user/assistant message、metadata 与 usage presence；共享层只传递 opaque envelope。Read tool result 已在副作用 durable 后作为下一次请求的 input-only native 增量提交，不重复 assistant tool call；MCP call 未来复用工具事实边界，但其 manifest/capability 另行版本化。
- 未采用方案及原因：未将 tool/thinking/MCP 预先塞入通用 `map[string]any`，也未以 UI transcript 反向构造 Provider 请求；这些做法无法保证类型、幂等和 opaque reasoning 无损。
- 回归测试：`internal/provider/openai/commit_test.go`、`internal/provider/anthropic/commit_test.go`、`internal/app/resume_e2e_test.go`。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0003；OpenSpec `add-jsonl-session-resume`。
- 后续行动：Read 的 tool call/result ledger、golden 和恢复 fixture 已由 `add-read-tool-loop` 补齐；MCP、subagent、compaction 分别在对应阶段扩展，不修改既有 payload。

### [P2][2026-09-28] 恢复正确性需要 envelope、batch、语义三层校验

- 状态：已解决当前 schema
- 影响版本或提交：OpenSpec `add-jsonl-session-resume`
- 现象：只校验每行 JSON 可解析，仍可能接受换序、跨线程混写、未知 required 记录、半个成功提交或完整 JSON 但未写完的尾批次。
- 触发条件：进程在多行 commit 中途退出、文件尾部半行、人工修改、磁盘错误或错误版本的 reader 打开新 schema。
- 根因：单行 checksum 不能表达多记录原子边界，也不能判断一组记录能否形成可恢复的 Provider 历史。
- 架构影响：Loader 第一层严格验证单一当前 schema/payload canary、canonical UUIDv7/UTC、checksum、文件归属和单调 seq；第二层只向 ReplayPlanner 暴露完整连续 batch；第三层由 Provider 事务式解码全部 native commits，任一错误都不返回部分历史。
- 缓存影响：阻止损坏历史生成看似合法但字节不同的续写请求。
- 修复方案：只允许修复 EOF 尾部半行和最后一个未完成 batch，修复后截断并 `Sync`；中段损坏、完整但校验失败的末行、未知 required 版本全部硬失败。若完整日志以未闭合 turn 结束，恢复先追加 `turn_failed(code=session_interrupted)` 补偿记录，再开始新 turn。
- 未采用方案及原因：不跳过坏行继续扫描，不自动猜测未知 required payload，也不把尾批次中的部分 native commit 交给 Provider。
- 回归测试：`internal/session/loader_test.go`、`internal/session/replay_test.go`、`internal/session/current_fixture_test.go`、`internal/app/resume_e2e_test.go`。
- 关联 ADR/Issue/PR：ADR-0003；OpenSpec `add-jsonl-session-resume`。
- 后续行动：稳定发布后首次引入真实 schema/payload revision 时必须先批准兼容窗口，并增加从不可变历史 fixture 到当前 replay model 的版本专属 regression；SQLite 重建只能消费相同 ReplayPlanner 输出。

### [P2][2026-09-28] 进程内 writer 串行化不能替代 journal 跨进程所有权

- 状态：已解决当前协作进程边界
- 影响版本或提交：OpenSpec `harden-session-journal-ownership`
- 现象：旧实现只在单个 `JournalWriter` 实例内串行化 append，Loader 在 load/repair 后关闭文件，再重新打开续写；两个 Repository 或进程可能从相同 seq 写入，活动 writer 存在时另一个进程也可能截断 torn tail。
- 触发条件：两个 EasyCode 进程同时 resume 同一 thread，或第二个进程在第一个 writer 活跃期间打开带损坏尾部的 journal。
- 根因：把 goroutine 级 actor 顺序误当成 thread journal 的完整所有权，且 load/repair 与 writer 启动之间存在无锁窗口。
- 架构影响：Repository 的可写入口只返回绑定实际 journal handle 的 exclusive lease；同一 lease 从首个 record 读取前覆盖 repair、ReplayPlanner、配置校验、Provider 事务式恢复、interrupted-tail 补偿和 writer 最终 `Sync`/关闭。busy 使用稳定 `session_busy`，在读取、Provider 恢复、TUI 或 journal 修改前快速失败。
- 缓存影响：lease、路径、PID 和时间状态不进入 Provider 请求；双 Provider 回归继续保证 uninterrupted 与 restored 请求的 canonical bytes、native 顺序和 fingerprint 等价。
- 修复方案：Darwin/Linux/Windows 使用非阻塞 OS file lock，Loader 借用、writer 单向接管同一 lease；append admission 使用有界非阻塞队列，Close 先线性化 closing 后继续 drain。真实子进程通过 pipe ready/control 握手验证竞争、正常关闭和异常退出释放，不用 sleep 猜测锁状态。
- 未采用方案及原因：不使用 PID lockfile、TTL 清理或进程级全局 mutex；它们不能绑定当前 journal handle，且会引入 stale metadata 或无法保护其他进程。
- 回归测试：`internal/session/lease_test.go`、`internal/session/writer_test.go`、`internal/session/current_fixture_test.go`、`internal/app/session_process_test.go`、`internal/app/session_service_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `harden-session-journal-ownership`。
- 后续行动：OS file lock 是 advisory lock，只约束当前协作版本；旧版或非协作进程仍可绕过。发布或回滚时禁止新旧二进制同时写同一 thread，绕过锁的修改继续依靠 checksum、seq 和完整回放校验判损。

### [P2][2026-09-28] Provider 内存提交必须晚于 durable batch

- 状态：已解决
- 影响版本或提交：OpenSpec `add-jsonl-session-resume`
- 现象：若 Provider 在收到成功终态时立即把输出写入内存，而 JSONL 随后写盘失败，当前进程能继续携带一段重启后不存在的历史；反过来先发布成功再持久化也会让 UI 与恢复状态分叉。
- 触发条件：成功 stream 终态之后发生短写、`Sync` 失败、取消与 completed 竞态，或 finalizer 被重复调用。
- 根因：Provider reducer、Session writer 和 Runtime terminal 缺少明确提交接缝。
- 架构影响：Provider 只生成 opaque `NativeCommitEnvelope` 和一次性 `PreparedSample`；Runtime 按“写入 native commit + turn complete 的完整 batch并 `Sync` -> finalize Provider memory -> 发布成功 terminal”排序。持久化结果不确定时 Runtime poison，禁止同实例继续采样。
- 缓存影响：同一 committed history 在 live 与 resume 路径产生相同下一请求 bytes/fingerprint。
- 修复方案：finalizer 使用 once 语义，Provider restore 先在临时历史事务式验证，再整体替换。取消/失败不生成 native commit；completed 已形成 prepared result 时，晚到取消不能撤销 durable 提交。
- 未采用方案及原因：不使用异步 best-effort writer，也不允许从 RuntimeEvent 回放 Provider history。
- 回归测试：`internal/runtime/runtime_test.go`、双 Provider `commit_test.go`、`internal/app/resume_e2e_test.go`。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0003；OpenSpec `add-jsonl-session-resume`。
- 后续行动：Read tool result 已沿用同一 durable-before-memory 原则；其他副作用继续复用该边界。compaction/fork 未来只能 append checkpoint/cursor 与 child metadata，不得重写原日志。

### [P2][2026-09-29] 机器 stdout 不能直接复用内部 RuntimeEvent

- 状态：已解决当前文本 headless 切片
- 影响版本或提交：OpenSpec `add-headless-text-output`
- 现象：直接输出 RuntimeEvent 会暴露 timestamp、opaque payload 和未实现 kind；文本 delta 边到边写又会让失败的 shell 命令留下看似成功的部分结果。stdout 写失败后若继续输出错误对象，还会形成不可解析的混合或半行协议。
- 触发条件：为 `--print/--json` 复用 TUI event envelope、从 transcript 提取最终答案，或让多个 goroutine 直接写 stdout。
- 根因：进程内宿主协议、Session 事实源和外部机器协议具有不同兼容性、提交时机与清理责任，不能因都使用 JSON 就合并。
- 架构影响：`internal/headless` 与 TUI 平级，只消费最小 `ChatSession`；JSONL v1 使用独立封闭 event union、严格身份/顺序校验和单 writer 背压。文本结果晚于 durable `turn_completed` 与 stream 闭合；app outcome 区分已报告终态和 stdout 失效，避免 cmd 重复输出。
- 缓存影响：headless 不读取 `SemanticHistoryView` 构造请求，也不把输出事件写入 Session；双 Provider 回归证明 uninterrupted 与 restored 的下一请求 canonical bytes 和 fingerprint 等价。
- 修复方案：在装配前完成 4 MiB 有界 UTF-8 prompt 解析；JSON encoder 先生成完整单行 object 再处理短写，首次失败后永久停用 stdout；取消和断管都执行一次 Interrupt 并 drain Runtime，再按唯一 owner 顺序释放 journal lease 和 Provider。
- 未采用方案及原因：不直接 marshal RuntimeEvent，不抓取 TUI transcript，不安装全局 stdout guard，也不提前声明 usage/reasoning/tool 机器事件；这些方案会固化内部字段、污染 stdout 或暴露没有完整 producer/persistence 的能力。
- 回归测试：`internal/headless/*_test.go`、`internal/app/headless_e2e_test.go`、`cmd/easycode/main_test.go`、双 Provider restore/fingerprint tests。
- 关联 ADR/Issue/PR：OpenSpec `add-headless-text-output`。
- 后续行动：新增外部事件必须通过 OpenSpec 演进 JSONL fixture；stdin JSON 与双向控制已由独立 streaming DTO/fixture 交付，不扩宽一次性协议；独立 usage 更新和 reasoning/tool 事件仍未实现。`turn.completed` v1 已通过 `add-durable-sample-usage` 增加 required usage；`--continue` 复用同一 resume/headless 投影路径。

### [P2][2026-10-05] Follow-up queue 不能伪装成 same-turn steer 或 durable acceptance

- 状态：已解决当前同进程多 turn 切片
- 影响版本或提交：OpenSpec `add-runtime-input-control-loop`
- 现象：活动 turn 期间收到的新输入容易被直接注入当前 prompt、与其他输入拼接，或仅凭 `queued` response 被调用方误认为已经写入 Session；stdin EOF 也容易被错误实现为取消当前工作。
- 触发条件：长期 headless 进程同时处理 submit、interrupt、EOF/shutdown 和 Provider terminal，且当前 turn 只有一次 sampling、没有 Tool Loop safe point。
- 根因：把 admission obligation、durable turn start 和模型内部 steer 混为同一语义，并让 reader/worker 分别读写共享 active/queue 状态。
- 架构影响：Session-bound AgentLoop 成为唯一 admission owner；活动期输入只进入有界 FIFO，每项形成后续独立 turn。command result 先输出，只有 `turn.started` 表示 durable；request identity、queue 与 control output 不进入 Session 或 Provider history。stdout 仍由唯一 writer 投影独立 streaming DTO。
- 缓存影响：control metadata 不参与 Provider request canonical bytes、ContextPlan segment 或 fingerprint；双 Provider 同进程回归与直接 facade 请求 bytes/fingerprint 等价。
- 修复方案：EOF 进入 draining 并执行全部已接受输入；显式 shutdown 进入 closing、取消活动 turn并发布 queue discard。interrupt 必须匹配活动 `turn_id`。caller context 只参与无缓冲 handoff，owner 接受后不再保存或观察该 context。
- 未采用方案及原因：未实现 Claude Code 风格全局 priority queue、`now/next/later` 或 same-turn injection，因为当前没有能维持 Provider-native history 与工具配对的安全插入点；这些能力留待 P3 Tool Loop。
- 回归测试：`internal/protocol/command_test.go`、`internal/runtime/agent_loop_test.go`、`internal/headless/stream_*_test.go`、`internal/app/headless_e2e_test.go`、`cmd/easycode/main_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `add-runtime-input-control-loop`。
- 后续行动：P3 定义 tool result 后的 sampling safe point，再单独设计 same-turn steer；TUI 需要 queue 交互时迁移到 AgentLoop 并删除旧 ChatSession facade。

### [P2][2026-09-29] 路径预检不能代替绑定实际句柄的安全判断

- 状态：已解决 macOS/Linux；其他平台失败关闭
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：先 `Lstat` 再 `Open` 时，攻击者可在两步之间把配置、日期目录或 journal 替换为 symlink/其他对象。
- 触发条件：路径组件在检查与打开之间发生确定性交换，尤其是 load/repair 与 writer 接管边界。
- 根因：安全判断绑定路径名的旧时快照，而后续读写绑定另一个实际对象。
- 架构影响：配置使用 no-follow opener；Session 从已打开父目录以 `openat/mkdirat` 逐级取得 handle，lease、load、repair 和 writer 复用最终 journal handle。
- 缓存影响：无直接影响；阻止数据根外内容被读入 Session 后污染恢复请求。
- 修复方案：从同一实际 handle 校验类型、权限和大小，使用真实 symlink 与故障注入交换点做确定性回归；不支持的平台明确失败关闭。
- 未采用方案及原因：未保留 `Lstat` + `Open` 或仅做字符串 containment，因为都不能证明检查对象就是使用对象。
- 回归测试：`internal/config/config_test.go`、`internal/session/repository_security_test.go`、`internal/session/lease_test.go`、`internal/app/session_service_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `harden-architecture-contract-compliance`；落实既有安全硬约束，不新增 ADR。
- 后续行动：P8 为 Windows 等平台实现等价 handle-relative 语义并运行真实平台测试；此前不得安全降级。

### [P2][2026-09-29] Session 核心 payload 不能通过 any 与反射 registry 传播

- 状态：已解决当前强类型记录集合
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：导出的 `RecordDraft.Payload any` 与 `DecodePayload() any` 允许调用方拼出 kind/payload mismatch，错误只能在写入或 replay 时通过 type assertion 暴露。
- 触发条件：Runtime/app 直接构造 draft，或 registry 用 reflect 分配 payload 后遗漏 revision-specific 语义校验。
- 根因：异构记录为了复用一个动态入口而牺牲了构造合法性与协议边界。
- 架构影响：`RecordDraft` 成为 sealed 值类型；每个当前 kind 各有 typed constructor、strict decoder 和 validator，descriptor 只返回常量元数据。
- 缓存影响：保持当前 JSONL canonical bytes、checksum 与恢复后 Provider request bytes 确定。
- 修复方案：constructor 立即验证并编码独立 `json.RawMessage`，ReplayPlanner 通过显式 switch 调用目标 decoder。
- 未采用方案及原因：未把动态类型转移到 callback interface 或泛型 registry，因为仍会隐藏 kind 契约；产品尚未稳定发布，因此不保留旧开发 wire reader。
- 回归测试：`internal/session/draft_test.go`、`codec_test.go`、`replay_test.go`、`current_fixture_test.go`。
- 关联 ADR/Issue/PR：ADR-0003；OpenSpec `harden-architecture-contract-compliance`，不新增长期决策。
- 后续行动：稳定发布后若引入真实 revision，必须同时增加 typed constructor、strict decoder、validator、不可变 migration fixture、兼容窗口和删除条件。

### [P2][2026-09-29] Durable append 前必须重新验证 PreparedSample

- 状态：已解决
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：导出类型的零值或非法复制可能让 Runtime 在 Provider constructor 之外收到无效 sample，并退化成空 envelope 或错误 finalizer 顺序。
- 触发条件：错误/恶意 Conversation 返回 nil、零值、已 finalize 或内部 payload 非法的完成样本。
- 根因：Runtime 只信任 Provider 创建路径，没有在不可逆 durable append 前守住自己的输入边界。
- 架构影响：envelope clone/validate 显式返回 error；Runtime 在构造 typed draft 和写盘前重新复制验证，并只在成功 `Sync` 后调用一次 finalizer。
- 缓存影响：无效样本不能进入 native history，避免 live memory、JSONL 和 restored request bytes 分叉。
- 修复方案：无效完成样本只 durable 收口 `turn_failed`，不得写 native commit/turn completed，也不得运行 finalizer。
- 未采用方案及原因：未依赖“正常 Provider 不会返回非法值”的假设，因为 Go 导出类型始终可构造零值，Runtime 必须独立防御。
- 回归测试：`internal/provider/provider_test.go`、`internal/runtime/runtime_test.go`、双 Provider commit/restore tests。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0003；OpenSpec `harden-architecture-contract-compliance`，不新增 ADR。
- 后续行动：未来工具副作用或其他 prepared commit 复用同一重验与 durable-before-memory 边界。

### [P2][2026-09-30] 会话发现不能把文件属性或派生数据库当作事实

- 状态：已解决当前 Catalog v1 与 `--continue` 切片
- 影响版本或提交：OpenSpec `add-rebuildable-session-catalog`
- 现象：若直接按 journal mtime 排序、字符串读取 head/tail 提取 metadata，或把 SQLite row 当成可恢复历史，外部触碰文件、半写 batch、未知 required revision 和陈旧索引都可能让应用选择错误会话。
- 触发条件：实现最近会话发现、删除后索引重建、活动 writer 协调或选择后恢复时绕过 Loader/ReplayPlanner。
- 根因：文件系统属性、轻量文本扫描和派生索引都无法证明 envelope、checksum、batch、lifecycle 与 Provider-native payload 已完整验证。
- 架构影响：JSONL 保持唯一事实源；Catalog 通过 descriptor-relative 枚举、逐 journal exclusive lease、Loader、ReplayPlanner 和纯 Projector 生成最小 SQLite 投影。`--continue` 只从精确兼容 row 选择 thread ID，关闭 Catalog 后仍进入既有 `sessionService.resume` 重新校验并取得连续 writer lease。
- 缓存影响：Catalog path、record timestamp、session/thread identity 和 SQLite metadata 不进入 Provider request；双 Provider e2e 比较 uninterrupted、显式 resume 与 continue 的实际 request bytes、native item 顺序和 fingerprint。
- 修复方案：recency 使用最后 committed record timestamp，并以 canonical thread ID 降序打破平局；reconciliation 在 transaction 外完成全部文件 I/O且任一时刻只持有一个 journal lease，短 transaction 只应用已验证投影。数据库缺失、损坏或 schema 不兼容时从 JSONL 原子重建。
- 未采用方案及原因：不采用 mtime 排序，因为它可被外部修改；不采用字符串 head/tail，因为它会形成绕过 strict decoder 的第二套解析器；不采用 DB-as-truth 或 DB-first 快路径，因为当前没有可靠实时索引维护，陈旧 row 不能替代恢复事实。
- 回归测试：`internal/session/catalog/*_test.go`、`internal/session/repository_enumeration_test.go`、`internal/app/continue_test.go`、`internal/app/headless_e2e_test.go`、`cmd/easycode/main_test.go`。
- 关联 ADR/Issue/PR：ADR-0003；OpenSpec `add-rebuildable-session-catalog`，未引入新的长期事实源决策。
- 后续行动：session picker、worktree/project catalog、title/tag/search 和实时索引仍延期；引入可靠增量维护前继续执行前台全量 reconciliation。2026-09-30 在 Darwin/arm64 Apple M1 Pro 上对 25 个 journal 的一次开发基准为约 5.55 ms、4.94 MB、14934 allocs/op，仅作为后续优化对照，不作为跨平台阈值。

### [P2][2026-10-04] Usage 必须与 Provider sample 共享 durable 边界

- 状态：已解决当前文本 sample 切片
- 影响版本或提交：OpenSpec `add-durable-sample-usage`
- 现象：只在完成事件中临时计算 usage 会在崩溃恢复后丢失事实；只保存 turn 累计量又无法与 Provider 原始响应审计对应。把缺失字段当成零还会制造虚假的精确数据。
- 触发条件：Provider completed response 包含完整、部分或显式为零的 usage，随后发生 Session 写入失败、进程恢复或 headless 投影。
- 根因：raw Provider 观测、共享 normalized sample 事实和 turn/宿主投影属于不同所有权层，但此前没有同一个 prepared/durable 边界。
- 架构影响：`PreparedSample` 原子携带 native envelope 与不可变 `SampleUsage`；Runtime 将 `provider_native_commit`、`sample_usage` 和 `turn_completed` 作为唯一 v1 成功 batch Sync，再 finalize 并发布带 usage 的完成事件。Session、protocol 和 headless 各自拥有强类型 wire DTO。
- 缓存影响：raw/normalized usage 不进入 RequestCompiler 或 fingerprint；后续缓存比例只能读取 normalized 三态指标，不能直接混用两家原始字段。
- 修复方案：五项指标显式区分 known、unknown、not-applicable，保留 `known:0`；两家 Provider 保留 raw usage 并投影 normalized usage，恢复与不中断请求的 canonical bytes/fingerprint 保持一致。
- 未采用方案及原因：不新增 v2 或双 reader，因为当前契约尚未稳定发布，保留错误开发基线只会制造永久兼容分支；不把 normalized usage 塞进 opaque native payload，因为 Session 不应解释 Provider wire。
- 回归测试：`internal/domain/usage_test.go`、双 Provider reducer/commit/restore tests、`internal/session/*_test.go`、`internal/runtime/runtime_test.go`、`internal/headless/*_test.go`、`cmd/easycode/main_test.go` 和双 Provider app e2e。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0003；OpenSpec `add-durable-sample-usage`，不新增 ADR。
- 后续行动：成本、配额、TUI usage 展示、缓存请求策略和多 sample 工具回合仍需独立 change；ContextPlanner/token estimator 已由 `add-deterministic-context-planning` 落地。只有冻结契约无法加法演进且旧消费者必须并存时才新增 revision。

### [P2][2026-10-04] 上下文占用不能由累计 usage 或单一语义视图推断

- 状态：已解决确定性规划基线
- 影响版本或提交：OpenSpec `add-deterministic-context-planning`
- 现象：累计每轮 normalized usage 会重复计算不断增长的历史前缀；只估算 `SemanticHistoryView` 又会漏掉 thinking、signature、redacted thinking 和 encrypted reasoning；把两种估算相加还会再次计算可见文本。根据 model 字符串维护窗口表则可能对自定义 `base_url` 做出错误硬拒绝。
- 触发条件：请求前估算下一轮上下文、恢复包含 opaque reasoning 的会话，或对用户配置的 context window 执行本地准入判断。
- 根因：Provider sample usage、可见语义投影、下一请求实际重放的 native history 和模型窗口属于不同事实边界，不能互相替代。
- 架构影响：ContextPlanner 只组合强类型快照；共享层从 `SemanticHistoryView` 估算可见文本，每个 Provider 从 committed native history 生成只含 family/revision/method/state/tokens 的 footprint。Runtime 在 durable `turn_started` 后、Provider stream 前完成规划。
- 缓存影响：semantic estimate 与 native footprint 使用 `max` 覆盖合并而不是相加；normalized usage 不进入 context occupancy、request compiler 或 cache fingerprint。只有 `provider_profile` 进入当前稳定前缀，history 与 input 保持尾部稳定级别。
- 修复方案：使用版本化本地 `byte_heuristic_v1` 和饱和运算，显式保留 `estimated/unknown`；窗口、输出预留和安全余量只接受用户 JSON 配置，不根据 model 名称推断。只有配置存在、估算完整且总量严格超过有效上限时返回 `context_limit_exceeded`。
- 未采用方案及原因：不调用远程 token count，避免规划引入网络副作用；不从 normalized usage 累计上下文，不从语义历史重建 Provider 请求；不把 prompt、opaque bytes 或完整计划写入 Session/Event/日志。
- 回归测试：`internal/domain/context_estimate_test.go`、`internal/context/estimate/*_test.go`、`internal/context/planning_test.go`、双 Provider `footprint_test.go`/restore tests、`internal/runtime/runtime_test.go`、`internal/config/config_test.go` 和 `internal/app/resume_e2e_test.go`。
- 关联 ADR/Issue/PR：ADR-0002；OpenSpec `add-deterministic-context-planning`，不新增 ADR。
- 后续行动：精确 tokenizer、远程计数、项目指令、tool/skill sources、实际 Provider cache policy、compaction 和 context UI 必须分别通过后续 change 接入现有来源/方法契约。

### [P2][2026-10-07] 项目指令发现与会话恢复必须是两个独立事实边界

- 状态：已解决层级项目指令切片
- 影响版本或提交：OpenSpec `add-hierarchical-project-instructions`
- 现象：若 resume 从 Session `creation_cwd` 重读旧规则，或把项目指令提交为普通 user history，新进程会使用过期上下文，连续轮次还会重复叠加规则并污染 native history。
- 触发条件：项目文件在进程运行期间变化、从不同绝对目录恢复同一 thread，或 RequestCompiler 与 finalizer 共用同一个 user item。
- 根因：启动环境上下文、Provider-native 对话事实和 Session 恢复事实具有不同生命周期；把三者合并会让热更新、缓存 revision 和历史重放互相污染。
- 架构影响：应用在 Session/Catalog/Provider 打开前只发现一次不可变快照；Runtime 构造期深拷贝并覆盖宿主逐轮附着值；Provider RequestCompiler 单独生成临时 context，consumer/finalizer 只持有真实 user item。
- 缓存影响：快照 canonical bytes 只包含 renderer/schema revision、项目根相对来源、正文、截断状态和预算；mtime、inode、绝对 cwd 与 Session identity 不参与 `project_stable` fingerprint。空快照不改变既有请求 bytes。
- 修复方案：Darwin/Linux 使用目录 fd、`openat`/`fstatat`、`O_NOFOLLOW` 和打开后 `fstat` 绑定检查与读取；拒绝 symlink/特殊文件/非法 UTF-8，按 root-to-cwd 顺序在包含 framing 的默认 32 KiB 总预算内截断。Loader 与领域快照共享 4 MiB 硬上限，极大参数在读取和预分配前拒绝；canonical bytes 统一由 `internal/codec` 生成。resume 使用当前启动目录重新发现，既有 journal 和 native commits 保持不变。
- 未采用方案及原因：不采用 `Lstat` 后按路径 `ReadFile`，因为存在 TOCTOU；不采用 lossy UTF-8 或跟随 symlink，避免隐藏来源变化；不把快照写入 Session，因为它不是对话事实；不实现文件 watcher，避免活动会话中途改变请求前缀。
- 回归测试：`internal/context/projectinstructions/*_test.go`、`internal/domain/project_instructions_test.go`、`internal/context/planning_test.go`、双 Provider request/integration/restore tests、`internal/runtime/runtime_test.go`、`internal/app/project_instructions_test.go` 和双 Provider app e2e。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0003；OpenSpec `add-hierarchical-project-instructions`，不新增长期 ADR。
- 后续行动：全局用户指令、includes、`.claude/rules`、权限 world state 与 tool/skill context 需独立 change，并继续保持来源、生命周期与缓存稳定性正交。

### [P2][2026-10-10] 未上线阶段保留多版本 reader 会把开发形状固化成产品债务

- 状态：已解决（`add-search-tools-and-ordered-parallelism`）
- 影响版本或提交：内部 protocol/context、Tool catalog/result、Session envelope
- 现象：固定 `Version()`、Tool input/result revision、Session required/optional registry 和按版本 fixture 同时存在时，每次契约调整都要求维护并未发布的旧 reader，调用方也会开始按版本分支。
- 触发条件：把尚无用户的开发 fixture 当成已发布兼容承诺，或把内容 fingerprint、外部 wire version 与进程内固定版本混为一谈。
- 根因：没有区分发布边界的 canary/外部协议和进程内单一当前强类型值，也没有为兼容代码设定进入条件与退出条件。
- 架构影响：进程内 Command/Event/ContextPlan 不携带固定版本；Tool catalog 只使用内容 fingerprint；Session 只保留唯一当前 canary 与按 kind 的唯一 decoder。Provider native payload、headless JSONL v1 和 SQLite `user_version` 仍是明确边界，不受此规则误伤。
- 缓存影响：删除虚构 revision 后，Tool segment 只由排序后的 facade 名称、描述与 canonical schema bytes 失效，动态 Session/cwd/time 不进入稳定前缀。
- 修复方案：直接替换 `testdata/current`，旧开发 shape 在 repair/恢复前失败关闭，并增加 AST/fixture 目录架构守卫禁止 V1/V2/Legacy、optional API 和 version 参数 registry 回流。
- 未采用方案及原因：不保留旧 reader、migration graph 或 feature flag，因为当前没有已发布数据需要兼容；也不删除 Provider/Session/headless/SQLite 的真实边界 canary。
- 回归测试：`internal/architecture/architecture_test.go`、`internal/session/current_fixture_test.go`、`internal/headless/*_test.go`、双 Provider restore tests。
- 关联 ADR/Issue/PR：OpenSpec `add-search-tools-and-ordered-parallelism`；这是发布前演进规则，不新增长期 ADR。
- 后续行动：稳定发布后首次需要破坏性 revision 时，必须先通过 OpenSpec 明确不可变历史 fixture、兼容窗口、迁移方向和删除条件。

重点关注：JSONL 尾部损坏、事件顺序、取消时 flush、SQLite 重建和 native history 恢复。

## 6. P3 Coding Tools 与安全执行

以下条目包含 2026-10-07 对 Claude Code 与 Codex 参考实现的设计调查，以及 `add-read-tool-loop` 实现期间验证过的具体风险。尚未落地的条目保持“发现”；已由实现与回归测试闭环的条目标记为“已解决”。

### [P3][2026-10-07] Call ID 不能把任意外部副作用变成 exactly-once

- 状态：发现（设计阶段）
- 影响版本或提交：P3 Tool 架构探索，尚未进入实现 change
- 现象：若把 call ID 去重描述为“保证不重复副作用”，进程可能在外部写入成功后、结果 durable 前崩溃；恢复器无法判断副作用是否发生，自动重试可能造成第二次写入或命令执行。
- 触发条件：`execution_started` 后执行文件、进程或网络副作用，随后在结果成功 `Sync` 前崩溃。
- 根因：本地 ledger 只能证明 EasyCode 是否已经接收调用，不能与所有外部系统建立原子提交。
- 架构影响：ledger 的统一承诺限定为自动执行至多一次；`execution_started_durable` 是接收线性化点，开始后无确定结果恢复为 `outcome_uncertain` 并禁止自动重跑。只有 executor 提供可靠 idempotency key 或可验证提交协议时，具体 capability 才能声明更强语义。
- 缓存影响：不确定状态和 ledger identity 不进入稳定 prompt 前缀；补偿 output 必须保持 Provider-native call/output 配对。
- 修复方案：先 durable ready 和 execution start，再执行；结果成功后单独 durable。恢复器对 started-without-result 失败关闭并要求显式处置。
- 未采用方案及原因：不使用参数哈希推断“相同调用”，因为相同参数可能是用户有意重复；不把未知结果当失败自动重试，因为会扩大副作用。
- 回归测试：规划随首个副作用工具 change 增加确定性崩溃点、重复 invocation、结果 `Sync` 失败、恢复不重跑和补偿 output fixture。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；后续对应 OpenSpec change。
- 后续行动：所有 Tool UI 和文档统一使用“至多一次自动执行”和“不确定结果”，禁止笼统宣传 exactly-once。

### [P3][2026-10-07] Provider sample 未 durable 前执行工具会制造不可恢复历史

- 状态：发现（设计阶段）
- 影响版本或提交：P3 Tool 架构探索，尚未进入实现 change
- 现象：Claude Code 可在模型流尚未结束时执行已完成的 tool block；如果直接复制该低延迟行为，工具可能已产生副作用，但包含该 call 的 Provider-native sample 尚未写入 Session，崩溃后无法合法恢复调用来源和结果配对。
- 触发条件：完整 tool block 先于 response terminal 到达，Runtime 立即执行，随后 stream、validation 或 Session append 失败。
- 根因：把 UI 可展示的流式完成、Provider sample 完成和副作用 durable 前置事实混为一个边界。
- 架构影响：P3 首版必须等待完整 `PreparedSample`，将 native commit、sample usage 和全部 ready calls 作为一个 batch 成功 `Sync` 后才能执行。流内提前执行只能由未来独立 change 重新证明协议。
- 缓存影响：只有 durable native history 能进入下一次请求；draft、delta 和未提交 sample 不参与 fingerprint 或续写。
- 修复方案：Runtime 统一拥有 `sample -> validate -> durable -> execute -> durable outputs -> next sample` 生命周期；draft 只投影给宿主。
- 未采用方案及原因：不为了首版 latency 复制推测执行，也不把 RuntimeEvent 当成崩溃恢复事实，因为两者都无法保证原生历史完整。
- 回归测试：规划在 `add-read-tool-loop` 覆盖 stream terminal 失败、durable batch 失败时零 executor 调用、成功 `Sync` 后才调用，以及 uninterrupted/restored 下一请求等价。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；推荐 OpenSpec `add-read-tool-loop`。
- 后续行动：首个 Read 闭环先证明该边界，再评估是否存在值得单独优化的流内执行延迟。

### [P3][2026-10-07] 每个 Tool Call 都必须得到合法 Output 才能继续采样

- 状态：发现（设计阶段）
- 影响版本或提交：P3 Tool 架构探索，尚未进入实现 change
- 现象：验证失败、权限拒绝、取消或崩溃恢复如果只发布 `turn_failed`，Provider-native history 会留下 call 而没有匹配 output；下一次采样可能被 Provider 拒绝，或让恢复与不中断路径产生不同请求。
- 触发条件：tool call 已进入 durable native history，但执行没有正常成功结果。
- 根因：把 Runtime turn 终态误当成 Provider call/output 配对修复机制。
- 架构影响：成功、验证失败、拒绝、取消、executor 失败和 `outcome_uncertain` 都必须生成与原 call 类型及 ID 匹配的 Provider-native output，再决定是否继续或终止 turn。
- 缓存影响：补偿 output 是 native history 的一部分；其内容和顺序必须确定，恢复与不中断 fingerprint 必须等价。
- 修复方案：由 Provider `ToolResultCodec` 编码强类型结果，Runtime 按原 call index 收集并 durable；Session replay 只依据原生 item 与 ledger，不从展示事件猜测。
- 未采用方案及原因：不删除已提交 call，也不把所有错误压成宿主日志，因为这会破坏 append-only 历史或 Provider 协议。
- 回归测试：规划覆盖双 Provider 的拒绝、取消、验证失败、部分并发失败、崩溃补偿和 resume request golden。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；后续 Tool Loop OpenSpec changes。
- 后续行动：任何新增 Provider tool wire 必须同时提供所有非成功终态的 output fixture。

### [P3][2026-10-07] Resume 不能成为隐式工具执行或 Provider 请求入口

- 状态：已解决（`add-read-tool-loop`）
- 影响版本或提交：P3 Read Tool Loop
- 现象：旧设计曾把ready-without-started定义为resume后继续执行，并把output-committed定义为自动继续sampling。这会让一个看似只读的恢复命令在用户没有新输入时访问文件或网络；未来替换为写工具后还会放大成隐式副作用。
- 触发条件：进程在ready、started、result或tool-output任一durable点退出，随后用户执行resume/continue。
- 根因：混淆“补齐Provider call/output pairing”与“继续完成旧用户请求”，并把Read低风险误当成通用恢复语义。
- 参考实现证据：Claude Code的streaming tool executor会为排队取消的调用直接合成paired `tool_result`，query取消路径会排空剩余调用以生成结果，消息修复对缺失结果只合成占位而不重执行；Codex在调度前先持久化call item，取消dispatch后返回 `aborted by user` output，history normalization对缺失output插入 `aborted`。两者都优先恢复配对，而不是在resume时重放工具副作用。
- 架构影响：正常路径保持 `ready -> started -> result`；只有started接受前的取消允许 `ready -> cancelled result`。resume将ready关闭为 `session_interrupted_before_execution`，将started-without-result关闭为 `outcome_uncertain`，复用已有preview补交output，并以唯一 `turn_failed`关闭旧turn。
- 缓存影响：本地补偿后的native history是下一次用户请求的唯一事实源；恢复本身不生成请求。相同reconciled history在当前进程和再次重启后的下一请求canonical bytes必须一致。
- 修复方案：ReplayPlanner只输出sealed typed recovery plan；Runtime提供显式local reconciliation生命周期；app在宿主暴露前执行它，全程禁止executor和Provider stream。
- 未采用方案及原因：不因Read只读而重试，因为该特例无法安全推广到Write/Exec；不自动继续sampling，因为用户没有授权新的网络动作；不删除call，因为会破坏append-only与Provider原生历史。
- 回归测试：双Provider分别覆盖ready、started、result和output四个截断点，恢复时替换workspace文件证明零重读，断言零Provider请求，并逐字节比较直接继续与再次重启后的下一请求。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；OpenSpec `add-read-tool-loop`。
- 后续行动：后续所有工具change复用该恢复生命周期；若某capability需要可靠重试，必须单独提出幂等协议和用户可见语义。

### [P3][2026-10-07] 用户批准不能替代 Sandbox 强制边界

- 状态：发现（设计阶段）
- 影响版本或提交：P3 Tool 架构探索，尚未进入实现 change
- 现象：若 approval 直接决定 executor 能做什么，用户批准一条命令可能意外开放 workspace 外写入、网络、敏感环境变量或平台无法安全支持的能力。
- 触发条件：Policy 返回 allow/用户点击批准后，executor 未再受独立 SandboxPlan 限制；或用户编辑命令后复用旧批准。
- 根因：把产品层的意图确认和内核层的 capability enforcement 合并成一个布尔值。
- 架构影响：Policy 只输出 allow/ask/deny 与批准事实；SandboxPlanner 独立生成实际执行边界。修改后的输入必须重新 decode、validate、policy 和 sandbox plan，批准不能覆盖失败关闭的平台能力。
- 缓存影响：临时 approval 状态和易变 sandbox world state 不进入稳定工具 schema；只有必要的确定结果进入 native output。
- 修复方案：权限协议和平台 sandbox 分 change 落地，但执行入口要求两者都满足；headless 遇到 ask 默认拒绝。
- 未采用方案及原因：不以 prompt 警告或用户确认替代 OS/handle 级约束，也不在无交互环境隐式批准。
- 回归测试：规划覆盖拒绝零副作用、批准仍禁止越界、修改输入触发重验、无交互 ask 失败关闭及不支持平台 fallback。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；推荐 OpenSpec `add-tool-approval-protocol` 及后续 sandbox change。
- 后续行动：P3 change 必须列出实际支持的平台 capability matrix，不得用一个 `sandbox=true` 空 flag 表示完成。

### [P3][2026-10-07] 大结果恢复时重新截断会改变模型所见历史

- 状态：发现（设计阶段）
- 影响版本或提交：P3 Tool 架构探索，尚未进入实现 change
- 现象：如果 Session 只保存完整 artifact descriptor，resume 时再按当前预算生成 preview，配置或算法变化会让模型看到与不中断执行不同的 tool output。
- 触发条件：工具结果超限、预算配置或截断算法升级后恢复旧会话。
- 根因：没有区分完整结果、模型预览、artifact 和 UI 摘要，也没有把“模型实际收到的字节”视为 Provider history 事实。
- 架构影响：结果归一化必须冻结模型预览字节、截断元数据、artifact descriptor 和完整性信息；Provider ResultCodec 只编码冻结预览，resume 不重新计算。
- 缓存影响：不同 preview 会直接改变下一请求 canonical bytes 和 fingerprint，因此必须与原生 output 一起持久化。
- 修复方案：每个 capability 定义确定性预算策略；完整结果可进入权限受控 artifact，模型 preview 与替换原因进入 durable fact。
- 未采用方案及原因：不使用单一全局尾部截断，也不依赖 artifact 在恢复时仍可读后重新渲染，因为两者都无法保证历史字节稳定。
- 回归测试：规划覆盖预算边界、artifact 写入失败、算法/config 变化后恢复、secret redaction 和 uninterrupted/restored request byte equivalence。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；推荐 OpenSpec `add-tool-result-budget`。
- 后续行动：首个工具先提供最小确定性 preview；统一预算 change 再扩展 capability-specific 策略，不重写既有 records。

### [P3][2026-10-10] 并行完成顺序不能成为 durable 或模型顺序

- 状态：已解决（`add-search-tools-and-ordered-parallelism`）
- 影响版本或提交：P3 Read/Glob/Grep 有序只读并行
- 现象：如果 worker 完成后直接 append result，较快的后序调用会先进入 Session 和 Provider output，破坏原始 call/output pairing；第 k 个 started 写入失败时直接返回，还会遗留已经接受但未执行的早期调用。
- 触发条件：同一 sample 包含多个只读调用、执行时长不同，或 started append 在批次中途确定失败/进入 poisoned 状态。
- 根因：把 worker completion 当成提交 owner，并在 admission 与执行之间缺少固定 call slot 和明确的失败边界。
- 架构影响：Runtime owner 先完整 durable ready batch，再按 call index 逐个 durable started；最多 8 个 worker 只写独占 slot，owner 等待全部已接受调用后按 call index durable result 和单个 native output commit。第 k 项 admission 失败时，只有更早 accepted 的调用恰好执行并被等待。
- 缓存影响：完成时序和 goroutine 调度不得改变 Provider native item 顺序、下一请求 canonical bytes、Catalog fingerprint 或冻结 preview。
- 修复方案：使用 owner 创建并关闭的 jobs channel、固定结果 slice 和有界 worker group；确定失败允许前序结果收口后写唯一 `turn_failed`，无法确认的写入使 Session poisoned 并阻止下一 sample。
- 未采用方案及原因：不让 worker 直接写 Session，不在普通 sibling error 时取消整组，也不把所有 started 合并为一个无法定位第 k 项接受状态的 batch。
- 回归测试：`internal/runtime/runtime_test.go` 用 channel gates 强制 2/0/1 完成顺序，覆盖 1/8/9 并发峰值、取消线性化点、sibling error、第 k 项确定/不确定失败及 race；`internal/app/tool_loop_e2e_test.go` 覆盖双 Provider 异构 Glob/Grep/Read 配对。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；OpenSpec `add-search-tools-and-ordered-parallelism`。
- 后续行动：写工具加入后必须在同一 owner 中实现 exclusive 屏障，不能复用只读 worker 语义推断写入安全。

重点关注：重复副作用、路径穿越、symlink、并发结果顺序、patch 增量解析和输出截断。

### [P3/P5][2026-10-11] Patch 批准必须绑定确定候选并在 Session 中先行落盘

- 状态：已解决（`add-approved-file-patch-flow`）
- 影响版本或提交：P3 单文件 Patch 与 P5 diff approval 薄切片
- 现象：若 UI 批准原始 patch 文本、批准后才读文件，或先执行再记录 allow，用户所见 diff、实际写入 bytes 与崩溃恢复事实可能分叉。
- 触发条件：同一 sample 有前序工具、批准等待期文件变化、allow/interrupt 竞态，或在 approval/started/result/output 任一 durable 边界崩溃。
- 根因：把交互提示当成执行授权，没有以 typed input、base evidence 与 final bytes 派生不可变 candidate fingerprint。
- 架构影响：Runtime 只在 ready 和前序 results durable 后准备 candidate；`allow fact -> execution_started -> executor -> result` 分批 Sync，deny fact 与 denied result 同 batch；Patch 是两个连续只读批次之间的 exclusive 屏障。
- 缓存影响：candidate/approval identity、cwd 和时间不进入 Catalog/request fingerprint；只有冻结原生 output 进入下一 Provider 请求。
- 修复方案：Unix 上用 descriptor-relative 重验、同目录 `O_EXCL` 临时文件、完整写入、文件 Sync、`renameat` 和目录 Sync；rename 后不确定结果 poison Session。
- 未采用方案及原因：不让 TUI 直接调 executor，不在 headless 隐式批准，不在 resume 重试 Patch，不以 `Lstat` 后 `Open` 的路径检查替代句柄级约束。
- 回归测试：`internal/tool/builtin/patch_*_test.go`、`internal/runtime/patch_runtime_test.go`、`internal/session/patch_draft_test.go`、`internal/session/replay_test.go`、`internal/tui/model_test.go`、`internal/tui/snapshot_test.go` 和 `internal/app/tool_loop_e2e_test.go`。
- 关联 ADR/Issue/PR：[Tool 系统架构设计](../architecture/tool-system.md)；OpenSpec `add-approved-file-patch-flow`。
- 后续行动：Write、Exec 或多文件变更不得直接复用本能力的授权范围，必须各自定义 candidate、sandbox 与崩溃语义。

### [P3][2026-10-24] Write 必须区分完整读取、原子发布和不确定提交

- 状态：已解决；真实 Linux 文件系统验收仍待可用 runner，当前记录为验证限制。
- 影响范围：`add-file-write-tool` 的 Read/Write、审批、Runtime、Session 与双 Provider 续写。
- 触发条件：同 sample Read 后立即覆盖、旧 Read 被 Patch 失效、创建时目标被抢占、发布后取消/Sync 失败，或强制终止进程。
- 根因：工具已经读到内容不代表模型已经见到完整内容；目标不存在检查不能代替原子 no-replace；rename 返回后的迟到取消不能证明文件未变。
- 修复方案：Read 证据在 outputs Sync/finalize 后生效，以路径 generation 失效当前和待提交观察；冻结完整候选；创建采用平台 no-replace，缺失目录使用私有树一次发布；发布后继续 Sync，无法确认结果则 poison 并停止后续副作用，健康 journal 仍配对输出。
- 生命周期：只清理 identity 匹配的自有暂存项；SIGKILL 的残留不在 resume 扫描。恢复只补 ready/allow/started/result 尾部，清空授权和观察。
- 验收发现：原恢复计划要求全组 started 先于所有 result，不能表达 Read→Write→Patch 屏障；已改为分组检查并拒绝越过未完成独占调用。headless 曾接收 UI 专用 approval-resolved 导致投影失败，现只向交互宿主发送此事件。审批按终端显示宽度布局，避免中文路径边框错位。
- 缓存影响：Read 观察、目录 evidence、授权和临时路径不进入 catalog fingerprint；只有真实 Write schema 增加稳定目录 segment，原生 history/opaque reasoning 与恢复 request bytes 保持等价。
- 回归测试：`internal/tool/builtin/write_crash_unix_test.go` 使用管道定位发布前后并真实杀死子进程；`write_unix_test.go` / `write_prepare_test.go` 注入 Sync/Close/unlink/身份替换故障；`internal/session/write_fixture_test.go` 读取固定 bytes；`internal/runtime/write_schedule_test.go` 验证屏障；`internal/app/write_stream_test.go`、`write_headless_test.go`、`write_resume_test.go` 验证双 Provider 纵向链路；`internal/tui/write_test.go` 覆盖展示和输入。
- 未采用方案：不拼接多段 Read 授权、不依赖单独 mtime、不回退直接写入、不恢复 grant、不把未知暂存目录递归删除。
- 后续行动：在真实 Linux 支持/不支持 `RENAME_NOREPLACE` 的文件系统执行现有发布用例；交叉编译不替代该运行验证。Exec 合并时另按其批准契约处理观察失效，不提前暴露占位接口。
### [P3][2026-10-24] 提前 Wait 会让后续进程组清理命中复用编号

- 状态：已解决（`add-command-execution`）
- 现象与根因：把直接子进程退出观察和最终回收合并，随后仍对旧 PGID 探测/发送信号，可能在编号复用后操作无关进程；快速退出还可能先于退出观察注册。
- 修复方案：Darwin 启动屏障先建立 kqueue 非回收退出观察，再放行子进程。保留未回收 leader 身份直到原组 TERM/KILL、探测与有界收口结束，最后唯一 Wait；回收后禁止任何旧 PID/PGID 操作。Wait 未完成时仍保留唯一 owner、done 和 writer 生命周期，不因 shutdown timeout 放弃资源。
- 未采用方案及原因：不通过定时扫描系统进程或重复启动 helper Wait 猜测存活，也不把已返回的 result 当作 cleanup 完成信号。
- 回归测试：`internal/tool/builtin/process_execution_test.go`、`exec_wait_owner_test.go`、`exec_darwin_test.go` 和 `internal/runtime/exec_shutdown_test.go`；用确定性屏障覆盖观察竞态、编号复用、最终 Wait 阻塞与下一 turn admission。
- 缓存与事实影响：PID/PGID 不离开 executor，不进入 Session、结果 metadata 或 fingerprint。
- 后续行动：新平台必须证明相同身份保持与唯一回收语义，不能仅因可交叉构建而启用执行。

### [P3][2026-10-24] 进程组清理不是任意后代进程 containment

- 状态：限制已明确并有回归（`add-command-execution` 批准范围）
- 现象与根因：后代通过 `setsid`/`setpgid` 脱离原组后，原 PGID 的 TERM/KILL 无法保证将其回收；若同时关闭 stdout/stderr，前台 owner 甚至不能根据管道检测其存在。系统 sandbox 限制文件/网络能力，但不是进程树容器。
- 当前处置：产品只承诺前台命令和原进程组清理，审批明确不支持 daemon 化。保留管道的脱离进程造成输出排空超时，结果为 `outcome_uncertain/output_cleanup_incomplete` 并 poison Runtime；关闭管道的脱离进程不妨碍前台结束，但仍继承 sandbox。
- 未采用方案及原因：不把 `kill(-pgid)` 宣称为整树终止，不遍历 PID 字符串进行不可靠补杀，不把任意脱离进程隐式登记成后台任务。
- 回归测试：`internal/tool/builtin/exec_detached_darwin_test.go` 覆盖两种真实 setsid 情况及其子进程的越界写入/网络拒绝。测试用独立控制通道回收直接拥有的 fixture 并执行唯一 Wait，不以测试清理能力冒充产品 containment。
- 后续行动：后台/PTY 或更强 containment 需要独立 OpenSpec，先定义长期身份、授权、取消传播与平台等价保证。

### [P3][2026-10-24] Sandbox 初始化 stderr 不能混入命令输出

- 状态：已解决（`add-command-execution`）
- 现象与根因：wrapper 初始化失败时的 stderr 可能包含底层路径和诊断；若直接连接用户 stderr，即使最终错误已脱敏，先前 progress 仍会泄漏内容，普通退出码也不能证明 sandbox 成功启用。
- 修复方案：wrapper stderr 与用户输出隔离；只有成功进入 sandbox 的 shell 才接入用户 stderr fd 并发送私有 ready 确认。初始化失败返回稳定 `sandbox_unavailable`，不发布原始诊断，不进行 unsandboxed retry。
- 回归测试：`internal/tool/builtin/exec_darwin_test.go` 确定注入无效 profile，断言 terminal/每条 progress 均不含诊断 marker、输出计数为零，同时验证正常用户 stderr 保留。
- 缓存与事实影响：底层 cause、绝对路径与宿主环境不进入 native output、Session 或 UI。
- 后续行动：后续平台 wrapper 必须区分隔离初始化与用户命令输出，不能仅在最终 result 上脱敏。

### [P3/P5][2026-10-24] 命令详情必须使用 durable 结果而不是重组进度

- 状态：已解决（`add-command-execution`）
- 现象与根因：容量 1 的进度通知会合并中间帧；把近期输出拼成“完整结果”既不可靠，也会使 UI 和模型看到不同字节。只有两行摘要则无法检查长命令与截断结果。
- 修复方案：Ctrl+O 详情复用批准中的完整命令，运行时显示有界近期输出，终态只接收 result 成功 Sync 后的 typed preview（UTF-8、至多 256 KiB）。完整预览禁止出现在 started/progress；外部 headless JSONL 不新增命令事件。当前无 artifact，省略部分不可恢复，必须保留截断标记。
- 回归测试：`internal/protocol/command_activity_test.go`、`internal/tui/command_detail_test.go`、`internal/app/exec_interactive_darwin_test.go`；覆盖事件 kind/预算、窄屏/滚动/resize、取消期间键盘路由和真实双 Provider 结果展示。
- 后续行动：完整输出 artifact 必须另行定义存储、权限、清理与恢复语义；不能让 UI 展开操作触发磁盘读取或重跑命令。

## 7. P4 缓存与上下文强化

暂无实际记录。

重点关注：非确定排序、动态状态污染稳定前缀、TTL 漂移、cache key 变化和 compaction rehydrate 膨胀。

## 8. P5 Claude 风格 TUI

### [P5][2026-09-19] 先交付 text-only Chat 薄切片，不提前扩张完整 TUI

- 状态：已采用
- 影响版本或提交：OpenAI Responses text chat 纵向切片
- 现象：等待完整 P5 才验证 RuntimeEvent 会延迟发现 terminal、取消、会话隔离和 UI 投影边界问题。
- 触发条件：把最基础的真实流式 Chat 与 Markdown、diff、permission、session picker 一次性实施。
- 根因：完整交互面过大，不适合作为首个 Provider 纵向验收入口。
- 架构影响：当前只增加 ChatSession facade、单行 draft、内存 transcript 和 typed text/failure projection；TUI 不依赖 Provider 或 transport。
- 缓存影响：无；TUI transcript 和 RuntimeEvent 不得反向构造 Provider 请求。
- 修复方案：以两天量级薄切片验证双轮、流式、取消和失败恢复，并把复杂 UI 能力留给 P5。
- 未采用方案及原因：未新增 textarea/Markdown/工具 cell 等依赖，避免掩盖当前协议边界问题。
- 回归测试：`internal/tui/model_test.go`、`internal/tui/snapshot_test.go`。
- 关联 ADR/Issue/PR：OpenSpec `openai-responses-text-chat-slice`。
- 后续行动：后续 P5 继续实现多行 composer、reasoning/tool/diff、permission 和 session picker；P2 负责 JSONL/resume 与 headless loop。

重点关注：高频重绘、终端恢复、按键歧义、overlay 输入泄漏和 snapshot 非确定性。

## 9. P6 扩展系统

暂无实际记录。

重点关注：插件信任、watcher 半文件、skill authority、MCP 故障隔离和 catalog 缓存失效。

## 10. P7 Subagent

暂无实际记录。

重点关注：共享可变 history、并发 slot 泄漏、取消传播、后台 completion pairing 和跨 provider fork。

## 11. P8 发布强化

暂无实际记录。

重点关注：跨平台路径/PTY 差异、迁移、诊断脱敏、磁盘故障和隐式全局缓存。

### [P2][2026-10-23] 会话列表不能把全局索引协调当作当前目录的完整事实

- 状态：已解决
- 影响变更：`add-session-list-command`
- 现象：接入列表时发现现有 `ReconciliationReport` 的 busy/invalid 来自全存储枚举，忙碌的已索引会话只保留旧元数据，而未索引会话可能完全缺席。
- 根因：目录筛选发生在协调后的查询阶段；索引是最后验证的投影，不是实时 owner 状态，也不能证明当前配置可恢复。
- 架构影响：列表入口独立装配 Repository/Catalog，继续复用 lease 与安全协调，保留 `--continue` 对 busy-unindexed 的失败关闭语义。
- 缓存影响：无，未修改 Provider request/native history/tool catalog。
- 修复方案：明确标注 storage-wide 警告、完整 ID 与配置匹配恢复提示；存储 owner 关闭后才输出，不新增逐行状态猜测、弱 reader 或隐式模型调用。
- 未采用方案及原因：不将 busy 全部视作列表失败，也不静默宣称结果完整；不把 limit 用作扫描中断条件。
- 回归测试：`internal/app/session_list_test.go`、`internal/app/session_list_integration_test.go`、`internal/session/catalog/list_integration_test.go`。
- 后续行动：worktree 聚合、标题与交互 picker 另立 change。

### [P5][2026-10-28] 多行正文必须贯穿宿主与 Provider，粘贴边界不能靠 UI 上限推断

- 状态：正文裁剪已解决；原始终端解码与缓冲限制明确保留。
- 影响变更：`add-multiline-chat-composer`。
- 根因：原双 Provider 在原生 user item 构造时 TrimSpace，即使 TUI 保留缩进，发送与持久化仍失真；Bubble Tea 在发出 paste 事件前独立累积字节并跳过 RuneError。
- 修复方案：校验与正文构造分离，所有宿主有效输入原样进入 Provider；只有 TUI 规范化 CRLF/CR。输入、布局、历史按职责组合，接收前保留快照，拒绝恢复且不产生幽灵 transcript。不要使用第二个 stdin reader 或计时猜测粘贴。
- 缓存影响：未裁剪正文将合法改变请求字节；固定多行 golden、原生顺序、恢复请求和 fingerprint 等价回归，Tool catalog 与稳定前缀不变。
- 验证：`internal/tui/composer_terminal_test.go` 用真实 Bubble Tea Program 和受控 reader 验证分块输入；`internal/app/composer_e2e_test.go` 验证双 Provider、三种 headless 模式和恢复。原始终端未闭合 paste 内存上限、U+FFFD 保真和增强键盘协议须单独设计，不冒充已解决。

## 2026-10-30：追加输入必须先预检再持久化

- 影响变更：`add-same-turn-steering`。
- 问题：若先保存输入再发现上下文或请求超限，该正文会留在原生历史，用户缩短重提仍无法恢复可请求状态。UI admission 也不能等同 durable commit。
- 处理：Provider 私有候选历史暴露只读 preview；Context 与请求编译使用相同快照纯内存预检，通过后提交输入关联与原生 commit。Sync/finalize 后才投影正式正文；取消遵循 writer admission，未知结果 poison 并禁止重试。
- 复用：输入与工具输出共享私有 prepared resolution 骨架；Session 保持唯一 writer/lease/codec；TUI 仅有一个控制输出消费者和不可执行本地副本。
- 验证：`internal/runtime/input_commit_test.go`、`internal/runtime/steering_test.go`、双 Provider 的 `steering_test.go`、`internal/session/input_commit_test.go`、`internal/tui/input_test.go` 和 `internal/app/resume_e2e_test.go` 覆盖取消、FIFO、原子取回、原生恢复及 input-only/steering 固定 fixtures。
- 后续边界：队列/returned 不跨进程保存，uncertain 不能重新排队；工具预算、原生 reasoning、已发布协议兼容均不得通过同轮输入偷偷降级。
