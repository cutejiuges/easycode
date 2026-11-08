# PostToolUse 归档核对

日期：2026-11-08。变更：`add-post-tool-use-command-hooks`。工作流：`spec-driven`。已归档目录：`openspec/changes/archive/2026-11-08-add-post-tool-use-command-hooks/`。

## 范围和完成状态

本轮按用户明确归档请求执行。proposal、design、specs、tasks 均完成，56/56 任务已勾选，无未完成项或跳过警告。另一活动变更 `add-pre-tool-use-command-hooks` 保持活动状态，任务与实现不变；仅对其共享规格区块做下述保真更新。

本轮仅同步规格、协调活动增量、修正文档并归档，不改动产品代码、脚本、依赖、fixture 或 golden。772 份 Go/依赖/终端脚本文件的归档前后内容快照一致。

## 主规格同步核对

使用 `openspec status --json` 返回的 10 条 specs 路径逐份读取并与主规格比对；归档指令无附加 context/guidance。同步前成功获取一次当前 specs 指令，无额外 rules，整个同步复用该快照。

共新增 10 条需求、修改 9 条需求，无删除或重命名。每条增量的正文和场景与同步后的主规格逐块一致；被修改需求的 88 个原有场景保留，未出现在增量中的主规格需求保持原文。同步后主规格不含 ADDED/MODIFIED 等操作标题；新能力的 Purpose 使用已审核增量正文，没有 TBD 占位。

| 主规格 | 处理 |
| --- | --- |
| `extension/command-hooks` | 修改 4 条：双事件配置、独立信任、共享受限生命周期、Pre/Post 输出语义隔离 |
| `extension/post-tool-use` | 新增能力和 4 条需求：成功后执行、元数据输入、显式披露、完整冻结反馈 |
| `provider/native-history-persistence` | 修改 1 条：typed 输出组合、独立恢复证据、原生配对与缓存等价 |
| `session/jsonl-store` | 修改 1 条、新增 1 条：独立后置状态机、当前 facts、预算与内容证据 |
| `session/resume` | 修改 1 条：仅追加补偿、保留成功和前缀、零重跑 |
| `tool/approval` | 新增 1 条：后置信任先于候选、批准与副作用 |
| `tool/invocation-loop` | 修改 1 条：Post terminal 前保持 exclusive 屏障 |
| `tool/result-budget` | 新增 1 条：分段预算、反馈预留与调用单次退休 |
| `tui/hooks` | 修改 1 条、新增 1 条：事件与披露审查、独立检查状态 |
| `tui/tool-transcript` | 新增 2 条：有界修订详情、原生提交刷新与恢复的受控摘要 |

另独立修正三处过时 Purpose：command-hooks 由仅 Pre 改为当前 Pre/Post 共同契约；result-budget 由七工具改为九工具及显式反馈；jsonl-store 将已交付 Tool/Hook/Compaction 与未来 MCP/Subagent 区分。这些修改只校正概述，不改变需求行为。

## 活动 Pre 增量的依赖协调

首次主规格同步后的全量检查发现，旧 Pre 增量的 session/resume、tool/invocation-loop 两条 MODIFIED 需求缺少刚交付的 Post 场景，全量校验失败；若继续同步旧区块，还会回退两事件的配置、信任、执行和展示正文。因此撤回第一次目录移动，在完成依赖协调和全部校验后重新归档，没有绕过校验或减弱主规格。

仅对 Pre change 的五份 delta 文件、八个重叠需求区块保真更新到当前主规格，并新增 rebase.md 说明来源。保留全部原有 Pre 场景及其余需求，不修改 Pre proposal/design/tasks、43/43 状态或产品代码，不归档该 change。这一依赖调整使之后归档 Pre 时不会覆盖当前 Post 契约。

## 架构、Roadmap、测试与事实名称

- 核对 README、总体架构 12.1/12.1.1、Tool 架构、capability-status、Roadmap P6 与 pitfall。均保持“Darwin 实际执行、默认 none、显式 model、真实 result 先 Sync、共享预算、恢复不重跑”的边界。
- P6 整体仍未退出；其余 Hook 事件、PostToolUseFailure、async、参数/结果改写、Plugins/MCP、Skills fork/权限扩张与热加载继续明确为未实现。没有将 Linux 交叉构建写成原生执行验收。
- 282 处源码、测试和脚本引用展开检查通过；13 份固定 `post_hook_*.jsonl` 和双 Provider 共 6 份 output golden 均存在。具体业务断言与任务映射见 [verification.md](verification.md)。
- 当前新增 Session kind 为 `tool_post_hook_planned`、`tool_post_hook_completed`；复用 `hook_execution_started/finished` 的 event/plan/ordinal 强类型身份。进程内 `post_hook_committed` 只在 Sync 和快照安装后发布，headless 明确校验后忽略；未增加外部 headless 事件或内部格式版本。
- 架构/Roadmap/能力清单中的活动 change 链接已更新为归档路径，过时的“尚未同步、后续归档”状态同步修正；不改写其他历史变更的验收结论。

## 最终检查

| 检查 | 结果 |
| --- | --- |
| 10 份增量与主规格的完整同步复核 | 通过，19 条需求一致，88 个既有场景保留 |
| 源码/测试/脚本路径及固定 fixture/golden 存在性 | 通过 |
| `openspec validate --specs --strict` | 退出码 0，52 个主规格通过 |
| `openspec validate add-post-tool-use-command-hooks --strict` | 退出码 0 |
| 归档阶段 `make verify` | 退出码 0，格式/vet/Staticcheck/架构/全量测试/race 全部通过 |
| 归档后 `openspec validate --all --strict` 与本地 Markdown 链接检查 | 退出码 0；52 个主规格与 1 个活动 change，共 53 项通过；85 个本地链接通过 |
| 代码快照、归档目录与依赖变更的保真核对 | 通过；772 份代码/依赖/脚本内容不变，元数据保留，Pre 仅八个共享区块及说明更新 |

本轮不重复运行 PTY 和交叉编译，因为产品代码未变；实施阶段的双 Provider、Pre/Post PTY、真实 Darwin 沙箱、OpenAI WS、恢复/缓存与 Linux 交叉编译结果仍由 [verification.md](verification.md) 记录。完整质量门在本轮重新执行，不以旧记录替代。

归档前协调后的全量校验为 54 项通过、0 失败；移动后为 53 项通过、0 失败，差异来自本 change 退出活动列表。只有过长 requirement/已存在 ADDED 需求的信息提示，无失败或未完成警告。质量门日志为 `/tmp/easycode-post-hook-archive-final-verify.log`，最终规格校验日志为 `/tmp/easycode-post-hook-afterarchive-validate.log`；这些是辅助记录，复验依赖仓库命令和固定测试。
