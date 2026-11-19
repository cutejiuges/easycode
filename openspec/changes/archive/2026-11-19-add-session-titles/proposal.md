## Why

EasyCode 已支持持久化多轮会话、离线列表和启动恢复选择器，但历史会话仍主要靠模型、目录、时间和 UUID 辨认。手动标题能直接改善多任务切换，且可复用已验收的 Session writer、Catalog 和选择器，先在 P2/P5 完成一条独立的用户路径。

## What Changes

- 在交互聊天中提供 `/rename <名称>`。空闲时，裸 `/rename` 打开预填现名的单行编辑框。新会话在第一轮对话前即可命名，保存由会话 owner 确认无活动操作或等待输入后接纳。
- 在当前 root journal 追加一条独立的标题事实。确认持久化后更新当前聊天的标题，由恢复路径读取最后一条有效标题。相同标题直接确认，避免重复记录。
- `sessions list` 展示已命名会话的标题。启动 `resume` 选择器优先展示标题，并将其纳入现有最多 200 条元数据的搜索。只读预览使用实际 journal 中校验出的标题。完整 thread ID 仍是恢复身份。
- 本次范围是手动修改当前互动会话。模型生成标题、清空标题、离线重命名、按名称直接恢复、正文索引与实时索引另行设计。

## Capabilities

### New Capabilities

- `session/titles` 定义标题值、交互命令、写入与恢复边界。

### Modified Capabilities

- `session/catalog` 增加显式标题投影与结构重建规则，继续按最后提交记录的时间排序。
- `session/list-command` 展示有名条目的标题，并保留完整 ID 和恢复提示。
- `session/preview` 返回已验证的独立标题字段。
- `tui/session-picker` 支持标题展示和搜索，保留既有选择、范围与恢复规则。
- `tui/input-completion` 增加 `/rename` 内置候选及其 Skill 同名优先级。

## Impact

影响 `domain`/内部 `protocol`、Runtime 的会话控制分支、Session record 与 replay、Catalog 结构、app 装配及 TUI。初始 metadata 和现有 record 的编码继续适用；新标题事实在同一 envelope 中追加。派生 SQLite 缺少标题列时按当前结构重建。Provider wire、模型工具目录、headless 外部 JSONL v1 和第三方依赖沿用现有契约。
