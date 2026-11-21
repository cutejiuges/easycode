## MODIFIED Requirements

### Requirement: Compile a text-only Responses request

系统 SHALL向解析后的`responses` endpoint发送OpenAI Responses请求。请求 MUST包含model、可选临时项目指令context item、完整native input、`stream=true`、`store=false`以及当前操作冻结的已声明 Tool Catalog 编译出的稳定 tools 数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

项目指令context item SHALL位于native history和真实当前输入之前，保持独立且不进入native history。当前冻结目录中已声明的 Read、Glob、Grep、Write、ReadArtifact 与 `exec_command` facade SHALL 编译为 strict function tools。已声明的 `apply_patch` SHALL 编译为唯一 custom freeform tool。名称、description、wire kind 和定义均来自该不可变 catalog snapshot。完整逻辑请求 MUST NOT 使用 previous_response_id；只有满足 provider/openai-websocket 契约的连接内 wire 派生可携带该字段及增量 input。HTTP/SSE 保持 stream=true，WebSocket create 省略 transport 专属 stream 字段，两者均保持 store=false。启用 thread 缓存策略时 SHALL 添加稳定 prompt_cache_key，omit 时完全省略。请求不得声明后台进程、write_stdin、其他未实现 custom/parallel tools，并继续请求保留 encrypted reasoning。普通请求 SHALL 按 provider/reasoning-control 编译显式 reasoning.effort、reasoning.summary 及输出上限；HTTP 与 WebSocket 派生 MUST 保留相同逻辑设置，省略新字段时维持原请求 bytes。Authorization secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first turn
- **WHEN** 普通模式下的空会话收到用户输入且 catalog 包含当前 OpenAI facades
- **THEN** 请求input包含真实用户item，tools包含稳定排序的Read、Glob、Grep、Write、ReadArtifact、exec_command strict functions及一个custom apply_patch，并启用stream、禁用store

#### Scenario: Compile the first turn without project instructions
- **WHEN** 项目指令为空
- **THEN** input不生成空context item，当前tools仍按稳定定义声明

#### Scenario: Compile a later turn from native history
- **WHEN** native history包含function/custom calls及匹配outputs
- **THEN** 下一请求按原顺序回放这些items并直接继续sampling，不从RuntimeEvent、扁平Message或合成user item重建

#### Scenario: Deterministic request compilation
- **WHEN** model、catalog、规划指令、项目指令、native history、冻结推理/输出设置、缓存策略和绑定 ThreadID 相同
- **THEN** 完整 canonical bytes 和 fingerprint 完全相同，cwd、mtime、invocation 和 approval identity 不参与；ThreadID 仅在显式 thread key 模式影响 key 字段，不改变内容分段 fingerprint

#### Scenario: Keep Write catalog bytes stable
- **WHEN** 相同 catalog 下改变 workspace 内容、Read observation 或 Write grant
- **THEN** tools canonical bytes 与 tool catalog fingerprint 保持一致

每条追加输入 SHALL 保持独立用户消息，InputID/turn identity 不得进入 wire 或内容 fingerprint。 每条为独立 user input_text item，位于全部匹配 function/custom outputs 之后，InputID 不得冒充 Provider item ID。

#### Scenario: Compile ordered steering inputs
- **WHEN** 整组工具 outputs 后提交两条自动输入再请求下一 sample
- **THEN** 两条用户消息按 admission 顺序各出现一次，calls/outputs 配对和 opaque reasoning 保持原样；恢复与不中断请求 canonical bytes/fingerprint 相同

#### Scenario: Omit optional cache and transport fields by default
- **WHEN** 普通模式采用默认 omit/http_sse 配置且所有新增推理/输出字段省略
- **THEN** 既有 HTTP 请求 bytes 不变，无 instructions、prompt_cache_key 或 previous_response_id

#### Scenario: Request reasoning without changing native inputs
- **WHEN** 显式 effort high 与 summary auto 配置合法
- **THEN** 顶层 reasoning 精确包含这两个字段，input、tools 和 include 不增加合成内容，store=false

Plan Mode 的普通采样 SHALL 使用原样的本机固定规划指令作为 Responses 顶层 `instructions`，并使用同一操作冻结的 Read、Glob、Grep、ReadArtifact、Skill、`request_user_input` 和 `update_plan` 七项定义。HTTP 与 WebSocket create SHALL 使用相同的规划指令与目录快照。规划指令 SHALL 独立于项目指令，并只随本次请求发送。完整逻辑 `input` 继续按原顺序回放原生历史，保留 call/output、phase 及 opaque reasoning。普通模式 MUST 省略 `instructions` 并使用原有完整目录。独立摘要 SHALL 使用既有完整工具定义、专用摘要提示和实际请求预算，保持 `tool_choice=none`，并省略普通 Plan Mode 的 `instructions`。

规划指令与当前已声明工具定义 SHALL 参与完整逻辑请求的确定性内容与属性比较。当存活连接中上一已提交普通请求的属性与当前请求因进出 Plan Mode 而不同时，WebSocket SHALL 按 provider/openai-websocket 发送 full create，并记录 `request_properties_changed`。若中间模式选择未发出实际请求，选回原模式且实际属性、原生前缀及连接资格均相同，选择行为本身 MUST NOT 使已有增量资格失效。

#### Scenario: Compile Plan Mode for both Responses transports
- **WHEN** 同一 Plan Mode 普通采样由 HTTP 或 WebSocket 编译
- **THEN** 顶层 `instructions` 和七项已声明定义相同，完整逻辑 `input` 中原生调用、结果、phase 和 encrypted reasoning 保持
- **THEN** 规划指令没有进入项目指令 context item 或原生提交，transport 字段仍按现有契约派生

#### Scenario: Send a full create when sampled mode properties change
- **WHEN** 同一连接已提交普通模式样本，之后在 Plan Mode 发出请求并提交成功样本，随后再回到普通模式发出请求
- **THEN** 两次属性变化后的首个请求都是 full create，无 `previous_response_id`，并报告 `request_properties_changed`
- **THEN** 原生历史完整回放，thread 缓存 key 策略保持

#### Scenario: Preserve eligible continuation after an unsampled switch
- **WHEN** 已完成可续写的普通样本，用户切换进入 Plan Mode 并回到普通模式，中间未发送请求，下一请求满足原有增量资格
- **THEN** 下一请求仍使用该已提交样本的 `previous_response_id` 与正确后缀，快照不因未采样的模式选择产生重置

#### Scenario: Keep ordinary Plan instructions out of a summary
- **WHEN** Plan Mode 发出独立摘要请求，随后恢复普通采样
- **THEN** 摘要请求使用既有完整目录与摘要提示，省略 `instructions` 并禁止工具选择，摘要格式与窗口替换保持既有语义
- **THEN** 下一普通请求按摘要后的 full create 规则发送，并恢复 Plan 规划指令与七项工具定义
