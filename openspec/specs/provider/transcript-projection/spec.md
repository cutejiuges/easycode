# provider/transcript-projection Specification

## Purpose
定义双 Provider 原生历史到宿主会话记录的单向只读展示投影，使文本、用户追加与工具调用保持真实顺序，恢复后能够重建同一活动位置，同时不泄漏原生私有内容或改变请求与缓存。

## Requirements

### Requirement: Transcript projection preserves ordered text and tool anchors

两家 Provider SHALL 从已提交 native history 返回强类型的用户文本、assistant 文本段与工具调用锚点，保持提交顺序和同一原生 entry 内的 item/block 顺序。文本 MUST 原样保留空格与换行；用户关联保留 InputID，调用锚点能够与当前 thread 的 ready invocation 唯一对应。投影 MUST 能区分同 sample 的 text/call/text，不能先合并所有文本再把工具追加到末尾。稳定位置不依赖 wall clock、显示宽度、动画、结果完成顺序或文本内容匹配。

#### Scenario: Project interleaved sample content
- **WHEN** 任一 Provider 已提交的 sample 依次包含文本 A、工具调用 C、文本 B
- **THEN** 展示投影保留 A/C/B 的位置，C 的结果更新不移动该位置

#### Scenario: Preserve same-turn steering
- **WHEN** 同 turn 的完整 outputs 后提交追加输入，再提交后续 sample
- **THEN** 输入以原 InputID 出现在两次 sample 之间，不能被误判为新 turn 或合并进先前输入

#### Scenario: Reject mismatched anchors
- **WHEN** 原生调用锚点与 ready facts 的身份、能力、数量或次序不一致
- **THEN** 宿主拒绝构造伪造或部分成功的记录，不按时间或名称猜测配对

### Requirement: Transcript projection excludes private data and has no effects

投影 SHALL 是纯内存独立快照，MUST NOT 暴露 thinking/signature/redacted thinking、reasoning/encrypted content、phase、Provider response ID、原始 wire JSON 或未知扩展。它 MUST NOT 改写 native history、Session 或请求；SemanticHistoryView 保持独立的文本估算口径，不重复计入工具摘要、显示标签与锚点。调用方改变返回集合不能影响 Provider。

#### Scenario: Preserve native continuation
- **WHEN** 对含 opaque reasoning 的相同历史反复投影、折叠、展开或修改返回副本
- **THEN** 后续 canonical request、原生顺序与 fingerprint 相同，展示值不含 opaque 内容

### Requirement: Live committed projection matches restored transcript

成功提交的 sample SHALL 用同一有序投影建立 live 与 restored 记录。streaming 文本只能作为未提交的临时区域；commit 后 MUST 恰好一次对齐已提交段与锚点。未成功提交的 partial tool arguments MUST NOT 产生工具活动、审批或执行。失败的临时文本 SHALL 明确未完成，恢复不得伪造已保存；独立 durable 的用户输入与前序 sample 保留。

#### Scenario: Finalize streamed text without duplication
- **WHEN** streamed 文本后 sample 成功 Sync/finalize
- **THEN** 临时区域被相应 committed 内容替换，正文不重复，重启后的文本/工具顺序相同

#### Scenario: Disconnect during arguments
- **WHEN** 工具参数未完整时 stream 断开
- **THEN** 不展示已经 ready 或正在执行的工具；已提交的先前输入与结果仍可恢复
