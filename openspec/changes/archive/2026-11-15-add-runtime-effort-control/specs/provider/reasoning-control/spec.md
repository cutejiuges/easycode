## MODIFIED Requirements

### Requirement: Compile frozen reasoning settings through every ordinary request path

普通请求 SHALL 使用当前完整操作捕获的同一个经验证推理策略：Anthropic thinking 原样编译为 disabled、adaptive 或 enabled/budget_tokens，adaptive/enabled 的可选 display 编译到同一 thinking 对象，effort 编译为 output_config.effort；OpenAI effort/summary 编译为 reasoning 下的对应字段，仅有一个字段时省略另一个，没有字段时省略整个 reasoning。输出上限 SHALL 遵守 config/json-file。参数只能出现在原生请求的对应位置，不注入用户消息、系统指令、工具描述或展示文本来模拟能力。启动设置是初始选择，唯一可在进程内更新的字段为 runtime/effort-control 定义的 effort。

Anthropic disabled/5m/1h 缓存路径与 OpenAI HTTP/WebSocket full/incremental 路径 MUST 保留相同操作推理设置；只允许既有 transport 字段差异。未配置新字段且没有 effort override 时，全部已有默认请求 golden bytes MUST 不变。不得发送未实现的 beta header、sequential_cutoff、configuration_update、reasoning mode/context 或任意扩展请求参数。

当前 turn 的输入预检、普通/工具续写、Stop 续轮和自动摘要 SHALL 始终携带启动该操作时捕获的策略；选定未来 effort 不能改变其预检、body、properties 或 receipt。固定相同 native 窗口和候选输入时，策略变化 SHALL 只改变相应原生 effort、provider_profile 和请求属性；控制更新本身不改 native history、目录或旧收据，正常新输入的原生追加不受影响。来源或 selection_revision 变化但 wire 设置等价时 MUST 保持相同规范身份；选择序号不得进入 wire、profile 或 receipt。

普通请求使用本操作显式捕获的 effort，不实现保持旧 request effort 并追加 configuration_update 的专有 pin 路径。只有没有中间操作启动的同值选择才保留原增量资格；真实操作失败、取消、摘要或重连继续遵守已有连接清理与完整请求规则，不得通过切回旧档位重建失效资格。配置已捕获不构成请求已经发送或远端采用的证据。

#### Scenario: Compile a cached manual Anthropic request
- **WHEN** manual budget 2048、普通上限 8192、effort high 与 1h 缓存同时启用
- **THEN** 普通请求同时保留 thinking、output_config、max_tokens、确定性 marker/TTL，原生 history 不含 cache marker

#### Scenario: Compile an OpenAI summary-only request
- **WHEN** 仅配置 summary auto，未配置 effort 和窗口
- **THEN** reasoning 仅包含 summary，不补默认 effort，include 继续保留 reasoning.encrypted_content

#### Scenario: Continue a WebSocket tool loop
- **WHEN** effort high/summary auto 的 WebSocket sample 完成工具调用，工具结果 durable 前后接受 low 更新
- **THEN** 当前回合增量续写仍保留 high 和原输出上限及有序工具结果，store=false，stream 字段按既有 WebSocket 规则省略

#### Scenario: Preserve default requests exactly
- **WHEN** 使用没有任何新增字段且未执行 effort 更新的既有合法配置
- **THEN** 两家普通请求的 canonical bytes、catalog 和既有 fingerprint 与修改前固定 golden 一致

#### Scenario: Preserve display through cached thinking requests
- **WHEN** adaptive 或 enabled 显式 display summarized/omitted 并使用任一支持的缓存策略
- **THEN** 普通与摘要的 thinking 对象保留同一 display，不新增 beta header；空正文不被当作请求未启用推理

#### Scenario: Start a new operation with a changed policy
- **WHEN** 原 high 回合已清理完毕，下一回合捕获 low
- **THEN** 其输入预检、实际请求及 receipt 一致为 low；OpenAI 属性不匹配时发 full，不携带旧 previous_response_id

#### Scenario: Omit only effort
- **WHEN** OpenAI summary=detailed 且用户选择 auto，或 Anthropic 保留 adaptive/display 并选择 auto
- **THEN** 下一操作只省略 effort，保留其他原生字段；不创建 configuration_update 或合成用户文本

#### Scenario: Ignore observation revisions in canonical requests
- **WHEN** 相同窗口、候选输入和 wire 设置分别具有不同来源或选择序号
- **THEN** 两家完整逻辑请求和 profile/native/request fingerprint 均等价，不发送选择序号或本地来源

#### Scenario: Coalesce unused intermediate selections
- **WHEN** high 基线存在，用户在没有任何中间操作启动时选择 low 后又选回 high
- **THEN** 新请求仍按实际 high 属性比较基线，不因设置次数或序号变化强制 full

#### Scenario: Preserve failure reset after selecting the old effort again
- **WHEN** low 操作真实开始后失败或取消并按现有规则清理连接，用户随后选回 high
- **THEN** 下一请求遵守既有 reset/full 规则，不恢复旧 previous_response_id，也不以设置相等宣称连接可复用
