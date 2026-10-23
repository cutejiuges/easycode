# Write 实施验收

验收日期：2026-10-24。实施基线为当前 worktree 的 `afb21d6`，没有合入 `feat/session-list-command` 或 `17ad2` 的 `add-command-execution`，未修改参考工程。当前唯一 catalog 是 Read/Glob/Grep/Patch/Write，两个 Provider 各五个 facade。

## 行为与测试证据

- 输入、候选、结果、审批和 Read 返回值为当前封闭强类型；重复/null/缺字段、非法 Unicode、原始与规范编码超限失败关闭。测试见 `internal/tool/write_test.go`、`internal/tool/read_observation_test.go`、`internal/protocol/write_approval_test.go`。
- WritePreparer 只读；同 fd Read 证据在 outputs durable/finalize 后激活。跨 sample/turn、1024 上限、generation 与输出失败测试见 `internal/runtime/read_observations_test.go`、`write_test.go`、`write_schedule_test.go`。
- 创建/覆盖、精确 CRLF/BOM/空文件、权限、祖先/目标变化、禁止覆盖竞争、发布前后取消、Sync/Close/unlink/未知清理项故障见 `internal/tool/builtin/write_unix_test.go`、`write_prepare_test.go`。`write_crash_unix_test.go` 以管道精确阻塞发布边界并实际杀死子进程，不使用 sleep 猜测时序。
- 双 Provider request 与 Write continuation 均有固定 golden；恢复逐字节比较 canonical request、native 顺序和 fingerprint，保留 opaque reasoning/signature/phase。测试见两家的 `write_test.go`、`restore_test.go`。
- 双 Provider 本地 HTTP/SSE 验收覆盖随机字节 chunk、单字节拆分 UTF-8、半包、EOF、取消、无效输入、单调用与聚合 16 MiB 预算，以及候选/执行零副作用。见 `internal/app/write_stream_test.go`。
- 创建新目录/空文件、先读后覆盖、再读验证、no-op、拒绝、截断读取和批准后外部修改见 `internal/app/write_e2e_test.go`。`write_headless_test.go` 覆盖三种 headless 模式与正常最终文本。
- 当前 Session canary 更新为 3；所有当前 fixture 是仓库固定 bytes，旧开发 canary 直接拒绝。`internal/session/write_fixture_test.go` 覆盖 ready/allow/started/result、create/overwrite/denied/conflict/no-op 和混合独占顺序；非法 journal 不被修改。
- `internal/app/write_resume_test.go` 验证四个中断点的本地补偿、零 Provider 请求、不重放文件、不扫描私有残留、恢复后无 grant/Read 授权。原 Session 真实子进程 lease/repair/崩溃释放测试继续纳入完整质量门。
- TUI 固定快照覆盖创建/覆盖、空文件、中文、可见控制字符、无色、窄屏和滚动；键盘隔离、默认 No、反馈、重复提交、中断、resize 与草稿恢复见 `internal/tui/write_test.go`。终端宽度复用已有 x/ansi module，无新增 module/version。

## 质量门

- `make verify`：通过（gofmt、go vet、Staticcheck、分支策略、架构边界、全量测试与 race）。
- `openspec validate add-file-write-tool --strict --no-interactive`：通过。
- `git diff --check`：通过。
- Linux amd64 与 Windows amd64 builtin 测试程序交叉编译：通过。
- 真实文件系统运行平台：macOS。当前没有 Linux runner/容器或虚拟机运行环境，未执行 Linux `RENAME_NOREPLACE` 与目录 Sync 的真实文件系统测试。交叉编译不代表这些运行验证已通过；剩余风险是 Linux 文件系统对相关 syscall/Sync 的支持与错误行为，必须在 Linux 环境运行既有用例确认。unsupported adapter 保持失败关闭，无普通 rename 或直接写入降级。

## 验收修复与限制

混合只读/独占恢复必须按组核验 started/result，不能要求所有 started 早于第一个 result。headless 不接收 UI 专用 approval-resolved 事件。失败 Read 无法确定规范路径时清空观察，避免留下错误授权。上述修复已纳入回归。

Write 只保留基本权限，不承诺 ACL/xattr/owner；完整 Read 仍受原 preview 预算约束。SIGKILL 可能留下私有暂存项，resume 不清理、不重试；该限制与批准设计一致。命令执行、会话列表、artifact、多文件事务及 Add/Delete/Move Patch 扩展仍属独立能力。
