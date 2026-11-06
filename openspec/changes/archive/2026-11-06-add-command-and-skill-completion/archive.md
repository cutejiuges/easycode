# 归档核对记录

- 归档日期：2026-11-06；变更：`add-command-and-skill-completion`；流程：`spec-driven`。
- proposal、specs、design、tasks 均完成，24/24 项任务完成，无未完成项。
- `tui/input-completion` 的七项新增要求与 Purpose、`tui/basic-chat` 的四项修改已同步主规格；归档前后逐项比较要求正文和场景，内容完全一致，basic-chat 的其余八项要求保留。
- 主规格已同步，因此执行 `openspec archive add-command-and-skill-completion --skip-specs --yes --json`，仅避免重复合并，不跳过主规格同步核对或验证；CLI 成功退出，元数据及全部验收证据随目录保留。
- README、总体架构、Roadmap P5、能力清单和踩坑记录已核对实际范围；架构和 Roadmap 的变更链接已改为归档地址。文件/参数值补全、正文 mentions、动态目录、reasoning 和 fork picker 等后续能力未标记为已交付。
- 测试文件及验收日志路径存在；未新增或改变 Provider、RuntimeCommand/Event、Session/native/checkpoint record vocabulary，补全状态仅驻留 TUI 内存。
- 归档后 `openspec list --json` 已无本变更，其他活动变更未改动；`openspec validate --specs` 为 51 passed、0 failed，见 [校验记录](verification/archive-specs.txt)。
- 归档后 `make verify` 全部通过，退出码 0，包含架构守卫、全量测试和 race；本次仅修改归档及文档，Go 测试复用了上一轮成功缓存，见 [质量门记录](verification/archive-make-verify.txt)。此前无缓存内核回归、约 13 万组 fuzz、双 Provider 及真实 PTY 证据见 [追加验证](revalidation.md)。
- 归档后的相对 Markdown 链接、旧活动路径引用和 `git diff --check` 均已核对。原生终端验证范围仍为 Darwin arm64，未新增其他平台验证承诺。
