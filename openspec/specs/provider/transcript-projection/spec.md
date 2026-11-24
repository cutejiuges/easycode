# provider/transcript-projection Specification

## Purpose
定义双 Provider 原生历史到宿主会话记录的单向只读展示投影，使文本、用户追加与工具调用保持真实顺序，恢复后能够重建同一活动位置，同时不泄漏原生私有内容或改变请求与缓存。

## Requirements

### Requirement: Transcript projection preserves ordered text and tool anchors

两家 Provider SHALL 从已提交 native history 返回强类型用户文本、assistant 文本、独立推理项、工具调用锚点及具有独立来源的 Stop 反馈条目和 SessionStart 上下文条目，保持 native entry 和内部 item/block 顺序。推理只含 provider/reasoning-display 白名单正文或无正文 unavailable；OpenAI 非空 summary_text parts 保留原序和空白，两个 LF 分隔，零字节段不加分隔，不改变原生 parts。文本 MUST 原样保留空格与换行，用户关联保留 InputID，调用锚点与当前 thread ready fact 唯一对应。推理/text/call/text 不得被合并重排，位置不依赖时间、宽度、完成顺序或正文匹配。

宿主主线模式 SHALL 在排除推理项及 SessionStart 上下文详情项后取最近最多 50 项。详情模式包含这些独立条目并按全部逻辑项翻页。两个模式各自保持同一原生 cursor 的顺序和明确模式，主线窗口预算只计该模式的可见项。正文范围读取 SHALL 与完整展示字节语义一致，不为每页重新拼接全部 summary parts。

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

hook_feedback SHALL 位于真实 native cursor，保持候选答复/反馈/可选真实纠正/续答的提交顺序，不使用用户 InputID 或伪工具锚点。仅显示已授权且 durable 的规范正文，不包含原始脚本双流。live committed 与 restored 必须同源同 cursor，压缩及详情分页不重编号原历史；现有窗口/正文页预算继续适用。continued 决定只产生待交付检查状态，不制造 native cursor；一记录或三记录交付 Sync/finalize 后才按实际 native entries 发布反馈及真实输入 cursor，不能重用 sample cursor 或把用户纠正合并进 Hook 条目。

#### Scenario: Render a checked continuation consistently
- **WHEN** 模型先答复、Stop 反馈与真实纠正同批被提交、模型再答复，随后重启恢复
- **THEN** live/restored 均保留答复/Hook/user/续答同一顺序、来源、正文与 cursor；recall 只包含真实用户纠正，不包含 Hook 正文

#### Scenario: Keep repeated answers at separate cursors
- **WHEN** 两次候选答复文本恰好相同而中间有 Hook 反馈
- **THEN** 保留两个原生位置，不按文本相等合并或替换旧答复

SessionStart SHALL 在 Ctrl+O 产生独立有界详情摘要行。用户明确读取该条目时，复用现有有界正文范围读取返回实际保存的规范文本。位置 SHALL 使用持有 native envelope 的关闭事实真实 seq 和原生 item 下标。startup/resume 项在同批真实用户输入之前，compact 项紧随实际 checkpoint 边界并位于其后原生输入、工具续写或 Stop 交付之前。手动压缩按真实 CompactionID/scope 标识，自动压缩保留真实 scope。两种来源均不占用户召回身份、Stop sample 次序或工具锚点。摘要行只携带有界来源和安全统计，宿主按既有终端安全规则转义正文副本。

#### Scenario: Read an authorized compact context detail
- **WHEN** 用户查看带有 compact 上下文的成功压缩，随后在 Ctrl+O 定位该项并读取正文
- **THEN** 详情列表按实际 seq 显示摘要边界和独立来源行，已授权正文按实际保存字节语义分页。主线和用户召回不出现该正文

#### Scenario: Keep identical bodies at distinct positions
- **WHEN** compact 和 startup/resume 的两个上下文具有相同正文并分别提交
- **THEN** 详情分别锚定两条实际关闭事实的位置与来源，读取不按正文相等合并或重编号

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

启动与恢复 SHALL 完整核对 native 与 tool facts 的锚定。实时 commit SHALL 仅投影此次已提交原生条目及其相关事实，原子更新受影响的条目和索引。不可见原生条目仍按真实 ordinal 推进，复合 Stop 交付及 SessionStart 首轮复合提交分别按各自原子批次发布。已有条目的 cursor 和正文不会因追加或计划归并而重算或重编号。当前工具事实错配的拒绝同样覆盖无界面宿主。

#### Scenario: Finalize streamed text without duplication
- **WHEN** 推理和文本 sample 成功 Sync/finalize
- **THEN** 按真实映射替换临时范围，恢复后内容和 cursor 相同，无重复

#### Scenario: Disconnect during arguments
- **WHEN** 工具参数未完整时断线且 sample 未进入提交
- **THEN** 不展示 ready/执行，本 sample 明确未保存，先前 durable 输入与结果保留

#### Scenario: Commit a selection outside the latest list page
- **WHEN** 用户正在阅读的活动块提交后不在最新 50 项详情页
- **THEN** 仍通过精确 cursor 读取相同正文，不退回正文搜索或错选其他项

#### Scenario: Append an atomic transcript suffix
- **WHEN** 已有长历史的会话完成一个复合 Stop 提交和后续带工具调用的 sample
- **THEN** 每次实时发布只访问新原生位置及相关事实，读者只能观察完整发布，恢复后 cursor、顺序、正文和工具关联一致

SessionStart 的现时详情 SHALL 使用已确认 Sync 并完成 native finalize 的事实与真实 cursor 构建。首轮批次的上下文位置与真实输入位置一次发布，随后才发布各自的有界宿主通知。compact 上下文是 checkpoint 已安装后的独立完成事实，原有摘要及其 cursor 保持，详情按实际关闭事实插入。过期实例、未应用关闭或 Sync 结果不确定 SHALL 不创建已保存详情。恢复使用同一规范投影，不重新读取脚本或依赖历史信任。现有 headless 协议只映射其当前公开事件，不透传此详情或正文。

#### Scenario: Publish the initial context and input together
- **WHEN** 查询与带上下文的首个输入 commit 竞争，随后恢复同一会话
- **THEN** 可见快照要么同时含两部分，要么两部分均不可见。实时与恢复的 seq、文本、来源、真实 InputID 及阅读锚点一致

#### Scenario: Keep unconfirmed context out of the detail index
- **WHEN** 关闭事实尚未确定成功，或者旧会话的已保存通知在切换后迟到
- **THEN** 当前详情不新增已保存行。恢复时以实际已验证的持久化事实为准
