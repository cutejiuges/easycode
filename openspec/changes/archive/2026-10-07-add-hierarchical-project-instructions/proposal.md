## Why

EasyCode 当前已经具备文本会话、Provider 原生历史、Session 恢复、上下文规划与缓存稳定性分层，但还不能读取并遵循项目仓库内的编码约束。对本地 coding agent 而言，这会让模型在进入具体工具执行之前就缺少最关键的项目事实：目录级规范、构建命令、架构边界与风格约束都只能由用户逐轮重复输入。产品路线图也把项目指令列为 P2 上下文能力的未完成退出项，因此它是补齐当前阶段闭环，而不是提前进入 P3 Tool Loop 或扩展生态。

选择该能力的主要原因如下：

- **阶段依赖最匹配。** 项目指令只依赖现有启动目录、ContextPlan、双 Provider 请求编译和 Session 恢复边界；这些基础均已存在。相比工具调用、同轮 steer、MCP 或插件，它不要求先引入尚未完成的副作用调度、权限、幂等 ledger 或扩展生命周期。
- **用户收益直接且可独立验收。** 实现后，EasyCode 在纯文本阶段就能稳定遵循仓库规则，用户无需在每次会话中重复粘贴说明。该收益可通过请求 golden、缓存 fingerprint 和恢复等价测试完整证明，不依赖未来 UI 或工具能力。
- **能够验证现有架构是否真正可扩展。** 当前 ContextPlan 只规划 Provider profile、已提交历史和当前输入，Runtime 也尚未把额外规划来源传入请求编译。新增一个真实的 `project_stable` 来源，可以在受控范围内打通“发现 -> 快照 -> 规划 -> Provider 原生注入 -> 恢复”的完整链路，而不引入无消费者的占位接口。
- **双 Provider 风险可控。** Claude Code 与 Codex 的参考实现都把项目指令视为独立于真实用户意图的上下文，并采用从项目根到当前目录的确定性合并。EasyCode 可以复用这一共同语义，同时继续由各 Provider 负责编译自身 wire，避免建立有损的统一消息模型。
- **为后续能力提供稳定地基。** Tool Loop、Skills、Hooks、Subagent 和缓存命中都需要确定、可追溯且不会污染会话历史的项目上下文。先完成项目指令，可以减少后续每项能力各自读取工作区文件、各自定义顺序和各自影响缓存的重复设计。

本次范围刻意保持为最小可交付闭环：只发现项目根到进程启动工作目录之间的 `AGENTS.md`，缺失时回退同目录的 `CLAUDE.md`；不实现用户级记忆、`.claude/rules`、`@include`、热重载、信任交互或运行中工作目录切换。

## What Changes

- 新增项目指令发现与不可变快照：以最近的 `.git` 边界为项目根，按根到启动工作目录顺序逐层选择 `AGENTS.md`，同层缺失时回退 `CLAUDE.md`，并保留仓库相对来源信息。
- 为发现过程定义总字节上限、确定性截断、UTF-8 处理、常规文件约束和基于已打开文件描述符的安全校验；缺失文件可忽略，安全或读取错误必须显式返回。
- 将项目指令作为新的强类型 ContextPlan 来源，映射为 `project_stable` 缓存段并纳入 token 预算；绝对路径、启动工作目录字符串和文件系统遍历时序不得进入稳定 fingerprint。
- Runtime 在每轮规划与请求编译中使用同一份不可变快照，并把它作为独立上下文交给 Provider；项目指令不成为真实用户输入、RuntimeEvent、Session JSONL record 或 Provider 原生历史项。
- OpenAI Responses 与 Anthropic Messages 分别生成自身的原生请求表示，并在每轮请求中确定性重新注入项目指令；不通过共享扁平 Message 反向构造请求。
- Session 恢复时按当前进程启动工作目录重新发现项目指令。指令快照相同时，恢复后请求 canonical bytes、原生 item 顺序与 fingerprint 必须等同于不中断会话；快照变化时只使外部上下文和缓存键发生可解释变化，不改写既有 append-only records。
- 增加发现顺序、安全读取、预算边界、双 Provider golden、缓存回归及 uninterrupted/restored 等价测试，并同步相关架构、路线图与踩坑文档。

## Capabilities

### New Capabilities

- `context/project-instructions`: 从受限项目层级安全发现项目指令，生成有界、确定、带来源信息的不可变快照，并定义启动与恢复时的生命周期。

### Modified Capabilities

- `context/planning`: 增加项目指令这一强类型来源、`project_stable` 分段、稳定排序和预算核算。
- `runtime/chat-turn`: 在 Provider 调用前规划并传递同一项目指令快照，且在发现或校验失败时保持零网络调用。
- `provider/openai-responses-text`: 在 OpenAI Responses 原生请求中确定性注入项目指令，同时保持真实用户输入和原生历史语义不变。
- `provider/anthropic-messages-text`: 在 Anthropic Messages 原生请求中确定性注入项目指令，同时保持真实用户输入和原生历史语义不变。
- `provider/native-history-persistence`: 明确项目指令属于请求时外部上下文而非可提交原生历史，并把快照纳入恢复等价性的前置条件。
- `session/resume`: 恢复时重新发现当前启动上下文；继续禁止把 `creation_cwd` 或绝对路径直接注入请求与缓存 fingerprint。

## Impact

- 主要影响 `internal/context`、`internal/runtime`、`internal/provider/openai`、`internal/provider/anthropic` 和应用启动装配；Session record schema 与 RuntimeCommand/RuntimeEvent vocabulary 不变。
- Provider request fixture/golden、ContextPlan 与 CachePlan 回归测试、Session 恢复集成测试需要更新；未配置项目指令时，现有文本会话请求行为保持兼容。
- 参考 Claude Code 的层级覆盖与上下文隔离语义，以及 Codex 的项目根边界、确定性预算和来源建模；不复制其用户记忆、规则目录、include、信任模型或热更新机制。
- 不新增外部依赖，不修改两个只读参考工程，也不提前暴露 Tools、Skills、Hooks、MCP 或 Subagent 的可执行接口。
