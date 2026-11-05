## ADDED Requirements

### Requirement: Anthropic user questions preserve native tool semantics

Anthropic SHALL 从当前 catalog 稳定编译 `AskUserQuestion` structured tool，按 indexed block 状态归并 input_json_delta，仅在 block stop、完整参数 strict decode 和 message_stop 全部合法后产生 `user.question` ready call。参数半包、非法 Unicode、重复键/题目 ID、未知字段或模型提供 answers MUST 使 sample 失败，零问卷和执行；schema 不接受预填宿主答案或权限字段。

原始 tool_use 与完整冻结回答 SHALL 以匹配 tool_use.id 的 tool_result 保持原 block/call 顺序；保留 thinking/signature、redacted thinking 和其他原生语义。回答不成为新用户输入身份，问卷状态不进入 Messages history。

#### Scenario: Complete a fragmented question block
- **WHEN** 中文问题参数跨随机 chunk、半包和 UTF-8 边界完成
- **THEN** message_stop 后恰好一个 typed ready call，输入完整前没有问卷事件

#### Scenario: Pair a declined question
- **WHEN** 用户显式跳过整组问题或无交互宿主返回不可用结果
- **THEN** 以准确语义生成同 call ID 的 tool_result，后续模型仍可在既有循环限额内完成回复
