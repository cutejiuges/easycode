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

- 状态：已解决设计口径
- 影响版本或提交：P0 架构基线
- 现象：如果使用 `cached_input_tokens / input_tokens` 作为统一公式，Anthropic 可能出现大于 1 的比例，而 OpenAI 的比例通常不大于 1。
- 触发条件：Anthropic 的 `input_tokens` 是未命中缓存的输入，cache read/write 是独立字段；OpenAI 的 `prompt_tokens` 已包含 `cached_tokens` 子集。
- 根因：两家 Provider usage 字段的分母语义不对称。
- 架构影响：UsageParser 必须先归一化为 `input_uncached`、`cache_read`、`cache_write` 三元组，指标层不得读取 Provider 原始字段计算 ratio。
- 缓存影响：错误口径会产生不可比、甚至大于 1 的命中率，误导缓存优化和成本诊断。
- 修复方案：归一化总输入为 `input_uncached + cache_read + cache_write`；Anthropic 按三者之和作为分母，OpenAI 从 prompt_tokens 与 cached_tokens 子集推导未缓存输入。字段区分 known、unknown 和 not-applicable，只有明确不适用才使用已知 0，缺失或零分母输出 unknown。
- 未采用方案及原因：不将两家原始字段强行命名为同一含义，也不把缺失值当 0。
- 回归测试：P1 UsageParser golden、Anthropic/OpenAI usage fixture 和 cache metrics regression。
- 关联 ADR/Issue/PR：ADR-0002、ADR-0004。
- 后续行动：P0 先固定领域结构，P1 Provider wire 接入时补全字段映射。

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

## 5. P2 Session 与 Headless Agent Loop

暂无实际记录。

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
