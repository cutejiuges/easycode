## Why

EasyCode 已具备严格的 append-only JSONL Session 事实源和显式 `--resume`，但仍无法发现既有会话或按当前目录继续最近一次兼容会话，且 P2 要求的可重建 `state.sqlite` 尚未落地。现在引入最小 Session Catalog，可以在不触碰 Provider-native 恢复语义的前提下交付 `--continue`，并为后续 session picker 提供可信查询基础。

## What Changes

- 新增从现有 JSONL journal 与 ReplayPlan 派生的 SQLite Session Catalog；JSONL 继续是唯一恢复事实源，删除或损坏索引后可重建。
- 新增安全、确定性的 journal 枚举与 Catalog reconciliation，复用现有 exclusive lease、Loader、ReplayPlanner 和 descriptor-bound 路径边界，不通过轻量字符串扫描推断 Session 语义。
- 新增 `--continue`，在当前规范化 `creation_cwd` 内选择 Provider family、wire、model 完全兼容且最近更新的 root Session，然后复用现有显式 resume 路径。
- `--continue` 支持交互、`--print` 和 `--json`，与 `--resume` 互斥；headless 继续只输出本次新 turn，JSON `thread.started.resumed` 保持为 `true`。
- 定义 busy、损坏、缺失、索引陈旧和 SQLite 重建失败的安全行为，确保 Catalog 不绕过恢复时的重新校验，也不改变已经持久化的 journal 事实。
- 同步架构、Roadmap 与踩坑文档，并增加 SQLite 重建、跨进程 lease、路径安全、选择稳定性和双 Provider 恢复回归测试。
- 本 change 不实现 TUI session picker、标题/tag/全文搜索、Git worktree 聚合、archive、parent-child graph、实时后台索引器或跨 Provider/model fork。

## Capabilities

### New Capabilities

- `session/catalog`: 定义 SQLite Session Catalog 的最小投影、确定性查询、安全 reconciliation、删除后重建和故障隔离契约。

### Modified Capabilities

- `session/resume`: 增加显式 `--continue` 的兼容会话选择、互斥参数、busy/空结果行为，以及选择后复用既有连续 lease 恢复路径的要求。
- `headless/text-output`: 允许 `--continue` 与 `--print`/`--json` 组合，并明确输出仅包含本次 turn、恢复标志和失败报告语义。

## Impact

- 主要影响 `internal/session` 的安全枚举边界、新的 `internal/session/catalog`、`internal/app` 装配与 `cmd/easycode` 参数解析；Runtime、Provider wire、native history codec 和 TUI 模型无需新增依赖。
- 默认数据布局增加 `~/.easycode/state.sqlite`；测试装配需要可独立注入 Session 根与 Catalog 路径，所有数据库生命周期由实例持有并显式打开/关闭。
- 引入 Go SQLite driver；最终选择必须支持无 CGO 构建目标和项目要求的 no-follow、实际句柄类型/权限校验，不能以 `Lstat` 后按路径打开替代安全保证。
- SQLite schema 和 `--continue` CLI 行为成为新的外部兼容面，需要版本化 schema、稳定英文错误和回归测试。
