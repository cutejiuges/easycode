# Windows 测试 helper 平台标签修复与追加验收（2026-11-11）

本报告对应前次独立回归后用户明确要求修复的 Windows 全量测试编译阻断。前次失败日志保留于 [原报告](regression-2026-11-11.md)，以下仅记录本次工作区的追加验证。

## 修复与边界

`session_picker_test.go` 和 `session_picker_terminal_test.go` 是平台无关测试，使用的 `listCatalogStub` 与 `listWriterFunc` 却定义在带 `darwin || linux` 标签的 `session_list_test.go` 中，Windows 编译因此缺少三个引用。

本次将两个 helper 原样移至不带平台标签的 `internal/app/session_list_helpers_test.go`，复用既有实现。保留原列表测试的平台标签、所有测试用例及业务断言；没有修改生产代码、能力契约、依赖或功能支持平台。除 gofmt 移除文件末尾空行外，helper 正文与修复前逐字相同。另用 `go list` 核对 Darwin arm64、Linux amd64/arm64、Windows amd64/arm64 五个目标的测试文件选择：共用 helper 与 picker 测试始终纳入，原列表测试仍只纳入 Darwin/Linux。

## 执行记录

本机为 Darwin arm64，Go 1.24.13；测试不使用真实凭据或云端 API。

| 检查 | 结果 |
| --- | --- |
| list/picker 定向测试，count=1 | 通过，exit=0 |
| helper 原文与五目标平台选择核对 | 通过 |
| make verify，不使用测试结果缓存 | 全部通过，exit=0；app 普通 98.550s / race 479.090s |
| 双 Provider 72 例 E2E，race + shuffle=20261111 + count=1 | 全部通过，exit=0；72 个叶用例，无失败、无跳过；62.168s |
| Linux amd64、arm64：全库生产构建与全量测试编译 | 全部通过，exit=0 |
| Windows amd64、arm64：全库生产构建与全量测试编译 | 全部通过，exit=0 |
| OpenSpec change 与 55 个主规格 strict 校验 | 全部通过，exit=0 |

质量门通过临时转发脚本仅为 `go test` 加 `-count=1`，其他命令原样转发；未跳过或弱化 fmt、vet、Staticcheck、分支规约、架构、普通测试与 race。

```sh
EASYCODE_REGRESSION_GO="$(command -v go)" make verify GO=<go-uncached.sh>
go test -count=1 ./internal/app -run 'TestListSessions|Test.*Picker'
go test -race -json -count=1 -shuffle=20261111 ./internal/app -run 'TestFailureBothProvidersRealHookSandboxAndFrozenRestore|TestPostBothBranchesRealExec' -timeout 240s
# 每个 GOOS/GOARCH 组合分别执行：linux/amd64、linux/arm64、windows/amd64、windows/arm64。
GOOS=<目标系统> GOARCH=<目标架构> go build ./...
GOOS=<目标系统> GOARCH=<目标架构> go test -exec=true ./...
openspec validate add-post-tool-use-failure-hooks --strict
openspec validate --specs --strict
```

`-exec=true` 只验证所有测试二进制可编译及链接，不执行目标平台测试。没有 Linux/Windows 原生 runner，因此不宣称原生运行验收，也不扩大命令/Hook 的平台支持。Darwin 全量质量门包含既有双 Provider、缓存/native history、Session 恢复与 lease、权限、安全 opener、TUI 及 Hook 回归。

原始证据：

- [实际命令与全部退出状态](evidence/windows-helper-2026-11-11/results.json)、[全量质量门日志](evidence/windows-helper-2026-11-11/quality-gates.log)、[禁用测试缓存的转发脚本](evidence/windows-helper-2026-11-11/go-uncached.sh)。
- [list/picker 定向结果](evidence/windows-helper-2026-11-11/focused.log)、[helper 原文与五目标文件选择](evidence/windows-helper-2026-11-11/helper-boundary.json)。
- [72 个 E2E 叶用例](evidence/windows-helper-2026-11-11/e2e-cases.json)、[原始 race JSONL](evidence/windows-helper-2026-11-11/e2e-race.jsonl)。
- [Windows amd64 全量测试编译日志](evidence/windows-helper-2026-11-11/windows-amd64-test-compile.log)、[其余七次跨平台构建/测试编译状态及耗时](evidence/windows-helper-2026-11-11/cross-platform-results.json)；同目录保存各目标原始日志。
- [change strict](evidence/windows-helper-2026-11-11/change-validation.log)、[55 个主规格 strict](evidence/windows-helper-2026-11-11/spec-validation.log)、[本轮源码指纹](evidence/windows-helper-2026-11-11/source-fingerprint.json)。

本次 Windows 全量测试编译阻断已消除，追加验证范围内无剩余失败。前次失败日志保持原样，不以本次通过覆盖历史证据。该轮未提交或归档 change；后续最终归档结果见 [archive.md](archive.md)。
