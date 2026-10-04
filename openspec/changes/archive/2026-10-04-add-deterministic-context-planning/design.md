## Context

参见 `proposal.md` 的动机。当前工程已经有三块可直接复用的基础：

- `internal/context` 的 `Segment`/`Plan` 已提供 canonical JSON、稳定性排序、深拷贝和 stable prefix fingerprint，但尚未定义一次请求的来源、token 估算或预算状态。
- `provider.Conversation` 同时拥有 Provider-native history 和单向 `HistoryProjector`。语义投影刻意丢弃 thinking、signature、encrypted reasoning 等数据，因此共享估算不能只看投影；反过来，共享层也不能解析 native item 或从投影重建请求。
- `runtime.RunTurn` 已把 durable `turn_started` 放在 `Conversation.Stream` 之前。这是加入无副作用规划并保证“明确超限时零网络调用”的自然线性化边界。

Claude Code 2.1.88 的参考实现会用最近一次 API usage 作为锚点，再估算尚未计量的尾部，避免随历史增长累计 usage；Codex 参考提交 `c248f6d48b` 则直接遍历将进入 prompt 的原生 item，以 byte heuristic 和饱和加法得到粗略下界。两者共同说明估算必须尊重实际历史结构和不确定性，但 EasyCode 当前没有可靠的跨 Provider usage 锚点语义，因此本切片选择对“下一请求会重放的已提交原生历史”做一致的本地估算，不把 normalized usage 混入上下文占用。

## Goals / Non-Goals

**Goals:**

- 建立可扩展但当前足够小的 `ContextPlan`，明确来源顺序、来源 revision、生命周期、缓存稳定级别、估算和预算状态。
- 保持 Provider-native history 是请求续写唯一事实源，同时让 Provider-private 内容参与数值 footprint。
- 对相同 live/restored native history 产生相同 footprint、context plan 和 cache fingerprint。
- 在显式配置且证据完整时，于网络副作用前拒绝超限请求。
- 为后续项目指令、工具 schema、skills、world state、Provider cache 策略和 compaction 留下可组合的来源边界。

**Non-Goals:**

- 不实现 tokenizer 精确计数、远程 token count、基于 usage 的增量校准或模型窗口注册表。
- 不改变 Anthropic/OpenAI request compiler、wire 字段、native payload revision 或 Session schema。
- 不实现自动压缩、历史裁剪、跨 Provider history 转换或 cache marker/key 写入。
- 不把 context plan 暴露为 RuntimeEvent、headless JSON、TUI 面板或持久化记录。
- 当前不引入尚无消费者的 `diff` 生命周期；只实现 `replace` 和 `append`。

## Decisions

### 1. 在 `domain` 放置最小估算值对象，在 `context` 组合计划

新增不可变强类型值对象，名称可在实现时按现有命名调整，但职责保持如下：

- `domain.TokenEstimate`：保存 estimator method、`estimated|unknown` 状态和 `uint64` token 数，提供 typed constructors、validator 和值拷贝 getter。
- `domain.NativeHistoryFootprint`：保存 Provider family、`uint64` committed revision 和 `TokenEstimate`。它只表达跨 Provider 都需要的只读事实，不承载 wire 或缓存策略。
- `context.Budget`：保存启用状态、window、reserved output 和 safety margin；构造时完成无溢出校验。
- `context.PlanningInput`：聚合 Provider profile、`SemanticHistoryView`、native footprint、当前输入和 budget。
- `context.ContextPlan`：保存固定 revision、深拷贝的有序 `PlannedSource`、既有 `context.Plan` cache snapshot、总估算和 budget decision。

`provider.Conversation` 组合一个小的 `HistoryFootprinter` 接口并返回 `domain.NativeHistoryFootprint`。这样 `domain/protocol` 不依赖 Provider 或 I/O，`context` 不需要导入具体 Provider，Runtime 只负责组装已经强类型化的快照。

不选择让 planner 接收 `provider.Conversation`，因为这会把历史读取时机和 Provider 生命周期藏进纯逻辑组件；也不选择 `map[string]any` 来源表，因为它违反核心协议的强类型约束并把校验推迟到运行期。

### 2. 使用固定的三个来源和两个正交分类轴

当前计划严格按下列顺序构造：

| 来源 | 内容 | 生命周期 | Cache stability | Source revision |
|---|---|---|---|---|
| `provider_profile` | family、model、profile schema revision | `replace` | `stable` | 固定 profile schema revision |
| `committed_history` | 语义历史快照和不含正文的 native footprint | `append` | `turn_stable` | footprint committed revision |
| `current_input` | 本轮用户文本 | `replace` | `volatile` | 固定 turn-input schema revision |

生命周期描述来源如何随运行推进，cache stability 描述它是否进入稳定前缀；两者使用不同枚举和 validator。`provider_profile` canonical JSON 明确排除 API key、base URL、窗口预算、cwd 和 Session identity。预算影响准入但不影响请求内容，因此也不进入 cache segment。

`committed_history` 的 cache segment 可以包含语义文本和 footprint 数值，但不能包含 native item 或 opaque bytes。其 source revision 使用 Provider committed revision，而不是语义 turn 数；这样未来 input-only tool result 即使没有新增可见 turn，也能正确使该来源 revision 前进。该分段是 `turn_stable`，所以它不会污染当前只覆盖静态 profile 的 stable prefix。

不把三个来源直接塞进现有 `context.Plan` 并让它承担全部语义，因为现有类型已经是明确的 CachePlanner 输入。`ContextPlan` 组合它，保留缓存契约，同时避免把 lifecycle、estimate 和 budget 硬塞进缓存对象。

### 3. 采用版本化 byte heuristic，并保留 unknown

新增一个无状态估算子包，例如 `internal/context/estimate`，由共享 planner 和两个 Provider footprint 实现共同调用。`byte_heuristic_v1` 对 UTF-8/opaque byte 长度使用确定性的 ceiling division，并由 Provider scorer为会实际重放的 role、item、content block 和 JSON framing 加入固定、可测试的结构开销。所有累计使用 `bits.Add64` 检测溢出或 `uint64` 饱和加法；溢出结果不能 wrap 为小值。

Provider scorer 必须贴着各自 RequestCompiler 的 native history 遍历顺序维护：

- Anthropic 计入下一次 `messages` 会重放的 user/assistant content blocks，包括 thinking/signature、redacted thinking 和受控 unknown block；排除 usage、stop metadata 和 transport 字段。
- OpenAI 计入下一次 `input` 会重放的 message、reasoning/encrypted content、phase 相关请求内容和受控 unknown item；排除 response ID、usage 和 transport metadata。

共享 visible history 估算和 Provider native footprint 使用同一个 method revision。因为 native footprint 是包含可见内容的总量，committed history 合并规则为：

```text
history_tokens = max(semantic_visible_tokens, native_history_tokens)
total_tokens   = saturating_add(history_tokens, current_input_tokens, profile_tokens)
```

这不是用 max 掩盖差异：若任一必需输入为 `unknown` 或 method 不兼容，history 和 total 都标记 `unknown`，并保留可诊断的来源状态。当前 profile 不包含 prompt 指令，token 贡献为零；后续加入系统指令时必须作为新来源显式计量。

不直接使用 normalized usage，原因是它描述已完成 sample 的计费/缓存字段，并不保证等于下一次请求的模型可见历史；累计多个 turn 的 input usage 还会重复计算前缀。未来若引入 usage 锚点，必须通过独立 OpenSpec 明确其 Provider 语义和去重边界。

### 4. Provider footprint 以 committed snapshot 为边界

Anthropic/OpenAI Conversation 在现有历史锁或不可变 snapshot 边界内读取 committed native history，锁内只取得内存快照或完成有界纯内存遍历，不执行 I/O、callback、channel 操作或等待。staging sample 不参与 footprint；prepared sample 只有在 durable success 后调用既有 finalizer，才同时进入 committed history并令 revision 恰好增加一次。

空 history revision 为 0。每个成功 native commit 推进一次 revision；restore 按已验证 commits 的顺序重建同一 revision，因此 uninterrupted/restored footprint 等价。revision 只是单调计数，不是内容 hash，不允许调用方注入，也不暴露 opaque 内容。若未来某种 native item 无法被当前 estimator 安全处理，Provider 返回 `unknown`，不得跳过该 item 后谎报较小的已知值。

不把 footprint 缓存在可变全局或 package registry；当前历史规模下每 turn 的一次有界遍历更简单，也避免缓存失效与 finalizer 竞争。后续若性能数据证明需要缓存，可在 Conversation 实例内按 committed revision memoize，且不能改变契约。

### 5. 配置采用显式可选窗口，不维护模型名注册表

顶层 `config.Config` 增加值类型 `ContextBudget`。文件 wire 使用能区分“字段省略”和显式零的可选数字表示，严格 decoder 对三个字段单独接受 JSON unsigned integer；既有 Provider 字段仍只接受 string。解析后统一构造 budget：

```text
disabled: context_window_tokens omitted, reserves must also be omitted
enabled:  window > 0, reserved >= 0, margin >= 0,
          reserved + margin does not overflow and is < window
```

省略两个 reserve 字段时均为零。这比内置 4K/20K 默认值更诚实，因为不同 Provider/model 的最大输出差异很大；用户可按实际服务显式配置。环境变量当前没有这些字段，保持现有 environment-only 启动兼容。错误继续映射到英文 `invalid_configuration`，不得回显配置正文。

不选择根据 model 字符串查表：自定义 `base_url` 可能复用任意 model 名，静态表会快速过期，并把错误的“已知窗口”升级为硬拒绝。模型注册和服务端 capability discovery 若以后需要，应作为独立能力设计。

### 6. Runtime 在 durable start 后、Stream 前规划

应用装配在创建 Runtime 时传入已验证的 profile、budget 和纯内存 planner。`Runtime.New` 验证 profile family 与 Conversation family 一致，构造过程仍不读取历史或执行 I/O。`RunTurn` 的顺序调整为：

```text
acquire active turn
allocate turn ID
durable append turn_started
emit turn_started
snapshot SemanticHistoryView
read NativeHistoryFootprint
build ContextPlan
if planning error or confirmed over_limit:
    durable append turn_failed
    emit turn_failed
    return
Conversation.Stream(ctx, input)
continue existing terminal/durability flow
```

明确超限使用新增 `fault.CodeContextLimitExceeded`，外部值固定为 `context_limit_exceeded`。错误消息包含 estimated total/effective limit/state 等安全数值，不包含来源正文。`not_enforced`、`within_limit`、`indeterminate` 都继续请求；这是有意的 fail-open：启发式或未知数据不能被包装成确定事实。Provider 服务端仍是最终窗口权威。

规划错误走既有 `failTurn`，因此 `turn_started` 和 `turn_failed` 都 durable，且 Session 写失败仍会 poison Runtime。计划本身不写 journal；恢复时用 restored native history 重新得到等价计划，避免新 schema、migration fixture 和 prompt 数据落盘。

不把规划放在 durable start 之前，因为被接受 turn 的失败应有完整 Session 生命周期；不放在 Provider 内部，因为 Runtime 才拥有 durable failure 与零网络调用的编排责任；不在 `Stream` 后检查，因为那时网络副作用已经发生。

### 7. 测试围绕等价性、零副作用和不泄密

测试分四层：

- `internal/context`: table tests 覆盖来源顺序、深拷贝、canonical bytes、stable prefix、max 合并、unknown 传播、边界等于、明确超限、算术溢出和 secret 排除。
- 两个 Provider: native fixture 覆盖可见文本、opaque reasoning、unknown raw item、staging 排除和 revision；golden/cache regression 证明读取 footprint 前后 request bytes、item 顺序和 fingerprint 不变。
- restore: 对同一 commits 比较 uninterrupted/restored footprint、计划输入基础和下一请求 canonical bytes。
- Runtime/config/app: 确认超限路径 timeline 为 `turn_started -> plan -> turn_failed`、Stream 调用次数为零、journal vocabulary 不变；配置测试覆盖省略、零、负数、非整数、overflow、reserve 无 window、权限与 secret redaction。

Provider request golden 的预期正文原则上不应变化；若实现导致 golden diff，必须先判定是否意外改变 RequestCompiler，而不是直接更新 fixture。

## Risks / Trade-offs

- [byte heuristic 与真实 tokenizer 有偏差，可能漏掉服务端超限] → 明确标记 `estimated`，只在证据完整且明确超过用户上限时提前拒绝；服务端仍是最终权威，后续可增加 tokenizer/remote counter method 而不改变计划形状。
- [Provider scorer 与 RequestCompiler 演进不同步] → 将同一 native fixture 同时用于 footprint、request golden 和 restore 等价测试；新增 native item 时要求 footprint 已知计量或显式 unknown。
- [对 semantic 与 native 取 max 可能隐藏 scorer 缺计] → 测试要求 native fixture 覆盖全部可重放类型，并把 method/revision 暴露给诊断；max 只用于防重复计数，不替代 Provider coverage。
- [每 turn 遍历全部历史带来 O(n) 成本] → 当前切片优先正确性；保留 committed revision 作为未来 Conversation-local memoization key，性能优化不得引入全局缓存。
- [计划持有当前输入和语义历史副本增加内存] → 受现有 cache segment 上限约束，只存于单次 `RunTurn` 栈生命周期，不持久化、不写日志，完成规划后不跨 turn 保留。
- [显式窗口配置可能被用户填错] → 不声称模型自动识别，错误配置只影响本地提前拒绝；文档说明服务端窗口为最终事实，删除三个字段即可无条件关闭本地硬守卫。

## Migration Plan

1. 先加入值对象、planner 与配置解码，默认 window disabled，确保所有现有配置行为不变。
2. 为 Anthropic/OpenAI 实现 footprint 和 revision，并先通过 request golden、native round-trip 与 restored equivalence。
3. 在应用装配中传入 profile/budget，再把 Runtime 规划点接到 durable start 与 Stream 之间。
4. 运行全量 `make verify`，确认 Provider request fixture、Session fixture、事件 snapshot 和 race test 无非预期变化。

回滚时可移除 Runtime 规划调用和新增配置字段，Provider request/native payload/Session records 无需迁移或重写。已使用新字段的配置在旧版本会按既有 unknown-field 规则被拒绝，因此版本回滚前需先删除三个新字段；实现与发布说明必须明确这一点。
