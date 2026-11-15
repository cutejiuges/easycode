## Why

当前已交付双 Provider 启动推理配置、原生恢复、请求证据和缓存诊断，但用户在同一会话中从快速修改转入复杂排查时，仍需退出并修改配置才能调整 effort。最适合当前阶段的切片是补齐 `/effort`：复用已有完整链路，以明确的回合冻结边界提高日常交互效率，同时保持两家原生能力。

相较 MCP、后台进程、Subagent 和跨 Provider fork，本切片不需要先建立新的外部执行生命周期或线程存储体系；相较继续增加 Hook 事件，它直接改善所有已有工具任务的交互。两个参考工程的源码取舍、固定快照及证据位置详见 design.md。

## What Changes

- 增加 Claude 风格本地 `/effort` 简短查询反馈、`/effort <level>`、`/effort auto` 和 `/effort reset`，支持 current/status、unset 和帮助参数；详细设置沿用 `/context`，不为快捷查询新增独占面板。提供当前操作捕获值、下一操作选择、来源和生效时点；只在当前进程有效。
- 同一输入控制 owner 接收设置；在新 turn 启动时捕获不可变推理策略。当前 turn 内的工具循环、自动追加、Stop 续轮和自动摘要始终保持原策略；独立手动摘要在其预检接纳时捕获策略。
- 明确尚未获得 durable TurnID 的启动阶段和终态后的清理阶段；加入进程内选择序号隔离迟到观测，不把已选择或已捕获误报为实际已发送。候选选择及响应预检成功后原子接收，窗口/缓存观测失败不撤销已接受设置。
- 保留 Anthropic 与 OpenAI 各自已支持的 effort 枚举；auto 表示不发送 effort，reset 恢复启动配置。thinking、display、summary、输出上限、模型及磁盘配置不在本次修改范围。
- 增加 `effort.query` / `effort.set` stream-json 控制及关联快照，TUI 和 headless 共用同一状态和校验。普通输入中的 slash 仍是正文。
- 将实际操作设置贯穿输入预检、ContextPlan、普通/摘要编译和请求收据；属性改变时 OpenAI 使用完整请求，不沿用旧属性的增量资格。相同 wire 设置不制造缓存失效。
- 更新 `/context` 的内部设置投影与 `/` 首词补全，保证保留名 Skill 仍能通过 `$effort` 和 `/skills` 调用；外部既有 context.snapshot 与一次性输出保持当前字段。
- **BREAKING**：调整唯一当前的进程内采样输入/设置快照契约，要求显式传递经验证的操作策略；同步更新所有消费者和测试，不提供旧 shape reader、可选降级分支或内部版本门禁。Session/native/checkpoint 的存储结构不变。

## Capabilities

### New Capabilities

- `runtime/effort-control`：强类型 effort 选择、进程作用域、单 owner 接收、完整操作冻结、启动/运行/清理快照、选择序号、观测失败隔离与取消/关闭保证。

### Modified Capabilities

- `provider/reasoning-control`：将 effort 的冻结范围从整个进程细化到完整操作，保持同源计划、原生字段、缓存身份和恢复等价。
- `context/compaction`：明确自动摘要继承活动 turn 策略，手动摘要捕获接纳时策略，保留独立输出额度和所有现有失败规则。
- `tui/context-commands`：新增 `/effort` 简短反馈及帮助/别名，从控制 owner 获取当前/下一操作值，以选择序号与请求身份隔离迟到状态；替换 effort 永久冻结于启动的展示约定。
- `tui/input-completion`：增加第七项内置 `/effort`，保持首词补全、焦点隔离及同名 Skill 可达性。
- `headless/stream-control`：扩展当前外部 v1 的明确命令/事件集合，新增严格 effort 控制和关联响应，不扩展已有 context.snapshot 或一次性输出。

## Impact

主要涉及 `internal/domain/inference_settings.go`、`internal/context`、`internal/protocol`、`internal/runtime`、两家 Provider 的 sampling request plan、`internal/tui`、`internal/headless` 和 app 装配。复用现有不可变设置、单 owner 控制、原生编译、预算、缓存比较和终端安全渲染；不增加依赖、模型工具、后台 goroutine 或外部资源 owner。

验收覆盖双 Provider 请求 golden、启动写盘/终态清理屏障、选择与观察失败原子性、迟到回执、工具及 Stop 期间冻结、手动/自动压缩、HTTP/WebSocket 属性切换、严格协议、无中断/恢复字节与 fingerprint 等价、TUI snapshot/真实 PTY、取消/断管及完整 `make verify`。二次源码审查的缺口、依据和修订映射见 design.md；六份规格和任务一并更新。实现完成后同步 README、能力清单、架构、Roadmap 和受影响主规格；本次提案不把计划能力标为已实现，不提交或发布代码。
