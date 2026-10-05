## 1. 进程内命令与结果契约

- [x] 1.1 将 `internal/protocol/command.go` 的占位结构替换为封闭的 v1 request identity、submit/interrupt/shutdown typed command、constructor/getter/validator/strict decoder，并验证单测覆盖每个合法 kind、缺失/未知/重复字段、尾随 value、非法 ID、非法 UTF-8、空白文本和 4 MiB 边界。
- [x] 1.2 增加 typed command result、correlated turn item 与 input-discard item，以及 `invalid_command`、`input_queue_full`、`duplicate_request`、`no_active_turn`、`turn_mismatch`、`session_closing`、`session_shutdown` 等安全 fault 映射，并验证零值、kind/result 不匹配和 secret-bearing cause 均被拒绝或投影为安全英文摘要。
- [x] 1.3 扩展架构约束测试，确认 command/result 的导出 API 不接受或返回 `any`、实例状态不使用可变包级 var、`New*` 不启动 goroutine或访问 I/O，并运行对应 protocol/architecture 单测。

## 2. 单 owner AgentLoop

- [x] 2.1 在 `internal/runtime` 实现纯内存 AgentLoop 配置与显式 `Run` 生命周期、无缓冲 request/reply admission 和固定 done 信号，并验证 nil/非法配置、重复 Start/Run、Run 前提交及 owner 正常退出的单测。
- [x] 2.2 实现实例级 FIFO follow-up queue、消息数/总字节容量、byte accounting、outstanding/recent identity ledger 和 lost-wakeup 防护，并用并发测试验证空闲并发 submit 恰好一个 `starting`、其余 `queued`、FIFO 一输入一 turn、queue overflow 无副作用、重复 identity 只执行一次且所有状态有界。
- [x] 2.3 实现唯一 turn worker 与 correlated RuntimeEvent 转发，确保 command result 先于 `turn_started`、worker 退出与 durable terminal 后才启动下一 turn，并验证普通 Provider failure 后继续、poisoned/Sync failure 后拒绝全部 queue、任意时刻最多一个 Provider stream且 native history 顺序不变。
- [x] 2.4 实现携带 expected turn ID 的定向 interrupt，并用确定性 barrier 测试匹配取消、idle rejection、stale target 不误取消新 turn、重复 interrupt 幂等，以及 interrupt/terminal/下一 turn 启动三方竞态只有一个 terminal。
- [x] 2.5 实现 graceful input close、显式 shutdown、queued input discard、重复 shutdown 和 deadline 后仍保留 cleanup ownership，并用阻塞 Provider/transport 故障注入验证 EOF 排空、shutdown 取消、队列不执行、超时升级、最终 done 关闭且无 goroutine/channel owner 泄漏。

## 3. Headless stream-json wire

- [x] 3.1 在 `internal/headless` 增加独立的流式 stdin v1 DTO 和不超过 32 MiB 的增量 NDJSON reader，使用两阶段严格解码映射为 protocol constructors，并验证随机 chunk、半行、UTF-8 多字节边界、EOF 尾行、空行、重复/未知字段、多个 value、非法 UTF-8、超长 encoded line 和 4 MiB decoded text 边界。
- [x] 3.2 增加 sealed streaming stdout DTO、constructor/validator/projector 和单 writer encoder，覆盖 `thread.started`、`control.response`、带 `input_id` 的 turn/delta/terminal、`input.discarded` 与 `error`；提交手工固定的 v1 compatibility fixture，并验证每行 canonical JSON、字段语义、usage/error 安全性、response/terminal/下一 turn 顺序及一次性 JSON vocabulary 未变化。
- [x] 3.3 实现 `StreamRunner.Run` 协调 reader、AgentLoop 和 writer，使用显式可关闭 input/output unblock capability、唯一 cancellation owner 和固定 done 信号；以真实 pipe 与确定性 fault writer 验证 stdin 阻塞、stdout backpressure/短写/断管、协议错误、根 context 取消和 cleanup timeout 均能解除阻塞、等待全部 owner 退出且不会向损坏 stdout 追加内容。
- [x] 3.4 验证流式退出状态：clean EOF 与显式 shutdown 返回 0、已机器报告的单 turn failure 不污染健康进程退出、协议/输出/cleanup failure 返回 1、CLI 用法错误返回 2，并确保 stdout/stderr 与 secret redaction 契约通过集成测试。

## 4. CLI 与应用生命周期装配

- [x] 4.1 在 CLI 增加默认 `text` 的 `--input-format`，将 `--json --input-format stream-json` 归一化为独立 app mode，并验证与 `--print`、位置 prompt、`-`、终端 stdin、`--resume`/`--continue` 的合法与非法矩阵全部在配置、Catalog、Session 和 Provider 副作用前完成判定。
- [x] 4.2 在 `internal/app` 装配 AgentLoop、stream transport 和最小 lifecycle escalation 接口，保持现有资源关闭顺序；验证正常 shutdown 及 deadline 后 `Provider.Close -> AgentLoop/Runtime wait -> Journal Sync/Close -> Repository Close` 的唯一 ownership，且锁内不执行回调、I/O、channel wait 或 Close。
- [x] 4.3 为 Anthropic Messages 与 OpenAI Responses 增加同进程多 turn 集成回归，验证两个独立 submit 生成两个 durable turn、第二次请求使用各自 Provider-native committed history、请求 canonical bytes/fingerprint 不受 control metadata 污染、失败/取消输入不进入后续 history。
- [x] 4.4 增加 CLI 端到端协议测试，覆盖新建、`--resume`、`--continue`、并发排队、interrupt、EOF drain、显式 shutdown、queue full 和 malformed input；同时验证既有 `--print`、一次性 `--json`、TUI 默认入口和 headless v1 fixture/snapshot 完全保持兼容。

## 5. 文档、契约与质量门

- [x] 5.1 更新总体架构、P2 Roadmap 和 pitfall log，记录已交付的是进程内 FIFO follow-up control loop、queue 非 durable、EOF/shutdown 区别及 same-turn steer 仍待 P3 safe point，并通过文档引用/record vocabulary 核对。
- [x] 5.2 运行 `openspec validate add-runtime-input-control-loop --strict`，修复 proposal/spec/design/tasks 的 capability path、delta operation、场景格式和依赖一致性问题，确认 change 校验通过。
- [x] 5.3 运行 `make verify`，确认 gofmt、go vet、Staticcheck、架构边界、全量测试和 race test 全部通过；复核无 dead code、无失效占位、无 secret 泄漏、无 Session schema/Provider wire 非预期变化，并记录任何无法执行的检查与风险。
