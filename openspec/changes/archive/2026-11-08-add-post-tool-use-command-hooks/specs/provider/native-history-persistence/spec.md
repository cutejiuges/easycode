## MODIFIED Requirements

### Requirement: Tool result preparation is pure and finalized after durability

每个Provider SHALL从按call index排列、capability、Provider call ID与原call wire kind匹配的强类型模型输出值（冻结原结果及独立冻结反馈）纯内存构造一个prepared tool-output entry。构造 MUST重新验证result status、preview bytes、数量、capability及function/custom/tool_use pairing，不得读取文件、网络、Session、approval registry或UI状态，也不得按result codec revision选择实现。

prepared tool-output entry SHALL提供可复制、自验证的当前envelope和恰好一次finalizer，但 MUST NOT携带sample usage。Runtime只有在对应`provider_native_commit`成功`Sync`后才能finalize；append/Sync失败时原生Conversation不得把output当作已提交history。


PostToolUse 反馈 SHALL 只在同调用已完整收口且通过披露/预算/身份校验后组合到对应 output 正文，保留原 status/code、is_error、call ID、function/custom/tool_result kind 和顺序。无匹配或全部 feedback=none 时 MUST 保持既有 output bytes 完全相同；model 模式空反馈只省略反馈段，原 preview 可因预留而缩短，编译只复用已经冻结的预览，不重新扩展。非空反馈为工具来源数据，不创建额外 call、InputID 或 system/developer 消息。tool-output restore 证据 SHALL 包含同组已验证结果/后置收口/反馈，使用同一编码规则核验存储正文，禁止根据正文标签猜测或从展示投影反建请求。

#### Scenario: Prepare heterogeneous outputs without I/O
- **WHEN** Runtime提供一组已durable的Glob、Patch与Read results
- **THEN** Provider在纯内存中产生按原call index和wire kind匹配的tool-output envelope，且不重新执行或访问文件系统

#### Scenario: Prepare outputs without I/O
- **WHEN** Runtime提供一组已durable的当前typed results
- **THEN** Provider只执行纯内存验证与编码，不读取文件、网络、Session或UI状态

#### Scenario: Reject reordered results
- **WHEN** Runtime提供的result顺序、capability、Provider call ID或wire kind与前一sample不匹配
- **THEN** preparation失败且不产生可持久化envelope或finalizer

#### Scenario: Finalize outputs after Sync
- **WHEN** tool-output native commit成功durable
- **THEN** finalizer恰好一次把outputs加入Conversation history
- **THEN** durable失败或重复finalize不会修改history

#### Scenario: Encode feedback for native tool facades
- **WHEN** Anthropic Edit/Write/Bash 或 OpenAI custom apply_patch/function Write/exec_command 具有合法冻结反馈
- **THEN** 只有对应 output 正文增加规范反馈段，原成功、wire kind、配对和 item 顺序不变

#### Scenario: Keep empty feedback byte identical
- **WHEN** 无 Post 计划或 feedback=none 且没有模型反馈
- **THEN** 原生 output 与旧路径逐字节相同，不附加空数组或标签

#### Scenario: Reject forged stored feedback
- **WHEN** native 正文含有与 result/post terminal/冻结反馈证据冲突的内容、顺序或身份
- **THEN** 整体恢复在 repair 或任何外部副作用前失败

#### Scenario: Preserve request and cache behavior
- **WHEN** 相同结果及反馈分别通过不中断、恢复与可选 WebSocket 路径继续
- **THEN** 完整逻辑 canonical request、原生顺序、opaque bytes 和内容 fingerprint 等价；未调用时 Hook 配置变化不污染稳定前缀

#### Scenario: Preserve reserved output when an enabled hook returns nothing
- **WHEN** model 计划使真实结果预览缩短，而 Hook 返回空对象
- **THEN** output 保持该次已冻结预览且没有空反馈包装，恢复 bytes 等于该次相同事实的不中断路径，不与无 Hook 的较大预览错误比较
