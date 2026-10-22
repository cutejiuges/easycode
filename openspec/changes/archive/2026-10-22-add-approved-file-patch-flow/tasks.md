## 1. Tool 强类型契约与目录

- [x] 1.1 在 `internal/tool` 增加 `fs.patch` capability、Provider wire kind、Patch input/replacement/ready/invocation 封闭联合与严格构造校验，并用单测验证未知操作、超限输入、联合错配和 clone 不可变性。
- [x] 1.2 实现 Anthropic `Edit` strict JSON decoder 与 OpenAI 单文件 `Update File` freeform parser，验证完整消费、多个 hunk、有序 replacement，以及 Add/Delete/Move/多文件/尾随内容/模糊输入均失败关闭。
- [x] 1.3 增加 Patch candidate、base evidence、approval request/response/decision、fingerprint 与 Patch result metadata 的强类型值，验证 ID、UTF-8、反馈16 KiB、决定集合和敏感字段边界。
- [x] 1.4 将 Tool result、renderer 与 executor/policy/preparer 接口扩展为四能力封闭联合，验证 denied、conflict、cancelled、error、success、outcome-uncertain 都产生稳定英文 code 和有界 preview。
- [x] 1.5 扩展 Catalog 为 structured/custom-freeform definition 联合及八个确定性 facades，验证排序、canonical bytes、fingerprint、getter 防变异和任一真实组件缺失时构造失败。

## 2. 双 Provider Patch Wire

- [x] 2.1 在 Anthropic request compiler 中加入稳定 strict `Edit` schema并更新request golden，验证路径前缀、工具顺序、cache fingerprint及动态approval/candidate状态不进入请求。
- [x] 2.2 扩展 Anthropic reducer归并完整`Edit` tool-use input并产出typed Patch ready call，使用随机chunk、半包、非法JSON、重复call ID、未知字段和取消测试验证partial input零副作用。
- [x] 2.3 扩展 Anthropic native entry、history restore与result codec支持Patch `tool_result`，验证thinking/signature/redacted thinking无损、call/output pairing及uninterrupted/restored请求bytes等价。
- [x] 2.4 在 OpenAI request compiler 中加入custom freeform `apply_patch` definition并更新request golden，验证Read/Glob/Grep仍为strict function、Patch不被伪装成function且工具顺序/fingerprint稳定。
- [x] 2.5 扩展 OpenAI reducer支持`custom_tool_call`和custom input delta/done状态机，使用随机chunk、半包、UTF-8、非法顺序、identity冲突、超限与取消测试验证完整后才产生ready call。
- [x] 2.6 扩展 OpenAI native entry、history restore与result codec支持`custom_tool_call_output`，验证custom/function wire kind、opaque reasoning、call/output顺序及uninterrupted/restored请求bytes等价。
- [x] 2.7 更新两家Provider capability声明与native/current golden，验证只有request、reducer、persistence/restore、result codec全链路存在时才报告对应Patch能力。

## 3. Patch 候选与安全执行

- [x] 3.1 实现对base bytes应用有序replacement的纯逻辑引擎，验证Anthropic单次/replace-all和OpenAI hunks的exact match、重叠、空变化、CRLF/末尾换行与UTF-8行为。
- [x] 3.2 实现确定性完整unified diff和changed-line summary，验证固定header/context/换行、相同输入逐字节稳定，以及超过256 KiB时返回`patch_preview_too_large`而不截断批准视图。
- [x] 3.3 扩展workspace handle边界以descriptor-relative方式安全打开Patch目标并生成base evidence，验证16 MiB上限、权限、常规文件、目录、FIFO、symlink、路径穿越和确定性TOCTOU替换。
- [x] 3.4 实现`PatchPreparer`与candidate fingerprint，验证ready durable前零文件I/O、准备阶段零写入，以及相同typed input/base/final bytes得到相同fingerprint而时间、绝对cwd和identity不参与。
- [x] 3.5 实现Unix同目录临时文件、完整写入、权限位、文件Sync、`renameat`与父目录Sync的Patch executor，并验证成功后目标逐字节等于candidate且无临时文件遗留。
- [x] 3.6 增加不支持平台的fail-closed实现与可注入write/Sync/rename/cleanup边界，验证rename前失败不改目标、base变化返回`patch_conflict`、rename后不确定结果poison且不可自动重试。

## 4. Runtime 协议与权限模型

- [x] 4.1 扩展`internal/protocol`的RuntimeEvent为typed approval requested/resolved payload，验证identity、fingerprint、相对路径、完整有界diff、decision集合及未知/敏感payload拒绝。
- [x] 4.2 增加`resolve_tool_approval` RuntimeCommand及typed结果，验证turn/request/invocation/fingerprint关联、仅deny允许反馈、重复/过期/非法command在任何Session或文件副作用前失败。
- [x] 4.3 将Tool Policy改为`allow/ask/deny`封闭决定并实现Runtime实例级Patch grant，验证只读自动allow、Patch ask、`allow_session`只在同workspace/Runtime命中且关闭或resume后消失。
- [x] 4.4 在组合配置中显式区分interactive与headless approval能力，验证headless ask稳定产生`approval_required` denied result、不等待stdin、不建立grant且Catalog保持一致。

## 5. Session 当前 Schema 与恢复

- [x] 5.1 增加`tool_approval_decided` typed draft/record并扩展ready/result保存Patch input、wire kind和metadata，验证所有constructor深拷贝、严格大小/identity/decision校验且无`any`公共边界。
- [x] 5.2 直接更新唯一当前payload/envelope canary与strict codec，验证未知字段、尾随JSON、旧开发shape、反馈/preview超限和capability/wire kind错配全部fail closed且错误不回显正文。
- [x] 5.3 扩展ReplayPlanner验证allow/deny/abort、candidate fingerprint、parallel-read batch与exclusive Patch屏障，验证非法started、乱序result、重复approval和function/custom output错配均在副作用前拒绝。
- [x] 5.4 实现Patch各崩溃点的resume reconciliation，验证before-approval、allow-before-started、started-before-result、result-before-output和output-before-terminal均只追加确定补偿且不显示approval、不访问workspace、不恢复grant。
- [x] 5.5 逐字节重写`internal/session/testdata/current`与预期replay plans，覆盖文本、异构调用、获批/拒绝Patch、exclusive屏障、所有中断尾部和superseded fixture，并验证fixture不是测试运行时生成。
- [x] 5.6 扩展Session进程级lease与恢复集成测试，验证竞争进程不能在approval/reconciliation期间repair或append、崩溃释放lease、补偿后seq单调且失败路径journal bytes符合契约。

## 6. Runtime Tool Loop 与控制 Owner

- [x] 6.1 将Runtime依赖扩展为Patch preparer/executor、typed Policy、approval ID source和交互能力，并验证纯构造不启动goroutine或访问文件系统、缺失依赖时Patch不暴露。
- [x] 6.2 将调度器实现为连续parallel-read批次与单个exclusive Patch屏障，验证`Read,Grep,Patch,Read`的准入/执行/result顺序、最多8个read workers及并发完成不改变call index。
- [x] 6.3 在Patch屏障处实现candidate准备、approval event发布与单owner等待，验证前序result durable后才读Patch base、后续call等待、同一request只发布/解决一次且无helper goroutine泄漏。
- [x] 6.4 实现allow fact、execution-started、result/output的durable顺序及deny同batch路径，使用故障注入验证每个append/Sync/finalizer失败点零越界副作用和不确定durability触发poison。
- [x] 6.5 实现approval response与interrupt/shutdown的竞态收口，验证allow被接受后恰好执行一次或取消先被接受时零执行，abort为全部未完成calls生成有序outputs且turn只有一个失败终态。
- [x] 6.6 扩展`AgentLoop`单owner路由active approval、普通follow-up FIFO与request去重，验证approval不成为新turn/same-turn steer、旧turn response不能命中新turn且accepted command得到确定结果。
- [x] 6.7 扩展Runtime与AgentLoop race/泄漏测试，验证approval等待、executor、stream和shutdown均有唯一cleanup owner、done信号与`go test -race`下稳定行为。

## 7. Claude 风格 TUI 与应用接线

- [x] 7.1 在Bubble Tea model中加入独立approval overlay状态与typed event投影，验证默认聚焦`No`、路径/完整diff/三个内核决定可见且Provider wire、绝对路径和完整文件不显示。
- [x] 7.2 实现overlay按键隔离、选择、滚动、resize和submitted防重复，验证普通字符/Enter/Tab不污染聊天草稿，Yes与session allow提交正确identity/fingerprint，小窗口仍可查看完整diff。
- [x] 7.3 实现仅在`No`上启用的拒绝反馈编辑和Esc/Ctrl+C语义，验证16 KiB/UTF-8边界、第一次Esc退出反馈、再次Esc定向中断、overlay只在resolved/terminal后关闭。
- [x] 7.4 增加正常/小尺寸/滚动/resize/反馈/允许/拒绝/中断/无颜色固定snapshot，验证同一candidate在布局变化后焦点、草稿、反馈与决定语义不变。
- [x] 7.5 在`internal/app`交互组合根接通TUI command与AgentLoop approval response，并保持print/json/stream-json无交互fail closed；通过app测试验证退出、取消、Session关闭与terminal输出顺序。

## 8. 端到端验证与文档收口

- [x] 8.1 增加Anthropic Edit和OpenAI apply_patch交互E2E，分别验证allow-once、session allow、deny反馈、conflict、成功修改及下一sample原生call/output pairing。
- [x] 8.2 增加headless与恢复E2E，验证两家Provider在approval_required、各crash tail、补偿后下一用户输入中的canonical bytes、native item顺序和fingerprint与规范一致。
- [x] 8.3 增加安全与回归审计测试，验证symlink/TOCTOU、write/Sync/rename故障、secret与正文不进入日志/错误/fixture、Catalog/cache fingerprint不受动态状态污染。
- [x] 8.4 同步Tool/Provider/Runtime/Session/TUI架构文档、Roadmap阶段与pitfall记录，逐项核对实际record vocabulary、测试路径和延期能力均未被写成已实现。
- [x] 8.5 运行`openspec validate add-approved-file-patch-flow --strict`与`make verify`，修复全部gofmt、vet、Staticcheck、架构、全量测试和race问题，并记录任何无法执行检查的原因与风险。
