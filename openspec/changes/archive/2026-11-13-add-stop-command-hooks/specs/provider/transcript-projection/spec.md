## MODIFIED Requirements

### Requirement: Transcript projection preserves ordered text and tool anchors

两家 Provider SHALL 从已提交 native history 返回强类型用户文本、assistant 文本、独立推理项、工具调用锚点及具有独立来源的 Hook 反馈条目，保持 native entry 和内部 item/block 顺序。推理只含 provider/reasoning-display 白名单正文或无正文 unavailable；OpenAI 非空 summary_text parts 保留原序和空白，两个 LF 分隔，零字节段不加分隔，不改变原生 parts。文本 MUST 原样保留空格与换行，用户关联保留 InputID，调用锚点与当前 thread ready fact 唯一对应。推理/text/call/text 不得被合并重排，位置不依赖时间、宽度、完成顺序或正文匹配。

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

hook_feedback SHALL 位于真实 native cursor，保持候选答复/反馈/可选真实纠正/续答的提交顺序，不使用用户 InputID 或伪工具锚点。仅显示已授权且 durable 的规范正文，不包含原始脚本双流。live committed 与 restored 必须同源同 cursor，压缩及详情分页不重编号原历史；现有窗口/正文页预算继续适用。continued 决定只产生待交付检查状态，不制造 native cursor；一记录或三记录交付 Sync/finalize 后才按实际 native entries 发布反馈及真实输入 cursor，不能重用 sample cursor 或把用户纠正合并进 Hook 条目。

#### Scenario: Render a checked continuation consistently
- **WHEN** 模型先答复、Stop 反馈与真实纠正同批被提交、模型再答复，随后重启恢复
- **THEN** live/restored 均保留答复/Hook/user/续答同一顺序、来源、正文与 cursor；recall 只包含真实用户纠正，不包含 Hook 正文

#### Scenario: Keep repeated answers at separate cursors
- **WHEN** 两次候选答复文本恰好相同而中间有 Hook 反馈
- **THEN** 保留两个原生位置，不按文本相等合并或替换旧答复
