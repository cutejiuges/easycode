# provider/prompt-caching Specification

## Purpose

定义双 Provider 的显式请求缓存策略，使 Anthropic 断点与 TTL、OpenAI 稳定路由 key 各自遵守原生 wire，同时保持内容指纹、持久化原生历史和恢复请求的可验证边界，避免缓存优化改变真实会话语义。

## Requirements

### Requirement: Cache policies are explicit immutable conversation inputs

系统 SHALL 按已校验配置为每个 Conversation 冻结独立缓存策略，并显式绑定合法 ThreadID；创建与恢复 SHALL 使用同一当前构造契约。构造、预检、getter 和策略选择 MUST 为纯内存操作，不得探测远端能力、读取环境或启动 goroutine。禁用策略时请求 SHALL 省略相应缓存字段，而不是发送 null。模型名或 Provider family 不得被用于猜测服务端缓存支持、最小长度或命中结果。

#### Scenario: Keep the default request unchanged
- **WHEN** 用户未配置缓存字段
- **THEN** Anthropic 请求没有 cache_control，OpenAI 请求没有 prompt_cache_key，HTTP 默认 golden bytes 保持不变

#### Scenario: Reject a missing conversation identity
- **WHEN** 调用方使用非法或空 ThreadID 创建或恢复 Conversation
- **THEN** 在网络、journal 创建和后台任务前返回安全英文错误

### Requirement: Anthropic markers preserve exact native content

启用 Anthropic 缓存时，系统 SHALL 在最终请求副本上确定性选择最多四个、去重的合法断点，优先覆盖工具前缀、非空项目指令、会话尾部及长间隔历史锚点；存在可表示的前次请求边界且其已超出尾部断点的 20-block 回看范围时 SHALL 保留该边界作为独立锚点。不存在对应内容时不得制造空 system/message/block。ephemeral_5m SHALL 编码 type=ephemeral 且省略 ttl；ephemeral_1h SHALL 编码 type=ephemeral、ttl=1h；一次请求内所有显式断点使用同一冻结策略。

thinking、redacted_thinking 和未知 opaque block MUST NOT 被显式标记。除合法缓存覆盖字段外，原 block 的已知字段、未知扩展、signature、opaque data、消息边界、工具配对及顺序 MUST 无损保留；已提交原生对象与 bytes MUST 不被就地改写。缓存逻辑不得静默丢弃已有受控扩展或覆盖冲突的 cache_control；冲突输入必须在发送前失败关闭。

#### Scenario: Cache a tool continuation with reasoning
- **WHEN** 先前 assistant 包含 thinking/signature、tool_use，随后提交了有序 tool_result
- **THEN** 请求保留原生内容和配对，仅在合法工具定义或非 thinking 内容块上添加断点
- **THEN** 编译前后 native commits 的 bytes 完全相同

#### Scenario: Preserve a distant reusable boundary
- **WHEN** 当前尾部与可表示的上次请求边界相距至少 20 个 block
- **THEN** 请求保留该历史边界的断点，总数不超过四个，恢复后选择相同位置

#### Scenario: Compile both supported lifetimes
- **WHEN** 相同原生内容分别使用 ephemeral_5m 和 ephemeral_1h
- **THEN** 策略和最终请求 fingerprint 有可解释差异，内容分段 fingerprint 不变

### Requirement: OpenAI keys have a stable non-secret scope

OpenAI thread 策略 SHALL 从固定应用命名空间与合法 ThreadID 确定性派生一个有界 key；同一 thread 的 turn、工具 sample、摘要和同配置恢复使用相同 key，不同 thread 使用不同 key。key MUST NOT 从 secret、路径、URL、用户正文、时间、随机重建值或调用/审批身份派生，也不得加入工具或项目指令的内容 fingerprint。omit 模式 SHALL 完全省略字段。系统 MUST NOT 声称 key 能保证命中、节省固定比例费用或适用于所有模型的路由优化。

#### Scenario: Restore a stable key
- **WHEN** 相同 thread、配置和原生历史被重新加载
- **THEN** 完整请求 key、canonical bytes 和 fingerprint 与不中断路径一致

#### Scenario: Separate routing identity from content
- **WHEN** 两个 thread 使用完全相同内容并启用 thread key
- **THEN** key 和完整请求 fingerprint 不同，稳定内容分段 fingerprint 仍一致

### Requirement: Summary requests use the same cache compiler without contaminating history

普通采样、输入候选预检和手动/自动摘要 SHALL 复用同一策略与最终请求编译规则。摘要请求断点 SHALL 覆盖可复用原生上下文，但不得落在仅本次摘要使用的指导后缀；摘要生成 provenance、checkpoint、原生历史和 Session envelope 不得加入缓存标记、key 或连接续写字段。成功压缩只替换有效窗口，不改变工具与项目指令的稳定内容 fingerprint；失败摘要不得推进普通 sample 的缓存基线。

#### Scenario: Compile a guided summary
- **WHEN** 摘要请求追加独有保留重点
- **THEN** 最终请求仅在可复用前缀上标记，无重复或过期断点，指导不进入下一普通窗口

#### Scenario: Continue after a failed summary
- **WHEN** 摘要失败而旧窗口仍合法
- **THEN** 旧原生窗口、普通缓存成功基线和工具授权事实保持，下一请求重新按旧窗口编译
