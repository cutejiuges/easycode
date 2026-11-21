## MODIFIED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL向解析后的相对`messages` endpoint发送Anthropic Messages请求并保留`base_url`路径前缀。请求 SHALL使用既有鉴权/version/JSON/SSE headers；正文 MUST包含model、正整数`max_tokens`、可选临时项目指令、按原顺序排列的Anthropic native history、`stream=true`和当前操作冻结的已声明 Tool Catalog 编译出的稳定 tools 数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

存在项目指令时，其user context message SHALL位于已提交history和真实当前输入之前，并保持独立且不进入native history。当前冻结目录中已声明的 Read、Glob、Grep、Write、ReadArtifact、Skill、AskUserQuestion 与 structured `Edit`、`Bash`、`TodoWrite` facade SHALL 编译为稳定名称、description 和 strict JSON input schema。tools 顺序与 schema bytes MUST 来自本次不可变 catalog snapshot。请求不得声明 custom/freeform tool、后台进程或 write_stdin。普通请求 SHALL 按 provider/reasoning-control 编译显式冻结的 thinking（含合法 display）、output_config.effort 及输出上限；新字段省略时维持原请求 bytes。启用缓存的请求副本 MUST 保留这些字段，不能因 cache marker 编译丢失。禁用缓存时 MUST 省略 cache_control；启用时 SHALL 按 provider/prompt-caching 在最终请求副本上添加合法、确定性断点及冻结 TTL，不修改原生历史。secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first Anthropic turn
- **WHEN** 普通模式下的空会话收到用户文本且 catalog 包含当前十个 Anthropic facades
- **THEN** 请求包含用户message以及稳定排序的Read、Glob、Grep、Write、ReadArtifact、Edit、Bash、Skill、AskUserQuestion 和 TodoWrite schemas，并保持model、max_tokens和stream字段确定

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
- **THEN** messages 按项目指令、包含当前已提交用户输入的原生 messages 顺序排列，且不从共享事件重建

#### Scenario: Produce stable request bytes
- **WHEN** model、catalog、规划指令、项目指令、native history、当前输入、冻结推理/输出设置与缓存策略相同
- **THEN** canonical request bytes和fingerprint完全相同，cwd、Session identity、invocation ID和approval状态不参与

#### Scenario: Compile a continuation after tool results
- **WHEN** 原生 history 已提交 assistant tool_use 及匹配的 user tool_result 且没有新增 user_inputs
- **THEN** 下一请求按原顺序回放call/output并直接继续sampling，不再追加新的user message

#### Scenario: Keep Write catalog bytes stable
- **WHEN** 相同 catalog 下改变 workspace 内容、Read observation 或 Write grant
- **THEN** tools canonical bytes 与 tool catalog fingerprint 保持一致

每条追加输入 SHALL 保持独立用户消息，InputID/turn identity 不得进入 wire 或内容 fingerprint。 tool_result user message 与后续用户文本 message 保持独立，不改写已提交 outputs 或自动合并消息。

#### Scenario: Compile ordered steering inputs
- **WHEN** 整组工具 outputs 后提交两条自动输入再请求下一 sample
- **THEN** 两条用户消息按 admission 顺序各出现一次，calls/outputs 配对和 opaque reasoning 保持原样；恢复与不中断请求 canonical bytes/fingerprint 相同

#### Scenario: Keep markers outside native persistence
- **WHEN** 使用同一原生历史启用 ephemeral_1h 并重复编译
- **THEN** 请求断点及 TTL 字节确定，原生 commit bytes 不变，thinking/opaque 内容和工具配对保持

#### Scenario: Preserve adaptive controls with cache markers
- **WHEN** 显式 adaptive/high 与任一已支持 TTL 一同启用
- **THEN** 普通 Messages 同时包含合法推理字段和确定性 marker；工具配对、原生 signature/redacted 内容不变

TodoWrite SHALL 使用 tool/task-progress 定义的唯一 structured 输入 schema。当前清单、任务来源、完成数、说明、折叠状态与查询 identity MUST NOT 写入稳定工具 schema/description 或 provider profile；改变任务只影响已提交原生历史及获批的 checkpoint 动态摘要包装。

#### Scenario: Keep the catalog stable across task updates
- **WHEN** 两次请求之间只成功更新、回退或清除任务，或改变任务面板折叠状态
- **THEN** tools canonical bytes 与稳定 catalog fingerprint 不变；请求历史只按真实已提交调用/结果或 checkpoint 变化

Plan Mode 的普通采样 SHALL 将本机固定的规划指令原样编译为 Messages 顶层 `system` 文本，并使用同一操作冻结的 Read、Glob、Grep、ReadArtifact、Skill、AskUserQuestion 和 TodoWrite 七项定义。缓存关闭及两种 TTL 的请求副本 SHALL 保留相同的规划指令与定义顺序。规划指令 SHALL 独立于项目指令，并只随本次请求发送。既有原生用户消息、助手消息及工具配对继续按原顺序回放。普通模式 MUST 省略 `system` 并使用原有完整目录。独立摘要 SHALL 使用既有完整工具定义、专用摘要提示和实际请求预算，保持 `tool_choice.type=none`，并省略普通 Plan Mode 的 `system`。

#### Scenario: Compile a Plan Mode request under each cache policy
- **WHEN** 同一 Plan Mode 普通采样分别按关闭缓存、5m 或 1h 的策略编译
- **THEN** 三份请求各自得到确定的 canonical bytes，顶层 `system` 与七项工具定义一致，缓存断点和 TTL 按既有规则应用
- **THEN** `messages` 保持包括原生命令调用及结果在内的已提交顺序，规划指令没有进入项目指令消息或原生提交

#### Scenario: Separate summary generation and the next ordinary sample
- **WHEN** Plan Mode 在同一会话触发摘要生成，之后继续普通采样
- **THEN** 摘要请求使用既有完整目录与摘要提示，省略规划指令并禁止工具选择，摘要生成与 checkpoint 采用既有格式
- **THEN** 后续普通请求仍使用对应操作冻结的规划指令与七项工具定义

#### Scenario: Preserve the default request encoding
- **WHEN** 固定同一原生历史、模型、缓存策略和其他实际输入，编译普通模式请求
- **THEN** 省略 `system`，保留完整目录和既有 canonical bytes
