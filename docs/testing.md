# 测试维护与归档精简

按 2026-11-18 用户要求直接精简，不新建或变更提案。测试以关键业务、真实故障和所有权边界为单位维护；不以穷举所有值、getter、格式及重复功能矩阵为目标。

当前普通质量门执行精简后现存的全部 Go Test/Example/Fuzz seed，以及 Python 入口自检。默认 race 使用 scripts/race_policy.json 正向清单，只编译涉及的包，聚焦真实并发、stream 取消、提交、writer/lease 及关闭恢复；手动 make test-race-full 仍可执行全部现存入口。关键清单中的重复、失效名称或空平台计划会失败。

OpenSpec 归档只保留 Markdown 方案/规格/决策/简短验收及 .openspec.yaml 元数据。原始 JSON/JSONL、日志、profile、源码快照与运行清单移出版本库，今后写到临时目录、忽略的 .test-results 或 CI artifact。当前测试实际使用的固定 fixture 与 request golden 继续属于测试输入。

此次精简接受覆盖范围缩小，不声称与旧穷举集合等覆盖。产品行为未变，生产代码仅删除无消费者的辅助函数并补齐已有平台编译约束；已确认关键缺陷回归、双 Provider 原生恢复与缓存、真实子进程 lease/取消/关闭，以及文件引用交互继续验收。归档文档记载当时的验证范围；已删除的历史矩阵不再代表当前质量门。

本轮对同一分支基线 `43dd89a8ec2f1bab3a2adab7acf30771c45e1ac0` 的全部受版本管理 Go `_test.go` 文件统计，582 个文件、95,673 行精简为 325 个文件、41,319 行，净删除 54,354 行（56.81%）。统计未通过压行、搬文件或跳过入口改变口径；Python 调度器测试另保留关键执行/取消/清单回归。

删除 493 个 OpenSpec 原始产物，合计 17,998,265 bytes；另删除 229 个无现存消费者的测试 fixture，合计 117,108 bytes。保留当前真实使用的 request golden、native/current JSONL 和必要 OpenSpec YAML 元数据；已清理指向删除产物的 Markdown 链接。删除降低当前工作树大小，不改写 Git 历史或回收历史 blob。

2026-11-18 在 Darwin arm64、Go 1.24.13 上验证：

- 主体精简完成时执行 `make verify TEST_OPTIONS='--output=/tmp/easycode-prune-verify-complete'` 退出 0，合计 67.400 秒；普通阶段 34.713 秒（含静态检查），关键 race 阶段 32.553 秒。普通 449 个顶层入口、race 54 个入口完成清单均与选择集合一致；没有测试入口 skip，仅无测试文件的包报告 skip。包含 gofmt、vet、Staticcheck、架构边界、分支策略及 24 项 Python 自检，未命中测试结果缓存；该轮验后源码指纹与报告一致。
- `openspec validate --all --strict`：65 项通过，0 项失败。
- `scripts/file-mentions-smoke.py`：双 Provider、彩色/无色共 4 个真实 PTY 场景通过。
- `scripts/effort-smoke.py`：双 Provider、彩色/无色与取消共 8 个真实 PTY 场景通过。
- `scripts/completion-smoke.py`：彩色/无色共 2 个真实 PTY 场景通过，含审批拒绝、缩放及终端恢复。

默认关键清单按平台为 Darwin 54、Linux 53、Windows 45 个入口；本轮只在 Darwin 原生运行质量门与 PTY。非 Darwin builtin 进行了 Linux/Windows 编译检查，不宣称原生运行通过；手动全量 race 本轮未运行。完整本地输出置于 `/tmp/easycode-prune-*`，不重新加入版本库。

2026-11-18 PR 交付补充：重新执行 `make verify` 通过，449 个普通入口与 54 个关键 race 入口完整执行，耗时 67.307 秒。随后核查删除最后一份无消费者的 detached-command 测试夹具，并将 CI 手动 profile 的失效默认名称改为当前插件生命周期与文件恢复入口；提交及推送继续由既有 hooks 执行同一质量门。

此次端到端验证执行全部 12 个现有脚本，62 个场景均退出 0：文件引用 4、effort 8、补全 2、会话选择 4、Markdown 2、用户问答 4、推理展示 8、推理配置 8、插件 12、三种工具 Hook 6、输入改写 2、Stop 2。覆盖双 Provider、本地 CLI/真实 PTY、恢复/取消、冻结与缓存、审批及终端状态恢复；插件另完成 16 次离线续写/恢复。全部使用本地模拟服务和 fixture key，未调用真实模型服务。原始输出仅保存在 `/tmp/easycode-pr-e2e-*`，OpenSpec 严格校验另有 65 项通过。
