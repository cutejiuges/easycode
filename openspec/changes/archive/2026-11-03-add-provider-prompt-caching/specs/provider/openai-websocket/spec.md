## Purpose

提供保持 store=false 的可选 OpenAI Responses WebSocket 续写路径，以完整原生请求为事实依据，在严格增量资格、持久化提交和连接所有权约束下减少重复发送，并保留 HTTP/SSE 默认行为及可解释的安全回退。

## ADDED Requirements

### Requirement: WebSocket is an explicit optional Responses transport

默认 transport SHALL 为 http_sse；仅显式 websocket_auto 配置允许在实际采样阶段连接 WebSocket。连接 SHALL 使用同一经过校验的 base_url 路径前缀和鉴权，http/https 分别映射 ws/wss，不接受用户注入的任意 endpoint、userinfo、query 或 fragment，不跟随可泄漏鉴权的重定向。构造与预检 MUST 不连接。WebSocket response.create SHALL 从完整 canonical 请求派生 transport 专属 envelope，省略 SSE 的 stream 字段，保持 store=false；响应 SHALL 复用同一原生事件归并、strict 参数验证和 terminal 规则。

#### Scenario: Preserve HTTP-only startup
- **WHEN** 使用默认配置创建并运行会话
- **THEN** 只建立既有 HTTP/SSE 请求，不尝试 WebSocket 或预热

#### Scenario: Preserve a custom endpoint prefix
- **WHEN** websocket_auto 的 base_url 带自定义路径前缀
- **THEN** WebSocket 和 HTTP 均在同一前缀下使用 responses，鉴权不被发送到其他 origin

### Requirement: Incremental requests require an exact durable native prefix

连接内续写 SHALL 仅引用同一存活连接中已完成且其 native commit 成功 Sync/finalize 的普通 sample。新完整 input MUST 精确以前次完整 input 加前次有序原生 outputs 为前缀，且全部非 input 模型上下文属性不变，才能发送 previous_response_id 与新增后缀。比较 MUST 保留 reasoning、encrypted content、phase、工具调用和输出的全部模型可见字段，不得用语义文本或忽略 opaque 差异决定可复用。新输入缩短、前缀或属性不符时 SHALL 发送 full create 并给出封闭原因。

#### Scenario: Continue after durable tool outputs
- **WHEN** 前次普通 sample 已 finalized，工具 outputs 亦已 durable，完整新请求是严格原生前缀扩展
- **THEN** 发送 previous_response_id 与后缀，所有工具 calls/outputs 的原生配对保持

#### Scenario: Do not use an uncommitted completion
- **WHEN** response.completed 已收到但 sample Sync 未成功或 prepared sample 被丢弃
- **THEN** response ID 不可用于续写，失败 Session 不启动新请求

#### Scenario: Reset on a changed request
- **WHEN** tools、项目指令、model、key、其他模型上下文属性改变，或任一原生前缀字段不同
- **THEN** 请求不包含 previous_response_id，完整 input bytes 未丢失

### Requirement: Restore compaction and cancellation reset transient continuation

恢复、连接重建、活动请求取消、协议错误、结果不确定或普通/摘要窗口边界变更 SHALL 清除增量资格。恢复首个请求和摘要请求 SHALL 为 full，摘要不得成为普通续写基线；摘要启动时关闭或重置普通连接基线，摘要后下一普通请求 SHALL 为 full，即使摘要失败仍如此。上述重置不得删除 durable native history、工具结果或稳定缓存 key。

#### Scenario: Recover with a full request
- **WHEN** 重启加载包含 response ID 的原生历史
- **THEN** 不重用旧连接身份，首次实际请求是 full，无恢复阶段网络或工具执行

#### Scenario: Cancel a blocked response
- **WHEN** 读取或写入 response.create 的活动 sample 被取消
- **THEN** owner 关闭连接以解除阻塞，清除基线并输出唯一失败终态，后续显式请求重新 full

### Requirement: Fallback never blindly repeats a generation request

websocket_auto SHALL 只在 response.create 写入开始前、确认没有模型生成请求发送时执行有界 HTTP 回退：显式握手不支持状态 404/405/426，或不涉及 TLS/鉴权/重定向的明确连接不可用。该会话在这类回退后 SHALL 固定 HTTP，快照解释原因；401/403、TLS 验证、策略或重定向错误 MUST 直接失败，不能隐藏为回退。response.create 写入开始后的任何部分写、断线、超时、服务端错误或 previous_response_not_found SHALL 结束当前 sample，清除基线，不自动重发生成请求；用户后续显式输入可从 full 请求继续。外层 context 已取消时 MUST 直接取消，不得将用户取消伪装为可回退的连接超时。错误分类 MUST 使用结构化状态而非匹配英文错误字符串。

#### Scenario: Fall back before sending a generation
- **WHEN** 升级请求返回 426 且未开始 response.create 写入
- **THEN** 恰好一次 HTTP/SSE 请求，后续会话请求保持 HTTP，诊断显示安全原因

#### Scenario: Fail a lost response without replay
- **WHEN** response.create 可能已部分或全部发送后连接断开
- **THEN** 不发送 HTTP 替代请求，不再次执行工具，当前 sample 按现有失败收口

#### Scenario: Keep authentication errors visible
- **WHEN** WebSocket 握手返回 401/403 或 TLS 校验失败
- **THEN** 返回安全错误且 HTTP 请求次数为零

### Requirement: A connection has one bounded lifecycle owner

每条 Conversation SHALL 至多拥有一个活动生成请求和一个 WebSocket 连接 owner；跨会话不得共享续写状态。owner SHALL 有显式启动、幂等关闭、最终结果及完成信号。锁内不得执行 dial、读写、channel 等待或关闭；请求体上限 SHALL 为 16 MiB，单个重组事件上限为 4 MiB，握手/写入期限为 30 秒，读取空闲期限使用现有 stream idle 配置。只接受 UTF-8 JSON 文本事件，不以二进制或不受限压缩绕过上限。空闲连接不启动隐藏 reader/heartbeat goroutine。取消与 shutdown 必须关闭底层连接并等待唯一 owner；超时不得提前释放 writer/lease 或遗留后台资源。

#### Scenario: Reject an oversized fragmented message
- **WHEN** 多个 WebSocket frame 组成超限事件
- **THEN** 在无界分配前关闭连接，以一次协议失败收口且无 sample commit

#### Scenario: Retain ownership through shutdown timeout
- **WHEN** shutdown 等待期限到达而 socket 读写仍被阻塞
- **THEN** 强制关闭实际连接，保持原 owner 和完成信号直至清理完成，lease 不提前释放
