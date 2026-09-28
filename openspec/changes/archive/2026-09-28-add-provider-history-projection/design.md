## Context

参见 `proposal.md` 的动机以及 `specs/provider/history-projection/spec.md` 的行为契约。当前两家 Conversation 都在成功 terminal 后提交 native history，但存储形态不同：Anthropic 已使用 `[]nativeTurn` 保存 user message、assistant message 和 metadata；OpenAI 仍使用 `[]NativeItem`，依靠每轮先提交 user item、再提交 output items 的位置关系隐式表达 turn 边界。共享 `provider.Conversation` 只暴露 stream，历史快照方法只供各 Provider 包内测试使用。

基础 TUI 的 live transcript 将同一 turn 的 assistant text deltas 追加到一个 cell，当前没有 reasoning 或 tool 的可见投影。设计必须保持 Provider-native history 是请求续写事实源、共享层不导入 wire 类型、opaque reasoning 不泄漏、请求 canonical bytes 不受投影影响，并避免在锁内执行不可控工作。

## Goals / Non-Goals

**Goals:**

- 建立足够支持后续 token、resume、Hook 和 Subagent 的最小共享文本历史类型，同时保留 Provider 来源与 turn 边界。
- 由每个 Provider 在自身包内实现唯一的 native-to-semantic 映射，保证 committed-only、顺序稳定、无副作用和并发安全。
- 使 OpenAI 与 Anthropic 的 live 文本和 replay 文本具有同一用户可见语义，并用 golden/集成回归固定边界。
- 通过类型和依赖方向阻止语义视图参与 Provider 请求编译或恢复。

**Non-Goals:**

- 不实现语义历史的持久化、反序列化、Session schema、resume UI 或 RuntimeEvent replay API。
- 不实现 token estimator、ContextPlanner 接入、Stop hook、Subagent completion 或其他消费者。
- 不投影 reasoning/thinking、phase、usage、tool call/result、metadata 或 unknown extension；这些能力在对应 live 语义和消费者存在后另行扩展。
- 不改变 Provider wire、缓存策略、capability 声明、TUI 展示和外部配置。

## Decisions

### 1. 共享语义类型放在 domain，保持最小 turn 文本模型

在 `internal/domain` 定义只含值类型的结构：

```text
SemanticHistoryView
  Provider ProviderFamily
  Turns    []SemanticTurn

SemanticTurn
  UserText
  AssistantText
```

字段使用稳定 JSON 名称，便于 golden 和未来 headless/session replay 测试，但本变更不承诺外部 wire 兼容。空历史使用非 nil 空 slice，输出形态确定；字符串和新分配的 slice 使视图天然不共享 native 可变 buffer。

当前不增加 role 枚举、item ID、completed flag 或通用 content union：每个元素已固定表达一次成功提交的 user/assistant turn，完成状态是集合成员资格的不变量。提前加入 reasoning/tool union 会在 live RuntimeEvent 尚未覆盖这些语义时制造虚假通用 Message。

备选方案是把类型放在 `provider` 包。它能减少一个 domain 文件，但会迫使 context、session、Hooks 和 subagent 依赖 Provider 层，与既定依赖方向不符，因此不采用。

### 2. Conversation 组合独立的 HistoryProjector 小接口

`internal/provider` 增加：

```text
HistoryProjector
  ProjectHistory() domain.SemanticHistoryView

Conversation
  HistoryProjector
  Family / Capabilities / Stream
```

投影不接收 `context.Context`、不返回 error：它只读取已经过 reducer 校验且成功提交的内存历史，不执行可取消 I/O；未来持久化恢复的解码和 schema 校验属于 Session/Provider restore 边界。接口嵌入会让所有 Conversation 和测试替身在编译期明确支持投影，避免调用方通过 family switch 或具体类型断言寻找能力。

Runtime 不主动调用 projector，也不把它转换为 RuntimeEvent；后续消费者由更高层按自身生命周期读取。备选方案是给 Runtime 增加 `History()` facade，但当前没有消费者，会扩大 Runtime 职责并模糊 native/semantic 边界，因此延期。

### 3. Projector 先取得深拷贝快照，再在锁外执行纯映射

每个 Conversation 的 `ProjectHistory` 先调用既有或重构后的 `nativeHistory.snapshot()`；snapshot 在读锁内只深拷贝 native 数据，释放锁后再连接文本并构造 semantic slice。这样并发提交的投影结果只可能看到提交前或提交后的完整历史，不会持锁执行遍历、序列化或用户回调。

投影结果每次重新分配 `Turns`，不返回 native slice、`json.RawMessage` 或 content slice。调用方修改结果不会影响会话、其他视图或请求编译。

备选方案是在 commit 时维护第二份 semantic history。它会引入双写一致性和失败回滚问题，并使语义视图更像事实源，因此不采用。

### 4. Anthropic 直接按 nativeTurn 投影，仅连接 text block

Anthropic 每个 `nativeTurn` 映射为一个 `SemanticTurn`：按 `User.Content` 和 `Assistant.Content` 顺序连接 `type=text` 的 `Text`。连接不插入分隔符，不 trim、不规范化换行；thinking、signature、redacted thinking、metadata、usage 和未知 raw block 全部跳过。

即使成功 turn 没有可见 assistant text，也保留对应 turn 并输出空字符串，因为 turn 边界已经成功提交；投影器不能编造占位文本或静默改变对话轮数。

### 5. OpenAI native history 改为显式 nativeTurn，RequestCompiler 保持原 wire

在 OpenAI 包内新增：

```text
nativeTurn
  User    NativeItem
  Outputs []NativeItem

nativeHistory
  turns []nativeTurn
```

成功 `response.completed` 时一次提交一个深拷贝 turn。RequestCompiler 接收 turn snapshot，按每轮 `User` 后跟 `Outputs` 的顺序展平成原有 `input`，最后追加当前 user item；因此首轮/多轮 request JSON、item 顺序、fingerprint 和 opaque reasoning 回放不变。

投影每个 turn 时，只读取 user message 的 `input_text` 与 assistant message item 的 `output_text`，按 output item/content part 顺序连接。reasoning item、summary、encrypted content、phase、ID、unknown raw item 和非当前文本类型全部忽略。只接受匹配 role/type 的可见文本，避免未知扩展偶然携带同名字段时被误投影。

备选方案是从扁平 `[]NativeItem` 遇到 user item时推断 turn。它依赖当前 item 排列的隐式约定，对未来 tool output、server item 或恢复数据不稳健，也无法可靠表达空输出 turn，因此现在收敛存储边界。

### 6. Golden 固定视图，集成测试固定 live/replay 等价

两家 Provider 各增加包含多片文本与 opaque 数据的 projection golden；golden 只出现 provider 和可见 turn 文本。单测覆盖空历史、顺序、无文本项、返回值修改和 conversation 隔离。

Conversation 集成测试使用本地 `httptest` 收集 live assistant delta，成功后调用 `ProjectHistory` 并断言最后一个 assistant 文本完全相同；失败、取消和提前 EOF 后断言投影不变。并发投影/提交由 `go test -race` 覆盖。

OpenAI 重构必须继续运行 request golden、稳定 fingerprint 和双轮 API prefix 测试，并增加“投影前后 request canonical bytes 相同”的回归；Anthropic 同样验证投影不改变后续 request。fixture 不包含真实 secret 或敏感 header。

### 7. 依赖和缓存边界通过结构保持

`domain` 只定义值对象，不导入 Provider、协议、I/O 或 TUI；具体 projector 位于 Provider 子包，依赖方向保持 `domain <- provider <- runtime`。共享消费者本变更不接入具体 wire，因此不新增跨层 import。

SemanticHistoryView 不进入 RequestCompiler 参数，也不参与 CachePlan/fingerprint。请求回归证明读投影无副作用；后续 token 估算只可读取视图并产生独立估算结果，不能将视图序列化回 prompt。

## Risks / Trade-offs

- [文本视图暂时无法表达 reasoning、phase 和 tools] → 只承诺当前 live TUI 已支持的文本语义；新增共享语义必须同时定义 live/replay 行为并扩展 spec/golden。
- [OpenAI native history 重构可能改变请求顺序或 opaque item 回放] → 用首轮、双轮、projection-before-request golden 与 fingerprint 回归锁定精确 wire bytes。
- [把 HistoryProjector 嵌入 Conversation 会修改内部公共接口] → 同一 change 一次性更新两家实现和所有 fake，借助编译失败发现遗漏；接口保持单方法。
- [字符串连接可能产生较大临时分配] → 当前 history 规模小且投影按需执行；先保证不可变快照和正确性，后续有 profiling 证据再优化，不缓存双份历史。
- [没有可见文本的成功 turn 在 UI 回放时可能成为空 cell] → 视图保留真实 turn 边界且不编造文本；未来 resume renderer 决定是否隐藏空 cell，不把展示策略放入 Provider。

## Migration Plan

1. 先增加 domain 语义类型和 Provider projector 接口，更新 fake 以恢复编译。
2. 实现 Anthropic 纯投影与 golden，验证既有 native history/request 不变。
3. 将 OpenAI history 收敛为 `nativeTurn`，先用 request golden/fingerprint 固定展平结果，再实现投影与 golden。
4. 增加两家 Conversation 的 committed-only、live/replay、一致性、隔离和并发回归。
5. 更新 Roadmap/README 中的 P1 实现状态，执行 OpenSpec 严格校验和 `make verify`。

当前没有持久化 Session 或外部稳定语义历史 API，因此无需数据迁移。若回滚，移除 projector 接口与 domain 类型，并将 OpenAI history 恢复为扁平 item slice；现有 Provider wire 和 TUI 数据无需迁移。
