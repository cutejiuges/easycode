## Context

当前 `internal/config` 只有 `LoadFromEnv`，`internal/app.Run` 直接读取环境变量，CLI 只解析 `--version` 和尚未实现的 `--print`。Provider 校验已经集中处理 scheme、host、userinfo、query、fragment、API key 和 model；本变更需要把文件配置接入该边界，而不是在 Provider 或 TUI 层重复解析。

配置文件包含 API key，因此读取路径、权限、错误脱敏和环境变量覆盖都属于配置边界的安全契约。项目没有现成配置文件依赖，Go 标准库 JSON 和文件 API 足以完成本切片。

## Goals / Non-Goals

**Goals:**

- 提供 `~/.config/easycode/config.json` 默认路径和 `--config` 显式路径。
- 将 JSON、环境变量和统一 `config.Config` 合并为确定的优先级。
- 保留无默认文件时的环境变量兼容，并对显式文件错误快速失败。
- 检查包含 API key 的文件权限，避免常见的 world/group-readable secret。
- 在 app 启动 TUI 前完成所有读取、合并、校验和脱敏错误处理。

**Non-Goals:**

- 不实现配置热加载、profile、多账户、加密 secret store 或环境变量以外的命令行字段覆盖。
- 不改变 Provider wire、Runtime、Session、TUI 交互或 `--print` 能力范围。
- 不自动创建配置文件；只在未来明确要求写配置时考虑安全创建流程。
- 不为 Windows 注册表或平台专用配置目录增加额外行为；本切片按约定的 home 下 `.config/easycode/config.json` 解析。

## Decisions

### 1. 使用扁平 JSON schema，并映射到现有 Config

文件使用与环境变量同名语义的扁平结构：`provider`、`base_url`、`api_key`、`model`。读取层先解码为专用 file DTO，再构造 `config.Config`；API key 立即包装为 `secret.Value`。不让 JSON 直接暴露到 Provider、Runtime 或 TUI，避免 `map[string]any` 跨层传播。

备选方案是嵌套 `provider` 对象或引入通用动态配置树。嵌套结构与当前环境变量一一映射不如扁平结构直观；动态配置树会扩大未知字段和类型错误的处理面，因此不采用。

### 2. 明确文件选择与字段合并顺序

CLI 先解析 `--config`。未提供时解析为 `home/.config/easycode/config.json`。默认文件不存在表示“无文件层”，继续使用环境变量；显式路径不存在、不可读或 JSON 无效均为错误。读取文件得到基础配置后，非空 `EASYCODE_PROVIDER`、`EASYCODE_BASE_URL`、`EASYCODE_API_KEY`、`EASYCODE_MODEL` 逐字段覆盖，最后调用现有 `ValidateProvider`。

这样既保持现有环境变量启动方式，又允许用户只在文件中保存稳定字段并临时用环境变量切换模型或 key。空环境变量不覆盖文件值，避免 shell 中误导出空值导致配置被清空。

### 3. 将配置加载做成显式入口参数

`app.Options` 增加 `ConfigPath` 或已加载配置注入边界；推荐由 `cmd/easycode` 将 flag 值传给 app，由 app 调用 `config.Load(path)`。这样 `--version` 可以在任何配置读取前退出，测试可以直接注入临时目录和 deterministic path，TUI 不需要知道配置文件。

不在 `config.Load` 内部读取 CLI flag，也不在全局变量中保存路径，避免配置包反向依赖命令行层和全局可变状态。

### 4. 以文件 mode 保护 API key

读取包含非空 API key 的配置文件时检查 `mode.Perm()&0077 == 0`；创建目录使用 `0700`，未来若创建文件使用 `0600`。符号链接、目录路径、读取失败和权限过宽统一转换为稳定英文 `invalid_configuration`，底层错误只保留不含正文和 secret 的安全摘要。

该检查会拒绝部分用户已有的宽权限配置文件，但这是显式的安全约束；用户可以通过 `chmod 600 ~/.config/easycode/config.json` 修复。默认文件不存在时不创建目录，减少启动副作用。

### 5. JSON 解码拒绝未知字段并保持稳定错误

使用标准库 `encoding/json.Decoder` 的 `DisallowUnknownFields` 解码专用 DTO，并限制顶层对象结构。类型错误、重复字段、null 或空必填字段在统一校验阶段失败。错误消息只报告字段名和稳定英文类别，不回显 JSON 正文、API key 或完整 URL。

## Risks / Trade-offs

- [严格 mode 检查可能阻止已有宽权限文件启动] → 在错误中明确给出 `chmod 600` 修复提示，并覆盖权限 table test。
- [环境变量覆盖可能让文件内容与实际请求不一致] → 文档和 `--help` 明确字段级优先级，配置摘要只展示脱敏后的最终值。
- [默认文件路径在不同平台的习惯不同] → 本切片固定用户指定的 `~/.config/easycode/config.json` 约定，后续跨平台变更单独设计。
- [配置文件误放入版本库] → 文档提供 `config.example.json` 而不提供真实 key，测试扫描错误和 snapshot 中不得出现 secret。

## Migration Plan

1. 增加配置 DTO、路径解析、文件权限检查和环境变量合并单测。
2. 将 app/CLI 接入 `--config`，先运行显式文件和默认文件场景，再验证旧的纯环境变量场景。
3. 更新 README、CLI help、`.gitignore`（如有必要）和配置示例，说明 `chmod 600` 与优先级。
4. 执行 `make verify`；失败时删除新增文件加载路径即可回退，环境变量入口保持可用。
