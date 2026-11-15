# 归档核对

归档后的 main 整合与交付检查见 [main-sync-2026-11-15.md](main-sync-2026-11-15.md)；本页的归档时校验及原始证据保持其历史基线。

归档日期：2026-11-15。变更：`add-runtime-effort-control`；schema：`spec-driven`。

## 完成与规格

- proposal、design、specs 和 tasks 均已完成；47/47 项任务完成，无未完成项或归档警告。
- 六份 delta 的 14 条 requirement 已与对应主规格当前正文逐段一致；归档不重复写入主规格。
- 使用 `openspec instructions archive` 获取的可选上下文没有附加要求。`.openspec.yaml`、全部方案、规格、任务、复审、回归和原始证据随目录一并保存。

## 验收依据

最近的完整回归见 [2026-11-15 回归与耗时排查](regression-2026-11-15.md) 和 [验收总表](verification.md)：完整未缓存 `make verify` 通过，普通全量 153.97 秒、race 781.18 秒，总计 937.83 秒；24 组真实 PTY 和 10 组脚本化真实 CLI 通过。

归档前重新核对 1,368 个源码/测试/构建及 fixture 输入，与该次成功质量门的 SHA-256 完全一致；404 份固定 fixture 未改变。归档只移动变更文档与证据并更新文档链接，不改变生产实现、测试断言、Provider wire 或 Session record vocabulary。输入和采样仍使用 `turn_inputs_committed`、`provider_native_commit`、`sample_usage` 等既有 record vocabulary，effort_snapshot 仅是进程控制观测，不新增 Session effort record。

## 归档收尾

README、Roadmap 和 pitfall 的活动 change 引用更新为归档地址。历史任务中“保留未归档”描述的是此前评审交付时的边界；此次收尾由用户新的归档请求触发。已执行日志、JSON、源码校验清单和验收脚本中的原始路径保留为执行时值，避免改写历史证据；复跑历史脚本时应重新指定输出目录。

原生执行范围为 Darwin/arm64 和本地模拟 Provider；不增加真实远端模型或 Linux/Windows 原生终端的验收承诺。归档步骤未执行 Git commit、push 或 PR 操作；后续交付以 main-sync 记录为准。归档后的严格规格、架构/文档检查及链接核对见 `evidence/archive/final-checks.json`。

## 归档后检查

- `openspec validate --specs --strict`：60/60 主规格通过。
- `openspec validate --all --strict`：61/61 通过；当前 change 已离开活动集合，因此较归档前少一项。
- `go test ./internal/architecture -count=1`：通过；`git diff --check` 通过。
- 原始 139 个文件全部保留，全部既有证据及 `.openspec.yaml` bytes 未改变；仅为归档状态更新三份说明文档，另新增本归档记录及核对证据。
- 归档前后主规格保持一致，47 项任务无未完成项。全部文档链接与活动集合退出结果见 [最终核对](evidence/archive/final-checks.json)。
