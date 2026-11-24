## MODIFIED Requirements

### Requirement: Title confirmation follows authoritative durable admission

每个标题提交 SHALL 绑定当前线程和唯一控制请求。会话单输入 owner MUST 仅在接受命令、没有活动 turn 或压缩、且等待输入队列为空时接纳写入；活动工具、问题、Stop 续轮和终态后的资源清理期间同样属于忙状态。忙时 SHALL 返回 `session_busy`，不排队或中断活动操作。等于当前规范标题的合法空闲请求 SHALL 成功且不追加记录。

写入 MUST 复用持有当前线程 exclusive lease 的现有单 writer。接纳前的请求取消或会话关闭 MUST 不写入。一旦接纳，关闭或调用方取消不能放弃等待确定结果。只有追加时 Sync 成功才能安装新标题、返回持久化成功和更新界面。提交状态无法确认 MUST 失败关闭，保留最后已确认的标题并结束该会话的写入资格，不自动重试。过期回执不得改变后续状态。

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

#### Scenario: Rename a branch independently of the source
- **WHEN** 用户在新分支或恢复的分支中通过 `/rename` 保存有效标题，源线程有自己的标题
- **THEN** 仅分支 writer 在自己的 lease 下追加单条标题事实。该追加 Sync 后确认分支标题，源 journal 与源标题保持已有事实

### Requirement: Titles use strict thread-local journal facts

唯一当前 Session 词汇 SHALL 包含 `session_title_updated`，payload 恰含必需规范 `title`。根线程每次改名 SHALL 在唯一初始 metadata batch 之后以一个单条 batch 追加。分支每次改名 SHALL 在本线程已验证的 `thread_forked` 之后以同样的单条 batch 追加。`turn_id` 和 `parent_thread_id` MUST 缺省，显式空字符串也不合法。已提交标题 batch MUST 位于完整活动操作之间，不得穿插在活动 turn、压缩、工具 ledger、Hook 或恢复补偿的未闭合前缀中。加载与回放 SHALL 拒绝未知/重复/缺失/null 字段、非规范标题或非法位置，在恢复 repair、补偿或 Provider 连接之前返回安全错误。

根线程没有已提交标题时 SHALL 显示未命名。分支 SHALL 使用其创建边界中的必填规范标题初始化本线程标题。对任一线程，存在已提交的本线程改名时，最大序号的改名 SHALL 成为当前值。继承前缀中的标题事实仅用于验证原记录，禁止作为本线程标题的回退。合法 EOF 半行或未完成尾 batch 遵循已有 committed 前缀与显式 repair 契约。现有 envelope、初始 metadata 和其他 record 编码继续适用。标题仅供宿主投影和发现，不参与 Provider 原生 item、请求或 cache fingerprint，不作为 transcript 消息、工具/Hook 输入、诊断正文或遥测。

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

#### Scenario: Use only titles belonging to the current branch
- **WHEN** 父线程有已提交标题，子线程携带用户提供或创建时派生的标题，且创建后没有已提交改名
- **THEN** 子线程显示其自身已保存的创建标题。父线程后来改名不改变分支标题，分支后续已提交改名优先于其创建标题，并在恢复及索引重建时保持相同

## RENAMED Requirements

- FROM: `### Requirement: Titles are one strict root journal fact`
- TO: `### Requirement: Titles use strict thread-local journal facts`
