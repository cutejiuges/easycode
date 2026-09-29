## 1. 收紧现有基础边界

- [x] 1.1 将 `internal/codec` 的稳定 JSON 配置改为不可替换的函数或显式不可变实例，移除生产代码中的可变包级 codec；运行现有 Provider golden、Session checksum 和 cache fingerprint 测试，验证 canonical bytes 完全不变
- [x] 1.2 删除没有真实 producer/consumer 的预留 RuntimeEvent kind，并以每个保留 kind 的 typed constructor、strict decoder 和 validator 替代导出的 `NewEventWithPayload(any)`；运行 protocol、runtime 和架构边界测试，验证非法 payload 与未知 kind 被拒绝
- [x] 1.3 将活动 turn 的完成信号改为 owner 持有且仅由 `runTurn` 关闭的 `done` channel，移除 `Shutdown` 每次等待时创建的 helper goroutine；增加并发 Shutdown、取消与完成竞态测试，并用 `go test -race` 验证无泄漏、无重复关闭和无锁内等待
- [x] 1.4 为应用资源关闭补齐“正常超时、强制取消或 transport close、等待唯一最终完成信号、再释放 journal/lease/provider”的显式升级路径；使用 channel 故障注入测试验证超时时资源所有权不丢失且测试不依赖 sleep

## 2. 实现 headless 输入和顶层结果契约

- [x] 2.1 实现纯 prompt resolver，支持位置参数、`-`、TTY 判定、管道附加上下文、合法 UTF-8、非空校验和 4 MiB 有界读取；用表驱动测试覆盖所有输入组合、尾部换行、读取错误、非法 UTF-8 和边界前后一个字节
- [x] 2.2 在 CLI 中增加互斥的 `--print` 与 `--json` 输出模式并限制最多一个位置参数，将现有 terminal helper 提升为直接依赖；用入口测试验证冲突参数、缺失终端输入和多余参数在配置加载、Session 创建和 Provider 调用前以退出码 `2` 失败，同时 `--version` 与默认 TUI 行为不变
- [x] 2.3 定义强类型 app outcome，区分 success、runtime failure、usage failure，以及未报告、已报告和 stdout 已失效状态；用单元测试验证 cmd 不会重复报告已输出的 JSON/text 终态，且 app 不直接调用 `os.Exit`
- [x] 2.4 实现统一 public failure projector，仅导出稳定英文 code、安全英文 message 和 cancelled 状态，未知错误映射为通用摘要；用包含 API key、Authorization、敏感 URL、prompt 和 Provider body 的嵌套 cause 测试验证 stdout、stderr 与 fixture 均不泄密

## 3. 实现版本化 headless 输出宿主

- [x] 3.1 新建 `internal/headless` 及最小 `ChatSession` 接口，定义 JSONL v1 的封闭 typed event union 和各事件构造器；通过编译期接口断言与单元测试验证宿主不依赖 Provider wire、Session store、TUI 或内部 opaque payload
- [x] 3.2 实现单 writer JSONL encoder，先完整 marshal 单个对象再执行 `writeFull` 与换行写入，并在首次写失败后永久标记 stdout 不可用；用 golden 和故障注入测试覆盖全部事件、字段顺序、短写、断管、控制字符、换行、U+2028 与 U+2029，验证每个完整输出行可独立解析
- [x] 3.3 实现内部 RuntimeEvent 到 JSONL v1 的严格 projector/state machine，校验 version、session/thread/turn identity、typed payload、事件顺序和唯一 terminal；用表驱动测试验证未知 kind、身份漂移、坏 payload、重复 terminal 与 terminal 前关闭均成为 `stream_protocol_error`，且不伪造成功
- [x] 3.4 实现文本模式聚合器，只在匹配的 durable `turn_completed` 后一次写出本 turn 最终文本并规范化为至多一个尾部换行；用测试验证空成功输出、已有换行、失败/取消/提前关闭时丢弃部分文本，以及完成后写失败不追加失败 Session 事实
- [x] 3.5 实现 JSON 模式 runner，按 `thread.started`、本次 turn 事件和唯一 terminal 的顺序同步消费并施加 writer 背压；用可控 fake session 验证成功、Runtime `turn.failed`、启动 `error`、零 delta 和多 delta 序列，且 stdout 不混入日志、旧 transcript 或纯文本诊断
- [x] 3.6 实现 context 取消与 writer 失败后的幂等 `Interrupt` 加 drain 逻辑，以 Runtime durable terminal 决定完成竞态结果；用确定性 channel 测试验证只中断一次、cleanup 完成后才返回、completed 胜出时只报告成功、stdout 失效后只向 stderr 诊断

## 4. 接入应用装配与命令行

- [x] 4.1 在 prompt 校验成功后复用 `newChatResources` 创建或恢复资源，并将 headless runner 作为与 TUI 平级的宿主接入 `internal/app`；用装配测试验证新 Session identity 稳定、resume 标志来自显式参数、headless 不消费 `SemanticHistoryView` 且无效输入不留下 Session
- [x] 4.2 让启动阶段的配置、Session 和 Provider 装配失败按模式输出：JSON stdout 仅一个 `error`，文本模式仅安全 stderr；用入口测试验证退出码为 `1`、无虚构 identity、无重复错误和无底层 cause 泄漏
- [x] 4.3 将 cmd 的 stdout、stderr、stdin、TTY 探测和退出状态接入强类型 outcome，并保持 cleanup defer 在返回状态前完成；用进程级测试验证成功为 `0`、运行/取消/输出失败为 `1`、用法错误为 `2`，且 JSON 模式每个 stdout 非空行都符合 v1 schema
- [x] 4.4 验证文本与 JSON 输出发生故障时仍由应用唯一 owner 完成 Runtime、journal lease、Repository 和 Provider 的有序关闭；用可阻塞 writer/stream/close 的集成测试验证不会提前返回、重复关闭或在锁内等待

## 5. 验证 Session 恢复与双 Provider 等价性

- [x] 5.1 为 Anthropic Messages 与 OpenAI Responses 增加 headless 新 Session 的 httptest/golden，验证请求编译、文本与 JSON 输出顺序、durable terminal、原生 item 顺序和稳定 fingerprint，且不新增 Session record kind 或写入 headless 协议行
- [x] 5.2 为两个 Provider 增加 uninterrupted 与 `--resume` 后下一 turn 的等价测试，比较 canonical request bytes、native history 顺序、cache fingerprint 和追加 seq；同时验证文本只含本次答案、JSON 首条为原 identity 且 `resumed=true` 的 `thread.started`
- [x] 5.3 增加 resume 缺失、占用、损坏、配置不兼容和 interrupted-tail 补偿测试，验证在新请求与新 turn append 前失败、既有 journal bytes 满足原 repair 契约且不创建替代 Session
- [x] 5.4 增加 Provider 失败、用户取消、事件 channel 提前关闭和 JSON broken pipe 的端到端测试，验证唯一失败终态、幂等中断、部分 assistant 输出不进入成功 native history以及退出前 lease 可被后续进程获取

## 6. 同步文档并通过质量门

- [x] 6.1 在功能及集成测试通过后更新 README、总体架构、产品 Roadmap 和 pitfall 记录，将 headless 文本/JSONL 的已实现边界、输入上限、退出码和未实现能力与源码保持一致；通过文档引用检查确认没有把 stdin JSON、tool/usage 事件或 `--continue` 写成已完成
- [x] 6.2 执行 `gofmt`、`go mod tidy` 和 `git diff --check`，核对依赖变更只包含 terminal helper 的 direct 标记及实现所需内容，并验证不存在无用代码、失效 flag、可变全局 registry/codec 或越层依赖
- [x] 6.3 执行 `make verify`，确认 go vet、Staticcheck、架构边界、全量测试、race test、OpenSpec 契约检查和新增 golden/fixture 全部通过，并记录任何无法执行的检查及剩余风险
