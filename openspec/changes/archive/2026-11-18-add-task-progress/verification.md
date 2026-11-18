# 任务进度清单验收记录

变更 `add-task-progress` 的 64 项实施任务已完成，13 份主规格已同步，已于 2026-11-18 完成归档。选择该能力是因为现有双 Provider 工具执行、Session 和宿主查询能形成完整的多步骤进度闭环：交互对齐 Claude Code，保留 TodoWrite 与 update_plan 各自原生输入和恢复语义。范围不包含 Plan Mode、子代理、后台任务或协作依赖。

## 合并与测试范围

2026-11-18 将 main 的文件引用与测试精简合入本地任务能力。遵循远端“轻、准、关键、快”原则：保留关键业务、失败关闭、提交取消、恢复和缓存断言；删除重复值矩阵、逐 getter 检查和重复跨模式 CLI，未恢复远端删除的测试。Makefile 保持远端统一 runner：20 分钟测试进程期限、不限制总预算、默认关键 race 正向清单，手动全量 race 入口保留。当前集合不宣称与旧穷举集合等覆盖。

OpenSpec 只保留方案、规格和简短验收。本次原始输出放在忽略的 `.test-results/task-progress-main-merge/`；历史原始证据移到 `.test-results/task-progress-pre-merge/`，不作为当前源码验证依据。仓库内实际被读取的 current fixture 与 request golden 继续作为不可变测试输入。

## 关键回归入口

- 领域、协议与工具：`internal/domain/task_progress{,_state}_test.go`、`internal/protocol/task_progress_test.go`、`internal/tool/task_test.go` 和 `internal/tool/builtin/task_test.go`，验证严格当前输入、完整替换/清除、来源和预算契约。
- Runtime：`internal/runtime/task_progress_test.go`、`task_boundary_test.go`、`task_context_test.go`，验证接收前取消复用同一预算票据、Sync 前旧投影、接收后确定提交、poison 与上下文内容身份。
- 原生与恢复：双 Provider 的 `task_progress_test.go`、`task_checkpoint_test.go`、`task_cache_test.go`；`internal/app/task_checkpoint_restore_test.go` 和 `task_progress_recovery_test.go`。保留四份固定 Session current bytes：双 Provider 各一份 result-tail 和四 checkpoint 链；ready/started 恢复直接截取固定记录前缀，不以当前 encoder 生成事实源。
- 组合能力：`internal/app/task_file_integration_test.go` 验证冻结文件与 Task 一同持久化、只读查询无 I/O、文件变化后恢复不重读，以及连续/恢复请求 canonical bytes、fingerprint 和原生顺序一致。双 Provider Task 续写另覆盖 Skill 与多个文件附件及 opaque reasoning。
- CLI 与交互：`internal/app/task_compaction_cli_test.go`、`task_transcript_test.go`、TUI 四份关键 snapshot，以及 `scripts/tasks-smoke.py` 和 `scripts/file-mentions-smoke.py` 的真实 Darwin PTY。`TestTasksAndFilesShareHeightAndModalFocus` 验证 Task 与文件补全共享空间、焦点及查询生命周期。
- Headless：`internal/headless/task_progress_test.go` 验证 typed 查询/快照、错误输入、EOF 排空、输出失败与 cleanup owner。

## 合并阶段验证

- `make verify TEST_OPTIONS='--output=.test-results/task-progress-main-merge/verify-complete'` 退出 0，81.080 秒；514 个普通入口、57 个关键 race 入口与选择清单完全一致，包含 gofmt、vet、Staticcheck、架构与分支规则、Python runner 自检。未命中测试结果缓存，验后源码 digest 一致。没有执行手动全量 race。
- `openspec validate --all --strict`：68 项通过，0 项失败。
- `python3 scripts/tasks-smoke.py --output-dir .test-results/task-progress-main-merge/tasks-pty`：双 Provider、彩色/无色共 4 个真实 PTY 场景通过，覆盖 Ctrl+T、详情、滚动/缩放、同名 Skill、问卷/批准焦点、取消及终端恢复。
- `python3 scripts/file-mentions-smoke.py`、`scripts/effort-smoke.py`、`scripts/completion-smoke.py`：分别 4、8、2 个真实 PTY 场景通过；与 Task 合计 18 个场景。
- Linux amd64 使用宿主编译的同版本 Staticcheck 分析全仓通过；`GOOS=linux GOARCH=amd64 go test ./... -run '^$' -exec=true` 测试编译通过。直接跨目标 `go tool staticcheck` 会尝试运行 Linux 工具，因此不能作为 Darwin 宿主的执行方式。

本轮门禁定位并修复两项测试装配遗漏：resume 测试改为处理全部有限 Init 命令，不再假定最后一项永远是 transcript；自动压缩 fixture 窗口由 20k 调为 22k，以容纳十工具目录下约 20.3k 的摘要准入预算，原压缩次数、失败窗口和恢复断言保留。删除测试精简后无消费者的 CLI helper，没有恢复远端删除的矩阵。

验证使用本地 HTTP/SSE 假服务及测试凭据，不访问真实模型 API。宿主为 Darwin arm64；本轮未运行 Linux/Windows 原生测试、Windows 全仓质量门或真实供应商端点，跨平台编译与静态检查不等于原生执行。

## 归档前回归（2026-11-18）

- `make verify TEST_OPTIONS='--output=.test-results/task-progress-archive/verify'` 退出 0，76.043 秒；514 个普通入口、57 个关键 race 入口完整执行，源码 digest 与前轮一致。
- 全部 13 个端到端脚本首轮通过，共 66 个场景：Task 4、文件 4、effort 8、补全 2、提问 4、Markdown 2、会话选择 4、推理 8、推理控制 8、插件 12、三类工具 Hook 6、参数改写 2、Stop 2。插件另完成 16 次离线恢复续写。所有场景使用本地假服务，真实 PTY 检查终端恢复、焦点、取消和干净退出。
- 64/64 任务、13 份 delta / 47 项需求已与主规格逐块核对一致；通过 CLI 归档到 `2026-11-18-add-task-progress`，保留 `.openspec.yaml`。架构、Roadmap、pitfall、当前测试路径及 record vocabulary 已核对，归档链接已更新。
- 归档后 `openspec validate --all --strict`：67 项通过，0 项失败。
- Linux amd64 测试编译证据对应最终源码，Linux 静态分析在前轮全仓通过的基础上补查最终 app 目标通过；未扩大原生平台验收范围。

本轮原始执行清单与日志仅保存在忽略的 `.test-results/task-progress-archive/`，不进入归档。提交与推送继续由既有 hooks 执行相同质量门，未绕过 hooks。
