## 1. 事件与命令契约

- [x] 1.1 将 `UserPromptSubmit` 接入事件、无 matcher 声明、冻结排序、诊断和声明 fingerprint。扩充配置用例，验证来源/id/上限沿用、无 matcher 与 `feedback: none` 的严格语义、其他事件规范 bytes 保持一致。
- [x] 1.2 实现可比较的输入目标和有界 typed stdin。验证实际输入换行与 UTF-8 保真、JSON 转义超限时完整返回，以及 stdin 只含约定字段和原文，无附件展开、历史或宿主凭据。
- [x] 1.3 将事件专属控制输出接入现有 runner。验证干净 exit 2（含空 stderr）、结构化阻断、严格拒绝非本事件输出，覆盖 I/O/超限或取消优先于退出分类。复用既有测试核对其他事件的退出码与披露语义。

## 2. Session 事实与恢复

- [x] 2.1 实现计划、运行、决策与收口的 typed record 和严格 replay。用固定 current 字节验证合法位置、单记录批次、完整成功前缀及终止决策关联。覆盖伪造的多记录尾声明在 repair 前拒绝。
- [x] 2.2 将计划通过项绑定普通、SessionStart 与 Stop 复合输入关联。验证通过项按原序只交付一次，停止收口、错位交付和绕过已占用 ID 均拒绝。用同一 fixture 验证完整回放及继承前缀的 metadata 路径。
- [x] 2.3 在同一 lease 下收口运行中断，再本地履行已接纳的 Stop 反馈及后续恢复。用中断和已开始恢复的前缀验证零脚本、零网络、幂等补偿、真实撕裂修复和坏批次拒绝。

## 3. Runtime 接入与交互

- [x] 3.1 在现有 turn worker 复用逐声明信任、进程 owner 和共享执行预算，先 Sync 启动证据再执行。用确定性屏障验证迟到信任、拒绝、信任等待取消和跨安全点额度，明确未经授权的项从未接收 prompt。
- [x] 3.2 将有序逐输入结果接入唯一 claim owner，验证承载请求被拒但后续项通过时的关联、字节额度、FIFO、strict-next 屏障及唯一终态。用混合检查的取消场景保留既有拒绝并返还其余输入，另核对原生写入结果不确定时不返还。
- [x] 3.3 接入初轮与工具后追加。验证全拒首轮零摘要、零 SessionStart、零请求，之后一次合格首轮仍运行激活检查。验证工具后混合追加独立结算，另验证全拒追加后仍使用既有工具输出继续。
- [x] 3.4 接入 Stop 纠正和 Plan 实施的正常入口。验证纯反馈与批准动作不触发，阻断纠正仍按现有路径交付反馈。验证 Plan 通过保持原确认正文与普通工具批准，拒绝时可在 Default 模式取回实施正文。
- [x] 3.5 复用 `/hooks`、信任与 returned 交互。验收窄屏防误授权、声明来源及 ID、`Ctrl+R`、当前草稿、内存召回、取消与陈旧通知隔离。用一次带 Skill 或文件内容的真实输入验证附件留在原有 Provider 路径，并核对不发起查询的本地命令跳过检查。
- [x] 3.6 用 `--print`、`--json` 和 stream-json v1 验证 `hook_trust_required`、零脚本及零 Provider 请求。验证严格 `input.submit` 和同批 `input.append` 的不同身份、回执与 EOF drain，确认 v1 无新增交互字段或帧。

## 4. 跨边界验收与规格同步

- [x] 4.1 用现有 Anthropic Messages 和 OpenAI Responses mock 及 golden 比较完整逻辑请求、原生顺序和 cache fingerprint，包括禁止新检查元数据进入 cache 和不中断与恢复续写等价。取同一端到端场景核对被拒正文、脚本原始流和自由文本原因未进 Provider、Session 或日志。
- [x] 4.2 用真实 Darwin CLI、PTY 和 sandbox 验收授权、执行、阻断、取回修改及 Ctrl+C 清理，Hook fixture 排空 stdin 后再正常退出。用 writer 接纳前与 Sync 后的确定性屏障区分运行未知和原生写入未知，按关键并发边界核对 race 清单。
- [x] 4.3 对非 Darwin 验证显式平台失败路径及 Linux/Windows 构建，记录实际运行平台。执行 `make verify` 和 OpenSpec 严格校验，记录命令、结果与必要新增用例的独立风险和耗时。
- [x] 4.4 同步 README、能力清单、路线图、架构和相关主规格，核对日志与未完成能力描述。验收确认后按 OpenSpec 流程归档，保留简短验收证据。

4.4 的文档、八份主规格和[验收记录](verification.md)已同步，合入主线后的回归和真实会话验收均通过。
