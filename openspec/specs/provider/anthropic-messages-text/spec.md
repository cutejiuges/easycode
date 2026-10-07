# provider/anthropic-messages-text Specification

## Purpose

定义 Anthropic Messages 在文本对话切片中的确定性请求、content-block 流式状态机和原生历史契约，使自定义 API 前缀能够完成多轮对话，同时无损保留 thinking signature 与 redacted thinking 等 Anthropic 原生数据。

## Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL向解析后的相对 `messages` endpoint发送Anthropic Messages请求并保留 `base_url`路径前缀。请求 SHALL使用既有鉴权/version/JSON/SSE headers；正文 MUST包含model、正整数 `max_tokens`、可选临时项目指令、按原顺序排列的Anthropic native history、`stream=true`和当前Tool Catalog编译出的稳定tools数组。首个sample或新用户turn必须追加真实当前用户输入；tool outputs后的continuation sample不得追加合成user文本或空message。

存在项目指令时，其user context message SHALL位于已提交history和真实当前输入之前，并保持独立且不进入native history。Read facade SHALL编译为名称 `Read`、稳定description和strict JSON input schema；tools顺序与schema bytes MUST来自本次不可变catalog snapshot。请求不得声明未实现工具、system、thinking配置或 `cache_control`。secret、项目指令正文、workspace绝对路径和动态调用identity MUST NOT进入错误、日志或普通fixture。

#### Scenario: Compile the first Anthropic turn
- **WHEN** 空会话收到用户文本且catalog只包含Read
- **THEN** 请求包含用户message和恰好一个Read tool schema，并保持model、max_tokens和stream字段确定

#### Scenario: Compile the first Anthropic turn without project instructions
- **WHEN** 无项目指令快照但启用相同Read catalog
- **THEN** messages不生成空context message，tools保持存在且顺序稳定

#### Scenario: Preserve an Anthropic API path prefix
- **WHEN** `base_url`包含路径前缀
- **THEN** 系统在该前缀后追加 `messages`而不回退host根路径

#### Scenario: Send Anthropic authentication and version headers
- **WHEN** 系统建立Messages SSE请求
- **THEN** 使用配置secret和 `anthropic-version: 2023-06-01`，且secret不进入错误或snapshot

#### Scenario: Compile a later Anthropic turn from native history
- **WHEN** 已完成文本或工具回合后使用相同外部snapshots提交下一输入
- **THEN** messages按项目指令、已提交原生messages和当前输入顺序排列，且不从共享事件重建

#### Scenario: Produce stable request bytes
- **WHEN** model、catalog、项目指令、native history和当前输入相同
- **THEN** canonical request bytes和fingerprint完全相同，cwd、Session identity和invocation ID不参与

#### Scenario: Compile a continuation after tool results
- **WHEN** 原生history已提交assistant `tool_use`及匹配的user `tool_result`
- **THEN** 下一请求按原顺序回放call/output并直接继续sampling，不再追加新的user message

### Requirement: Reduce Anthropic content blocks with an indexed state machine

系统 SHALL独立处理既有Messages事件并按block index与wire顺序归并content blocks。text/thinking/signature/redacted-thinking行为保持既有契约；`tool_use` block MUST包含非空id与已暴露名称，`input_json_delta.partial_json`只可追加到对应活动tool block。只有block stop后完整JSON通过strict facade decoder，且 `message_stop`在全部blocks停止后到达，tool call才可进入completed prepared sample。

重复index、未开始block的delta/stop、delta类型不匹配、重复call ID、未知tool、损坏/超限JSON、未完成block上的 `message_stop` MUST终止为stream protocol error。参数delta不得产生executor I/O或durable ready fact。

#### Scenario: Stream ordered assistant text
- **WHEN** text block依次收到text deltas并停止
- **THEN** 系统按顺序投影文本且原生block只保存最终文本一次

#### Scenario: Preserve thinking and its signature
- **WHEN** thinking/signature或redacted block合法完成
- **THEN** thinking与signature按既有规则保存在原生history且不作为可见文本

#### Scenario: Preserve redacted thinking
- **WHEN** redacted thinking block合法完成
- **THEN** opaque data不被解释或改写并保留在Anthropic原生item中

#### Scenario: Reject a mismatched delta
- **WHEN** text block收到thinking delta或tool block收到不匹配的delta类型
- **THEN** Provider产生stream protocol failure且不提交sample staging

#### Scenario: Ignore an unknown well-formed event
- **WHEN** 服务端发送当前切片未知但格式正确的event type
- **THEN** Provider不panic、不伪造call或输出并继续等待受支持事件或终态

#### Scenario: Complete a Read tool_use block
- **WHEN** `tool_use` block收到多个partial JSON deltas并以合法Read参数停止
- **THEN** 原生block无损保存call ID/name/input，prepared sample产生一个typed ready call

#### Scenario: Reject incomplete tool input
- **WHEN** tool block在JSON不完整、schema非法或名称未暴露时停止
- **THEN** Provider产生stream protocol failure且不提交sample或ready call

### Requirement: Native Messages history commits transactionally

系统 SHALL在Anthropic native history中保持message边界、content block顺序和原生字段。可选当前输入、完成assistant blocks、metadata、raw usage和ready calls SHALL先进入sample staging，仅在合法 `message_stop` 后形成prepared sample。首个sample必须暂存真实user message；tool outputs后的continuation sample不得暂存或提交新的user message。失败、取消、timeout、协议错误或EOF MUST丢弃整个staging。

Read results SHALL由Anthropic result codec编码为一个user message中的有序 `tool_result` blocks；每个block必须匹配前一assistant message中尚未闭合的 `tool_use.id`，并保存冻结模型preview与error状态。该tool-output entry只有在Session commit durable后才能进入Conversation history。

#### Scenario: Commit a successful Anthropic turn
- **WHEN** assistant message包含文本和一个合法Read `tool_use`
- **THEN** user输入、完整assistant blocks、metadata和usage一次性进入sample native commit

#### Scenario: Compile a later turn from native history
- **WHEN** 已提交文本或tool call/result原生entries后开始下一sample
- **THEN** Provider直接按原生顺序编译history并保留thinking及call/output pairing

#### Scenario: Discard a partial failed turn
- **WHEN** 已产生文本或tool delta后sample失败
- **THEN** 本sample输入、assistant blocks和calls均不进入已提交history

#### Scenario: Preserve final message metadata without inventing usage
- **WHEN** message start/delta提供部分stop metadata或usage
- **THEN** Provider保存最终原始值并将缺失字段保持unknown而不是伪装为零

#### Scenario: Commit matching tool results
- **WHEN** 对应Read result已durable
- **THEN** result codec生成匹配call ID的 `tool_result` user block并在durable finalizer后进入history

#### Scenario: Commit a continuation without a synthetic user message
- **WHEN** tool-output entry已提交且下一Anthropic sample完成最终assistant message
- **THEN** sample entry只提交assistant message、metadata和usage，不复制tool results或构造空user message

#### Scenario: Reject mismatched tool results
- **WHEN** result缺少call ID、重复ID或与未闭合calls数量/顺序不匹配
- **THEN** tool-output entry构造或恢复失败且不修改history

### Requirement: Completion and capabilities match implemented Anthropic behavior

Anthropic Provider MUST仅在合法 `message_stop` 后产生一次completed terminal；失败、取消、timeout、oversized frame、非法序列或EOF继续遵守唯一terminal和不自动重放契约。Provider只有在Read request编译、tool reducer、native codec/restore、result codec和golden全部通过后才声明function tools可用；parallel tools、custom/freeform tools、prompt cache control/key、previous response和未实现reasoning展示 MUST保持unsupported。

#### Scenario: Complete only on message_stop
- **WHEN** 全部blocks停止且收到合法 `message_stop`
- **THEN** Provider产生一次携带native commit、usage和ready calls的completed terminal并关闭channel

#### Scenario: Reject EOF before message_stop
- **WHEN** SSE在 `message_stop`前结束
- **THEN** Provider失败且不提交staging或自动重放

#### Scenario: Cancel or time out an Anthropic stream
- **WHEN** sample context取消或流超过idle timeout
- **THEN** Provider关闭资源、等待清理并产生一次可识别失败或取消terminal

#### Scenario: Inspect Anthropic capabilities
- **WHEN** Runtime查询capabilities
- **THEN** streaming、thinking-signature和function tools准确为可用
- **THEN** parallel/custom tools及延期cache能力未被声明

### Requirement: Anthropic usage reduces to a validated sample fact

Anthropic reducer SHALL 分别消费 `message_start.message.usage` 与 `message_delta.usage`，并在合法 `message_stop` 前形成最终 raw usage。start 中明确提供的零 SHALL 保存为已知零；delta 中 input、cache creation 和 cache read 的零 MUST NOT 覆盖 start 中已观察到的正值，也不得在没有 start 值时把占位零解释为已知零；delta 中明确提供的 output 零 SHALL 保存为已知零。后续正值 SHALL 按字段更新最终值，缺失字段保持 unknown。

所有已提供 token 值 MUST 为非负整数。负值、超出公共计数范围的值或无法按统一规则归一化的 usage MUST 使当前 stream 以协议错误失败，不提交 native history、`sample_usage` 或成功终态。合法 `message_stop` 产生的 prepared sample SHALL 同时携带保留 raw usage 的 native commit 和对应 normalized usage。

#### Scenario: Preserve start usage across zero delta

- **WHEN** `message_start` 报告 input `12`、cache creation `3`、cache read `7`，随后 `message_delta` 对这些字段报告零并报告 output `9`
- **THEN** 最终 raw usage 保持 `12`、`3`、`7`、`9`
- **THEN** normalized usage 以相同数值产生对应公共指标

#### Scenario: Keep absent Anthropic usage unknown

- **WHEN** 合法 Anthropic sample 没有提供某个可表达 usage 字段
- **THEN** raw usage 与 normalized usage 中对应字段均保持 unknown，而不是已知零

#### Scenario: Preserve an explicit output zero

- **WHEN** `message_delta.usage.output_tokens` 明确为零
- **THEN** output usage 为 `known: 0`

#### Scenario: Reject negative Anthropic usage

- **WHEN** start 或 delta usage 包含负 token 值
- **THEN** Provider 产生 stream protocol failure 并丢弃当前 sample staging
