# 阈值自动压缩验收记录

## 环境与契约

- 日期：2026-11-01。
- 执行环境：Darwin arm64，Go 1.24.13；未在 Linux 或 Windows 上执行运行时验证。
- Session envelope 无版本字段或 canary；native/checkpoint 同样无内部格式版本，headless 保留独立外部协议。唯一 current fixture 已替换，未知字段、缺失字段与损坏直接拒绝；不提供版本门禁、兼容 reader 或迁移。
- 本地 HTTP/SSE mock 覆盖双 Provider，不使用真实 API key、不调用外部模型服务；实际模型的摘要质量与服务端容量仍取决于用户配置的服务。

## 功能与证据

| 范围 | 验证证据 |
|---|---|
| 配置与确定阈值 | internal/config/auto_compaction_test.go、internal/context/auto_compaction_test.go：默认/显式开关、无窗口 true 拒绝、严格布尔、重复字段、小窗口、unknown、阈值边界与整数极值 |
| 真实双 Provider 接续 | internal/app/auto_compaction_test.go：完整 HTTP/SSE、待提交正文不进入摘要、已提交纠正进入摘要请求、输入缩进保真、成功替窗、可恢复失败保留旧窗、重启恢复 |
| 普通与流式宿主 | 同文件验证 text/JSON 不输出压缩帧、stream-json 保留自动来源与事件边界、EOF 排空；internal/headless/context_test.go 验证原 submit 关联且无伪造 response |
| 工具安全点 | internal/runtime/auto_compaction_test.go：outputs durable 后才压缩、同 turn 下一 sample、一次摘要且工具不重跑；双 Provider compaction_test.go 覆盖工具/steering 窗口恢复、完整 transcript、native 顺序与 cache plan 等价 |
| 故障与取消 | internal/runtime/auto_compaction_test.go、compaction_test.go：无缩减、网络失败、跨 revision 连续三次熔断、手动重置、Sync 失败、成功 Sync 后取消保留新窗口、硬超限返还 queued auto、query/shutdown 和 cleanup 所有权；公共摘要骨架使用受控 deadline 验证 120 秒期限 |
| 双 Provider 摘要失败消费 | compaction_stream_test.go 的 manual/auto 两条路径均覆盖 UTF-8 分块、refusal/incomplete、工具调用、空白、断流与 HTTP 错误，不安装失败候选、不重试 |
| Session 与恢复 | internal/session/compaction_test.go 验证初始/工具边界、目标 sample/turn、pending 调用、已保存输入及重复摘要拒绝；internal/app/compaction_fixture_test.go 用固定 auto/manual bytes 验证完成、started、半行、来源/摘要损坏与非法附加版本字段及本地收口 |
| 跨进程所有权 | internal/session/compaction_process_test.go 对固定 auto/manual fixture 运行真实子进程 lease 竞争、占用时禁止 repair、崩溃释放、续写 seq 和前缀保持 |
| TUI | internal/tui/context_test.go 与 testdata/auto_compacting.golden：活动任务中的压缩状态、Ctrl+C 定向中断 turn、上下文状态/原因；既有窄屏、无色、历史/详情、队列与 composer 回归保持 |

## 质量门

- `openspec validate add-threshold-auto-compaction --strict`：通过。
- `openspec validate --specs --strict`：39 项通过，0 项失败；存在长 requirement 的信息提示。
- `git diff --check`：通过。
- `make verify`：全工程内部版本门禁清理后完整重跑通过，退出码 0；覆盖分支策略、gofmt、go vet、Staticcheck、架构守卫、全量测试与 race test。最终 race 中 app 为 166.177 秒、Session 为 116.197 秒，所有包通过。

## 边界与后续

- token 占用仍为版本化本地粗估；软阈值不保证服务端一定接受任意大窗口，摘要与普通请求分别预检。
- 自动调度状态仅进程内存在；resume 只本地收口，不自动联网或重跑工具。
- 不包含远程专用 compaction、自动删历史、Hooks、跨 Provider fork 或后台摘要，不据此宣布 P4 整体完成。
- 主 specs 已同步，change 已于 2026-11-01 经 archive 工作流归档，目录为 openspec/changes/archive/2026-11-01-add-threshold-auto-compaction。

## 用户修正：彻底删除 Session 版本机制

删除 EnvelopeVersion、Record/checksumEnvelope 的 SchemaVersion/PayloadVersion、writer/draft 的赋值和 decoder 相等性门禁。架构守卫拒绝重新引入 Version/Revision/Canary 信封字段或 Session 版本常量；回归验证带版本字段的记录按未知字段拒绝。唯一 current fixture 在测试外重新固定无版本 bytes 与 checksum，移除历史版本 fixture，保留非法附加字段的负向 fixture。AGENTS 明确规定只有用户明确指示后才能设计多版本兼容，即使未来发布也不能自行推定授权。

删除 Session 外层版本机制时的完整 make verify 与 OpenSpec 严格校验均通过；后续全工程审计见下。


## 全工程审计：移除剩余内部格式门禁

- 删除 native/checkpoint/Session.NativeCommitPayload 的 payload_version、版本常量、构造参数与 getter，以及恢复/usage provenance 的固定相等性门禁。
- 删除 CompactionSource 固定 prompt_revision；保留实际摘要与生成 provenance、family/wire/model、来源 digest、前驱和 committed/context 计数。恢复仍不重新生成或包装摘要。
- 删除 Catalog schemaVersion 与 PRAGMA user_version 设置/检查；保留完整性、实际字段和索引检查，以及 JSONL 派生重建。测试证明无关 user_version=99 不会清空现有索引行，错误表结构仍重建。
- 当前 fixture 在测试外直接替换并重新固定 checksum；负例改为非法附加版本字段。Provider restore/compaction 回归验证下一请求 canonical bytes、原生顺序与 fingerprint 等价。
- 防回流架构守卫覆盖 Session/native/checkpoint 格式字段、固定 prompt revision/版本常量和 Catalog user_version。
- 保留 headless 外部 v1、Anthropic API header、macOS sandbox 外部语法、应用版本与 UUIDv7；保留内容 revision、窗口计数、缓存 fingerprint/来源/renderer 标识与估算方法身份，它们不路由历史 decoder。resume 的 Provider/wire/model/目录匹配继续用于配置安全边界。
- 未发现生产历史 reader、迁移器、双写或按版本选择的兼容分支；opaque Provider 未知 item 继续无损保真，不作为内部多版本兼容清理。
- 本轮 `make verify` 完整通过（退出码 0），OpenSpec change 严格校验通过，39 项主规范严格校验全部通过，`git diff --check` 通过；任务进度 28/28。Darwin arm64 本地验证，未执行 Linux/Windows 运行时验证。


## 归档前一致性复核（2026-11-01）

- schema 为 spec-driven，proposal/specs/design/tasks 全部完成，28/28 任务已勾选。
- 逐 requirement 对比状态输出列出的六个 capability delta：architecture/single-current-contract、context/auto-compaction、headless/stream-control、provider/native-history-persistence、session/catalog、session/jsonl-store；主 specs 已完整包含对应描述与 scenarios，无待同步差异。
- 对照纯策略、Runtime 采样准备/失败计数、配置回归、Session scope 与实际 compaction_started/completed/failed 词汇、双 Provider codec、宿主事件和固定 fixture 核对现有架构及验收证据。
- 修正文档遗漏：删除 README/架构/Roadmap/pitfall 中残留的 Catalog v1 描述；Roadmap 的旧交付清单改为阈值自动压缩已交付；修正架构自动压缩小节编号。没有修改生产代码或测试行为。
- P4 仍未整体完成：远程 token count、真实 Provider cache 策略、Hooks、跨 Provider fork 与后台摘要未计为已交付。
- 沿用本次清理后的完整 make verify 通过记录，未因纯文档复核重复执行；归档时同步迁移验收链接并检查不存在失效引用。
