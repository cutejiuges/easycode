# config/json-file Specification

## Purpose

为本地交互使用提供可复用的用户级 JSON 配置入口，同时保留环境变量配置兼容性，并在读取、合并和错误处理过程中保护 API key 等敏感信息。

## Requirements

### Requirement: Load configuration from a default or explicit JSON path

系统 SHALL 支持从 JSON 文件读取 `provider`、`base_url`、`api_key` 和 `model` 字段。未提供 `--config` 时 SHALL 使用 `~/.config/easycode/config.json`；提供 `--config <path>` 时 SHALL 使用用户指定路径。路径中的 `~` SHALL 按当前用户 home 目录解析，不能通过工作目录猜测。

#### Scenario: Load the default user configuration

- **WHEN** 用户未提供 `--config` 且默认文件存在且内容合法
- **THEN** 应用读取 `~/.config/easycode/config.json` 作为配置基础值
- **THEN** 应用在发起 Provider 请求前完成统一配置校验

#### Scenario: Load an explicitly selected configuration

- **WHEN** 用户提供 `--config /tmp/easycode.json` 且该文件存在且内容合法
- **THEN** 应用读取指定文件而不是默认路径
- **THEN** 配置路径不会被写入请求正文或 API 错误

#### Scenario: Keep environment-only startup compatible

- **WHEN** 未提供 `--config` 且默认文件不存在
- **THEN** 应用继续允许仅通过环境变量提供配置
- **THEN** 应用不会为了创建默认文件而产生磁盘写入

### Requirement: Merge configuration with explicit precedence

系统 SHALL 先读取 JSON 文件，再使用非空环境变量覆盖对应字段，最后执行现有 Provider 校验。字段覆盖 SHALL 按字段独立处理；空环境变量不得覆盖 JSON 中已有值。`--config` 只选择文件路径，不得改变 provider wire 或隐式补全字段。

#### Scenario: Environment variables override JSON fields

- **WHEN** JSON 文件包含完整配置且 `EASYCODE_MODEL` 设置为另一个非空模型名
- **THEN** 合并后的 model SHALL 使用环境变量值
- **THEN** 未被环境变量覆盖的 provider、base URL 和 API key SHALL 保留 JSON 值

#### Scenario: Empty environment variables do not erase file values

- **WHEN** JSON 文件包含完整配置且某个环境变量未设置或为空字符串
- **THEN** 合并后的对应字段 SHALL 继续使用 JSON 值

### Requirement: Reject invalid or unsafe configuration files

配置文件内容 MUST 是 JSON object；未知顶层字段、重复或类型错误字段、malformed JSON、缺少必填 Provider 字段和非法 `base_url` MUST 返回稳定英文配置错误。错误文本、配置摘要和 CLI 输出 MUST NOT 包含 API key、Authorization、文件中的 secret 或完整敏感 URL 凭据。

#### Scenario: Reject malformed JSON before starting TUI

- **WHEN** 配置文件不是合法 JSON
- **THEN** 应用返回 `invalid_configuration`
- **THEN** 应用不创建 Provider、不发起网络请求且错误不包含文件 secret

#### Scenario: Reject missing explicit configuration file

- **WHEN** 用户通过 `--config` 指定的文件不存在或不可读
- **THEN** 应用返回 `invalid_configuration`
- **THEN** 应用不会静默回退到默认文件或环境变量配置

#### Scenario: Reject insecure base URL values from JSON

- **WHEN** JSON 中的 `base_url` 包含 userinfo、query、fragment、无效 host 或非 HTTP(S) scheme
- **THEN** 应用返回 `invalid_configuration`
- **THEN** 错误不回显 URL 中的密码、token 或 API key

### Requirement: Protect local configuration secrets

包含非空 `api_key` 的配置文件 SHALL 在 Unix 文件系统上使用用户私有权限（不得允许 group/other 读取或写入）；应用创建配置目录或文件时 MUST 使用用户私有权限。读取已有权限过宽的文件 MUST 返回明确英文配置错误，而不是继续启动。

#### Scenario: Accept a private configuration file

- **WHEN** 配置文件包含 API key 且权限为用户私有可读（例如 `0600`）
- **THEN** 应用可以读取并继续完成配置校验
- **THEN** API key 不会出现在配置摘要、错误或 TUI snapshot

#### Scenario: Reject a world-readable secret file

- **WHEN** 配置文件包含 API key 且 group 或 other 具有读取或写入权限
- **THEN** 应用返回 `invalid_configuration`
- **THEN** 应用不发起 Provider 请求

### Requirement: Expose configuration selection through the CLI

CLI SHALL 提供 `--config <path>` 帮助文本，默认行为和显式路径行为必须可从 `--help` 识别。已有 `--version` 行为 SHALL 保持不变；本变更不实现或扩展 `--print`、JSON event、resume 或其他 headless 能力。

#### Scenario: Show config flag in help

- **WHEN** 用户执行 `easycode --help`
- **THEN** 输出包含 `--config` 及其默认路径语义

#### Scenario: Version flag does not require configuration

- **WHEN** 用户执行 `easycode --version` 且未配置环境变量或配置文件
- **THEN** 应用正常输出版本并退出

### Requirement: Configuration security checks bind to the opened file

配置加载 SHALL 在读取正文前，以不跟随 symbolic link 的方式打开目标，并从同一个已打开文件句柄校验普通文件类型、大小和支持平台上的权限。路径检查与文件打开之间发生的替换 MUST NOT 使应用读取未经校验的文件；无法提供该保证的平台 MUST 明确拒绝不安全路径，而不是降级为先检查路径再打开。

#### Scenario: Reject a symbolic-link configuration

- **WHEN** 默认或显式配置路径本身是 symbolic link
- **THEN** 应用在读取正文前返回安全英文 `invalid_configuration` 错误
- **THEN** 应用不发起 Provider 请求且错误不暴露链接目标

#### Scenario: Reject a file swapped during secure open

- **WHEN** 测试在路径解析与文件打开边界确定性地把目标替换为 symlink、目录或权限不安全的文件
- **THEN** 应用拒绝配置或只读取已经取得并完成句柄校验的原文件
- **THEN** 未经同一句柄校验的替换目标内容不会进入配置

#### Scenario: Validate permissions on the opened file

- **WHEN** 配置文件包含非空 API key 且已打开文件的实际权限允许 group 或 other 访问
- **THEN** 应用根据该文件句柄的 metadata 返回 `invalid_configuration`
- **THEN** 先前对路径执行的检查结果不能覆盖句柄检查结果

### Requirement: Configure an explicit context token budget

JSON 配置 SHALL 接受可选的顶层整数 `context_window_tokens`、`reserved_output_tokens` 和 `context_safety_margin_tokens`。`context_window_tokens` 存在时 MUST 大于零；两个预留字段省略时 SHALL 默认为零，存在时 MUST 为非负整数，且其无溢出之和 MUST 严格小于 context window。预留字段在 context window 缺失时 MUST 被拒绝，避免产生看似生效但无法执行的配置。

这三个字段 SHALL 只由显式 JSON 配置提供；当前切片不新增环境变量覆盖。系统 MUST NOT 根据 Provider 或 model 名称填充缺失窗口。类型错误、负数、超出受支持整数范围、算术溢出或非法字段组合 MUST 返回稳定英文 `invalid_configuration`，且不得启动 Provider 请求。

#### Scenario: Load a complete context budget

- **WHEN** JSON 配置包含 `context_window_tokens: 200000`、`reserved_output_tokens: 20000` 和 `context_safety_margin_tokens: 4096`
- **THEN** 合并后的配置保留三个显式数值供上下文规划使用
- **THEN** 既有 Provider 字段合并和校验语义保持不变

#### Scenario: Default omitted reserves to zero

- **WHEN** JSON 配置只增加合法的 `context_window_tokens`
- **THEN** 输出预留和安全余量均为零
- **THEN** 系统不从 model 名称或远程服务补全预留值

#### Scenario: Keep an omitted window unenforced

- **WHEN** JSON 配置和环境变量启动方式均未提供 context window 或预留字段
- **THEN** 配置加载保持兼容且上下文预算为未启用
- **THEN** 现有环境变量启动不需要新增变量

#### Scenario: Reject reserves without a window

- **WHEN** JSON 配置包含 `reserved_output_tokens` 或 `context_safety_margin_tokens` 但没有 `context_window_tokens`
- **THEN** 应用返回英文 `invalid_configuration`
- **THEN** 应用不创建 Provider 或发起网络请求

#### Scenario: Reject an impossible or overflowing budget

- **WHEN** 任一预算字段为负数、非整数、超出受支持范围，或两个预留之和大于等于窗口或发生整数溢出
- **THEN** 应用返回英文 `invalid_configuration`
- **THEN** 错误不包含 API key、prompt、历史正文或完整敏感 URL 凭据

### Requirement: Configure a bounded compaction output budget

JSON SHALL 接受可选顶层整数 compaction_output_tokens，省略时默认 4096，显式值必须在 1..65536；类型错误、null、负数、零、重复和溢出 MUST 返回 invalid_configuration。字段只由 JSON 配置，不新增环境变量或按模型猜测值。无 context_window_tokens 时允许该字段但不推断容量；显式窗口和显式摘要输出值存在时，其与安全余量之和 MUST 严格小于窗口。省略字段产生的默认值不适配小窗口时只拒绝压缩预检，不使原本合法普通配置启动失败。该配置不得修改普通 reserved_output_tokens 或普通请求输出策略。

#### Scenario: Preserve default startup
- **WHEN** 既有合法配置没有 compaction_output_tokens
- **THEN** 普通启动语义不变，摘要默认上限为 4096

#### Scenario: Apply an explicit summary budget
- **WHEN** 配置合法摘要输出上限 8192
- **THEN** 两家摘要请求分别发送对应实际 wire 上限 8192，摘要预检使用同一值

#### Scenario: Reject impossible explicit budgets
- **WHEN** 显式摘要输出值不在范围内或加安全余量大于等于显式窗口
- **THEN** 在 Provider 请求前拒绝配置，错误不包含配置正文或 secret

#### Scenario: Keep a small ordinary window usable
- **WHEN** 窗口为 2048、普通预留合法且未配置摘要输出上限
- **THEN** 普通启动成功，手动压缩以预算不足拒绝且不写 started

### Requirement: Configure explicit Provider cache and transport policies

JSON 配置 SHALL 接受三个可选顶层字符串：anthropic_prompt_cache 为 disabled、ephemeral_5m 或 ephemeral_1h，默认 disabled；openai_prompt_cache_key 为 omit 或 thread，默认 omit；openai_transport 为 http_sse 或 websocket_auto，默认 http_sse。字段名与枚举值大小写精确匹配；null、未知值、非字符串、重复字段和未知字段 MUST 以安全英文 invalid_configuration 拒绝。三个字段只由 JSON 显式设置，本切片不增加环境变量、CLI 覆盖或模型名推断。

既有 Provider 环境变量合并完成后，非 Anthropic family MUST 拒绝非 disabled 的 anthropic_prompt_cache，非 OpenAI family MUST 拒绝非 omit 的 openai_prompt_cache_key 或非 http_sse 的 openai_transport；无关 family 的缺省值允许。websocket_auto SHALL 表达用户允许尝试连接与严格安全回退，不表示远端支持已验证。配置错误不得在 Provider 网络、Session 创建或后台任务后才被发现。

#### Scenario: Keep environment-only startup unchanged
- **WHEN** 用户仅提供既有 Provider 环境变量
- **THEN** 缓存字段省略、HTTP/SSE 为默认，无新必填配置

#### Scenario: Enable the complete OpenAI path
- **WHEN** 合并后的 family 为 openai，key 为 thread，transport 为 websocket_auto
- **THEN** 配置成功，key 与 transport 独立生效，不自动改变 store=false

#### Scenario: Enable an Anthropic lifetime
- **WHEN** family 为 anthropic，anthropic_prompt_cache 为 ephemeral_1h
- **THEN** 普通与摘要请求使用固定一小时显式策略，OpenAI 字段仍为默认

#### Scenario: Reject a family changed by the environment
- **WHEN** JSON 启用 Anthropic 缓存而环境变量将最终 family 覆盖为 openai
- **THEN** 合并后拒绝非法组合，错误不回显文件、API key 或 URL

#### Scenario: Reject an invalid cache value
- **WHEN** 任一新字段为 null、布尔、未知枚举、大小写别名或重复字段
- **THEN** 返回 invalid_configuration，数据目录与 Provider 请求均未改变

### Requirement: Configure explicit native reasoning settings

JSON SHALL 接受可选字段 `anthropic_thinking`、`anthropic_effort`、`openai_reasoning_effort` 和 `openai_reasoning_summary`。anthropic_thinking MUST 是封闭判别对象：disabled 只允许 type；adaptive 允许 type 及可选 display；enabled 要求 type/budget_tokens 并允许可选 display。budget_tokens MUST 为 1024 至普通有效输出上限减一的整数，其他分支不得携带。display SHALL 仅接受 summarized/omitted；省略时不发字段，disabled 携带 display 必须拒绝，不支持 updates 或隐式补 mode。

anthropic_effort SHALL 仅接受 low/medium/high/xhigh/max；openai_reasoning_effort SHALL 仅接受 none/minimal/low/medium/high/xhigh/max；openai_reasoning_summary SHALL 仅接受 auto/concise/detailed。省略表示不发送对应字段，不等于关闭或某个固定强度。xhigh/max 的 Anthropic effort 与显式 disabled thinking MUST 被拒绝。

字段名、判别值和枚举 MUST 大小写精确匹配；未知、重复、跨分支字段、null、空串、错误类型、非整数、非法 Unicode、尾随内容及溢出 MUST 返回安全英文 invalid_configuration。配置只从既有 JSON 入口读取，不增加环境变量、CLI 推理开关或任意参数透传。

#### Scenario: Configure adaptive thinking
- **WHEN** Anthropic 配置包含 adaptive thinking 和 high effort
- **THEN** 配置成功且不产生 budget_tokens，不按模型名称改写设置

#### Scenario: Validate manual budget boundaries
- **WHEN** Anthropic 普通有效输出上限为 4096，manual budget 分别为 1023、1024、4095、4096
- **THEN** 仅 1024 与 4095 成功，不自动夹取或提高输出上限

#### Scenario: Distinguish omitted and disabled thinking
- **WHEN** 分别省略 anthropic_thinking 与显式设置 disabled
- **THEN** 前者不发送 thinking，后者发送明确 disabled，两者不被归一化为同一 wire

#### Scenario: Reject cross-branch and malformed settings
- **WHEN** adaptive/disabled 对象带 budget_tokens，enabled 缺 budget_tokens，或对象含 null、重复键、未知字段
- **THEN** 返回 invalid_configuration，零 Provider 请求和零 Session 创建

#### Scenario: Reject incompatible disabled effort
- **WHEN** Anthropic 显式 disabled 与 xhigh 或 max effort 同时存在
- **THEN** 配置在资源装配前失败，不静默降低 effort

#### Scenario: Request summarized or omitted thinking explicitly
- **WHEN** adaptive 或 enabled 分支显式设置 display 为 summarized 或 omitted
- **THEN** 合法配置精确保留该值，display 省略时不补默认值；正文是否返回仍由服务端决定

#### Scenario: Reject malformed display before opening resources
- **WHEN** disabled 带 display，或 display 是 updates、null、大小写别名、错误类型或重复字段
- **THEN** 返回 invalid_configuration，零请求与零 Session 创建，不删除 display 后重试

### Requirement: Configure bounded ordinary output independently from compaction

JSON SHALL 接受可选整数 `max_output_tokens`，范围 1..65536；该范围是本地实现边界，不表示远端模型上限。普通 Anthropic 请求 SHALL 映射为 max_tokens，省略时沿用 4096；OpenAI SHALL 映射为 max_output_tokens，省略时继续不发送该字段。不得修改 compaction_output_tokens 或从输出预留反向生成 wire 上限。

普通 manual budget 在普通有效上限内即为合法普通配置；与摘要独立上限不相容时 SHALL 由 context/compaction 在操作预检时拒绝，不将该问题误报为普通启动配置无效，不修改任何配置值。

配置窗口存在且显式 max_output_tokens 时，reserved_output_tokens MUST 大于等于该值。窗口存在且任一新增 Anthropic 推理字段存在时，预留 MUST 覆盖 Anthropic 普通有效输出上限；任一新增 OpenAI 推理字段存在时 MUST 同时显式配置 max_output_tokens 并由预留覆盖。违反时在资源装配前拒绝，不自动修改预留或推断窗口；所有新字段省略时维持原有预算行为。

#### Scenario: Apply an explicit ordinary output cap
- **WHEN** 配置 max_output_tokens 为 16384 且预算组合合法
- **THEN** 两家普通请求使用各自 wire 名称发送 16384，摘要继续使用独立上限

#### Scenario: Reject an insufficient ordinary reserve
- **WHEN** 窗口已配置，max_output_tokens 为 16384 而 reserved_output_tokens 为 8192
- **THEN** 返回 invalid_configuration，不发起请求且不将任一字段自动改成 16384

#### Scenario: Require a known OpenAI cap for configured reasoning with a window
- **WHEN** OpenAI 配置窗口和显式 effort 或 summary，但未配置 max_output_tokens
- **THEN** 启动失败，不能声称已为未知输出上限充分预留

#### Scenario: Preserve existing small-window startup
- **WHEN** 所有新增字段省略且既有小窗口/预留配置合法
- **THEN** 启动和预算判定维持原行为，不因新增固定 Provider 默认上限而失败

#### Scenario: Keep an unconfigured window unknown
- **WHEN** 用户显式配置推理参数但未配置窗口
- **THEN** 允许结构合法配置，不推断容量、不要求窗口外的输出预留、不保证服务器接受

#### Scenario: Keep an ordinary manual configuration usable
- **WHEN** 普通 manual budget 为 8192、上限 16384，摘要默认上限为 4096，其他既有配置约束合法
- **THEN** 普通启动成功，后续摘要必须在本地预检拒绝，不自动扩大摘要额度或使普通启动失败

### Requirement: Freeze and validate reasoning after Provider environment overrides

既有 Provider 环境变量合并后，非 Anthropic family MUST 拒绝任何显式 Anthropic 推理字段，非 OpenAI family MUST 拒绝任何显式 OpenAI 推理字段，即使其语义类似缺省或关闭。校验 SHALL 在 Provider 网络、Session 创建及后台任务前完成，错误不得回显密钥、完整 URL、配置正文或非法值。合法设置 SHALL 在启动时冻结；配置文件后续变化不影响活动进程。resume/continue SHALL 使用本次启动的显式配置，不从旧 journal 猜测或恢复未保存的设置。

#### Scenario: Reject a family overridden by the environment
- **WHEN** JSON 配置 Anthropic thinking，环境变量把最终 family 改为 OpenAI
- **THEN** 合并后拒绝组合，零网络与零 Session 创建，错误不含配置中的合成 secret

#### Scenario: Ignore later file edits
- **WHEN** 合法启动后修改配置文件的推理字段
- **THEN** 活动进程后续请求维持冻结设置，render/getter 不重读文件

#### Scenario: Resume with current startup settings
- **WHEN** 同一 family/model 的会话用不同合法推理配置重新启动
- **THEN** 新请求使用当前设置，旧 append-only records 保持逐字节不变，不自动请求或重跑工具
