## Context

参见 [proposal.md](proposal.md) 的动机。当前 `make verify` 已通过，因此本变更不是修复现有测试失败，而是消除测试尚未覆盖的契约缺口：配置和 Session 仍存在 `Lstat` 后再打开的窗口；`RecordDraft.Payload` 与 `DecodePayload` 暴露动态类型；transport 而非 Provider 拥有 request serialization；`PreparedSample` 的非法复制可退化为零值；ChatSession 自建 background context；OpenAI reducer 不关联 created/completed ID；架构测试只扫描少数文件的源码字符串；分支命名没有自动门禁。

仓库已有不可变 JSONL v1 fixture、双 Provider request golden、durable-before-memory 顺序和跨进程 lease。设计必须在收紧边界时保留这些已发布行为。Tool、extension、Subagent 和 telemetry 已由用户确认为暂存占位，本变更只记录结构化 TODO 与 allowlist，不实现或删除它们。

## Goals / Non-Goals

**Goals:**

- 让安全判断、后续读写和 lease 绑定同一个实际文件或目录句柄，并用可确定触发的故障注入证明 TOCTOU 行为。
- 用 sealed typed draft 和 revision-specific codec/validator 替代 Session 反射与动态 payload，同时逐字节保持 JSONL v1。
- 形成 `Provider compiler -> immutable canonical JSON -> transport` 的单向请求边界，并让 cache plan 只持有深拷贝值对象。
- 固定 turn context、prepared commit、OpenAI stream terminal 和 shutdown cleanup 的唯一 owner 与线性化点。
- 以可执行架构测试和本地/CI 共用脚本固化 AGENTS 约束，避免同类违规回归。

**Non-Goals:**

- 不实现 Tool executor/schema 消费、Hook、MCP、Plugin、Skill、Subagent、OpenTelemetry、SQLite、compaction、跨 Provider fork 或 P3/P4/P6/P7 的其他功能。
- 不修改 JSONL envelope/payload revision、Provider wire、RuntimeEvent/Headless JSON 外部协议或 capability flags。
- 不重写既有 journal、fixture、golden、Git 历史或已归档 OpenSpec change。
- 不把本次仓库文件提交等同于 GitHub ruleset 已启用；托管平台 required checks 仍需管理员配置。

## Decisions

### 1. 文件安全使用平台适配的 descriptor/handle-relative walker

配置文件通过平台专属 secure opener 以 no-follow 语义取得句柄，再从该句柄执行类型、大小和权限检查及读取。Session 数据根由显式 `OpenOrCreate` 生命周期入口取得目录句柄；日期目录和 journal 相对于当前父目录句柄逐级打开或创建，每一级都从实际句柄验证类型与权限。journal lease、Loader、repair 和 writer transfer 继续使用最终同一文件句柄。

Unix 实现使用 `golang.org/x/sys/unix` 已有依赖提供的 `open/openat`、`mkdirat`、`O_NOFOLLOW`、`O_DIRECTORY`、`fstat` 与目录 `fsync`；Windows 使用 handle-relative、拒绝 reparse point 的平台 API，并从同一 handle 校验文件类型。其他无法提供等价保证的平台返回明确不支持错误，禁止回退到 `Lstat` + `Open`。平台文件使用显式 build tags，避免把 `_unix.go` 文件名误认为 Go 自动识别的 GOOS 后缀。

测试注入放在实例持有的私有操作接口或 handle factory 中，不使用可变包级函数。真实 symlink 用例验证操作系统行为；交换点 fake 在“父句柄已取得”“目标即将打开”“句柄已打开”处用 channel 握手，确定性替换路径，不依赖 sleep 或概率竞态。

未采用：继续使用 `Lstat` 后 `Open`；它无法证明检查对象就是读取对象。也不只依赖 containment string check；它不能约束 symlink、mount 或文件类型。

### 2. Session draft 成为 sealed typed value，registry 不再持有 reflect type

`RecordDraft` 保留为跨 runtime/session 的值类型，但所有字段改为不可由包外直接写入；六个 v1 kind 分别提供 typed constructor。constructor 立即执行 revision-specific validator 和 deterministic payload encoding，并保存独立 `json.RawMessage`。writer 只接受这些已准备 draft，不再接收 `Payload any`，也不能由调用方组合出 kind/payload mismatch。

每个 v1 kind 同时具有对应严格 decoder；decoder 检查 kind、payload version、requirement、未知字段、尾随 JSON 和语义不变量。ReplayPlanner 直接调用目标 decoder，不再通过 `DecodePayload() (any, error)`、反射分配和 type assertion。registry 仅用常量/switch 返回 revision、requirement、cardinality 与 placement，不持有 reflect type、map 或全局可变状态。

v1 struct JSON tag、字段顺序、omitempty、canonical codec、envelope 和 checksum 算法保持不变。新增固定参数的 all-kinds byte regression，并继续用仓库内 migration fixture 验证 load/replay/append 不重写前缀。

未采用：为异构 payload 定义 `interface{}` registry callback；它只是把 `any` 从 draft 移到 registry。也不引入泛型 union；六个稳定 revision 用显式 switch 更清楚，且未来 revision 可以独立增加 codec。

### 3. Canonical JSON 是不可变值对象，序列化归 Provider compiler 所有

在 `internal/codec` 增加持有私有 byte slice 的 canonical JSON 值对象：构建时验证合法、canonical 与大小边界，读取时返回副本。OpenAI/Anthropic 分别保留强类型 request builder，再由各自 compiler 生成该值对象；compiler 的 bytes 直接用于 request golden、cache segment 和 transport。

`SSERequest` 不再暴露 `Body any`，而持有 canonical JSON 值对象或在构造时深拷贝的 canonical bytes。transport 只验证 method/path/header/body 上限并发送快照，不再调用 JSON marshal。这样 request bytes 只有一个 owner，恢复前后和实际网络发送的比较使用同一产物。

`context.Segment` 和 `Plan` 字段私有化。Segment 只接收 canonical JSON 值对象以及已校验的 ID/stability/revision；Plan constructor 返回 error 并一次完成版本、重复 ID、稳定前缀和 fingerprint 校验。所有 bytes/slice getter 深拷贝。Provider request tests 使用 compiler 结果构造 segment，不再把任意 Go value 交给 `NewSegment(... any)`。

未采用：让 transport 继续 marshal typed request，因为这会让 golden/fingerprint bytes 与真正发送 bytes 存在两个生成点。也不公开可变 `[]byte`/`[]Segment` 字段。

### 4. PreparedSample 失败显式化，Runtime 在 durable append 前重验

`NativeCommitEnvelope` 提供返回 error 的校验/复制路径；任何 getter 都深拷贝 payload。`PreparedSample.Envelope` 调用该路径，因此 nil、零值或内部非法状态不能成功返回零值 envelope。`NewPreparedSample` 复制已校验 envelope 并要求 finalizer 非空，`Finalize` 仍以原子状态保证恰好一次。

Runtime 收到 completed terminal 后依次检查 fresh sample、读取并重验 envelope、构造 typed Session native-commit draft，再执行 durable batch。任何一步失败都只追加 `turn_failed`；只有 batch Sync 成功才 finalize 内存 history，再发布 `turn_completed`。这保留现有 durable-before-memory 顺序并封住恶意或错误 Conversation 实现。

未采用：信任 Provider constructor 是唯一入口。`PreparedSample` 是导出类型，Go 零值始终可构造，公共方法必须自行拒绝非法状态。

### 5. context 由宿主传入，cleanup 由 ChatSession 的单一完成信号收口

`ChatSession.Submit` 调整为 `Submit(context.Context, string)`，nil context 在 admission 前失败。每个被接受 turn 只保存其派生 cancel function 和 `done` channel，context 本身不进入长期 struct；Interrupt、调用 context 取消和 Shutdown 最终汇聚到相同 cancel/完成路径。headless 直接传入 `Run` context；TUI 的异步 command 在执行提交时持有该次操作 context，Model 不保存 context 字段。应用 owner 在 Bubble Tea 或 headless 返回后执行统一关闭序列。

I/O 或 goroutine 生命周期入口改用动词：`newSessionService`/`newChatResources` 改为 `open...`，Repository 使用 `OpenOrCreate...`，writer 继续使用 `Start...`。UUID 创建入口使用 `Generate...` 表达随机生成而不是纯值构造。`New*` 只做参数校验、复制和内存装配。

Shutdown 超时时不放弃资源：第一次等待超时后，app owner 关闭 Provider transport 解除阻塞，再等待同一 ChatSession done，随后依次关闭 writer、Repository 和 Provider。测试用“已取消的等待 context -> 必须返回 deadline/cancel -> 释放故障点 -> 第二次等待完成”的握手证明阻塞，不再用 `time.After(20ms)` 证明某事尚未发生。

未采用：在 ChatSession 内部创建 `context.Background()`；它切断宿主取消。也不为每次等待创建 `WaitGroup.Wait` helper goroutine。

### 6. OpenAI reducer 是每 sample 独立状态机

把无状态 `reduceResponsesEvent` 替换为每次 stream 新建的 reducer，状态至少包含 `created`、`responseID`、`terminal` 和已归并 items。`response.created` 恰好一次建立 identity；所有支持事件要求 active identity；completed/failed/incomplete 解码 response reference 并与 identity 比较；任何 terminal 恰好一次，terminal 后调用 reducer 一律错误。未知 well-formed event 仅在 active、non-terminal 状态被忽略。

Provider consumer 不再忽略 reducer 返回的 response ID。测试和 httptest fixtures 补齐真实 `response.created`，增加 duplicate/conflict/order/after-terminal table cases；unknown-event forward compatibility 仍保留。

未采用：只在 Provider loop 中保存一个字符串并继续用无状态 reducer，因为状态转换和单元测试会分散在两个 owner 中。

### 7. 占位代码使用显式、可审计的临时例外

下列文件是本变更唯一允许的占位范围：

- `internal/protocol/command.go`
- `internal/protocol/event.go` 中的未来 `ItemID`/`CallID` 字段
- `internal/domain/types.go` 中的 `ItemID`/`CallID`
- `internal/tool/tool.go`
- `internal/tool/builtin/specs.go`
- `internal/extension/extension.go`
- `internal/extension/hook/hook.go`
- `internal/extension/mcp/mcp.go`
- `internal/extension/plugin/plugin.go`
- `internal/extension/skill/skill.go`
- `internal/subagent/control.go`
- `internal/telemetry/logger.go`

每处添加中文结构化 TODO，格式包含 Roadmap 阶段、当前保留原因，以及“由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除”的退出条件。Tool schema 不接入 Provider，其他占位包不接入 Runtime/app。`AGENTS.md` 记录该 exact allowlist，并继续禁止任何新占位；新增或扩大例外必须先更新 OpenSpec。

未采用：删除占位，因为用户明确要求保留。也不把 TODO 写成无阶段、无验收条件的永久注释。

### 8. 架构守卫解析 AST 和 import graph

新增集中式 architecture test，使用 Go parser/AST 构建生产文件的内部 import graph，并执行：

- domain/protocol 不依赖 I/O、Provider、Session、Runtime、UI 等上层包；Provider/Session/Context/Tool/Extension/Subagent 不反向依赖 Runtime/app/TUI；Runtime 不依赖具体 Provider wire 或宿主；headless/TUI 不依赖具体 Provider、Session 或彼此。
- 生产包级变量只允许不可导出 `errors.New` sentinel 和 `var _ Interface = ...` 编译期断言；现有 transport 导出 sentinel 改为私有 sentinel 加 `Is...` predicate。
- domain/protocol/provider/context/session 的导出 constructor、interface method 和 draft 不出现 `any`/`interface{}`；受控 `json.RawMessage` 允许。
- 从 `cmd/easycode` 不可达的 production package 必须位于占位 allowlist并含匹配 TODO；Runtime/app 不能导入这些占位能力。

现有 provider/headless 字符串扫描测试在集中守卫覆盖后删除或缩减，避免两套不一致规则。

未采用：继续增加字符串搜索目录；alias import、分组 import、build tag 和新目录会漏报。

### 9. 分支规则由一个 POSIX 脚本驱动本地和 CI

`scripts/check-branch-name.sh` 接受可选显式 branch 参数；否则按可信 CI source branch 变量、再按本地 symbolic ref 解析。普通开发分支匹配：

```text
^(feat|fix|refactor|docs|test|ci|build|perf|chore|revert)/[a-z0-9]+(-[a-z0-9]+)*$
```

本 change 的实现分支为 `fix/architecture-contract-compliance`。脚本拒绝 detached HEAD、`main`、`master`、`codex/*`、用户名/工具前缀、下划线和空泛 ticket-only 后缀。shell regression table 同时覆盖 accept/reject 与 CI source selection。

pre-commit 和 pre-push 调用同一脚本；pre-commit 继续执行 `make verify`。GitHub Actions 在 pull request 上用 source branch 运行 branch job，并独立运行 `make verify`；主分支 push 可运行 verify，但不把合并后的 `main` 误当 PR source。README/PR template 说明命名规则，README 或 AGENTS 记录需由管理员把两个 jobs 配成 required checks 并禁止 direct push。

未采用：只依靠文档或在三个 hook/workflow 中复制 regex；规则会漂移。也不通过 commit message 猜测分支意图。

### 10. 文档区分当前实现、目标架构和批准例外

README 的架构图和 capability 表明确标注 Tool/extension/Subagent/telemetry 是目标或占位；“跨 Provider 自动创建 compacted fork”改为 P4 计划，不再描述为当前行为。总体架构保留长期目标但链接当前阶段；Roadmap 记录本次 P2 基础契约收紧及仍延期能力；pitfall log 增加 FD-bound security、typed Session、canonical request ownership 和 branch governance 的原因与回归位置。

这些是对现有决策的落实，不新增 ADR。若 apply 阶段发现必须改变持久化格式、外部协议或长期 Provider 策略，应暂停并先更新本 change，而不是临时增加 ADR 或静默扩张。

## Affected Files

预计实现文件范围如下；测试可在同目录按现有命名补充，但不得越过本设计的 capability 边界：

- 安全打开与 Repository 生命周期：`internal/config/config.go`、`internal/config/config_test.go`、新增 `internal/config/secure_open_*.go`；`internal/session/repository.go`、`repository_test.go`、`lifecycle.go`、`lifecycle_test.go`、`lease.go`、`lease_test.go`、新增 `internal/session/secure_path_*.go`；`internal/app/session_service.go` 及测试。
- typed Session 与兼容：`internal/session/types.go`、`registry.go`、`codec.go`、`writer.go`、`replay.go`、`loader_test.go`、`codec_test.go`、`writer_test.go`、`replay_test.go`、`migration_v1_test.go`；`internal/runtime/runtime.go`、`internal/app/session_service.go` 及相关 app/runtime 测试。
- canonical request/cache：`internal/codec/json.go`、`json_test.go`；`internal/context/cache_plan.go`、`cache_plan_test.go`；`internal/provider/transport/client.go`、`client_test.go`；双 Provider 的 `request.go`、`provider.go`、request/provider/integration/restore/history tests 与现有 golden。
- Provider/Runtime lifecycle：`internal/provider/provider.go`、`provider_test.go`、`internal/provider/transport/sse.go`；`internal/runtime/session.go`、`runtime.go` 及测试；`internal/headless/runner.go` 及测试；`internal/tui/model.go` 及 model/snapshot tests；`internal/app/app.go`、session/headless/resume e2e tests；`internal/domain/id.go`、`id_test.go`。
- OpenAI 状态机：`internal/provider/openai/reducer.go`、`provider.go`、`reducer_test.go`、`provider_test.go`、`integration_test.go`。
- TODO-only 文件：Decision 7 列出的十二个文件；不在这些文件中增加实现或生产消费者。
- 架构门禁：新增 `internal/architecture/architecture_test.go`，并在覆盖等价后调整或删除 `internal/provider/boundary_test.go`、`internal/headless/boundary_test.go`。
- 分支与 CI：`AGENTS.md`、`Makefile`、`.githooks/pre-commit`，新增 `.githooks/pre-push`、`scripts/check-branch-name.sh`、`scripts/check-branch-name_test.sh`、`.github/workflows/verify.yml`、`.github/pull_request_template.md`。
- 文档：`README.md`、`docs/architecture/overall-architecture.md`、`docs/roadmap/product-roadmap.md`、`docs/roadmap/pitfall-log.md`。除非实现发现新的长期不可逆决策，否则不新增 ADR。

## Risks / Trade-offs

- [平台安全 API 的语义和 build tag 容易分叉] → 每个平台实现同一私有 conformance suite；当前平台运行真实 symlink/TOCTOU 回归，受支持目标至少执行 cross-build；不安全平台 fail closed。
- [typed draft 重构意外改变 v1 JSON 字段顺序或 checksum] → 在替换调用方前先建立六种 payload 的固定 byte baseline，并让 migration fixture 走生产 Loader/ReplayPlanner；任一 byte drift 都阻止合并。
- [canonical value object 增加复制成本] → 请求和 record 已有 16/64 MiB 上限，边界只复制一次以换取不可变性；用 benchmark/alloc 观察，不引入共享可变 buffer 优化。
- [更严格 OpenAI 状态机可能拒绝不完整的兼容网关] → 该行为符合 Responses event identity 契约；以明确 stream protocol error 失败，不静默接受可能串流的响应。若要支持偏离协议的网关，必须另开 wire/capability change。
- [大范围内部 API 变更造成合并冲突] → 按 tasks 的 leaf-to-root 顺序提交，每一组保持可测试；不把未来功能夹带进来。
- [本地 hook 可被跳过、GitHub ruleset 不在 Git 中] → GitHub Actions 提供权威 required check，文档明确管理员配置和审计步骤；不声称仓库文件已自动保护主分支。
- [占位例外被误当成已实现能力] → README/Roadmap/TODO 同时标记，architecture test 禁止生产可达和新增未登记占位。

## Migration Plan

1. Apply 前从当前 detached HEAD 切换到 `fix/architecture-contract-compliance`；不改写已有提交历史。
2. 先加入兼容 baseline、架构守卫和 branch script tests，再按 canonical value、typed Session、安全 Repository、Provider/Runtime、OpenAI 状态机、宿主适配、TODO/文档/CI 的顺序实施。
3. 每个边界先运行对应 package tests；Session/Provider 修改后运行 migration fixture、双 Provider golden、uninterrupted/restored canonical bytes 与 race tests。
4. 执行 shell branch regression、workflow 静态检查（如仓库可用）、`openspec validate --strict` 和完整 `make verify`。无法执行的跨平台或 GitHub 托管配置检查必须列为交付风险。
5. 合并 workflow 后，由管理员把 branch-governance 与 verify jobs 设为 `main` required checks，并禁止 direct push；在设置完成前文档状态保持“待外部启用”。
6. 本变更不需要数据迁移。回滚可恢复旧二进制，因为 JSONL v1 和 wire bytes 不变；但不得让新旧版本同时写同一活动 thread。若发现任何持久化 byte drift，回滚代码并保留原 journal，不执行就地重写。
