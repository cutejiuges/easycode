# 会话标题验收

验收日期为 2026-11-19。运行环境为 Darwin arm64、Go 1.24.13，Provider 使用本地模拟服务。

| 验收面 | 结果 |
| --- | --- |
| Provider 原生恢复 | OpenAI Responses 与 Anthropic Messages 各对比同一 thread 身份下无标题、改名后继续、改名后重启三条路径。含工具与 checkpoint 的固定前缀保持原字节，后续完整 HTTP/SSE 请求、原生 item 顺序及缓存指纹相等。OpenAI WebSocket 的增量与新连接完整恢复回归通过。两家 headless 对 `/rename` 文字按字面提交。 |
| JSONL 与资源所有权 | 固定当前记录覆盖最新标题、半行与完整非法尾行。活动 turn 或摘要中的标题、显式空身份与伪造批次均在 repair/补偿/联网前失败。故障注入覆盖交付后接纳前取消、追加 Sync 回执顺序与失败关闭、最终 Sync 等待。真实子进程验证宿主关闭超时后 lease 仍被持有，清理后可重开。 |
| 离线发现 | Catalog 验证缺列与错结构重建、已有 busy 行、观察快照与后续刷新。同名条目按完整 ID 区分，选中条目可在窄屏查看完整标题和正文。`--continue` 使用改名后的最近提交时间。 |
| 真实终端 | `python3 -B scripts/session-titles-smoke.py` 在两家 Provider 分别验证首次命名前的格式拒绝、行内保存、预填编辑、取消、离线列表、缩窄窗口、搜索、空正文预览、按原 ID 恢复和继续。两条流程均恢复终端模式，Anthropic 使用 `NO_COLOR=1`。 |
| 额外平台 | Linux amd64 全仓交叉构建与受影响 Go 测试包交叉编译通过。真实 PTY、文件系统和 Provider 测试在 Darwin 执行。 |

实现验收的 `make verify` 通过，用时 81.8 秒，覆盖格式、Go vet、Staticcheck、架构守卫、分支治理、全量普通测试及审计 race 清单。OpenSpec 变更和全部 66 份主规格通过严格校验。六份增量规格已同步，归档前核对了命令与记录词汇、README、总体架构、能力清单、路线图、踩坑记录和测试路径。

原始 PTY 与质量门输出保存在忽略目录 `.test-results/session-titles/`。

## 全仓回归与补充端到端验收

- Darwin arm64 的 `make verify` 通过，用时 84.3 秒，覆盖 33 个包的 537 个普通测试入口与默认清单的 60 个关键 race 入口。
- 两家的实际 CLI 连接本地模拟服务。验证首轮前命名、`stream-json --resume` 对 `/rename …` 的字面传递、外部 v1 事件与原生续写中的标题隔离、未知命名控制和错误模型配置的拒绝，以及 `--continue` 恢复原线程。删除配置和派生 SQLite 后，离线重建的列表逐字一致，journal 保持原字节。
- 真实终端的双 Provider 命名流程、既存选择器的四个流程，以及各一个补全和任务面板场景通过。用既有无标题 Anthropic journal 验证了预览、恢复、命名和原有 24 条记录的字节前缀不变。
- 回归复现了 PTY 驱动在答案出现后过早发送 Ctrl+C 的时序问题。脚本改为等待相同标题的成功回执再退出，并检查标题事实仍只有原来的两条。修改后两家流程通过。补充 CLI 与旧会话场景使用临时数据及现有验收设施。

本轮机器摘要和终端输出保存在忽略目录 `.test-results/session-titles-regression/`。
