## Why

本地 Skills、显式调用与补全已经形成闭环，PreToolUse/PostToolUse 也已交付，但工具执行尝试确定失败后尚无受控的项目诊断反馈入口。补齐同步 `PostToolUseFailure` 能复用现有信任、进程 owner、结果预算和恢复边界，为模型提供项目特定的失败处理建议，适合当前 P6 阶段；它不包含命令语义解释或测试失败自动修复。

四方向比较及两个参考工程的逐项证据见 design.md。交互沿用 Claude Code 的事件名称、工具关联检查和 additionalContext；内核采用明确的类型、调用身份和独立结果，保留 EasyCode 的双 Provider 原生历史与 Codex 风格的生命周期边界。

## What Changes

- 在现有两来源冻结 hooks.json 中增加 `PostToolUseFailure`，复用字面 matcher、默认拒绝的单项信任、只读/禁网沙箱和同步执行。
- 仅在三种 mutation 的执行尝试准入与确定 error/conflict result 均 Sync 后选择失败事件。started 不证明进程已启动或文件已改变；准入后的启动失败也可形成确定失败。准备失败、权限或 Hook 拒绝、原结果取消或不确定均不选择失败事件；正常非零退出的 Exec（包括 go test 返回 1）继续走 PostToolUse。
- 后置选择统一为 success/failure/none，只由同一调用已持久化的准入与结果事实决定。当前 context、预算及恢复模式不改变选择，只决定检查能否启动及其终态；原错误保存后再取消，仍保留 failure 选择并以 cancelled 收口。
- 在候选准备和普通批准前冻结并审查可能执行的两个后置分支。计划只在调用级保存 invocation/capability，分支仅保存事件、引用和 delivery；共享一个预算 ticket 和一次调用收口，实际至多运行一个分支。
- 默认不披露输出；显式 `feedback=model` 且获独立信任时，仅接收与失败事件一致的严格 additionalContext。原错误不被覆盖、不自动重试、不提升为系统指令。
- **BREAKING**：直接替换唯一当前后置计划/终态结构及相关固定 current fixtures，支持两个互斥分支与严格恢复校验；不增加版本字段、历史 reader 或迁移器。
- 补充完整审查边界回归：合法的 8 KiB 原始命令经过最坏 JSON 转义后仍须完整进入三事件的精确信任审查；信任请求保留有界编码预算，不因内部 16 KiB 假设误取消回合，也不放宽原始命令、字段或授权校验。
- `/hooks` 和 Ctrl+O 展示失败检查、来源与披露用途；计划通知只标识调用、计划、持久化序列与阶段，不预填结果未知的事件。沿用 headless 信任拒绝与现有外部输出协议，不新增模型工具。

## Capabilities

### New Capabilities

- `extension/post-tool-use-failure`：执行尝试确定失败后的强类型 stdin、事实驱动事件选择、独立启动门禁、同步检查、明确披露和结果隔离。

### Modified Capabilities

- `extension/command-hooks`：三事件严格配置、事件相关信任和共享执行预算。
- `tool/result-budget`：成功/失败两个后置分支只预留一次，按实际分支计费并关闭。
- `session/jsonl-store`：调用归属只保存一次的当前分支计划、success/failure/none 选择证据、终态、预算与事件关联回放。
- `session/resume`：不改变事实选择的各崩溃切点离线收口、冻结反馈恢复与零重跑。
- `tui/hooks`：第三事件浏览、执行前审查、结果未知时的中性计划投影及原工具/诊断状态分离。

## Impact

主要影响 `internal/domain`、`extension/hook`、`runtime`、`session`、`tool`、`protocol`、`app` 和 `tui`。两家 Provider 只扩展既有 typed ModelOutput 的合法反馈事件，不改变 wire、native item 或工具 schema；须补 canonical request/native order/fingerprint 恢复等价、缓存 golden、故障注入、真实进程和 TUI 验收。

不引入依赖，不修改两个只读参考工程。实施后同步 README、能力状态、架构、Roadmap 和相关踩坑说明，再执行 `make verify` 与专项验证。当前提案不表示代码已实现或 P6 整体完成；实现必须由后续明确 apply 请求触发。
