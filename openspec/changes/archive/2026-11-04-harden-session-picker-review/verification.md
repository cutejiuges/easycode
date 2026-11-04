# 会话选择器复核报告

## 发现与修复

| 优先级 | 问题及后果 | 修复与直接证据 |
|---|---|---|
| P1 | 父取消已完成、worker 尚未 Stop 时，Accept 仍接受恢复资源；锁内还调用了 context 的 Err | `picker_owner.go` 使用 done 信号作为接收/admission 检查，锁内无外部回调；`TestPickerOwnerRejectsCancelledHandoff` 在无 worker 调度的情况下复现窗口，结合原有唯一关闭/迟到真实恢复/父取消等待测试 |
| P2 | 安全展示预算恰好填满或剩余空间不足时，后续正文被静默省略；UTF-8 剩余不足时生成空消息 | `previewBody` 回收尾部空间保留省略标记，消息级截断可见；`boundSessionPreview` 不产生空条目并复制截断字符串；精确预算 0/1/18 bytes 余量与 emoji 回归 |
| P2 | 初次加载失败冒充空列表，错误在短屏被裁掉；陈旧列表未注明实际范围 | 区分 loaded 状态、错误优先、保存 entriesAll；`TestPickerReviewLoadingAndFailureViews` |
| P2 | 正文换行/Tab 丢失，元数据 Unicode 行/段分隔符未转义 | 共享转义骨架按正文布局放行换行/Tab，转义 Zl/Zp；`TestPickerReviewPreviewDisplayBoundaries` 与更新的 preview golden |
| P2 | 短屏确认无法查看完整目录/当前选项，短屏预览始终被头部占据 | 确认目录可滚动且固定选择项，预览允许滚过头部；两个 tiny/confirmation 直接回归 |
| P2 | 搜索超长被静默拒绝，未显示规格要求的上限提示 | 独立输入提示不覆盖加载失败；`TestPickerExcessiveSearchShowsLimitAndPreservesSelection` |
| P2 | 初次扫描完成但关闭边界取消时，discovery 仍标记初始化成功 | 关闭完成后检查取消；`TestPickerDiscoveryCancellationAtCleanupDoesNotInitialize` |

前四个基础回归在修复前运行失败，修复后通过。没有删除、跳过或弱化既有测试；preview golden 仅把正文中的字面 `\u000a` 改为规格要求的换行。

## 规格覆盖补强

- **Change the journal after preview**：新增 `TestPickerResumeReadsJournalAppendedAfterPreview`，使用另一真实 writer 追加已 Sync 的合法输入，再通过 picker executor 恢复；直接断言新 turn、InputID、正文及旧预览未改变。此前 removed/corrupt/mismatch 只能证明拒绝变化，不能代替这条场景。
- **Cancel while native validation is in progress**：新增双 Provider `TestPreviewCancellationDuringNativeValidationReleasesLease`，在 reader 逐条 Err 检查处用 barrier 阻塞，取消后仍持有 lease 直到释放 barrier；随后无正文、lease 可重获、journal bytes 不变。无 sleep 或概率调度。
- **无变更失败与修复后失败**：`TestPickerRecoveryReloadsFactsAndClassifiesMutation` 将原来误称 clean_failure 的输入尾补偿场景更名为 compensation_failure；真正 clean_failure 使用完整 root fixture，断言可重试且 bytes 不变。repair/compensation 分支增加完整日志可加载、无悬挂尾部、保留原始前缀、确有补偿追加和 lease 释放断言。
- 既有真实子进程竞争/崩溃释放、修复禁止、seq 单调、两家 Provider request/stream golden、cache fingerprint/opaque 等价和 shutdown 故障注入继续保留；离线 reader 与正式恢复共享严格解码，不将共享实现间相等误称为独立 golden 证据。

## AGENTS 核对

- 仅修改 app/TUI 私有实现和测试；无新增跨包接口、Provider capability、Session record vocabulary、版本标记或兼容分支，无新增依赖。
- owner 保持一个 worker、一个待处理槽、一个清理完成信号；不增加 watcher/helper goroutine；取消/Close/等待在锁外。
- Repository lease、writer Sync、artifact/Provider/查询 owner 沿用已有生命周期；预览保持只读，正式恢复重新获得连续 lease 并验证原生事实。
- 构造器纯内存、render 无 I/O；正文不进入 Catalog/日志/请求；生产无新全局变量，中文注释与英文错误。
- pre-commit 仍执行 make verify；架构、Roadmap、pitfall 已同步。原规格保持不变，本修复使用 skip_specs。

## 验证记录

- 定向 app/TUI 回归通过。
- macOS 实际 PTY 四项通过：搜索/预览/跨目录默认返回/resize、Esc 退出 0、NO_COLOR Ctrl+C 130、恢复到 Chat 再退出 0；终端属性恢复，忽略既有脚本定义的内核 PENDIN 状态位。
- OpenSpec 主规格严格校验：44 passed / 0 failed；本 change 严格校验通过。
- 完整 `make verify` 退出 0：分支规则、gofmt、go vet、Staticcheck、包含架构边界的全量测试与全量 race 测试全部通过。日志：`/tmp/easycode-picker-review-verify.log`；PTY：`/tmp/easycode-picker-review-pty.json`；规格：`/tmp/easycode-picker-review-specs.log`。

验证不使用真实凭据或外网。PTY 实测限于当前 Darwin 环境；没有声称验证其他操作系统或真实模型服务。
