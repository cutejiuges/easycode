## MODIFIED Requirements

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

### Requirement: Advertised capabilities match implemented behavior

OpenAI Provider SHALL只声明已经完整实现并通过测试的能力。Read/Glob/Grep/Write function tools以及custom apply_patch的request编译、stream归并、native persistence/restore和result codec全部存在后，function tools和custom tools SHALL准确报告可用；其他 custom tools、parallel tool calls 和主动请求 reasoning/summary 的未实现能力 MUST 保持 unsupported。PromptCacheKey 只有在 key 编译、原生流归并、持久化/恢复和 golden 全部验证后且 thread 策略开启时才能声明；PreviousResponse 只有在 WebSocket 全链路及故障/恢复验证完成、websocket_auto 开启且未发生会话级 HTTP 回退时才能声明。禁用策略或未实现能力不得用空策略置为 true。

Write SHALL 只有在 request、stream、native persistence/restore、result codec 与 golden 全部存在后可用；不得借本变更声明额外 apply_patch 操作、命令或未完成能力。

#### Scenario: Inspect capabilities after initialization
- **WHEN** Runtime查询OpenAI capabilities
- **THEN** streaming、原生保留、function tools和custom apply_patch准确报告可用
- **THEN** parallel tools及其他延期能力保持false或unsupported

#### Scenario: Distinguish configured transport from a fallback
- **WHEN** websocket_auto 会话在发送生成请求前安全回退到 HTTP
- **THEN** 有效 PreviousResponse 为 false，缓存 key 的有效能力仍按独立配置判断，快照解释回退

EncryptedReasoning SHALL 只声明已实现的原生保留；ReasoningSummary 明确表示主动编译 summary 请求的能力，本切片未新增该请求策略，仍为 false；RawReasoning 仍为 false。被动接收并展示允许 summary 不等于主动请求或服务端必支持，不新增空能力位、占位策略或按模型名称推断。

#### Scenario: Read a returned summary without advertising request support
- **WHEN** 服务端自行返回 summary_text 与 encrypted_content
- **THEN** 只展示允许文字，ReasoningSummary/RawReasoning 保持 false，原请求、include、缓存及 transport 字段不变
