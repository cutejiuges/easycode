## Why

EasyCode 已具备会话发现、双 Provider 原生恢复和完整 Coding Loop，但用户仍需复制 thread ID 才能恢复旧会话，同模型、多次运行的记录也难以辨认。现在可复用既有安全存储与历史投影，交付接近 Claude Code 的键盘选择和按需预览，同时保留 Codex 风格的明确异步身份与资源所有权。

## What Changes

- 新增交互入口 `easycode resume [--all] [--config PATH]`，支持目录范围切换、最近 200 条元数据搜索、键盘浏览、完整身份详情、配置不匹配提示及跨目录确认。
- 纳入用户确认的按需只读文本预览：显式加载所选 thread 的已提交用户与 assistant 文本，最多返回 32 KiB 正文，不修复 journal、不创建 writer、不补偿旧 turn、不执行模型或工具。
- 为双 Provider 增加离线原生历史校验与语义投影入口，与正式恢复共享严格 decoder、checkpoint/source 校验和投影逻辑，不依赖连接配置或伪造凭据。
- 使用单一 Bubble Tea Program 与独立 picker/chat 模型，宿主串行拥有存储操作，处理过期响应、取消、恢复结果接收和最终清理。
- 正式选择复用既有连续 lease 恢复路径；区分写入前拒绝、已发生合法 repair/compensation 后失败以及结果不确定，禁止静默创建新会话或自动重试。
- 同步相关帮助、架构、Roadmap 和验收说明；修正列表帮助中仍声称扫描会 repair 的过时说明。

## Capabilities

### New Capabilities

- `tui/session-picker`: 启动选择器入口、有限搜索、键盘路由、预览展示、查询身份和单终端生命周期。
- `session/preview`: 在短暂 exclusive lease 下只读加载、严格验证和有界文本预览，保持 journal bytes 不变。

### Modified Capabilities

- `provider/history-projection`: 增加不构造联网 Provider 的离线投影契约，共享 native 恢复验证并保持请求与缓存不变量。
- `session/resume`: 增加交互选择后的显式恢复、跨目录确认、取消接收边界和失败分类；现有 `--resume`、`--continue` 与 headless 语义不变。

## Impact

- 涉及 `cmd/easycode`、`internal/app`、`internal/tui`、`internal/provider` 及两家 Provider 实现；复用 `internal/session`、Catalog 与现有测试 fixture。
- 增加有真实消费者的强类型跨包查询/投影接口；不增加 RuntimeCommand/Event、Session record、SQLite 字段、格式版本或迁移 reader。
- 无新增外部服务或第三方依赖；参考工程只读，不参与构建。
- 新增 TUI snapshot、宿主故障注入、真实子进程 lease 与双 Provider golden/cache/restore regression；实现后必须执行 `make verify` 和真实终端 smoke test。
- 本次不包含会话内 `/resume`、全历史搜索/分页、自动标题、正文索引、完整工具预览、重命名/删除、worktree 聚合、跨 Provider fork 或模型自动切换。
