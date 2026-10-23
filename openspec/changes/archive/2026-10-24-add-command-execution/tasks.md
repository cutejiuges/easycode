## 1. 强类型命令契约与目录

- [x] 1.1 在`internal/tool`增加`process.exec` capability、`ExecInput`构造器/strict decoder/validator及边界常量，覆盖空命令、重复/未知字段、尾随JSON、UTF-8、相对workdir、穿越和timeout上下界单测，并运行`go test ./internal/tool/...`
- [x] 1.2 增加不可变`ShellProfile`、`EnvironmentPolicy`、`SandboxProfile`、`ExecCandidate`及canonical candidate fingerprint，验证getter深拷贝、动态环境值/identity不污染fingerprint和输入变化会改变fingerprint，并运行对应`internal/tool`测试
- [x] 1.3 扩展`Invocation`、`ReadyCall`、调度分类、typed result/metadata与安全错误构造以封闭支持Exec，验证capability/input/result错配、非法状态组合、绝对路径/PID/环境值和超限preview均被拒绝
- [x] 1.4 为Anthropic `Bash`和OpenAI `exec_command`加入稳定structured facade/schema，将五项真实能力纳入唯一catalog排序、canonical bytes与fingerprint，更新catalog golden并验证缺少planner/executor/renderer时构造失败
- [x] 1.5 更新context tool-catalog估算与cache-plan回归，验证新增facade会改变catalog segment/总估算/stable-prefix fingerprint，而实际command、workdir、approval和环境值不会污染稳定前缀

## 2. 双 Provider 原生 Wire

- [x] 2.1 扩展Anthropic request compiler稳定发送`Bash` schema，补充首轮/续轮request golden并验证base URL前缀、工具顺序、canonical bytes与secret隔离
- [x] 2.2 扩展Anthropic indexed reducer与strict decoder生成typed Exec ready call，覆盖随机chunk、半包、UTF-8、partial JSON、重复call ID、未知字段、超限命令及非法terminal，并运行Anthropic reducer测试
- [x] 2.3 扩展Anthropic native codec/restore/result codec保留Bash `tool_use`顺序并编码匹配`tool_result`，覆盖成功、非零退出、拒绝、取消、timeout、`outcome_uncertain`及uninterrupted/restored下一请求canonical bytes等价
- [x] 2.4 扩展OpenAI request compiler稳定发送`exec_command` function schema，补充首轮/续轮request golden并验证tools排序、Responses-only wire、canonical bytes与Authorization隔离
- [x] 2.5 扩展OpenAI function-call reducer与strict decoder生成typed Exec ready call，覆盖随机chunk、半包、UTF-8、partial arguments、重复call ID、未知字段、超限命令及非法terminal
- [x] 2.6 扩展OpenAI native codec/restore/result codec保留exec_command item顺序并编码匹配`function_call_output`，覆盖全部Exec终态及uninterrupted/restored下一请求canonical bytes等价
- [x] 2.7 更新双Provider capabilities、tool wire golden与cache regression，验证仅在request/reducer/native restore/result codec全链路存在时声明当前function/structured tool能力

## 3. Session 当前契约与恢复

- [x] 3.1 扩展`tool_call_ready` typed payload/codec保存Exec输入，验证canary、capability、wire kind、sample/call index及strict unknown-field/尾随JSON拒绝
- [x] 3.2 扩展`tool_approval_decided`展示metadata与`tool_call_result` Exec metadata/preview codec，验证不保存PID、绝对路径、环境值、输出chunk或底层cause
- [x] 3.3 扩展ReplayPlanner的Exec状态机，覆盖ready无approval、allow无started、started无result、result后待output commit及损坏顺序，验证恢复阶段零approval、零executor、零进程探测
- [x] 3.4 逐字节替换`internal/session/testdata/current`相关fixture并增加含获批/拒绝/中断Exec的不可变fixture，验证fixture由生产Loader读取且测试运行时不使用当前encoder生成
- [x] 3.5 补充真实子进程lease竞争下Exec interrupted-tail恢复测试，验证lease先于load/repair、补偿seq单调、journal失败路径bytes不变且started命令只产生`outcome_uncertain`

## 4. Approval、环境与 Sandbox

- [x] 4.1 将approval presentation改为Patch/Exec封闭联合并扩展candidate-bound policy，验证Exec始终只提供`allow_once/deny/abort`、Patch session grant语义不变、stale/重复response零副作用
- [x] 4.2 扩展headless approval失败关闭路径，验证`--print`、一次性`--json`与stream-json遇到Exec时产生`approval_required`配对output、零进程启动且外部JSONL词汇不增加命令progress
- [x] 4.3 在应用显式Open生命周期构造不可变环境snapshot与non-login shell profile，使用名称allowlist和确定覆盖值排除Provider/API/Authorization/Cookie/代理凭据及内部变量，并通过脱敏测试证明secret不进入子进程fixture、日志、错误或Session
- [x] 4.4 实现纯内存SandboxPlanner并验证plan冻结workspace/workdir、网络禁用、可读/可写范围、临时/缓存范围及profile revision，approval不得修改或扩大plan
- [x] 4.5 实现Darwin安全workdir打开和系统sandbox adapter，使用descriptor-bound验证与确定profile golden覆盖workspace内允许、workspace外写拒绝、网络拒绝、symlink swap及sandbox不可用失败关闭
- [x] 4.6 提供非Darwin build-tag unsupported adapter并验证目标平台可编译且执行稳定返回`unsupported_platform`，不得调用普通`cmd.Run`降级

## 5. Process owner与输出预算

- [x] 5.1 实现stdout/stderr各512 KiB的确定性head/tail buffer和固定stdout后stderr的256 KiB UTF-8模型preview，覆盖chunk边界、超长UTF-8、精确预算、省略计数、两个流及深拷贝测试
- [x] 5.2 定义可故障注入的process launcher/handle/clock边界与唯一`ProcessExecution` owner，确保`New*`纯内存、`Run`显式启动、owner持有单一done并由创建者关闭内部channel
- [x] 5.3 实现非交互shell启动、独立进程组、stdout/stderr并发排空、exit/wait与typed成功/非零/启动失败结果，使用确定性fake process验证不会因pipe写满阻塞且跨流观察顺序不进入durable preview
- [x] 5.4 实现取消与timeout的TERM到KILL有界升级、原组终止、正常退出后的原组残留清理、有界reader排空和最终wait；故障注入覆盖接受前/后取消、timeout、组内子孙、保留pipe的setsid子进程、终止/探测失败与outcome_uncertain，验证result不代替cleanup done且测试不得依赖sleep；拆分非回收退出观察与最终Wait，增加启动屏障、回收后编号复用及禁止旧PGID信号/探测的确定性回归
- [x] 5.5 实现容量1非阻塞progress合并器与不可变snapshot，验证慢消费者不会阻塞pipe reader、锁内无阻塞channel/Wait/Close/回调、最终snapshot先于terminal且无goroutine泄漏
- [x] 5.6 在Darwin集成测试中运行真实前台命令，覆盖正常退出、非零退出、stdout/stderr、大输出、cwd、环境过滤、timeout、取消和原进程组清理；增加setsid保留/关闭输出的限制回归，独立控制通道回收并Wait夹具，验证sandbox继承，使用确定等待信号而非概率竞态

## 6. Runtime有序执行与进度协议

- [x] 6.1 将Exec接入existing exclusive scheduler barrier，验证`Read -> Exec -> Grep`和`Patch -> Exec`严格等待前序result durable、执行不重叠且最终results/outputs仍按call index提交
- [x] 6.2 接入Exec candidate、approval、allow fact、execution started、executor与result的durable顺序，故障注入每个Append/Sync/finalizer边界并验证started前零进程、accepted后恰好执行一次、失败时正确poison
- [x] 6.3 增加强类型command started/progress/terminal RuntimeEvent与validator，验证identity、UTF-8、大小、顺序、terminal后事件拒绝以及progress不写Session
- [x] 6.4 让turn owner在cancel/interrupt/shutdown时持续拥有ProcessExecution直到done，覆盖approval竞态、started竞态、shutdown timeout升级与下一turn不得早于旧命令cleanup完成；outcome_uncertain后禁止新副作用，未完成Wait仍保留唯一reaper和writer所有权
- [x] 6.5 扩展工具结果提交和恢复补偿，验证Exec每种状态都产生匹配Anthropic/OpenAI原生output、result durable后才进入下一sample、恢复不重跑且preview逐字节一致

## 7. Claude Code风格TUI与应用装配

- [x] 7.1 实现Exec approval overlay，显示完整命令、相对workdir、timeout与能力摘要，默认聚焦No且仅允许Yes/No/Abort，覆盖normal/small、滚动、resize、stale identity和重复提交snapshot/行为测试
- [x] 7.2 实现命令timeline的Waiting/Running/近期输出/elapsed/bytes/exit/timeout/cancelled/truncated呈现，普通摘要限制两行160 code points；新增Ctrl+O可滚动详情，复用完整approval命令及terminal-only typed result preview，校验256 KiB/UTF-8/事件kind边界并固定时钟完成TUI snapshot测试
- [x] 7.3 保持composer草稿、modal键盘路由和定向interrupt语义，验证approval或running期间不能提交并发turn、取消中等待cleanup terminal、完成后恢复输入且不消费旧response
- [x] 7.4 在`internal/app`装配environment、shell、sandbox、runner与Runtime依赖，保持资源逆序cleanup和shutdown escalation，验证构造失败不泄漏lease/provider/workspace/process资源

## 8. 端到端、文档与质量门

- [x] 8.1 为Anthropic与OpenAI分别增加“Read -> approved Patch -> approved Exec测试 -> final summary”端到端用例，验证native item顺序、durable facts、tool pairing、usage聚合、catalog/cache fingerprint和最终TUI行为
- [x] 8.2 增加deny、abort、interrupt、timeout、sandbox denial、spawn failure、大输出与崩溃恢复端到端回归，验证文件/进程/网络副作用、secret隔离、唯一terminal和cleanup所有权
- [x] 8.3 更新`docs/architecture/tool-system.md`、`docs/roadmap/product-roadmap.md`与`docs/roadmap/pitfall-log.md`，准确记录Darwin前台Exec已实现范围、Claude Code交互取向、两参考工程能力取舍及后台/PTY/artifact/跨平台延期条件
- [x] 8.4 运行`gofmt`、定向包测试与fixture/golden更新，再执行`make verify`并确认gofmt、go vet、Staticcheck、架构守卫、全量测试和race test全部通过
- [x] 8.5 对照proposal/spec/design逐项审查职责与依赖方向、Provider原生语义、cache稳定性、Session恢复、无重复副作用、无secret泄漏、无dead code/旧reader/无用依赖，并用`openspec validate add-command-execution --strict`验证变更工件
