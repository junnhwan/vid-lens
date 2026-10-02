# 问答事实与时间边界

## 当前实现

Chat 检索回答、视频概览、Agent 规划和最终回答共用 `qaGroundingPolicy`；检索改写复用其中的时间规则。实现入口：

- [公共规则和服务端时间](../../internal/service/qa_grounding.go)
- [Chat / 概览消息](../../internal/service/chat_messages.go)
- [Agent 规划消息](../../internal/service/video_agent_loop_planner.go)
- [Agent 最终回答消息](../../internal/service/video_agent_tools.go)
- [检索改写](../../internal/service/rag_rewrite.go)

每次构造模型输入时读取服务端时钟，明确提供 RFC3339 时间、当前日期、UTC 时间。当前没有用户时区设置，产品日历参考固定为 UTC+08:00，不依赖部署机器的本地时区或 tzdata，不将它声称为用户所在地。设置页展示静态规则，实际调用时追加时间；不会把配置页打开日期永久保存为“今天”。服务端时钟错误仍会影响时间基准。

时间规则区分四件事：模型知识截止时间、现实当前时间、视频录制/发言时间、视频播放时间。不会配置一个猜测的模型知识截止日期。过去月份不代表事件已经发生；录制时间未知时，“明年”不能用服务器今年推算；只有月份或年份时不补造具体日。当前链路没有联网核验结果，最新状态只能在现有材料的时效范围内作答。

事实规则要求核对问题前提和事实归属，保留数字、单位、条件、可能性和来源。摘要、历史回答、记忆、中间结论不能成为独立证据。检索未命中或工具失败不等于事实不存在，也不证明全片没有提及。证据冲突时保留各自来源；能回答的部分直接回答，缺失部分明确说明。

原文、补充视频上下文、用户回答偏好、Chat 记忆及 Agent 历史/记忆使用普通消息传递，产品规则保持 system 消息。相关组装在 [chat_prepare.go](../../internal/service/chat_prepare.go)、[memory_preferences.go](../../internal/service/memory_preferences.go)、[video_agent_tools.go](../../internal/service/video_agent_tools.go)。这是输入权限层级和提示约束，不是可以证明完整的提示注入隔离。

单视频检索不可用而继续用摘要/转写作答时，模型收到显式限制说明，接口保留 `retrieval_unavailable` 降级原因。正常概览不伪报检索失败。没有候选编号的概览和降级路径不得生成引用编号。

## 验证方式

[qa_grounding_test.go](../../internal/service/qa_grounding_test.go) 验证跨日、跨年、闰日及不同时区输入，五个模型入口的时间/规则传递，资料的消息角色，以及同步/流式检索失败的输入与返回约束。记忆采纳的集成测试使用真实消息构造器，保证用户偏好仍被采纳而不升级为 system 内容。

```powershell
go test ./internal/service -run 'TestQA|TestChatMemoryOutboxMultiDimension' -count=1
```

[qa_grounding_real_test.go](../../internal/service/qa_grounding_real_test.go) 提供显式启用的 12 个合成材料探针：过去月份、原文相对时间、错误前提、缺失证据、事实归属、最新状态、资料内伪指令、历史污染、中间结论污染、摘要冲突、正常可回答问题、跨模态冲突。直接使用生产消息构造器和已配置的对话客户端，不连接数据库或写入聊天历史。调用外部模型会使用所选配置的额度。

```powershell
$env:VIDLENS_QA_GROUNDING_CONFIG = (Resolve-Path config.yaml).Path
$env:VIDLENS_QA_GROUNDING_REPORT = Join-Path (Get-Location) 'docs-private/eval/qa-grounding.json'
go test -tags real_llm ./internal/service -run '^TestQAGroundingRealModel$' -count=1 -v
```

报告保存模型名、时间、实际输入、原始输出和逐题审阅标准，不含端点或密钥。`human_semantic_success` 默认为 null，必须由真人审阅后另行标注。测试 PASS 只代表调用完成且答案非空，不代表语义正确，也不能当作幻觉率降低的统计结果。真实报告和生成数据留在忽略目录。

## 尚未保证的部分

这些改动不增加一次在线 LLM 审判调用，也不增加联网搜索；每次调用的输入 token 会增加。检索召回率、ASR/OCR 原始错误、错误但编号有效的引用、复杂推理和不同模型的服从度，仍须单独评估。单条引用的编号合法性不能替代“原文是否支持该结论”的语义验证。

正式比较应冻结模型配置、材料、问题与日历基准，重复运行改前/改后版本，分别记录：有依据且答对、无依据却断言、正确说明不足、可回答却拒答、引用支持率，以及调用失败/延迟/成本。有限样本的模型审阅结果与真人标签分开保存，不把调用成功率包装成问答准确率。
