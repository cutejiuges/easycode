## Context

动机见 [proposal.md](./proposal.md)。变更前 Runtime 在一次文本 turn 中只执行一个 Provider sample：Provider 成功终态返回 `PreparedSample`，Runtime 将 `[provider_native_commit, turn_completed(v1)]` 作为一个 batch Sync，随后 finalize 原生历史并发布无 payload 的 `turn_completed`。Anthropic reducer 已保存四个 raw usage 字段及 known/unknown 状态，OpenAI reducer 只读取 completion ID，未保存 usage。

共享层必须保持 Provider wire opaque，同时 Session JSONL 是事实源、记录不得在 resume 时原地迁移、成功终态只能在 durable facts 后发布。项目仍处于 P2，仓库没有稳定发布版本；现有 v1 fixture 是开发期基线而非需要长期共存的外部兼容承诺。Roadmap 要求基础设计不成立时直接回到当前阶段重构，而不是叠加兼容分支。

参考工程提供了两类有用经验：Claude Code 证明 Anthropic start/delta usage 需要字段级合并，并直接在既有 result/message shape 中增加 usage；Codex 将单响应 raw observation、独立 `TokenUsageRecord` 和 UI/宿主累计快照分层，后续增加 usage 字段时也没有为该能力创建新的 Exec 或 rollout 主版本。两者的公开 v1/v2 只用于整套外部 API 的代际变化，而不是把每次字段或 record 扩展当成版本升级。两者仍存在 EasyCode 不采用的做法：字段缺失默认零、累计量重复持久化、全局可变成本状态，以及依赖对象引用和延迟序列化完成落盘。

## Goals / Non-Goals

**Goals:**

- 建立一个位于共享底层、不可变、可校验的 normalized usage value model，供 Provider、Session、Runtime 和宿主共同使用。
- 保留 Provider raw usage，并让 raw 与 normalized facts 来自同一个 completed sample。
- 让 sample usage 与 native commit 具有同一 durable 原子边界，并把尚未发布的开发期契约重写为唯一 v1 基线。
- 为未来一个 turn 多次 Provider sample 预留正确的聚合与持久化粒度，不提前实现工具循环。
- 保持 headless JSONL v1 的事件序列和终态语义不变，同时把 usage 纳入完整的 v1 completion shape。

**Non-Goals:**

- 不引入价格表、货币、成本、配额、rate limit 或订阅语义。
- 不把 usage 当作 ContextPlanner 的 token estimate 或 compaction 依据。
- 不持久化 turn/thread/session 累计量，不修改 SQLite Catalog。
- 不新增 TUI usage UI、失败终态 usage、独立 headless usage 事件或 JSON 版本选择参数。
- 不实现跨 Provider 历史转换、tool loop、input-only commit 的 usage、旧 journal 原地补数或开发期旧格式兼容读取。

## Decisions

### 1. 以 sample usage 作为唯一持久化计量原语

数据流固定为：

```text
Provider completion
  |-- provider-owned raw usage --> native commit payload
  `-- normalized SampleUsage ----> sample_usage Session record
                                        |
                                        v
                              turn aggregation in memory
                                        |
                                        v
                         RuntimeEvent --> headless projection
```

一次 Provider completion 是服务端 usage 的真实归属边界，因此 `sample_usage` 与 native commit 配对；turn usage 是派生投影。当前文本 turn 只有一个 sample，未来 tool loop 可以在同一 turn 中追加多个 sample pair 后使用同一聚合器。

替代方案是只保存 `turn_usage`。这会在多 sample turn 中丢失响应归属、取消/工具失败前已经发生的用量和逐 sample raw 对照，因此不采用。

### 2. normalized usage 放在 `internal/domain`，使用不可变三态值

共享模型放在最低依赖层 `internal/domain`，避免 Session 依赖 Provider 或 Provider 依赖 Runtime。建议使用私有字段与纯内存 typed constructors 表达：

```text
UsageMetric = Known(uint64) | Unknown | NotApplicable
SampleUsage = {
  input_uncached,
  cache_read,
  cache_write,
  output,
  reasoning_output
}
```

getter 返回值拷贝；`Known(0)` 与 `Unknown` 不同。Session、protocol 和 headless 各自拥有强类型 wire DTO，使用显式 `state` 和仅在 known 时存在的 `value`，不让 JSON tag 或 `omitempty` 决定业务状态。所有转换都调用同一 domain validator。

计数使用 `uint64`，Provider wire 解码直接拒绝负数和溢出。聚合按指标执行：unknown 优先；否则 known 求和并忽略 not-applicable；全部 not-applicable 才得到 not-applicable。这样非 reasoning sample 不会污染同一 turn 中其他已知 reasoning sample，同时任何真正缺失的数据都会阻止虚假精确合计。

替代方案是 `*int64` 或 value+known bool。前者不能区分 unknown/not-applicable，后者容易产生 known=false 但带非零 value 的非法组合，因此不采用。

### 3. Provider 保留 raw usage，并在 completion 边界投影 normalized usage

Anthropic 继续在 Provider 包内维护 raw usage。`message_start` 的显式零是 known zero；`message_delta` 的 input/cache 零按 Anthropic 累计流语义视为占位，不覆盖 start 正值，也不单独建立 known zero；output delta 的显式零有效。最终只在合法 `message_stop` 创建 normalized usage。

OpenAI reducer 扩展 `response.completed.response` 的强类型 wire，仅在 response ID 校验后读取 usage。总 input 与 cached input 同时已知时才计算 uncached；cached 大于 total 是协议错误，不采用 Codex 的 clamp-to-zero。raw details 缺失时保留 unknown。

Anthropic native payload v1 已有 raw usage，保持 canonical shape。OpenAI native payload v1 直接补充 raw usage，并同步重写当前开发期 codec fixture；decoder 只接受完成后的严格 v1 shape，不保留缺少 raw usage 字段的旧 v1 分支。RequestCompiler 仍只读取 native input/output items，usage metadata 不进入请求或 fingerprint。

替代方案是只在共享 `SampleUsage` 中保存数值。它无法审计归一化是否正确，也会破坏 Provider-native 无损恢复原则，因此不采用。

### 4. `PreparedSample` 原子携带 native envelope、normalized usage 和 finalizer

`PreparedSample` 增加不可变 `SampleUsage`，constructor 必须同时验证 envelope、usage 和 finalizer，getter 返回值拷贝。Runtime 在 append 前再次读取并校验两部分；任一失败都走现有 stream protocol failure 路径。finalizer 仍只负责纯内存 native history，且只能在 durable batch 成功后执行一次。

不把 usage 放在独立 StreamEvent：独立事件可能先于 terminal 被宿主观察，或者与 native completion 在错误路径上脱离。把两者绑定到 prepared sample 能保持“同一 response、同一成功决策”。

### 5. Session 直接重写为单一 v1 成功 batch

公共 envelope `schema_version` 保持 1，所有当前 payload revision 也保持 1。新增：

- `sample_usage` payload v1：五个 normalized metrics，仅包含共享事实。
- `turn_completed` payload v1：保持当前 revision，并由 ReplayPlanner 的 v1 状态机要求本次 completion 必须有配对 usage。

唯一合法的成功 batch 为：

```text
[provider_native_commit(v1), sample_usage(v1), turn_completed(v1)]
```

Writer 和 ReplayPlanner 只实现这一种 v1 形状，并将 usage 作为 `SampleUsageRecord` 暴露。当前 `internal/session/testdata/migrations/v1` fixture 与 replay plan 原位重写为完整三记录基线，并在本变更验收后作为不可变 v1 历史 fixture 冻结。本变更前生成的两记录完成 journal 在语义回放阶段 fail closed，不被识别为合法历史变体，也不创建全零 record。

`sample_usage` 与其前一个 native commit 通过同一 batch、turn identity、固定相邻顺序和连续 seq 配对，不新增独立 `SampleID`。当前 Session batch 已经是原子 sample completion 边界；额外 ID 会制造另一套需要校验和迁移的身份体系。未来若一个 batch 需要容纳多个 sample，应先判断现有 v1 状态机能否通过新增 record kind 加法表达；只有必须与已冻结 v1 并存的不兼容语义才引入新 revision。

不使用 `turn_completed(v2)` 区分开发期旧写法，因为这会把尚未发布的错误基线永久化并制造双 replay 状态机。重写 v1 后，任何缺少 `sample_usage` 的成功 batch 都是确定的非法结构，不存在“合法历史缺失”歧义。把 normalized usage 塞进 opaque native payload则会迫使 Session 解释 Provider wire，也不采用。

### 6. Runtime 与 headless 都在现有 v1 中补全 completion

进程内 `protocol.Event` 只在同一仓库内同步编译，当前没有独立部署消费者，因此 `protocol.CurrentVersion` 保持 1，`turn_completed` 在 v1 中直接从空 payload 改为强类型 turn usage，并同步所有生产者和消费者。Runtime 顺序固定为：

```text
validate prepared envelope + usage
  --> build native/usage/completion drafts
  --> AppendBatch + Sync
  --> Finalize native history
  --> publish turn_completed(usage)
```

若 append/Sync 失败，Runtime poison 并发布失败；若 durable success 后 finalizer 异常，仍按现有不可继续状态处理，不回滚 journal，也不发布成功 usage。当前 turn aggregate直接使用单个 sample；聚合器作为纯逻辑同时落地，供未来 tool loop 复用。

不持久化 turn aggregate，因为它可由 sample facts 确定性推导；避免 Codex 式 raw/turn/thread 多份累计事实发生漂移。

### 7. Headless `turn.completed` 冻结为完整 v1 shape

外部 headless JSONL 的 `version: 1` 保持不变，`turn.completed` 增加 required `usage`。producer 总是输出该字段，v1 golden 原位更新并在验收后冻结；缺少 usage 的 completion 不再属于当前 v1 契约。消费者仍应忽略已知事件的未知字段，以允许真正的加法扩展。事件类型、顺序、终态和退出码均不变。

这与 Claude Code 把 usage 放在最终 result、Codex Exec 把 usage 放在 `turn.completed` 的做法一致。新增 `usage.updated` 会改变封闭事件集合及事件数量；引入 `--json-version 2` 会在没有破坏性变化时增加 CLI 和测试矩阵，均不采用。

只有在 v1 已发布或冻结后，出现无法加法表达的不兼容变化，并且旧数据或客户端必须与新契约并存时，才升级外部 JSON 主版本。内部 compile-together 类型变化、新 record kind、可选字段扩展或稳定发布前的基线修正都不是升级理由。

### 8. 测试以 wire、durability 和兼容边界分层

- Domain：三态构造/校验、known zero、聚合 unknown/not-applicable、溢出和不可变性。
- Provider：Anthropic start/delta 零覆盖回归；OpenAI 完整/部分/缺失/非法 usage SSE；raw commit golden 与 normalized mapping。
- Native restore：OpenAI v1、Anthropic v1 round-trip，以及 uninterrupted/restored request canonical bytes 和 fingerprint 等价。
- Session：typed codec、唯一三记录 v1 batch、非法 placement、尾部 repair、重写后 v1 fixture 逐字节稳定，以及旧两记录开发 journal fail closed。
- Runtime：append/Sync/finalize/publish 时间线、invalid prepared usage、poison 和取消路径。
- Headless：重写后的 JSONL v1 golden、unknown/known/not-applicable encoding、required usage 和既有事件顺序。

## Risks / Trade-offs

- [兼容服务的 usage shape 与官方 wire 有差异] -> 只接受明确支持的强类型字段；未知字段留在 Provider 受控 raw envelope，缺失字段为 unknown，非法关系 fail closed。
- [三态模型让 JSON 比纯整数更冗长] -> 保留显式状态以避免长期数据污染；usage 每个 sample 只写一次，大小相对 native payload 可忽略。
- [新 required record 使旧二进制无法恢复新 journal] -> 这是 required record 契约的预期 fail-closed 行为；不提供向下写兼容或原地降级。
- [本变更前的开发期 v1 journal 无法 resume] -> 在 load 的语义回放阶段返回稳定不兼容错误，不 repair、不 append、不发起 Provider 请求；保留原文件供人工检查，接受开发期基线重置的成本。
- [durable success 后 finalizer 失败导致 journal 与进程内 history暂时不一致] -> 保持 Runtime poisoned，禁止下一请求；重启后以 JSONL 恢复为准，沿用现有两阶段提交恢复模型。
- [开发期 headless 消费者仍按旧 completion shape 校验] -> 明确这是稳定发布前的破坏性基线修正，更新仓库内 golden 和消费者测试；不为旧 shape 增加 v2 或缺字段兼容分支。
- [usage 被误用于上下文压缩] -> domain 命名和 API 不暴露 context-window 含义，设计与测试明确 usage 不参与 RequestCompiler、cache fingerprint 或 compaction。

## Migration Plan

1. 先增加 domain usage 和两家 Provider 的完整 v1 raw/normalized usage，再同步切换 `PreparedSample` 契约。
2. 在同一 change 中更新 Session v1 registry、ReplayPlanner、writer 与 Runtime v1 completion，使成功路径只产生三记录 batch；不引入过渡期双 reader/writer。
3. 原位重写 OpenAI native v1 fixture、Session migration v1 fixture/replay plan 和 headless JSONL v1 golden，并在新契约通过测试后重新冻结。
4. 运行 `make verify`，并核对 request canonical bytes、fingerprint、record vocabulary 与未改动路径一致；使用 `rg` 确认该 change 没有 v2 或双版本兼容实现任务。

回滚到变更前代码时，变更后生成的开发期 journal 必须在 repair、append 或 Provider 请求前只读失败。回滚不得通过删除 `sample_usage`、改写 completion payload 或重编码 native payload降级；需要保留旧环境时只能使用独立测试数据副本。

后续只有当一个契约已经稳定发布或明确冻结、新表示无法加法表达、旧数据或客户端必须继续运行，并且已经定义兼容窗口与旧版本退出条件时，才引入 v2。版本号是兼容成本边界，不是功能迭代计数器。
