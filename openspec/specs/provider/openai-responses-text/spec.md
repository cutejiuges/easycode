# provider/openai-responses-text Specification

## Purpose

定义 OpenAI Responses 在首个文本对话切片中的请求、流式归并和原生历史契约，使多轮续写不依赖 UI 事件重建并保留后续扩展所需的 provider 原生数据。

## Requirements

### Requirement: Compile a text-only Responses request

系统 SHALL向解析后的`responses` endpoint发送OpenAI Responses请求。请求 MUST包含model、可选临时项目指令context item、完整native input、`stream=true`、`store=false`以及当前Tool Catalog编译出的稳定tools数组。首个sample或新用户turn必须追加真实当前用户input item；tool outputs后的continuation sample不得追加合成user item或空input。

项目指令context item SHALL位于native history和真实当前输入之前，保持独立且不进入native history。Read、Glob、Grep facade SHALL编译为strict function tools；`apply_patch` SHALL编译为唯一custom freeform tool，名称、description、wire kind和定义来自不可变catalog snapshot。请求 MUST NOT使用`previous_response_id`或声明Write、Bash、未实现custom/parallel tools，并继续请求保留encrypted reasoning。Authorization secret、项目指令正文、workspace绝对路径、candidate fingerprint和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

#### Scenario: Compile the first turn
- **WHEN** 空会话收到用户输入且catalog包含当前OpenAI facades
- **THEN** 请求input包含真实用户item，tools包含稳定排序的Read、Glob、Grep strict functions及一个custom apply_patch，并启用stream、禁用store

#### Scenario: Compile the first turn without project instructions
- **WHEN** 项目指令为空
- **THEN** input不生成空context item，当前tools仍按稳定定义声明

#### Scenario: Compile a later turn from native history
- **WHEN** native history包含function/custom calls及匹配outputs
- **THEN** 下一请求按原顺序回放这些items并直接继续sampling，不从RuntimeEvent、扁平Message或合成user item重建

#### Scenario: Deterministic request compilation
- **WHEN** model、catalog、项目指令、native history和当前输入相同
- **THEN** canonical bytes和fingerprint完全相同，cwd、mtime、Session、invocation和approval identity不参与

### Requirement: Reduce supported Responses stream events

系统 SHALL以单个Responses sample状态机归并事件：有效`response.created`先建立唯一response ID；text delta继续投影文本；完成的output items按wire顺序保存；只有identity匹配的首次`response.completed`为成功terminal。

function call SHALL从原生function-call item及arguments delta/done事件归并；custom apply_patch SHALL从`custom_tool_call` item及`response.custom_tool_call_input.delta/done`归并。两类调用均须保留非空call ID、名称、wire kind和完整input。只有item完成、input完整且通过匹配facade strict decoder后才能产生typed ready call；partial input或patch draft不得产生approval、executor I/O或durable ready fact。重复call ID、未知tool、wire kind与facade不匹配、冲突item identity、非法/超限input、重复terminal、terminal后事件或identity冲突 MUST作为stream protocol failure。

#### Scenario: Establish one response identity
- **WHEN** 服务端首先发送非空ID的`response.created`
- **THEN** reducer固定唯一sample identity并拒绝第二个created

#### Scenario: Stream assistant text
- **WHEN** 活动response发送多个output text deltas
- **THEN** 系统按接收顺序投影文本

#### Scenario: Preserve completed native items
- **WHEN** response完成message、reasoning、function-call或custom-tool-call item
- **THEN** 已知字段和受控opaque扩展按wire顺序保留在OpenAI包内

#### Scenario: Complete only on response.completed
- **WHEN** 首个identity匹配且全部function/custom inputs完整的`response.completed`到达
- **THEN** Provider产生且只产生一次completed terminal

#### Scenario: Reject a conflicting completion identity
- **WHEN** completed/failed/incomplete identity缺失或与created冲突
- **THEN** Provider产生stream protocol failure且不提交staging

#### Scenario: Reject illegal event order
- **WHEN** 受支持事件在created前或任一terminal后到达
- **THEN** reducer返回stream protocol failure且不产生第二terminal

#### Scenario: Surface failed and incomplete responses
- **WHEN** identity匹配的`response.failed`或`response.incomplete`到达
- **THEN** Provider产生唯一失败terminal且错误不含请求正文或敏感header

#### Scenario: Ignore unknown well-formed event
- **WHEN** 活动response发送当前切片未知但格式正确的event
- **THEN** Provider不panic、不伪造call或完成事件并继续等待terminal

#### Scenario: Complete an apply_patch custom call
- **WHEN** custom tool input跨多个delta到达并以完整合法单文件Update File patch完成
- **THEN** custom item按wire顺序保留且prepared sample产生匹配typed Patch ready call

#### Scenario: Complete a Read function call
- **WHEN** function call arguments跨多个delta到达并以完整合法Read输入完成
- **THEN** output item按wire顺序保留且prepared sample产生匹配typed Read ready call

#### Scenario: Reject partial tool input
- **WHEN** `response.completed`到达时function arguments或custom input仍不完整或schema非法
- **THEN** Provider以协议错误失败且不提交native sample或ready call

#### Scenario: Reject partial function arguments
- **WHEN** `response.completed`到达时function arguments仍不完整或schema非法
- **THEN** Provider以协议错误失败且不提交native sample或ready call

### Requirement: Native history commits transactionally

系统 SHALL将可选当前用户item、本sample完成output items、raw usage和ready calls暂存在sample staging，仅在合法`response.completed`后形成prepared sample。首个sample必须暂存真实user item；tool outputs后的continuation sample不得暂存或提交新的user item。失败、取消、timeout或EOF不得把部分item提交为history。

Read、Glob、Grep results SHALL编码为按call index排列的`function_call_output`；Patch result SHALL编码为匹配原custom call的`custom_tool_call_output`。每项必须匹配前一sample尚未闭合的call ID和wire kind并包含冻结模型preview。approval request/decision不得成为OpenAI native item；只有最终tool result进入Provider history。tool-output entry只有在Session commit durable后才能进入Conversation history。

#### Scenario: Commit a successful turn
- **WHEN** response完成function calls与一个custom apply_patch call
- **THEN** 用户item、全部output items和usage按wire顺序进入sample native commit

#### Scenario: Discard a partial failed turn
- **WHEN** 已收到文本、tool input delta或output item后sample失败
- **THEN** 当前sample staging不进入history且下一请求不包含部分输出

#### Scenario: Preserve reasoning data without rendering it
- **WHEN** completed原生items包含reasoning summary或encrypted content
- **THEN** Provider无损保留opaque reasoning且TUI展示选择不改变后续请求

#### Scenario: Commit matching heterogeneous outputs
- **WHEN** Read与Patch results已durable
- **THEN** result codec按call index生成匹配call ID和wire kind的function/custom outputs并在durable finalizer后进入history

#### Scenario: Commit matching function outputs
- **WHEN** Read、Glob或Grep results已durable
- **THEN** result codec生成匹配call IDs的function outputs并在durable finalizer后进入history

#### Scenario: Commit a continuation without a synthetic user item
- **WHEN** tool-output entry已提交且下一Responses sample完成最终output items
- **THEN** sample entry只提交原生outputs和usage，不复制tool outputs或构造空user item

#### Scenario: Reject mismatched outputs
- **WHEN** output call ID缺失、重复、wire kind错误或与未闭合calls不匹配
- **THEN** tool-output entry构造或恢复失败且history不被部分修改

#### Scenario: Reject mismatched function outputs
- **WHEN** function output call ID缺失、重复或与未闭合function calls不匹配
- **THEN** tool-output entry构造或恢复失败且history不被部分修改

### Requirement: Advertised capabilities match implemented behavior

OpenAI Provider SHALL只声明已经完整实现并通过测试的能力。Read/Glob/Grep function tools以及custom apply_patch的request编译、stream归并、native persistence/restore和result codec全部存在后，function tools和custom tools SHALL准确报告可用；其他custom tools、parallel tool calls、prompt cache key、`previous_response_id`和未实现reasoning展示 MUST保持unsupported。

#### Scenario: Inspect capabilities after initialization
- **WHEN** Runtime查询OpenAI capabilities
- **THEN** streaming、原生保留、function tools和custom apply_patch准确报告可用
- **THEN** parallel tools及其他延期能力保持false或unsupported

### Requirement: Responses completion preserves and normalizes usage

OpenAI reducer SHALL 只从与活动 response ID 一致的首个合法 `response.completed.response.usage` 取得最终 sample usage，并将受支持的 raw usage 字段保存在 Provider-native commit 中。缺失整个 usage 或缺失可表达字段 MUST 保持 unknown，不得以默认零补齐；明确提供的零 SHALL 保存为已知零。

已提供的 token 计数 MUST 为非负整数，cached input MUST NOT 大于总 input，details 子字段与总量之间的关系必须通过语义校验。损坏结构、负值、不可能的计数关系或溢出 MUST 将 completed event 转为 stream protocol failure，并丢弃 sample staging。合法 completed response 产生的 prepared sample SHALL 同时携带保存 raw usage 的 native commit 和对应 normalized usage。

#### Scenario: Normalize a complete Responses usage object

- **WHEN** `response.completed` 报告 input、cached input、cache write、output 和 reasoning output
- **THEN** Provider 保留 raw usage，并产生通过统一映射得到的五项 normalized usage

#### Scenario: Complete with missing Responses usage

- **WHEN** 兼容服务返回合法 `response.completed` 但省略整个 usage
- **THEN** sample 仍可完成，raw usage 和所有可表达 normalized 指标保持 unknown
- **THEN** 系统不生成全零 usage

#### Scenario: Preserve explicit Responses zeros

- **WHEN** Responses usage 明确报告任一 token 字段为零且字段关系合法
- **THEN** 对应 raw 与 normalized 指标保存为已知零

#### Scenario: Reject cached input above total input

- **WHEN** Responses usage 报告 cached input 大于总 input
- **THEN** Provider 产生 stream protocol failure，不生成成功 prepared sample

#### Scenario: Bind usage to the active response

- **WHEN** completion response ID 与 `response.created` 建立的 ID 不同
- **THEN** Provider 拒绝该 completion 及其 usage，不提交任何当前 sample 事实
