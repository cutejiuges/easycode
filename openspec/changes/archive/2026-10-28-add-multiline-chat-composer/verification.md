# 多行 composer 实现验收

## 选择依据与方案修订

当前 Read/Glob/Grep/Patch/Write/Darwin Exec 已形成可运行的 coding loop，输入却只能末尾编辑，直接阻碍代码片段、日志和长需求的使用。多行 composer 的消费者、输入协议和测试宿主均已存在，能改善现有能力的日常使用，不需要提前增加尚无消费者的内核接口。相比补全、slash command 或完整 transcript 重构，它的范围可独立验收；相比继续增加工具，它优先解决已有执行能力的输入瓶颈。此交付是 P5 的一个切片，不表示 P2/P3/P5 整体退出。

参考阅读的快照、文件与行为差异见 [design.md](design.md) 的证据表。交互采用 Claude 风格的换行键、视觉行优先导航和边界历史；保留 Codex 在字素、preferred column、布局缓存、草稿所有权及失败恢复上的优点。冲突处明确选择一种行为：历史导航采用 Claude 规则，正文保真优先于参考工程的 trim；没有拼接两套历史规则，也没有引入缺少平台支持的键盘承诺。

用户批准扩大正文保真修订后，双 Provider 仅移除 user item 构造时的 TrimSpace，复用已有合法性校验。TUI 规范化 CRLF/CR；headless 保留换行原值。此改变影响实际模型输入，但没有改变 Provider wire shape、RuntimeCommand/Event、Session record vocabulary 或 headless JSONL v1。

## 复用与职责

- composer 组合编辑、布局、输入意图和有界历史；没有通用 editor 包、registry、继承模拟或占位策略。
- 编辑都经过 `replace` 原子校验；复用 `protocol.MaxInputBytes`、锁定的 uniseg 和已有 ChatSession facade。
- 正文、不可变布局和写时复制历史支持值快照；Model 管理 admission 状态，composer 不处理 I/O 或 Runtime 生命周期。
- 同一显示映射驱动移动、折行和光标；用户 transcript 共享控制字符转义规则。显示副本不会回写正文。
- 纵向回归复用 app 资源装配、mock SSE、真实会话 owner、恢复设施和双 Provider 的现有 continuation golden/fingerprint 断言。

## 需求与证据

| 验收内容 | 测试证据 |
|---|---|
| Unicode 字素、逻辑/视觉移动、快照、历史预算与恢复 | `internal/tui/composer_test.go` |
| 换行、保真、原子 paste、恰好 4 MiB/超限拒绝 | `internal/tui/composer_input_test.go` |
| 安全显示、极小窗口、缓存复用、软折行 | `internal/tui/composer_layout_test.go` |
| admission 拒绝/接受、取消屏障、焦点与 resume 种子 | `internal/tui/composer_model_test.go` |
| 固定 80x24/32x10/极小、多行/长行/滚动/召回/等待/接受/拒绝快照；overlay 关闭恢复 | `internal/tui/testdata/composer_*.golden` 与上述 model 测试 |
| 完整及分块 bracketed paste、Alt+Enter/Ctrl+J、独立 Enter、Program/reader 清理 | `internal/tui/composer_terminal_test.go` |
| 双 Provider 原生文本、不同尺寸路径 canonical request 等价、resume、真实停止会话拒绝零新增请求 | `internal/app/composer_e2e_test.go` |
| text/json/stream-json 保留空白与原始 CRLF，空白输入零请求 | 同上 |
| 多行固定 golden、恢复后原生顺序、请求字节及 fingerprint 等价 | 两个 Provider 的 `restore_test.go` 与 `testdata/write_continuation.golden.json` |
| 原有审批、命令清理、headless、Session、架构约束 | 全量 `make verify` |

固定 fixture 已写入仓库；测试不通过当前 encoder 动态生成预期 golden。没有升级依赖；uniseg v0.4.7 仅提升为直接依赖。没有修改两个参考工程。

## 性能记录

2026-10-28，darwin/arm64，Apple M1 Pro；`go test ./internal/tui -run '^$' -bench Composer -benchtime=1x -benchmem`。单次采样用于记录上限输入成本，不作为统计分布或交互延迟保证。

| 近 4 MiB 样本 | 时间 | 分配字节 | 分配次数 |
|---|---:|---:|---:|
| ASCII 长行布局 | 84.2 ms | 72,619,072 | 272,389 |
| CJK 布局 | 137.4 ms | 64,859,456 | 183,990 |
| 多行布局 | 97.6 ms | 130,452,016 | 838,916 |
| 长行末尾插入 | 79.0 ms | 76,813,344 | 272,390 |

布局元数据采用连续值数组，没有每字素 heap 对象；视觉行 builder/切片扩容仍有分配，短行密集文本成本最高。普通光标移动和重复 View 复用布局；文本编辑与宽度变化重建布局。4 MiB 是正文上限，不是布局峰值内存上限，也不承诺大草稿逐键编辑低于一帧。

## 验证范围与限制

- OpenSpec 严格校验通过。
- 最终 `make verify` 通过：branch policy、gofmt、go vet、Staticcheck、架构守卫、全量单测/集成测试与全量 race 均通过（app race 69.166 s，TUI race 2.863 s）。`git diff --check` 通过。
- 未执行真实终端人工烟测：当前 stdin/stdout 均非 TTY，环境仅报告 TERM=xterm-256color、TERM_PROGRAM=kanban，无法取得真实终端版本或人工交互证据。中文/emoji 显示、物理组合键、拖动 resize 与退出恢复仍需真实终端人工验收。不能把受控 reader 测试称为人工烟测。
- Bubble Tea v1.3.10 在关闭 bracketed-paste 标记前累积原始字节，解码时可能跳过 RuneError；未闭合粘贴内存上限、字面 U+FFFD 保真与通用 Shift+Enter 不在本 change 保证范围。README、design 与 delta spec 保持一致。
- README、总体架构、Roadmap、pitfall 已同步；Write/命令执行状态矛盾已修正。2026-10-28 已按用户指令同步三份主 specs，并归档至 `openspec/changes/archive/2026-10-28-add-multiline-chat-composer/`；同步后全部增量要求逐项匹配，原有无关要求与场景保留。主规格严格校验 29/29 通过，归档质量门 `make verify` 通过。
