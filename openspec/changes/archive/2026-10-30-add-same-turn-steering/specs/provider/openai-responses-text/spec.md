## MODIFIED Requirements

### Requirement: Compile a text-only Responses request

系统 SHALL向解析后的`responses` endpoint发送OpenAI Responses请求。请求 MUST包含model、可选临时项目指令context item、完整native input、`stream=true`、`store=false`以及当前Tool Catalog编译出的稳定tools数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

项目指令context item SHALL位于native history和真实当前输入之前，保持独立且不进入native history。Read、Glob、Grep、Write与`exec_command` facade SHALL编译为strict function tools；`apply_patch` SHALL编译为唯一custom freeform tool，名称、description、wire kind和定义来自不可变catalog snapshot。请求 MUST NOT使用`previous_response_id`或声明后台进程、write_stdin、其他未实现custom/parallel tools，并继续请求保留encrypted reasoning。Authorization secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first turn
- **WHEN** 空会话收到用户输入且catalog包含当前OpenAI facades
- **THEN** 请求input包含真实用户item，tools包含稳定排序的Read、Glob、Grep、Write、exec_command strict functions及一个custom apply_patch，并启用stream、禁用store

#### Scenario: Compile the first turn without project instructions
- **WHEN** 项目指令为空
- **THEN** input不生成空context item，当前tools仍按稳定定义声明

#### Scenario: Compile a later turn from native history
- **WHEN** native history包含function/custom calls及匹配outputs
- **THEN** 下一请求按原顺序回放这些items并直接继续sampling，不从RuntimeEvent、扁平Message或合成user item重建

#### Scenario: Deterministic request compilation
- **WHEN** model、catalog、项目指令、native history和当前输入相同
- **THEN** canonical bytes和fingerprint完全相同，cwd、mtime、Session、invocation和approval identity不参与

#### Scenario: Keep Write catalog bytes stable
- **WHEN** 相同 catalog 下改变 workspace 内容、Read observation 或 Write grant
- **THEN** tools canonical bytes 与 tool catalog fingerprint 保持一致

每条追加输入 SHALL 保持独立用户消息，InputID/turn identity 不得进入 wire 或内容 fingerprint。 每条为独立 user input_text item，位于全部匹配 function/custom outputs 之后，InputID 不得冒充 Provider item ID。

#### Scenario: Compile ordered steering inputs
- **WHEN** 整组工具 outputs 后提交两条自动输入再请求下一 sample
- **THEN** 两条用户消息按 admission 顺序各出现一次，calls/outputs 配对和 opaque reasoning 保持原样；恢复与不中断请求 canonical bytes/fingerprint 相同

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
