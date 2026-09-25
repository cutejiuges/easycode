## 1. 配置模型与文件读取

- [ ] 1.1 增加严格的 JSON file DTO 和字段解码，拒绝未知字段、错误类型、重复/空配置，并验证 malformed JSON 的稳定英文错误与 secret 脱敏。
- [ ] 1.2 实现默认路径 `~/.config/easycode/config.json`、显式路径和 `~` 展开；区分默认文件不存在的兼容场景与显式文件不可读的失败场景，并用临时目录测试验证。
- [ ] 1.3 实现包含 API key 时的 Unix mode 检查、私有目录/文件创建约束和安全错误分类；用权限 table test 验证 `0600` 接受、group/other 读写拒绝和不创建默认文件。

## 2. 配置合并与应用装配

- [ ] 2.1 实现 JSON 基础配置与非空环境变量的字段级合并，复用现有 Provider/base URL 校验；用优先级、空环境变量和非法 URL 单测验证最终 Config。
- [ ] 2.2 将 `ConfigPath` 从 CLI/app 入口传入配置加载边界，确保 `--version` 在任何配置读取前退出，显式 `--config` 不静默回退；用 app 单测验证缺失、无效和有效配置。
- [ ] 2.3 保持 Provider、Runtime、TUI 只接收统一强类型 `config.Config`，验证配置路径和 API key 不进入 Provider 请求错误、诊断输出或 snapshot。

## 3. CLI、文档与回归

- [ ] 3.1 增加 `--config <path>` 帮助文本和默认路径说明，保持 `--print`、`--version` 现有语义；用 CLI smoke test 验证 help、version、默认文件和显式文件路径。
- [ ] 3.2 更新 README 与配置示例，说明 JSON schema、环境变量覆盖优先级、`chmod 600` 和 API key 脱敏，并确保示例不包含真实 secret。
- [ ] 3.3 增加配置 fixture、环境隔离和无外网 app 集成测试，覆盖默认文件、显式文件、纯环境变量、错误恢复和两种 base URL。
- [ ] 3.4 执行 `make verify`，确认 gofmt、go vet、Staticcheck、全量测试和 race test 全部通过，并检查新增配置代码没有 secret 泄漏、无用依赖或未解释兼容分支。
