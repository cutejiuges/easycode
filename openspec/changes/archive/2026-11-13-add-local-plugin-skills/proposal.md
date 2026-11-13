## Why

P6 已有可复用的本地 Skill 发现、显式调用、按需读取和双 Provider 恢复链路，但团队还不能把一组技能作为具名插件直接复用；继续扩充 Hook 事件的边际收益低于补齐这个缺口。选择“本地 Plugin 技能包”能够交付接近 Claude Code 的 `--plugin-dir`、命名空间调用和插件浏览，同时吸收 Codex 的来源身份、不可变目录与贡献适配边界，不提前承担 P7 的线程调度与恢复复杂度。

## What Changes

- 新增可重复的 `--plugin-dir PATH`，覆盖普通启动、`--resume`、`--continue` 和 `resume` 选择器入口；只加载用户本次明确提供的本地目录，不安装、复制或自动扫描插件。
- 提供 EasyCode 原生 manifest，以及 Claude `.claude-plugin/plugin.json`、Codex `.codex-plugin/plugin.json` 和当前 Codex 参考实现支持的 Agent Plugins 根 `plugin.json` 的严格、明确列举的技能子集导入。外部格式只在读取边界适配为一套当前内部贡献契约。
- 支持标准 `skills/<name>/SKILL.md` 与各 importer 明确支持的技能根布局，分别保留 Claude 的目录引用名与 Codex 的 frontmatter 引用名；插件技能使用唯一 `plugin:skill` 名称，与现有本地 Skill 共用目录、准备、工具结果和预算。
- 导入 Codex/Agent Plugins 技能可选 `agents/openai.yaml` 中的 `policy.allow_implicit_invocation`，与现有禁止模型调用策略按更严格结果合并；副文件的存在性、内容与主文档共同绑定来源，未支持的依赖/产品限制明确拒绝，不静默忽略。
- 新增只读 `/plugins` 列表及详情，关联现有 `/skills` 和首词补全；选择技能只填入 composer，正式提交仍使用现有 admission/Sync 链路。
- 严格定义 manifest 选择、损坏阻断、重复插件/技能处理、启动冻结、来源改变拒绝、实际目录枚举/读取额度和故障隔离；为显式输入准备与模型读取提供统一来源关闭 owner，显式目录仅授权读取指令，不授予命令、文件写入或网络权限。
- 扩展当前 Skill 来源和限定名称的校验，保持现有附件/结果 envelope 字段及 record vocabulary；不新增版本、旧 reader、安装记录或恢复时的隐式插件加载。
- 交付两个参考工程的源码对照、固定示例、双 Provider golden/cache/恢复回归、确定性路径故障注入、CLI/headless/TUI/PTY 验收及完整 `make verify` 证据。

范围已由用户确认：本轮只交付本地技能贡献。marketplace、安装/更新/卸载、插件 Hook/脚本执行、MCP、Agent/commands importer、热加载和动态启停均不在本 change；manifest 显式声明这些未支持行为时报告错误，不伪装为完整插件兼容。原有 Hook 能力继续使用自己的来源与信任链路。

## Capabilities

### New Capabilities

- `extension/local-plugins`：显式本地插件目录、受限 manifest importer、强类型技能贡献、确定性身份/冲突/生命周期、安全边界和失败隔离。
- `tui/plugins`：启动快照的本地插件浏览、诊断、详情与技能选择。

### Modified Capabilities

- `extension/local-skills`：插件来源、限定引用、统一发现/读取/准备与同 turn 资源资格。
- `tui/input-completion`：新增 `/plugins` 候选及 `plugin:skill` 名称编辑和焦点规则。
- `provider/native-history-persistence`：插件 Skill 附件与工具结果的严格身份校验、离线恢复和请求等价性。

## Impact

主要涉及 `cmd/easycode` 的两个启动入口，`internal/domain` 的 Skill/插件展示值，`internal/extension`、`extension/plugin`、`extension/skill`、`extension/localfile` 的实际贡献与安全读取，`internal/app` 装配、`internal/runtime` 的启动前 Skill 快照安装及 `internal/tui`。替换已获批的 `extension`/`plugin` 占位为本 change 真正消费的技能贡献；不把 MCP、Subagent 或其他贡献提前接入。

Provider 只继续消费现有 Skill metadata/attachment/result，不导入插件 loader 或外部 manifest DTO；Session 继续保存当前事实，SQLite 不增加插件表。复用 Sonic、现有 YAML parser 和本地安全 opener，预计不增加生产依赖。实现时同步 README、能力清单、架构、Roadmap 与踩坑记录；当前提案不把计划能力写为已实现。
