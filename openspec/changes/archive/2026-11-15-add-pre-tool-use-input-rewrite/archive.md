# 归档记录

- 变更：`add-pre-tool-use-input-rewrite`
- Schema：`spec-driven`
- 归档日期：2026-11-15（Asia/Shanghai）
- 归档目录：`openspec/changes/archive/2026-11-15-add-pre-tool-use-input-rewrite`
- 任务与 artifacts：全部完成，49/49 项任务，无未完成警告。
- 同步策略：按用户“再次同步后归档”选择，重新读取有效 specs 规则快照；六份主规格与 14 条需求、88 个场景一致，幂等同步无须改写文件，再次逐份复核通过。

完整回执见 [receipt.json](evidence/archive/receipt.json)，同步与归档前检查见 [sync-verification.json](evidence/archive/sync-verification.json)。原 `.openspec.yaml`、验收日志、test2json gzip、CPU profile 及其他非 Markdown 附件保持原 bytes；生产代码、测试与脚本未因归档修改。

[verification.md](verification.md) 和 [再次回归与耗时排查](regression-2026-11-15.md) 中的链接及复现命令已迁至当前目录；ADR、能力清单及 Roadmap 指向归档后的资料。复现命令使用独立临时输出，避免覆盖已冻结证据。旧验收 JSON 中的执行路径、当时任务状态与 `archived: false` 是归档前的历史事实，原样保存，不代表当前归档状态。

主规格已经同步，归档后检查的实际命令与结果保存在 `evidence/archive/`。Linux/Windows 仍仅有构建与测试编译结论，Darwin 双 Provider 的完整回归、真实执行与 PTY 结论见原验收记录。

## 归档后验证

`make verify` 已通过（exit 0），包含默认正常全量与 race 测试，详见 [命令与退出码](evidence/archive/make-verify.json)、[完整日志](evidence/archive/make-verify.log)。归档后全库 strict 61/61、归档检查 41/41 均通过，详见 [validation.json](evidence/archive/validation.json)。15 份相关 Markdown 链接有效；967 份源码、测试及脚本、六份主规格、61 份原始非 Markdown 附件及原 metadata 字节均复核一致。

## CI 后续验证

首次 Ubuntu CI 的平台构建范围问题及修复、跨目标静态分析和 Windows 既有检查边界见 [ci-followup-2026-11-15.md](ci-followup-2026-11-15.md)。该记录补充原验收事实，原始证据未改写。
