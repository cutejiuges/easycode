## 1. 强类型问题与回答

- [x] 1.1 在 `internal/protocol` 实现不可变值并由 `internal/tool` 别名复用 QuestionID、Input/Option/AnswerSet、Invocation 与 fingerprint 的封闭值、构造和深拷贝；验证数量、单选/多选、索引规范化、跳过、16 KiB 文字与所有精确边界单测。
- [x] 1.2 实现 input/answer/result strict codec 和完整确定性 preview；验证重复键/ID/标签、大小写别名、未知字段、null、尾随 JSON、非法 UTF-8、模型预填答案均拒绝，getter 变更不污染源值。
- [x] 1.3 扩展 ReadyCall、Invocation、InvocationResult、result metadata 与 product ToolName 当前联合；验证每种不匹配字段失败，现有八能力构造/codec 回归仍通过。

## 2. 目录、执行器与调度分类

- [x] 2.1 增加真实 QuestionExecutor/QuestionResponder 小接口及 builtin 执行器，纯 New 构造，不依赖 Provider/TUI；验证作答、跳过、无交互、取消与错误结果，以及构造无 I/O/goroutine。
- [x] 2.2 增加 `user.question` 与两家 facade/schema/description，装配真实组件、稳定排序与九能力目录；更新 catalog golden，验证注册顺序、getter 修改及动态题目不改变定义，缺失 executor 必须构造失败。
- [x] 2.3 分离 Exclusive 与 RequiresApproval，逐项处理 scheduler、approval validator、readonly material、replay/recovery 的判定；验证 Question 独占且无审批，Patch/Write/Exec 的 allow/grant/sandbox 约束与五种并行读分类保持正确。

## 3. Provider 原生闭环与输出预检

- [x] 3.1 为 OpenAI `request_user_input` 接入 request、strict decoder、reducer 与能力名称映射；增加 request/stream/result golden，验证随机 chunk、半包、UTF-8、断线/取消、完整 terminal 前零问卷事件。
- [x] 3.2 为 Anthropic `AskUserQuestion` 接入同等完整链路；增加 tool_use/tool_result fixtures，验证 block index、message_stop、非法参数零 ready/交互，thinking/signature/redacted 原样保留。
- [x] 3.3 在现有 ToolResultPreparer 增加不可变单项 ToolOutputPreflight 策略，复用两家私有 output 编码器；验证错误 call/capability、真实编码大小、快照深拷贝，以及预检不修改 history/revision、不 finalize、不执行 I/O。
- [x] 3.4 扩展两家 native codec、完整恢复与离线校验，验证 Question call/output、preview、题目/回答身份和有序混合组，保留 OpenAI phase/encrypted reasoning；错误数据事务性拒绝，无部分 Conversation。

## 4. 回答控制与 Runtime 生命周期

- [x] 4.1 新增 resolve_user_question 和 user_question_requested/resolved 的强类型 constructor、strict decoder、validator、control error/result 路由；验证 kind/payload、非法/旧身份和重复 RequestID 在副作用前失败，headless 外部 decoder 不暴露新回答命令。
- [x] 4.2 在 Runtime 实例实现单 pending 请求与 responder，复用既有 turn worker 与 AgentLoop owner；用确定性 barrier 验证 answer/stop/父取消竞态、无活动/过期/重复回答和接收后的不可撤回义务，锁内无编码/回调/I/O/阻塞等待。
- [x] 4.3 把 Question 独立控制策略接入有序 scheduler 和 started/result 骨架；验证 Read -> Question -> Write -> Question 的屏障、started Sync 前零问卷、所有结果按原 index，回答不能授予任何权限。
- [x] 4.4 接入回答 allowance、Session/Provider 实际编码预检和完整结果冻结；验证超限/转义膨胀拒绝保留活动请求、不写盘不扣账，接收后的回答不被截断、steering 不重置预算，耗尽前不弹问卷。
- [x] 4.5 实现 resolved 仅在已请求且 result Sync 后发布，以及 host detach/stop/shutdown 收口；故障注入验证 Sync 失败不报成功、timeout 后仍有唯一 owner/done、旧响应不能影响下一 turn，现有清理和 lease 顺序不变。

## 5. Session 当前格式与恢复

- [x] 5.1 扩展 ready/result 当前 payload、typed draft/strict decoder 与关联检查，答案正文只保留唯一冻结 preview；验证无新版本/optional record、无 UI 草稿/临时请求 ID、无 diagnostic/Catalog 正文副本及所有错误字段为安全英文。
- [x] 5.2 扩展 ReplayPlanner 的非审批独占 grammar 和恢复补偿，验证 Question ready/started/result/output 四边界、非法 approval、越过屏障、伪造答案关联、重复 result 与 terminal placement 全部失败关闭。
- [x] 5.3 提交两家不可变 `testdata/current` 问卷与混合调用 fixture，包含 ready/started/result 尾部、跳过、不可用和 checksum 合法语义错误；生产 loader/replay 读取固定 bytes 验证，测试不得用当前 encoder 生成输入 fixture。
- [x] 5.4 增加双 Provider uninterrupted/restored 等价回归，比较完整 canonical bytes、native item 顺序、opaque bytes、footprint/revision 和 fingerprints；覆盖 result/output 缺口、多次压缩后恢复与零终端/工具/网络调用。
- [x] 5.5 复用真实子进程 lease harness 覆盖等待问卷期间竞争、占用禁止 repair、进程崩溃释放、清理阻塞保留 lease 与续写 seq；验证全部拒绝路径 journal bytes 不变，不用 sleep 证明竞态。

## 6. 宿主装配与 TUI

- [x] 6.1 在 app/ChatSession 装配同一真实提问 executor 和 typed 回答适配器；验证 TUI 可回答、--print/--json/stream-json 返回配对 user_input_unavailable，零 stdin 等待、零问卷事件，原格式和退出码不变。
- [x] 6.2 实现 choosing/editing/reviewing/submitting 问卷状态机，复用独立 composer 编辑原语；验证 1–4 题、2–4 选项、单选/复选、说明/其他、逐题草稿、明确跳过和复核后一次提交，无默认代答。
- [x] 6.3 接入 modal 输入优先级、Esc/Ctrl+C/Tab/Shift+Tab、显式 paste 与 submitting 去重；验证聊天草稿/光标/历史/浏览锚点保持，迟到事件不能关闭新问题，提交拒绝保留原问卷。
- [x] 6.4 实现安全可滚动布局与提交结果反馈，补充 80x24、34x12、12x6、无色和 resize snapshots；覆盖中文/emoji/控制字符、长题目/选项/说明、复核动作始终可达，View 无 I/O 或隐式提交。
- [x] 6.5 为主线和 Ctrl+O typed history/detail 增加 Question 活动及完整分页回看；验证分组边界、恢复后同一答案、只读旧问卷、最多四页共享缓存和 64 KiB 页预算，不新增无界历史副本。

## 7. 跨模块验收与缓存回归

- [x] 7.1 用本地假双 Provider 跑“Read -> 提问 -> 回答 -> 新 sample -> Patch/Write -> Exec -> 总结”完整集成场景，分别覆盖同意选项、其他文字、显式跳过、取消和无交互；验证权限仍独立、零重复执行和原生 pairing。
- [x] 7.2 更新 ContextPlanner 的九能力 golden/cache 来源快照及失效矩阵；验证新增 schema 仅产生可解释 tool_catalog 失效，动态答案/ID/尺寸不污染稳定前缀，恢复后的 HTTP/SSE 与 WebSocket 完整请求等价，incremental wire 单独匹配 golden。
- [x] 7.3 增加确定性真实 PTY smoke 脚本，使用本地假服务验证作答继续、停止、窄屏/resize、无色、退出 0/130 与终端恢复；记录平台和脚本输出，不使用真实 API key 或外网。
- [x] 7.4 执行 `make verify` 与 `openspec validate add-structured-user-questions --strict`，验证架构守卫、全量测试及 race 全通过；故障修正只补真实业务回归，不跳过或弱化已有测试。

## 8. 文档与交付

- [x] 8.1 在 README、总体架构、Tool 架构、Roadmap 与 pitfall 同步实际能力、平台范围、独占/审批区别、问题取消/恢复和无交互限制；逐条检查引用路径、九能力目录与实际 record/event vocabulary，无延期能力冒充已实现。
- [x] 8.2 生成本 change 的实际 verification.md，映射关键 spec 场景到测试路径/命令/结果，区分基线与实现验收；列明不能执行的检查和风险，并核对无 secret、dead code、无用依赖或新增兼容 reader。
- [x] 8.3 验收完成后通过 OpenSpec 同步本 change 的 main specs 并复核，再按 archive 流程归档；验证工具输出、归档路径和文档引用一致，不在实现前勾选验收或归档任务。
