# RAG 与 Milvus 拷打

> 目标：把 RAG 讲成“检索数据源、索引状态、召回融合、引用留痕”的工程链路。不要把它说成泛泛的“接了向量数据库”。

### 1. RAG 为什么用 ASR 转写文本，不用 AI 摘要？

- 题目：RAG 数据源第一问。
- 面试官想听什么：摘要是二次压缩结果，不适合作为唯一知识源。
- 简答：摘要已经被 LLM 压缩过，可能丢细节或加入模型表达。RAG 应该尽量检索接近原始内容的文本，所以 VidLens 用 `video_transcriptions.content` 切 chunk，而不是用 summary。
- 深答：

  <details>
  <summary>展开深答</summary>

  视频问答的目标是让用户问视频里的具体内容。摘要适合快速浏览，但它不是完整事实来源。比如视频里某个细节、术语、时间点或例子没有进入摘要，后续 RAG 就永远召回不到。

  当前 `BuildTaskIndex` 会先查 task，再读取 transcription；如果 transcription 不存在或为空，就无法构建索引。然后用转写全文切 chunk、生成 embedding、保存 MySQL chunk 和 Milvus vector。这个选择也方便返回 citation，因为引用片段来自 ASR 文本，而不是模型摘要。
  </details>

- 延伸追问：
  - ASR 有错怎么办？
  - 摘要有没有用？
  - 能不能把字幕、标题、评论都放进 RAG？
- 项目证据：
  - `README.md:247` 明确 RAG 知识源是 ASR 转写全文。
  - `internal/service/rag_index.go:78` 构建索引读取 transcription。
  - `internal/service/rag_index.go:82` 转写为空则失败。
  - `docs/troubleshooting-and-interview-notes.md:542` 记录为什么不用 AI 总结做知识库。
- 当前边界：当前主要知识源是单视频 ASR 文本，不是多源知识库。

### 2. chunk size 和 overlap 怎么取舍？

- 题目：chunking 基础题。
- 面试官想听什么：太大太小的代价。
- 简答：chunk 太大，embedding 会压缩过多信息，检索不精准；chunk 太小，上下文不完整，数量变多，索引和检索成本上升。overlap 可以缓解句子被切断，但也会增加重复内容和向量数量。
- 深答：

  <details>
  <summary>展开深答</summary>

  VidLens 当前是按字符数切分转写文本，这是第一版简单可控的方案。它的优点是实现稳定，不依赖复杂中文分句或语义切分；缺点是可能切断一句话，或者把强相关上下文分散到相邻 chunk。

  面试里我会说当前先解决“能索引、能检索、能引用”的基础链路。后续如果要提升质量，可以做句子/段落感知切分、邻居 chunk 扩展、parent-child retrieval 或基于 ASR 时间戳切分。不要把当前固定切分吹成最优。
  </details>

- 延伸追问：
  - overlap 多大合适？
  - 长视频 chunk 很多怎么办？
  - ASR 分片和 RAG chunk 是一回事吗？
- 项目证据：
  - `internal/service/rag_index.go:89` `SplitTextIntoChunks` 切分 transcription。
  - `internal/service/rag_index.go:103` RAG index 记录 embedding model 和 dim。
  - `docs/troubleshooting-and-interview-notes.md:3000` 未来评估纯 vector、keyword、hybrid。
- 当前边界：当前没有语义切分和邻居 chunk 扩展。

### 3. Milvus collection 如何做用户和视频隔离？

- 题目：多租户和数据隔离题。
- 面试官想听什么：不能跨用户召回。
- 简答：Milvus vector 存储带 `user_id`、`task_id`、`chunk_id`、`chunk_index`、`embedding_model`、`content` 和 `embedding` 等字段。搜索时 filter 带 `user_id + task_id + embedding_model`，避免召回其他用户或其他视频的 chunk。
- 深答：

  <details>
  <summary>展开深答</summary>

  RAG 最怕数据串用户。VidLens 的检索是围绕单个视频 session 的，不是全站知识库。Milvus collection 里每条 vector 都带 user、task 和 embedding model，搜索时表达式按这几个字段过滤。这样即使 collection 是共享的，也不会把用户 A 的视频片段召回给用户 B。

  还要带 embedding model，是因为不同模型维度和向量空间不一定兼容。用户级 BYOK 允许不同用户配置不同 embedding provider/model，所以检索必须跟构建索引时的 model 对齐。
  </details>

- 延伸追问：
  - 为什么不每个用户一个 collection？
  - embedding 维度不一致怎么办？
  - task_id 过滤意味着不能跨视频问答吗？
- 项目证据：
  - `internal/vector/milvus.go:80` collection 字段包含 `user_id`。
  - `internal/vector/milvus.go:81` collection 字段包含 `task_id`。
  - `internal/vector/milvus.go:85` collection 字段包含 `embedding_model`。
  - `internal/vector/milvus.go:170` 搜索 filter 带 `user_id`。
  - `internal/vector/milvus.go:172` 搜索 filter 带 `embedding_model`。
- 当前边界：当前是单视频 RAG，不是跨视频知识库。

### 4. 为什么还要关键词召回？向量检索不够吗？

- 题目：检索质量题。
- 面试官想听什么：语义召回和精确术语召回互补。
- 简答：向量检索擅长语义相似，但对专有名词、缩写、数字、代码名等精确匹配不一定稳定。VidLens 在 Milvus vector candidates 外，又从 MySQL `video_chunks` 做 Go 侧 BM25 风格关键词召回，再融合结果。
- 深答：

  <details>
  <summary>展开深答</summary>

  第一版 RAG 只做向量 Top-K，对于“这段讲了什么”这种语义问题够用。但如果用户问一个很明确的术语、英文缩写或数字，embedding 可能不如关键词匹配稳定。旧版曾经做过 LIKE fallback，但完整问题字符串很难直接命中 chunk，所以后来升级成提取 query terms，Go 侧计算 BM25 风格分数。

  这个实现是符合当前数据规模的折中：单视频 chunk 数量有限，从 MySQL 拉出 task 范围的 chunks 后在 Go 里打分，方便测试，也不引入 Elasticsearch/OpenSearch。未来如果跨视频或 chunk 数量大，再换专门搜索引擎更合理。
  </details>

- 延伸追问：
  - 当前是不是 Elasticsearch？
  - 中文分词怎么处理？
  - BM25 风格和完整搜索引擎 BM25 有什么区别？
- 项目证据：
  - `internal/service/chat.go:184` 向量召回后合并关键词 chunk。
  - `internal/service/chat.go:260` 调用 `SearchByBM25`。
  - `internal/repository/video_chunk.go:47` Go 侧 BM25 风格召回入口。
  - `docs/troubleshooting-and-interview-notes.md:2937` 说明当前没有引入 Elasticsearch。
- 当前边界：这是轻量 Go 侧 BM25 风格实现，不是专业搜索引擎。

### 5. RRF 为什么比直接分数相加更稳？

- 题目：检索融合题。
- 面试官想听什么：不同召回源分数不可比。
- 简答：向量分数和 BM25 风格分数不是同一尺度，直接相加不稳定。RRF 用排名而不是绝对分数，分别看一个 chunk 在 vector 和 keyword 结果里的 rank，再做融合。
- 深答：

  <details>
  <summary>展开深答</summary>

  Milvus 的向量相似度和关键词 BM25 风格分数含义不同，一个来自向量空间距离或相似度，一个来自词频、逆文档频率和长度归一化。直接把 0.78 和 3.2 相加没有明确意义，也容易让某一路分数尺度压过另一路。

  RRF 的思路是只看排名：一个 chunk 在 vector 里排第 1，在 keyword 里排第 3，就比两个列表都靠后的 chunk 更值得进入上下文。VidLens 的 `FuseRetrievedChunks` 会记录 vector rank、keyword rank、source 和 rrf score，最后截取 topK 给 LLM。
  </details>

- 延伸追问：
  - RRF 的 k 怎么取？
  - 如果 keyword 结果质量差会不会污染？
  - 为什么不直接 rerank？
- 项目证据：
  - `internal/service/retrieval_fusion.go:14` 定义默认 RRF k。
  - `internal/service/retrieval_fusion.go:17` RRF 融合函数入口。
  - `internal/service/chat.go:275` 返回融合后的 chunks。
  - `docs/troubleshooting-and-interview-notes.md:2919` 说明分数尺度不同，不能直接相加。
- 当前边界：当前没有接 rerank 模型，rerank 是后续评估稳定后的优化。

### 6. citation 和 retrieval snapshot 有什么价值？

- 题目：RAG 可解释性题。
- 面试官想听什么：回答要能追溯上下文。
- 简答：citation 让用户知道答案来自哪些转写片段；retrieval snapshot 把当次检索结果随 chat message 保存，后续排查模型幻觉、检索失败或答案争议时可以回看当时给了 LLM 什么上下文。
- 深答：

  <details>
  <summary>展开深答</summary>

  RAG 不是把问题直接丢给 LLM。后端先检索 citations，再拼接 prompt。模型答错时，要区分是检索没捞到正确片段，还是模型没有用好片段。如果没有 retrieval snapshot，事后只能猜。

  当前 `saveChatExchange` 保存 user message、assistant message，并把 citations 序列化到 `retrieval_snapshot`。这对简历项目很重要，因为它体现了可观察性和调试意识，而不只是“接了一个聊天接口”。
  </details>

- 延伸追问：
  - snapshot 会不会占空间？
  - citation 返回给前端有什么用？
  - 模型回答和 citation 不一致怎么办？
- 项目证据：
  - `internal/service/chat.go:197` 构造 RAG messages。
  - `internal/service/chat.go:208` 保存问答交换。
  - `internal/service/chat.go:228` 保存 retrieval snapshot。
  - `internal/model/chat.go:24` chat message 模型包含 retrieval snapshot。
- 当前边界：当前没有自动 faithfulness 评分，主要保存证据便于排查。

### 7. SSE 是真正 token streaming 吗？

- 题目：流式输出边界题。
- 面试官想听什么：能区分接口 SSE 和 provider streaming。
- 简答：当前 OpenAI-compatible client 支持 provider 级 `StreamChat`，会用 `stream=true` 解析 SSE delta；但如果某个 provider/client 不支持 streaming，服务层会 fallback 到完整回答后按块输出。所以不能笼统说所有 provider 都是真 token streaming。
- 深答：

  <details>
  <summary>展开深答</summary>

  旧版只是 `Ask` 等完整 LLM 响应，再按 80 rune 切块通过 SSE 发给前端，这只能算接口形态是流式，不是真正 token streaming。后续在 AI client 层增加了可选 `StreamChat` 接口，支持的 OpenAI-compatible client 才会逐 delta 输出。

  服务层保留 fallback 是为了兼容不支持 streaming 的 provider。面试里我会这样说：VidLens 已经支持 provider 级 streaming 的路径，但不是所有 provider 都保证支持；不支持时仍会完整生成后切块输出。
  </details>

- 延伸追问：
  - fallback 会不会误导前端？
  - streaming 时 retrieval snapshot 怎么保存？
  - token 级审计有做吗？
- 项目证据：
  - `internal/service/chat.go:285` `AskStream` 入口。
  - `internal/service/chat.go:300` 支持 streaming client 时调用 `StreamChat`。
  - `internal/service/chat.go:312` 不支持时 fallback 切块。
  - `docs/troubleshooting-and-interview-notes.md:3542` 说明 provider streaming 与 fallback。
- 当前边界：当前没有在 SSE event 里标注 `stream_mode`，也没有 token 级计费。

### 8. RAG 质量怎么评估和优化？

- 题目：从能跑到好用。
- 面试官想听什么：先评估检索，再谈 rerank 等优化。
- 简答：RAG 不能只靠肉眼看回答。VidLens 已经有离线评估核心，用 question 和 expected chunk keywords 计算 Recall@K、MRR、无结果率、检索耗时和来源占比。后续优化顺序是评估集、chunking、邻居扩展、query rewrite，再考虑 rerank。
- 深答：

  <details>
  <summary>展开深答</summary>

  RAG 的第一步不是“让模型答得像”，而是确认正确上下文有没有被检索出来。如果检索没命中，LLM 可能凭常识编一个看似合理的答案，掩盖问题。

  当前评估核心不依赖真实 LLM，也不需要真实用户视频内容。case 里写问题和期望命中的关键词，评估 retrieval topK 是否包含这些关键词，再算 Recall@K、MRR、no-result rate 和 latency。这种方式轻量但可复现。未来真正优化要用脱敏真实 case，比较纯向量、关键词和 hybrid，然后再决定是否接 rerank。
  </details>

- 延伸追问：
  - 为什么不用答案准确率直接评估？
  - expected keywords 会不会太粗？
  - rerank 放在哪个阶段？
- 项目证据：
  - `docs/troubleshooting-and-interview-notes.md:3285` 记录 RAG 离线评估背景。
  - `docs/troubleshooting-and-interview-notes.md:3331` 记录 EvaluateRAGRetrieval。
  - `docs/troubleshooting-and-interview-notes.md:3379` 口语化说明评估指标。
  - `docs/troubleshooting-and-interview-notes.md:3387` 说明 expected keywords 的边界。
- 当前边界：评估框架有了，但还需要更真实、脱敏的评估集。

