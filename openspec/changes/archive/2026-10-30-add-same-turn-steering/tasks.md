## 1. 当前强类型契约

- [x] 1.1 在 internal/domain 与 internal/protocol 定义独立 InputID、有序用户输入、投递意图、claim resolution、自动提交/取回/stop 命令及输入状态；补构造、validator、strict decoder 测试，验证零值、重复 IDs、非法 kind/payload 和越界均无副作用，不启用既有 ItemID/CallID 占位。
- [x] 1.2 将 SemanticTurn 替换为真实 turn identity 下的有序强类型消息，用户消息保留 InputID；同步全部编译期消费者，单测验证深拷贝、消息顺序、非法身份拒绝与无旧 UserText/AssistantText 兼容分支。
- [x] 1.3 在 Provider 边界增加纯内存 UserInputPreparer、PreparedUserInputs 和强类型 restore association；仅组合复用与工具输出相同的私有生命周期骨架，测试 finalize/discard 互斥、重复 resolution、未决清理与纯构造。

## 2. 双 Provider 原生输入与规划

- [x] 2.1 更新 OpenAI 当前 native canary 为 2，加入 user_inputs，删除 sample input 字段并采用封闭三 kind 校验；补 request/native golden，验证独立 input_text items、function/custom outputs 后顺序、失败保留用户输入和 reasoning/encrypted/phase 无损。
- [x] 2.2 更新 Anthropic 当前 native canary 为 2，加入独立 user_inputs 与对应 footprint/restore；补 golden 验证 tool_result 消息后独立用户文本、连续 user 消息、thinking/signature/redacted 内容无损且不合并历史消息。
- [x] 2.3 两家 projector 使用真实 restore turn association 生成有序消息；补失败/input-only turn、预检丢弃不改变历史、多个 steering 边界、空可见输出与深拷贝测试，验证单向投影不参与 request 重建。
- [x] 2.4 修改 internal/context 规划和 Provider 采样输入契约，所有真实文本从 committed history 计量，current_input 只表达无正文采样；回归覆盖 max 可见/native 估算、unknown、提交前预算失败返还输入且历史不变与仅改变 InputID/turn ID 时 fingerprint 不变。
- [x] 2.5 增加纯内存输入可表示性、最大前缀编码预算及候选请求预检计算，复用唯一 canonical codec；边界测试覆盖 JSON 转义膨胀、record envelope 开销、单条 4 MiB、native/record 16 MiB 和 batch 64 MiB，确认可预判错误不进入 writer。

## 3. Session 事实、回放与恢复

- [x] 3.1 更新唯一 Session canary 为 5，为 turn_inputs_committed 增加强类型 draft/strict decoder/validator；测试 placement、target_sample_index、有序非重复 IDs 与禁止正文/RequestID/未知字段，保持 SQLite schema 不变。
- [x] 3.2 扩展 replay 的 initial/steering 输入 batch grammar 与 native restore association；测试缺失/重复 initial、错误 target、未闭合 outputs、terminal 后输入、重复 InputID、opaque role/关联不一致全部失败关闭且 journal bytes 不变。
- [x] 3.3 更新 app 恢复映射及 input-only interrupted-tail 本地补偿；集成测试证明已保存输入可见、旧 turn 先 durable 失败收口、恢复零网络/工具调用、未提交队列不重放，补偿失败禁止新任务。
- [x] 3.4 直接替换 testdata/current 的固定当前 bytes 及相关 digest，覆盖文本、全部七工具、输入后崩溃和混合 steering；通过生产 loader/replay 测试，不在测试运行时生成输入 fixture，旧 canary 在 repair/append 前拒绝且不保留双 reader。

## 4. Runtime 单 owner 投递与持久化

- [x] 4.1 在 agent_loop 的明确输入队列组件中实现单 FIFO、strict-next 屏障和自动前缀初始领取，复用现有 request ledger；确定性测试验证 auto/strict 交错、并发 admission、首条 origin 与 64 条/16 MiB queued+claims 预算。
- [x] 4.2 在 RunTurn 统一首轮输入 prepare、候选预检、input batch Sync、finalize 与 committed 事件，输入 Sync 前先执行候选规划和请求编译预检；测试首次请求前失败、预算失败返还后缩短重提、sample 失败与成功路径，确保不重复保存或发送用户输入。
- [x] 4.3 在完整 tool outputs durable/finalize 后接入有界安全点 claim 握手与 resolution，保持 owner 可处理控制命令；测试 late input、无 calls final、严格屏障、前缀分批及 16 samples/64 calls 上限不被重置。
- [x] 4.4 实现原子按 IDs 取回、定向交互 stop、停止期间拒绝 auto、approval abort 返还及 strict-next 保留；用控制 channel 固定 claim/retract/stop/terminal 交错，验证每条输入唯一 committed/returned/uncertain 结果与旧 interrupt 不变。
- [x] 4.5 将取消线性化绑定 writer admission，保持接收前可取消、接收后等待确定结果；注入 Sync 失败和阻塞提交，验证成功取消竞态不返还、未知 poison 不重试、claim 未决不释放额度/lease。
- [x] 4.6 扩展 EOF/shutdown/宿主断开清理并复用既有唯一 owner/done 路径；测试输出背压、阻塞 stream、超时升级、重复关闭及真实子进程 lease 竞争，确认 cleanup 完成前不能取得 lease，无逐次 helper goroutine。

## 5. 宿主和外部协议

- [x] 5.1 将 app ChatSession 接入一个长期控制输出消费链，移除旧 TUI 单次提交的重复路由；集成测试验证一个 owner、一个消费者、事件顺序与关闭等待实际运行实例，不增加第二个可执行队列。
- [x] 5.2 在 headless stream decoder 增加 input.append/input.retract/turn.stop，输出 entry_id 与不含正文的 input.status；fixture 测试覆盖碎片/UTF-8/null/重复未知字段、response 先于状态、committed 先于 delta 及多输入同 turn 的单份输出。
- [x] 5.3 保持 input.submit、turn.interrupt、一次性 text/JSON 的既有外部形状和行为，过滤新增内部事件；运行原有 headless golden 与 stream e2e，并增加 strict/auto 混合 FIFO、EOF、shutdown 和断管回归。

## 6. Claude 风格交互

- [x] 6.1 普通运行中开放 composer 与显式 paste，Enter 发送自动输入，admission 等待仍保留草稿；固定尺寸 snapshot 与模型测试覆盖 running、queued、claimed、committed、失败和同 turn 交错消息，InputID 移入 transcript 恰好一次。
- [x] 6.2 实现空草稿 Up/普通 Esc 的原子取回、指定 IDs 和合并正文 4 MiB 预检，等待时冻结编辑；测试 claim 竞态失败不改草稿、新队列项不误取、Unicode 光标和 recall 100 条/8 MiB 不回退。
- [x] 6.3 实现 Ctrl+C/无可取回队列 Esc 的定向 stop 及有界 returned tray，提供明确逐条取回/丢弃操作与可见提示；测试超过 composer 总量仍完整保留、现有草稿不覆盖、共用保留额度和不自动执行。
- [x] 6.4 保持审批、反馈编辑器、命令详情的局部键盘优先级和安全粘贴，approval abort 接入返还规则；snapshot/模型测试覆盖 overlay Esc 层级、停止中的输入拒绝、窄屏、无色和 resize，不在 render/getter 中 I/O。

## 7. 跨模块验收与文档

- [x] 7.1 双 Provider 各执行首轮、多轮、工具后追加多条、输入已保存后失败/取消/崩溃的恢复集成测试，对比 uninterrupted/restored canonical request bytes、native item 顺序、opaque 内容、footprint 和 fingerprint，验证输入不重复及工具不重执行。
- [x] 7.2 更新架构、Roadmap、pitfall 与用户帮助中的输入状态、键盘路由、record vocabulary、canary、恢复与内存队列边界；校准实际 Claude 2.1.88/Codex 6f8bf683c 参考快照并保留历史 ADR 语境，核对引用路径存在且未把延期能力写为已实现。
- [x] 7.3 执行 openspec validate add-same-turn-steering --strict 和增量/主规范契约审查，执行 make verify（gofmt、vet、Staticcheck、架构边界、全量测试与 race）；逐项记录结果及无法执行项，不弱化测试，不在本任务中自动归档。
