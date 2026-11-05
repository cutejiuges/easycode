## ADDED Requirements

### Requirement: OpenAI user questions preserve native function semantics

OpenAI SHALL 从当前 catalog 稳定编译 `request_user_input` strict function，归并完整 arguments 后调用提问 strict decoder，仅在原生 item 和 response.completed 全部合法后产生 `user.question` ready call。参数半包、非法 Unicode、重复键/题目 ID、未知字段、模型提供 answers 或未声明 facade MUST 使 sample 失败，零提问事件和工具执行。

完成调用与回答 SHALL 分别保存在原始 function_call 和匹配 call ID 的 function_call_output；按原 item/call 顺序编码完整冻结 preview，保持 phase、reasoning 和 encrypted content。宿主回答不伪造 user item，不启用其他 wire 或未来能力。

#### Scenario: Complete a fragmented question call
- **WHEN** 中文问题参数跨随机 chunk、半包和 UTF-8 边界完成
- **THEN** 恰好一个 typed ready call，输入完整前没有问卷事件

#### Scenario: Continue after a question result
- **WHEN** 回答结果已 durable 且所有 sibling outputs 已提交
- **THEN** 下一 sample 使用原始有序 function call/output，回答只出现一次且 opaque reasoning 不变
