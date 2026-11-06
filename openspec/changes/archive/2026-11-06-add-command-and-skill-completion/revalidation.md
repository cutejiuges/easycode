# 命令与 Skill 补全追加验证

## 复核结论与已修复问题

此前的质量门通过不足以覆盖以下输入组合。本轮先添加回归，观察失败，再修复；没有删除、跳过或弱化原有测试。修改仍在现有补全、字素编辑、空粘贴、可见焦点和草稿恢复契约内，不新增 Provider、Session 或公共接口。

| 问题 | 复现与影响 | 修复及回归 |
| --- | --- | --- |
| 组合空白后的光标 | `$rev` 后已有空格加组合重音、变体选择符或 ZWJ 时，补全只跳过首个 rune，光标进入字素中间，退格会拆开字符 | 复用 `composer.horizontal` 越过完整分隔字素；`TestCompletionSeparatorPreservesGraphemeCursor` 验证原文、光标和随后退格，普通空格、全角空格、Tab/LF 也覆盖。 |
| 空粘贴关闭候选 | 已解码 paste 正文为空时，原文未改变但候选被抑制；后续 Enter 语义意外改变 | 入口保留完整状态；`TestCompletionEmptyPasteIsNoOp` 验证草稿、候选、提交均不变，真实 bracketed paste 也重验。 |
| 接收拒绝后恢复旧窗口 | 冻结草稿时缩放为 28×10，接收被拒绝或取消查询后恢复旧 80×24 布局，正文超出当前终端 | 各恢复路径共用 `restoreComposer`，按当前尺寸恢复并失效解析缓存；`TestCompletionRejectedAdmissionKeepsCurrentWindow` 覆盖输入拒绝、查询拒绝、查询取消。 |
| 极窄但有高度的窗口 | 1×24 中仍有多行省略号候选，Tab 可以接受不可辨认的选择；之前的 1×1 用例只覆盖了高度不足 | 少于 13 列时隐藏并停用候选；`TestCompletionExtremelyNarrowWindowCannotAcceptBlindSelection` 覆盖 1/2/4/8/12×24 及恢复宽度后的选择保留。 |

恢复助手另由 `TestCompletionRestoredSnapshotInvalidatesEditingCache` 验证：两个草稿即使有相同编辑序号和光标，也不能复用另一草稿的候选。所有新增确定性回归在 `internal/tui/completion_revalidation_test.go`。

## 扩展验证

| 检查 | 结果/证据 |
| --- | --- |
| TUI 完整测试 | 新增四类修复后通过；最终结果同时纳入完整质量门。 |
| 重点回归重复五次 | 已通过，[输出](verification/revalidation-regressions.txt)，包括补全、取消、过期回复、取回和 Hooks 草稿。 |
| 状态机 fuzz，20 秒、两个 worker | 已通过，130177 组序列；[输出](verification/revalidation-fuzz.txt)。逐步检查 UTF-8、字素边界、布局正文、不可变候选快照、匹配/选择范围、焦点可见性、行宽和确定性渲染。 |
| 双 Provider 请求/恢复对照 | 八个组合均通过；扩展到普通/手动 Skill 和 ASCII/组合重音/中文/ZWJ emoji 参数，验证手输与已有参数中的名称补全、原文附件、原生顺序、canonical bytes、来源/缓存指纹及恢复等价；[输出](verification/revalidation-providers.txt)。 |
| Darwin 真实 PTY | 普通配色与 NO_COLOR 均通过；增加空 bracketed paste、8×24、组合空白后的补全/退格，保留 28×10/80×24、审批隔离、拒绝零文件写入及完整终端模式恢复；[输出](verification/revalidation-pty.json)。 |
| Provider/Session/Runtime/headless 无缓存回归 | `go test ./internal/provider/... ./internal/session/... ./internal/runtime ./internal/headless -count=1` 已通过；[输出](verification/revalidation-kernel.txt)。 |
| 最终 `make verify` | 全部通过，退出码 0；包含分支策略回归、gofmt、vet、Staticcheck、全量测试、架构守卫和全量 race；[输出](verification/revalidation-make-verify.txt)。 |
| OpenSpec 契约检查 | change 严格校验及全部 51 项主规格通过；[输出](verification/revalidation-openspec.txt)。 |

fuzz 是补充探索；四项缺陷的正确性由固定输入和确定事件序列的回归测试证明，不以随机运行或 sleep 代替取消/恢复断言。

## 范围与限制

验证环境沿用 Darwin arm64。本次没有 Linux/Windows 原生 runner，未验证其原生 PTY。两家模型调用均使用本地假 SSE 服务与固定假凭据，不访问真实模型；本记录证明本地协议与状态行为，不声称覆盖远端服务的所有变化。主规格保持现有七项补全要求，13 列下限是满足可辨认选择所需的本地布局预算。
