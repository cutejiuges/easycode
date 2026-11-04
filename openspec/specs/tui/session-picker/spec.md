# tui/session-picker Specification

## Purpose

提供启动时可用的交互式会话选择器，使用户能通过有界元数据搜索和显式只读文本预览辨认历史会话，并在确认恢复身份与工作目录后安全进入已有聊天流程，保持终端输入、取消和资源清理的一致语义。

## Requirements

### Requirement: Interactive resume is an explicit startup command

系统 SHALL 提供 `easycode resume [--all] [--config PATH]`。默认范围为当前规范化启动目录；`--all` 仅移除目录筛选。命令 SHALL 要求 stdin/stdout 均为终端，并在打开存储前校验参数与终端条件；不得回退读取其他终端。帮助 SHALL 返回 0 且不打开配置或存储；未知参数、多余位置参数、缺失选项值及非终端输入输出 SHALL 返回 2 和安全英文诊断。

命令 SHALL 在启动界面前加载并验证当前聊天配置，配置错误返回 1；配置和启动 cwd 在本次启动中保持固定。只有首参数 `resume` SHALL 进入该命令。已有 `--resume <thread-id>`、`--continue`、headless 和独立 `sessions list` 契约 MUST 保持不变。选择前 MUST NOT 创建 Session journal、读取项目指令、执行工具或发起模型请求。

#### Scenario: Open the picker with valid configuration
- **WHEN** 用户在交互终端使用有效配置执行 `easycode resume`
- **THEN** 界面加载当前目录候选，尚未创建新 thread 或发起 Provider 请求

#### Scenario: Reject usage before resource acquisition
- **WHEN** 用户请求帮助或传入未知选项、位置参数，或 stdin/stdout 任一不是终端
- **THEN** 帮助返回 0，非法调用返回 2，配置和存储均未打开

#### Scenario: Preserve a headless prompt
- **WHEN** 用户执行 `easycode --print "resume"`
- **THEN** 文本作为原有 headless prompt，不进入选择器

### Requirement: Discovery and metadata search have explicit bounds

每次成功加载 SHALL 使用既有 Catalog 协调与查询语义，按 committed 更新时间和 canonical thread ID 双降序返回当前范围最多 200 条。当前目录 SHALL 精确匹配规范化 creation cwd，不包含子目录或兄弟 worktree。搜索 SHALL 只覆盖已加载条目的 thread ID、Provider family、wire、model 和 creation cwd，按 Unicode 小写转换后的字面子串匹配，不解释正则，不搜索预览正文。

搜索输入 SHALL 限制为 256 个 Unicode code point；decoded paste 的 CR/LF/Tab SHALL 转为空格，超过限制的编辑整体拒绝且不丢失原查询。退格 SHALL 按字素边界删除。UI SHALL 区分空目录、没有匹配项和加载失败；有额外条目时 MUST 明确搜索仅覆盖最近 200 条。协调的 busy/invalid 提示 MUST 标明全存储范围、可能陈旧或遗漏，不能声称每个显示条目当前可恢复。键入搜索 MUST NOT 触发 journal 扫描。

#### Scenario: Search a bounded mixed-provider list
- **WHEN** 当前目录有超过 200 条、包含两家 Provider 的会话，用户输入模型名
- **THEN** 仅在最近 200 条中筛选，不自动隐藏其他 Provider，也不搜索正文，界面说明搜索边界

#### Scenario: Distinguish no matches from incomplete discovery
- **WHEN** 搜索没有结果，且协调报告 busy/invalid 或存在更多未加载条目
- **THEN** 界面说明已加载范围无匹配，保留不完整提示，不宣称存储中不存在该会话

#### Scenario: Reject excessive pasted search text
- **WHEN** 一次粘贴经空白规范化后使查询超过 256 个 code point
- **THEN** 原查询、选择和列表不变，界面显示安全的输入上限提示

### Requirement: Keyboard routing preserves explicit selection and cancellation

界面 SHALL 支持上下键、PageUp/PageDown、输入搜索、Ctrl+A 切换范围、Ctrl+R 刷新、Ctrl+V 预览及 Enter 请求恢复。更新列表 SHALL 按 thread ID 保持仍存在的选择，无法保留时选择首项；不得通过旧索引确定恢复目标。没有候选时 Enter/Ctrl+V 不产生操作。

Esc SHALL 优先关闭预览或跨目录确认，再清空非空搜索，最后取消启动并在清理成功后返回 0。Ctrl+C/信号取消 SHALL 在清理完成后返回 130；清理失败返回 1。加载中取消 MUST 等待清理，不能创建替代 Session。进入正式恢复后重复 Enter MUST NOT 发起第二次恢复，普通输入 MUST NOT 成为聊天 prompt。

#### Scenario: Return from preview without losing the selection
- **WHEN** 用户打开预览后按 Esc
- **THEN** 返回原列表并保留查询和所选 thread，预览正文被丢弃

#### Scenario: Clear search and then exit
- **WHEN** 用户在列表非空搜索中按 Esc，再按 Esc
- **THEN** 第一次清空搜索，第二次取消启动；没有新 Session 或自动模型请求

#### Scenario: Ignore repeated confirmation while resuming
- **WHEN** 正式恢复已被接收，用户继续按 Enter 或输入字符
- **THEN** 只存在一个恢复请求，没有自动提交的聊天输入

### Requirement: Display is bounded terminal-safe and identity-preserving

列表 SHALL 显示更新时间、Provider/model，并在选择详情中提供完整 thread ID、wire、创建目录和实际恢复目录。时间 SHALL 使用当前机器本地时区的固定日期时间并显示时区。配置三元组不匹配 SHALL 显示具体差异，不能自动换模型或隐藏历史。

model/wire 的转义展示 SHALL 限制为 256 个 code point，目录为 512 个；元数据的换行、Tab、ESC、C0/C1、DEL、Unicode 行/段分隔符及双向控制符 MUST 可见转义。预览正文 SHALL 保留可见换行和制表布局，其他终端控制符必须转义，并遵循预览展示预算。窄屏、resize 和无色模式 MUST 保持选择与查询不变、无负尺寸，并能辨认当前选择；完整身份可换行/滚动，不得使用缩写身份发起操作。render MUST 无 I/O。

#### Scenario: Render hostile metadata and narrow screens
- **WHEN** model/cwd 含终端控制字符、中文、emoji 或超长文本，且终端被缩窄
- **THEN** 输出无可执行控制序列，展示有界，所选身份不变且详情可查看完整 ID

#### Scenario: Show compatibility without claiming availability
- **WHEN** 候选的 family、wire 或 model 与当前配置不匹配
- **THEN** 界面显示不匹配字段，仍允许符合 session/preview 契约的只读预览，不自动转换历史

### Requirement: Async loading cannot corrupt current state or leak owners

列表、刷新和预览 SHALL 具有独立请求身份，预览还绑定 thread ID。过期结果或错误 MUST NOT 覆盖当前范围、预览或错误状态。系统 SHALL 最多运行一个存储/恢复操作，快速切范围只保留最新待处理意图，在旧操作取消及清理后开始新操作。

清理成功的加载错误 SHALL 保留已显示状态并明确标记；若请求范围与已显示范围不同，旧列表 MUST 标注其实际范围且在成功加载当前范围前禁止恢复/预览，不能把旧数据冒充新范围。用户可显式刷新重试。首次协调未成功时 MUST NOT 将旧索引查询视作已完成刷新；切范围不能丢弃最近协调的不完整提示。任何清理失败 SHALL 结束启动。退出或终端输出失败 MUST 停止接收新操作，取消在途操作并等待唯一 owner 完成清理；超时不得遗弃资源。选择器到 Chat 的输入/终端所有权 MUST 连续且唯一，不得同时存在两个输入 reader。

#### Scenario: Ignore an old scope response
- **WHEN** 用户连续切换范围，旧请求在新意图建立后返回
- **THEN** 旧结果不更新当前范围，存储操作不并发，最终显示最新范围的结果

#### Scenario: Fail a refresh without faking an empty result
- **WHEN** 刷新失败但已有成功加载的列表
- **THEN** 原列表和选择保留并标记刷新失败，错误不是空列表，只有显式操作才重试

#### Scenario: Exit during preview or terminal failure
- **WHEN** 预览正在加载时收到退出请求或终端输出失败
- **THEN** 取消查询并等到 lease、目录与任务释放后退出，迟到结果不会重新打开界面
