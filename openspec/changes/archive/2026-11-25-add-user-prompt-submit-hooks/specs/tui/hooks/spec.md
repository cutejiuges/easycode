## ADDED Requirements

### Requirement: Prompt check review and return preserve the submitted draft

TUI SHALL 在 `/hooks` 展示 `UserPromptSubmit` 声明。默认拒绝的审查说明完整命令、来源与身份、只读禁网限制、将读取实际明文的事实，以及当前声明一经信任也适用于本会话实例中后续匹配的提交。普通输入与用户批准的实施正文都适用。终端达到现有无法安全审查的最小尺寸限制时，继续提供拒绝及调整尺寸后的审查路径。取消开始即移除此检查的信任 overlay，迟到的响应不得恢复授权界面。运行、拦截、错误和取消使用独立于普通工具的固定摘要，可携带安全声明来源及 ID。

被拦截的真实输入 SHALL 留在既有本地 returned 列表，保留原有终端安全的预览和内存召回语义，无持久化 transcript 项。回执不得自动修改当前 composer。仅在用户按 `Ctrl+R` 时按既有行为取回最早可取回的文本，已有草稿时在其前插入。合并超限时保持草稿与 returned 项。`Ctrl+D` 丢弃本地项。检查通知必须匹配当前已领取的 InputID 和实际 TurnID，旧通知不得改写其他项或焦点。Plan 审阅确认后的实施检查使用本地保留的同一实施文本，原有提交后才进入实施召回的语义保持。

#### Scenario: Edit a blocked prompt while preserving another draft

- **WHEN** 检查期间用户开始输入新的正文，而先前输入被拦截
- **THEN** 当前正文和光标保留，并可选择取回此前被拦截的输入。Shell 输出及被拦截文本均未进入 transcript

#### Scenario: Ignore a review from a cancelled or resolved input

- **WHEN** 输入已被取消或返还，随后到达旧审查或执行通知
- **THEN** 该通知不重新开启 overlay、不改变信任，也不更新当前其他输入的状态
