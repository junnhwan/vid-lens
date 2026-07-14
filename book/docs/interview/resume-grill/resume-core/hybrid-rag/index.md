# 简历主线四：Milvus + BM25 + RRF 混合检索

> 练法：先只看问题口述 30 秒，再展开答案；每题都要能说出当前边界。

## Q1：RAG 为什么使用 ASR 全文而不是摘要？

**直接回答：**
摘要会压缩细节，无法稳定回答原句、术语和局部事实。建索引时明确读取 VideoTranscription.Content，再切成 chunk。

**追问与防守：**
摘要是展示结果，不是当前检索知识源。

**项目证据：** `internal/service/rag_index.go:99-113`。

## Q2：转写文本怎么切 chunk？

**直接回答：**
BuildTaskIndex 使用配置的 chunk size 和 overlap 切分，并把 chunker strategy、version、size、overlap 写入索引状态。

**追问与防守：**
固定字符切分简单可复现，但不一定遵循语义边界。

**项目证据：** `internal/service/rag_index.go:110-136`。

## Q3：为什么同时写 MySQL 和 Milvus？

**直接回答：**
Milvus 保存向量和检索所需字段；MySQL 保存可管理的 VideoChunk 文本、索引和元数据，并支撑 Go 侧 BM25。

**追问与防守：**
这是双存储一致性问题，建索引失败需要状态标记和重建能力。

**项目证据：** `internal/service/rag_index.go:188-210`；`internal/vector/milvus.go:103-147`。

## Q4：Milvus 如何隔离用户和视频？

**直接回答：**
搜索 filter 同时约束 user_id、task_id 和 embedding_model，避免跨用户、跨视频或跨模型空间召回。

**追问与防守：**
应用层仍要先校验任务归属，不能只依赖向量库 filter。

**项目证据：** `internal/vector/milvus.go:240-268`；`internal/service/rag_index.go:91-97`。

## Q5：为什么还需要 BM25？

**直接回答：**
向量召回擅长语义相似，但专有名词、缩写、数字和原词匹配可能不稳定。BM25 根据词频、逆文档频率和长度归一化补充关键词信号。

**追问与防守：**
当前是在单视频 chunk 集合上用 Go 计算，不是 Elasticsearch/OpenSearch。

**项目证据：** `internal/repository/video_chunk.go:67-151`。

## Q6：中文 BM25 怎么分词？

**直接回答：**
Latin 文本按连续字母数字 token；连续汉字生成 2 到 4 字 n-gram，避免依赖 MySQL 环境特定的中文全文解析器。

**追问与防守：**
n-gram 可复现但会产生更多 token，也不是专业中文分词器。

**项目证据：** `internal/repository/video_chunk.go:182-235`。

## Q7：为什么不直接比较向量分和 BM25 分？

**直接回答：**
两路分数含义和尺度不同，直接加权容易被某一路数值范围支配。RRF 只使用排名，以 1/(k+rank) 累加，更容易融合异构召回。

**追问与防守：**
RRF 的 k 和 candidateK 仍需评估集调参。

**项目证据：** `internal/service/retrieval_fusion.go:14-82`。

## Q8：检索管道的完整调用顺序是什么？

**直接回答：**
对 query 分别执行 embedding/Milvus 和 BM25，先做单 query RRF，再跨 query 融合，之后可选扩展或 rerank，最后限制 topK。简历主卖点仍是稳定的向量+BM25+RRF。

**追问与防守：**
可选模块存在不代表所有部署都开启，回答要以配置为准。

**项目证据：** `internal/service/rag_pipeline.go:87-181`。

## Q9：引用片段怎么返回？

**直接回答：**
RetrievedChunk 保留 evidence_id、chunk_index、content 和来源；流式接口先 emit citations，再输出 answer，最后保存问答。

**追问与防守：**
引用提高可追溯性，不保证模型一定忠实引用。

**项目证据：** `internal/service/chat.go:45-65`；`internal/service/chat.go:449-493`。

## Q10：如何复盘当时检索到了什么？

**直接回答：**
保存 assistant message 时把 citations 序列化为 retrieval_snapshot，避免只在前端短暂展示。

**追问与防守：**
快照便于审计，但还需要数据保留、脱敏和版本策略。

**项目证据：** `internal/service/chat.go:364-397`；`internal/model/chat.go:18-27`。

## Q11：混合检索效果怎么评估？

**直接回答：**
不能只凭主观示例。应准备问题、相关 chunk 标注，比较 vector-only、BM25-only、hybrid 的 Recall@K、MRR、引用命中和延迟。项目已有 rag-eval 基础，但简历不要把尚未稳定的实验结果写成生产收益。

**追问与防守：**
离线检索指标不等于最终答案正确率，还需答案 groundedness 评估。

**项目证据：** `internal/service/rag_eval.go:1-180`；`cmd/rag-eval/main.go:1-220`。

## Q12：当前混合检索的最大限制是什么？

**直接回答：**
BM25 每次读取单任务 chunks 后在 Go 内计算，适合项目规模但不适合大语料；当前问答主要是单视频范围。未来可引入倒排索引、邻居扩展、稳定 rerank 和评估基线。

**追问与防守：**
未来方向必须明确说“计划”，不能冒充已完成事实。

**项目证据：** `internal/repository/video_chunk.go:79-151`；`internal/vector/milvus.go:248-268`。
