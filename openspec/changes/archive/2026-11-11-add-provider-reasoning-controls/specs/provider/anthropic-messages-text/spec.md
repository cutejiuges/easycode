## MODIFIED Requirements

### Requirement: Compile a deterministic text-only Messages request

系统 SHALL向解析后的相对`messages` endpoint发送Anthropic Messages请求并保留`base_url`路径前缀。请求 SHALL使用既有鉴权/version/JSON/SSE headers；正文 MUST包含model、正整数`max_tokens`、可选临时项目指令、按原顺序排列的Anthropic native history、`stream=true`和当前Tool Catalog编译出的稳定tools数组。所有真实用户输入 SHALL 已独立提交到 native history，编译仅按原生顺序回放，不额外追加当前正文、合成 user 或空消息。

存在项目指令时，其user context message SHALL位于已提交history和真实当前输入之前，并保持独立且不进入native history。Read、Glob、Grep、Write、ReadArtifact facade与structured `Edit`/`Bash` facade SHALL编译为稳定名称、description和strict JSON input schema；tools顺序与schema bytes MUST来自本次不可变catalog snapshot。请求不得声明 custom/freeform tool、后台进程、write_stdin 或 system 配置。普通请求 SHALL 按 provider/reasoning-control 编译显式冻结的 thinking（含合法 display）、output_config.effort 及输出上限；新字段省略时维持原请求 bytes。启用缓存的请求副本 MUST 保留这些字段，不能因 cache marker 编译丢失。禁用缓存时 MUST 省略 cache_control；启用时 SHALL 按 provider/prompt-caching 在最终请求副本上添加合法、确定性断点及冻结 TTL，不修改原生历史。secret、项目指令正文、workspace绝对路径、candidate fingerprint、实际环境和动态调用/approval identity MUST NOT进入错误、日志或普通fixture。

Write 的稳定 schema/description SHALL 来自同一不可变 catalog；文件存在性、Read 证据、候选内容、目录清单、命令输入和批准状态不得注入稳定工具定义。

#### Scenario: Compile the first Anthropic turn
- **WHEN** 空会话收到用户文本且catalog包含当前九个 Anthropic facades
- **THEN** 请求包含用户message以及稳定排序的Read、Glob、Grep、Write、ReadArtifact、Edit、Bash、Skill 和 AskUserQuestion schemas，并保持model、max_tokens和stream字段确定

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
- **WHEN** model、catalog、项目指令、native history、当前输入、冻结推理/输出设置与缓存策略相同
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

### Requirement: Completion and capabilities match implemented Anthropic behavior

Anthropic Provider MUST仅在合法`message_stop`后产生一次completed terminal；失败、取消、timeout、oversized frame、非法序列或EOF继续遵守唯一terminal和不自动重放契约。Provider只有在Read/Glob/Grep/Edit/Write request编译、tool reducer、native codec/restore、result codec和golden全部通过后才声明function tools可用；parallel tools、custom/freeform tools、prompt cache key、previous response、主动 summary 和 raw reasoning 的未实现能力 MUST 保持 unsupported。显式 thinking/effort 编译 SHALL 以 provider/reasoning-control 的完整实现及验收为前提，不用新增空 capability 位表示它。PromptCacheControl 只有在两种 TTL 的 request 编译、stream usage 归并、native persistence/restore 及 golden 全部验证后，且当前显式策略开启时才能声明；disabled 必须为 false，不推断任意 base_url 服务端支持。

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

ThinkingSignature SHALL 只表示已实现的原生 thinking/signature 保存；ReasoningSummary 表示主动编译 summary 请求，RawReasoning 表示 raw reasoning 能力，两者在本切片保持 false。被动白名单展示由已实现事件与投影契约表达，不从显示正文推断服务端能力，也不新增空能力位。

#### Scenario: Display returned thinking without enabling request reasoning
- **WHEN** 合法响应已经返回 thinking，用户阅读其白名单文字
- **THEN** 原请求 thinking 配置、max_tokens、缓存策略和能力声明不因阅读改变，signature/redacted 不可见

### Requirement: Anthropic usage reduces to a validated sample fact

Anthropic reducer SHALL 分别消费 `message_start.message.usage` 与 `message_delta.usage`，并在合法 `message_stop` 前形成最终 raw usage。start 中明确提供的零 SHALL 保存为已知零；delta 中 input、cache creation 和 cache read 的零 MUST NOT 覆盖 start 中已观察到的正值，也不得在没有 start 值时把占位零解释为已知零；delta 中明确提供的 output 零 SHALL 保存为已知零。后续正值 SHALL 按字段更新最终值，缺失字段保持 unknown。

message_delta usage 的可选 output_tokens_details.thinking_tokens SHALL 单独保留观测值；显式零是 known，缺失不覆盖已观测值，从未观测则 unknown。thinking 计数不得套用 input/cache 占位零规则；实际显示正文、display omitted 或 disabled 配置均不能替代计量。

所有已提供 token 值 MUST 为非负整数。负值、超出公共计数范围的值或无法按统一规则归一化的 usage MUST 使当前 stream 以协议错误失败，不提交 native history、`sample_usage` 或成功终态。最终 output 与 thinking 同时已知时 MUST 校验 thinking 不大于 output；output 缺失时保留独立 thinking 观测且不推导总量。合法 `message_stop` 产生的 prepared sample SHALL 同时携带包含必需 thinking_tokens 状态字段的当前 native commit 和对应 normalized usage，遵守 provider/usage-accounting。未知或损坏的当前持久化形状不得用旧 reader 兜底。

#### Scenario: Preserve start usage across zero delta

- **WHEN** `message_start` 报告 input `12`、cache creation `3`、cache read `7`，随后 `message_delta` 对这些字段报告零并报告 output `9`
- **THEN** 最终 raw usage 保持 `12`、`3`、`7`、`9`
- **THEN** normalized usage 以相同数值产生对应公共指标

#### Scenario: Keep absent Anthropic usage unknown

- **WHEN** 合法 Anthropic sample 没有提供某个可表达 usage 字段
- **THEN** raw usage 与 normalized usage 中对应字段均保持 unknown，而不是已知零

#### Scenario: Preserve an explicit output zero

- **WHEN** `message_delta.usage.output_tokens` 明确为零
- **THEN** output usage 为 `known: 0`

#### Scenario: Reject negative Anthropic usage

- **WHEN** start 或 delta usage 包含负 token 值
- **THEN** Provider 产生 stream protocol failure 并丢弃当前 sample staging

#### Scenario: Preserve the final thinking breakdown
- **WHEN** 起始四项 usage 与最终 message_delta 的 output 40/thinking_tokens 30 合法
- **THEN** 两个输出指标分别为 known:40/known:30，raw/native 与 normalized 进入同一 durable sample

#### Scenario: Distinguish omitted text from zero usage
- **WHEN** thinking 文本为空但显式 thinking_tokens 为 30，或没有报告该计数字段
- **THEN** reasoning_output 分别为 known:30 或 unknown，不由 signature/正文/请求策略推断计量

#### Scenario: Reject a thinking count above the final output
- **WHEN** 最终 output 为 20、thinking_tokens 为 30
- **THEN** 合法 message_stop 也不能形成成功 sample，丢弃本次 staging 并保留先前已提交事实

#### Scenario: Update thinking observations across distinct delta events
- **WHEN** 不同合法 message_delta 依次报告 thinking_tokens 30、0，再省略该字段，或先报告 0 再报告 30
- **THEN** 最终观测分别为 known:0 和 known:30，缺失不擦除已知值，明确零不套用 input/cache 占位规则
