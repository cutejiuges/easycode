## Why

当前 P6 已有四类同步 Hook、本地 Skills/Plugins 和成熟的工具审批/恢复链路，但 PreToolUse 只能放行或拒绝，项目脚本无法在执行前规范化命令、写入内容或修改目标；这也是 Roadmap 中 block/rewrite 验收尚未闭合的一项。优先补齐参数改写，可以直接复用已有安全与生命周期边界，形成双 Provider 都能使用的完整切片。

## What Changes

- 为现有三项 mutation 能力增加严格的 `hookSpecificOutput.updatedInput` 完整替换子集：Edit/apply_patch、Write、Bash/exec_command；保留各来源 facade 的匹配模式与可表达范围，不增加模型工具或 Hook 事件。
- 按既有 user/project、声明数组顺序串行改写，后续检查读取前一步已 durable 的检查候选参数；每次替换都重新执行 typed input 校验，不合并半个对象，不按完成时序竞争。
- 区分模型原始调用、检查期间的候选参数与 gate 全部通过后选中的有效参数；只有选中值可以进入普通候选、权限、Write 完整 Read 资格、sandbox 和工具执行。中途拒绝仅保留候选审计，不能把它冒充实际执行输入。
- 对改写后的执行准入结果附完整有效参数说明，对尚未准入的失败/拒绝/取消及恢复补偿使用明确状态的短说明；均纳入原调用冻结结果及现有预算。完整说明无法容纳时在副作用前拒绝。
- 分别约束原始双流、规范参数、完整 finished、模型说明及宿主投影的编码字节；规范替换对象最多 16 KiB，防止 JSON 转义使合法原始输出突破持久化上限。
- **BREAKING**：调整唯一当前 PreToolUse finished 的强类型执行证据，记录无变化或完整参数替换及前后内容摘要；直接替换受影响的 current fixtures，旧开发 shape 失败关闭，不增加版本、迁移或兼容 reader。
- 复用 Claude Code 的 `updatedInput` 与实际参数审批体验，采用 Codex 的每能力输入适配及身份保持边界，并保留 EasyCode 的确定性顺序、失败关闭、durable 决定与零重跑恢复。
- 在现有 Hook 审查、工具批准和 Ctrl+O 中区分检查候选、已选参数与执行准入；以先安装快照再通知的方式关联持久化 sequence，阻止过期详情覆盖。补齐双 Provider、恢复、缓存重入、取消交界、故障注入和真实 Darwin PTY 验收。

范围不包含 allow/ask、PreToolUse additionalContext、交互式工具代答、异步 Hook、Plugin Hook、MCP、热加载、审批中编辑或平台执行能力扩张。

## Capabilities

### New Capabilities

- `extension/pre-tool-use-input-rewrite`：三能力完整参数替换、稳定串行组合、封闭输入与原始/有效调用身份。

### Modified Capabilities

- `extension/command-hooks`：将严格拒绝输出子集扩展为有界参数改写子集，明确新增的信任用途及事件隔离。
- `tool/approval`：候选、批准与执行绑定最终有效参数，保持权限、Read 证据和 sandbox 约束。
- `session/jsonl-store`：保存与回放 Pre 参数变化的必要执行事实，严格校验当前格式和中断补偿。
- `provider/native-history-persistence`：恢复使用已验证的有效执行证据校验结果，同时保留原生调用参数、配对与请求等价性。
- `tui/hooks`：审查参数改写用途，展示实际批准/执行内容与有界来源详情，保持草稿和过期响应隔离。

## Impact

主要涉及 `internal/domain`、`internal/extension/hook`、`internal/tool`、`internal/tool/builtin/hook_process.go`、`internal/runtime`、`internal/session`、Provider 恢复证据以及 app/protocol/TUI 的既有投影。复用现有进程 owner、信任 owner、独占调度和详情查询，不引入通用 Hook manager、新依赖或 Provider wire 互转。

实施时同步 README、能力清单、总体架构、Roadmap 与 pitfall；仅在实施和验收完成后同步主规格。当前阶段只交付本提案、设计、delta specs 与任务清单，随后由明确的 apply 请求启动实现。
