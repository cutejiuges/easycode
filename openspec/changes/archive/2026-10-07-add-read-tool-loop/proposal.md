## Why

EasyCode 已具备双 Provider 文本采样、Provider-native history、durable Session、恢复和输入控制，但模型仍不能读取工作区，也不能在同一用户 turn 中完成“调用工具 -> 接收结果 -> 继续采样”。现在以只读、低副作用的 Read 建立首个完整 Tool Loop，可以先证明双 Provider wire、多 sample、durable ledger、崩溃恢复和缓存稳定性，再逐步引入搜索、写文件、权限交互和命令执行。

## What Changes

- 引入只有真实 Read 能力的不可变 Tool Catalog snapshot：使用稳定 facade、严格 JSON schema、确定性排序、canonical bytes、source revision 和 fingerprint；未实现的 Glob、Grep、Edit、Write、Bash、Skill、MCP 与 Agent 不对模型暴露。
- 将 Tool Catalog 作为请求前的强类型稳定上下文来源；相同 catalog 产生相同 stable-prefix fingerprint，schema 或 revision 变化产生可定位失效，cwd、Session identity、时间和随机调用 ID 不污染该指纹。
- 为 Anthropic Messages 增加原生 `tool_use`/`tool_result` wire，为 OpenAI Responses 增加 function call/function call output wire；两家分别保留自己的 schema、流式归并、原生 item 和 result codec，不建立可反向构造请求的统一扁平 Message。
- **BREAKING** 重构两家 Provider 当前仅供开发使用的 native history：删除把用户输入与助手输出永久绑定的 `nativeTurn` 结构及旧 payload codec，以单一当前 sealed entry 模型表达 sample 与 tool outputs；不保留旧 Provider payload reader、版本后缀业务类型或混合 revision 恢复路径，并同步重写开发期 fixture。
- 扩展 prepared sample，使完整 Provider sample 在参数严格解码后携带按模型顺序排列的 typed ready calls。Runtime 必须先原子 durable 提交 native sample、sample usage 和 ready-call facts，再允许 Read 执行；不完整参数、失败 stream 或 durable 失败保持零工具 I/O。
- 将 Runtime 从单 sample 文本 turn 演进为有界多 sample Tool Loop：无工具调用的 sample 正常完成 turn；包含 Read 调用的 sample 依次执行、持久化结果、提交 Provider-native tool outputs，再进入下一 sample。首版顺序执行多个 Read，不声明或实现并行工具。
- 增加 required Session ledger records，分别保存 ready、execution started 和确定结果。每个调用同时保留 Provider call ID 与 EasyCode invocation ID；自动执行只承诺至多一次，不宣称通用 exactly-once。
- 定义取消与恢复状态：正常执行遵循 ready -> started -> result；若取消在 executor 接受前线性化，可直接记录 cancelled result。恢复只做本地补偿，不重跑 Read、不自动请求 Provider：未开始调用关闭为 `session_interrupted_before_execution`，started 后缺失结果关闭为 `outcome_uncertain`，已有 result 复用持久化预览补交缺失 output，最后以唯一失败终态关闭旧 turn。
- 实现 workspace 受限的 Read executor：启动时冻结 workspace root，支持 workspace 相对路径及 containment 内绝对路径，只读取由实际打开 handle 验证的常规 UTF-8 文件，拒绝 traversal、symlink、特殊文件、二进制/非法 UTF-8、超限文件和不支持平台，不使用 `Lstat` 后再按路径打开的安全假设。
- 定义有界 Read 输入与确定性模型结果，包括 1-based offset、line limit、稳定行号、长行/总输出预算和明确截断元数据。完整模型预览作为 ledger 与 Provider-native output 事实持久化，resume 不重新读取文件或按新算法重算。
- 聚合同一 turn 内全部成功 sample usage，在最终 `turn_completed` 发布总 usage；已经 durable 的中间 sample usage不得因后续工具或 Provider 失败被删除或改写。
- 增加双 Provider request/stream/native golden、当前 Session fixture、崩溃点、重放、取消、TOCTOU、恢复等价、缓存 fingerprint 及端到端“请求读取 -> Read -> 模型总结”测试。
- 本变更不实现 approval/ask、用户可配置权限规则、并行调度、Glob/Grep、文件修改、artifact store、后台进程、same-turn 用户 steer、Tool RuntimeEvent/TUI 展示、Skill、MCP 或 Subagent；这些能力继续由后续独立 change 提出。

## Capabilities

### New Capabilities

- `tool/invocation-loop`: 定义 Tool Catalog snapshot、typed ready call、多 sample Runtime 编排、durable execution ledger、顺序结果提交、取消和恢复语义。
- `tool/read-file`: 定义 workspace 受限 Read 的输入、handle-bound 路径安全、UTF-8 文本读取、确定性有界结果及错误行为。

### Modified Capabilities

- `context/planning`: 增加稳定的 Tool Catalog 来源、固定来源顺序、token 估算与 stable-prefix fingerprint 规则。
- `provider/anthropic-messages-text`: 编译 Read facade，归并完整 `tool_use`，保留原生调用并编码匹配的 `tool_result`。
- `provider/openai-responses-text`: 编译 Read function tool，归并完整 function call，保留原生调用并编码匹配的 function call output。
- `provider/native-history-persistence`: 以单一当前 sealed entry schema 支持含 tool call 的 sample、tool outputs、无新用户输入的 continuation sample，以及恢复时的 call/output 配对校验。
- `provider/usage-accounting`: 将既有聚合代数正式应用于一个 turn 的多个 Provider samples，并定义后续失败时已提交 sample usage 的保留语义。
- `runtime/chat-turn`: 将单 sample 成功路径扩展为有界多 sample Tool Loop，并固定 durable-before-execution、最终 usage 聚合和唯一 turn terminal。
- `session/jsonl-store`: 新增强类型 required Tool ledger records、工具 sample 的状态转换、tool-output native commit placement，并以一套当前 fixture 固定完整 Tool Loop。
- `session/resume`: 在连续 lease 下恢复未完成 Tool Loop，在不执行工具和不访问 Provider 的前提下完成本地补偿；下一次用户输入从已正确配对的 Provider 原生历史继续。

## Impact

- 主要影响 `internal/tool`、`internal/tool/builtin`、`internal/provider` 及两家 Provider 实现、`internal/context`、`internal/runtime`、`internal/session` 和 `internal/app` 的启动装配与恢复流程。
- 当前 Tool 占位类型将被删除或收紧为具有真实 Read 消费者的 sealed typed contracts；不会保留导出 `json.RawMessage` 输入、万能 registry 或未使用 facade。
- Session JSONL 新增 required kind并重写当前开发期 fixture；SQLite Catalog 仍是可重建投影，不把调用参数、文件正文或 ledger 细节复制进索引。
- Provider capability 只有在请求编译、stream 归并、native persistence/restore、结果编码和 golden 全部存在后才声明 function tools；parallel/custom tools 继续保持 unsupported。
- 现有无工具文本 turn、一次性 headless 输出和长期 stream-control 命令保持兼容；本变更不新增外部 wire event，工具活动只通过模型最终回复可见。
- 不新增第三方依赖，不修改只读参考工程；实现必须同步 Tool 架构、Roadmap 与踩坑记录并通过 `make verify`。
