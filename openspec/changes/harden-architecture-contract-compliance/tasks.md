## 1. 实现分支与兼容基线

- [x] 1.1 在开始代码修改前确认当前分支为 `fix/architecture-contract-compliance`；若仍是 detached HEAD，则从当前提交创建并切换该分支，并用 `git branch --show-current` 验证精确名称。
- [x] 1.2 记录未修改实现时的质量基线并运行 `make verify`，确认现有测试通过；若失败，先记录与本 change 无关的既有失败，不得通过弱化测试继续。
- [x] 1.3 为六种 JSONL v1 payload 增加固定 identity/time/batch 参数的 canonical bytes 与 checksum 回归，使用不可变期望值而非测试运行时自生成 baseline，并用 `go test ./internal/session -run 'V1|Migration|Compatibility'` 验证。
- [x] 1.4 运行并固定双 Provider 现有 request golden、native commit/restore 和 fingerprint 基线，使用 `go test ./internal/provider/openai ./internal/provider/anthropic -run 'Golden|Request|Restore|Fingerprint|Commit'` 验证变更前期望。

## 2. 分支命名治理

- [x] 2.1 新增 `scripts/check-branch-name.sh`，实现显式参数、可信 CI source branch 和本地 symbolic ref 的解析优先级，以及约定 regex、detached HEAD、主分支和非语义名称拒绝；用脚本直接验证一个 `feat/*` 和一个 `fix/*` 成功、一个 `codex/*` 失败。
- [x] 2.2 新增 `scripts/check-branch-name_test.sh` 覆盖全部允许前缀、kebab-case 边界、`main/master`、用户名/工具前缀、下划线、裸 ticket、detached 和 CI merge-ref/source-ref 选择，并运行该脚本验证全部表驱动 case。
- [x] 2.3 在 `Makefile` 增加分支策略回归 target 并把“规则测试”纳入 `make verify`，同时保持“当前分支检查”为独立 target；运行 `make <branch-policy-test-target>` 和 `make verify` 验证 CI detached checkout 不会被误判。
- [x] 2.4 更新 `.githooks/pre-commit` 并新增 `.githooks/pre-push`，两者复用同一 branch checker，且 pre-commit 继续执行完整 `make verify`；更新 `install-hooks` 权限设置并用临时 Git 分支/显式参数验证两个 hook 的成功与失败路径。
- [x] 2.5 新增 `.github/workflows/verify.yml`，在 pull request source branch 上运行 branch-governance job，并独立运行 `make verify`；新增双语 PR 模板提示语义分支与中英文标题/描述，用 workflow 文件检查和 shell regression 验证 job 名称与传入 branch 一致。

## 3. Canonical JSON、缓存计划与 Transport 边界

- [x] 3.1 在 `internal/codec` 增加不可变 canonical JSON 值对象，构造时校验合法/canonical/大小并在 getter 返回深拷贝；补充非法 JSON、非 canonical bytes、超限和输入/输出变异测试，运行 `go test ./internal/codec`。
- [x] 3.2 重构 `internal/context` 的 Segment/Plan 为私有字段和强类型 constructor，移除 `NewSegment(... any)`，校验 stability、revision、重复 ID、稳定前缀、内容 fingerprint 与版本，并用 `go test ./internal/context` 验证深拷贝和 fingerprint 回归。
- [x] 3.3 将 OpenAI Responses request 拆成强类型 builder 与产生 canonical JSON 的 compiler，更新 request/provider 调用方和测试，并用 `go test ./internal/provider/openai -run 'Request|Golden|Fingerprint'` 验证现有 golden bytes 不变。
- [x] 3.4 将 Anthropic Messages request 拆成强类型 builder 与产生 canonical JSON 的 compiler，更新 request/provider 调用方和测试，并用 `go test ./internal/provider/anthropic -run 'Request|Golden|Fingerprint'` 验证现有 golden bytes 不变。
- [x] 3.5 将 `transport.SSERequest.Body any` 替换为已编译 canonical body 快照，移除 transport JSON marshal，增加空/非法/超限正文与调用方 mutation 回归，并运行 `go test ./internal/provider/transport` 验证实际 HTTP body 与 compiler bytes 逐字节一致。
- [x] 3.6 更新双 Provider uninterrupted/restored request 等价测试，使实际发送 bytes、native item 顺序和 cache fingerprint 使用同一 compiler 产物，并运行 `go test ./internal/provider/openai ./internal/provider/anthropic -run 'Restore|Fingerprint|Integration'`。

## 4. Session 强类型 Record 契约

- [x] 4.1 将 `RecordDraft` 改为不可伪造的 sealed 值类型，为六种 v1 kind 增加 typed constructor 和语义 validator，移除导出 draft 中的 `Payload any`，并用 `go test ./internal/session -run 'Draft|Codec'` 验证 kind/payload mismatch 无法构造。
- [x] 4.2 为六种 v1 kind/revision 增加专属 strict decoder，删除 `DecodePayload() (any, error)` 与 registry 的 reflect type，保留 switch/常量元数据，并用未知字段、尾随 JSON、非法值和错误 revision 表驱动测试验证 `go test ./internal/session -run 'Decode|Replay'`。
- [x] 4.3 更新 writer、record builder、ReplayPlanner 和 lifecycle 使用 typed draft/decoder，保证 unknown optional record 仍作为有界 opaque JSON 保留，并运行 `go test ./internal/session -run 'Writer|Replay|Loader'`。
- [x] 4.4 更新 Runtime 与 app/session service 的所有 Session draft 调用点和测试 fake，禁止跨包直接拼装 kind/payload，并运行 `go test ./internal/runtime ./internal/app -run 'Runtime|Session|Resume'`。
- [x] 4.5 运行 `go test ./internal/session -run 'V1|Migration|Compatibility|Replay|Writer'`，逐字节确认 v1 fixture 前缀、canonical records、checksum、append 后 seq 和 ReplayPlan 均未漂移。

## 5. 配置与 Session 的句柄绑定安全

- [x] 5.1 为配置加载增加带 build tag 的平台 secure opener，以 no-follow 方式打开并从同一 handle 校验类型、大小和权限；移除 `Lstat` 结果参与打开后安全决策，并运行 `go test ./internal/config`。
- [x] 5.2 为配置增加真实 symlink 和私有 opener 故障注入测试，在确定交换点覆盖 symlink/目录/宽权限文件替换，断言只读取已校验 handle 或安全失败，且用 `go test ./internal/config -run 'Symlink|TOCTOU|Permission'` 验证无 sleep/概率竞态。
- [x] 5.3 把 Repository 纯内存配置与 `OpenOrCreate` 外部资源获取分离，移除执行 mkdir/open 的 `NewRepository` 路径，并将 service/resource helper 改为 `Open`/`Create`/`Start` 语义；用单测验证纯构造不会创建目录、文件、lease 或 goroutine。
- [x] 5.4 新增 Session 平台 secure path walker，相对于已打开父目录 handle 逐级创建/打开日期目录和 journal，校验实际 handle 的目录/普通文件类型与权限，并确保 lease、Loader、repair、writer transfer 复用最终 journal handle；运行 `go test ./internal/session -run 'Repository|Lease|Lifecycle'`。
- [x] 5.5 增加 Session 真实 symlink 与确定性路径组件交换测试，覆盖数据根、日期目录、journal、create/resume、repair 前替换和数据根外 bytes 不变，并运行 `go test ./internal/session ./internal/app -run 'Symlink|TOCTOU|Repair|Resume'`。
- [x] 5.6 更新 app session service 的 open/close ownership 与错误映射，验证 create/resume 失败路径释放所有临时 handle 且不产生网络副作用；运行 `go test ./internal/app -run 'SessionService|Resume|Process'`。
- [x] 5.7 对本机运行真实安全测试，并至少执行 `GOOS=linux GOARCH=amd64 go build ./...` 与 `GOOS=windows GOARCH=amd64 go build ./...` 验证平台文件/build tags；任何无法提供等价安全语义的平台必须显式 fail closed 并记录风险。

## 6. Provider PreparedSample 与 Durable 提交

- [x] 6.1 重构 `NativeCommitEnvelope` 的校验/复制 API，确保 payload 深拷贝且非法 clone 返回 error；强化 `PreparedSample` 的 nil/零值/fresh/finalize-once 不变量，并运行 `go test ./internal/provider -run 'NativeCommit|PreparedSample'`。
- [x] 6.2 在 Runtime durable append 前重新验证 completed sample 和 envelope，并通过 typed draft 写入；无效 sample 只能 durable 失败收口，不能写 native commit、completed 或调用 finalizer，使用 `go test ./internal/runtime -run 'Prepared|Completion|Durable'` 验证顺序。
- [x] 6.3 更新双 Provider commit/finalizer 适配并验证 append/Sync 失败、重复 finalize、零值 sample 和恢复后下一请求等价，运行 `go test ./internal/provider/... ./internal/runtime -run 'Commit|Prepared|Restore|Sync|Finalize'`。

## 7. Runtime Context 与生命周期 Ownership

- [x] 7.1 将 `ChatSession.Submit` 改为 context-first API，nil context 在 admission 前失败，接受后只保存 cancel/done 而不长期保存 context；补充取消传播、并发提交、重复 Interrupt 和关闭后提交测试，运行 `go test ./internal/runtime -run 'ChatSession'`。
- [x] 7.2 更新 headless 最小接口和 runner 将 `Run` context 传给 Submit，删除 nil context 到 background 的静默降级，并用 `go test ./internal/headless` 覆盖 nil、取消、输出失败和唯一终态。
- [x] 7.3 更新 TUI 异步 submit command 与 app 装配，在 Model 不保存 context 的前提下传递本次操作 context；更新固定尺寸 snapshot，并运行 `go test ./internal/tui ./internal/app -run 'TUI|Model|Snapshot|Headless'`。
- [x] 7.4 将执行 I/O、随机生成或启动 goroutine的误导性 `New*`/`new*` 生命周期入口改为 `Open`、`Generate` 或 `Start`，更新 domain/session/app 调用点，并通过 AST 搜索和 `go test ./internal/domain ./internal/session ./internal/app` 验证构造器纯内存约束。
- [x] 7.5 用 channel/已取消等待 context 的确定性握手替换 Runtime shutdown 测试中的 `time.After` 否定证明，验证首次超时不放弃 owner、transport 强制关闭后相同 done 最终完成；运行 `go test ./internal/runtime ./internal/app -run 'Shutdown|Close|Cancel'`。
- [x] 7.6 运行 `go test -race ./internal/runtime ./internal/headless ./internal/tui ./internal/app`，验证 goroutine owner、channel close、context cancel 和 cleanup 顺序无竞态或泄漏。

## 8. OpenAI Responses Sample 状态机

- [x] 8.1 将 OpenAI reducer 改为每 stream 独立的有状态 sample reducer，固定 created response ID、合法事件顺序和唯一 terminal，并用 table tests 覆盖 duplicate created、missing created、ID conflict、duplicate/after-terminal、unknown active event。
- [x] 8.2 更新 OpenAI provider consumer 使用 reducer state，不再忽略 response ID；失败/incomplete 必须校验匹配 identity 且不得提交 staged history，运行 `go test ./internal/provider/openai -run 'Reducer|Provider'`。
- [x] 8.3 为所有 OpenAI httptest/SSE fixtures 补齐 `response.created`，增加冲突 ID 和非法顺序 integration regression，并运行 `go test ./internal/provider/openai -run 'Integration|Stream|Restore'` 验证 unknown event 兼容和 native item 顺序不变。

## 9. 占位例外与架构自动守卫

- [x] 9.1 在 design allowlist 的十二个文件中仅添加中文结构化 TODO，分别标明 P2/P3/P6/P7/P8、保留原因和后续 OpenSpec 的启用/删除条件；更新 `AGENTS.md` 登记 exact allowlist，并用 `rg 'TODO\(P[0-9]'` 与人工 diff 确认未增加实现或生产消费者。
- [x] 9.2 将 transport 导出 package sentinel 改为不可导出 sentinel 加稳定 predicate，审计全部生产 package-level `var` 只保留不可导出 sentinel 和接口断言，并运行 `go test ./internal/provider/... ./internal/session` 验证错误分类不变。
- [x] 9.3 新增 `internal/architecture/architecture_test.go`，用 AST/import graph 校验依赖方向、核心导出 API/draft 无 `any`、生产 package var 白名单，并运行 `go test ./internal/architecture` 验证对合规代码通过、测试 fixture 对违规样例能失败。
- [x] 9.4 在 architecture test 中加入从 `cmd/easycode` 的 production reachability 与 placeholder TODO allowlist 检查，禁止 Runtime/app 导入占位能力或新增未登记包，并运行 `go test ./internal/architecture -run 'Placeholder|Reachability'`。
- [x] 9.5 在集中守卫覆盖等价断言后删除或缩减 provider/headless 的源码字符串扫描测试，运行 `go test ./internal/architecture ./internal/provider ./internal/headless` 验证没有规则缺口或重复冲突。

## 10. 文档与仓库治理同步

- [x] 10.1 更新 `README.md`，把 Tool/extension/Subagent/telemetry 标为目标/批准占位，把 compacted cross-provider fork 标为 P4 计划，并补充分支 regex、示例、hook 安装和 GitHub required checks 管理员步骤；用文档链接检查和 `rg` 确认不再把延期能力写成已实现。
- [x] 10.2 更新总体架构与 Roadmap，区分当前 P2 实现、此次基础契约强化和 P3/P4/P6/P7/P8 延期范围，并核对所有受影响 spec/test 路径存在。
- [x] 10.3 在 pitfall log 记录 FD-bound security、typed Session、canonical request ownership、prepared sample 重验、OpenAI identity state 和 branch governance 的原因、未采用方案与回归路径；确认没有出现需要新增 ADR 的长期决策，若出现则暂停并先更新本 change。
- [x] 10.4 更新 `.github/pull_request_template.md` 与仓库治理说明，明确 `branch-governance`、`verify` 两个 required checks 和禁止 direct push 需要管理员在 GitHub 外部启用；检查文档不得声称 ruleset 已自动生效。

## 11. 综合验收

- [x] 11.1 运行 `scripts/check-branch-name_test.sh`、当前分支 checker、全部新增 focused tests 和 `git diff --check`，确认分支策略、格式和局部回归全部通过。
- [x] 11.2 运行双 Provider golden/cache/native/restore suite 与 Session migration/lease/TOCTOU suite，确认 uninterrupted/restored canonical bytes、原生 item 顺序、fingerprint、JSONL v1 bytes 和数据根外 bytes 全部等价或不变。
- [x] 11.3 运行 `openspec validate harden-architecture-contract-compliance --strict`，确认 proposal、九个 delta specs、design 和 tasks 一致且所有 requirement/scenario 可解析。
- [x] 11.4 运行完整 `make verify`，确认 gofmt、go vet、Staticcheck、architecture tests、全量 tests 和 race tests 全部通过；任何无法运行的检查必须记录原因与风险，禁止声称全部通过。
- [x] 11.5 对最终 diff 执行职责/依赖、secret、dead code、placeholder allowlist、文档现状和外部 GitHub ruleset 待办审计；确认没有 JSONL/wire/capability 漂移、未授权未来功能或无删除条件 TODO 后再进入 spec sync/review，暂不归档。
