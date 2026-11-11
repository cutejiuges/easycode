# EasyCode

EasyCode 是一个使用 Go 构建的本地优先 coding agent。项目在产品体验上主要参考 Claude Code，同时保留 OpenAI/Codex 模式的重要原生能力，并面向兼容 Anthropic Messages 或 OpenAI Responses 协议的服务商。

用户只需要提供 `base_url`、`api_key` 和模型名称即可连接服务，不需要账号登录、OAuth、设备码或订阅鉴权。

> 项目按纵向切片持续推进。Anthropic Messages 与 OpenAI Responses 已支持基础 TUI、命令/Skill 首词补全、原生多轮 Session、`--resume`/`--continue`、headless/FIFO 输入、离线 `sessions list`，以及九项工具能力：Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill 和结构化用户提问。命令执行当前支持 Darwin；已支持同步 PreToolUse/PostToolUse/PostToolUseFailure 命令 Hook、被动推理阅读和启动推理/display/输出配置；其余 Hook 事件、运行中推理切换等能力仍按 Roadmap 分阶段推进。

## 交互式恢复会话

运行 `easycode resume [--all] [--config PATH]`，在当前目录或全部创建目录中选择会话。该入口需要有效模型配置以及终端 stdin/stdout；无需配置的发现入口仍是 `easycode sessions list`。已有 `--resume ID`、`--continue` 和 headless 行为保持一致。

输入搜索 ID、Provider、wire、model、创建目录；搜索只覆盖当前范围最近 200 条元数据，最多 256 个 Unicode 码点。上下键/翻页选择，Ctrl+A 切范围，Ctrl+R 重新协调索引，Ctrl+V 按需预览，Tab 查看可滚动完整身份，Enter 恢复。Esc 逐层返回、清空搜索、退出；Ctrl+C 取消启动。

预览严格校验原生历史，只显示前 128 条非空用户/assistant 文本，正文总量最多 32768 UTF-8 bytes，安全转义后的展示最多 196608 bytes。预览不访问模型、不执行工具、不修复日志；不完整尾部显示已提交前缀并明确提示。工具详情、opaque reasoning 和压缩摘要不伪装成原始消息，正文不进入索引或日志，关闭即丢弃。

不兼容配置可浏览和预览，但不能直接恢复。跨目录恢复默认返回，明确确认后仍使用当前工作目录和项目指令。预览不是恢复缓存：正式恢复重新获取独占 lease 并校验日志。正常取消浏览退出 0，启动中断退出 130；配置、不可确定的恢复或清理失败退出 1，非法参数或非终端调用退出 2。

## 结构化用户提问

模型可通过 Anthropic `AskUserQuestion` 或 OpenAI `request_user_input` 提出 1–4 个问题，每题 2–4 项选择，支持单选、多选、“其他”文字和补充说明。上下键选择，单选 Enter 确认，多选 Space 切换后 Enter 确认；Tab 编辑说明，Shift+Tab 返回上一题，PgUp/PgDn 滚动。全部回答进入复核页，明确提交后继续任务；未答题必须明确跳过，界面不会按默认焦点代答。

编辑文字时 Esc 返回选择；其余 Esc 或 Ctrl+C 停止当前任务。聊天草稿与问卷分别保存。接收回答后仍显示提交中，结果成功 Sync 后才关闭问卷。提问独占当前工具位置，回答不授予文件或命令权限；依赖答案的新参数应由模型在下一次采样生成。Ctrl+O 可只读回看原题、所有选项和冻结答案，左右键切换输入与结果。

回答文字合计最多 16 KiB，完整结果最多 128 KiB，并受当前 sample/turn 剩余额度约束；超限拒绝并保留草稿。`--print`、`--json`、stream-json 不等待 stdin，立即返回配对的 `denied/user_input_unavailable` 工具结果。恢复不会重新提问：started 前取消配对；started 后没有结果标为 `outcome_uncertain`；已有结果按原字节复用。未提交草稿不写入 Session。 详细取舍见[设计方案](openspec/changes/archive/2026-11-05-add-structured-user-questions/design.md)，测试与平台记录见[验收报告](openspec/changes/archive/2026-11-05-add-structured-user-questions/verification.md)。

## 命令与 Skill 补全

聊天输入的首词输入 `/` 可浏览内置命令和可显式调用的 Skill，输入 `$` 只浏览 Skill。候选显示说明、参数提示和来源，按名称精确、前缀、子序列及说明匹配筛选；↑/↓ 选择，Tab 填入。Enter 选择内置命令后执行其现有操作，选择 Skill 后只填入调用，再输入参数并按独立 Enter 提交。已有参数、空白与换行保留，参数提示不会写入正文。

候选打开时 Esc 先关闭，后续独立 Esc 沿用队列取回或任务停止规则；关闭后可用 Tab 重新打开，再次 Tab 填入。Ctrl+C 仍直接按当前任务停止/空闲退出规则处理。输入进入参数区后候选关闭；窗口不足 13 列或没有候选行空间时隐藏候选并恢复普通编辑。审批、问卷和详情界面使用各自按键。

目录使用启动时冻结的 Skill 来源规则，只有有效且允许用户调用的获胜条目参与补全；禁止模型自行调用的手动 Skill 仍可选择。同名内置 slash 优先，Skill 可用 `$name` 调用。显式粘贴只插入原文并暂时关闭候选，后续独立 Enter 提交实际文本；编辑名称或按 Tab 可重新请求候选。

本次范围仅覆盖命令与 Skill 首词，不包含文件、参数值、正文 mentions 或动态目录补全。设计及双 Provider、恢复、缓存与终端验证证据见[归档核对记录](openspec/changes/archive/2026-11-06-add-command-and-skill-completion/archive.md)和[追加验证报告](openspec/changes/archive/2026-11-06-add-command-and-skill-completion/revalidation.md)。

## 多行聊天输入

未激活补全时 Enter 提交；Alt+Enter、Ctrl+J 或光标前的反斜杠加 Enter 换行，优先于补全接受。左右键按字素移动，Backspace/Delete 删除前后字素；Home/End、Ctrl+A/E 到逻辑行首尾。上下键在候选激活时选择候选，其余时候先移动视觉行，到顶部/底部后浏览当前会话输入历史，再向下越过最新记录恢复原草稿及光标。编辑召回内容后脱离历史浏览。手动 Tab 仅按上述补全规则重开或接受候选，其他情况无操作；粘贴中的 Tab 保留为正文。

输入视口最多 8 行，按窗口大小缩放；草稿上限 4 MiB，超限整次拒绝。历史最多 100 条、8 MiB，折叠相邻重复，最旧优先淘汰；resume 由当前 thread 语义历史初始化。提交被会话接收后才清空草稿并记录历史；接收失败恢复草稿，已接收后的 turn 失败不自动重提。审批、命令详情、提交等待和取消清理期间由各自状态处理按键；候选关闭后 Esc 按队列/任务规则处理，Ctrl+C 中断活动任务并等待清理，空闲空草稿 Ctrl+C 退出。

TUI 将 CRLF/CR 规范化为 LF，保留缩进、Tab、尾空格及尾换行；双 Provider 和 headless 不再裁剪有效正文，headless 不额外规范化换行。全空白输入仍被拒绝。控制字符在输入区和用户 transcript 中显示为可见转义，提交正文不因显示而改变。

粘贴保证针对 Bubble Tea 已解码的 bracketed-paste 事件：一次原子插入，正文中的 Enter/Esc/Ctrl+C 不作为快捷键。当前 Bubble Tea v1.3.10 在结束标记前仍会累积原始缓冲，并可能跳过 RuneError（含字面 U+FFFD）；4 MiB 限额不覆盖该框架缓冲。无 bracketed paste 的终端不具备粘贴识别保证。Shift+Enter 取决于终端映射，请使用上述明确支持的换行键。

## 设计目标

- 提供接近 Claude Code 的 REPL、流式输出、工具调用、权限确认、diff、Session、Hooks、Plugins、Skills 和 Subagent 体验。
- 同时支持 Anthropic Messages 与 OpenAI Responses，不使用有损的统一消息模型抹平协议差异。
- 原样保留 Anthropic thinking signature、redacted thinking，以及 OpenAI reasoning summary、encrypted reasoning 和 message phase。
- 将缓存命中率作为一级架构目标，保证稳定前缀、工具定义、上下文顺序和序列化结果可预测、可回归。
- 通过小接口、单向依赖和事件协议保持内核纯净，避免上帝包、循环依赖及 UI 与 Provider 相互侵入。
- 保持本地优先、单二进制和可测试，让 TUI 与 headless 共享 Runtime，并为未来 IDE 或 app-server 宿主保留演进空间。

## 当前能力

当前代码与限制的完整对照见[当前能力清单](docs/roadmap/capability-status.md)（2026-11-06 核对）。Roadmap 的阶段目标不等于已实现功能。

| 领域 | 当前状态 |
|---|---|
| CLI/TUI 入口 | 已实现流式 Markdown Chat、多行/FIFO 输入、命令/Skill 首词补全、工具/问卷/Hook 审查、历史详情与 resume 选择器；支持一次性 text/JSON 和 stream-json 控制 |
| 双 Provider | Anthropic Messages 与 OpenAI Responses 已支持流式文本多轮、各自原生历史及 committed-only 文本历史投影 |
| HTTP/SSE | 已建立基于 Resty v3 的公共传输层 |
| JSON 与缓存 | 已实现不可变 canonical JSON、Context/CachePlan、来源 fingerprint、Anthropic 显式断点/TTL、OpenAI thread key/可选 WebSocket 增量及缓存查询 |
| 上下文 | 已实现本地估算与显式窗口预算、`/context`、手动与阈值自动压缩；完整历史保留，远端精确计数和跨 Provider fork 尚未实现 |
| Runtime | 已实现多 sample 工具循环、同轮自动追加与严格 FIFO 屏障、原子取回、定向停止和 durable-before-memory 输入提交 |
| 安全配置 | 已实现 JSON 配置、环境变量覆盖、API key 脱敏，以及 macOS/Linux 上绑定实际句柄的 no-follow 校验；其他平台当前安全失败关闭 |
| Tools | 九项能力包含 Read/Glob/Grep、Edit/apply_patch、Write、Darwin Exec、ReadArtifact、Skill 和结构化提问；连续只读最多 8 路并行，Patch/Write/Exec 需批准，Question 独占但不进入权限批准 |
| Session | 已实现 append-only JSONL、唯一当前无版本格式的强类型 draft/decoder、UUIDv7、跨进程 lease、同句柄 load/repair/write、`Sync`、尾部修复、fixture、显式恢复，以及可从 JSONL 重建的 SQLite Catalog、`--continue` 与离线 `sessions list`，以及 `resume` 启动选择器和有界元数据搜索/只读文本预览；全文搜索尚未实现 |
| 扩展系统 | P6 目标；本地 Skills 已实现发现、显式调用、按需工具读取和原生恢复；同步 PreToolUse/PostToolUse/PostToolUseFailure command Hooks 已实现；Plugins、MCP 和其余 Hook 事件待后续推进 |
| Subagent | P7 目标；当前仅为经批准的 TODO 控制边界，调度、线程树和生命周期尚未实现 |
| Telemetry | P8 目标；当前 logger 仅为经批准的 TODO 占位，未接入请求正文、Session 或运行时链路 |

本地 Skills 支持 `/<name> 参数` 和 `$name 参数`。TUI 中输入 `/skills` 查看来源与诊断，Enter 只填入输入框，再次提交才执行。启动时扫描用户 `~/.config/easycode/skills`、`~/.agents/skills`、`~/.claude/skills`，以及项目根到当前目录内的 `.easycode/skills`、`.agents/skills`、`.claude/skills`；每个技能为直接子目录中的 `SKILL.md`。项目优先用户、近目录优先远目录，同层 EasyCode > agents > Claude，损坏的优先项不会回退。

```markdown
---
name: review-local
description: Review the current local changes
argument-hint: scope
---
Review $ARGUMENTS. Explain findings and run the relevant tests.
```

仅支持静态 frontmatter：`name`、`description`、`argument-hint`、`user-invocable`、`disable-model-invocation`、`license` 和字符串 `metadata`。不支持的行为字段会禁用该条目。模型目录只包含元数据；Skill 工具加载完整正文或同来源相对资源，不提升工具权限。manual-only 技能仅在当前 turn 的显式附件成功持久化后，允许同来源、同 revision 的辅助资源读取。参数仅作字面量替换，不执行 shell。目录在启动时冻结，修改后重启；已提交附件和工具正文从 Session 恢复，不重读文件。

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

无需配置模型或 API key 即可发现历史会话：

```bash
./bin/easycode sessions list
./bin/easycode sessions list --all --limit 50
```

默认按最近提交活动列出当前目录的 20 个 root 会话，数量范围为 1–200；`--all` 显示所有创建目录。每项保留完整 thread ID、UTC 更新时间和 Provider/wire/model，可复制 ID 给 `--resume`。列表展示所有模型，但恢复仍需配置匹配，并使用恢复进程的当前目录，不会自动切换到创建目录。

列表不会启动模型、读取 stdin 或创建新会话。它会全量协调本地索引，可能创建/重建 SQLite，但不会修改 journal；不完整尾部仅在 resume 完整校验后、同一 exclusive lease 下修复；`--limit` 只限制展示数量，不限制扫描工作量。被占用或无效的 journal 会产生 stderr 警告，旧索引可能过时或缺失；这是元数据列表，不是完整可恢复性保证。当前安全存储支持 macOS/Linux，其他平台失败关闭。

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

当前 TUI 和 headless 模式都支持 Anthropic Messages 与 OpenAI Responses 的文本会话。成功文本回合会把 Provider-native commit、五项三态 normalized sample usage 和完成边界作为同一 batch 写入权限受控的 append-only JSONL；`--json` 的 `turn.completed` v1 同步输出该 usage，`--print` 仍只输出最终文本。显式 `--resume <thread-id>` 在连续 exclusive lease 下恢复同一 Provider 的原生历史，`--continue` 则通过可重建 SQLite Catalog 选择当前 cwd 与 Provider 配置完全兼容的最近 root Session，再进入同一恢复路径。TUI 通过单向 `TranscriptProjector` 与宿主范围查询重建记录，headless 不回放旧 transcript。`SemanticHistoryView` 始终排除全部推理。已返回的 Anthropic thinking 文字和 OpenAI summary_text 可在 Ctrl+O 中阅读；签名、redacted data、密文、raw reasoning 和未知扩展只保留在原生历史中。`/cache` 已显示最近已提交的三态 usage 与缓存证据，启动 `resume` 已支持元数据搜索和预览。Anthropic cache TTL、OpenAI prompt cache key、可选 WebSocket 原生增量及下述启动推理控制已实现；独立 usage 流、费用/配额、外部 reasoning/tool JSON 事件、正文全文搜索与 fork 尚未实现。

### 启动推理与普通输出

以下五个字段仅通过 JSON 配置，在启动时冻结；TUI 和全部 headless 请求共用同一策略。新字段全部省略时，保留既有请求：Anthropic 普通 `max_tokens=4096`，OpenAI 不发送普通输出上限，也不补推理强度或 summary。

| 字段 | 允许值与规则 |
| --- | --- |
| `anthropic_thinking` | `{"type":"disabled"}`；`{"type":"adaptive","display":"summarized"}`；或 `{"type":"enabled","budget_tokens":8192,"display":"omitted"}`。display 可省略，仅 adaptive/enabled 允许 summarized/omitted；manual budget 为整数且 `1024 <= budget < 普通上限` |
| `anthropic_effort` | low/medium/high/xhigh/max；独立于 thinking。显式 disabled 与 xhigh/max 不相容 |
| `openai_reasoning_effort` | none/minimal/low/medium/high/xhigh/max |
| `openai_reasoning_summary` | auto/concise/detailed；可独立于 effort 配置 |
| `max_output_tokens` | 普通每 sample 输出上限，整数 1..65536；Anthropic 映射为 max_tokens，OpenAI 映射为 max_output_tokens |

例如在 Anthropic 配置中加入 `"anthropic_thinking":{"type":"adaptive","display":"omitted"},"anthropic_effort":"high","max_output_tokens":16384`；OpenAI 可加入 `"openai_reasoning_effort":"high","openai_reasoning_summary":"auto","max_output_tokens":16384`。不要同时配置两家的专属字段；最终 family 在环境覆盖后校验，即使 disabled/none 也不能跨 family。空值、未知/重复字段、大小写别名及非整数预算均拒绝，不静默改写。

若配置 context window，显式普通输出上限必须由 reserved_output_tokens 覆盖；显式 Anthropic 推理也须覆盖实际普通上限（缺省 4096）。显式 OpenAI 推理加窗口时还必须提供普通上限。无窗口不推断容量；全部新字段省略时保留既有小窗口行为。

手动和自动摘要沿用同一 thinking/display/effort/summary，独立使用 compaction_output_tokens。manual budget 大于或等于摘要上限时，在 started/网络前返回 compaction_budget_exceeded，不夹取或自动扩大预算。例如 budget=8192、普通上限=16384 可以正常启动，但默认摘要上限 4096 会拒绝摘要。自动预检拒绝会抑制同来源重试，旧窗口仍可请求才继续原任务。高 effort 仍可能耗尽摘要额度，不能保证可见回答。

`/context` 只读展示冻结值和 explicit/default/未发送来源，内容超屏可上下/PageUp/PageDown 滚动，Esc 关闭。修改配置后需重启；resume 使用新进程的当前设置，旧 journal 保持原样。只有显式 OpenAI summary 的实例声明本地 ReasoningSummary 能力，不能视作 endpoint 支持证明；400 等错误不会删参数后重试。

Anthropic 最终 thinking_tokens 明确报告时归一为 reasoning_output 的 known（包括零），缺失为 unknown；output 已包含 thinking，不能相加。display omitted、空正文和 usage 独立，summarized 也不保证返回文字。当前 native usage 必须保存 thinking_tokens 状态；旧开发格式直接拒绝，不提供历史 reader 或迁移。

### 推理阅读

只有服务端实际返回推理项时，主线才出现紧凑提示；块完成后等待保存，sample 成功保存后撤下提示。Ctrl+O 的统一记录可查看活动项和已保存推理，上下选择、Enter 阅读、Esc 返回、r 切换完整正文的 Markdown/安全原文。长正文按 64 KiB 原文范围分页，片段固定原文；只有签名或密文时显示 unavailable，不提供正文。

活动内容共享 64 KiB/50 项显示预算，选中项优先保留。取消或失败按真实结果提示未保存、已保存但刷新失败、保存状态不确定；下一 turn 释放上一份失败快照。主线先排除推理再取最近 50 项，完整历史仍在详情分页。阅读、原文切换和 resize 不新增模型请求，不改变冻结推理或缓存策略。不发送 sequential_cutoff，也不因展开详情而请求替代正文。

本地验证入口为 `scripts/reasoning-smoke.py`、`scripts/reasoning-controls-smoke.py`（Darwin 双 Provider 真实 PTY），以及 `internal/app/reasoning_e2e_test.go`、`internal/app/reasoning_controls_e2e_test.go`（双 Provider 三种 headless 模式）。新增 `internal/app/reasoning_cli_e2e_test.go` 使用真实二进制验证工具续写、跨进程恢复、参数拒绝和非法配置，`internal/app/reasoning_malformed_stream_test.go` 验证重复 JSON 成员不会变成成功样本或工具执行；完整结果见[回归记录](openspec/changes/archive/2026-11-11-add-provider-reasoning-controls/regression-2026-11-11.md)。不以 Darwin 验收宣称其他平台的原生终端支持。

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

当前共享层负责多 sample Tool Loop、最多 8 路只读并行、独占写入屏障、批准/ledger、Session 和宿主事件；通用 sandbox、其余 Hook 事件与 Subagent 是后续阶段目标。每个 Provider 当前独立负责原生请求、流归并、原生历史和语义投影；当前两家 Provider 均已有以下请求、归并、持久化、工具、压缩、缓存和计量接缝；运行中推理切换等剩余能力仍按 Roadmap 推进：

- 原生请求编译与 Header 策略。
- SSE 事件归并和状态机。
- Thinking/Reasoning 原生数据保存。
- Tool wire 编解码。
- 缓存规划和上下文压缩。
- Usage、停止原因和兼容能力解析。

Provider-native item 是恢复会话和构建下次请求的事实依据，RuntimeEvent 只用于 UI、日志及宿主投影，二者不能相互替代。

共享层需要读取历史时使用 Provider 提供的单向 `HistoryProjector` 和 `SemanticHistoryView`。当前已实现 committed-only 文本投影和 resume 后的 TUI 历史回放；ContextPlanner/token 估算已接入，Stop Hook 文本和 Subagent completion 消费者将在后续阶段接入；当前 PreToolUse 消费三种强类型工具输入，PostToolUse 另接收成功结果的封闭元数据投影。该视图不可反向生成 Provider 请求。

更完整的设计见[总体架构文档](docs/architecture/overall-architecture.md)。

## 文件写入

两家 Provider 均通过 `Write(file_path, content)` 创建或整文件覆盖 UTF-8 工作区文件；局部修改继续使用 Anthropic `Edit` 或 OpenAI 原生 custom `apply_patch`。交互显示 **Create file / Overwrite file**、完整转义内容或 diff、待建目录，默认选择 **No**，支持拒绝反馈和独立的 Write 会话授权。`--print`、`--json`、stream-json 中需要批准的写入返回 `approval_required`，不会创建目标或父目录。

覆盖必须先有同一 Runtime 中已回传给模型的完整 Read；同 sample 的 Read、多片段拼读和截断 Read 不算完整读取。当前 Read 最多 2000 行、每行 2000 个码点、总预览 256 KiB，底层文件上限 16 MiB，因此不是所有 16 MiB 文件都能获得覆盖授权。相同内容返回 `write_no_changes`，不会询问或写入。Read 证据和 session grant 不从磁盘恢复。

Write 输入（原始 JSON 与规范编码）上限 1 MiB，审批完整展示上限 256 KiB；路径只能相对工作区，最多 64 个组件，新建目录最多 16 层。保留内容原始字节（含 CRLF、BOM、无末尾换行），新文件为 `0600`、新目录为 `0700`，覆盖只保留基本 `0777` 权限，不承诺 owner/ACL/xattr 保留。拒绝 symlink、特殊文件、NUL/无效 UTF-8 及带特殊权限位的覆盖目标。

创建使用禁止覆盖的原子发布，缺失目录链先在私有树中准备，再一次发布；审批后目标或祖先变化返回 conflict。发布后的目录 Sync 或清理失败报告不确定结果并停止后续工具。进程被强制杀死可能留下 `.easycode-write-*` 私有暂存项，resume 不扫描、不清理、不重放这些写入。macOS 文件系统与故障注入已验收；Linux adapter 已交叉编译，但当前开发环境无 Linux runner，真实 Linux 文件系统验证尚待补充；不支持安全发布的平台失败关闭。

## 本地工具 Hooks

启动时读取用户 `~/.config/easycode/hooks.json` 和工作区 `.easycode/hooks.json`，当前进程使用冻结快照。两个来源均默认未信任，用户来源先于项目来源执行。例如：

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Edit|Write|Bash",
      "hooks": [{
        "id": "check-change",
        "type": "command",
        "command": "cat >/dev/null; printf '{}'",
        "timeout": 5
      }]
    }]
  }
}
```

`/hooks` 提供本地目录、完整命令与限制详情；空闲时按 `r` 清除选中项的信任或拒绝，下一次真实工具调用会重新审查。首次运行的审查默认拒绝，只允许本会话信任；普通工具仍按原规则审批。信任不会跨重启或 resume 恢复，也不冻结脚本依赖，审查后脚本文件仍可能变化。

| 边界 | 当前行为 |
| --- | --- |
| 匹配 | `*` 或字面 `Edit/apply_patch`、`Write`、`Bash/exec_command`，以 `|` 分隔；别名只执行一次；读取工具不触发 |
| 来源 | 严格 JSON、no-follow/实际 FD 校验；每来源 64 KiB，三个事件合计 32 项/来源、64 项/两来源，每事件每调用最多 8 项；任一已存在来源无效则阻断全部三项 mutation |
| 执行 | Darwin，非 login `/bin/sh`，最小环境，工作区只读、禁网；仅 `/dev/null` 写入；其他平台失败关闭 |
| 预算 | 单项默认 5 秒，可设整数 1..30；每次工具调用总执行预算 30 秒，信任等待不计费；stdin 最大 512 KiB，stdout/stderr 各 16 KiB |
| Pre 放行 | exit 0 且空输出或 `{}`；放行后仍进入普通工具审批 |
| Pre 拒绝 | exit 2，或 exit 0 返回下列严格结构；其他非零退出、无效输出、超时或 I/O 错误阻断该调用 |
| headless | 匹配项返回 `denied/hook_trust_required`，不运行脚本或目标工具，不等待交互；无匹配保持 `approval_required` |
| 隐私与恢复 | 默认只保存安全 code、引用和统计；显式 model 反馈另经信任与预算接纳。原始 stdout/stderr/reason 不自动进入模型、Session、日志或 artifact；恢复仅补偿，不重新运行检查或工具 |

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Local check failed"}}
```

stdin 提供真实 `tool_name`、会话/回合/调用身份、cwd 与规范 `tool_input`：Patch 使用 `file_path/replacements`，Write 使用 `file_path/content`，Exec 使用 `command/workdir/timeout_ms`。参数只通过 stdin 传递。拒绝 reason 必须非空且最多 4 KiB，但仅用于校验，不作为模型反馈。Pre 不允许 feedback 字段。其余事件、async、allow/ask、参数/结果改写和 plugin hooks 尚未实现。

### 成功后的 PostToolUse 检查

PostToolUse 只检查真实执行成功的 Edit/apply_patch、Write 和 Bash/exec_command；正常非零 Exec exit 也按现有成功语义触发。完整后置计划在工具候选与普通批准之前审查，Pre/Post 信任相互隔离。真实工具结果先 Sync，随后检查；后置失败不会撤销或改写工具成功，下一工具和模型采样等待检查收口。主线保留工具成功并显示检查状态；Ctrl+O 中按右键进入独立 `post_hooks` 详情。

在同一配置的 `hooks` 对象内增加以下数组；id 在同一来源的三个事件之间也必须唯一：

```json
"PostToolUse": [{
  "matcher": "Edit|apply_patch|Write|Bash|exec_command",
  "hooks": [{
    "id": "check-todo",
    "type": "command",
    "command": "cat >/dev/null; if /usr/bin/grep -q TODO main.go; then printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PostToolUse\",\"additionalContext\":\"Read-only check: main.go still contains TODO.\"}}'; else status=$?; case \"$status\" in 1) printf '{}' ;; *) exit \"$status\" ;; esac; fi",
    "timeout": 5,
    "feedback": "model"
  }]
}]
```

`feedback` 缺省为 `none`。只有显式 `model` 并经当前会话信任的严格 `additionalContext` 才发送给服务端并保存到 Session。空白或 `{}` 表示无反馈；Post 的任意非零退出（包括 2）是检查错误，不使用 Pre 的 deny 语义。普通 stdout、stderr、未知控制字段和未授权正文均丢弃，只保留安全分类。每项正文至多 4 KiB，最终反馈段含来源与包装至多 16 KiB；超额整块拒绝，不驱逐先前已保存正文。

下面是配置中同一命令的多行写法，可用 `/bin/sh` 执行。发现 TODO 返回 exit 0 的结构化上下文，grep 执行异常才非零；它只读文件、不写工作区或缓存、不联网。`main.go` 应当存在；更换检查对象时也需要审查完整命令。

```sh
cat >/dev/null
if /usr/bin/grep -q TODO main.go; then
  printf '%s' '{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"Read-only check: main.go still contains TODO."}}'
else
  status=$?
  case "$status" in
    1) printf '{}' ;;
    *) exit "$status" ;;
  esac
fi
```

Post stdin 的固定顶层字段为 `hook_event_name/session_id/thread_id/turn_id/tool_use_id/tool_name/cwd/tool_input/tool_response`。`tool_use_id` 是 Provider call ID，`cwd` 仅发送给本地检查。`tool_input` 沿用三个封闭分支，`tool_response` 只包含 `capability/status/code/metadata`，其中 status/code 固定为 success/ok：

| 能力 | metadata 完整字段 |
| --- | --- |
| Patch | `file_path/replacement_count/added_lines/removed_lines` |
| Write | `file_path/operation/bytes/lines/created_directories` |
| Exec | `workdir/exit_known/exit_code/stdout_bytes/stderr_bytes/output_truncated/timed_out/cancelled` |

不传递原 preview、输出正文、artifact ID、PID、Provider opaque 数据或私有存储路径。合并 stdin 超过 512 KiB 时不启动后置脚本，保留工具成功并明确跳过后缀。两阶段共用每调用 30 秒执行预算；预算耗尽的未启动项不会伪造 timeout 或执行记录。

有 model 项时，从原调用额度 A 预留 `R=min(16 KiB,max(0,A−1 KiB))`，原结果按 A−R 冻结；检查返回 `{}` 也不会重读、扩展原预览。例如 A=256 KiB 时原预览额度为 240 KiB。没有匹配或全部 none 时保持既有模型输出 bytes。小额度连安全提示槽都不足时标记 `budget_unavailable`，只执行检查，不披露正文或模型提示。恢复重用已保存前缀，不补发新的提示，不自动重试工具或脚本。

### 执行尝试失败后的 PostToolUseFailure 诊断

PostToolUseFailure 复用同一配置、信任审查、只读沙箱与后置详情。执行尝试已持久化且原结果为 error/conflict 时选择失败分支，包括进程启动失败和确认清理的超时。正常退出的 `go test`/`rg` 即使 exit=1 也选择 PostToolUse；准备失败、拒绝、原工具 cancelled/outcome_uncertain 不选择后置事件。这里的准入只证明执行尝试已接收，不证明进程启动或文件改变。该能力提供诊断，不解析测试语义，也不自动修复或重试。

在 `hooks` 对象加入以下分支：

```json
"PostToolUseFailure": [{
  "matcher": "Edit|apply_patch|Write|Bash|exec_command",
  "hooks": [{
    "id": "diagnose-failure",
    "type": "command",
    "command": "cat >/dev/null; printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PostToolUseFailure\",\"additionalContext\":\"Inspect the original failure before retrying.\"}}'",
    "timeout": 5,
    "feedback": "model"
  }]
}]
```

失败 stdin 沿用八个公共字段，另带 `error` 和固定 `is_interrupt=false`，没有 `tool_response`。`error` 仅由安全 code 生成 `Tool execution failed: <code>`，不包含原错误正文、preview、stdout/stderr 或 artifact。严格输出只能声明自己的事件；none/model、4 KiB/项及 16 KiB/调用限制与成功分支相同。

候选前冻结成功、失败两个可能的非空分支，逐项审查最多 16 个精确声明。任何分支需要 model 时只预留一次 R，各分支独立判断提示槽是否足够。即使选择另一分支，原预览仍按 A−R 冻结。选择固定为 success/failure/none：none 不表示工具没执行；所选事件未配置时以 skipped/hook_event_unmatched 收口，零脚本、反馈或伪后缀。原错误 Sync 后再取消仍为 failure/cancelled，零新脚本。检查状态与原工具 Error/Conflict 并列，检查通过不会显示为已修复。

`post_hook_committed` 只携带调用、计划摘要、详情序列和阶段。计划保存时显示中性状态，不提前声称运行 PostToolUse；实际事件从已提交快照读取。恢复复用同一选择和冻结前缀，不执行工具、脚本，不读旧配置或 artifact，不新增模型提示。

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
│   ├── executor/process/      Exec/Hook 共用的受控进程生命周期
│   ├── extension/             本地 Skills 与 PreToolUse Hooks；Plugins、MCP 为 P6 批准占位
│   ├── headless/              prompt 解析、最终文本与 JSONL v1 宿主
│   ├── protocol/              Command 与 RuntimeEvent 协议
│   ├── provider/              Provider Kernel 与 HTTP/SSE/WebSocket 传输
│   ├── runtime/               共享 turn 编排
│   ├── secret/                敏感值脱敏
│   ├── session/               JSONL 事实源、lease、修复与恢复规划
│   ├── subagent/              P7 批准占位：子代理控制边界
│   ├── telemetry/             P8 批准占位：结构化日志边界
│   ├── tool/                  九项真实工具、批准、结果预算与执行边界
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

| 阶段 | 主题 | 当前交付与剩余范围 |
|---|---|---|
| P0 | 工程与协议基线 | 工程、协议与质量门已落地；doctor 等诊断入口待实现 |
| P1 | 双 Provider Kernel | 双 wire 流式、原生历史/工具/缓存及启动推理控制已交付；运行中推理切换待实现 |
| P2 | Session 与 Headless Loop | root Session、JSONL/SQLite、恢复、三种 headless 与 FIFO 已交付；全文索引/子线程待实现 |
| P3 | Coding Tools | 九项工具与批准/恢复/预算已交付；PTY、后台进程、通用跨平台 sandbox 待实现 |
| P4 | 缓存与上下文 | 缓存、usage 归一、手动/自动压缩已交付；费用、精确计数与跨 Provider fork 待实现 |
| P5 | Claude 风格 TUI | Markdown、输入及命令/Skill 补全、审批/问卷/Hook、详情、被动推理阅读和 resume picker 已交付；fork picker 待实现 |
| P6 | 扩展系统 | 本地 Skills、同步 PreToolUse/PostToolUse/PostToolUseFailure 已交付；其余 Hook、Plugins/MCP/热加载待实现 |
| P7 | Subagent | 尚未实现；保留批准占位，未接入 Runtime/app |
| P8 | 发布强化 | 部分安全/故障验证已落地；全平台发布矩阵、诊断、遥测待实现；兼容迁移须另行明确授权 |

## 文档

- [文档索引](docs/README.md)
- [当前能力、限制与代码证据](docs/roadmap/capability-status.md)
- [Tool 系统设计](docs/architecture/tool-system.md)
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

### 运行中追加和取回输入

任务运行时仍可编辑草稿；没有激活补全且不匹配换行规则时，Enter 将输入排队。完整工具结果保存后，追加内容可以进入同一轮的下一次模型请求。没有下一次工具采样时，输入按顺序开启新轮。queued/claimed 表示尚未保存；正文进入对话记录才表示 committed。

补全激活时 Up 选择候选，Esc 只关闭候选；其余时候，空草稿首行 Up 或普通 Esc 将尚未领取的排队输入取回编辑。Ctrl+C（或无补全和可取回输入时 Esc）停止当前轮。停止后返还的内容使用 Ctrl+R 逐条放回草稿，Ctrl+D 逐条丢弃，均不会自动发送。审批和命令详情优先使用各自快捷键。队列和返还区只在本进程保留；已保存的输入可以恢复，未保存内容不会在重启后自动执行。


### 上下文查询与手动压缩

- `/cache`：查看进程内最近已提交的缓存观测、来源变化、三态 usage 与实际 transport；上下滚动，Esc 关闭。不创建 turn 或输入回忆。
- `/context`：查看显式缓存策略，以及已提交窗口的分类 token 估算、模型、revision、窗口与预留；运行中可查询，排队输入和 staging 不计入。估算不是 usage，未配置窗口时不显示比例。
- `/compact` 或 `/compact 保留失败测试与下一步`：空闲且没有排队输入时总结当前窗口。完成后显示简短边界，Ctrl+O 查看摘要与全部旧历史。没有后续输入时保持等待。
- 压缩期间仍可编辑和排队；Ctrl+C 取消当前压缩。Esc 先按当前焦点关闭详情或补全，再取回 queued auto，无可取回输入时取消压缩。失败保持旧窗口并返还自动输入，strict-next 保留。

JSON 配置可设置 `"compaction_output_tokens": 4096`，范围 1–65536，不提供环境变量覆盖；这是摘要输出上限，与普通输出预留独立。网络期限为 120 秒；超预算、摘要无效或无法缩减时明确失败，不重试或自动删历史。默认 4096 与小窗口冲突只拒绝压缩，不阻断普通启动。

配置显式 `context_window_tokens` 时默认启用自动压缩，可用 `"auto_compact": false` 关闭；显式 true 但没有窗口会拒绝配置。阈值为 `floor((window - safety - max(reserved_output, compaction_output)) × 80%)`，只是本地粗估软阈值，实际请求仍做预算与编码预检。未配置窗口或摘要预留不可容纳时不会自动压缩，`/context` 显示状态、原因和可用阈值。

自动压缩发生在下一采样准备阶段：新输入保持逐字原文，工具结果完整持久化后才允许切窗；摘要成功后继续同一任务，不重新执行工具。Ctrl+C 取消活动任务及其摘要；completed 成功 Sync 后的取消不会回滚新窗口。网络失败、超时或无缩减且旧窗口仍可请求时继续工作，否则结束任务并返还未保存输入。相同来源失败不重复尝试，连续三次自动失败后在当前进程会话熔断；成功手动压缩可重置。不会自动裁历史、换模型或重试摘要。

Session/native/checkpoint envelope 不含 schema_version、payload_version 或同义版本标记，压缩来源不保存固定 prompt revision，Catalog 不依赖 user_version；只有一种当前字段结构，不引入版本门禁、迁移或兼容 reader。stream-json 自动压缩事件的 `scope` 包含 trigger、turn_id、sample_index，并关联原 submit 的 request_id；没有额外 compact response，一次性 text/JSON 不输出这些控制帧。


stream-json v1 使用显式控制，不把普通输入中的 slash 当命令：

```json
{"version":1,"type":"cache.query","request_id":"cache-1"}
{"version":1,"type":"context.query","request_id":"context-1"}
{"version":1,"type":"context.compact","request_id":"compact-1","instructions":"保留未解决问题"}
{"version":1,"type":"context.cancel","request_id":"cancel-1","expected_compaction_id":"<accepted response 中的 compaction_id>"}
```

控制响应先于 `cache.snapshot`、`context.snapshot` 或 `compaction.started/completed/failed`。压缩输出关联独立 CompactionID，不含 TurnID、摘要正文或 opaque 内容；失败带 `durable`。stdin EOF 等待已接受操作完成，显式 shutdown 取消并等待清理。

Session envelope 没有版本字段，headless 仍遵循独立外部协议。旧开发 journal 在修复或追加前失败关闭。压缩保留原始 JSONL 和工具详情；resume 本地恢复 checkpoint，未完成摘要本地收口，不重执行工具。阈值自动压缩已实现；其余 Hook 事件和跨 Provider fork 尚未实现。

### 显式 Provider 缓存配置

以下字段只通过 JSON 配置，不提供新的环境变量或 CLI 开关；最终环境变量合并后再校验 Provider 组合，未知值、null、重复字段与无关 Provider 的非默认策略会失败。

| 字段 | 默认 | 可选值 |
|---|---|---|
| anthropic_prompt_cache | disabled | ephemeral_5m、ephemeral_1h（仅 Anthropic） |
| openai_prompt_cache_key | omit | thread（仅 OpenAI） |
| openai_transport | http_sse | websocket_auto（仅 OpenAI） |

Anthropic 5m 使用 ephemeral 标记并省略 TTL，1h 显式请求 1 小时，可能有更高缓存写入费用；一条活动会话内策略固定。OpenAI thread key 可在同 ThreadID 恢复后保持一致。websocket_auto 可与 key 独立启用：保留 HTTP/SSE，只有同一连接与 durable 原生前缀满足条件才增量续写，始终 store=false。仅生成写入前的明确连接不可用/404/405/426 可回退；鉴权、TLS、重定向、取消或生成写入后错误不自动重发。

/cache 明确区分配置与已观测状态，恢复后显示 restored_baseline。已知零、unknown、not_applicable 与未观测各自保留；只有适用输入计量全部已知且分母非零才有读取比例。关闭显式策略不代表服务端不会进行隐式缓存；相同 fingerprint 不证明命中，也不保证 TTL、延迟或费用收益。查询不显示真实 key、正文、路径、凭据或 opaque reasoning；一次性 --print/--json 不新增这些输出。
