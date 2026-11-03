## MODIFIED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL向解析后的相对`messages` endpoint发送Anthropic Messages请求并保留`base_url`路径前缀。请求 SHALL使用既有鉴权/version/JSON/SSE headers；正文 MUST包含model、正整数`max_tokens`、可选临时项目指令、按原顺序排列的Anthropic native history、`stream=true`和当前Tool Catalog编译出的稳定tools数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

存在项目指令时，其user context message SHALL位于已提交history和真实当前输入之前，并保持独立且不进入native history。Read、Glob、Grep、Write、ReadArtifact facade与structured `Edit`/`Bash` facade SHALL编译为稳定名称、description和strict JSON input schema；tools顺序与schema bytes MUST来自本次不可变catalog snapshot。请求不得声明 custom/freeform tool、后台进程、write_stdin、system 或 thinking 配置。禁用缓存时 MUST 省略 cache_control；启用时 SHALL 按 provider/prompt-caching 在最终请求副本上添加合法、确定性断点及冻结 TTL，不修改原生历史。secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first Anthropic turn
- **WHEN** 空会话收到用户文本且catalog包含当前七个 Anthropic facades
- **THEN** 请求包含用户message以及稳定排序的Read、Glob、Grep、Write、ReadArtifact、Edit 和 Bash schemas，并保持model、max_tokens和stream字段确定

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
- **WHEN** model、catalog、项目指令、native history、当前输入与缓存策略相同
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


### Requirement: Completion and capabilities match implemented Anthropic behavior

Anthropic Provider MUST仅在合法`message_stop`后产生一次completed terminal；失败、取消、timeout、oversized frame、非法序列或EOF继续遵守唯一terminal和不自动重放契约。Provider只有在Read/Glob/Grep/Edit/Write request编译、tool reducer、native codec/restore、result codec和golden全部通过后才声明function tools可用；parallel tools、custom/freeform tools、prompt cache key、previous response 和未实现 reasoning 展示 MUST 保持 unsupported。PromptCacheControl 只有在两种 TTL 的 request 编译、stream usage 归并、native persistence/restore 及 golden 全部验证后，且当前显式策略开启时才能声明；disabled 必须为 false，不推断任意 base_url 服务端支持。

Write SHALL 只有在 request、stream、native persistence/restore、result codec 与 golden 全部存在后可用；不得借本变更声明额外 apply_patch 操作、命令或未完成能力。

#### Scenario: Complete only on message_stop
- **WHEN** 全部blocks停止且收到合法`message_stop`
- **THEN** Provider产生一次携带native commit、usage和ready calls的completed terminal并关闭channel

#### Scenario: Reject EOF before message_stop
- **WHEN** SSE在`message_stop`前结束
- **THEN** Provider失败且不提交staging或自动重放

#### Scenario: Cancel or time out an Anthropic stream
- **WHEN** sample context取消或流超过idle timeout
- **THEN** Provider关闭资源、等待清理并产生一次可识别失败或取消terminal

#### Scenario: Inspect Anthropic capabilities
- **WHEN** Runtime查询capabilities
- **THEN** streaming、thinking-signature和包含Edit的function tools准确为可用
- **THEN** parallel/custom tools、key/previous-response 能力未被声明；PromptCacheControl 与已完成实现和当前显式策略一致

#### Scenario: Keep a disabled cache capability false
- **WHEN** Anthropic 缓存策略为 disabled
- **THEN** PromptCacheControl 为 false，请求无 cache_control，已实现工具能力保持原样
