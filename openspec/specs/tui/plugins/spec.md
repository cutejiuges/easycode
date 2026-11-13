# tui/plugins Specification

## Purpose

定义当前会话本地插件的只读发现、来源诊断和技能选择体验，使用户能够在不访问模型、修改安装状态或丢失草稿的前提下理解可用能力，并沿用现有技能提交与权限链路。

## Requirements

### Requirement: Plugins are browsed from the frozen session snapshot

TUI SHALL 提供无参数 `/plugins`，从本会话冻结快照显示插件 ID、可选 version、manifest format、`Skills only`、可用技能数和安全诊断。状态 SHALL 区分 available、partial、empty、unavailable、invalid、conflict：合法有技能且无错误为 available，有技能且有叶级错误为 partial，无候选且无错误为 empty，读取/平台/资源额度阻断为 unavailable，manifest/布局非法或存在叶级错误却没有可用技能为 invalid，插件 ID 冲突为 conflict。

有效 ID SHALL ASCII 排序，无法获得 ID 的来源用本地 CLI 参数序号稳定定位。系统 MUST NOT 显示宿主绝对路径、原始 manifest/底层错误或凭据；源数据中的控制与双向字符 SHALL 可见转义，终端列宽和 NO_COLOR SHALL 与现有列表一致。没有配置插件时 SHALL 显示空列表与下次使用 `--plugin-dir` 启动的简洁说明。页面 MUST NOT 暗示可以安装、信任、启停或热加载插件。状态和数量 SHALL 在所有技能策略、冲突及目录预算纳入决定后一次冻结，不能仅根据 manifest 可解析就显示成功。

#### Scenario: Inspect plugins without executing work
- **WHEN** 用户打开 /plugins 并浏览列表
- **THEN** Provider、磁盘来源读取、Session 写入和进程启动次数均不增加

#### Scenario: Explain a partially usable plugin
- **WHEN** 插件有一个合法技能及一个重复或非法技能
- **THEN** 列表显示 partial 和正确计数，详情可解释禁用项，不声称全部贡献可用

#### Scenario: Render an empty or hostile snapshot safely
- **WHEN** 没有插件或 metadata 含控制/双向字符，终端为 NO_COLOR 或窄屏
- **THEN** 空状态或诊断仍可读，不产生控制序列，所有行受当前列宽约束

### Requirement: Plugin details select skills through the existing composer

在插件列表按 Enter SHALL 打开详情，显示该插件冻结的限定技能名、描述、参数提示、手动/模型调用性和禁用原因。↑/↓ SHALL 导航/滚动，选择保持可见。对合法 user-invocable Skill 按 Enter SHALL 仅将 `/plugin:skill ` 放入 composer，供用户编辑并独立提交；disable-model-invocation 不得阻止合法手动选择。

model-only、invalid、unavailable 或 conflict 项 MUST NOT 被接受。选择本身 MUST NOT 加载正文、建立资源资格、提交输入、批准工具或执行插件；正式提交 SHALL 使用共享准备、来源变化检查、预算、admission 和 Sync 路径。参数提示不得成为真实参数。只替换真实调用文字，来源路径、sidecar 注解和完整默认 prompt 不得混入 composer。`/skills` SHALL 同时显示相同来源的插件限定名，与详情可用性一致。

#### Scenario: Fill a manual skill without submitting
- **WHEN** 用户在详情选择 qa:review 并按 Enter
- **THEN** composer 填入 /qa:review 和一个分隔空格，没有输入 admission、文件读取或 Provider 请求

#### Scenario: Keep a manual only workflow discoverable
- **WHEN** user-invocable=true 且 disable-model-invocation=true
- **THEN** 详情与 /skills 仍允许选择，模型 metadata 继续排除该项

#### Scenario: Reject an unavailable selection
- **WHEN** 用户在禁用或 model-only 项上按 Enter
- **THEN** 不改写草稿、不提交，也不切换到其他同名来源

#### Scenario: Validate source changes at actual submission
- **WHEN** 用户先选择技能，再修改其 manifest 或主文档后正式提交
- **THEN** 共享准备返回来源变化错误，保留可编辑草稿，不新增用户事实或 Provider 请求

### Requirement: Plugin browsing preserves focus drafts and cancellation

插件列表和详情 SHALL 独占其焦点，并与批准、问卷、Hooks、Skills、上下文、transcript 和清理状态遵守相同互斥规则；补全不得在插件面板可见时显示或处理按键。Esc SHALL 从详情返回列表、从列表返回打开前的完整命令草稿与光标，不引入补全附加空格或提交文字。手动提交时打开前草稿是当时的完整 /plugins 命令文本；从补全 Enter 打开时是替换候选前的 composer（如 /plug）。本轮没有从任意其他任务草稿打开面板的新入口，不推定存在未保存的旧草稿。恢复正文/光标时 SHALL 保留当前窗口布局与尺寸，不安装过期 layout。

活动 turn 期间浏览 SHALL 不修改队列、停止状态或运行权限；Ctrl+C SHALL 继续使用现有定向停止和清理完成规则。decoded paste 中的控制字符不得被当作面板命令或提交。resize/普通流式事件 SHALL 保持所选插件/技能身份；极小终端应保留返回/停止入口，不让不可见候选截获输入。

#### Scenario: Preserve an existing multiline draft
- **WHEN** 用户手动提交含前后空白/换行的 /plugins 命令草稿，或从 /plug 补全进入详情后逐层 Esc 返回
- **THEN** 各自真实入口的正文、尾部空白和光标与打开前相同，保持当前窗口尺寸，不新增历史或队列项

#### Scenario: Isolate panel keys from completion
- **WHEN** 插件面板打开时用户按 Tab、Enter、Esc 或方向键
- **THEN** 只有当前面板消费相应导航，旧补全不会接受、执行或改写草稿

#### Scenario: Stop an active turn from the panel
- **WHEN** 活动 turn 运行时用户在插件面板按 Ctrl+C
- **THEN** 仍定向停止并等待唯一 owner 完成清理，不能先执行插件或接受技能

#### Scenario: Keep current layout when returning from an older draft snapshot
- **WHEN** 插件面板打开后窗口从 80x24 缩为 28x10，再取消或选择技能
- **THEN** 保留当前 28x10 布局并正确放置光标，不把打开前的尺寸或补全空格恢复回来

#### Scenario: Keep selection stable on resize and stream updates
- **WHEN** 浏览期间收到流式增量或窗口缩小到 28x10
- **THEN** 原选中身份与可返回性保持，不扫描目录、不丢失草稿

### Requirement: Plugin browsing does not become protocol or cache state

打开、关闭、滚动、查看详情及选择技能 SHALL 只修改本地内存投影；不得添加 RuntimeEvent/headless 消息、Session/SQLite 记录、原生历史或缓存 segment。相同最终用户文本经手动输入、/plugins、/skills 或补全生成时 SHALL 得到相同准备结果和对应 Provider 请求。

headless 和 stream-json SHALL 继续使用既有文本与 typed 控制契约；普通 `/plugins` 文本不得被当作终端面板控制。两个入口仍可显式调用插件技能，恢复后面板只显示本次明确配置的来源，不根据历史附件重新启用插件。

#### Scenario: Compare manual and selected skill requests
- **WHEN** 同样的 /qa:review args 分别手动键入和从详情选择后编辑提交
- **THEN** 两家 Provider 各自的附件、native item 顺序、canonical request 和 fingerprint 一致

#### Scenario: Keep recovery history separate from active availability
- **WHEN** 恢复包含插件附件的历史，但本次没有传入 plugin-dir
- **THEN** 历史正文仍可读，/plugins 为空，不扫描旧来源或创建权限
