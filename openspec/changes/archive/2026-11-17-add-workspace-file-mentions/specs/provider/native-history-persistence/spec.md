## ADDED Requirements

### Requirement: Native file inputs use one mandatory current metadata shape

两家 Provider 的唯一当前 user_inputs 关联元数据 SHALL 包含必需、非 null 的 files 数组；无文件时为 []。每个数组项 MUST 关联本次 InputIDs 中一个真实身份和其非空有序文件附件集合，同身份不得重复出现，组顺序与输入顺序一致。文件集合 MUST 校验引用、实际范围、截断、大小与内容派生 digest，最多八项；既有 Skill 关联保持独立。缺失必需字段、未知字段、重复成员、空集合、错关联或不合法冻结数据 MUST 整体拒绝。

files 项 SHALL 只包含 input_id 和 attachments。附件的必需字段为 path、selection、total_lines、actual_start_line、actual_end_line、ends_mid_line、truncated、content、digest；selection 只接受 whole 或带必需 start_line/end_line 的 lines，拒绝不属于当前 kind 的字段。digest MUST 为除自身外全部附件字段 canonical bytes 的小写 SHA-256。原生文件文本 SHALL 是固定标题 `# Attached workspace file\n`、同一不含 digest 的 canonical JSON 和末尾换行，不含格式版本。total_lines、原始 LF 行数、请求/实际范围及部分末行/完整性 MUST 单独交叉验证；一致性 digest 不替代这些语义检查。

每条原生用户消息 SHALL 按原始文字、可选 Skill、按引用顺序排列的文件包装编码；Anthropic 使用自身 text blocks，OpenAI 使用自身 input_text parts。strict codec MUST 校验类型、数量、角色、输入关联和全部包装 bytes 一致；非空文件集合必须恰好等于原文解析得到的完整有序唯一引用集合，不接受部分、额外项或乱序。原文字面 @ 且 files 为空 SHALL 合法；恢复不能据此自行激活附件。

本变更 SHALL 直接替换唯一当前结构和不可变 current fixture，不增加 entry kind、外层 Session record、版本标记、旧 reader、迁移器或可选 activation record；旧开发结构缺失 files 必须失败关闭。

#### Scenario: Encode text Skill and files in the same native input
- **WHEN** 一个合法输入包含原文、Skill 和两个文件附件
- **THEN** 两家分别编码四个有序文本块，与共享关联元数据一致，保持同一真实 InputID

#### Scenario: Reject a missing semantic field
- **WHEN** 存储的 user_inputs metadata 缺失 files，即使其余字段和 checksum 合法
- **THEN** 当前 codec 拒绝整份历史，不选择旧 reader、不 repair 或重写 journal

#### Scenario: Reject a forged attachment block
- **WHEN** 文件元数据、原引用、块数量/顺序或原生包装正文互不一致
- **THEN** 恢复事务性失败，无部分 Conversation、来源读取或外部副作用

#### Scenario: Reject an internally inconsistent range with a valid digest
- **WHEN** checksum、digest 与原生文本相互一致，但源总行数、实际范围、部分末行或完整性与正文冲突
- **THEN** 在 repair 前拒绝整份历史，journal bytes 不变，不把摘要校验当作语义证明

#### Scenario: Reject a partial reference set
- **WHEN** 原文有两个不同显式文件引用，但非空 metadata 和原生块只保存其中一项
- **THEN** 当前恢复拒绝，不能把遗漏项解释成无文件的 literal 输入

### Requirement: Native file attachments restore without source access

附件 SHALL 在现有用户输入 batch 成功 Sync 后 finalize。恢复、离线投影和旧 turn 本地收口 MUST 只消费实际已提交的文件事实，不读取来源、不重新截断、不改变包装、不自动执行工具或请求 Provider；尚未保存的队列不能从原文 @ 猜测恢复。实时候选重建与 Stop 真实 steering SHALL 复用已准备的冻结附件，不重读或重新截断；后续 Provider 采样只通过既有目标 sample 预检和 durable 提交后的正常 turn 生命周期进行。用于校验的纯内存包装比对不得替换存储 native 文本。

所有 metadata 和返回值 MUST 深拷贝。原文与文件内容不允许通过语义投影、RuntimeEvent、UI 或当前磁盘反向补全；来源删除、修改或当前工作区变化不能改变历史附件。

#### Scenario: Restore after deleting every mentioned file
- **WHEN** 含文件输入已 Sync，来源随后删除，进程从固定当前 bytes 恢复
- **THEN** 历史原文、文件包装和 native 顺序逐字节保留，来源读取与 Provider 网络调用为零

#### Scenario: Crash after input Sync before sampling
- **WHEN** 文件输入已 durable 而没有后续 sample，之后恢复两次
- **THEN** 只按既有中断规则关闭旧 turn，不重读、不重发输入，不重复追加事实

#### Scenario: Mutate a metadata copy
- **WHEN** 消费者修改 getter 或历史投影返回的文件集合、字段或正文
- **THEN** 已冻结原生历史、其他副本和后续请求保持不变

### Requirement: File continuations preserve native and cache equivalence

相同配置、ThreadID、项目/Skill/工具快照、实际已提交事实、候选输入及实际操作捕获的完整推理设置下，文件附件的 uninterrupted/restored 下一完整 canonical request、native item 次序、opaque bytes、context footprint/revision、内容分段与请求/native/profile fingerprints SHALL 相同；覆盖普通输入、工具 steering、Stop 交付及 checkpoint 后窗口。文件附件 MUST 不改变 Anthropic thinking/signature/redacted 或 OpenAI reasoning/encrypted content/phase。

运行时 effort override 与 selection_revision SHALL 继续只存在于进程，恢复先使用本次启动配置并重置序号。等价验证 MUST 在两侧显式选定相同 wire 设置，不能从附件、摘要、旧 UI 或 journal 推断旧选择，不得为此新增策略 record 或 metadata。相同设置但来源/序号不同 MUST 不改变请求身份；恢复后未重选而实际设置不同，只保证历史附件/native/opaque 保真，不承诺下一请求等价。

Anthropic cache marker/TTL、OpenAI thread key 及 WebSocket full/incremental 派生 MUST 各自验证。完整逻辑请求等价不能替代实际 wire golden，也不表示 full 与 incremental 帧相同。

#### Scenario: Compare both Provider continuations
- **WHEN** 含文件、Skill、工具配对和 opaque reasoning 的相同固定事实分别直接续写与恢复续写，且两侧在新操作前显式选定相同推理设置
- **THEN** 完整请求 bytes、native 顺序、footprint/revision 和 fingerprints 一致，opaque 内容保真

#### Scenario: Compare a live incremental and restored full request
- **WHEN** 连续 OpenAI 连接使用 incremental，而重启后使用 full
- **THEN** 独立编译的完整逻辑请求相同，各自 wire 匹配 golden，附件原生前缀/suffix 可以精确重组

#### Scenario: Reapply a process-only effort before comparing file continuations
- **WHEN** uninterrupted 路径已选择 low，恢复路径先回到启动 high，再显式选择 low，且两侧选择序号或来源不同
- **THEN** 相同后续文件输入的完整请求及各指纹一致，journal 不含 effort 设置，已保存附件不重读

#### Scenario: Restore files without reapplying an old effort
- **WHEN** 以启动 high 恢复一个曾在进程内选择 low 且含文件附件的会话，不执行重选
- **THEN** 文件、原生顺序和 opaque bytes 保真且不自动请求；下一显式操作使用 high，不能以请求不同误判附件损坏或改写历史
