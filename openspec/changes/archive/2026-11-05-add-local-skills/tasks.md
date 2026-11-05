## 1. 强类型与来源目录

- [x] 1.1 实现 domain 中 Skill metadata、来源引用、附件及独立投影值对象，全部纯内存构造、校验和深拷贝；用零值、错误字段、字节上限、调用方修改测试验证，无 any 或版本路由。
- [x] 1.2 在 extension/skill 实现严格 frontmatter parser；确认 go.mod 当前无 YAML 依赖后选定并固定单一 YAML parser，验证重复键、alias/tag、错误类型、unsupported 字段及参数纯文本替换。
- [x] 1.3 实现固定根、项目边界、优先级和有界稳定 catalog，验证乱序枚举、损坏高优先项阻断、遮蔽、空目录、数量/bytes 边界和绝对根变化不影响 metadata fingerprint。
- [x] 1.4 实现 descriptor-bound local source 主文档和相对资源读取；通过确定性故障注入验证 symlink/TOCTOU、特殊文件、越界、源更改、取消与无跨来源 fallback，unsupported 平台失败关闭。

## 2. 第八项工具与预算

- [x] 2.1 为 skill.read 实现主文档/资源 typed input、strict decoder、ready call、result、executor 窄接口和 app adapter；测试 args/resource 互斥、未知字段、关联错配及 safe error 不泄露正文；验证仅当前 turn 已 durable 显式选择后的同来源同 revision 资源读取可使用例外，拒绝 queued/旧 turn/主文档/源变更。
- [x] 2.2 将唯一当前 Catalog 和双 Provider facade 更新为八项能力，加入 Skill 的完整消费者；request golden 验证排序、schema、固定描述及空目录可执行的安全失败，不保留七能力分支。
- [x] 2.3 接入现有只读调度、started admission、取消、幂等 ledger 和按序 result 提交；集成测试验证 partial args 零 I/O、重复调用不重读、与 Patch/Exec 屏障顺序及崩溃不重跑。
- [x] 2.4 为 Skill 增加 128 KiB 成功 preview 上限及全有或全无 finalization；以异构并行结果测试 sample/turn 预留、超额错误配对、无半份正文和无预算重置。

## 3. 输入与 Provider 原生事实

- [x] 3.1 扩展当前 UserInput 为原文加可选单份附件，更新所有 admission/claim/record 编码预算；测试原文保真、附件计费、pure admission、超限 journal bytes 不变及 getter 隔离。
- [x] 3.2 两家 Provider 独立实现附件原生编码、strict restore 与 prepared finalize/discard；固定 golden 验证顺序、同一 InputID、opaque 数据保真、畸形附件拒绝及 Sync 前不可见。
- [x] 3.3 扩展单向语义/离线/有效窗口投影，recall 只返回原文且详情独立显示附件；测试投影前后请求不变、无 I/O、预览与恢复一致和估算不重复。
- [x] 3.4 扩展 Skill 当前 result 与恢复配对，新增仓库固定 current fixture；测试文件删除后的恢复、result/output 缝隙补偿、started 未完成 uncertain、零文件读取和相同目录下 canonical bytes/item 顺序/fingerprint 等价。

## 4. 上下文与缓存

- [x] 4.1 增加可选 skill_catalog 来源、稳定 metadata 渲染及完整估算，更新 ContextPlan validator 与双 Provider 临时请求上下文；golden 验证空目录省略、固定顺序、实际发送 bytes 与预算一致。
- [x] 4.2 扩展 cache diagnostics 与 context 查询投影，验证 metadata 修改定位、绝对路径/时间/调用身份不影响稳定前缀、加载正文只改变尾部，以及恢复 full 请求和 OpenAI incremental 各自回归。
- [x] 4.3 将显式附件接入候选请求预检和已有自动压缩流程；测试过限不提交、摘要失败后的确定行为、queued 快照不因重试变化，压缩后不从磁盘自动激活 Skill。

## 5. 三种宿主与 TUI

- [x] 5.1 实现共享显式首词解析和准备服务，支持 /name 与 $name、内置命令优先及 manual/model flag；表驱动测试覆盖未知名称、绝对路径、代码块、原文、参数和未支持字段。
- [x] 5.2 接入 TUI、一次性 headless、stream-json，准备在 admission 前由唯一 owner 取消/清理，保留输入顺序；端到端 mock Provider 验证同内容、失败零提交、取消、EOF/shutdown、过期响应与资源最终关闭。
- [x] 5.3 实现 /skills 的来源分组、状态/诊断、Enter 放入 composer、Esc 保留草稿；固定正常/窄屏/空/损坏/遮蔽 snapshot 与按键测试证明无 Provider 请求、View 无 I/O。
- [x] 5.4 增加简洁 Skill 工具主线和附件详情，保持 queued/returned/recall 原文语义；测试 durable 前不宣称加载成功、取回再提交重新准备、工具资源读取不会伪称激活。

## 6. 契约检查与交付验证

- [x] 6.1 只退出 skill 的 TODO/占位 allowlist 并保留其他扩展限制；更新架构测试对真实边界的断言，验证 import graph、纯构造、包级变量、typed API 与未实现能力不可达。
- [x] 6.2 核对并同步 README、总体架构、Roadmap、pitfall 和实际 record vocabulary；将涉及七能力/原文/附件的描述与本 change delta 对齐，不把后续 hooks/plugins/fork/热加载写成已实现。
- [x] 6.3 运行完整 `make verify` 与新增双 Provider golden/cache/恢复测试，记录命令、平台、结果及限制；不得以当前基线通过替代新能力验收，fixture 不在运行时生成、测试不访问真实 home 或外网。
- [x] 6.4 执行 `openspec validate add-local-skills --strict`，逐项核对 proposal/design/specs/tasks、测试路径和契约无矛盾；验收成功后同步主 specs，保留真实验证记录供后续归档。
