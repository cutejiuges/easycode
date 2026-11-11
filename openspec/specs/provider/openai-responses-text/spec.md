# provider/openai-responses-text Specification

## Purpose

定义 OpenAI Responses 在首个文本对话切片中的请求、流式归并和原生历史契约，使多轮续写不依赖 UI 事件重建并保留后续扩展所需的 provider 原生数据。

## Requirements

### Requirement: Compile a text-only Responses request

系统 SHALL向解析后的`responses` endpoint发送OpenAI Responses请求。请求 MUST包含model、可选临时项目指令context item、完整native input、`stream=true`、`store=false`以及当前Tool Catalog编译出的稳定tools数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

项目指令context item SHALL位于native history和真实当前输入之前，保持独立且不进入native history。Read、Glob、Grep、Write、ReadArtifact 与 `exec_command` facade SHALL编译为strict function tools；`apply_patch` SHALL编译为唯一custom freeform tool，名称、description、wire kind和定义来自不可变catalog snapshot。完整逻辑请求 MUST NOT 使用 previous_response_id；只有满足 provider/openai-websocket 契约的连接内 wire 派生可携带该字段及增量 input。HTTP/SSE 保持 stream=true，WebSocket create 省略 transport 专属 stream 字段，两者均保持 store=false。启用 thread 缓存策略时 SHALL 添加稳定 prompt_cache_key，omit 时完全省略。请求不得声明后台进程、write_stdin、其他未实现 custom/parallel tools，并继续请求保留 encrypted reasoning。普通请求 SHALL 按 provider/reasoning-control 编译显式 reasoning.effort、reasoning.summary 及输出上限；HTTP 与 WebSocket 派生 MUST 保留相同逻辑设置，省略新字段时维持原请求 bytes。Authorization secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first turn
- **WHEN** 空会话收到用户输入且catalog包含当前OpenAI facades
- **THEN** 请求input包含真实用户item，tools包含稳定排序的Read、Glob、Grep、Write、ReadArtifact、exec_command strict functions及一个custom apply_patch，并启用stream、禁用store

#### Scenario: Compile the first turn without project instructions
- **WHEN** 项目指令为空
- **THEN** input不生成空context item，当前tools仍按稳定定义声明

#### Scenario: Compile a later turn from native history
- **WHEN** native history包含function/custom calls及匹配outputs
- **THEN** 下一请求按原顺序回放这些items并直接继续sampling，不从RuntimeEvent、扁平Message或合成user item重建

#### Scenario: Deterministic request compilation
- **WHEN** model、catalog、项目指令、native history、冻结推理/输出设置、缓存策略和绑定 ThreadID 相同
- **THEN** 完整 canonical bytes 和 fingerprint 完全相同，cwd、mtime、invocation 和 approval identity 不参与；ThreadID 仅在显式 thread key 模式影响 key 字段，不改变内容分段 fingerprint

#### Scenario: Keep Write catalog bytes stable
- **WHEN** 相同 catalog 下改变 workspace 内容、Read observation 或 Write grant
- **THEN** tools canonical bytes 与 tool catalog fingerprint 保持一致

每条追加输入 SHALL 保持独立用户消息，InputID/turn identity 不得进入 wire 或内容 fingerprint。 每条为独立 user input_text item，位于全部匹配 function/custom outputs 之后，InputID 不得冒充 Provider item ID。

#### Scenario: Compile ordered steering inputs
- **WHEN** 整组工具 outputs 后提交两条自动输入再请求下一 sample
- **THEN** 两条用户消息按 admission 顺序各出现一次，calls/outputs 配对和 opaque reasoning 保持原样；恢复与不中断请求 canonical bytes/fingerprint 相同

#### Scenario: Omit optional cache and transport fields by default
- **WHEN** 采用默认 omit/http_sse 配置且所有新增推理/输出字段省略
- **THEN** 既有 HTTP 请求 bytes 不变，无 prompt_cache_key 或 previous_response_id

#### Scenario: Request reasoning without changing native inputs
- **WHEN** 显式 effort high 与 summary auto 配置合法
- **THEN** 顶层 reasoning 精确包含这两个字段，input、tools 和 include 不增加合成内容，store=false

### Requirement: Reduce supported Responses stream events

系统 SHALL以单个Responses sample状态机归并事件：有效`response.created`先建立唯一response ID；text delta继续投影文本；完成的output items按wire顺序保存；只有identity匹配的首次`response.completed`为成功terminal。

function call SHALL从原生function-call item及arguments delta/done事件归并；custom apply_patch SHALL从`custom_tool_call` item及`response.custom_tool_call_input.delta/done`归并。两类调用均须保留非空call ID、名称、wire kind和完整input。只有item完成、input完整且通过匹配facade strict decoder后才能产生typed ready call；partial input或patch draft不得产生approval、executor I/O或durable ready fact。重复call ID、未知tool、wire kind与facade不匹配、冲突item identity、非法/超限input、重复terminal、terminal后事件或identity冲突 MUST作为stream protocol failure。

Write SHALL 严格归并完整 structured 参数，未知/重复字段、null、非法编码、尾随内容和超限输入按协议错误拒绝；不得根据 delta 预读文件或产生审批。

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

#### Scenario: Complete a structured Write
- **WHEN** Write 参数跨随机 chunk、半包与 UTF-8 边界到达，并以合法原生 terminal 完成
- **THEN** 保留原始调用身份/参数，产生一个匹配 fs.write 的 ready call

#### Scenario: Interrupt partial Write input
- **WHEN** content 未完成时 stream 被取消或断开
- **THEN** staging 丢弃，没有 ready、approval、文件读取或写入

Responses reasoning SHALL 按 provider/reasoning-display 的默认增量、段完成快照与完整 item 接受矩阵处理，不协商 sequential_cutoff。summary parts 在 Provider 内独立关联并仅发布有序前缀，完成快照与已知文字一致，raw reasoning 不展示。合法 item-only 路径在 native item 前产生有界 Started/Completed 序列。相同重复段完成幂等，已关闭已知 item 的迟到 text.done/part.done 在 Provider 边界丢弃；未知引用、错误 index、冲突文字或关闭后新 delta 失败。这些已知语义不适用“未知且格式正确事件忽略”的分支，内部非法事件不得被忽略。

#### Scenario: Assemble nonsequential summary parts before publication
- **WHEN** 后段先有文字，前段随后结束或由完整 item 确认为空
- **THEN** 共享事件按真实段序发布确定前缀，最终展示与 native 投影一致，native items 仍保持 wire 完成顺序

#### Scenario: Distinguish completion-only and stale completion
- **WHEN** 完整 reasoning item 首次提供 summary，随后该已关闭项的迟到 done 到达
- **THEN** 前者只形成一份正文，后者不产生内部事件、不改变 native、不挂接其他活动 item

### Requirement: Native history commits transactionally

系统 SHALL 保持原生 message/item 边界与字段。用户输入独立 prepare、Sync、finalize 为 user_inputs。sample staging 仅保存完成模型输出、metadata、raw usage 和 ready calls，合法 Provider terminal 后形成 prepared sample；失败、取消、timeout、协议错误或 EOF 丢弃本 sample staging，但不得删除先前已提交的用户输入。

Read、Glob、Grep、Write results SHALL编码为按call index排列的`function_call_output`；Patch result SHALL编码为匹配原custom call的`custom_tool_call_output`。每项必须匹配前一sample尚未闭合的call ID和wire kind并包含冻结模型preview。approval request/decision不得成为OpenAI native item；只有最终tool result进入Provider history。tool-output entry只有在Session commit durable后才能进入Conversation history。

Write 的完整调用参数与冻结结果 SHALL 无损保存和恢复；Read observation/approval 展示不是 native item，不得影响已提交 native history。

#### Scenario: Commit a successful turn
- **WHEN** response完成function calls与一个custom apply_patch call
- **THEN** 全部 output items 和 usage 按 wire 顺序进入 sample native commit，用户 item 已独立提交且不重复保存

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

#### Scenario: Restore Write native history without reexecution
- **WHEN** 恢复含 Write 调用及冻结 function_call_output 的原生历史
- **THEN** 下一请求 canonical bytes、native item 顺序与 fingerprint 和不中断路径一致
- **THEN** opaque reasoning/signature/phase 保持既有无损契约，workspace I/O 次数为零

### Requirement: Advertised capabilities match implemented behavior

OpenAI Provider SHALL只声明已经完整实现并通过测试的能力。Read/Glob/Grep/Write function tools以及custom apply_patch的request编译、stream归并、native persistence/restore和result codec全部存在后，function tools和custom tools SHALL准确报告可用；其他 custom tools、parallel tool calls 和 raw reasoning 的未实现能力 MUST 保持 unsupported。显式 effort/summary 只有在 provider/reasoning-control 的完整实现及验收成立后才能启用。PromptCacheKey 只有在 key 编译、原生流归并、持久化/恢复和 golden 全部验证后且 thread 策略开启时才能声明；PreviousResponse 只有在 WebSocket 全链路及故障/恢复验证完成、websocket_auto 开启且未发生会话级 HTTP 回退时才能声明。禁用策略或未实现能力不得用空策略置为 true。

Write SHALL 只有在 request、stream、native persistence/restore、result codec 与 golden 全部存在后可用；不得借本变更声明额外 apply_patch 操作、命令或未完成能力。

#### Scenario: Inspect capabilities after initialization
- **WHEN** Runtime查询OpenAI capabilities
- **THEN** streaming、原生保留、function tools和custom apply_patch准确报告可用
- **THEN** parallel tools及其他延期能力保持false或unsupported

#### Scenario: Distinguish configured transport from a fallback
- **WHEN** websocket_auto 会话在发送生成请求前安全回退到 HTTP
- **THEN** 有效 PreviousResponse 为 false，缓存 key 的有效能力仍按独立配置判断，快照解释回退

EncryptedReasoning SHALL 只声明已实现的原生保留；ReasoningSummary 明确表示本实例主动编译 summary 请求的已完成路径，仅在显式配置 summary 且 request/stream/native persistence/restore/golden 全部实现后为 true，否则为 false；这不是任意 endpoint 的支持保证。RawReasoning 仍为 false。被动接收并展示允许 summary 不等于主动请求或服务端必支持，不新增空能力位、占位策略或按模型名称推断。

#### Scenario: Read a returned summary without advertising request support
- **WHEN** 未显式配置 summary，但服务端自行返回 summary_text 与 encrypted_content
- **THEN** 只展示允许文字，ReasoningSummary/RawReasoning 保持 false，原请求、include、缓存及 transport 字段不变

#### Scenario: Advertise an explicitly configured summary path
- **WHEN** OpenAI summary auto 已配置且完整编译、流、恢复和 golden 验收通过
- **THEN** ReasoningSummary 为 true，显示开关或正文缺失不改变它；RawReasoning 为 false

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

### Requirement: OpenAI exec_command calls preserve native function semantics

OpenAI reducer SHALL从原生function-call item及arguments delta/done归并`exec_command`，仅在item完整、call ID唯一且完整arguments通过当前catalog绑定的Exec strict decoder后产生typed ready call。partial、重复call ID、未知field、超限command或facade/wire不匹配 MUST使sample失败且不得产生approval或进程I/O。

完成exec_command item及其与message、reasoning和encrypted content的顺序 MUST无损保存在OpenAI native history。每个durable Exec result SHALL编码为匹配原call ID的`function_call_output`，内容使用冻结模型preview并准确表达成功、非零退出、拒绝、取消、timeout或`outcome_uncertain`；approval与progress不得成为native item。

#### Scenario: Complete a streamed exec_command call
- **WHEN** function arguments跨多个delta到达并以合法command完成
- **THEN** reducer保留原生function call并产生一个匹配`process.exec`的typed ready call

#### Scenario: Restore an exec_command call and output
- **WHEN** Session恢复包含exec_command call与匹配function output的native envelope
- **THEN** 下一Responses请求逐字节等价回放原生顺序且不重新执行命令

### Requirement: Preserve validated user text across hosts

Provider SHALL 将 TUI、一次性 headless 和 stream-json 提供的合法非空白用户正文原样放入自身原生用户消息，不得 trim 外围空白、规范化 Unicode 或重新解释文本。空白验证、输入上限及已有 wire shape SHALL 保持不变。TUI 在编辑阶段的换行规范化不扩展到其他宿主。提交与恢复 SHALL 保留相同正文及原生 item 顺序；正文变化可以改变完整请求 fingerprint，UI 状态不得污染稳定前缀。

#### Scenario: Preserve indentation and trailing whitespace
- **WHEN** 任一宿主提供含首行缩进、Tab、尾部空格和尾换行的合法用户文本
- **THEN** 原生用户消息及模型请求包含完全相同的正文
- **THEN** 不增加宿主专属 wire、兼容 reader 或协议版本

#### Scenario: Preserve restored request equivalence
- **WHEN** 已提交上述文本后从 Session 恢复并提交下一输入
- **THEN** 后续请求 canonical bytes、原生顺序和 fingerprint 与 uninterrupted 路径一致

#### Scenario: Reject blank input before network effects
- **WHEN** 输入只有空白字符
- **THEN** 原有验证仍拒绝请求，不发起网络或生成原生提交

### Requirement: OpenAI user questions preserve native function semantics

OpenAI SHALL 从当前 catalog 稳定编译 `request_user_input` strict function，归并完整 arguments 后调用提问 strict decoder，仅在原生 item 和 response.completed 全部合法后产生 `user.question` ready call。参数半包、非法 Unicode、重复键/题目 ID、未知字段、模型提供 answers 或未声明 facade MUST 使 sample 失败，零提问事件和工具执行。

完成调用与回答 SHALL 分别保存在原始 function_call 和匹配 call ID 的 function_call_output；按原 item/call 顺序编码完整冻结 preview，保持 phase、reasoning 和 encrypted content。宿主回答不伪造 user item，不启用其他 wire 或未来能力。

#### Scenario: Complete a fragmented question call
- **WHEN** 中文问题参数跨随机 chunk、半包和 UTF-8 边界完成
- **THEN** 恰好一个 typed ready call，输入完整前没有问卷事件

#### Scenario: Continue after a question result
- **WHEN** 回答结果已 durable 且所有 sibling outputs 已提交
- **THEN** 下一 sample 使用原始有序 function call/output，回答只出现一次且 opaque reasoning 不变
