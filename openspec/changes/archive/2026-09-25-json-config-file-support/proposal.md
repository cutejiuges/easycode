## Why

当前 EasyCode 只能通过环境变量配置 Provider，适合临时验证，但不适合本地长期使用：每次启动都需要重新 export API 地址、模型和密钥，也不利于保存一份可审阅的本地配置。现在基础 TUI 和 OpenAI Responses 链路已经可用，增加用户级 JSON 配置文件可以降低启动成本，同时保留环境变量兼容和现有 secret 脱敏约束。

## What Changes

- 增加用户级 JSON 配置文件，默认路径为 `~/.config/easycode/config.json`。
- 增加 CLI `--config <path>` flag，允许显式指定配置文件路径。
- 定义包含 `provider`、`base_url`、`api_key`、`model` 的 JSON 配置格式，并复用现有 Provider 校验。
- 按“配置文件基础值，环境变量非空值覆盖，最终统一校验”的规则合并配置。
- 默认配置文件不存在时保持环境变量启动兼容；显式指定的配置文件不存在或不可读时返回英文配置错误。
- 拒绝 malformed JSON、非法字段值和不安全的 `base_url`，错误、日志和配置摘要不得暴露 API key。
- 对包含 API key 的配置文件提供本地权限检查/创建约束，并补充脱敏、优先级、路径和 CLI 回归测试。

## Capabilities

### New Capabilities

- `config/json-file`: 从默认或显式 JSON 文件加载用户级 Provider 配置，并与环境变量安全合并。

### Modified Capabilities

- 无。

## Impact

- 修改 `internal/config`，新增 JSON 文件读取、路径解析、字段合并和权限校验。
- 修改 `cmd/easycode` 与 `internal/app`，解析 `--config` 并在启动 TUI 前加载合并后的配置。
- 更新 README、架构/roadmap 文档和 CLI 帮助文本。
- 新增无外网配置 fixture、临时目录测试、权限和 secret 脱敏测试；不新增第三方依赖。
