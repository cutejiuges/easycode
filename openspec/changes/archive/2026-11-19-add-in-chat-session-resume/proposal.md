## Why

用户目前要退出聊天才能打开会话选择器或按 ID 恢复，来回查看两段历史会打断工作。P2 已有有界会话发现、只读预览、跨进程 journal lease 和双 Provider 原生恢复，P5 已有本地 slash 命令和首词补全，适合把这些能力接成一个聊天内的会话切换流程。

## What Changes

- 在交互聊天中提供 `/resume` 和 `/resume <thread-id>`。无参数时复用启动选择器的搜索、刷新和只读预览，排除当前会话。完整 ID 可直接定位目标，包括列表范围以外的会话。
- 当前会话完全空闲且输入均已处理时才允许切换。目标必须是同一规范化工作目录、与冻结 Provider family、wire、model 匹配的 root thread。其他目录的会话可浏览和预览，界面提示从其创建目录使用 CLI 恢复。
- 在同一终端中准备目标控制实例和首屏历史。接管前取消或目标失败时，确认候选资源已清理且源仍可用，再返回原聊天。接管获准后，确认旧 writer 已关闭并释放租约，再显示目标历史并接收新输入。
- 在同一终端生命周期复用冻结配置、项目指令、Hook 声明、Skill 与插件来源。每次接管独立恢复目标历史和运行状态，临时授权及推理强度覆盖从新会话实例开始。
- 本切片范围限于交互式 TUI 和宿主接管。标题查找、跨目录活动切换、Provider 切换、会话 fork 和 headless 切换留在各自能力范围。

## Capabilities

### New Capabilities

复用现有会话选择器与恢复能力。

### Modified Capabilities

- `tui/session-picker` 增加聊天内入口、直接定位和取消后返回当前会话的交互。
- `session/resume` 定义聊天内恢复的权威目录校验、目标恢复边界与资源接管。
- `tui/input-completion` 增加交互式 `/resume` 首词候选及同名 Skill 路由。
- `runtime/input-control` 增加交互宿主的空闲交接许可，覆盖接纳前准备、已排队输入和实际 worker 结束。
- `runtime/effort-control` 定义同一进程内恢复到新控制实例时选择与观测序号的初始化。

## Impact

`internal/app` 统一交互会话装配与生命周期，`internal/tui` 组合聊天和选择器，`internal/runtime` 提供仅供交互宿主使用的强类型交接控制。复用当前 journal、Catalog、原生恢复、Transcript 查询和终端测试入口。验证覆盖双 Provider 请求等价、来源冻结、资源与租约清理、异步消息隔离及真实 PTY。
