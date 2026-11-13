## MODIFIED Requirements

### Requirement: One owner decides admission and turn execution

每个活动thread SHALL只有一个输入控制owner串行决定command admission、活动turn身份、活动approval、follow-up queue转换和关闭状态。空闲时接受的`submit_input` SHALL得到`starting` disposition；存在活动turn时接受的输入 SHALL得到`queued` disposition并进入该Session实例持有的FIFO follow-up queue。`resolve_tool_approval`只能路由到同一owner当前记录的一个活动request，不进入follow-up queue，也不得启动新turn。

并发调用方不得通过先读取状态再自行启动turn或完成approval的方式绕过owner。任意时刻最多一个`RunTurn`可以修改该Conversation；输入 MUST 按 admission FIFO 处理，正文不得拼接、重排或并行执行。严格 submit_input 保持每条一个独立 turn；自动输入只可领取队首连续自动前缀，在完整工具输出或 Stop continued 冻结决定已闭合的合法安全点进入当前 turn，或在空闲时形成一个含多条独立消息的新 turn。严格输入是不可跨越的 FIFO 屏障；任何输入均不得注入活动 stream 或 approval feedback。

#### Scenario: Serialize concurrent submissions
- **WHEN** 两个submit在空闲thread上并发到达
- **THEN** owner按唯一顺序接受它们，恰好一个得到`starting`，另一个得到`queued`
- **THEN** Provider任意时刻只存在一个活动stream

#### Scenario: Route approval without creating a turn
- **WHEN** 当前turn等待approval且匹配response到达
- **THEN** owner把response交给该turn并保持原turn identity
- **THEN** response不进入普通输入FIFO、不生成用户turn也不建立并发Provider stream

#### Scenario: Preserve one input per turn
- **WHEN** 活动turn期间连续接受两个严格 submit_input follow-up输入
- **THEN** 当前turn先收口，两个输入再按FIFO各自启动新的独立turn
- **THEN** 系统不拼接两个文本，也不把它们加入当前turn的Provider请求

#### Scenario: Close the lost-wakeup window
- **WHEN** 新输入或approval response在owner观察状态转换之间到达
- **THEN** owner重新观察已接受工作并最终处理它
- **THEN** 命令不会滞留到下一条无关命令到达后才执行

同一 owner SHALL 同时管理压缩与普通 turn，任意时刻最多一个活动 worker；手动压缩命令只在 idle 且输入队列为空时接受，不能跨越 FIFO；自动摘要沿统一 worker 的合法采样候选安全点运行，不新建并行 owner。活动压缩期间接受的新输入只能 queued，不能 claim 或进入摘要；context.query 可读最近已提交快照。查询和控制不得被网络等待阻塞。

#### Scenario: Serialize a compact and a submit race
- **WHEN** idle 状态下 compact 与 input submit 并发到达
- **THEN** owner 先接受 compact 时输入 queued；先接受 submit 时 compact 被 session_busy 拒绝，任意时刻只有一个网络 worker

Stop 自动续轮 SHALL 在 continued 冻结决定已保存且下一普通 sample 有余量后，由原输入 owner 领取一次队首连续 auto；strict-next 仍不可跨越，Hook 自身不创建 InputID 或改变真实正文。通过/拒绝检查后未续轮的队列仍沿原下一 turn 路径。反馈与合法 claimed 输入先组成候选；新到 queued 输入不进入已冻结候选或进行中的摘要。

#### Scenario: Deliver a correction before a stop continuation
- **WHEN** Stop 运行时依次排队 auto A、strict-next B、auto C，随后合法 continued
- **THEN** 下一普通请求之前只领取 A，原 ID/正文不变，B/C 保留 FIFO，任意时刻仍只有一个 worker

#### Scenario: Keep the next-turn path after a passed check
- **WHEN** Stop 通过且队列有待输入
- **THEN** 当前 turn 按唯一成功 terminal 关闭，再按原 FIFO 启动后续 turn，不伪造一次 Hook 续轮

## ADDED Requirements

### Requirement: Stop continuation claims resolve with atomic native delivery

Stop 的本地反馈交付 SHALL 独立于真实用户输入的 admission。无真实追加时只保存 native hook_feedback；有合法 claimed 输入时必须以 [native hook_feedback, turn_inputs_committed(steering), native user_inputs] 同批 Sync/finalize 后，才报告真实输入 committed。反馈在前、真实纠正在后，真实用户输入不会变成 Hook 数据或批准响应。

复合交付接收前取消、确定本地拒绝或预检超限 SHALL 返还 claim，不能因反馈已接受而用不可取消 context 强制提交用户原文；反馈义务另以 FeedbackOnly 本地收口。复合交付接收后 Sync 成功则用户输入已保存，不返还/重发；durability 不确定必须 uncertain/poison，禁止再次交付或采样。owner 保留唯一 claim resolution、预算和清理完成信号，无法确认前不得放行下一 turn。

#### Scenario: Cancel before the compound batch is admitted
- **WHEN** 用户取消发生在复合交付 writer admission 前
- **THEN** 用户 claim 确定返还，零该用户正文写入；已接收反馈单独交付后关闭旧 turn，零新网络

#### Scenario: Cancel after successful compound admission
- **WHEN** 复合交付已接收并 Sync 成功，期间发生取消
- **THEN** 反馈及用户原文都保留，用户输入 committed 且只解析一次，不返还或重发，不开始下一请求

#### Scenario: Poison an uncertain compound batch
- **WHEN** 复合交付 Sync 结果不确定
- **THEN** 不把用户输入返还或再提交，不追加替代反馈，保留 ownership；恢复按实际完整 batch 校验
