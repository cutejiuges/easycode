## 1. 当前强类型与身份

- [x] 1.1 在 domain 定义仅含 PreToolUse/PostToolUse 的事件及 none/model 披露策略，并纳入声明/reference/fingerprint；通过构造、未知值、默认值和复制隔离单测。
- [x] 1.2 更新 plan/run identity，使事件、plan digest、invocation 和 ordinal 唯一关联；以相同 invocation/ordinal 的前后事件不碰撞及交叉配对拒绝测试验证。
- [x] 1.3 定义封闭 Post 结果投影、反馈候选、冻结反馈及后置终态，加入新的稳定 code 和 strict decoder；测试缺字段/重复键/null/未知字段/跨种类字段/越限拒绝。
- [x] 1.4 调整 Hook 执行返回值，使无正文统计与显式反馈候选分离，按事件校验 Post exit 2；编译全部当前 fake/adapter，测试 Pre 与未授权结果不能携带正文，且无 any/动态 map 接口。

## 2. 配置、来源与受限执行

- [x] 2.1 扩展现有 hook loader 接受 PostToolUse 与仅该事件支持的 feedback，保持未知事件失败关闭；配置回归覆盖缺省 none、非法 Pre feedback、双事件及固定顺序。
- [x] 2.2 对两个事件合计来源/总数上限及来源内唯一 id，按事件匹配八项；测试重复、别名只执行一次、对象键顺序无关和披露修改使 revision/fingerprint 变化。
- [x] 2.3 复用 localfile 的同 FD/权限/symlink/一致性边界，不新增打开通道；既有 Skill/Hook 安全回归与确定性路径替换测试全部通过。
- [x] 2.4 为共享进程骨架实现独立的 Post 严格输出策略，Pre 保持原 deny 子集；测试空白/空对象/additionalContext、全部不支持字段、事件错配和非零含 exit 2。
- [x] 2.5 在 process owner 内保持双流 16 KiB、stdin 512 KiB、取消/TERM/KILL/Wait/Done；故障注入验证阻塞 stdin、输出超限、I/O 失败及 cleanup 不确定，不以 sleep 证明正确性。
- [x] 2.6 Darwin 集成验证 Post 只读/禁网、私有 Session 隔离和最小环境；其他平台 adapter 在执行前失败关闭，不将交叉构建描述为运行验收。

- [x] 2.7 按 Pre/Post 分别验证 exit 2 与结果 status/code，不全局放宽 Pre；矩阵测试覆盖两事件及取消/实际超时/输出超限/I/O 同时发生时的唯一分类。

## 3. 反馈组合与预算

- [x] 3.1 在 tool 实现组合冻结真实结果与独立反馈的强类型模型输出值及固定 canonical 反馈段；测试无匹配/全部 none 的输出逐字节不变，model 空反馈不错误还原预览，来源明确且无 system/developer 或伪造 InputID。
- [x] 3.2 在有序预算 owner 实现调用 ticket 的 reserve/base/feedback/close，按 R 调整 base allowance；测试每段 Sync 后计费、最终 close 只推进一次调用数、未关闭时拒绝后序 admission，以及无匹配/none 零影响。
- [x] 3.3 实现每项 4 KiB、每调用最终反馈段 16 KiB 与原单次/sample/turn 共同上限，整块验证实际编码；测试来源包装、JSON 转义膨胀、512-byte 后续提示槽、最小槽不足的 budget_unavailable、临界额度/零额度和后续安全预留。
- [x] 3.4 拒绝未授权和超预算整块且只保存安全 code，接收正文不写 diagnostic/artifact；合成 secret 哨兵测试覆盖 stderr、普通 stdout、非法结构及 none 模式。
- [x] 3.5 从已 durable 的真实 metadata 构造三分支完整字段的 Post stdin，不透传 preview/stdout/stderr 或字符串删除 artifact 提示；固定 fixture 覆盖全部字段、非零 Exec exit、缺失/跨能力字段拒绝、超限零启动及无 artifact ID/内部路径/PID/宿主凭据。

- [x] 3.6 添加 A=256 KiB/R=16 KiB/250 KiB 预览且 Hook 返回空对象的回归，确认仅输出已冻结的 240 KiB 额度预览，不重读或扩展，原 status/code 保持。
- [x] 3.7 用多项 model Hook 验证前序反馈 durable 后最后一项失败只用预留槽，不能驱逐前序内容；覆盖 none 混排、空对象释放槽及 ticket 关闭后总额/计数正确。

## 4. Session 当前词汇与严格回放

- [x] 4.1 新增 tool_post_hook_planned/completed 的 typed draft、strict decoder、validator，并更新执行事实事件身份/反馈联合；当前 record 构造与未知旧 shape 拒绝测试通过，不添加版本或兼容 reader。
- [x] 4.2 实现 Pre/Post 独立回放状态及 plan/真实 result/started/finished/terminal 合法批次；测试真实结果先 Sync、最后 finished+completed 原子，以及 processed/unstarted_from、terminal origin/suffix_feedback、无运行后缀和普通 error 后继续的严格语法。
- [x] 4.3 强化屏障与证据验证：后序 call/outputs/steering 不能越过 Post terminal；测试无成功结果却 post started、重复/错序/跨事件身份、未授权/超预算正文、伪造 suffix_feedback/预算终值和 forged native 关联拒绝。
- [x] 4.4 提交不可变 current fixtures，覆盖成功反馈、none、拒绝、skipped、每个合法中断点及损坏；更新所有受新身份影响的 Pre fixtures，测试运行时只读取固定 bytes。
- [x] 4.5 对新事实的 Append/Sync 接收与取消线性化加入确定性故障测试；验证无法确认时 poison，已保存真实 result 不改写，不保存未确定反馈。

- [x] 4.6 固定 bytes 覆盖共享 stdin 超限零 started、部分 finished 后预算耗尽的 skipped 后缀、正常 suffix_feedback 与 recovery 禁止新反馈；strict replay 验证没有假 finished/timed_out 统计。

## 5. Runtime 信任与前后置编排

- [x] 5.1 在现有 exclusive 策略骨架中冻结 Post plan 并在候选/普通审批前完成独立信任；测试仅 Post、Pre+Post、既有 grant、拒绝、headless 及旧响应零副作用。
- [x] 5.2 将 event/feedback 纳入内存 trust/reset 身份，保留真实 session/workspace 隔离；测试前后同命令不共享授权、披露策略改变需重审、resume 不恢复信任。
- [x] 5.3 在真实工具 result 成功 Sync 后运行或 skipped Post；覆盖 Patch/Write/Exec 的成功、准备失败、拒绝、conflict、取消、不确定和非零正常 exit，保证 result 只写一次。
- [x] 5.4 Post 普通错误保持真实结果及后序独立调用，取消/cleanup 不确定停止后续工具；混合 Read/Write/Read/Exec 顺序测试验证屏障、原 pairing 和零自动重试。
- [x] 5.5 Pre/Post 对同调用共用 30 秒执行预算并沿用唯一 process owner；以注入时钟/屏障验证剩余预算、信任等待不计费、hook_execution_budget_exhausted 未启动后缀与实际 hook_timed_out 分开、不构造假执行统计。
- [x] 5.6 分离 Pre 缓存、真实结果缓存和 Post 收口状态，重复消费不短路未完成检查或重跑已完成项；重放/幂等回归覆盖相同 invocation 和伪造结果。
- [x] 5.7 为 result/plan/started/finished/completed 各 Sync 失败、过期信任、runner 错配与 shutdown timeout 加入故障注入；验证 owner/lease 保留到最终 Done，零 helper waiter 泄漏。

## 6. 双 Provider 原生输出与恢复证据

- [x] 6.1 将唯一当前 ToolResultPreparer/Preflight 接口及全部消费者改为强类型模型输出组合值；全包编译和架构边界测试通过，不保留旧接口/备用 reader。
- [x] 6.2 Anthropic tool_result 编码原 preview+冻结反馈且保持原 is_error/call ID；Edit/Write/Bash golden 覆盖有/无反馈及真实成功不变。
- [x] 6.3 OpenAI function/custom outputs 编码同一组合内容且保持 status/code/wire kind；Write/exec_command/custom apply_patch golden 覆盖完整配对与顺序。
- [x] 6.4 为 tool-output RestoreRecord 绑定已验证 result/Post/反馈证据；两家 strict restore 测试拒绝伪造正文、错序、重复、跨调用或缺失证据，不按文本标签猜配。
- [x] 6.5 验证 model 空反馈预留后冻结预览、feedback/none、无匹配、取消、压缩及恢复后的 canonical bytes、原生 item 顺序、opaque 数据与 fingerprint；覆盖 OpenAI 可选 WebSocket 的 durable 增量与 full/reset。
- [x] 6.6 缓存回归验证 Hook 配置/trust/披露不污染 tool catalog、skill metadata 和稳定前缀，实际反馈仅改变可解释历史尾部；使用同事实不中断/恢复对照断言。

## 7. 离线 reconciliation 与进程间所有权

- [x] 7.1 扩展 recovery plan 区分 Post plan 无执行、tool started 无结果、成功 result 无 Post started、Post started 无 finished、部分 finished 后下一项未 started/已 started 两种尾部、terminal 无 outputs；矩阵测试证明旧工具/Hook 零执行。
- [x] 7.2 保留真实 success 和已保存反馈前缀，追加对应 skipped/cancelled/uncertain 后置收口再补 outputs/turn_failed；验证 origin=recovery/suffix_feedback 为空、不补发提示、不查询 PID、不读取脚本/配置/artifact、不自动采样。
- [x] 7.3 让正式恢复与离线预览共用当前严格 evidence 校验，并保持验证先于 repair、新来源 Load/install 晚于旧补偿；测试配置删除/修改与预算改变不影响冻结 bytes。
- [x] 7.4 在每个补偿 batch 后再次模拟崩溃，重复恢复不产生第二个 result/finished/completed/output/terminal；验证 seq 单调和请求等于同一已补偿历史。
- [x] 7.5 使用真实子进程验证 Post 活动 lease 竞争、崩溃释放、占用期间禁止 repair；非法完整前缀+半行的恢复保持全部 journal bytes 不变。

- [x] 7.6 分别验证部分 finished 后下一项未 started 仅补 cancelled terminal，以及下一项已 started 才补 uncertain finished；二次恢复不新增后缀运行或模型提示。

## 8. TUI 与宿主详情

- [x] 8.1 /hooks 列表及详情支持事件、feedback、来源、完整命令和原 reset，固定尺寸/窄屏/NO_COLOR snapshot 验证草稿保持及无 I/O。
- [x] 8.2 Post 信任 overlay 在工具前展示运行时机、只读限制、发送与保存用途；测试默认 Enter/Esc 拒绝、paste/stale/modal 隔离和 Ctrl+C 定向取消。
- [x] 8.3 主线保留工具成功并独立显示 Post 检查中/通过/失败/取消，合法 Post checking 不因 toolClosed 丢弃，旧 Pre 不得重开；snapshot 和输入路由测试验证 cleanup 前不 idle。
- [x] 8.4 扩展 typed 查询的 post_hooks source、期望 detail_sequence 和 transcript_changed，cache key 绑定 sequence 且失效时保留原工具锚点；测试四页/64 KiB、未授权无正文页、旧回复/混页拒绝、恢复同位置与安全转义。
- [x] 8.5 在 Post 批次 Sync 并安装快照后发布 typed post_hook_committed（invocation/event/plan/sequence/阶段），供 TUI 刷新；headless 明确校验后忽略，三入口 fixture 证明外部协议与失败关闭不变。

- [x] 8.6 修改 session/tool_history 与 app/transcript_owner 的关联键，测试同 invocation 的 Pre 0/Post 0 及后置普通错误后继续，不再以单一数组长度或 invocation-only plan map 关联。
- [x] 8.7 保持后置详情打开，按屏障注入新 durable 反馈、过期回复及跨修订分页；验证 post_hook_committed 驱动即时刷新、generation 失效、首屏重载和锚点保持。

## 9. 端到端、文档与验收

- [x] 9.1 双 Provider mock transport + 实际运行链验证 Patch/Write/Exec 成功→检查→模型收到反馈→继续；断言调用顺序、预算、唯一结果与无授权泄漏。
- [x] 9.2 新增 Darwin PTY smoke，覆盖独立信任、普通批准、/hooks、反馈详情、取消清理和终端恢复；使用合成凭据/本地 server，不访问外网。
- [x] 9.3 同步 README、总体/Tool 架构、capability-status、Roadmap 与真实踩坑，准确记录只读/平台/未实现事件及披露限制，提供可复制的 metadata-only stdin/结构化反馈只读 shell 包装和 model 空反馈预算说明；核对所有测试路径与实际 record vocabulary。
- [x] 9.4 运行 make verify、openspec validate add-post-tool-use-command-hooks --strict 和全部主规格校验；在 verification.md 记录命令、平台、退出码与矩阵证据，未执行项注明原因/风险。
- [x] 9.5 核对每条 task 的业务断言与新旧回归、无全局可变状态/跨层依赖/死代码/secret 泄漏；确认 proposal/design/specs 与最终实现一致后交付，spec 同步与归档由后续明确流程执行。
- [x] 9.6 使用文档中的同一只读检查包装运行 mock/PTY 验证：发现问题返回 exit 0 严格 additionalContext、执行异常非零、默认 none 不披露；示例不写工作区/缓存、不联网且不使用真实凭据。
