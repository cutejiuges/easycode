## MODIFIED Requirements

### Requirement: Compile a text-only Responses request

系统 SHALL向解析后的`responses` endpoint发送OpenAI Responses请求。请求 MUST包含model、可选临时项目指令context item、完整native input、`stream=true`、`store=false`以及当前Tool Catalog编译出的稳定tools数组。首个sample或新用户turn必须追加真实当前用户input item；tool outputs后的continuation sample不得追加合成user item或空input。

项目指令context item SHALL位于native history和真实当前输入之前，保持独立且不进入native history。Read、Glob、Grep与`exec_command` facade SHALL编译为strict function tools；`apply_patch` SHALL编译为唯一custom freeform tool，名称、description、wire kind和定义来自不可变catalog snapshot。请求 MUST NOT使用`previous_response_id`或声明Write、Bash、后台进程、write_stdin、其他未实现custom/parallel tools，并继续请求保留encrypted reasoning。Authorization secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

#### Scenario: Compile the first turn
- **WHEN** 空会话收到用户输入且catalog包含当前OpenAI facades
- **THEN** 请求input包含真实用户item，tools包含稳定排序的Read、Glob、Grep、exec_command strict functions及一个custom apply_patch，并启用stream、禁用store

#### Scenario: Compile the first turn without project instructions
- **WHEN** 项目指令为空
- **THEN** input不生成空context item，当前tools仍按稳定定义声明

#### Scenario: Compile a later turn from native history
- **WHEN** native history包含function/custom calls及匹配outputs
- **THEN** 下一请求按原顺序回放这些items并直接继续sampling，不从RuntimeEvent、扁平Message或合成user item重建

#### Scenario: Deterministic request compilation
- **WHEN** model、catalog、项目指令、native history和当前输入相同
- **THEN** canonical bytes和fingerprint完全相同，cwd、mtime、Session、invocation和approval identity不参与

## ADDED Requirements

### Requirement: OpenAI exec_command calls preserve native function semantics

OpenAI reducer SHALL从原生function-call item及arguments delta/done归并`exec_command`，仅在item完整、call ID唯一且完整arguments通过当前catalog绑定的Exec strict decoder后产生typed ready call。partial、重复call ID、未知field、超限command或facade/wire不匹配 MUST使sample失败且不得产生approval或进程I/O。

完成exec_command item及其与message、reasoning和encrypted content的顺序 MUST无损保存在OpenAI native history。每个durable Exec result SHALL编码为匹配原call ID的`function_call_output`，内容使用冻结模型preview并准确表达成功、非零退出、拒绝、取消、timeout或`outcome_uncertain`；approval与progress不得成为native item。

#### Scenario: Complete a streamed exec_command call
- **WHEN** function arguments跨多个delta到达并以合法command完成
- **THEN** reducer保留原生function call并产生一个匹配`process.exec`的typed ready call

#### Scenario: Restore an exec_command call and output
- **WHEN** Session恢复包含exec_command call与匹配function output的native envelope
- **THEN** 下一Responses请求逐字节等价回放原生顺序且不重新执行命令
