## Why

双 Provider 文本 Runtime、append-only JSONL 和显式 `--resume` 已经形成可恢复的完整执行底座，但 EasyCode 仍只能通过 TUI 使用，现有 `--print` 只是返回未实现错误，脚本、CI 和其他宿主无法消费稳定结果。现在应以一个受控的单 turn headless 切片验证宿主边界和机器输出协议，同时复用既有 durable terminal，不提前引入工具、SQLite 或双向控制协议。

## What Changes

- 新增互斥的 `--print` 文本模式与 `--json` JSONL 模式；两者各执行一个新 turn 后退出，并支持与现有 `--config`、`--resume <thread-id>` 组合。
- 定义位置参数、stdin、显式 `-`、附加管道上下文、UTF-8 校验、输入大小边界和空输入的确定性规则；无效输入必须在创建 Session、修改 journal 或发起 Provider 请求前失败。
- 文本模式只在收到 durable `turn_completed` 后把本次最终 assistant 文本写入 stdout；失败或取消不泄漏部分文本，安全诊断写入 stderr。
- 新增独立于内部 RuntimeEvent 的版本化 JSONL 投影，首版仅暴露 `thread.started`、`turn.started`、`assistant.text.delta`、`turn.completed`、`turn.failed` 和不可恢复的 `error`；stdout 每行必须是一个完整 JSON object，诊断、日志和历史回放不得污染机器流。
- 明确 headless 的取消、输出写失败、事件流提前关闭、资源清理和进程退出语义；一次调用只产生一个新 turn，退出码区分成功、运行失败和 CLI 用法错误。
- 扩展显式 resume 契约到 headless 宿主：恢复只用于重建 Provider-native history 并续写原 thread，不向本次 stdout 重放既有 transcript，也不自动选择最近 Session。
- 收紧本切片依赖的内部边界：RuntimeEvent 只保留实际实现的 kind，payload 通过具体 typed constructor 构造，JSON codec 不再由可替换包级变量持有，ChatSession 等待复用 owner 持有的完成信号而不是每次创建 helper goroutine。
- 增加 CLI 输入矩阵、文本输出、JSONL golden、stdout/stderr 隔离、Unicode 行边界、失败/取消、短写/断管、双 Provider 新建与 resume、durable 顺序及 race 回归测试。
- 本变更不实现同一进程内多次输入、`--continue`、SQLite/session picker、历史事件 replay、Claude 风格双向 structured input/control、结构化模型输出、usage/reasoning/tool 事件、权限请求或 coding tools。

## Capabilities

### New Capabilities

- `headless/text-output`: 定义单 turn headless 的 CLI 输入、最终文本输出、稳定 JSONL 事件、stdout/stderr、取消、失败和退出码契约。

### Modified Capabilities

- `session/resume`: 将 root Session 创建与显式 resume 扩展到 headless 宿主，并明确恢复时只续写 Provider 原生历史、不向当前 headless 输出回放既有 transcript。

## Impact

- 主要影响 `cmd/easycode`、`internal/app`、新增的 `internal/headless`、`internal/runtime`、`internal/protocol`、`internal/fault`、`internal/codec` 及对应测试和 JSON golden fixture。
- `app.Run` 的内部调用契约需要区分“已输出机器终态但应非零退出”和“尚未对外报告的启动错误”，避免 JSON 模式重复输出终态或把诊断写入 stdout。
- Provider request compiler、stream reducer、native history、Session JSONL schema/record vocabulary 和 cache fingerprint 不改变；headless 仍通过共享 Runtime 和同一 durable writer 路径运行。
- 不新增第三方依赖。stdin 是否为终端由入口层显式解析并注入，测试不得依赖真实终端、sleep 或外网。
- 对外新增稳定 CLI 和 JSONL v1 协议；首版不承诺未实现的 event kind，未来 usage、reasoning、tool 或双向输入必须通过后续 OpenSpec change 扩展。
