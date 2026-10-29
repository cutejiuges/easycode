# 命令输出 artifact 验收记录

验收日期：2026-10-29。本文记录本 change 的实现证据，不替代需求、设计或后续归档。

## 场景与证据

下表将九份 delta spec 的新增场景及受影响的既有契约对应到仓库测试；表内路径相对仓库根目录。原有调用、审批、Session lease、Provider SSE 与 headless 行为仍由完整质量门回归。

| Delta spec | 场景及实现边界 | 测试证据 |
| --- | --- | --- |
| `session/output-artifacts` | 实际目录身份、祖先关系、系统读根、路径别名及执行前替换复核；真实 sandbox 隔离 | `internal/session/artifact/layout_unix_test.go`、`internal/app/artifact_boundary_test.go`、`internal/tool/builtin/artifact_isolation_darwin_test.go` |
| `session/output-artifacts` | 连续前缀、独立双流、16 MiB 上限、小输出清理、短写/ENOSPC/Sync 故障、EOF 与保存完整性分离 | `internal/session/artifact/store_unix_test.go`、`cleanup_unix_test.go`（同目录）、`internal/app/artifact_exec_darwin_test.go`、`artifact_resume_darwin_test.go`（同目录） |
| `session/output-artifacts` | 跨进程原子预留、竞争与崩溃释放、实存保留、不重复计费、损坏 reservation 拒绝 | `internal/session/artifact/quota_process_test.go`、`quota_validation_test.go`（同目录） |
| `session/output-artifacts` | no-replace 发布、FD 上的权限/类型/链接检查、同长度篡改与对象缺失 | `internal/session/artifact/store_unix_test.go`、`cleanup_unix_test.go`、`quota_validation_test.go`（同目录） |
| `session/output-artifacts` | 未提交对象不授权，result Sync 后授权；持 lease 且 replay 验证后清理，保留其他 thread 和任意年龄的已引用文件 | `internal/runtime/artifact_commit_test.go`、`internal/session/artifact/cleanup_unix_test.go`、`internal/app/artifact_resume_darwin_test.go` |
| `session/output-artifacts` | 阻塞 write/seal 时取消、timeout、shutdown 不丢失 owner；完成前子进程不能取得 thread lease | `internal/tool/builtin/artifact_owner_test.go`、`internal/app/artifact_shutdown_darwin_test.go` |
| `tool/read-artifact` | strict 参数、默认值、非法/重复/null 字段、当前 thread 授权；错误无路径/正文，无 workspace ReadObservation | `internal/tool/artifact_test.go`、`internal/tool/builtin/read_artifact_test.go`、`internal/app/artifact_exec_darwin_test.go`、`internal/runtime/artifact_recovery_test.go` |
| `tool/read-artifact` | UTF-8 跨页、极小 limit、无效字节、转义预算、空 EOF、保存前缀 EOF 与 sourceComplete 分离 | `internal/tool/artifact_page_test.go`、`internal/session/artifact/store_unix_test.go`、`internal/app/artifact_resume_darwin_test.go` |
| `tool/command-execution` | 保存策略进入审批 fingerprint；小/大输出四种保存状态，提示与正文共享 256 KiB；退出码与保存失败分离 | `internal/tool/artifact_approval_test.go`、`artifact_preview_test.go`（同目录）及固定 golden、`internal/app/artifact_exec_darwin_test.go`、`internal/tool/builtin/artifact_owner_test.go` |
| `tool/invocation-loop` | 双 Provider 的七工具目录与真实执行器；严格参数、分片 SSE、错误终止、native output pairing | 两家 `internal/provider/{openai,anthropic}/artifact_stream_test.go`、`artifact_restore_test.go`、`artifact_catalog_test.go` 及 request/continuation golden；既有 stream 测试回归取消、UTF-8 和断线 |
| `tool/invocation-loop` | ReadArtifact 最多 8 路 parallel_read、与 Read/Patch/Write/Exec 的有序屏障、取消及至多一次；结果提交失败不继续执行 | `internal/runtime/artifact_scheduler_test.go`、`artifact_commit_test.go`、`exec_fault_test.go`（同目录） |
| `session/jsonl-store` | 唯一 canary 4、typed 联合、字段归属/长度/状态校验、旧 shape 拒绝；固定 current bytes，经生产 loader/replay 读取 | `internal/session/artifact_fixture_test.go`、`artifact_history_test.go`、`current_fixture_test.go`、`exec_fixture_test.go`（同目录）及 `internal/session/testdata/current/` |
| `session/jsonl-store` | result 前文件/目录 Sync，result 后 native output Sync；不同边界故障失败关闭，无重复副作用 | `internal/session/artifact/cleanup_unix_test.go`、`internal/runtime/artifact_commit_test.go`、`exec_fault_test.go`、`artifact_recovery_test.go`（后两者同 runtime 目录） |
| `session/command-history` | 全量有效 facts 投影，多 turn 与成功/拒绝/取消/不确定结果；最多 50 项稳定游标、当前 thread 查询、取消和可等待关闭 | `internal/session/command_history_test.go`、`exec_replay_test.go`（同目录）、`internal/app/command_queries_test.go` |
| `session/resume` | 已有 result 原样补齐 output，中断 ReadArtifact 不重读；未知或未来 descriptor 不能授权历史成功页 | `internal/runtime/artifact_recovery_test.go`、`internal/session/artifact_history_test.go`、`artifact_fixture_test.go`（同目录） |
| `session/resume` | 双 Provider uninterrupted/restored canonical bytes、native 顺序与 fingerprint 等价；reasoning/signature/encrypted content 保留；对象删除/篡改不改变冻结历史 | 两家 `internal/provider/{openai,anthropic}/artifact_restore_test.go`、`internal/app/artifact_resume_darwin_test.go`；固定 `artifact_*_tail.jsonl` 覆盖 result 后/output 前断点 |
| `tui/basic-chat` | Ctrl+O 列表、Enter 详情、双流分页、滚动、Esc 返回、审批优先级、Ctrl+C 清理等待；主线紧凑、控制字符安全、固定尺寸展示 | `internal/tui/command_browser_test.go`、`command_detail_test.go`（同目录）及 `internal/tui/testdata/command_detail_*.golden`、`internal/app/exec_interactive_darwin_test.go` |
| `tui/basic-chat` | generation 丢弃迟到回复、换 session 取消、独立流游标、最多四页缓存、View/resize 零 I/O；查看恢复历史不调用 Provider 或写 journal | `internal/tui/command_browser_test.go`、`internal/app/command_queries_test.go`、`artifact_resume_darwin_test.go`（同目录） |
| `context/planning` | 稳定七工具 schema 与估算，运行状态/目录/对象/查看动作不污染 stable prefix；双 Provider request fingerprint 回归 | `internal/context/planning_test.go`、`internal/context/testdata/exec_catalog_*.golden`、双 Provider `artifact_restore_test.go`、`internal/app/artifact_resume_darwin_test.go` |

## 文档与契约核对

- 总体架构、Tool 专题、Roadmap、pitfall 和 ADR-0005 已同步；命令输出切片完成不等于 P3 或全工具结果预算完成。
- Session 当前 envelope/payload canary 为 4；native payload revision 为 1，外部 headless JSONL 仍为 v1；不保留旧开发 shape reader。
- `tool_call_ready` / `tool_execution_started` / `tool_call_result` 保留执行事实，`provider_native_commit` 保存原生续写依据；没有新增进度或宿主查询 journal record。
- builtin 通过消费端接口使用存储，protocol 仅包含宿主查询值；构造器不启动 I/O，文件读取在明确生命周期或异步查询边界执行。
- 默认配额：每流 16 MiB、每调用预留 32 MiB、thread 256 MiB、root 1 GiB。完整性校验读取最多一个对象，内存只保留当前有界范围；已引用对象不按年龄回收。

## 执行记录与平台范围

- `openspec validate add-command-output-artifacts --strict`：通过。
- `GOOS=linux GOARCH=amd64 go build ./...`：通过。
- `GOOS=windows GOARCH=amd64 go build ./...`：通过。
- 最终 `make verify`：通过，包含 gofmt、go vet、Staticcheck、架构边界检查、全量测试与 race test。

本机为 Darwin；真实 sandbox、进程退出、跨进程 lease 与阻塞清理测试在本机运行。Linux/Windows 只验证交叉编译，未执行这些平台的运行测试。Exec 仍只支持 Darwin；不具备安全存储实现的平台在启动时失败关闭，交叉编译通过不代表跨平台执行能力完成。测试使用本地 fixture/SSE 服务，无真实 API 或凭据。

2026-10-29：用户确认同步后归档。九份 delta specs 已逐份合并并验证一致；`openspec validate --specs --strict` 的 32 份主规范全部通过。归档目录为 `openspec/changes/archive/2026-10-29-add-command-output-artifacts/`，设计与 ADR 的相对链接已同步。
