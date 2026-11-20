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

## 继续阅读

- [当前功能与平台支持](docs/roadmap/capability-status.md)
- [文档索引](docs/README.md)
- [路线图](docs/roadmap/product-roadmap.md)
- [贡献指南](AGENTS.md) · [许可证](LICENSE)
