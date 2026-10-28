## Why

EasyCode 已具备双 Provider 的读、搜、改、写、前台命令及审批/恢复闭环，但聊天输入仍只有末尾追加、末尾删除和 Enter 提交，难以编辑代码片段、日志与长需求。当前最有价值的交互切片是以 Claude Code 的多行编辑和历史导航为产品基准，吸收 Codex 的 Unicode、粘贴及焦点隔离设计，让既有执行能力更容易日常使用。

## What Changes

- 用独立的纯内存 composer 替代单行 rune 草稿，提供字素安全的光标移动、前后删除、多行编辑和受限高度的视口。
- Enter 提交，Alt+Enter、Ctrl+J 与反斜杠加 Enter 换行；根据当前 Bubble Tea 的真实解码能力说明 Shift+Enter 限制。
- 显式处理 bracketed paste、CRLF/CR 换行、控制字符的安全显示，以及 4 MiB 草稿上限；不把粘贴正文中的换行或控制字符当作操作键。
- 按 Claude 风格在视觉行边界进入当前会话历史，保留浏览前草稿/光标，限制历史条数与总字节，不增加磁盘历史文件。
- **BREAKING**：TUI 与双 Provider 不再 TrimSpace 改写正文，headless 模型输入也保留外围空白，只用空白判断拒绝空输入；Delete 改为删除光标后字素。保持协议 shape 不变。
- 提交等待期间冻结草稿，接收成功后记录用户消息和输入历史；接收失败恢复完整草稿，已接收后的 turn 失败不自动重提。
- 保持审批、命令详情、活动 turn 对输入的优先路由；不增加排队或 same-turn steering。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `provider/openai-responses-text`：所有宿主的合法输入原样进入原生用户 item。
- `provider/anthropic-messages-text`：所有宿主的合法输入原样进入原生用户 message。
- `tui/basic-chat`：多行编辑、换行/提交、粘贴边界、会话输入历史、布局和提交失败恢复。

## Impact

- 主要影响 `internal/tui/model.go`，新增 composer 编辑、布局、历史与对应测试文件；复用 `ChatSession.Submit` 和现有 typed RuntimeEvent。
- 使用仓库已锁定的 `github.com/rivo/uniseg v0.4.7` 进行字素与宽度计算；直接使用时提升为直接依赖，不升级 Bubble Tea、不新增终端解析器。
- 双 Provider 集成回归覆盖多行正文、恢复请求等价与稳定前缀；双 Provider 用户消息构造移除 TrimSpace，headless 模型输入开始保留外围空白；不改变 wire shape、RuntimeCommand/Event、Tool schema、Session record vocabulary、headless 输出格式或协议版本。
- 实现阶段同步 README、总体架构、Roadmap 和 pitfall；修正 Roadmap 中 Write 进度的矛盾，但不宣称 P2/P3/P5 整体完成。

## Non-goals

不包含 Markdown/transcript 视口重构、slash command、补全、Vim/撤销系统、附件、粘贴占位符、跨会话历史搜索、剪贴板读取、后台任务、运行中输入排队和 steering。无 bracketed paste 的时间启发式识别、终端原始字节解码/缓冲修复及增强键盘协议不属于本次能力承诺，具体限制见 design。
