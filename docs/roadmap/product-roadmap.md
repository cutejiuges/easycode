# EasyCode 产品与工程 Roadmap

> 状态：初始规划  
> 更新时间：2026-09-18  
> 依赖设计：[`../architecture/overall-architecture.md`](../architecture/overall-architecture.md)

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
5. 无未解释的 dead code、占位兼容分支和永久 TODO。
6. 所有已有测试通过，格式化和 lint 无警告。
7. 新踩坑更新到 [`pitfall-log.md`](pitfall-log.md)，重要决策补充 ADR。

建议阶段门命令：

```text
make verify
```

## 3. 阶段总览

| 阶段 | 主题 | 用户结果 | 核心架构结果 |
|---|---|---|---|
| P0 | 工程与协议基线 | 可构建、可测试的空壳 CLI | Go module、依赖边界、事件协议、cache fingerprint 基线 |
| P1 | 双 Provider Kernel | 两种 provider 都能可靠流式对话 | 原生 item、StreamReducer、RequestCompiler、CachePlanner |
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
- 从第一天定义 `CachePlanVersion`、canonical serializer 和 segment fingerprint。
- 暂不引入独立 daemon/app-server，保留 `SessionService` 接口。

### 4.3 工作内容

- 初始化 Go package 结构和核心接口。
- 建立统一错误分类：配置、provider、stream、tool、permission、session、user cancellation。
- 定义 SessionId、ThreadId、TurnId、ItemId、CallId。
- 定义第一版 RuntimeCommand 和 RuntimeEvent envelope。
- 实现 cancellation token 和 shutdown 生命周期骨架。
- 建立配置加载层级：default、user、project、CLI override。
- 实现 secret/redaction 类型。
- 建立 canonical JSON 与 cache segment fingerprint 工具。
- 配置 gofmt、go vet、test、race、dependency/license 检查和 CI。
- 添加 architecture boundary 测试或依赖检查脚本。

### 4.4 交付物

- 可以执行 `easycode --version`、`easycode doctor` 的二进制。
- 可版本化的 protocol package。
- 最小配置解析和脱敏诊断。
- cache fingerprint 单元测试。
- CI 和本地质量门脚本。

### 4.5 验收标准

- 全工程构建、gofmt、go vet、单测和 race test 通过。
- protocol 序列化 snapshot 稳定，并包含 schema version。
- 相同 cache segment 输入重复运行 100 次得到完全相同的 canonical bytes/hash。
- 任意 secret 在 Debug、Display、错误链和配置诊断中均被脱敏。
- 依赖图不存在环，domain/protocol 不依赖 HTTP、数据库或 TUI。
- `doctor` 能报告配置来源但不显示 secret 值。

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

让 Anthropic Messages 和 OpenAI Responses 都能完成无工具的流式对话，并无损保留各自原生 item、推理数据、usage 和缓存字段。

### 5.2 架构选择

- 基于 Resty v3 封装小而强类型的 HTTP/SSE transport，避免官方模型 SDK 限制任意 `base_url`。
- 每个 provider 拥有独立 RequestCompiler、StreamReducer、NativeHistory、UsageParser 和 CachePlanner。
- 共享层只接收 RuntimeEvent，不解析 provider wire。
- Resty SSESource 与 provider reducer 分离；Resty 负责 SSE frame，provider 只处理 Anthropic/OpenAI event 状态。
- Provider capabilities 明确配置，unsupported 能力显式降级。

### 5.3 工作内容

#### Anthropic

- Messages request、system、tools 占位和 headers。
- content block start/delta/stop 状态机。
- text、thinking、signature、redacted thinking。
- message delta、stop reason、usage、stream idle timeout。
- `cache_control` plan 的基本编译。

#### OpenAI

- Responses request、instructions、input 和 headers。
- output item added/done、text delta、completed。
- reasoning summary、section、raw/encrypted content。
- commentary/final phase。
- response ID、usage、prompt cache key。

#### 共享基础设施

- retry 分类：连接前、无输出断开、已有输出断开、不可重试错误。
- stream cancellation 和 idle watchdog。
- mock server 和录制 fixture。
- capability override 与兼容服务商错误诊断。

### 5.4 交付物

- `anthropic.Provider` 和 `openai.Provider`，共同满足小型 `provider.Kernel` 接口。
- 两套 typed native item 和 stream reducer。
- request/response golden fixture 集。
- 最小 `easycode provider probe` 或 doctor provider 检查能力。

### 5.5 验收标准

- 两个 provider 都能通过自定义 base URL 完成流式文本输出。
- fixture 中 thinking signature 和 encrypted reasoning 可序列化、恢复并原样进入下一次请求。
- SSE fixture 在随机 chunk 边界下得到相同最终 item，至少覆盖空行、跨 UTF-8 边界、多个 event 和半包。
- 对未知 event/field 的策略明确：安全保留或可诊断忽略，不 panic。
- 相同请求输入产生相同 canonical request 和稳定 cache fingerprint。
- Anthropic cache marker 和 OpenAI prompt cache key 有 golden test。
- API key、完整 Authorization header 不进入任何测试 snapshot。

### 5.6 已知踩坑与规避

- **把 Anthropic block 映射成 OpenAI item 再处理**：两边 reducer 完全独立。
- **把 Resty SSE event 直接当成 provider 完成项**：Resty 只负责 frame，仍需独立 provider reducer 处理 item/block 生命周期。
- **收到部分文本后盲目重试**：已有输出重试必须防止重复 item 和未来工具副作用。
- **丢弃 opaque 字段**：signature/encrypted content 进入强类型 native envelope。
- **兼容服务不返回标准 usage**：usage 字段可 unknown，不用 0 伪装。
- **动态 header 改变缓存输入**：鉴权 header 不参与 prompt fingerprint。

### 5.7 退出条件

两套 provider 的 golden、chunk-boundary、取消、超时和无损 round-trip 测试全部通过。

## 6. P2：Session 与 Headless Agent Loop

### 6.1 目标

建立共享 turn 模板、上下文规划和 append-only session。用户可以用 `--print` 或 `--json` 完成多轮对话、停止进程并恢复。

### 6.2 架构选择

- `TurnRuntime<P>` 只模板化生命周期，不统一 provider-native history。
- JSONL 作为事实源，SQLite 只做可重建索引。
- RuntimeEvent 同时驱动 headless output 和 session projection。
- 高频 text delta 默认不持久化，item/turn boundary 持久化。
- ContextPlanner 输出有来源和稳定性标记的 segment，而不是直接拼 prompt。

### 6.3 工作内容

- 实现 user input -> sample -> assistant output -> stop 的 turn loop。
- 支持 queued/steered input 的基础语义。
- 实现 context source、稳定排序、token 估算接口。
- 实现 JSONL SessionMeta、native item、turn boundary 和 usage。
- 实现单 writer、flush、尾部半行修复和 schema version。
- 实现 SQLite session/thread/project 索引和重建。
- 实现 `--print`、`--json`、`--resume`、`--continue`。
- 实现用户中断和优雅 shutdown。
- 提供 transcript/debug log 分离。

### 6.4 交付物

- 可多轮运行的 headless coding chat。
- session JSONL 和 `state.sqlite`。
- session list/resume/continue CLI。
- 稳定 JSON event 输出协议。

### 6.5 验收标准

- 进程重启后可恢复两种 provider 的原生历史并继续对话。
- JSONL 尾部被截断时能保留此前合法记录并报告修复。
- 删除 SQLite 后可以从 JSONL 重建 session 列表和 thread metadata。
- `--json` 不混入人类可读日志，stdout/stderr 职责清晰。
- session 文件不包含 API key 或 Authorization header。
- 连续 turn 的稳定 segment fingerprint 不因 timestamp、session path 或日志配置改变。
- Ctrl+C/取消能结束当前 turn，flush 已完成 item，并且不损坏 session。

### 6.6 已知踩坑与规避

- **把 RuntimeEvent 全量写入 JSONL**：只记录恢复所需事实，避免 delta 膨胀。
- **SQLite 成为唯一事实源**：任何索引都必须可重建。
- **resume 时通过 UI message 重建 provider history**：使用 native item。
- **写入顺序与事件顺序不一致**：单 writer 分配单调 seq。
- **context source 每轮全量重排**：稳定层固定，动态 world state 放尾部或 diff。
- **取消导致最后一个完成 item 丢失**：item 完成先入 durable queue，再发布 terminal event。

### 6.7 退出条件

两种 provider 的多轮、重启恢复、取消、JSONL 修复和 SQLite 重建测试通过。

## 7. P3：Coding Tools 与安全执行

### 7.1 目标

形成最小可用 coding agent：读取、搜索、修改文件、应用 patch、执行命令，并具有权限、sandbox、并发和幂等保证。

### 7.2 架构选择

- ToolCapability、ToolFacade、ToolExecutor、ToolPolicy、ToolScheduler、ToolPresenter 分离。
- 相同能力允许 Anthropic/OpenAI 使用不同工具名称、schema 和流式 decoder。
- 只读并发，写操作和非并发安全工具独占。
- 工具执行 ledger 以 call ID 保证不重复副作用。
- 工具结果有统一大小预算，完整内容可写 artifact。

### 7.3 工作内容

- 实现 Read、Glob、Grep、Edit、Write、apply_patch。
- 实现 exec/write_stdin、后台进程、输出截断和取消。
- 实现 workspace root、路径规范化、symlink 和目录穿越检查。
- 实现 allow/ask/deny 权限策略和 approval event。
- 引入平台 sandbox 抽象，先完成当前开发平台实现和其他平台 fallback。
- 实现工具 schema 编译和稳定排序。
- 实现 Anthropic JSON Edit decoder 和 OpenAI freeform patch decoder。
- 实现 PatchDraftUpdated、ToolProgress 和有序 result collection。
- 实现 tool call/result pairing 修复与拒绝策略。

### 7.4 交付物

- 能完成真实仓库读取、修改和测试命令的 headless agent。
- 工具权限配置和交互式 approval 协议。
- patch/exec artifacts 和结构化工具事件。

### 7.5 验收标准

- 两种 provider 都通过“读文件 -> 修改 -> 执行测试 -> 总结”的端到端场景。
- 权限拒绝时没有文件、进程或网络副作用。
- 相同 call ID 被重放时不会重复执行写操作。
- 多个只读工具可并行，写工具独占，结果仍按模型调用顺序提交。
- patch delta 可以逐步形成稳定 diff；不完整或非法 patch 不执行。
- 大工具输出被截断，完整内容进入权限受控 artifact，模型获得可定位摘要。
- tool schema 顺序在文件系统枚举或注册顺序变化后仍保持稳定。
- tool call/output pairing 在 resume、取消和部分失败后仍合法。

### 7.6 已知踩坑与规避

- **把 render 方法塞入 ToolExecutor**：TUI 只消费 typed event/metadata。
- **所有 provider 共用一个工具 schema**：共享 capability/executor，facade 分开。
- **部分 JSON 到达就执行 Edit**：只有 ToolCallReady 后才能有副作用。
- **流重试重复运行 Bash/Edit**：execution ledger 先检查 call ID。
- **并行结果按完成顺序回传**：执行可并行，模型结果按调用顺序收集。
- **用字符串前缀判断路径是否在 workspace**：必须 canonicalize 并处理 symlink/平台差异。
- **工具结果无限进入上下文**：先制定 token/字符预算和 artifact 策略。

### 7.7 退出条件

基本 coding loop、安全策略、并发、有序结果、patch streaming 和幂等测试全部通过。

## 8. P4：缓存与上下文强化

### 8.1 目标

让长会话的 token 成本、首 token 延迟和上下文质量稳定可控，建立可量化的缓存命中、失效诊断和 compaction 体系。

### 8.2 架构选择

- ContextPlanner 正式输出分段 CachePlan。
- Built-in tools、project instructions、skill/plugin catalog 分别形成稳定层。
- world state 使用 full snapshot + diff，不重复注入整块动态内容。
- Anthropic 和 OpenAI 各自决定 cache marker/key/incremental request。
- compaction 替换模型窗口但不删除原始 transcript。
- 跨 provider 切换通过 compacted fork，而不是强行转换 thinking/reasoning。

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

### 9.1 目标

提供可日常使用的交互体验，在不耦合 provider/tool 实现的前提下，对齐 Claude Code 的输入、流式、thinking、diff、权限、任务和 session 操作。

### 9.2 架构选择

- Bubble Tea，Model/Update/View 只负责 RuntimeEvent projection，耗时操作通过 `tea.Cmd` 返回消息。
- TUI 是 RuntimeEvent 的 projection，不拥有业务状态真相。
- message cell 按语义类型渲染，provider 差异在 event metadata 中表达。
- 高频文本按帧合并，边界事件即时刷新。
- overlay、composer、history viewport 和 status 独立组件。

### 9.3 工作内容

- composer、多行输入、paste 保护、历史、补全和 slash command。
- assistant commentary/final、thinking/reasoning 折叠。
- tool use/result、并行工具组、Bash 实时输出。
- Edit/apply_patch 渐进 diff 和最终结果。
- permission、AskUser、MCP elicitation overlay。
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

### 10.2 架构选择

- HookEngine 使用强类型 request/outcome，外部 wire 可兼容 Claude。
- Skill catalog 只注入 metadata，正文按需加载。
- 每个 skill/resource 绑定 authority 和 source。
- Plugin 内部归一化为 PluginContribution；Claude/Codex manifest 只作为 importer。
- 扩展变更通过 revision/fingerprint 精确失效缓存。

### 10.3 工作内容

- 实现首批 hooks、matcher、timeout、async、block/rewrite/context/result。
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

### 11.3 工作内容

- 实现 Agent tool facade 和 AgentDefinition。
- 实现 spawn、foreground/background、list、wait、interrupt、follow-up。
- 实现 model/provider/effort/tool/permission/max-turns 继承与覆盖。
- 实现 fork history 截断、parent metadata 和 completion envelope。
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

完成跨平台兼容、性能和稳定性验证，建立配置/session 迁移、诊断、发布和兼容矩阵，使项目可以长期维护。

### 12.2 架构选择

- provider 兼容性以 capability profile 和 fixture 矩阵管理。
- session/protocol schema 采用向前可迁移的版本。
- 性能优化必须基于 benchmark、pprof 和结构化指标，不引入不可解释缓存。
- release artifact 为单二进制，平台特定 sandbox/PTY 能力显式报告。

### 12.3 工作内容

- macOS/Linux/Windows 进程、路径、PTY、权限和终端验证。
- 大 session、长输出、大仓库、慢 MCP 和多 subagent 压测。
- provider 故障注入：限流、5xx、半流、乱序、缺 usage、格式扩展。
- session migration、备份、repair 和 archive。
- log rotation、diagnostic bundle 和隐私审查。
- binary packaging、版本信息、changelog 和升级说明。
- 性能基准：启动时间、TTFT 开销、TUI FPS、内存、session append、cache planning。
- 安全审查：命令注入、路径穿越、symlink、hook/plugin trust、secret 泄漏。

### 12.4 交付物

- 支持平台的发布包。
- provider/OS/terminal 兼容矩阵。
- migration/repair/doctor 手册。
- 性能与安全基准报告。

### 12.5 验收标准

- 所有支持平台通过核心 E2E 和终端恢复测试。
- 旧版 session/config fixture 可以迁移或给出明确不可迁移诊断。
- 进程崩溃、磁盘满、数据库损坏、网络半断等故障不会静默损坏已有 transcript。
- 大 session list/resume 在性能预算内，JSONL 不需要每次全量解析。
- 日志轮转有效，diagnostic bundle 自动脱敏。
- 发布前全量测试、缓存回归、兼容矩阵和安全检查通过。

### 12.6 已知踩坑与规避

- **在 Linux 实现完成后才考虑 Windows 路径/进程语义**：从 ToolPolicy 和 Path 类型开始保留平台抽象。
- **session schema 只依赖默认 JSON 解码兼容**：使用显式 version 和 migration fixture。
- **为了性能引入跨 turn 隐式全局缓存**：缓存必须有 owner、revision、失效和指标。
- **诊断包收集完整请求/文件内容**：默认只收摘要，敏感内容必须显式 opt-in。
- **兼容服务商名称等同能力**：以实际 capability/fixture 为准。

### 12.7 退出条件

达到既定平台、性能、安全和迁移门槛后，形成第一个稳定发布版本。

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
- Provider/cache/session 协议发生破坏性变化时，需要 ADR 和 migration 计划。
- 新功能先归属现有边界；如果必须新建模块，要证明其职责和依赖方向。
- 不以“后续重构”为理由合入已知反模式。
- 已完成阶段发现回归时，相关阶段重新进入未通过状态，修复并补充回归测试后再关闭。
