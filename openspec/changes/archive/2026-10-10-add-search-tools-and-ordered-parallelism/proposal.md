## Why

EasyCode 已完成单个 `Read` 调用的端到端闭环，但模型仍无法先发现文件、再定位代码，工具循环也尚未证明同一 sample 内多个只读调用能够安全并行且按模型顺序提交。现在引入 `Glob`/`Grep` 正好会触及 Tool、Provider native history、Session ledger、恢复与上下文指纹的共同边界，应在产品尚无历史用户时一次完成泛化，并移除会诱发 V1/V2 双实现的开发期兼容脚手架。

## What Changes

- 新增面向模型的 `Glob` 与 `Grep` capability，使 Anthropic Messages 和 OpenAI Responses 可以在 workspace 内执行确定性、严格有界、不跟随符号链接的文件发现与内容搜索。
- 将当前仅支持 `Read` 的 Tool Catalog、ready call、结果、Provider output 和恢复链路泛化为 `Read`/`Glob`/`Grep` 的封闭强类型联合；未完整实现的工具仍不得暴露 schema。
- 为 `parallel_read` 工具增加同一 sample 内的有界并行调度：副作用准入逐调用 durable，执行可并行，Session result 与 Provider output 始终按原始 call index 提交。
- 明确取消、崩溃和恢复语义：未被 executor 接收的调用关闭为取消，已被接收但没有 durable result 的调用关闭为 `outcome_uncertain`，恢复不得重跑文件系统操作。
- **BREAKING**：把仓库和进程内部的“版本号驱动 decoder/router”收敛为唯一当前 typed contract；移除未知 optional Session record 透传、工具 input/result revision 路由，以及纯内存 Context/Runtime command 的固定版本轴。
- **BREAKING**：直接替换当前开发期 Session schema 与固定 fixture；旧开发 journal 明确拒绝恢复，不提供旧 reader、migration graph、V1/V2 DTO 或混合 revision 路径。SQLite 仍是可重建索引，schema 不匹配时重建而非迁移。
- 保留真正的边界 canary 与内容演进标识：外部 headless JSONL `version: 1`、Session/Provider envelope 的单一当前 revision、Provider opaque item、内容派生 source revision/fingerprint、数据库 `user_version` 与应用版本；这些字段不得成为多实现分派入口。
- 更新工程约束、架构、Roadmap 与踩坑文档，把“发布前直接替换当前契约”和“发布后才引入显式兼容窗口”固化为可测试规则。

## Capabilities

### New Capabilities

- `tool/search-files`: 定义 `Glob`/`Grep` 的 schema、workspace 安全遍历、匹配语义、确定性排序、资源预算、截断结果与错误契约。
- `architecture/single-current-contract`: 定义产品发布前每个协议边界仅允许一个当前 encoder/decoder/typed model，并区分边界 canary、内容 revision 与禁止的运行时版本路由。

### Modified Capabilities

- `tool/invocation-loop`: 从仅 `Read`、顺序执行扩展为三个只读工具的封闭强类型调用链路和按 call index 提交的有界并行执行。
- `session/jsonl-store`: 重写当前工具 ledger schema，移除 optional 未知记录与多 revision 兼容语义，并记录有序并行结果所需的当前 typed facts。
- `session/resume`: 将恢复补偿从 `Read` 泛化到全部当前只读工具，并要求旧开发 schema 在任何 repair、append 或外部副作用前失败。
- `provider/native-history-persistence`: 支持同一 sample 的异构、有序工具结果，同时保持每个 Provider 只有一个当前 native payload codec。
- `runtime/chat-turn`: 在 sample durability、execution admission 与 output durability 之间加入有界并行只读调度，并保持 terminal/poison 语义。
- `context/cache-plan`: 移除纯内存缓存计划的版本分派，保留由内容与 source revision 驱动的失效和 fingerprint。
- `context/planning`: 将 Tool Catalog 从单个 `Read` 扩展为 `Read`/`Glob`/`Grep`，并移除纯内存 ContextPlan 的固定 plan revision。
- `runtime/input-control`: 内部 RuntimeCommand 使用唯一当前强类型契约，不再携带或分派 revision；外部 headless stream-json v1 保持独立且不变。

## Impact

- 主要影响 `internal/tool`、`internal/tool/builtin`、`internal/runtime`、`internal/session`、`internal/provider`、`internal/context`、`internal/protocol` 以及 app/headless 适配边界。
- Session 开发期 journal 与 fixture 将一次性失效；仓库当前 fixture、Provider golden、cache regression、恢复测试和架构边界测试必须同步替换。产品尚未上线，因此不承担历史用户数据迁移成本。
- 搜索实现使用 Go 标准库、handle-relative 文件系统操作和 Go regexp 语义，不引入 `rg` 子进程或新的生产依赖；首个切片不读取 `.gitignore`，不支持 PCRE、多行正则、外部目录或完整结果 artifact。
- 外部 headless JSONL v1、Provider wire、公开错误码和现有 `Read` 用户语义保持不变；变更只扩展工具能力并收紧内部/持久化契约。
