## MODIFIED Requirements

### Requirement: Reduce Anthropic content blocks with an indexed state machine

系统 SHALL独立处理既有Messages事件并按block index与wire顺序归并content blocks。text/thinking/signature/redacted-thinking行为保持既有契约；`tool_use` block MUST包含非空id与已暴露名称，`input_json_delta.partial_json`只可追加到对应活动tool block。只有block stop后完整JSON通过匹配facade的strict decoder，且`message_stop`在全部blocks停止后到达，tool call才可进入completed prepared sample。

重复index、未开始block的delta/stop、delta类型不匹配、重复call ID、未知tool、损坏/超限JSON、Edit未知字段或不支持操作、未完成block上的`message_stop` MUST终止为stream protocol error。参数delta与展示draft不得产生approval、executor I/O或durable ready fact。

Write SHALL 严格归并完整 structured 参数，未知/重复字段、null、非法编码、尾随内容和超限输入按协议错误拒绝；不得根据 delta 预读文件或产生审批。

#### Scenario: Stream ordered assistant text
- **WHEN** text block依次收到text deltas并停止
- **THEN** 系统按顺序投影文本且原生block只保存最终文本一次

#### Scenario: Preserve thinking and its signature
- **WHEN** thinking/signature或redacted block合法完成
- **THEN** thinking 与 signature 按既有规则保存在 native history；只有 thinking 白名单文字可作为独立推理展示，signature 不可见，两者均不混入普通 assistant 文本

#### Scenario: Preserve redacted thinking
- **WHEN** redacted thinking block合法完成
- **THEN** opaque data不被解释或改写并保留在Anthropic原生item中

#### Scenario: Reject a mismatched delta
- **WHEN** text block收到thinking delta或tool block收到不匹配的delta类型
- **THEN** Provider产生stream protocol failure且不提交sample staging

#### Scenario: Ignore an unknown well-formed event
- **WHEN** 服务端发送当前切片未知但格式正确的event type
- **THEN** Provider不panic、不伪造call或输出并继续等待受支持事件或终态

#### Scenario: Complete a structured Edit tool_use block
- **WHEN** `tool_use` block收到多个partial JSON deltas并以合法Edit参数停止
- **THEN** 原生block无损保存call ID/name/input，prepared sample产生一个typed Patch ready call

#### Scenario: Complete a Read tool_use block
- **WHEN** `tool_use` block收到多个partial JSON deltas并以合法Read参数停止
- **THEN** 原生block无损保存call ID/name/input，prepared sample产生一个typed Read ready call

#### Scenario: Reject incomplete tool input
- **WHEN** tool block在JSON不完整、schema非法或名称未暴露时停止
- **THEN** Provider产生stream protocol failure且不提交sample或ready call

#### Scenario: Complete a structured Write
- **WHEN** Write 参数跨随机 chunk、半包与 UTF-8 边界到达，并以合法原生 terminal 完成
- **THEN** 保留原始调用身份/参数，产生一个匹配 fs.write 的 ready call

#### Scenario: Interrupt partial Write input
- **WHEN** content 未完成时 stream 被取消或断开
- **THEN** staging 丢弃，没有 ready、approval、文件读取或写入

Anthropic 推理展示 SHALL 遵守 provider/reasoning-display 的 start/delta/stop 口径：start 的 thinking/signature 初值仍忽略，delta 累计，stop 产生有界完成展示与原生 item。不得为完成快照读取 start 初值或创建新的服务端 completion-only 分支；redacted 仅有无正文状态，完整历史恢复直接使用已保存 native 值。

#### Scenario: Ignore repeated start thinking content
- **WHEN** thinking start 含初值，后续 thinking_delta 重复该文字
- **THEN** 原生与展示只累计 delta 一次；没有 delta 时不得从 start 补出可阅读正文

### Requirement: Completion and capabilities match implemented Anthropic behavior

Anthropic Provider MUST仅在合法`message_stop`后产生一次completed terminal；失败、取消、timeout、oversized frame、非法序列或EOF继续遵守唯一terminal和不自动重放契约。Provider只有在Read/Glob/Grep/Edit/Write request编译、tool reducer、native codec/restore、result codec和golden全部通过后才声明function tools可用；parallel tools、custom/freeform tools、prompt cache key、previous response 和主动请求 reasoning/summary 的未实现能力 MUST 保持 unsupported。PromptCacheControl 只有在两种 TTL 的 request 编译、stream usage 归并、native persistence/restore 及 golden 全部验证后，且当前显式策略开启时才能声明；disabled 必须为 false，不推断任意 base_url 服务端支持。

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
