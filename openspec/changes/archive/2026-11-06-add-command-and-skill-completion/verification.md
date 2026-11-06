# 命令与 Skill 输入补全验收

后续追加验证发现并修复四项边界问题，新增确定性回归、状态机 fuzz、Unicode 请求/恢复和真实 PTY 场景。最新结论与日志见 [追加验证](revalidation.md)；以下保留首轮验收记录。

## 范围与平台

- 本次实现 `/` 内置命令/Skill 与 `$` Skill 首词补全、确定性筛选、范围替换、焦点隔离、粘贴抑制和有界布局；选择依据及两个固定参考快照见 [design.md](design.md)。
- 验收宿主为 Darwin arm64（macOS 27.0.1）。真实 PTY 验证普通配色及 NO_COLOR，缩放为 28×10、80×24；固定 snapshot 另外覆盖 20×5 和 1×1。
- 本次没有新增 Provider wire/capability、工具 schema、公共接口、RuntimeCommand/Event、Session/native/checkpoint 字段、版本标记、迁移器、依赖、goroutine 或后台 owner。
- 当前没有 Linux/Windows 原生 runner，未运行其原生 PTY；跨平台终端差异未经本次验证，沿用既有平台承诺。文件、后台进程、参数值、正文 mentions 和动态目录补全不在本次范围。

## 命令与结果

| 命令 | 结果与证据 |
| --- | --- |
| `go test ./internal/tui ./internal/architecture` | 已通过；包含原有 Skills/Hooks 草稿与审批/问卷隔离回归。 |
| `go test ./internal/tui ./internal/app -run Completion -count=1` | 已通过；[输出](verification/completion-tests.txt)。真实终端 helper 仅由显式 smoke 环境启用，普通运行不接管终端。 |
| `go test ./internal/tui -run '^$' -bench BenchmarkCompletion -benchtime=100x -benchmem` | 已通过；[基准记录](verification/bench.txt)。256 Skill 匹配约 3–4 μs/次；4 MiB 草稿参数区不随正文大小复制；无关更新保留匹配切片与布局。数值只代表该宿主，不作为跨平台时间硬门。 |
| `python3 scripts/completion-smoke.py` | 两种配色均通过；[PTY 记录](verification/pty.json)。每例两次模型请求：Skill 后 Write 审批、拒绝后总结；`/context` 和选择阶段零请求，目标文件未创建。 |
| `openspec validate add-command-and-skill-completion --strict` | 已通过；[校验输出](verification/openspec.txt)。 |
| `openspec validate --specs` | 51 项规格通过；[校验输出](verification/specs.txt)，四项修改要求保留原有全部场景，其余八项要求未修改。 |
| `make verify` | 全部通过；包含分支策略回归、gofmt、vet、固定版本 Staticcheck、全量测试、架构守卫及 race，[完整质量门记录](verification/make-verify.txt)。 |

PTY 通过独立确认管道同步实际按键、控制事件与 resize，不使用 sleep 猜测完成。终端退出检查进程状态以及完整 termios；仅按现有 PTY 验收规则忽略 Darwin 的内核 PENDIN 待重输标记。

## 场景与测试对应

| 场景 | 入口与关键断言 |
| --- | --- |
| 目录和权限 | `internal/tui/completion_test.go`：manual-only 可选，model-only/invalid/shadowed 不可选，同名内置 slash 优先而 `$` 可达，枚举顺序不影响候选。超限返回整个目录不可用。 |
| 目标与替换 | 同文件：首词/光标中间/裸 sigil/64 字节/路径/代码围栏/Unicode 空白；完整 token 替换后参数和首尾空白不变，hint 不写正文，4 MiB 超限原子失败。 |
| 编辑及状态 | `completion_state_test.go`：Tab、Skill Enter 只填入；内置命令共用提交/错误路径；Esc 先关闭后取回；Ctrl+C 直接定向停止；历史浏览选择不动草稿，接受后作为新编辑。 |
| 隔离与抑制 | 同文件及已有 `hooks_test.go`、`skills_test.go`、`user_questions_test.go`、`model_test.go`：各独占投影期间候选不可见，返回后不自动重开；流式/resize 不解除关闭，显式 Tab 先重开再接受。粘贴内操作字符无行为，独立 Enter 提交实际文本。 |
| 安全与容量 | `completion_view_test.go`：八份固定尺寸/配色 snapshot、控制及 bidi 元数据可见转义、零可见行停用按键、选择滚动、名称及类别/来源先于长参数提示分配列宽、主线高度扣除、超大列宽和长期保留容量约束。 |
| 终端解码 | `completion_terminal_test.go`：完整及分段 bracketed paste，真实 Tab/Enter/Esc/Alt+Enter/Ctrl+J，确认单次提交与唯一清理 owner 完成。 |
| 双 Provider | `internal/app/completion_e2e_test.go`：普通/恢复装配、普通/手动 Skill，选择前后 journal bytes 与请求次数不变，正式提交恰好一个带完整附件的用户输入。 |
| 缓存与恢复 | 同文件：相同 thread 的手输/补全 canonical 请求逐字节相等；稳定/内容/来源及原生指纹相等、native footprint 相等；恢复后的下一请求与未中断分支完全一致，原生回答顺序明确断言存在且有序。不删除随机字段或 cache key 制造等价。 |
| 准备失败 | 同文件及已有 `internal/tui/skills_test.go`：source_changed、准备取消、上下文超限、过期失败回复。被拒绝输入和附件不进入 Provider 或持久化，原文保留供编辑/取回。预算拒绝允许既有 turn 生命周期事实。 |

## 架构与文档核对

候选与元数据只用于内存展示；执行仍由正式提交的来源读取、预算、admission、Sync 和既有 Provider 原生链路完成。内置命令共用 `submitComposer`，显示与执行没有第二套 registry。内存编辑 revision 不进入任何 record。没有任何补全状态写入缓存前缀或 Session。

已同步 README、capability-status、Roadmap P5、总体架构及踩坑记录；主规格新增 `tui/input-completion` 七项要求，并合并 `tui/basic-chat` 四项修改，未覆盖其余八项要求或删除旧场景。实现只新增同包私有值与函数，使用既有字素布局、原子 replace、冻结来源、主线视口和 app 装配。主规格与 delta、实际测试路径及上述能力范围已核对；24 项实现/验收任务已完成，完整验收证据随本 change 归档保存。
