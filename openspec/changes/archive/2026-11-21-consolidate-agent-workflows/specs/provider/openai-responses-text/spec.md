## MODIFIED Requirements

### Requirement: Reduce supported Responses stream events

系统 SHALL以单个Responses sample状态机归并事件：有效`response.created`先建立唯一response ID；text delta继续投影文本；完成的output items按wire顺序保存；只有identity匹配的首次`response.completed`为成功terminal。

function call SHALL从原生function-call item及arguments delta/done事件归并；custom apply_patch SHALL从`custom_tool_call` item及`response.custom_tool_call_input.delta/done`归并。两类调用均须保留非空call ID、名称、wire kind和完整input。只有item完成、input完整且通过匹配facade strict decoder后才能产生typed ready call；partial input或patch draft不得产生approval、executor I/O或durable ready fact。重复call ID、未知tool、wire kind与facade不匹配、冲突item identity、非法/超限input、重复terminal、terminal后事件或identity冲突 MUST作为stream protocol failure。

Write SHALL 严格归并完整 structured 参数，未知/重复字段、null、非法编码、尾随内容和超限输入按协议错误拒绝；不得根据 delta 预读文件或产生审批。

普通 HTTP SSE 在接纳成功终态之前 SHALL 核对本请求 transport 已接收的完整帧。终态之后已接收的 Responses 数据帧 MUST 被交给当前归并规则判为协议错误，不能当成可忽略的 cleanup 数据。已接收范围 SHALL 有可确定的有限边界，合法终态的收口无需等待远端 EOF。WebSocket 继续沿用 response identity 和连接生命周期契约，已发送的生成请求不因失败向 HTTP 重发。

普通 message SHALL 与其他已知输出项共同校验非空的已提供 item ID、已提供的 output index 和完成身份。已观察到的开始项必须在成功终态前唯一完成。合法的只有完成项的消息可归并，现行 wire 允许缺少相应 ID 或 index 的情形仍受支持。冲突或重复完成 MUST 使本次 sample 失败。原生样本保存与恢复 SHALL 拒绝同一成功样本中的冲突已知 item ID，保持合法输出的原生 wire 次序和 opaque 内容。

Responses HTTP 和 WebSocket SHALL 按安全解码后的 `type:"error"` 识别 Provider 失败，支持它在 created 前到达。SSE 是否附带事件名不得改变该结果。任何已收到的 Provider 错误不产生 prepared success，独立摘要沿用其失败边界，已发送生成请求不得重试到另一传输，诊断不暴露原始错误字段。

#### Scenario: Reject contradictory or unfinished known messages

- **WHEN** 普通消息身份与已有已知项冲突、同一消息再次完成，或成功终态到达时仍有未完成的普通消息
- **THEN** 样本仅以协议失败收口，未提交 usage 或原生输出
- **THEN** 合法的只完成项及其 wire 顺序保持

#### Scenario: End a Responses stream on a typed provider error

- **WHEN** HTTP data-only、带显式事件名的 SSE 或 WebSocket 收到 `type:"error"`
- **THEN** 产生稳定的失败，当前生成不落成功样本，后续未提交帧不得恢复成功

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

Responses reasoning SHALL 按 provider/reasoning-display 的默认增量、段完成快照与完整 item 接受矩阵处理，不协商 sequential_cutoff。summary parts 在 Provider 内独立关联并仅发布有序前缀，完成快照与已知文字一致，raw reasoning 不展示。合法 item-only 路径在私有 reducer 中归并 native item，同时产生有界且有序的 Started/Completed 序列。相同重复段完成幂等，已关闭已知 item 的迟到 text.done/part.done 在 Provider 边界丢弃；未知引用、错误 index、冲突文字或关闭后新 delta 失败。这些已知语义不适用“未知且格式正确事件忽略”的分支，内部非法事件不得被忽略。

#### Scenario: Assemble nonsequential summary parts before publication
- **WHEN** 后段先有文字，前段随后结束或由完整 item 确认为空
- **THEN** 共享事件按真实段序发布确定前缀，最终展示与 native 投影一致，native items 仍保持 wire 完成顺序

#### Scenario: Distinguish completion-only and stale completion
- **WHEN** 完整 reasoning item 首次提供 summary，随后该已关闭项的迟到 done 到达
- **THEN** 前者只形成一份正文，后者不产生内部事件、不改变 native、不挂接其他活动 item

#### Scenario: Reject a terminal followed by an already received Responses frame
- **WHEN** 同一 HTTP SSE 请求的匹配 completed 后另一个 Responses 数据帧已进入 transport 接收边界
- **THEN** 唯一终态为 stream protocol failure，既有 durable 历史不被改写，当前 sample 不产生 prepared success

#### Scenario: Close a completed HTTP stream without waiting for EOF
- **WHEN** 服务端发送合法 completed 并保持连接打开，收口时没有已接收尾帧
- **THEN** 系统收口传输并发布一次合法终态，归并无需等待该连接被服务端关闭
