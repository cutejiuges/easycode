## MODIFIED Requirements

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
