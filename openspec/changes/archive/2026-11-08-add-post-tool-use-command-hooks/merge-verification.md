# 合并远端 main 的一致性验证

日期：2026-11-08。用户要求拉取远端 main，合并到当前 PostToolUse 分支，解决冲突后推送并创建 PR。合并父提交为本分支 `3dd9992` 与远端 main `d79dc5c`（已合入被动推理阅读）；本轮沿用两项已批准能力，不新增能力范围或历史兼容结构。

## 冲突与接缝处理

- README、能力清单与 Roadmap 同时保留 Pre/Post Hooks 和被动推理阅读的已交付状态；主动推理配置、其余 Hook 事件和 P6 剩余范围不改为已实现。
- `TranscriptListItem` 同时保留 Reasoning、PostHook、detail_sequence 的独立字段与各 kind 校验；新增回归拒绝把 Post 修订/摘要塞入推理项，或把推理摘要塞入工具项。
- `transcriptPageKey` 同时保留 main/detail 模式与后置详情修订号；Post 刷新回归分别验证纯工具列表和含推理项的列表，工具锚点、旧回复丢弃及四页缓存约束不变。
- Runtime 的测试 journal 复用 main 的锁内单调 seq 分配，保留 Post 测试需要的 kind、turn 和 payload，不重复声明 records 或在回调之后重算序号。
- Post 的 app 查询显式使用当前 detail 模式，TUI 测试使用共享 detailItems 适配。推理 Hook modal 固定快照仅增加已交付的 `Feedback: none` 一行，既有焦点、粘贴、取消断言保持。
- 自动合并部分另核对 Provider 输出接口、原生恢复、Runtime reasoning 生命周期、headless 的严格消费及主线恢复；不以“无文本冲突”代替语义核对。

## 共存回归

复用同一推理 fixture 给三项 mutation 的实际沙箱样本注入允许展示的正文及 opaque/raw 哨兵，没有复制第二套测试协议。OpenAI/Anthropic × Patch/Write/Exec × none/model（及既有非法输出/双流哨兵分支）验证：

- 后置信任仍早于普通工具批准；工具成功 result 先持久化，后置错误不改写原成功，唯一结果与三个后置提交批次保持。
- 同一请求续写保留 Provider-native 推理及 opaque 数据、冻结 Post 反馈；详情只读允许的推理文字，不显示 raw/encrypted 哨兵。
- main 列表先过滤推理项；detail 列表同时保留推理、工具和后置摘要。推理页修订号为零，后置页绑定其自己的 durable detail_sequence。
- 恢复后推理摘要/正文、原工具 cursor/preview、Post 状态一致，不重跑脚本、不自动发起模型请求、不修改 journal bytes。

定向 app 共存/推理 headless、protocol、TUI、Runtime 和 headless 测试通过。初轮完整门曾命中新加测试中缺少工具 summary 的 fixture 错误，已补全合法 fixture 并重新运行；不放宽生产 validator。最终质量门结果单独记录，初轮失败不作为通过证据。

## 最终执行记录

| 检查 | 结果 |
| --- | --- |
| `make verify` | 退出码 0；格式/vet/Staticcheck/架构边界/全量普通及 race 均通过 |
| `go test ./internal/app -run 'TestPostBothProvidersRealSandboxThreeCapabilitiesAndDisclosure\|TestReasoningHeadlessBothProvidersAllModesWithToolAndEOF' -count=1` | 退出码 0 |
| protocol/TUI 的 Post、Reasoning 定向回归 | 退出码 0；跨 kind 与混合列表修订回归通过 |
| `scripts/smoke-hooks.py` 的 Pre/Post 双事件 × 双 Provider | 退出码 0，4/4 |
| `scripts/reasoning-smoke.py` | 退出码 0，8/8；两家 Provider × 彩色/无色 × 成功/取消，终端属性恢复 |
| 提问/补全/会话选择/Markdown 的真实终端脚本 | 退出码 0，4+2+4+2 场景通过；合并后共 24 个 PTY 场景 |
| `GOOS=linux GOARCH=amd64 go build ./...` 和 app `go test -c` | 退出码 0；仅交叉构建，不声明 Linux 原生运行验收 |
| `openspec validate --all --strict` | 退出码 0；54 个主规格和 1 个活动 change，共 55 项通过 |
| 冲突标记、差异空白、本地文档链接与两个归档增量一致性 | 通过；96 个链接有效，19+21 条需求与主规格一致，无冲突标记 |

实际运行验收平台为 Darwin arm64 / Go 1.24.13；Provider/PTY 只使用本地 mock 与合成凭据。完整合并门日志为 `/tmp/easycode-post-hook-merge-final-verify.log`，其余命令与固定断言均可在仓库复验。初次功能验收和归档核对分别见 [verification.md](verification.md)、[archive.md](archive.md)，合并前首次推送回归见 [revalidation.md](revalidation.md)。

最终质量门期间 1098 个源码、脚本及固定输入文件保持同一内容快照；之后只补齐文档与 Git/PR 状态，不改动测试断言或实现。
