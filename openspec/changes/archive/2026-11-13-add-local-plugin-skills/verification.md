# 本地 Plugin 技能包验收记录

日期：2026-11-12–13。实际运行平台：macOS 27.0.1 / Darwin arm64，Go 1.24.13。分支：`feat/local-plugin-skills`。本文只记录本变更新增实现的验收；design 中的实施前基线不是本功能通过的证据。

## 已完成的功能验证

| 层次 | 实际证据 | 结果 |
| --- | --- | --- |
| 身份、严格当前结构 | `internal/domain/plugins_test.go`、`internal/tool/plugin_skill_test.go`、`internal/session/plugin_skill_test.go` | 限定名 129 bytes、来源交叉校验、固定 current bytes、伪造拒绝、clone 隔离通过 |
| 导入与来源 | `internal/extension/plugin/manifest_test.go`、`source_owner_test.go`、`internal/extension/localfile/relative_unix_test.go` | 四种固定包、优先级/损坏阻断、合法 ID 保留冲突、无跟随 FD、16 借用、唯一关闭与超时保留所有权通过 |
| 统一 Skill | `internal/extension/skill/plugin_test.go`、`plugin_safety_test.go` | 格式命名差异、硬链接资源根、主文档资源别名拒绝、副文件策略/存在性、冻结与各级预算通过 |
| app 宿主与恢复 | `internal/app/plugins_test.go` | 四格式 × 双 Provider × once/interactive/stream 共 24 组；实际请求包含指令、模型结果/资源正文；旧 turn 各前缀先补偿，删除来源后离线恢复通过 |
| 输入冻结 | `internal/app/plugin_freeze_test.go`、`internal/runtime/skills_test.go`、`skill_install_test.go` | queued 后删除来源仍提交原附件；副文件增删改只影响未来目录；Sync 失败/poisoned 不产生显式资格，安装恰好一次通过 |
| 请求与缓存 | 两家 `plugin_restore_test.go`、`skill_test.go`、`internal/context/plugin_skills_test.go` | 固定原生 bytes 的不中断/恢复请求、opaque、顺序、footprint 与 fingerprint 等价；私有 revision 不污染模型内容通过 |
| 真实 WS | `internal/app/plugin_websocket_test.go` | 两次工具输出以匹配 call ID 的增量 suffix 提交，恢复新连接使用完整请求，保留指令、资源及 opaque reasoning，通过 |
| UI 路径等价 | `internal/app/completion_e2e_test.go::TestPluginBrowserManualAndRestoredRequestsBothProviders` | 补全、/plugins、/skills，各双 Provider × manual-only 开关 × ASCII/Unicode 参数，逐字节对照手输与恢复请求，通过 |
| CLI 运行入口 | `cmd/easycode/plugin_integration_test.go`、`plugin_dirs_test.go` | 四格式 × 双 Provider × print/json/stream 共 24 组，另含 16 次 continue/resume；无效来源隔离、无额外 stdout 协议、参数隐私与帮助通过；非法参数先于后续帮助/版本分支拒绝 |
| 实际二进制与 PTY | `scripts/plugins-smoke.py`、[独立回归证据](evidence/regression-2026-11-13-plugins-smoke.json) | 实际 CLI 二进制 8 组、40 次请求、16 次删除来源后的跨进程恢复；双 Provider × 普通颜色/NO_COLOR 四组真实 PTY，浏览/详情/选择/草稿/保留名/paste/活动停止及 80×24、28×10、8×2 与终端模式恢复通过 |
| TUI | `internal/tui/plugins_test.go`、新增固定 goldens 与 completion goldens | 六类状态、窄屏控制、恶意 metadata 可见转义、独占焦点、4 MiB 原子拒绝通过 |
| Linux 构建与测试编译 | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...`；相同目标平台 `go test -c` 编译 extension、artifact、app 与 CLI | 全库生产构建和上述测试二进制交叉编译通过，见 [linux-crosscompile.log](evidence/linux-crosscompile.log)；未在 Linux 原生执行，不据此宣称 Linux 完整运行验收 |

模型交互全部使用本地 HTTP/SSE/WebSocket fixture server 和不可用的固定测试 key；没有真实模型服务或外网访问。PTY 通过 ACK/窗口/事件屏障驱动，不以固定 sleep 证明正确。

可在仓库根目录复现主要验收：

```sh
make verify
python3 scripts/plugins-smoke.py
openspec validate --specs --strict --json
./scripts/check-branch-name.sh
git diff --check
```

`plugins-smoke.py` 使用 Python 标准库，自行在临时目录构建真实 CLI 和 PTY helper，生成本地配置并清理进程、终端及文件；当前要求 Darwin。没有真实 API key、默认外网连接或仓库外安装状态。

## R1–R9 二次设计审计的实施落点

| 项 | 反例与实际回归 |
| --- | --- |
| R1 | `TestImporterNamesLayoutsAndNoFallback`、`TestFixedPluginPackagesShareInvocationAndResourceRules`，分别验证 Claude 目录名、Codex frontmatter 名及 Agent Plugins DirectChildren |
| R2 | `TestSidecarStrictSubsetAndFrozenPresence`、24 组宿主测试中的 Codex 分支，验证更严格策略、拒绝模型主文档与已提交显式资源例外 |
| R3 | `TestChangedSidecarOnlyChangesFutureCatalogOnRestore`、两家固定原生恢复测试，验证存在性/内容与历史事实分离 |
| R4 | `TestPluginGlobalEnumerationAndCandidateLimits`、`TestPluginPerSourceRawBudgetAndCancelledPublication`、metadata/诊断/16 MiB 边界测试，验证原始条目也计费且整组纳入；`TestPluginStartupBudgetChargesRejectedMainAndSidecarReads` 及 localfile 拒绝读取测试证明一致性失败/读取后取消仍保留实际读取量且不披露正文 |
| R5 | `TestAllFixedFormatsAndConflictReservation`、`TestPluginConflictsHardlinksAndOverlappingRoots`，验证非法但明确的 ID、硬链接目录差别和最终计数 |
| R6 | `TestSourceCloseWaitsForPreadmissionPluginPreparation`、`TestCloseTimeoutKeepsReadStackAndBlockedFileCloseOwned`、`TestSourceBorrowLimitAndCloseGate`，验证真正完成后才释放依赖 |
| R7 | TUI 草稿/resize/保留名回归及真实 PTY，验证 /plug 取消恢复、$plugins 选择及窄屏返回/停止提示 |
| R8 | `TestPluginFlagsRejectPrivateInvalidValuesBeforeResources`、帮助副本测试和直接 app 参数测试，验证错误、默认值与帮助不回显参数 |
| R9 | queued 来源删除、固定伪造 journal 原 bytes 不变、旧前缀恢复零来源/Provider 调用以及关闭门测试，验证冻结 metadata 不成为新读取授权 |

## 契约自检

- `extension` 和 `plugin` 已由真实技能贡献替换并退出临时占位 allowlist；MCP、Subagent 与 telemetry 范围不变。`SourceHandle` 不向 Skill 暴露裸 FD；plugin 不依赖 Session，app 注入实际目录存储边界。
- Runtime 的 Config 不再保留旧 catalog/preparer 初始化入口；恢复后仅以 `InstallSkills` 安装未来快照。Provider/Session/Runtime 不解析插件 manifest。
- 没有新模型工具、RuntimeEvent、headless frame、SQLite 表、Session record kind、格式版本、历史 reader 或 activation/install record。
- manifest/main/sidecar 原始摘要、复合附件 revision 与模型内容指纹分开。实际私有路径、inode、版本注解和副文件界面注解不进入稳定前缀。
- 关闭状态锁只进行有界内存转换；回调、I/O、文件关闭及等待均在锁外，清理仅有一个显式启动点和固定完成信号。
- 参考工程只读，提交保持 Claude `a8a678cb6244e6770e1e421767ff0987a1d95549`、Codex `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`；未构建或依赖参考工程，未复制其大段实现。

## 正式规格同步

已按本地 OpenSpec 的 status 所列五项 delta 同步，写入前取得当前 specs 指令；新增 `extension/local-plugins`、`tui/plugins`，更新 `extension/local-skills`、`tui/input-completion`、`provider/native-history-persistence`。共新增 16 项、修改 10 项需求；新增 78 个场景，三份既有规格中的 110 个场景均保留，19 项未涉及需求的正文未改。新规格 Purpose 来自批准的 delta，既有 Purpose 保持原文；重复合并的内存校验结果相同，没有 delta 操作标题进入正式规格。

同步与保留检查见 [spec-sync.json](evidence/spec-sync.json)。`openspec validate --specs --strict --json` 实际结果为 **58 passed / 0 failed**，见 [specs-validation.json](evidence/specs-validation.json)；本 change 的严格校验通过，见 [change-validation.json](evidence/change-validation.json)。

## 最终质量门

第一轮全量测试仅发现两份新增合法 fixture 未列入既有预览测试的白名单，见 [make-verify-first.log](evidence/make-verify-first.log)。已补齐白名单，保留原有预览内容、只读 bytes、lease 与伪造 fixture 拒绝断言；对应回归通过。随后完整 `make verify` 通过，见 [make-verify-before-final-argument-check.log](evidence/make-verify-before-final-argument-check.log)。

补充非法插件参数不能被后续帮助/版本分支掩盖的前置校验后，全量质量门再次通过，见 [make-verify-before-rejected-read-accounting.log](evidence/make-verify-before-rejected-read-accounting.log)。最终预算审计进一步发现：一致性检查失败时原读取边界会丢弃正文，也一并丢失累计计费依据。现在受控读取值保留实际读取量，错误时仍不披露正文；主文档和副文件失败都计入 16 MiB，耗尽后不再读取。确定性修改/取消及主文档/副文件额度回归、extension 全套与架构测试均通过。

上述最终代码对应的完整 **`make verify` 已通过（exit 0）**，见 [make-verify.log](evidence/make-verify.log)。实际执行分支策略测试、gofmt 检查、`go vet ./...`、Staticcheck、`go test ./...` 和 `go test -race ./...`，包含架构边界、现有 Skills/Hooks/Question、真实子进程 lease/崩溃与 fixture 恢复回归。分支 `feat/local-plugin-skills` 检查及 `git diff --check` 通过；新增源码/文档空白、注释语言、文档和测试名称引用也已核对。没有跳过或弱化测试，没有新增依赖或改动参考工程。

实现收尾时以最终源码重建 CLI 与 PTY helper，运行 `python3 scripts/plugins-smoke.py`，**exit 0**；当时 8 组二进制 CLI 与普通颜色/NO_COLOR 两组真实终端全部通过，终端模式均恢复。[plugins-smoke.json](evidence/plugins-smoke.json) 保存该阶段结果，修正读取计费前的结果另存为 `plugins-smoke-before-read-accounting.json`。45/45 项任务均已有对应验证。

用户随后要求再次独立全面验收，本轮补充模糊测试、10 轮乱序 race、双 Provider 四组真实 PTY，以及二进制模型工具续写和删除来源后的跨进程恢复；最新结果见 [2026-11-13 独立全面回归](regression-2026-11-13.md)。

范围保持静态本地技能包：没有 marketplace、安装/更新、脚本/插件 Hook/MCP/Agent 贡献、热加载或启停。Linux 没有原生运行，Windows 等平台仍按安全 opener 能力失败关闭。变更已于 2026-11-13 归档；核对见[归档记录](archive.md)。
