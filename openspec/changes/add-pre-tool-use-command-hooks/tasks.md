## 1. 强类型契约与有界来源

- [x] 1.1 在 domain 定义 Hook 来源/声明/计划、三分支输入、封闭 verdict 与运行结果值；验证构造器、深拷贝、非法组合及输入 512 KiB 边界单测通过，核心导出 API 无 any/map 动态泄漏。
- [x] 1.2 抽取 `internal/extension/localfile` 的 no-follow/FD 校验骨架并接回 Skills；以既有 `skill/secure_unix_test.go`、Skill parser/catalog 与 cache tests 验证来源、TOCTOU 和 request/fingerprint 行为不变。
- [x] 1.3 实现 Hook 两来源显式 Load、FD 权限/大小/一致性捕获和安全诊断；在 `hook/config_test.go` 覆盖缺失、特殊文件、symlink、权限、读取中变化及一个坏来源阻断全部 mutation，普通聊天/只读仍可用。
- [x] 1.4 实现严格配置 decoder、字面 matcher 别名、顺序、64 KiB/32 条来源/8 条匹配上限与内容 revision；用重复键/null/未知字段/事件、alias 去重、枚举与对象键变化、数组次序、绝对路径迁移及 getter 变异用例验证。
- [x] 1.5 实现纯内存 Hook stdin 编译与 stdout 子集解析；`hook/output_test.go` 直接覆盖三种 typed tool_input、实际 facade、exit0/2/其他、空对象、structured deny、所有不支持字段与输出上限，不截断请求或 JSON 后放行。

## 2. 共享进程生命周期与平台 adapter

- [x] 2.1 从 `builtin/process_execution.go` 抽取不依赖 tool/provider/session/runtime 的 `internal/executor/process` owner 和窄 launcher/handle/clock；用假进程测试验证 New 无副作用、Run 单次接收、唯一 Done、Wait 前原组身份与 Wait 后零信号操作。
- [x] 2.2 为共享 owner 加入有界 stdin writer 和独立双流排空；用控制 channel 验证不读/提前关闭 stdin、双流洪泛、半 UTF-8、管道不关闭以及取消时所有 reader/writer 最终退出，无 sleep 或逐次 WaitGroup helper。
- [x] 2.3 将既有 Exec adapter 接到共享 owner，保留 candidate、approval、progress、renderer 和 artifact 策略；运行原进程/输出/预算/保存/脱离进程测试及 golden，证明非零退出和所有现有 fingerprint 不变。
- [x] 2.4 将 Darwin 受控启动骨架适配为中性冻结计划，保留目录 FD/helper/kqueue 注册后放行与失败清理；通过既有 `exec_darwin_test.go` 和确定性注册/放行故障验证无未受限命令执行，其他平台仍拒绝。
- [x] 2.5 实现 Hook 只读/禁网 profile、最小环境、固定 workspace cwd 与 16 KiB/流策略；`hook_process_darwin_test.go` 真实进程验证 JSON stdin、工作区读取成功、写入/本机受控网络/外部 home/Session 访问拒绝及无 unsandboxed fallback。
- [x] 2.6 验证单项 timeout、30 秒总预算、TERM/KILL、原组收口、独立输出排空与 detached writer；假时钟/受控子进程测试证明 cleanup 不确定与 shutdown 超时保留 owner，结果不能冒充任意子孙进程全部退出。

## 3. Session 词汇、校验与恢复计划

- [x] 3.1 增加四类 Hook Session facts 的 typed constructor/strict decoder/validator 与唯一当前 codec；用单测验证必需字段、受控 origin、code/status、identity、ordinal、plan digest、exit/cleanup 组合及所有 raw output/路径/版本字段拒绝。
- [x] 3.2 扩展 ReplayPlanner 与 ToolRecoveryPlan 的封闭 Hook 子状态；`hook_replay_test.go` 覆盖单 plan/decision、顺序与前缀、全成功条件、普通 approval/started 前置、call index/exclusive 屏障，错误发生前不进行 repair。
- [x] 3.3 实现最后成功 finished/gate、阻断 finished/gate/result、信任拒绝 gate/result 和恢复不确定三事实的原子 batch 约束；正反例证明缺失/拆批/重复/错序为 corruption，合法 started 尾部仍可显式补偿。
- [x] 3.4 提交固定 current JSONL/replay 预期 bytes，覆盖 design 恢复矩阵每个窗口与正常 continue/deny/error/headless；测试必须读取仓库 fixture，不在运行时用当前 encoder 生成输入，无历史目录或兼容 reader。
- [x] 3.5 补语义损坏叠加 torn tail、编码/行大小、Sync 故障与 journal bytes 不变回归；生产 loader/replay 路径直接断言错误优先于 repair/append/副作用。

## 4. 信任命令与唯一授权 owner

- [x] 4.1 在 protocol 实现 `resolve_hook_trust`、`reset_hook_trust` 和 requested/resolved events 的 typed 构造/strict 校验；测试默认拒绝、真实调用身份、缺失/未知字段、过期/重复响应与 reset 目标身份。
- [x] 4.2 在 Runtime/AgentLoop/ChatSession 接入单项内存 trust/deny 与取消等待；单测证明工作区/Session/来源 revision/命令指纹互不授权，取消后的响应不安装 grant，不从普通 approval/Skill 或 journal 恢复信任。
- [x] 4.3 实现空闲 reset 的 owner 路由和相关确认；测试活动 turn/reset 身份错配拒绝、成功 reset 仅清除状态、下一真实调用重新审查，零伪造 invocation、零自动重试或配置写入。
- [x] 4.4 实现合法匹配项的 headless hook_trust_required 前置拒绝及交互平台前检；覆盖三个 headless 入口、unsupported 交互路径与无匹配时既有 approval_required，断言零候选/Hook/工具 I/O 和不等待 stdin。

## 5. Exclusive 门禁与故障收口

- [x] 5.1 在现有 `executeApproved` 的预算/取消前检后、candidate 前插入唯一门禁；`runtime/hooks_test.go` 验证 ready Sync/finalizer、前序读批次、三能力匹配与只读不触发、无匹配旧路径不新增 facts。
- [x] 5.2 实现 plan 冻结/Sync、串行 ordinal、逐项 started/finished 与短路决定；用执行计数和 journal 顺序断言别名一次运行、后序项不越过阻断、Hook deny 高于已有 Patch/Write grant、全继续后仍需普通 Exec 审批。
- [x] 5.3 为 Hook 阻断/error/cancelled 生成原调用身份的 capability typed result，安全摘要不超过 1 KiB 并复用既有预算/原子提交；验证普通错误不取消独立 sibling，全部结果和 native outputs 仍按 call index 配对。
- [x] 5.4 注入每类 Hook fact/结果 Sync 的接收前取消、接收后取消、确定失败和不确定失败；`hook_fault_test.go` 验证仅成功 Sync 后 runner 恰好一次、已取消 context 仍交付、后序副作用停止、无重复执行或伪造 durable success。
- [x] 5.5 注入 runner identity/结果错配与 cleanup 不确定；验证 Hook outcome_uncertain、目标工具 cancelled/hook_outcome_uncertain、健康 journal 有序收口与 Runtime poisoned，不把工具伪标 started 或已执行。
- [x] 5.6 扩展 shutdown owner 测试，控制阻塞进程/输入/输出清理并取消；验证 timeout 后 lease/workspace/owner 保留且新 turn 拒绝，释放阻塞后最终 Done 恰好一次，不弱化既有 Exec shutdown 测试。

## 6. 离线恢复与宿主生命周期

- [x] 6.1 实现 Hook recovery 矩阵：计划未开始/成功前缀取消、started 无 finished 不确定、gate continue 后普通取消、现有 result/output 复用；`runtime/hook_recovery_test.go` 对每个窗口断言全部 runner/trust/候选/工具计数为零。
- [x] 6.2 调整 app 装配，使旧 turn reconciliation 先于当前 Hook Load/install、Session.Start 最后执行；`app/hook_resume_test.go` 通过 loader spy 和删除/损坏配置证明旧事实恢复零读配置、不重新信任，未来新调用才使用新快照。
- [x] 6.3 在补偿 batch、outputs 与 terminal 后再次注入崩溃并重复恢复；验证 finished/gate/result/outputs 不重复、原 bytes 前缀不变、旧 turn 只有一个失败 terminal，仍不自动调用 Provider。
- [x] 6.4 扩展真实子进程 lease 测试，覆盖 Hook 活动/阻塞清理/恢复期间竞争、崩溃释放、占用期间禁止 repair、seq 单调与失败 bytes 不变；不以同进程 mutex 或 sleep 证明跨进程所有权。

## 7. 双 Provider、缓存和隐私

- [x] 7.1 为两家 Provider 增加本地 fixture E2E，分别跑三工具 Hook continue 后普通批准/拒绝、exit2、structured deny、timeout、headless 拒绝；验证 function/custom/tool_result 原调用配对、当前九能力目录和无 Hook 虚构消息。
- [x] 7.2 在 OpenAI/Anthropic 的 request/native golden 中固定 Hook 安全结果，并覆盖 result 已 Sync、native outputs 未提交的恢复；验证 frozen preview bytes、原生 item 顺序与 opaque reasoning/signature/encrypted content 不变。
- [x] 7.3 增加 `app/hook_cache_test.go`，比较相同已补偿历史的 uninterrupted/restored canonical request、footprint、segment/request fingerprint；分别验证 Anthropic cache 配置与 OpenAI full/incremental 派生，不混称不同 wire 等价。
- [x] 7.4 验证只改 Hook 配置/trust 而没有历史新增时稳定 prompt/tool/skill 段不变，实际阻断只改变历史尾部；运行既有 Skill 与 Exec cache regression，证明共享代码未引入绝对路径/环境/时间污染。
- [x] 7.5 注入合成凭据、敏感 Header 与用户机密哨兵到子进程环境候选、stdout/stderr/structured reason；检查 journal、errors、logs、snapshot、模型请求和 artifact 仅含安全摘要，stdin 不含 Provider/Session/完整历史。

## 8. Claude 风格 TUI 与输入路由

- [x] 8.1 实现 /hooks 的冻结目录/事件/matcher/来源浏览及空/无效/不支持状态；`tui/hooks_test.go` 和固定大小 golden 验证零 I/O/Provider/Session 写入、稳定排序、Esc 草稿保留。
- [x] 8.2 实现完整命令与限制的滚动详情、空闲 reset、首次自动信任 overlay 和默认拒绝；snapshot 覆盖正常/窄屏/长命令，行为测试证明 reset 不授权、Enter/Esc 默认拒绝、普通工具 overlay 互斥和按键隔离。
- [x] 8.3 实现 Hook 检查中/阻断/失败与真正工具 Running 的不同投影；测试 durable 前不报工具运行，详情仅有安全 code/统计且输出未收录，Ctrl+C 等待实际清理并丢弃过期响应。
- [x] 8.4 用本地模拟 OpenAI/Anthropic 服务分别做 PTY/TUI smoke，记录审查、拒绝、重新审查、普通工具批准和取消链；headless smoke 验证 stdout 单 writer 和原外部 JSONL shape 不变，无真实 API key/公网请求。

## 9. 契约守卫、文档与验收

- [x] 9.1 在完整 Hook 消费者接入时删除旧占位事件/任意 payload API、退出 Hook exact allowlist，保持其他占位不变；新增共享包 import graph、any/生产全局/构造副作用负例并通过架构测试。
- [x] 9.2 更新 README 配置样例和支持/失败/反馈隐私矩阵、总体架构第 12 章的真实状态、Roadmap P6 切片与 pitfall 的信任/计划/恢复边界；逐项核对实际 vocabulary/测试路径，不把其余 Hook/Plugins/MCP 写成已实现。
- [x] 9.3 执行 `make verify` 与 `openspec validate add-pre-tool-use-command-hooks --strict`，保存实际命令、平台和结果到 change 的验收记录；未执行项逐项说明原因与风险，不用基线结果替代新能力验收。
- [x] 9.4 对照七份 delta specs 和 design 验证矩阵核验交付，确认 no-dead-code/无兼容 reader/无新增依赖/无 secret/无重复副作用；同步主 specs 后再按归档流程处理，提案阶段不得勾选实施任务或归档。
