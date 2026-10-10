## 1. 单一当前契约基线

- [x] 1.1 删除进程内 `protocol.Command`/`protocol.Event` 的固定 version 与内部 command JSON decoder，把 stream-json v1 的严格 wire 解码和 JSONL v1 编码留在 headless adapter；运行 `go test ./internal/protocol ./internal/headless ./internal/runtime` 验证 TUI/Runtime 使用无版本强类型值且外部 v1 fixture 逐字节不变
- [x] 1.2 删除 `context.Plan` 与 `ContextPlan` 的固定 version 字段、getter、旧版本构造/校验分支，保留 source revision、estimator method 与 fingerprint；运行 `go test ./internal/context/...` 验证计划不可变性、预算与 cache regression 不变
- [x] 1.3 在 `internal/architecture` 增加带明确边界 allowlist 的单一当前契约守卫，禁止 Session optional API、version 参数 decoder registry、Tool input/result revision API、内部 Plan/Command/Event 固定版本、V1/V2/Legacy 当前业务类型和按版本并存 fixture 目录；运行 `go test ./internal/architecture` 验证合法 Provider/Session/headless/SQLite canary 不被误报

## 2. 三工具强类型核心与目录

- [x] 2.1 在 `internal/tool` 实现 `GlobInput`、`GrepInput`、output mode、默认值与严格 validator，覆盖非法 UTF-8、绝对/穿越 path、非法 glob/regexp、未知 mode、context/limit 边界；运行 `go test ./internal/tool -run 'Glob|Grep|Input'`
- [x] 2.2 将 `ReadyCall`、typed invocation 与 `InvocationResult` 泛化为 Read/Glob/Grep 封闭联合，增加 capability 专属 constructor/accessor/metadata，并组合小型 `ReadExecutor`、`GlobExecutor`、`GrepExecutor` 接口；运行 `go test ./internal/tool` 验证 capability/payload 错配和零值均被拒绝且核心 API 不接受 `any`
- [x] 2.3 删除 `CatalogRevision`、Read input/result/renderer revision 与所有调用方路由字段，使 Catalog source revision 由稳定排序后的 facade name、description、canonical schema bytes 派生；运行 `go test ./internal/tool ./internal/context` 验证相同目录 fingerprint 稳定且任一三工具 schema/description 变化都会失效
- [x] 2.4 在 `internal/tool/builtin` 提供只包含 Read、Glob、Grep 且都绑定真实 executor/validator/renderer 的 Catalog 组合入口，更新缺失 executor 与未知 capability 拒绝测试；运行 `go test ./internal/tool/...` 并验证 Edit/Write/Bash/MCP/Skill/Agent 不出现在 snapshot

## 3. Workspace 安全搜索引擎

- [x] 3.1 扩展 Darwin/Linux `workspaceHandle` 为 handle-relative 目录枚举、child directory/file open 与 `fstat` 类型校验原语，并让 unsupported 平台 fail closed；运行 `go test ./internal/tool/builtin -run 'Workspace|Unsupported'` 验证纯构造无 I/O、所有 handle 及时关闭且无路径式回退
- [x] 3.2 实现带 memo 的分段 Glob matcher，支持 `*`、`?`、字符类与跨 segment 的 `**`，并覆盖非法 pattern、4 KiB/256 segment 上限和 workspace-relative `/` 规范化；运行对应 matcher 单测并验证同输入结果不受 OS separator 影响
- [x] 3.3 实现确定性 secure walker，包含 dotfile、跳过 VCS metadata、限制深度 64 与目录项 200,000，最终按完整规范相对路径全局字节序排序；运行目录树单测验证枚举顺序、mtime 与 goroutine 时序不改变结果
- [x] 3.4 实现 Glob executor 与 typed result/metadata，覆盖默认/最大 limit、普通文件限定、已知省略数、扫描预算耗尽原因和稳定安全错误；运行 `go test ./internal/tool/builtin -run Glob`
- [x] 3.5 实现 Grep executor 的 RE2 单行匹配、可选 path/glob、大小写、context 与 content/files/count 三种 mode，限制文件 50,000、单文件 16 MiB、累计 256 MiB；运行 `go test ./internal/tool/builtin -run Grep` 验证 matching-line count、context 去重、binary/非法 UTF-8/超大文件跳过统计
- [x] 3.6 实现搜索结果的 64 KiB 稳定 preview renderer，保留有序首尾、明确中间省略并把单行限制为 500 code points；运行 renderer 单测验证 UTF-8 边界、逐字节确定性、typed metadata 与 preview 上限
- [x] 3.7 增加确定性 Unix 安全集成测试，在枚举与 open 之间替换 symlink/目录项并注入权限、消失和非普通文件故障；运行 `go test ./internal/tool/builtin` 验证 workspace 外 bytes 不变、绝对路径/cause 不泄漏且测试不依赖 sleep 或概率竞态

## 4. Provider 调用与原生历史泛化

- [x] 4.1 泛化 OpenAI Responses 的 facade 编译、stream strict decode、prepared ready calls 与 tool-output preparation，使异构 results 按 call index 校验数量/call ID/capability/status/preview；运行 `go test ./internal/provider/openai` 验证未知字段、半包、错序和错 capability fail closed
- [x] 4.2 泛化 Anthropic Messages 的 facade 编译、stream strict decode、prepared ready calls 与 tool-result blocks，并无损保留 thinking/signature/redacted thinking；运行 `go test ./internal/provider/anthropic` 验证异构配对、错序拒绝和 opaque 内容往返
- [x] 4.3 删除两家 Provider 对 Tool input/result revision 的依赖，同时保留各自 native envelope 的单一当前 payload canary 与唯一 codec；运行 `go test ./internal/provider/...` 并用架构测试确认不存在旧 reader、版本后缀 DTO 或两家 wire 互转
- [x] 4.4 更新两家三工具 request、parallel call/output 与 history projection golden，增加随机 SSE chunk、半包、UTF-8、取消和断线回归；运行全部 Provider golden/fixture 测试验证 Catalog 顺序和 native item 顺序稳定

## 5. Session 当前 schema 与恢复

- [x] 5.1 重写当前 Session envelope：删除 `ReplayRequirement`/`ReplayOptional`/`OptionalRecords` 与冗余 `SessionMetaPayload.SchemaRevision`，把 canary 校验与按 `EventKind` 的唯一 decoder 分离；运行 `go test ./internal/session -run 'Codec|Loader|Draft'` 验证 unknown/旧/较新 shape 在 repair 前只读失败
- [x] 5.2 将 `tool_call_ready` 与 `tool_call_result` payload 改为 capability-tagged 的 Read/Glob/Grep typed union，删除 input/result revision，保证每种 record 可独立 strict decode；运行 Session codec/draft 单测验证未知字段、尾随 JSON、capability/payload 错配和超限 preview 被拒绝
- [x] 5.3 更新 ReplayPlanner 的并行 call group 状态机，要求 ready、started、result 分别按 call index 单调且 output commit 完整配对；运行 `go test ./internal/session -run Replay` 验证完成顺序写盘、重复/缺失 result、错 capability/call ID 与非法旁路均 fail closed
- [x] 5.4 一次性替换 `internal/session/testdata/current` 的固定历史 bytes 与预期 ReplayPlan，覆盖文本 turn、异构并行 Tool Loop、ready tail、started tail、results/no-output 和旧 schema 拒绝；运行 current fixture tests 验证 fixture 不由当前 encoder 在测试时生成且仓库不存在 v1/v2/legacy fixture 树
- [x] 5.5 泛化 app/session resume reconciliation，按 call index 把 ready/no-started 关闭为 cancelled、started/no-result 关闭为 `outcome_uncertain`、results/no-output 从冻结 previews 重建，并始终以一个 `turn_failed` 收口；运行 `go test ./internal/app ./internal/session` 验证不调用任何 executor/Provider、不重复 output且 lease 全程持有
- [x] 5.6 更新 SQLite catalog schema 检测回归，确认 `user_version` 不匹配只从已验证 JSONL 重建索引而无 migration graph；运行 `go test ./internal/session/catalog` 验证 JSONL bytes 不被修改

## 6. Runtime 有序只读并行

- [x] 6.1 在 Runtime 实例中组合三种 executor 与最大并发 8 的 `parallel_read` scheduler，使用固定 call slots、owner 创建/关闭的 jobs channel 和有退出路径的 workers；运行 scheduler 单测验证 1、8、超过 8 个 calls 的并发峰值和无 goroutine 泄漏
- [x] 6.2 实现“完整 ready batch durable -> 按 index 逐个 started Sync -> 并行执行”的 admission 流程，取消只在 started acceptance 前阻止执行；运行故障注入测试验证线性化点两侧分别为零次和恰好一次 executor 调用
- [x] 6.3 让 workers 只写各自 result slot，由协调 owner 等待全部 started workers 后按 call index durable results 并提交一个有序 native output commit；使用可控 gates 强制 2/0/1 完成顺序，运行 Runtime/Session 集成测试验证 durable seq、preview 与 Provider bytes 仍为 0/1/2
- [x] 6.4 处理第 k 个 started append 的确定失败和不确定失败：早期已 accepted 调用仍恰好执行并被等待，k 及之后不执行，Session 失败或 poisoned 且不开始下一 sample；运行 fault-injection tests 验证 ownership、唯一 terminal 与后续副作用隔离
- [x] 6.5 覆盖 sibling error、无匹配结果、turn cancellation、tool loop sample/call 上限和 shutdown cleanup，保证普通 tool error 不取消其他 slots；运行 `go test ./internal/runtime -run 'Tool|Parallel|Cancel|Shutdown'` 及 `go test -race ./internal/runtime`

## 7. 应用接线、缓存与端到端验证

- [x] 7.1 让 app 只打开一次 Workspace，并以明确生命周期组合 Read/Glob/Grep executors、完整 Catalog 与 Runtime；运行 app lifecycle tests 验证启动失败无半开放资源、Close 幂等且不暴露半完成工具 schema
- [x] 7.2 更新 Context Tool Catalog segment、token estimate 和 cache fingerprint regression，覆盖三工具稳定排序及 schema/description 改变；运行 `go test ./internal/context ./internal/provider/...` 验证动态 cwd/time/session ID 不污染稳定前缀
- [x] 7.3 更新 headless/TUI/app 适配与 snapshot，使外部 JSONL v1 继续由 adapter 产生且 tool results 不把内部 event/Session/native payload 直接序列化；运行 `go test ./internal/headless ./internal/tui ./internal/app` 验证外部协议 shape 和 stdout/stderr 隔离
- [x] 7.4 为 OpenAI 与 Anthropic 增加 `Glob -> Grep -> Read -> final text` 端到端测试及乱序完成场景，验证同一 turn 只有一个 terminal、call/output pairing 正确且下一 sample 仅在 outputs durable 后开始
- [x] 7.5 增加 uninterrupted 与 restored 等价测试，逐字节比较两家下一请求 canonical bytes、原生 item 顺序、Catalog/cache fingerprint 与冻结 previews；运行对应 app/provider/session 集成测试验证恢复从不重读或重新搜索文件

## 8. 文档同步与质量门

- [x] 8.1 更新根 `AGENTS.md`、总体架构、Tool 系统设计、Roadmap 与 pitfall log，统一“发布前直接替换唯一当前 fixture、旧开发数据 fail closed、稳定发布后兼容窗口另行审批”的规则；用 `rg` 核对不再存在要求开发期 V1/V2 并存或无条件 migration fixture 的矛盾表述
- [x] 8.2 核对 specs、设计、实际类型/record vocabulary、测试文件路径和完成的 Roadmap 状态，运行 `openspec validate add-search-tools-and-ordered-parallelism --strict` 确认 artifacts 一致且未把 `.gitignore`、PCRE、artifact、写工具或 Windows 安全实现写成已完成
- [x] 8.3 运行 `gofmt`、`go vet`、Staticcheck、架构边界、全量测试和 race test 的统一 `make verify`，并确认 `go.mod` 没有新增搜索依赖、没有真实外网/API key、没有 dead code/全局可变 registry/secret 泄漏；仅在全部通过后完成本 change 的实现任务
