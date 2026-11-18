2026-11-18 合并说明：64 项能力实施状态保留；测试按用户明确要求遵循 main 的“轻、准、关键、快”原则，使用代表业务链和关键故障回归，不继续维护下述早期规划中的重复穷举矩阵。实际保留范围及本轮验证见 `verification.md`。

## 1. 纯任务值与宿主协议

- [x] 1.1 在 domain 增加不可变规范任务步骤、三种 status 和完整 TaskUpdate，只校验公共规则并允许规范 active_form 为空，不依赖 facade profile/Provider DTO；以非法状态、纯内存构造和调用方修改副本的单测验证。
- [x] 1.2 实现完整 TaskUpdate 的 0–20 项、精确重复标签、至多一个 in_progress、每字段 512 UTF-8 bytes、说明 2 KiB、总文字 16 KiB 与规范快照 64 KiB 校验；以各边界相邻值、非法 UTF-8/外围空白/控制字符、合法 Unicode 原样保存及合法状态回退测试验证。
- [x] 1.3 实现带真实 result_sequence/turn_id/invocation_id 的任务来源及 none/clear/snapshot 封闭联合，none 禁止 source/update，其他分支完整必需且空/非空一致；以缺字段、null、错配来源、清除保留说明和全完成仍为 snapshot 的测试验证。
- [x] 1.4 提供唯一当前任务规范值的严格 decoder、canonical 编码与大小预检，不增加版本路由或可变 codec；以重复键、未知字段、尾随 JSON、转义膨胀、canonical 逐字稳定和深拷贝回归验证。
- [x] 1.5 在 protocol 定义任务只读查询、快照及 Sync 后进度通知的强类型构造/decoder/validator，校验线程、来源和派生计数；以固定 golden、非法三态/计数/身份及完整快照 96 KiB 实际编码边界验证。

## 2. 工具输入、结果与十能力目录

- [x] 2.1 在 tool.TaskInput 保存封闭 facade profile 与规范 domain.TaskUpdate，为 TodoWrite.todos 和 update_plan.plan/explanation 分别实现严格 decoder 和来源字段约束，OpenAI activeForm 归一化为空；让目录、原生输入核验和结果预检显式使用已核验 family/profile，不猜 JSON 字段。以两家合法/清空输入、重复键/null/缺字段、跨 profile、raw 64 KiB 和分片前缀测试验证；strict 参数错误使整 sample 失败且无 ready/result。
- [x] 2.2 将 task.progress 接入 ReadyCall、Invocation 及输入证据的封闭联合，补全 typed constructor、validator、clone 和错误结果路由；以能力/参数/wire/profile 错配、原 call identity 保留与所有十能力联合回归验证。
- [x] 2.3 定义成功完整 TaskUpdate metadata、失败禁止 applied state 以及固定英文计数/清空确认，确认最多 1 KiB 且不回显任务正文；以 ready/result 逐字一致、实际确认 bytes、失败联合拒绝及恢复确认稳定测试验证。
- [x] 2.4 增加纯内存 TaskExecutor 和对应 policy/执行装配，执行器只构造冻结候选，不持有当前列表；以零文件/进程/网络/终端访问、无 goroutine、无普通 approval/grant 和无 mutation Tool Hook 的测试验证。
- [x] 2.5 把两家任务 facade、强类型服务端 function schema 模式和真实消费者接入唯一当前 Catalog，维持五项 parallel_read 与五项 exclusive；仅 update_plan 为 best_effort，其他现有 function 保持 strict，custom/Anthropic 为不适用。以十能力 golden、模式参与 canonical/revision/fingerprint、改变模式产生可解释身份变化、缺消费者拒绝和动态任务不污染目录验证。
- [x] 2.6 接入现有 ResultCodec、结果预算与 ticket 生命周期，同步 Allowance、初始/冻结/恢复的 1 KiB cap，确认计入 sample 512 KiB/turn 2 MiB，完整 metadata 另做实际编码检查；以首次 Task 后 BodyExhausted=false 且后续 Read 仍有正文、确认不足/转义膨胀/真实 turn 耗尽、失败不应用及恢复不重分配/重复扣账验证。
- [x] 2.7 将任务硬限制、完整替换/清除/跨 turn、允许零个进行中、完成仅模型报告和成功后无需复述全文写进首个真实请求的 facade/schema description；以请求 golden 及 512/513 bytes 中文组合验证规则确实对模型可见，maxLength 不冒充 UTF-8 bytes，不引入 deferred ToolSearch 或简单任务强制清单。

## 3. 双 Provider 原生调用与输出

- [x] 3.1 在 Anthropic request 编译和 indexed stream reducer 接入 TodoWrite，保持 tools 稳定排序和 message_stop 前不产生 ready；以 request/stream golden、随机 chunk、半包、UTF-8 边界、取消/断线、重复 call ID 和非法参数零成功提交测试验证。
- [x] 3.2 在 OpenAI request/stream 接入 update_plan，编译器消费 facade 模式并用可表达 true/false/不适用的 DTO 显式输出 strict:false，保留可选非 null explanation；以 HTTP/WS/摘要 request golden、其他既有 facade wire bytes 不变、custom 仍省略 strict、原生身份/分片/取消和跨 profile 拒绝验证，本地 strict decoder 不放宽。
- [x] 3.3 扩展两家原生结果编码、待配对调用验证及 typed Task ready/result restore evidence，只使用已冻结英文确认生成匹配 outputs；以成功/预算错误/取消/uncertain 配对、metadata 与原生参数冲突及缺 outputs 离线重建测试验证。
- [x] 3.4 更新两家 native/current fixtures 与相关 golden，保持 thinking/signature/redacted/encrypted reasoning、message phase 及未知 opaque 内容原样；以相同事实不中断/恢复的完整 canonical request、item 顺序、revision、footprint 和 fingerprint 等价验证。
- [x] 3.5 使本地 CLI 假服务在返回 SSE 前验证真实请求目录的 function schema、required 与 strict 字段，明确拒绝可选 explanation 配 strict:true 及省略 false 的候选；用真实 CLI 验证已选 strict:false 请求可接受、可选说明可省略且 null 仍由本地拒绝，不能只回放固定成功流。

## 4. Session 当前结构、回放与恢复证据

- [x] 4.1 扩展现有 ready/result payload、draft、strict decoder 和 validator 以保存完整规范任务输入/metadata，保持唯一无版本 envelope 和既有大小上限；以 typed 构造、非法联合/未知字段/缺字段/preview 与预算错配测试验证，确认无新增 UI 或任务专用 record。
- [x] 4.2 在 ReplayPlanner 验证 Task ready→started→result 的无审批 exclusive grammar、sample 原子 facts、call index 和结果来源，只归约完整成功 result；以越过屏障、非法 approval/Hook、success 无 started、失败含 applied state 和重复 result/source 的拒绝测试验证。
- [x] 4.3 为 ToolHistoryEntry 增加实际结果的 ResultSequence，并让 live ToolHistoryObserver 从 Append 返回的结果 record 取得同一序号，禁止使用 ready/detail/native seq；据有序 ledger 构造无 facade profile 的 domain.TaskProgressState，以 live/replay 来源逐字一致、none/clear/全完成、回退/两次替换和失败 turn 保留事实的测试验证。
- [x] 4.4 将两家任务成功/清除/失败/预算拒绝、ready-only、started-only、result-tail 和混合 exclusive 场景保存为仓库固定 current bytes；通过生产 Loader/codec/replay 验证预期，确认测试运行时不以当前 encoder 生成 fixture 输入。
- [x] 4.5 在 app 的连续 lease 恢复装配中传递完整 Task 原生调用/结果证据，所有 replay/Provider 语义校验先于 repair、writer transfer 与补偿；以 checksum 合法语义损坏加半行尾部的 bytes 不变和零外部调用回归验证。
- [x] 4.6 将 orderedRestoreRecords、RestoreRecord 结果联合及两家 offline HistoryReader 的 Task/期望 checkpoint evidence 完整贯通，使 Session picker 离线预览复用联网恢复核验；以合法 checkpoint 可预览，以及 ready/detail/native seq、另一合法/未来结果、跨 profile 参数、wrapper 篡改后重算 checksum 均拒绝且零 repair/网络、bytes 不变验证。

## 5. Runtime 有序提交、取消与当前投影

- [x] 5.1 在 Runtime 实例中接纳经过验证的初始任务状态并提供只读不可变查询，锁内仅做有界内存转换、通知在锁外；以并发查询/race、构造零 I/O、getter 隔离及无新增 worker 的测试验证。
- [x] 5.2 将任务执行接入现有 exclusive 策略与 ordered scheduler，等待前序 result/预算关闭，成功或失败收口后才准入后序调用；以 Read→Task→Read、Task→Write→Task、只读乱序完成及 approval/Tool Hook 零调用测试验证。
- [x] 5.3 在任务 slot 执行完整确认/metadata/record/Provider output 预检并冻结结果，保持执行器候选无业务应用；以执行后 result Sync 被门控时查询仍为旧列表，以及预算/编码失败不保存部分成功的测试验证。
- [x] 5.4 为 Task success 使用可取消 writer admission 的窄策略：接收前取消复用原 budget ticket 保存唯一 cancelled result，清除 success metadata，不二次 reserve/close、不因正常取消 poison；接收后等待确定 resolution。以两侧门控验证零应用与 Sync 成功恰好一次应用，并验证真正写入/Sync 不确定才 poison，不用 sleep 证明竞态。
- [x] 5.5 仅在成功 result Sync 后安装真实 sequence 绑定的当前投影、提交预算并发布 typed 通知，不等待整组 native outputs 才确认任务事实；以通知前崩溃、重复消费、两次更新顺序及通知/投影失败后不重写已保存成功的测试验证。
- [x] 5.6 处理 started/result append 或 Sync 失败、durability 不确定、取消与输出失败，保留旧清单或已确定成功且阻止后序 admission；以确定性故障注入验证 poisoned、唯一 cleanup owner、最终完成信号及未完成清理不释放 lease。
- [x] 5.7 接入任务恢复补偿与跨 turn 生命周期，ready 未 started 补 before_execution、started 无 result 补 uncertain、成功结果缺 outputs 仅补原 bytes；以重复崩溃、16 samples/64 calls、steering、Question/Stop/取消不清单重置和全部完成不授予权限的回归验证。

## 6. 精确任务 checkpoint、计量与缓存

- [x] 6.1 在手动/自动压缩 admission 显式冻结当前 TaskProgressState 到 CompactionInput，仅供 checkpoint 构造和来源校验，普通 TurnInput 不携带动态任务附件；以压缩期间查询、普通请求无重复注入和状态来源冻结测试验证。
- [x] 6.2 扩展两家 checkpoint payload 的必需 task_progress，只保存 domain 规范快照与成功来源，不重复工具 facade profile，保留原 generation 并确定性包装任务资料；以 none 无附加正文、clear 防旧列表复活、snapshot 全字段保真和 provenance identity 不进入 prompt 的 golden 验证。
- [x] 6.3 在 Session/app 按各自 compaction_started 前缀计算期望来源，通过窄 typed evidence 让 Provider 验证 payload、来源、规范 update bytes 和原生 wrapper；以 A→checkpoint→B→checkpoint、来源后移/跨线程/失败 result/自洽伪造及修复前整体拒绝测试验证。
- [x] 6.4 保持语义摘要和 ProjectCheckpoint 仅投影真实模型 generation，使 native footprint 覆盖摘要及完整任务包装，Planner 沿既有 max 覆盖规则计一次；以固定来源顺序、无新增 task source、100/150 覆盖值及 archived 不重计量的回归验证。
- [x] 6.5 更新固定 checkpoint current bytes，覆盖 none/clear/部分完成/全完成、旧 shape 缺字段、source 伪造及包装错配；以手动/自动/重复压缩、candidate 超限/unknown/无缩减保留旧窗口、completion Sync 前后取消与 finalize 前崩溃测试验证。
- [x] 6.6 扩展两家缓存回归：Anthropic disabled/5m/1h 和 OpenAI omit/thread-key、HTTP/WS full/incremental、checkpoint 后重建及连接失败；比较逻辑完整 canonical request、opaque 顺序与 fingerprint，验证更新仅改历史、查询/折叠/resize 不改请求。
- [x] 6.7 从已验证 TaskProgressState 构造 source-free 内容 stamp：none 省略，clear/snapshot 对 state+domain.TaskUpdate canonical bytes 计算 SHA-256，排除 profile/来源/UI 身份并纳入既有 committed_history metadata；以等 revision/summary/token 的等字节 A/B 内容改变指纹、相同内容不同来源相同及 none/clear 区分验证，不新增估算/source/网络正文。
- [x] 6.8 把对应任务 stamp 绑定普通、候选窗口、手动/自动摘要和恢复规划，保留原 NativeHistoryFootprint 与 TurnInput 边界；以失败更新不推进、压缩前后任务内容相同则 stamp 相同、查询/折叠不改变它及 Provider 完整请求证据仍独立的集成回归验证。

## 7. 宿主查询与历史投影

- [x] 7.1 将当前任务查询和 durable 更新通知接入 AgentLoop/app 的既有控制与宿主查询路径，支持 idle、模型运行、工具/审批/Question/Stop/压缩期间读取 coherent snapshot；以查询零 journal/网络/工具调用、线程隔离及 pending Sync 只见旧状态的集成测试验证。
- [x] 7.2 扩展统一 ToolActivity/Result 展示为 TaskProgress，并让任务更新打断 Read/Glob/Grep 分组，保持原调用位置和真实成功/失败；以双 facade 等价活动、分组边界和失败候选不冒充已应用列表的投影测试验证。
- [x] 7.3 为原调用位置的 Ctrl+O typed 详情提供完整冻结输入、规范更新、说明与结果，复用四页/每页 64 KiB 查询缓存；以 A→B→clear 后阅读 A、失败 B、恢复阅读、迟到页回复及新当前更新不回填旧详情的集成测试验证。

## 8. TUI 当前清单、面板与焦点

- [x] 8.1 实现 composer 邻近的有界任务区域、完成数量及进行中项显示，Anthropic 优先 activeForm、OpenAI 使用 step，保持原序和准确隐藏计数；以 20 项中后段进行中、无进行中及尺寸高度预算 snapshot 验证。
- [x] 8.2 实现新未完成更新自动展开、全完成折叠、Ctrl+T 本地切换与 none/clear 无操作，无删除计时器；以跨 turn 保留、同来源重复查询/resize 不撤销手动折叠及完成状态不改变 Stop/权限的 TUI 测试验证。
- [x] 8.3 接入无参数只读 /tasks 完整滚动面板，区别 none/clear/全完成，保留完整 content/step、activeForm、explanation 和统计；以活动回合查询、非法参数保留原草稿、错误可返回和 Esc 恢复草稿/光标/锚点测试验证。
- [x] 8.4 在现有补全与保留名称策略中加入第八项 tasks 内置命令，同名 Skill 保持 $tasks 和原 256 Skill 上限；以 Tab/Enter 单次查询、参数原样保留、/skills 冲突填入与 paste 不执行命令测试验证。
- [x] 8.5 为初始/resume hydration、更新通知和面板查询实现 thread/result_sequence/UI generation 去重与过期回复隔离；以旧查询晚于新更新、面板关闭后回复、查询失败保留可识别旧状态及事件丢失后重查的确定性测试验证。
- [x] 8.6 接入既有审批、问卷、Hook 和 transcript 的焦点优先级，隔离 Ctrl+T、Enter、导航与 paste，沿用 Ctrl+C 停止/退出规则；以面板被高优先交互打断、返回保持阅读位置且按键不泄漏到 composer/批准的路由测试验证。
- [x] 8.7 复用安全文本呈现和当前布局，覆盖 80×24、120×40、40×12、13×6、NO_COLOR、长中文/字素与 resize；以合法双向格式字符和显示 helper 的控制/ESC 防御测试验证零横向溢出、composer 优先及显示不改原始事实/fingerprint。
- [x] 8.8 将任务、completion、effort 通知和主线纳入统一高度预算及布局缓存 key，先保证 composer/队列/状态/高优先交互，再保证正在操作的补全候选，任务降为一行或隐藏；以 20 任务+5 候选+多行草稿+队列/状态的窄屏/resize/Ctrl+T snapshot 和 PTY 验证所选候选始终可见且无重复高度分配。
- [x] 8.9 增加纯 UI 的 pending/failed/ready hydration 状态，首次成功前不得显示业务 none；以首次查询阻塞/失败重试、未完成与全完成的首屏展开策略、旧初始 none 晚于新更新及无 Session/协议状态扩展验证。
- [x] 8.10 对同 result_sequence 比较完整 source、规范 TaskUpdate 与派生计数，只有完全相同才幂等；以逐字段冲突安全拒绝并保留已验清单、精确重复不重开清单和更高序号正常替换的确定性 TUI 测试验证。

## 9. Headless 与 stream-json 控制

- [x] 9.1 将 tasks.query 接入外部 v1 严格 NDJSON decoder，判明 type 后在专属解码/admission 前限制完整行 4 KiB，禁止 payload/text/null/重复/未知字段；以随机 chunk、UTF-8 边界、EOF 无换行、超长空白和安全错误不回显测试验证。
- [x] 9.2 接入只含规范 domain 任务数据、真实来源和派生计数的 typed 快照，不披露工具 facade profile，完整编码最多 96 KiB；以 none/clear、20 项最大合法快照、编码膨胀、伪造计数/来源及未知 profile 字段拒绝的固定 golden 和 decoder 测试验证。
- [x] 9.3 通过既有唯一 stdout owner 输出一次 queried control.response 后一次同 request_id/session/thread 的 tasks.snapshot，支持活动查询且不建 turn/input/CompactionID；以响应顺序、请求去重、session_closing、查询前后 journal bytes 和网络次数不变验证。
- [x] 9.4 对只供宿主的任务投影事件显式校验并过滤，使 --print、一次性 --json 与 stream-json 均能正常运行任务工具；以一次性不增加任务专用帧、无 unsolicited snapshot 及 input.submit/input.append 中 /tasks 保持字面正文的回归验证。
- [x] 9.5 将 tasks.query 纳入既有背压、EOF drain、shutdown、短写/断管与资源 owner 清理路径；以 response 已输出后 EOF、snapshot 写失败和活动任务/Provider 清理门控验证不重试、不回滚事实、不提前释放所有权。

## 10. 集成验收、文档与交付证据

- [x] 10.1 增加双 Provider 端到端组合回归，覆盖初次创建、连续替换/回退/清空、Question/批准/Hook/Stop 组合与后续验证工具；以真实 Session 全链路断言调用顺序、无扩大权限、失败不更新及最后清单/详情正确验证。
- [x] 10.2 使用本地假服务运行两家真实 CLI 的 --print、一次性 --json、stream-json 查询/恢复/压缩矩阵，不使用真实 API key 或外网；保存输出和请求断言，验证全文、pairing、查询帧顺序及恢复 canonical bytes 等价。
- [x] 10.3 增加并运行 Darwin 真实 PTY smoke，覆盖 Ctrl+T、/tasks、$tasks、Ctrl+O、20 项滚动、草稿/paste、resize/NO_COLOR、活动审批/问卷/Hook 与取消；保存可复跑脚本及明确断言，验证终端恢复和显示不污染请求。
- [x] 10.4 用真实子进程验证任务恢复/压缩期间 lease 竞争、占用禁止 repair、崩溃释放、继续 seq 单调及失败路径 journal bytes 不变；用确定性门控验证 Sync/cleanup 阻塞，不以 sleep 或概率竞态替代业务断言。
- [x] 10.5 更新 README、能力清单、总体/Tool 架构、Roadmap 和 pitfall，写明两家输入、/tasks/Ctrl+T、预算、恢复/压缩边界及 completed 仅模型报告；通过实际词汇、全部引用/测试路径及未实现能力的逐项核对验证，没有提前宣称 P5/P6/P7 或平台完成。
- [x] 10.6 审查本次跨包依赖、导出强类型边界、实例状态、纯构造/渲染、锁范围、secret/诊断隔离及唯一当前格式，清理临时代码并确认无无用依赖；以现有架构守卫和有针对性的 diff 审查记录验证，不扩大 change 范围。
- [x] 10.7 执行 make verify、openspec validate add-task-progress --strict 及全量 OpenSpec strict 校验，完整保留 gofmt、vet、Staticcheck、架构测试、全量测试和 race 质量门；以每条命令实际结果验证完成，失败不得通过跳过/弱化测试或绕过 hooks 处理。
- [x] 10.8 生成 verification.md，逐项关联当前全部任务、固定 fixture/golden、故障注入、真实 CLI/PTY 与质量门证据，列明实际平台和未运行范围/风险；以完整证据逐项验证任务状态、二轮 review 缺口闭合与实际交付一致。

本阶段依据已评审 artifacts 执行 apply；每项通过对应实现及验证后勾选。验收后同步主规格，归档由后续流程处理；本清单不要求提交或发布。
