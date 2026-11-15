# 2026-11-15：PR #40 的 Linux 静态检查修复

## 失败与根因

[初次 CI](https://github.com/cutejiuges/easycode/actions/runs/43702628474) 在 Ubuntu 的 `go tool staticcheck ./...` 阶段失败：`internal/app/post_output_helpers_test.go:28:6: func mustEntryInputEvidence is unused (U1000)`。当时尚未进入全量测试阶段，分支命名和 vet 已通过。

`mustEntryInputEvidence` 只由 `post_hook_e2e_darwin_test.go` 和 `post_branch_regression_darwin_test.go` 使用，却位于通用 `_test.go` 文件。Darwin 有真实消费者，Linux 编译上下文没有，因而本地 Darwin 检查与 Linux 结果不同。原跨平台 `go test -c` 只证明可编译，不会报告未引用的函数，不能替代对应平台的 Staticcheck。

## 修复

把该辅助函数原样迁入 `post_output_helpers_darwin_test.go`，文件名与 `//go:build darwin` 同时限定其真实消费者的平台；通用 `testModelOutputs` 继续留在原文件。生产代码、接口、测试用例、业务断言及 CI 工作流均未改变，没有新增静态检查抑制项。该调整只涉及测试辅助代码的构建范围，既有 OpenSpec 需求和 49 项实施任务保持不变。

## 实际验证与平台边界

- 修复前，使用仓库锁定版本、为宿主构建的 Staticcheck 分析器，在 `GOOS=linux GOARCH=amd64` 下复现同一条 U1000；见 [linux-staticcheck-before.log](evidence/ci-followup-2026-11-15/linux-staticcheck-before.log)。
- 修复后，Darwin/arm64、Linux/amd64 的全库 Staticcheck 均通过；见 [staticcheck-contexts.json](evidence/ci-followup-2026-11-15/staticcheck-contexts.json)。
- 两个实际调用该辅助函数的 Darwin 后置执行回归均重新通过；见 [命令与退出码](evidence/ci-followup-2026-11-15/darwin-post-regression.json)、[输出](evidence/ci-followup-2026-11-15/darwin-post-regression.log)。
- Windows/amd64 全库 Staticcheck 仍有 53 条 U1000。对父基线 `8492ec25c489605f7ef6a3b42f15b86842f8717e` 的临时只读快照执行同一分析，结果集合完全相同，新增为零；见 [基线对照](evidence/ci-followup-2026-11-15/windows-staticcheck-baseline.json)、[原始基线](evidence/ci-followup-2026-11-15/windows-staticcheck-main-baseline.log)。本次不扩大为既有 Windows 支持范围的整改，也不声称 Windows 全库静态检查通过。

宿主分析器执行跨目标静态分析，不代表目标平台原生测试。完整本地 `make verify` 由正常 pre-commit/pre-push hooks 执行；Linux 原生结果以 [PR #40](https://github.com/cutejiuges/easycode/pull/40) 最新提交的 Ubuntu 检查为准。此前归档证据原样保留，本记录补充其平台验证范围。

复现方式：先为当前宿主构建工具，再设置目标平台分析；避免直接用目标 GOOS 构建并执行工具本身而触发 `exec format error`。

```sh
go build -o /tmp/easycode-host-staticcheck honnef.co/go/tools/cmd/staticcheck
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 /tmp/easycode-host-staticcheck ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 /tmp/easycode-host-staticcheck ./...
```
