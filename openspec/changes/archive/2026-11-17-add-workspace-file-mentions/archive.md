# 工作区文件引用归档记录

日期：2026-11-17。变更：`add-workspace-file-mentions`。Schema：`spec-driven`。

用户已确认直接归档，目录为 `openspec/changes/archive/2026-11-17-add-workspace-file-mentions/`。四份规划产物均为 done，47/47 项任务完成，活动 changes 列表中已移除本变更。

## 规格与内容核对

六份 delta 的 19 项 ADDED、4 项 MODIFIED 和 110 个场景已完整同步到主规格。归档前再次逐 Requirement 核对全部 23 项描述和场景，与主规格一致；44 项未涉及的既有 Requirement 及原有其他场景保留。本次用户选择直接归档，没有重写主规格。

- `context/file-mentions`：新增 5 项。
- `context/planning`：新增 1 项，修改 1 项。
- `provider/history-projection`：新增 2 项。
- `provider/native-history-persistence`：新增 3 项。
- `runtime/conversational-input`：新增 3 项。
- `tui/input-completion`：新增 5 项，修改 3 项。

原目录 39 份文件全部随目录移动，包括原始 27 份验证证据与 `.openspec.yaml`。后者的 schema 和 created 日期保持原始 bytes；六份 delta、全部原验证证据、proposal 和 47 项任务字节保持。design、verification、review 仅更新归档阶段说明。移前 SHA-256 与主规格摘要见 [archive-manifest.json](evidence/archive-manifest.json)。

README、总体架构、capability-status、product-roadmap 的四处验收引用已指向归档目录，Roadmap 状态已更新。能力范围、测试路径和实际 record vocabulary 已核对：用户输入仍为 turn_inputs_committed 与 provider_native_commit，native kind 为 user_inputs/sample/tool_outputs/hook_feedback，没有把 MCP、Subagent 等后续能力写成已实现。

## 验证

- 归档前本 change 严格校验通过；全部 changes/specs 为 64/64。
- 归档后全部严格校验为 63/63，含 62 项主规格及 1 项剩余活动 change；减少的一项是本次归档变更，见 [archive-validation.json](evidence/archive-validation.json)。
- 活动变更列表已排除本变更，见 [archive-active-changes.json](evidence/archive-active-changes.json)。
- 最近一次最终代码的完整 `make verify` 退出码 0，全量普通与 race 通过；14/14 PTY、4/4 回执单测通过，详见 [review.md](review.md) 与 [verification.md](verification.md)。本次仅移动归档目录并更新文档，沿用该次代码验收，没有重复执行 Go 测试。

归档没有执行 commit、push 或创建 PR。
