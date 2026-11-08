## Why

当前 P5 已有流式 Markdown、工具主线、审批和 Ctrl+O 历史阅读，两家 Provider 也已保存原生推理，但用户仍无法阅读服务端明确返回的 thinking/summary。此时复用现有投影与分页接入受控推理展示，可以改善现有九项工具工作流，同时将提交身份、取消结果和高频增量的边界一次闭合。

## What Changes

- 白名单只允许 Anthropic thinking 文字与 OpenAI summary_text；signature、redacted data、encrypted content、raw reasoning、未知扩展始终不可展示。opaque-only 项保留无正文 unavailable 状态，没有推理项时不伪造活动。
- 活动时显示紧凑 Thinking/Reasoning 提示；已完成推理进入 Ctrl+O 统一记录，不逐块占用主线 50 项窗口。活动和已提交正文共用阅读入口、Markdown/安全原文和有界分页。
- 定义 sample/block 展示生命周期、Provider 内部 part 组装和完成快照规则；活动身份以显式映射连接已提交 cursor，输入提交与 sample 提交分开，不按正文相等猜测去重。
- 取消、提交接收后的成功、确定未提交及结果不确定按真实提交结果展示；失败收口不修改先前已提交 sample，不承诺“Sync 返回前取消就一定没有历史”。
- 对新增推理累计、元数据、单事件、排队副本和 UI 缓存分别设限；高频推理 delta 复用最近 committed 上下文快照，不逐 token 重算规划。
- **BREAKING**：修改唯一当前的进程内展示/提交/失败解析和查询契约，更新 typed constructor、strict decoder、validator 与所有消费者；不增加兼容 reader、内部格式版本或 Session/native/checkpoint 展示记录。
- headless 三种模式校验并消费内部推理/提交解析，外部文本与 JSONL 词汇保持不变；补双 Provider 原生恢复、缓存和取消线性化回归。

保持已确认范围：只展示服务端实际返回内容，不增加 thinking/effort/budget/summary 请求配置，不主动开启推理，不保证服务端返回正文。OpenAI 本切片支持默认增量、段完成快照与完整 item 路径，不协商 sequential_cutoff。MCP、Subagent、模型切换、raw reasoning、成本面板和跨 Provider 转换不在范围内。

## Capabilities

### New Capabilities

- `provider/reasoning-display`：可展示白名单、各 Provider 交付规则、流式状态和端到端资源上限。
- `tui/reasoning-transcript`：活动提示、Ctrl+O 阅读、显示预算、提交身份交接和焦点保护。

### Modified Capabilities

- `provider/transcript-projection`：独立推理项、允许正文及 unavailable、有序位置、独立于语义历史的只读分页。
- `provider/anthropic-messages-text`：澄清 thinking 展示与原生累计/签名保真的区别，保持请求和 terminal 规则。
- `provider/openai-responses-text`：固定 summary 增量/快照交付规则，区分主动请求能力与返回内容的展示能力。
- `runtime/chat-turn`：sample 身份与提交映射、取消线性化/不确定结果、高频增量的快照复用。
- `headless/text-output`：消费内部推理及提交/失败解析，保持三种外部输出和协议错误规则。

## Impact

- 预计影响 domain/protocol、两家 Provider reducer/PreparedSample 展示附属元数据、Runtime/AgentLoop、app transcript、TUI 与 headless；不增加第三方依赖、模型工具或后台 owner。
- 复用 native persistence、TranscriptProjector、TranscriptQueries、through/generation 交接、四页缓存和安全渲染；不把展示派生值加入 SemanticHistoryView、ContextPlanner、工具结果或稳定 prompt 来源。
- 通过 sample/block 到 journal cursor 的纯内存映射保持活动阅读连续性；恢复从已有 native facts 重建，不持久化展示 ID 或在 resume 重写 journal。
- 实施验收及主规格同步已完成；已按用户后续明确请求于 2026-11-08 归档，检查记录见 verification.md。
- 参考 Claude Code 2.1.88 sourcemap 与 Codex `6f8bf683cee83ed4d580a2e106b587d8451db936` 的完整调用链和测试；证据、取舍与九项缺口对应见 design.md。
