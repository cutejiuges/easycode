## 1. 单一统计口径与可执行门禁

- [x] 1.1 实现全平台 Go 物理行数和测试脚本总量的只读统计、专属支持分类及 fixture 数量/字节报告；用基线快照复核 85,784/60,682，并证明新增未跟踪测试也被计入。
- [x] 1.2 建立单一规模策略来源与 `make test-ratio`，按用户 2026-11-25 的修订采用整数 `2P >= 3T`、一个脚本总预算和明确分类；扩充现有 Python 自检，以少量代表用例验证精确边界、支持文件移动不改善比例、全平台统计、预算超限且不写回，不新增测试框架。

## 2. 原生交互自动化先于覆盖迁移

- [x] 2.1 将现有关键 smoke 按独立交互风险整理为明确的代表计划，复用 app.test、tui.test、CLI 三个构建产物；为必要脚本补充复用入口，验证同一场景业务 ACK、退出、取消和终端还原均有真实完成证据。
- [x] 2.2 按用户最新授权，将统一 `make smoke` 纳入本地 pre-commit 并取消远端 smoke，按 design §5 保留关键链路并去无关排列；在 macOS 执行选定全集，记录实测耗时、构建次数和失败 cleanup，非支持平台不能报告成功。
- [x] 2.3 更新 CI/PR 模板的必要交付检查说明；核对本地 pre-commit 包含 verify 和 smoke，远端只要求 branch-governance 与 verify，并明确远端 ruleset 尚需实际核实，不以文件修改代替已启用证明。

## 3. 纠正无效测试与删除死亡装配

- [x] 3.1 修正两家 `post_output_test.go` 的有效来源对照，确认正常 records 成功且仅伪造 native output 时失败；不能让缺失 ThreadID 的前置错误满足断言。
- [x] 3.2 删除 Session 测试自写 `taskProgressBefore` 及其自测矩阵，以真实 record prefix 经过生产 replay 验证实际来源/替换/清空/失败回合；保留并运行 runtime 的 completed→pending→clear 回归。
- [x] 3.3 删除 app resume 的死亡参数、不可达 Markdown helper 与 completion 单值维度；核对跨平台消费者后删除失效支持，并运行原有实际恢复与补全关键入口。

## 4. Provider 共享机制与原生差异分离

- [x] 4.1 合并 checkpoint/replay、离线投影的共享不变量和装配；运行两家固定 checkpoint/current fixtures、连续压缩、来源拒绝及 uninterrupted/restored 请求与 fingerprint 对照。
- [x] 4.2 将输入 prepare/discard/finalize、历史快照和 Stop 的共用生命周期集中验证，家族包保留 native 接线；验证文件、Skill、Stop、SessionStart 的角色/顺序与 opaque 不泄漏，计算包含 adapter 的净行数。
- [x] 4.3 收敛 Hook rewrite/post/failure、Task/Question、artifact 的重复 evidence 编排；先补齐所属边界必要共享回归，再移除重复组合，完整保留实际 wire golden、冻结证据与严格参数 decoder 接线。
- [x] 4.4 将 stream 的 cache/trigger/chunk 笛卡尔积改为代表场景，共用真实 server 与消费编排；验证两家 wire 终态解释、UTF-8/分包、HTTP 取消及失败不提交，更新并验证关键 race 名称没有失效或遗漏。
- [x] 4.5 清理上述迁移后无消费者的 fixture/helper，优先 external provider_test，必要支持文件计入 T；检查正式 CLI 依赖图不含测试支持、未增加生产导出 API、全部仍消费的固定 golden 保持有效。

## 5. app 与 TUI 代表性链路收敛

- [x] 5.1 合并 resume/headless 的重复三轮成功恢复装配，移除已有 Read→Question→Write/推理链路承接的独立 Read-only 成功 E2E；运行两家恢复、preview 只读、usage/order、取消和崩溃回归，保留不同故障边界。
- [x] 5.2 收缩 skill/plugin 来源×宿主正常矩阵，复用请求记录与 journal 装配；以加载器、代表 CLI/交互和 Provider 原生恢复共同验证格式、Codex invocation policy、冻结后不重读和参数接线。
- [x] 5.3 在 2.2 通过后，将 task/title/completion/picker 的重复正常展示、滚动与编辑交互交由对应 PTY 承接；运行固定关键快照以及 hydration、过期结果、跨会话、审批焦点、取消和关闭回归。

## 6. 核心边界与测试支持精简

- [x] 6.1 合并 Session 的固定坏前缀与半行尾装配，保留每类输入及明确拒绝阶段；验证失败后 journal bytes 不变、禁止非法 repair、固定 fixture 不由当前 encoder 临时生成。
- [x] 6.2 合并 runtime artifact Sync 授权与语义一致的 journal 假实现，移除 Task 下游已由所属边界检出的纯值重复；验证 Sync 前不可见、失败 poisoned、错误来源、调用顺序和唯一副作用。
- [x] 6.3 将历史 Hook encoder 实现镜像收敛为能检出同一关键格式风险的固定 canonical bytes/digest 代表；保留三能力、转义、返回值隔离和必要坏输入，并明确记录不再保留的旧实现 fuzz 对照范围。

## 7. 达标核对与长期规则

- [x] 7.1 重算各组实际净减及剩余缺口，复核 app/provider/runtime 尚存支持代码消费者和跨层重复链路；交付简短组级删除/承接说明。若 `2P < 3T` 且仍需牺牲独立关键风险才能达标，暂停并更新具体取舍，不将本任务或本 change 标记完成。
- [x] 7.2 在最终快照满足 `2P >= 3T` 后，将唯一规模检查前置接入 `make verify` 并锁定脚本实际总预算；以可控超额快照验证快速非零退出、错误差额和清理，保留全部既有静态/普通/race 检查，确认无 warning-only、环境覆盖、自动重置或包级豁免；将 AGENTS §7 改为长期风险准入与 3:2 硬约束，并同步 docs/testing.md 的口径和成本。
- [x] 7.3 同步 Roadmap、pitfall 当前策略、受影响测试路径和两份主规格；保留历史验收事实，删除失效引用，确认没有把未执行平台或未完成能力写成已通过。

## 8. 删除重复 push hook

- [x] 8.1 删除 `.githooks/pre-push`，保留 pre-commit 的分支检查与完整质量门，更新 `make install-hooks` 的权限清单；运行安装并核对实际 hooksPath、剩余文件和现有分支策略回归，不修改用户 `.git/hooks`。
- [x] 8.2 核对规范和说明不再要求本地 pre-push；确认提交后 push 无重复仓库 hook，而 PR source-branch 检查仍能拒绝非法名称，说明远端保护的实际启用状态。

## 9. 最终验收与归档准备

- [x] 9.1 对最终稳定快照执行 `make verify`，确认比例/脚本预算、静态检查、全部现存普通入口和关键 race 均通过；记录与 77.132 秒基线相比的执行范围及耗时变化，仅在有变更/失败/未决问题时重复运行。
- [x] 9.2 执行最终关键原生 PTY 计划及受测试迁移影响的 Linux/Windows 静态或编译检查，核对 fixture/golden 消费者与跨平台支持分类；逐项列出无法原生执行的检查及风险。
- [x] 9.3 按用户最新授权，以真实本地 pre-commit 依次执行 verify 与全部原生 smoke 场景完成验收；核对 verify 失败不启动 smoke、smoke 失败阻止提交。取消远端 smoke 完成条件，保留分支及 Linux verify 的实际 CI 检查，如实列明远端保护状态。
- [x] 9.4 执行 OpenSpec 严格校验，核对 tasks、specs、AGENTS、文档、record vocabulary 和实际测试入口一致；仅保存简短验收结论，原始日志/JSON/profile 留在忽略输出或 CI artifact。首次归档按用户授权保留原 9.3 的 CI 待办；后续按用户确认的本地 hook 验收替代该要求，完成后同步规格并归档。

## 10. 2026-11-25 主线整合与新增测试

- [x] 10.1 安全保留既有精简改动，将最新 main 合入当前分支，核对并解决文本和语义冲突。
- [x] 10.2 审查 #55/#56 新增及修改的测试，合并重复装配与无关成功排列；保留 SSE、历史切点、输入门禁的独立关键风险，记录相对合入 main 的净成本。
- [x] 10.3 按记忆中的对应 Provider 凭据与授权端点执行正式 CLI/PTY 真实会话，核对多轮、恢复、拒绝/编辑和终端清理；原始输出留在忽略目录，凭据不写入产物。
- [x] 10.4 对合入 main 的快照执行完整 verify、统一 smoke、相关跨平台检查与 OpenSpec 校验；重算规模并完成 archive 工作流评估，首次评估时按用户选择保留 change；后续 3:2 实施与验证已按 tasks 7/9/11 执行，最终归档按 verification 中的明确授权处理。

## 11. 用户确认 3:2 后的第二轮收敛

- [x] 11.1 在 app 复用尚存语义一致的 Hook/PTY 装配，收缩由既有 CLI/PTY 承接的重复成功链路；保留 input owner、权限/取消、冻结恢复与 ACK，运行受影响入口并记录包含 helper 的实际净减。
- [x] 11.2 在 Provider 继续收敛镜像 setup 和普通成功断言；各家 wire/opaque/cache/恢复及唯一终态保持实质验证，测试支持全部计入 T，执行直接回归并记录净成本。
- [x] 11.3 在 runtime/session/domain/tool/tui/headless 收敛重复 fixture 与普通值验证，保留独立 Sync/lease/TOCTOU/资源 owner 边界和固定 bytes；运行受影响入口并记录净成本。
- [x] 11.4 以稳定快照确认 `2P >= 3T`，演算整数边界及业务缩减、测试/脚本增长的拒绝行为，确认硬门禁先于编译且只读；复用原有自检并在 tasks 7/9 完成全仓验证和规范同步。

## 归档说明（2026-11-25）

首次归档按用户明确授权保留原 9.3 未完成，当时为 35/36。用户随后要求取消远端 smoke，并将相同验收强制接入本地 pre-commit；本次完整本地 hook 与规范校验通过，相关任务 36/36 完成，归回原目录；历史远端 smoke 不宣称通过。
