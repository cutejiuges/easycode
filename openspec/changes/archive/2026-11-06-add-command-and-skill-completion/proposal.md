## Why

EasyCode 已交付双 Provider、九项工具、多行 composer、本地 Skills、Hooks 与会话恢复，但现有五项本地命令及 Skill 入口仍需用户记忆和完整输入。当前最合适的下一块能力是 P5 的命令与 Skill 输入补全：复用已工作的输入和扩展链路，让现有能力更容易发现和调用，并以明确的焦点规则补齐 Claude 风格交互。

## What Changes

- 为首个非空白 token 的 `/` 命令及 `$` Skill 名称提供本地候选，展示名称、说明、参数提示和相对来源。
- 使用冻结 Skill 展示快照，仅提供可显式调用的有效获胜条目；内置 slash 命令优先，同名 Skill 可通过 `$name` 发现。
- 确定性匹配采用名称完全匹配、前缀、子序列及描述子串分级；同级使用稳定名称与来源排序，不记录使用频率。
- Tab 只填入；Enter 对内置命令交回现有提交路径，对 Skill 只填入待编辑调用。保留已有参数、空白和其他正文。
- 定义候选导航、Esc 关闭与禁止立即重开、普通 Ctrl+C、modal 焦点、显式粘贴、窄屏和 NO_COLOR 行为。
- **BREAKING**：将 `tui/basic-chat` 中手动 Tab 恒为无操作的开发期契约改为有可见候选时接受补全；未激活补全时保持无操作。补全占据焦点时上下键选择候选，Esc 先关闭候选，再沿用现有取回/停止语义。
- 增加纯逻辑、固定 snapshot、输入路由、真实终端解码及双 Provider 请求/恢复/缓存回归；实现阶段同步使用说明和能力状态。

## Capabilities

### New Capabilities

- `tui/input-completion`：候选来源、确定性搜索、首词编辑范围、接受与执行、焦点、展示预算，以及无副作用和请求等价边界。

### Modified Capabilities

- `tui/basic-chat`：补全激活时的 Tab/Enter/上下键与 Esc 优先级；其余编辑、粘贴、召回和取消规则完整保留。

## Impact

- 实现重点在 `internal/tui`：独立 completion 值与纯函数，接入 `model.go`、`composer.go`、`composer_input.go`、`skills.go` 和 `transcript_viewport.go`；共享既有提交分支，不建立第二套命令执行器。
- `internal/app` 已通过 `assembleChatModel` 同时为普通启动及恢复装配冻结 Skills，无需新增文件扫描或公共服务接口。集成测试验证此装配和原生请求等价。
- 不改变 Provider wire/capability、模型工具目录、RuntimeCommand/Event、Session/native/checkpoint、权限、沙箱或 Skill 加载契约；不新增依赖、配置、后台 worker、外部请求或持久化记录。
- 只读参考 Claude Code `2.1.88` 的候选呈现与 Tab/Enter/Esc 行为，以及 Codex `6f8bf683cee83ed4d580a2e106b587d8451db936` 的局部状态、范围替换和焦点隔离。逐项依据、差异和选择理由见 `design.md`。
- 本切片不包含 `@file`、正文中间 Skill 自动激活、`/help`、新业务命令、动态目录、MRU、模型建议、Plugins/MCP、reasoning UI 或后台任务；这些能力各有额外契约与生命周期，需要独立变更。
