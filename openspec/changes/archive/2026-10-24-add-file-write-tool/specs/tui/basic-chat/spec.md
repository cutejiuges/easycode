## MODIFIED Requirements

### Requirement: File approval uses a Claude-style deterministic overlay

TUI SHALL将typed Patch approval request投影为居中的modal overlay，按稳定顺序显示workspace相对路径、稳定英文风险摘要、完整有界unified diff和内核提供的三个选择：`Yes`、`Yes, allow all edits during this session`、`No`。初始焦点 SHALL为`No`；上下方向键在允许选择间循环，Enter提交当前选择，TUI不得根据终端尺寸或Provider来源改变决定语义。

只有`No`聚焦时Tab SHALL切换到拒绝反馈编辑器，提示用户说明“应该怎么做”；反馈为合法UTF-8，去除外围空白后至多16 KiB。反馈编辑器的Enter提交deny，Esc先退出反馈编辑器并保留overlay，再次Esc按中断语义处理。`Yes`与session allow不接受反馈，也不得提供候选内联编辑。Runtime声明的decision集合缺失、未知或与当前overlay identity不一致时，TUI必须失败关闭而不是猜测默认批准。

固定尺寸不足以完整显示diff时，overlay SHALL在自身viewport中滚动并持续显示路径、选择和当前位置；不得截断diff后仍允许批准。颜色不可用时仍须用文本、边框和焦点标记区分新增、删除和当前选择。resize只改变布局，不改变request、草稿、反馈或焦点语义。

Write approval SHALL 沿用同一 modal、默认 No、反馈、键盘隔离、滚动与 resize 契约，但使用明确的 Create file / Overwrite file 标题。create 展示完整新内容或全新增 diff、空文件说明、行/字节数和缺失父目录清单；overwrite 展示完整变化 diff。内容中控制字符必须可见转义而不执行终端控制。

Write 的 session allow 标签 SHALL 明确仅授权本 Runtime 的创建和覆盖，不沿用 Patch 的 all edits 标签，也不能授权 Patch/Exec。TUI MUST 仅渲染已验证的封闭 presentation，不访问文件系统、不修改候选、不自行推断操作或扩充 decisions。approval resolved 只表示决议已收口，不表示文件写入成功。

#### Scenario: Render an approval at a normal terminal size
- **WHEN** TUI在固定尺寸收到合法Patch approval request
- **THEN** snapshot包含相对路径、完整diff、三个有序选择和默认聚焦的`No`
- **THEN** snapshot不包含workspace绝对路径、完整未变文件或Provider wire

#### Scenario: Allow one patch
- **WHEN** 用户聚焦`Yes`并按Enter
- **THEN** TUI提交匹配identity和fingerprint的`allow_once`response且禁用重复提交

#### Scenario: Allow edits for the current Runtime session
- **WHEN** 用户聚焦session选项并按Enter
- **THEN** TUI提交`allow_session`，其标签明确限定为当前session
- **THEN** TUI不写用户配置、项目配置或持久权限规则

#### Scenario: Deny with guidance
- **WHEN** 用户在`No`上按Tab、输入合法反馈并提交
- **THEN** TUI提交deny与该反馈，聊天composer内容不变
- **THEN** overlay关闭后模型可通过新的tool call提出修订方案

#### Scenario: Scroll a large bounded diff
- **WHEN** 完整合法diff超过当前overlay viewport但未超过内核大小边界
- **THEN** 用户可以滚动查看全部diff且选择始终可达
- **THEN** resize后显示内容仍来自同一candidate fingerprint

#### Scenario: Fail closed on an unsupported decision set
- **WHEN** approval event缺少`No`、含未知decision或identity与当前活动turn不匹配
- **THEN** TUI不发送allow response并以安全错误请求中断当前turn

#### Scenario: Render a new empty file
- **WHEN** Write create 候选 content 为空并需要创建父目录
- **THEN** 固定 snapshot 包含 Create file、空文件说明、全部目录、Write 专属选择和默认 No

#### Scenario: Render overwrite distinctly
- **WHEN** Write 覆盖一个已有空文件或非空文件
- **THEN** 标题为 Overwrite file，显示同一候选的完整 diff，不能将空 base 显示成 create

#### Scenario: Preserve draft while scrolling Write preview
- **WHEN** 在小窗口中滚动、resize 或输入拒绝反馈
- **THEN** 聊天草稿保持原样且按键不会提交给模型，identity 和 fingerprint 不改变

#### Scenario: Keep rendering free of filesystem effects
- **WHEN** 多次渲染同一个 Write approval
- **THEN** 不读取目标、不探测目录、不重新生成候选或授予权限
