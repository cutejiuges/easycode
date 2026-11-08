# EasyCode 总体架构设计

当前能力及平台证据见[当前能力清单](../roadmap/capability-status.md)。本文中的“后续目标”不代表已经暴露的 schema、事件或命令；当前行为契约以 [OpenSpec 主规格](../../openspec/specs/) 和代码为准。

## 当前契约与生命周期基线

当前九项工具包括 Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill、Question。结构化问题和回答的不可变值位于 `protocol/question_values.go`，tool 使用类型别名组合 Invocation/Result，避免 protocol 反向依赖 tool。Provider 只处理各自原生 wire；Runtime 拥有等待与回答接收；TUI 仅编辑草稿和提交 typed 控制命令。恢复时 app 按原生提交 seq 把已验证的 ready/result 绑定为纯内存 RestoreRecord 事实，两家在安装历史前核对完整题目及冻结输出一致，防止两套事实各自合法却彼此不同。

`Exclusive()` 是有序调度屏障，`RequiresApproval()` 只对 Patch/Write/Exec 为真。Question 在 started Sync 后发布 `user_question_requested`；回答通过 `resolve_user_question` 接收前验证完整 Session/Provider 编码和预算，result Sync 后才发布 `user_question_resolved`。不可变 `ToolOutputPreflight` 只持有单项原生调用快照，不在控制 owner 上访问活动 Conversation。取消接收后的回答义务由既有 turn worker 完成，关闭仍等待同一个 owner。

命令输出存储位于 `internal/session/artifact`；Runtime 在 durable result 后授权引用并更新工具事实投影，app 装配 typed 宿主查询，TUI 不读文件。ReadArtifact 的命令预览与保存前缀分离；保存文件缺失不影响 Provider 原生恢复。预算、目录隔离和 owner 细节见 [ADR-0005](adr/0005-private-command-output-and-host-queries.md) 与 [Tool 输出存储](tool-system.md#命令输出存储与查询)。

Session/native/checkpoint envelope 均不含格式版本字段；headless JSONL 独立使用外部 v1，不得类推到内部持久化。prepared sample/tool outputs 仅提供显式 finalize/discard 构造器，回调完成后才能报告 finalized。

`internal/codec` 从固定 Sonic 配置值编码，不读取可替换的第三方默认 codec。封闭 JSON 对象拒绝重复键、大小写别名、未知字段、尾随内容和有损 Unicode。工具缺省参数保留默认值，显式 null 失败；持久化 record 按 json 标签要求非 omitempty 字段存在，标量与对象不接受 null，当前 nil 集合仍允许编码为 null。自定义联合 decoder 负责分支字段；Provider opaque 原生项保留独立扩展规则和原始字节。

TUI 与一次性 headless 由显式 `ChatSession.Start` 启动 AgentLoop adapter；stream-json 由 StreamRunner 启动同一 AgentLoop 内核，在返回前完成 join。ChatSession 不再直接启动 RunTurn。宿主退出先停止投递、排空控制输出并等待 worker、loop、dispatcher，应用随后关闭 Provider 的活动及空闲连接，再释放 writer、workspace、repository。等待超时保留错误，transport 取消登记的活动请求，由 SSE owner 关闭实际 body；lease 保留至最终清理完成。重复资源关闭等待同一个完成信号并保留最终错误。

Runtime 的有序调度、实例审批状态和独占执行骨架分别位于 `tool_scheduler.go`、`approval.go`、`exclusive.go`。骨架持有强类型策略，统一 started 接收、结果关联验证与 durable 收口；各工具保留候选、安全和展示规则。架构守卫覆盖 tool 核心 API、codec 越界及构造器可解析调用链；动态接口与第三方实现另由边界生命周期测试验证。

> 状态：持续演进
> 更新时间：2026-11-06
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

- 当前本地参考快照提交为 `6f8bf683cee83ed4d580a2e106b587d8451db936`（命令/Skill 补全调研核对）。
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
TurnRuntime -- durable tool loop --> ToolScheduler / Policy / Executor
```

应用在取得启动工作目录后、打开 Session/Catalog/Provider 资源前，通过 `context/projectinstructions` 执行一次 descriptor-bound 发现。Loader 以最近 `.git` 为边界，从项目根到启动目录逐层选择 `AGENTS.md`，仅在同层主文件不存在时回退 `CLAUDE.md`，并生成只含项目根相对来源的有界不可变快照。应用默认模型可见预算为 32 KiB，构造器硬上限为 4 MiB；零值、负值和超限配置均在文件读取与预分配前拒绝。活动进程不热更新；resume/continue 使用新进程当前启动目录重新发现的快照，不读取 Session `creation_cwd` 中的旧规则。

Runtime 构造时深拷贝该快照，并在每轮把同一值分别交给 ContextPlanner 与 Provider RequestCompiler。两家 Provider 都在全部 committed native history 和当前真实用户输入之前生成一个临时 user context item/message；它不进入 `PreparedSample`、native history、`SemanticHistoryView`、RuntimeEvent 或 Session JSONL。

### 4.1 建议的 Go Module 结构

下列结构同时包含当前实现与目标边界。`tool` 已实现 ReadArtifact、Read、Glob、Grep、Patch、Write、Exec、Skill、Question 九项能力；`extension/skill` 已接入；其他扩展、`subagent`、`telemetry` 仍保留经批准的结构化 TODO 占位，分别等待 P6/P7/P8 的 OpenSpec 提供真实消费者、验证与测试；不得从目录存在推断能力已经可用。

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
    transport/              Resty HTTP/SSE 与可选 WebSocket 公共传输
    anthropic/              Anthropic Messages 实现
    openai/                 OpenAI Responses 实现
  tool/                     当前强类型输入、Facade、Executor、Policy 与 ResultCodec
    builtin/                九项工具的已实现执行/准备边界
  context/                  上下文分层、token budget、compact、cache plan
  session/                  JSONL、artifact、root-thread resume；fork 未实现
    catalog/                可重建 SQLite Catalog 与前台 reconciliation
  executor/process/         Exec/Hook 共用进程生命周期
  extension/                本地 Skills 与 PreToolUse/PostToolUse Hooks；P6 后续目标：Plugins、MCP
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

当前依赖方向为 `domain/protocol <- provider|tool|context|session|extension <- runtime <- app/tui/headless/cmd`。Skill/Hook 已有真实消费者，宿主显式 Load 后注入 Runtime；共享 `executor/process` 与 `extension/localfile` 不依赖业务层。Subagent、Plugin/MCP、telemetry 及 extension 根占位仍未接入 Runtime/app。TUI/headless 通过宿主接口通信，不直接导入 Runtime 实现。

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
| 文件监听 | 后续选择，当前未引入 | Skills/Hook 启动冻结，Plugins/MCP 与热加载尚未实现 |
| Golden/Snapshot | 标准库 testing + `testdata` golden files | 避免测试依赖过重，便于审阅 wire 差异 |

Resty v3 当前使用 `resty.dev/v3` 导入路径；在项目初始化时最新可用版本为 `v3.0.0-rc.4`，升级到稳定版前必须重新运行 provider/SSE 契约测试。Streaming 请求使用 Resty raw body，不使用 RC `SSESource` 作为工程契约；`internal/provider/transport` 自行处理 LF/CRLF、任意 chunk、UTF-8 边界、4 MiB 默认 event 上限、取消和 idle watchdog。Sonic 与 Resty 的自动 JSON 行为不得混用：只有无内部依赖的 `internal/codec` 可以直接导入 Sonic，并由其统一提供 map key 排序、字符串校验、严格解码与 canonical JSON；domain/protocol 可依赖该基础设施包，Provider 与 Session 通过它生成确定字节后再交给 Resty 或存储边界。

Sonic 在 amd64/arm64 之外会使用 fallback 实现。单二进制发布仍以兼容性矩阵和跨平台 golden/test 为准，不得假设所有架构都拥有相同的 SIMD 性能；P8 发布强化必须记录该差异及基准结果。

Provider HTTP 层不直接依赖官方模型 SDK。原因是 EasyCode 需要支持任意兼容 `base_url`、自定义 header、精确 SSE 状态机、opaque 字段保留和请求字节稳定性。若未来使用官方 SDK，只能将其封装在 provider 边界内，并通过同一套 golden/cache 测试证明行为等价。

首版 CLI、TUI 和 runtime 在同一进程内通过 channel/接口通信。只有 IDE、远程工作区或多客户端需求实际出现后，才引入 app-server/JSON-RPC，届时复用现有 protocol，而不是重新定义业务模型。

## 5. Provider Kernel

### 5.1 模板方法边界

共享 Runtime 的当前采样流程：

1. AgentLoop 按 FIFO/strict-next 领取用户输入，冻结独立 InputID。
2. 准备输入候选、上下文预算及适用的阈值摘要；输入 batch 成功 Sync 后 finalize。
3. 由当前 ContextPlan 和 Provider 策略编译原生请求及缓存证据。
4. 消费 stream，归并原生 item 与瞬态语义事件，验证完整 PreparedSample。
5. 原子提交 native sample、usage 与 ready calls 或最终 terminal，成功后 finalize。
6. 工具按只读批次/exclusive 屏障执行；三项 mutation 先过适用 PreToolUse 门禁和 PostToolUse 独立信任，再候选/普通批准；Question 单独等待回答。
7. 按 call index 冻结并 Sync results，提交各 Provider 匹配的原生 outputs。
8. outputs 完整后开放同轮输入安全点，继续下一 sample 或结束 turn。
9. 发布已提交的 usage、context/cache 与 transcript 投影；关闭仍等待唯一 owner。

UserPromptSubmit、Stop 等其余 Hook 事件尚未实现，不能在上述流程中视作已执行步骤。

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

Go 中通过小接口与组合实现。Provider 持有不可变配置和 transport，并通过显式 ThreadID 的 `provider.Factory.NewConversation` / `RestoreConversation` 创建会话级 `provider.Conversation`；`runtime.Runtime` 只依赖 Conversation，避免多个 Session 共享可变 native history。运行时装配具体实现：

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

当前文本会话使用两阶段提交。Provider 在成功终态生成同时携带 opaque `NativeCommitEnvelope` 与 normalized `SampleUsage` 的 `PreparedSample`，不立即修改 committed native history。Runtime 重新验证两者，将 `[provider_native_commit(sample), sample_usage, turn_completed]` 作为同一 JSONL batch 写入并执行 `Sync`，随后调用一次性 finalizer，最后才发布携带聚合 turn usage 的成功终态。持久化失败会 poison 当前 Runtime，未 finalize 的 staging 不得进入下一请求。

OpenAI 当前 native payload 保存用户 Responses item、有序 output items 和原始 usage；Anthropic 当前 native payload 保存 user/assistant message、最终 metadata 和原始 usage。两者都显式保留 known/unknown 状态，归一化结果由独立 `sample_usage` 保存。恢复时对应 Provider 先事务式解码全部 commits，任一错误都不返回部分历史。共享 Session、Runtime 和 TUI 不解析具体 Provider wire。

## 6. 缓存架构

### 6.1 目标

缓存设计需要同时优化正确性、命中率和可解释性：

1. 相同稳定上下文必须得到完全相同的序列化前缀和指纹。
2. turn 级动态状态只能影响尾部，不得污染稳定前缀。
3. 新增动态 MCP 工具时，不应无条件破坏所有内置工具的缓存前缀。
4. 缓存失效必须能够定位到具体 segment 和原因。
5. 缓存优化不得复用已经过期的权限、工具或 instruction。

### 6.2 上下文分段

当前已实现不可变、强类型且可验证的 `CachePlan`、stable-prefix fingerprint 和 ContextPlanner。计划依次输出 `provider_profile`、`tool_catalog`、可选 `project_instructions`、可选 `skill_catalog`、`committed_history`、`current_input`；九项真实工具的稳定 facade/schema 进入 catalog segment、估算与 fingerprint，实际 command、workdir、approval 和宿主环境不得污染稳定前缀。项目快照非空时以 `replace + project-stable` 进入稳定前缀，history 为 turn-stable，当前输入为 volatile。当前分段如下：

```text
provider_profile       replace / stable
tool_catalog           replace / stable
[project_instructions] replace / project-stable
[skill_catalog]        replace / project-stable
committed_history      append / turn-stable
current_input          replace / volatile
```

来源和 segment 使用强类型身份、lifecycle/stability、内容 revision、canonical bytes 与 fingerprint；`CacheSources` 单独提供内容/稳定 fingerprint 和失效解释。Provider 的最终 marker/key/增量收据独立于共享分段，不把服务商缓存命中视为本地可保证结果。Skill 正文进入显式输入或工具结果的历史尾部；Hook 配置和 trust 不进入 prompt。Plugin/MCP metadata、动态 cwd/git/env/world-state diff 及自动 rehydrate 尚未形成可执行来源。

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

已实现 disabled、ephemeral_5m 与 ephemeral_1h。策略在构造时冻结；5m 省略 ttl，1h 显式 ttl=1h。最终请求副本选择最多四个断点：最后内置工具、非空项目指令、满足至少 20 blocks 距离的先前普通输入边界、最新合法可复用内容。只标记 text/tool_use/tool_result，不标记 thinking、redacted、未知 block 或一次性摘要后缀；Raw 包装保留未知字段且拒绝冲突标记。普通、候选预检与手动/自动摘要共享最终编译入口，原生持久化结构不变。动态 MCP/插件工具尚未实现。

### 6.5 OpenAI 缓存与瞬态连接

已实现 omit/thread 的稳定 prompt_cache_key；key 只由命名空间与合法 ThreadID 派生。HTTP/SSE 默认且继续由 Resty 提供；可选 websocket_auto 通过 transport 封装 coder/websocket v1.8.14，保持 store=false，不转用服务端持久化。

完整逻辑请求总是可独立编译。WebSocket 的 response.create 省略 stream，只有同一连接上已经 durable finalize 的响应、完全相同的非 input 属性和精确原生前缀才可使用 previous_response_id + suffix。首次、恢复、失败、外层取消、换连接与压缩后发送 full；摘要独立 full 连接，不成为普通基线。写生成请求前仅对明确不可用或握手 404/405/426 固定回退 HTTP；写入后失败不自动重发。thinking/encrypted/phase 与未知字段参与字节比较。

每个连接一个活动读写 owner；空闲无 reader，实例登记持有唯一完成信号。握手/写期限 30 秒、事件 4 MiB、请求 16 MiB、读空闲复用既有配置。关闭先 join Session、取消并等待 Provider 所有连接，再关闭 writer lease。详见 [ADR-0009](adr/0009-provider-cache-and-transient-continuation.md)。

### 6.6 缓存失效矩阵

| 变更 | 当前失效范围 |
| --- | --- |
| Provider/model/profile 内容变化 | 从 `provider_profile` 开始 |
| 内置工具 schema/描述变化 | 从 `tool_catalog` 开始 |
| 项目指令内容/相对来源变化 | 从可选 `project_instructions` 开始 |
| Skill metadata catalog 变化 | 从可选 `skill_catalog` 开始 |
| 新增已提交对话 item、Skill 正文或工具输出 | `committed_history` 尾部变化 |
| 当前采样标记变化 | `current_input`；不改变稳定前缀 |
| cwd/git/status、Hook trust、UI 尺寸或日志级别 | 当前不直接进入这些来源；不得污染稳定前缀 |

Plugin/MCP 动态目录和 world-state 来源尚未实现，其失效规则应随实际来源另行设计，不能套用不存在的 segment 编号。

### 6.7 缓存可观测性

当前 `/cache` 与 stream-json `cache.query` 提供 process 级已提交快照：provider/wire/model、显式策略与独立策略 fingerprint、六种有序来源的 revision/content fingerprint、stable-prefix fingerprint、首个来源变化、原生 digest/关系、完整逻辑请求 fingerprint、最近实际 transport/full/incremental/reset/fallback，以及最近成功 sample/turn/摘要的 normalized usage。正文、真实缓存 key、URL、路径、凭据和原生 reasoning 不进入诊断。没有 cache plan 版本或原始 usage 日志。

Runtime 在实际采样前冻结同一 ContextPlan 的有界元数据，收据在 append 前验证，Sync/finalize 后发布给单 owner。候选预检与查询不推进观测；摘要独立计量，失败后保留已提交 sample，不伪造成功 turn。恢复显示 restored_baseline，不重建进程外历史统计。状态只保存固定六来源与最近三项观测，编码上限 64 KiB。

Anthropic input_tokens 是 uncached 输入；OpenAI total 包含 cached 子集，只有二者 known 才相减。使用统一 input_uncached/cache_read/cache_write/output/reasoning_output 的 known、unknown、not_applicable。所有适用输入分量已知、和无溢出且大于零才计算 cache_read_ratio；N/A 不贡献数值，unknown 不作零。cache_read>0 才表示观察到读取；相同指纹不证明命中。无已解释客户端变化的读取降低只显示 unexplained_cache_read_drop，不确定推断 TTL 或驱逐。费用、累计遥测和实际远端收益未验证。

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

当前已消费的进程内事件包括：

- `turn_started`、`assistant_text_delta`、`turn_completed`、`turn_failed`、`user_input_committed`。
- `tool_approval_requested/resolved`、`tool_ready/started/progress/terminal`、`transcript_committed`。
- `user_question_requested/resolved`、`hook_trust_requested/resolved`、`hook_activity`。
- `context_snapshot`、`cache_snapshot`、`compaction_started/completed/failed`。

这些事件有 typed 构造、validator 和实际消费者。工具 running、结果/回答收口及 Hook activity 分别受对应 durable 边界约束，事件本身不替代 Session/native facts。普通 usage、摘要 usage 与来源证据在成功 Sync/finalize 后才可投影；独立实时 usage、reasoning 和 patch delta 事件尚未实现。外部 headless DTO 的词汇独立于本枚举，不自动公开所有内部事件。

### 7.2 Headless JSONL v1

`internal/headless` 与 TUI 平级。一次性模式只依赖最小 `ChatSession` 接口；流式模式只依赖最小 `StreamControlLoop` 接口。两者都不会直接序列化 RuntimeEvent，而是严格校验 version、身份、顺序和 typed payload 后，单向投影为各自 sealed wire DTO。一次性 v1 保持 `thread.started`、`turn.started`、`assistant.text.delta`、`turn.completed`、`turn.failed` 与 stream-level `error`；`--json --input-format stream-json` 另行增加 `control.response`、携带 `input_id` 的 turn 事件和 `input.discarded`。外部 `turn.completed` v1 的 `usage` 为 required，使用 headless 自有强类型 DTO，不泄露 Provider raw 字段。stdout 由单 writer 顺序写入，每行一个完整 JSON object；日志和人类诊断不得进入 JSON stdout。

`--print` 在内存中聚合文本，仅在 Runtime 已 durable 发布 `turn_completed` 且事件流正常闭合后输出最终值。`--json` 可实时输出 delta，但同样不从 channel close 或已见文本推断成功。取消、短写和断管会触发幂等 Interrupt 并 drain Runtime；已 durable 完成后的输出失败只改变进程交付状态，不回滚 Session。

一次性 headless prompt 在应用装配前解析：无位置参数或显式 `-` 时读取 stdin；显式 prompt 与非终端 stdin 同时存在时使用稳定 `<stdin>` 边界追加；组合内容必须是合法 UTF-8、非空且不超过 4 MiB。流式模式只由 `--json --input-format stream-json` 显式启用，要求可关闭的非终端 stdin/stdout transport，以不超过 32 MiB 的单行 NDJSON 接收严格 `input.submit`、自动 `input.append`、按 InputID 原子 `input.retract`、定向 `turn.stop`/`turn.interrupt` 、`cache.query`、`context.query/compact/cancel` 和 `session.shutdown`。`--continue` 会在当前 cwd、Provider family/wire 和 model 完全匹配时恢复最近 root Session，并与 `--resume` 互斥。当前仍不支持独立 usage 更新或 reasoning/tool 事件。

### 7.3 AgentLoop 输入控制

`runtime.AgentLoop` 位于单 turn `Runtime.RunTurn` 之上，是 Session 实例级的唯一 admission owner。空闲 submit 得到 `starting`，活动期间的 submit 进入同时受条数和 UTF-8 字节约束的 FIFO，并得到 `queued`；strict-next 输入独立形成 durable turn，自动输入可在完整工具 outputs 后进入同轮后续 sample；两者均不注入正在运行的 Provider stream。command result 只确认进程内 obligation，只有关联的 `turn.started` 表示 `turn_started` 已成功 `Sync`。外部 request identity、queue 状态与 control metadata 都不写 Session，也不进入 Provider request/cache fingerprint。

queue 是有界、非 durable 的进程内状态。stdin EOF 只停止新 admission，并按 FIFO 排空已经接受的输入；显式 shutdown 则停止 admission、取消活动 turn、为所有尚未开始的输入发布 `session_shutdown` discard，并等待 Runtime durable terminal 与 worker cleanup。interrupt 必须携带当前活动 `turn_id`，过期目标不得误取消下一 turn。普通 Provider failure 后 loop 可以继续；journal poisoned 或 durable 结果不确定时停止所有后续 Provider 副作用。

该控制器同时处理严格跨 turn follow-up 与自动同轮输入。自动输入只在完整 durable tool outputs 后、下一 sample 前领取，strict-next 保持 FIFO 屏障。TUI 通过 ChatSession 的长期控制流接入同一 AgentLoop；一次性 headless 保留严格提交接口，二者共享唯一执行 owner。

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

当前等待完整 sample 的 native commit、usage 与 ready calls 成功 Sync 后执行工具；流内已完整的单个 block 也不会提前产生副作用。

### 7.5 背压与渲染节流

- 原始网络 delta 可以高频进入 reducer。
- 当前 TUI 按有序事件增量更新 Markdown projection，并复用解析/布局缓存；没有独立的 16–33 ms 刷新调度承诺。
- 工具边界、权限请求、错误和 item 完成事件不得被合并丢失。
- 高频 delta 不进入 session writer；成功终态和副作用边界必须等待 durable batch，不能为降低延迟而越过 `Sync` 确认。

## 8. Tool 系统

完整设计、参考工程取舍、恢复语义和分 change 推进顺序见 [Tool 系统架构设计](tool-system.md)。本章只保留总体架构必须遵守的边界。

本章同时描述已实现的 P3 基线与长期目标。当前 `internal/tool` 与 `internal/tool/builtin` 已由 Runtime/app 真实消费，提供 Read、Glob、Grep、需批准的单文件 Patch/Write、Darwin 前台 Exec、ReadArtifact、Skill 与 Question；未在能力分期中明确标记已实现的其他内容仍是目标，不得由接口或文档描述推断为可用。

### 8.1 分层

```text
ToolCapability    共享能力标识，例如 fs.read、fs.patch、process.exec
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

两家 Provider 各自归并完整参数，并通过各自 Tool WireCodec 解析 structured Edit 或 freeform apply_patch，生成共享 typed Patch 输入。当前没有通用 `ToolInputStreamDecoder`、`PatchDraftUpdated` 或渐进 diff 事件；完整候选 diff 由工具准备边界生成，供普通批准界面读取。

只有参数完整、strict decode 和语义验证全部通过后才能形成 `ToolCallReady`；副作用还必须等待整个 sample 的 durable batch、Hook gate、普通批准与执行开始事实。

### 8.3 Durable 边界、调度与幂等

- 当前 Provider sample 的 native commit、usage 和 ready calls 必须先作为一个 durable batch 成功 `Sync`，之后才能开始副作用。
- 并发安全的只读工具可并行。
- 非并发安全或写工具获得独占门。
- 结果按模型调用顺序提交，避免破坏 tool call/result pairing。
- 每次调用以唯一 invocation ID 建立 execution ledger，并保留 Provider call ID 用于原生配对。
- ledger 保证 EasyCode 自动执行至多一次，不宣称任意外部副作用 exactly-once；已 durable 开始但结果未知的调用恢复为 `outcome_uncertain`，默认不得自动重试。
- 每个 call 最终都必须得到成功、拒绝、取消、失败或恢复补偿 output，不能留下悬空 Provider 配对。
- 取消必须以 durable `execution_started` append是否被接受为线性化点：接受前直接记录cancelled且零工具I/O，接受后恰好调用一次executor并传入取消context。
- resume 只做本地配对补偿：普通 ready 调用关闭为 `session_interrupted_before_execution`，尚未批准的 mutation 为 `session_interrupted_before_approval`，started-without-result 为 `outcome_uncertain`；Question 与 Hook 按专用中断事实收口。已有 result 复用持久化 preview；恢复阶段禁止 executor 和 Provider 网络调用，以唯一 `turn_failed` 结束旧 turn。

### 8.4 权限与结果

- Approval 只表达用户意图，SandboxPlan 负责实际强制边界；批准不能自动放开 workspace、网络、环境变量或平台不支持能力。
- 文件 containment、类型、权限和 symlink 判断必须绑定实际打开的 handle，不能依赖字符串前缀或 `Lstat` 后再次按路径打开。
- 完整结果、模型预览、artifact 和 UI 摘要是四种表示；模型实际收到的预览字节必须持久化，resume 时不得按新配置重新截断。
- Presenter 只消费 RuntimeEvent 与受控 metadata，Executor 不依赖 Provider wire、Session 或 TUI。

### 8.5 能力分期

P3 当前已实现 Read、Glob、Grep、Anthropic structured Edit/OpenAI custom freeform apply_patch、Anthropic Bash/OpenAI exec_command、双 Provider structured Write、durable approval/ledger、最多 8 路只读并发、Patch/Write/Exec 独占屏障、有序结果提交和本地恢复补偿。Patch 限于一个已有 UTF-8 工作区常规文件，在 Unix 使用 descriptor-relative 重验与同目录原子替换；其他平台失败关闭。Write 的 structured facade 支持原子创建/整文件覆盖：覆盖要求先前已回传的完整 Read，新目录链通过私有树和 no-replace 发布；审批展示区分 create/overwrite，独立 Write grant 不在 resume 恢复。详见 [Tool 已实现边界](tool-system.md#101-write-已实现边界)。Exec 限于 Darwin 前台 non-login `/bin/sh`：每次单独批准、最小环境、workspace 写入、禁止网络、确定性头尾输出、原进程组清理与唯一最终 Wait；非 Darwin 返回 `unsupported_platform`，不降级。`setsid` 等脱离组后代不属于保证清理范围，但仍受继承的 sandbox 限制；输出收口不确定时 poison Runtime。详细预算与测试边界见 [Tool 系统架构](tool-system.md#93-进程工具)。write_stdin、PTY/后台进程控制、全工具 artifact、跨能力通用 sandbox 和跨平台等价执行仍按独立 change 增加。

本地 Skill 和 PreToolUse/PostToolUse Hook 已交付；其余 Hooks、Plugins/MCP 属于 P6 后续范围，Agent/Subagent 属于 P7；完整跨平台 sandbox 矩阵属于 P8。未到对应阶段前不得提前暴露空 schema、event kind、facade 或 capability flag。

## 9. Context 与 Compaction

本章同时描述当前 P2 基线和 P4 目标。当前已实现包含 Tool Catalog 的确定性 ContextPlanner、可选项目指令、本地 token estimate、Provider-private HistoryFootprint、可选显式 token budget 和请求前超限守卫；手动 compaction、有效窗口查询与 durable checkpoint 已实现；阈值自动压缩已实现；本地 Skill catalog 与双 Provider cache policy 已实现；动态 Plugin/MCP 来源和跨 Provider fork 尚未实现。

### 9.1 上下文来源

当前已接入 Runtime 的来源只有：

- `provider_profile`：family、model、schema revision，`replace + stable`。
- `tool_catalog`：九项工具的确定性 schema，`replace + stable`。
- `project_instructions`：启动时发现的有界结构化快照，存在文档时为 `replace + project-stable`，空快照省略。
- `skill_catalog`：冻结的本地 Skill metadata，非空时为 `replace + project-stable`；正文不进入此来源。
- `committed_history`：`ActiveContextView` 中的独立摘要与增量历史，结合不含 opaque 正文的 Provider footprint，`append + turn-stable`。
- `current_input`：采样标记，`replace + volatile`；正文已进入有效原生历史，排队和 staging 输入不重复计量。

后续候选来源包括 Plugin/MCP metadata、动态 cwd/git/env/world-state 与自动文件 rehydrate，必须在存在真实消费者、验证和测试时另行接入。Skill 正文已通过显式输入或工具结果进入原生历史，不是独立来源；排队/草稿不计入已提交上下文。

ContextPlanner 必须明确每一项的来源、优先级、稳定性、token 估算和生命周期；来源生命周期与 cache stability 是两个正交强类型概念，不能复用一个字符串表达。

### 9.2 历史不变量

- tool call 必须有对应 output。
- orphan output 在发送前被拒绝或修复，并留下诊断。
- 不能丢失 thinking signature、encrypted reasoning 或 provider item 顺序。
- 大工具结果按能力预算冻结模型预览；当前仅命令/搜索原文按策略保存 artifact，不能承诺所有工具保存完整内容。
- 当前用户与工具闭环为文本；图像、音频等输入须随真实 capability 另行实现，不能由 opaque 原生保留推定可用。

### 9.3 手动 Compaction

Provider 同时保留完整原生历史和有效请求窗口。checkpoint 安装后，普通请求只发送 Provider 保存的摘要包装及新增原生条目；完整 transcript、thinking/signature/encrypted content、工具位置和冻结预览保持不变。独立摘要不伪造 UserInput、TurnID 或 native ordinal。

AgentLoop 只允许空闲且队列为空时接收压缩。预检通过才生成 CompactionID，成功保存 `compaction_started` 后进行一次最长 120 秒的独立网络请求；双 Provider 保留工具目录与项目指令，但禁用 tool choice，并再次拒绝流中的工具调用。输出上限由 JSON-only `compaction_output_tokens` 控制，默认 4096，范围 1..65536。只接受完整、非空、合法 UTF-8、至多 256 KiB 的摘要，并要求新窗口估算严格缩减且满足配置预算。

候选沿用 prepared resolution：`compaction_completed` 成功 Sync 后才 finalize；取消发生在 writer admission 后仍以确定提交结果为准。失败保留旧窗口，无法确认持久化时 poison 并关闭；`compaction_failed` 单独记录 observed/unavailable usage，压缩费用不混入普通 turn。操作和队列状态不写入请求指纹。

恢复按 seq 验证全部原生记录、来源 digest、模型、前驱与 checkpoint 包装，再修复物理尾部。started-only 只追加本地 interrupted 终态，不联网、重执行工具或读取旧 artifact 内容。摘要不恢复审批 grant、文件观察或 artifact 授权。新进程重新发现当前项目指令。详见 [ADR-0008](adr/0008-durable-context-checkpoints.md)。

TUI 提供 `/context`、`/compact [instructions]`、定向取消及 Ctrl+O 完整历史；headless stream v1 提供 `context.query/compact/cancel`。完成后等待显式输入，已有队列则清理完成后继续；失败返还 auto，保留 strict-next。自动阈值压缩、本地 Skills 和 PreToolUse/PostToolUse Hooks 已有独立切片；其余 Hook、MCP、跨 Provider fork 与自动文件 rehydrate 仍是后续能力。

## 10. Session 与存储

### 10.0 术语

- `session`：一个逻辑会话和持久化命名空间，拥有一棵线程树，不等同于单条消息历史。
- `root thread`：Session 创建时的根线程，承载默认用户交互历史。
- `thread`：Session 内一条线性的 turn/native history；未来 Subagent 与跨 Provider fork 可创建 child thread；当前手动与自动压缩始终留在原 thread。
- `turn`：Thread 内一次用户输入到模型停止/工具循环结束的完整生命周期。

`session_id` 用于定位逻辑会话，`thread_id` 用于定位具体执行链；EventEnvelope 同时携带二者，不能只用其中一个替代另一个。线程树决策见 [ADR-0003](adr/0003-session-thread-tree.md)。

### 10.1 当前默认目录

```text
~/.config/easycode/config.json       主配置（或 --config PATH）
~/.config/easycode/hooks.json        用户 Hook 来源
~/.config/easycode/skills/           一种用户 Skill 根
~/.easycode/
  sessions/YYYY/MM/DD/<thread-id>.jsonl
  sessions/artifacts/<thread-id>/... 私有已保存输出
  state.sqlite                      可重建索引与受控 sidecar/lock
<workspace>/.easycode/hooks.json     项目 Hook 来源
```

其他 Skill 来源和项目指令按各自发现规则加载，见 README 与能力清单。当前没有 TOML 主配置、默认诊断日志文件、plugin 安装器或 watcher；设计中的日志/插件目录不是已创建资源。

### 10.2 JSONL 事实源

```text
EventEnvelope
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

当前理解的记录词汇如下；每条路径只要求其适用的 facts：

- `session_meta` 与 `thread_meta`；
- `turn_started`、`turn_inputs_committed`；
- `provider_native_commit`、`sample_usage` 与 `turn_completed`；
- `tool_call_ready`、`tool_approval_decided`、`tool_execution_started` 与 `tool_call_result`；
- `turn_failed`；
- `compaction_started`、`compaction_completed`、`compaction_failed`；
- `tool_hook_gate_planned`、`hook_execution_started`、`hook_execution_finished`、`tool_hook_gate_decided`。

Session envelope 只使用唯一当前字段结构，不包含 schema_version、payload_version、canary 或同义版本标记。decoder 只按 event_kind 选择唯一 strict decoder，字段/类型、checksum 和 replay 状态机共同校验恢复事实；未知 kind、未知字段与损坏在 repair、Provider 恢复和 append 前失败关闭。

Provider native commit 只记录 Provider 已验证的原生增量和 raw usage；紧邻的 `sample_usage` 记录共享五项三态指标。无工具的成功 sample 仍以 native commit、usage 与 terminal 原子提交；带调用的 sample 则把有序 ready facts 放入同一 durable batch，随后按需要 durable 批准决定，再提交 started、result 和 Provider 原生 tool outputs。当前 approval fact 保存 decision source、candidate fingerprint 与安全展示 metadata；Exec 不持久化 PID、宿主环境、输出 chunk 或底层 cause。缺失 usage、错序、遗漏配对或重复调用身份一律 fail closed。其余 Hook 与 Subagent 仍需按各自恢复语义定义记录；cache/compaction checkpoint 与当前 PreToolUse/PostToolUse facts 使用各自当前强类型契约；不能把 RuntimeEvent 或 UI transcript 当作 native history。

Session、Provider native/checkpoint 和压缩来源不承担格式版本管理。Catalog 不设置或校验 user_version，按实际结构与完整性重建派生索引。当前没有存量用户或业务数据，直接修改唯一当前格式与 current fixture。未经用户明确指示，禁止引入多版本兼容、历史 reader、迁移器或以 canary/revision 为同义替代的版本门禁；即使未来发布也不能推定获得授权。headless JSONL 是独立外部协议。

Headless JSONL v1 是 stdout 外部协议，不是 Session record：不得写入 journal，也不得在 resume 时回放。headless resume 只复用连续 lease 下恢复出的 Provider-native history，并只发布本次新 turn；`SemanticHistoryView` 仍只供 TUI 等只读消费者使用。

默认不持久化每个文本 delta、spinner 或窗口状态；在 item 完成时持久化最终值。需要崩溃恢复时，可以增加有节制的 checkpoint，而不是把所有 UI delta 写入日志。

### 10.3 SQLite projection

`internal/session/catalog` 是 JSONL 事实源之上的可重建投影边界。当前唯一 schema 只包含 `threads` 表及 continue 组合索引，保存 root thread 的 session/thread identity、journal 相对定位、创建 cwd、Provider family/wire、model、最后 committed record 的 timestamp/seq/checksum；不保存 prompt、response、native payload、title、tag、worktree 或全文搜索内容。

Catalog 在 `--continue` 路径按以下顺序工作：

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

全部 journal 文件 I/O 在 SQLite transaction 外完成；协调过程不启动后台 goroutine，也不会同时持有多个 journal lease。busy 的已索引行保持原值，未索引 busy thread 会阻止自动选择，避免静默跳到更旧会话。真正缺失或无效 journal 的旧行会删除；Catalog 不修复尾部；resume 完整验证 replay、Provider 兼容及原生内容后，才在同一 exclusive lease 下显式 Repair、truncate 并 `Sync`。

`state.sqlite.lock` 为 schema、重建和 reconciliation 提供跨进程唯一 owner。Unix adapter 从实际打开的 handle 校验私有 data home、数据库、rollback journal 与 sidecar 的普通文件类型和 `0600` 权限，并使用 no-follow 打开；无法提供等价语义的平台失败关闭。数据库缺失、损坏或 schema 不兼容时，在同一 Catalog 锁下从 JSONL 构建临时数据库，`Sync` 后原子替换。

选择结果不是恢复事实：Catalog 关闭后，app 仍通过既有 `sessionService.resume` 重新取得 journal lease并完整执行 load、repair、ReplayPlanner、Provider-native restore 与兼容性校验。选择和恢复之间发生占用、替换、损坏或配置不兼容时直接失败，不回退旧 thread。worktree/project catalog、title/tag、全文搜索、实时索引和 thread graph 仍属后续能力。

`sessions list` 新增独立发现入口：`cmd/easycode/sessions.go -> app.ListSessions -> catalog.Reconcile/List`，不进入聊天配置、项目指令、Provider 或 Runtime 装配。`ListQuery` 明确区分规范化创建目录和全部目录，使用原 schema 按 committed `updated_at`/thread ID 双降序、先筛选后 limit+1，返回至多 200 条及 HasMore。查询不依赖 Provider selector，`LatestCompatible` 仍保留原四字段精确匹配。

列表沿用上述只读 journal 协调，随后先关闭 Catalog/Repository，再格式化和输出。busy/invalid 是全存储扫描报告，stderr 明确提示元数据可能未刷新或会话缺席；列表不补偿未完成 turn，也不把可见条目当作恢复事实。CLI 采用紧凑文本、完整 ID、受限并转义的字段，不读取对话正文作标题。当前目录不等于项目/worktree，limit 不是扫描预算；project/worktree 聚合、title/tag/全文检索仍是后续能力；交互 picker 已交付并复用相同只读协调。

### 10.4 写入与权限

- JSONL append-only；每个活动 thread 通过绑定 journal handle 的跨进程 exclusive lease 保证只有一个 writer。新建 metadata 或对既有 journal 执行 load/repair 前必须先取得 lease，并连续保持到 writer 完成最终 `Sync` 和关闭 handle；每个 durable batch 连续编号。
- macOS/Linux 从已打开数据根使用 no-follow、descriptor-relative walker 逐级创建/打开日期目录与 journal，并从实际 handle 校验类型和权限；lease、Loader、repair 与 writer transfer 复用最终 journal handle。Windows 等尚未提供等价语义的平台当前明确失败关闭，跨平台实现与真实平台验证留在 P8。
- 文件使用用户私有权限，Unix 下目标为文件 `0600`、目录 `0700`。
- Loader 严格校验 canonical UUIDv7、UTC 时间、checksum、ID 归属、序号和 batch 完整性；只读返回完整 committed 前缀及待修复凭据。显式 Repair 绑定 lease、原始大小与 digest，只允许在完整语义/Provider 校验后截去 EOF 尾部半行或未完成尾批次；截断接收后必须等待 Sync 确定结果，失败 poison lease，不允许跳过中段损坏。
- ReplayPlanner 只重放完整 batch。普通文本turn尾部直接追加 `turn_failed(code=session_interrupted)`；工具turn先按ledger在本地补齐cancelled/outcome-uncertain result与缺失Provider output，再追加唯一失败终态。两类恢复都不自动发起Provider请求。
- 当前 `--resume <thread-id>` 通过日期编码的 UUIDv7 定位文件，恢复 native history 和只读语义视图；不恢复旧 API key、base URL、cwd 或其他动态 world state。
- 仓库已建立不可变的当前文本与 Tool Loop fixture，用于验证 Loader/ReplayPlanner 的读取、续写 prefix 不变、账本顺序和双 Provider 恢复等价性；项目没有线上历史格式，因此开发期旧 fixture 与旧 reader 已删除。未经用户明确要求和新 OpenSpec，不引入历史 reader、迁移器或兼容窗口；不得在 resume 原地改写既有 records。
- exclusive lease 是协作进程间的 advisory lock，不能阻止旧版 EasyCode 或非协作进程绕过锁直接写文件；混合版本运行前必须确保目标 thread 没有其他 owner，绕过锁产生的损坏继续由 checksum、seq 和完整回放校验发现。
- SQLite Catalog、`--continue`、离线 `sessions list` 与启动 `resume` picker 已实现；worktree/project catalog、title/tag、全文搜索、实时索引和全工具 artifact 尚未实现。内部格式继续保持唯一当前契约，不预设未来兼容授权。
- 手动与阈值自动 compaction 追加独立 checkpoint，保留原始 JSONL；跨 Provider fork 与 child-thread 元数据仍为后续目标。摘要是可辨识的历史参考，不构成新的用户输入或授权。
- artifact 文件路径必须防止目录穿越。

### 10.5 自动压缩与活动回合

`internal/context/auto_compaction.go` 是纯值策略，`internal/runtime/auto_compaction.go` 在既有 turn worker 内协调输入候选、一次摘要尝试与普通请求重新预检；不存在并行摘要 owner。策略只使用显式窗口及 byte_heuristic_v1，不猜服务商容量，未配置、禁用、小窗口和 unknown 明确呈现。失败来源与连续三次熔断状态由 Runtime 实例持有，不写 Session。

手动入口取得 Runtime.active，自动路径复用已持有 owner 的摘要骨架。初始输入提交前或完整工具 outputs 后，冻结已领取输入，候选必须 Discard 后才允许 Kernel 摘要，成功后重新准备相同正文和 InputID。同一目标 sample 共用一次尝试额度；sample/call/result 预算不重置。compaction usage 不混入普通 sample usage。

Session 的 compaction_started/completed/failed 具有强类型 scope（manual，或 auto + turn_id + sample_index），checkpoint 保存同一 scope 并验证确定性接续包装。Replay 校验目标采样、工具闭合、同一 sample 不重复压缩，以及摘要区间不插入普通事实。恢复先收口悬空摘要，再关闭活动 turn；durable completed 恢复新窗口，不自动联网。取消与 completed Sync 竞争时已确定提交优先，后续任务停止，窗口不回滚。

自动事件通过 turn worker 的 publication 通道交给唯一 AgentLoop，使用关联原 submit 的 context item；查询观察 coherent committed snapshot，活动阶段显示 compacting，TUI 的摘要完成不代表 turn 完成。可恢复摘要失败只有在确定收口且普通请求重新预检通过时才继续；内部生命周期与 durability 错误停止工作。任务失败后返还 queued auto，strict-next 保持原顺序。TUI/stream-json 使用真实边界，一次性输出过滤摘要控制帧。

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

本章区分当前交付与 P6 后续目标。本地 Skills 与同步 PreToolUse/PostToolUse 命令 Hook 已接入 Runtime/app；Plugin、MCP 仍保留经批准的 TODO 边界，不接入运行。

### 12.1 当前 PreToolUse 命令 Hook

`extension/hook.Loader.Load` 在旧 turn reconciliation 之后显式捕获 user/project 两来源；`extension/localfile` 与 Skills 共享逐段 no-follow/同 FD 校验，保留各自来源权限策略。配置是严格有界 JSON，matcher 只映射 Patch、Write、Exec，声明与 source revision 不含绝对路径、环境值或授权状态。

Runtime 在现有 exclusive 屏障内、候选准备之前冻结计划并 Sync。AgentLoop 是信任响应和空闲 reset 的唯一授权 owner；授权绑定 Runtime、Session/workspace、来源 revision、声明指纹，只保存在实例内存。用户与项目声明均默认拒绝，Hook 信任与普通工具 grant 分离。headless 匹配项直接得到 `hook_trust_required`；无匹配不新增 Hook facts。

Exec 与 Hook 共用 `executor/process.Owner`，由窄 launcher/handle/clock 注入平台策略，保持独立业务结果、输出与 artifact 策略。Darwin Hook adapter 复用目录 FD、内部 helper、kqueue 注册后放行的启动屏障，采用工作区只读、禁网及最小环境；不存在未受限 fallback。Owner 同时持有 stdin writer、双流 reader、退出观察、原组清理和直接 Wait，Done 只在 owned 清理结束后关闭。此保证不等价于任意脱离子孙进程容器。

Session 当前词汇增加 `tool_hook_gate_planned`、`hook_execution_started`、`hook_execution_finished`、`tool_hook_gate_decided`。计划与 started 各自单批；最后成功 finished/gate 同批，终止 finished/gate/result 同批，信任拒绝 gate/result 同批。计划没有 command，结果没有 stdout/stderr/reason、PID 或环境。恢复先消费冻结计划：未开始/成功前缀补取消；started 无 finished 补 Hook 不确定和目标工具 cancelled；不把 Hook started 当作目标工具 started。恢复从 frozen results 补齐原生 outputs，最后关闭旧 turn，不加载配置、询问信任或执行脚本；当前快照随后只服务未来调用。

TUI `/hooks` 从内存快照浏览与滚动审查；`resolve_hook_trust`、`reset_hook_trust` 使用强类型身份。瞬态 `hook_trust_requested`、`hook_trust_resolved`、`hook_activity` 分别投影审查、确认和已 durable 的检查统计，不能替代 ledger。`HookHistoryObserver` 与 Session `HookHistoryEntry` 在成功 Sync 后提供历史安全详情，和模型 frozen preview 分开；没有保存或读取脚本输出 artifact 的入口。

单项 timeout 默认 5 秒/最大 30 秒，每调用总执行预算 30 秒，信任等待可取消但不计执行预算。stdin 最大 512 KiB，双输出各 16 KiB，模型安全摘要最多 1 KiB。只接受 exit 0 空输出/空对象和严格 structured deny、exit 2；其他行为失败关闭。上述拒绝规则只适用于 Pre；当前 PostToolUse 的边界见 12.1.1。其余事件、async、改写和 plugin hooks 仍是后续目标。

#### 12.1.1 当前 PostToolUse 与显式反馈

成功后的检查复用同一 Loader/localfile/进程 owner，不增加调度器或模型工具。domain 中事件与 none/model 策略进入声明、信任、计划和运行身份。Post 计划包含有序引用及冻结预算，整体 digest 同时绑定二者；工具候选/普通批准之前完成全部后置信任，headless 仍先返回 hook_trust_required。两阶段每调用合计 30 秒执行预算，信任等待不计时。

exclusive owner 先 Sync 唯一真实 result，再运行 Post；原 status/code 不受检查失败影响。`tool_post_hook_planned` 和 `tool_post_hook_completed` 独立于 Pre gate，复用的 started/finished 显式关联事件/plan/ordinal。finished 分开保存无正文统计与 content/code/empty 冻结选择；terminal 保存前缀计数、未运行后缀、安全 suffix_feedback、预算终值、规范反馈内容摘要及自身摘要。中间 finished 单批，最后 finished/terminal 同批；未启动后缀不生成运行统计。恢复区分下一项未 started 的 cancelled terminal 与已 started 的 recovery uncertain finished/terminal，保留原成功和已有反馈，不补发后缀提示。

Post stdin 仅投影当前三种工具的真实成功元数据，禁止透传含 artifact 提示的 preview。默认 none 丢弃正文；model 仅接收严格 additionalContext，单项 4 KiB，最终段含来源/包装 16 KiB。ResultBudget ticket 以 reserve/base/feedback/close 分段核算，只在 Sync 后计费；反馈以 durable sequence 去重，close 恰好退休一次调用。Session 从此前事实重算计划 A/R，交叉核验原 result、冻结内容、预算增量与 terminal feedback_digest。

`tool.ModelOutput` 组合冻结真实结果与反馈，Provider 当前唯一 Preparer/Preflight 接口均使用该值。RestoreRecord 的 tool_outputs 必须带经回放校验的输出证据；两家在纯内存中重编码并核对原生输出，保持 kind、call ID、status/is_error、opaque 数据和顺序。反馈只改变历史尾部，不改变工具目录、Skill 元数据或稳定缓存前缀；model 空反馈不扩展已因预留缩短的预览。

`post_hook_committed` 在每个后置批次 Sync、宿主快照安装后发布一次，headless 校验后忽略。HookActivity 的 Post completed/skipped 是聚合投影，不能充当 execution finished；工具主线保留真实终态；列表携带受控 PostHook 摘要，原生提交刷新与恢复不丢失检查状态，迟到的 Pre 活动不能覆盖工具后的检查。详情使用 post_hooks source 与 detail_sequence，旧修订返回 transcript_changed，TUI 失效对应旧页、推进 generation、从该 source 首屏重载并保留工具锚点，仍最多四页且每页 64 KiB。完整设计见[归档变更](../../openspec/changes/archive/2026-11-08-add-post-tool-use-command-hooks/design.md)。

### 12.2 当前本地 Skills

固定 user/project 根提供严格静态 frontmatter、稳定 metadata catalog、`/skills`、显式附件与按需 Skill/同来源资源读取。附件和工具结果保持双 Provider 原生恢复；技能不授权命令。system/plugin 来源、path activation、allowed-tools、model、context=fork 和隐式资源激活仍为后续目标。来源读取与 Hooks 共用 `extension/localfile`，正文加载、authority 和 catalog 生命周期仍由 Skill 自己持有。

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

### 13.2 当前视图与剩余目标

当前提供用户/assistant 流式 Markdown、代码/表格/原文阅读、紧凑工具主线、前台 Shell 进度、完整 Patch/Write/Exec 批准、结构化问卷、Hook 浏览/信任、context/cache 面板、压缩边界、Ctrl+O 分页详情和启动 resume picker。视图保持无 I/O，按需查询由宿主处理并用身份/generation 拒绝迟到响应。

完整 thinking/reasoning 折叠、后台任务、渐进 patch delta、MCP elicitation、Subagent/task 与 fork picker 尚未实现。测试中使用真实 PTY 验收界面，不表示模型命令工具支持 PTY。

### 13.3 REPL 行为

当前多行输入由 `internal/tui/composer.go`、`composer_input.go`、`composer_layout.go` 和 `composer_history.go` 组合实现。composer 拥有 UTF-8 正文与字素边界光标；同一不可变布局供视觉行移动、视口和无色光标渲染复用，普通移动与重复 View 不重建布局。历史以写时复制保留至多 100 条/8 MiB，直接消费 SemanticHistoryView，不执行 Session I/O。编辑和输入校验共享 `replace`，没有通用 editor registry 或新的宿主接口。

Model 将问卷、Hook 信任/浏览、普通审批、详情和提交状态按焦点优先路由，最后才由 composer 消费输入；普通活动 turn 期间可编辑并 Enter 自动追加。ChatSession 的交互模式只有一个长期 Outputs 消费者，Append 返回不是正式 transcript 事实。pending 快照冻结到 admission response：拒绝保留正文/光标/历史，接受后记入 recall 并清空草稿；只有 UserInputCommitted 才将对应 InputID 的正文移入 transcript。Up（空草稿首行）/普通 Esc 原子取回 queued 项，Ctrl+C 或无 queued 的 Esc 定向停止；Ctrl+R 逐条取回 returned，Ctrl+D 逐条丢弃，不自动执行。状态副本与 returned 共用 64 条/16 MiB 本地保留额度，uncertain 不允许取回重试。布局与用户 transcript 的控制字符只在显示副本转义，实际输入除 TUI 的 CRLF/CR → LF 外保真；headless 保留原换行。

#### 同轮输入与持久化边界

AgentLoop 单 owner 管理 auto/strict-next 共用 FIFO；strict-next 每条独立 turn，自动前缀可共享初始 turn。只有完整工具 outputs durable/finalize 后、下一 sample 前可以 claim 自动前缀，16 samples/64 calls 上限不重置。每条独立 InputID，RequestID 只用于控制响应，实际 turn 身份由 claim 决定。

输入先经 UserInputPreparer 纯内存候选历史、Context 预算和 Provider 请求编码预检，再提交相邻 `turn_inputs_committed` + `provider_native_commit` batch；Sync 成功后 finalize 并发出 UserInputCommitted 与 committed 状态。可预判超限返还输入且不改变历史；网络失败保留已保存输入；无法确认 durability 则 uncertain/poison，不自动重试。single writer 的接收线性化、lease 与 cleanup owner 继续生效。

Provider 当前 native kind 封闭为 `user_inputs`/`sample`/`tool_outputs`，sample 不再携带用户输入。原生 user_inputs 保存有序独立消息及 turn/placement/target_sample_index/input_ids；Session 输入事实只保存关联，无正文和 RequestID。SemanticHistoryView 按真实 TurnID 投影有序强类型消息，InputID 不进入请求 bytes/token fingerprint。PreparedUserInputs 和 PreparedToolOutputs 仅共享私有 resolution 生命周期骨架；Provider 编译始终读取原生历史，不反向使用投影。

input-only 崩溃尾部先在本地 durable 收口；工具安全点的 steering 尾部复用现有工具补偿路径，不重跑工具或自动联网。内存队列与 returned tray 不持久化，重启不猜测未保存输入。外部 headless JSONL 仍为 v1，自动 admission 增加 entry_id，input.status 不含正文；一次性 text/JSON 过滤新增内部输入事件。


已实现多行、字素编辑、显式 decoded paste 与有界历史；原始终端缓冲/RuneError 的框架限制见 README。当前本地命令为 `/cache`、`/context`、`/compact [instructions]`、`/skills`、`/hooks`，显式 Skill 使用 `/<name>` / `$name`。已支持命令和显式 Skill 的首词补全；尚无 `/help`、`/clear`、`/resume`、`/fork`、`/model`、`/provider`、`/permissions`、`/plugins`、`/mcp`、`/agents`、`/cost`、`/doctor`；`easycode resume` 是独立 CLI 入口。

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
- base_url 拒绝 userinfo/query/fragment；当前不提供自定义 header/query 配置面。错误不回显地址中的敏感内容。
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
- 唯一当前 Session 结构、固定 bytes 与损坏恢复；仅在用户另行要求并批准兼容设计后增加迁移测试。

### 16.3 集成测试

- mock provider 下完整 turn -> tool -> continuation。
- stream 断开、重试、取消、超时。
- 权限拒绝不产生副作用。
- JSONL 写入、崩溃尾部修复、resume/fork/compact。
- hooks、skills、plugins、MCP 生命周期。
- subagent 并发、限制和取消传播。

以上条目按对应 Roadmap 阶段启用。当前已落地双 Provider 文本 stream、Session/restore/headless、可重建 SQLite Catalog 与 `--continue`、descriptor-bound 配置与 journal 安全、不可变当前 fixture，以及架构静态门禁；不得把未来条目视作现有测试覆盖。

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
13. Session 当前 draft/decoder 强类型且按 kind 与当前字段结构校验，不通过 `any` 或反射 registry 传播核心 payload。
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


### 启动会话选择与离线投影

`cmd/easycode/resume.go -> app.RunSessionPicker -> tui.StartupModel` 在一个 Bubble Tea Program 中组合选择器与既有 Chat Model。配置与 cwd 在启动时冻结，项目指令仅在正式恢复时加载。`assembleChatModel` 由普通聊天和选择器共用，工具、审批、上下文、缓存与 transcript 消费逻辑保持单一实现。

`pickerOwner` 使用一个 worker、一个最新意图槽、单调请求身份和唯一 done。取消旧操作后等待清理再开始下一操作；成功资源先由宿主持有，经 UI 显式 Accept 后进入 Chat，关闭始终由宿主负责。锁内只转换状态，取消、结果发送和关闭均在锁外。`recoveryAttempt` 区分纯验证、可能修改 journal 和清理失败；后两类失败结束启动，不把已有合法事实当作未发生。

列表与独立 `sessions list` 共用 `loadSessionCatalog` 生命周期；首次和刷新协调，成功后的切范围仅查询，最多 200 条，正文不入 Catalog。`previewSession` 在 descriptor-bound lease 下 Load/Replay、恢复顺序及压缩用量校验，关闭句柄后才发布预览；不用 resume、writer、Repair、artifact 或 Runtime。`provider.StoredHistoryReader` 是有实际消费者的单向离线接口，两家独立 reader 与正式恢复共享私有 native 校验和压缩来源计算，无连接、凭据或新增 capability。

选择理由、参考工程版本、预算和验证方案见 [OpenSpec 变更](../../openspec/changes/archive/2026-11-04-add-interactive-session-picker/design.md)。这是一块恢复入口的完整切片，不代表 P5 整体完成；Markdown 已由独立切片交付，reasoning 和 fork picker 仍未实现；命令/Skill 补全已由独立切片交付。

选择器接收以 owner 锁内对操作/父取消 done channel 的非阻塞检查为线性化点；此前已取消的结果不能进入 Chat。资源仍由同一 worker 持有到最终关闭，不在锁内调用 context 实现或执行关闭。预览的原文预算和安全展示预算分别裁剪，任何省略都保留可见标记；展示不会持有被截掉的大字符串 backing storage。短屏确认保留当前选项并允许滚动目录，短屏预览允许滚过状态头部。


### 本地 Skills 当前边界

`extension/skill.Local.Load` 显式建立有界来源快照，使用 no-follow 描述符逐段打开、实际句柄类型校验和全文摘要检测文件变化。`domain.SkillCatalog` 只持有元数据；`skill_catalog` 是 project-stable 来源，正文通过用户附件或 `skill.read` 工具进入历史尾部。

宿主在 AgentLoop admission 前调用共享准备服务；队列保存原文与单份 `SkillAttachment`，预算同时计费。两家 Provider 保存独立文本块与 `input_metadata.skills` 关联，不从展示投影重建请求。Skill 的工具事实复用 `tool_call_ready`、`tool_execution_started`、`tool_call_result` 和原生 outputs；当前结果字段为 `skill_input` / `skill_metadata`。附件/结果在 Sync 后可见，恢复不访问来源文件。Runtime 从当前 turn 已提交输入派生短期资源资格，不写独立授权 record，结束 turn 即清除。

### 助手正文的流式 Markdown 投影

TUI 在 Update 中按消息身份准备布局。未提交文字按本地身份追加，持久化正文按 transcript cursor 读取。两种来源按已提交序号交接，查询等待期间保留可见行。阅读位置使用原文 UTF-8 偏移，缩放终端和切换原文时按新行宽定位。

Goldmark 的块起点与围栏闭合事件决定稳定前缀。未闭合围栏先展示代码，收口后由实例词法扫描器着色。扫描器和语法树共享 32768 个节点预算，布局、映射和高亮容量共享 2 MiB。主线、历史列表、正文与命令附页共享四个查询缓存位置。超出单页的持久化正文通过原文字节范围查询浏览。

展示状态由 TUI 实例拥有。继续采样时由 Provider 自身的 native history 还原请求。交互与缓存契约见 [Markdown transcript 规格](../../openspec/specs/tui/markdown-transcript/spec.md)。


### 命令与 Skill 输入补全

`internal/tui/completion.go`、`completion_state.go` 和 `completion_view.go` 分别负责不可变候选/范围/匹配、实例焦点状态及安全有界布局。`WithSkills` 使用冻结展示条目，过滤不可显式调用及非获胜来源，保留 manual-only Skill；模型 SkillCatalog 不承担手动补全目录职责。

Model 在 Update 中先准备补全占用，再准备 Markdown 与 hydration；主线高度扣除实际候选行数。composer 的内存编辑 revision 和字节光标标识解析缓存，不持有旧完整草稿。恢复草稿统一经 `restoreComposer` 按当前终端尺寸准备布局并失效解析缓存，不能把不同快照的相同编辑序号当作相同正文。Esc、paste 与 modal 返回有短 token 抑制，流式增量和 resize 不清除抑制。新状态与发布后不可变切片随 Model 值复制，未引入全局 registry、后台 worker 或公共协议。

名称替换使用既有 `composer.replace`；内置 Enter 交回共享 `submitComposer`，Skill Enter 与 Tab 只编辑。进入 Skills/Hooks 浏览时保留打开前草稿，返回不被尾部补全空格改写。只有正式提交才进入原有 Prepare/admission/Sync 链；UI 的候选、选择和布局不进入 Provider 请求、Session 或稳定缓存前缀。验证依据见 [补全设计](../../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/design.md) 与同 change 的验收记录。
