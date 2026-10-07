## MODIFIED Requirements

### Requirement: Compile a text-only Responses request

系统 SHALL 向解析后的 `responses` endpoint 发送 OpenAI Responses 请求。请求 MUST 包含配置的 model、可选的临时项目指令上下文 item、完整会话原生 input 和当前用户输入，并 MUST 设置 `stream=true` 与 `store=false`。

存在项目指令文档时，RequestCompiler SHALL 把规范化快照编码为一个 OpenAI 原生 user context item，并将它置于全部已提交原生历史和当前用户 item 之前。该 item MUST 与真实用户输入保持独立，使用确定的来源边界，并在每轮从同一快照重新生成；它不得进入 Conversation native history、prepared sample 或 semantic history。不存在项目指令文档时，编译结果 MUST 与既有请求完全相同且不得生成空 context item。

本阶段请求 MUST NOT 使用 `previous_response_id`，MUST NOT 声明工具，并 SHALL 请求保留服务端返回的 encrypted reasoning 内容。Authorization SHALL 使用配置的 secret，且该 secret、项目指令正文和绝对来源路径 MUST NOT 进入错误、日志、fixture 或 snapshot；项目指令只可出现在专门验证请求编译的脱敏 golden 中。

#### Scenario: Compile the first turn

- **WHEN** 空会话在存在项目指令文档时收到第一条非空用户文本
- **THEN** 请求 input 先包含一个项目指令 user context item，再包含该用户输入对应的独立 OpenAI 原生 item
- **THEN** 请求启用 stream、禁用 store 且不包含 `previous_response_id`

#### Scenario: Compile the first turn without project instructions

- **WHEN** 空会话使用无文档项目指令快照收到第一条非空用户文本
- **THEN** 请求 input 只包含该用户输入对应的 OpenAI 原生 item
- **THEN** 请求 canonical bytes 与本变更前的相同输入 fixture 完全一致

#### Scenario: Compile a later turn from native history

- **WHEN** 已完成一轮对话后使用同一项目指令快照提交下一条用户文本
- **THEN** 请求 input 按项目指令 context item、已提交用户 item、assistant 原生 output items、新用户 item 的顺序排列
- **THEN** 请求不是从 RuntimeEvent、TUI transcript 或扁平共享 Message 反向构造

#### Scenario: Deterministic request compilation

- **WHEN** model、项目指令快照、原生历史和当前输入完全相同
- **THEN** 编译结果的稳定 JSON bytes 完全相同
- **THEN** 项目根绝对路径、cwd、mtime 和 Session metadata 不参与请求编译
