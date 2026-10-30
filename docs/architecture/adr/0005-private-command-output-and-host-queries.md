# ADR-0005：私有命令输出与独立宿主查询

- 状态：accepted
- 日期：2026-10-29
- Roadmap 阶段：P3
- OpenSpec：[add-command-output-artifacts](../../../openspec/changes/archive/2026-10-29-add-command-output-artifacts/design.md)

## 背景与选择

头尾预览会省略命令中间输出，重启后原有 TUI 瞬态详情也不能恢复。采用私有、按流保存的连续前缀以及独立宿主查询，使用户与模型能够读取相同的已提交内容。

Claude Code 的紧凑主线、按需展开与输出落盘提供交互参考；Codex 的有界头尾缓冲、稳定调用身份及进程 owner 提供内核参考。参考路径和版本记录在 change design 中。拒绝把文件路径直接交给 TUI、把双流合并为虚构总顺序、以异步 flush 代替 durable Sync，或以文件存在推断命令成功。

## 决策与职责

`tool` 定义不可变 descriptor、保存状态、ReadArtifact 和消费端接口；`tool/builtin` 只依赖这些接口。`session/artifact` 显式 Open/Start，负责 FD 绑定、配额、发布、校验读取和孤儿清理。Runtime 在 result Sync 后授权引用并通知内存历史投影；app 持有查询 owner，protocol 仅保存展示值，TUI Update 发起异步查询，View 不执行 I/O。

执行 owner 保留同步写入、seal 和清理的所有权。磁盘故障不停止排空，阻塞 I/O 不能因取消而丢弃资源。存储完整性与 exit、预览省略独立。每流 16 MiB、调用预留 32 MiB、thread 256 MiB、root 1 GiB；预算统计原始数据，不含目录开销。root accounting lease 串行预留，调用 lease 保持到清理结束，崩溃后按实存保守对账。没有自动 TTL。

Session 私有根与 workspace 不得相同或互为祖先，也不得位于 sandbox 的其他读写根下。启动与执行前均通过实际目录身份验证；只规范化 macOS 系统级别名，私有 symlink 拒绝。文件权限与类型检查绑定实际 FD，发布不覆盖同名对象。此边界不声称抵御产品之外的同用户恶意进程。

文件 Sync、no-replace 发布、目录 Sync、result Sync、native output Sync 顺序固定。只有 durable descriptor 可以授权；持有 thread lease、完成全量 replay 后才能回收本线程未引用文件。对象删除或损坏仅影响读取，不改变冻结预览和原生恢复。

## Provider、缓存与当前格式

两家 Provider 都增加 structured ReadArtifact。catalog 共七项能力，稳定 schema 进入 fingerprint；对象 ID、路径、配额使用量、时间和查询动作不进入稳定前缀。ReadArtifact 属于 parallel_read，不产生 workspace ReadObservation。

Session envelope/payload canary 从 3 替换为 4，仅保留当前 reader；native payload revision 仍为 1，外部 headless JSONL 仍为 v1。旧开发 journal 在 repair/append 前失败关闭，不原地迁移。恢复与 uninterrupted 请求逐字节等价，不访问对象文件。

## 代价、验证与回滚

全额预留会降低剩余空间利用率；同步磁盘写入可能背压；每页读取先校验最多 16 MiB 完整对象，但仅保留所需范围。范围最多 64 KiB raw bytes，转义后最多 256 KiB。TUI 列表最多 50 条，最多缓存四页，用 generation 丢弃迟到回复。

验证见 `internal/session/artifact/*_test.go`、`internal/runtime/artifact_commit_test.go`、`internal/tool/builtin/artifact_owner_test.go`、`internal/app/artifact_resume_darwin_test.go`、两家 Provider 的 `artifact_stream_test.go`/`artifact_restore_test.go` 和 `internal/tui/transcript_browser_test.go`。

回滚必须使用匹配旧 canary 的独立开发数据；禁止旧程序读取新 journal。PTY、后台控制、全工具输出预算和自动过期均需后续独立 OpenSpec。

统一七工具记录、搜索保存及结果预算的后续决策见 [ADR-0006](0006-tool-transcript-and-result-budget.md)。本 ADR 中 4/1 canary 与 command-only 描述保留为当时决策背景。
