## Context

动机和完整范围见 [proposal](proposal.md)。本文记录已确认方案与实施前接缝；最终实际路径、完成状态和验证证据见 [verification](verification.md)，不由设计目标推断完成。

### 当前接缝与明确冲突

| 当前位置 | 观察到的实现 | 本次接入点 |
|---|---|---|
| `internal/context/{planning,cache_plan}.go` | 五类有序来源、内容 canonical bytes、revision、稳定前缀 fingerprint；尚无差异/观测消费者 | 只抽取元数据，增加纯比较与有界诊断值，不将语义历史反向编译请求 |
| `internal/provider/{openai,anthropic}/request.go` | 完整原生请求；缓存字段不存在，golden 显式禁止这些延期字段 | 保留基础编译，增加 Provider 私有策略和请求覆盖 |
| 两家的 `user_input.go:compileSamplingRequest` | 输入预检与实际采样共用入口，先选有效窗口 | 预检生成相同完整请求但不改变运行状态 |
| 两家的 `compaction.go:compileCompactionRequest` | 普通请求编译后追加摘要指导及输出限制 | 分离请求构建与最后一次缓存覆盖，避免旧尾部标记流入摘要 |
| `internal/provider/provider.go` | Factory 未接收 thread 身份，PreparedSample/PreparedCompaction 控制 finalize/discard | 明确身份构造，增加不可变请求收据，finalize 后才建立优化基线 |
| `internal/provider/transport/{client,sse}.go` | Resty、无重试、16 MiB request/4 MiB event、活动请求取消与显式 Close | HTTP/SSE 保留；新增独立 WebSocket owner，复用 URL/安全策略而非复制 transport 大类 |
| `internal/runtime/{runtime,input_commit,compaction,agent_loop,compaction_loop}.go` | 单 worker 写事实，AgentLoop 单 owner 发布快照，查询不读取 staging | durable 观测、只读查询和安全 publication |
| `internal/tui/context.go`、`internal/headless/{stream_input,context_event}.go` | 本地命令与 stream-json 查询边界已存在 | 增加缓存查询，保持正文与控制面隔离 |

现有 OpenAI spec 禁止 previous_response_id 并将 cache key 标为 unsupported；Anthropic spec 禁止 cache_control。对应 delta **完整替换这两项请求/capability requirement**，不能只增加一条新 spec 而保留矛盾。现有请求说明仍遗漏 ReadArtifact，本次在所修改的完整 requirement 中按实际七工具目录核对。Session/native/checkpoint 数据结构不需要改变，恢复仍用唯一当前格式；不增加记录版本、optional 兼容事实或 Catalog 字段。

### 两个参考工程的证据与取舍

参考根目录按主 checkout 的相邻工程解析；本次实际只读目录为 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap` 与 `/Users/cutejiuge/IdeaProjects/codex`，并非 worktree 的物理相邻目录。它们不参与 EasyCode 构建。

- Claude Code：sourcemap 还原版本 **2.1.88**，参考仓库提交 `a8a678cb6244e6770e1e421767ff0987a1d95549`；该仓库不是官方原始工程结构。
- Codex：提交 **`6f8bf683cee83ed4d580a2e106b587d8451db936`**。历史文档的旧参考提交保留原语境，不将旧提交冒充本次证据。

| 参考文件/位置（相对参考根） | 具体证据 | 本次采用 / 不采用 |
|---|---|---|
| Claude `restored-src/src/services/api/claude.ts`：getCacheControl、should1hCacheTTL（约 360–440 行） | 标记与 TTL 分开，会话内锁定 eligibility/allowlist，避免动态变化打断缓存 | 采用不可变会话策略；以用户配置替代订阅、额度和 feature gate，不引入账号体系 |
| 同文件 userMessageToMessageParam/assistantMessageToMessageParam（约 588–675 行） | 在请求副本覆盖末块标记，thinking/redacted 不可显式标记，克隆防污染 | 采用请求覆盖与合法目标规则；EasyCode 还必须保留 Raw 的未知字段，不能只改 typed Text 字段 |
| 同文件 addCacheBreakpoints（约 3059–3120 行） | 消息尾部单标记与 skipCacheWrite 针对其服务端页策略 | 采用增长尾部可复用思想；不照搬其私有页策略，显式覆盖公共 API 的长 block 回看边界 |
| 同文件 buildSystemPromptBlocks（约 3213 行） | 有限标记预算和 system 前缀切分 | 不为缓存制造现有产品没有的 system 正文；本次标记已有工具、项目 context 与原生消息 |
| Claude `restored-src/src/utils/api.ts`（约 211–230 行） | 缓存基础 tool schema 与 per-request cache_control 覆盖分离 | 保持 Tool Catalog canonical bytes 不包含移动标记、TTL 或命令环境 |
| Claude `restored-src/src/tools.ts:assembleToolPool`（约 342–365 行） | 内置/MCP 各自排序后拼接，避免平铺排序破坏前缀 | 固定七工具已有顺序；不提前创建 MCP 空分区/registry，P6 接入时再扩展 |
| Claude `restored-src/src/services/api/promptCacheBreakDetection.ts`（约 245–650 行） | 请求前差异记录，响应后结合 cache_read；将 TTL/服务端驱逐视作可能原因 | 采用证据分层；不复制全局 map、正文闭包、内部遥测或 prompt diff 落盘，也不照搬 5%/2000-token 的私有告警阈值 |
| Codex `codex-rs/core/src/thread_manager.rs:PromptCacheKey`（约 276 行）与 `core/src/client.rs`（约 708 行） | key 有 session/custom/omit 策略，与 header identity 分离 | 采用 omit/thread 显式策略；本次不开放任意自定义 key、不发送 Codex 私有 session header |
| Codex `core/src/client.rs:responses_request_properties_mismatch`（约 425 行） | 全量列举非 input 属性；key、tools、model 等变化会重置 | 采用 typed 属性比较；新增字段必须在测试/编译期清单中做显式决定 |
| Codex `core/src/client.rs:get_incremental_items`（约 1635 行） | 以前次 input 加服务器 outputs 为基线，验证新 input 前缀后取 suffix | 保留原生顺序；不采用忽略模型可见 opaque 字段的比较，不通过 SemanticHistoryView 判断 |
| Codex `core/tests/suite/prompt_caching.rs`（约 550、820 行） | 配置变化后 key 稳定、请求前缀回归 | 增加 thread、目录、UI、工具执行状态与恢复矩阵 |
| Codex `core/tests/suite/client_websockets.rs`（约 1967、2207、2263、2318、2420、2475 行） | 增量、前缀缺失、指令变化、属性变化、错误后 full 等测试 | 采用同类确定性场景，另补 EasyCode Sync/finalize、prepared discard 和 ledger 不重跑 |
| Codex `core/tests/suite/websocket_fallback.rs` | 握手 426 回退、重试与 session-scoped fallback | 仅采用发送前安全回退；EasyCode 不复制宽泛模型请求重试、预热、auth recovery 或多路复用 |

### 公共 API 与依赖核查

核查日期：2026-11-01。参考源码解释产品选择，实际 wire 约束以公共文档及本地模拟服务契约为准：

- [Anthropic prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)：tools/system/messages 层次、最多四断点、20-block 回看、thinking 不能显式标记以及两种 TTL。本文只依赖这些基本规则，不内置模型价格/最小 token 表。
- [OpenAI prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)：key 是可选策略，不保证命中，路由收益具有模型差异。故不按模型名猜测或在 UI 承诺固定收益；本次不擅自新增新的 OpenAI TTL/显式 breakpoint wire。
- [OpenAI WebSocket mode](https://developers.openai.com/api/docs/guides/websocket-mode)：create 使用专属 envelope，store=false 的连接内状态不是可跨重启事实。本文只实现单 lane，full 恢复，不实现 stream_id、多路复用、warmup 或服务端 steering。
- [coder/websocket](https://github.com/coder/websocket)：选择其小型 context-aware API，封装在 transport。apply 时固定一个已审查发布版本及 checksum，核对依赖、license 和 close/read-limit 行为并运行本地边界测试；不得在运行时拉取代码。HTTP/SSE 继续使用 Resty，不改用官方模型 SDK。

## Goals / Non-Goals

**Goals:**

- 完成 proposal 列出的全部能力；HTTP 与 WebSocket 共用完整原生编译、reducer、工具与 durable 提交，不复制 AgentLoop。
- 明确三种 fingerprint 的用途和字节级恢复验证；缓存、压缩、续写可分别解释。
- 新能力在同一 change 中具备真实消费者、严格验证、golden 和生命周期测试，之后才声明 capability。
- 每个状态对象有单 owner、有界内存；序列化与渲染不发起 I/O。

**Non-Goals:**

- 不构建统一 Provider 扁平 Message、缓存代理、后台保温、订阅鉴权、遥测平台或未知能力猜测器。
- 不改变七工具 schema/权限，不接入 P6/P7 的扩展占位；不实现多 WebSocket lane、并行样本、WebSocket 中途服务端注入、连接预热或增量持久化游标。
- 不建立 Session 历史兼容/迁移体系，不改变 store=false，不承诺实际远端缓存命中率或确定费用。

## Decisions

### D1. 配置独立，默认请求保持现状

| JSON 字段 | 允许值 | 默认 | 最终 family 限制 |
|---|---|---|---|
| anthropic_prompt_cache | disabled / ephemeral_5m / ephemeral_1h | disabled | 非默认仅 anthropic |
| openai_prompt_cache_key | omit / thread | omit | 非默认仅 openai |
| openai_transport | http_sse / websocket_auto | http_sse | 非默认仅 openai |

在 `internal/config/config.go` 的 strict object 解码中加入有界枚举，既有环境变量覆盖 Provider 后再校验组合。无关 family 的默认值允许，非默认失败；不新增 env/CLI 开关。启用 1h 是显式付费策略选择，配置帮助说明可能存在更高写入费用，不加入价格表或交互阻塞确认。

三个策略分开，允许 OpenAI 只设 key、只用 WebSocket 或两者同时启用；websocket_auto 自身允许合格时增量，不再增加含糊的第二个 bool。关闭缓存不能控制服务端自身的隐式缓存，UI 必须表述为本客户端未发送显式缓存字段。

考虑过统一 `cache_enabled`：无法表达两家不同机制，且误将 key、TTL、transport 当成同一能力，因此不用。

### D2. 显式绑定身份，不添加持久化字段

调整 `provider.Factory` 的唯一当前方法为 `NewConversation(domain.ThreadID) (Conversation, error)` 与 `RestoreConversation(domain.ThreadID, []RestoreRecord) (Conversation, error)`，二者保持纯内存。`app/session_service.go` 在已有 create 身份生成后、journal 创建前传入 ThreadID；resume 在现有 lease、replay/config 验证后传入已验证身份。更新两个实现与所有测试 doubles，不保留旧重载或 optional identity。

OpenAI thread key 固定为 `easycode:` 加 `SHA256("easycode/prompt-cache/thread" + NUL + canonical ThreadID)` 的小写十六进制；域分隔字符串是用途命名空间，不是格式版本路由。key 只留在本次请求与会话内存，不输出到诊断，也不写 Session。Anthropic 接收并验证同一身份契约，但不会将身份注入请求内容。

恢复使用当前显式缓存配置；同配置保持 bytes 等价。用户有意改变策略允许改变最终请求，不推断或重放上一进程的配置。由于未保存旧策略，恢复时诊断为 restored_baseline，而不是编造一次跨进程策略变化。现有 family/wire/model 的 resume 匹配仍保留。

### D3. 三类 fingerprint 与数据流分开

1. **内容 fingerprint**：复用 ContextPlan 分段、source revision 和 stable-prefix 算法。ThreadID、TTL、key、连接、时间和 UI 不进入该算法。
2. **策略 fingerprint**：对封闭、非敏感的策略枚举做 canonical hash，不包含真实 key、URL 或 secret。
3. **请求 fingerprint**：对 Provider 最终完整逻辑 canonical JSON 做 hash，包含实际可选缓存字段；用于精确请求验证。WebSocket full/incremental 的 transport envelope 另有 wire golden，不冒充相同字节。

```text
Native history ----> Provider full request compiler ----> final cache overlay
Catalog ----------->              |                              |
Project snapshot -->              |                              v
                                  |                    full canonical request
ContextPlanner ----> metadata ----+                              |
       |                                                         v
       v                                            HTTP SSE / WS full or delta
content comparison                                               |
       |                                                         v
       +---- Runtime frozen inputs <---- prepared request receipt + normalized usage
                         |
                  durable Sync + finalize
                         |
                  immutable publication
                         |
                 AgentLoop query snapshot
                         |
                    TUI / headless
```

诊断模型名保持有界：长度超过 512 bytes、含控制字符/非法 UTF-8 或具有 Unix/Windows 绝对路径形状时，只显示 `sha256:<模型值摘要>`；不得为诊断改变实际请求 model，或令原本合法模型配置因显示限制而失败。严格快照 validator 拒绝这些原始不安全标签。

规划内容 fingerprint 不是原生 opaque 内容的完整证明。Provider 请求收据另带有效 native input 的 digest 及 first/append/replaced/boundary 关系；基线是前次完整 input 加 durable outputs，判断对 HTTP 同样执行。正常历史追加显示 history_appended，不将正常每轮增长误报为旧前缀失效；规划元数据相同而 native digest 不同时显示 native_context_changed。

Provider 不读取 ContextPlan 的语义正文，不依赖 `context` 根包来重建请求；Runtime 冻结上下文元数据，Provider 只返回自己实际编译/发送的请求收据。比较器位于 `context`，只接受不含正文的强类型值；需要跨包复用的枚举和值下沉 `domain`，不在每层复制相同语义的字符串 registry。

### D4. Anthropic 断点选择与 Raw 覆盖

先构建最终原生请求，包括项目临时 message、有效窗口、摘要专属后缀和 tool_choice/output 限制；然后调用纯私有 cache planner，最后 canonical 编码。所有调用入口包括候选预检走同一流程；状态变更只发生在实际请求提交后。

普通请求的候选断点，按以下规则确定并按真实 wire 顺序去重：

1. 最后一个真实内置工具定义：保护工具前缀；当前目录只有七项能力，不制造未来动态工具。
2. 非空项目指令 message 的最后合法 block：保护项目稳定内容。
3. 最后一个完成普通 sample **之前**的可复用请求边界：从 native entry 顺序推导，不使用进程内“上次 marker index”。若该位置距尾部至少 20 blocks，添加历史锚点以扩展回看；若位置不可标记，向前寻找最后合法 block。
4. 当前请求可复用内容的最后合法 block：普通采样包含当前已 durable 输入/tool outputs；摘要排除仅本次摘要指导后缀。无任何合法目标则省略该断点，不生成空消息。

最多四个；空项目或重合目标自然减少数量。工具定义与 text/tool_use/tool_result 是当前允许的显式目标；thinking、redacted、未知类型永不覆盖。候选选择只取决于当前窗口、请求 purpose 与固定策略，故重启和恢复不改变断点。压缩后的 summary-native 可作为现有文本目标，不复制旧历史锚点。

现有 `NativeItem.MarshalJSON` 在 Raw 非空时直接返回原 bytes，因此不能仅为 typed item 加一个字段。使用 Provider 私有 request block 包装：保留原始 native block；无标记直接委托原序列化，启用策略后，所有请求 block 均经固定 codec 检查顶层成员，已有 cache_control 即拒绝（包括未当选断点的块），防止隐藏标记突破数量或混用 TTL；当选块添加唯一 typed cache_control 后 canonical 编码，其他块继续返回原 bytes。受控 RawMessage 只留在 Provider 边界；未知成员完整保留，不传播 map[string]any。持久化对象永不带上该包装。Tool schema 同样通过请求 DTO 覆盖，不改变 FacadeView。

考虑过修改 NativeItem 持久化 shape 或直接设置 Raw=nil：前者污染事实源，后者丢未知扩展，都拒绝。也不引入自动顶层 cache_control 与显式标记叠加的第二套机制。

### D5. 请求收据与 prepared 提交共用现有骨架

在 `domain` 定义纯值请求收据，由 `provider` 的唯一当前 prepared 契约强制携带并严格验证，包含 purpose、有效 native input digest/前缀关系、完整请求 fingerprint、有效策略摘要、transport/full-or-incremental、封闭 reset/fallback 原因及 Anthropic 断点数等有界信息；不包含正文或 key。`PreparedSample` 与 `PreparedCompaction` 都必须携带有效收据，包括默认 disabled/full 模式，不能用 optional reader 兼容旧构造。Runtime 在 append 前验证收据与冻结请求、family/purpose 的关联，和 envelope/usage 一起复制。

收据在实际 transport 选择后冻结，纯预检只生成计划不发布观测。普通样本 finalize 在同一内存提交中建立 native history 与合格的 WebSocket 候选基线；discard 清除该候选。Runtime 随后以同一 sample 的冻结 ContextPlan 元数据、收据和 normalized usage 调用纯诊断函数，并经既有 publication 交给 AgentLoop。普通采样与摘要的适配各有明确入口，共享 prepared resolution，不扩张成统一大 executor。

诊断输入合法性必须在 durable 写入前已完整检查，保证 finalize 后的纯组装不会引入一个“日志失败导致已提交任务失败”的新副作用。最终 sample 提交入口使用统一 discard 退出守卫，覆盖收据验证及计量聚合等 append 前失败；成功 finalize 后守卫不再执行回调，丢弃仍由现有单次 resolution 控制。最近成功 turn 只在现有 turn_completed durable/finalize 后更新；中间 sample 已 durable 而后续工具失败时，保留 sample 观测，不伪造成功 turn 聚合。

失败尝试只显示既有安全错误分类与失败 activity，不制造成功收据/比例；有效 transport 显示为最近确认的模式，尚未有成功观测时明确未观测，不能把 configured websocket_auto 等同连接已成功。摘要合法响应但候选被拒绝时，原有 failed compaction usage/provenance 仍按旧契约保存，不混入成功缓存统计。

### D6. WebSocket transport 独立 owner 与共享 reducer

新增 `internal/provider/transport/websocket.go` 和对应本地测试；`github.com/coder/websocket` 的生产引用只允许在 transport；本地模拟服务测试可直接使用该库生成标准帧，不向应用生产接口泄漏库类型。`provider/openai` 增加小型 Responses transport 选择策略，持有 HTTP adapter 和单连接 WS adapter；它们输入是已编译请求、输出是有界原始 JSON 事件，业务 reducer 不接触库连接对象。无需 Anthropic 接入 WS。

URL 从现有已校验 http/https base 派生，保留 path prefix 后追加 responses；握手转 ws/wss，不引入新 endpoint 配置。禁用重定向和压缩，限制请求 16 MiB、重组事件 4 MiB；只接受合法 UTF-8 文本 JSON，库负责 RFC frame 分片，固定 codec 负责 JSON。握手和单次写期限 30 秒；外层 context 已取消时直接取消，不能把用户取消当成可回退连接超时；读空闲使用现有配置（默认 5 分钟），通过可注入 deadline/timer 测试，不能靠真实等待证明。

HTTP 专属 stream 字段不进入 response.create；不透传 background、未知 extra body、Codex 私有 beta/header、stream_id 或 account metadata。鉴权仍只来自现有 secret，错误只保留安全类别和 status。

连接由 Conversation 生命周期拥有，首次实际 Stream 时显式打开，不在 New 中启动。每次实际 sample 的 stream owner 是唯一 reader/writer；同一 Conversation 串行，请求期间才启动受控 consumer。完成 terminal 后该 sample 的输出 channel 关闭并 join，但底层 socket可保持空闲；空闲不启动 reader/heartbeat goroutine。后续读到不属于本次 response 的迟到/重复帧必须按 identity/order 失败关闭，不将其归为新的用户响应。持久连接不能用“socket EOF”代表每个 sample 的完成，sample 边界由经过验证的原生 terminal 定义。

Provider 持有纯内存构造的 WS client；实际 Open 时将连接登记到该 client 的实例 registry，关闭时移除。Conversation 持有自己的连接与增量状态，Provider.Close 只负责汇总取消/关闭并等待已登记连接 owner 的 done，不在 factory 全局共享基线。这样现有 app 的 forceCloseProvider 也能解除 WS 阻塞；registry 不保存调用方 Context，也不在锁内执行回调或 Close。两家原生前缀诊断只持有一个有上限的前次完整请求基线及其完成输出，属于 Provider 私有状态，不进入 64 KiB 诊断快照。

owner 的 mutex 只执行有界状态转换，不包住 dial/read/write/Close/channel。close 在活动请求时先强制关闭实际 socket，原 stream owner 排空/退出后关闭持有的 done；空闲关闭由 Conversation 自己同步完成。重复 Close 等待同一完成信号并保留最终错误。不得每次 Wait 另起 helper goroutine；app 正常关闭顺序明确调整为 Session shutdown/join、Provider.Close（关闭空闲及活动 socket 并 join transport）、writer Sync/Close、其余 workspace/repository；超时路径先 forceCloseProvider 再等待同一 Session owner。writer/lease 在 transport 最终完成前不释放，不为此另建 cleanup owner。

### D7. 增量基线与错误状态机

```text
idle_full --> connecting --> full_in_flight --> prepared --> durable_finalize
    ^             |               |               |                 |
    |             v               v               v                 v
    |        http_fallback    failed/reset     discard/reset    reusable_baseline
    |                                                               |
    +---------- prefix/config/reset <----- eligibility check <-------+
                                                |
                                                v
                                       incremental_in_flight
```

基线绑定 socket generation、最后 durable response ID、其完整 input、完成 outputs 和完整非 input 属性。最多保存一个前次请求与完成输出，受现有请求/native 上限约束；只保留不可变拷贝，不持有调用方 Context 或可变历史别名。新请求必须精确满足 `previous_input ++ previous_outputs` 为其 input 前缀；只比较 Provider 原生 canonical 字段，不通过文本或 call ID 猜测。诊断的 append 关系复用同一纯前缀判定，但不要求 socket 存活；WS 资格在它之外另加连接与 durable response 条件。剩余 suffix 包含已 durable tool outputs 与 steering 用户输入，原样有序发送。比较允许空 suffix 但绝不自行发起无宿主驱动的新采样。

非 input 属性比较覆盖 model、tools、tool_choice、输出约束、include、store、key 及后续真实字段；新增字段须显式纳入比较测试，不能默默遗忘。线程已绑定，不将 invocation/input 身份注入模型内容。前缀变化、属性变化和 restored_history 等原因使用封闭 enum。

response.created/completed 本身不建立基线；必须等 Runtime 成功 Sync 后 prepared finalize。若 Sync 或 finalize 失败，Session poisoned、关闭连接且不再请求；若 prepared 被 discard 或请求在写后失败，清除连接基线。响应未完成不得使用其 response ID。

摘要始终 full 且独立：进入摘要前关闭普通 socket 并清除基线，摘要可使用一次性 WS 连接或已经安全固定的 HTTP 路径，其 owner 同样可取消；摘要连接在该操作结束时关闭，不成为普通基线。无论摘要是否成功，下一普通采样 full。这样不会将未进入普通历史的摘要生成链误用于正文续写。

### D8. 回退只发生在确认未发送生成请求时

| 边界/结果 | 当前 sample | 后续状态 |
|---|---|---|
| 握手 404/405/426，尚未写 create | 关闭握手资源，恰好一次 HTTP 请求 | 本 Conversation 固定 HTTP，原因 handshake_unsupported |
| 明确拨号不可用或连接超时，未写 create；非 TLS/鉴权/redirect | 恰好一次 HTTP 请求 | 固定 HTTP，原因 connect_unavailable |
| 握手 401/403、TLS 验证、重定向/策略错误 | 安全错误，无 HTTP 回退 | 清理；不自动重试 |
| create 写入开始、部分写或返回结果未知 | 唯一失败终态，不重发 | 关闭 socket，清除基线；下一显式请求 full |
| 接收后 response.failed/error/previous_response_not_found | 当前失败，不重发 | 清除基线；下一显式请求 full |
| 用户取消、shutdown、损坏事件、超限、断线 | 取消并 join 唯一 owner | 清除基线，保持已 durable 原生事实 |

边界线性化点取 **开始写 response.create**，不是 write 返回成功：之后无法证明远端未接收。错误必须由 dial/HTTP status/受控服务端 error code 形成 typed 分类，不匹配 error.Error()。即便远端明确 previous_response_not_found，本切片也不在原 sample 自动补发 full；保守规则可验证，不继承参考工程更宽泛的 retry policy。无故障状态下的正常不合格增量可直接 full，不属于重试。

### D9. 诊断保存证据，不保存 prompt

新增 `internal/context/cache_diagnostics.go` 的纯值与比较器。输入只有固定五来源的 kind/revision/fingerprint、策略摘要、request receipt 和 normalized usage；不得持有 source canonical 内容、prompt 闭包、路径或原生 item。按固定顺序比较 added/removed/content/revision，并区分原生 append 与 replaced，固定优先级：窗口边界与首次/恢复基线、首个来源结构/内容/revision 变化；策略变化和 transport reset 独立字段，避免一个含糊字符串混合全部原因。

实例只保存最近 durable 普通 sample、最近成功 turn 聚合、最近成功摘要、普通比较基线及最新安全失败状态，不保存无界历史或全局 map；编码快照最多 64 KiB。没有跨进程统计假象：scope=process，首个成功观测前状态 no_observation；resume 标记 restored_baseline。历史 sample_usage 仍可按既有方式查询，但不能重建未持久化的来源诊断。

比例为 `cache_read / (input_uncached + cache_read + cache_write)`；适用分量必须全部 known、加法无溢出且分母非零。not_applicable 在该数学式中不贡献值，但不改写其 metric state；unknown、零分母或无法表示的比例输出 unknown。数值计算使用检查整数/有界 basis points，不引入浮点 NaN 或用 token 估算补账单。OpenAI 常见响应未返回 cache_write 时允许比例 unknown，这比假造零更准确。

相同指纹是可复用条件；服务端明确 cache_read>0 才表示观察到读取。降低到零只记录该事实；无客户端解释时为 unexplained_cache_read_drop。TTL 是否实际到期、路由驱逐均不是本客户端能证明的事实。本次不保留时间采样来制造确定解释，也不做主线告警阈值或自动“修复缓存”。

### D10. Claude 风格查询与单 owner 宿主投影

新增唯一当前 `QueryCache` 命令、`CacheSnapshot` 语义事件及 correlated control item，所有 kind 有 typed constructor、validator 与严格宿主 decoder；进程内不增加 format version。扩展 ChatSession 小接口及 AgentLoop command switch，沿用最近 committed snapshot publication。查询不直接调用 Provider getter、数据库或文件；活动 worker 与 loop 的数据交接只传不可变值，过期 publication 由既有 operation identity 拒绝。

TUI `context.go` 将 /cache 纳入精确命令分派，增加独立 `cache.go` 面板投影，不把整个诊断堆进 Model.View。/cache 无参数、拒绝保留草稿；/context 显示当前策略和配置 transport 的简短摘要，/cache 显示最近确认的实际 transport，区分“已配置”和“已观测”。活动审批继续优先隔离输入；面板 Esc 只关闭，Ctrl+C 先关闭面板再沿既有任务/审批取消路由；面板外的 Esc/returned tray 顺序不变。面板请求使用 generation/request identity；关闭后迟到响应不能重开或覆盖新查询。

stream-json 添加 `cache.query` 和显式 `cache.snapshot`，继续外部 version=1；只有显式查询才输出缓存帧，先 control.response(disposition=queried) 后 snapshot。单 writer、背压、EOF drain、shutdown、断管清理保持；一次性 --print/--json 无新增词汇。/cache 作为 headless input.submit 正文时保持正文，不特殊执行。截图及输出只允许安全元数据，model 等已有可配置文本继续 terminal escape/截断，key 值永不展示。

### D11. Capability 声明和代码复用边界

完成全部实现和验收后，Factory 的 PromptCacheControl/PromptCacheKey/PreviousResponse 按已校验显式配置声明；这表示已实现并被用户允许的 wire 能力，不是 base_url 的主动探测结论。Conversation 的 PreviousResponse 额外与会话固定 HTTP 回退状态相交；“当前是否存在可续写基线”由快照独立表达，不让 capability 随每个首请求抖动。disabled/omit/http_sse 对应能力保持 false。其他延期能力原样 false。

复用固定 codec、native commit/replay、prepared resolution、原 reducer、工具 scheduler/ledger、AgentLoop publication、终端安全格式化和 headless writer。只为两个真实 transport 引入小策略接口；Anthropic cache planner 保持私有纯函数，不为模式创建单实现空 facade，不使用 embed/shadow 模拟动态分派。

### D12. 验证标准与证据矩阵

以下保留设计时的验证目标；实际测试路径、业务断言和命令结果见 verification，测试名按现有风格细化且不删减验收范围。

| 编号 | 验收及关键断言 | 目标测试 |
|---|---|---|
| V1 | 三配置默认、严格枚举/null/重复/跨 family/env 覆盖后校验；构造零网络/资源 | `internal/config/config_test.go`、`internal/app/cache_e2e_test.go`、Factory tests |
| V2 | Anthropic 0–4 标记、去重、无项目/空窗口、两 TTL、20-block 边界、thinking/redacted/未知块、冲突标记；编译不改 native bytes | `internal/provider/anthropic/cache_test.go`、request golden |
| V3 | Raw 已知块含未知字段不丢；schema 排序、动态命令/环境不入 catalog；跨工具/steering 内容完整 | 两家 `request_test.go`、`cache_test.go`、`artifact_catalog_test.go` |
| V4 | OpenAI omit/thread、不同 thread 仅允许 key 差异、同 thread 恢复稳定；无 secret/path/随机状态 | `internal/provider/openai/cache_test.go`、restore golden |
| V5 | 两家摘要最终覆盖只执行一次、指导后缀不缓存、手动/自动成功失败与 checkpoint 等价 | 两家 `compaction_test.go`、`compaction_stream_test.go` |
| V6 | HTTP 默认 wire bytes 不变；WS full 专属 envelope、相同 events 归并和完整工具输入验证 | `internal/provider/openai/websocket_test.go`、HTTP/WS golden |
| V7 | durable 后增量、原生前缀错配/缩短、model/tools/key/指令变化、opaque/phase 差异、steering、匹配 outputs；HTTP 也正确诊断 append；suffix 精确重组完整 input | `internal/provider/openai/incremental_test.go` |
| V8 | completed 前后 Sync 阻塞/失败、discard/finalize 错误；基线不提前推进，工具零重复执行 | `internal/runtime/cache_test.go`、`internal/app/cache_e2e_test.go` |
| V9 | 握手 426 等允许回退一次；401/403/TLS/redirect 不回退；create 部分写/断线/服务端错误不重发；下一显式 full | `internal/provider/transport/websocket_test.go`、OpenAI WS integration |
| V10 | frame 重组、UTF-8、超限/二进制、事件错序/身份冲突、读取/写入阻塞、空闲取消、重复关闭、shutdown timeout；有唯一 done 且资源释放 | transport WS tests、`internal/runtime/session_owner_test.go`、app lifecycle tests |
| V11 | 固定当前 fixture 同配置 uninterrupted/restored 完整 canonical bytes/native 顺序/全部 fingerprints；HTTP/WS full 各自 wire equality，增量另外验证前缀 | 两家 `restore_test.go`、`compaction_test.go`、`internal/app/cache_fixture_test.go` |
| V12 | 五来源逐项变化、revision-only、策略-only、added/removed、append/replaced、opaque-only、压缩、恢复、unknown/known zero/N/A/溢出；大量回合有界、实例隔离 | `internal/context/cache_diagnostics_test.go` |
| V13 | 查询无网络/无新 turn/不推进计数；中间 durable sample 后失败保留 sample，不伪造 turn 聚合；摘要隔离 | runtime cache tests、app E2E |
| V14 | /cache 严格识别、参数拒绝/保稿、Esc/迟到响应、审批隔离、窄屏/无色/控制字符；UI 不影响请求 | `internal/tui/cache_test.go`、`internal/tui/testdata/cache_*.golden` |
| V15 | fragmented cache.query strict 解码、response/snapshot 顺序、重复 request_id、EOF drain、断管和一次性输出不变 | `internal/headless/cache_test.go`、stream golden、app headless E2E |
| V16 | 模型端点固定 localhost、禁止默认外网；无 secret/prompt/opaque 出现在诊断/错误/快照 | 所有新 integration 与泄漏哨兵测试 |

V11 必须实际比较完整 canonical bytes，不能只比较 hash 或 semantic view。incremental 的展开验证是附加条件，不替代原有恢复门。固定 Session fixture 在测试中只读取既有不可变 bytes；新测试优先复用当前 fixture，不在运行时用 encoder 生成“历史”。若确需新增场景 fixture，在测试外生成并逐字节提交，禁止引入历史版本。

WS/SSE 取消、Sync、close 与 shutdown 竞争用可控 reader/writer、barrier、fake deadline、故障注入确定顺序，不使用 sleep 或概率重试。本次未改变 lease 算法，但仍需跑既有真实子进程竞争/崩溃/禁止 repair/续写 seq 的完整回归。

质量门：`openspec validate add-provider-prompt-caching --strict`、`openspec validate --specs --strict`、`git diff --check` 和 `make verify`。先跑相关单测/golden，再完整质量门；所有真实平台 smoke 和未覆盖平台需在 verification 明示，不声称本地 Darwin 覆盖 Linux/Windows。性能只保证结构性有界与不随 turn 无限增长，不写未经测量的延迟/费用数字。

## Risks / Trade-offs

- [WebSocket 扩大生命周期复杂度] → 显式 opt-in、单连接单样本、无预热/多路复用、唯一 close owner 和故障矩阵；不复制 Runtime。
- [网关接受 wire 的能力各异] → 默认省略字段，不按 model/family 猜测；只有允许的握手失败可回退，不通过重试隐藏鉴权/TLS问题。
- [缓存标记误改 Raw 造成不可恢复语义损失] → 请求包装器只加受控字段，固定 raw 扩展 fixture 与前后 native bytes 比较。
- [过度诊断泄漏正文或产生伪因果] → 仅元数据、无正文 diff、process scope、unknown 与可能原因区分，不接入 telemetry allowlist 占位。
- [失败重发导致额外计费或生成] → 写 create 前后明确线性化，写后不自动重发；恢复与下一显式请求 full。
- [完整请求与增量帧被错误比较] → 三层 fingerprint 和各自 golden，完整恢复字节相等始终独立验证。
- [一小时策略或新模型缓存行为影响费用] → 用户显式配置、无收益保证；不将参考快照中的订阅资格/价格复制进产品。
- [相同 key 不等于相同内容] → key 身份独立，失效仍按 source revision/content/策略与 actual usage 分层。
- [空闲 socket 没有后台 reader，可能直到下一请求才发现远端关闭] → 下一发送/读取失败结束当前 sample，不自动重发；避免隐式后台生命周期，UI 只展示最近确认状态。

## Migration Plan

此处是当前契约变更的交付步骤，不是数据迁移器设计：

1. 评审并明确批准本 proposal/specs/design/tasks 后，由后续明确 apply 请求实现；本轮不新增 ADR、改主 specs 或代码。
2. 先实现纯值/strict config/身份构造与完整请求覆盖，再接 durable 收据、原生前缀判定与诊断、WebSocket/增量、宿主查询；这些是同一 change 的实施顺序，不是删减或延期能力。
3. 新增 ADR（建议 `0009-provider-cache-and-ephemeral-continuation.md`，实施时核对下一个空闲编号），记录请求事实与瞬态连接分离；更新现有 ADR 索引、架构、Roadmap 和 pitfall，保持 P4 其他未实现目标状态准确。
4. 所有默认配置维持 HTTP 与缓存字段省略；启用策略是用户可逆配置。回退配置为 disabled/omit/http_sse 即停止新优化，不 rewrite journal，也不伪装清除远端缓存。
5. 执行 V1–V16、全量质量门并记录实际证据后，才声明 capability、同步主 specs 和按独立 archive 工作流归档。任一完整能力未通过时变更仍未完成，不用空实现、测试 skip 或静默缩范围交付。

### 查询关联的实现细化

TUI 在提交 `/context` 或 `/cache` 前分配带本地 generation 的 RequestID，通过唯一当前 `ChatSession.QueryContext(ctx, requestID)` / `QueryCache(ctx, requestID)` 传入。关联标识必须在命令开始前可见，不能等异步返回后再猜测快照属于哪次打开。关闭面板或取消待查询使该 generation 失效；结果与快照均核对 request ID，迟到结果不得清空新的草稿或重开面板。该细化不新增持久化字段或兼容重载。

### turn 结束时的连接资格清理

Runtime 自身会在消费完 completed 后取消采样子 context，因此 Provider 不能把该清理取消误当作用户取消，也不能保存 context 等待下一轮探测。采用小型 `provider.ContinuationResetter` 生命周期接口，由实现可复用连接的 OpenAI Conversation 提供。Runtime 在原 turn owner 的退出路径，遇到 turn 错误或外层 context 取消时显式重置连接；普通成功退出保留连接。重置发生在 stream owner 已退出之后，不创建监视 goroutine、不改 ledger，不把 durable sample 回滚。Anthropic 不实现没有意义的空策略。
