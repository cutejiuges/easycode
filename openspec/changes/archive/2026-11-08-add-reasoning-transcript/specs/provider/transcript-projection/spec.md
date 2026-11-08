## MODIFIED Requirements

### Requirement: Transcript projection preserves ordered text and tool anchors

两家 Provider SHALL 从已提交 native history 返回强类型用户文本、assistant 文本、独立推理项与工具调用锚点，保持 native entry 和内部 item/block 顺序。推理只含 provider/reasoning-display 白名单正文或无正文 unavailable；OpenAI 非空 summary_text parts 保留原序和空白，两个 LF 分隔，零字节段不加分隔，不改变原生 parts。文本 MUST 原样保留空格与换行，用户关联保留 InputID，调用锚点与当前 thread ready fact 唯一对应。推理/text/call/text 不得被合并重排，位置不依赖时间、宽度、完成顺序或正文匹配。

宿主主线模式 SHALL 在排除推理项后取最近最多 50 项，详情模式包含推理并按全部逻辑项翻页；两个模式各自保持同一原生 cursor 的顺序和明确模式，不把隐藏条目仍计入主线窗口。正文范围读取 SHALL 与完整展示字节语义一致，不为每页重新拼接全部 summary parts。

#### Scenario: Project interleaved sample content
- **WHEN** sample 依次保存推理 R、文本 A、调用 C、文本 B
- **THEN** detail 保留 R/A/C/B，main 保留 A/C/B，结果更新或阅读不移动 cursor

#### Scenario: Preserve same-turn steering
- **WHEN** 同 turn 的工具 outputs 后保存追加输入，再保存后续 sample
- **THEN** 输入以原 InputID 位于两次 sample 之间，不误判新 turn 或合并前序输入

#### Scenario: Reject mismatched anchors
- **WHEN** call 锚点与 ready facts 的身份、能力、数量或次序不符
- **THEN** 拒绝整份投影，不按时间或名称猜测配对

#### Scenario: Filter before limiting the main window
- **WHEN** 最近历史含超过 50 个推理项及多条普通回答/工具项
- **THEN** main 的 50 项窗口按非推理项计算，detail 仍能按真实 cursor 翻到推理，无伪造或重编号

### Requirement: Transcript projection excludes private data and has no effects

投影 SHALL 是独立、纯内存快照；只有明确允许的 thinking/summary 文字可进入独立推理值。signature、redacted data、encrypted content、raw reasoning/content、phase、Provider ID、原始 JSON 和未知扩展 MUST NOT 暴露。unavailable 禁止正文；SemanticHistoryView 完全排除推理，文本估算/Hook 等消费者不获得新内容。投影不得改写 native、Session、请求或工具授权，返回集合的修改不得影响其他视图或会话。

#### Scenario: Preserve native continuation
- **WHEN** 相同历史被反复投影、阅读或修改返回副本
- **THEN** 后续请求 canonical bytes、native 顺序和 fingerprint 不变，展示仅含白名单值

#### Scenario: Keep semantic consumers private
- **WHEN** 同一历史分别生成 transcript 与文本语义视图
- **THEN** 只有 transcript 含允许推理，语义历史和摘要请求不被展示派生值扩充

### Requirement: Live committed projection matches restored transcript

成功 sample SHALL 以相同 native 投影生成 live committed 与 restored 内容，包含准确来源、文字和 unavailable。活动 `{turn,sample,block}` SHALL 通过经过验证的纯内存绑定连接 `{sequence,item}`，不依赖正文相等、最新列表是否保留目标或工具 ready facts。streaming 推理/文本与 block Completed 均不证明已保存；只有真实提交结果才能解析保存状态。

成功提交后临时范围恰好替换一次；提交结果不确定时 MUST 明示未知，恢复以严格验证的实际事实为准，不推断记录必定不存在。partial tool arguments 仍不得产生 ready、审批或执行；先前已提交用户输入和 sample 保留。未提交失败内容不得伪造恢复历史。

#### Scenario: Finalize streamed text without duplication
- **WHEN** 推理和文本 sample 成功 Sync/finalize
- **THEN** 按真实映射替换临时范围，恢复后内容和 cursor 相同，无重复

#### Scenario: Disconnect during arguments
- **WHEN** 工具参数未完整时断线且 sample 未进入提交
- **THEN** 不展示 ready/执行，本 sample 明确未保存，先前 durable 输入与结果保留

#### Scenario: Commit a selection outside the latest list page
- **WHEN** 用户正在阅读的活动块提交后不在最新 50 项详情页
- **THEN** 仍通过精确 cursor 读取相同正文，不退回正文搜索或错选其他项
