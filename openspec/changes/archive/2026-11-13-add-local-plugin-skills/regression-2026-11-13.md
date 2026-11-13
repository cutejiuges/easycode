# 本地 Plugin 独立全面回归（2026-11-13）

平台：macOS 27.0.1 / Darwin arm64，Go 1.24.13；分支：`feat/local-plugin-skills`。本轮针对完成实现后的当前工作区独立执行，模型交互均使用本地 HTTP/SSE/WebSocket 模拟服务和固定测试 key。

## 本轮补强

- 新增 `internal/extension/plugin/manifest_fuzz_test.go`：向合法导入声明追加空对象、空数组或 null 的 Hook/MCP/Agent/commands 等贡献，必须拒绝并保留无歧义 ID 的冲突资格。
- 新增 `internal/extension/skill/sidecar_fuzz_test.go`：合法副文件追加依赖必须拒绝，追加显式禁止时不得无声忽略；覆盖 YAML 文档边界、注释和块标量。
- 真实 PTY 原先采用 OpenAI，现将 `plugins_terminal_test.go` 和 `scripts/plugins-smoke.py` 扩展为双 Provider × 双色彩模式，共用同一文本响应帧生成逻辑。终端确认包含真实 Provider wire 和正式 turn_completed 事件。
- 实际 CLI 二进制从一次显式输入扩展为三次采样的 Skill 主文档/资源工具续写；删除临时插件目录后，再分别跨进程 continue/resume。只在本轮测试代码与验收脚本中补强，未修改产品行为或协议，固定 current fixtures 和主规格不变。

## 已执行的能力与端到端验证

| 层次 | 实际结果 | 原始证据 |
| --- | --- | --- |
| 乱序能力矩阵 | seed=20261113；9 个包，107 个叶级测试执行；0 failure / 0 skip | [ability.jsonl](evidence/regression-2026-11-13-ability.jsonl) |
| 生命周期与安全 race | 8 项顶层测试各 10 轮，210 个叶级执行；0 failure / 0 skip | [focused-race.jsonl](evidence/regression-2026-11-13-focused-race.jsonl) |
| Manifest fuzz | 两 worker、60 秒，2,071,937 次执行，PASS | [manifest-fuzz.log](evidence/regression-2026-11-13-manifest-fuzz.log) |
| 调用策略副文件 fuzz | 两 worker、60 秒，690,003 次执行，PASS | [sidecar-fuzz.log](evidence/regression-2026-11-13-sidecar-fuzz.log) |
| 实际二进制 CLI | 四种固定包 × 双 Provider，共 8 组、40 次请求；16 次删除来源后的跨进程继续/恢复，全部通过 | [plugins-smoke.json](evidence/regression-2026-11-13-plugins-smoke.json) |
| 真实终端 | OpenAI/Anthropic × 普通颜色/NO_COLOR 共 4 组；浏览、详情、草稿恢复、保留名、paste、正常提交、活动停止、80×24/28×10/8×2 和终端模式恢复通过 | 同上 |
| 汇总计数 | 逐条解析 JSONL 的叶级结果，保留父项统计但不重复计入叶级数 | [results.json](evidence/regression-2026-11-13-results.json) |

Fuzz 次数为 Go 引擎的原始执行次数，包含非法输入的快速拒绝；业务断言另由固定种子、定向回归和端到端矩阵验证。模糊测试没有发现 panic、放行未支持贡献或忽略追加调用限制的失败输入。

CLI 逐组验证：显式正文进入首个请求；模型主文档加载或 Codex 预期的 skill_model_invocation_disabled；同来源辅助资源 Check correctness 进入后续请求；真实 stdout 仅有最终文本；无效来源隔离；删除临时插件后两种恢复仍包含原指令与资源；每组 5 次请求均不包含插件绝对路径。原始仓库内固定插件包只读，删除只发生在本轮自建临时副本。

race 反例通过 channel/故障检查点精确建立：16 借用门、重复关闭、文件关闭被阻塞、admission 前准备尚未返回、来源逐层替换、失败读取累计预算、伪造 journal 不修复、queued 来源删除和一次安装。重复/乱序进一步检查共享状态，正确性断言不依赖 sleep。

## 最终回归与契约检查

先执行 go clean -testcache 后启动首次完整 make verify；普通全量通过，race app 套件在 600.507 秒触发默认十分钟总超时。当时活跃项为既有 TestWriteSSEChunksCancellationEOFAndLimitsPrecedeSideEffects/anthropic/aggregate-limit（该子项运行 8 秒），日志没有 data race 或业务断言失败。原始日志保留于 [首次质量门](evidence/regression-2026-11-13-make-verify-before-dual-pty.log)。测试期间观察到另一工作区也在执行全量质量门；这可能影响运行时间，不据此断言根因。没有调整超时或删减测试。该 Write 流组随后单独以 race、count=1 复测，16 个子场景全部通过，耗时 50.048 秒，见 [Write 流复测](evidence/regression-2026-11-13-write-stream-race.jsonl)。

最终 **make verify 已通过（exit 0）**，见 [最终质量门日志](evidence/regression-2026-11-13-make-verify.log)。执行范围完整保留：分支策略、gofmt、go vet、Staticcheck、架构检查、全量测试和全量 race，覆盖已有 Skills/Hooks/Question、两家 request/stream/native golden、缓存、Session lease/崩溃/修复及权限回归。当前测试 helper 和新增 fuzz seeds 均纳入最终质量门。

本 change 严格校验通过；58 项主 specs 严格校验通过，见 [change 校验](evidence/regression-2026-11-13-change-validation.json)和 [主规格校验](evidence/regression-2026-11-13-specs-validation.json)。分支名和 git diff --check 通过；新增注释/文档使用中文、无新增生产依赖、无协议或 record vocabulary 变化。产品源码与固定 fixture/主规格的指纹保持不变，新增能力验证没有弱化已有断言。

Linux amd64 的全库生产构建与 extension/artifact/app/CLI 测试编译通过，见 [交叉编译日志](evidence/regression-2026-11-13-linux-crosscompile.log)；没有原生运行。

最终源码、fixture 和主规格指纹记录于 [source.json](evidence/regression-2026-11-13-source.json)，已在交付前逐项核对；当前产品源码未改，测试增强不引入新工具、RuntimeEvent、Session record、兼容 reader 或依赖。

复现命令：

```sh
go test ./internal/extension/plugin -run '^$' -fuzz '^FuzzPluginManifestCannotEnableBehavior$' -fuzztime=60s -parallel=2
go test ./internal/extension/skill -run '^$' -fuzz '^FuzzPluginSidecarCannotRelaxRestrictions$' -fuzztime=60s -parallel=2
go test -count=1 -shuffle=20261113 ./internal/app ./cmd/easycode ./internal/provider/anthropic ./internal/provider/openai ./internal/context ./internal/session ./internal/tui ./internal/domain ./internal/tool -run 'Plugin|Sidecar|SkillReference'
python3 scripts/plugins-smoke.py
make verify
openspec validate --specs --strict --json
```

Linux 只做构建和测试编译，不将其作为原生执行验收；真实模型服务和 Windows 原生运行未执行。本轮没有发现产品功能缺陷，补强了测试覆盖并保留首次超时证据。本轮验证时尚未归档；后续归档核对见[归档记录](archive.md)。
