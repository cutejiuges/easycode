## Why

EasyCode 已具备双 Provider、工具审批、输入队列、上下文预算和 Session 恢复，但还不能发现、调用或恢复可复用的本地工作流。Skills 能以最少的新生命周期依赖形成用户可感知的 P6 首个切片，并为后续 Plugins 提供真实贡献消费者；Hooks 当前需要额外解决命令信任、阻断/改写和副作用恢复，Plugins 则尚缺贡献消费者。

## What Changes

- 发现用户级与项目级本地 `SKILL.md`，生成有界、稳定排序、带来源身份的不可变目录；支持 EasyCode 目录及明确列出的 Claude/Codex 本地目录。
- 提供 Claude 风格 `/skills` 列表、`/<name> args` 显式调用，并支持 Codex 风格首词 `$name args`；TUI 与 headless 使用同一准备服务。
- 提供双 Provider structured `Skill` 工具，按需完整读取正文及同来源的相对文本资源；模型只预先获得轻量目录。
- 显式调用保存原始用户正文与独立的强类型 Skill 附件；模型调用复用现有工具 ledger、审批边界和冻结结果恢复。
- 加入来源隔离、描述/正文预算、变更检测、错误隔离、缓存诊断与完整恢复回归。Skill 不授予文件修改、命令或网络权限。
- **BREAKING**：当前 Tool Catalog 从七能力变为八能力，扩展唯一当前 input/native/result 契约并替换当前 fixture；不建立旧格式 reader、迁移器或格式版本路由。

## Capabilities

### New Capabilities

- `extension/local-skills`：本地发现、格式子集、来源读取、显式与模型调用及 `/skills` 展示。

### Modified Capabilities

- `context/planning`：加入可选稳定 Skill metadata source，计量真实发送的目录与已加载内容。
- `runtime/conversational-input`：显式 Skill 输入附带冻结正文，保持原始输入、队列预算、取回与 durable resolution。
- `provider/history-projection`：Skill 附件独立投影，保留用户原文和离线预览一致性。
- `provider/native-history-persistence`：保存 Skill 附件和工具结果，恢复不重读本地 Skill。
- `tool/invocation-loop`：八能力目录、Skill typed ready call、同来源资源读取与恢复配对。
- `tool/result-budget`：Skill 完整正文的全有或全无预算规则。

## Impact

- 涉及 `internal/extension/skill`、domain/protocol 强类型、context、双 Provider、tool/runtime/session、app/headless/tui 及对应测试；只退出 skill 占位 allowlist，其他 P6/P7 占位不接入。
- YAML frontmatter 使用可审计的 typed parser；实施时确认现有依赖，只有缺少适用直接依赖时增加单一 YAML 库，不写宽松自制 YAML parser。
- 实施后同步总体架构、Roadmap、pitfall、README 及有关主 specs；本提案不表示 P6 已完成。

## Non-goals

本次不执行 hooks、不加载 plugins/MCP、不实现 fork/subagent、model override、allowed-tools 自动授权、动态 shell 插值、隐式脚本访问检测、路径自动激活、远程来源、安装器、watcher 或热加载。遇到有行为含义但不支持的 frontmatter 必须显示不支持并禁用该 Skill，不能假装兼容。
