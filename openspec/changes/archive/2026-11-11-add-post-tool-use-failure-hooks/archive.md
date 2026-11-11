# PostToolUseFailure 最终契约核对与归档（2026-11-11）

归档对象为 `add-post-tool-use-failure-hooks`，schema 为 `spec-driven`。本次用户已明确授权最终检查、同步 specs 并归档；其他 change 不在范围内。以下记录最终状态，前期 proposal/design 的规划基线及各轮报告中的未归档/Windows 编译失败均属于历史快照。

## 契约、主规格与实现

最终逐项比对 6 项 capability、12 个 requirement、111 个场景，主规格与 delta 全文一致。保留 36 个未受影响 requirement 及 63 个受影响 requirement 的既有场景；新 capability 的 Purpose 完整，主规格没有 delta 操作标题或 TBD。主规格此前已同步，本次再次执行语义同步核对无剩余差异，不重写无关内容。

| 边界 | 核对结论与实际实现/测试入口（均为仓库相对路径） |
| --- | --- |
| 配置与信任 | 三事件、默认 none、8 KiB 命令、来源合计及每分支 8 项；两分支在候选前审查最多 16 项，跨事件不共享信任。`internal/extension/hook/config.go`、`failure_test.go`、`internal/runtime/failure_hooks_test.go`。 |
| 执行事实与选择 | `internal/tool/post_selection.go` 只读同调用 durable 准入和原结果；正常非零 exit 为 success，已准入 error/conflict 为 failure，其他为 none。当前取消、预算和恢复不改选择；`internal/runtime/exclusive.go` 保留准入并在 poisoned 时拒绝重入。 |
| 唯一计划与通知 | `internal/domain/post_hooks.go` 的根字段为 invocation_id/capability/branches/allowance/reservation/digest，branch 为 event/hooks/delivery，terminal 为 route；`internal/protocol/post_hook_commit.go` 恰为 invocation_id/plan_digest/detail_sequence/phase。无版本、兼容 reader 或伪 pending 事件。 |
| 输入、输出与隐私 | `internal/domain/hook_input.go` 失败输入为公共八字段加安全 error/is_interrupt=false；`internal/extension/hook/post_output.go` 严格匹配事件，默认不披露，exit 2 为检查错误。未授权 stdout/stderr、preview、cause/artifact 不进入反馈。 |
| 预算与进程 | `internal/tool/post_feedback.go`、`internal/domain/post_hooks.go`、`internal/runtime/post_hooks.go` 使用一次 reserve/base/feedback/close，各分支独立提示槽；只读/禁网、30 秒共享期限及唯一 cleanup owner 保持。未选 model 分支仍可能缩短原预览，终态不回涨。 |
| Session 与恢复 | 实际 vocabulary 为 tool_post_hook_planned、hook_execution_started/finished、tool_post_hook_completed；`internal/session/post_hooks.go` 严格交叉验证 route/调用/预算/前缀，`internal/runtime/post_hook_recovery.go` 只用冻结事实补偿，不执行、不授权、不读旧配置/artifact、不访问 Provider。 |
| Provider 与 UI | 两家 `failure_output_test.go`、`post_output_test.go` 及 OpenAI `post_websocket_test.go` 保持原错误/配对/opaque/cache；`internal/app/post_hook_query_test.go`、`internal/tui/post_hook_commit_test.go` 验证中性计划、原 Error/Conflict 与独立诊断、旧序列和分支隔离。 |

六项主规格为 extension/command-hooks、extension/post-tool-use-failure、session/jsonl-store、session/resume、tool/result-budget、tui/hooks。同步与任务核对详见[机器记录](evidence/archive-2026-11-11/pre-archive-audit.json)。40/40 任务已完成，4 项 planning artifacts 均为 done。

## 文档修正

本轮只修正文档，不修改生产代码、测试或契约：Roadmap 明确三事件各自时机；Tool 架构分别描述成功元数据与失败安全摘要，并把“不改变原 bytes”限定为无匹配或所有分支均为 none，明确另一分支 model 的预留代价；总体架构明确一至两个非空分支和已实现失败事件。design 中提案阶段文字标为历史基线，旧报告保留原始失败与当时未归档状态，并链接本记录。归档迁移后更新总体架构、Tool 架构、能力清单、Roadmap、pitfall 的引用，核对报告、测试/fixture 与证据路径。

## 验证与限制

本轮在修正文案后执行：

- `go test -race -count=1 ./internal/domain ./internal/extension/hook ./internal/tool ./internal/runtime ./internal/session ./internal/protocol ./internal/provider/openai ./internal/provider/anthropic ./internal/tui -run 'Post|Failure|Hook' -timeout 300s`：exit=0，9 包全部通过，[原始日志](evidence/archive-2026-11-11/contracts.log)。
- `go test -count=1 ./internal/architecture`：exit=0，[原始日志](evidence/archive-2026-11-11/architecture.log)。
- `openspec validate add-post-tool-use-failure-hooks --strict`：exit=0，[原始日志](evidence/archive-2026-11-11/change-validation.log)。
- `openspec validate --specs --strict`：55 passed、0 failed，[原始日志](evidence/archive-2026-11-11/spec-validation.log)。既有长 requirement 信息提示不影响结果。
- `git diff --check`、主规格同步幂等检查、当前 fixture/测试入口、证据 SHA-256 与文档链接核对：通过；归档后再次校验链接与全部主规格。归档后架构测试 exit=0（[日志](evidence/archive-2026-11-11/architecture-post-archive.log)），主规格仍为 55 passed、0 failed（[日志](evidence/archive-2026-11-11/spec-validation-post-archive.log)）；活动列表仅剩原有 PreToolUse change（[记录](evidence/archive-2026-11-11/active-changes.json)）。

已核对当前 AGENTS/Makefile/go.mod/go.sum/internal/cmd/scripts 的源码指纹为 `2279345e75a1e5fc0d8f96683eefa4ee1a0cb50602b7de493e19f8311de14d1a`，与[上一轮全量质量门](windows-helper-regression-2026-11-11.md)完全一致，19 份证据校验和有效。因此沿用刚完成的不缓存 make verify、72 例双 Provider E2E race 和 Linux/Windows amd64/arm64 全库构建/测试编译结果；本轮文档修改后另跑架构测试与契约 race，不把历史结果伪称为再次全量执行。

Linux/Windows 没有原生 runner，交叉编译不等于运行验收。真实命令/Hook 安全 adapter 仍仅支持 Darwin，其他平台继续失败关闭。未使用真实云端服务或凭据；不宣称 P6 整体完成，不包含命令语义分类、自动修复、其他 Hook、Plugins/MCP 或 Subagent。前两轮原始 PTY、fuzz、故障注入及恢复证据继续随本 change 保存，见[首次验收](verification.md)与[独立回归](regression-2026-11-11.md)。

## 归档状态

已通过 `openspec archive add-post-tool-use-failure-hooks --yes --skip-specs --json` 归档至 `openspec/changes/archive/2026-11-11-add-post-tool-use-failure-hooks/`，CLI exit=0；[原始归档回执](evidence/archive-2026-11-11/archive-cli.json)。主规格已由前置语义同步核对确认一致，`--skip-specs` 仅避免 CLI 重复写入，未跳过归档校验；回执 specsUpdated=false 表示这次移动没有再次改写 specs，不表示主规格未同步。没有执行 Git commit 或 PR 操作，也未归档 add-pre-tool-use-command-hooks。

最终链接、固定 fixture 字段、源码与证据校验见[收口记录](evidence/archive-2026-11-11/post-archive-audit.json)。
