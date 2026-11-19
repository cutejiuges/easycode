## MODIFIED Requirements

### Requirement: Preview validates stored history under continuous short-lived ownership

预览 SHALL 接收明确的 canonical root thread ID，从安全 Repository 获取 descriptor-bound exclusive lease，在同一连续句柄所有权下读取并验证当前 Session、root 身份、record 顺序、native role/关联、checkpoint/source、压缩 usage/provenance 和标题事实，再由原生 Provider 离线投影正文。MUST NOT 使用 Catalog row、文件 mtime 或显示文本替代事实校验，也不得要求预览来源与当前聊天配置匹配。

预览 SHALL 在关闭 lease 和 Repository 成功后才发布显示结果；用户阅读期间不得持有 journal lease。不存在、busy、不支持的 family/wire、损坏记录或任何验证/关闭失败 SHALL 返回安全英文错误，不返回部分成功结果或未经完整校验的标题及正文。受支持 family/wire 的模型使用 journal 已验证 metadata，不要求网络凭据。

#### Scenario: Preview another supported provider without its credentials
- **WHEN** 当前聊天配置为 OpenAI，用户预览合法 Anthropic root thread
- **THEN** 使用该 journal 的来源 metadata 离线验证并投影，不需要 Anthropic API key，不调用网络

#### Scenario: Reject a busy or replaced journal
- **WHEN** 真实另一进程持有目标 lease，或目录/journal 在打开边界被替换为 symlink
- **THEN** 返回安全错误或继续使用已安全取得的原句柄，不读取或修改未授权目标，不等待猜测 owner 退出

#### Scenario: Release ownership before showing the result
- **WHEN** 预览验证和投影成功
- **THEN** 结果发布时其他进程可获得该 journal lease，UI 不保留句柄

#### Scenario: Read the verified title even if discovery is stale

- **WHEN** Catalog 最近一次协调看到的标题已经过时，且目标 journal 当前可在 lease 下完整验证
- **THEN** 预览返回本次实际读取的 committed 标题，并在关闭 lease 和 Repository 后发布
- **THEN** 无法完整验证或关闭资源时，预览只返回安全错误

### Requirement: Preview text is a bounded independent projection

预览 SHALL 按完整历史原始顺序返回最多前 128 条非空用户/assistant 消息，并保留 role、turn 和已有 InputID，正文总量最多 32768 UTF-8 bytes。最后一条超限时 SHALL 在 UTF-8 边界截断并标记该消息被截断，任何消息或正文被省略时 SHALL 明确 truncated。空消息不占条数，无可见文本返回有效空预览。预算只影响展示副本，不改变完整验证或 native history。

预览 MUST NOT 展示工具输入/输出、artifact、reasoning summary、thinking/signature/redacted 内容、encrypted content、phase、usage、原始 JSON 或未知 opaque 数据；压缩摘要不伪造普通 user/assistant 消息。安全显示的正文副本 SHALL 最多 196608 bytes，展开控制字符超过预算仍需明确省略。预览文本仅存在于当前预览内存，关闭即丢弃，不进入日志、遥测、Catalog、搜索字段或 Provider 请求。

预览 SHALL 将最后已验证的显式标题作为独立元数据字段返回，并按单行终端安全规则展示。未命名会话不展示标题。标题字段遵循会话标题自身的 UTF-8 大小上限，正文的消息数量、字节、截断标志和顺序均按正文单独计算。

#### Scenario: Bound a long multilingual preview
- **WHEN** 已命名会话的已验证历史含超过 128 条消息或 32768 bytes 的中文/emoji 正文
- **THEN** 返回独立标题、正文的原始有序前缀、合法 UTF-8 和准确省略标记，正文与转义展示均按原预算裁剪

#### Scenario: Exclude opaque and tool content
- **WHEN** 双 Provider fixture 同时包含可见文本、原生 reasoning、工具与压缩 checkpoint
- **THEN** 预览正文仅显示原始用户/assistant 文本，不能出现 opaque 或工具内容，也不以摘要替代完整历史

#### Scenario: Preview a named journal before its first turn

- **WHEN** 一个已命名的合法 root journal 尚无可见对话正文
- **THEN** 预览返回标题、空消息列表及正常的空正文提示
