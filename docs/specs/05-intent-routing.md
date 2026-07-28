# Spec 05: 意图识别前置路由层（bullet A）

> 推进顺序第五份。决策母本见 `docs/specs/00-refactor-decisions.md`（第 4 节 (A) 借鉴度与诚信约束）；领域语言见 `CONTEXT.md`（Intent / ExecutionPolicy / Signal / RuleIntentClassifier / LLMIntentClassifier）。
> 依赖：spec 04 (B) 的 ExecutionPolicy 参数语义（intent → 检索/生成预算）。spec 04 A 段已用 `isVideoOverviewQuestion` + `session.ScopeType` 作 intent 占位，本 spec 把占位替换为真分类器。
> 共享：spec 01 的共享 query 池（每条 case 的 `category` 即黄金 intent 标签，复用为评测正例/反例/边界）。

## Problem Statement

VidLens 的问答入口现在对每个问题都走完整检索 + LLM 生成，没有"这次提问到底要不要检索、要检索多大范围"的前置判断。从简历与面试角度有两个真问题：

1. **intent 分类只有单点雏形，没成级联**。`isVideoOverviewQuestion`（`chat_messages.go:63`）只判 overview 一类（纯关键词 contains），`ClassifyVideoAgentTemplate`（`video_agent.go:73`）面向实验 agent 路径不进产品默认。没有一个"规则层短路 + LLM 兜底"的级联分类器把所有 intent taxonomy 覆盖掉。每个问题都无差别走检索 + LLM，overview 类问题白白跑向量检索、闲聊白白烧 LLM。决策记录第 4 节稀缺点 = "级联分类短路省 LLM 调用"，现在没做。

2. **指代消解靠 LLM 改写，没有无副作用 Signal 提取**。`rag_rewrite.go` 的 query rewrite 已有"补全上下文、省略指代"语义（line 108），但那是 LLM 改写（有副作用、要调 LLM、可能编造——line 109 自己标"不要编造最近对话里不存在的实体"）。CONTEXT.md 定义的 Signal = "无副作用正则提取的结构化线索（时间戳/实体/章节/比较）"，用于指代消解与检索过滤——这是真空缺。用户问"第 15 分钟讲了什么"，时间戳 Signal 应无副作用提取出来缩检索范围，现在没有。

痛点从用户视角：作为维护者，我没法诚实回答"你怎么决定 overview 不检索、闲聊不烧 LLM、'第15分钟'怎么缩范围"——分类靠单点 if、指代靠 LLM 改写、Signal 没有。决策记录第 4 节稀缺点 = "级联分类短路省 LLM 调用 + 指代消解二次提取"，两个都没成体系。

## Solution

把单点 if 扩成级联分类器 + 新建无副作用 Signal 提取，复用 spec 01 共享 query 池作评测、spec 04 ExecutionPolicy 作消费方：

### 1. RuleIntentClassifier（规则层，短路）

- 打分维度三路（CONTEXT.md 已定，决策记录第 9 节第 3 条）：**关键词命中 + signal 模式 + 历史 intent 加权**。
  - 关键词命中：扩 `isVideoOverviewQuestion` 的 contains 思路到全 taxonomy（overview/direct_qa/topic_compare/series_locate/timeline_locate/small_talk）。
  - signal 模式：时间戳（"第15分钟"/"15:00"）/ 比较句式（"对比"/"比较"/"异同"）/ 概览句式（"讲了什么"/"总结"）/ 闲聊（"你好"/"谢谢"）。
  - 历史 intent 加权：会话最近 N 轮 intent（Redis 已有 recent messages，复用解析），同 intent 连续出现加权。
- confidence 达阈值 → 短路，<1ms，不调 LLM。
- **每阈值/维度选择理由写 audit trail**（决策记录第 4 节硬约束）：短路阈值为何这么定、各维度权重为何这么分、历史 intent 加权窗口为何 N 轮，全写理由进 spec + 代码注释。

### 2. LLMIntentClassifier（LLM 兜底）

- 规则层未短路时调用，独立低温廉价模型，confidence<0.5 回退规则结果（CONTEXT.md 已定）。
- 不照搬 wali 三段式阈值/0.8 短路/二次提取全套（决策记录第 4 节"不 1:1 移植 wali"）。

### 3. Signal 提取（无副作用，指代消解 + 检索过滤）

- 无副作用正则提取：时间戳 / 实体 / 章节 / 比较，用于指代消解（"它/这个"→ 上文实体）与检索过滤（时间戳缩 chunk 时间范围）。
- 与 rag_rewrite LLM 改写边界：Signal 是无副作用的（纯正则，不调 LLM、不编造）；rewrite 是 LLM 改写（有副作用、调 LLM、可能编造）。两层正交，Signal 先于 rewrite 提取结构化线索，rewrite 再补语义改写。
- timeline_locate intent 用 Signal 时间戳缩检索范围（接 spec 04 ExecutionPolicy 留的接口位）。

### 4. 接 spec 04 ExecutionPolicy

- 分类器输出 intent → spec 04 取对应 ExecutionPolicy → 按参数走检索/生成。
- 替换 spec 04 A 段的占位分类（`isVideoOverviewQuestion` + scope）为真分类器。
- 短路 = 省 LLM 调用：规则层短路率 X% → 等价省 X% 的 LLM 分类调用（决策记录第 4 节，省 LLM 只作短路率派生结论，不单独跑端到端成本测）。

## User Stories

1. 作为项目维护者，我想要规则层覆盖全 intent taxonomy（不只 overview），以便每个问题都有前置分类。
2. 作为项目维护者，我想要关键词命中维度，以便"讲了什么/总结"这类明显 intent 快速短路。
3. 作为项目维护者，我想要 signal 模式维度（时间戳/比较/概览/闲聊），以便结构化线索辅助分类。
4. 作为项目维护者，我想要历史 intent 加权维度，以便同 intent 连续出现时提高置信度。
5. 作为项目维护者，我想要 confidence 达阈值短路、不调 LLM，以便省 LLM 分类调用。
6. 作为项目维护者，我想要规则层未短路时 LLM 兜底，以便覆盖规则层判不出的边界问题。
7. 作为项目维护者，我想要 LLM confidence<0.5 回退规则结果，以便 LLM 不确定时不硬猜。
8. 作为项目维护者，我想要无副作用 Signal 提取（时间戳/实体/章节/比较），以便指代消解与检索过滤有结构化线索。
9. 作为项目维护者，我想要 Signal 与 LLM rewrite 边界清晰（无副作用 vs 有副作用），以便面试能答清两层分工。
10. 作为项目维护者，我想要 timeline_locate intent 用时间戳 Signal 缩检索范围，以便"第15分钟"类问题不全文检索。
11. 作为项目维护者，我想要分类器输出 intent 接 spec 04 ExecutionPolicy，以便替换占位分类、走统一路由。
12. 作为项目维护者，我想要每阈值/维度选择理由写 audit trail，以便反 overclaim（决策记录第 4 节硬约束）。
13. 作为项目维护者，我想要固化 case 集（正例/反例/边界/0.79vs0.81 短路）跨规则层与 LLM 层跑准确率/召回/短路率/LLM 兜底命中率，以便验收有量化（决策记录第 4 节验收=仅分类层评测）。
14. 作为项目维护者，我想要共享 query 池复用 spec 01 的 `category` 黄金 intent 标签，以便不维护两套评测集。
15. 作为项目维护者，我想要省 LLM 调用只作短路率派生结论，以便不单独跑端到端成本测（决策记录第 4 节）。
16. 作为项目维护者，我想要 intent taxonomy 用 vid-lens 自己语义（不用 wali 的 DIAGNOSE/CONFIGURE），以便借鉴形态不照搬。

## Implementation Decisions

### 复用现有 seam，不重建

- `isVideoOverviewQuestion`（`chat_messages.go:63`）——扩成全 taxonomy 的关键词维度雏形，不另起。
- `ClassifyVideoAgentTemplate`（`video_agent.go:73`）——compare_topics template 是 topic_compare intent 的雏形参考，不接产品默认（agent 是实验路径）。
- Redis recent messages（`chat_memory.go`）——历史 intent 加权的数据源，复用解析。
- spec 01 共享 query 池——固化 case 集的数据来源，`category` = 黄金 intent。
- spec 04 ExecutionPolicy——分类器输出的消费方，替换占位。

### RuleIntentClassifier 形态

- 三维打分：关键词命中（含权重）+ signal 模式 + 历史 intent 加权。
- 输出 (intent, confidence)。confidence 达阈值短路。
- **阈值与权重留 audit trail**：本 spec 不写死数值，写"为何这么定"的理由框架，具体数值由实现会话在固化 case 集上调优后写理由（决策记录第 9 节第 3 条"具体权重数值留给 (A) spec 的 audit trail 写理由"）。

### LLMIntentClassifier 形态

- 独立低温廉价模型，输出 (intent, confidence)。
- confidence<0.5 回退规则结果。
- 不照搬 wali 三段式（决策记录第 4 节）。

### Signal 提取形态

- 纯正则，无 LLM 调用，无编造。
- 输出结构化线索：时间戳（毫秒区间）/ 实体 / 章节 / 比较标志。
- 用于指代消解（上文实体回指）+ 检索过滤（时间戳缩 chunk 范围）。
- 与 rag_rewrite 边界：Signal 无副作用先提，rewrite LLM 改写后补，两层正交。

### 接 spec 04

- 分类器输出 intent → spec 04 取 ExecutionPolicy → 走检索/生成。
- 替换 `prepareChatByMode` 的 `isVideoOverviewQuestion` + scope 占位为真分类器。
- timeline_locate 用 Signal 时间戳接 spec 04 留的接口位。

### 验收 = 仅分类层评测（决策记录第 4 节）

- 固化 case 集（正例/反例/边界/0.79vs0.81 短路）跨规则层与 LLM 层跑：准确率/召回/短路率/LLM 兜底命中率。
- 复用 spec 01 共享 query 池（`category` = 黄金 intent）。
- 省 LLM 调用 = 短路率派生结论（"规则层短路率 X% → 等价省 X% 的 LLM 分类调用"），不单独跑端到端成本测。

### 单一测试 seam

- 验收 seam：`internal/service` 的分类层行为测试。外部行为 = 固化 case 集跨规则层/LLM 层的分类结果。
- 复用 spec 01 共享 query 池作 case 来源。

## Testing Decisions

### 什么算好测试

- 只测外部行为：固化 case 集的分类准确率/召回/短路率/LLM 兜底命中率。
- 不测打分函数内部权重数值（那是 audit trail 调优产物）。
- 不测 Signal 正则实现细节（测提取结果对不对）。
- 不测端到端成本（省 LLM 是派生结论）。

### 测试模块

- `internal/service/intent_classifier_test.go`（新增）：固化 case 集（正例/反例/边界/0.79vs0.81 短路）跨规则层/LLM 层。
- `internal/service/signal_extract_test.go`（新增）：时间戳/实体/章节/比较的提取结果。

### Prior art

- `internal/service/chat_messages.go` 的 `isVideoOverviewQuestion` —— 关键词维度雏形。
- `internal/service/video_agent.go` 的 `ClassifyVideoAgentTemplate` —— 多 template 分类雏形。
- spec 01 共享 query 池 —— 固化 case 集数据来源。

## Out of Scope

- **不照搬 wali 全套**。借鉴级联形态，阈值/signal 维度/intent taxonomy 用 vid-lens 自己语义（决策记录第 4 节）。
- **不单独跑端到端成本测**。省 LLM 是短路率派生结论（决策记录第 4 节）。
- **不做 spec 04 ExecutionPolicy**。本 spec 只产 intent，消费方是 spec 04。
- **不重建 query rewrite**。Signal 是无副作用提取，与 LLM rewrite 正交，不替换 rewrite。
- **不接 agent 实验路径**。`ClassifyVideoAgentTemplate` 是参考，不进产品默认。
- **不写死阈值/权重数值**。写理由框架，数值由实现会话在 case 集调优后写 audit trail。

## Further Notes

### 与 00-refactor-decisions.md 的对齐

- (A) 意图识别前置路由（决策记录第 1 节）✅
- 借鉴形态不照搬 wali，intent taxonomy 用 vid-lens 语义（决策记录第 4 节）✅
- 每阈值/维度 audit trail（决策记录第 4 节硬约束）✅
- 验收 = 仅分类层评测，省 LLM 作短路率派生（决策记录第 4 节）✅
- (A)/(B) 共享 query 池，category 复用为 intent 标签（决策记录第 3 节）✅
- ExecutionPolicy 消掉 prepareChatByMode 硬编码（决策记录第 4 节）—— spec 04 已建占位，本 spec 替换为真分类器 ✅
- 规则层维度 = 关键词 + signal 模式 + 历史 intent 加权（决策记录第 9 节第 3 条）✅

### 数字占位符（本 spec 产出的简历可用数字）

> 实跑数字（2026-07-28，固化 case 集 = spec 01 train+dev split，16 条，
> test-sealed 不进分类评测；运行命令 `go test ./internal/service/ -run TestIntentClassifierEvalOnDataset -v`）：

- 规则层短路率 `18.8%`（固化 case 集跑出，非估算）
- 分类准确率 `93.8%`（规则层 + LLM 兜底，跨固化 case 集；规则层单层 87.5%）
- LLM 兜底命中率 `6.2%`（规则层未短路且规则层判错时 LLM 兜底命中黄金 intent 的比例；oracle 上界，非真实 LLM）
- 省 LLM 调用 `18.8%`（= 短路率派生结论）
- 固化 case 集规模 `16` 条（复用 spec 01 共享 query 池）

诚信约束（必带）：LLM 兜底用 oracle chat client（返回黄金 intent + 0.9 置信度）
测的是"LLM 兜底若可用能命中什么"的上界，非真实 LLM 数字；真实 LLM 效果需线上
对比测。短路率 18.8% 偏低因 spec 01 dataset 以 direct_qa 精确问答为主（13/16），
直接问答弱命中不短路是保守设计（避免误压其他 intent 的 LLM 兜底）。

### 简历允许写什么 / 禁止写什么（本 spec 对应 (A) bullet 的预演）

**允许写**：
- "规则层三维打分（关键词 + signal 模式 + 历史 intent 加权）短路、LLM 兜底级联分类，规则层短路率 `__`% 等价省 `__`% 的 LLM 分类调用；固化 case 集（正例/反例/边界/0.79vs0.81 短路）跨规则层/LLM 层跑准确率 `__`%。"
- "无副作用 Signal 提取（时间戳/实体/章节/比较，纯正则不调 LLM）用于指代消解与检索过滤，与 LLM rewrite 改写两层正交。"
- 具体数字（短路率、准确率、LLM 兜底命中率）—— **必须**在固化 case 集跑出后填，不许估算。

**禁止写**：
- "借鉴 wali 三段式分类" —— 不照搬，用 vid-lens 自己维度（决策记录第 4 节）。
- "省了 __ token 成本" —— 只能写"省 __% LLM 分类调用"（短路率派生），token 成本未测。
- "智能意图路由" —— 规则层是三维打分，不是智能，不能拔高。
- "我建了指代消解框架" —— Signal 是无副作用提取，不是框架。
- 任何未在固化 case 集跑出的数字。
