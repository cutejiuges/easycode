## Context

见 proposal.md。现有单 worker 持有资源，Accept 只检查 generation/标志，未检查父取消。预览原文与安全展示有两层预算；UI 初次加载尚无真实列表。

## Goals / Non-Goals

修复已有契约，增加确定性回归；不引入后台取消 watcher、公共协议、兼容版本或第二套恢复路径。

## Decisions

- owner 只持有取消 done channel，在接收与 admission 线性化点非阻塞检查；cancel/close 均在锁外。已有 worker 仍负责所有资源释放。
- 预算溢出统一回收末尾空间保留显式标记，UTF-8 边界裁剪；原文零剩余完整 rune 时不创建空消息。截断字符串复制以免保留大 backing storage。
- 元数据和正文共享受控转义骨架，正文仅放行换行与 Tab，元数据额外转义 Unicode 行/段分隔符。逐消息截断可见。
- 加载状态与空结果分离；短屏错误优先展示；陈旧列表记录成功加载范围。确认正文可滚动且保留选择项。
- 测试通过已取消 context、精确预算、真实 writer 追加和失败注入证明边界，不依赖 sleep。

## Risks / Trade-offs

- 并发取消与 Accept 同时发生 → 非阻塞 done 检查为接收线性化点，之后取消由唯一 owner 清理。
- 极窄终端无法同时显示全部文字 → 换行滚动、保留确认选项，测试固定尺寸和真实 PTY。
- 全量预览仍需验证完整历史 → 只限制投影副本，不削弱源数据验证。
