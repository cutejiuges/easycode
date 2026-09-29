# EasyCode

EasyCode 是一个使用 Go 构建的本地优先 coding agent。项目在产品体验上主要参考 Claude Code，同时保留 OpenAI/Codex 模式的重要原生能力，并面向兼容 Anthropic Messages 或 OpenAI Responses 协议的服务商。

用户只需要提供 `base_url`、`api_key` 和模型名称即可连接服务，不需要账号登录、OAuth、设备码或订阅鉴权。

> 项目当前正在推进 P2 Session 与 Headless Agent Loop。Anthropic Messages 与 OpenAI Responses 已支持基础 TUI、多轮文本 Session、显式 `--resume`，以及单 turn `--print`/`--json` headless 输出。SQLite 索引、coding tools 和完整缓存/推理界面仍将按照 Roadmap 分阶段实现。

## 设计目标

- 提供接近 Claude Code 的 REPL、流式输出、工具调用、权限确认、diff、Session、Hooks、Plugins、Skills 和 Subagent 体验。
- 同时支持 Anthropic Messages 与 OpenAI Responses，不使用有损的统一消息模型抹平协议差异。
- 原样保留 Anthropic thinking signature、redacted thinking，以及 OpenAI reasoning summary、encrypted reasoning 和 message phase。
- 将缓存命中率作为一级架构目标，保证稳定前缀、工具定义、上下文顺序和序列化结果可预测、可回归。
- 通过小接口、单向依赖和事件协议保持内核纯净，避免上帝包、循环依赖及 UI 与 Provider 相互侵入。
- 保持本地优先、单二进制和可测试，让 TUI 与 headless 共享 Runtime，并为未来 IDE 或 app-server 宿主保留演进空间。

## 当前能力

| 领域 | 当前状态 |
|---|---|
| CLI/TUI 入口 | 已实现 Bubble Tea 基础文本 Chat、历史回放、显式 `--resume`，以及单 turn `--print` 最终文本和 `--json` JSONL v1 输出 |
| 双 Provider | Anthropic Messages 与 OpenAI Responses 已支持流式文本多轮、各自原生历史及 committed-only 文本历史投影 |
| HTTP/SSE | 已建立基于 Resty v3 的公共传输层 |
| JSON 与缓存 | 已建立不可变 canonical JSON、Provider request compiler 和 cache segment fingerprint；完整 Provider CachePlanner 尚未接入 |
| Runtime | 已实现共享文本 turn 生命周期、稳定身份、durable-before-memory 提交及 typed text/failure event |
| 安全配置 | 已实现 JSON 配置、环境变量覆盖、API key 脱敏，以及 macOS/Linux 上绑定实际句柄的 no-follow 校验；其他平台当前安全失败关闭 |
| Tools | P3 目标；当前只有经批准的 TODO 占位，不对模型暴露 schema，也没有执行器或生产消费者 |
| Session | 已实现 append-only JSONL、强类型 v1 draft/decoder、UUIDv7、跨进程 lease、同句柄 load/repair/write、`Sync`、尾部修复、fixture 和显式恢复；SQLite/`--continue` 尚未实现 |
| 扩展系统 | P6 目标；Hooks、Plugins、Skills 和 MCP 目前仅为经批准的 TODO 占位，未接入 Runtime/app |
| Subagent | P7 目标；当前仅为经批准的 TODO 控制边界，调度、线程树和生命周期尚未实现 |
| Telemetry | P8 目标；当前 logger 仅为经批准的 TODO 占位，未接入请求正文、Session 或运行时链路 |

完整阶段和验收标准见[产品 Roadmap](docs/roadmap/product-roadmap.md)。

### 首版 Provider Wire 范围

首版优先实现并支持 OpenAI Responses，同时支持 Anthropic Messages；首版不隐式支持 OpenAI Chat Completions。因此，只实现 Chat Completions 的国产生态、本地模型和第三方兼容网关在 v1 中不可用；`base_url + api_key` 只说明连接方式，不代表 endpoint 支持所有 OpenAI wire。未来的 Chat Completions 降级模式必须单独实现、单独探测并单独验证，详见 [ADR-0004](docs/architecture/adr/0004-openai-responses-first-wire-scope.md)。

## 技术栈

- Go 1.24+
- HTTP/SSE：[Resty v3](https://resty.dev/)
- JSON：[Sonic](https://github.com/bytedance/sonic)
- TUI：[Bubble Tea](https://github.com/charmbracelet/bubbletea)
- 日志：Go 标准库 `log/slog`
- 测试：Go 标准库 `testing`、集成测试与 race detector

## 快速开始

### 环境要求

- Go 1.24 或更高版本
- GNU Make 或兼容的 Make 实现
- Git

### 获取与构建

```bash
git clone git@github.com:cutejiuges/easycode.git
cd easycode
make
```

默认目标会构建完整二进制程序：

```text
bin/easycode
```

检查当前版本：

```bash
./bin/easycode --version
```

直接运行 TUI：

```bash
./bin/easycode
```

未指定恢复参数时，每次启动都会创建新的 root Session。使用已有 thread ID 显式恢复：

```bash
./bin/easycode --resume <thread-id>
```

只输出本次 turn 的最终文本：

```bash
./bin/easycode --print "summarize this repository"
```

逐行输出版本化 JSON 事件：

```bash
./bin/easycode --json "explain the current architecture"
```

prompt 也可以来自 stdin；显式 prompt 与管道同时存在时，管道内容会作为带 `<stdin>` 边界的附加上下文：

```bash
cat README.md | ./bin/easycode --print "summarize"
cat README.md | ./bin/easycode --json -
```

headless 输入必须是合法 UTF-8、非空且不超过 4 MiB。`--print` 仅在 turn durable 完成后发布最终文本；`--json` 的 stdout 只包含 JSONL v1，当前事件为 `thread.started`、`turn.started`、`assistant.text.delta`、`turn.completed`、`turn.failed` 和 `error`。退出码 `0` 表示结果已完整交付，`1` 表示运行、取消、输出或清理失败，`2` 表示参数或 prompt 用法错误。

两个 headless 模式都可与显式恢复组合，并且只输出本次新 turn，不回放旧 transcript：

```bash
./bin/easycode --json --resume <thread-id> "continue"
```

## Provider 配置

### JSON 配置文件

EasyCode 默认读取：

```text
~/.config/easycode/config.json
```

可参考仓库中的 `config.example.json`：

```json
{
  "provider": "openai",
  "base_url": "https://api.openai.com/v1",
  "api_key": "your-api-key",
  "model": "your-model"
}
```

创建用户配置并限制权限：

```bash
mkdir -p ~/.config/easycode
cp config.example.json ~/.config/easycode/config.json
chmod 600 ~/.config/easycode/config.json
```

然后编辑 `api_key` 和 `model`。也可以指定其他文件：

```bash
./bin/easycode --config /path/to/config.json
```

显式指定的文件不存在、不可读或 JSON 无效时会直接报错。默认文件不存在时，仍可仅使用环境变量启动。包含非空 `api_key` 的配置文件在 macOS/Linux 上必须是用户私有权限，例如 `0600`；文件通过 no-follow 打开，并从同一实际句柄校验类型、大小和权限后读取。Windows 等尚未提供等价安全 opener 的平台当前会明确失败关闭，不会退回 `Lstat` 后再 `Open`；配置路径和 API key 不会进入错误或配置摘要。

### 环境变量

以下非空环境变量会逐字段覆盖 JSON 配置：

| 环境变量 | 说明 | 示例 |
|---|---|---|
| `EASYCODE_PROVIDER` | Provider 协议家族 | `anthropic` 或 `openai` |
| `EASYCODE_BASE_URL` | 兼容服务的 API 地址 | `https://api.example.com` |
| `EASYCODE_API_KEY` | API 密钥 | 不应写入仓库或日志 |
| `EASYCODE_MODEL` | 模型名称 | 由服务商决定 |

示例：

```bash
export EASYCODE_PROVIDER=openai
export EASYCODE_BASE_URL=https://api.example.com/v1
export EASYCODE_API_KEY=your-api-key
export EASYCODE_MODEL=your-model
```

使用 Anthropic Messages 时，将 `EASYCODE_PROVIDER` 改为 `anthropic`，并把 `EASYCODE_BASE_URL` 设置为 Messages API 前缀。EasyCode 会在该前缀后追加 `messages`；OpenAI 模式则追加 `responses`。两种模式都不会隐式补充 `/v1`。

配置优先级为：JSON 文件提供基础值，非空 `EASYCODE_*` 环境变量覆盖对应字段，最后统一校验。`base_url` 是 API 路径前缀，不会自动补 `/v1`，且不能包含 userinfo、query 或 fragment。

当前 TUI 和 headless 模式都支持 Anthropic Messages 与 OpenAI Responses 的文本会话。成功文本回合会写入权限受控的 append-only JSONL；显式 `--resume <thread-id>` 在连续 exclusive lease 下恢复同一 Provider 的原生历史。TUI 通过只读 `SemanticHistoryView` 重建可见 transcript，headless 则不消费或回放旧 transcript。Anthropic thinking/signature/redacted thinking 与 OpenAI encrypted reasoning 会保留在各自原生历史中，不进入当前文本投影。stdin JSON/双向控制、usage/reasoning/tool JSON 事件、token estimator、完整 Provider UsageParser/CachePlanner、tools、SQLite 索引、`--continue`、prompt cache 控制、主动 thinking 配置和高级 reasoning UI 尚未实现。

## 架构概览

EasyCode 使用“共享生命周期模板 + Provider 原生内核”的双层设计：

```text
CLI / TUI / Headless
         |
    app lifecycle
         |
 ChatSession / Turn Runtime
      /      \
Anthropic   OpenAI
 Kernel      Kernel
      \      /
    Runtime Event
      /       |       \
 TUI View  Headless  Session JSONL

P3+ 目标：Tool Policy / Executor、扩展系统与 Subagent
```

当前共享层负责文本 turn 生命周期、Session 和宿主事件；工具调度、权限、Hooks 与 Subagent 是后续阶段目标。每个 Provider 当前独立负责原生请求、流归并、原生历史和语义投影；下列其他策略按 Roadmap 分阶段补齐：

- 原生请求编译与 Header 策略。
- SSE 事件归并和状态机。
- Thinking/Reasoning 原生数据保存。
- Tool wire 编解码。
- 缓存规划和上下文压缩。
- Usage、停止原因和兼容能力解析。

Provider-native item 是恢复会话和构建下次请求的事实依据，RuntimeEvent 只用于 UI、日志及宿主投影，二者不能相互替代。

共享层需要读取历史时使用 Provider 提供的单向 `HistoryProjector` 和 `SemanticHistoryView`。当前已实现 committed-only 文本投影和 resume 后的 TUI 历史回放；token 估算、Hook 文本和 Subagent completion 等消费者将在后续阶段接入。该视图不可反向生成 Provider 请求。

更完整的设计见[总体架构文档](docs/architecture/overall-architecture.md)。

## 工程结构

```text
.
├── cmd/easycode/              可执行程序入口
├── internal/
│   ├── app/                   依赖装配与应用生命周期
│   ├── codec/                 稳定 JSON 编解码
│   ├── config/                配置加载与校验
│   ├── context/               上下文和缓存规划
│   ├── domain/                核心值对象与领域语义
│   ├── extension/             P6 批准占位：Hooks、Plugins、Skills、MCP
│   ├── headless/              prompt 解析、最终文本与 JSONL v1 宿主
│   ├── protocol/              Command 与 RuntimeEvent 协议
│   ├── provider/              Provider Kernel 与 HTTP/SSE 传输
│   ├── runtime/               共享 turn 编排
│   ├── secret/                敏感值脱敏
│   ├── session/               JSONL 事实源、lease、修复与恢复规划
│   ├── subagent/              P7 批准占位：子代理控制边界
│   ├── telemetry/             P8 批准占位：结构化日志边界
│   ├── tool/                  P3 批准占位：工具能力与执行边界
│   └── tui/                   Bubble Tea 交互层
├── docs/                      架构、ADR、Roadmap 与踩坑记录
├── .githooks/                 可版本化 Git Hooks
├── AGENTS.md                  模型与工程实现约束
└── Makefile                   构建和质量门入口
```

## 构建与开发命令

```bash
# 构建 bin/easycode
make

# 检查格式、执行静态分析、全量测试和竞态测试
make verify

# 分别执行普通测试或竞态测试
make test
make test-race

# 执行固定版本 Staticcheck
make lint

# 删除本地构建产物
make clean
```

## 分支命名与提交前质量门

普通开发分支必须匹配：

```text
^(feat|fix|refactor|docs|test|ci|build|perf|chore|revert)/[a-z0-9]+(-[a-z0-9]+)*$
```

新增功能使用 `feat/...`，Bug、安全缺陷或契约违规修复使用 `fix/...`；例如 `feat/add-session-picker`、`fix/session-symlink-race`。`main`、`master`、detached HEAD、`codex/...`、用户名/工具前缀、下划线和只有 ticket 编号的后缀都会被拒绝。可单独运行：

```bash
make branch-check
make branch-policy-test
```

项目提供可版本化的 `pre-commit` 与 `pre-push` hook。首次克隆或初始化仓库后执行：

```bash
make install-hooks
```

该命令将当前仓库的 `core.hooksPath` 设置为 `.githooks`。两个 hook 共用同一个分支校验脚本；pre-commit 还会执行 `make verify`。以下任意检查失败都会中止提交：

1. 分支策略回归。
2. `gofmt -l .`。
3. `go vet ./...`。
4. `go tool staticcheck ./...`。
5. `go test ./...`。
6. `go test -race ./...`。

仓库 workflow 提供 `branch-governance` 和 `verify` 两个 job。管理员仍须在 GitHub 的 `main` branch ruleset 中把二者设为 required checks，并禁止 direct push；提交仓库文件本身不会自动启用托管平台保护。

## 缓存与 Provider 原生语义

缓存稳定性会直接影响响应延迟、费用和交互体验，因此所有进入模型请求的稳定内容都必须确定性排序和序列化。系统需要分别处理：

- Anthropic cache breakpoint、TTL 和 thinking signature。
- OpenAI prompt cache key、incremental response 和 encrypted reasoning。
- System/developer instructions、tool schema、Skills、Plugins 与 MCP catalog 的稳定顺序。
- 动态工作区状态和实时信息对稳定前缀的隔离。

当前仅支持同一 Provider/wire 的原生恢复。跨 Provider 时禁止伪造或转换 opaque reasoning；“创建 compacted fork，再由目标 Provider 建立新原生历史”是 P4 计划，尚未实现。

## Roadmap

| 阶段 | 主题 | 目标结果 |
|---|---|---|
| P0 | 工程与协议基线 | 可构建、可测试的 CLI 和清晰模块边界 |
| P1 | 双 Provider Kernel | Anthropic/OpenAI 均可完成流式对话 |
| P2 | Session 与 Headless Loop | 多轮对话、JSONL、恢复和取消 |
| P3 | Coding Tools | 文件、搜索、Patch、命令、权限和 Sandbox |
| P4 | 缓存与上下文 | 稳定缓存、压缩、指标和跨 Provider 分支 |
| P5 | Claude 风格 TUI | 日常可用的交互、diff 和权限确认体验 |
| P6 | 扩展系统 | Hooks、Skills、MCP 和本地 Plugins |
| P7 | Subagent | 前后台子代理、任务树、预算和取消传播 |
| P8 | 发布强化 | 跨平台、迁移、诊断和发布体系 |

## 文档

- [文档索引](docs/README.md)
- [总体架构设计](docs/architecture/overall-architecture.md)
- [技术栈 ADR](docs/architecture/adr/0001-use-go-resty-sonic-and-bubble-tea.md)
- [Provider 原生历史与语义投影 ADR](docs/architecture/adr/0002-provider-native-history-and-semantic-projection.md)
- [Session 线程树 ADR](docs/architecture/adr/0003-session-thread-tree.md)
- [首版 OpenAI Responses Wire ADR](docs/architecture/adr/0004-openai-responses-first-wire-scope.md)
- [产品 Roadmap](docs/roadmap/product-roadmap.md)
- [踩坑记录](docs/roadmap/pitfall-log.md)
- [工程与模型约束](AGENTS.md)

## 开发约束

参与实现前请先阅读 [AGENTS.md](AGENTS.md)。核心要求包括：

- 禁止上帝类、上帝包、循环依赖和跨层捷径。
- 保持 Provider 原生语义，不建立有损的统一消息模型。
- 全局代码注释使用中文，对外错误码和错误消息使用英文。
- 每次逻辑变更必须补充相应单元测试或集成测试。
- 修改 Provider、上下文、工具描述或扩展目录时必须评估缓存影响。
- API key、Authorization、Cookie 和敏感 Header 不得进入日志、Session、测试快照或错误消息。
- 每次实现完成后必须执行 `make verify` 并完成架构自检。

契约变更统一使用 OpenSpec：`explore -> propose -> review/confirm -> apply -> verify -> archive`。Provider wire、RuntimeEvent、Tool schema、Session schema、缓存口径、权限/sandbox、扩展 manifest、Subagent thread 和跨包公共接口等变更，必须先创建 OpenSpec change，再开始实现。详细约束和命令见 [AGENTS.md](AGENTS.md)。

## 安全说明

不要把真实 API key 写入代码、配置样例、命令历史、测试 fixture 或版本控制。若发现安全问题，请不要在公开 Issue 中披露密钥、用户代码、完整请求或其他敏感数据。
