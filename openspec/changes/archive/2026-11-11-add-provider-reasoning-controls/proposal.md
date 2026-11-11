## Why

EasyCode 已具备双 Provider 原生推理保存、被动阅读、九项工具、恢复及缓存，但普通请求尚不能显式配置推理强度、thinking 模式或 summary。补齐启动时冻结的主动推理配置，可以复用这些已验收基础，让复杂 coding 任务具有可解释的推理与输出预算；相较 MCP、fork 或 Subagent，这个纵向切片的依赖更集中。

## What Changes

- 增加严格 JSON 配置：Anthropic thinking 判别联合、adaptive/enabled 分支可选的 display（summarized/omitted）及独立 effort、OpenAI effort/summary，以及普通请求输出上限；省略时保持现有请求 bytes，不猜测模型或自定义 endpoint 的支持范围。
- 在应用装配前完成 family、参数组合和输出/上下文预留校验；启动后设置不可变，非法配置零 Provider 网络、零 Session 创建。
- 两家分别编译原生请求；覆盖 Anthropic disabled/5m/1h 缓存结构，以及 OpenAI HTTP、WebSocket full/incremental 和安全回退，保留全部 native opaque 字段及工具配对。
- 将实际请求设置纳入确定性 profile 与请求证据；相同设置下恢复与不中断续写保持 canonical bytes、原生顺序和 fingerprint 等价。
- 普通采样和摘要显式区分用途，但沿用同一冻结 thinking/display/effort/summary 策略；摘要独立使用 compaction_output_tokens。manual budget 不小于摘要上限时在 started/网络之前明确拒绝，不夹取、不降级、不自动提高额度；工具结果 durable 不等同于 Provider assistant turn 结束。
- 两家在 Provider 内用同一不可变请求计划导出预检、实际 body、请求属性、输入 fingerprint 和 receipt；摘要证据不得从普通请求重新生成，WebSocket 派生不得丢失逻辑参数。
- **BREAKING**：Anthropic 新增解析 output_tokens_details.thinking_tokens，reasoning_output 从固定 not_applicable 改为报告时 known、缺失时 unknown；明确零、总量关系、普通 turn 聚合和独立摘要计量均保持可信。
- **BREAKING**：Anthropic sample raw usage 当前结构增加必需 thinking_tokens 状态字段，checkpoint generation 和失败 provenance 使用相同当前结构；实施时替换相关固定 current fixtures，旧开发形状失败关闭，不增加版本、迁移或历史 reader。
- `/context` 展示启动选择、实际发送设置及 display 来源；返回正文/opaque 和 usage 是独立观测，不能把请求值当成服务端生效证明。沿用活动提示和 Ctrl+O，不以阅读操作改变请求。
- 补充 display、摘要策略与预算拒绝、请求证据一致性、计量三态/固定 bytes、工具循环、缓存/恢复、headless 隐私与 Darwin PTY 验证，最终执行 `make verify`。
- 全面回归已补齐双 Provider 完整事件帧校验：重复成员不能覆盖计量或响应身份，异常终态不能执行已返回的工具调用；新增真实 CLI、异常流及计量模糊测试，保留合法未知扩展和连续观测语义。

## Capabilities

### New Capabilities

- `provider/reasoning-control`：启动冻结的双 Provider 推理/display 策略、同源请求证据、原生计量当前结构、失败、缓存及恢复一致性。

### Modified Capabilities

- `config/json-file`：增加严格推理、display 与普通输出配置，明确跨 family 和上下文预留约束。
- `provider/anthropic-messages-text`：允许显式 thinking/display/effort，补齐 thinking token 流式归并与 raw/normalized usage，同时保留默认请求 bytes 和原生内容。
- `provider/openai-responses-text`：允许显式 effort/summary/输出上限，按实际已实现且已配置的 summary 策略声明能力。
- `provider/reasoning-display`：主动请求与被动阅读继续隔离，能力位不由显示操作改变。
- `provider/usage-accounting`：Anthropic thinking_tokens 映射为 reasoning_output，补齐 known/unknown、总量校验、聚合与摘要/恢复计量。
- `context/planning`：profile 包含非默认请求策略的确定性身份，维持来源顺序及既有预算公式。
- `context/compaction`：摘要保留冻结推理策略，独立上限与 manual budget 预检，采用真实摘要计划的请求证据且不污染普通 baseline。
- `tui/context-commands`：`/context` 增加冻结设置摘要，保持只读、窄屏和输入焦点行为。

## Impact

涉及 `internal/domain` 不可变值、`internal/config`、两家 request/cache/WebSocket/compaction/evidence、Anthropic usage/reducer/native codec、`internal/context` profile、Runtime 压缩预算错误映射、app 装配及 TUI。复用固定 codec、transport、Session、查询和现有 owner；不新增依赖、goroutine、Runtime command/event kind 或模型工具。

主规格中禁止主动推理、profile 限定字段和 Anthropic reasoning_output 为 not_applicable 的条款通过完整 MODIFIED 块更新。Session/checkpoint 外层 envelope、SQLite、OpenAI native 结构和 headless 外部字段/版本保持；Anthropic native usage 及嵌套 provenance 当前 shape 会改变，headless 已有 reasoning_output 指标的值/状态随契约更新。设置仍不持久化，不引入迁移或兼容 reader。实施同步 README、架构、能力清单、Roadmap 和必要踩坑；实际验收范围与结果见 verification.md。

## Non-goals

不实现运行中 `/effort` 或 thinking 切换、设置文件回写、远端模型发现、自动探测/降级/重试、raw reasoning、display updates/between_tools 等额外模式、sequential_cutoff、configuration_update native item、reasoning mode/context 高级参数、额外 beta header、跨 Provider fork、MCP、Plugins 或 Subagent。首个切片不宣称完整 P1/P5/P6 退出，也不保证显式 display/summary 必然返回正文、推理计量必然报告或任务质量提升。
