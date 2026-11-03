## Why

现有双 Provider、七工具、原生恢复与手动/自动压缩已经形成可工作的纵向切片，但 ContextPlanner 的稳定分段尚未驱动实际缓存策略，用户也无法区分内容变化、服务端缓存用量和传输优化。此时推进完整的 P4 缓存闭环，能够复用现有原生历史、确定性 codec、usage 和宿主控制边界，同时为长会话提供可验证、可解释的优化。

## What Changes

- 增加显式、严格校验的缓存和 transport 配置：Anthropic 关闭/5 分钟/1 小时，OpenAI 省略/thread 派生缓存 key，以及默认 HTTP/SSE、可选 WebSocket 自动安全回退。
- Anthropic 在最终请求副本上确定性放置合法缓存断点，保持会话内 TTL 稳定、原生 block/opaque 数据保真，并覆盖工具续写与摘要请求。
- OpenAI 生成可恢复的稳定缓存 key；可选 WebSocket 复用现有 reducer，只有成功持久化的响应和精确原生前缀才可增量续写，始终保持 `store=false`。
- 区分内容 fingerprint、策略 fingerprint、服务端 normalized usage 和实际 full/incremental transport；以有界实例状态分析来源变化，不把相同指纹视作实际命中。
- 提供 Claude 风格 `/cache` 详情、`/context` 策略摘要和 stream-json `cache.query`，查询不进入模型历史、不联网、不推进统计。
- **BREAKING**：调整唯一当前 Factory 构造契约以显式绑定 ThreadID；调整进程内控制协议及相关 strict decoder。HTTP 默认请求、Session/native/checkpoint 当前格式和一次性输出契约保持；不引入历史 reader 或迁移器。
- 建立双 Provider golden、缓存失效矩阵、完整请求恢复等价、WebSocket 连接生命周期及失败关闭验证。

## Capabilities

### New Capabilities

- `provider/prompt-caching`：Provider 私有缓存策略、请求副本、稳定身份、断点及摘要请求处理。
- `provider/openai-websocket`：可选 WebSocket transport、durable 增量基线、安全回退、取消与唯一 cleanup owner。
- `context/cache-diagnostics`：强类型有界观测、内容/策略差异、三态 usage 和不泄漏正文的查询快照。

### Modified Capabilities

- `config/json-file`：三个显式策略字段、默认值及合并后跨 Provider 校验。
- `provider/openai-responses-text`：完整 canonical 请求、可选 key、WebSocket wire 派生及准确 capability 声明。
- `provider/anthropic-messages-text`：合法缓存标记与完成后的 capability 声明，修正现有禁止缓存的契约。
- `provider/native-history-persistence`：明确完整请求恢复等价和瞬态 transport 状态的边界。
- `runtime/chat-turn`：请求观测与成功 Sync/finalize 的关联、压缩与普通 sample 隔离。
- `headless/stream-control`：严格 `cache.query`、关联 `cache.snapshot` 和单 writer 顺序。
- `tui/context-commands`：本地 `/cache`、策略摘要、窄屏与未知值展示。

## Impact

主要涉及 `internal/config`、`domain/protocol`、`provider/{openai,anthropic,transport}`、`context`、`runtime`、`app`、`headless` 和 `tui`。WebSocket 拟采用封装于 transport 的 `github.com/coder/websocket`；HTTP/SSE 继续使用 Resty，JSON 继续经过固定 Sonic codec。需新增长期架构 ADR，更新架构、Roadmap、pitfall 与测试证据，均在后续 apply 阶段执行。

参考 Claude Code 2.1.88 的请求覆盖、TTL 稳定、诊断和工具排序，参考 Codex `6f8bf683c` 的 key 策略、精确增量检查及连接重置测试；具体文件与取舍见 design。范围不包含登录、Chat Completions、跨 Provider fork、远程 compaction、MCP/插件实现、WebSocket 多路复用、预热或完整 telemetry。上述边界不删减已确认的双 Provider 缓存、两种 TTL、诊断、宿主查询或增量续写能力。
