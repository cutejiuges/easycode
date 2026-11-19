## 1. 冻结配置和强类型检查边界

- [x] 1.1 在共享 Hook 声明、来源规范化和 matcher 中接入 `PreCompact`，用现有配置测试验证 `manual` / `auto` / `*`、固定顺序、共享上限、跨事件 ID 冲突及无匹配行为。
- [x] 1.2 增加带真实 CompactionID 和 scope 的 Hook 目标与严格 stdin，验证手动无普通 turn 身份、自动含真实 turn/sample，以及各项均收到同一原始重点且不含待提交输入、反馈、历史或凭据。
- [x] 1.3 扩展同一 runner 的事件解析与安全执行，验证空输出、显式阻断跳过后缀、exit 2、拒绝 stopReason、未授权重点、重复字段、跨事件输出隔离、单项 4 KiB 及执行限额。

## 2. 压缩 durable 事实和恢复

- [x] 2.1 实现 `compaction_started.pre_compact` 必需联合与三类专属记录的 typed constructor、strict decoder 和 validator，替换受影响的 `testdata/current` 固定字节并验证原有两家 Provider 当前 fixture 可回放。
- [x] 2.2 在现有 compaction replay owner 验证计划、规范 matcher 与 trigger、连续 ordinal、披露总字节数和终态配对。用伪造 exit 2 阻断、超限仍通过和非法多记录 batch 尾半行验证 checksum 合法也在 repair 前拒绝。
- [x] 2.3 复用当前 lease 和 interrupted compaction 收口，验证计划、执行准入或已通过决定后退出均只追加必要 terminal，不重新评审、不重跑脚本、不请求摘要，待交付 Stop 反馈在收口后原字节补齐一次。

## 3. 运行时接入和失败边界

- [x] 3.1 在手动和自动共用的压缩 worker 接入门禁。验证基础预检失败为零压缩/检查事实、零进程和零摘要，合法无匹配继续原摘要请求，非法来源、后续声明拒绝及非交互匹配均在任一 runner 启动前确定收口。
- [x] 3.2 在 Runtime owner 复用信任等待和进程清理，按真实 scope 路由审查、迟到回复和定向取消。以阻塞注入验证多条活动事件不提前结束 auto 压缩，started Sync 前失败能准确收口，查询、排队和 shutdown 保持清理顺序。
- [x] 3.3 追加结构化重点并以同一冻结 source、effort、任务与 scope 重编译最终请求，验证 16 KiB 整体限制、窗口或编码预算失败时零摘要调用，以及通过决定的 Sync 先于网络动作。
- [x] 3.4 接入失败码和自动回退，验证确定失败只计一次、信任 reset 不清除抑制、旧窗口预算通过则继续且超限则返还未保存输入。以 Sync 阻塞注入验证摘要准入前取消为零请求、完成 checkpoint Sync 后仍保留新窗口，以及 Sync 或 cleanup 不确定时关闭接纳。

## 4. TUI 和非交互入口

- [x] 4.1 复用 `/hooks` 和信任 overlay，验证脚本读取原手动重点和 `feedback:model` 两项披露、窄屏、默认拒绝及按键隔离。真实 PTY 验证手动取消在无 trust resolved 时仍清除 modal，迟到回执不影响草稿和下一次输入，自动取消定向活动 turn。
- [x] 4.2 渲染准备、审查、检查、实际摘要、失败及清理等待，根据 CompactionID 隔离。以 PTY 验证网络准入前不显示摘要阶段，拒绝路径没有摘要阶段，窗口安装后才显示成功，输出无检查正文。
- [x] 4.3 将五个新安全码接入既有 `compaction.failed`，验证 stream-json v1 在 auto started 尚未 durable 时的唯一失败、手动和自动相关标识、计量和顺序，以及 `--print`、`--json` 不出现新的 Hook 或压缩控制帧。

## 5. 双 Provider 端到端与交付

- [x] 5.1 复用两家摘要请求、任务 checkpoint 和 cache 测试，验证未配置与不匹配时的 canonical 请求和 fingerprint 等价，匹配追加只改变摘要请求重点及对应证据，保留稳定前缀、opaque、禁工具和 item 顺序。扩充代表性 OpenAI WS 场景，验证实际完整摘要载荷、握手回退复用同一检查决定、阻断时零摘要连接。
- [x] 5.2 通过 loopback Provider 验证重点顺序、auto pending/Stop 字节隔离、禁披露和服务端拒绝后零二次摘要或检查。比较未中断与恢复后续写的 canonical bytes、item 顺序和 fingerprint，观察恢复阶段零旧进程和旧摘要请求。
- [x] 5.3 执行 `make verify`、OpenSpec 严格检查与可运行平台的 PTY/真实进程验收。复用现有 race policy 场景，只有现有项无法覆盖独立所有权风险时才调整，并记录实测平台、命令及限制。
- [x] 5.4 根据经验证的行为同步主规格、README、能力清单、Roadmap、总体架构和踩坑记录。逐项核对新 record 词汇、测试文件路径、P4/P6 未交付边界及简短验收记录，准备按仓库流程归档。
