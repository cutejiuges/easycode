## Why

当前实现已经通过既有质量门，但仍存在文件安全检查未绑定实际句柄、Session 核心协议暴露 `any`、Provider 完成样本可在 durable 写入前缺少完整校验、运行时上下文与生命周期边界不显式、OpenAI 流状态关联不足，以及分支命名只能依赖人工约定等架构债务。需要在继续扩展 Tool、缓存和 Subagent 之前集中收紧这些基础契约，同时保留已确认的未来能力占位并明确其退出条件。

## What Changes

- 将配置文件和 Session 路径的类型、权限、symlink 与 TOCTOU 检查绑定到实际打开的文件或目录句柄，并用显式 `Open`/`Create` 生命周期替代执行 I/O 的 `New*`。
- 将 Session 六种 v1 record draft、payload 解码和语义校验改为按 kind/revision 的强类型构造器、严格 decoder 与 validator，同时保持现有 JSONL v1 canonical bytes、checksum 和历史 fixture 兼容。
- 强化 `NativeCommitEnvelope` 与 `PreparedSample` 不变量，要求 Runtime 在 durable append 前再次验证完整 envelope，避免无效完成样本先落盘后才在 finalize 阶段失败。
- 让 Provider RequestCompiler 产生有界 canonical JSON bytes，transport 只发送已编译正文；同时将 cache plan 改为强类型、不可变、可校验的有序快照，并保持 Provider request golden、恢复前后 bytes 与 fingerprint 等价。
- 为 ChatSession、headless、TUI 和应用装配显式传递调用 context，拆分纯内存构造与会执行 I/O、启动 goroutine 的生命周期操作，并为 shutdown ownership 提供确定性测试信号。
- 将 OpenAI Responses reducer 改为单 sample 状态机，校验 created/completed response ID 关联、唯一终态、重复或冲突事件以及 terminal 后事件。
- 保留已圈定的 Tool、extension、Subagent、telemetry、RuntimeCommand 及相关未来字段占位，仅添加带 Roadmap 阶段、保留原因和启用/删除条件的中文 TODO；不在本变更实现这些未来能力。
- 增加基于 Go AST/import graph 的全仓架构守卫，覆盖依赖方向、生产包级变量、核心导出 API 的 `any` 以及占位 allowlist，替换容易漏报的局部源码字符串扫描。
- 新增强制分支规范：新增功能使用 `feat/<kebab-case>`，Bug 或安全修复使用 `fix/<kebab-case>`，其余仅允许登记的语义前缀；由共享脚本、pre-commit、pre-push 和 GitHub Actions 一致校验。
- 修正文档中的“当前实现”与“目标架构”混淆，并同步架构、Roadmap、pitfall 和占位例外边界。
- **BREAKING（仓库内部 API）**：Session draft/decoder、transport request body、ChatSession submit 和若干构造生命周期签名会变更；对外 CLI、Provider wire、RuntimeEvent JSON 和 JSONL v1 bytes 不变。

## Capabilities

### New Capabilities

- `context/cache-plan`: 定义强类型、不可变、有界的缓存分段与计划，以及稳定前缀 fingerprint 的校验和复制语义。
- `engineering/branch-governance`: 定义语义化分支名称、允许前缀、kebab-case 规则和本地/CI 一致门禁。

### Modified Capabilities

- `config/json-file`: 将配置文件安全校验绑定到实际打开的文件句柄，并覆盖确定性的 symlink/TOCTOU 回归。
- `session/jsonl-store`: 收紧 typed draft/decoder/validator、显式 Repository 生命周期和句柄绑定的路径安全，同时保持 v1 bytes 兼容。
- `session/resume`: 要求同一安全句柄链和连续 lease 完成打开、校验、修复、恢复与续写，不允许检查后替换路径组件。
- `runtime/chat-turn`: 显式传播宿主 context，强化完成样本的 durable 前置校验、取消线性化和唯一 cleanup owner。
- `provider/http-stream-transport`: transport 仅接收 Provider 已编译的有界 canonical JSON bytes，不再负责结构化请求序列化。
- `provider/native-history-persistence`: 完整验证 prepared native commit，禁止零值或无效 envelope 进入 durable Session。
- `provider/openai-responses-text`: 以 response ID 关联并约束 Responses sample 事件状态机和唯一终态。

## Impact

- 主要影响 `internal/config`、`internal/session`、`internal/provider`、`internal/context`、`internal/runtime`、`internal/app`、`internal/headless`、`internal/tui` 及其测试与 golden/fixture。
- 工程门禁影响 `AGENTS.md`、`Makefile`、`.githooks/`、`scripts/`、`.github/workflows/`、PR 模板和仓库分支保护配置说明。
- 文档影响 `README.md`、总体架构、产品 Roadmap 与 pitfall log；只有出现新的长期不可逆决策时才新增 ADR。
- 不新增外部依赖，不实现 Tool/Hook/MCP/Plugin/Skill/Subagent/telemetry，不修改 Provider 能力声明，不重写历史 Session fixture，也不扩展 P3/P4/P6/P7 的延期功能。
