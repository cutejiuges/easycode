# 会话分支验收

## 用户路径

Darwin arm64 上执行 `python3 -B scripts/session-picker-smoke.py`，使用 app 测试二进制在真实 PTY 完成 9 个场景。覆盖 Tab 完成命令、命名分支、从分支再次用 `/fork` 派生、返回源线程、用选择器进入子线程、窄屏 `Ctrl+G` 显示完整恢复命令及 `NO_COLOR`。用真实 artifact 锁暂停接管后取消，CLI 保留原草稿并显示已发布分支的确切 ID。SIGTERM 和正常退出均恢复终端设置。另用终端输出失败场景验证 stderr 诊断，随后凭完整 ID 通过 headless 恢复。 [PTY 验收](../../../../scripts/session-picker-smoke.py) · [CLI 回归](../../../../cmd/easycode/branch_test.go)

## 关键契约

- 双 Provider 本地 SSE 验证创建期间无模型请求，源文件在冻结后删除仍能续写。用相同分支 ID 比较即时启动与无源文件的冷启动，canonical 请求字节、原生 item 顺序、冻结输入、工具配对、opaque reasoning、上下文 footprint 和缓存 fingerprint 一致。覆盖压缩有效窗口、Anthropic TTL、从新传输启动的 OpenAI WebSocket 完整首请求及分支 key。分支从 Default 和启动 effort 开始，继承的任务可独立更新并再次压缩。父子改名独立，离线 Catalog 可重建，headless v1 可恢复分支。双 SSE 用例 1.87 秒，WebSocket 用例 0.32 秒。 [原生续写回归](../../../../internal/app/session_branch_e2e_test.go)
- 固定当前 JSONL 夹具覆盖三段血缘、原始前缀 SHA-256、祖先 artifact 证据、单条分支事实及末段修复。真实子进程占用源 lease 并被强制关闭，期间子线程仍可独立恢复。注入短写、文件与目录 Sync 失败、目标冲突和路径替换，验证源 bytes、已发布或不确定分类及连续 lease。 [固定夹具与租约回归](../../../../internal/session/branch_test.go) · [发布故障回归](../../../../internal/session/branch_publication_test.go)
- 实际命令验证源 artifact 的预览与保存状态可继承，原文查询仍取决于生成线程。分支自己的输出可读，源对象字节保持一致。复用恢复的运行时装配以及现有审批、Hook 信任和 Read 资格回归。Race 清单仅增加终端终止与已接纳发布结果交错的场景，实测 0.17 秒。 [输出授权回归](../../../../internal/app/session_branch_artifact_darwin_test.go) · [宿主交接回归](../../../../internal/app/picker_branch_test.go) · [Race 清单](../../../../scripts/race_policy.json)

## 归档前回归

- 用 `go build ./cmd/easycode` 构建的 CLI 在隔离 HOME、真实 PTY 和本地双 Provider SSE 上各完成两次派生和六次模型请求。空会话及不同工作目录拒绝创建。两次分支的原始物理前缀、SHA-256 和序号均匹配，创建期间请求数保持不变。源线程允许当前会话写入后，分支仍须独立批准并实际写入。锁住两份祖先日志后可按完整 ID 恢复叶线程。移走祖先并重建离线 Catalog 后，`--continue` 恢复叶线程，保留两次原生工具调用及其结果。
- 复用现有 PTY 入口回归会话选择器 9 项、双 Provider 会话命名和恢复 2 项、双 Provider Plan Mode 审阅至写入审批。另用既有分支、租约、Catalog 和终端失败用例进行定向 race 验证。 [会话命名](../../../../scripts/session-titles-smoke.py) · [Plan Mode](../../../../scripts/plan-mode-smoke.py)

- 编译后的 CLI 配合真实子进程 lease 验证叶线程的半行修复。目标被占用时返回 busy、原字节保持且没有模型调用。强制结束持有者后，离线列表识别父线程且保留半行。两代祖先均缺失时，显式恢复只截断叶线程的半行，保留 14 条既有记录的原始字节和两处血缘摘要，并以连续序号和原生推理记录续写。

## 质量门与执行范围

- Darwin arm64 的 `make verify` 通过，归档前复跑耗时 57.156 秒，覆盖全量普通测试、定向 race、gofmt、go vet、Staticcheck 和架构边界。
- `openspec validate add-session-branch --strict`、`openspec validate --specs --strict`、`openspec validate --all --strict` 通过。主规格 71 项，连同当前变更共 72 项。11 份增量已同步至主规格。
- Linux amd64 编译全部包及测试二进制。Windows amd64 交叉构建 CLI。运行验证在 Darwin 进行，Linux 原生排他 rename、Windows 的不支持分支路径及真实上游服务未在本轮运行。Provider 交互使用本地 HTTP/SSE 和 WebSocket 夹具。
