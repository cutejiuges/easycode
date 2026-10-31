# 手动压缩验收记录

## 范围与当前契约

本变更实现 Claude 风格的 /context、/compact、定向取消、完整历史浏览和 headless 显式控制。完整 Provider-native history 保留；仅有效请求窗口切换为摘要加增量。Session/native/headless 分别使用唯一当前 7/3/1，旧开发 journal 不兼容，恢复前失败关闭。未增加依赖，两个参考工程保持只读。

## 场景与测试映射

| 任务/规格 | 验收证据 |
| --- | --- |
| 1.1、runtime/input-control、身份与作用域 | internal/domain/compaction_test.go；internal/protocol/context_test.go：非法身份、错 scope、严格解码、独立 CompactionID、复制隔离 |
| 1.2、provider/native-history-persistence | internal/provider/compaction_test.go：来源/predecessor、zero value、严格 codec、candidate 复制、并发单次 finalize/discard |
| 1.3、config/json-file | internal/config/compaction_test.go：默认 4096、范围/null/重复/溢出、小窗口与显式配置约束 |
| 1.4–1.5、context/planning、context/compaction | internal/context/compaction_test.go：请求预算边界与溢出、unknown、未配置、严格缩减、候选超限、归档正文不计量、profile/tools/project 稳定 segment 与重复查询 |
| 2.1–2.4、双 Provider 请求 | 双 Provider compaction_test.go 和固定 compaction_request.golden.json；普通 request golden 保留；摘要显式禁用工具并设置输出预算，HTTP 拒绝只调用一次 |
| 2.5、终态与 UTF-8 | 双 Provider compaction_stream_test.go：逐字节 UTF-8、refusal/incomplete/长度终止、断流、意外工具、空白、取消；compaction_test.go：256 KiB、非法 UTF-8、合法 Error 文本 |
| 2.6–2.7、原生恢复和 usage | 双 Provider compaction_test.go、steering_test.go；internal/app/compaction_fixture_test.go；internal/runtime/compaction_test.go：两次 checkpoint、来源校验、opaque 保留、observed/unavailable、失败不混入 turn |
| 3.0、session/catalog 与 session/resume | internal/session/repair_test.go、lease_test.go；internal/app/repair_order_test.go；catalog/list_integration_test.go：只读 Load、完整校验后 Repair、同 lease/大小/digest、重复/取消/变化/Sync 故障、Catalog 不修复 |
| 3.1–3.2、Session 语法 | internal/session/compaction_test.go：互斥、孤立/重叠/重复终态、单记录 batch、usage shape、started-only；已有工具 grammar 全量回归 |
| 3.3–3.4、不可变 fixture 和故障 | session/testdata/current/compaction_completed_{openai,anthropic}.jsonl；internal/app/compaction_fixture_test.go：固定 bytes 往返、旧 Session 6/native 2、checksum 合法语义损坏加半行保持 bytes、半完成行本地修复；repair/writer 故障测试与 runtime compaction Sync 注入 |
| 4.1–4.2、查询与 durable 顺序 | internal/runtime/compaction_test.go：查询不写 journal、不触发网络；预检拒绝；busy 与队列不能插队；started Sync 先于生成、completed Sync 先于 finalize；无隐式 turn |
| 4.3–4.5、取消、队列、关闭 | 同文件：120 秒期限以过去 deadline 确定性注入；过期取消、提交接收后取消仍 completed、失败 terminal Sync poison；FIFO/auto returned/strict-next；阻塞 cleanup 与超时 Stop 保留 owner |
| 5.1–5.2、恢复和 transcript | internal/app/compaction_fixture_test.go、compaction_e2e_test.go：恢复前全部校验、started-only 不联网收口、completed 不追加事实、独立 seq 边界与摘要范围查询；双 Provider transcript ordinal 回归 |
| 5.3、安全与 artifact | internal/app/compaction_security_test.go：旧对象缺失仍可压缩、伪造摘要不增加 artifact 授权或改变工具事实、当前项目指令重新发现；原审批/文件观察/目录 Catalog 测试保留 |
| 5.4、真实进程 lease | internal/session/compaction_process_test.go：子进程握手持有压缩 journal、竞争禁止 repair、强制终止释放、修复后单调 seq、原 prefix 不变 |
| 6.1–6.4、tui/context-commands | internal/tui/context_test.go；context/context_narrow/context_tiny/context_unknown/context_window 与 compacting/compact_completed/compact_failed/compact_resumed 固定 golden：精确分发、草稿光标、recall、数值和 unknown、无色/尺寸、过期事件、Esc 与 Ctrl+C；原 composer/队列/工具详情回归 |
| 7.1–7.2、headless/stream-control | internal/headless/context_test.go 与 testdata/compaction.golden.jsonl：单字节 UTF-8、strict decoder、response 顺序、独立身份、durable、无正文/opaque/TurnID |
| 7.3、8.2、真实宿主生命周期 | internal/app/compaction_headless_test.go：真实 Session、本地 HTTP、EOF drain、失败、取消、断管、协议错误、释放 lease、prefix 和 secret 隔离；原 StreamRunner 背压/取消测试 |
| 8.1–8.2、双 Provider 综合矩阵 | compaction_e2e_test.go 通过真实 TUI composer 提交 /compact，断言 uninterrupted/restored 下一请求 canonical bytes 一致；双 Provider steering_test.go 与 compaction_test.go 覆盖工具闭环、opaque、失败已提交输入、两次摘要、其后 steering、ordinal/revision/cache fingerprint |
| 8.3、文档 | README、总体架构、tool-system、Roadmap、pitfall、ADR-0007/0008 与 12 项主规格已同步；自动压缩、Hook、跨 Provider fork 仍未实现 |

## 架构审阅

- 查询快照由 worker 在提交边界发布；owner 不与 worker 并发读取多份可变 history。首次完整 race 检查暴露的快照读取竞态已据此修复，Runtime race 专项通过。
- Provider 私有 checkpoint codec 是请求窗口唯一恢复入口；SemanticHistoryView 不反向编译请求。生成 provenance 与窗口 summary 分开，完整 transcript 的原 ordinal 不变。
- New/getter/render 只操作内存；网络、文件、进程与 Sync 保留显式生命周期；新增包级变量仅接口断言。
- 复用 prepared resolution、transport/reducer、Session writer/lease、FIFO、transcript query 与单 stdout writer；没有新增通用管理器、兼容 reader、空策略、外部依赖或全局 registry。
- fixed fixture/golden 的一次性制作已结束，永久测试只读仓库 fixture，不在测试中重新生成事实源。
- 不写 Git commit，不改参考工程；本 change 于 2026-10-31 归档。

## 最终质量门

- make verify：通过（退出码 0）。包含分支规约测试、gofmt、go vet、Staticcheck、全量单测/集成测试、架构边界测试及全量 race。修复快照发布竞态后两次完整质量门通过；最终一次覆盖最终代码与固定 golden。
- openspec validate add-durable-manual-compaction --strict：通过。
- openspec validate --specs --strict：38 项通过、0 失败；仅保留既有长 requirement 的信息级提示。
- git diff --check：通过。
- 37/37 tasks 完成；12 项 delta 已同步主规格，change 已归档。
- 当前机器为 macOS/arm64；未连接真实模型服务，Provider 测试使用本地 HTTP/SSE。Linux/Windows 原生运行与真实服务兼容性未在本次验证，不据此声称已验收。
