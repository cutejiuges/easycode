## 1. 共享 Usage 领域模型

- [x] 1.1 在 `internal/domain` 实现不可变 `UsageMetric`/`SampleUsage`、`known`/`unknown`/`not_applicable` typed constructors、getter 和严格 validator；以 `go test ./internal/domain` 验证 known zero、非法状态、负数不可进入模型及值拷贝语义。
- [x] 1.2 实现无副作用的 sample-to-turn 聚合器，覆盖 known 求和、unknown 传播、not-applicable 规则、空集合和 `uint64` 溢出；以 domain 表驱动测试验证所有代数组合。
- [x] 1.3 为 Session、Runtime protocol 和 headless 分别定义显式 state/value wire DTO 与 domain 转换，禁止共享动态 map 或依赖 `omitempty` 推断状态；以各包 codec 单测验证 `known:0` 保留 value、非 known 不输出 value。

## 2. Prepared Sample 契约

- [x] 2.1 扩展 `provider.PreparedSample`，使 constructor、clone/getter 和 finalize 生命周期同时校验并携带 native envelope 与 normalized usage；以 `go test ./internal/provider` 验证 nil/零值/非法 usage、深拷贝和恰好一次 finalize。
- [x] 2.2 更新两家 Provider 及 Runtime 测试 fixture 的 prepared sample 构造调用，确保不存在无 usage 的成功 sample 或兼容空占位；以 `rg "NewPreparedSample" internal` 人工核对全部调用点并运行相关包测试。

## 3. Anthropic Usage

- [x] 3.1 将 Anthropic usage wire 计数改为可拒绝负数/溢出的严格表示，保留 start 明确零、delta output 零及 input/cache 占位零不覆盖规则；以 reducer 单测覆盖 start/delta/缺失/负数/最大值边界。
- [x] 3.2 在合法 `message_stop` 创建 normalized usage 并与现有 raw-usage native payload共同放入 prepared sample，失败路径丢弃两者；以 Provider 单测和 SSE 集成测试验证 raw/normalized 数值、unknown 和 not-applicable 映射。
- [x] 3.3 扩展 Anthropic native round-trip/golden 和恢复测试，证明 raw usage known/unknown 保真且 uninterrupted/restored 下一请求 canonical bytes 与 fingerprint 不变；运行 `go test ./internal/provider/anthropic`。

## 4. OpenAI Usage 与 Native v1

- [x] 4.1 扩展 Responses completion wire/reducer，按活动 response ID 严格解析 input、cached input、cache write、output 和 reasoning output，拒绝负值、溢出及 cached 大于 total；以 reducer 单测和随机 SSE chunk 集成测试覆盖完整、部分、缺失、显式零及非法 usage。
- [x] 4.2 实现 OpenAI raw usage 与 normalized mapping；缺失字段保持 unknown，uncached 仅在 total/cached 均已知时相减，任何非法关系不得 clamp；以 Provider 单测验证五项公共指标及 completed sample 绑定。
- [x] 4.3 直接补全 OpenAI native payload v1 以保存 raw usage，保持单一 strict decoder/restore，并原位重写仓库内 v1 fixture；以 codec/restore 测试验证完整 v1、未知字段、损坏 revision 和不共享 buffer，不保留旧 shape 或 v2 分支。
- [x] 4.4 扩展 OpenAI request golden/cache regression，证明 raw/normalized usage 不进入 RequestCompiler，uninterrupted 与从当前 v1 commit restored 的下一请求 item 顺序、canonical bytes 和 fingerprint 等价；运行 `go test ./internal/provider/openai`。

## 5. Session Usage 事实与 v1 基线重写

- [x] 5.1 在 Session registry 中新增 required `sample_usage(v1)` descriptor，并补全 `turn_completed(v1)` 的成功 batch 约束；实现专属 typed draft constructor、strict decoder 和 validator，以 Session codec/draft 测试验证未知字段、尾随 JSON、非法三态和 kind/revision 错配均被拒绝。
- [x] 5.2 重写 ReplayPlanner，使其只接受 `[provider_native_commit(v1), sample_usage(v1), turn_completed(v1)]` 成功 batch，输出有序 `SampleUsageRecord` 并拒绝缺失、重复、乱序、孤立 usage 及旧两记录完成结构；以 replay 表驱动测试覆盖所有 placement。
- [x] 5.3 更新 writer/loader/lifecycle 测试，验证新三记录 batch 具有同一 identity、连续 seq、全有或全无 Sync/repair 语义，append/Sync 失败不会恢复部分 native/usage事实；运行 `go test ./internal/session`。
- [x] 5.4 原位重写 `internal/session/testdata/migrations/v1/root.jsonl` 和 `replay_plan.json` 为完整 usage 基线并重新冻结；验证重写后的 fixture 可逐字节重编码，旧两记录开发 journal 在 repair、append 或 Provider 请求前 fail closed，且实现中不存在双 replay 路径。
- [x] 5.5 扩展真实子进程 Session 并发/lease 回归，确认 usage record 不改变 lease、repair、seq 单调和失败路径 journal bytes 不变契约；运行 Session 集成测试及 `go test -race ./internal/session/...`。

## 6. Runtime Durable 发布

- [x] 6.1 保持进程内 RuntimeEvent 协议版本为 1，直接为 `turn_completed(v1)` 增加强类型 turn usage constructor/decoder/validator，并同步 TUI/headless 只读消费者；以 `go test ./internal/protocol ./internal/tui/... ./internal/headless` 验证所有 v1 completion 都携带合法 usage 且不存在旧空 payload 分支。
- [x] 6.2 修改 Runtime completed 路径，在 append 前重验 prepared envelope/usage，并按 `[native commit(v1), sample usage(v1), completion(v1)] -> Sync -> Finalize -> emit turn_completed(usage)` 执行；以时间线测试验证顺序和单 sample aggregate 等价。
- [x] 6.3 扩展 Runtime 故障注入，覆盖非法 prepared usage、draft 失败、append/Sync 失败、durable 后 finalizer 失败、取消竞态和 poisoned 后拒绝新 turn；验证所有失败路径不发布成功 usage、不重复 finalize 且 durable journal 保持确定结果。

## 7. Headless JSONL v1

- [x] 7.1 在外部 `turn.completed` v1 中增加 required 强类型 `usage` 字段，保持顶层 `version:1`、事件集合、顺序和退出码不变；以 projector/encoder 单测验证 producer 总是输出五项指标且不泄露 raw Provider 字段。
- [x] 7.2 更新 `internal/headless/testdata/events.golden.jsonl` 及 CLI 集成测试，覆盖 known zero、unknown、not-applicable、resume 和失败流；验证 stdout 每行可解析且 `--print` 输出完全不变。
- [x] 7.3 增加 v1 契约测试，证明缺少 usage 的 `turn.completed` 不属于当前 schema、已知事件的未知字段仍可忽略，而未知事件 type 仍失败；运行 `go test ./internal/headless ./cmd/easycode`。

## 8. 文档与质量门

- [x] 8.1 同步 `docs/architecture/overall-architecture.md` 的 UsageParser、PreparedSample、单一 v1 Session batch 和 Runtime/headless 数据流，并明确版本号只在已冻结的不兼容契约需要并存时升级；核对文档中的 record vocabulary 与实际 codec 一致。
- [x] 8.2 同步 `docs/roadmap/product-roadmap.md` 的 P2 usage/cache 进度、已完成范围和仍延期的成本/context/TUI/cache request 能力；检查没有把非目标写成已交付。
- [x] 8.3 对 Provider/native/Session/headless fixtures 做 secret 扫描与 canonical bytes/fingerprint 回归，确认无 API key、Authorization、base URL、请求正文诊断副本或本机路径；运行相应 golden 测试。
- [x] 8.4 执行 `make verify`，确认 gofmt、go vet、Staticcheck、架构边界、全量测试和 race test 全部通过，并复核 OpenSpec tasks、实际文件路径和实现 vocabulary 一致。
- [x] 8.5 运行 `rg -n "v2|V2|Version2|revision 2" internal openspec/changes/add-durable-sample-usage docs` 并逐项核对命中，确认本能力没有新增 v2 常量、codec、fixture、迁移分支或文档承诺；已有无关命中必须记录来源而不得机械删除。
