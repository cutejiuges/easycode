## 1. 当前强类型目标与完成检查值

- [x] 1.1 实现 tool_call/turn_stop 封闭 HookTarget、声明 scope 和逐 kind constructor/strict decoder/validator；以 domain/protocol 单测验证互斥字段、真实身份、深拷贝、旧 invocation-only shape 和 any/包级状态架构守卫。
- [x] 1.2 定义 StopPlan 的 checks/unavailable 与 StopDecision 的六种 disposition、封闭 code/origin、plan/hook 来源、processed 和续轮计量；以表驱动单测拒绝假通过、空 checks、错 ordinal/target、无 sample 余量、第三次 continued、未启动计数及错 code 组合。
- [x] 1.3 定义独立 typed StopInput、精确 assistant sample reference 与有界冻结反馈；以 UTF-8/空白/JSON 转义边界、getter 变异及无 tool/opaque/版本字段单测验证。
- [x] 1.4 扩展 HookExecution 的事件专属反馈准入，仅实际 Stop 项正常 exit 0、清理确认且 denied/hook_denied/model 可携带合法 candidate；用四事件、exit2、兄弟声明与取消正反例验证原状态/正文组合未被放宽。
- [x] 1.5 定义 FeedbackOnly/WithSteering 封闭候选及最终反馈字节/真实输入关联，区分交付义务和请求预算；以 constructor/strict validator 单测验证深拷贝、不可重渲染、无伪 UserInput/InputID、无任意 envelope 列表。

## 2. 冻结配置、严格输出与执行边界复用

- [x] 2.1 扩展既有 Loader/decoder 到四事件，Stop group 拒绝 matcher，四事件共享来源数量/id/revision 规则；在 stop_config_test.go 验证对象键/枚举扰动稳定、数组顺序、null/重复/未知/超限和跨事件 id 冲突。
- [x] 2.2 为无效来源/Stop 匹配超限产生明确 unavailable 计划输入，合法无 Stop 保留快速路径；以测试验证坏来源不部分放行、已保存正文/只读诊断可用，以及合法 tool-only 请求 bytes 不变。
- [x] 2.3 实现 Stop stdout 的空/严格 block 子集、none/model 披露和 exit 2 无原文规则；在 stop_output_test.go 与 fuzz 覆盖非空/空 reason、4 KiB/16 KiB 实际编码、非法 Unicode/重复键/尾随值和四事件隔离。
- [x] 2.4 将 typed StopInput/目标接入现有 HookRunner、受控 launcher 与 process owner；运行既有 Exec/Pre/Post/Failure 进程回归并以纯构造、单次执行、取消/阻塞双流及无新 cleanup owner 断言验证复用。

## 3. 双 Provider 精确投影及独立原生反馈

- [x] 3.1 为两家 HistoryProjector 实现按真实 turn/sample 的 assistant 正文读取；以 stop_projection_test.go 验证空 sample 不回退、相同正文不同身份、空白原样、目标不存在和所有 opaque 字段不可见，投影前后请求/指纹不变。
- [x] 3.2 分离并复用现有 prepared 的包内一次 resolution，组合 StopContinuationPreparer/PreparedStopContinuation 与完整候选预览/请求编码预检；以生命周期测试验证一/两 native entry 的原子内存提交、revision、pending 互斥、Sync 前不可见、一次 finalize/discard，不串联两个单 entry preparer。
- [x] 3.3 两家当前 native codec 增加 hook_feedback entry/restore role、user-level 包装及来源/冻结正文交叉校验；提交 request/native golden，拒绝伪 InputID/call/output、system/developer role、旧 shape 和跨来源内容。
- [x] 3.4 更新原生完整历史/有效窗口 footprint、语义独立 Hook 来源与 transcript cursor；以 live/restored、离线投影、用户 recall、相同正文不同 cursor 和返回副本变异测试验证，无 Provider wire 泄漏。
- [x] 3.5 将已交付 hook_feedback 纳入后续 checkpoint 来源及窗口编译，pending 决定不推进 native revision；以双 Provider golden 验证旧整窗摘要契约、native +1/+2 与 checkpoint 计数、完整历史/opaque/footprint 和请求权限。

## 4. Session 当前 grammar、固定 fixtures 与故障校验

- [x] 4.1 增加 turn_stop_hook_planned/decided typed drafts/strict decoder/validator，并更新共享 Hook 运行身份；以 kind 全覆盖及缺失/未知/版本/原始输出拒绝测试验证，直接替换受影响 current fixtures 而无 reader/迁移器。
- [x] 4.2 扩展 sample replay 到 native/usage/Stop plan 原子批次和连续 index；以生产 loader/replay 测试拒绝未检查 completion、开放计划内新输入/工具/sample/压缩及错误 native role。
- [x] 4.3 实现 Stop 连续前缀、单独 started、中间 finished、最终 finished+决定及适用 terminal 的规则，continued 只冻结交付义务；以正反例覆盖通过、none/exit2、正常授权 block、真实 denied 后取消、uncertain、兄弟来源伪造及重复决定。
- [x] 4.4 提交不可变 stop_*.jsonl 与预期回放 fixtures，覆盖设计恢复矩阵全部窗口及正常三 sample；测试必须直接读取固定仓库 bytes，断言身份、seq、usage、反馈和 terminal，不在运行时生成输入 fixture。
- [x] 4.5 增加 checksum 合法但语义非法、重复计费、第三次 continued、反馈 bytes/digest/来源伪造及损坏前缀叠加 torn tail 回归；断言失败早于 Repair/append/执行且原 journal bytes 不变。
- [x] 4.6 实现 feedback_pending 到一记录 FeedbackOnly/三记录 WithSteering 的独立 grammar，绑定 prior 决定、真实输入/附件及样本索引；用固定 fixture 测试拒绝漏反馈采样、拆批输入、重复交付、错角色和首次消费前再压缩。

## 5. 信任、Stop 状态机与唯一完成边界

- [x] 5.1 泛化当前信任等待的真实 target 关联并接入 Stop 全声明审查，复用默认拒绝/reset/实例授权；以 stop_trust_test.go 验证错 target/plan/事件/来源/旧 sample、取消后迟到响应及最大转义命令均无错误授权。
- [x] 5.2 将 Runtime 无 calls 分支接入独立 Stop 完成组件，保留合法无 Stop 原路径；以 stop_hooks_test.go 验证 sample 已保存但 turn 未完成、全部通过后一个 terminal、错误/取消/摘要无 Stop 及无假工具。
- [x] 5.3 实现串行短路、两次续轮、false/true/true、30 秒累计与文本额度，冻结取消/来源/headless/平台/编码/执行预算优先级；以假 clock/受控 runner 验证第三次 model block、16th sample 通过或拒绝续轮、64 calls 后纯文本可继续、none/exit2 不被错误重分类。
- [x] 5.4 实现 sample/决定/交付/terminal 独立取消线性化及已接收反馈的本地交付义务；在 stop_fault_test.go 注入每个 append/Sync 前后取消、失败和不确定，验证真实 finished 不改写、未接收真实输入返还、已接收输入不重发、单终态及 poison 后零写入/网络。
- [x] 5.5 保持唯一进程/stream/writer/lease cleanup owner；用确定性 blocked stdin/stdout/Wait 与 shutdown 超时测试证明 Done 前不能 idle/新 turn/释放 lease，无逐次 helper goroutine 或重跑。
- [x] 5.6 接入 --print/--json/stream-json 的无交互信任前检与安全失败；以三个入口行为测试证明零新增 Stop 专用投影/平台探测/runner/等待/bypass，单独计入既有启动 availability，当前外部 JSONL 与合法无 Stop golden 不变。
- [x] 5.7 同步 tool loop 的无 calls/完成与追加安全点契约，在 continued 后复用唯一 input owner 领取一次 FIFO auto 前缀，保留 strict 屏障、InputIDs/冻结 Skill 附件；以 claim/retract/cancel/交付竞争和 Skill 回归验证只有实际复合提交激活附件并发布一次用户 committed，反馈单独交付不改变输入归属。

## 6. 零执行恢复与跨进程所有权

- [x] 6.1 实现独立 StopRecoveryPlan 及 checks 前缀/unavailable/started 尾部补偿；以 runtime/stop_recovery_test.go 对每个窗口断言计划/来源 code、真实 processed、唯一 terminal 和 loader/trust/runner/工具/Provider 全零。
- [x] 6.2 在 app 既有恢复顺序中先完整验证/合法 Repair/Sync，再收口旧 Stop，最后安装当前来源；以删除/损坏配置及 loader spy 测试验证旧恢复零配置/脚本/artifact 读取，不恢复信任。
- [x] 6.3 覆盖一/三记录交付已保存、内存 finalize/通知/下一 sample 未发生及后续工具尾部的重复恢复；断言反馈/真实用户/Skill/wrapper/usage/decision/terminal 不重复，原 bytes 前缀不变，只有新真实输入可发请求。
- [x] 6.4 扩展真实子进程 lease 竞争、崩溃释放及恢复/阻塞清理期间禁止 repair 测试；用控制握手验证 session_busy、失败路径 journal bytes 不变、续写 seq 单调，不以 sleep 或进程内 mutex 证明。
- [x] 6.5 实现 continued 已保存而 native 未交付的 FeedbackOnly 离线补偿；覆盖摘要未启动/started/completed、交付后再次崩溃及编码/Sync 故障，断言先收口摘要、原字节交付一次、零重渲染/queue 重建/重复计费/配置读取/网络。

## 7. 上下文、自动压缩与缓存回归

- [x] 7.1 在 continued 已 Sync、native 尚未交付时增加候选 auto-compaction 安全点，并同步 Session scope 校验；以 stop_compaction_test.go 验证开放检查零摘要、先 discard 候选再只摘要旧窗口、精确 next sample index、一次尝试及预算不重置。
- [x] 7.2 复用窗口 guard、失败抑制/熔断、取消及 checkpoint 恢复，摘要后重建同一冻结候选；以本地 fixture 验证用户超限只返还 claim、反馈单独仍超限时本地交付后失败、零重复摘要/下一普通请求、已接收反馈不放弃或重发。
- [x] 7.3 增加双 Provider stop_hook_cache_test.go，比较完整 uninterrupted/restored/reconciled 请求 canonical bytes、native 次序、opaque、footprint/revision 与分段/请求 fingerprint；分别验证 Anthropic marker/TTL、OpenAI key 与 full/incremental golden。
- [x] 7.4 运行既有 Skill、Hook、用户输入与工具结果预算/cache regression，加入仅改 Stop 配置/trust、不同 cwd/时间、未调用 Stop 的对照；验证稳定前缀、工具目录和已有模型预览不受污染。
- [x] 7.5 构造故意省略阻断 reason 的合法摘要、带 Skill 的真实纠正及空摘要/取消失败路径；双 Provider 请求 golden 必须逐字节包含下一轮首次消费的冻结反馈与纠正，断言候选不进入摘要、交付后首次请求前不再压缩。

## 8. Claude 风格 TUI 与只读历史

- [x] 8.1 扩展 /hooks 和信任 overlay 的回合范围、完整命令、none/model 披露用途与先前工具不回滚说明；以固定尺寸 80x24/100x30/40x16、NO_COLOR、默认 Enter/Esc、paste 隔离及 reset 测试验证零浏览副作用。
- [x] 8.2 接入已保存答复、Checking completion、Preparing continuation 和实际交付后继续状态；以输入路由/快照测试验证 pending 意图无伪 cursor、正文/推理状态不倒退、cleanup 前不 idle、队列/草稿/strict-next 正确归属。
- [x] 8.3 接入 target/plan/detail sequence 及 plan/decision/delivery 阶段绑定的 Stop 通知与只读历史查询，复用四页缓存/异步 owner；以旧 target/plan/序列/代次及已淘汰 cursor 测试证明迟到结果不覆盖当前状态，render 零 I/O。
- [x] 8.4 展示独立 Hook 反馈条目、候选/反馈/续答 cursor 与安全统计；以 live/restored 和 Ctrl+O 分页测试验证只有授权 durable 正文可读、无原始双流/伪用户 recall，重复文本不合并。

## 9. 纵向验收、文档及交付核对

- [x] 9.1 新增 Darwin 真实只读检查与本地双 Provider E2E：通过、两阻断后通过、拒绝、none、exit2、错误/超限、取消和工具修复链；断言请求数/TurnID、权限、文件 bytes、单终态和恢复零重跑。
- [x] 9.2 运行受控进程沙箱及隐私测试，向原始双流/未授权 reason/候选环境注入合成哨兵；断言写工作区/联网/私有 Session 访问拒绝，journal/日志/错误/快照/artifact 不泄漏未授权原文，既有命令 owner 回归通过。
- [x] 9.3 实现并运行 scripts/stop-hook-smoke.py 的双 Provider PTY 场景，覆盖完整审查、/hooks、Ctrl+O、默认拒绝、reset、resize、NO_COLOR、取消和退出；仅使用本地假服务，并保存实际命令/平台/结果。
- [x] 9.4 更新 README、总体架构、能力清单、Roadmap 和 pitfall 的真实交付边界；逐项核对 Session/native/决定 vocabulary、引用和实际测试文件，明确两次续轮、只读限制、坏配置失败及非 Darwin 未支持，不把规划写成已实现。
- [x] 9.5 执行 make verify、主 specs 与本 change 的 strict validation、定向故障/恢复/cache/压缩验证及实际可用的交叉构建；验收记录区分原生执行与编译、基线与新功能，未运行项列明原因/风险，无绕过质量门。
- [x] 9.6 对照十二份 delta specs、设计状态表和两参考工程固定快照完成逐条审阅，验证无 any/全局可变/重复副作用/secret/dead code/新依赖/兼容 reader；验收后先同步主 specs，再按独立归档流程处理，提案阶段不勾选或归档。
- [x] 9.7 将本次交付状态模型转为实现回归：验证旧整窗摘要可丢 reason、修订路径逐字节保留、多故障 headless 优先级、第 16 sample 无余量、64 calls 纯文本、真实最后 exit0/model 来源及反馈/用户取消线性化；记录新功能实测结果，不以设计模型替代。
