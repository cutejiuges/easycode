## Purpose

定义启动时冻结的双 Provider 主动推理控制，使用户显式配置可以可靠进入普通与摘要原生请求，同时保持预算、同源证据、缓存身份、工具续写、计量恢复和展示隐私一致。该能力只声明本地已验证的编译路径，不将厂商名称或自定义服务地址当成远端支持证据。

## ADDED Requirements

### Requirement: Compile frozen reasoning settings through every ordinary request path

普通请求 SHALL 使用同一个经验证的冻结设置：Anthropic thinking 原样编译为 disabled、adaptive 或 enabled/budget_tokens，adaptive/enabled 的可选 display 编译到同一 thinking 对象，effort 编译为 output_config.effort；OpenAI effort/summary 编译为 reasoning 下的对应字段，仅有一个字段时省略另一个，没有字段时省略整个 reasoning。输出上限 SHALL 遵守 config/json-file。参数只能出现在原生请求的对应位置，不注入用户消息、系统指令、工具描述或展示文本来模拟能力。

Anthropic disabled/5m/1h 缓存路径与 OpenAI HTTP/WebSocket full/incremental 路径 MUST 保留相同普通推理设置；只允许既有 transport 字段差异。未配置新字段时，全部已有默认请求 golden bytes MUST 不变。不得发送本切片未实现的 beta header、sequential_cutoff、configuration_update、reasoning mode/context 或任意扩展请求参数。

#### Scenario: Compile a cached manual Anthropic request
- **WHEN** manual budget 2048、普通上限 8192、effort high 与 1h 缓存同时启用
- **THEN** 普通请求同时保留 thinking、output_config、max_tokens、确定性 marker/TTL，原生 history 不含 cache marker

#### Scenario: Compile an OpenAI summary-only request
- **WHEN** 仅配置 summary auto，未配置 effort 和窗口
- **THEN** reasoning 仅包含 summary，不补默认 effort，include 继续保留 reasoning.encrypted_content

#### Scenario: Continue a WebSocket tool loop
- **WHEN** 启用 effort high/summary auto 的 WebSocket sample 完成工具调用，结果 durable 后连接内增量续写
- **THEN** 派生请求保留相同 reasoning/输出上限及有序工具结果，store=false，stream 字段按既有 WebSocket 规则省略

#### Scenario: Preserve default requests exactly
- **WHEN** 使用没有任何新增字段的既有合法配置
- **THEN** 两家普通请求的 canonical bytes、catalog 和既有 fingerprint 与修改前固定 golden 一致

#### Scenario: Preserve display through cached thinking requests
- **WHEN** adaptive 或 enabled 显式 display summarized/omitted 并使用任一支持的缓存策略
- **THEN** 普通与摘要的 thinking 对象保留同一 display，不新增 beta header；空正文不被当作请求未启用推理

### Requirement: Preflight transmission and evidence describe one logical request

同一采样或摘要操作的预检、编码 body、输入证据、请求属性和 receipt SHALL 从同一个冻结用途、设置、原生窗口及来源构造。摘要证据 MUST 包含真实摘要的工具禁用、独立上限和独有指导后缀，不能用普通请求的证据代替；运行时 MUST 在执行前核对来源仍匹配预检，不重新读取配置或从共享语义视图重建请求。

请求 fingerprint SHALL 对应实际发送前完整逻辑 canonical body；WebSocket 派生及回退仍以该完整逻辑请求为依据。输入 fingerprint SHALL 对应同一计划的有序逻辑 input/messages（包括临时项目/Skill 和摘要指导，排除 cache marker 覆盖），不是只对 journal payload 求 hash；properties SHALL 排除该输入数组但保留实际用途的其他字段。窗口来源 digest 与输入 fingerprint 含义不同，MUST 分开验证。摘要不安装普通 baseline，准备/预检/失败不能推进普通 baseline。

#### Scenario: Match a guided summary body and evidence
- **WHEN** 摘要使用 effort high、独立输出 4096、tool choice none 和保留重点
- **THEN** body、properties、输入 fingerprint 和 receipt 都对应这个摘要；不能包含普通输出上限或遗漏指导后缀

#### Scenario: Preserve evidence across transport derivation
- **WHEN** 相同完整逻辑请求分别走 HTTP、WebSocket full/incremental 或允许的生成前回退
- **THEN** 逻辑设置、输入顺序和完整请求 fingerprint 一致，transport mode 单独解释，不能将增量 body 当完整逻辑 fingerprint

#### Scenario: Reject a changed preflight source
- **WHEN** 执行前窗口 revision/source digest 与冻结预检不一致
- **THEN** 在发送及新副作用前拒绝；不把旧 body 与新 evidence 拼成有效操作

#### Scenario: Keep the ordinary baseline after summary failure
- **WHEN** 摘要准备后取消或返回失败
- **THEN** 普通 baseline、原生窗口和冻结策略保持，后续采样从原普通计划的合法关系继续

### Requirement: Request settings participate in deterministic cache identity

实际发送的普通推理、display 和输出字段 SHALL 进入确定性的请求属性及非默认 profile 身份。配置来源、是否经 UI 阅读、文件路径、时间、真实密钥和动态调用 ID MUST NOT 进入该身份。默认 profile 的既有 canonical bytes 保持；非默认请求属性变化 SHALL 可定位到 provider_profile，而工具、项目指令、Skill 目录和原生历史各自 fingerprint 不变。raw/normalized usage 不进入这些请求身份，读计量不能改变 prompt。

相同设置与原生前缀才允许 OpenAI 连接内增量；改变属性的独立重建实验 MUST 使用 full 请求。显式 cache key 和 Anthropic marker/TTL 仍使用既有独立策略；fingerprint 相同不能被展示为服务端命中证明。

#### Scenario: Isolate a policy-only change
- **WHEN** 相同历史/目录仅将 effort low 改为 high 后分别构造两个实例
- **THEN** 完整请求与 profile fingerprint 改变，首个失效来源是 provider_profile，目录与原生内容 fingerprint 不变

#### Scenario: Keep equivalent settings deterministic
- **WHEN** JSON 键顺序或配置文件位置不同但最终设置和全部其他输入相同
- **THEN** 请求、profile canonical bytes 和 fingerprint 相同

#### Scenario: Reset an incompatible request baseline
- **WHEN** 请求属性与 retained WebSocket baseline 不同
- **THEN** 不生成带旧 previous_response_id 的增量请求，不删除设置以追求缓存命中

### Requirement: Restore reasoning without policy-derived history rewriting

已提交推理、工具调用、工具结果和 metadata SHALL 继续由各 Provider 当前 native codec 无损保存。相同 ThreadID、model、设置及显式上下文来源下，uninterrupted 和 restored 的完整逻辑请求 canonical bytes、native item 顺序、request/native/profile fingerprint MUST 等价；WebSocket 恢复按既有 full 重建规则单独核验。

设置不新增到 Session/native/checkpoint envelope，不生成合成原生消息或 opaque 数据。改变设置的 resume SHALL 仅影响下一次显式输入后的请求，不回写历史、不自动采样、不重跑工具，也不承诺与不同设置的 uninterrupted 请求等价。

Anthropic raw usage SHALL 按 provider/usage-accounting 扩展唯一当前形状；固定 current fixture 替换为该形状后，同一记录的恢复和读取仍保真。默认请求 bytes 保持的承诺不意味着旧开发 native fixture 继续可读，也不意味着所有计量状态仍是 not_applicable。设置与 native usage 事实 MUST 分开保存和解释。

#### Scenario: Restore after reasoning and tool results
- **WHEN** 固定 journal 包含 thinking/signature/redacted 或 summary/encrypted/phase，以及完整调用/结果，随后同配置恢复
- **THEN** 下一请求逐字节等价，所有 opaque 哨兵完整、结果只出现一次、执行器计数为零

#### Scenario: Continue after a compacted restore
- **WHEN** 启用推理配置的会话压缩成功，再以相同配置恢复并提交相同新输入
- **THEN** 新窗口与不中断路径请求、顺序、fingerprint 一致，旧记录前缀不变

### Requirement: Explicit reasoning failures never trigger implicit strategy changes

显式设置不保证远端支持。鉴权、限流、不支持参数、HTTP 错误、断流、超时、取消和输出耗尽 SHALL 沿既有安全 typed failure/terminal 和 cleanup 契约处理，不删除推理字段、不换模式或模型、不增加输出限额、不自动重发。OpenAI 既有仅生成前连接不可用时的 WebSocket 安全回退继续允许，回退 HTTP MUST 携带同一逻辑设置；参数拒绝不得成为回退理由。

当前 sample 的 partial 推理或工具参数 MUST NOT 变成 durable 成功或 ready 调用。已有合法 Anthropic message_stop/max_tokens 的普通采样处理保持，摘要仍拒绝截断；本切片不得重新定义两家终态语义。所有错误、诊断和三种 headless 输出 MUST 继续排除推理正文与 opaque 哨兵。

两家 Provider MUST 在当前事件改变任何归并状态或接受终态之前验证完整 JSON 帧，拒绝任意对象层级的重复成员、尾随内容和有损 Unicode。后值不得覆盖先前的非法计量或冲突响应身份而使样本成功；格式正确的未知扩展继续沿既有受控规则处理，不得因完整帧校验而一律拒绝。

#### Scenario: Reject unsupported reasoning once
- **WHEN** endpoint 对显式推理参数返回 400
- **THEN** 仅一次生成请求，返回安全错误，无降级、重发、工具 ready 或成功 sample 提交

#### Scenario: Keep settings during safe transport fallback
- **WHEN** WebSocket 在生成写入前符合既有 404 安全回退条件
- **THEN** HTTP 请求保留相同 reasoning 和输出上限，不额外改变模型或缓存 key 策略

#### Scenario: Cancel a reasoning stream with partial tool input
- **WHEN** 流已返回允许推理但工具参数未完成时取消或断开
- **THEN** staging 丢弃、零工具副作用、唯一 owner 完成关闭且无隐藏重试

#### Scenario: Exhaust OpenAI reasoning output
- **WHEN** response.incomplete 报告输出限额且没有可见回答
- **THEN** 沿既有失败契约收口，不将部分推理作为最终回答或增加限额后重发

#### Scenario: Reject ambiguous final facts after complete tool arguments
- **WHEN** 完整工具参数已经到达，但随后事件含重复 usage 成员，或 OpenAI 终态含先冲突后匹配的重复 response ID
- **THEN** 当前 sample 以 stream_protocol_error 失败，不提交成功 native/usage、不执行工具、不隐式重发；已保存输入和先前事实保持，headless 不泄露异常帧正文

#### Scenario: Preserve valid provider extensions during frame validation
- **WHEN** 正常事件在外层或 usage details 包含格式正确、无重复成员的未知扩展
- **THEN** 已支持的事件、计量和合法终态保持原语义，未知扩展不会生成工具调用、正文或额外成功事实

### Requirement: Active summary capability is independent from passive presentation

OpenAI ReasoningSummary SHALL 仅在本地请求编译、流式归并、native persistence/restore、golden 已全部实现且本实例显式配置 summary 时为 true；未配置时为 false，不因读取返回的 summary 改变。该值只描述本地启用路径，不保证自定义 endpoint 接受。Anthropic ThinkingSignature/ OpenAI EncryptedReasoning 仍只描述原生保留；两家 RawReasoning 和 Anthropic ReasoningSummary 保持 false。不新增没有当前消费者的能力位或空策略。

#### Scenario: Configure active summary explicitly
- **WHEN** OpenAI 显式 summary auto 且完整实现验收通过
- **THEN** 本实例 ReasoningSummary 为 true，读取/关闭详情或服务端返回空 summary 均不改变该值

#### Scenario: Receive unsolicited summary without opting in
- **WHEN** 没有配置 summary 的服务端自行返回允许正文
- **THEN** 沿既有被动展示契约阅读，ReasoningSummary 仍为 false，不主动请求替代正文
