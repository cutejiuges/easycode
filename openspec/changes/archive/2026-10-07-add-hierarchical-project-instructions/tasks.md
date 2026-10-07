## 1. 领域快照与安全发现

- [x] 1.1 在明确归属的领域文件中实现强类型、不可变的项目指令文档与快照，包含空快照、schema/renderer revision、深拷贝 getter、确定性渲染和内容派生 SHA-256 revision；以领域单测验证非法来源、顺序、预算、调用方 mutation 以及相同输入 canonical bytes 一致。
- [x] 1.2 实现纯内存 `NewLoader` 与显式 I/O `Load`，完成最近 `.git` 边界、无项目时只读启动目录、root-to-startup 顺序和同层 `AGENTS.md` 优先/`CLAUDE.md` 回退；以表驱动测试覆盖普通仓库、worktree `.git` 文件、无边界、空文件和同层双文件。
- [x] 1.3 实现 32 KiB 默认总预算、包含渲染边界的逐文件消费和 UTF-8 rune 边界截断；以单测验证多字节字符、恰好上限、超限停止后续目录、截断状态及 mtime/inode 变化不影响 revision。
- [x] 1.4 为 Darwin/Linux 实现基于目录 fd、`openat`/`fstatat`、`O_NOFOLLOW` 和打开后 `fstat` 的安全读取，为其他平台实现 fail-closed 分支；以确定性故障注入测试覆盖指令 symlink、`.git` 伪边界、特殊文件、非法 UTF-8、不可读文件和检查期间替换，执行 `go test ./internal/context/... ./internal/domain/...` 验证。

## 2. ContextPlan 与缓存分段

- [x] 2.1 扩展 PlanningInput 与 ContextPlan revision，使合法来源支持可选的 `project_instructions`，并保持无文档时既有三来源 bytes 与顺序；以 `internal/context` 单测验证深拷贝、非法顺序、重复来源和空快照兼容。
- [x] 2.2 将非空项目快照映射为 replace lifecycle、`project_stable` segment 和 `byte_heuristic_v1` token 估算，并计入饱和总预算；以 cache regression 验证只改项目内容或相对来源会改变稳定前缀，而只改 cwd/mtime/当前输入不会错误改变该分段。
- [x] 2.3 增加有项目指令的 exact-limit、over-limit 与 indeterminate 预算用例，并执行 `go test ./internal/context/...` 验证 ContextPlan、CachePlan 和 fingerprint 契约。

## 3. Provider 请求编译与原生历史隔离

- [x] 3.1 扩展共享 Provider TurnInput 的 typed attach/getter，使 Runtime 可附加深拷贝项目快照且宿主真实文本保持独立；以 Provider 公共契约测试验证零值、空快照、mutation 隔离和 family 无关性。
- [x] 3.2 扩展 OpenAI RequestCompiler，在 input 前确定性加入单一临时 user context item并保持“历史 + 当前真实 user”原顺序；更新有/无指令 request golden，验证无指令 bytes 不变、绝对路径不出现、后续轮次不叠加 context。
- [x] 3.3 验证 OpenAI reducer/prepared sample/finalizer 只提交真实 user item 与服务端 output；增加成功、失败和取消回归，证明临时 context 不进入 native commit、HistoryProjector 或恢复 payload。
- [x] 3.4 扩展 Anthropic RequestCompiler，在 messages 前确定性加入单一临时 user context message，继续禁止 `system`、tools、thinking 配置和 `cache_control`；更新有/无指令 request golden并验证路径前缀与鉴权行为不变。
- [x] 3.5 验证 Anthropic reducer/prepared sample/finalizer 只提交真实 user message 与 assistant message；增加成功、失败和取消回归，证明临时 context 不进入 native commit、HistoryProjector 或恢复 payload，执行 `go test ./internal/provider/...` 验证双 Provider。

## 4. Runtime 与应用启动装配

- [x] 4.1 让 Runtime Config 持有并深拷贝启动快照，在同一 turn 中把同一值传给 Planner 与 Provider；以 fake planner/conversation 测试验证快照一致、宿主不能逐轮替换、超限或非法计划保持零 Provider 调用且项目正文不进入事件和 journal。
- [x] 4.2 在 app 取得启动 cwd 后、打开 Session/Catalog/Provider 资源前加载一次快照，并把它贯穿新建、`--resume`、`--continue`、interactive、text、JSON 与 stream-JSON 装配；以 app 测试验证发现失败不创建 Session/Catalog、不调用 Provider且不泄露绝对路径或正文。
- [x] 4.3 验证同一进程修改项目文件后仍使用原快照，新进程才重新加载；执行 `go test ./internal/runtime/... ./internal/app/... ./internal/headless/...` 覆盖长期 AgentLoop 与所有宿主模式。

## 5. Session 恢复与端到端回归

- [x] 5.1 为 OpenAI 与 Anthropic 增加 uninterrupted/restored 对照：在相同 commits、下一输入和项目快照下比较 canonical request bytes、原生 item/message 顺序、native footprint 与 stable prefix fingerprint 完全相同。
- [x] 5.2 增加快照变化恢复用例，验证既有 journal bytes、Conversation revision、opaque native items 和 Session metadata 不变，下一请求仅替换一个临时 context 且 project-stable fingerprint 可归因变化。
- [x] 5.3 增加从不同绝对 cwd 恢复但规范化快照相同的测试，验证不 `chdir`、不从 `creation_cwd` 加载旧规则且绝对路径差异不改变请求；执行 `go test ./internal/app/... ./internal/session/...` 完成恢复回归。

## 6. 文档与质量门

- [x] 6.1 更新 `docs/architecture/overall-architecture.md`，记录项目指令 Loader、领域快照、Context/Provider 边界和请求时临时注入的数据流，并核对未把 Session 或语义历史描述成项目指令事实源。
- [x] 6.2 更新 `docs/roadmap/product-roadmap.md` 的 P2 状态与退出项；把 descriptor 发现、symlink 拒绝、预算截断和恢复快照条件中可复用的经验补充到 `docs/roadmap/pitfall-log.md`，核对文档只宣称本 change 实际完成的能力。
- [x] 6.3 运行 `gofmt`、定向 golden/cache/restore 测试和 `git diff --check`，确认测试 fixture 不含真实项目内容、绝对本机路径或 secret。
- [x] 6.4 运行 `make verify`，确认 gofmt、go vet、Staticcheck、架构边界、全量测试和 race test 全部通过，并在归档前用 `openspec validate add-hierarchical-project-instructions --type change --strict --no-interactive` 复核 artifacts 与实现一致。

## 7. 代码整洁与边界回补

- [x] 7.1 将项目指令 canonical JSON 改由 `internal/codec.MarshalCanonical` 生成，删除领域包中的 Sonic 配置副本；扩展架构门禁，允许 domain 依赖零内部依赖的 codec，并禁止其他生产包直接导入 Sonic，验证 canonical bytes、revision、双 Provider request golden 与 cache fingerprint 不变。
- [x] 7.2 定义 4 MiB 项目指令模型可见字节硬上限，让领域快照与 Loader 构造器共同拒绝零值、负值和超限值；将文件读取预分配限制在已验证上限内，以单测覆盖 4 MiB 边界、边界加一、`MaxInt`、UTF-8 截断及无副作用拒绝。
- [x] 7.3 将历史 headless 请求/恢复 E2E 的启动目录改为独立临时目录，并让直接装配与完整 `Run` 路径共享该显式目录；保留专门项目指令 fixture，证明历史 E2E 不依赖仓库真实 `AGENTS.md`。
- [x] 7.4 重新执行定向 domain/context/provider/app 测试、`go mod tidy -diff`、`git diff --check`、严格 OpenSpec validate 与完整 `make verify`，确认架构、golden、缓存、恢复及 race 门禁全部通过。
