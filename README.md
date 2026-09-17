# EasyCode

EasyCode 是一个使用 Go 构建的本地优先 coding agent。项目在产品体验上主要参考 Claude Code，同时保留 OpenAI/Codex 模式的重要原生能力，并面向兼容 Anthropic Messages 或 OpenAI Responses 协议的服务商。

用户只需要提供 `base_url`、`api_key` 和模型名称即可连接服务，不需要账号登录、OAuth、设备码或订阅鉴权。

> 项目当前处于 P0 工程与协议基线阶段。应用入口、核心协议、Provider 边界、缓存规划、HTTP/SSE 传输、扩展边界和 TUI 骨架已经建立；真实模型对话、coding tools 和持久化 Session 将按照 Roadmap 分阶段实现。

## 设计目标

- 提供接近 Claude Code 的 REPL、流式输出、工具调用、权限确认、diff、Session、Hooks、Plugins、Skills 和 Subagent 体验。
- 同时支持 Anthropic Messages 与 OpenAI Responses，不使用有损的统一消息模型抹平协议差异。
- 原样保留 Anthropic thinking signature、redacted thinking，以及 OpenAI reasoning summary、encrypted reasoning 和 message phase。
- 将缓存命中率作为一级架构目标，保证稳定前缀、工具定义、上下文顺序和序列化结果可预测、可回归。
- 通过小接口、单向依赖和事件协议保持内核纯净，避免上帝包、循环依赖及 UI 与 Provider 相互侵入。
- 保持本地优先、单二进制和可测试，为未来的 headless、IDE 或 app-server 宿主保留演进空间。

## 当前能力

| 领域 | 当前状态 |
|---|---|
| CLI/TUI 入口 | 已建立 Bubble Tea 应用入口和 headless 骨架 |
| 双 Provider | 已定义 Kernel、能力模型及 Anthropic/OpenAI 原生数据边界 |
| HTTP/SSE | 已建立基于 Resty v3 的公共传输层 |
| JSON 与缓存 | 已建立 Sonic 稳定序列化和 cache segment fingerprint |
| Runtime | 已建立共享 turn 生命周期和 RuntimeEvent 骨架 |
| 安全配置 | 已实现环境变量配置、校验和 API key 脱敏值对象 |
| Tools | 已定义能力、执行器及内置工具描述边界，尚未实现真实执行 |
| Session | 已定义存储契约，JSONL/SQLite 持久化尚未实现 |
| 扩展系统 | 已建立 Hooks、Plugins、Skills 和 MCP 包边界 |
| Subagent | 已建立控制协议边界，调度和生命周期尚未实现 |

完整阶段和验收标准见[产品 Roadmap](docs/roadmap/product-roadmap.md)。

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

检查当前版本或运行无交互骨架：

```bash
./bin/easycode --version
./bin/easycode --print
```

直接运行 TUI：

```bash
./bin/easycode
```

## Provider 配置

配置模块使用以下环境变量：

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

当前 P0 入口不会发起真实模型请求。这组配置将在双 Provider Kernel 阶段接入 CLI 和 Runtime。

## 架构概览

EasyCode 使用“共享生命周期模板 + Provider 原生内核”的双层设计：

```text
CLI / TUI / Headless
         |
   Runtime Command
         |
    Turn Runtime
      /      \
Anthropic   OpenAI
 Kernel      Kernel
      \      /
    Runtime Event
      /      \
 TUI View   Session
         |
 Tool Policy / Executor
```

共享层负责 turn 生命周期、工具调度、权限、Hooks、Session 和 UI 事件；每个 Provider 独立负责：

- 原生请求编译与 Header 策略。
- SSE 事件归并和状态机。
- Thinking/Reasoning 原生数据保存。
- Tool wire 编解码。
- 缓存规划和上下文压缩。
- Usage、停止原因和兼容能力解析。

Provider-native item 是恢复会话和构建下次请求的事实依据，RuntimeEvent 只用于 UI、日志及宿主投影，二者不能相互替代。

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
│   ├── extension/             Hooks、Plugins、Skills、MCP
│   ├── protocol/              Command 与 RuntimeEvent 协议
│   ├── provider/              Provider Kernel 与 HTTP/SSE 传输
│   ├── runtime/               共享 turn 编排
│   ├── secret/                敏感值脱敏
│   ├── session/               Session 存储契约
│   ├── subagent/              子代理控制边界
│   ├── telemetry/             结构化日志与诊断
│   ├── tool/                  工具能力与执行边界
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

# 删除本地构建产物
make clean
```

## 提交前质量门

项目提供可版本化的 `pre-commit` hook。首次克隆或初始化仓库后执行：

```bash
make install-hooks
```

该命令将当前仓库的 `core.hooksPath` 设置为 `.githooks`。此后每次正常提交前都会执行 `make verify`，以下任意检查失败都会中止提交：

1. `gofmt -l .`
2. `go vet ./...`
3. `go test ./...`
4. `go test -race ./...`

## 缓存与 Provider 原生语义

缓存稳定性会直接影响响应延迟、费用和交互体验，因此所有进入模型请求的稳定内容都必须确定性排序和序列化。系统需要分别处理：

- Anthropic cache breakpoint、TTL 和 thinking signature。
- OpenAI prompt cache key、incremental response 和 encrypted reasoning。
- System/developer instructions、tool schema、Skills、Plugins 与 MCP catalog 的稳定顺序。
- 动态工作区状态和实时信息对稳定前缀的隔离。

跨 Provider 恢复不会伪造或转换 opaque reasoning 数据，而是创建经过压缩的会话分支，再由目标 Provider 建立新的原生历史。

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

## 安全说明

不要把真实 API key 写入代码、配置样例、命令历史、测试 fixture 或版本控制。若发现安全问题，请不要在公开 Issue 中披露密钥、用户代码、完整请求或其他敏感数据。

