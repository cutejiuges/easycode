## 1. 共享语义历史契约

- [x] 1.1 在 `internal/domain` 增加带稳定 JSON 字段的 `SemanticHistoryView` 与 `SemanticTurn` 值类型，约定空历史使用非 nil 空 turn 列表，并用单元测试验证 Provider 来源、turn 顺序和稳定序列化形态。
- [x] 1.2 在 `internal/provider` 增加单方法 `HistoryProjector` 并组合进 `Conversation`，更新 Runtime 测试 fake 及编译期接口断言；运行 `go test ./internal/domain ./internal/provider ./internal/runtime` 验证共享契约不引入具体 Provider 依赖或 Runtime 行为变化。

## 2. Anthropic HistoryProjector

- [x] 2.1 在 Anthropic 包实现基于深拷贝 `nativeTurn` snapshot 的纯文本投影，按 message/block 顺序连接 text 且忽略 thinking、signature、redacted data、metadata、usage 和 unknown raw；用表驱动单测覆盖空历史、多 turn、多 text block、空 assistant 文本和非文本项。
- [x] 2.2 添加 Anthropic projection golden，验证输出只包含 `provider/turns/user_text/assistant_text`，固定 opaque 数据不泄漏、顺序稳定且重复投影一致；修改返回视图后重新投影并编译下一轮 request，验证 native history、canonical request 和 fingerprint 均未改变。
- [x] 2.3 扩展 Anthropic Conversation 本地集成测试，验证成功 turn 的 live delta 拼接与 replay 投影一致，活动/失败/取消/提前 EOF turn 不可见，两个 conversation 投影隔离，并由 `go test -race ./internal/provider/anthropic` 验证并发投影与提交无数据竞争或部分 turn。

## 3. OpenAI Turn 边界与 HistoryProjector

- [x] 3.1 将 OpenAI `nativeHistory` 从扁平 item slice 收敛为可深拷贝的 `nativeTurn{User, Outputs}`，让成功 terminal 原子提交一个 turn，并使 RequestCompiler 按原顺序展平历史；运行首轮 request golden、双轮 API prefix、reasoning round-trip 和 fingerprint 测试，确认 wire bytes、item 顺序及失败不提交语义不变。
- [x] 3.2 实现 OpenAI 纯文本投影，只连接合法 user message 的 `input_text` 与 assistant message outputs 的 `output_text`，忽略 reasoning summary、encrypted content、phase、ID、unknown raw 和非文本项；用表驱动单测覆盖空历史、多 turn、多 output item/content part、空 assistant 文本和严格 role/type 过滤。
- [x] 3.3 添加 OpenAI projection golden 与投影独立性回归，验证 opaque 数据不泄漏、返回视图可修改而不影响后续投影，并验证投影前后下一轮 canonical request、fingerprint 和 encrypted reasoning 回放完全一致。
- [x] 3.4 扩展 OpenAI Conversation 本地集成测试，验证成功 turn 的 live delta 拼接与 replay 投影一致，活动/失败/取消/提前 EOF turn 不可见，conversation 隔离，并由 `go test -race ./internal/provider/openai` 验证并发投影与提交只返回完整快照。

## 4. 跨 Provider 契约与依赖回归

- [x] 4.1 增加共享 fixture/assertion，用语义等价的 Anthropic 与 OpenAI 原生历史验证两家投影产生相同的 turn 文本语义，同时确认 Provider-private 字段不会改变共享视图。
- [x] 4.2 检查 `domain/protocol/context/session/extension/subagent/tui` 的 import graph，确认语义历史消费者边界不导入 `internal/provider/openai` 或 `internal/provider/anthropic`，且 RequestCompiler 参数仍只接受 Provider-native 类型；用 `go list -deps ./...`、相关包测试和代码审查记录验证。
- [x] 4.3 回归现有 Runtime、App 和 TUI 测试，确认新增 Conversation 方法没有让 Runtime 主动投影历史、没有新增 RuntimeEvent 或改变实时 transcript；运行 `go test ./internal/runtime ./internal/app ./internal/tui` 验证。

## 5. 文档与质量门

- [x] 5.1 更新 README 与 Roadmap 的 P1 进展，说明双 Provider 已提供 committed-only 文本 HistoryProjector，并继续明确 Session/resume、token estimator、reasoning/tool projection、UsageParser 和 CachePlanner 尚未实现；核对 ADR-0002 无需改写且文档不声称阶段已全部退出。
- [x] 5.2 运行两家 projection/request golden、native round-trip、Conversation 集成与 cache fingerprint 回归，确认 fixture 不访问外网且不包含 API key、Authorization、Cookie、signature、redacted data、encrypted content 或其他 opaque secret 的语义投影副本。
- [x] 5.3 执行 `openspec validate add-provider-history-projection --strict`、`openspec validate --specs --strict` 和 `make verify`，并人工检查依赖方向、锁内无不可控工作、无共享可变快照、无 dead code、无无用依赖及 OpenSpec/架构/Roadmap 一致性。
