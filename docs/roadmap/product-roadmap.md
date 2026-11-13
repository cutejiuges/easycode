# EasyCode 产品与工程 Roadmap

2026-11-05 已交付结构化用户提问：双 Provider 第九项工具、1–4 题单选/多选/说明/跳过/复核、独立回答控制、严格结果预算、原生持久化恢复、无交互失败关闭与只读历史。选择该切片是因为现有 Tool Loop、Session、AgentLoop 和 composer 已具备前置能力，可以直接补齐用户决策后继续任务的闭环。它不代表完整 P5，也不包含 Plan Mode、后台问题、自动代答或 MCP elicitation。验证入口为 `scripts/user-question-smoke.py` 和 `internal/app/questions_e2e_test.go`；平台范围沿用现有安全 opener，Darwin 完成真实 PTY 与命令链验收，其他平台未据交叉编译宣称原生支持。

`add-tool-transcript-and-result-budget` 初次交付七工具统一记录、搜索原文保存、按顺序冻结的 sample/turn 预算和 Claude 风格主线，目前记录和预算已覆盖九项工具；质量门及平台范围见该 change 的 [验收记录](../../openspec/changes/archive/2026-10-30-add-tool-transcript-and-result-budget/verification.md)，change 已归档。当前 Session/native/checkpoint 均无格式版本字段；headless JSONL 保留独立外部 v1 契约。

当前契约治理变更 `unify-current-contract-lifecycle` 收敛了固定 codec、prepared commit、宿主 owner 与副作用 admission。TUI、一次性 headless、stream-json 共用 AgentLoop；关闭期间排空事件，活动 transport 可显式取消，journal lease 保留至清理完成。Patch/Write grant 仅在批准 Sync 后生效，三种副作用工具统一验证结果关联并以 uncertain 收口错配结果。本次未引入迁移或兼容 reader，后续 P4/P6/P7/P8 能力及占位退出条件保持原有范围。

> 状态：持续演进（各阶段按纵向切片验收）
> 更新时间：2026-11-11
> 依赖设计：[`../architecture/overall-architecture.md`](../architecture/overall-architecture.md)
> Tool 专题设计：[`../architecture/tool-system.md`](../architecture/tool-system.md)

## 当前状态总览（2026-11-11）

实现与测试逐项核对见[当前能力清单](capability-status.md)。P0–P8 是并行推进的阶段目标，不以某个后期切片交付推定整个阶段完成。当前已交付九项模型工具、本地 Skills、同步 PreToolUse/PostToolUse/PostToolUseFailure、结构化提问、流式 Markdown、命令/Skill 首词补全、启动会话选择、Provider 缓存、手动/自动压缩、双 Provider 推理阅读及启动推理/display/输出配置；本地 Plugin 技能包及受限 importer 已交付；仍缺少运行中推理切换、跨 Provider fork、后台进程、插件可执行贡献/MCP、Subagent 与完整发布矩阵。

下文“工作内容/交付物/验收标准”保留阶段的完整目标；已完成状态以本总览、各节进度和当前能力清单为准。所有迁移、历史 reader 或兼容窗口必须先有用户明确要求及新 OpenSpec，不因路线图提到未来发布而预先授权。

## 1. Roadmap 使用方式

本路线图以“可工作的纵向切片”为单位推进，不以创建多少模块或复制多少参考代码衡量进度。

每个阶段必须同时完成：

- 用户可感知的能力。
- 对应的架构边界。
- 单元、golden、集成或 TUI 测试。
- 缓存影响评估。
- 文档、ADR 和踩坑记录。
- 阶段验收与全量自检。

阶段可以细分为多个迭代，但不得跳过退出条件。后续阶段发现基础设计不成立时，应回到对应阶段修正，而不是在上层堆叠兼容分支。

## 2. 全阶段质量门

以下条件适用于每一个阶段：

1. 不引入上帝类、上帝包、循环依赖或跨层捷径。
2. 新逻辑有单元测试；跨模块行为有集成测试；bug 修复有回归测试。
3. Provider、context、tool schema 或 skill/plugin catalog 的变化必须检查缓存稳定性。
4. API key、Authorization、cookie 和敏感 header 不出现在日志、错误、snapshot 和 fixture 中。
5. 无未解释的 dead code、占位兼容分支和永久 TODO；唯一临时占位必须位于 `AGENTS.md` exact allowlist，并带阶段、保留原因和后续 OpenSpec 启用或删除条件。
6. 所有已有测试通过，格式化和 lint 无警告；本地统一执行 `make verify`。
7. 新踩坑更新到 [`pitfall-log.md`](pitfall-log.md)，重要决策补充 ADR。

建议阶段门命令：

```text
make verify
```

当前 `make verify` 覆盖分支策略回归、`gofmt -l .`、`go vet ./...`、`go test ./...`、`go test -race ./...` 和固定版本的 `staticcheck`；其中全量测试包含 AST/import graph 架构守卫。如果新增 lint 规则，必须同步更新 Makefile、pre-commit 和本文。

## 3. 阶段总览

| 阶段 | 主题 | 用户结果 | 核心架构结果 |
|---|---|---|---|
| P0 | 工程与协议基线 | 可构建、可测试的空壳 CLI | Go module、依赖边界、事件协议、cache fingerprint 基线 |
| P1 | 双 Provider Kernel | 两种 provider 都能可靠流式对话 | Responses-first wire、原生 item、StreamReducer、RequestCompiler、CachePlanner、HistoryProjector |
| P2 | Session 与 Headless Agent Loop | `--print/--json` 可完成多轮会话 | turn 模板、JSONL、context、resume、错误恢复 |
| P3 | Coding Tools 与安全执行 | 能读取、搜索、修改文件并执行命令 | Tool 分层、权限、sandbox、并发、幂等、patch decoder |
| P4 | 缓存与上下文强化 | 长会话成本和延迟稳定可控 | segment cache plan、compaction、指标、跨 provider fork |
| P5 | Claude 风格 TUI | 可日常使用的交互式 coding agent | Bubble Tea projection、diff、permission、reasoning、session picker |
| P6 | 扩展系统 | 支持 hooks、skills、MCP 和本地插件 | authority、trust、热加载、manifest importer |
| P7 | Subagent | 支持前台/后台子代理和任务管理 | thread tree、fork、mailbox、预算和取消传播 |
| P8 | 发布强化 | 跨平台、可诊断、可迁移 | 性能、故障注入、兼容矩阵、发布与迁移体系 |

## 4. P0：工程与协议基线

### 4.1 目标

建立可持续演进的 Go module 和架构约束。在还没有真实 provider 功能时，就固定核心 ID、事件协议、依赖方向、测试方法和缓存指纹概念。

### 4.2 架构选择

- Go 1.24+ module，代码默认放在 `internal/`，只将需要对外稳定复用的 API 放入 `pkg/`。
- HTTP/SSE 使用 Resty v3，JSON 使用 Sonic，TUI 使用 Bubble Tea。
- `domain` 与 `protocol` 位于依赖图底部。
- CLI、TUI、provider、session、tools 不直接相互穿透。
- Runtime 使用 command/event 边界，不让 TUI 成为业务控制器。
- 使用唯一当前 CachePlan、固定 canonical serializer 与内容 fingerprint；内部对象不设置兼容版本门禁。
- 暂不引入独立 daemon/app-server，保留 `SessionService` 接口。

### 4.3 工作内容

- 初始化 Go package 结构和核心接口。
- 建立统一错误分类：配置、provider、stream、tool、permission、session、user cancellation。
- 定义 SessionId、ThreadId、TurnId、ItemId、CallId。
- 定义第一版 RuntimeCommand 和 RuntimeEvent envelope。
- 实现 cancellation token 和 shutdown 生命周期骨架。
- 当前主配置为用户/显式 JSON 文件加 Provider 环境变量覆盖；项目级主配置合并仍是后续选择，不与项目指令或扩展来源混用。
- 实现 secret/redaction 类型。
- 建立 canonical JSON 与 cache segment fingerprint 工具。
- 配置 gofmt、go vet、test、race、dependency/license 检查和 CI。
- 添加 architecture boundary 测试或依赖检查脚本。

### 4.4 交付物

- 已交付 `easycode --version`；`easycode doctor` 仍是未实现的诊断目标。
- 可版本化的 protocol package。
- 最小配置解析和脱敏诊断。
- cache fingerprint 单元测试。
- CI 和本地质量门脚本。

### 4.5 验收标准

- 全工程构建、gofmt、go vet、单测和 race test 通过。
- 内部 protocol 序列化 snapshot 稳定且无格式版本门禁；headless 外部协议单独严格校验 version。
- 相同 cache segment 输入重复运行 100 次得到完全相同的 canonical bytes/hash。
- 任意 secret 在 Debug、Display、错误链和配置诊断中均被脱敏。
- 依赖图不存在环，domain/protocol 不依赖 HTTP、数据库或 TUI。
- 未来 doctor 的目标是报告配置来源且不显示 secret；当前使用 CLI 安全错误和 `/context`、`/cache` 查询。

### 4.6 已知踩坑与规避

- **过早创建超级 core package**：P0 只放真正共享的领域类型，不把未来模块都塞进 core。
- **协议与内部对象共用一个大枚举**：外部协议需要版本化，内部状态可以独立演进。
- **使用 HashMap 直接序列化缓存输入**：统一 canonical serializer 和稳定排序。
- **错误类型携带完整请求**：错误只保存安全摘要和 correlation ID。
- **先写 CLI 再补边界**：所有入口从 SessionService/command-event 边界开始。

### 4.7 退出条件

只有在依赖边界、协议版本、secret 脱敏和 cache fingerprint 测试均稳定后进入 P1。

## 5. P1：双 Provider Kernel

### 5.1 目标

优先让 OpenAI Responses 完成无工具的流式对话，再接入 Anthropic Messages；两者最终都必须无损保留各自原生 item、推理数据、usage 和缓存字段。首版 OpenAI wire 只支持 Responses；Chat Completions 降级模式不属于 P1 的隐式兼容范围。

### 5.2 架构选择

- 基于 Resty v3 封装小而强类型的 HTTP/SSE transport，避免官方模型 SDK 限制任意 `base_url`。
- 每个 provider 拥有独立 RequestCompiler、StreamReducer、NativeHistory、UsageParser 和 CachePlanner。
- 每个 provider 还拥有独立 HistoryProjector，将 native history 单向投影为共享只读语义视图；token、resume、Hook 和 Subagent 不得各自解析 native item。
- 共享层只接收 RuntimeEvent，不解析 provider wire。
- Resty raw body、内部 SSE frame parser 与 provider reducer 分离；transport 负责 frame，provider 只处理 Anthropic/OpenAI event 状态。
- Provider capabilities 明确配置，unsupported 能力显式降级。

### 5.3 工作内容

#### 第一项交付：Resty SSE transport spike

- 先使用 Resty v3 `SSESource` 建立最小 transport contract，不先实现完整 RequestCompiler/Reducer。
- 使用 httptest/mock server 验证随机 chunk boundary、空行、半包、跨 UTF-8、多个 event、取消、idle timeout 和断线行为。
- 验证 SSE frame 层与 Provider event reducer 层的职责分离，并记录逐帧回调、背压和错误语义。
- 如果 RC 的逐帧消费不满足要求，立即在 `internal/provider/transport` 内切换到 Resty raw body + 自研 frame parser；不得等 Provider Kernel 中段再推迟决策。

#### 2026-09-28 纵向切片进展

- Transport spike 已确认采用 Resty raw body + 内部 SSE frame parser；`SSESource` 的默认 event buffer 和 frame/lifecycle 语义不作为项目契约。
- 已完成 OpenAI Responses text-only request、stream reducer、会话级 native history、事务提交、显式 terminal、两种 API prefix 的双轮回归，以及基础 TUI Chat（现已补充多行 composer）。
- 已完成 Anthropic Messages text-only request、indexed content-block reducer、会话级 native history、thinking/signature/redacted thinking 无损回放、显式 `message_stop` terminal、两种 API prefix 的双轮回归，并接入同一基础 TUI Chat。
- 已完成双 Provider 共享 `StreamEvent` 的封闭构造与纯内存验证；Runtime 在消费端协议错误时取消派生 context并同步排空到 producer 关闭，AgentLoop 只在前一 stream 清理完成后启动下一 turn。
- 已完成双 Provider 的 committed-only 文本 `HistoryProjector`：投影保留 turn 边界和可见文本，但不暴露 thinking/signature、redacted thinking、reasoning summary、encrypted content、phase、usage 或未知扩展，也不参与后续请求编译。
- Anthropic 当前声明 streaming、thinking-signature 原生保留与 structured tools（含 Edit/Write/Bash），OpenAI 当前声明 streaming、encrypted reasoning 原生保留、function tools（含 Write/exec_command）与 custom freeform apply_patch。P2 已落地 completed-sample usage、原生历史 footprint、JSONL Session、resume/headless 和 ContextPlanner；P3 已补充 Read/Glob/Grep/Patch/Write/Exec Tool Loop 与 approval，后续已交付通用工具主线/详情、CachePlanner、三态 usage/cache UI、prompt cache key 与可选 WebSocket `previous_response_id`；被动 thinking/summary 阅读已由 P5 独立切片交付；启动冻结的推理配置、Anthropic thinking 计量和同源请求证据已实现；成本、运行中推理切换和通用自动重试仍未实现。
- TUI 只消费 typed RuntimeEvent 和 ChatSession facade；已提供 Patch diff/Write、Exec approval overlay、命令进度和 Ctrl+O 历史列表与分页详情，多行 composer、字素编辑、显式 decoded paste 和有界历史；助手流式 Markdown 已接入，本地 `/cache`、`/context`、`/compact`、`/skills`、`/hooks`、`/plugins` 已接入，通用 permission 面板仍待实现；启动 session picker 已于 2026-11-04 接入。

#### OpenAI Responses（首要 Provider）

- Responses request、instructions、input 和 headers。
- output item added/done、text delta、completed。
- reasoning summary、section、raw/encrypted content。
- commentary/final phase。
- response ID、usage、prompt cache key。

#### Anthropic Messages

- Messages request、system、当前 tools schema 和 headers。
- content block start/delta/stop 状态机。
- text、thinking、signature、redacted thinking。
- message delta、stop reason、usage、stream idle timeout。
- `cache_control` plan 的基本编译。

#### 共享基础设施

- retry 分类：连接前、无输出断开、已有输出断开、不可重试错误。
- stream cancellation 和 idle watchdog。
- mock server 和录制 fixture。
- capability override 与兼容服务商错误诊断。

### 5.4 交付物

- 通过随机切块、取消、超时和断线 fixture 验证的 SSE transport contract，以及继续使用 SSESource 或切换 raw body parser 的明确结论。
- `anthropic.Provider` 和 `openai.Provider`，通过 Provider Factory 创建满足小型 `provider.Conversation` 接口的会话实例。
- 两套 typed native item 和 stream reducer。
- request/response golden fixture 集。
- `easycode provider probe` 或 doctor 检查属于尚未实现目标；当前没有这些子命令。

### 5.5 验收标准

- 两个 provider 都能通过自定义 base URL 完成流式文本输出。
- fixture 中 thinking signature 和 encrypted reasoning 可序列化、恢复并原样进入下一次请求。
- SSE fixture 在随机 chunk 边界下得到相同最终 item，至少覆盖空行、跨 UTF-8 边界、多个 event 和半包。
- 对未知 event/field 的策略明确：安全保留或可诊断忽略，不 panic。
- 相同请求输入产生相同 canonical request 和稳定 cache fingerprint。
- Anthropic cache marker 和 OpenAI prompt cache key 有 golden test。
- API key、完整 Authorization header 不进入任何测试 snapshot。
- P1 第一阶段的 SSE transport gate 先于 RequestCompiler/StreamReducer 开工通过；失败时只能进入 raw body parser 方案，不能带着未知 SSE 风险继续堆 Provider 逻辑。
- Anthropic/OpenAI native history 均能通过各自 HistoryProjector 产生语义视图；token、resume、Hook 和 Subagent fixture 不导入 Provider wire 类型。

### 5.6 已知踩坑与规避

- **把 Anthropic block 映射成 OpenAI item 再处理**：两边 reducer 完全独立。
- **把 Resty SSE event 直接当成 provider 完成项**：Resty 只负责 frame，仍需独立 provider reducer 处理 item/block 生命周期。
- **把 RC `SSESource` 当成稳定 frame 契约**：streaming 统一走 raw body 和内部有界 parser，升级 Resty 时必须重跑 chunk、取消、timeout 和 EOF fixture。
- **把 SSE 风险推迟到 Provider 中段**：transport spike 和随机切块 fixture 是 P1 第一项交付及第一道退出检查。
- **收到部分文本后盲目重试**：已有输出重试必须防止重复 item 和未来工具副作用。
- **丢弃 opaque 字段**：signature/encrypted content 进入强类型 native envelope。
- **兼容服务不返回标准 usage**：usage 字段可 unknown，不用 0 伪装。
- **动态 header 改变缓存输入**：鉴权 header 不参与 prompt fingerprint。

### 5.7 退出条件

两套 provider 的 golden、chunk-boundary、取消、超时和无损 round-trip 测试全部通过。

## 6. P2：Session 与 Headless Agent Loop

### 6.0 当前进度（2026-11-06）

已交付 root Session 的 append-only JSONL、typed 当前 codecs、UUIDv7 日期定位、跨进程连续 lease、验证后 repair/Sync、双 Provider 原生恢复、工具补偿和不可变 fixture。SQLite Catalog 可重建，仅保存 root-thread 元数据；支持 `--resume`、`--continue`、离线 `sessions list` 及启动 `resume` 元数据搜索/只读预览。没有 child-thread 执行、全文搜索、title/tag、project/worktree 聚合或实时索引。

TUI、一次性 `--print`/`--json` 和 stream-json 共用 AgentLoop。自动输入可在完整工具 outputs 后进入同一 turn 的下一 sample，strict-next 仍是独立 turn；原子取回、停止返还、EOF drain 与 shutdown 已实现。队列只在内存，成功 Sync 的输入与原生正文才是恢复事实。API key/base URL 不保存；Session metadata 保存创建 cwd，但恢复继续使用新进程当前 cwd，不回放旧动态环境。

ContextPlanner 固定六类来源：`provider_profile -> tool_catalog -> [project_instructions] -> [skill_catalog] -> committed_history -> current_input`，使用 canonical bytes、内容 revision 与本地 `byte_heuristic_v1`。九项真实工具纳入 schema/预算；配置/trust/真实命令和环境不污染稳定前缀。层级项目指令和本地 Skill catalog 在启动时冻结。Provider 缓存、三态 usage、`/cache`、`/context`、手动与阈值自动压缩已在 P4 切片交付，不能继续列为 P2 未实现。

P2 后续范围为更完整的会话检索/线程组织、费用与配额、后台索引和跨平台运行矩阵；全工具 artifact、Subagent 与跨 Provider fork 在对应阶段另行推进。没有 migration/legacy reader，不把尚未授权的兼容转换列为当前实现承诺。工程与平台证据分别见[能力清单](capability-status.md)、各 change 验收和根 AGENTS 规约。

### 6.1 目标

建立共享 turn 模板、上下文规划和 append-only session。用户可以用 `--print` 或 `--json` 完成多轮对话、停止进程并恢复。

### 6.2 架构选择

- Runtime 通过组合策略模板化生命周期，不统一 provider-native history。
- JSONL 作为事实源，SQLite 只做可重建索引。
- RuntimeEvent 驱动 TUI 与 headless 的只读投影；Session 事实由 Runtime 在发布 durable terminal 前独立提交，外部 JSONL 不反向写入 Session。
- 高频 text delta 默认不持久化，item/turn boundary 持久化。
- ContextPlanner 输出有来源和稳定性标记的 segment，而不是直接拼 prompt。
- 可见文本 token 估算统一消费 `HistoryProjector` 输出；opaque 原生内容只由对应 Provider 估算为不含正文的 HistoryFootprint，ContextPlanner 和 resume 都不得解析 native item。

### 6.3 工作内容

- 已实现 text user input、Read/Glob/Grep/Patch/Write 多 sample Tool Loop 与 final assistant output；Patch approval 已具备交互与 headless 失败关闭语义，整文件 Write 已支持完整 Read 后批准覆盖及原子创建，Darwin 前台命令已交付，same-turn steer 已通过独立输入 batch 接入。
- 已实现有界 FIFO queued follow-up、request identity 去重、定向 interrupt、EOF drain 与显式 shutdown；同轮自动输入已接入 P3 safe point。
- 已实现 `provider_profile`、可选 `project_instructions`、`committed_history`、`current_input` 的固定顺序、正交 lifecycle/cache stability、canonical segments、stable prefix、SemanticHistoryView 可见估算与 Provider-native footprint 覆盖合并；后续来源必须随真实消费者独立落地。
- 已实现 JSONL SessionMeta、native commit、`sample_usage` 和文本 turn boundary；费用与长期累计指标仍待实现；P4 缓存切片已增加有界的进程级来源失效解释。
- 已实现由同一 exclusive lease 覆盖 load/repair、Provider 恢复、续写和最终关闭的跨进程单 writer，及 `Sync`、尾部半行/未完成尾批修复、唯一当前无版本 envelope 与不可变 current fixture；旧开发 shape 失败关闭，未经用户明确指示不得引入版本转换或兼容设计。
- 已实现 SQLite Catalog 的 root thread 最小索引、全量前台 reconciliation 与损坏/不兼容数据库重建；project/worktree、title/tag/search、实时索引和 thread graph 仍待实现。
- 已实现 `--resume <thread-id>`、`--continue`、单 turn `--print`/JSONL v1 `--json`，以及独立 `stream-json` 双向控制模式的同进程多 turn。
- 已实现当前文本 Chat 的用户中断、依赖有序 shutdown 和中断 turn 补偿；后台任务的完整 shutdown 随对应能力补充。
- 提供 transcript/debug log 分离。
- 九项 Tool、RuntimeCommand、Skill/Hook 已有真实消费者、validator 与测试。仅 MCP、Subagent、telemetry 及预留 ItemID/CallID 仍在 AGENTS.md 精确 TODO allowlist；extension 根和 plugin 已由真实技能贡献替换并退出占位。

### 6.4 交付物

- 已交付可 resume/continue 的一次性 headless 文本 chat 与同进程 FIFO AgentLoop；Read/Glob/Grep/Patch/Write 已交付，Darwin 前台命令已交付，同轮追加、取回与停止返还已交付。
- 已交付 session JSONL 与可从 JSONL 重建的 `state.sqlite` Catalog。
- 已交付显式 resume 与兼容性精确匹配的 `--continue`；离线 `sessions list` 已交付，启动 `resume` 选择器已交付，支持元数据搜索与显式只读预览。
- 已交付一次性 thread/turn/text/failure JSONL v1 与独立 streaming control v1，成功 `turn.completed` 必含 normalized usage；独立 usage 更新、reasoning 与 tool 事件尚未交付。
- 已交付不新增 Session/RuntimeEvent vocabulary 的请求前 ContextPlan 和显式预算守卫；Provider cache marker/key、可选 WebSocket 增量、手动/阈值自动压缩与 context/cache UI 已交付；远程精确 token count 尚未实现。
- 已交付启动时层级项目指令发现、descriptor-bound 安全读取、确定性预算截断、双 Provider 临时注入和恢复隔离；规则热重载、全局用户指令、includes 与 `.claude/rules` 尚未交付。

### 6.5 验收标准

- 进程重启后可恢复两种 provider 的原生历史并继续对话。
- resume 可通过 HistoryProjector 重建用户可见历史，但续写请求仍只使用 native history。
- JSONL 尾部被截断时能保留此前合法记录并报告修复。
- 删除 SQLite 后可以从 JSONL 重建 session 列表和 thread metadata。
- `--json` 不混入人类可读日志，stdout/stderr 职责清晰。
- completed sample 的 raw usage 可随 native history 无损恢复，normalized usage 与 native commit 同 batch durable，headless completion 与该事实一致。
- session 文件不包含 API key 或 Authorization header。
- 连续 turn 的稳定 segment fingerprint 不因 timestamp、session path 或日志配置改变。
- 相同规范化项目指令在不同绝对 cwd 下产生相同快照和请求；修改项目指令只影响新进程的临时上下文与 project-stable fingerprint，不改写既有 Session/native history。
- 显式配置且完整已知的估算超过有效输入上限时，Runtime 在 Provider 网络副作用前 durable 失败；未配置窗口或 unknown 估算不伪装成超限。
- Ctrl+C/取消能结束当前 turn，flush 已完成 item，并且不损坏 session。

### 6.6 已知踩坑与规避

- **把 RuntimeEvent 全量写入 JSONL**：只记录恢复所需事实，避免 delta 膨胀。
- **SQLite 成为唯一事实源**：任何索引都必须可重建。
- **resume 时通过 UI message 重建 provider history**：使用 native item。
- **写入顺序与事件顺序不一致**：跨进程 lease 保护的单 writer 分配单调 seq；advisory lock 不能约束旧版或非协作进程，禁止新旧二进制同时写同一 thread。
- **context source 每轮全量重排**：稳定层固定，动态 world state 放尾部或 diff。
- **把项目指令写入 history 或 Session**：每轮从 Runtime 启动快照生成临时 Provider context，prepared commit 与恢复 codec 只处理真实用户项。
- **取消导致最后一个完成 item 丢失**：item 完成先入 durable queue，再发布 terminal event。

### 6.7 退出条件

两种 provider 的同进程 FIFO 多轮、重启恢复、取消、JSONL 修复、SQLite 重建、`--continue`、durable usage、层级项目指令、确定性 context planning/token estimator 和两套 headless 输出协议测试已通过；剩余会话组织、检索、费用/配额和平台验收仍按对应范围推进；不据局部切片宣布 P2 整体退出。

## 7. P3：Coding Tools 与安全执行

命令输出切片 `add-command-output-artifacts` 已接入双流私有前缀存储、当时七工具 catalog 中的 ReadArtifact（当前共九项）、durable descriptor 授权、完整命令历史和 Ctrl+O 列表/分页详情。默认预算为每流 16 MiB、调用预留 32 MiB、thread 256 MiB、root 1 GiB；后续 `add-tool-transcript-and-result-budget` 已统一工具主线与完整 transcript（当前九项），增加搜索保存及 sample/turn 累计预算，Session 当前格式不含版本字段。P3 尚未整体完成；PTY、后台命令、非 Darwin 命令执行和自动过期仍未实现。平台运行验证范围以 change 的最终验收记录为准。

### 7.1 目标

形成最小可用 coding agent：读取、搜索、修改文件、应用 patch、执行命令，并具有权限、sandbox、并发和幂等保证。

### 7.2 架构选择

- ToolCapability、ToolFacade、CatalogSnapshot、InputDecoder、ToolExecutor、ToolPolicy、ToolScheduler、ToolPresenter 和 ResultCodec 分离。
- 相同能力允许 Anthropic/OpenAI 使用不同工具名称、schema 和流式 decoder。
- 只读并发，写操作和非并发安全工具独占。
- 当前 sample 的 native commit、usage 和 ready calls 成功 `Sync` 后才允许副作用。
- execution ledger 保证同一 invocation 的自动执行至多一次；崩溃后的不确定外部结果失败关闭，不宣称通用 exactly-once。
- 工具turn恢复只做本地补偿：不重跑ready调用、不自动继续Provider sample；补齐原生call/output配对并关闭旧turn后，等待下一次真实用户输入。
- 完整结果、模型预览、artifact 和 UI 摘要分离；恢复复用已持久化的模型预览字节。
- Approval 与 SandboxPlan 分离，用户批准不能绕过内核能力边界。

### 7.3 推荐纵向切片

P3 不采用一个覆盖全部工具的长线 change。以下名称是推荐顺序，不代表 change 已创建；每项仍需独立执行 `propose -> review/confirm -> apply -> verify -> archive`：

当前状态：`add-read-tool-loop`、`add-search-tools-and-ordered-parallelism`、`add-approved-file-patch-flow`、`add-file-write-tool` 与 `add-command-execution` 已实现，提供 Read/Glob/Grep/单文件 Patch/Write/Darwin 前台 Exec、双 Provider 原生 wire、多 sample Runtime、最多 8 路只读并行、Patch/Write/Exec 独占屏障、按 call index durable/提交、崩溃补偿和本地-only resume。Patch 已实现 allow/ask/deny、Runtime 局部 session grant、headless 失败关闭和 Claude 风格 diff approval overlay；Write 已实现 create/overwrite 完整审批、独立 session grant、先前完整 Read 证据和私有目录树 no-replace 发布；Exec 始终只允许单次批准并在 headless 失败关闭。命令/搜索输出 artifact、ReadArtifact、当前九项工具主线与统一详情已接入；Read/Patch/Write 自动 artifact、PTY 和后台进程控制仍未实现。Linux adapter 已交叉编译，真实 Linux 文件系统测试因当前无 runner 尚未运行，详见 Tool 架构的验证范围。

Exec 取舍：Claude Code 2.1.88 的完整命令批准、默认 No、精简摘要与展开详情决定交互；Codex `c248f6d48b` 的 unified_exec、Seatbelt 和进程生命周期分层决定内核边界。当前固定 non-login `/bin/sh`、环境 allowlist、workspace 写入和禁用网络；非 Darwin 稳定返回 `unsupported_platform`。清理保证只覆盖原进程组，`setsid`/脱离组后代不属于后台控制能力；保留输出造成排空失败时返回不确定结果并 poison Runtime。Linux/Windows 已交叉构建，尚无原生运行验证，不代表 P8 跨平台支持已完成。

1. `add-read-tool-loop`（已实现）：以 Read 建立 catalog、双 Provider wire、多 sample Runtime、durable ready/result、ledger 和恢复的最小闭环。
2. `add-search-tools-and-ordered-parallelism`（已实现）：增加 Glob/Grep、连续只读并发和按调用顺序提交。
3. `add-approved-file-patch-flow`（已实现）：同时落地 allow/ask/deny、headless 失败关闭、Anthropic structured Edit、OpenAI custom freeform apply_patch、base evidence、完整 diff、原子替换和 approval 崩溃恢复。
4. `add-file-write-tool`（已实现）：增加整文件写入、原子替换、权限保留与冲突策略。
5. `add-command-execution`（已实现）：Darwin 前台 exec、进程 owner、取消/timeout、确定性输出预算、sandbox、单次批准与命令详情；双 Provider 的 Read → Patch → Exec → 总结及拒绝/中断/崩溃恢复均有回归。
6. `add-command-output-artifacts`（已实现）：命令输出保存、当前线程 ReadArtifact、完整历史查询与 Claude 风格列表/详情；保持冻结预览和原生恢复独立于对象存在性。
7. `add-background-process-control`：先定义受控 process handle、PTY/stdin、后台 admission、超时与唯一 shutdown owner，再增加 write_stdin；不得把当前前台命令脱离原组视为实现。
8. `add-tool-transcript-and-result-budget`（已实现，合并原工具预算与 UI 切片）：单调用 256 KiB（搜索 64 KiB）、sample 512 KiB、turn 2 MiB，顺序冻结、搜索原文保存、工具紧凑主线（当前九项）及四页缓存详情；不改写历史 native outputs，不实现上下文替换。
9. `add-same-turn-steering`（已实现）：活动 composer 自动追加，单 FIFO/strict 屏障；仅在完整 tool outputs 后、下一 sample 前注入独立原生输入。支持按 IDs 原子取回、停止返还、提交前预检、input-only 恢复，当前只维护唯一无版本 Session 结构，自动压缩直接更新其字段与 fixture。

首个切片选择 Read，是因为它能用最低副作用风险验证最关键的双 Provider、Session durable 和 call/output 配对边界，并让接口由真实消费者驱动；不先建立万能 registry、空 facade 或没有行为证据的完整框架。

### 7.4 工作内容

- 实现 Read、Glob、Grep、Edit、Write、apply_patch。
- 实现 exec/write_stdin、后台进程、输出截断和取消。
- 实现 workspace root、路径规范化、symlink 和目录穿越检查。
- 实现 allow/ask/deny 权限策略和 approval event。
- 引入实际 sandbox plan 与平台 adapter，P3 只完成已验证的 Darwin 前台命令隔离，缺少能力时失败关闭，禁止普通进程执行 fallback；完整 macOS/Linux/Windows sandbox 与兼容矩阵延后 P8。
- 实现工具 schema 编译和稳定排序。
- 实现 Anthropic JSON Edit decoder 和 OpenAI freeform patch decoder。
- 实现 PatchDraftUpdated、ToolProgress 和有序 result collection。
- 实现 tool call/result pairing 修复与拒绝策略。

本地 Skill 作为第八项模型工具、PreToolUse/PostToolUse/PostToolUseFailure 分别作为三项 mutation 的前置门禁、成功后检查与执行尝试失败诊断，已在 P6 切片接入；结构化 Question 为第九项模型工具。其余 Hooks、插件可执行贡献/MCP 与 Agent/Subagent 仍在 P6/P7 建立真实契约后接入。

### 7.5 交付物

- 能完成真实仓库读取、修改和测试命令的 agent；当前命令需要交互式单次批准，headless 预授权必须另行定义契约，不能由此目标推断为可用。
- 工具权限配置和交互式 approval 协议。
- patch/exec artifacts 和结构化工具事件。

### 7.6 验收标准

- 两种 provider 都通过“读文件 -> 修改 -> 执行测试 -> 总结”的端到端场景。
- 权限拒绝时没有文件、进程或网络副作用。
- 相同 invocation ID 被重放时不会重复执行写操作；Provider call ID 仍保持原生配对。
- 已 durable `execution_started` 但没有确定结果的调用恢复为 `outcome_uncertain`，且不会自动重跑。
- ready 但未批准的 Patch/Write 恢复为 `session_interrupted_before_approval`；已允许但未 started 的调用恢复为 `session_interrupted_before_execution`。恢复阶段零 preparer/executor 与 Provider 请求，不恢复 Read 观察或 grant，不清理私有暂存项。
- 多个只读工具可并行，写工具独占，结果仍按模型调用顺序提交。
- patch delta 可以逐步形成稳定 diff；不完整或非法 patch 不执行。
- 大工具输出按 capability 策略生成权限受控 artifact 和确定性预览；resume 后模型获得完全相同的预览字节。
- tool schema 顺序在文件系统枚举或注册顺序变化后仍保持稳定。
- tool call/output pairing 在 resume、取消和部分失败后仍合法。
- uninterrupted 与 restored 的下一次 Provider request canonical bytes、native item 顺序和 fingerprint 等价。

### 7.7 已知踩坑与规避

- **把 render 方法塞入 ToolExecutor**：TUI 只消费 typed event/metadata。
- **所有 provider 共用一个工具 schema**：共享 capability/executor，facade 分开。
- **部分 JSON 到达就执行 Edit**：只有 ToolCallReady 后才能有副作用。
- **把 call ID 当成 exactly-once 保证**：execution ledger 只保证自动执行至多一次；不确定结果不得自动重试。
- **模型 sample 未 durable 就运行工具**：native commit、usage 和 ready calls 先原子 `Sync`，再写 execution start 并执行。
- **并行结果按完成顺序回传**：执行可并行，模型结果按调用顺序收集。
- **用字符串前缀判断路径是否在 workspace**：必须 canonicalize 并处理 symlink/平台差异。
- **把 Approval 当作 Sandbox**：用户同意与内核强制能力分开判断。
- **resume 时重新截断工具结果**：持久化模型实际收到的预览字节和截断元数据。

### 7.8 退出条件

基本 coding loop、安全策略、并发、有序结果、patch streaming、至多一次自动执行、不确定结果失败关闭和恢复等价测试全部通过。当前前台 Exec 与命令输出 artifact 闭环已补齐，共享 sample/turn 输出预算与主线/详情已经交付；后台控制、渐进 patch delta 和其余 P3 目标仍未完成，不能据此宣布整个 P3 退出。

## 8. P4：缓存与上下文强化

`add-provider-prompt-caching` 已实现并归档，提供双 Provider 显式策略：Anthropic 5m/1h 断点、OpenAI thread key、默认 HTTP/SSE 与可选 WebSocket durable 原生增量、process 级 /cache 与 cache.query。内容/策略/请求/原生证据分开，未知计量保持未知，摘要与普通用量隔离；恢复、窗口、取消和关闭边界均回归。没有新增内部格式版本或迁移。验收见[矩阵](../../openspec/changes/archive/2026-11-03-add-provider-prompt-caching/verification.md)与 [ADR-0009](../architecture/adr/0009-provider-cache-and-transient-continuation.md)。跨 Provider fork、Plugin/MCP 动态目录、远端费用和累计遥测仍未实现；本地 Skill 目录已交付，不代表 P4 整体完成。


`add-durable-manual-compaction` 已实现手动压缩、独立上下文查询、双 Provider 有效窗口、durable checkpoint 和 stream-json 控制。Load 只读，完整 replay/Provider 校验后在同一 lease 下 Repair 与 Sync；Catalog 不修改 journal。摘要预算默认 4096，取消有独立身份，失败保留旧窗口；完整历史与工具授权边界保持。后续自动压缩状态见下文；其余 Hook 事件、远程 token count 和跨 Provider fork 未实现，P4 尚未整体完成。变更已归档，验收见[验收记录](../../openspec/changes/archive/2026-10-31-add-durable-manual-compaction/verification.md)。

`add-threshold-auto-compaction` 已实现阈值自动摘要：统一采样准备、未提交输入保真、工具输出闭合后切窗、可恢复失败继续、来源抑制与三次熔断、活动 turn 的 durable 恢复和宿主控制投影。Session/native/checkpoint 不包含格式版本或 canary；压缩来源不保存固定 prompt revision，Catalog 只校验实际结构与完整性。质量门结果见 [验收记录](../../openspec/changes/archive/2026-11-01-add-threshold-auto-compaction/verification.md)；change 已归档，不据此宣布 P4 整体完成。

### 8.1 目标

让长会话的 token 成本、首 token 延迟和上下文质量稳定可控，建立可量化的缓存命中、失效诊断和 compaction 体系。

### 8.2 架构选择

- ContextPlanner 正式输出分段 CachePlan。
- Built-in tools、project instructions、skill/plugin catalog 分别形成稳定层。
- world state 使用 full snapshot + diff，不重复注入整块动态内容。
- Anthropic 和 OpenAI 各自决定 cache marker/key/incremental request。
- compaction 替换模型窗口但不删除原始 transcript。
- 跨 provider 切换通过 compacted fork，而不是强行转换 thinking/reasoning。

上述 compacted cross-provider fork 是 P4 计划，当前尚未实现；现有 resume 只允许同一 Provider/wire 的原生恢复。

### 8.3 工作内容

- 完成 segment revision、fingerprint 和 invalidation reason。
- 实现 cache debug/doctor 输出和 metrics。
- 实现 Anthropic cache breakpoint/TTL/session stability。
- 实现 OpenAI prompt cache key 和 capability-gated previous response incremental。
- 实现 built-in 与 MCP/plugin tool 分区排序。
- 实现 tool output、附件和历史 token 预算。
- 实现手动 `/compact` 和阈值自动 compact。
- 实现 compact 前后 hooks 和重要 context 重新注入。
- 实现 provider/model compatibility hash。
- 实现跨 provider fork/summary 迁移。

### 8.4 交付物

- cache plan 诊断和每 turn cache metrics。
- manual/auto compaction。
- context window usage/status。
- 跨 provider fork 命令或内部能力。

### 8.5 验收标准

- 相同稳定输入的 request prefix 和 segment fingerprint 100% 一致。
- 只修改 cwd/git/world-state 时，稳定 segment 不变。
- 增加、删除或重排动态 MCP 工具时，built-in tool segment 不变。
- plugin/skill 扫描顺序变化不影响 catalog fingerprint。
- 服务端返回 cached token 时，TUI/JSON/日志中的指标一致；不返回时显示 unknown。
- compact 后 context token 显著下降，项目指令、活动 skill、计划和必要文件上下文仍存在。
- 连续多次 compact 不产生 orphan tool output 或重复 system/context block。
- 跨 provider 切换不会复制、伪造或泄漏 signature/encrypted reasoning。
- cache regression suite 覆盖所有失效矩阵条目。

### 8.6 已知踩坑与规避

- **缓存只看文本相同，不看序列化字节**：以最终 canonical wire prefix 为准。
- **HashMap/文件枚举导致 schema 顺序漂移**：所有输入显式 stable sort。
- **每轮把完整 git/env 放进 system prompt**：动态状态进入尾部 diff。
- **运行中切换 Anthropic TTL**：session 内锁定缓存策略。
- **prompt cache key 直接使用 API key/路径**：使用非敏感、稳定的随机 session/thread ID 派生值。
- **compaction summary 最后又附加大量旧文件**：所有 rehydrate 内容有独立预算。
- **把服务商未返回 cached token 当作零**：unknown 与 zero 分开。

### 8.7 退出条件

缓存失效矩阵、长会话、重复 compact 和跨 provider fork 的自动化验收全部通过。

## 9. P5：Claude 风格 TUI

### 9.0 当前进度（2026-11-08）

已交付多行/字素输入、同轮追加与返还、流式 Markdown 与代码/表格/原文阅读、紧凑工具主线及 Ctrl+O 分页详情、完整 Patch/Write/Exec 审批、结构化问卷、Hook 信任、context/cache 面板和启动 resume picker。当前本地命令为 `/cache`、`/context`、`/compact`、`/skills`、`/hooks`、`/plugins`，以及 Skill 显式入口。命令/Skill 首词补全和被动 thinking/summary 活动提示及 Ctrl+O 阅读已交付；启动推理配置与 /context 只读设置已实现；运行中推理切换、fork picker、后台任务 UI、cost/综合状态面板与其余命令尚未实现。

补全复用冻结 Skill 展示列表、字素编辑和既有命令提交；Tab 只填入、Skill Enter 只选择、Esc 优先关闭，粘贴不接受候选。验收覆盖双 Provider 原生请求/恢复/缓存等价、固定尺寸和 Darwin PTY，见 [验收记录](../../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/verification.md)。本切片不包含文件/参数值补全、正文 mentions 或动态目录。

### 9.1 目标

提供可日常使用的交互体验，在不耦合 provider/tool 实现的前提下，对齐 Claude Code 的输入、流式、thinking、diff、权限、任务和 session 操作。

### 9.2 架构选择

- Bubble Tea，Model/Update/View 只负责 RuntimeEvent projection，耗时操作通过 `tea.Cmd` 返回消息。
- TUI 是 RuntimeEvent 的 projection，不拥有业务状态真相。
- message cell 按语义类型渲染，provider 差异在 event metadata 中表达。
- 高频文本按帧合并，边界事件即时刷新。
- overlay、composer、history viewport 和 status 独立组件。
- live 模式消费 RuntimeEvent，resume/history replay 消费单向 TranscriptProjector 与宿主范围查询；SemanticHistoryView 继续排除推理，展示不能反向构造请求。

### 9.3 工作内容

2026-09-19 已先交付验证 RuntimeEvent 边界的薄切片；2026-10-11 又交付了与 Patch candidate 精确关联的 diff approval overlay，包含默认 No、一次/本 Runtime session 允许、拒绝反馈、小窗口滚动、按键隔离和固定 snapshot。它不改变 P5 的完整范围，2026-10-28 已补充多行 composer、字素编辑、显式 decoded paste、有界 recall 与提交拒绝恢复。本地 slash commands、通用工具主线/详情、Markdown 与启动 resume picker 已有各自切片；命令/Skill 首词补全由 `add-command-and-skill-completion` 交付；被动 thinking/summary 阅读已由 `add-reasoning-transcript` 交付；启动推理配置已由 `add-provider-reasoning-controls` 交付；运行中推理切换与 fork picker 仍是后续目标。

- composer、多行输入、paste 保护、历史、补全和 slash command。
- assistant commentary/final、thinking/reasoning 折叠。
- tool use/result、并行工具组、Bash 实时输出。
- Edit/apply_patch 渐进 diff 和最终结果。
- permission 与 AskUser overlay 已交付独立切片；MCP elicitation overlay 留待独立设计。
- Ctrl+C、Esc、Ctrl+D 和终端恢复。
- status line：model/provider/context/cache/cost/task。
- session resume/fork picker。
- terminal resize、窄屏、无色终端和 alternate-screen 策略。

### 9.4 交付物

- 默认 `easycode` 交互式 TUI。
- slash command 基线。
- TUI snapshot 和输入路由测试。

### 9.5 验收标准

- 两种 provider 的 text/reasoning/tool/patch 在同一 TUI 中正确展示。
- commentary 与 final phase 有不同语义，但缺失 phase 时有兼容回退。
- 原始不可展示 reasoning 不会被错误显示；允许展示的 summary/thinking 可折叠。
- 高频流式输出无明显闪烁，CPU 和内存保持在设定预算内。
- permission overlay 期间输入不会误发给模型。
- 异常退出、panic hook 或 Ctrl+C 后终端模式得到恢复。
- 固定终端尺寸 snapshot 覆盖核心 cell 和 overlay。
- 相同 native fixture 的 live stream 与 resume replay snapshot 在可见语义上等价。
- 主题、窗口尺寸和 UI 配置不影响 provider request/cache fingerprint。

### 9.6 已知踩坑与规避

- **TUI 直接订阅 provider SSE**：只订阅 RuntimeEvent。
- **每个 token 都立即完整重绘**：使用 coalescer 和脏区域/帧刷新。
- **工具自己返回 widget**：工具只返回 typed event metadata。
- **Esc/Ctrl+C 语义混乱**：区分关闭 overlay、中断 turn、退出程序。
- **终端恢复依赖正常 return**：设置 panic/error cleanup guard。
- **snapshot 包含时间、随机 ID**：渲染测试使用确定性 clock/ID。

### 9.7 退出条件

核心交互在支持的平台终端通过手工 smoke test，自动化 snapshot、输入路由和终端恢复测试通过。

## 10. P6：扩展系统

### 10.1 目标

支持 Claude 风格 hooks、渐进披露 skills、MCP 和本地 plugins，并保证来源权限、信任、缓存稳定和热加载安全。

当前已交付本地 Skills 首个切片：固定 user/project 根发现、严格静态 frontmatter、`/skills` 列表、`/name` / `$name` 显式附件、Skill 主文档/同来源资源读取、缓存元数据来源与双 Provider 原生恢复。同步 PreToolUse/PostToolUse/PostToolUseFailure command Hooks 已交付；本地 Plugin 技能包与 plugin skills 已由后续切片交付；其余 Hook 事件、MCP、system skills、path activation、allowed-tools、fork 与热加载仍未实现，不能将本切片视为 P6 退出。

### PostToolUse 同步检查与显式模型反馈

`add-post-tool-use-command-hooks` 交付三种真实成功 mutation 的后置检查。复用已有 FD 来源、只读/禁网沙箱与进程 owner，先信任后置计划、再批准原工具；真实结果与后置生命周期分别保存。默认 none 不披露正文；model 只接收严格 additionalContext，并与原结果共享有序预算 ticket。原 status/code、Provider native pairing 和稳定前缀保持，错误后不自动重试。

后置计划/运行/terminal 支持严格当前回放、部分前缀恢复和内容/预算拒伪造；恢复不重读配置/脚本/artifact，不给未启动项制造运行记录或新的模型提示。TUI 保留真实工具成功并刷新后置详情，headless 不增加外部事件。P6 仍未退出：其余事件、async、改写、插件可执行贡献/MCP 和热加载继续待实现。主规格已同步，change 已归档；验证见[验收记录](../../openspec/changes/archive/2026-11-08-add-post-tool-use-command-hooks/verification.md)与[归档核对](../../openspec/changes/archive/2026-11-08-add-post-tool-use-command-hooks/archive.md)。

### PostToolUseFailure 执行尝试失败诊断

本切片在既有后置生命周期内加入 failure 分支，优先补齐三种 mutation 的确定失败诊断。产品保持 Claude Code 的精确事件、默认拒绝、独立检查和显式反馈；内核保留双 Provider 原生历史、强类型边界、durable 执行尝试证据和零重跑恢复。正常非零退出不按命令文字推断测试失败，不做自动修复。双分支在候选前冻结/信任，单预算预留，事件选择与当前取消分离；失败/冲突主线和检查状态并列，中性计划通知不假定成功。详见[实现验收](../../openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/verification.md)。主规格已同步，change 已归档；最终契约、文档及证据核对见[归档记录](../../openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/archive.md)。P6 其余事件、插件可执行贡献/MCP 与热加载仍未交付。

### PreToolUse 首个切片的交付边界

`add-pre-tool-use-command-hooks` 完成 user/project 冻结来源、三种 mutation 字面匹配、默认拒绝的单项内存信任、Darwin 只读/禁网命令、30 秒总预算、`/hooks` 浏览/reset/审查及四类 durable facts。两个 Provider 复用相同 gate，并保留原生 function/custom/tool_result 配对、普通审批和缓存前缀。恢复不重跑脚本，先补偿旧事实再 Load 新来源；无匹配保持旧行为。Exec 与 Hook 共用进程资源 owner，Skills 与 Hooks 共用 FD 安全打开骨架。

P6 尚未退出：下面的其他 Hook 事件、async、rewrite/result、其他 context 扩展、插件可执行贡献、MCP 与热加载仍是后续计划。验收入口为 change 的 `verification.md`；实现测试分布于 `extension/hook`、`executor/process`、`runtime/hook*_test.go`、`session/hook*_test.go`、`app/hook*_test.go` 与 `tui/hooks_test.go`。

### 2026-11-12：本地 Plugin 技能包

`add-local-plugin-skills` 用可重复 `--plugin-dir` 提供进程级显式来源，支持 EasyCode、Claude、Codex JSON 和 Agent Plugins 的静态技能子集，复用 Skill 准备、工具、预算、权限和双 Provider 原生恢复。`/plugins` 只读列表/详情和 `/skills`、补全共享最终快照；限定名保留两家引用规则，Codex 副文件调用限制按更严格结果合并。

manifest ID 冲突先于扫描，原始枚举、候选、读取和合并目录均有界。根/manifest/正文/副文件变化拒绝新读取；来源关闭覆盖 pre-admission Prepare，超时不放弃 FD/清理 owner。恢复先收口旧事实再安装未来快照；无新 Session record、模型工具、内部版本或旧 reader。验证入口见[验收记录](../../openspec/changes/archive/2026-11-13-add-local-plugin-skills/verification.md)。主规格已同步，change 已归档；最终核对见[归档记录](../../openspec/changes/archive/2026-11-13-add-local-plugin-skills/archive.md)。本切片不表示 P6 退出，marketplace、安装/更新、插件脚本/Hook/MCP/Agent 贡献、热加载仍待独立设计。

### 10.2 架构选择

- HookEngine 使用强类型 request/outcome，外部 wire 可兼容 Claude。
- Stop hook 需要的 assistant 文本来自 HistoryProjector，不解析或拼接 Provider-native item。
- Skill catalog 只注入 metadata，正文按需加载。
- 每个 skill/resource 绑定 authority 和 source。
- Plugin 技能包在读取边界归一化为 extension.Contribution；四种 manifest 只作为受限 importer，不统一 Provider 对话协议。
- 扩展变更通过 revision/fingerprint 精确失效缓存。

### 10.3 工作内容

- 实现首批 hooks、matcher、timeout、async、block/rewrite/context/result。
- 实现受限 Hook 语义上下文，其中 last assistant message 由 HistoryProjector 提供。
- 实现 project/plugin hook trust review。
- 实现 system/user/project/plugin skills discovery 和 precedence。
- 实现 `$skill`、Skill tool、path activation、allowed-tools、fork context。
- 实现 MCP stdio 与目标网络 transport、tool discovery 和 lifecycle。
- 实现本地 plugin 安装/加载、manifest validation 和 contribution registry。
- 实现 Claude plugin importer 和 Codex plugin importer 的首批字段。
- 实现 file watcher、debounce、原子 snapshot swap 和错误回退。

### 10.4 交付物

- `/hooks`、`/skills`、`/plugins`、`/mcp` 管理和诊断界面。
- 本地 plugin 示例与兼容 fixture。
- extension trust 和 source diagnostics。

### 10.5 验收标准

- PreToolUse 能 block/rewrite，PostToolUse 能追加反馈，timeout 不会永久阻塞 turn。
- 未信任的项目 hook/plugin 不执行命令。
- 显式 skill 调用完整加载 SKILL.md；未使用 skill 只占用 metadata token。
- skill 不能绕过 authority 读取其他环境资源。
- plugin/skill 文件损坏时保留上一个合法 snapshot，并显示诊断。
- MCP server 失败不会使内置工具不可用。
- 扩展加载顺序稳定，catalog/tool fingerprint 可预测。
- manifest importer 不把 provider conversation wire 混入 extension model。

### 10.6 已知踩坑与规避

- **把所有 skill 正文预加载到 prompt**：严格渐进披露和 token budget。
- **文件 watcher 读到半写入文件**：debounce + parse temporary snapshot + atomic swap。
- **插件 hook 默认可信**：按 source 建立信任和权限边界。
- **MCP 工具插入内置工具中间**：built-in prefix 与动态后缀分区。
- **插件格式归一化过度**：只归一化 contribution，不抹平来源和能力差异。
- **hook 修改后无法解释缓存失效**：hook additional context 标记 source revision 和 segment。

### 10.7 退出条件

hook、skill、MCP、plugin 的信任、热加载、失败隔离和缓存回归测试通过。

## 11. P7：Subagent

### 11.1 目标

支持前台和后台 subagent、上下文 fork、任务状态、消息投递、等待和中断，同时控制并发、深度、预算和 session 可恢复性。

### 11.2 架构选择

- root session 下维护 thread tree 和共享 AgentControl。
- 每个 agent 独立 native history、JSONL writer 和 cancellation token。
- `none/full/last_n_turns` 显式决定上下文继承。
- parent-child 只通过 mailbox/command-event 通信，不共享可变 history。
- 并发、深度、token/time 预算由 root 统一控制。
- Completion envelope 的用户可见正文只从 child 的 HistoryProjector 输出提取，不解析 child Provider wire。

### 11.3 工作内容

- 实现 Agent tool facade 和 AgentDefinition。
- 实现 spawn、foreground/background、list、wait、interrupt、follow-up。
- 实现 model/provider/effort/tool/permission/max-turns 继承与覆盖。
- 实现 fork history 截断、parent metadata 和 completion envelope。
- 实现基于 HistoryProjector 的 completion 正文抽取，并保留 child thread/native history 的独立性。
- 实现后台完成通知和 task artifact。
- 实现 subagent hooks、usage/cost 归属和 session resume。
- 为未来 worktree/team mailbox 预留协议，但不提前实现空壳层。

### 11.4 交付物

- TUI/headless 可用 Agent 工具。
- subagent task panel 和 transcript。
- thread tree/session graph 查询能力。

### 11.5 验收标准

- foreground agent 返回结果后主 agent 可继续推理。
- background agent 不阻塞主 turn，并能可靠通知完成、失败或取消。
- full/last-N/none fork 的上下文内容符合定义，且不复制父线程不可继承状态。
- 超出最大并发或深度时明确拒绝，不泄漏 slot。
- parent 取消可按策略传播；child 独立失败不破坏 root session。
- 每个 child 可以独立 resume，SQLite thread graph 可从 JSONL/meta 重建。
- usage、cache 和工具副作用能归属到正确 thread/turn。

### 11.6 已知踩坑与规避

- **子代理共享父 ContextManager 可变引用**：只通过 snapshot/fork 继承。
- **只限制同时运行数，不限制已注册线程**：区分 active slot、resident thread 和总深度。
- **等待工具忙轮询**：使用 watch/notification，并为 wait 设置取消和超时。
- **后台结果写回父历史时破坏 tool pairing**：使用 completion envelope 和明确 call ID。
- **fork 复制父 usage hint/临时权限**：按继承策略过滤上下文。
- **不同 provider 的 child 直接继承 native history**：跨 provider 时走 compacted fork。

### 11.7 退出条件

并发、深度、fork、取消、resume、usage 归属和故障隔离的集成测试通过。

## 12. P8：发布强化

### 12.1 目标

完成跨平台兼容、性能和稳定性验证，建立诊断、发布和平台矩阵；兼容迁移只有在明确授权后才进入范围，使项目可以长期维护。

### 12.2 架构选择

- provider 兼容性以 capability profile 和 fixture 矩阵管理。
- Session/native/checkpoint 继续只有唯一当前无版本格式；外部 headless 单独版本化，未来兼容策略须明确授权后另经 OpenSpec。
- 性能优化必须基于 benchmark、pprof 和结构化指标，不引入不可解释缓存。
- release artifact 为单二进制，平台特定 sandbox/PTY 能力显式报告。
- P8 完成多平台 sandbox 强化；P3 capability flag 和安全 fallback 不等于所有平台都已沙箱化。

### 12.3 工作内容

- macOS/Linux/Windows 进程、路径、PTY、权限、sandbox 和终端验证。
- 在发布兼容矩阵记录 Sonic 在非 amd64/arm64 架构使用 fallback 实现的正确性与性能基准。
- 大 session、长输出、大仓库、慢 MCP 和多 subagent 压测。
- provider 故障注入：限流、5xx、半流、乱序、缺 usage、格式扩展。
- Session 备份、repair 和 archive 工具；migration 仅在用户明确要求后单独设计。
- log rotation、diagnostic bundle 和隐私审查。
- binary packaging、版本信息、changelog 和升级说明。
- 性能基准：启动时间、TTFT 开销、TUI FPS、内存、session append、cache planning。
- 安全审查：命令注入、路径穿越、symlink、hook/plugin trust、secret 泄漏。
- 为 Windows 等当前失败关闭的平台实现与 Unix descriptor/handle-bound opener 等价的配置与 Session 安全语义，并加入真实平台回归；在此之前不得回退到路径先检查后打开。

### 12.4 交付物

- 支持平台的发布包。
- provider/OS/terminal 兼容矩阵。
- 当前格式/repair/doctor 手册；迁移手册仅随获批的实际迁移能力交付。
- 性能与安全基准报告。

### 12.5 验收标准

- 所有支持平台通过核心 E2E 和终端恢复测试。
- 当前唯一格式严格校验，旧开发 shape 失败关闭；只有获批兼容范围才增加历史 fixture/迁移验收。
- 进程崩溃、磁盘满、数据库损坏、网络半断等故障不会静默损坏已有 transcript。
- 大 session list/resume 在性能预算内，JSONL 不需要每次全量解析。
- 日志轮转有效，diagnostic bundle 自动脱敏。
- 发布前全量测试、缓存回归、兼容矩阵和安全检查通过。

### 12.6 已知踩坑与规避

- **在 Linux 实现完成后才考虑 Windows 路径/进程语义**：从 ToolPolicy 和 Path 类型开始保留平台抽象。
- **session schema 只依赖默认 JSON 解码兼容**：当前字段结构与 kind decoder 必须 strict；只有用户明确要求兼容迁移后才能经 OpenSpec 增加相关实现和测试。
- **为了性能引入跨 turn 隐式全局缓存**：缓存必须有 owner、revision、失效和指标。
- **诊断包收集完整请求/文件内容**：默认只收摘要，敏感内容必须显式 opt-in。
- **兼容服务商名称等同能力**：以实际 capability/fixture 为准。

### 12.7 退出条件

达到既定平台、性能、安全及明确获批的兼容门槛后，形成第一个稳定发布版本。

## 13. 跨阶段验收场景

以下场景应从首次实现开始保留，并随着阶段扩展持续运行：

### 13.1 双 Provider Coding Loop

```text
用户要求修改一个文件
  -> 模型读取文件
  -> 生成 Edit/apply_patch
  -> 权限通过
  -> 修改文件
  -> 执行测试
  -> 返回 final answer
```

Anthropic 和 OpenAI 必须共享工具执行结果，但保留各自原生 wire 和历史。

### 13.2 缓存稳定性

```text
Turn 1 建立稳定前缀
  -> Turn 2 只增加用户消息
  -> Turn 3 修改 git 状态
  -> Turn 4 增加动态 MCP 工具
```

验收每一步的 segment fingerprint、首个失效点和 provider cache 字段符合失效矩阵。

### 13.3 崩溃恢复

```text
流式输出中断 / 工具完成后进程退出 / JSONL 尾部半行
  -> 重启
  -> repair
  -> resume
  -> 不重复执行副作用
```

### 13.4 Compaction 与 Provider 切换

```text
长对话达到阈值
  -> compact
  -> 保留项目指令和活动 skill
  -> 创建跨 provider fork
  -> 新 provider 正常继续
```

不得伪造或错误转换 signature/encrypted reasoning。

## 14. 版本定义建议

- `0.1.x`：P0-P3，headless 最小可用 coding agent。
- `0.2.x`：P4-P5，缓存/上下文成熟并具备日常可用 TUI。
- `0.3.x`：P6，扩展生态。
- `0.4.x`：P7，subagent。
- `1.0.0`：P8 完成，兼容、迁移、性能和安全门槛稳定。

版本号仅表达能力成熟度，不替代各阶段验收标准。

## 15. Roadmap 变更规则

- 阶段目标或退出条件改变时，必须说明原因和架构影响。
- Provider/cache/session 协议发生破坏性变化时，必须先创建 OpenSpec change，并补充 ADR 和 migration 计划。
- 新功能先归属现有边界；如果必须新建模块，要证明其职责和依赖方向。
- 不以“后续重构”为理由合入已知反模式。
- 已完成阶段发现回归时，相关阶段重新进入未通过状态，修复并补充回归测试后再关闭。

### 同轮输入参考与边界（2026-10-30）

本切片参考只读 Claude Code 2.1.88 和 Codex `6f8bf683c`：产品侧采用运行中输入、排队展示和取回编辑；内核侧采用强类型输入身份、有序命令与可测试提交边界。不会中断正在产生的 sample 来伪造请求变更，不承诺远端撤销，不增加后台进程、PTY、Subagent 或完整缓存策略。引用与取舍详见 `openspec/changes/archive/2026-10-30-add-same-turn-steering/design.md`；既有 Exec ADR/历史记录中的旧参考 revision 保持原语境。


### 2026-11-04：启动会话选择器

新增 `easycode resume [--all] [--config PATH]`：复用 Catalog 与连续 lease 恢复，采用 Claude Code 的搜索/范围/显式预览/分层返回交互，以及 Codex 的请求身份和过期结果隔离。选择这一能力是因为现有 Session、Catalog 和双 Provider 原生恢复已具备基础，能直接改善会话辨认与续接体验，同时避免提前扩张工具或扩展系统边界。

交付范围包含最近 200 条元数据搜索、跨目录默认返回确认、128 条/32 KiB 文本预览、离线严格原生校验和唯一 owner 清理。无标题/正文索引、自动模型切换、worktree 聚合、活动会话切换或 fork；P5 仍未整体完成。对应变更：`add-interactive-session-picker`。

2026-11-04 复核补强：修复选择器父取消接收窗口、预览预算和窄屏/失败展示，补充真实 journal 追加后恢复、双 Provider 校验中取消以及无变更/补偿后失败分类的直接断言。范围仍为现有启动选择器；跟踪变更 `harden-session-picker-review`。

### 2026-11-05：助手 Markdown 阅读

P5 的助手阅读切片覆盖流式消息、已提交主线和历史正文。PgUp/PgDn 在最近 50 项内回看和恢复跟随，Ctrl+O 可进入完整历史，r 在完整助手详情切换安全原文。超过 64 KiB 的正文按原文分页浏览。无限虚拟滚动、跨页富文本与可配置主题属于后续独立范围。

## 2026-11-08 推理展示切片

`add-reasoning-transcript` 接入双 Provider 被动推理阅读：主线仅保留当前活动/等待保存提示，Ctrl+O 查看允许正文或 unavailable；已保存推理不占主线 50 项窗口。Started/Delta/Completed、输入/sample 提交来源、纯内存 native ordinal 绑定与 typed failure resolution 保持阅读身份和真实保存状态。活动缓冲共享 64 KiB/50 项，全文按原文分页；同一高频事件分类在 publication 生产和 AgentLoop 消费两侧复用 committed 快照。

交付不包含主动 thinking/effort/budget/summary 请求、sequential_cutoff、raw reasoning、外部推理 JSON 事件或 fork。两家当前 native/Session/checkpoint 格式和恢复/缓存语义保持不变；该切片不代表完整 P5。验证见[归档验收记录](../../openspec/changes/archive/2026-11-08-add-reasoning-transcript/verification.md)，本地入口为 `scripts/reasoning-smoke.py`、`internal/app/reasoning_e2e_test.go` 以及两家 `reasoning_restore_test.go`；真实 PTY 证据限 Darwin。

### 2026-11-08 启动推理控制切片

`add-provider-reasoning-controls` 复用已有原生推理、缓存、工具和恢复，实现 JSON-only 冻结 thinking/display/effort/summary 与普通输出额度。摘要沿用策略并独立预检 manual budget；Anthropic thinking_tokens 进入五指标和唯一当前 native shape，raw/normalized 在 repair 前核对；请求计划统一 body/evidence/receipt。/context 显示来源并支持受限终端滚动。主规格已同步，change 已归档，验收矩阵见[归档验收记录](../../openspec/changes/archive/2026-11-11-add-provider-reasoning-controls/verification.md)。该切片不代表 P1/P5/P6 整体退出，运行中 /effort、远端模型发现、自动降级和额外推理模式仍未交付。

### 2026-11-11 启动推理控制全面回归与归档

两家 Provider 已在完整事件帧校验后才归并，拒绝重复成员覆盖计量或响应身份，保留合法未知扩展和连续 thinking 零值语义。新增 24 组真实 CLI、6 组异常流端到端与计量边界/fuzz 回归；八套 32 个 PTY 场景、5 轮乱序 race、421,051 次 fuzz 及清除测试缓存后的完整 `make verify` 均通过。实现、架构、主规格、实际 record vocabulary、测试路径与 pitfall 的归档核对见[归档记录](../../openspec/changes/archive/2026-11-11-add-provider-reasoning-controls/archive.md)，原始结果见[全面回归报告](../../openspec/changes/archive/2026-11-11-add-provider-reasoning-controls/regression-2026-11-11.md)。验证范围为 macOS 与本地模拟服务，不新增平台或真实模型支持承诺。
