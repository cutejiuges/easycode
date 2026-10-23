## MODIFIED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL向解析后的相对`messages` endpoint发送Anthropic Messages请求并保留`base_url`路径前缀。请求 SHALL使用既有鉴权/version/JSON/SSE headers；正文 MUST包含model、正整数`max_tokens`、可选临时项目指令、按原顺序排列的Anthropic native history、`stream=true`和当前Tool Catalog编译出的稳定tools数组。首个sample或新用户turn必须追加真实当前用户输入；tool outputs后的continuation sample不得追加合成user文本或空message。

存在项目指令时，其user context message SHALL位于已提交history和真实当前输入之前，并保持独立且不进入native history。Read、Glob、Grep、structured `Edit` 与structured `Bash` facade SHALL编译为稳定名称、description和strict JSON input schema；tools顺序与schema bytes MUST来自本次不可变catalog snapshot。请求不得声明Write、custom/freeform tool、后台进程、write_stdin、system、thinking配置或`cache_control`。secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

#### Scenario: Compile the first Anthropic turn
- **WHEN** 空会话收到用户文本且catalog包含当前五个Anthropic facades
- **THEN** 请求包含用户message以及稳定排序的Read、Glob、Grep、Edit和Bash schemas，并保持model、max_tokens和stream字段确定

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
- **THEN** messages按项目指令、已提交原生messages和当前输入顺序排列，且不从共享事件重建

#### Scenario: Produce stable request bytes
- **WHEN** model、catalog、项目指令、native history和当前输入相同
- **THEN** canonical request bytes和fingerprint完全相同，cwd、Session identity、invocation ID和approval状态不参与

#### Scenario: Compile a continuation after tool results
- **WHEN** 原生history已提交assistant`tool_use`及匹配的user`tool_result`
- **THEN** 下一请求按原顺序回放call/output并直接继续sampling，不再追加新的user message

## ADDED Requirements

### Requirement: Anthropic Bash calls preserve native tool semantics

Anthropic reducer SHALL按既有indexed content-block状态机归并`Bash`的`tool_use`与`input_json_delta`，仅在block完整、call ID唯一且完整输入通过当前catalog绑定的Exec strict decoder后产生typed ready call。partial、重复index/call ID、未知field、超限command或facade不匹配 MUST使sample失败且不得产生approval或进程I/O。

完成Bash原生block及其与text/thinking/signature/redacted-thinking的顺序 MUST无损保存在Anthropic native history。每个durable Exec result SHALL编码为匹配原tool-use ID的user `tool_result`，内容使用冻结模型preview并准确表达成功、非零退出、拒绝、取消、timeout或`outcome_uncertain`；approval与progress不得成为native block。

#### Scenario: Complete a streamed Bash call
- **WHEN** Bash参数跨多个input JSON delta到达并以合法command完成
- **THEN** reducer保留原生tool_use并产生一个匹配`process.exec`的typed ready call

#### Scenario: Restore a Bash call and result
- **WHEN** Session恢复包含Bash tool_use与匹配tool_result的native envelope
- **THEN** 下一Messages请求逐字节等价回放原生顺序且不重新执行命令
