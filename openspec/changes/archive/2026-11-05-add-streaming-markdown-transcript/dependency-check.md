# 高亮依赖检查记录

2026-11-04，任务 1.1 尚未通过，全部 26 项仍未完成。本记录不替代方案审批，不改变既定高亮范围。

## 实际检查

- Goldmark v1.7.8：Go 1.19、MIT；尚未完成适配及预算验证。
- Chroma v2.15.0：Go 1.19、MIT，依赖 regexp2 v1.11.4。
- Chroma v2.20.0：Go 1.22、MIT，依赖 regexp2 v1.11.5。
- 查询到的最新 Chroma v2.27.0 声明 Go 1.25，不适合作为本项目 Go 1.24 基线的直接替换；未验证其实现。
- 依赖只下载到模块缓存；工程 go.mod/go.sum 及生产代码未修改。

## 可复现证据

在仓库外的临时 Go module 中固定 Chroma v2.20.0 和 regexp2 v1.11.5，仅导入 Chroma 核心包，不导入 lexers/quick。使用 NewLexer 创建 root 状态的两条规则：`[a-z]+` 输出 Name，`.` 输出 Text；对 `hello` 调用 Tokenise 并消费迭代器至 EOF。各步骤用 runtime.Stack 的全部 goroutine 快照检查 `regexp2.runClock`，得到：

```text
before constructor: clock=false
after constructor: clock=false
after tokenization: clock=true
after global test cleanup: clock=false
```

探针最后调用 regexp2.StopTimeoutClock，仅清理隔离测试进程，不建议用于产品。上游明确将此全局函数限定为测试用途。复现不依赖 sleep 或概率竞争。

源码交叉检查：

- Chroma v2.20.0 `regexp.go:375` 将每条规则的 MatchTimeout 固定为 250ms；`regexp.go:499` 调用匹配，错误不向调用者传播，而是继续规则选择。v2.15.0 对应位置为 345、458。
- regexp2 v1.11.5 `fastclock.go:87` 在匹配期间隐式启动全局 runClock；`regexp.go:111` 说明 StopTimeoutClock 的测试限定。v1.11.4 具有同样的全局时钟机制。
- 构造本身纯内存，问题发生在首次匹配生命周期。仅避免全局 lexer 注册不足以消除该问题。
- 单条规则超时不是代码块总耗时预算；适配层在 token 之间计数不能中断正在进行的规则匹配。本轮未运行病理输入性能测量，不宣称已测出特定 UI 延迟。

因此，已检查的两个 Chroma 版本不能直接满足已批准设计要求的实例所有权、可清理后台生命周期及高亮执行预算。此结论不推广到所有版本或其他高亮实现。

## 待确认的修订建议

保留 Goldmark 与全部可见高亮要求，将高亮实现改为项目内有限语言词法扫描器：Go、JSON、Python、JS/TS、Shell；以实例状态和显式字节/token 预算运行，不启动 goroutine，不使用回溯正则。共享扫描游标与 token 输出，按语言隔离规则；保留未知语言、未闭合围栏、失败/超预算的明确纯文本路径。需先具体修订 design/tasks，说明字符串、注释、模板与嵌套结构的规则及回归样例，再实施，不把词法着色描述为完整语义分析。

另一方向是隔离进程内运行 Chroma，但会新增进程所有权、取消、IPC 和部署成本，不建议为本切片扩大到该方向。

## 修订设计前的验证边界

现有工程 `make verify` 已通过，覆盖格式、vet、Staticcheck、架构测试、全量测试及 race；多数 Go 测试命中缓存。这次检查发生在实现之前，仅作为依赖决策的基线。后续实现的测试见 [验收记录](verification/README.md)。
