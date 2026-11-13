# 本地 Plugin 技能包归档核对

日期：2026-11-13（Asia/Shanghai）。变更：`add-local-plugin-skills`，schema：`spec-driven`。按用户明确的“核对一致后完成归档”授权执行；归档前五项规格均已同步，采用 Archive now 路径，没有重复改写主规格。

## 完成状态与规格

proposal、specs、design、tasks 四项 artifact 均为 done；45/45 项任务完成，无未完成警告。完整保留 `.openspec.yaml`、方案、delta、验收及原始失败/成功证据。

| 主规格 | 本 change 核对需求 | 场景 | 状态 |
| --- | --- | --- | --- |
| [extension/local-plugins](../../../specs/extension/local-plugins/spec.md) | 10 | 44 | 当前正文与 delta 一致 |
| [extension/local-skills](../../../specs/extension/local-skills/spec.md) | 8 | 25 | 当前正文与 delta 一致 |
| [provider/native-history-persistence](../../../specs/provider/native-history-persistence/spec.md) | 1 | 9 | 当前正文与 delta 一致 |
| [tui/input-completion](../../../specs/tui/input-completion/spec.md) | 3 | 15 | 当前正文与 delta 一致 |
| [tui/plugins](../../../specs/tui/plugins/spec.md) | 4 | 14 | 当前正文与 delta 一致 |

合计 26 项需求、107 个场景。逐项比较需求正文和场景，确认无待同步差异，其他需求与既有 Purpose 保持原样；无正在执行的规格同步。归档前本 change 严格校验通过，见 [校验结果](evidence/archive-change-validation.json)。

## 实现、文档和 record vocabulary

- [README](../../../../README.md) 与[能力清单](../../../../docs/roadmap/capability-status.md)：显式 plugin-dir、四格式优先级/受限字段、命名差别、副文件限制、只读浏览与冻结来源均对应真实实现。
- [总体架构](../../../../docs/architecture/overall-architecture.md)：Contribution/SourceHandle 与唯一关闭 owner、恢复后安装未来 Skill 快照、FD 存储边界和严格来源校验均有实际消费者。清除旧 PluginManager 名称，修正“全部 Plugins 待实现”和笼统的插件信任要求；静态技能包不产生 Hook 信任或工具执行授权。
- [Roadmap](../../../../docs/roadmap/product-roadmap.md)：记录本地静态切片已交付并归档，补齐当前 /plugins 入口；P6 其余事件、可执行贡献/MCP、marketplace、安装/更新、热加载仍待实现，未宣称 P6/P7 退出。
- [Pitfall](../../../../docs/roadmap/pitfall-log.md)：命名差异、调用策略、失败读取计费、冲突/硬链接、关闭所有权、参数隐私、草稿和缓存分离均映射现有测试文件。
- 当前固定 plugin_input_openai/anthropic.jsonl 仅使用现有 session_meta、thread_meta、turn_inputs_committed、turn_started、provider_native_commit；插件 Skill/资源 payload 继续使用既有 tool_call_result 结构，执行链仍使用 tool_call_ready/tool_execution_started/tool_call_result，不增加插件 record。
- 没有新增模型工具、RuntimeEvent、headless frame、SQLite 表、安装记录、内部版本或兼容 reader。Scope=plugin 与四个 importer kind 为唯一当前 Skill 来源校验的一部分；原生 thinking/signature/encrypted content 及双 Provider call/output pairing 仍由原策略持有。

## 验证依据

归档前逐项核对 [最终源文件快照](evidence/regression-2026-11-13-source.json) 的 1,285 个文件，源码、测试、脚本、固定 fixture、AGENTS 和主规格指纹均与最后独立验收一致。归档仅调整文档和目录位置，没有改动上述受验代码或固定数据。

- [实现验收](verification.md)与[独立全面回归](regression-2026-11-13.md)均保留真实命令、平台与限制。
- [最终质量门](evidence/regression-2026-11-13-make-verify.log)通过：含 gofmt、vet、Staticcheck、架构检查、全量测试与 race；app 普通测试 123.581 秒、race 573.642 秒。
- 能力乱序 107 个叶级执行、生命周期/安全 10 轮共 210 个叶级执行，均无 failure/skip；两项 fuzz 共 2,761,940 次引擎执行通过。
- [真实二进制与 PTY](evidence/regression-2026-11-13-plugins-smoke.json)：四格式 × 双 Provider 共 8 组 CLI、40 次请求、16 次删除来源后的跨进程恢复；双 Provider × 普通颜色/NO_COLOR 共 4 组真实终端，全部通过。
- 首轮 race 套件十分钟总超时及 Write 组单独复测均完整保留；最终质量门没有放宽超时或删减测试。Linux 仅完成构建/测试编译，真实模型服务、Linux/Windows 原生运行未执行。

## 归档后检查

归档位置：`openspec/changes/archive/2026-11-13-add-local-plugin-skills/`。README、架构、Roadmap、能力清单及 pitfall 已改用归档链接；历史验证日志保留原内容，不重写证据中的旧执行路径。

归档后的 **make verify 已通过（exit 0）**，见 [归档质量门](evidence/archive-make-verify.log)；58 项主规格严格校验全部通过，见 [主规格校验](evidence/archive-specs-validation.json)。5 项 delta 再次逐项匹配当前主规格，148 处相对文档链接有效，活动列表已无本 change；其他活动 change 保持不变。源码/fixture/主规格 1,285 项指纹复核一致，分支与 git diff --check 通过。结构化汇总见 [archive-checks.json](evidence/archive-checks.json)。没有执行 Git commit 或 PR 操作。


## 提交前展示格式检查

暂存后的完整 git diff --cached --check 首次包含新增 golden，发现两份插件详情快照在空描述后仍有分隔空格。提交前仅在描述非空时追加分隔符与描述，保留原有导航、选择、权限和终端行为；对应两份固定快照去除该多余字符，原有快照全字节比较保持。此前源文件快照记录归档时实际状态，提交后的当前快照见 [publish-source.json](evidence/publish-source.json)，差异仅为 internal/tui/plugins.go 与 plugins_detail/plugins_detail_narrow.golden。针对插件浏览的定向快照测试通过；重新构建后的真实 CLI/PTY 验收再次通过（8 组 CLI、40 次请求、16 次恢复、4 组终端），见 [publish-smoke.json](evidence/publish-smoke.json)。最终发布前由 pre-commit/pre-push 执行完整 make verify，结果记录于提交和 PR。
