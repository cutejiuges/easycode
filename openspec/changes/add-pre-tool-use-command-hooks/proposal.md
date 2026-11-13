## Why

本地 Skills 已完成发现、显式调用、工具读取和双 Provider 恢复，但用户还不能在修改文件或执行命令前自动运行仓库检查。当前有序工具调度、审批、Session Sync 与受控进程生命周期已经提供接缝，适合推进一个有真实消费者的 PreToolUse Hook 切片；直接建设 Plugins 则仍需同时解决贡献装配、命名空间和安装生命周期。

## What Changes

- 支持用户 `~/.config/easycode/hooks.json` 与启动工作区 `.easycode/hooks.json` 的严格、有界、确定性配置快照；仅提供同步 command 类型的 `PreToolUse`，作用于 `fs.patch`、`fs.write`、`process.exec`。
- 提供 Claude 风格 `/hooks` 来源/事件/匹配项浏览、命令详情和首次运行信任确认。信任绑定工作区、来源、配置快照及单条命令，仅在当前进程有效；headless 不隐式授权。
- Hook 使用 JSON stdin、最小环境和独立的工作区只读/禁网 sandbox。支持继续、退出码 2 阻断及结构化 deny；配置、执行、超时和解析失败均阻断当前调用。Hook 不批准工具，也不能绕过普通权限与 session grant 检查。
- 从现有 Exec 抽取真实共用的进程等待、排流、取消和清理 owner；保留 Exec 专属审批、输出预算、artifact 和进度策略，不以伪造 Exec 调用运行 Hook。
- **BREAKING**：扩展唯一当前 Session 词汇，加入 Hook 计划、执行开始/完成和门禁决定，更新 typed recovery plan 与固定 current fixtures。恢复只使用 durable 事实补偿，不读取配置、不重新请求信任、不运行旧 Hook 或工具。
- 提供双 Provider 原生配对与恢复/cache regression、信任交互 snapshot、真实进程 sandbox smoke、确定性取消/Sync/清理故障注入，并在实现验收时同步文档与 Hook 占位退出。

### Non-Goals

本次不实现其他 Hook 事件、只读工具 Hook、输入改写、additional context、Hook 自动批准、异步/后台 Hook、HTTP/prompt/agent Hook、热加载、持久信任、CLI 信任绕过、Claude/Codex 配置文件自动导入、Plugins/MCP 或跨平台 sandbox 扩张。首批执行平台沿用当前 Darwin 安全 adapter；其他平台失败关闭。本切片不等于 P6 完成。

## Capabilities

### New Capabilities

- `extension/command-hooks`：配置、匹配、信任、受限命令执行、输出子集、资源上限和错误分类。
- `tui/hooks`：只读配置浏览、完整信任审查、请求关联、取消和安全状态展示。

### Modified Capabilities

- `architecture/single-current-contract`：Hook 占位退出、强类型边界与被 Exec/Hook 实际共用的进程生命周期约束。
- `tool/invocation-loop`：在现有 exclusive 屏障中加入独立的 Hook 门禁，保持有序结果、幂等与取消边界。
- `tool/approval`：区分命令信任与工具批准；明确 Hook 阻断优先于 grant，以及无交互模式的拒绝优先级。
- `session/jsonl-store`：新增四类 required typed Hook facts、批次约束、校验、故障与恢复状态机。
- `session/resume`：按冻结计划恢复未完成 Hook 门禁，补偿原 call slot，保证零重跑与原生请求等价。

## Impact

主要涉及 `internal/extension/hook`、`internal/domain`、`internal/protocol`、`internal/runtime`、`internal/session`、`internal/app`、`internal/tui` 和现有 `internal/tool/builtin` 进程执行边界；新增职责明确的 `internal/executor/process` 共享生命周期包，并将 Skills 与 Hooks 实际共用的 no-follow 文件打开骨架收敛到 `internal/extension/localfile`。Provider wire、九项 Tool Catalog 与 headless 外部 JSONL 契约保持现有形状；Provider golden/cache tests 必须证明这一点。

不新增第三方依赖，不修改两个参考工程，不添加内部格式版本或兼容 reader。设计依据为 EasyCode `1a327e7`、Claude Code sourcemap `a8a678cb` 与 Codex `6f8bf683` 的本地只读快照，具体证据、取舍和验收映射见 design.md。

### 当前规格对齐说明（2026-11-13）

上述范围保留本变更最初交付 PreToolUse 的历史归属。PostToolUse、PostToolUseFailure 和 Stop 已由后续独立变更交付；本变更尚未归档，其 delta 现已完整对齐所覆盖需求的当前主规格，保留后续已交付场景、强类型 target 与恢复/输入安全点，避免将来同步或归档时覆盖现有能力。这是规格漂移修复，不将后续能力归为本变更新增，不改变业务代码、实施范围或完成任务数；实际核对结果见 verification.md。
