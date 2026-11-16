## 1. 固定可复现的测量与覆盖基线

- [x] 1.1 在无其他重测试竞争时记录当前完整普通/race 的包、顶层测试和默认 fuzz seed/Example 清单及墙钟时间；产出机器、提交、命令、缓存状态和竞争进程说明，校验所有阶段退出码。
- [x] 1.2 为严格 JSON 的小对象、深层结构和大文本建立 benchmark，报告普通/race 下 ns/op、B/op、allocs/op；保留当前两个预览 fixture 和两组重测试作为端到端性能对照。
- [x] 1.3 审核最慢 app/runtime/session/provider 测试中的重复装配、大文本和真实进程开销，产出优化清单；任何拟合并用例必须列出原断言到保留测试的对应关系。

## 2. 消除严格解码重复工作

- [x] 2.1 合并 UnmarshalRecord 的重复基础与形状校验，复用调用局部的已验证字段视图；通过 codec 正反例，确认没有新增公开绕过入口或包级可变缓存。
- [x] 2.2 减少 ObjectMembers/嵌套形状遍历对大子树的重复扫描、物化与拷贝；验证输入和结果所有权、escaped key、重复键、未知字段、null、必需字段及自定义 decoder 边界。
- [x] 2.3 对新增或修改的扫描路径补充 differential/fuzz 与有界输入回归，覆盖语法、Unicode、数字、深度和尾随内容；记录执行语料与结果，不能依赖当前 encoder 生成坏格式预期。
- [x] 2.4 运行固定 Session、Provider native/golden/cache 和 uninterrupted/restored 等价测试，并复测 benchmark/预览 profile；记录 canonical bytes、fingerprint、累计分配和耗时的前后比较。

## 3. 有界执行与测试装配重构

- [x] 3.1 实现当前平台测试入口自动枚举和确定性重包分片，普通/race 分别保存清单；通过集合断言证明 Test、Example、Fuzz seed 不遗漏、不重复，新入口自动纳入。
- [x] 3.2 实现单一 owner 的进程调度、并发上限、日志和计时；用可控子进程验证峰值并发、失败传播、取消、超时、停止派发与最终回收。
- [x] 3.3 按基线清单优化可复用测试装配，保持环境/临时目录和可变状态隔离；任何测试合并都附断言映射并通过对应普通/race 回归。
- [x] 3.4 对优化后的 app 与其他重包比较有界并发组合，记录完整时长、峰值资源与失败情况，选择满足五分钟目标且可复跑的默认配置。

## 4. 接入统一质量门

- [x] 4.1 将 Makefile 的普通、race 和完整测试入口接入统一调度，make verify 保留分支策略、格式、vet、Staticcheck 与架构检查；通过命令及执行清单确认范围等价，并验证 make -j 不会无界叠加普通/race。
- [x] 4.2 更新 CI 的执行/计时与诊断产物配置，保留 hooks 对同一 make verify 的调用；检查失败退出传播与性能预算报告，不以缓存结果或单阶段五分钟替代完整目标。
- [x] 4.3 更新 README、总体架构测试策略与 pitfall，说明入口、默认并发、覆盖集合、计时口径和已验证根因；核对所有命令与实际实现一致。

## 5. 完整验收与规格沉淀

- [ ] 5.1 在最终源码上连续两轮执行全部测试结果不命中缓存的 make verify，要求每轮总墙钟时间均小于 300 秒；记录静态检查、普通/race 分阶段及合计时间、全覆盖清单与退出码。
- [ ] 5.2 在 Linux 原生 CI runner 上取得同一最终实现的完整质量门计时与通过证据，记录 runner 配置；没有实际执行证据时保持未验证，不把交叉编译标记为完成。
- [x] 5.3 对 codec、调度器和受影响测试执行针对性的随机顺序/race 回归，确认未改变 fixed fixture bytes、canonical/native 顺序、权限和资源清理边界；记录剩余平台限制。
- [ ] 5.4 编写 verification 报告并同步 engineering/test-execution 主规格，执行 change/main specs 的严格验证及 git diff --check；逐项核对 AGENTS、文档和实际测试入口，只有全部目标达成后标记任务完成。
