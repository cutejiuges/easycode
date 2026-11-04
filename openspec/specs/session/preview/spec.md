# session/preview Specification

## Purpose

定义未启动聊天会话时对已存储 root thread 的按需只读预览，使宿主能在完整验证 Session 与 Provider 原生历史后展示有界可见文本，同时保证不修复、不补偿、不执行模型或工具，并释放全部临时资源。

## Requirements

### Requirement: Preview validates stored history under continuous short-lived ownership

预览 SHALL 接收明确的 canonical root thread ID，从安全 Repository 获取 descriptor-bound exclusive lease，在同一连续句柄所有权下读取并验证当前 Session、root 身份、record 顺序、native role/关联、checkpoint/source 及压缩 usage/provenance，再由原生 Provider 离线投影。MUST NOT 使用 Catalog row、文件 mtime 或显示文本替代事实校验，也不得要求预览来源与当前聊天配置匹配。

预览 SHALL 在关闭 lease 和 Repository 成功后才发布显示结果；用户阅读期间不得持有 journal lease。不存在、busy、不支持的 family/wire、损坏记录或任何验证/关闭失败 SHALL 返回安全英文错误，不返回未经完整校验的部分成功正文。受支持 family/wire 的模型使用 journal 已验证 metadata，不要求网络凭据。

#### Scenario: Preview another supported provider without its credentials
- **WHEN** 当前聊天配置为 OpenAI，用户预览合法 Anthropic root thread
- **THEN** 使用该 journal 的来源 metadata 离线验证并投影，不需要 Anthropic API key，不调用网络

#### Scenario: Reject a busy or replaced journal
- **WHEN** 真实另一进程持有目标 lease，或目录/journal 在打开边界被替换为 symlink
- **THEN** 返回安全错误或继续使用已安全取得的原句柄，不读取或修改未授权目标，不等待猜测 owner 退出

#### Scenario: Release ownership before showing the result
- **WHEN** 预览验证和投影成功
- **THEN** 结果发布时其他进程可获得该 journal lease，UI 不保留句柄

### Requirement: Preview never repairs compensates or executes

预览 MUST 保持所有 journal bytes、checksum 和 seq 不变；不得 Repair、Sync journal、启动 writer、追加中断终态或工具输出、恢复临时 grant、访问 workspace 工具目标、清理 artifact、执行工具或发起 Provider 请求。预览 MUST NOT 创建新 Session、正文索引、正文 artifact 或历史迁移。

可修复 EOF 半行/不完整尾 batch SHALL 只投影经过验证的 committed 前缀并标记 incomplete。已提交输入或 sample SHALL 按真实身份保留；未提交输出不得补造。中段损坏、source/usage 不一致 SHALL 失败关闭。预览成功不构成未来恢复承诺。

#### Scenario: Inspect an interrupted tool conversation
- **WHEN** journal 以合法 ready、started、已完成 result 未配对输出或 input-only 状态结束
- **THEN** 仅展示真实已提交可见文本，工具/模型调用为零，前后 journal bytes 完全一致

#### Scenario: Inspect a recoverable half-line tail
- **WHEN** journal 只有合法 committed 前缀加 EOF 半行
- **THEN** 返回前缀预览并标记 incomplete，半行保留且不追加任何记录

#### Scenario: Reject invalid checkpoint evidence
- **WHEN** journal 结构合法但 checkpoint source 或压缩 usage/provenance 与原生事实不一致
- **THEN** 预览失败，不输出部分正文，全部 journal bytes 保持不变

### Requirement: Preview text is a bounded independent projection

预览 SHALL 按完整历史原始顺序返回最多前 128 条非空用户/assistant 消息，并保留 role、turn 和已有 InputID，正文总量最多 32768 UTF-8 bytes。最后一条超限时 SHALL 在 UTF-8 边界截断并标记该消息被截断，任何消息或正文被省略时 SHALL 明确 truncated。空消息不占条数，无可见文本返回有效空预览。预算只影响展示副本，不改变完整验证或 native history。

预览 MUST NOT 展示工具输入/输出、artifact、reasoning summary、thinking/signature/redacted 内容、encrypted content、phase、usage、原始 JSON 或未知 opaque 数据；压缩摘要不伪造普通 user/assistant 消息。安全显示的正文副本 SHALL 最多 196608 bytes，展开控制字符超过预算仍需明确省略。预览文本仅存在于当前预览内存，关闭即丢弃，不进入日志、遥测、Catalog、搜索字段或 Provider 请求。

#### Scenario: Bound a long multilingual preview
- **WHEN** 已验证历史含超过 128 条消息或 32768 bytes 的中文/emoji 正文
- **THEN** 返回原始有序前缀、合法 UTF-8 和准确省略标记，正文与转义展示均不超预算

#### Scenario: Exclude opaque and tool content
- **WHEN** 双 Provider fixture 同时包含可见文本、原生 reasoning、工具与压缩 checkpoint
- **THEN** 预览仅显示原始用户/assistant 文本，不能出现 opaque 或工具内容，也不以摘要替代完整历史

### Requirement: Preview cancellation has a verifiable completion boundary

预览 SHALL 支持取消，在读取、逐条原生验证和结果发布边界检查取消；取消成功或失败结果 MUST 在临时 owner 清理完成后返回。输入取消早于请求接收时不得开始读取。过期结果只能被丢弃，不能更新另一个 thread 的预览。后续正式恢复 SHALL 重新获取 lease 并重读事实源，不复用预览内存作为 native history。

#### Scenario: Cancel while native validation is in progress
- **WHEN** 故障注入在逐条校验之间阻塞并收到取消
- **THEN** 恢复执行后返回取消、释放 lease，未修改 journal，也不向 UI 发布成功正文

#### Scenario: Change the journal after preview
- **WHEN** 预览结束后另一个 owner 更新 journal，随后用户请求恢复
- **THEN** 正式恢复读取并验证最新事实，不从旧预览构造请求或跳过恢复检查
