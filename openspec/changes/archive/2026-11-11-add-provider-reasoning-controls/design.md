## Context

动机和范围见 [proposal.md](proposal.md)。本次调研以当前 checkout 为准，参考目录仅只读，不参与构建；以下“新增”描述本 change 的设计目标；实际实现与验收状态以 tasks.md 和 verification.md 为准。

### 当前实现与需要修改的契约

| 位置 | 已观察行为 | 本次落点 |
| --- | --- | --- |
| `internal/config/config.go` | 严格 JSON 成员解析，Provider 环境覆盖后校验；输出预留只是 planner 数值 | 新增五个顶层字段，资源打开前完成最终参数组合验证 |
| `internal/provider/anthropic/request.go`、`provider.go` | 普通 max_tokens 默认 4096，无 thinking/output_config | 编译冻结原生设置；用单一设置值持有输出上限 |
| `internal/provider/anthropic/cache.go` | cachedMessagesRequest 手工复制普通请求字段 | 显式传递新增字段，三个 cache mode 的语义一致 |
| `internal/provider/openai/request.go` | 无 reasoning；普通 max_output_tokens 省略；include 保留 encrypted content | 编译 effort/summary/输出上限，不改变 native input |
| `internal/provider/openai/websocket.go` | websocketCreate 另行列举请求字段 | full/incremental 和安全 HTTP 回退保留相同逻辑设置 |
| 两家 `request_evidence.go` | 通过普通编译路径读取输入/properties，摘要也复用此路径 | 同一不可变用途计划生成 body/evidence/receipt，禁止摘要重编译普通证据 |
| 两家 `compaction.go` | 从普通采样编译路径派生请求，再覆盖输出上限 | 摘要保留冻结推理/display，独立上限及 manual budget 预检 |
| Anthropic `reducer.go/native.go/commit.go` | raw usage 仅四项，reasoning_output 固定 not_applicable | 解析 thinking_tokens，归一为 known/unknown，补齐唯一当前持久化字段 |
| `internal/context/planning.go` | profile 仅 family/model/既有 schema_revision | 增加可省略的非默认请求身份，来源仍为六类 |
| `internal/app/app.go` | Provider、planner、TUI 集中装配 | 同一 resolved 值传给真实消费者，避免各层重新解析 |
| `internal/tui/context.go` | committed snapshot 展示；固定高度可能裁剪 | 从启动装配读取设置，扩展有界布局并测试关键值可见性 |
| `internal/headless/context_event.go` | 外部 v1 直接嵌入 ContextSnapshotPayload | 不向该 payload 填配置展示字段，避免无意扩张外部协议 |

已有固定恢复 bytes 位于两家 Provider `testdata/reasoning_restore.current.jsonl`。现成测试包括 `reasoning_restore_test.go`、`cache_test.go`、OpenAI `websocket_test.go` 和 `internal/app/cache_e2e_test.go`；新用例扩展这些真实断言，不以 encoder 临时生成的 journal 代替恢复 fixture。实施时直接替换受 Anthropic raw usage 当前形状影响的固定 bytes（含内嵌 provenance/normalized usage/checksum），并保留相同业务/损坏恢复断言；没有历史 fixture 或兼容 reader。

主规格有四处需要显式调整：Anthropic 禁止 thinking 配置、OpenAI 主动 summary 为 false、profile 限定字段，以及 Anthropic reasoning_output 固定 not_applicable。本 change 用完整 MODIFIED 块保留既有场景，并补 provider/usage-accounting delta。Session/checkpoint 外层 envelope、SQLite、OpenAI native 与外部 headless 字段/版本不变；Anthropic sample/provenance 内 raw usage 当前结构和既有 reasoning_output 的值/状态会改变，不能继续承诺所有 native fixture 和计量语义不变。

### 两份参考工程与公开 API

本地快照：Claude Code 2.1.88 sourcemap，Git `a8a678cb6244e6770e1e421767ff0987a1d95549`；Codex `6f8bf683cee83ed4d580a2e106b587d8451db936`。引用相对于 `/Users/cutejiuge/IdeaProjects`，不是当前 worktree 的兄弟目录；本 worktree 下相对 `../codex` 与 `../claude-code-sourcemap` 不存在。

| 参考位置 | 观察 | 采纳与取舍 |
| --- | --- | --- |
| Claude `restored-src/src/commands/effort/{index.ts,effort.tsx}` | `/effort` 查询/设置、auto 与显式值区分、部分值只在会话内保存 | 采纳显式值与默认值可辨认的反馈；首切片在 `/context` 只读展示，运行时切换/全局回写不在范围 |
| Claude `src/utils/{thinking.ts,effort.ts}` | thinking 为 disabled/adaptive/enabled+budget 联合，effort 单独解析；模型/第三方 override 决定支持 | 采纳原生联合和两个维度独立；自定义 endpoint 由用户显式配置，不复制模型字符串启发式、账号和实验开关 |
| Claude `src/services/api/claude.ts` | adaptive 不发 budget；manual budget 与 max output 关联，另有 output_config.effort | 采纳原生字段；本项目非法预算明确拒绝，不静默 clamp |
| Codex `codex-rs/core/src/client.rs` | effort 与 summary 分别构造，模型 metadata gate summary，reasoning 变化会使复用失效 | 采纳参数独立、属性比较及完整 transport 覆盖；本项目没有远端模型目录，不伪造支持声明 |
| Codex `codex-rs/core/src/session/reasoning_effort.rs` | 动态 effort 更新具有 surviving history、baseline pin 和压缩生命周期 | 这解释了为何本切片冻结设置；不提前增加 configuration_update 或丢弃其恢复语义 |
| Codex `codex-rs/protocol/src/openai_models/reasoning_effort.rs` | UI 别名和内部配置可能经模型规则归一化 | 本切片仅接受公开 wire 枚举，不透传 ultra/persistent/任意 custom 值 |
| Claude `src/services/compact/compact.ts:1181/1305` | 缓存共享主路径保留 thinking 且不调低上限；独立备用路径才 disabled | 采纳策略稳定原则，不复制双路径 fallback；本项目保留冻结策略并在摘要预检处理额度冲突 |
| Codex `codex-rs/core/src/compact.rs:835`、`session/reasoning_effort.rs:94` | 压缩沿用 effort/summary，查找摘要 effort 不提前改变 live pin | 摘要保持冻结推理，准备/失败不能更新普通 baseline；独立上限不等于清空策略 |

2026-11-08 核对的公开来源：[OpenAI reasoning](https://developers.openai.com/api/docs/guides/reasoning)、[Anthropic extended thinking](https://platform.claude.com/docs/en/build-with-claude/extended-thinking)、[Anthropic steering](https://platform.claude.com/docs/en/build-with-claude/thinking-steering-and-cost)、[Anthropic effort](https://platform.claude.com/docs/en/build-with-claude/effort)、[thinking/display/工具回合规则](https://platform.claude.com/docs/en/build-with-claude/thinking)。其中 display=summarized/omitted 与 output_tokens_details.thinking_tokens 来自当前公开 wire；旧参考快照本身不足以证明这些新字段。公开参数已经与旧参考快照有差异，例如 effort 枚举与 manual thinking 的模型适用范围；实现边界以本提案明确字段为准，参考快照不能证明任意 endpoint 支持。

## Goals / Non-Goals

目标是单一、不可变、可验证的推理请求策略，贯穿普通/摘要编译、预算、同源证据、缓存、计量、恢复和只读显示；产品展示继续沿用 Claude 风格的紧凑主线和 Ctrl+O，内核保留两家的原生优势。没有新增网络探测、调度器、资源 owner、全局 registry、协议版本或无消费者策略接口。

本切片覆盖 request-level thinking/display/effort/summary 及已返回的 thinking token 计量。运行中设置更新、display updates/between_tools、额外 beta header、原生 configuration_update、推理 mode/context、sequential_cutoff 及其他未列字段不暴露 schema 或执行入口。没有外部真实模型质量提升实验，不能把本地 fixture 通过写成某模型质量或 endpoint 可用性保证。

## Decisions

### 1. 唯一冻结值和依赖方向

在 `internal/domain` 增加私有字段的不可变 `InferenceSettings` 及封闭 family 分支，提供 typed 默认/Anthropic/OpenAI 构造器、validator、独立副本 getter 和规范身份读取；外部 thinking JSON 联合由配置边界真实使用的 strict decoder 处理，不为没有序列化消费者的内部值预建通用 decoder。Anthropic thinking 自身是 omit/disabled/adaptive/enabled 的封闭联合，只有 enabled 持有 budget，adaptive/enabled 可持有 display，disabled 不允许 display；omit 是语义缺省状态，不是内部格式兼容分支。没有 any、map[string]any、可变包级值或可替换 codec。

`config` 解析外部值，执行既有环境覆盖后，以最终 family 解析出唯一值。`app` 在打开 Session/Catalog/Provider 之前完成校验，再把同一值传给 Provider、context profile 和 TUI。两家 Provider Config 使用当前单一设置契约，替换 Anthropic 现有独立 MaxOutputTokens 真源并同时更新全部调用点及测试；不保留双配置优先级或旧 API 兼容 facade。

所有构造器、解码、校验、getter、指纹和 View 都只做内存操作；配置读取仍由已有 Load 与 descriptor-bound opener 承担。Provider 网络和清理仍由既有 Stream/Close owner 承担。不为设置新增 goroutine、锁、通道或生命周期接口。Runtime 通过 profile 持有设置身份，不理解 Provider wire；TUI 不依赖 config/provider 包。保留用户显式选择/来源与实际发送值的区分，不能从 resolved 值反推是否显式配置；真实正文及 usage 使用已有返回事件/计量事实，不增加空的观测 facade。

备选是给 TurnInput 增加可变设置，或让 UI 直接改 Provider。它们会增加 turn 内安全点、保存/取消和恢复义务，且与冻结范围不符，因此不采用。也不为两家只用一次的字段赋值增加新的通用策略框架。

### 2. 精确配置与缺省行为

新增字段如下，全部仅通过既有 JSON 文件配置；枚举大小写精确匹配，禁止别名/任意透传。

| 字段 | 合法值 | 省略行为 |
| --- | --- | --- |
| anthropic_thinking | disabled 只含 type；adaptive 可含 display；enabled 要求 budget_tokens、可含 display | 不发 thinking |
| anthropic_effort | low/medium/high/xhigh/max | 不发 output_config.effort |
| openai_reasoning_effort | none/minimal/low/medium/high/xhigh/max | 不发 reasoning.effort |
| openai_reasoning_summary | auto/concise/detailed | 不发 reasoning.summary |
| max_output_tokens | 1..65536 整数，本地切片边界 | Anthropic 沿用 4096；OpenAI 不发上限 |

这里仍是五个顶层字段。thinking 对象的 budget_tokens 仅 enabled 允许，display 仅 adaptive/enabled 允许，值为 summarized/omitted。省略 display 不发送字段，不推定服务端默认显示正文；disabled 带 display、updates、null、重复字段或大小写别名均在读取配置时拒绝。Provider-specific 字段只要显式出现，就必须属于最终 family；不允许在另一家配置里写 disabled/none 作为无效占位。错误保持 invalid_configuration 和稳定英文，不回显未验证值、配置正文、密钥或完整 URL。

manual budget 范围为 `1024 <= budget < effective_max_output`，与 enabled 分支绑定；adaptive/disabled 带 budget 或 enabled 缺 budget 均拒绝。Anthropic 显式 disabled+xhigh/max 拒绝；其余 effort 可独立配置，服务端模型适用性由真实请求结果判定。manual 仅面向实际支持该公开形状的模型，并不增加旧内部历史 reader；新模型拒绝它时不自动改成 adaptive。

示例仅展示新增配置片段，实际仍需既有 Provider 字段，不在 artifact 中放真实 key：

```json
{
  "anthropic_thinking": {"type": "adaptive", "display": "summarized"},
  "anthropic_effort": "high",
  "max_output_tokens": 16384
}
```

```json
{
  "openai_reasoning_effort": "high",
  "openai_reasoning_summary": "auto",
  "max_output_tokens": 32768
}
```

示例不含窗口。若配置窗口，还必须按下一节提供足够的 reserved_output_tokens。默认全部省略时旧配置、Provider 普通请求 golden、profile bytes 与 headless 字段保持；Anthropic reasoning_output 的值/状态按新计量契约更新；不能用“设置了默认”偷偷启用推理或自动提高输出上限。

### 3. 普通上限与上下文预留保持独立

普通 wire 上限不从 reserved_output_tokens 派生；预留也不被自动扩大。沿用 `input_limit = window - reserved_output - safety` 及现有 overflow/unknown 规则：

- 显式 max_output_tokens 且有窗口时，要求 reserve >= max_output_tokens。
- 有窗口且存在任一显式 Anthropic 推理字段时，reserve 必须覆盖普通有效上限（显式值或现有 4096）。
- 有窗口且存在任一显式 OpenAI 推理字段时，必须显式 max_output_tokens，且 reserve 覆盖它。不能声称已为未知服务端输出上限充分预留。
- 全部新增字段省略时不追加默认上限一致性限制，既有小窗口仍按原规则启动。
- 无窗口时不推断容量，也不增加没有窗口的 reserve 字段；只校验参数自身与 manual budget。输出上限是每 sample 的硬上限，不承诺整个工具 turn 的费用上限。

自动压缩继续使用现有 `max(reserved_output, compaction_output)` 阈值公式，不修改其状态机。新增普通配置通过启动校验保证所需输出已被预留。未知 token 估算的普通非阻断语义保持，摘要独立 unknown 拒绝语义保持。普通 manual 设置只按普通上限校验，不能因为默认摘要 4096 较小而让合法普通启动失败；摘要冲突在操作预检返回既有预算拒绝。

与自动扩大 reserve 相比，显式拒绝避免 silently 改写现有预算和触发阈值；与将 reserve 直接作为 wire 上限相比，保留两个概念使现有配置含义明确且默认请求不变。

### 4. 两家原生编译与全部派生路径

Anthropic request 增加 typed thinking 和 output_config，不借 user message 或 prompt 文本模拟推理。thinking omit 时完整省略，disabled 仅含 type，adaptive/enabled 精确保留可选 display，enabled 必有 budget_tokens；只有 effort 存在才创建 output_config。普通 max_tokens 取冻结值。`cachedMessagesRequest` 复制相同 typed 字段，沿原断点算法处理工具、项目/Skill 和历史；默认普通请求、5m/1h 的既有 bytes 全部不变。

OpenAI request 增加 typed reasoning，成员独立省略，没有成员则不编码空对象。include 继续请求 encrypted_content，store=false、function/custom tool 及 input 顺序保持。`websocketCreate` 必须携带同一 reasoning 和输出上限；仅 stream/type/previous_response_id/增量 input 沿既有 transport 差异。生成前安全回退到 HTTP 保留完全相同逻辑策略，参数 400 不触发重试或回退。

以现有请求用途枚举区分 ordinary/summary，把“用途选择”放到原生请求计划生成阶段；两家的普通和摘要编译都从同一原生窗口构造，且都使用同一冻结推理/display 设置；summary 只替换工具选择、独立输出上限并追加摘要指导，不改推理策略。无需新建可执行空策略，无法识别的用途直接失败。请求预检、实际编译和 evidence 必须共用同一设置解析，不能分别组装相互矛盾的字段。

工具调度与原生内容归并复用已有机制；Anthropic usage parser/native usage codec 按下述计量契约扩展，不能声称它们完全不变。开启思考后返回 thinking/summary 的 fixture 必须经过完整采样与工具结果再续写；不能仅检查 request JSON 即声明功能完成。尤其保留 Anthropic 原始 thinking/signature/redacted 顺序和 OpenAI summary/encrypted/phase，以及当前完整工具 call/output 配对。

### 5. 缓存身份与默认字节保真

provider_profile 仍是第一个稳定来源，不新增第七种来源。纯内存规范化得到实际非默认普通请求属性身份，再在 profile canonical 中按需加入 `request_policy`；无变化时完整省略，不添加空对象。既有 schema_revision 不递增、不成为版本路由。默认 profile source revision 维持原值；存在实际非默认属性时，其 source revision 从当前 canonical 内容派生，满足既有长度/字符约束。

该身份包含实际发送的 thinking/display、effort、summary 与输出偏差，不含 explicit/default 的 UI 来源标签。Anthropic 显式 4096 与既有省略值产生同一 max_tokens，因此没有其他设置时 profile 仍等价；显式 disabled 与省略 thinking 的 wire 不同，身份必须不同。OpenAI 显式上限与未发送不能合并。不能根据模型名称推断“显式 high 等于默认 high”。

两家 request_evidence 的 properties 保留新增字段，继续通过 RequestBaseline 比较输入前缀和请求属性；OpenAI 属性不同则 full，不能复用旧 previous_response_id。`/cache` 复用已有 source_content_changed/source_revision_changed 与 provider_profile，不添加新事件或 unbounded 观测。正常恢复首个请求按既有 restored_baseline 解释；策略变化来源的直接验证使用两个独立实例生成的 source 比较，不承诺恢复时凭空知道旧进程未持久化的设置。

只改实际发送的普通策略（包括 display）时，profile/完整请求/稳定前缀指纹发生预期变化，tools、project、skills 和原生内容各自指纹不变。配置键顺序、配置路径、UI 阅读和实时 identity 不参与。这里的 profile 是本地保守失效解释；不保证服务端 cache hit，也不保证设置变化时两家具有完全相同缓存行为。

### 6. 摘要沿用策略、独立额度与三种边界

摘要使用与普通请求完全相同的冻结 thinking/display/effort/summary，包括显式/省略语义。只变更用途、tool choice none、输出上限和独有指导后缀；模型、项目/Skill、工具目录、原生窗口与缓存编译规则保持。独立 compaction_output_tokens 不从普通上限或 reserve 推导；不为提高成功率移除 summary、降低 effort、关闭 thinking 或走第二摘要分支。

需要区分 sample 完成、工具结果 durable 和 Provider assistant turn 完成。Runtime 当前在完整工具结果后调用 prepareAutomaticSample；这个本地安全点不证明远端回答结束。普通工具续写、同轮追加及工具后摘要都维持冻结策略，完整保留 thinking/signature/redacted 及调用/结果顺序。摘要是单独用途，不是允许改变推理模式或重跑工具的边界。

manual 的 `budget_tokens < compaction_output_tokens` 必须在 preflight 校验，与是否配置窗口无关。8192/4096 或 8192/8192 均拒绝；8192/8193 通过该单项检查，但不保证剩余一 token 足够摘要。普通上限 16384 时这些配置仍可合法启动。摘要拒绝返回 compaction_budget_exceeded，零 started/failed records、零摘要网络、零工具副作用；错误只给安全分类和预算数值，用户可显式修改下次启动配置，不自动提高额度或 clamp。

实现落点是 Provider 预检增加有真实消费者的 CompactionBudgetExceeded typed error code，Runtime preflight 映射到已有 ControlErrorCompactionBudgetExceeded，而非通用 invalid_command 或 stream_protocol_error。自动摘要由该既有预算失败路径更新进程内失败/来源抑制计数；旧窗口可通过普通预检时继续，不可通过时沿既有任务失败和未提交输入返还规则，不能发出超预算请求。同来源抑制和取消、uncertain 清理 owner 不另建机制。

摘要失败/取消不改变冻结策略、窗口或普通成功 baseline；完成 Sync 后安装新窗口，后续普通请求仍使用普通上限。生成推理保存在当前 provenance、usage 归属 CompactionID，但不发布普通推理展示。refusal、incomplete/截断、工具调用、空白、超限仍按既有 typed 终态拒绝。保留策略能维护连续性，不能保证服务端命中缓存或摘要一定成功；高 effort 仍可能耗尽独立额度。

### 6.1. 同一计划产生 body、预检与证据

在两家 Provider 包内分别使用有界、不可变的原生请求计划，组合现有具体 wire 类型、用途、冻结设置、来源 revision/digest 和不可变原生输入。没有跨 Provider 通用 wire builder，也不增加公共空策略。计划由纯内存步骤构造；headers/key secret 留在 transport 边界，不能进入计划身份、诊断或 TUI。

同一计划必须得到以下一致结果：

| 结果 | 精确定义 |
| --- | --- |
| 预检输入与估算 | 含实际项目/Skill、原生窗口、独有摘要指导和真实用途参数的完整逻辑请求；使用该用途输出预留 |
| canonical body | 最终逻辑请求的编码，Anthropic 添加 marker/TTL，OpenAI 添加既有 key；无无意参数丢失 |
| properties | 从同一逻辑计划排除 input/messages 后的非输入属性，包含 thinking/display/effort/summary、实际上限及 tool choice，不由普通请求补填摘要属性 |
| NativeFingerprint | 同一计划未加 cache marker 的有序逻辑 input/messages 的 hash，包括临时项目/Skill 和摘要指导；不等同于 journal payload hash |
| RequestFingerprint | 完整逻辑 canonical body 的 hash；OpenAI WS full/incremental 去 stream/加 type/截后缀是传输派生，receipt 继续引用完整逻辑 hash |
| CompactionSource.Digest | 已提交有效原生窗口来源 digest，不能混同包含临时指导的输入 fingerprint |

每次 preflight 或执行构造自己的冻结计划；执行先核对窗口来源与预检相同，再从执行计划同时取 body/evidence/receipt，不在开始前或收到 terminal 后再次从普通 sampling 路径生成 evidence。两次计划在相同来源/用途/设置下必须逐字节等价；来源不同直接拒绝，不能拼接旧 body 与新证据。纯预检的计划不用持有网络、writer 或长期资源。

Anthropic 缓存 marker 只覆盖最终 wire，不进入逻辑原生输入 fingerprint 或持久化 content；OpenAI 增量准入继续比较完整逻辑 input 前缀及 properties。summary 始终沿既有 full/连接重置规则，不安装普通 baseline；收到成功响应但 checkpoint 尚未 durable 也不能提前改变普通基线。失败后下一普通请求依据旧普通成功 baseline；成功 checkpoint 后按既有 compacted boundary/full 规则重建。

### 6.2. Anthropic 推理计量与唯一当前形状

当前 reducer.usageEventWire 只读取四项 token 字段，rawUsage.normalized 固定返回 NotApplicable；这与当前公开 output_tokens_details.thinking_tokens 不一致。本次明确扩展，而不从文本长度或请求参数估算 token。

message_delta 显式提供的 thinking_tokens 更新独立观测值；零为 known:0，缺失不擦除已观测值，从未观测为 unknown。保留 input/cache 在 start/delta 的占位零规则，thinking 不复用这些规则。负数、非整数、溢出或损坏的已知 detail 使 sample 协议失败；最终 thinking 与总 output 都已知时要求 thinking<=output。只知道 thinking 时保留其 known 与总 output 的 unknown。输出是已包含 thinking 的总量，不将两项相加，也不通过相减伪造“可见 output”指标。原有未知 Provider 扩展的受控处理不扩张为任意透传。

具体 rawUsage 增加私有 ThinkingTokens 观测，Anthropic rawUsageCommit 增加必需 `thinking_tokens`，沿用已有 `{known:false}` / `{known:true,value:N}` 状态结构；零的 value 必须存在，unknown 不带 value。所有当前 sample metadata 以及 checkpoint generation、失败计量 provenance 都使用相同字段；缺失、null、跨状态 value 或损坏 current shape 失败关闭。同步更新 encode/decode、clone、semantic validation 与 normalized 交叉校验，不增加格式标记、双 reader 或可选旧记录。

wire 缺字段得到当前结构里的 unknown，不能省略必需字段来兼容旧四项持久化形状。raw/native 与 normalized sample_usage 进入既有 prepared/Sync/finalize 边界；恢复逐项核对，原始 30 与 normalized 0/unknown/NA 冲突必须拒绝。摘要成功、完整响应但候选拒绝分别保存自身真实计量；没有可信 terminal 时仍为 unavailable，不制造账单。

归一化 reasoning_output 在 Anthropic 由固定 not_applicable 改成 known/unknown，是明确行为变更。普通 turn 聚合沿既有 unknown 传播，摘要另归属 CompactionID；没有第六项公共 metric，既有 headless/cache 五指标字段和外部 v1 保持，但数值状态必须更新。display omitted、thinking 空、配置 disabled 都不能替代服务端 usage；signature/推理正文不能随计量泄漏。

raw/normalized usage 不进入模型请求、profile、工具目录或 prompt fingerprint，也不直接驱动压缩。读计量不得影响正文、opaque item 顺序或后续请求。只有当前 native payload 中的 raw usage 形状变化，配置设置不持久化。

### 7. 恢复、失败与能力语义

配置设置不写入 Session/native/checkpoint，也不修改 SQLite；与设置分开的真实 Anthropic usage 按新当前形状保存。resume/continue 在恢复启动路径使用本次 Load 的合法设置，保持现有 family/wire/model 匹配；换设置不影响历史 strict 校验、lease 或恢复补偿。原生 reasoning、工具结果仍从固定 journal 验证后安装，恢复本身零网络、零工具执行，不改写任何 append-only bytes。

同配置等价实验必须固定 ThreadID、模型、目录/项目快照、原生历史和下一输入，比较 logical full request、native 顺序、request/native/profile fingerprint。OpenAI 新连接恢复 full；增量比较仅在两条各自合法的活动连接中进行，并独立核验其 full 派生等价，不能直接将 full body 与 incremental body 作 bytes 相等断言。改设置恢复是合法的新请求策略，不宣称与不同设置请求相等。

配置无效在资源打开前拒绝；服务端参数拒绝只返回既有安全 typed failure，不删除字段、改强度、换模型或重发。取消/断流/输出耗尽沿既有 reducer 终态、prepared admission、durable Sync 和唯一 owner 退出。OpenAI incomplete 不能把部分思考变成 final；Anthropic 普通合法 message_stop/max_tokens 沿已有行为，摘要仍拒绝截断。没有新增正常控制流字符串匹配。

ReasoningSummary 的精确含义是“本实例已启用、且全链路已实现的主动 summary 请求路径”：仅 OpenAI 显式 summary 时为 true；没有 summary 即 false。当前检索发现该位由 Provider 声明并在测试中检查，尚无 Runtime 控制分支；本次 app 装配真实 Provider 时校验显式 summary 与能力声明一致，不把 false 当作“不能接收被动 summary”，也不新增无人使用的能力位或空策略。该位不表示探测出服务端支持。Anthropic ThinkingSignature、OpenAI EncryptedReasoning 保持原生保留语义，Anthropic ReasoningSummary 和两家 RawReasoning 均 false。

### 8. TUI 装配和受限终端

从同一 resolved 设置生成 typed、非敏感、不可变启动展示值，app 随当前 Model 组合装配传入；不改 ContextSnapshotPayload、ControlItem、headless v1 schema 或事件 kind。启动来源标签在此值中保留，以区分 explicit、Anthropic default 4096 与 OpenAI server default/unknown；它不进入 cache identity。正常应用装配必须提供该值，纯 TUI fixture 若未提供则显式 unavailable，不能猜测 family。

`/context` 仍精确本地命令：动态数值来自 committed snapshot，设置来自冻结启动值。显示 thinking、manual budget、display、effort、summary、ordinary cap，以及“configured; endpoint support not verified”的简短说明；实际活动和正文仍由原有流与 Ctrl+O 决定；用户选择、实际发送值和返回 usage/可阅读状态分开，不以 display summarized 保证正文，空 thinking 也不等于零消耗。没有 `/effort` 注册项、模型工具、输入消息或自动设置回写。三种 headless 在真实请求中使用配置，不新增设置展示字段或推理正文事件；已有 reasoning_output 字段按更新的计量契约产生 known/unknown，不能宣称其数值语义也保持不变。

当前 context 面板按高度裁剪；新增信息要纳入同一有界布局，在普通高度完整展示，窄屏折行，极小屏优先关键值和 Esc 关闭提示。若信息多于可用屏幕，使用面板本地有界滚动，不改变 composer/审批/问卷/Hook/详情的优先级；不能以最底部裁剪掉设置后声称已可查询。模型 metadata 仍按既有安全终端规则过滤。View、getter 和 repeated query 零 I/O、零 revision/fingerprint 变化。

### 9. 验证矩阵和落点

以下是实施验收目标；新增文件名表示计划落点，允许在相同边界内使用更清晰名字，但验收记录必须写实际路径。

| 层次 | 必须覆盖的业务断言 | 现有或计划入口 |
| --- | --- | --- |
| domain/config | thinking/display 联合、零值、null/重复/大小写/跨分支、budget/output 边界、family 环境覆盖、原配置保真 | `internal/domain/inference_settings_test.go`、`internal/config/reasoning_test.go` |
| 启动副作用 | 无效配置零请求/零数据目录/零后台启动；secret 哨兵不出错/输出 | `internal/app/reasoning_config_test.go`，复用 cache_e2e 模式 |
| Anthropic 编译 | omit/disabled/adaptive/manual、display summarized/omitted/省略、独立 effort/输出，各 cache mode 新 golden；旧默认请求 golden 不变 | `internal/provider/anthropic/reasoning_request_test.go`、`cache_test.go`、`testdata` |
| OpenAI 编译 | effort-only/summary-only/both/default、每个公开枚举、输出、HTTP/WS full/incremental、参数拒绝与生成前安全回退 | `internal/provider/openai/reasoning_request_test.go`、`websocket_test.go`、`testdata` |
| 活动模型循环 | 实际请求携带设置，流返回推理+Read/Question/副作用调用，完整结果 durable 后续写；工具仅执行一次、每 sample 设置一致 | `internal/app/reasoning_e2e_test.go` 扩展；Provider/runtime 既有 loop 回归 |
| cache/planner | 默认请求/profile bytes 保真、display/effort 变化定位 profile、usage 不入 prompt、其他 segment 不变、属性不一致禁止增量 | `internal/context/reasoning_test.go`、两家 request evidence 与 cache 测试 |
| 恢复 | 更新当前 Anthropic fixture，raw/normalized 逐项核对、opaque/工具顺序、同配置请求与 fingerprint 等价；旧 shape 失败且 journal 不变 | 两家 `reasoning_restore_test.go`、`internal/app/resume_e2e_test.go` |
| 压缩 | 两家摘要保留冻结策略、独立额度；manual budget>=摘要上限预检拒绝、自动抑制、旧窗口继续/返还；取消/Sync uncertain 收口 | 两家 `compaction_test.go`、`internal/app/compaction_e2e_test.go` |
| 请求证据 | preflight/实际 body/properties/输入与请求 hash 同源；摘要后缀、tool choice、额度真实；来源变化拒绝，准备/失败不更新普通 baseline | 两家 `request_evidence`、`compaction_test.go`、OpenAI WS/fallback 测试 |
| Anthropic usage | 正值/零/缺失/非法/超总量，当前必需状态字段、raw/normalized 一致、unknown 聚合、摘要独立归属，缺字段旧 shape 拒绝 | `internal/provider/anthropic/usage_test.go`、`native_test.go`、`commit`、`internal/app/resume_e2e_test.go` |
| 失败生命周期 | 400 请求恰好一次、incomplete、随机 chunk/半包/UTF-8、部分参数取消、阻塞 stream/shutdown owner 完成；不泄漏正文 | 两家 reasoning_stream 测试与 transport/runtime 既有故障注入 |
| TUI/headless | 选择/发送/观测分开、display 空正文非零 usage、受限终端与焦点、只读 fingerprint；外部字段不增、已有计量状态更新 | `internal/tui/context_test.go`、`internal/headless/reasoning_test.go`、app fixture |
| 真实终端 | 双 Provider 本地 fixture：display/usage、/context、工具后摘要和预算拒绝、Ctrl+O、取消、退出终端恢复 | `scripts/reasoning-controls-smoke.py`，Darwin PTY |

所有生成请求均为本地 httptest/WebSocket fixture、合成凭据，不访问公网或真实模型。新恢复 fixture 在仓库中逐字节固定，不能由被测 encoder 生成；原默认请求 golden 保留；受计量当前形状影响的恢复、结果/usage golden 按新契约替换，全部业务断言保留。Sync、阻塞流、关闭和取消使用已有确定性门闩/故障注入，不用 sleep 证明正确性。完成后执行 make verify（格式、vet、Staticcheck、架构/全量/race），并核对新增参数没有突破分层或安全断言。

实施后同步 README、能力清单、Roadmap、总体架构和本次实际发现的 pitfall；只描述已验证字段与平台。无需为了本切片强行新增 ADR；若实施暴露需长期决策的边界，再先修订本 change。不能把测试失败变成“已验证”，也不能删除/跳过/弱化既有断言。

## Risks / Trade-offs

- [参考快照落后于公开 API，任意 base_url/model 可能拒绝参数] → 明确公开枚举与本地形状，不按厂商推定支持；失败安全返回，不隐式探测、换模式或重试。
- [高 effort 可能耗尽输出且没有可见回答] → 独立可见的普通上限、明确窗口预留及 incomplete 验收；不承诺每 sample 上限等于整个工具 turn 成本上限。
- [多套 request 派生结构漏字段] → 固定 golden 同时覆盖 Anthropic 缓存和 OpenAI HTTP/WS full/incremental/fallback，实际请求证据也覆盖字段。
- [冻结设置不具备 Claude 的运行中 /effort 便利性] → 范围明确为启动配置和只读查询；动态更新先有自己的 native/cache/恢复设计，当前不提前暴露入口。
- [保留推理策略可能增加摘要成本或令 manual budget 超过独立额度] → 预检明确预算拒绝，不自动夹取/删字段；旧窗口合法才继续，实际高 effort 输出仍可能耗尽，不保证缓存或摘要成功。
- [display 请求不保证正文，正文长度不等于真实消耗] → 正文、请求选择和计量分开；省略保持不发送，空 thinking 可有正值/unknown usage。
- [Anthropic 计量当前结构变化影响全部内嵌 fixture] → 全量枚举 sample/checkpoint/failed provenance 和 normalized 断言，替换固定 current bytes，不保留旧开发 reader。
- [Session 不保存设置，因此不能从旧 journal 推导历史 effort] → resume 使用当前启动设置；只在相同设置下承诺请求等价，旧记录与 opaque 数据保持原样。
- [已有全量质量门出现其他失败] → 保留真实错误与实际范围；不得用隔离重跑通过替代全门结果，需修复契约或扩张范围时先更新 artifacts。

## Migration Plan

不执行数据迁移。新增外部配置与当前进程内设置值；Anthropic raw usage 当前形状增加必需 thinking_tokens 状态，包含 sample、checkpoint generation 和 failed provenance；相关固定 current fixture、normalized usage/checksum 一同替换。旧四项 native shape 在 repair/追加前失败关闭，不加版本标记、双 reader、旧 API facade 或 resume 重写。Session/checkpoint 外层、SQLite、OpenAI native 和 headless 字段/版本保持；已有 Anthropic reasoning_output 值/状态按新契约更新。默认请求和 profile 的旧 golden 仍保持，并不要求计量 fixture bytes 不变。

apply 启动先复核 artifacts 与仓库差异，然后按 tasks 实施；发现字段支持矩阵、预算、请求用途或恢复前提不成立时先更新 artifacts，不能静默延期或扩张。验收完成后再同步主规格并按独立归档流程操作。提案阶段只创建本 change 文件，不提交 Git、不修改生产代码或主规格。

## 初次提案阶段验证历史（2026-11-08）

以下保留初次提案当时的历史，不是本次修订后的验证或验收。初次只创建本 change 的 metadata、proposal/design/tasks 和八份 delta，未修改代码、测试或主规格；当时 32 项任务未勾选，当前任务已随修订重排。

- `openspec status --change add-provider-reasoning-controls`：4/4 规划 artifact 齐备，无跳过项；不表示功能已实现。
- `openspec validate add-provider-reasoning-controls --strict --json --no-interactive`：1 项通过，无 issue。
- `openspec validate --all --strict --json --no-interactive`：56 项通过、0 失败（2 个活动 change、54 个主规格）；既有长 requirement 只产生 INFO。
- 本地一致性检查：proposal 八个 capability 与八份 delta 一一对应，共 16 条 requirement、76 个 scenario；六个 MODIFIED requirement 保留主规格既有 34 个场景名称，正文只做所需的契约修改及当前九工具 vocabulary 校正。
- Markdown 本地链接、文件行尾空白、任务编号唯一性/未完成状态、改动目录隔离和 `git diff --check`：通过；全部新增文件仅位于当前 change。
- `make verify`：exit 0；分支策略回归、gofmt、go vet、Staticcheck、全量测试（含架构守卫）及全量 race 均通过。race 的 app package 用时 436.105 秒，为本次命令的实际耗时，不代表性能目标或失败。

上述是初次提案的代码基线与规划校验，不能作为本次修订后的功能验收。本轮不重新执行实现测试或 make verify；新 display/计量、摘要策略、同源证据、固定 bytes 和 PTY 必须在后续明确 apply 后实际实现并验证，最终重新运行 make verify。

## 本次规划修订与一致性校验（2026-11-08）

本次按用户明确要求修订 proposal/design/specs/tasks，不实现代码。保留 CLI metadata 和仍然成立的 OpenAI delta；其他既有 artifacts 同步调整，并在已授权的计量范围补齐 provider/usage-accounting delta。没有修改主规格、实现、测试、current fixture、其他 change、参考工程或 Git 提交。

四项结论已在契约、设计和验收任务中闭合：摘要保留冻结策略及独立额度/manual 预检；display 的两个值及合法分支；thinking_tokens 的来源、三态映射、当前 native shape 和恢复；同一请求计划生成预检/body/证据/receipt。用户选择、实际发送值与返回观测分开；工具 durable 与 Provider turn 结束分开；默认请求 bytes、外层 envelope/外部字段保持，与 native usage shape/已有指标状态变更分开。

- 当前九个 capability 与九份 delta 一一对应，共 22 条 requirement、111 个 scenario；八个 MODIFIED 块覆盖主规格原有 42 个场景，场景名称和无关语义保留，计量场景按明确的新契约修订。
- 40 项实施任务编号唯一、连续且全部未勾选，每项包含业务断言或验证入口；未将旧的 make verify 通过记为本次实现验收。
- change strict 校验通过；全库 `openspec validate --all --strict --json --no-interactive` 为 56 项通过、0 失败（2 个 change、54 个主规格），无 WARNING/ERROR，仅有既有长条款 INFO。
- capability/path 对应、完整 MODIFIED 场景保真、Markdown 本地链接、行尾空白、失效策略措辞、任务状态和改动目录隔离检查通过，`git diff --check` 通过。

本轮只执行规划一致性校验，不重新运行代码测试或 make verify。下一步先审阅修订，再由明确 apply 请求触发实现；届时必须替换相应 current bytes、执行全部新场景及最终质量门，不能据本记录宣称功能已交付。

## Apply 记录（2026-11-08）

- 基线 HEAD：`27be6353d4c50f70f214ab9a308ed0fba61c3729`，apply 开始时除本 change 外工作区无改动。九份 delta 与当前实现逐项复核；change strict 校验通过。沿用同一 HEAD 在初次提案实际执行的完整 `make verify` 成功记录作为未修改代码的质量门基线，最终必须重新执行。

- 默认 Messages/Responses request、缓存及 profile golden 保留；配置/domain/context 与双 Provider 包的阶段测试通过。Anthropic raw usage/current fixture 的变更仅限必需 thinking_tokens 与对应 normalized 状态/checksum；没有旧 reader 或版本标记。

- 实施与验收已完成，40/40 tasks；完整 `make verify` exit 0（含架构/全量/race，app race 421.014 秒），默认请求 golden 保持。双 Provider 16 个 Darwin PTY 场景、raw/normalized 当前格式恢复、自动摘要与预算拒绝及 strict 一致性校验通过；细节、实际路径和完整质量门输出见 [verification.md](verification.md)。本轮未提交、同步主规格或归档。

## 全面回归与归档补充（2026-11-11）

全面回归发现两个 reducer 在类型解码前未校验完整事件帧，重复成员可由后值覆盖，掩盖非法计量、总量关系或响应 ID 冲突。修复在现有 reducer 入口复用 `codec.ValidateJSON`，先拒绝重复成员、尾随内容和有损 Unicode，再执行原有类型解码；不新增 codec、全局状态、资源 owner 或跨层接口。合法未知扩展继续由当前 Provider 受控处理，不使用封闭对象解码替代整个 wire。不同事件的合法观测更新与同一对象的重复成员分别处理。

新增两家 `usage_validation_regression_test.go`、`internal/app/reasoning_malformed_stream_test.go` 和 `internal/app/reasoning_cli_e2e_test.go`。8 个修复前失败复现、6 组异常流、24 组真实 CLI、32 个 PTY 场景、5 轮乱序 race 和 421,051 次模糊输入及完整质量门的证据见 [全面回归记录](regression-2026-11-11.md)。原来 40 项任务之外，本次将回归和归档核对独立记入 tasks，规范化完整帧拒绝和连续零值更新场景后再同步主规格。
