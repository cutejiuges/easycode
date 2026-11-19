## Purpose

让用户通过互动聊天显式命名当前根会话，并在下次恢复时辨认同一会话。标题作为可验证的宿主元数据持久化，模型上下文、权限和恢复身份分别由原有事实源决定。

## ADDED Requirements

### Requirement: Users can name the current interactive session locally

互动 TUI SHALL 支持 `/rename <名称>` 行内保存，以及空闲时的裸 `/rename` 打开单行编辑框。界面已知有活动操作或等待输入时，裸命令 SHALL 提示 `session_busy` 并保留草稿。编辑框 SHALL 使用当前已确认标题预填，未命名时为空。普通 Enter 提交，提交前的 Esc 或 Ctrl+C 取消编辑并恢复之前的聊天草稿与光标。审批、提问等既有输入所有者 SHALL 优先接收输入，包括编辑框打开后收到的请求。无效标题或可修正的保存拒绝 MUST 保留可编辑的用户输入并显示安全英文错误。成功时当前聊天 SHALL 显示已确认标题，并以简短状态反馈保存结果。

标题 SHALL 是去除首尾 Unicode 空格分隔符（`Zs`）后非空的单行合法 UTF-8，按规范文本计算最多 256 bytes，保留内部空格、大小写与 Unicode 内容。终端控制字符（C0、DEL、C1）、Unicode 行/段分隔符及双向控制符 MUST 拒绝，包括外围的非法字符。控制命令参数中的 `@`、引号和其他普通字符 SHALL 按标题文本解释。错误 MUST NOT 回显非法输入。

识别到精确首词 `/rename` 后，系统 SHALL 保留命令词末尾之后的原参数做校验。参数为空或全由 `Zs` 组成时为裸命令。检测到禁用字符的参数 MUST 在本地拒绝，且不得转为裸命令、文件引用或普通聊天输入。普通 Enter 提交识别出的命令，反斜杠仍是标题字符。标题编辑框 SHALL 限制原始编辑缓冲为 1 KiB，并在收到已解码粘贴时原子插入或整体拒绝；粘贴中可见的禁用字符、Alt+Enter 和 Ctrl+J 不能插入换行或触发保存。标题值校验适用于宿主实际收到的解码文本。

标题允许重名。命名 SHALL 在首轮前和恢复后均可用；未输入有效标题时不能通过编辑框提交空值。互动命令及其草稿 MUST NOT 进入模型输入、输入回顾、文件引用或 Skill 准备。一次性或 stream-json 宿主的聊天文本 `/rename ...` 仍按普通文本处理，外部 JSONL v1 不新增命名控制。

#### Scenario: Inline rename before the first turn

- **WHEN** 用户在新会话中输入 `/rename Parser @work 复核` 并提交
- **THEN** 当前会话确认同名标题，不分配 turn、读取附件或发起模型请求

#### Scenario: Reopen or cancel the editor

- **WHEN** 已命名会话打开裸 `/rename` 的编辑框并按 Escape
- **THEN** 现名曾作为可编辑初值，关闭后恢复聊天草稿和光标，标题与 journal bytes 保持原值

#### Scenario: Reject hostile or empty input

- **WHEN** 标题为空、非法 UTF-8、包含换行或 bidi 控制符，或规范化后超过 256 UTF-8 bytes
- **THEN** 编辑内容仍可纠正，返回安全英文错误且无持久化或模型输入

#### Scenario: Preserve literal command arguments and the one-line editor

- **WHEN** 用户普通 Enter 提交带尾部反斜杠的标题，或者从粘贴得到尾部 Tab 或换行的 `/rename` 命令
- **THEN** 合法反斜杠按原文保存，带控制字符的命令在本地失败，保留草稿且不打开编辑框或提交给模型
- **THEN** 标题编辑框拒绝含禁用字符或超出缓冲上限的粘贴，现有编辑内容和光标保持原值，未发生保存

### Requirement: Title confirmation follows authoritative durable admission

每个标题提交 SHALL 绑定当前会话和唯一控制请求。会话单输入 owner MUST 仅在接受命令、没有活动 turn 或压缩、且等待输入队列为空时接纳写入；活动工具、问题、Stop 续轮和终态后的资源清理期间同样属于忙状态。忙时 SHALL 返回 `session_busy`，不排队或中断活动操作。等于当前规范标题的合法空闲请求 SHALL 成功且不追加记录。

写入 MUST 复用持有 root exclusive lease 的现有单 writer。接纳前的请求取消或会话关闭 MUST 不写入。一旦接纳，关闭或调用方取消不能放弃等待确定结果。只有追加时 Sync 成功才能安装新标题、返回持久化成功和更新界面。提交状态无法确认 MUST 失败关闭，保留最后已确认的标题并结束该会话的写入资格，不自动重试。过期回执不得改变后续状态。

保存中 Ctrl+C SHALL 请求退出和取消尚未接纳的请求。正常键盘退出 SHALL 在收到匹配结果（含未交付错误）或控制流关闭后退出界面；收到不确定结果或失联后，即使草稿非空也必须能够关闭，且不得接受后续输入或保存。信号或终端故障使界面先行停止时，宿主仍 SHALL 等待已接纳写入的确定结果和 writer 的最终 Sync/Close，再释放会话资源及 lease。等待超时 MUST NOT 放弃 owner 或自动重试写入。

#### Scenario: Do not rename across an active operation

- **WHEN** 命名请求到达时有活动 turn、压缩或已入队输入
- **THEN** 返回忙，标题不排队，原活动操作继续由同一 owner 收口

#### Scenario: Wait for Sync after admission

- **WHEN** 标题提交已被接纳，用户请求退出，而 journal Sync 仍在进行
- **THEN** 界面不能宣称提前成功或回滚，关闭等待原 writer 的确定结果并保留 lease 至清理完成

#### Scenario: Distinguish shutdown before admission from writer cleanup

- **WHEN** 调用方取消或聊天宿主在请求交付后、owner 接纳前开始关闭
- **THEN** 不追加标题，关闭可以在当前 owner 完成清理后正常结束
- **WHEN** 已接纳标题取得成功回执，但关闭时最终 Sync 尚未完成
- **THEN** 关闭超时不释放会话所有权，进程退出流程等待同一 writer 完成并保留超时结论

#### Scenario: Resolve an uncertain write on the next validated open

- **WHEN** 已写入的标题 batch 在 Sync 边界失败
- **THEN** 此次提交无成功确认，活动 writer 失败关闭，无自动重试
- **THEN** 后续正常打开以实际可验证的完整 committed journal 前缀为事实源

### Requirement: Titles are one strict root journal fact

唯一当前 Session 词汇 SHALL 包含 `session_title_updated`，payload 恰含必需规范 `title`。每次变化 SHALL 在唯一初始 metadata batch 之后以一个单条 batch 追加，仅适用于 root thread。`turn_id` 和 `parent_thread_id` MUST 缺省，显式空字符串也不合法。已提交标题 batch MUST 位于完整活动操作之间，不得穿插在活动 turn、压缩、工具 ledger、Hook 或恢复补偿的未闭合前缀中。加载与回放 SHALL 拒绝未知/重复/缺失/null 字段、非规范标题或非法位置，在恢复 repair、补偿或 Provider 连接之前返回安全错误。

没有标题记录的合法会话 SHALL 显示未命名。有多次更新时，最大已提交序号的标题 SHALL 成为当前值。合法 EOF 半行或未完成尾 batch 遵循已有 committed 前缀与显式 repair 契约。现有 envelope、初始 metadata 和其他 record 编码继续适用。标题仅供宿主投影和发现，不参与 Provider 原生 item、请求或 cache fingerprint，不作为 transcript 消息、工具/Hook 输入、诊断正文或遥测。

#### Scenario: Read the latest complete title after interruption

- **WHEN** journal 有两次完整命名，末尾第三次命名为尚未完整提交的半行
- **THEN** 只读验证返回第二个标题且保留全部原 bytes，正式恢复按现有流程校验后才可修复尾部

#### Scenario: Reject a misplaced committed title before repair

- **WHEN** 一个完整且 checksum 正确的标题 batch 位于活动 turn 或压缩内，末尾另有可修复半行
- **THEN** 校验失败，全部原 bytes 保留，无模型请求、repair 或恢复补偿

#### Scenario: Reject a complete line claiming an impossible title batch

- **WHEN** EOF 前一条换行终止且 checksum 正确的标题记录宣称 `batch_size` 大于 1，或携带 `turn_id` / `parent_thread_id` 字段，即使值为空字符串
- **THEN** 加载报告损坏并保留全部 bytes，不能把该行判断为可修复的未完成 batch

#### Scenario: Continue the original native conversation after rename

- **WHEN** 用户重命名并恢复任一 Provider 的会话，然后提交同一后续输入
- **THEN** 会话身份保持一致，同一 transport 形态下的完整 canonical request、原生 item 顺序和 cache fingerprint 与相同对话未改名时等价，OpenAI WebSocket 恢复仍遵循新连接完整请求规则
