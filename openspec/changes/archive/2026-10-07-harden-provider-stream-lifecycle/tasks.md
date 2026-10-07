## 1. 封闭共享流事件契约

- [x] 1.1 为 `protocol.Event` 增加按已知 kind 分派现有 strict decoder/validator 的纯内存验证入口，明确允许 Provider 阶段尚未装饰 identity 的合法 assistant delta，并以 protocol 单测验证零值、未知版本、未知 kind、非法 payload 和全部合法事件。
- [x] 1.2 将 `provider.StreamEvent` 字段改为私有，加入 semantic、native item、completed、failed、cancelled 五类 typed constructor、只读 kind/payload accessor 和 `Validate`，并以表驱动测试覆盖每个合法构造、零值、未知 kind、nil payload、冲突字段、nil terminal error、非法 native item 和不适用 accessor。
- [x] 1.3 让 completed 构造和验证拒绝零值、非法或已 finalized 的 prepared sample，确认校验不执行 finalizer，并通过 `internal/provider` 单测覆盖 envelope、usage、finalizer 与 finalized 边界。
- [x] 1.4 一次性迁移全仓生产代码和测试 fake 对 `StreamEvent` 导出字段的直接构造与读取，运行 `rg` 确认不存在旧字段访问，并执行 `go test ./internal/protocol ./internal/provider/... ./internal/runtime` 验证编译与契约测试通过。

## 2. 收紧两家 Provider 的生产者生命周期

- [x] 2.1 将 OpenAI Responses producer 的 semantic、native、completed、failed、cancelled 发布路径迁移到 typed constructor，保持生产者唯一关闭 channel、取消后排空 transport、释放 active 状态后再关闭的顺序，并通过 OpenAI provider/reducer 单测。
- [x] 2.2 扩充 OpenAI integration tests，确定性验证成功、请求失败、用户取消、idle timeout 和 wire protocol error 各自产生一次合法 terminal、terminal 后无事件且 channel 最终关闭，并确认关闭后可启动下一 stream。
- [x] 2.3 将 Anthropic Messages producer 的 semantic、native、completed、failed、cancelled 发布路径迁移到 typed constructor，保持生产者唯一关闭 channel、取消后排空 transport、释放 active 状态后再关闭的顺序，并通过 Anthropic provider/reducer 单测。
- [x] 2.4 扩充 Anthropic integration tests，确定性验证成功、请求失败、用户取消、idle timeout 和 wire protocol error 各自产生一次合法 terminal、terminal 后无事件且 channel 最终关闭，并确认关闭后可启动下一 stream。
- [x] 2.5 运行两家 Provider request golden、native history restore 和 cache fingerprint 回归，确认请求 canonical bytes、原生 item 顺序、项目指令注入与 fingerprint 未变化。

## 3. Runtime 消费 ownership 与阶段拆分

- [x] 3.1 为每次 `Conversation.Stream` 创建派生 context，并实现单一消费 helper：逐项验证、记录唯一 terminal、正常路径读取至 channel 关闭，协议错误时保留首错、立即取消、停止转发并同步排空；以 helper 级测试验证状态转换。
- [x] 3.2 将 `Runtime.RunTurn` 拆分为 turn 开始、上下文规划、stream 消费、成功提交和失败收口等同层级私有 helper，保持 `turn_started`、Session batch、Provider finalizer 和 Runtime terminal 的既有 durable 顺序，并通过现有 runtime 顺序与 poisoned journal 测试。
- [x] 3.3 增加确定性故障注入，覆盖零值或非法事件、terminal 前关闭、重复 terminal、terminal 后 semantic/native/terminal、completed sample 在尾随事件后被丢弃，并断言派生 context 被取消、producer 退出、channel 被排空、finalizer 未误执行且只有一个 durable failure。
- [x] 3.4 增加调用取消与 completed/failed terminal 的确定性竞态测试，断言 Runtime 等待 channel 关闭、只发布一个终态、错误码与 `errors.Is` 语义保持不变，且不依赖 sleep 或概率竞态。
- [x] 3.5 增加连续 turn 回归：首个 stream 在协议错误后必须完成取消与排空，第二个 turn 才能启动且不受残留 goroutine、事件或 Conversation active 状态污染，并执行 `go test -race ./internal/runtime`。

## 4. 长期输入 owner 状态重构

- [x] 4.1 引入仅属于单次 `AgentLoop.Run` 的私有状态对象，集中 mode、queue、queued bytes、active turn、request ledger 和关闭转换，移除捕获这些可变状态的局部闭包，同时保持 owner goroutine 是唯一状态写者；运行 `internal/runtime` AgentLoop 全部测试。
- [x] 4.2 为提取后的 AgentLoop 状态方法补充直接回归测试，覆盖 starting/queued、队列上限、重复 request、targeted interrupt、terminal 竞态、drain、shutdown 和 poisoned journal 转换。
- [x] 4.3 增加 Provider stream 清理与 follow-up/shutdown 的排序断言，证明前一 channel 关闭和 producer 退出之前不会启动下一 turn或关闭最终 done，并通过 `go test -race ./internal/runtime`。

## 5. Headless runner 状态重构

- [x] 5.1 引入仅属于单次 `StreamRunner.Run` 的私有运行状态，分别封装 transport 请求、幂等 stop、reader/loop completion、首个 failure 和事件处理，保持 Run goroutine 是唯一 stdout writer、transport owner 是唯一 closer；运行 `internal/headless` 全部测试。
- [x] 5.2 为提取后的状态转换补充直接回归测试，覆盖正常 EOF 排空、显式 shutdown、调用取消、非法输入、broken output、loop 提前失败和 stop 竞态，并断言 cleanup owner 与最终完成信号唯一。
- [x] 5.3 执行 headless 历史 E2E 与 stream JSONL fixture 回归，确认输出 revision、事件顺序、错误字段和用户可见语义不变。

## 6. Provider 装配去重

- [x] 6.1 将 `newProviderResource` 收窄为只创建并返回 Provider resource，由 `providerWire` 单独负责 family 到 wire 的唯一映射，迁移调用方并删除被丢弃的 wire 返回值。
- [x] 6.2 补充 Provider 工厂表驱动测试，覆盖 OpenAI、Anthropic 和未知 family，证明 resource 创建不重复映射 wire，且 `providerWire` 是唯一映射来源；运行 `go test ./internal/app`。

## 7. 契约审计与完整质量门

- [x] 7.1 运行架构边界测试并搜索生产代码，确认没有旧 `StreamEvent` 直接字段访问、无直接绕过 typed constructor、无新增可变包级状态、无 owner goroutine 或重复 wire 映射。
- [x] 7.2 重新逐条核对根 `AGENTS.md`、本 change 的 proposal/design/specs 与实际实现，确认依赖方向、channel 关闭权、取消线性化、durable 顺序、错误码、Provider native/cache 稳定性和文档表述一致。
- [x] 7.3 执行 `openspec validate harden-provider-stream-lifecycle --type change --strict --no-interactive`、`go mod tidy -diff` 与 `git diff --check`，修复全部 artifact、依赖和格式问题。
- [x] 7.4 执行完整 `make verify`，确认 gofmt、go vet、Staticcheck、架构门禁、全量测试与 race test 全部通过，并在失败时记录未通过检查及风险而不弱化测试。
