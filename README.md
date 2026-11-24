# EasyCode

EasyCode 是一个本地运行的终端编程助手。它能读取和搜索工作区、在批准后修改文件，并恢复本地会话。使用自己的 API 地址、密钥和模型即可启动。

## 快速开始

安装 Go 1.24+、Git 和 Make，然后构建 EasyCode。

```bash
git clone https://github.com/cutejiuges/easycode.git
cd easycode
make
export PATH="$PWD/bin:$PATH"
```

创建本地配置，再填入 `base_url`、`api_key` 和 `model`。[配置样例](config.example.json)

```bash
mkdir -p ~/.config/easycode
cp -i config.example.json ~/.config/easycode/config.json
chmod 600 ~/.config/easycode/config.json
```

`provider` 可选 `openai`（OpenAI Responses）或 `anthropic`（Anthropic Messages）。`base_url` 使用 API 路径前缀，例如 `https://api.openai.com/v1`。仅提供 Chat Completions 的服务暂不支持。其他配置文件可用 `--config PATH` 指定。[配置参考](openspec/specs/config/json-file/spec.md)

在要处理的项目目录中运行 `easycode`。

```bash
easycode
easycode --print "概述这个项目"
easycode sessions list
easycode resume
```

交互模式中，输入 `/` 浏览命令，输入 `@` 引用工作区文件。用 `/rename` 为会话命名，用 `/resume` 切换会话。

在已有对话的空闲会话中，输入 `/branch [标题]` 或 `/fork [标题]` 从当前已提交历史创建分支。成功后进入分支，界面给出返回源线程的 `/resume <thread-id>`。分支沿用当前工作区和模型，各自保存后续对话及标题。可通过会话列表、选择器或 `easycode --resume <thread-id>` 恢复。工作区文件由两条对话共用，修改仍按当前线程的工具审批执行。 [会话分支规格](openspec/specs/session/branch/spec.md)

在空闲会话中输入 `/plan` 或按 `Shift+Tab` 进入只读 Plan Mode。模型可以读取和搜索工作区、查看已授权输出和技能、提问并维护任务进度。此时修改文件和执行命令的工具不可用。

模型给出完整方案后，可滚动审阅并选择 `Keep planning` 或 `Implement with normal approvals`。输入 `/plan review` 可重新打开当前有效方案。确认实施会在同一会话开启新的 `Default` 回合，即使首个请求前发生自动压缩，也会携带所审方案的完整原文。修改文件和执行命令仍各自经过普通审批。

也可以用 `/plan off` 或 `Shift+Tab` 回到 `Default`，自行发送实施指令。会话恢复从 `Default` 开始，先前方案可在转录或 `Ctrl+O` 中查阅。 [Plan Mode 规格](openspec/specs/tui/plan-mode/spec.md)

## 本地命令 Hooks

`/hooks` 浏览启动时加载的用户和项目声明。支持 `PreToolUse`、`PostToolUse`、`PostToolUseFailure`、`Stop`、`PreCompact`、`PostCompact` 和 `SessionStart`。交互模式逐项审查，默认拒绝。当前命令执行支持 macOS，工作区只读且禁网。

将下面的声明合并到项目的 `.easycode/hooks.json` 或用户的 `~/.config/easycode/hooks.json`，重新打开会话。运行 `/compact 保留 main.go 的变更`，在新窗口保存后审查并授权检查。示例用 macOS 的 `plutil` 读取完整 JSON 输入并提取 `compact_summary`，以退出状态报告摘要是否包含 `main.go`。

```json
{
  "hooks": {
    "PostCompact": [
      {
        "matcher": "manual",
        "hooks": [
          {
            "id": "check-summary",
            "type": "command",
            "command": "summary=$(/usr/bin/plutil -extract compact_summary raw -expect string -o - -- -) || exit 1; case \"$summary\" in *main.go*) exit 0 ;; *) exit 1 ;; esac"
          }
        ]
      }
    ]
  }
}
```

`manual` 匹配 `/compact`，`auto` 匹配已启用的阈值自动压缩，`*` 匹配两者。脚本通过 stdin 读取当前已保存的摘要。TUI 分别显示压缩和检查的结果，脚本输出会丢弃。检查失败、拒绝或取消时保留已保存的新窗口。非交互模式跳过匹配的后置检查并照常提交成功压缩。

### 会话上下文

`SessionStart` 的 matcher 支持 `startup`、`resume`、`compact` 和 `*`。在首条通过预检的普通对话中，新建根会话处理 `startup`，创建并进入分支或恢复已有线程处理 `resume`。手动和自动压缩都在新窗口保存、PostCompact 收口后处理 `compact`。

以下项目声明在交互终端获准后为模型提供一条工作区说明。stdin 的 `source` 可用于区分触发时机。要向模型提供正文，使用 `feedback: model` 并在成功退出时输出事件对应的 `additionalContext` 对象。

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "id": "project-context",
            "type": "command",
            "command": "printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"SessionStart\",\"additionalContext\":\"Build and test with make verify.\"}}'",
            "feedback": "model"
          }
        ]
      }
    ]
  }
}
```

`Ctrl+O` 可以查看实际保存的上下文。`*` 的信任或拒绝适用于当前会话实例内该声明的后续匹配。已保存的正文可随有效历史进入后续请求或摘要。非交互模式跳过新命令执行，已保存的上下文随原生历史恢复。

## 继续阅读

- [当前功能与平台支持](docs/roadmap/capability-status.md)
- [文档索引](docs/README.md)
- [路线图](docs/roadmap/product-roadmap.md)
- [贡献指南](AGENTS.md) · [许可证](LICENSE)
