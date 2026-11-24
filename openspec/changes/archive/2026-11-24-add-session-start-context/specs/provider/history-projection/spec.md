## MODIFIED Requirements

### Requirement: Project committed native history into ordered semantic turns

每个受支持的 Provider SHALL 将已成功提交的原生历史投影为按提交顺序排列的语义 turn。每个语义 turn MUST 使用真实 turn identity，并以强类型有序消息保留用户与 assistant 可见文本；用户消息包含 InputID，同 turn 中不同用户输入不得合并，视图 MUST 标识原生历史所属的 Provider family。

投影 SHALL 只读取已提交历史；未提交 staging 和部分 assistant 数据 MUST NOT 出现在视图中；失败、取消、timeout 或提前 EOF 不得删除此前独立提交的用户输入及成功 sample。空会话 SHALL 产生来源明确且 turn 列表为空的有效视图。

#### Scenario: Project an empty conversation

- **WHEN** 任一受支持 Provider 的会话尚未提交任何 native history
- **THEN** 投影结果标识该 Provider family，且语义 turn 列表为空

#### Scenario: Preserve committed turn order

- **WHEN** 会话已按顺序成功提交多个 turn
- **THEN** 投影结果按相同顺序包含每个 turn 的用户文本和 assistant 可见文本

#### Scenario: Exclude an uncommitted turn

- **WHEN** 一个新 turn 仍在 streaming，或最终以失败、取消、timeout 或提前 EOF 结束
- **THEN** 投影保留该 turn 已提交的用户输入及成功 sample，不包含未提交 assistant 输出

#### Scenario: Preserve interleaved messages in one turn
- **WHEN** 同一真实 turn 依次提交用户、assistant sample、中途用户和最终 assistant
- **THEN** 投影保持四者消息顺序与同一 turn 归属，恢复不能以每条用户消息推断新 turn

已提交 hook_feedback SHALL 具有独立的 Hook 来源语义角色及真实 turn/source sample 关联，不能制造 InputID、用户召回文本或助手完成。其正文只来自已验证 native 事实，不重新读取脚本。既有用户/assistant 消息的原始顺序与身份保持。仅有 continued 冻结决定而 native 尚未交付时，语义历史不得提前出现反馈；真实纠正随三记录交付保存后，保留原 InputID/Skill 附件归属，不与 Hook 来源合并。

#### Scenario: Keep a hook message distinct from real user input
- **WHEN** 同一 turn 的候选答复后保存 Stop 反馈与真实纠正，再保存模型续答
- **THEN** 投影保持 assistant/Hook/user/assistant 顺序及真实用户身份；反馈不成为真实 user 或 recall 条目

已提交的 SessionStart 上下文 SHALL 独立标识来源并保留原生提交顺序。startup/resume 上下文位于实际所属语义 turn 的第一条真实 initial 输入之前。compact 上下文 SHALL 以有序的 checkpoint 关联项投影，自动来源可关联实际作用域，手动来源以真实 CompactionID 和 checkpoint 位置锚定。语义 turn 始终使用真实身份，来源项保存获准且已提交的规范模型正文及其来源。上下文 MUST NOT 变成真实用户消息、用户召回条目、Stop 反馈或助手 sample。只有已应用的 native 内容产生该语义项。

#### Scenario: Keep the initial input association intact
- **WHEN** 已保存的 startup 或 resume 上下文和带有 Skill 或文件附件的首个真实输入同批提交
- **THEN** 语义历史保持上下文、用户及后续助手项的顺序，既有 InputID、Skill 和文件附件只关联真实输入

#### Scenario: Project manual compact context outside the turn list
- **WHEN** 手动压缩后已保存 compact 上下文，而用户尚未提交下一条真实输入
- **THEN** 完整语义视图保留实际 checkpoint 来源与顺序，不产生新的 turn、用户消息或助手 sample

### Requirement: Active window projection is distinct from complete history

完整历史和 transcript 投影 SHALL 保持原始用户/assistant 消息、真实 turn/InputID、原生提交 ordinal 和工具锚点，不因压缩删除或重新编号。系统 SHALL 另提供有效窗口的只读语义快照供规划，包括带独立来源的摘要文本与 checkpoint 后可见消息。summary MUST NOT 伪造用户/assistant turn。所有投影只从 Provider-native facts 派生且不得反向构造请求。压缩摘要详情 SHALL 从 Provider checkpoint 只读投影产生，app 以 Session sequence 插入边界，不把压缩输出追加到普通 transcript native ordinal。

#### Scenario: Preserve complete history and expose a shorter window
- **WHEN** 一个含工具的会话完成手动压缩
- **THEN** 原始 ProjectHistory/Transcript 消息和锚点保持不变，有效窗口快照只包含摘要与之后增量
- **THEN** 两种投影均无 I/O、不暴露 opaque reasoning，修改返回副本不影响窗口

#### Scenario: Render a checkpoint summary
- **WHEN** 宿主显示压缩摘要
- **THEN** 使用独立压缩边界和摘要详情，不生成假的用户 InputID、turn 或 assistant completion

有效窗口 SHALL 在已安装原生摘要之后及其后的真实历史之前，保留本 checkpoint 已提交的 compact 上下文。真实 turn 内的 startup/resume 上下文按原生位置参与有效窗口。后续 checkpoint 覆盖的旧上下文退出有效窗口，完整历史仍保留真实来源与位置供读取。既有 /resume 预览继续只选择真实用户及 assistant 文本，SessionStart 上下文不占正文或条数预算。

#### Scenario: Project a compacted window after both sources
- **WHEN** 首轮普通自动压缩后依次保存本 checkpoint 的 compact 上下文、当次 resume 上下文和真实输入
- **THEN** 有效窗口保留摘要、compact 上下文、resume 上下文和真实输入的内容顺序。完整历史仍可投影旧事实

#### Scenario: Preview a history with saved hooks
- **WHEN** 离线预览含两种 SessionStart 作用域及真实用户和助手文本
- **THEN** 验证覆盖全部原生事实，预览只返回真实用户和助手文本，且正文预算不会被 Hook 内容占用
