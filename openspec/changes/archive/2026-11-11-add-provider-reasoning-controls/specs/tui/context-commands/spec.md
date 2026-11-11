## ADDED Requirements

### Requirement: Context inspection explains frozen ordinary inference settings

`/context` SHALL 展示本次启动冻结的非敏感设置：Provider、thinking 模式/manual budget、Anthropic display、effort、summary 策略、普通输出上限及其 explicit/default/未设置来源。用户选择、实际发送值和真实返回观测 MUST 分开；省略项表示 provider default 或未发送，不伪造具体默认强度、不等于 disabled；OpenAI 无显式输出上限标记 server default/unknown。显示 summarized 不表示必然有正文，omitted/空 thinking 不表示 reasoning usage 为零。

该启动摘要 SHALL 由应用纯内存装配传入，动态窗口数值仍取既有 committed snapshot；不得通过 UI 直接访问 Provider/config 文件或扩展 RuntimeEvent/headless 外部 context.snapshot 字段。查询/重绘无 I/O、不推进 revision、不改变请求、输入队列或设置。所有字段共用有界布局，普通/窄屏/极小尺寸/NO_COLOR 下仍可辨认有效状态和关闭操作，长设置按既有安全终端规则处理。

#### Scenario: Inspect adaptive settings
- **WHEN** 启动配置为 Anthropic adaptive/high、普通上限 16384 后打开 /context
- **THEN** 显示真实冻结值及配置来源，活动提示与 Ctrl+O 阅读仍只反映实际返回内容

#### Scenario: Explain omitted settings honestly
- **WHEN** OpenAI 所有新增字段省略
- **THEN** 显示参数未发送、普通输出上限未知，不将其标成 medium、零或已关闭

#### Scenario: Query while a tool or summary is active
- **WHEN** 任务或摘要活动中打开/关闭 context 面板
- **THEN** 设置保持同一冻结值，窗口仍是准确 committed snapshot，原任务、队列、取消和焦点优先级保持

#### Scenario: Preserve external output contracts
- **WHEN** 配置主动推理后通过 stream-json 查询 context，或使用 text/json 输出
- **THEN** 外部字段/版本不因 TUI 设置展示而扩张，推理正文和 opaque 哨兵不可见

已有 headless reasoning_output 字段 SHALL 按 provider/usage-accounting 返回更新后的 known/unknown 数值状态；字段结构/版本保持不等于计量语义仍为 not_applicable。TUI 设置展示不得承诺估算预算是已计费 usage 或配置值已被 endpoint 验证。

#### Scenario: Render a constrained terminal
- **WHEN** 窄屏、极小高度、NO_COLOR 或含 hostile metadata 的模型名下打开面板
- **THEN** 文本经过安全处理、有界裁剪，Esc 关闭仍可辨认，重绘不触发网络或配置读取

#### Scenario: Separate display choice from observed thinking
- **WHEN** 显式 display omitted 且 thinking_tokens 为 30，或 summarized 但响应没有可阅读正文
- **THEN** 配置、真实计量和 unavailable 各自准确展示，不推断零消耗、不制造正文，Ctrl+O 不改变请求
