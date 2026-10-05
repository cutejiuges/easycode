## Why

EasyCode 已具备可恢复、可取消且 durable 的单 turn Runtime，但活动 turn 期间的新输入只能被拒绝，headless 进程也只能消费一次 prompt 后退出，因而无法形成可供 TUI、自动化宿主和未来 Tool Loop 复用的长期会话控制面。现在补齐同进程多轮、排队、定向中断和有序关闭，可以直接复用现有 Provider-native history 与 Session 提交边界，同时避免在 P3 安全插入点出现前虚假暴露 same-turn steer。

## What Changes

- 正式启用版本化、强类型的 Runtime 输入命令及提交结果，提供 request identity、目标 turn 前置条件、严格构造/解码/校验和稳定拒绝原因。
- 引入 Session 实例持有的单 owner 控制循环：空闲输入启动新 turn，活动期间输入进入有界 FIFO follow-up queue；任意时刻仍只有一个 Provider stream 修改原生历史。
- 明确定义取消与关闭语义：interrupt 只针对匹配的活动 turn；stdin EOF 停止 admission 并排空已接受输入；显式 shutdown 停止 admission、取消活动 turn、明确拒绝尚未启动的排队输入，并等待 durable terminal 与资源清理。
- 增加独立的 headless 流式控制模式，以增量 NDJSON 接收命令，并通过单一顺序 stdout writer 输出命令响应和既有 RuntimeEvent 的 headless 投影；现有一次性 `--print` 与 `--json` 行为保持兼容。
- 为队列容量、输入大小、命令去重、取消线性化、lost wakeup、断管、半包/UTF-8、EOF drain、并发提交和 shutdown race 增加确定性测试。
- 本变更不实现同一活动 turn 内的模型 steer，不增加优先级队列，不合并多个用户输入，不新增 Session record kind，也不修改 Provider wire/native history schema；真正的 same-turn steer 留待 P3 Tool Loop 提供多次 sampling 的安全插入点后单独变更。

## Capabilities

### New Capabilities

- `runtime/input-control`: 定义单 owner 的长期输入控制、FIFO follow-up queue、提交结果、定向 interrupt、EOF drain 与 shutdown 生命周期。
- `headless/stream-control`: 定义 headless NDJSON 双向控制模式、命令/响应 wire、顺序输出、输入边界和进程退出语义。

### Modified Capabilities

- `runtime/chat-turn`: 区分低层单 turn 执行 context 与长期控制器的 admission context，保持取消传播与 cleanup ownership，同时禁止把调用方 context 保存进排队状态。
- `headless/text-output`: 将“恰好一个新 turn”约束限定为既有一次性模式，并定义其与新增流式输入模式的 CLI 兼容和互斥关系。

## Impact

- 主要影响 `internal/protocol`、`internal/runtime`、`internal/headless`、`internal/app` 和 `cmd/easycode`，并更新对应单元、集成、race、CLI 与协议 fixture 测试。
- 当前占位 `RuntimeCommand` 将被替换为具有真实消费者的强类型契约；外部 headless wire DTO 与进程内 command/event 类型保持分离。
- 现有 `Runtime.RunTurn`、Provider compiler/reducer、Provider-native history、Session JSONL/SQLite schema、headless JSONL v1 turn 事件和一次性 CLI 调用保持兼容。
- 架构文档、P2 Roadmap 与 pitfall 记录将同步真实交付边界，明确 follow-up queue 已实现而 same-turn steer 仍未实现。
