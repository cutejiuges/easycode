## Why

手动 `/compact` 与阈值自动压缩都会缩减模型可见的历史。用户需要在交接摘要发出之前指定应保留的内容，并能用受信任的本地检查阻止不适当的压缩。现有同步 Hook、双 Provider 压缩和恢复已具备可复用的执行与持久化边界，适合先交付 Roadmap P4/P6 交集中的 `PreCompact`。

## What Changes

- 为现有全窗口 `/compact` 和阈值自动摘要增加同步 `PreCompact` 命令 Hook，按 `manual`、`auto` 或 `*` 匹配，沿用冻结来源、逐声明默认拒绝信任和只读禁网执行。
- 压缩前运行检查。受信任命令可以显式停止压缩，或在声明 `feedback: model` 时提供有界摘要重点。用户的 `/compact` 重点在前，后续内容仅进入本次摘要请求，并计入完整请求预算。
- `/hooks` 展示触发范围与披露策略。审查分别说明手动重点如何交给本地脚本，以及哪些脚本输出可进入摘要请求。压缩按实际阶段显示准备、审查、检查、摘要和失败。非交互宿主沿用已有压缩事件与安全错误码，匹配声明要求信任。
- **BREAKING（当前开发期内部格式）**：`compaction_started` 加入必需的压缩前检查状态，匹配操作持久化独立的检查尝试和决定。更新受影响的当前固定 fixture。恢复沿原 lease 只收口未完成操作，已完成窗口继续使用原生 checkpoint。
- 本切片范围限于 `PreCompact`，`PostCompact` 在 checkpoint 安装之后有独立生命周期。

## Capabilities

### New Capabilities

- `extension/pre-compact-hooks`：压缩前检查的触发、输出、授权、披露、额度和耐久语义。

### Modified Capabilities

- `extension/command-hooks`：纳入第五种事件及共享配置、信任、执行与输出边界。
- `context/compaction`：区分无副作用基础预检、压缩前检查、最终请求预检和纯摘要生成。
- `context/auto-compaction`：明确检查失败回退、来源抑制与活动期间的输入边界。
- `session/jsonl-store`：压缩 started 当前结构、专属执行事实和严格回放。
- `session/resume`：在新输入及 Stop 反馈交付前收口有部分检查事实的压缩。
- `runtime/input-control`：以真实压缩身份关联信任、取消及清理。
- `tui/hooks`：压缩事件浏览、信任审查和可观察状态。
- `headless/stream-control`：以已有 `compaction.failed` 输出安全检查失败原因。

## Impact

影响 `internal/domain`、`protocol`、`extension/hook`、`runtime`、`session`、`app`、`tui`、`headless`，以及两家 Provider 摘要请求与恢复的回归验收。复用现有 runner、Writer、压缩 worker、Provider 请求编译器和终端审查。无需新的命令、远程接口或外部依赖。外部 stream-json 仍为 v1，新增明确的压缩失败码。
