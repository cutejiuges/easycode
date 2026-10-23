## Why

EasyCode 已具备可靠的 Session、可重建 Catalog、`--continue` 和显式 `--resume`，但用户无法发现历史 thread ID，也无法从多个旧会话中定位恢复目标。当前最适合补齐独立的会话列表入口：复用已有事实源与安全边界，形成用户可见闭环，同时避开另一工作树正在实现的命令执行链路。

## What Changes

- 增加 `easycode sessions list [--all] [--limit N]`，默认列出当前规范化目录最近 20 个 root 会话，`--all` 跨目录，limit 范围为 1–200；数量上限只约束返回结果，不冒充索引扫描预算。
- 采用接近 Claude Code 的紧凑会话条目与明确恢复提示，展示完整 thread ID、更新时间、Provider/wire/model；跨目录时增加创建目录。吸收 Codex 的独立查询、范围标识、有界结果和显式失败提示；设计记录两参考工程的源码依据及取舍。
- 列表不依赖 Provider 配置或 API key，不启动 TUI、Runtime、模型请求或工具执行，不消费 stdin；当前目录内展示所有 Provider/model，避免当前配置隐藏旧会话。恢复继续使用既有 `easycode --resume <thread-id>` 和严格兼容校验。
- 扩展 Catalog 的强类型列表查询，固定按 committed `updated_at`、canonical thread ID 双降序，支持 `has_more`，保持现有 SQLite schema 和 JSONL record vocabulary。
- 复用现有安全 reconciliation，明确其可能重建索引并在 exclusive lease 下修复可恢复尾部；busy/invalid 导致的不完整刷新以 stderr 警告表达，不冒充完整、实时或已验证可恢复的列表。
- 覆盖无配置运行、空列表、跨目录/模型筛选、稳定排序、限制、损坏/占用/重建、取消、输出失败、终端控制字符和既有恢复入口回归。

## Capabilities

### New Capabilities

- `session/list-command`: 独立会话列表 CLI、范围与数量选项、安全文本展示、恢复指引及退出语义。

### Modified Capabilities

- `session/catalog`: 新增不依赖 Provider selector 的有界 root 会话列表查询与确定性 `has_more` 语义，沿用既有协调、lease 与隐私契约。

## Impact

- 实现预计新增 `internal/session/catalog/list.go`、`internal/app/session_list.go`、`cmd/easycode/sessions.go` 及对应测试；仅在 `cmd/easycode/main.go` 的 `run` 增加精确子命令路由，避免修改 app 的聊天装配、Runtime、Tool、Provider、TUI 和 Session record/codec。
- 更新架构、Roadmap、必要踩坑说明及 CLI 使用文档；不新增 Go 依赖，不改变 Provider request、native history、tool catalog 或 cache fingerprint。
- `17ad2/easycode` 当前 change 为 `add-command-execution`；入口同文件不同位置与共享文档可能产生文本冲突，apply 前必须重新检查其差异，保持子进程启动分支独立。
- 本次不包含交互式 picker、无参数 `--resume`、标题/摘要/全文搜索、worktree 聚合、自动切目录、clipboard、JSON 输出、分页 cursor 或实时索引。后续能力须另立 change，不能以本切片替代完整 Claude Code 恢复体验。
