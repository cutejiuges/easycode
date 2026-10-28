## 1. 纯内存编辑与输入规则

- [x] 1.1 在 `internal/tui/composer.go`（编辑逻辑合并在同职责文件） 建立正文、字节光标和不可变快照，直接使用锁定版本 uniseg；通过 `composer_test.go` 验证快照不被后续编辑改写、无构造/render I/O，并确认 go.mod 无依赖升级。
- [x] 1.2 实现字素左右、前后删除、逻辑行首尾与组合字符插入后的光标归位；通过表驱动测试验证 CJK、组合重音、ZWJ emoji、空行、尾换行与边界无操作。
- [x] 1.3 在 `composer_input.go` 实现 Alt+Enter/Ctrl+J/反斜杠换行、CRLF/CR 规范化和空白提交判断；测试首行缩进、尾部空白保留，以及普通 Enter 与换行路径各自的提交次数。
- [x] 1.4 实现显式 paste 原子插入、空 paste 与 4 MiB 字节限额；用 `composer_input_test.go` 验证控制字符不触发动作、恰好上限接受、超限保持草稿/光标/浏览状态，且不写部分结果。

## 2. 安全显示和输入视口

- [x] 2.1 在 `composer_layout.go` 建立统一字素/显示列/视觉行映射、4 列 Tab 与控制字符可见转义；测试 ESC/OSC、C0/C1/DEL、双向控制、孤立零宽字符与宽字素，不允许正文被显示函数改写。
- [x] 2.2 实现最多 8 行输入视口、无色插入标记、软折行边界和 preferred column；通过长短行往返、光标首尾、单列与 resize 测试验证光标可见、无越界且正文/字节光标不变。
- [x] 2.3 让 View 消费已准备布局并将安全显示应用到用户 transcript；固定 snapshot 验证提交前后控制字符都不执行，重复 View 不重建布局或修改状态。
- [x] 2.4 添加近 4 MiB ASCII/CJK/长单行编辑与布局 benchmark，记录耗时及分配；验证光标移动复用布局、元数据没有逐字素 heap 对象或无限缓存。

## 3. 当前会话输入历史

- [x] 3.1 在 `composer_history.go` 实现最多 100 条/8 MiB、连续重复折叠和最旧优先淘汰；测试两种预算分别触发、单条上限与不影响已存历史的修改。
- [x] 3.2 实现视觉行优先的历史导航、原草稿/光标保存恢复与编辑召回后的脱离；用多行、软折行、纯空白草稿和最旧/最新边界用例验证行为。
- [x] 3.3 使用已提供的 SemanticHistoryView 初始化有界 recall；测试新会话为空、resume 仅使用本 thread 用户文本、超限/空白种子不入 recall，并通过架构守卫确认 TUI 不依赖 Session/Provider I/O。

## 4. Model 提交和焦点集成

- [x] 4.1 用 composer 替换 `model.go` 的旧 draft 分支并删除旧单行路径；路由测试验证审批 > 详情 > submitting/active > composer，普通键和 paste 不跨焦点修改或提交。
- [x] 4.2 增加 TUI 内部 submitting/pending snapshot 与专用提交失败消息，成功接收后才追加用户 transcript/recall；fake session 测试验证拒绝恢复、连续 Enter 单次提交及已接收后失败不自动恢复/重提。
- [x] 4.3 保留 pending interrupt、活动 turn 中断和 cleanup 后恢复输入；确定性 channel 屏障测试覆盖接收等待时取消、接收失败清除取消意图、approval 与命令详情中断且无后续 turn 误取消。
- [x] 4.4 更新并新增固定尺寸 snapshot，覆盖 80x24、32x10、极小窗口、多行/长行/无色光标、历史召回、提交等待、拒绝恢复、审批关闭和命令详情关闭后的状态恢复。

## 5. 纵向回归

- [x] 5.1 新增 `internal/tui/composer_terminal_test.go`，通过真实 Bubble Tea Program 与受控 reader 验证完整/分块 bracketed paste、Alt+Enter/Ctrl+J 和 paste 后独立 Enter；使用握手屏障而非 sleep，并验证 Program/reader 退出清理。仅对框架支持的 decoded-text 范围断言保真，记录 RuneError/未闭合 paste 的限制。
- [x] 5.2 新增 `internal/app/composer_e2e_test.go`，使用 mock Provider 分别执行两种 Provider 的多行提交，检查原生 user item 与包含缩进、Tab、尾换行的输入一致，并验证 TUI 接收失败无重复请求。
- [x] 5.3 补充双 Provider golden/cache 回归，固定同一正文在不同 UI 尺寸/光标路径下的 canonical bytes 和稳定前缀；沿用 resume 测试设施验证 uninterrupted/restored 下一请求、原生顺序与 fingerprint 等价，不用运行时 encoder 生成预期 fixture。
- [x] 5.4 运行既有 headless、审批、命令进度与 Session 恢复回归，验证 headless 外围空白原样进入模型（空白拒绝及输出格式不变），确认没有改变外部协议、活动期禁止提交、默认审批决定或副作用/清理边界。

- [x] 5.5 移除双 Provider 原生用户消息构造中的 TrimSpace，保留原输入验证；用 TUI、一次性 headless 和 stream-json 的正文回归验证首行缩进、尾空格/换行一致且空白输入零请求。

## 6. 文档和验收

- [x] 6.1 更新 README、总体架构、Roadmap 和 pitfall，说明按键、历史预算、正文保留、拒绝恢复和已解码粘贴边界；修正 Write 状态矛盾，核对文档路径及实际 record vocabulary，不宣称 P2/P3/P5 已退出。
- [x] 6.2 在可用真实终端手动验证中文/emoji、换行、粘贴、resize、取消和退出恢复，记录终端版本及已验证范围；不可用终端明确列为未验证，不以 mock 代替人工结果。
- [x] 6.3 执行 `openspec validate add-multiline-chat-composer --strict` 与 `make verify`，核对依赖方向、纯构造/render、无新 registry/goroutine/兼容分支，并记录结果或不可执行原因。
- [x] 6.4 按 delta spec 逐项检查交付、测试路径与文档一致性，形成可审阅验收说明；仅在实现验收完成后进入 specs 同步和归档流程。

验收证据、基准与真实终端未验证范围见 [verification.md](verification.md)。任务 6.2 按“不可用终端明确列为未验证”分支记录，不代表人工烟测通过。
