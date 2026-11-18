# 文件引用实现代码复审

日期：2026-11-17。分支：`feat/workspace-file-mentions`。复审依据为仓库 AGENTS.md、本 change 的 proposal/design/tasks/六份 delta、当前源码与固定 fixture。原 47 项任务保持完成；本轮修复原契约中的实现遗漏。

最终结论：三个生产缺陷与一处 PTY 验收同步问题均已修复。最终代码的完整 `make verify` 退出码 0，全量普通与 race 通过；14/14 PTY、4/4 回执单测及 64/64 OpenSpec 严格校验通过。没有遗留已确认而未修复的问题。

## 结论与修复

### P1：并发取消时缺少系统调用期间的描述符借用

`internal/tool/builtin/workspace_unix.go` 的目录发现由取消回调关闭句柄，而 `openChild` 原先用 `os.File.Fd()` 取得数字后执行 `openat`；`read_observation_unix.go` 的 `fstat` 有相同窗口。数字不能代表仍被持有的对象，并发 Close 可以使其失效或被其他打开复用，破坏 FD-bound 边界。

修复：工作区根和子目录的相对打开共用 `openWorkspaceRelative`，在 `SyscallConn.Control` 的借用内执行 `openat`；实际文件快照在同样的借用内执行 `fstat`。原关闭 owner、取消返回和最终清理顺序保持，系统调用未退出时仍保留责任。

回归：`internal/tool/builtin/workspace_unix_test.go` 的 `TestWorkspaceOpenChildPinsDirectoryUntilOpenatReturns` 用 `os.File` 已进入关闭门的可观测状态作屏障，验证已接纳的打开继续绑定原目录、关闭后禁止新打开。没有用 sleep 或概率 FD 复用证明正确性。现有 symlink/父目录替换、变化检查与取消读取栈测试继续覆盖来源读取。

### P2：Anthropic 缓存桥接遗漏 Stop 反馈消息位置

`internal/provider/anthropic/cache.go` 计算历史桥接时只累计普通用户输入、sample 与 tool outputs，而请求编译还会插入 `FeedbackInput`。两次 Stop 反馈、Skill+多文件纠正及长采样内容会使桥接标记从最后一个真实采样前文件块错移到更早的反馈块。

修复：按实际请求布局计入反馈消息，仍由现有缓存 overlay 在请求副本上放置标记。恢复测试 helper 同时识别当前 `hook_feedback`，未知 kind 明确失败，避免用空 RestoreRecord 掩盖遗漏。

回归：`internal/provider/anthropic/file_cache_bridge_test.go` 与 `testdata/files_stop_bridge_markers.golden.json` 固定两个反馈、两组 Skill+多文件和二十块采样窗口。十二个组合覆盖有/无项目前缀、普通采样/一次性摘要、disabled/5m/1h；分别核对实际 request marker 位置、TTL、四标记上限、摘要后缀不缓存、uninterrupted/restored canonical bytes、native 顺序/opaque、footprint、投影与内容 fingerprints。固定 golden 的预期位置直接写入文件，不在运行时从生产 encoder 生成。

### P2：读后检查内取消可能被归为文件变化

`internal/tool/builtin/workspace_file_content.go` 在读后快照调用前检查取消，但在该调用返回后直接判断属性/错误。如果取消恰好发生在快照检查内，原路径可能返回 `file_reference_changed`，检查本身成功时还可能返回可用正文，而没有保持这一准备边界的取消结果。

修复：读后检查返回后先核对 context，再处理文件变化。`TestWorkspaceFileSnapshotCancellationDuringFinalCheckRemainsCancellation` 确定性地在第二次快照内取消，分别注入成功和失败，断言两者均返回 `context.Canceled`、没有可用正文且保留实际读取计数。原先真实属性变化的拒绝语义保持。

### 验收修复：PTY 回执乱序与首次搜索边界

实际运行暴露 `scripts/file-mentions-smoke.py` 等待某一键盘回执时会丢弃先到异步通知；直接复用保留下来的旧 admission 又会误判后续输入。最初的整段粘贴还让首次 Tab 与后台结果到达顺序相关。

修复：`AckInbox` 按序保留未消费回执，动作水位约束新按键/admission/取回/resize，事件只消费一次；新视图等待忽略旧视图。先粘贴路径、再追加范围及尾部，明确保持 paste 抑制，然后显式 Tab 重开。生产 TUI 的接受规则保持。

回归：`scripts/file-mentions-smoke_test.py` 四项纯单测覆盖 probe/key 两种次序、连续终态单次消费、新按键与新 admission 的动作边界、旧视图拒绝。`internal/tui/file_completion_test.go` 新增分段粘贴范围、离开再返回目标、零提前搜索及一次显式重开的业务断言。

## 四条重点链路复核

| 链路 | 本轮核对与验收依据 |
| --- | --- |
| 双 Provider | 原文/Skill/文件块的角色、类型、次序和完整引用集合；mandatory files、严格 decoder、克隆隔离；两家 file_input/stop_hook_cache golden，OpenAI WS full/incremental 与失败 reset，Anthropic 新增长窗口桥接回归 |
| TUI | 光标路径区/行范围、引号 serializer、过期/ABA/index revision、旧行失效、paste/modal/极窄视口、两次独立 Enter、Ctrl+O；固定 snapshots、来源搜索与准备计数、真实 PTY 和新增粘贴断言 |
| 持久化恢复 | 固定 22 份 file current fixtures，语义拒绝早于 repair 且 journal bytes 不变；真实 input-only Sync 后崩溃、lease 占用禁止 repair、二次恢复、删除来源后的离线恢复；用户原文与 frozen content 分别保留 |
| 缓存与操作策略 | 实际包装只计一次，native footprint 覆盖；搜索/身份/digest 不污染稳定来源，checkpoint 不 rehydrate；effort 只在实际操作启动捕获，Tool/Stop/摘要保留同轮设置，恢复相同 wire 设置后比较请求 |

## 两个参考工程

全程只读，重新确认 HEAD 未变：

- Claude Code sourcemap：`a8a678cb6244e6770e1e421767ff0987a1d95549`。重新读取 `restored-src/src/hooks/fileSuggestions.ts` 的 applyFileSuggestion、`hooks/useTypeahead.tsx` 的光标前后 token，以及 `utils/attachments.ts` 的 processAtMentionedFiles/extractAtMentionedFiles/parseAtMentionedFileLines。继续采用正文光标目标、引号/范围及提交读取体验，维持本工程的独立补全/提交和 descriptor-bound 冻结。
- Codex：`c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee`。重新读取 `codex-rs/tui/src/file_search.rs`、`bottom_pane/chat_composer/completion_target.rs` 与 `bottom_pane/file_search_popup.rs`。核对 session token、路径目标、pending/display query、完整插入路径与展示截短；本工程继续核对完整代次并立即停用旧行。

## 验证记录

修复前通过临时 Go overlay 只还原本轮三个生产缺口，保留新回归测试，未回滚或改写用户工作区。关闭门用例以 `bad file descriptor` 失败，缓存用例在八个启用缓存组合中报告错误桥接位置，读后取消两种分支均被新增断言拒绝。修复后三个新回归在 race 模式重复三轮通过。发现最后一处取消问题后，停止旧的质量门并从最终代码重新执行，旧运行不作为完成证据。

| 检查 | 结果 |
| --- | --- |
| 修复前反证 | 预期退出 1，三个缺口均触发新增断言；[原始输出](evidence/review/before-fixes.txt) |
| 新增回归 `-race -count=3` | 通过；[原始输出](evidence/review/regressions-race.txt) |
| 回执单测 | 4/4 通过；[原始输出](evidence/review/smoke-unit.txt) |
| 文件引用真实 PTY | 4/4 通过，双 Provider × 普通/NO_COLOR，每组 3 次请求、4 次正文读取、5 次扫描，终端恢复；[JSON](evidence/review/file-smoke.json) |
| effort 与原有补全 PTY | 8/8、2/2 通过，全部终端恢复；[effort](evidence/review/effort-smoke.json)、[completion](evidence/review/completion-smoke.json) |
| OpenSpec 全部严格校验 | 64/64 通过，其中 62 specs、2 changes；[JSON](evidence/review/openspec-all.json) |
| Linux/Windows amd64 app 测试二进制 | 两项目标编译成功，只编译未原生执行；[JSON](evidence/review/platform-compilation.json) |
| 完整 `make verify` | 通过，退出码 0；branch policy、gofmt、vet、Staticcheck、架构、全量普通与 race。app 普通 151.245 秒、race 1055.594 秒；[完整日志](evidence/review/make-verify.txt)、[退出码](evidence/review/make-verify.exit) |

新回归显式使用 `-count=3` 执行 race；全量质量门按 Go 的输入校验复用部分未变更包的测试缓存，app 与 builtin 在最终代码上实际运行。CLI apply 状态为 all_done、47/47，见 [apply-final.json](evidence/review/apply-final.json)。

本轮实际运行平台为 Darwin/arm64，使用本地 HTTP/SSE/WebSocket 假服务；没有真实远端模型调用。Linux/amd64 与 Windows/amd64 只做测试二进制交叉编译，不声明原生运行验收。复审阶段未归档、提交或推送；随后按用户确认于 2026-11-17 归档，记录见 [archive.md](archive.md)。
