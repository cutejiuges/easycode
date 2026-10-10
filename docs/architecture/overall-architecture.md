# EasyCode 总体架构设计

> 状态：持续演进
> 更新时间：2026-10-07
> 适用范围：EasyCode CLI、Agent Runtime、Provider、工具、扩展系统、会话存储和 TUI

## 1. 背景与目标

EasyCode 是一个本地优先的 coding agent。产品体验主要参考 Claude Code，同时保留 OpenAI/Codex 模式下的原生能力。

首要使用方式是由用户提供 `base_url`、`api_key` 和模型名称。项目不实现 Claude Code 或 Codex 的账号登录、OAuth、订阅校验等鉴权体系。

本设计的核心目标如下：

1. 同时完整支持 Anthropic Messages 和 OpenAI Responses 两种协议家族。
2. 保持 provider 原生语义，不能为了统一接口丢失 thinking signature、encrypted reasoning、message phase 或工具流式信息。
3. 将 prompt cache 命中率作为一级架构目标，保证请求前缀稳定、缓存失效可解释、可观测、可回归测试。
4. 产品交互尽量对齐 Claude Code，包括 REPL、流式输出、工具展示、权限确认、diff、session、skills、plugins、hooks 和 subagent。
5. 内核采用清晰的模块边界，避免上帝类、上帝包、循环依赖和 provider 逻辑泄漏。
6. 支持 headless、TUI 及未来 IDE/app-server 等不同宿主，而不复制 Agent 核心逻辑。

## 2. 参考实现与使用原则

### 2.1 Claude Code

参考目录：`../claude-code-sourcemap`

- 当前快照由 `@anthropic-ai/claude-code@2.1.88` sourcemap 还原，并非官方完整源码。
- 主要用于参考产品能力、交互行为、Anthropic Messages 处理、hooks/plugins/skills、session 和 TUI 体验。
- 不应假设还原后的文件组织、重复代码或内部实验功能都是应当照搬的架构。

### 2.2 Codex

参考目录：`../codex`

- 当前参考提交为 `c248f6d48b`。
- 主要用于参考 Rust workspace、Responses API、事件协议、工具执行、权限与 sandbox、session rollout、subagent control、Ratatui 和可观测性设计。
- Codex 当前只支持 Responses wire，不能直接作为 Anthropic provider 抽象。

### 2.3 取舍原则

- 产品体验优先参考 Claude Code。
- 内核分层、协议边界和安全执行优先吸收 Codex 的设计。
- 不复制任何一方的账号登录体系。
- 不为“代码统一”牺牲 provider 原生能力或缓存稳定性。
- 参考工程默认只读，EasyCode 不依赖其源码参与构建。

## 3. 架构原则

### 3.1 模板化生命周期，差异化 Provider Kernel

共享层负责 turn 生命周期、工具调度、权限、hooks、session 和 UI 事件；每个 provider 独立负责请求编译、流式归并、原生历史、推理数据、工具 wire 和缓存策略。

不使用“统一扁平 Message + 薄 Adapter”作为核心模型。薄 adapter 无法正确承载：

- Anthropic `thinking`、`signature`、`redacted_thinking`。
- OpenAI reasoning summary、raw reasoning、`encrypted_content`。
- OpenAI `commentary` / `final_answer` phase。
- Anthropic JSON tool input 与 OpenAI freeform/custom tool input 的流式差异。
- Anthropic 显式 cache breakpoint 与 OpenAI prompt cache/incremental response 的差异。

### 3.2 原生数据与共享语义双轨并存

- Provider-native item 用于精确续写、恢复、缓存和协议回放。
- Runtime semantic event 用于 TUI、headless 输出、日志和外部宿主。
- UI 不直接依赖 provider SDK 类型。
- 下一次模型请求不能通过 UI event 反向重建。

### 3.3 缓存优先

所有可能进入请求前缀的内容都必须具有稳定顺序、稳定序列化和明确失效规则。缓存不是网络层的附加功能，而是 ContextPlanner、ToolCatalog、SkillCatalog、PluginManager 和 Provider Kernel 的共同约束。

### 3.4 事件驱动，副作用集中

- Agent Runtime 通过显式 command 接收输入，通过 RuntimeEvent 输出状态。
- 文件、进程、网络、session、日志等副作用由边界模块执行。
- Context 规划、权限判断、事件归并和缓存指纹计算尽量保持纯函数。

### 3.5 依赖单向流动

底层 domain/protocol 不依赖 provider、TUI、存储或具体工具。上层可以组合下层，禁止反向引用和循环依赖。

### 3.6 安全默认值

- API key 默认从环境变量读取，日志必须脱敏。
- 项目级 hooks/plugins 首次启用需要信任确认。
- 有副作用工具必须经过权限策略和 sandbox capability。P3 只要求 capability flag 和当前开发平台的最小实现，其他平台可以安全降级并明确报告；完整跨平台 sandbox 放到 P8 发布强化。
- 网络流重试不得导致工具重复执行。

## 4. 总体架构

```text
                     CLI / app lifecycle
                              |
                       SessionService
                              |
               ChatSession / AgentLoop / TurnRuntime
                     /        |          \
                    /         |           \
       Anthropic Kernel   RuntimeEvent   OpenAI Kernel
       Messages/native        |          Responses/native
                              |
                    +---------+----------+
                    |                    |
              TUI Projection      Headless Projection
                    |                    |
            Bubble Tea views      text / JSONL v1

TurnRuntime -- durable records --> append-only Session JSONL
TurnRuntime -- future tool loop --> ToolScheduler / Policy / Executor
```

应用在取得启动工作目录后、打开 Session/Catalog/Provider 资源前，通过 `context/projectinstructions` 执行一次 descriptor-bound 发现。Loader 以最近 `.git` 为边界，从项目根到启动目录逐层选择 `AGENTS.md`，仅在同层主文件不存在时回退 `CLAUDE.md`，并生成只含项目根相对来源的有界不可变快照。应用默认模型可见预算为 32 KiB，构造器硬上限为 4 MiB；零值、负值和超限配置均在文件读取与预分配前拒绝。活动进程不热更新；resume/continue 使用新进程当前启动目录重新发现的快照，不读取 Session `creation_cwd` 中的旧规则。

Runtime 构造时深拷贝该快照，并在每轮把同一值分别交给 ContextPlanner 与 Provider RequestCompiler。两家 Provider 都在全部 committed native history 和当前真实用户输入之前生成一个临时 user context item/message；它不进入 `PreparedSample`、native history、`SemanticHistoryView`、RuntimeEvent 或 Session JSONL。

### 4.1 建议的 Go Module 结构

下列结构同时包含当前实现与目标边界。`tool`、`extension`、`subagent`、`telemetry` 目前只保留经批准的结构化 TODO 占位，分别等待 P3/P6/P7/P8 的 OpenSpec 提供真实消费者、验证与测试；不得从目录存在推断能力已经可用。

```text
cmd/
  easycode/                 可执行程序入口
internal/
  app/                      依赖装配和应用生命周期
  codec/                    无内部依赖的统一 JSON/canonical 编解码基础设施
  headless/                 prompt 解析、一次性/流式 JSONL v1 投影与 stream transport
  domain/                   核心 ID、值对象、错误和领域语义
  protocol/                 进程内 Command 与 RuntimeEvent
  runtime/                  session、turn loop、队列、取消和恢复协调
  provider/                 ProviderKernel 契约和能力模型
    transport/              Resty HTTP/SSE 公共基础设施
    anthropic/              Anthropic Messages 实现
    openai/                 OpenAI Responses 实现
  tool/                     P3 目标：ToolSpec、Executor、Policy、Scheduler
    builtin/                P3 目标：read/edit/write/grep/glob/bash/patch/plan 等
  context/                  上下文分层、token budget、compact、cache plan
  session/                  JSONL、artifact、resume/fork
    catalog/                可重建 SQLite Catalog 与前台 reconciliation
  extension/                P6 目标：hooks、skills、plugins、MCP
  subagent/                 P7 目标：thread tree、mailbox、后台任务和限流
  tui/                      Bubble Tea、输入、消息 view、overlay 和 diff
  telemetry/                P8 目标：slog、redaction、metrics 和 diagnostics
pkg/                        仅放需要对外稳定复用的 API，默认保持为空
```

实际初始化时可以合并尚未形成独立边界的小 package，但不得创建长期承载所有杂项的 `common`、`utils` 或超级 `core` 包。Go 的 `internal/` 用来强制实现边界；只有确认需要供外部程序复用的稳定 API 才能进入 `pkg/`。

### 4.2 依赖方向

```text
domain <- protocol
domain <- provider <- provider/anthropic
                   <- provider/openai
domain <- tool <- tool/builtin
domain <- context
domain <- session
domain <- extension
domain <- subagent

runtime  -> protocol + providers + tools + context + session + extensions + subagents
tui      -> protocol + ChatSession interface
headless -> domain + fault + protocol + ChatSession interface
app      -> runtime + tui + headless
cmd      -> app
```

上图是长期目标；当前 P2 运行路径为 `domain/protocol <- provider|context|session <- runtime <- app/tui/headless/cmd`。`context` 已通过纯内存 ContextPlanner 接入 Runtime，Provider 只向共享层暴露不含原生正文的 HistoryFootprint；Tool、extension、Subagent 和 telemetry 仍不可从 Runtime/app 到达。

禁止：

- provider 依赖 TUI。
- ToolExecutor 依赖 Bubble Tea model/view。
- session 存储反向调用 runtime。
- domain 引入 HTTP、数据库或终端库。
- 通过全局单例绕过依赖边界。

### 4.3 技术选型基线

首版采用单一 Go 技术栈，避免为了拆分 TUI/内核过早引入跨进程协议：

| 领域 | 首选方案 | 选择原因 |
|---|---|---|
| 语言与运行时 | Go 1.24+、goroutine、channel、context | 单二进制、并发模型直接、跨平台工具链成熟 |
| HTTP/SSE | `resty.dev/v3` + 内部 SSE frame parser | Resty 负责 HTTP 与 raw body，内部 parser 固定 frame 上限、chunk、取消和 idle 语义 |
| 序列化 | `github.com/bytedance/sonic` | 高性能 JSON，统一 request、wire、JSONL 和 canonical 编码入口 |
| Schema | 强类型 ToolSchema + 稳定 canonicalizer | 控制工具 schema 顺序和缓存字节，避免反射输出漂移 |
| CLI | 标准库 `flag` 起步，复杂子命令出现后再评估 Cobra | 首版减少依赖和空壳命令层 |
| TUI | `github.com/charmbracelet/bubbletea` | Elm 风格状态更新适合 RuntimeEvent projection 和可测试交互 |
| 数据库 | `github.com/ncruces/go-sqlite3 v0.32.0` | JSONL 事实源之上的本地索引；纯 Go、无 CGO，并由独立 adapter 固定安全打开与事务语义 |
| 日志 | 标准库 `log/slog` | 结构化字段、分组、handler 和敏感字段过滤 |
| ID | UUID v7 实现 | 全局唯一且大致按时间有序 |
| Secret | 自定义不可打印值对象 | 防止 String/GoString/日志意外泄漏 |
| 文件监听 | `fsnotify` | skills/plugins/hooks 热加载 |
| Golden/Snapshot | 标准库 testing + `testdata` golden files | 避免测试依赖过重，便于审阅 wire 差异 |

Resty v3 当前使用 `resty.dev/v3` 导入路径；在项目初始化时最新可用版本为 `v3.0.0-rc.4`，升级到稳定版前必须重新运行 provider/SSE 契约测试。Streaming 请求使用 Resty raw body，不使用 RC `SSESource` 作为工程契约；`internal/provider/transport` 自行处理 LF/CRLF、任意 chunk、UTF-8 边界、4 MiB 默认 event 上限、取消和 idle watchdog。Sonic 与 Resty 的自动 JSON 行为不得混用：只有无内部依赖的 `internal/codec` 可以直接导入 Sonic，并由其统一提供 map key 排序、字符串校验、严格解码与 canonical JSON；domain/protocol 可依赖该基础设施包，Provider 与 Session 通过它生成确定字节后再交给 Resty 或存储边界。

Sonic 在 amd64/arm64 之外会使用 fallback 实现。单二进制发布仍以兼容性矩阵和跨平台 golden/test 为准，不得假设所有架构都拥有相同的 SIMD 性能；P8 发布强化必须记录该差异及基准结果。

Provider HTTP 层不直接依赖官方模型 SDK。原因是 EasyCode 需要支持任意兼容 `base_url`、自定义 header、精确 SSE 状态机、opaque 字段保留和请求字节稳定性。若未来使用官方 SDK，只能将其封装在 provider 边界内，并通过同一套 golden/cache 测试证明行为等价。

首版 CLI、TUI 和 runtime 在同一进程内通过 channel/接口通信。只有 IDE、远程工作区或多客户端需求实际出现后，才引入 app-server/JSON-RPC，届时复用现有 protocol，而不是重新定义业务模型。

## 5. Provider Kernel

### 5.1 模板方法边界

共享 `TurnRuntime` 是模板化流程：

1. 接收用户输入或 continuation。
2. 执行 UserPromptSubmit 等前置 hook。
3. 构建上下文和 cache plan。
4. 由 Provider Kernel 编译原生请求。
5. 消费 provider stream，并归并为原生 item 与 RuntimeEvent。
6. 执行已完成的工具调用。
7. 将工具结果编码成 provider 原生输入。
8. 根据 stop/tool/end-turn 状态决定继续采样或结束。
9. 执行 stop hook，持久化 turn boundary 和 usage。

Provider Kernel 不是单个庞大 interface，而是以下策略的组合：

```text
ProviderKernel
  RequestCompiler
  StreamReducer
  NativeHistory
  ToolWireCodec
  ReasoningPolicy
  CachePlanner
  CompactionCodec
  UsageParser
  HistoryProjector
```

Go 中通过小接口与组合实现。Provider 持有不可变配置和 transport，并通过 `provider.Factory` 创建会话级 `provider.Conversation`；`runtime.Runtime` 只依赖 Conversation，避免多个 Session 共享可变 native history。运行时装配具体实现：

```text
anthropic.Provider
openai.Provider
```

不建议将原生 item 放进无结构的 `map[string]any` 后到处类型断言；应该使用 provider 包内的强类型结构，并只在明确的未知扩展字段上保留 `json.RawMessage` 等 opaque 数据。

### 5.2 Provider 能力模型

不能仅凭 `family = "openai"` 假设服务商具备所有能力。每个 provider profile 需要声明或探测：

- streaming
- reasoning summary/raw/encrypted
- thinking signature
- function tools
- custom/freeform tools
- parallel tool calls
- strict schema
- prompt cache control
- prompt cache key
- previous response incremental
- image/audio input
- server tools
- remote compaction
- usage/cached token reporting

默认能力按 wire 提供，用户配置可以覆盖。运行时遇到服务端不支持时要产生可诊断的 capability downgrade，而不是静默丢失数据。当前两个 Provider reducer 已把完成响应的原始 usage 保存在各自 native payload 中，并投影为共享的 `input_uncached`、`cache_read`、`cache_write`、`output`、`reasoning_output` 五项三态指标；缺失字段保持 unknown，Provider 不存在的独立概念才使用 not-applicable。

### 5.3 Wire 范围

首版完整支持，且 P1 实现顺序优先 OpenAI Responses：

- `family = "anthropic", wire = "messages"`
- `family = "openai", wire = "responses"`

首版不支持 `wire = "chat_completions"`。因此，只有 Chat Completions 的国产生态、本地模型和第三方兼容网关在 v1 中不可用；`base_url + api_key` 只表示连接方式，不代表 endpoint 支持任意 OpenAI wire。未来增加 Chat Completions 时必须拥有独立 RequestCompiler、StreamReducer、NativeHistory、ToolWireCodec、UsageParser、HistoryProjector 和 fixture，不得由 Responses adapter 隐式降级。详见 [ADR-0004](adr/0004-openai-responses-first-wire-scope.md)。

### 5.4 跨 Provider 恢复

- 相同 provider/wire：原生恢复。
- 同 family 切换模型：通过 capability 和 compaction compatibility 检查后恢复。
- Anthropic 与 OpenAI 互切：创建新 fork，将旧上下文压缩为中立摘要，再由新 provider 建立原生历史。
- 禁止伪造 Anthropic signature 或 OpenAI encrypted reasoning。

当前只实现第一项。模型兼容性检查与 compacted cross-provider fork 均属于 P4 计划，现阶段不得自动切换或把 opaque reasoning 转换到另一协议。

### 5.5 HistoryProjector 与语义视图

双轨历史需要一个明确的共享接缝，但不能退回统一消息模型。每个 Provider 实现自己的 `HistoryProjector`，将 native history 单向投影为只读的 `SemanticHistoryView`：

```text
Provider-native history
          |
   HistoryProjector
          |
  SemanticHistoryView
   |      |      |      |
 token  resume  hooks  subagent
 estimate render text   result
```

投影视图可以表达用户/assistant 文本、可展示的 reasoning summary、工具调用与结果摘要、phase 和完成状态；不得包含可用于伪造续写的 signature、encrypted content 或 Provider wire 请求字段。以下共享消费者必须使用它：

- token 预算和估算器；
- resume/history UI 回放；
- Stop hook 所需的最后 assistant 文本；
- Subagent completion envelope 的正文抽取。

RequestCompiler、NativeHistory、Session 事实源和 Provider resume 只能使用 native history，不能从语义视图反向构建请求。投影视图允许有损，按 Provider 各自实现并通过 golden fixture 保证 live stream 与 replay 的可见语义一致。详细决策见 [ADR-0002](adr/0002-provider-native-history-and-semantic-projection.md)。

### 5.6 Durable native commit 接缝

当前文本会话使用两阶段提交。Provider 在成功终态生成同时携带 opaque `NativeCommitEnvelope` 与 normalized `SampleUsage` 的 `PreparedSample`，不立即修改 committed native history。Runtime 重新验证两者，将 `[provider_native_commit(v1), sample_usage(v1), turn_completed(v1)]` 作为同一 JSONL batch 写入并执行 `Sync`，随后调用一次性 finalizer，最后才发布携带聚合 turn usage 的成功终态。持久化失败会 poison 当前 Runtime，未 finalize 的 staging 不得进入下一请求。

OpenAI payload v1 保存用户 Responses item、有序 output items 和原始 usage；Anthropic payload v1 保存 user/assistant message、最终 metadata 和原始 usage。两者都显式保留 known/unknown 状态，归一化结果由独立 `sample_usage` 保存。恢复时对应 Provider 先事务式解码全部 commits，任一错误都不返回部分历史。共享 Session、Runtime 和 TUI 不解析具体 Provider wire。

## 6. 缓存架构

### 6.1 目标

缓存设计需要同时优化正确性、命中率和可解释性：

1. 相同稳定上下文必须得到完全相同的序列化前缀和指纹。
2. turn 级动态状态只能影响尾部，不得污染稳定前缀。
3. 新增动态 MCP 工具时，不应无条件破坏所有内置工具的缓存前缀。
4. 缓存失效必须能够定位到具体 segment 和原因。
5. 缓存优化不得复用已经过期的权限、工具或 instruction。

### 6.2 上下文分段

当前已实现不可变、强类型且可验证的 `CachePlan`、stable-prefix fingerprint 和最小 ContextPlanner。当前计划固定输出 `provider_profile`、可选 `project_instructions`、`committed_history`、`current_input`；项目快照非空时以 `replace + project-stable` 进入稳定前缀，空快照保持既有三段 canonical bytes 不变，history 为 turn-stable，当前输入为 volatile。下列其余分段仍是 P3/P4/P6 随真实消费者逐项扩展的目标，不能从目录或设计推断为已实现：

```text
CachePlan
  Segment 0: provider/model base instructions       stable
  Segment 1: built-in tool schemas                  stable
  Segment 2: project instructions                   project-stable
  Segment 3: skill/plugin metadata catalog          session-stable
  Segment 4: native conversation history            append-only
  Segment 5: active skill bodies / restored context turn-stable
  Segment 6: cwd/git/env/world-state diff            volatile
  Segment 7: current user input                      volatile
```

每个 segment 至少包含：

```text
segment_id
stability_class
canonical_bytes_hash
source_revision
invalidation_reason
provider_cache_policy
```

### 6.3 稳定序列化

- 工具、skills、plugins、MCP servers 必须按明确的稳定键排序。
- 禁止依赖 HashMap 的非确定迭代顺序。
- JSON schema 使用 canonical serialization；可选字段和默认值的输出规则固定。
- 不把时间戳、随机 ID、绝对临时路径、实时 git 状态写入稳定前缀。
- 工具描述、system prompt 和 catalog 的变更必须显式提高 revision 或改变 fingerprint。
- Provider request golden test 应覆盖最终 wire JSON，而不仅是中间领域对象。

当前 ContextPlanner 的 `provider_profile` 只包含 family、model 和固定 schema revision，不包含 API key、base URL、预算、cwd、Session identity 或其他动态状态。`project_instructions` 使用结构化快照的内容派生 revision 和 canonical bytes，不包含绝对项目根、mtime、inode 或 Session identity；正文或项目根相对来源变化才改变该 segment。`committed_history` 的 source revision 来自 Provider committed native revision，而不是语义 turn 数；Provider footprint 与语义历史估算是包含关系，使用 `max(semantic_visible, provider_native)` 合并，禁止相加造成可见文本重复计数。

token 估算当前使用版本化本地 `byte_heuristic_v1`，明确标记 `estimated` 或 `unknown`。显式 JSON 配置可以提供 context window、输出预留和安全余量；未配置窗口或估算 unknown 时不执行硬拒绝，也不根据模型名猜测窗口。明确超限只在 durable `turn_started` 后、Provider stream 前以既有 `turn_failed` 收口，计划本身不写 Session、不新增 RuntimeEvent。

### 6.4 Anthropic 缓存策略

Anthropic CachePlanner 负责：

- 在 system、tool schema、message content 上放置合法的 `cache_control`。
- 根据服务能力选择 ephemeral/TTL，且同一 session 内保持策略稳定。
- 内置工具形成连续稳定前缀，动态 MCP/插件工具进入独立后缀。
- 保证 thinking/tool result/content block 的原始顺序不因缓存处理改变。
- 记录 cache creation/read input token，并映射到统一 usage metrics。

### 6.5 OpenAI 缓存策略

OpenAI CachePlanner 负责：

- 生成 session/thread 稳定的 `prompt_cache_key`。
- 保持 instructions、tools 和 input prefix 的精确稳定。
- 在 provider 支持且请求连续时维护 `previous_response_id`。
- capability 不支持增量请求时回退到完整 history，不影响正确性。
- 保留 reasoning encrypted content 和原始 item 顺序。

### 6.6 缓存失效矩阵

| 变更 | 应失效范围 |
|---|---|
| base instructions 或模型能力变化 | 从 Segment 0 开始 |
| 内置工具 schema/描述变化 | 从 Segment 1 开始 |
| 项目指令文件变化 | 从 Segment 2 开始 |
| plugin/skill catalog 变化 | 从 Segment 3 开始 |
| 新增对话 item | 只追加 Segment 4 |
| 激活 skill 正文 | Segment 5 及之后 |
| cwd/git/status 变化 | Segment 6 及之后 |
| UI 主题、窗口尺寸、spinner | 不得影响请求缓存 |
| 日志级别变化 | 不得影响请求缓存 |

### 6.7 缓存可观测性

每次模型请求记录以下非敏感诊断字段：

- provider、wire、model。
- cache plan version。
- 各 segment fingerprint 的短摘要，不记录正文。
- 与上次请求相比首个变化 segment。
- provider 原始 usage 摘要和归一化后的 `input_uncached/cache_read/cache_write` token。
- cacheable prefix token 估算。
- previous response 是否复用及回退原因。

核心指标：

```text
normalized_input_total = input_uncached + cache_read + cache_write
cache_read_ratio       = cache_read / normalized_input_total
cache_write_ratio      = cache_write / normalized_input_total
stable_prefix_reuse    = repeated stable fingerprint requests / eligible requests
prefix_churn_by_source = invalidations grouped by segment/source
```

Anthropic 的 `input_tokens` 表示未命中缓存的输入，`cache_read_input_tokens` 与 `cache_creation_input_tokens` 是额外字段；OpenAI 的总 input 包含 cached input 子集。因此不能直接用两家原始字段做同一个分母。当前 normalized usage 已按 Provider 语义保存五项指标：Anthropic 直接映射三类输入，OpenAI 仅在 total 与 cached 同时 known 时相减得到 uncached，并拒绝 cached 大于 total。字段状态区分 known、unknown 和 not-applicable，`known:0` 不等于缺失。缓存比例、成本和 telemetry 仍属后续能力；实现时只能读取 normalized usage，分母未知或为零时 ratio 保持 unknown。

### 6.8 缓存测试门槛

- 同一输入重复编译，canonical request 和稳定 segment fingerprint 必须完全一致。
- 只修改 cwd/git 状态时，Segment 0-5 fingerprint 不变。
- 只增加动态 MCP 工具时，内置工具 segment fingerprint 不变。
- 插件发现顺序、文件系统枚举顺序变化不能改变最终 catalog 顺序。
- Anthropic cache marker 数量和位置有 golden test。
- OpenAI prompt cache key、item 顺序和 incremental fallback 有 golden test。
- 每次修改 system prompt、tool schema、skill/plugin catalog 或 context planner，必须运行 cache regression suite。

## 7. RuntimeEvent 与流式归并

### 7.1 RuntimeEvent 当前范围

```text
TurnStarted
AssistantTextDelta { text }
TurnCompleted { usage }
TurnFailed
```

当前进程内 RuntimeEvent 只声明已有真实 producer、consumer 和 validator 的四种文本事件，并携带匹配的 `session_id/thread_id/turn_id`。`turn_completed(v1)` 必须携带五项三态 turn usage；当前单 sample turn 与 durable `sample_usage` 相同。独立 usage 更新、reasoning、tool、patch 和 compaction kind 尚未实现；未来必须随 producer、consumer、typed payload 和测试在同一变更中加入，不能提前保留空枚举。

### 7.2 Headless JSONL v1

`internal/headless` 与 TUI 平级。一次性模式只依赖最小 `ChatSession` 接口；流式模式只依赖最小 `StreamControlLoop` 接口。两者都不会直接序列化 RuntimeEvent，而是严格校验 version、身份、顺序和 typed payload 后，单向投影为各自 sealed wire DTO。一次性 v1 保持 `thread.started`、`turn.started`、`assistant.text.delta`、`turn.completed`、`turn.failed` 与 stream-level `error`；`--json --input-format stream-json` 另行增加 `control.response`、携带 `input_id` 的 turn 事件和 `input.discarded`。外部 `turn.completed` v1 的 `usage` 为 required，使用 headless 自有强类型 DTO，不泄露 Provider raw 字段。stdout 由单 writer 顺序写入，每行一个完整 JSON object；日志和人类诊断不得进入 JSON stdout。

`--print` 在内存中聚合文本，仅在 Runtime 已 durable 发布 `turn_completed` 且事件流正常闭合后输出最终值。`--json` 可实时输出 delta，但同样不从 channel close 或已见文本推断成功。取消、短写和断管会触发幂等 Interrupt 并 drain Runtime；已 durable 完成后的输出失败只改变进程交付状态，不回滚 Session。

一次性 headless prompt 在应用装配前解析：无位置参数或显式 `-` 时读取 stdin；显式 prompt 与非终端 stdin 同时存在时使用稳定 `<stdin>` 边界追加；组合内容必须是合法 UTF-8、非空且不超过 4 MiB。流式模式只由 `--json --input-format stream-json` 显式启用，要求可关闭的非终端 stdin/stdout transport，以不超过 32 MiB 的单行 NDJSON 接收 `input.submit`、定向 `turn.interrupt` 和 `session.shutdown`。`--continue` 会在当前 cwd、Provider family/wire 和 model 完全匹配时恢复最近 root Session，并与 `--resume` 互斥。当前仍不支持独立 usage 更新或 reasoning/tool 事件。

### 7.3 AgentLoop 输入控制

`runtime.AgentLoop` 位于单 turn `Runtime.RunTurn` 之上，是 Session 实例级的唯一 admission owner。空闲 submit 得到 `starting`，活动期间的 submit 进入同时受条数和 UTF-8 字节约束的 FIFO，并得到 `queued`；每条输入仍独立形成一个 durable turn，绝不拼接、并行或注入当前 Provider stream。command result 只确认进程内 obligation，只有关联的 `turn.started` 表示 `turn_started(v1)` 已成功 `Sync`。外部 request identity、queue 状态与 control metadata 都不写 Session，也不进入 Provider request/cache fingerprint。

queue 是有界、非 durable 的进程内状态。stdin EOF 只停止新 admission，并按 FIFO 排空已经接受的输入；显式 shutdown 则停止 admission、取消活动 turn、为所有尚未开始的输入发布 `session_shutdown` discard，并等待 Runtime durable terminal 与 worker cleanup。interrupt 必须携带当前活动 `turn_id`，过期目标不得误取消下一 turn。普通 Provider failure 后 loop 可以继续；journal poisoned 或 durable 结果不确定时停止所有后续 Provider 副作用。

本能力是跨 turn 的 follow-up control loop，不是 same-turn steer。当前 Read Tool Loop 已允许一个 turn 内发生多个顺序 Provider sample，并在 durable tool output 后形成明确边界；但该边界尚未接入排队用户输入，真正的 same-turn steer 仍需独立变更定义 admission、缓存与历史语义。TUI 继续使用 `ChatSession`，待需要 queue 交互时再迁移到同一控制器并删除重复 facade。

### 7.4 Provider StreamReducer

Anthropic reducer 负责处理：

- `message_start/delta/stop`
- `content_block_start/delta/stop`
- text/thinking/signature/input_json delta
- stop reason、usage、异常 block 顺序和中断恢复

OpenAI reducer 负责处理 `response.created`、文本 delta、function call item/arguments delta/done、完成 output item，以及 identity 匹配的 completed/failed/incomplete；Anthropic reducer相应归并 `tool_use` 与 `input_json_delta`。两家都只在参数完整、strict decode 和调用身份一致后产出有序 typed ready calls。completed usage 会在活动 response identity 校验后严格解析并保留 raw/normalized 两层表示。每条 stream 使用独立状态机，要求起始事件恰好一次、所有支持事件位于 active 状态且 terminal 唯一。reasoning 的宿主事件投影和未交付的其他工具类型仍是后续目标。

共享 `provider.StreamEvent` 使用私有字段和 semantic、native item、completed、failed、cancelled 五类 typed constructor，零值、冲突 payload、nil terminal error、非法 native item 与失效 prepared sample 均不能进入正常路径。Provider 生产者是输出 channel 的唯一关闭者，且必须在 transport 排空、Conversation active 状态释放后关闭 channel；channel close 是生产 goroutine 的清理完成信号，不代表业务成功。

Runtime 为每次 Provider stream 创建派生 context，逐项验证事件并记录唯一 terminal，但仍持续读取到 channel 关闭。非法事件、重复 terminal 或 terminal 后事件会保留首个 protocol error、立即取消派生 context、停止向宿主转发并由同一 owner 同步排空；只有生产者退出后才 durable 收口失败或允许 AgentLoop 启动下一 turn。应用 shutdown 超时继续由既有 Provider transport 强制关闭路径解除阻塞，不创建失去 owner 的后台 waiter。

- response/item created/added/done/completed
- output text delta
- reasoning summary/raw delta 和 section break
- function/custom tool input delta
- message phase、usage、end_turn 和 server response id

只在完整 tool call 已得到可靠参数后执行副作用。可以在整个模型流尚未结束时启动已经完成的工具 block，但不能根据不完整参数提前执行。

### 7.5 背压与渲染节流

- 原始网络 delta 可以高频进入 reducer。
- TUI projection 按 16-33ms 合并纯文本刷新，降低闪烁和 CPU 占用。
- 工具边界、权限请求、错误和 item 完成事件不得被合并丢失。
- 高频 delta 不进入 session writer；成功终态和副作用边界必须等待 durable batch，不能为降低延迟而越过 `Sync` 确认。

## 8. Tool 系统

完整设计、参考工程取舍、恢复语义和分 change 推进顺序见 [Tool 系统架构设计](tool-system.md)。本章只保留总体架构必须遵守的边界。

本章描述 P3 目标架构。当前 `internal/tool` 与 `internal/tool/builtin` 仅是经批准的 TODO 占位，没有可执行 schema、Provider wire、权限策略、执行器或 Runtime/app 消费者；下列内容不表示能力已经实现。

### 8.1 分层

```text
ToolCapability    共享能力标识，例如 fs.read、fs.patch、shell.exec
ToolFacade        provider/model 可见的名称、描述和 schema
CatalogSnapshot   单次 sampling 使用的不可变、有序 schema 与路由快照
InputDecoder      归并 provider-native delta，完整后产生 typed input
ToolExecutor      实际执行，不感知 UI 和 provider
ToolPolicy        权限、sandbox、路径和网络规则
ToolScheduler     并发、独占、取消和有序结果
ToolPresenter     TUI 对 RuntimeEvent/typed metadata 的展示
ToolResultCodec   编码为 Anthropic/OpenAI 原生结果
```

禁止把 schema、执行、权限、终端渲染和 provider result mapping 全部塞入一个 Tool 类。

### 8.2 Provider 差异工具

同一能力允许有不同 facade。例如：

```text
fs.patch
  Anthropic: Edit / structured JSON
  OpenAI:    apply_patch / custom freeform
  Executor:  shared PatchExecutor
```

流式参数由 `ToolInputStreamDecoder` 处理：

- `AnthropicJsonEditDecoder`
- `OpenAiFreeformPatchDecoder`

两者都可以产生共享 `PatchDraftUpdated`，但不得共享错误的解析假设。

只有参数完整、strict decode 和语义验证全部通过后才能形成 `ToolCallReady`。参数 delta 和 diff draft 只用于展示，不得产生副作用。

### 8.3 Durable 边界、调度与幂等

- 当前 Provider sample 的 native commit、usage 和 ready calls 必须先作为一个 durable batch 成功 `Sync`，之后才能开始副作用。
- 并发安全的只读工具可并行。
- 非并发安全或写工具获得独占门。
- 结果按模型调用顺序提交，避免破坏 tool call/result pairing。
- 每次调用以唯一 invocation ID 建立 execution ledger，并保留 Provider call ID 用于原生配对。
- ledger 保证 EasyCode 自动执行至多一次，不宣称任意外部副作用 exactly-once；已 durable 开始但结果未知的调用恢复为 `outcome_uncertain`，默认不得自动重试。
- 每个 call 最终都必须得到成功、拒绝、取消、失败或恢复补偿 output，不能留下悬空 Provider 配对。
- 取消必须以 durable `execution_started` append是否被接受为线性化点：接受前直接记录cancelled且零工具I/O，接受后恰好调用一次executor并传入取消context。
- resume只做本地配对补偿：ready关闭为 `session_interrupted_before_execution`，started-without-result关闭为 `outcome_uncertain`，已有result复用持久化preview；恢复阶段禁止executor和Provider网络调用，并以唯一 `turn_failed`结束旧turn。

### 8.4 权限与结果

- Approval 只表达用户意图，SandboxPlan 负责实际强制边界；批准不能自动放开 workspace、网络、环境变量或平台不支持能力。
- 文件 containment、类型、权限和 symlink 判断必须绑定实际打开的 handle，不能依赖字符串前缀或 `Lstat` 后再次按路径打开。
- 完整结果、模型预览、artifact 和 UI 摘要是四种表示；模型实际收到的预览字节必须持久化，resume 时不得按新配置重新截断。
- Presenter 只消费 RuntimeEvent 与受控 metadata，Executor 不依赖 Provider wire、Session 或 TUI。

### 8.5 能力分期

P3 当前已实现 Read、Glob、Grep、双 Provider Tool Loop、durable ledger、最多 8 路只读并发、有序结果提交和本地恢复补偿。Edit/apply_patch、Write、exec/write_stdin、approval、artifact 和跨平台 sandbox 仍按独立 change 逐项增加，不得由现有只读能力推断为已完成。

Skill 与 hooks/plugins 属于 P6；MCP 和 Agent/Subagent 属于 P7；完整跨平台 sandbox 矩阵属于 P8。未到对应阶段前不得提前暴露空 schema、event kind、facade 或 capability flag。

## 9. Context 与 Compaction

本章同时描述当前 P2 基线和 P4 目标。当前已实现包含 Tool Catalog 的确定性 ContextPlanner、可选项目指令、本地 token estimate、Provider-private HistoryFootprint、可选显式 token budget 和请求前超限守卫；动态扩展/skill 来源、真实 Provider cache policy、compaction 以及跨 Provider fork 尚未实现。

### 9.1 上下文来源

当前已接入 Runtime 的来源只有：

- `provider_profile`：family、model、schema revision，`replace + stable`。
- `project_instructions`：启动时发现的有界结构化快照，存在文档时为 `replace + project-stable`，空快照省略。
- `committed_history`：`SemanticHistoryView` 与不含 opaque 正文的 Provider footprint，`append + turn-stable`。
- `current_input`：本轮用户文本，`replace + volatile`。

后续阶段将在存在真实消费者、验证和测试时逐项增加以下来源：

- base/developer instructions
- provider/model/collaboration mode
- tool schemas
- plugin/skill catalog
- 已激活 skill 正文
- 原生 conversation history
- retained context
- cwd、workspace roots、git 和权限 world state
- 当前用户输入与排队消息

ContextPlanner 必须明确每一项的来源、优先级、稳定性、token 估算和生命周期；来源生命周期与 cache stability 是两个正交强类型概念，不能复用一个字符串表达。

### 9.2 历史不变量

- tool call 必须有对应 output。
- orphan output 在发送前被拒绝或修复，并留下诊断。
- 不能丢失 thinking signature、encrypted reasoning 或 provider item 顺序。
- 大工具结果在进入历史前截断，并将完整内容存入 artifact。
- 图像、音频和其他 modality 按模型能力过滤。

### 9.3 Compaction

- 支持手动和阈值自动 compact。
- compact 前后触发 hooks。
- summary 替换模型窗口，但原始 JSONL 保留。
- 重新注入项目指令、活动 skill、计划、关键文件和必要 retained context。
- CompactionCodec 可以 provider-specific；summary 的领域含义共享。
- 对模型或 provider 切换，使用 compaction compatibility 判断是否需要重建窗口。

## 10. Session 与存储

### 10.0 术语

- `session`：一个逻辑会话和持久化命名空间，拥有一棵线程树，不等同于单条消息历史。
- `root thread`：Session 创建时的根线程，承载默认用户交互历史。
- `thread`：Session 内一条线性的 turn/native history；Subagent、fork 和 compaction 可以创建 child thread。
- `turn`：Thread 内一次用户输入到模型停止/工具循环结束的完整生命周期。

`session_id` 用于定位逻辑会话，`thread_id` 用于定位具体执行链；EventEnvelope 同时携带二者，不能只用其中一个替代另一个。线程树决策见 [ADR-0003](adr/0003-session-thread-tree.md)。

### 10.1 目录

```text
~/.easycode/
  config.toml
  sessions/YYYY/MM/DD/<thread-id>.jsonl
  state.sqlite
  logs/easycode.log.*
  artifacts/<session-id>/<tool-call-id>/...
  skills/
  plugins/
```

### 10.2 JSONL 事实源

```text
EventEnvelope
  schema_version
  payload_version
  seq
  timestamp
  session_id
  thread_id
  parent_thread_id
  turn_id
  event_kind
  batch_id
  batch_index
  batch_size
  payload
  checksum
```

当前必需记录包括：

- `session_meta` 与 `thread_meta`；
- `turn_started`；
- `provider_native_commit`、`sample_usage` 与 `turn_completed`；
- `tool_call_ready`、`tool_execution_started` 与 `tool_call_result`；
- `turn_failed`。

`schema_version` 与 `payload_version` 是单一当前 canary，decoder 在 canary 校验后只按 `event_kind` 选择唯一 strict decoder。所有当前记录都参与恢复；未知 kind、未知字段、旧/新 canary 或损坏必须在 repair、Provider 恢复和 append 前失败关闭，不存在 optional record 或多版本 decoder registry。

Provider native commit 只记录 Provider 已验证的原生增量和 raw usage；紧邻的 `sample_usage` 记录共享五项三态指标。无工具的成功 sample 仍以 native commit、usage 与 terminal 原子提交；带调用的 sample 则把有序 ready facts 放入同一 durable batch，随后单独提交 started、result 和 Provider 原生 tool outputs。缺失 usage、错序、遗漏配对或重复调用身份一律 fail closed。permission、hook、subagent、cache 与 compaction checkpoint 仍需按各自恢复语义增加版本化记录；不能把 RuntimeEvent 或 UI transcript 当作 native history。

当前 Session envelope/payload 使用唯一 canary，进程内 RuntimeEvent 不携带固定版本；headless JSONL v1 是独立外部 wire。稳定发布前的契约补全直接替换唯一 current fixture，不保留双 reader；只有契约已冻结、变化无法加法表达且已发布数据或客户端必须并存时，才引入新 revision，并同时定义兼容窗口与退出条件。

Headless JSONL v1 是 stdout 外部协议，不是 Session record：不得写入 journal，也不得在 resume 时回放。headless resume 只复用连续 lease 下恢复出的 Provider-native history，并只发布本次新 turn；`SemanticHistoryView` 仍只供 TUI 等只读消费者使用。

默认不持久化每个文本 delta、spinner 或窗口状态；在 item 完成时持久化最终值。需要崩溃恢复时，可以增加有节制的 checkpoint，而不是把所有 UI delta 写入日志。

### 10.3 SQLite projection

`internal/session/catalog` 是 JSONL 事实源之上的可重建投影边界。当前 schema v1 只包含 `threads` 表及 continue 组合索引，保存 root thread 的 session/thread identity、journal 相对定位、创建 cwd、Provider family/wire、model、最后 committed record 的 timestamp/seq/checksum；不保存 prompt、response、native payload、title、tag、worktree 或全文搜索内容。

Catalog 只在 `--continue` 路径按以下顺序工作：

```text
Open Catalog lock
  -> descriptor-safe enumerate journals
  -> 对每个候选依次取得一个 Repository lease
  -> Loader / ReplayPlanner / Projector
  -> 释放该 lease
  -> 单次短 SQLite transaction 做 upsert/delete
  -> latest-compatible 精确选择
  -> Close Catalog
  -> 使用选中的 thread ID 进入既有 sessionService.resume
```

全部 journal 文件 I/O 在 SQLite transaction 外完成；协调过程不启动后台 goroutine，也不会同时持有多个 journal lease。busy 的已索引行保持原值，未索引 busy thread 会阻止自动选择，避免静默跳到更旧会话。真正缺失或无效 journal 的旧行会删除；可修复尾部只能在 exclusive lease 下 truncate 并 `Sync`。

`state.sqlite.lock` 为 schema、重建和 reconciliation 提供跨进程唯一 owner。Unix adapter 从实际打开的 handle 校验私有 data home、数据库、rollback journal 与 sidecar 的普通文件类型和 `0600` 权限，并使用 no-follow 打开；无法提供等价语义的平台失败关闭。数据库缺失、损坏或 schema 不兼容时，在同一 Catalog 锁下从 JSONL 构建临时数据库，`Sync` 后原子替换。

选择结果不是恢复事实：Catalog 关闭后，app 仍通过既有 `sessionService.resume` 重新取得 journal lease并完整执行 load、repair、ReplayPlanner、Provider-native restore 与兼容性校验。选择和恢复之间发生占用、替换、损坏或配置不兼容时直接失败，不回退旧 thread。session picker、worktree/project catalog、title/tag、全文搜索、实时索引和 thread graph 仍属后续能力。

### 10.4 写入与权限

- JSONL append-only；每个活动 thread 通过绑定 journal handle 的跨进程 exclusive lease 保证只有一个 writer。新建 metadata 或对既有 journal 执行 load/repair 前必须先取得 lease，并连续保持到 writer 完成最终 `Sync` 和关闭 handle；每个 durable batch 连续编号。
- macOS/Linux 从已打开数据根使用 no-follow、descriptor-relative walker 逐级创建/打开日期目录与 journal，并从实际 handle 校验类型和权限；lease、Loader、repair 与 writer transfer 复用最终 journal handle。Windows 等尚未提供等价语义的平台当前明确失败关闭，跨平台实现与真实平台验证留在 P8。
- 文件使用用户私有权限，Unix 下目标为文件 `0600`、目录 `0700`。
- Loader 严格校验 canonical UUIDv7、UTC 时间、checksum、ID 归属、序号和 batch 完整性；只允许截去 EOF 尾部半行或未完成尾批次，不允许跳过中段损坏。
- ReplayPlanner 只重放完整 batch。普通文本turn尾部直接追加 `turn_failed(code=session_interrupted)`；工具turn先按ledger在本地补齐cancelled/outcome-uncertain result与缺失Provider output，再追加唯一失败终态。两类恢复都不自动发起Provider请求。
- 当前 `--resume <thread-id>` 通过日期编码的 UUIDv7 定位文件，恢复 native history 和只读语义视图；不恢复旧 API key、base URL、cwd 或其他动态 world state。
- 仓库已建立不可变的当前文本与 Tool Loop fixture，用于验证 Loader/ReplayPlanner 的读取、续写 prefix 不变、账本顺序和双 Provider 恢复等价性；项目没有线上历史格式，因此开发期旧 fixture 与旧 reader 已删除。未来真实 schema/payload revision 的版本专属转换仍未实现，也不得在 resume 时原地改写既有 records。
- exclusive lease 是协作进程间的 advisory lock，不能阻止旧版 EasyCode 或非协作进程绕过锁直接写文件；混合版本运行前必须确保目标 thread 没有其他 owner，绕过锁产生的损坏继续由 checksum、seq 和完整回放校验发现。
- SQLite Catalog v1 与 `--continue` 已实现；session picker、worktree/project catalog、title/tag、搜索、实时索引、artifact 和未来 schema 转换尚未实现。
- 未来 compaction/fork 只能追加 checkpoint/cursor 和 child-thread 元数据，原始 JSONL 继续保留；不得重写、截短或把 summary 伪装成原生历史。该能力目前仅有约束，尚未实现。
- artifact 文件路径必须防止目录穿越。

## 11. Subagent

本章描述 P7 目标架构。当前仅保留经批准的 TODO 控制边界，没有生产消费者、thread tree、调度或 completion envelope。

### 11.1 模型

- 一个 root session 包含 thread tree。
- 每个 subagent 拥有独立 thread、native history、取消 token 和 session JSONL。
- parent/child 共享 AgentControl、并发额度和总预算。
- 支持 `none`、`full`、`last_n_turns` 三种上下文 fork。

### 11.2 V1 范围

- 前台同步 Agent。
- 后台 Agent 和完成通知。
- model/provider/effort 继承或显式覆盖。
- tool allowlist、permission mode、max turns。
- list、wait、interrupt、follow-up。

团队 mailbox、worktree isolation 和 swarm coordination 放到后续阶段，但 thread/session 模型需要预留扩展空间。

## 12. Hooks、Skills、Plugins 与 MCP

本章描述 P6 目标架构。当前相关 package 仅保留经批准的 TODO 边界，未被 Runtime/app 导入，不执行 Hook、不发现或加载 Skill/Plugin，也不启动 MCP server。

### 12.1 Hooks

首批兼容事件：

- PreToolUse / PostToolUse / PostToolUseFailure
- PermissionRequest
- UserPromptSubmit
- SessionStart / SessionEnd / Stop
- SubagentStart / SubagentStop
- PreCompact / PostCompact

支持 command hook、matcher、timeout、同步/异步、block/allow、输入改写、additional context 和 result rewrite。项目/plugin hook 启用前必须进行 trust review。

### 12.2 Skills

- 支持 system/user/project/plugin 多层发现。
- prompt 只注入稳定排序的 metadata catalog。
- 显式调用或模型决定使用后，再完整加载 `SKILL.md`。
- 支持 `$skill`、Skill tool、path activation 和隐式资源访问检测。
- 兼容 allowed-tools、model、hooks、context=fork、agent 等常用 frontmatter。
- 资源读取绑定 source authority，不能从一个 provider 枚举、再绕过它从其他通道读取。

### 12.3 Plugins

内部使用统一 `PluginContribution`：

```text
skills
agents
hooks
mcp_servers
commands
output_styles
lsp_servers
apps/channels
```

外部提供：

- EasyCode native manifest loader。
- Claude plugin importer。
- Codex plugin importer。

manifest adapter 可以统一扩展描述，但不能用于统一 provider 对话协议。V1 优先本地插件，远程 marketplace 和安装缓存后置。

### 12.4 MCP

- MCP tool 进入 ToolCatalog，但放置在稳定内置工具前缀之后。
- MCP server 生命周期、认证、工具变更和错误必须独立可观测。
- server/tool 名称冲突有确定性解析规则。
- MCP tool result 同样执行大小限制、权限和 session 持久化。

## 13. TUI 与 REPL

### 13.1 技术方案

采用 Bubble Tea。TUI 的 Model/Update/View 只消费 RuntimeEvent 和 SessionService command，不直接调用 provider 或工具 executor；耗时 I/O 通过 `tea.Cmd` 转换为消息返回更新循环。历史回放消费 `HistoryProjector` 生成的语义视图，不直接解析 Provider-native item。

### 13.2 主要视图

- 用户、assistant、commentary/final message。
- thinking/reasoning summary 折叠块。
- tool use、tool result、并行工具组。
- Bash 实时输出和后台任务。
- Edit/apply_patch 渐进 diff。
- permission、AskUser、MCP elicitation overlay。
- compact boundary、usage 和 cache 状态。
- subagent/task 状态。
- resume/fork/session picker。

### 13.3 REPL 行为

- 多行输入、paste 保护、历史和补全。
- Ctrl+C/Esc 中断当前 turn，不能误杀整个进程。
- 支持 `/help`、`/clear`、`/compact`、`/resume`、`/fork`、`/model`、`/provider`、`/permissions`、`/skills`、`/plugins`、`/hooks`、`/agents`、`/cost`、`/doctor`。
- 同一 runtime 同时支持 TUI、`--print` 和稳定 JSON event stream。

## 14. 配置与密钥

当前首个可用配置格式为用户级 JSON，默认路径为 `~/.config/easycode/config.json`，也可通过 `--config` 指定其他路径：

```json
{
  "provider": "openai",
  "base_url": "https://example.com/v1",
  "api_key": "...",
  "model": "..."
}
```

- JSON 提供基础值，非空 `EASYCODE_*` 环境变量逐字段覆盖；默认文件不存在时允许纯环境变量启动。
- 包含明文 API key 的 Unix 配置文件必须使用用户私有权限，例如 `0600`；读取后立即包装为 secret type，并在 String/GoString/slog/JSON diagnostic 中脱敏。
- macOS/Linux 配置文件使用 no-follow opener，并从同一实际 handle 校验普通文件类型、大小和权限后读取；Windows 等未实现等价 secure opener 的平台当前明确失败关闭，不回退到 `Lstat` 后再 `Open`。
- 配置文件路径、JSON 正文和 key 不进入 Provider 请求错误或 TUI snapshot；显式配置文件缺失时不得静默回退。
- base URL、额外 header 和 query 参数需要校验，错误信息不能回显 secret。
- 不实现账号登录、设备码、OAuth 或订阅系统。

## 15. 日志与可观测性

必须区分：

1. Session transcript：可恢复的业务事实。
2. Diagnostic log：调试 runtime/provider/tool 问题。
3. Telemetry：可选 metrics/traces，不参与恢复。

P8 目标是使用 `log/slog` 建立结构化日志上下文，并在需要跨进程 trace 时从 telemetry 边界接入 OpenTelemetry。当前 telemetry logger 仅为经批准的 TODO 占位，尚未接入生产运行路径：

```text
session -> turn -> sampling attempt -> provider request
                         -> tool call -> permission -> execution
                         -> hook run
                         -> compact
```

日志要求：

- Authorization、API key、cookie、敏感 header 永不输出。
- 请求正文默认不记录，仅在显式安全诊断模式写入脱敏副本。
- tool output 和文件内容按大小限制与敏感规则处理。
- 日志轮转，不能产生无限增长的单文件。

## 16. 测试策略

### 16.1 单元测试

- StreamReducer 状态转换。
- request canonicalization 和 cache fingerprint。
- context ordering、token budget、tool pairing。
- permission policy、path validation、result truncation。
- plugin/skill/hook parser。

### 16.2 Golden/契约测试

- Anthropic 和 OpenAI request JSON。
- SSE 任意 chunk 边界拆分。
- thinking signature/encrypted reasoning 的无损 round-trip。
- tool call/result 顺序。
- cache marker/key 和 prefix fingerprint。
- session schema 与 migration。

### 16.3 集成测试

- mock provider 下完整 turn -> tool -> continuation。
- stream 断开、重试、取消、超时。
- 权限拒绝不产生副作用。
- JSONL 写入、崩溃尾部修复、resume/fork/compact。
- hooks、skills、plugins、MCP 生命周期。
- subagent 并发、限制和取消传播。

以上条目按对应 Roadmap 阶段启用。当前已落地双 Provider 文本 stream、Session/restore/headless、可重建 SQLite Catalog 与 `--continue`、descriptor-bound 配置与 journal 安全、迁移 fixture，以及架构静态门禁；不得把未来条目视作现有测试覆盖。

### 16.4 TUI 测试

- 固定终端尺寸的 snapshot。
- reasoning/tool/diff/permission/session picker。
- 按键路由、粘贴、多行输入和中断。

### 16.5 完成门槛

逻辑变更必须补充匹配层级的测试；bug 修复必须包含回归测试。实现完成后至少执行：

```text
make verify
```

涉及 provider、context 或缓存时还必须执行对应 golden/cache regression suite。

## 17. 设计模式使用边界

推荐模式：

- Template Method：共享 turn lifecycle；Go 中只允许“骨架持有策略接口”的组合形态，禁止依赖 struct embed 后覆写方法模拟动态分派。Go 的方法 shadow 不会让基类方法内的 self-call 分派到外层类型，编译通过但运行时可能静默失效。
- Strategy：provider request、stream、cache、compaction 策略。
- State：SSE reducer 和 turn 状态机。
- Command：runtime command 和 tool invocation。
- Observer/Event Bus：RuntimeEvent 到 TUI/session/telemetry。
- Repository：session store 和 index。
- Decorator/Pipeline：hooks、telemetry、policy 包裹工具执行。
- Adapter：Claude/Codex plugin manifest 导入。

设计模式用于明确变化边界，不得为了模式而增加空壳接口、单实现层或无意义的工厂链。

## 18. 架构不变量

以下规则属于硬约束：

1. Provider 原生 item 不通过通用扁平消息反向重建。
2. UI 不依赖 Anthropic/OpenAI wire 类型。
3. HistoryProjector 是 native history 到 SemanticHistoryView 的单向只读投影，不能反向构建请求。
4. ToolExecutor 不包含 UI 渲染代码。
5. 缓存相关集合必须稳定排序、稳定序列化。
6. 工具调用必须有唯一 call ID 和幂等 ledger。
7. JSONL 是 session 事实源，SQLite 可重建。
8. API key 和敏感 header 不得进入日志、错误或 session。
9. domain/protocol 层不包含网络、数据库和终端副作用。
10. 禁止上帝类、上帝包、循环依赖和跨层捷径。
11. 每次逻辑变更都需要测试、自检并保持全量测试通过。
12. Provider request canonical bytes 由 Provider compiler 唯一生成，transport 只发送不可变快照。
13. Session v1 draft/decoder 强类型且按 kind/revision 校验，不通过 `any` 或反射 registry 传播核心 payload。
14. 配置与 Session 的类型、权限和 symlink 安全判断绑定实际打开句柄；无法提供等价语义的平台必须失败关闭。
15. 生产包级 `var` 只允许私有 sentinel error 和编译期接口断言；依赖方向、核心动态类型与占位 reachability 由 `internal/architecture` 的 AST/import graph 回归守卫。
16. 项目指令只由当前进程启动目录的一次安全发现产生；它可以进入 ContextPlan 与 Provider 临时请求上下文，但不得进入 native commit、Session、RuntimeEvent 或语义历史。

## 19. 架构决策和踩坑记录

需求、契约和实施工作统一由 OpenSpec change 管理；任何契约变更必须先完成 `explore -> propose -> review/confirm -> apply -> verify -> archive`。ADR 不承担任务管理，只记录已经接受且需要长期保留理由的重要架构选择，至少包含：

```text
背景
约束
候选方案
最终决策
代价与风险
缓存影响
迁移/回滚方式
验证和回归测试
```

ADR 存放在 `docs/architecture/adr/`，模板见该目录的 `README.md`，并与产生该决策的 OpenSpec change 双向引用。实际开发中遇到的 provider 兼容、缓存失效、流式边界、工具幂等、终端渲染和 session 恢复问题，统一记录到 `docs/roadmap/pitfall-log.md`，并关联 OpenSpec change、修复提交、测试和 ADR。
