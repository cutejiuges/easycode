## 1. HTTP 与 SSE Transport

- [x] 1.1 将 `base_url` 校验和 endpoint 解析收敛到 transport 边界，拒绝 userinfo/query/fragment 并保留已有路径前缀；用 table test 验证 host-only、带 `/v1` 前缀、尾斜杠和非法 URL。
- [x] 1.2 增加 Resty raw-body 流式请求入口，使用稳定 JSON bytes、禁用 streaming 自动重试并安全分类非成功 HTTP 状态；用 httptest 验证 method、headers、正文稳定性和 secret 不出现在错误中。
- [x] 1.3 实现有界 SSE frame parser，覆盖 LF/CRLF、comment、`event`/`id`、多行 `data`、EOF 和默认 4 MiB event limit；用 fixture 验证所有 frame 语义。
- [x] 1.4 增加随机 chunk 与跨 UTF-8 边界测试，重复使用不同 chunk size 解析同一 fixture 并验证事件序列完全一致。
- [x] 1.5 实现 stream supervisor 的 context cancel、5 分钟默认 idle watchdog、body close、reader 等待和 channel owner 清理；用短 timeout、取消和 race test 验证无阻塞与无 goroutine/channel 生命周期错误。

## 2. Provider 与协议契约

- [x] 2.1 将共享 Provider contract 调整为 session-scoped Conversation 与显式 semantic/native/completed/failed/cancelled stream kind，并更新所有 fake provider；用 provider/runtime 单测验证 terminal 恰好一次且 terminal 后无事件。
- [x] 2.2 在 `fault` 中补充 stream protocol、idle timeout、provider request 和 user cancellation 的稳定英文错误码；用错误分类测试验证 `errors.Is(context.Canceled)` 与错误文本脱敏。
- [x] 2.3 为 `assistant_text_delta` 和 `turn_failed` 增加强类型 payload、构造器和 decoder；用 protocol round-trip 测试验证 JSON shape、必填字段和版本 envelope 稳定。
- [x] 2.4 重构 OpenAI Provider 为不可变配置/transport owner，并创建持有独立 NativeHistory 的 Conversation；用两个 conversation 实例测试验证历史互不共享且 Provider Close 正确释放 transport。

## 3. OpenAI Responses 文本能力

- [x] 3.1 定义 text-only Responses request 和 user/message/reasoning native item envelope，已知字段强类型化且未知扩展限制在 OpenAI 包内 `json.RawMessage`；用 marshal/unmarshal round-trip 测试验证 opaque 数据不丢失。
- [x] 3.2 实现首轮和后续轮 request compiler，发送完整 native input、`stream=true`、`store=false` 和 encrypted reasoning include，且不发送 tools、prompt cache key 或 `previous_response_id`；用 golden 与 cache fingerprint regression 验证稳定 bytes。
- [x] 3.3 实现 Responses reducer 对 created、text delta、output item done、completed、failed 和 incomplete 的处理；用 event fixture 验证顺序、错误分类、未知 event 忽略和已知 event 缺字段失败。
- [x] 3.4 实现 turn staging 与 completed 后事务提交，失败、取消、timeout 和 completed 前 EOF 丢弃 staging；用单测验证第二轮历史只包含成功 turn 的 user/output items。
- [x] 3.5 将 OpenAI Conversation 接到 raw SSE transport 并收敛 Capabilities 到已实现能力；用 httptest 验证 Authorization、Accept/Content-Type、terminal、提前断线和 capability matrix。

## 4. Runtime 与会话编排

- [x] 4.1 重写 `RunTurn` 的终态状态机，仅 completed 产生 `turn_completed`，failed/cancelled/terminal 前 channel close 产生一次失败结果；用顺序测试覆盖成功、失败、提前关闭和 emitter 事件序列。
- [x] 4.2 增加单 conversation active-turn guard 和取消/完成竞争处理，锁内不执行网络或 emitter；用并发测试验证第二次提交被拒绝且不会同时产生 completed/failed。
- [x] 4.3 实现供宿主消费的 ChatSession facade，负责创建 turn context、顺序转发 RuntimeEvent、interrupt、shutdown 和等待清理；用生命周期测试验证取消、重复 interrupt、关闭和 goroutine 退出。

## 5. TUI 与应用装配

- [x] 5.1 将 TUI Model 扩展为单行 draft、idle/streaming 状态、用户/assistant transcript 和错误摘要投影；用固定消息序列单测验证 delta 按顺序追加且已有 transcript 不被失败清除。
- [x] 5.2 实现 Enter 提交、空白忽略、streaming 禁止并发提交、Esc/Ctrl+C 中断、idle 空输入 Ctrl+C 退出及 `q` 普通输入；用 key event 测试逐项验证行为。
- [x] 5.3 通过 `tea.Cmd` 接入 ChatSession submit/event wait/interrupt，不让 Model 直接依赖 Provider 或 transport；用 fake ChatSession 测试 completed、cancelled、failed 后均恢复可输入状态。
- [x] 5.4 在 app 中加载并校验环境配置，装配 OpenAI Provider、Conversation、Runtime、ChatSession 和 TUI，并在退出时 shutdown/close；用 app 测试验证缺失配置、Anthropic unsupported 和 secret 脱敏。
- [x] 5.5 更新 CLI 交互模式说明，移除 scaffold 文案但不扩展 `--print`、JSON 或 resume；运行 CLI/TUI smoke test 验证有效配置进入 Chat、无效配置在请求前失败。

## 6. 集成、文档与质量门

- [x] 6.1 增加无外网的双轮 httptest 集成测试，分别使用 host-only 和带路径前缀的 base URL，并断言第二轮 request input 包含第一轮完成的原生 output item。
- [x] 6.2 增加固定终端尺寸 TUI snapshot，覆盖 idle、draft、streaming、completed、cancelled 和 failed，并确认 snapshot 不包含时间戳、API key、Authorization 或请求正文。
- [x] 6.3 更新总体架构中的 Codex 参考提交为 `7498521d2`，在 Roadmap/踩坑记录中记录 raw-body SSE parser 结论、本纵向切片边界和延期能力；人工核对文档与 proposal/spec/design 一致。
- [x] 6.4 执行 `make verify`，确认 gofmt、go vet、Staticcheck、全量测试和 race test 全部通过，并检查工作区不存在 dead code、真实 API key、无用依赖或未解释的兼容分支。
