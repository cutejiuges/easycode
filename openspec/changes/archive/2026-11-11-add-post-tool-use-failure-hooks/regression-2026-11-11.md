# PostToolUseFailure 独立回归（2026-11-11）

> 后续 Windows helper 平台标签修复与质量门重跑见 [追加验收](windows-helper-regression-2026-11-11.md)。下文保留当次结果，包括修复前 Windows 测试编译失败。

本轮针对当前工作区重新执行测试，未用首次验收的通过记录替代本次结果。保留已有开发变更；没有提交、归档或修改参考工程。修复了一处实际异常，修复后的 Darwin 全量质量门通过。

## 发现与修复

合法 Hook 命令中的 `<` 会被 canonical JSON 转义为六字节。原始命令未超过 8 KiB，但携带声明的内部信任请求可能超过 decoder 原来的 16 KiB 上限。AgentLoop 因此将合法请求视作无效控制事件并取消回合，实际反馈预算检查还没有开始。

- [修复前端到端失败](evidence/regression-2026-11-11/e2e-before-fix.log)：两家 Provider、三类工具的 escaped-budget 用例均复现。
- [修复前协议失败](evidence/regression-2026-11-11/trust-before-fix.log)：三事件最大命令在 JSON 扩张后均无法往返。
- 修复 `internal/protocol/hooks.go`：信任请求采用 `6 × MaxHookCommandBytes + 4 KiB` 的有界编码预算。原始 8 KiB 限制、未知字段拒绝、精确信任、默认拒绝，以及响应/reset/活动消息的原有限制均保持。
- [修复后定向结果](evidence/regression-2026-11-11/focused-fixed.log)：协议与应用用例通过。新增测试同时验证完整命令不截断、原始超限仍拒绝、编码消息本身仍有上限。

## 用例与覆盖

| 层次 | 本轮执行与业务断言 |
| --- | --- |
| 双 Provider 主端到端矩阵 | 66 例：2 Provider × Patch/Write/Exec × 11 模式；覆盖 model/none、非法输出、exit 2、unmatched、信任拒绝、普通审批拒绝、空反馈、错事件、单项超限、JSON 编码后整段超预算。保持原 Error/Conflict、唯一配对、完整接收或拒绝、无未授权正文及重复副作用。 |
| 双分支真实 Exec | 6 例：两家分别执行 exit 0、exit 1、真实 grep 无匹配 exit 1；两分支均在普通批准前审查，只运行成功事件；预算只预留一次，磁盘 marker 精确为一次执行，模型只收到所选反馈。 |
| 主端到端 race 与乱序 | 上述 72 个叶用例以 race、固定 shuffle 种子 20261111、count=1 完整通过；[逐例清单](evidence/regression-2026-11-11/e2e-cases.json)与[原始 JSONL](evidence/regression-2026-11-11/e2e-race.jsonl)。 |
| 核心边界重复 | domain、extension/hook、tool、runtime、session、executor/process、protocol、tui 共 8 包，以 race、固定种子乱序重复 10 轮；含拒绝/取消、Sync 故障、poisoned 重入、预算、恢复、真实子进程 lease、损坏与尾部修复、shutdown owner 及过期消息隔离。 |
| 解析 fuzz | 30 秒、2 worker，最终 1,429,208 次输入通过；验证两个 Post 事件的 none/model 披露边界、错误/非零退出不带候选正文、原始与编码上限、UTF-8/NUL、严格事件来源与完整字段。 |
| 原生历史与缓存回归 | 全量覆盖两家 canonical request、native 顺序与 opaque 数据、usage/缓存口径、压缩与恢复，以及 OpenAI WebSocket incremental/discard/fallback/reset；没有新建 Provider 兼容路径。 |
| 真实 PTY | 三事件 × 两家，共 6 组，全部 exit=0；默认拒绝、信任/reset、普通批准、独立检查、Ctrl+O 详情、草稿/锚点返回与取消清理。失败路径退出后使用生产 Loader/ReplayPlanner 再验证 conflict/failure/cancelled。 |
| 严格契约 | 三事件的最大原始命令与 JSON 扩张、未知字段和双重大小上限；固定 current fixtures、golden、四字段通知、旧 shape 拒绝、架构依赖与文档一致性检查。 |

新增用例位于 `internal/app/post_branch_regression_darwin_test.go`、`internal/extension/hook/post_output_fuzz_test.go`、`internal/protocol/hooks_test.go`；既有 `failure_hook_e2e_darwin_test.go` 增加 5 种模式。主端到端总数从 36 增至 72，新增 36 例；未删、跳过或弱化原断言。

## 实际命令与结果

所有结果汇总见 [results.json](evidence/regression-2026-11-11/results.json)。

| 检查 | 退出状态与证据 |
| --- | --- |
| 不使用测试结果缓存的 make verify | **0**；[实际日志](evidence/regression-2026-11-11/quality-gates.log)。完整执行分支规约、gofmt、vet、Staticcheck、架构、全量普通测试与全量 race；app 包分别耗时 102.518s / 486.664s。 |
| 72 个主 E2E：race + shuffle + count=1 | **0**；72 个叶用例全部通过。 |
| 8 包核心边界：race + shuffle + count=10 | **0**；[实际日志](evidence/regression-2026-11-11/core-stress.log)。 |
| 输出 fuzz | **0**；[实际日志](evidence/regression-2026-11-11/output-fuzz.log)。 |
| 6 组真实 PTY | **0**；[逐组状态、阶段、字节数与 SHA-256](evidence/regression-2026-11-11/pty-summary.json)，同目录保留原始 PTY 输出。 |
| OpenSpec change 与主规格 strict 校验 | **0**；[change](evidence/regression-2026-11-11/change-validation.log) 与 [55 个主规格](evidence/regression-2026-11-11/spec-validation.log) 全部通过。 |

质量门只通过一个临时 Go 转发脚本，为 `go test` 加 `-count=1`，其他子命令原样转发；没有跳过或改变质量门范围。[转发脚本](evidence/regression-2026-11-11/go-uncached.sh)已保留。

```sh
EASYCODE_REGRESSION_GO="$(command -v go)" make verify GO=<go-uncached.sh>
go test -race -shuffle=20261111 -count=10 ./internal/domain ./internal/extension/hook ./internal/tool ./internal/runtime ./internal/session ./internal/executor/process ./internal/protocol ./internal/tui -run '(Post|Failure|Hook|Owner)' -timeout 600s
go test -race -json -count=1 -shuffle=20261111 ./internal/app -run 'TestFailureBothProvidersRealHookSandboxAndFrozenRestore|TestPostBothBranchesRealExec' -timeout 240s
go test ./internal/extension/hook -run '^$' -fuzz '^FuzzPostHookOutputDisclosureBoundary$' -fuzztime=30s -parallel=2
go test -c -o <app.test> ./internal/app
python3 scripts/smoke-hooks.py <app.test> --event PostToolUseFailure --output-dir <pty-failure>
python3 scripts/smoke-hooks.py <app.test> --event PostToolUse --output-dir <pty-success>
python3 scripts/smoke-hooks.py <app.test> --event PreToolUse --output-dir <pty-pre>
```

## 平台与限制

- Darwin arm64：上述普通、race、沙箱、实际端到端与 PTY 均真实执行。
- Linux amd64：`GOOS=linux GOARCH=amd64 go test -exec=true ./...` 编译通过，[日志](evidence/regression-2026-11-11/linux-compile.log)；没有执行 Linux 原生测试。
- Windows amd64：`GOOS=windows GOARCH=amd64 go build ./...` 生产代码编译通过；[日志](evidence/regression-2026-11-11/windows-build.log)。
- Windows 全量测试编译仍为 **1**：[原始失败](evidence/regression-2026-11-11/windows-test-compile.log)。既有 session picker 测试引用了仅 darwin/linux 构建的 session_list_test.go 内 listCatalogStub/listWriterFunc；本轮未修改这些文件，不把它记录为通过。未执行 Windows 原生测试。
- 不使用真实服务端、凭据或外网模型。双 Provider 协议与模型续写由本地 fixture server 验证，不能据此声称已经做真实云端服务验收。

## 结论与收口

本轮实际发现的信任审查编码异常已修复，修复后上述 Darwin 验收范围没有剩余失败。平台不可执行项与既有编译阻断如上单列。既有原始命令、披露、执行准入、取消、资源 owner、Session 与原生历史契约保持。

proposal/design/specs/tasks 已记录修复，主规格、总体架构、能力清单和 pitfall 已同步。保留修复前失败与修复后通过证据；该轮未归档；后续最终归档结果见 [archive.md](archive.md)。
