## 1. 强类型契约与纯规划

- [x] 1.1 在 `internal/domain` 和 `internal/protocol` 增加 CompactionID、上下文查询/压缩/定向取消命令及事件作用域；提供 typed constructor、strict decoder、validator 和复制隔离，通过非法身份、错误 scope、未知字段与普通 turn 契约回归测试。
- [x] 1.2 在 `internal/provider` 定义有效窗口只读视图、来源身份、checkpoint envelope、独立压缩结果及 prepared lifecycle；复用已有私有 resolution 骨架，通过零值、重复 finalize/discard、来源错配和 getter 深拷贝测试，禁止新增通用 Operation 或空策略。
- [x] 1.3 在 `internal/config` 增加 JSON-only `compaction_output_tokens` 及默认/显式来源判定；通过默认 4096、范围、null/重复/溢出、显式窗口冲突和小窗口普通启动回归测试。
- [x] 1.4 在 `internal/context` 实现摘要请求预检和候选缩减判定，ContextPlanner 使用有效窗口视图及 context revision；通过精确预算边界、unknown、无窗口、溢出、无缩减和归档正文不计量测试。
- [x] 1.5 更新 context cache regression，确保 summary/text canonical 内容不含操作身份，稳定 profile/tools/project 前缀不因压缩改变；用固定输入断言 segment fingerprint、source revision 和重复查询零变更。

## 2. Provider 有效窗口与摘要生成

- [x] 2.1 为 OpenAI 实现完整原生记录与有效窗口分离，普通 request/prepared input 从有效窗口编译；用无 checkpoint 普通请求 golden 不变、摘要后增量顺序、完整历史 ordinal 不变测试验收。
- [x] 2.2 为 Anthropic 实现相同窗口生命周期，保持 thinking/signature/redacted thinking 原始记录；通过普通请求 golden、tool-use 配对、独立摘要投影与复制隔离测试。
- [x] 2.3 实现原创版本化摘要指示及 OpenAI 独立摘要请求，复用 transport/reducer，发送 `tool_choice: "none"` 和实际 `max_output_tokens`；以请求 golden 和工具调用零执行测试验证，普通输出策略保持不变。
- [x] 2.4 实现 Anthropic 独立摘要请求，发送 `tool_choice: {"type":"none"}` 和实际 `max_tokens`；通过 request golden、工具目录/项目指令保留及供应端拒绝时无降级重试测试。
- [x] 2.5 为双 Provider 实现强类型摘要终态验证及 256 KiB 限额；通过 UTF-8 分片、空白、refusal、incomplete、长度终止、断流、意外工具调用和含 Error 的合法正文测试，失败不产生可安装候选。
- [x] 2.6 实现双 Provider checkpoint 编码与事务式 restore，当前 native canary 更新为 3；通过来源/前驱/digest、摘要与原生包装一致性、未知 canary、完整历史损坏、连续两次 checkpoint 的严格解码测试。
- [x] 2.7 实现独立压缩 usage 及失败计量 provenance；验证可信终态后候选被拒仍保留 observed usage，无可信终态为 unavailable，raw usage 不泄漏至公共事件且不混入普通 turn 累计。

## 3. Session 事实与恢复语法

- [x] 3.0 将 Loader 改为只读加载加显式 Repair，绑定 lease/handle/size/digest 与单次消费，app 在完整验证后修复，Catalog 不修复；以语义损坏/不兼容模型加残缺尾部 bytes 不变、有效修复 next seq、取消、跨 lease、重复消费、文件变化和 Truncate/Sync 故障测试验收，真实进程验证连续 lease。


- [x] 3.1 在 `internal/session` 增加三个 compaction record 的强类型 payload、registry、placement 和 codec，将当前 schema/payload 更新为 7；通过无 TurnID、必需 CompactionID、大小上限和未知 shape 失败关闭测试。
- [x] 3.2 扩展 replay 以验证 turn/compaction 互斥、单记录 start/terminal batch 和唯一终态；测试重叠、孤立 terminal、重复 terminal、非法 usage/checkpoint 及 started-only reconciliation，原 tool grammar 回归通过。
- [x] 3.3 更新 `testdata/current` 的固定字节 fixtures 与 canary 断言；测试旧 Session 6/native 2 在 repair/append 前拒绝且 journal bytes 不变，不新增旧格式 reader 或测试时动态生成事实源 fixture。
- [x] 3.4 为 compaction 追加故障注入测试，覆盖 started Sync、completion admission/Sync、失败终态 Sync、尾部半行/未完整 batch 和 checksum 合法的语义损坏；验证只有尾部物理损坏可修复，确定提交和 poison 结果明确。

## 4. Runtime 操作与取消所有权

- [x] 4.1 扩展 AgentLoop 的封闭活动状态和只读快照查询；测试 idle/turn/compacting 均可查询 committed 快照、纯预检拒绝零写入/零网络，以及 busy/队列非空不能插队。
- [x] 4.2 在 `internal/runtime/compaction.go` 实现 started Sync、独立摘要 worker、候选验证、completed Sync、finalize、terminal 顺序；验证成功前窗口不变、成功后仅一次安装、无 queued input 时不触发普通 turn。
- [x] 4.3 实现 expected CompactionID 定向取消、120 秒网络期限和唯一 cleanup owner；以可控时钟/阻塞 transport 测试过期取消、超时、取消与 writer admission/Sync 竞争，禁止 sleep 证明竞态。
- [x] 4.4 将压缩期间输入接入现有 FIFO/returned tray，成功清理后继续、失败/取消返还 auto 并保留 strict-next；测试摘要不包含 queued 输入、顺序不变、旧完成事件不影响后续操作。
- [x] 4.5 实现不确定 durability 的 poison 和关闭收敛；测试 accepted 但无 durable started 的唯一失败、提交成功后取消仍 completed、shutdown 超时不释放 lease，后台 owner 最终完成可等待。

## 5. App 恢复与完整历史

- [x] 5.1 扩展 app 会话装配与 resume，将普通 native commit/checkpoint 按 seq 恢复；测试先验证全部来源再补偿、started-only 本地收口、completion Sync 后崩溃恢复新窗口，网络/工具/文件重读次数为零。
- [x] 5.2 在 transcript projection 按 Session sequence 插入独立压缩边界及摘要详情；验证原 entry ordinal、工具 cursor、冻结 preview 和历史 recall 不变，恢复不重播实时状态。
- [x] 5.3 保持 artifact、permission grant 和文件观察边界；用摘要伪造批准/引用、旧 artifact 缺失及项目指令改变场景验证无越权、续写不依赖 artifact 内容，Catalog 可按最新 seq 重建。
- [x] 5.4 增加真实子进程压缩 lease/崩溃集成测试；验证清理期间竞争实例不能 repair、崩溃后释放、续写 seq 单调、失败路径 journal prefix 不变，并使用确定性握手。

## 6. Claude 风格 TUI

- [x] 6.1 在 composer submit 接入精确 `/context`、`/compact [instructions]` 本地分发；测试多行/参数边界、未知 slash 普通正文、拒绝保留草稿光标、接受只进入本地 recall，命令不写 user_inputs。
- [x] 6.2 实现上下文分类展示、estimated/unknown、窗口未配置与活动快照提示；通过固定尺寸、窄屏、极小尺寸、无色 snapshot 和 render 零 I/O 测试。
- [x] 6.3 实现简短压缩进度/成功边界及 Ctrl+O 摘要和旧历史详情；通过成功、失败、恢复及工具详情 snapshot 验证不刷普通 assistant delta、不自动续写、不丢旧条目。
- [x] 6.4 接入 Ctrl+C/详情 Esc/queued auto 取回/压缩取消优先级；通过已有 composer/队列键盘回归及过期事件测试，验证后台清理中仍能编辑且草稿不丢失。

## 7. Headless 显式控制

- [x] 7.1 为外部 v1 增加 `context.query/compact/cancel` strict decoder 和 typed response；覆盖分片/UTF-8、必需 instructions、null/未知/重复字段、超限、错误身份和旧命令 golden 不变。
- [x] 7.2 实现 snapshot/compaction 事件编码与 RequestID/CompactionID 关联；通过 golden 验证 response 先于事件、无 TurnID/input_id、无摘要/opaque 正文、failed durable 标记及单次唯一终态。
- [x] 7.3 将压缩接入 stdout 单 writer、EOF drain 和 shutdown；用背压、断管、协议失败及取消集成测试验证唯一 cleanup/lease 归属与既有退出码，未使用新命令及一次性模式的输出不变。

## 8. 综合验收与文档

- [x] 8.1 增加双 Provider uninterrupted/restored 等价矩阵，覆盖文本、工具闭环、失败已提交输入、opaque reasoning、两次压缩和后续 steering；逐字节断言下一 canonical request、native item 顺序、context revision 与 cache fingerprint。
- [x] 8.2 增加端到端手动压缩场景，串联 TUI/headless 控制、真实 Session 和本地模拟 Provider；验证完成后等待或仅处理显式排队输入、失败保留原窗口、摘要服务零工具副作用与 secret 不进入错误/日志。
- [x] 8.3 同步 README、`docs/architecture/overall-architecture.md`、Roadmap、pitfall 和长期架构 ADR，说明操作/窗口/恢复边界及默认配置；核对所有 record vocabulary、测试路径和能力声明，自动压缩/Hook 仍标未实现。
- [x] 8.4 执行 `openspec validate add-durable-manual-compaction --strict` 与 `make verify`，审阅依赖方向、全局状态、构造器副作用、dead code、全部 specs 场景映射；记录真实结果和未通过项，不在质量门未满足时归档。
