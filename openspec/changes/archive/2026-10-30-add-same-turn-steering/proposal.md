## Why

EasyCode 已具备双 Provider、可恢复工具循环和多行 composer，但运行中 TUI 忽略普通输入，现有控制队列只能开启后续独立 turn。现在补齐 Claude Code 式运行中追加、排队预览和取回编辑，可以直接改善已有 coding loop；同时必须解决输入消费竞态、原生持久化与恢复展示顺序，不能只放开输入框。

选择这一能力有三个阶段性理由：

1. **直接改善现有主路径。** 双 Provider、七工具与可恢复循环已经存在，但用户在长工具任务中无法继续补充要求。运行中输入、预览、取回与停止，能让已有能力形成更接近 Claude Code 的连续协作体验；收益不依赖新增工具或外部服务。
2. **已有架构接缝可复用。** 当前 AgentLoop 的唯一 owner、FIFO、request ledger，以及 Runtime 在整组工具 outputs 提交后的边界，能够承接调度与安全点。Provider prepared commit、Session 原子 batch 和投影器也已有基础；本方案扩展明确职责，不另造调度器或把队列藏在 TUI。
3. **先补交互和恢复基础，再扩展能力面。** 相比在此阶段增加 MCP、subagent 或后台任务，追加输入不引入新的外部执行边界，却能提前解决输入归属、取消和历史顺序这些后续同样需要的基础问题。代价是首轮持久化与历史投影必须一起调整，因此本 change 明确包含它们，不把协议占位或恢复问题留到以后。

参考取舍：Claude Code 提供主要交互方向——运行中仍可输入、队列可取回、工具循环边界接收补充；Codex 提供可借鉴的输入身份、定向控制与竞争处理思路。EasyCode 继续坚持双 Provider 原生历史和成功 Sync 后才确认保存，不照搬参考工程的全局队列、后台持久化或全部交互分支。具体参考位置与未采用方案见 design.md。

## What Changes

- 新增自动追加输入：空闲时开始任务，忙碌时排队，在整组工具结果 durable 后优先进入当前 turn；错过安全点则顺延。队首连续自动输入可作为一个 turn 的多条独立用户消息，既有显式 `submit_input` 保持一条输入一个后续 turn 的 FIFO 屏障。
- AgentLoop 单 owner 管理有界队列、输入身份、领取、原子取回、定向停止和关闭；TUI/stream-json 共享控制路径，宿主不持有第二份可执行队列。
- TUI 支持运行中编辑、排队预览、取回编辑和停止后返还未消费输入；保留审批/详情焦点隔离、Unicode 与安全粘贴规则。
- **BREAKING**：统一首轮和中途真实用户输入的独立 native 提交，提交前对候选历史执行纯内存上下文/请求大小预检，通过后成功 Sync、finalize，再发起模型请求。已提交用户输入在模型失败后仍保留；sample 不再重复携带用户输入。更新 Session 当前 canary 与双 Provider native canary，直接替换 current fixtures，旧开发 shape fail closed。
- **BREAKING**：语义历史从每 turn 两段文本改为带真实 turn 归属的有序可见消息，保留中途追加位置；同步估算、恢复渲染和输入历史消费者，不保留旧类型。
- 增加 stream-json v1 自动追加、取回与交互式停止的显式命令/反馈，保留既有 input.submit、turn.interrupt 和一次性输出的外部语义。
- 增加 deterministic race、故障注入、双 Provider request/native/cache golden 与 restored/uninterrupted 等价验证。

## Capabilities

### New Capabilities

- `runtime/conversational-input`: 自动输入投递、队首屏障、领取/返还、编码预算与控制反馈。

### Modified Capabilities

- `runtime/input-control`: 单 owner 管理自动队列和既有严格队列、撤回及停止策略。
- `runtime/chat-turn`: 首轮/中途输入 durable 边界、安全点握手与上下文规划。
- `provider/native-history-persistence`: user_inputs/sample/tool_outputs 单一当前原生契约与事务性恢复。
- `provider/openai-responses-text`: 从已提交输入编译 Responses，请求不重复追加用户输入。
- `provider/anthropic-messages-text`: 原生输入独立提交，tool_result 与追加文本保持合法顺序。
- `provider/history-projection`: 同一 turn 内有序 user/assistant 文本投影及失败后已提交输入可见性。
- `session/jsonl-store`: 输入关联记录、合法 batch grammar、当前 canary 和严格回放。
- `session/resume`: 已提交输入恢复与未完成 turn 本地收口，不恢复内存队列。
- `context/planning`: 对已提交输入计量一次，保留稳定前缀和估算不确定性。
- `tool/invocation-loop`: outputs durable 后开放输入安全点，维持调用上限和配对。
- `headless/stream-control`: 显式自动输入控制、身份关联、返还与关闭反馈。
- `tui/basic-chat`: Claude 风格运行中 composer、队列预览、取回与停止交互。

## Impact

涉及 internal/domain、protocol、runtime、provider/{openai,anthropic}、session、context、app、headless 和 tui 的当前契约及测试；不新增第三方依赖，不改变七工具 catalog、执行权限或 sandbox。规划基于 Claude Code 2.1.88 还原源码和本地 Codex 6f8bf683c；参考工程只读且不参与构建。

首版不加入网络请求抢占、审批期间聊天输入穿透、后台任务、slash command 框架、自动压缩、持久化待执行队列或跨进程 request 去重。无工具的成功 sample 仍结束当前 turn；未消费自动输入顺延。新增行为全部在本 change 中接入真实宿主并验证，不能把协议/恢复/UI 留成占位。
