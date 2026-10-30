## MODIFIED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL向解析后的相对`messages` endpoint发送Anthropic Messages请求并保留`base_url`路径前缀。请求 SHALL使用既有鉴权/version/JSON/SSE headers；正文 MUST包含model、正整数`max_tokens`、可选临时项目指令、按原顺序排列的Anthropic native history、`stream=true`和当前Tool Catalog编译出的稳定tools数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

存在项目指令时，其user context message SHALL位于已提交history和真实当前输入之前，并保持独立且不进入native history。Read、Glob、Grep、Write facade与structured `Edit`/`Bash` facade SHALL编译为稳定名称、description和strict JSON input schema；tools顺序与schema bytes MUST来自本次不可变catalog snapshot。请求不得声明custom/freeform tool、后台进程、write_stdin、system、thinking配置或`cache_control`。secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first Anthropic turn
- **WHEN** 空会话收到用户文本且catalog包含当前六个Anthropic facades
- **THEN** 请求包含用户message以及稳定排序的Read、Glob、Grep、Write、Edit和Bash schemas，并保持model、max_tokens和stream字段确定

#### Scenario: Compile the first Anthropic turn without project instructions
- **WHEN** 无项目指令快照但启用相同catalog
- **THEN** messages不生成空context message，tools保持存在且顺序稳定

#### Scenario: Preserve an Anthropic API path prefix
- **WHEN** `base_url`包含路径前缀
- **THEN** 系统在该前缀后追加`messages`而不回退host根路径

#### Scenario: Send Anthropic authentication and version headers
- **WHEN** 系统建立Messages SSE请求
- **THEN** 使用配置secret和`anthropic-version: 2023-06-01`，且secret不进入错误或snapshot

#### Scenario: Compile a later Anthropic turn from native history
- **WHEN** 已完成文本或工具回合后使用相同外部snapshots提交下一输入
- **THEN** messages 按项目指令、包含当前已提交用户输入的原生 messages 顺序排列，且不从共享事件重建

#### Scenario: Produce stable request bytes
- **WHEN** model、catalog、项目指令、native history和当前输入相同
- **THEN** canonical request bytes和fingerprint完全相同，cwd、Session identity、invocation ID和approval状态不参与

#### Scenario: Compile a continuation after tool results
- **WHEN** 原生 history 已提交 assistant tool_use 及匹配的 user tool_result 且没有新增 user_inputs
- **THEN** 下一请求按原顺序回放call/output并直接继续sampling，不再追加新的user message

#### Scenario: Keep Write catalog bytes stable
- **WHEN** 相同 catalog 下改变 workspace 内容、Read observation 或 Write grant
- **THEN** tools canonical bytes 与 tool catalog fingerprint 保持一致

每条追加输入 SHALL 保持独立用户消息，InputID/turn identity 不得进入 wire 或内容 fingerprint。 tool_result user message 与后续用户文本 message 保持独立，不改写已提交 outputs 或自动合并消息。

#### Scenario: Compile ordered steering inputs
- **WHEN** 整组工具 outputs 后提交两条自动输入再请求下一 sample
- **THEN** 两条用户消息按 admission 顺序各出现一次，calls/outputs 配对和 opaque reasoning 保持原样；恢复与不中断请求 canonical bytes/fingerprint 相同

### Requirement: Native Messages history commits transactionally

系统 SHALL 保持原生 message/item 边界与字段。用户输入独立 prepare、Sync、finalize 为 user_inputs。sample staging 仅保存完成模型输出、metadata、raw usage 和 ready calls，合法 Provider terminal 后形成 prepared sample；失败、取消、timeout、协议错误或 EOF 丢弃本 sample staging，但不得删除先前已提交的用户输入。

Read、Glob、Grep、Patch与Write results SHALL由Anthropic result codec编码为一个user message中的有序`tool_result` blocks；每个block必须匹配前一assistant message中尚未闭合的`tool_use.id`，并保存冻结模型preview与error状态。approval request/decision不得伪装成Anthropic message；只有最终tool result进入Provider history。tool-output entry只有在Session commit durable后才能进入Conversation history。

Write 的完整调用参数与冻结结果 SHALL 无损保存和恢复；Read observation/approval 展示不是 native item，不得影响已提交 native history。

#### Scenario: Commit a successful Anthropic turn
- **WHEN** assistant message包含文本以及合法Read和Edit `tool_use`
- **THEN** 完整 assistant blocks、metadata 和 usage 进入 sample native commit；用户输入已在独立 user_inputs commit 中保存

#### Scenario: Compile a later turn from native history
- **WHEN** 已提交文本或tool call/result原生entries后开始下一sample
- **THEN** Provider直接按原生顺序编译history并保留thinking及call/output pairing

#### Scenario: Discard a partial failed turn
- **WHEN** 已产生文本或tool delta后sample失败
- **THEN** 本 sample 的 assistant blocks 和 calls 不进入已提交 history，已保存用户输入保持不变

#### Scenario: Preserve final message metadata without inventing usage
- **WHEN** message start/delta提供部分stop metadata或usage
- **THEN** Provider保存最终原始值并将缺失字段保持unknown而不是伪装为零

#### Scenario: Commit matching heterogeneous tool results
- **WHEN** Read与Patch results已durable
- **THEN** result codec按call index生成匹配call IDs的`tool_result` blocks并在durable finalizer后进入history

#### Scenario: Commit matching tool results
- **WHEN** Read、Glob、Grep或Patch results已durable
- **THEN** result codec生成匹配call IDs的有序`tool_result` blocks并在durable finalizer后进入history

#### Scenario: Commit a continuation without a synthetic user message
- **WHEN** tool-output entry已提交且下一Anthropic sample完成最终assistant message
- **THEN** sample entry只提交assistant message、metadata和usage，不复制tool results或构造空user message

#### Scenario: Reject mismatched tool results
- **WHEN** result缺少call ID、重复ID、capability错误或与未闭合calls数量/顺序不匹配
- **THEN** tool-output entry构造或恢复失败且不修改history

#### Scenario: Restore Write native history without reexecution
- **WHEN** 恢复含 Write 调用及冻结 tool_result 的原生历史
- **THEN** 下一请求 canonical bytes、native item 顺序与 fingerprint 和不中断路径一致
- **THEN** opaque reasoning/signature/phase 保持既有无损契约，workspace I/O 次数为零
