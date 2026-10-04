# EasyCode 踩坑与经验记录

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

## 5. P2 Session 与 Headless Agent Loop

### [P2][2026-09-28] JSONL 不能只复制 Claude Code 或 Codex 的表面报文

- 状态：已解决当前文本切片
- 影响版本或提交：OpenSpec `add-jsonl-session-resume`
- 现象：Claude Code 的 transcript 会混合用户/assistant/tool/progress 等消息，Codex rollout 则记录 response item、event 和上下文；二者都包含工具相关事实，但记录边界、恢复来源和宿主事件并不相同。直接照搬任一 JSONL 形状会把 Provider wire、运行时展示和副作用事实耦合。
- 触发条件：在工具尚未实现时先设计 Session schema，并要求未来无损承接 thinking、tool use 和 MCP call。
- 根因：把“JSONL 是容器”误当成“所有事件共享同一语义”。Provider native history、工具幂等 ledger 和 RuntimeEvent 实际具有不同提交时机与恢复责任。
- 架构影响：公共 envelope 只负责版本、顺序、批次、归属与完整性；`provider_native_commit` payload 由对应 Provider 私有解码。当前 required v1 包含 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`sample_usage`、`turn_completed` 和 `turn_failed`；未来 tool、permission、hook、subagent、cache 使用独立强类型 kind，optional 展示事实不得阻断恢复。
- 缓存影响：Session envelope 和动态路径不参与 Provider 请求 canonical bytes；恢复后的请求字节必须与未退出进程的下一轮一致。
- 修复方案：OpenAI 保存 user input item 与有序 output items，Anthropic 保存 user/assistant message、metadata 与 usage presence；共享层只传递 opaque envelope。未来 tool result 在副作用 durable 后作为下一次请求的 input-only native 增量提交，不重复 assistant tool call；MCP call 复用工具事实边界，但其 manifest/capability 另行版本化。
- 未采用方案及原因：未将 tool/thinking/MCP 预先塞入通用 `map[string]any`，也未以 UI transcript 反向构造 Provider 请求；这些做法无法保证类型、幂等和 opaque reasoning 无损。
- 回归测试：`internal/provider/openai/commit_test.go`、`internal/provider/anthropic/commit_test.go`、`internal/app/resume_e2e_test.go`。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0003；OpenSpec `add-jsonl-session-resume`。
- 后续行动：P3 增加 tool call/result ledger 时更新 OpenSpec schema、golden 和恢复 fixture；MCP、subagent、compaction 分别在对应阶段扩展，不修改既有 v1 payload。

### [P2][2026-09-28] 恢复正确性需要 envelope、batch、语义三层校验

- 状态：已解决当前 schema
- 影响版本或提交：OpenSpec `add-jsonl-session-resume`
- 现象：只校验每行 JSON 可解析，仍可能接受换序、跨线程混写、未知 required 记录、半个成功提交或完整 JSON 但未写完的尾批次。
- 触发条件：进程在多行 commit 中途退出、文件尾部半行、人工修改、磁盘错误或错误版本的 reader 打开新 schema。
- 根因：单行 checksum 不能表达多记录原子边界，也不能判断一组记录能否形成可恢复的 Provider 历史。
- 架构影响：Loader 第一层严格验证 schema/payload version、required/optional、canonical UUIDv7/UTC、checksum、文件归属和单调 seq；第二层只向 ReplayPlanner 暴露完整连续 batch；第三层由 Provider 事务式解码全部 native commits，任一错误都不返回部分历史。
- 缓存影响：阻止损坏历史生成看似合法但字节不同的续写请求。
- 修复方案：只允许修复 EOF 尾部半行和最后一个未完成 batch，修复后截断并 `Sync`；中段损坏、完整但校验失败的末行、未知 required 版本全部硬失败。若完整日志以未闭合 turn 结束，恢复先追加 `turn_failed(code=session_interrupted)` 补偿记录，再开始新 turn。
- 未采用方案及原因：不跳过坏行继续扫描，不自动猜测未知 required payload，也不把尾批次中的部分 native commit 交给 Provider。
- 回归测试：`internal/session/loader_test.go`、`internal/session/replay_test.go`、`internal/session/migration_v1_test.go`、`internal/app/resume_e2e_test.go`。
- 关联 ADR/Issue/PR：ADR-0003；OpenSpec `add-jsonl-session-resume`。
- 后续行动：首次引入新 schema/payload revision 时必须增加从不可变历史 fixture 到当前 replay model 的版本专属 regression；SQLite 重建只能消费相同 ReplayPlanner 输出。

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
- 回归测试：`internal/session/lease_test.go`、`internal/session/writer_test.go`、`internal/session/migration_v1_test.go`、`internal/app/session_process_test.go`、`internal/app/session_service_test.go`。
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
- 后续行动：tool result 和其他副作用沿用同一 durable-before-memory 原则；compaction/fork 未来只能 append checkpoint/cursor 与 child metadata，不得重写原日志。该能力当前尚未实现。

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
- 后续行动：新增外部事件必须通过 OpenSpec 演进 JSONL fixture；stdin JSON、双向控制、独立 usage 更新和 reasoning/tool 事件仍未实现。`turn.completed` v1 已通过 `add-durable-sample-usage` 增加 required usage；`--continue` 复用同一 resume/headless 投影路径。

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

- 状态：已解决 v1 六种记录
- 影响版本或提交：OpenSpec `harden-architecture-contract-compliance`
- 现象：导出的 `RecordDraft.Payload any` 与 `DecodePayload() any` 允许调用方拼出 kind/payload mismatch，错误只能在写入或 replay 时通过 type assertion 暴露。
- 触发条件：Runtime/app 直接构造 draft，或 registry 用 reflect 分配 payload 后遗漏 revision-specific 语义校验。
- 根因：异构记录为了复用一个动态入口而牺牲了构造合法性与协议边界。
- 架构影响：`RecordDraft` 成为 sealed 值类型；六种 v1 kind 各有 typed constructor、strict decoder 和 validator，registry 只返回常量元数据。
- 缓存影响：保持 JSONL v1 canonical bytes、checksum 与恢复后 Provider request bytes 不变。
- 修复方案：constructor 立即验证并编码独立 `json.RawMessage`，ReplayPlanner 通过显式 switch 调用目标 decoder。
- 未采用方案及原因：未把动态类型转移到 callback interface 或泛型 registry，因为仍会隐藏 kind/revision 契约；也未修改已发布 v1 wire。
- 回归测试：`internal/session/draft_test.go`、`codec_test.go`、`replay_test.go`、`migration_v1_test.go`。
- 关联 ADR/Issue/PR：ADR-0003；OpenSpec `harden-architecture-contract-compliance`，不新增长期决策。
- 后续行动：未来 revision 必须同时增加 typed constructor、strict decoder、validator 和不可变 migration fixture。

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

重点关注：JSONL 尾部损坏、事件顺序、取消时 flush、SQLite 重建和 native history 恢复。

## 6. P3 Coding Tools 与安全执行

暂无实际记录。

重点关注：重复副作用、路径穿越、symlink、并发结果顺序、patch 增量解析和输出截断。

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
