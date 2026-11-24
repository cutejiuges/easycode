## Why

EasyCode 已支持调查、方案审阅、命名和恢复会话。用户试走另一条实施路径时，需要一条能保留这段调查及原生上下文、可独立续写的分支。P5 已具备空闲交接和会话选择器，P2 已具备独占 journal lease 和双 Provider 原生回放，适合用一个纵向切片把这些能力连起来。

## What Changes

- 交互聊天支持 `/branch [标题]` 及别名 `/fork`。从当前空闲线程的最新已提交历史创建分支，成功后切换，并显示返回源线程的 `/resume <thread-id>`。标题沿用本地命名校验，支持首词补全。
- 在同一逻辑 Session 下创建有明确父线程和切点的可恢复子线程。分支 journal 自包含，继承记录保留源身份、序号和 Provider 原生事实。创建、预览与恢复以各自的 journal 为依据。
- 同一工作区、Provider family、wire、model 内延续已提交的对话、工具历史、任务状态与有效原生窗口。继承的保存输出展示历史预览和实际生成线程。新线程以 Default 模式和当前启动配置开始，审批、信任、文件读取证据和连接续写状态按新实例初始化。
- 分支纳入会话列表、选择器、直接恢复及最近兼容线程选择。分支的活动标题与最近活动各自从本线程的 durable facts 投影。
- 首版创建入口为交互式 TUI，切点为当前线程的最新完整提交。历史任意切点、跨 Provider 或模型迁移、工作区或 Git worktree 分叉、Subagent 与 headless 创建各有独立契约。

## Capabilities

### New Capabilities

- `session/branch` 定义本地交互式创建、切点及血缘、durable 发布与交接、继承的原生内容和执行授权边界。

### Modified Capabilities

- `session/jsonl-store` 定义单一 journal 内允许的已验证分支边界、记录顺序及回放归属。
- `session/resume` 让根线程和可恢复分支共享精确定位、最近兼容选择、原生恢复和交接。
- `session/catalog` 投影当前线程的分支元数据、标题与时间，并独立校验可查询线程。
- `session/list-command` 在现有本地会话列表中识别分支条目。
- `session/preview` 对分支执行完整 lineage、回放与原生校验后生成只读预览。
- `session/titles` 允许分支的本地标题更新，并从其创建标题或最新改名投影当前值。
- `runtime/input-control` 将现有交接许可用于分支创建，定义源实例、候选和取消的接纳关系。
- `tui/basic-chat` 在恢复根线程或分支时初始化相应 transcript。
- `tui/input-completion` 提供两个命令名的本地补全及同名 Skill 处理。
- `tui/session-picker` 列出分支并按同工作区、兼容配置的规则接管。

## Impact

- `internal/session` 负责严格事实验证、快照读取和原子发布。`internal/app` 复用会话装配、原生恢复和终端交接。`internal/runtime` 拥有源会话准入，`internal/tui` 负责编辑、焦点、命令和反馈。
- 影响内部 journal 语义与 Catalog 当前快照结构。Provider 延续使用现有 Messages、Responses 及原生 checkpoint。外部 headless JSONL v1 继续使用现有操作。
- 验收覆盖原生续写等价、进程间独占、确定性取消与发布故障、终端分支往返。实施验收后同步规格、README、架构和能力清单。
