## Purpose

定义 Claude 风格的结构化问题交互，使用户能够在不中断当前任务的情况下逐题选择、输入说明、检查并提交回答，同时确保聊天草稿、浏览位置、终端安全显示和真实持久化状态不被弹窗混淆。

## ADDED Requirements

### Requirement: Questions have explicit selection editing and review states

TUI SHALL 显示题目 header、当前题号/总数、完整可滚动问题和全部选项；上下键移动焦点，单选 Enter 确认并进入下一题，多选 Space 切换、Enter 确认。自动提供其他文字入口，Tab 切换选项与说明输入，Shift+Tab 返回上一题并保留各题草稿。输入采用既有 Unicode 字素编辑、换行与显式 paste 规范，不把 paste 中的 Enter/Esc/Ctrl+C 当成操作。

全部题目完成后 SHALL 显示回答复核页，支持返回修改、显式跳过未答题及提交；未答题不能被默认为第一项，必须明确标为 skipped 后才能提交。默认焦点不表示已选择，也不触发自动提交。选择和说明只有在用户提交整组回答后才离开本地草稿状态。

#### Scenario: Review a mixed questionnaire
- **WHEN** 用户完成一题单选、一题多选和一题其他文字回答
- **THEN** 复核页显示三个完整决定，并可返回修改任一题而不丢失其他答案

#### Scenario: Submit a skipped question explicitly
- **WHEN** 某题未选择且没有非空文字
- **THEN** 复核页要求返回作答或显式标为跳过，不能把默认焦点当成回答

### Requirement: Question focus preserves the chat and transcript

活动提问 SHALL 独占焦点，优先于聊天 composer 和 transcript 浏览；它与副作用审批不同时活动，冲突事件必须失败关闭。普通字符、粘贴、Enter、方向键和 Tab 只路由到当前问卷控件；聊天草稿、光标、历史浏览和 transcript 锚点保持。Esc 在文字编辑态先返回问卷；其余问卷状态 Esc/Ctrl+C 定向 stop 当前 turn，禁用重复操作并等待内核收口。复核页的显式跳过仅产生 skipped 回答，不中断 turn。

#### Scenario: Paste while answering
- **WHEN** 聊天中已有多行草稿，用户在问题说明框粘贴含换行和控制字符的正文
- **THEN** 只改变问卷草稿，聊天不提交、不修改，paste 内的控制字符不取消问卷

#### Scenario: Interrupt while browsing is suspended
- **WHEN** 历史浏览被问卷接管后用户 Ctrl+C
- **THEN** 发送当前 turn 的 stop；收口后恢复原草稿和浏览位置，不能取消随后替换的 turn

### Requirement: Answer submission reflects durable completion

提交后 TUI SHALL 禁用重复提交并显示提交中。command accepted 仅表示 owner 接管，不能显示已保存；只有成功 result Sync 后的 resolved 事件才显示已提交回答。校验或预算拒绝保留全部草稿并显示安全英文错误；durability 不确定显示失败/不确定并等待 terminal，不能自动重提。迟到、重复、request/turn/invocation/fingerprint 不匹配事件不得关闭或覆盖新问卷。

#### Scenario: Reject an oversized answer without losing it
- **WHEN** 文字合法但当前完整结果编码超过可用预算
- **THEN** 留在问卷并保留选项、正文和光标，用户可缩短后再提交；Session 无该回答

#### Scenario: Fail Sync after acceptance
- **WHEN** 回答已被 owner 接收但 result Sync 失败
- **THEN** 界面不声称保存成功，不自动重复提交，并遵循 Runtime 的失败/poison 收口

### Requirement: Question rendering remains safe bounded and deterministic

问卷 SHALL 在 80x24、34x12、12x6、无色及 resize 下保留可达的题目、选项、当前输入位置和提交/返回动作；超出视口可滚动，不能无提示截断问题或已选项。展示副本遵循既有终端控制字符可见转义和字素宽度规则。渲染不得执行 I/O、改变题目/回答、决定授权或启动后台任务。

#### Scenario: Resize during review
- **WHEN** 用户在含中文、emoji 和长选项的复核页反复 resize
- **THEN** 原始回答和题目 fingerprint 不变，所有决定可查看，渲染不触发提交或查询副作用
