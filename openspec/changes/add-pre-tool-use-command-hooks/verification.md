# PreToolUse 命令 Hook 实施验收

日期：2026-11-05—2026-11-06。环境：Darwin arm64，Go 1.24.13。变更：`add-pre-tool-use-command-hooks`。

## 交付范围与选择依据

本切片补齐已经存在的 Skills、Tool approval/ledger 与 Session 恢复之间的执行前检查边界。相比尚需 manifest、安装和多来源贡献生命周期的 Plugins，PreToolUse 可以直接复用现有独占调度、批准与原生 outputs，交付一个完整且可验证的闭环。参考工程的固定 revision、代码入口、行为比较和取舍保留在 `design.md` 的参考证据表中。

交付 user/project 严格冻结配置、三项 mutation 字面 matcher、逐声明默认拒绝的本地会话信任、Darwin 只读/禁网命令、完整 `/hooks` 浏览/reset/滚动审查、四类 durable facts、离线恢复与双 Provider 结果配对。信任不授予普通工具权限；恢复不重新运行 Hook 或工具。其余事件、async、allow/ask、rewrite/context/result、Plugins、MCP 和热加载没有被声明为已实现。

## 契约与验证对应

| 边界 | 实施及直接验证 |
| --- | --- |
| 配置/来源 | `extension/hook/config_test.go`、`extension/localfile/read_test.go`；严格 JSON、null/重复/未知字段、字面别名、来源与匹配上限、内容 revision、FD/no-follow/权限/变化捕获；原 Skills 安全与 cache tests 保持通过 |
| 强类型/输出 | `domain/hooks_test.go`、`extension/hook/output_test.go`；三种规范输入、512 KiB 上限、真实 facade、值拷贝、状态/exit/cleanup 矛盾拒绝、strict deny 和输出限制 |
| 共享进程 | `executor/process/owner_test.go` 与既有 builtin Exec tests；纯构造、单次接收、独立双流洪泛/UTF-8、stdin 不读/提前关闭、假时钟 timeout/TERM/KILL、阻塞 Wait、detached writer、唯一 Done 与 Wait 后零信号 |
| 实际沙箱 | `tool/builtin/hook_process_darwin_test.go`；真实 JSON stdin、最小环境、工作区只读、受控本机网络拒绝、外部私有文件拒绝、exit 与输出上限；原 Darwin 注册/放行故障和 Exec artifact/预算 tests 同门验证 |
| Session | `session/hook_replay_test.go`、`hook_fixture_test.go`；plan/ordinal/digest/identity、当前严格词汇、前序屏障、原子 finished/gate/result、非法拆批/重复/跳项、语义损坏优先于半行 repair |
| 固定 bytes | `session/testdata/current/hook_*.jsonl` 与 `hook_replay_plan.json`；覆盖 plan、started、成功前缀、continue、deny、error、headless、recovery 不确定、普通 allow/tool started/result、outputs，逐行 canonical bytes/checksum 回归；测试不生成输入 fixture |
| 信任/故障 | `protocol/hooks_test.go`、`runtime/hooks_test.go`、`hook_fault_test.go`；默认拒绝、精确身份、取消/重复/跨来源响应、reset、headless/unsupported、现有 grant 不能绕过、30 秒总预算、逐 fact Sync 前后故障与取消、runner 错配 poison |
| Owner/lease | `runtime/hook_shutdown_test.go`、`app/hook_host_owner_test.go`、`session/hook_fixture_test.go`；实际子进程 lease 竞争、持有期禁止 repair、崩溃释放、单调 seq、失败 bytes 不变、阻塞清理期间拒绝新 turn、超时后保留 owner 至最终 Done |
| 恢复 | `runtime/hook_recovery_test.go`、`app/hook_resume_test.go`；每批崩溃窗口、补偿后再次崩溃、重复恢复不新增事实、旧 bytes 前缀不变、删除/损坏当前配置、loader spy 确认先补偿后 Load、恢复零 runner/trust/tool/Provider 执行 |
| 双 Provider | `app/hook_e2e_darwin_test.go`；OpenAI/Anthropic × Patch/Write/Exec × continue 后允许/拒绝、exit 2、structured deny、timeout；真实进程与普通审批、九能力目录、原 call/output 配对、独占 Read/Write/Read 与独立 sibling |
| 原生/缓存 | 两家 `provider/*/exec_restore_test.go` 与 `hook_denied_continuation.golden.json` 保留 encrypted reasoning/signature；`app/hook_cache_test.go`、`hook_resume_test.go` 比较 canonical request、footprint 与 request fingerprint；`hook_websocket_test.go` 分别核验 OpenAI incremental 后缀与 restored full，Anthropic TTL/marker 独立验证 |
| 无历史的配置/信任 | `runtime/hooks_test.go` 比较 trust 前后完整 cache sources；app cache 测试在相同历史下修改配置并恢复，稳定 tool/prompt 段不变；既有 Skill/Exec cache regression 纳入完整门 |
| 隐私 | 实际 Hook 向环境候选、stdout/stderr/structured reason 注入合成哨兵；journal、冻结结果和模型请求无哨兵，详情只有安全统计。Hook runner 没有 artifact 保存接口；其输入不读取 Provider native/transcript/凭据 |
| TUI | `tui/hooks_test.go` 与八份固定尺寸 golden；浏览零提交、草稿返回、正常/窄屏/长命令、默认 Enter/Esc 拒绝、按键隔离、两类 overlay 互斥、reset 不授权、checking 不冒充 running、过期事件与提交后 Ctrl+C |
| Headless | `TestHookHeadlessModesFailClosedForBothProviders` 对 text/json/stream-json 验证 `hook_trust_required`、无候选/脚本/目标工具副作用、stdout 不泄漏内部请求；无 Hook 的原 `approval_required` 用例继续通过 |
| 架构 | 移除旧 Hook placeholder 与 exact allowlist；共享文件/进程包禁止反向业务依赖；新增构造副作用/import 负例，沿用全仓库 any/全局可变/单当前格式守卫；没有新增 Go 依赖或兼容 reader |

## 实际执行的命令

- `go test ./internal/domain ./internal/extension/localfile ./internal/extension/skill ./internal/extension/hook` 及相关 Skill/cache regression：通过。
- `go test ./internal/executor/process ./internal/tool/builtin`：通过；真实 Darwin 进程和既有 Exec 生命周期/预算/artifact 用例通过。
- Hook 专项 tests（protocol/runtime/session/tui/app）与两家 `TestExecNativeResultsRestoreExactly`：通过。
- `go test -c ./internal/app -o /tmp/easycode-hook-app.test`；`python3 scripts/smoke-hooks.py /tmp/easycode-hook-app.test`：两家 Provider 均通过完整实际 PTY 链路。
- `GOOS=linux GOARCH=amd64 go test -c ./internal/tool/builtin -o /tmp/easycode-hook-linux.test`：编译通过。
- `GOOS=windows GOARCH=amd64 go test -c ./internal/tool/builtin -o /tmp/easycode-hook-windows.test.exe`：编译通过。
- `openspec validate --specs`：48 项主规格通过，0 失败；仅有既有长 requirement 的信息提示。
- `openspec validate add-pre-tool-use-command-hooks --strict`：通过。
- `git diff --check`：通过。
- `make verify`：最终文件状态复验通过（exit 0），包含分支策略测试、gofmt、go vet、Staticcheck、架构边界、全量测试和全量 race test；此前两轮完整质量门亦通过。

实际 PTY 的两个 Provider 均观察到：启动 → 首次默认拒绝 → 配对最终回复 → `/hooks` 列表/完整详情 → 空闲 reset → 草稿返回 → 再次审查 → 显式信任 → 普通 Write 批准 → 执行完成 → 下一调用 checking → Ctrl+C/清理完成 → 正常终端退出。仅使用本地 httptest 服务和合成 key，没有公网模型请求。

## 平台与生命周期限制

Linux/Windows 本次只完成失败关闭 adapter 的交叉编译，未在这些系统实际运行测试或声称命令执行支持；真实沙箱、PTY 和进程清理验收平台是 Darwin。原进程组与直接 Wait/owned I/O 的收口保证不等于任意脱离子孙容器。配置只在启动时捕获，信任不冻结外部脚本依赖；这些是批准设计中的明确边界。

七份主规格已同步，README、总体架构第 12 章、P6 Roadmap 和 pitfall 已更新。本 change 保持 apply 后可审阅状态，未执行独立 archive 操作。


## 合入 main 后的集成复验（2026-11-06）

从 origin 获取并快进合入 `b579a38`（结构化用户提问），当前分支为 `feat/add-pre-tool-use-command-hooks`。原先 detached HEAD 下的 Hook 工作区通过临时 stash 完整保存后恢复，12 个文本冲突按两项能力并存解决；85 个原未跟踪文件均已核对恢复；恢复时保留全部待提交改动，未丢弃原实现。

- 协议命令、事件、AgentLoop owner、Session replay 和历史详情同时保留 Question 与 Hook 分支；问卷仍是 exclusive 且无需普通权限批准。
- 保留 main 的九项工具目录，Hook 的两家 continuation golden 和目录断言跟随当前目录；Hook 不增加模型工具。
- 问卷、Hook 信任与普通审批三类 overlay 互斥，草稿和当前请求身份保持隔离。新增 `TestHookAndQuestionOverlaysRemainMutuallyExclusive`。
- 新增 `TestQuestionHookQuestionKeepsIndependentAuthorizationAndPairing`，两家 Provider 均验证 Question / Hook 拒绝 Write / Question 原序执行，只有 mutation 具有 Hook plan，没有伪造 approval，拒绝不取消独立提问。
- 冲突涉及的主 specs 与当前 delta requirements 合并保留 Question 和 Hook 的完整条款、场景；`openspec validate --specs` 为 50 项通过、0 失败，当前 change 的 strict 校验通过。
- `python3 scripts/smoke-hooks.py /tmp/easycode-hook-main-merge-app.test`：两家 Provider 的完整真实 PTY 链路通过。
- `python3 scripts/user-question-smoke.py`：selection、resize_other、stop、picker_cancel 四项通过，均确认终端模式恢复；picker 取消按契约退出 130。
- 冲突标记、unmerged index 与 `git diff --check` 已检查；分支命名校验通过。`make verify` 合并后完整复验通过（exit 0），含 gofmt、go vet、Staticcheck、架构边界、全量测试和全量 race test。


## 全库文档核对（2026-11-06）

按真实 CLI 路由、九项 Tool Catalog、Context/CacheSources、配置装配、Session 存储和测试核对 README、功能大纲、总体架构、Tool 专题、ADR 与文档索引，新增 `docs/roadmap/capability-status.md` 统一当前状态及证据。主要修复缓存/选择器仍被列为未实现、Tool 占位、旧工具数量与分段编号、未交付 Hook/流式 diff/流内提前执行被混入运行流程、错误存储路径与内部格式版本等描述。

ADR 保留原始决策并补当前对照；归档 change 只修复一个失效相对链接，不把历史设计改成当前实现。主规格与本 change 对应 delta 中已过时的“七工具全量目录”表述改为已实现工具，固定七工具测试样本仍保留；没有增加新行为或兼容承诺。

- 扫描 404 份 Markdown、151 个本地文件链接、5 个本地标题锚点：无失效引用；README/docs 中显式列出的当前源码路径均存在。
- `openspec validate --specs`：50 项通过，0 失败；`openspec validate add-pre-tool-use-command-hooks --strict`：通过。
- 文档核对后的 `make verify`：exit 0；提交前同时缩短 Hook 会话信任选项，避免窄屏标签跨行和 golden 行尾空格，三组信任 snapshot 与 Hook 专项测试通过；pre-commit 再执行同一完整质量门。
- `git diff --check`、无冲突 index、分支语义规则与全部原未跟踪文件恢复核对通过。Linux/Windows 运行验证限制仍按上文披露。


## PR Linux 质量门修复（2026-11-06）

PR 首轮 Linux CI 的 Staticcheck 发现仅供 Darwin 调用的 launch policy 与 E2E 辅助函数放在通用文件中，产生 U1000。将启动策略移至 `sandbox_launch_darwin.go`，双 Provider 场景辅助函数移至 `hook_fixture_darwin_test.go`；共用 SandboxPlan/Planner 与跨平台配置辅助函数仍保留通用文件，没有删除测试或忽略检查，执行/信任/恢复契约不变。使用宿主平台编译的 Staticcheck 二进制按 `GOOS=linux GOARCH=amd64` 检查全仓库，已通过；Darwin 再由 pre-commit 执行完整 `make verify`，Linux CI 继续复验。

补充检查 `GOOS=windows GOARCH=amd64` 全仓库 Staticcheck 未通过，包含既有的 picker 测试辅助函数缺失及不支持平台上的 U1000。对 `origin/main` 只读源码快照执行同一检查，两份输出均为 56 行，归一化位置行号后完全一致，没有本变更新增诊断；未修改无关主干问题，也不声称 Windows 全库质量门通过。既有 builtin 适配器交叉编译结论仍有效。这些检查不扩大 Linux/Windows 的命令执行能力。

## 后续能力交付后的规格漂移修复（2026-11-13）

本变更仍为活动状态，原 43 项实施任务保持完成；用户此次只要求修复旧 delta，随后归档 Stop，不归档本变更。

- 按 CLI 返回的七份现有 delta 路径逐项对照主规格，修复 11 项落后需求，补回后续已交付的 30 个场景；保留原全部需求、场景及 ADDED/MODIFIED 归属。所有 16 项需求块现与相应主规格一致，重复对齐无差异。
- 修正共享架构规则中仅 PreToolUse 的过时措辞，明确四类已交付 command Hook 仍需完整消费者/严格协议/测试；未实现事件与行为继续禁止暴露。共同配置主规格的 Purpose 同步四事件范围，不新增执行能力。
- Proposal/design/tasks 明确区分原始 Pre 实施归属和当前需求基线，不将后续 Post/Failure/Stop 归为本变更新增，不改变业务代码、依赖、格式版本或恢复消费者。
- 修复前 `openspec validate --all --strict --json` 为 58/59；修复后为 59/59，两个活动变更均零 issue，原始结果 `/tmp/easycode-pre-drift-before.json` 与 `/tmp/easycode-pre-drift-after.json`。原 ADDED 已存在的归档冲突提示也已消除。
- Stop 的 12 份 delta、42 项需求及 295 个场景均已与主规格同步，48 项任务完整；后续按用户要求独立归档，不移动或删除本变更。

完成后再次与 HEAD 对比：七份 delta 的 16 项需求名称和顺序均保留，原 158 个场景完整保留，当前共 188 个，补回 30 个、删除 0 个。Stop 已通过正常 CLI 归档至 `openspec/changes/archive/2026-11-13-add-stop-command-hooks`，主规格内容在归档操作前后不变；本变更仍活动。归档后全仓库 strict 为 58/58，通过；普通 `make verify` 完整退出 0（`/tmp/easycode-stop-archive-make-verify.log`），本轮没有改动业务代码或依赖。
