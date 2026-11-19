# 聊天内会话恢复验收

2026-11-19，基线 `f6998f6`，功能分支 `feat/in-chat-session-resume`。

| 验收 | 结果 |
| --- | --- |
| Darwin arm64 `make verify` | 通过。包含 gofmt、go vet、Staticcheck、架构守卫、全量普通测试及正向清单中的关键 race。 |
| Darwin arm64 `python3 -B scripts/session-picker-smoke.py` | 7 个真实 PTY 场景通过。覆盖两种交互入口、窄屏预览与命令、恢复原草稿和光标、A → B → A、阻塞候选取消、重复按键和粘贴、SIGTERM、NO_COLOR 与终端模式恢复。 |
| Anthropic Messages 与 OpenAI Responses | 使用本地 mock 比较聊天内恢复和显式恢复的下一次规范请求、原生项目顺序、请求及缓存指纹，以及下一条 journal seq。目标历史含 opaque 推理、文件附件、任务调用与压缩 checkpoint。切换及回放阶段没有模型请求或旧工具执行。 |
| 租约与失败 | 真实子进程竞争、目录替换、完整 ID 直达、已提交修复或孤儿维护后的取消、目标 write/Sync 不确定、目标或源提前退出、源最终 Sync 超时及终端输出失败均通过回归。收口验证包括源和目标租约释放、目标接管前禁入及清理失败报告。 |
| 输入与设置 | 定向覆盖附件准备与空闲许可、迟到的控制和 UI 结果、接管后 Esc 与按键隔离、既有同名 Skill 分发。取消保留源 effort，成功挂载恢复目标任务与历史，临时设置从启动配置初始化。 |
| Linux amd64 | CLI 交叉构建通过。App、Runtime、Session、Artifact 与 TUI 测试二进制交叉编译通过。 |
| 规格 | 变更通过严格校验，5 份主规格已同步，全量主规格严格校验 65/65。无 Session、Catalog、Provider 或 headless wire 格式迁移。 |

实现证据见 [双 Provider 续写回归](../../../../internal/app/chat_resume_e2e_test.go)、[交接与目录回归](../../../../internal/app/picker_handoff_test.go)、[故障与取消回归](../../../../internal/app/in_chat_recovery_test.go)、[最终关闭和租约回归](../../../../internal/app/host_lease_test.go)、[Runtime 许可](../../../../internal/runtime/resume_permit_test.go) 和 [真实终端脚本](../../../../scripts/session-picker-smoke.py)。

没有 Linux 原生运行或 Windows 运行环境，Linux 上的实际终端交互和租约行为未执行本次原生验收。Provider 验收使用本地 mock，实际远端服务行为未验证。

## 全量回归复验

- macOS arm64 执行 `make verify TEST_OPTIONS='--race-scope=full --jobs=4 --heavy-jobs=2'` 通过。普通和全量 race 各覆盖 33 个 Go 包、534 个顶层入口，包含 5 个 fuzz 种子套件。归档前全量 OpenSpec 严格校验 68/68 通过。
- 现有脚本的 61 个真实 PTY 场景和 8 个 CLI 场景通过，覆盖会话选择、输入补全、文件引用、Markdown、问答、任务、推理、插件和各 Hook。Provider 交互使用本地 mock。
- 单独使用编译后的 CLI 与隔离数据目录启动两个进程，验证目标租约冲突时保留源会话与草稿，释放后重试可以继续目标历史，源历史未进入续写请求。两个进程退出后终端模式均已恢复。
- 仅在既有用例追加迟到取消和窄屏跨目录 ID 直达验证，复用原有故障注入、终端脚本和测试入口。

归档时逐条复核五份主规格，9 项新增要求与 5 项修改要求均已同步，20 项任务均已完成。原始回归日志保存在 Git 忽略的本地测试结果目录。

## 合入会话标题后的复验

合入 `main` 的 `9db64da` 后，Darwin 会话选择器的 7 个场景和会话标题的双 Provider 场景通过。另用实际 CLI 在 Anthropic 和 OpenAI 上分别验证重命名、按含空格的标题片段搜索、只读预览、按完整 ID 切回已改名会话和离线标题持久性。切换未触发模型请求，退出后终端模式已恢复。合并后的 OpenSpec 严格校验 68/68 通过。[会话标题脚本](../../../../scripts/session-titles-smoke.py)
