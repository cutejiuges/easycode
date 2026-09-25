# EasyCode 总体架构设计

> 状态：初始基线  
> 更新时间：2026-09-18  
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

- 当前参考提交为 `7498521d2`。
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
                     CLI / TUI / --print / JSON
                               |
                         SessionService
                               |
        HookEngine -> ContextPlanner -> TurnRuntime
                                          |
                 +------------------------+------------------------+
                 |                                                 |
       AnthropicProviderKernel                          OpenAIProviderKernel
       - Messages request                              - Responses request
       - content-block reducer                         - response-item reducer
       - thinking/signature                            - summary/encrypted reasoning
       - JSON tool input                               - function/freeform input
       - cache_control                                 - prompt_cache_key/incremental
                 |                                                 |
                 +------------------------+------------------------+
                                          |
                                    RuntimeEvent
                           +--------------+--------------+
                           |                             |
                      UI Projection               Session Recorder
                           |                             |
                   Bubble Tea views            append-only JSONL
                                          |
                                 ToolScheduler / Policy
                                          |
                               Hooks -> Executor -> Result
                                          |
                              Provider ToolResult Encoder
                                          |
                                      next sample
```

### 4.1 建议的 Go Module 结构

```text
cmd/
  easycode/                 可执行程序入口
internal/
  app/                      依赖装配和应用生命周期
  domain/                   核心 ID、值对象、错误和领域语义
  protocol/                 Command、RuntimeEvent、可版本化外部协议
  runtime/                  session、turn loop、队列、取消和恢复协调
  provider/                 ProviderKernel 契约和能力模型
    transport/              Resty HTTP/SSE 公共基础设施
    anthropic/              Anthropic Messages 实现
    openai/                 OpenAI Responses 实现
  tool/                     ToolSpec、Executor、Policy、Scheduler
    builtin/                read/edit/write/grep/glob/bash/patch/plan 等
  context/                  上下文分层、token budget、compact、cache plan
  session/                  JSONL、artifact、resume/fork、SQLite projection
  extension/                hooks、skills、plugins、MCP
  subagent/                 thread tree、mailbox、后台任务和限流
  tui/                      Bubble Tea、输入、消息 view、overlay 和 diff
  telemetry/                slog、redaction、metrics 和 diagnostics
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

runtime -> protocol + providers + tools + context + session + extensions + subagents
tui     -> protocol + SessionService interface
app     -> runtime + tui
cmd     -> app
```

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
| 数据库 | `database/sql` + pure-Go SQLite driver | JSONL 事实源之上的本地索引，保持跨平台单二进制 |
| 日志 | 标准库 `log/slog` | 结构化字段、分组、handler 和敏感字段过滤 |
| ID | UUID v7 实现 | 全局唯一且大致按时间有序 |
| Secret | 自定义不可打印值对象 | 防止 String/GoString/日志意外泄漏 |
| 文件监听 | `fsnotify` | skills/plugins/hooks 热加载 |
| Golden/Snapshot | 标准库 testing + `testdata` golden files | 避免测试依赖过重，便于审阅 wire 差异 |

Resty v3 当前使用 `resty.dev/v3` 导入路径；在项目初始化时最新可用版本为 `v3.0.0-rc.4`，升级到稳定版前必须重新运行 provider/SSE 契约测试。Streaming 请求使用 Resty raw body，不使用 RC `SSESource` 作为工程契约；`internal/provider/transport` 自行处理 LF/CRLF、任意 chunk、UTF-8 边界、4 MiB 默认 event 上限、取消和 idle watchdog。Sonic 与 Resty 的自动 JSON 行为不得混用：EasyCode 使用 `sonic.ConfigStd` 的 map key 排序、字符串校验和标准兼容行为产生确定请求字节，再交给 Resty 发送；响应也由 provider wire 层显式调用 Sonic 解码。

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

默认能力按 wire 提供，用户配置可以覆盖。运行时遇到服务端不支持时要产生可诊断的 capability downgrade，而不是静默丢失数据。`UsageParser` 还必须将不同 Provider 的 usage 语义归一化为 `input_uncached`、`cache_read`、`cache_write` 三元组，并保留原始字段和 unknown 状态。

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

## 6. 缓存架构

### 6.1 目标

缓存设计需要同时优化正确性、命中率和可解释性：

1. 相同稳定上下文必须得到完全相同的序列化前缀和指纹。
2. turn 级动态状态只能影响尾部，不得污染稳定前缀。
3. 新增动态 MCP 工具时，不应无条件破坏所有内置工具的缓存前缀。
4. 缓存失效必须能够定位到具体 segment 和原因。
5. 缓存优化不得复用已经过期的权限、工具或 instruction。

### 6.2 上下文分段

ContextPlanner 输出有序 `CachePlan`，而不是直接拼接字符串：

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

Anthropic 的 `input_tokens` 通常表示未命中缓存的输入，`cache_read_input_tokens` 与 `cache_creation_input_tokens` 是额外字段；OpenAI 的 `prompt_tokens` 通常已经包含 `cached_tokens` 子集。因此不能直接用两家原始字段做同一个分母。`UsageParser` 必须先输出归一化三元组：Anthropic 的 `normalized_input_total` 为三者之和，OpenAI 则从 `prompt_tokens` 和 cached 子集推导未缓存输入。字段状态需要区分 known、unknown 和 not-applicable；只有 Provider 契约明确“不适用”时才归一化为已知 0，预期字段缺失时相关 ratio 必须标记为 unknown。分母为 0 时 ratio 也保持 unknown。

### 6.8 缓存测试门槛

- 同一输入重复编译，canonical request 和稳定 segment fingerprint 必须完全一致。
- 只修改 cwd/git 状态时，Segment 0-5 fingerprint 不变。
- 只增加动态 MCP 工具时，内置工具 segment fingerprint 不变。
- 插件发现顺序、文件系统枚举顺序变化不能改变最终 catalog 顺序。
- Anthropic cache marker 数量和位置有 golden test。
- OpenAI prompt cache key、item 顺序和 incremental fallback 有 golden test。
- 每次修改 system prompt、tool schema、skill/plugin catalog 或 context planner，必须运行 cache regression suite。

## 7. RuntimeEvent 与流式归并

### 7.1 RuntimeEvent 示例

```text
SessionStarted
TurnStarted
AssistantItemStarted
AssistantTextDelta { item_id, phase, text }
ReasoningStarted { presentation_kind }
ReasoningDelta
ReasoningSectionBreak
ToolCallStarted
ToolInputDelta
ToolCallReady
PatchDraftUpdated
ToolExecutionStarted
ToolProgress
ToolExecutionCompleted
UsageUpdated
ContextCompacted
TurnCompleted
TurnFailed
```

Event 必须带有足够的 `session_id/thread_id/turn_id/item_id/call_id`，使 UI 和 session projection 可以独立消费。

### 7.2 Provider StreamReducer

Anthropic reducer 负责处理：

- `message_start/delta/stop`
- `content_block_start/delta/stop`
- text/thinking/signature/input_json delta
- stop reason、usage、异常 block 顺序和中断恢复

OpenAI reducer 负责处理：

- response/item created/added/done/completed
- output text delta
- reasoning summary/raw delta 和 section break
- function/custom tool input delta
- message phase、usage、end_turn 和 server response id

只在完整 tool call 已得到可靠参数后执行副作用。可以在整个模型流尚未结束时启动已经完成的工具 block，但不能根据不完整参数提前执行。

### 7.3 背压与渲染节流

- 原始网络 delta 可以高频进入 reducer。
- TUI projection 按 16-33ms 合并纯文本刷新，降低闪烁和 CPU 占用。
- 工具边界、权限请求、错误和 item 完成事件不得被合并丢失。
- 慢 session writer 不应阻塞网络流；持久化队列必须有容量限制和明确降级策略。

## 8. Tool 系统

### 8.1 分层

```text
ToolCapability    共享能力标识，例如 fs.read、fs.patch、shell.exec
ToolFacade        provider/model 可见的名称、描述和 schema
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

### 8.3 调度与幂等

- 并发安全的只读工具可并行。
- 非并发安全或写工具获得独占门。
- 结果按模型调用顺序提交，避免破坏 tool call/result pairing。
- 每次调用以 `call_id` 建立 execution ledger。
- 网络重试、session replay 和 UI 重连不能重复执行已有副作用。
- 取消必须区分“终止工具”和“等待工具完成后丢弃/保留结果”。

### 8.4 首批内置工具

- Read
- Glob
- Grep
- Edit
- Write
- apply_patch
- Bash/exec + write_stdin
- ViewImage
- Todo/Plan
- AskUser/RequestUserInput
- Skill
- Agent
- MCP tools

## 9. Context 与 Compaction

### 9.1 上下文来源

- base/developer instructions
- 项目级 AGENTS/CLAUDE 类指令
- provider/model/collaboration mode
- tool schemas
- plugin/skill catalog
- 已激活 skill 正文
- 原生 conversation history
- retained context
- cwd、workspace roots、git 和权限 world state
- 当前用户输入与排队消息

ContextPlanner 必须明确每一项的来源、优先级、稳定性、token 估算和生命周期。

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
  seq
  timestamp
  session_id
  thread_id
  parent_thread_id
  turn_id
  event_kind
  payload
```

持久化内容包括：

- SessionMeta 和配置快照。
- provider-native 完成 item。
- user/tool/compact/permission/hook/subagent/turn boundary。
- usage、cache 指标和迁移版本。

默认不持久化每个文本 delta、spinner 或窗口状态；在 item 完成时持久化最终值。需要崩溃恢复时，可以增加有节制的 checkpoint，而不是把所有 UI delta 写入日志。

### 10.3 SQLite projection

SQLite 不是模型历史的唯一事实源，只负责：

- session/thread 元数据索引。
- project、cwd、title、tag、时间和 provider 检索。
- parent-child agent graph。
- 可选的日志索引和全文搜索。

SQLite 可以从 JSONL 重建。数据库损坏不应导致 transcript 永久丢失。

### 10.4 写入与权限

- JSONL append-only，单 writer 保证 seq 单调。
- 文件使用用户私有权限，Unix 下目标为文件 `0600`、目录 `0700`。
- 支持 flush、尾部半行修复和 schema migration。
- artifact 文件路径必须防止目录穿越。

## 11. Subagent

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

示例：

```toml
[providers.anthropic]
family = "anthropic"
wire = "messages"
base_url = "https://example.com"
api_key_env = "ANTHROPIC_API_KEY"
model = "..."

[providers.openai]
family = "openai"
wire = "responses"
base_url = "https://example.com/v1"
api_key_env = "OPENAI_API_KEY"
model = "..."
```

- 默认只保存环境变量名称，不保存明文 key。
- 如未来支持配置文件内 token，必须使用 secret type、限制文件权限并在所有 String/GoString/slog/JSON diagnostic 中脱敏。
- base URL、额外 header 和 query 参数需要校验，错误信息不能回显 secret。
- 不实现账号登录、设备码、OAuth 或订阅系统。

## 15. 日志与可观测性

必须区分：

1. Session transcript：可恢复的业务事实。
2. Diagnostic log：调试 runtime/provider/tool 问题。
3. Telemetry：可选 metrics/traces，不参与恢复。

使用 `log/slog` 建立结构化日志上下文；需要跨进程 trace 时，在 telemetry 边界接入 OpenTelemetry：

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
