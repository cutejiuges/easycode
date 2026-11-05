# 助手 Markdown 最终验收

测试运行于 macOS、arm64、Apple M1 Pro、Go 1.24.13。Provider 验证使用本地 HTTP/SSE fixture，PTY 使用真实终端和本地会话替身。Linux、Windows 的终端延迟未实测。基准运行期间同机执行了仓库质量门，数据包含该负载。

## 场景与复现

下表的 TUI 测试文件均位于 `internal/tui/`。

| 规格场景 | 验证与观察 |
|---|---|
| 编码回复、富文本外部内容、无色显示 | `markdown_test.go` 比较去除 SGR 后的文本，检查列表、任务状态、链接目标、图片替代文字、代码空行和缩进。启动策略由 `internal/app/markdown_color_test.go` 覆盖。 |
| 词法分类、隔离、预算耗尽 | `markdown_highlight_test.go` 覆盖六种语言、别名、跨行语法、引号内变量、模板和 here-document。连续区间必须覆盖每个输入字节。用深度、队列、步骤、容量边界注入失败，检查部分结果已释放。架构检查位于 `internal/architecture/markdown_test.go`。 |
| 代码闭合、后到引用、稳定前缀、暂留 | `markdown_stream_test.go` 比较逐前缀布局与整篇解析，检查代码扫描次数，覆盖后到的引用、嵌套未闭合围栏、同一 delta 内的表头与半行。最小反例保存在 `testdata/fuzz/FuzzMarkdownStreamingFinal/`。 |
| 中文、窄表、终端注入、源偏移 | `markdown_test.go` 检查重复及空表头、四行阈值、长路径、实体解码后的 ESC、bidi、组合字符和 ZWJ。 |
| 可见正文、第五个候选、过期查询、流式截断 | `transcript_viewport_test.go` 的五个候选共享一页列表及三页正文，断言中间的工具位置。迟到响应、代际切换和失败交接见 `transcript_markdown_test.go` 与 `transcript_state_test.go`。 |
| 原文分页、模式切换、审批焦点、滚动锚点 | `transcript_browser_test.go` 验证跨缓存页往返。`transcript_viewport_test.go` 验证极窄窗口仍可到达 64 KiB 页尾及原文源锚点。`transcript_markdown_test.go` 检查草稿和回看位置。PTY 在审批打开时输入 r 并检查焦点。 |
| 延迟、连续、失败提交与新输出 | `transcript_markdown_test.go` 延迟列表或正文响应，检查旧画面、新 delta、查询失败提示及过时代次；实际 PTY 在查询屏障关闭期间输入，释放后等待投影完成。 |
| 长历史重绘与 resize | `markdown_test.go`、`transcript_markdown_test.go` 以解析计数检验 100 次重绘、50 项主线、宽度变化。`markdown_bench_test.go` 记录缓存热路径分配。 |
| 双 Provider 继续与恢复 | `internal/app/markdown_transcript_test.go` 使用原生 opaque fixture、浏览前后 journal 字节、后续原生顺序、请求字节及 fingerprint 作为断言。`exec_interactive_darwin_test.go` 在同一采样加入 Markdown、Read、Markdown，核对 TUI 顺序、审批次数、执行次数、持久化配对和恢复。见 [离线集成结果](providers.txt)。 |
| 固定窗口阅读 | `markdown_snapshot_test.go` 记录 80×24、34×12、12×6 的有色及无色界面。18 个固定输出分为结构、阅读交接及六语言高亮。34 列快照能看到长路径换行和缩略操作提示，12 列使用逐字段表格，主线跟随时保留末尾可见行。 |

## 容量审计

四个查询页按实例共享。Markdown 树节点和高亮 token 共享 32768 个节点，2 MiB 衡量派生布局、源映射及高亮存储。Go 测试的 B/op 则统计一次操作的累计分配，两种度量分别记录。

本次审计修复了三个分配点：拆分稳定前缀时需扣除同时存活的解析文档，合并行数组时需预留旧数组、输入数组及目标数组，构造展示行时需计入暂存字节和最终行字符串。预览与未闭合围栏也使用调用方的剩余布局预算。超过预算时保留摘要和原文入口，原文详情按窗口物化。五十项主线、窄窗口、长表、嵌套引用及合并容量临界点见 [边界测试记录](budget.txt)。

## 执行记录

- `go test ./internal/tui -run '^$' -bench 'Markdown|Highlight' -benchmem -count=5`，六语言 1、8、32、64 KiB，另含 1、8、32 字符增量、冷扫描、热历史、密集 token、深模板、长终止符及净化扩展。reserved-B/op 包含预留的 16 KiB scratch 和数组扩容时同时存活的容量。[五次原始样本](bench.txt)
- 两个 fuzz target 各运行 30 秒，分别检查任意有效 UTF-8 的区间覆盖及完整行前缀等价。[扫描记录](highlight-fuzz.txt) · [流式记录](stream-fuzz.txt)
- `python3 scripts/markdown-transcript-smoke.py`，按键通过真实 PTY 写入，独立回执记录 Update 受理时间。包含排在冷闭合扫描之后的输入、预算回退期间的审批与 stop、查询屏障、resize、终端属性恢复。每种模式首个启动样本单独列出。[逐次测量](pty.json)
- `make verify` 覆盖分支策略自测、gofmt、vet、Staticcheck、架构测试、全量测试和 race。[质量门输出](make-verify.txt)

## 本机实测

| PTY 模式 | 受理样本 | p95 | 最大 |
|---|---:|---:|---:|
| 有色 | 75 | 4.11 ms | 4.80 ms |
| 无色 | 75 | 2.95 ms | 4.16 ms |

两种模式均通过 p95 100 ms、最大 250 ms 门槛，并恢复终端属性。六语言的 64 KiB 冷扫描共 30 个样本，范围 0.50–1.55 ms。64 KiB Go 围栏按 1、8、32 字符分块，到最终布局及首次高亮耗时 3.60–5.16 ms。每次解析计数为 65,542 字节，保留 32,760 个 token，扫描器扩容峰值预留 606,184 字节。[收口基准](cold-completion.txt)

3 列宽、50 项的测试记录 405,386 字节常驻派生数据，4 列宽的四组长正文样例中最大为 855,924 字节。具体输入和节点数见容量测试记录。这些数值按应用持有的容量统计；进程 RSS 还包含 Go 堆、解析库和终端运行时。
