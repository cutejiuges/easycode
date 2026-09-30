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
