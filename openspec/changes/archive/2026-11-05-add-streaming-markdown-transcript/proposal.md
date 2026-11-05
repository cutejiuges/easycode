## Why

现有双 Provider coding loop、持久会话、压缩、缓存和启动选择器已经可用，但助手回复仍显示 Markdown 标记，代码、列表和表格难以阅读。优先补齐 P5 的流式 Markdown 阅读切片，能直接改善每轮编码交互，且无需扩大 Provider、工具执行或 Session 契约。

## What Changes

- 为助手完整正文提供标题、强调、列表、引用、基础语法高亮代码块、链接及表格的确定性终端展示；窄屏表格切换为逐行字段展示，无色模式仍可辨认结构。
- 流式正文采用稳定块、活动尾块和未完成行预览，分别验证解析等价与展示稳定性；提交时有界暂留画面并原子交接，不先清空正文。
- 主线与 Ctrl+O 助手正文详情共享渲染器；完整正文使用 Markdown，512 字节摘要、丢失前缀的流式片段及跨 64 KiB 页的正文显式显示原文片段，不猜测缺失语法上下文。
- 按可见范围和阅读锚点调度正文加载，保留仍在预算内的可见旧回复；主线与详情共享四页查询缓存，并分别限制解析、布局及交接状态占用。
- 主线提供有界回看和恢复跟随；详情支持 Markdown/原文切换，resize 与切换尽量保持源位置锚点。工具结果、diff、用户输入和启动选择器维持各自展示语义。
- 增加高亮词法分类与逐字节保真、实例隔离、确定性扫描预算、病理输入及分块等价、安全、固定尺寸 snapshot、双 Provider 恢复与缓存不变性、性能和真实 PTY 验证。

## Capabilities

### New Capabilities

- `tui/markdown-transcript`：助手正文的流式 Markdown、安全终端布局、有界缓存、完整与片段展示，以及原文切换。

### Modified Capabilities

- `tui/tool-transcript`：明确主线阅读锚点与可见正文加载、共享查询预算、详情模式切换和审批期间的阅读位置保持。保留既有全部导航、取消、查询所有权和工具原文要求。

## Impact

- 主要修改 `internal/tui`，新增专用 Markdown 解析适配、布局和状态文件；`Model` 只组合这些职责。
- 复用 `TranscriptQueries`、`More`、正文偏移和 generation；不新增 RuntimeEvent、Session record、版本字段或 Provider capability。
- 计划引入固定版本的 Goldmark 解析器，仅在 TUI 内适配 AST；基础代码高亮由实例持有、预算受控的有限语言词法扫描器实现，覆盖 Go、JSON、Python、JavaScript/TypeScript、Shell，不引入 Chroma/regexp2 或后台高亮进程。复用现有 ANSI 显示宽度与字素库，依赖符合 Go 1.24 和无全局可变配置约束。
- 更新 Roadmap、架构的 TUI 展示说明及实际踩坑记录；实现后执行 `make verify`，附专项验证证据。
- 非目标：语法高亮主题系统、OSC 超链接/剪贴板、图片加载、HTML 执行、数学/Mermaid 渲染、任意长度跨页 Markdown 重建、reasoning 展示、重试、扩展或 Subagent。
