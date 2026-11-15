# ADR-0010：原始调用、检查候选与有效执行输入分别建模

- 状态：accepted
- 日期：2026-11-14
- Roadmap 阶段：P6 纵向切片
- 对应变更：[add-pre-tool-use-input-rewrite](../../../openspec/changes/archive/2026-11-15-add-pre-tool-use-input-rewrite/proposal.md)

## 背景与约束

当前两家 Provider 的三项 mutation 已有 typed 输入、独立普通审批、同步 Hook、durable Session 及恢复消费者。参数改写必须同时保留原生调用事实与用户实际批准的执行事实，不能用一个可变调用同时表达二者。stdout 的合法性也不能替代输入、来源语义或结果预算验证。

参考基线为 Claude Code 恢复源码 2.1.88，以及 Codex `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`。Claude 的原始可观察输入与实际执行输入分离、正常权限路径和交互审查值得采用；Codex 的各能力正反向适配与改写后重新 review 适合强类型内核。两份参考仅供只读，未成为构建依赖。

## 候选方案与决定

拒绝直接修改 native call、Provider wire 或 ready；它们仍是原请求 O。拒绝任意 JSON 合并、最后完成项覆盖及兼容 reader。采用如下分工：

1. domain 持有封闭来源 profile、完整替换与 unchanged/replaced effect。Edit 保持单 replacement 子串，apply_patch 保持有界 LF 行，不能由脚本切换语义。
2. tool 纯适配 O 与规范输入，按已 Sync 的 effect 推导检查候选 C。全 gate continue 才选择 E；拒绝时结果仍绑定 O。`InputSelection` 绑定实际 Session/thread/turn/sample/call，完整证明不能缩成最终参数 hash。
3. Runtime 沿现有 exclusive、runner 和单 writer 顺序执行。下一项读前项已提交 C；最后正常 finished 与 gate continue 同批，即使晚到取消也保存真实结论，再在普通执行边界处理取消。该串行语义明确区别于 Claude 的共享 stdin 并行处理和参考 Codex 的最后完成项规则。
4. 仅 E 进入候选、policy、完整普通审批、executor、metadata 与 Post。Hook 不能修改身份、capability、wire、workspace、shell、环境、sandbox、预算或审批决定。
5. `tool_execution_started` Sync 后登记唯一准入，独立于 Post；结果说明按同一事实区分真实执行尝试、未准入结果及恢复补偿。准入不证明 OS 已启动。说明是纯渲染策略，优先占用原有 ticket，并在普通候选前预检；artifact 与 Post typed stdin 不混入说明。
6. 宿主先安装不可变 Pre 快照，再用真实 sequence 发出有界活动。Ctrl+O 详情沿异步 generation 与四页缓存刷新，保留原请求与阅读锚点。安装失败 poison，不回滚、不重跑已提交事实。

## 代价与边界

需要把原调用证明贯穿九能力的通用结果与 native restore 边界，只有三项 mutation 开放改写。以较严格的一套当前证明换取无 Post 时、恢复时和改回 O 时的一致行为。原始/中间输入会增加 Session 与详情大小，完整规范替换限 16 KiB；不截断对象、不接受任意大 Write 替换。

raw stdout/stderr 各 16 KiB、finished 64 KiB、stdin 512 KiB、来源说明 1 KiB、短说明 256 bytes、Pre 活动块 4 KiB/整消息 16 KiB 分别按实际编码验证。必要输入变化经信任说明后保存；原始双流和自由 reason 继续丢弃。改写审批在小于 32×18 的终端停用，直到能完成必要审查。

## Provider、缓存和 Session

两家 native history 仍保存 O、opaque thinking/signature/encrypted/phase 及原 item 顺序。恢复逐一对照原 ready 和 native 参数，并以相同选择/准入证明核验输出。Hook 配置、信任、UI 状态不进入稳定 prompt 或工具 schema；变化只进入必要的工具结果。Anthropic marker/TTL 与 OpenAI key/WebSocket 分别验证完整逻辑请求，不能把 full 与 incremental 帧直接当成相等。

不新增 record kind，不添加格式数字、兼容窗口或迁移器。唯一当前 Pre finished 必需 input_effect；旧开发 shape 失败关闭。语义校验先于尾部 repair，append-only 旧事实不得原地改写。恢复只追加确定补偿，已有冻结结果原字节复用，不读取旧脚本、不探测 PID、不重跑工具或网络采样。

## 验证与替代

纯契约、parser/fuzz、Runtime 取消/额度/重入、固定 JSONL 损坏和真实 lease、双 Provider request/cache golden、真实 Darwin sandbox、TUI snapshot 与 PTY 的对应证据见本 change 的 [verification.md](../../../openspec/changes/archive/2026-11-15-add-pre-tool-use-input-rewrite/verification.md)。Linux/Windows 交叉编译不作为原生 sandbox 验收。

未来若改为并行 Hook、部分合并或新增可改写能力，必须另经 OpenSpec 明确顺序、审查、预算与恢复契约；不能静默更换本决策或复用 E 覆盖 O。
