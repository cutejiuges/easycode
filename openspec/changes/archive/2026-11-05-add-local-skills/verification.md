# 本地 Skills 验收记录

验证日期：2026-11-04。执行环境：macOS（darwin/arm64），Go 1.24.13。

## 验收结果

- `make verify` 通过：分支规则测试、gofmt、go vet、Staticcheck、全量测试（含架构边界）及全量 race 测试。
- `openspec validate add-local-skills --strict` 通过。
- `openspec validate --specs --strict`：45 项通过、0 项失败；长需求文本提示不影响校验结果。
- `git diff --check` 通过。
- `GOOS=linux GOARCH=amd64 go test -c ./internal/extension/skill -o /tmp/easycode-skill-linux.test` 通过。此项仅证明交叉编译，不代表 Linux 运行时验收。

## 关键证据

| 契约 | 测试文件与断言 |
|---|---|
| 来源、解析与隔离 | `internal/extension/skill/local_test.go`：严格 frontmatter、参数字面替换、优先级、损坏项阻断、目录上限、来源变更、模型/用户调用标志、同来源资源权限；`secure_unix_test.go`：确定性 symlink 替换、特殊文件拒绝、项目根失败不影响用户根 |
| 当前 turn 权限 | `internal/runtime/skills_test.go`：排队附件不授权、Sync 失败不授权、成功持久化后授权、turn 边界清除；`local_test.go`：匹配来源/revision 的资源例外，不允许借资源入口加载主文档 |
| 强类型与完整预算 | `internal/domain/skills_test.go`、`internal/tool/skill_test.go`：严格字段/互斥参数、字节上限、值隔离、结果身份匹配、额度不足移除附件，不产生部分成功正文 |
| 混合调度 | `internal/runtime/result_budget_test.go`：Read/Skill 八 worker 正序与倒序完成的结果配对、预算和正文一致，下一 sample 不重置 turn 额度；`artifact_scheduler_test.go`：Skill 与其他只读工具共享批次，遵守 Patch/Write/Exec 独占屏障 |
| 原生恢复与缓存 | 两家 Provider 的 `skill_test.go` 和固定 `testdata/skill_input.golden.json`：prepared finalize/discard、附件顺序、严格恢复、canonical request bytes/fingerprint 等价；`internal/context/skills_test.go`：目录变化与稳定来源隔离 |
| Session 冻结结果 | `internal/session/skill_test.go` 与固定 `testdata/current/skill_result.json`：当前结构严格解码及配对；无版本路由或迁移 reader |
| 三种宿主 | `internal/app/skills_test.go`：两家 Provider × once/interactive/stream 六种组合；显式原文与附件、模型加载、源文件删除后恢复；ready/started/result 崩溃缝隙补偿不请求 Provider、不重读 Skill；准备失败保持 journal 不变 |
| TUI | `internal/tui/skills_test.go` 与三个固定 snapshot：正常/窄屏/空目录，含损坏与遮蔽项；Enter 仅填 composer，Esc 保留草稿，准备取消和过期响应，metadata 控制字符转义 |

现有通用队列、工具 ledger、审批、取消、自动压缩、Session lease/损坏恢复、Provider 原生 reasoning 和 incremental 请求回归均由全量质量门继续覆盖。新增测试使用临时用户根及本地 mock Provider，不使用真实 API key 或访问外网；app 的 TestMain 隔离 HOME 并保留命令子进程入口。

完整验证曾发现 `TestStreamRunnerBrokenOutputStopsWithoutAppendingError` 的既有测试调度假设：固定数据可能在 transport 关闭前全部被读取。已改为持续发送可忽略的空白行直到关闭，保留写入错误及输出次数断言，避免超长非法输入触发另一条关闭路径；最终版本在 race 模式重复 100 次通过。未修改生产关闭逻辑。

## 文档与范围核对

已同步七份相关主规格、README、总体架构、工具架构、Roadmap 和 pitfall。仅删除 skill 的临时占位及 allowlist 项；hooks/plugins/MCP 等仍保持原限制。当前 native kind 仍为 `user_inputs`、`sample`、`tool_outputs`，Skill 复用现有工具 ready/started/result 事实，没有旁路激活日志或持久授权记录。

本次未实现 hooks 执行、plugin 加载、fork、热加载或第三方 Skill 全格式兼容；未知行为字段明确禁用。目录变更需重启，已冻结历史仍可恢复。双 Provider 验收为本地协议级 mock 与 golden，未执行付费线上模型调用。change 保留供后续归档。
